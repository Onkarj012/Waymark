package web

import (
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/Onkarj012/Waymark/internal/store"
)

func sourceTestServer(t *testing.T) (*Server, *store.Store, *sql.DB, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "pages.db")
	st, err := store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return newDeviceTestServer(t, st), st, db, seedDeviceToken(t, st)
}

func sourceRequest(h http.Handler, host, id, token string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(http.MethodGet, "http://"+host+"/api/pages/"+id+"/source", nil)
	if token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
	response := httptest.NewRecorder()
	h.ServeHTTP(response, request)
	return response
}

func assertSourceHeaders(t *testing.T, response *httptest.ResponseRecorder) {
	t.Helper()
	for name, want := range map[string]string{
		"Content-Type": "application/json; charset=utf-8", "Cache-Control": "no-store",
		"X-Content-Type-Options": "nosniff", "Referrer-Policy": "same-origin",
		"Content-Security-Policy": "default-src 'none'; style-src 'self'; form-action 'self'; base-uri 'none'; frame-ancestors 'none'",
	} {
		if got := response.Header().Get(name); got != want {
			t.Errorf("%s = %q, want %q", name, got, want)
		}
	}
	if response.Header().Get("Access-Control-Allow-Origin") != "" {
		t.Error("source response enabled CORS")
	}
}

func TestSourceExportExactEnvelopeAndWorkersTimestamps(t *testing.T) {
	for _, raw := range []bool{true, false} {
		t.Run(fmt.Sprintf("raw=%t", raw), func(t *testing.T) {
			h, st, db, token := sourceTestServer(t)
			html := "\ufeff<p>café 🌍 e\u0301 &amp; <script>alert('source')</script></p>\r\n\x00"
			if raw {
				html = strings.ReplaceAll(validRawHTML("Report", html), "\n", "\r\n")
			}
			created := "2026-11-09T09:00:00.120Z"
			updated := "2026-11-09T09:01:00.120Z"
			var expiry any
			if raw {
				expiry = "2100-11-09T09:00:00.120Z"
			}
			if _, err := db.Exec(`INSERT INTO pages
				(id, title, slug, html, raw, created_at, updated_at, expires_at)
				VALUES (?, ?, ?, ?, ?, ?, ?, ?)`, "workers-page", "<Report>", "", html, raw, created, updated, expiry); err != nil {
				t.Fatal(err)
			}
			before, err := st.GetSource("workers-page")
			if err != nil {
				t.Fatal(err)
			}
			response := sourceRequest(h, "control.localhost", before.ID, token)
			if response.Code != http.StatusOK {
				t.Fatalf("status = %d: %s", response.Code, response.Body.String())
			}
			assertSourceHeaders(t, response)
			var got map[string]any
			if err := json.Unmarshal(response.Body.Bytes(), &got); err != nil {
				t.Fatal(err)
			}
			want := map[string]any{
				"schema": "waymark-source", "version": float64(1),
				"control_origin": "http://control.localhost", "public_origin": "http://pages.localhost",
				"id": before.ID, "url": "http://pages.localhost/p/" + before.ID,
				"title": "<Report>", "slug": "", "raw": raw, "html": html,
				"created_at": created, "updated_at": updated, "expires_at": expiry,
				"utf8_bytes": float64(len(html)), "sha256": fmt.Sprintf("%x", sha256.Sum256([]byte(html))),
			}
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("source envelope = %#v, want %#v", got, want)
			}
			after, err := st.GetSource(before.ID)
			if err != nil || !reflect.DeepEqual(before, after) {
				t.Fatalf("export changed row: before %#v, after %#v, error %v", before, after, err)
			}
			metadata := authenticatedRequest(t, h, http.MethodGet, "control.localhost", "/api/pages/"+before.ID, token, nil)
			var meta map[string]any
			if err := json.Unmarshal(metadata.Body.Bytes(), &meta); err != nil {
				t.Fatal(err)
			}
			if _, present := meta["html"]; present {
				t.Fatal("metadata API included source")
			}
		})
	}
}

func TestSourceExportRequiresReadTokenAndControlHost(t *testing.T) {
	h, _, db, token := sourceTestServer(t)
	for _, credential := range []string{"", "invalid"} {
		response := sourceRequest(h, "control.localhost", "missing", credential)
		if response.Code != http.StatusUnauthorized {
			t.Fatalf("unauthenticated status = %d", response.Code)
		}
		assertSourceHeaders(t, response)
	}
	cookie, _ := adminLogin(t, h)
	request := httptest.NewRequest(http.MethodGet, "http://control.localhost/api/pages/missing/source", nil)
	request.AddCookie(cookie)
	response := httptest.NewRecorder()
	h.ServeHTTP(response, request)
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("owner-session source status = %d", response.Code)
	}
	assertSourceHeaders(t, response)
	for _, scopes := range []string{"pages:write", "pages:read"} {
		if _, err := db.Exec(`UPDATE api_tokens SET scopes = ?`, scopes); err != nil {
			t.Fatal(err)
		}
		response := sourceRequest(h, "control.localhost", "missing", token)
		want := http.StatusForbidden
		if scopes == "pages:read" {
			want = http.StatusNotFound
		}
		if response.Code != want {
			t.Fatalf("scopes %q status = %d, want %d", scopes, response.Code, want)
		}
		assertSourceHeaders(t, response)
	}
	if _, err := db.Exec(`INSERT INTO pages
		(id, title, slug, html, raw, created_at, updated_at)
		VALUES ('readable', 'Report', '', '<p>legacy</p>', 0, ?, ?)`, time.Now().UTC().Format(time.RFC3339Nano), time.Now().UTC().Format(time.RFC3339Nano)); err != nil {
		t.Fatal(err)
	}
	if response := sourceRequest(h, "control.localhost", "readable", token); response.Code != http.StatusOK {
		t.Fatalf("read-only source status = %d", response.Code)
	}
	if _, err := db.Exec(`UPDATE api_tokens SET expires_at = '2000-01-01T00:00:00Z'`); err != nil {
		t.Fatal(err)
	}
	expired := sourceRequest(h, "control.localhost", "readable", token)
	if expired.Code != http.StatusUnauthorized {
		t.Fatalf("expired token source status = %d", expired.Code)
	}
	assertSourceHeaders(t, expired)
	if _, err := db.Exec(`UPDATE api_tokens SET expires_at = '2100-01-01T00:00:00Z', revoked_at = '2026-01-01T00:00:00Z'`); err != nil {
		t.Fatal(err)
	}
	revoked := sourceRequest(h, "control.localhost", "readable", token)
	if revoked.Code != http.StatusUnauthorized {
		t.Fatalf("revoked token source status = %d", revoked.Code)
	}
	assertSourceHeaders(t, revoked)
	for _, host := range []string{"pages.localhost", "unknown.localhost"} {
		if response := sourceRequest(h, host, "missing", token); response.Code != http.StatusMisdirectedRequest {
			t.Fatalf("host %q status = %d", host, response.Code)
		}
	}
}

func TestSourceExportExpiryDeletedInvalidUTF8AndOversized(t *testing.T) {
	h, st, _, token := sourceTestServer(t)
	now := time.Now().UTC()
	h.now = func() time.Time { return now }
	for _, test := range []struct {
		name    string
		html    string
		expires *time.Time
		deleted bool
		legacy  bool
		status  int
	}{
		{name: "expired", html: "secret", expires: timePointer(now.Add(-time.Hour)), status: 404},
		{name: "expiry-boundary", html: "secret", expires: &now, status: 404},
		{name: "deleted", html: "secret", deleted: true, status: 404},
		{name: "invalid-utf8", html: "secret\xff", status: 422},
		{name: "invalid-legacy-utf8", html: "secret\xff", legacy: true, status: 422},
		{name: "invalid-legacy-surrogate", html: "secret\xed\xa0\x80", legacy: true, status: 422},
		{name: "expired-invalid", html: "secret\xff", expires: &now, status: 404},
		{name: "oversized", html: strings.Repeat("é", maxBodyBytes/2+1), status: 200},
	} {
		t.Run(test.name, func(t *testing.T) {
			page := &store.Page{ID: test.name, Title: "Test", HTML: test.html, Raw: !test.legacy,
				CreatedAt: now, UpdatedAt: now, ExpiresAt: test.expires}
			if err := st.Create(page); err != nil {
				t.Fatal(err)
			}
			if test.deleted {
				if err := st.Delete(page.ID); err != nil {
					t.Fatal(err)
				}
			}
			response := sourceRequest(h, "control.localhost", page.ID, token)
			if response.Code != test.status {
				t.Fatalf("status = %d, want %d", response.Code, test.status)
			}
			assertSourceHeaders(t, response)
			if test.status != 200 {
				if strings.Contains(response.Body.String(), "secret") {
					t.Fatal("error response leaked source")
				}
				return
			}
			var source sourceResp
			if err := json.Unmarshal(response.Body.Bytes(), &source); err != nil {
				t.Fatal(err)
			}
			if source.HTML != test.html || source.UTF8Bytes != len(test.html) || source.SHA256 != fmt.Sprintf("%x", sha256.Sum256([]byte(test.html))) {
				t.Fatal("oversized source was not exported exactly")
			}
		})
	}
}

func timePointer(value time.Time) *time.Time {
	return &value
}

func TestSourceExportRejectsInvalidStoredMetadata(t *testing.T) {
	h, st, db, token := sourceTestServer(t)
	now := time.Now().UTC()
	for _, field := range []string{"title", "slug"} {
		for index, invalid := range []string{"\xff", "\xed\xa0\x80", "\xe2\x82"} {
			id := fmt.Sprintf("invalid-%s-%d", field, index)
			if err := st.Create(&store.Page{ID: id, Title: "Report", HTML: "<p>legacy</p>", CreatedAt: now, UpdatedAt: now}); err != nil {
				t.Fatal(err)
			}
			if _, err := db.Exec(`UPDATE pages SET `+field+` = ? WHERE id = ?`, invalid, id); err != nil {
				t.Fatal(err)
			}
			before, err := st.GetSource(id)
			if err != nil {
				t.Fatal(err)
			}
			response := sourceRequest(h, "control.localhost", id, token)
			if response.Code != 422 || apiError(t, response) != "stored source is not valid UTF-8" {
				t.Fatalf("invalid %s status = %d: %s", field, response.Code, response.Body.String())
			}
			assertSourceHeaders(t, response)
			after, err := st.GetSource(id)
			if err != nil || !reflect.DeepEqual(before, after) {
				t.Fatal("export repaired invalid stored metadata")
			}
		}
	}
}
