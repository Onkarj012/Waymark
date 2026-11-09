package main

import (
	"crypto/sha256"
	"database/sql"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Onkarj012/Waymark/internal/store"
	"github.com/Onkarj012/Waymark/internal/web"
)

func TestPullAgainstGoBackend(t *testing.T) {
	t.Setenv("WAYMARK_CONFIG_DIR", t.TempDir())
	t.Setenv("WAYMARK_TOKEN", "")
	t.Setenv("WAYMARK_URL", "")
	databasePath := filepath.Join(t.TempDir(), "waymark.db")
	persistence, err := store.Open(databasePath)
	if err != nil {
		t.Fatal(err)
	}
	defer persistence.Close()
	database, err := sql.Open("sqlite", databasePath)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	const token = "waymark_integration.secret"
	digest := fmt.Sprintf("%x", sha256.Sum256([]byte(token)))
	if _, err := database.Exec(`INSERT INTO api_tokens (id, token_hash, display_prefix, device_label, scopes, created_at, expires_at) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		"integration", digest, "waymark_integration", "test device", "pages:read pages:write", time.Now().UTC().Format(time.RFC3339Nano), time.Now().Add(time.Hour).UTC().Format(time.RFC3339Nano)); err != nil {
		t.Fatal(err)
	}
	control := httptest.NewUnstartedServer(nil)
	content := httptest.NewUnstartedServer(nil)
	controlURL := "http://" + control.Listener.Addr().String()
	contentURL := "http://" + content.Listener.Addr().String()
	handler, err := web.NewConfigured(persistence, web.Config{AdminPasscode: "local-test-secret", PublicBaseURL: contentURL, ControlBaseURL: controlURL})
	if err != nil {
		t.Fatal(err)
	}
	control.Config.Handler = handler
	content.Config.Handler = handler
	control.Start()
	content.Start()
	defer control.Close()
	defer content.Close()
	if err := saveConfig(config{URL: controlURL, Token: token}); err != nil {
		t.Fatal(err)
	}
	cli, err := newClient(&controlURL)
	if err != nil {
		t.Fatal(err)
	}
	const html = "<!DOCTYPE html>\r\n<html><head><meta name=\"viewport\" content=\"width=device-width\"><title>Report</title><style>body{}</style></head><body>café 你好</body></html>\r\n"
	var page pageResp
	if err := cli.do(http.MethodPost, "/api/pages", map[string]any{"title": "Report", "html": html, "raw": true, "ttl_days": 1}, &page); err != nil {
		t.Fatal(err)
	}
	output := filepath.Join(t.TempDir(), "report.html")
	if err := cmdPull([]string{"--server", contentURL, "--output", output, page.ID}); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(output)
	if err != nil || string(data) != html {
		t.Fatalf("end-to-end source differs: %v", err)
	}
	if err := cmdDoctor([]string{"--json"}); err != nil {
		t.Fatal(err)
	}
}
