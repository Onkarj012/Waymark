package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type doctorCheck struct {
	Name    string `json:"name"`
	Status  string `json:"status"`
	Message string `json:"message"`
}

type doctorCredential struct {
	Type      string     `json:"type"`
	ExpiresAt *time.Time `json:"expires_at"`
}

type doctorReport struct {
	CLIVersion string            `json:"cli_version"`
	OK         bool              `json:"ok"`
	Credential *doctorCredential `json:"credential,omitempty"`
	Checks     []doctorCheck     `json:"checks"`
}

func cmdDoctor(args []string) error {
	fs := flag.NewFlagSet("doctor", flag.ContinueOnError)
	server := commonFlags(fs)
	jsonOut := fs.Bool("json", false, "print diagnostics as JSON")
	if err := parse(fs, args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return errors.New("doctor does not accept positional arguments; put flags before positional arguments")
	}
	report := doctorReport{CLIVersion: version, OK: true, Checks: []doctorCheck{}}
	report.inspectSkills()
	configured, err := newClient(server)
	if err != nil {
		report.add("configuration", "fail", "Could not load a configured server and credential; check the server URL, login, and configuration file.")
	} else {
		report.add("configuration", "pass", "Configured server and credential loaded.")
		report.inspect(configured, safeHTTPClient(15*time.Second))
	}
	if *jsonOut {
		if err := printJSON(report); err != nil {
			return err
		}
	} else {
		fmt.Printf("Waymark doctor (CLI %s)\n", report.CLIVersion)
		for _, check := range report.Checks {
			fmt.Printf("%s %s: %s\n", strings.ToUpper(check.Status), check.Name, check.Message)
		}
		if report.Credential != nil && report.Credential.ExpiresAt != nil {
			fmt.Printf("Scoped credential expires: %s\n", report.Credential.ExpiresAt.UTC().Format(time.RFC3339))
		}
	}
	if !report.OK {
		return errors.New("doctor found failed checks")
	}
	return nil
}

func (report *doctorReport) inspectSkills() {
	foundCopy := false
	for _, agentClient := range []string{"agents", "codex", "claude"} {
		destination, err := skillDestination(agentClient)
		if err != nil {
			continue
		}
		info, err := os.Lstat(destination)
		if err != nil {
			continue
		}
		entrypoint, err := os.Stat(filepath.Join(destination, "SKILL.md"))
		if err != nil || !entrypoint.Mode().IsRegular() {
			continue
		}
		if info.Mode()&os.ModeSymlink != 0 {
			report.add("agent-skill", "pass", "An agent skill is linked to a local source directory.")
			return
		}
		foundCopy = true
	}
	if foundCopy {
		report.add("agent-skill", "warn", "Installed skill copies may drift from this checkout; inspect them before replacing them with a source link.")
		return
	}
	report.add("agent-skill", "warn", "No installed skill found; run `waymark skill install --source <checkout>/.skills/waymark`.")
}

func (report *doctorReport) add(name, status, message string) {
	report.Checks = append(report.Checks, doctorCheck{Name: name, Status: status, Message: message})
	if status == "fail" {
		report.OK = false
	}
}

func (report *doctorReport) inspect(configured *client, transport *http.Client) {
	discovery, err := discover(configured.base)
	if err != nil {
		report.add("discovery", "fail", "Discovery request failed or returned an invalid response.")
		report.inspectAuth(configured, transport)
		return
	}
	control, controlErr := normalizeServerURL(discovery.ControlURL)
	content, contentErr := normalizeServerURL(discovery.ContentURL)
	if controlErr != nil || contentErr != nil {
		report.add("discovery", "fail", "Discovery must advertise safe control and content origins.")
		return
	}
	report.add("discovery", "pass", "Control and content origins discovered.")
	if doctorSameOrigin(control, content) {
		report.add("origins", "fail", "Control and content must use distinct origins.")
	} else {
		report.add("origins", "pass", "Control and content use distinct origins.")
		report.add("site-isolation", "warn", "Distinct origins can still be same-site; use separate registrable domains when site isolation is required.")
	}
	if !doctorSameOrigin(configured.base, control) {
		report.add("authentication", "fail", "Configured server is not the discovered control origin; credential was not forwarded.")
	} else {
		report.inspectAuth(configured, transport)
	}
	for _, origin := range []struct {
		name string
		base string
	}{{"control", control}, {"content", content}} {
		if err := doctorGet(transport, origin.base, "/healthz", "", http.StatusOK, nil); err != nil {
			report.add(origin.name+"-health", "fail", err.Error())
		} else {
			report.add(origin.name+"-health", "pass", "GET /healthz returned HTTP 200.")
		}
		advertised, err := discover(origin.base)
		if err != nil {
			report.add(origin.name+"-discovery", "fail", "Discovery request failed or returned an invalid response.")
		} else {
			advertisedControl, controlErr := normalizeServerURL(advertised.ControlURL)
			advertisedContent, contentErr := normalizeServerURL(advertised.ContentURL)
			if controlErr != nil || contentErr != nil || !doctorSameOrigin(control, advertisedControl) || !doctorSameOrigin(content, advertisedContent) {
				report.add(origin.name+"-discovery", "fail", "Discovery does not agree on the control and content origins.")
			} else {
				report.add(origin.name+"-discovery", "pass", "Discovery agrees on the control and content origins.")
			}
		}
	}
	for _, probe := range []struct {
		name string
		base string
		path string
	}{{"content-rejects-api", content, "/api/pages"}, {"control-rejects-pages", control, "/p/doctorcheck1"}} {
		if err := doctorGet(transport, probe.base, probe.path, "", http.StatusMisdirectedRequest, nil); err != nil {
			report.add(probe.name, "fail", err.Error())
		} else {
			report.add(probe.name, "pass", "Wrong-origin route rejected with HTTP 421.")
		}
	}
}

func (report *doctorReport) inspectAuth(configured *client, transport *http.Client) {
	var info authInfoResponse
	if err := doctorGet(transport, configured.base, "/api/auth", configured.token, http.StatusOK, &info); err != nil {
		report.add("authentication", "fail", err.Error())
		return
	}
	readScope := false
	for _, scope := range info.Scopes {
		if scope == "pages:read" {
			readScope = true
		}
	}
	if !info.OK || info.CredentialType != "device_token" || !readScope || info.ExpiresAt == nil {
		report.add("authentication", "fail", "Auth response must identify a scoped device credential with pages:read and an expiry.")
		return
	}
	report.Credential = &doctorCredential{Type: "device_token", ExpiresAt: info.ExpiresAt}
	report.add("authentication", "pass", "Configured scoped credential accepted.")
	remaining := time.Until(*info.ExpiresAt)
	switch {
	case remaining <= 0:
		report.add("credential-expiry", "fail", "Scoped credential has expired.")
	case remaining <= 7*24*time.Hour:
		report.add("credential-expiry", "warn", "Scoped credential expires within seven days.")
	default:
		report.add("credential-expiry", "pass", "Scoped credential expires in more than seven days.")
	}
}

func doctorGet(transport *http.Client, base, path, token string, expected int, out any) error {
	req, err := http.NewRequest(http.MethodGet, base+path, nil)
	if err != nil {
		return errors.New("Could not construct diagnostic request.")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := transport.Do(req)
	if err != nil {
		return errors.New("Diagnostic request failed; check connectivity and TLS.")
	}
	defer resp.Body.Close()
	if resp.StatusCode != expected {
		return fmt.Errorf("Expected HTTP %d, received HTTP %d.", expected, resp.StatusCode)
	}
	if out != nil {
		decoder := json.NewDecoder(io.LimitReader(resp.Body, 64*1024))
		if err := decoder.Decode(out); err != nil {
			return errors.New("Server returned an invalid diagnostic JSON response.")
		}
		if err := decoder.Decode(new(any)); err != io.EOF {
			return errors.New("Server returned an invalid diagnostic JSON response.")
		}
	}
	return nil
}

func doctorSameOrigin(first, second string) bool {
	firstURL, firstErr := url.Parse(first)
	secondURL, secondErr := url.Parse(second)
	if firstErr != nil || secondErr != nil {
		return false
	}
	port := func(origin *url.URL) string {
		if origin.Port() != "" {
			return origin.Port()
		}
		if origin.Scheme == "https" {
			return "443"
		}
		return "80"
	}
	return strings.EqualFold(firstURL.Scheme, secondURL.Scheme) && strings.EqualFold(firstURL.Hostname(), secondURL.Hostname()) && port(firstURL) == port(secondURL)
}
