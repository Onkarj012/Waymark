package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

const doctorTestToken = "waymark_doctor.private-test-token"

type doctorFixture struct {
	control  *httptest.Server
	content  *httptest.Server
	info     authInfoResponse
	override func(http.ResponseWriter, *http.Request, bool) bool
}

func newDoctorFixture(t *testing.T) *doctorFixture {
	t.Helper()
	t.Setenv("WAYMARK_CONFIG_DIR", t.TempDir())
	t.Setenv("WAYMARK_URL", "")
	t.Setenv("WAYMARK_TOKEN", doctorTestToken)
	t.Setenv("WAYMARK_ADMIN_PASSCODE", "doctor-private-admin-secret")
	expires := time.Now().UTC().Add(30 * 24 * time.Hour).Truncate(time.Second)
	fixture := &doctorFixture{info: authInfoResponse{
		OK: true, CredentialType: "device_token", Scopes: []string{"pages:read"}, ExpiresAt: &expires, Label: doctorTestToken,
	}}
	handler := func(control bool) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodGet {
				t.Errorf("doctor made a non-read request: %s %s", r.Method, r.URL.Path)
			}
			if r.UserAgent() != "Waymark/"+version+" (self-hosted publishing CLI)" {
				t.Errorf("User-Agent = %q", r.UserAgent())
			}
			wantAuth := ""
			if control && r.URL.Path == "/api/auth" {
				wantAuth = "Bearer " + doctorTestToken
			}
			if r.Header.Get("Authorization") != wantAuth {
				t.Errorf("unexpected credential presence on control=%t path=%s", control, r.URL.Path)
			}
			if fixture.override != nil && fixture.override(w, r, control) {
				return
			}
			switch {
			case r.URL.Path == "/.well-known/waymark":
				json.NewEncoder(w).Encode(discoveryResponse{ControlURL: fixture.control.URL, ContentURL: fixture.content.URL, DeviceAuthorization: true})
			case r.URL.Path == "/healthz":
				io.WriteString(w, "ok")
			case control && r.URL.Path == "/api/auth":
				json.NewEncoder(w).Encode(fixture.info)
			case !control && r.URL.Path == "/api/pages", control && r.URL.Path == "/p/doctorcheck1":
				w.WriteHeader(http.StatusMisdirectedRequest)
			default:
				t.Errorf("unexpected diagnostic route on control=%t: %s", control, r.URL.Path)
				http.NotFound(w, r)
			}
		})
	}
	fixture.control = httptest.NewServer(handler(true))
	fixture.content = httptest.NewServer(handler(false))
	t.Cleanup(fixture.control.Close)
	t.Cleanup(fixture.content.Close)
	return fixture
}

func runDoctor(t *testing.T, args ...string) (string, error) {
	t.Helper()
	output, err := os.CreateTemp(t.TempDir(), "doctor-output")
	if err != nil {
		t.Fatal(err)
	}
	defer output.Close()
	previous := os.Stdout
	os.Stdout = output
	defer func() { os.Stdout = previous }()
	commandErr := cmdDoctor(args)
	if _, err := output.Seek(0, io.SeekStart); err != nil {
		t.Fatal(err)
	}
	data, err := io.ReadAll(output)
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{doctorTestToken, "doctor-private-admin-secret"} {
		if strings.Contains(string(data), secret) || commandErr != nil && strings.Contains(commandErr.Error(), secret) {
			t.Fatal("doctor disclosed a credential or admin secret")
		}
	}
	return string(data), commandErr
}

func decodeDoctorReport(t *testing.T, output string) doctorReport {
	t.Helper()
	var report doctorReport
	if err := json.Unmarshal([]byte(output), &report); err != nil {
		t.Fatalf("invalid report: %v\n%s", err, output)
	}
	return report
}

func requireDoctorCheck(t *testing.T, report doctorReport, name, status string) {
	t.Helper()
	for _, check := range report.Checks {
		if check.Name == name {
			if check.Status != status {
				t.Fatalf("check %s = %s, want %s: %s", name, check.Status, status, check.Message)
			}
			return
		}
	}
	t.Fatalf("missing check %s", name)
}

func TestDoctorHealthyJSONIsReadOnly(t *testing.T) {
	fixture := newDoctorFixture(t)
	if err := saveConfig(config{URL: fixture.control.URL, Token: doctorTestToken}); err != nil {
		t.Fatal(err)
	}
	t.Setenv("WAYMARK_TOKEN", "")
	before, err := os.ReadFile(configPath())
	if err != nil {
		t.Fatal(err)
	}
	output, err := runDoctor(t, "--json")
	if err != nil {
		t.Fatal(err)
	}
	report := decodeDoctorReport(t, output)
	if !report.OK || report.CLIVersion != version || report.Credential == nil || report.Credential.Type != "device_token" || !report.Credential.ExpiresAt.Equal(*fixture.info.ExpiresAt) {
		t.Fatalf("unexpected report: %s", output)
	}
	for _, name := range []string{"configuration", "discovery", "origins", "authentication", "credential-expiry", "control-health", "content-health", "control-discovery", "content-discovery", "content-rejects-api", "control-rejects-pages"} {
		requireDoctorCheck(t, report, name, "pass")
	}
	requireDoctorCheck(t, report, "site-isolation", "warn")
	after, err := os.ReadFile(configPath())
	if err != nil || string(after) != string(before) {
		t.Fatal("doctor changed saved configuration")
	}
}

func TestDoctorHumanOutput(t *testing.T) {
	fixture := newDoctorFixture(t)
	output, err := runDoctor(t, "--server", fixture.control.URL)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"CLI " + version, "PASS authentication", "WARN site-isolation", "same-site", "Scoped credential expires: " + fixture.info.ExpiresAt.Format(time.RFC3339)} {
		if !strings.Contains(output, want) {
			t.Errorf("output missing %q: %s", want, output)
		}
	}
}

func TestDoctorAuthFailuresAndExpiry(t *testing.T) {
	for _, name := range []string{"unauthorized", "missing expiry", "expired", "expiring soon", "not scoped", "missing read scope", "not ok", "malformed JSON"} {
		t.Run(name, func(t *testing.T) {
			fixture := newDoctorFixture(t)
			wantCheck, wantStatus := "authentication", "fail"
			switch name {
			case "unauthorized", "malformed JSON":
				fixture.override = func(w http.ResponseWriter, r *http.Request, control bool) bool {
					if control && r.URL.Path == "/api/auth" {
						if name == "unauthorized" {
							w.WriteHeader(http.StatusUnauthorized)
						}
						io.WriteString(w, doctorTestToken)
						return true
					}
					return false
				}
			case "missing expiry":
				fixture.info.ExpiresAt = nil
			case "expired", "expiring soon":
				expires := time.Now().Add(-time.Hour)
				if name == "expiring soon" {
					expires = time.Now().Add(time.Hour)
					wantStatus = "warn"
				}
				fixture.info.ExpiresAt = &expires
				wantCheck = "credential-expiry"
			case "not scoped":
				fixture.info.CredentialType = doctorTestToken
			case "missing read scope":
				fixture.info.Scopes = []string{"pages:write"}
			case "not ok":
				fixture.info.OK = false
			}
			output, err := runDoctor(t, "--server", fixture.control.URL, "--json")
			if (err == nil) != (wantStatus == "warn") {
				t.Fatalf("doctor error = %v, want status %s", err, wantStatus)
			}
			report := decodeDoctorReport(t, output)
			if report.OK != (err == nil) {
				t.Fatalf("report success disagrees with exit error: %s", output)
			}
			requireDoctorCheck(t, report, wantCheck, wantStatus)
			requireDoctorCheck(t, report, "content-health", "pass")
			requireDoctorCheck(t, report, "control-rejects-pages", "pass")
		})
	}
}

func TestDoctorHostRoutingAndHealthFailures(t *testing.T) {
	for _, test := range []struct {
		name    string
		path    string
		control bool
		status  int
	}{
		{"content-health", "/healthz", false, 503},
		{"control-health", "/healthz", true, 503},
		{"content-rejects-api", "/api/pages", false, 401},
		{"content-rejects-api", "/api/pages", false, 404},
		{"control-rejects-pages", "/p/doctorcheck1", true, 404},
		{"control-rejects-pages", "/p/doctorcheck1", true, 200},
	} {
		t.Run(test.name+http.StatusText(test.status), func(t *testing.T) {
			fixture := newDoctorFixture(t)
			fixture.override = func(w http.ResponseWriter, r *http.Request, control bool) bool {
				if r.URL.Path == test.path && control == test.control {
					w.WriteHeader(test.status)
					io.WriteString(w, doctorTestToken)
					return true
				}
				return false
			}
			output, err := runDoctor(t, "--server", fixture.control.URL, "--json")
			if err == nil {
				t.Fatal("doctor accepted a failing probe")
			}
			requireDoctorCheck(t, decodeDoctorReport(t, output), test.name, "fail")
		})
	}
}

func TestDoctorNeverFollowsAuthRedirects(t *testing.T) {
	for _, sameOrigin := range []bool{false, true} {
		t.Run(map[bool]string{true: "same-origin", false: "cross-origin"}[sameOrigin], func(t *testing.T) {
			fixture := newDoctorFixture(t)
			var followed atomic.Int32
			destination := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				followed.Add(1)
				if r.Header.Get("Authorization") != "" {
					t.Error("credential forwarded to redirect destination")
				}
			}))
			t.Cleanup(destination.Close)
			fixture.override = func(w http.ResponseWriter, r *http.Request, control bool) bool {
				if r.URL.Path == "/redirected" {
					followed.Add(1)
					return true
				}
				if control && r.URL.Path == "/api/auth" {
					target := destination.URL
					if sameOrigin {
						target = fixture.control.URL + "/redirected"
					}
					http.Redirect(w, r, target, http.StatusTemporaryRedirect)
					return true
				}
				return false
			}
			output, err := runDoctor(t, "--server", fixture.control.URL, "--json")
			if err == nil || followed.Load() != 0 {
				t.Fatalf("redirect followed %d times; error = %v", followed.Load(), err)
			}
			requireDoctorCheck(t, decodeDoctorReport(t, output), "authentication", "fail")
		})
	}
}

func TestDoctorRejectsUnsafeOrInconsistentDiscovery(t *testing.T) {
	for _, name := range []string{"same origin", "unsafe content", "unsafe control", "wrong control", "content disagreement", "malformed discovery"} {
		t.Run(name, func(t *testing.T) {
			fixture := newDoctorFixture(t)
			fixture.override = func(w http.ResponseWriter, r *http.Request, control bool) bool {
				if r.URL.Path == "/api/pages" || r.URL.Path == "/p/doctorcheck1" {
					w.WriteHeader(http.StatusMisdirectedRequest)
					return true
				}
				if r.URL.Path != "/.well-known/waymark" {
					return false
				}
				discovery := discoveryResponse{ControlURL: fixture.control.URL, ContentURL: fixture.content.URL}
				switch name {
				case "same origin":
					discovery.ContentURL = fixture.control.URL
				case "unsafe content":
					discovery.ContentURL = "http://remote.example"
				case "unsafe control":
					discovery.ControlURL = "https://user:" + doctorTestToken + "@remote.example"
				case "wrong control":
					discovery.ControlURL, discovery.ContentURL = fixture.content.URL, fixture.control.URL
				case "content disagreement":
					if !control {
						discovery.ControlURL = fixture.content.URL
					}
				case "malformed discovery":
					io.WriteString(w, doctorTestToken)
					return true
				}
				json.NewEncoder(w).Encode(discovery)
				return true
			}
			output, err := runDoctor(t, "--server", fixture.control.URL, "--json")
			if err == nil || decodeDoctorReport(t, output).OK {
				t.Fatal("doctor accepted invalid discovery")
			}
		})
	}
}

func TestDoctorNeverFollowsDiscoveryRedirects(t *testing.T) {
	fixture := newDoctorFixture(t)
	var followed atomic.Int32
	destination := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		followed.Add(1)
	}))
	t.Cleanup(destination.Close)
	fixture.override = func(w http.ResponseWriter, r *http.Request, control bool) bool {
		if r.URL.Path == "/.well-known/waymark" {
			http.Redirect(w, r, destination.URL, http.StatusFound)
			return true
		}
		return false
	}
	output, err := runDoctor(t, "--server", fixture.control.URL, "--json")
	if err == nil || followed.Load() != 0 {
		t.Fatalf("discovery redirect followed %d times; error = %v", followed.Load(), err)
	}
	report := decodeDoctorReport(t, output)
	requireDoctorCheck(t, report, "discovery", "fail")
	requireDoctorCheck(t, report, "authentication", "pass")
}

func TestDoctorConnectionFailure(t *testing.T) {
	fixture := newDoctorFixture(t)
	fixture.control.Close()
	output, err := runDoctor(t, "--server", fixture.control.URL, "--json")
	if err == nil {
		t.Fatal("doctor accepted an unreachable control origin")
	}
	report := decodeDoctorReport(t, output)
	requireDoctorCheck(t, report, "discovery", "fail")
	requireDoctorCheck(t, report, "authentication", "fail")
}

func TestDoctorConfigurationFailuresAreRedactedAndReadOnly(t *testing.T) {
	for _, name := range []string{"missing server", "missing token", "unsafe URL", "malformed config", "wrong saved origin"} {
		t.Run(name, func(t *testing.T) {
			t.Setenv("WAYMARK_CONFIG_DIR", t.TempDir())
			t.Setenv("WAYMARK_URL", "")
			t.Setenv("WAYMARK_TOKEN", "")
			args := []string{"--json"}
			switch name {
			case "missing token":
				t.Setenv("WAYMARK_URL", "https://control.example")
			case "unsafe URL":
				t.Setenv("WAYMARK_URL", "https://user:"+doctorTestToken+"@control.example")
			case "malformed config":
				if err := os.WriteFile(configPath(), []byte(`{"token":"`+doctorTestToken+`", invalid}`), 0o600); err != nil {
					t.Fatal(err)
				}
			case "wrong saved origin":
				if err := saveConfig(config{URL: "https://saved.example", Token: doctorTestToken}); err != nil {
					t.Fatal(err)
				}
				args = append(args, "--server", "https://other.example")
			}
			before, _ := os.ReadFile(configPath())
			output, err := runDoctor(t, args...)
			if err == nil {
				t.Fatal("doctor accepted invalid configuration")
			}
			report := decodeDoctorReport(t, output)
			requireDoctorCheck(t, report, "configuration", "fail")
			if report.CLIVersion != version || report.OK {
				t.Fatalf("unexpected report: %s", output)
			}
			after, _ := os.ReadFile(configPath())
			if string(before) != string(after) {
				t.Fatal("doctor changed invalid configuration")
			}
			entries, err := os.ReadDir(filepath.Dir(configPath()))
			if err != nil || len(entries) > 1 {
				t.Fatalf("unexpected configuration writes: %v, %v", entries, err)
			}
		})
	}
}

func TestDoctorRejectsPositionalArguments(t *testing.T) {
	if err := cmdDoctor([]string{"unexpected", "--json"}); err == nil {
		t.Fatal("doctor accepted positional arguments")
	}
}

func TestDoctorSameOrigin(t *testing.T) {
	for _, test := range []struct {
		first  string
		second string
		want   bool
	}{
		{"https://CONTROL.example", "https://control.example:443", true},
		{"http://localhost", "http://localhost:80", true},
		{"http://localhost:8080", "http://localhost:8081", false},
		{"http://localhost", "https://localhost", false},
		{"https://control.example", "https://pages.example", false},
	} {
		if got := doctorSameOrigin(test.first, test.second); got != test.want {
			t.Errorf("doctorSameOrigin(%q, %q) = %t, want %t", test.first, test.second, got, test.want)
		}
	}
}
