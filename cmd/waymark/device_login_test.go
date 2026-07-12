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
