package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func sourceFixture(t *testing.T, mutate func(*sourceEnvelope)) (*httptest.Server, *sourceEnvelope, *int) {
	t.Helper()
	t.Setenv("WAYMARK_CONFIG_DIR", t.TempDir())
	t.Setenv("WAYMARK_TOKEN", "")
	t.Setenv("WAYMARK_URL", "")
	const html = "<!DOCTYPE html>\r\n<html><head><title>café</title></head><body>你好</body></html>\r\n"
	const publicOrigin = "https://pages.example.test"
	digest := sha256.Sum256([]byte(html))
	envelope := &sourceEnvelope{Schema: "waymark-source", Version: 1, PublicOrigin: publicOrigin, ID: "AbCdEf123456", Title: "test", HTML: html, Raw: true, CreatedAt: "2026-11-09T09:00:00.000Z", UpdatedAt: "2026-11-09T09:01:00.120Z", UTF8Bytes: len(html), SHA256: hex.EncodeToString(digest[:])}
	requests := new(int)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json; charset=utf-8")
		if request.UserAgent() == "" || !strings.HasPrefix(request.UserAgent(), "Waymark/") {
			t.Error("missing application User-Agent")
		}
		if request.URL.Path == "/.well-known/waymark" {
			if request.Header.Get("Authorization") != "" {
				t.Error("discovery received credentials")
			}
			json.NewEncoder(writer).Encode(discoveryResponse{ControlURL: envelope.ControlOrigin, ContentURL: publicOrigin})
			return
		}
		*requests = *requests + 1
		if request.URL.Path != "/api/pages/AbCdEf123456/source" || request.Header.Get("Authorization") != "Bearer saved-token" {
			t.Errorf("unexpected source request %s", request.URL.Path)
		}
		json.NewEncoder(writer).Encode(envelope)
	}))
	t.Cleanup(server.Close)
	envelope.ControlOrigin = server.URL
	envelope.URL = envelope.PublicOrigin + "/p/" + envelope.ID
	if mutate != nil {
		mutate(envelope)
	}
	if err := saveConfig(config{URL: server.URL, Token: "saved-token"}); err != nil {
		t.Fatal(err)
	}
	return server, envelope, requests
}

func TestPullPreservesSourceAndTimestamp(t *testing.T) {
	_, envelope, requests := sourceFixture(t, nil)
	output := filepath.Join(t.TempDir(), "report.html")
	if err := cmdPull([]string{"--output", output, envelope.ID}); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(output)
	if err != nil || string(data) != envelope.HTML || *requests != 1 {
		t.Fatalf("source mismatch or request count: %v, %d", err, *requests)
	}
	assertMode(t, output, 0o600)
	assertMode(t, output+".source.json", 0o600)
	data, err = os.ReadFile(output + ".source.json")
	if err != nil {
		t.Fatal(err)
	}
	var metadata map[string]any
	if err := json.Unmarshal(data, &metadata); err != nil {
		t.Fatal(err)
	}
	if metadata["updated_at"] != envelope.UpdatedAt || metadata["source_path"] != "report.html" || metadata["html"] != nil || metadata["publishable"] != true || strings.Contains(string(data), "saved-token") {
		t.Fatal("sidecar did not preserve metadata safely")
	}
	if err := cmdPull([]string{"--output", output, envelope.ID}); err == nil || *requests != 1 {
		t.Fatal("existing output not rejected before export")
	}
}

func TestPullRejectsInvalidSourceWithoutFiles(t *testing.T) {
	for rule, expected := range map[string]string{
		"checksum":   "source byte count or SHA-256 checksum does not match",
		"byte-count": "source byte count or SHA-256 checksum does not match",
		"identity":   "source identity or origin does not match the requested page",
		"origin":     "source identity or origin does not match the requested page",
		"timestamp":  "source contains an invalid timestamp",
		"legacy":     "page is a legacy fragment",
	} {
		t.Run(rule, func(t *testing.T) {
			sourceFixture(t, func(envelope *sourceEnvelope) {
				switch rule {
				case "checksum":
					envelope.SHA256 = strings.Repeat("0", 64)
				case "byte-count":
					envelope.UTF8Bytes++
				case "identity":
					envelope.ID = "WrongId12345"
				case "origin":
					envelope.PublicOrigin = "https://evil.example.test"
					envelope.URL = envelope.PublicOrigin + "/p/" + envelope.ID
				case "timestamp":
					envelope.UpdatedAt = "not-a-time"
				case "legacy":
					envelope.Raw = false
				}
			})
			output := filepath.Join(t.TempDir(), "report.html")
			if err := cmdPull([]string{"--output", output, "AbCdEf123456"}); err == nil || !strings.Contains(err.Error(), expected) {
				t.Fatalf("invalid %s envelope: got %v, want %q", rule, err, expected)
			}
			for _, path := range []string{output, output + ".source.json"} {
				if _, err := os.Lstat(path); !os.IsNotExist(err) {
					t.Fatalf("invalid export left %q", path)
				}
			}
		})
	}
}

func TestPullSavedTokenCannotChangeOrigin(t *testing.T) {
	server, _, requests := sourceFixture(t, nil)
	if err := saveConfig(config{URL: "https://other.example.test", Token: "saved-token"}); err != nil {
		t.Fatal(err)
	}
	if _, err := fetchSource(server.URL, "AbCdEf123456"); err == nil || *requests != 0 {
		t.Fatal("saved token escaped its configured origin")
	}
	t.Setenv("WAYMARK_TOKEN", "saved-token")
	if _, err := fetchSource(server.URL, "AbCdEf123456"); err != nil || *requests != 1 {
		t.Fatalf("explicit env token destination not respected: %v", err)
	}
}

func TestPullLegacyOptIn(t *testing.T) {
	_, envelope, _ := sourceFixture(t, func(envelope *sourceEnvelope) { envelope.Raw = false })
	output := filepath.Join(t.TempDir(), "fragment.html")
	if err := cmdPull([]string{"--output", output, "--legacy-fragment", envelope.ID}); err != nil {
		t.Fatal(err)
	}
	metadata, err := os.ReadFile(output + ".source.json")
	if err != nil || !strings.Contains(string(metadata), `"publishable": false`) {
		t.Fatal("legacy fragment not marked non-publishable")
	}
}

func TestSourceDestinationRejectsTraversalAndSymlinks(t *testing.T) {
	directory := t.TempDir()
	link := filepath.Join(directory, "link")
	if err := os.Symlink(directory, link); err != nil {
		t.Skip(err)
	}
	for _, output := range []string{filepath.Join(link, "report.html"), directory + "/../report.html"} {
		if root, _, err := sourceDestination(output); err == nil {
			root.Close()
			t.Fatalf("accepted unsafe output %q", output)
		}
	}
}

func TestInstallSourceNeverClobbersSidecar(t *testing.T) {
	directory := t.TempDir()
	root, err := os.OpenRoot(directory)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	if err := os.WriteFile(filepath.Join(directory, "report.html.source.json"), []byte("original"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := installSource(root, "report.html", sourceEnvelope{HTML: "exported"}); err == nil {
		t.Fatal("sidecar collision should fail")
	}
	data, err := os.ReadFile(filepath.Join(directory, "report.html.source.json"))
	if err != nil || string(data) != "original" {
		t.Fatal("existing sidecar overwritten")
	}
}

func TestAuthenticatedRedirectNeverForwarded(t *testing.T) {
	requests := 0
	target := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { requests++ }))
	defer target.Close()
	redirect := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		http.Redirect(writer, request, target.URL, http.StatusTemporaryRedirect)
	}))
	defer redirect.Close()
	client := &client{base: redirect.URL, token: "secret"}
	if err := client.do(http.MethodGet, "/api/pages", nil, nil); err == nil || requests != 0 {
		t.Fatal("authenticated redirect was followed or accepted")
	}
}

func TestMutationCommandsRejectTrailingFlags(t *testing.T) {
	commands := []struct {
		name string
		run  func([]string) error
		args []string
	}{
		{"create", cmdCreate, []string{"file.html", "--ttl", "7"}},
		{"update", cmdUpdate, []string{"AbCdEf123456", "file.html", "--ttl", "7"}},
		{"delete", cmdDelete, []string{"AbCdEf123456", "--server", "https://elsewhere.test"}},
	}
	for _, command := range commands {
		t.Run(command.name, func(t *testing.T) {
			if err := command.run(command.args); err == nil || !strings.Contains(err.Error(), "flags before positional") {
				t.Fatalf("trailing flag accepted: %v", err)
			}
		})
	}
}

func TestSourceUnicodeRejectsUnpairedSurrogates(t *testing.T) {
	for _, value := range []string{`"\ud800"`, `"\udc00"`, `"\ud800x"`, `"\ud800\u0061"`} {
		if validSourceUnicode([]byte(value)) {
			t.Fatalf("accepted invalid Unicode %s", value)
		}
	}
	for _, value := range []string{`"\ud83c\udf0d"`, `"\\ud800"`, `"café"`, `"\u0000"`} {
		if !validSourceUnicode([]byte(value)) {
			t.Fatalf("rejected valid Unicode %s", value)
		}
	}
}

func TestCommandsRejectUnexpectedPositionals(t *testing.T) {
	for _, command := range []func([]string) error{cmdList, cmdStatus, cmdLogout} {
		if err := command([]string{"unexpected"}); err == nil {
			t.Fatal("command silently ignored a positional argument")
		}
	}
	if err := cmdCreate([]string{"--title", "test", "--ttl", "7", "--permanent", "report.html"}); err == nil || !strings.Contains(err.Error(), "choose --ttl") {
		t.Fatalf("conflicting lifetime flags accepted: %v", err)
	}
}
