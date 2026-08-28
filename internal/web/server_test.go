package web

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Onkarj012/Waymark/internal/store"
)

func validRawHTML(title, inner string) string {
	return `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>` + title + `</title>
<style>body { margin: 0; }</style>
</head>
<body>
` + inner + `
</body>
</html>
`
}

func createJSON(t *testing.T, title, html string, extra map[string]any) []byte {
	t.Helper()
	body := map[string]any{"title": title, "html": html, "raw": true}
	for k, v := range extra {
		body[k] = v
	}
	b, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestCreateAndServeRawPage(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "pages.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })

	h := newDeviceTestServer(t, st)
	token := seedDeviceToken(t, st)

	html := validRawHTML("Status <check>", `<script>window.demo=true</script><p>Hello</p>`)
	createdResponse := authenticatedRequest(t, h, http.MethodPost, "control.localhost", "/api/pages", token, bytes.NewReader(createJSON(t, "Status <check>", html, nil)))
	if createdResponse.Code != http.StatusCreated {
		t.Fatalf("create status = %d, body = %s", createdResponse.Code, createdResponse.Body.String())
	}
	var created struct {
		ID  string `json:"id"`
		URL string `json:"url"`
		Raw bool   `json:"raw"`
	}
	if err := json.NewDecoder(createdResponse.Body).Decode(&created); err != nil {
		t.Fatal(err)
	}
	if created.URL != "http://pages.localhost/p/"+created.ID {
		t.Fatalf("created URL = %q", created.URL)
	}
	if !created.Raw {
		t.Fatal("created page was not raw")
	}

	pageResp := doRequest(t, h, http.MethodGet, "pages.localhost", "/p/"+created.ID, nil, nil, "")
	defer pageResp.Body.Close()
	page, err := io.ReadAll(pageResp.Body)
	if err != nil {
		t.Fatal(err)
	}
	if string(page) != html {
		t.Fatalf("raw page was not served verbatim: %s", page)
	}
	if bytes.Contains(page, []byte(`<link rel="stylesheet" href="/theme.css">`)) {
		t.Fatal("raw page linked the house theme")
	}
	faviconResp := doRequest(t, h, http.MethodGet, "pages.localhost", "/favicon.svg", nil, nil, "")
	defer faviconResp.Body.Close()
	if faviconResp.StatusCode != http.StatusOK || faviconResp.Header.Get("Content-Type") != "image/svg+xml" {
		t.Fatalf("favicon response = status %d, content type %q", faviconResp.StatusCode, faviconResp.Header.Get("Content-Type"))
	}
	if got := pageResp.Header.Get("Referrer-Policy"); got != "no-referrer" {
		t.Fatalf("Referrer-Policy = %q", got)
	}
	if got := pageResp.Header.Get("X-Content-Type-Options"); got != "nosniff" {
		t.Fatalf("X-Content-Type-Options = %q", got)
	}
	if got := pageResp.Header.Get("Content-Security-Policy"); got != "frame-ancestors 'none'" {
		t.Fatalf("Content-Security-Policy = %q", got)
	}
	if got := pageResp.Header.Get("X-Frame-Options"); got != "DENY" {
		t.Fatalf("X-Frame-Options = %q", got)
	}
}

func TestServeLegacyThemedPageAndThemeCSS(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "pages.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })

	h := newDeviceTestServer(t, st)
	now := time.Now().UTC()
	p := &store.Page{
		ID: "legacythemed1", Title: "Status <check>", HTML: `<script>window.demo=true</script><p>Hello</p>`,
		CreatedAt: now, UpdatedAt: now,
	}
	if err := st.Create(p); err != nil {
		t.Fatal(err)
	}

	pageResp := doRequest(t, h, http.MethodGet, "pages.localhost", "/p/"+p.ID, nil, nil, "")
	defer pageResp.Body.Close()
	page, err := io.ReadAll(pageResp.Body)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(page, []byte("<title>Status &lt;check&gt;</title>")) {
		t.Fatalf("title was not escaped: %s", page)
	}
	if !bytes.Contains(page, []byte("<script>window.demo=true</script><p>Hello</p>")) {
		t.Fatalf("publisher HTML contract changed: %s", page)
	}
	if !bytes.Contains(page, []byte(`<link rel="stylesheet" href="/theme.css">`)) {
		t.Fatalf("legacy themed page did not link /theme.css: %s", page)
	}
	if got := pageResp.Header.Get("Content-Security-Policy"); got != "frame-ancestors 'none'" {
		t.Fatalf("Content-Security-Policy = %q", got)
	}
	if got := pageResp.Header.Get("X-Frame-Options"); got != "DENY" {
		t.Fatalf("X-Frame-Options = %q", got)
	}
	themeResp := doRequest(t, h, http.MethodGet, "pages.localhost", "/theme.css", nil, nil, "")
	defer themeResp.Body.Close()
	if themeResp.StatusCode != http.StatusOK || !strings.Contains(themeResp.Header.Get("Content-Type"), "text/css") {
		t.Fatalf("theme.css response = status %d, content type %q", themeResp.StatusCode, themeResp.Header.Get("Content-Type"))
	}
}

func TestUpdateHonorsIfUpdatedAtAndReturnsAuthoritativeMetadata(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "pages.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })

	h := newDeviceTestServer(t, st)
	token := seedDeviceToken(t, st)
	fixed := time.Now().UTC()
	h.now = func() time.Time { return fixed }
	createdResponse := authenticatedRequest(t, h, http.MethodPost, "control.localhost", "/api/pages", token, bytes.NewReader(createJSON(t, "Original", validRawHTML("Original", "<p>x</p>"), nil)))
	if createdResponse.Code != http.StatusCreated {
		t.Fatalf("create status = %d, body = %s", createdResponse.Code, createdResponse.Body.String())
	}
	var created struct {
		ID        string    `json:"id"`
		UpdatedAt time.Time `json:"updated_at"`
	}
	if err := json.NewDecoder(createdResponse.Body).Decode(&created); err != nil {
		t.Fatal(err)
	}

	firstUpdate := fmt.Sprintf(`{"title":"Fresh","if_updated_at":%q}`, created.UpdatedAt.Format(time.RFC3339Nano))
	updatedResponse := authenticatedRequest(t, h, http.MethodPut, "control.localhost", "/api/pages/"+created.ID, token, strings.NewReader(firstUpdate))
	if updatedResponse.Code != http.StatusOK {
		t.Fatalf("conditional update status = %d, body = %s", updatedResponse.Code, updatedResponse.Body.String())
	}
	var updated struct {
		Title     string    `json:"title"`
		UpdatedAt time.Time `json:"updated_at"`
	}
	if err := json.NewDecoder(updatedResponse.Body).Decode(&updated); err != nil {
		t.Fatal(err)
	}
	if updated.Title != "Fresh" || !updated.UpdatedAt.Equal(created.UpdatedAt.Add(time.Nanosecond)) {
		t.Fatalf("conditional response = %#v, want fresh authoritative metadata", updated)
	}

	stale := fmt.Sprintf(`{"title":"Stale","if_updated_at":%q}`, created.UpdatedAt.Format(time.RFC3339Nano))
	conflict := authenticatedRequest(t, h, http.MethodPut, "control.localhost", "/api/pages/"+created.ID, token, strings.NewReader(stale))
	if conflict.Code != http.StatusConflict {
		t.Fatalf("stale update status = %d, body = %s", conflict.Code, conflict.Body.String())
	}
	meta := authenticatedRequest(t, h, http.MethodGet, "control.localhost", "/api/pages/"+created.ID, token, nil)
	var got struct {
		Title string `json:"title"`
	}
	if err := json.NewDecoder(meta.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if got.Title != "Fresh" {
		t.Fatalf("conflict changed stored title to %q", got.Title)
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

func apiError(t *testing.T, rec *httptest.ResponseRecorder) string {
	t.Helper()
	var body struct {
		Error string `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode error body %q: %v", rec.Body.String(), err)
	}
	return body.Error
}

func TestCreateRequiresExplicitRawTrueAndValidDocument(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "pages.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	h := newDeviceTestServer(t, st)
	token := seedDeviceToken(t, st)
	html := validRawHTML("Doc", "<p>Hi</p>")

	for _, tc := range []struct {
		name string
		body string
		want string
	}{
		{"omitted", `{"title":"Doc","html":` + mustJSON(t, html) + `}`, "raw:true is required"},
		{"false", `{"title":"Doc","html":` + mustJSON(t, html) + `,"raw":false}`, "raw:true is required"},
		{"null", `{"title":"Doc","html":` + mustJSON(t, html) + `,"raw":null}`, "raw:true is required"},
		{"invalid html", `{"title":"Doc","html":"<p>Hi</p>","raw":true}`, "invalid raw HTML"},
		{"house theme", `{"title":"Doc","html":` + mustJSON(t, strings.Replace(html, "<style>body { margin: 0; }</style>", `<link rel="stylesheet" href="/theme.css">`, 1)) + `,"raw":true}`, "house theme"},
		{"plan profile", `{"title":"Doc","html":` + mustJSON(t, validRawHTML("Doc", `<article data-plan-profile="systems">x</article>`)) + `,"raw":true}`, "data-plan-profile"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rec := authenticatedRequest(t, h, http.MethodPost, "control.localhost", "/api/pages", token, strings.NewReader(tc.body))
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
			}
			if got := apiError(t, rec); !strings.Contains(got, tc.want) {
				t.Fatalf("error = %q, want substring %q", got, tc.want)
			}
		})
	}

	list := authenticatedRequest(t, h, http.MethodGet, "control.localhost", "/api/pages", token, nil)
	var out struct {
		Pages []struct{} `json:"pages"`
	}
	if err := json.NewDecoder(list.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	if len(out.Pages) != 0 {
		t.Fatalf("rejected creates still stored %d pages", len(out.Pages))
	}
}

func TestUpdateRawSemanticsAndLegacyConversion(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "pages.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	h := newDeviceTestServer(t, st)
	token := seedDeviceToken(t, st)
	now := time.Now().UTC()
	legacy := &store.Page{
		ID: "legacyupdate1", Title: "Legacy", HTML: "<p>old</p>",
		CreatedAt: now, UpdatedAt: now,
	}
	if err := st.Create(legacy); err != nil {
		t.Fatal(err)
	}
	created := authenticatedRequest(t, h, http.MethodPost, "control.localhost", "/api/pages", token, bytes.NewReader(createJSON(t, "Raw", validRawHTML("Raw", "<p>raw</p>"), nil)))
	if created.Code != http.StatusCreated {
		t.Fatalf("create status = %d, body = %s", created.Code, created.Body.String())
	}
	var rawPage struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(created.Body).Decode(&rawPage); err != nil {
		t.Fatal(err)
	}

	t.Run("raw false rejected", func(t *testing.T) {
		rec := authenticatedRequest(t, h, http.MethodPut, "control.localhost", "/api/pages/"+legacy.ID, token, strings.NewReader(`{"raw":false}`))
		if rec.Code != http.StatusBadRequest || !strings.Contains(apiError(t, rec), "raw cannot be false") {
			t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
		}
	})
	t.Run("conversion without html rejected", func(t *testing.T) {
		rec := authenticatedRequest(t, h, http.MethodPut, "control.localhost", "/api/pages/"+legacy.ID, token, strings.NewReader(`{"raw":true}`))
		if rec.Code != http.StatusBadRequest || !strings.Contains(apiError(t, rec), "raw:true requires html") {
			t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
		}
		got, err := st.Get(legacy.ID)
		if err != nil {
			t.Fatal(err)
		}
		if got.Raw || got.HTML != "<p>old</p>" {
			t.Fatalf("legacy page mutated: raw=%v html=%q", got.Raw, got.HTML)
		}
	})
	t.Run("legacy metadata-only stays themed", func(t *testing.T) {
		rec := authenticatedRequest(t, h, http.MethodPut, "control.localhost", "/api/pages/"+legacy.ID, token, strings.NewReader(`{"title":"Still themed"}`))
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
		}
		var resp struct {
			Title string `json:"title"`
			Raw   bool   `json:"raw"`
		}
		if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
			t.Fatal(err)
		}
		if resp.Title != "Still themed" || resp.Raw {
			t.Fatalf("metadata-only conversion = %#v", resp)
		}
	})
	t.Run("legacy html replacement converts atomically", func(t *testing.T) {
		next := validRawHTML("Converted", "<p>new</p>")
		payload, err := json.Marshal(map[string]any{"html": next})
		if err != nil {
			t.Fatal(err)
		}
		rec := authenticatedRequest(t, h, http.MethodPut, "control.localhost", "/api/pages/"+legacy.ID, token, bytes.NewReader(payload))
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
		}
		var resp struct {
			Raw bool `json:"raw"`
		}
		if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
			t.Fatal(err)
		}
		if !resp.Raw {
			t.Fatal("legacy replacement did not convert to raw")
		}
		got, err := st.Get(legacy.ID)
		if err != nil {
			t.Fatal(err)
		}
		if !got.Raw || got.HTML != next {
			t.Fatalf("stored conversion = raw=%v html=%q", got.Raw, got.HTML)
		}
	})
	t.Run("raw replacement stays raw", func(t *testing.T) {
		next := validRawHTML("Raw", "<p>replaced</p>")
		payload, err := json.Marshal(map[string]any{"html": next})
		if err != nil {
			t.Fatal(err)
		}
		rec := authenticatedRequest(t, h, http.MethodPut, "control.localhost", "/api/pages/"+rawPage.ID, token, bytes.NewReader(payload))
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
		}
		var resp struct {
			Raw bool `json:"raw"`
		}
		if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
			t.Fatal(err)
		}
		if !resp.Raw {
			t.Fatal("raw replacement lost raw:true")
		}
		got, err := st.Get(rawPage.ID)
		if err != nil {
			t.Fatal(err)
		}
		if got.HTML != next {
			t.Fatalf("stored html = %q", got.HTML)
		}
	})
	t.Run("invalid replacement rejected", func(t *testing.T) {
		rec := authenticatedRequest(t, h, http.MethodPut, "control.localhost", "/api/pages/"+rawPage.ID, token, strings.NewReader(`{"html":"<p>nope</p>"}`))
		if rec.Code != http.StatusBadRequest || !strings.Contains(apiError(t, rec), "invalid raw HTML") {
			t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
		}
	})
}

func mustJSON(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}
