package main

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestSaveConfigSecuresExistingPaths(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix permission bits are not enforced on Windows")
	}

	dir := filepath.Join(t.TempDir(), "config")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "config.json")
	if err := os.WriteFile(path, []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("WAYMARK_CONFIG_DIR", dir)

	want := config{URL: "https://control.example.com", Token: "waymark_test.secret"}
	if err := saveConfig(want); err != nil {
		t.Fatal(err)
	}

	got, err := loadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("loadConfig() = %#v, want %#v", got, want)
	}
	assertMode(t, dir, 0o700)
	assertMode(t, path, 0o600)
}

func TestNormalizeServerURL(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    string
		wantErr bool
	}{
		{name: "https", input: "https://pages.example.com/", want: "https://pages.example.com"},
		{name: "localhost", input: "http://localhost:8080", want: "http://localhost:8080"},
		{name: "ipv4 loopback", input: "http://127.0.0.1:8080", want: "http://127.0.0.1:8080"},
		{name: "plain remote http", input: "http://pages.example.com", wantErr: true},
		{name: "credentials", input: "https://user:pass@pages.example.com", wantErr: true},
		{name: "path", input: "https://pages.example.com/base", wantErr: true},
		{name: "relative", input: "pages.example.com", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := normalizeServerURL(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("normalizeServerURL(%q) returned no error", tt.input)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if got != tt.want {
				t.Fatalf("normalizeServerURL(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestResolveReturnsConfigParseError(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("WAYMARK_CONFIG_DIR", dir)
	if err := os.WriteFile(filepath.Join(dir, "config.json"), []byte("not json"), 0o600); err != nil {
		t.Fatal(err)
	}

	if _, _, _, _, err := resolve(""); err == nil {
		t.Fatal("resolve() returned no error for malformed config")
	}
}

func TestGetRejectsFlagsAfterID(t *testing.T) {
	err := cmdGet([]string{"page-id", "--json"})
	if err == nil || !strings.Contains(err.Error(), "flags before positional arguments") {
		t.Fatalf("cmdGet() error = %v", err)
	}
}

func assertMode(t *testing.T, path string, want os.FileMode) {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != want {
		t.Fatalf("%s mode = %04o, want %04o", path, got, want)
	}
}
