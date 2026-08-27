package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestResolveTokenPrecedenceAndIgnoresOldPasscode(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("WAYMARK_CONFIG_DIR", dir)
	if err := saveConfig(config{URL: "https://saved.example", Token: "saved-token"}); err != nil {
		t.Fatal(err)
	}
	t.Setenv("WAYMARK_TOKEN", "env-token")
	server, token, _, source, err := resolve("")
	if err != nil {
		t.Fatal(err)
	}
	if server != "https://saved.example" || token != "env-token" || source != "env" {
		t.Fatalf("resolve() = %q, %q, %q", server, token, source)
	}
	t.Setenv("WAYMARK_TOKEN", "")
	_, token, _, source, err = resolve("")
	if err != nil || token != "saved-token" || source != "config" {
		t.Fatalf("saved token = %q, %q, %v", token, source, err)
	}

	oldDir := t.TempDir()
	t.Setenv("WAYMARK_CONFIG_DIR", oldDir)
	if err := os.WriteFile(filepath.Join(oldDir, "config.json"), []byte(`{"url":"https://old.example","passcode":"old-secret"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	server, token, _, _, err = resolve("")
	if err != nil || server != "https://old.example" || token != "" {
		t.Fatalf("old config = %q, %q, %v", server, token, err)
	}
}

func TestLoginWithDeviceAgainstHTTPServer(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("WAYMARK_CONFIG_DIR", dir)
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/.well-known/waymark":
			json.NewEncoder(w).Encode(discoveryResponse{ControlURL: server.URL, ContentURL: server.URL, DeviceAuthorization: true})
		case "/api/auth/device/code":
			w.WriteHeader(http.StatusCreated)
			json.NewEncoder(w).Encode(deviceCodeResponse{DeviceCode: "device-code", UserCode: "ABCD-EFGH", VerificationURI: server.URL + "/activate", VerificationURIComplete: server.URL + "/activate?code=ABCD-EFGH", ExpiresIn: 30, Interval: 1})
		case "/api/auth/device/token":
			json.NewEncoder(w).Encode(map[string]any{"access_token": "waymark_test.secret", "token_type": "Bearer", "expires_in": 3600})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)
	if err := loginWithDevice(server.URL, "test device", false); err != nil {
		t.Fatal(err)
	}
	cfg, err := loadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.URL != server.URL || cfg.Token != "waymark_test.secret" {
		t.Fatalf("saved config = %#v", cfg)
	}
}

func TestNormalizeServerURLAcceptsLocalhostSubdomain(t *testing.T) {
	if _, err := normalizeServerURL("http://control.localhost:18080"); err != nil {
		t.Fatal(err)
	}
}

func TestUpdateSendsIfUpdatedAtPrecondition(t *testing.T) {
	bodyPath := filepath.Join(t.TempDir(), "body.html")
	body := `<!DOCTYPE html><html><head><meta name="viewport" content="width=device-width"><title>Updated</title><style>body{}</style></head><body><p>updated</p></body></html>`
	if err := os.WriteFile(bodyPath, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("WAYMARK_TOKEN", "test-token")
	const expected = "2026-08-18T12:00:00Z"
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut || r.URL.Path != "/api/pages/page1" {
			t.Fatalf("request = %s %s", r.Method, r.URL.Path)
		}
		var request map[string]any
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatal(err)
		}
		if request["if_updated_at"] != expected {
			t.Fatalf("if_updated_at = %#v, want %q", request["if_updated_at"], expected)
		}
		if request["raw"] != true {
			t.Fatalf("raw = %#v, want true when replacement HTML is sent", request["raw"])
		}
		if request["html"] != body {
			t.Fatalf("html = %#v", request["html"])
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"id": "page1", "url": server.URL + "/p/page1", "title": "Updated",
			"updated_at": "2026-08-18T12:01:00Z",
		})
	}))
	t.Cleanup(server.Close)

	if err := cmdUpdate([]string{"--server", server.URL, "--if-updated-at", expected, "page1", bodyPath}); err != nil {
		t.Fatal(err)
	}
}

func TestDeviceDiscovery404RequestsDeploymentUpgrade(t *testing.T) {
	t.Setenv("WAYMARK_CONFIG_DIR", t.TempDir())
	server := httptest.NewServer(http.NotFoundHandler())
	t.Cleanup(server.Close)

	err := loginWithDevice(server.URL, "test device", false)
	if err == nil {
		t.Fatal("loginWithDevice() returned no error for missing discovery")
	}
	if !strings.Contains(err.Error(), "server returned HTTP 404") || !strings.Contains(err.Error(), "upgrade the Waymark deployment") {
		t.Fatalf("404 error missing upgrade guidance: %v", err)
	}
}

func TestDeviceDiscoveryTransportFailurePreservesCause(t *testing.T) {
	t.Setenv("WAYMARK_CONFIG_DIR", t.TempDir())
	server := httptest.NewServer(http.NotFoundHandler())
	serverURL := server.URL
	server.Close()

	err := loginWithDevice(serverURL, "test device", false)
	if err == nil {
		t.Fatal("loginWithDevice() returned no error for transport failure")
	}
	if !strings.Contains(err.Error(), "device discovery failed") || !strings.Contains(err.Error(), "upgrade the Waymark deployment") {
		t.Fatalf("transport error missing context: %v", err)
	}
}

func TestLoginRejectsRemovedLegacyFlags(t *testing.T) {
	for _, arg := range []string{"--legacy-passcode", "--force"} {
		t.Run(arg, func(t *testing.T) {
			if err := cmdLogin([]string{arg}); err == nil {
				t.Fatalf("cmdLogin(%q) succeeded; want removed flag rejected", arg)
			}
		})
	}
}

func TestCreateAlwaysSendsRawTrueAndRejectsRawFlag(t *testing.T) {
	htmlPath := filepath.Join(t.TempDir(), "page.html")
	html := `<!DOCTYPE html><html><head><meta name="viewport" content="width=device-width"><title>Report</title><style>body{}</style></head><body><p>Report</p></body></html>`
	if err := os.WriteFile(htmlPath, []byte(html), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("WAYMARK_TOKEN", "test-token")
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/pages" {
			t.Fatalf("request = %s %s", r.Method, r.URL.Path)
		}
		var request map[string]any
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatal(err)
		}
		if request["raw"] != true {
			t.Fatalf("raw = %#v, want true", request["raw"])
		}
		if request["title"] != "Report" {
			t.Fatalf("title = %#v", request["title"])
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"id": "page1", "url": server.URL + "/p/page1", "title": "Report", "raw": true,
			"created_at": "2026-08-18T12:00:00Z", "updated_at": "2026-08-18T12:00:00Z",
		})
	}))
	t.Cleanup(server.Close)
	if err := cmdCreate([]string{"--server", server.URL, "--title", "Report", htmlPath}); err != nil {
		t.Fatal(err)
	}
	if err := cmdCreate([]string{"--server", server.URL, "--title", "Report", "--raw", htmlPath}); err == nil {
		t.Fatal("cmdCreate(--raw) succeeded; want unknown flag")
	}
}

func TestCreateAndUpdateRejectInvalidRawHTMLLocally(t *testing.T) {
	path := filepath.Join(t.TempDir(), "fragment.html")
	if err := os.WriteFile(path, []byte("<p>fragment</p>"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{
		{"--server", "http://127.0.0.1:1", "--title", "Bad", path},
		{"--server", "http://127.0.0.1:1", "page1", path},
	} {
		err := func() error {
			if len(args) == 4 && args[2] == "page1" {
				return cmdUpdate(args)
			}
			return cmdCreate(args)
		}()
		if err == nil || !strings.Contains(err.Error(), "fragment.html is not a complete self-contained HTML document") {
			t.Fatalf("args %v error = %v", args, err)
		}
	}
}

func TestUpdateMetadataOmitsRawAndReplacementSendsRawTrue(t *testing.T) {
	t.Setenv("WAYMARK_TOKEN", "test-token")
	var got map[string]any
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Fatal(err)
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"id": "page1", "url": server.URL + "/p/page1", "title": "Renamed",
			"updated_at": "2026-08-18T12:01:00Z",
		})
	}))
	t.Cleanup(server.Close)
	if err := cmdUpdate([]string{"--server", server.URL, "--title", "Renamed", "page1"}); err != nil {
		t.Fatal(err)
	}
	if _, ok := got["raw"]; ok {
		t.Fatalf("metadata-only update sent raw = %#v", got["raw"])
	}
	if _, ok := got["html"]; ok {
		t.Fatalf("metadata-only update sent html = %#v", got["html"])
	}
}
