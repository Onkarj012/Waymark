package web

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"html/template"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/Onkarj012/Waymark/internal/store"
)

const (
	preAuthSessionLifetime = 15 * time.Minute
	adminSessionLifetime   = 30 * 24 * time.Hour
)

var adminPage = template.Must(template.New("admin").Parse(`<!doctype html>
<html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1">
<title>{{.Title}} - Waymark</title><link rel="stylesheet" href="/admin/style.css"><link rel="icon" href="/favicon.svg" type="image/svg+xml"></head>
<body><main><header><a href="/admin/tokens">Waymark</a><span>Control</span></header>{{template "content" .}}</main></body></html>`))

const adminCSS = `:root{color-scheme:light dark;font:16px/1.5 system-ui,sans-serif}body{margin:0;background:#f5f6f7;color:#17191c}main{max-width:42rem;margin:3rem auto;padding:0 1rem}header{display:flex;justify-content:space-between;margin-bottom:2rem}section{background:#fff;border:1px solid #dfe2e5;border-radius:6px;padding:1.5rem;margin-bottom:1rem}label{display:block;font-weight:600;margin:.75rem 0 .25rem}input{box-sizing:border-box;width:100%;padding:.65rem;border:1px solid #a9afb5;border-radius:4px}button{padding:.65rem 1rem;border:0;border-radius:4px;background:#1769aa;color:#fff;font-weight:600;cursor:pointer}.danger{background:#a62b2b}.actions{display:flex;gap:.75rem;margin-top:1rem}.muted{color:#687078;font-size:.9rem}code{font-family:ui-monospace,monospace}@media(prefers-color-scheme:dark){body{background:#111315;color:#e8eaed}section{background:#191c1f;border-color:#34393e}input{background:#111315;color:#fff;border-color:#596169}a{color:#77bdf2}.muted{color:#aab0b6}}`

type adminView struct {
	Title       string
	Next        string
	CSRF        string
	Code        string
	Grant       *store.DeviceAuthorization
	Tokens      []store.APIToken
	Error       string
	Message     string
	ControlURL  string
	CurrentTime time.Time
}

func (s *Server) handleAdminCSS(w http.ResponseWriter, r *http.Request) {
	s.setControlHeaders(w)
	w.Header().Set("Content-Type", "text/css; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Write([]byte(adminCSS))
}

func (s *Server) handleAdminLogin(w http.ResponseWriter, r *http.Request) {
	s.setControlHeaders(w)
	next := sanitizeNext(r.URL.Query().Get("next"))
	if _, _, err := s.adminSession(r); err != nil {
		sourceKey, _ := s.requestSource(r)
		if !s.limiter.allow("session:"+sourceKey, 10, 15*time.Minute, s.now()) {
			http.Error(w, "too many session requests", http.StatusTooManyRequests)
			return
		}
	}
	_, raw, err := s.ensureAdminSession(w, r)
	if err != nil {
		if errors.Is(err, store.ErrSessionLimit) {
			http.Error(w, "too many active sessions", http.StatusTooManyRequests)
			return
		}
		http.Error(w, "could not start session", http.StatusInternalServerError)
		return
	}
	s.renderAdminLogin(w, http.StatusOK, adminView{Title: "Sign in", Next: next, CSRF: s.csrfToken(raw)})
}

func (s *Server) handleAdminLoginPost(w http.ResponseWriter, r *http.Request) {
	s.setControlHeaders(w)
	if !parseAdminForm(w, r) {
		return
	}
	if !s.validOrigin(r) {
		http.Error(w, "invalid origin", http.StatusForbidden)
		return
	}
	session, raw, err := s.adminSession(r)
	if err != nil || !s.validCSRF(raw, r.FormValue("csrf")) {
		http.Error(w, "invalid session", http.StatusForbidden)
		return
	}
	sourceKey, _ := s.requestSource(r)
	if !s.limiter.allow("login:"+sourceKey, 5, 15*time.Minute, s.now()) {
		http.Error(w, "too many attempts", http.StatusTooManyRequests)
		return
	}
	if !constantTimeSecretEqual(r.FormValue("passcode"), s.adminPasscode) {
		s.renderAdminLogin(w, http.StatusUnauthorized, adminView{
			Title: "Sign in", Next: sanitizeNext(r.FormValue("next")), CSRF: s.csrfToken(raw),
			Error: "Incorrect admin passcode. Try again.",
		})
		return
	}
	if err := s.store.AuthenticateAdminSession(session.ID, s.now().Add(adminSessionLifetime)); err != nil {
		http.Error(w, "could not authenticate session", http.StatusInternalServerError)
		return
	}
	s.setAdminCookie(w, raw, adminSessionLifetime)
	http.Redirect(w, r, sanitizeNext(r.FormValue("next")), http.StatusSeeOther)
}

func (s *Server) renderAdminLogin(w http.ResponseWriter, status int, view adminView) {
	tmpl := template.Must(adminPage.Clone())
	template.Must(tmpl.New("content").Parse(`<section><h1>Owner sign in</h1>{{if .Error}}<p role="alert">{{.Error}}</p>{{end}}<form method="post" action="/admin/login"><input type="hidden" name="csrf" value="{{.CSRF}}"><input type="hidden" name="next" value="{{.Next}}"><label for="passcode">Admin passcode</label><input id="passcode" name="passcode" type="password" required autofocus autocomplete="current-password"><div class="actions"><button type="submit">Sign in</button></div></form></section>`))
	s.renderAdminStatus(w, status, tmpl, view)
}

func (s *Server) requireAdmin(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		s.setControlHeaders(w)
		session, _, err := s.adminSession(r)
		if err != nil || !session.Authenticated {
			nextPath := sanitizeNext(r.URL.RequestURI())
			http.Redirect(w, r, "/admin/login?next="+url.QueryEscape(nextPath), http.StatusSeeOther)
			return
		}
		next(w, r)
	}
}

func (s *Server) handleActivate(w http.ResponseWriter, r *http.Request) {
	_, raw, _ := s.adminSession(r)
	code := strings.ToUpper(strings.TrimSpace(r.URL.Query().Get("code")))
	view := adminView{Title: "Activate device", Code: code, CSRF: s.csrfToken(raw)}
	if code != "" {
		sourceKey, _ := s.requestSource(r)
		if !s.limiter.allow("lookup:"+sourceKey, 20, 10*time.Minute, s.now()) {
			view.Error = "Too many code lookups. Try again later."
		} else {
			grant, err := s.store.DeviceAuthorizationByUserCode(s.hashLowEntropy(normalizeUserCode(code)), s.now())
			if err != nil || grant.Status != "pending" {
				view.Error = "That code is invalid or expired."
			} else {
				view.Grant = grant
			}
		}
	}
	tmpl := template.Must(adminPage.Clone())
	template.Must(tmpl.New("content").Parse(`<section><h1>Activate a device</h1>{{if .Error}}<p>{{.Error}}</p>{{end}}{{if .Message}}<p>{{.Message}}</p>{{end}}{{if .Grant}}<p><strong>{{.Grant.DeviceLabel}}</strong></p><p class="muted">Scopes: <code>{{.Grant.Scopes}}</code><br>Requested {{.Grant.CreatedAt.Format "Jan 2, 2006 at 3:04 PM UTC"}}<br>Source: {{.Grant.SourceHint}}</p><form method="post" action="/activate"><input type="hidden" name="csrf" value="{{.CSRF}}"><input type="hidden" name="code" value="{{.Code}}"><div class="actions"><button name="decision" value="approved" type="submit">Approve</button><button class="danger" name="decision" value="denied" type="submit">Deny</button></div></form>{{else}}<form method="get" action="/activate"><label for="code">Device code</label><input id="code" name="code" value="{{.Code}}" placeholder="ABCD-EFGH" required autofocus><div class="actions"><button type="submit">Continue</button></div></form>{{end}}</section>`))
	s.renderAdmin(w, tmpl, view)
}

func (s *Server) handleActivateDecision(w http.ResponseWriter, r *http.Request) {
	if !parseAdminForm(w, r) {
		return
	}
	if !s.validOrigin(r) {
		http.Error(w, "invalid origin", http.StatusForbidden)
		return
	}
	_, raw, err := s.adminSession(r)
	if err != nil || !s.validCSRF(raw, r.FormValue("csrf")) {
		http.Error(w, "invalid csrf token", http.StatusForbidden)
		return
	}
	decision := r.FormValue("decision")
	if decision != "approved" && decision != "denied" {
		http.Error(w, "invalid decision", http.StatusBadRequest)
		return
	}
	code := strings.ToUpper(strings.TrimSpace(r.FormValue("code")))
	if err := s.store.DecideDeviceAuthorization(s.hashLowEntropy(normalizeUserCode(code)), decision, s.now()); err != nil {
		http.Error(w, "code is invalid or expired", http.StatusBadRequest)
		return
	}
	tmpl := template.Must(adminPage.Clone())
	template.Must(tmpl.New("content").Parse(`<section><h1>Device {{.Message}}</h1><p>You can return to the terminal.</p></section>`))
	s.renderAdmin(w, tmpl, adminView{Title: "Device " + decision, Message: decision})
}

func (s *Server) handleAdminTokens(w http.ResponseWriter, r *http.Request) {
	_, raw, _ := s.adminSession(r)
	tokens, err := s.store.ListAPITokens()
	if err != nil {
		http.Error(w, "could not list tokens", http.StatusInternalServerError)
		return
	}
	tmpl := template.Must(adminPage.Clone())
	template.Must(tmpl.New("content").Parse(`<section><h1>Device tokens</h1>{{range .Tokens}}<article><p><strong>{{.DeviceLabel}}</strong> <code>{{.DisplayPrefix}}</code></p><p class="muted">{{.Scopes}}<br>Expires {{.ExpiresAt.Format "Jan 2, 2006"}}{{if .RevokedAt}} - revoked{{end}}</p>{{if not .RevokedAt}}<form method="post" action="/admin/tokens/{{.ID}}/revoke"><input type="hidden" name="csrf" value="{{$.CSRF}}"><button class="danger" type="submit">Revoke</button></form>{{end}}</article>{{else}}<p>No device tokens have been issued.</p>{{end}}</section><form method="post" action="/admin/logout"><input type="hidden" name="csrf" value="{{.CSRF}}"><button type="submit">Sign out</button></form>`))
	s.renderAdmin(w, tmpl, adminView{Title: "Device tokens", Tokens: tokens, CSRF: s.csrfToken(raw)})
}

func (s *Server) handleAdminTokenRevoke(w http.ResponseWriter, r *http.Request) {
	if !parseAdminForm(w, r) {
		return
	}
	if _, ok := s.validatedAdminSession(r); !ok {
		http.Error(w, "invalid form", http.StatusForbidden)
		return
	}
	if err := s.store.RevokeAPIToken(r.PathValue("id"), s.now()); err != nil && !errors.Is(err, store.ErrNotFound) {
		http.Error(w, "could not revoke token", http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, "/admin/tokens", http.StatusSeeOther)
}

func (s *Server) handleAdminLogout(w http.ResponseWriter, r *http.Request) {
	if !parseAdminForm(w, r) {
		return
	}
	session, ok := s.validatedAdminSession(r)
	if !ok {
		http.Error(w, "invalid form", http.StatusForbidden)
		return
	}
	if err := s.store.DeleteAdminSession(session.ID); err != nil {
		http.Error(w, "could not end session", http.StatusInternalServerError)
		return
	}
	s.clearAdminCookie(w)
	http.Redirect(w, r, "/admin/login", http.StatusSeeOther)
}

func (s *Server) ensureAdminSession(w http.ResponseWriter, r *http.Request) (*store.AdminSession, string, error) {
	if session, raw, err := s.adminSession(r); err == nil {
		return session, raw, nil
	}
	raw, err := randomBase64(32)
	if err != nil {
		return nil, "", err
	}
	id, err := randomBase64(12)
	if err != nil {
		return nil, "", err
	}
	now := s.now().UTC()
	session := store.AdminSession{ID: id, SessionHash: s.hashLowEntropy(raw), CreatedAt: now, ExpiresAt: now.Add(preAuthSessionLifetime)}
	if err := s.store.CreateAdminSession(session); err != nil {
		return nil, "", err
	}
	s.setAdminCookie(w, raw, preAuthSessionLifetime)
	return &session, raw, nil
}

func (s *Server) adminSession(r *http.Request) (*store.AdminSession, string, error) {
	cookie, err := r.Cookie(s.adminCookieName())
	if err != nil || cookie.Value == "" {
		return nil, "", store.ErrNotFound
	}
	session, err := s.store.AdminSessionByHash(s.hashLowEntropy(cookie.Value), s.now())
	return session, cookie.Value, err
}

func (s *Server) adminCookieName() string {
	if s.secureCookie {
		return "__Host-waymark_admin"
	}
	return "waymark_admin"
}

func (s *Server) clearAdminCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{Name: s.adminCookieName(), Value: "", Path: "/", HttpOnly: true, Secure: s.secureCookie, SameSite: http.SameSiteStrictMode, MaxAge: -1})
}

func (s *Server) setAdminCookie(w http.ResponseWriter, raw string, lifetime time.Duration) {
	http.SetCookie(w, &http.Cookie{Name: s.adminCookieName(), Value: raw, Path: "/", HttpOnly: true, Secure: s.secureCookie, SameSite: http.SameSiteStrictMode, MaxAge: int(lifetime.Seconds())})
}

func (s *Server) csrfToken(raw string) string {
	h := hmac.New(sha256.New, []byte(s.adminPasscode))
	h.Write([]byte("csrf:" + raw))
	return hex.EncodeToString(h.Sum(nil))
}

func (s *Server) validCSRF(raw, supplied string) bool {
	want := s.csrfToken(raw)
	return subtle.ConstantTimeCompare([]byte(want), []byte(supplied)) == 1
}

func (s *Server) validOrigin(r *http.Request) bool {
	return r.Header.Get("Origin") == s.controlURL
}

func (s *Server) validatedAdminSession(r *http.Request) (*store.AdminSession, bool) {
	session, raw, err := s.adminSession(r)
	if err != nil || !s.validOrigin(r) || !s.validCSRF(raw, r.FormValue("csrf")) {
		return nil, false
	}
	return session, true
}

func (s *Server) setControlHeaders(w http.ResponseWriter) {
	w.Header().Set("Cache-Control", "no-store")
	// Chrome serializes same-origin form submissions with Origin: null under
	// no-referrer, which makes exact-origin CSRF enforcement reject valid forms.
	w.Header().Set("Referrer-Policy", "same-origin")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; style-src 'self'; form-action 'self'; base-uri 'none'; frame-ancestors 'none'")
}

func (s *Server) renderAdmin(w http.ResponseWriter, tmpl *template.Template, view adminView) {
	s.renderAdminStatus(w, http.StatusOK, tmpl, view)
}

func (s *Server) renderAdminStatus(w http.ResponseWriter, status int, tmpl *template.Template, view adminView) {
	var output bytes.Buffer
	if err := tmpl.Execute(&output, view); err != nil {
		http.Error(w, "could not render page", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	_, _ = w.Write(output.Bytes())
}

func sanitizeNext(value string) string {
	if value == "" || !strings.HasPrefix(value, "/") || strings.HasPrefix(value, "//") || strings.Contains(value, "\\") {
		return "/activate"
	}
	u, err := url.Parse(value)
	if err != nil || u.IsAbs() || u.Host != "" {
		return "/activate"
	}
	return u.RequestURI()
}

func parseAdminForm(w http.ResponseWriter, r *http.Request) bool {
	r.Body = http.MaxBytesReader(w, r.Body, 64<<10)
	if err := r.ParseForm(); err != nil {
		http.Error(w, "invalid form", http.StatusBadRequest)
		return false
	}
	return true
}
