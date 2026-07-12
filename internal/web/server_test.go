package web

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Onkarj012/Waymark/internal/store"
)

func TestCreateAndServeThemedPage(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "pages.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })

	h := newDeviceTestServer(t, st)
	token := seedDeviceToken(t, st)

	body := `{"title":"Status <check>","html":"<script>window.demo=true</script><p>Hello</p>"}`
	createdResponse := authenticatedRequest(t, h, http.MethodPost, "control.localhost", "/api/pages", token, strings.NewReader(body))
	if createdResponse.Code != http.StatusCreated {
		t.Fatalf("create status = %d, body = %s", createdResponse.Code, createdResponse.Body.String())
	}
	var created struct {
		ID  string `json:"id"`
		URL string `json:"url"`
	}
	if err := json.NewDecoder(createdResponse.Body).Decode(&created); err != nil {
		t.Fatal(err)
	}
	if created.URL != "http://pages.localhost/p/"+created.ID {
		t.Fatalf("created URL = %q", created.URL)
	}

	pageResp := doRequest(t, h, http.MethodGet, "pages.localhost", "/p/"+created.ID, nil, nil, "")
	defer pageResp.Body.Close()
	page, err := io.ReadAll(pageResp.Body)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(page, []byte("<title>Status &lt;check&gt;</title>")) {
		t.Fatalf("title was not escaped: %s", page)
	}
	if !bytes.Contains(page, []byte("<script>window.demo=true</script>")) {
		t.Fatalf("publisher HTML contract changed: %s", page)
	}
	credit := `<footer class="waymark-credit">
<a href="https://github.com/Onkarj012/Waymark" target="_blank" rel="noopener noreferrer">generated on Waymark</a>
</footer>`
	if !bytes.Contains(page, []byte(credit)) {
		t.Fatalf("generated credit is missing or unsafe: %s", page)
	}
	if bytes.Index(page, []byte(credit)) < bytes.Index(page, []byte("<script>window.demo=true</script>")) {
		t.Fatalf("generated credit appeared before publisher content: %s", page)
	}
	if got := pageResp.Header.Get("Referrer-Policy"); got != "no-referrer" {
		t.Fatalf("Referrer-Policy = %q", got)
	}
	if got := pageResp.Header.Get("X-Content-Type-Options"); got != "nosniff" {
		t.Fatalf("X-Content-Type-Options = %q", got)
	}
}

func TestAPIRejectsMissingTokenAndPublicHost(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "pages.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })

	h := newDeviceTestServer(t, st)
	for _, header := range []string{"", "X-Passcode"} {
		t.Run(header, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "http://control.localhost/api/auth", nil)
			req.Host = "control.localhost"
			if header != "" {
				req.Header.Set(header, "test-passcode")
			}
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)
			if rec.Code != http.StatusUnauthorized {
				t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
			}
		})
	}
	req := httptest.NewRequest(http.MethodGet, "http://pages.localhost/api/auth", nil)
	req.Host = "pages.localhost"
	req.Header.Set("Authorization", "Bearer token")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusMisdirectedRequest {
		t.Fatalf("public-host API status = %d, want %d", rec.Code, http.StatusMisdirectedRequest)
	}
}

func TestEmptyBearerTokenFailsClosed(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "pages.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })

	req := httptest.NewRequest(http.MethodGet, "http://control.localhost/api/auth", nil)
	req.Host = "control.localhost"
	req.Header.Set("Authorization", "Bearer ")
	rec := httptest.NewRecorder()
	newDeviceTestServer(t, st).ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
}

func seedDeviceToken(t *testing.T, st *store.Store) string {
	t.Helper()
	now := time.Now().UTC()
	grant := store.DeviceAuthorization{
		ID: "grant", DeviceCodeHash: "code", DeviceSecretHash: "secret", UserCodeHash: "user",
		DeviceLabel: "test device", Scopes: "pages:read pages:write", SourceKey: "source", SourceHint: "local",
		CreatedAt: now, ExpiresAt: now.Add(time.Minute), PollIntervalSeconds: 5,
	}
	if err := st.CreateDeviceAuthorization(grant, 5, 50); err != nil {
		t.Fatal(err)
	}
	if err := st.DecideDeviceAuthorization(grant.UserCodeHash, "approved", now); err != nil {
		t.Fatal(err)
	}
	raw := "waymark_test.secret"
	token := store.APIToken{
		ID: "token", TokenHash: hashHighEntropy(raw), DisplayPrefix: "waymark_test", DeviceLabel: grant.DeviceLabel,
		Scopes: grant.Scopes, CreatedAt: now, ExpiresAt: now.Add(time.Hour),
	}
	if _, err := st.PollDeviceAuthorization(grant.DeviceCodeHash, grant.DeviceSecretHash, now, token); err != nil {
		t.Fatal(err)
	}
	return raw
}

func TestLogPathRedactsPublicPageIDs(t *testing.T) {
	for input, want := range map[string]string{
		"/p/secret-page-id":         "/p/[redacted]",
		"/api/pages/secret-page-id": "/api/pages/[redacted]",
		"/api/pages":                "/api/pages",
	} {
		if got := logPath(input); got != want {
			t.Errorf("logPath(%q) = %q, want %q", input, got, want)
		}
	}
}
