package web

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/Onkarj012/Waymark/internal/store"
)

func TestConfiguredOriginValidationAndHostGating(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "pages.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	if _, err := NewConfigured(st, Config{PublicBaseURL: "https://same.example", ControlBaseURL: "https://same.example", AdminPasscode: "admin", TokenTTLDays: 90}); err == nil {
		t.Fatal("same content and control origin was accepted")
	}
	if _, err := NewConfigured(st, Config{PublicBaseURL: "https://same.example", ControlBaseURL: "https://same.example:443", AdminPasscode: "admin", TokenTTLDays: 90}); err == nil {
		t.Fatal("equivalent default-port origins were accepted")
	}
	if _, err := NewConfigured(st, Config{PublicBaseURL: "https://pages.example", ControlBaseURL: "https://control.example", TokenTTLDays: 90}); err == nil {
		t.Fatal("control origin without admin passcode was accepted")
	}
	if _, err := NewConfigured(st, Config{AdminPasscode: "admin", ControlBaseURL: "https://control.example", TokenTTLDays: 90}); err == nil {
		t.Fatal("missing public origin was accepted")
	}
	if _, err := NewConfigured(st, Config{AdminPasscode: "admin", PublicBaseURL: "https://pages.example", TokenTTLDays: 90}); err == nil {
		t.Fatal("missing control origin was accepted")
	}
	h := newDeviceTestServer(t, st)

	for _, tt := range []struct {
		host, path string
		want       int
	}{
		{"pages.localhost", "/theme.css", http.StatusOK},
		{"pages.localhost", "/admin/login", http.StatusMisdirectedRequest},
		{"control.localhost", "/theme.css", http.StatusMisdirectedRequest},
		{"unknown.localhost", "/healthz", http.StatusOK},
		{"unknown.localhost", "/", http.StatusMisdirectedRequest},
	} {
		req := httptest.NewRequest(http.MethodGet, "http://"+tt.host+tt.path, nil)
		req.Host = tt.host
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != tt.want {
			t.Errorf("%s%s status = %d, want %d", tt.host, tt.path, rec.Code, tt.want)
		}
	}

	req := httptest.NewRequest(http.MethodGet, "http://pages.localhost/.well-known/waymark", nil)
	req.Host = "pages.localhost"
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	var discovery map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&discovery); err != nil {
		t.Fatal(err)
	}
	if discovery["control_url"] != "http://control.localhost" || discovery["content_url"] != "http://pages.localhost" || discovery["device_authorization"] != true {
		t.Fatalf("discovery = %#v", discovery)
	}
}

func TestDeviceApprovalScopeAndRevocation(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "pages.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	h := newDeviceTestServer(t, st)
	secret := "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"
	code := createDeviceGrant(t, h, secret, []string{"pages:read"})
	cookie, _ := adminLogin(t, h)

	activate := doRequest(t, h, http.MethodGet, "control.localhost", "/activate?code="+code.UserCode, nil, cookie, "")
	body, _ := io.ReadAll(activate.Body)
	activate.Body.Close()
	if bytes.Contains(body, []byte("<escaped>")) || !bytes.Contains(body, []byte("&lt;escaped&gt;")) {
		t.Fatalf("device label was not escaped: %s", body)
	}
	for header, want := range map[string]string{
		"Cache-Control": "no-store", "Referrer-Policy": "same-origin", "X-Content-Type-Options": "nosniff",
	} {
		if got := activate.Header.Get(header); got != want {
			t.Fatalf("%s = %q, want %q", header, got, want)
		}
	}
	if !strings.Contains(activate.Header.Get("Content-Security-Policy"), "frame-ancestors 'none'") {
		t.Fatalf("CSP = %q", activate.Header.Get("Content-Security-Policy"))
	}
	csrf := extractCSRF(t, string(body))
	form := url.Values{"csrf": {csrf}, "code": {code.UserCode}, "decision": {"approved"}}
	decision := doRequest(t, h, http.MethodPost, "control.localhost", "/activate", strings.NewReader(form.Encode()), cookie, "http://control.localhost")
	if decision.StatusCode != http.StatusOK {
		data, _ := io.ReadAll(decision.Body)
		t.Fatalf("approve status = %d: %s", decision.StatusCode, data)
	}
	decision.Body.Close()

	pollBody, _ := json.Marshal(map[string]string{"device_code": code.DeviceCode, "device_secret": secret})
	poll := doJSONRequest(t, h, http.MethodPost, "control.localhost", "/api/auth/device/token", bytes.NewReader(pollBody), nil, "")
	var issued struct {
		AccessToken string `json:"access_token"`
	}
	if err := json.NewDecoder(poll.Body).Decode(&issued); err != nil {
		t.Fatal(err)
	}
	poll.Body.Close()
	if issued.AccessToken == "" {
		t.Fatal("poll did not return an access token")
	}
	replay := doJSONRequest(t, h, http.MethodPost, "control.localhost", "/api/auth/device/token", bytes.NewReader(pollBody), nil, "")
	var replayResult map[string]string
	if err := json.NewDecoder(replay.Body).Decode(&replayResult); err != nil {
		t.Fatal(err)
	}
	replay.Body.Close()
	if replay.StatusCode != http.StatusBadRequest || replayResult["error"] != "expired_token" {
		t.Fatalf("replayed grant = HTTP %d %#v, want expired_token", replay.StatusCode, replayResult)
	}
	tokensPage := doRequest(t, h, http.MethodGet, "control.localhost", "/admin/tokens", nil, cookie, "")
	tokensBody, _ := io.ReadAll(tokensPage.Body)
	tokensPage.Body.Close()
	if tokensPage.StatusCode != http.StatusOK || bytes.Contains(tokensBody, []byte(issued.AccessToken)) || !bytes.Contains(tokensBody, []byte("waymark_")) {
		t.Fatalf("token page status/body = %d, %s", tokensPage.StatusCode, tokensBody)
	}

	auth := authenticatedRequest(t, h, http.MethodGet, "control.localhost", "/api/auth", issued.AccessToken, nil)
	if auth.Code != http.StatusOK {
		t.Fatalf("auth status = %d: %s", auth.Code, auth.Body.String())
	}
	create := authenticatedRequest(t, h, http.MethodPost, "control.localhost", "/api/pages", issued.AccessToken, strings.NewReader(`{"title":"x","html":"x"}`))
	if create.Code != http.StatusForbidden {
		t.Fatalf("read-only create status = %d, want 403", create.Code)
	}
	contentAuth := authenticatedRequest(t, h, http.MethodGet, "pages.localhost", "/api/auth", issued.AccessToken, nil)
	if contentAuth.Code != http.StatusMisdirectedRequest {
		t.Fatalf("device token on content origin = %d, want 421", contentAuth.Code)
	}
	revoke := authenticatedRequest(t, h, http.MethodPost, "control.localhost", "/api/auth/revoke", issued.AccessToken, strings.NewReader(`{}`))
	if revoke.Code != http.StatusOK {
		t.Fatalf("revoke status = %d: %s", revoke.Code, revoke.Body.String())
	}
	after := authenticatedRequest(t, h, http.MethodGet, "control.localhost", "/api/auth", issued.AccessToken, nil)
	if after.Code != http.StatusUnauthorized {
		t.Fatalf("revoked auth status = %d, want 401", after.Code)
	}
}

func TestDeviceDenialExpiryAndPendingLimit(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "pages.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	h := newDeviceTestServer(t, st)
	baseTime := time.Date(2026, 6, 18, 12, 0, 0, 0, time.UTC)
	now := baseTime
	h.now = func() time.Time { return now }

	secret := "BBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBB"
	denied := createDeviceGrant(t, h, secret, []string{"pages:read"})
	if err := st.DecideDeviceAuthorization(h.hashLowEntropy(normalizeUserCode(denied.UserCode)), "denied", now); err != nil {
		t.Fatal(err)
	}
	payload, _ := json.Marshal(map[string]string{"device_code": denied.DeviceCode, "device_secret": secret})
	response := doJSONRequest(t, h, http.MethodPost, "control.localhost", "/api/auth/device/token", bytes.NewReader(payload), nil, "")
	var body map[string]string
	if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusBadRequest || body["error"] != "access_denied" {
		t.Fatalf("denied poll = HTTP %d %#v", response.StatusCode, body)
	}

	expiringSecret := "CCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCC"
	expiring := createDeviceGrant(t, h, expiringSecret, []string{"pages:read"})
	now = now.Add(11 * time.Minute)
	payload, _ = json.Marshal(map[string]string{"device_code": expiring.DeviceCode, "device_secret": expiringSecret})
	response = doJSONRequest(t, h, http.MethodPost, "control.localhost", "/api/auth/device/token", bytes.NewReader(payload), nil, "")
	body = map[string]string{}
	if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusBadRequest || body["error"] != "expired_token" {
		t.Fatalf("expired poll = HTTP %d %#v", response.StatusCode, body)
	}

	now = baseTime.Add(20 * time.Minute)
	for i := 0; i < 5; i++ {
		createDeviceGrant(t, h, fmt.Sprintf("%043d", i), []string{"pages:read"})
	}
	payload, _ = json.Marshal(map[string]any{"device_secret": fmt.Sprintf("%043d", 9), "device_label": "sixth", "scopes": []string{"pages:read"}})
	response = doJSONRequest(t, h, http.MethodPost, "control.localhost", "/api/auth/device/code", bytes.NewReader(payload), nil, "")
	response.Body.Close()
	if response.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("sixth pending grant status = %d, want 429", response.StatusCode)
	}
}

func TestDeviceTokenLookupFailureReturnsInternalError(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "pages.db"))
	if err != nil {
		t.Fatal(err)
	}
	h := newDeviceTestServer(t, st)
	secret := "DDDDDDDDDDDDDDDDDDDDDDDDDDDDDDDDDDDDDDDDDDD"
	code := createDeviceGrant(t, h, secret, []string{"pages:read"})
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}

	payload, _ := json.Marshal(map[string]string{"device_code": code.DeviceCode, "device_secret": secret})
	response := doJSONRequest(t, h, http.MethodPost, "control.localhost", "/api/auth/device/token", bytes.NewReader(payload), nil, "")
	response.Body.Close()
	if response.StatusCode != http.StatusInternalServerError {
		t.Fatalf("closed-store token lookup status = %d, want 500", response.StatusCode)
	}
}

func TestAdminFormsRequireOriginAndCSRF(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "pages.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	h := newDeviceTestServer(t, st)
	cookie, csrf := adminLogin(t, h)
	form := url.Values{"csrf": {csrf}}
	resp := doRequest(t, h, http.MethodPost, "control.localhost", "/admin/logout", strings.NewReader(form.Encode()), cookie, "http://evil.localhost")
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("bad-origin logout status = %d, want 403", resp.StatusCode)
	}
	resp.Body.Close()
	resp = doRequest(t, h, http.MethodPost, "control.localhost", "/admin/logout", strings.NewReader(form.Encode()), cookie, "null")
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("null-origin logout status = %d, want 403", resp.StatusCode)
	}
	resp.Body.Close()
	resp = doRequest(t, h, http.MethodPost, "control.localhost", "/admin/logout", strings.NewReader("csrf=bad"), cookie, "http://control.localhost")
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("bad-csrf logout status = %d, want 403", resp.StatusCode)
	}
	resp.Body.Close()
	forged := &http.Cookie{Name: "waymark_admin", Value: "missing-session"}
	request := httptest.NewRequest(http.MethodPost, "http://control.localhost/admin/logout", strings.NewReader(form.Encode()))
	request.Host = "control.localhost"
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Header.Set("Origin", "http://control.localhost")
	request.AddCookie(forged)
	recorder := httptest.NewRecorder()
	h.handleAdminLogout(recorder, request)
	if recorder.Code != http.StatusForbidden {
		t.Fatalf("forged-session logout status = %d, want 403", recorder.Code)
	}
}

func TestRenderAdminBuffersTemplateBeforeWriting(t *testing.T) {
	tmpl := template.Must(template.New("failing").Funcs(template.FuncMap{
		"fail": func() (string, error) { return "", errors.New("render failed") },
	}).Parse(`partial output{{fail}}`))
	recorder := httptest.NewRecorder()
	(&Server{}).renderAdmin(recorder, tmpl, adminView{})
	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("render failure status = %d, want 500", recorder.Code)
	}
	if strings.Contains(recorder.Body.String(), "partial output") {
		t.Fatalf("partial template output was committed: %q", recorder.Body.String())
	}
}

func TestAnonymousAdminSessionCreationIsRateLimited(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "pages.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	h := newDeviceTestServer(t, st)
	for i := 0; i < 10; i++ {
		response := doRequest(t, h, http.MethodGet, "control.localhost", "/admin/login", nil, nil, "")
		response.Body.Close()
		if response.StatusCode != http.StatusOK {
			t.Fatalf("session request %d status = %d, want 200", i+1, response.StatusCode)
		}
	}
	response := doRequest(t, h, http.MethodGet, "control.localhost", "/admin/login", nil, nil, "")
	response.Body.Close()
	if response.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("eleventh session request status = %d, want 429", response.StatusCode)
	}
}

func TestWrongAdminPasscodeRendersStyledRetryWithActivationContext(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "pages.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	h := newDeviceTestServer(t, st)
	next := "/activate?code=ABCD-EFGH"
	login := doRequest(t, h, http.MethodGet, "control.localhost", "/admin/login?next="+url.QueryEscape(next), nil, nil, "")
	body, _ := io.ReadAll(login.Body)
	login.Body.Close()
	cookie := login.Cookies()[0]
	csrf := extractCSRF(t, string(body))

	form := url.Values{"csrf": {csrf}, "passcode": {"wrong"}, "next": {next}}
	retry := doRequest(t, h, http.MethodPost, "control.localhost", "/admin/login", strings.NewReader(form.Encode()), cookie, "http://control.localhost")
	retryBody, _ := io.ReadAll(retry.Body)
	retry.Body.Close()
	if retry.StatusCode != http.StatusUnauthorized {
		t.Fatalf("wrong passcode status = %d, want 401", retry.StatusCode)
	}
	if retry.Header.Get("Content-Type") != "text/html; charset=utf-8" || !bytes.Contains(retryBody, []byte("Owner sign in")) || !bytes.Contains(retryBody, []byte("Incorrect admin passcode. Try again.")) {
		t.Fatalf("wrong passcode response was not styled: %s", retryBody)
	}
	if !bytes.Contains(retryBody, []byte(`name="next" value="/activate?code=ABCD-EFGH"`)) {
		t.Fatalf("activation context was not preserved: %s", retryBody)
	}
	if retry.Header.Get("Cache-Control") != "no-store" || retry.Header.Get("Referrer-Policy") != "same-origin" {
		t.Fatalf("security headers missing from retry: %#v", retry.Header)
	}

	form.Set("csrf", extractCSRF(t, string(retryBody)))
	form.Set("passcode", "admin-secret")
	success := doRequest(t, h, http.MethodPost, "control.localhost", "/admin/login", strings.NewReader(form.Encode()), cookie, "http://control.localhost")
	success.Body.Close()
	if success.StatusCode != http.StatusSeeOther || success.Header.Get("Location") != next {
		t.Fatalf("retry success = %d, location %q", success.StatusCode, success.Header.Get("Location"))
	}
}

func TestProductionAdminCookieIsHostOnlySecureAndStrict(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "pages.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	h, err := NewConfigured(st, Config{PublicBaseURL: "https://pages.example.com", ControlBaseURL: "https://control.example.com", AdminPasscode: "admin", TokenTTLDays: 90})
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodGet, "https://control.example.com/admin/login", nil)
	req.Host = "control.example.com"
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	cookies := rec.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatalf("cookies = %d, want 1", len(cookies))
	}
	cookie := cookies[0]
	if cookie.Name != "__Host-waymark_admin" || !cookie.Secure || !cookie.HttpOnly || cookie.SameSite != http.SameSiteStrictMode || cookie.Domain != "" || cookie.Path != "/" {
		t.Fatalf("unsafe production cookie: %#v", cookie)
	}
}

func TestRequestSourceTrustsForwardedIPOnlyWhenConfigured(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "pages.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	h := newDeviceTestServer(t, st)
	req := httptest.NewRequest(http.MethodGet, "http://control.localhost/admin/login", nil)
	req.RemoteAddr = "198.51.100.10:4321"
	req.Header.Set("X-Real-IP", "203.0.113.20")

	directKey, directHint := h.requestSource(req)
	if directHint != "198.51.100.x" {
		t.Fatalf("direct source hint = %q, want peer address", directHint)
	}
	h.trustForwardedIP = true
	forwardedKey, forwardedHint := h.requestSource(req)
	if forwardedHint != "203.0.113.x" {
		t.Fatalf("trusted source hint = %q, want forwarded address", forwardedHint)
	}
	if directKey == forwardedKey {
		t.Fatal("peer and forwarded addresses produced the same source key")
	}
}

type deviceCodeTestResponse struct {
	DeviceCode string `json:"device_code"`
	UserCode   string `json:"user_code"`
}

func newDeviceTestServer(t *testing.T, st *store.Store) *Server {
	t.Helper()
	h, err := NewConfigured(st, Config{AdminPasscode: "admin-secret", PublicBaseURL: "http://pages.localhost", ControlBaseURL: "http://control.localhost", TokenTTLDays: 90})
	if err != nil {
		t.Fatal(err)
	}
	return h
}

func createDeviceGrant(t *testing.T, h http.Handler, secret string, scopes []string) deviceCodeTestResponse {
	t.Helper()
	body, _ := json.Marshal(map[string]any{"device_secret": secret, "device_label": "test device <escaped>", "scopes": scopes})
	resp := doJSONRequest(t, h, http.MethodPost, "control.localhost", "/api/auth/device/code", bytes.NewReader(body), nil, "")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		data, _ := io.ReadAll(resp.Body)
		t.Fatalf("device code status = %d: %s", resp.StatusCode, data)
	}
	var code deviceCodeTestResponse
	if err := json.NewDecoder(resp.Body).Decode(&code); err != nil {
		t.Fatal(err)
	}
	return code
}

func adminLogin(t *testing.T, h http.Handler) (*http.Cookie, string) {
	t.Helper()
	login := doRequest(t, h, http.MethodGet, "control.localhost", "/admin/login?next=/activate", nil, nil, "")
	body, _ := io.ReadAll(login.Body)
	login.Body.Close()
	cookies := login.Cookies()
	if len(cookies) != 1 {
		t.Fatalf("login cookies = %d, want 1", len(cookies))
	}
	csrf := extractCSRF(t, string(body))
	form := url.Values{"csrf": {csrf}, "passcode": {"admin-secret"}, "next": {"/activate"}}
	response := doRequest(t, h, http.MethodPost, "control.localhost", "/admin/login", strings.NewReader(form.Encode()), cookies[0], "http://control.localhost")
	response.Body.Close()
	if response.StatusCode != http.StatusSeeOther {
		t.Fatalf("admin login status = %d, want 303", response.StatusCode)
	}
	return cookies[0], csrf
}

func extractCSRF(t *testing.T, body string) string {
	t.Helper()
	match := regexp.MustCompile(`name="csrf" value="([a-f0-9]+)"`).FindStringSubmatch(body)
	if len(match) != 2 {
		t.Fatalf("csrf token missing from %s", body)
	}
	return match[1]
}

func doRequest(t *testing.T, h http.Handler, method, host, path string, body io.Reader, cookie *http.Cookie, origin string) *http.Response {
	t.Helper()
	return doTypedRequest(t, h, method, host, path, body, cookie, origin, "application/x-www-form-urlencoded")
}

func doJSONRequest(t *testing.T, h http.Handler, method, host, path string, body io.Reader, cookie *http.Cookie, origin string) *http.Response {
	t.Helper()
	return doTypedRequest(t, h, method, host, path, body, cookie, origin, "application/json")
}

func doTypedRequest(t *testing.T, h http.Handler, method, host, path string, body io.Reader, cookie *http.Cookie, origin, contentType string) *http.Response {
	t.Helper()
	req := httptest.NewRequest(method, "http://"+host+path, body)
	req.Host = host
	if body != nil {
		req.Header.Set("Content-Type", contentType)
	}
	if cookie != nil {
		req.AddCookie(cookie)
	}
	if origin != "" {
		req.Header.Set("Origin", origin)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec.Result()
}

func authenticatedRequest(t *testing.T, h http.Handler, method, host, path, token string, body io.Reader) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, "http://"+host+path, body)
	req.Host = host
	req.Header.Set("Authorization", "Bearer "+token)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}
