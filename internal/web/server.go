// Package web implements the Waymark HTTP server: a scoped JSON API for
// managing pages, and public, unguessable view URLs.
package web

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/Onkarj012/Waymark/internal/render"
	"github.com/Onkarj012/Waymark/internal/store"
	"github.com/Onkarj012/Waymark/theme"
)

const maxBodyBytes = 8 << 20 // 8 MiB cap on uploaded HTML

// Server is the HTTP handler for Waymark.
type Server struct {
	store            *store.Store
	adminPasscode    string
	baseURL          string
	controlURL       string
	publicHost       string
	controlHost      string
	tokenTTLDays     int
	secureCookie     bool
	trustForwardedIP bool
	mux              *http.ServeMux
	limiter          *rateLimiter
	now              func() time.Time
}

// Config controls the public/content and private/control origins.
type Config struct {
	AdminPasscode    string
	PublicBaseURL    string
	ControlBaseURL   string
	TokenTTLDays     int
	TrustForwardedIP bool
}

// NewConfigured builds a server and rejects unsafe origin combinations.
func NewConfigured(st *store.Store, cfg Config) (*Server, error) {
	publicURL, publicHost, _, err := parseConfiguredOrigin(cfg.PublicBaseURL)
	if err != nil {
		return nil, fmt.Errorf("PUBLIC_BASE_URL: %w", err)
	}
	controlURL, controlHost, secure, err := parseConfiguredOrigin(cfg.ControlBaseURL)
	if err != nil {
		return nil, fmt.Errorf("CONTROL_BASE_URL: %w", err)
	}
	if cfg.AdminPasscode == "" {
		return nil, errors.New("WAYMARK_ADMIN_PASSCODE is required")
	}
	if publicURL == "" {
		return nil, errors.New("PUBLIC_BASE_URL is required")
	}
	if controlURL == "" {
		return nil, errors.New("CONTROL_BASE_URL is required")
	}
	if publicURL == controlURL {
		return nil, errors.New("PUBLIC_BASE_URL and CONTROL_BASE_URL must use different origins")
	}
	if cfg.TokenTTLDays == 0 {
		cfg.TokenTTLDays = 90
	}
	if cfg.TokenTTLDays < 1 || cfg.TokenTTLDays > 365 {
		return nil, errors.New("WAYMARK_TOKEN_TTL_DAYS must be between 1 and 365")
	}
	s := &Server{
		store: st, adminPasscode: cfg.AdminPasscode,
		baseURL: publicURL, controlURL: controlURL, publicHost: publicHost, controlHost: controlHost,
		tokenTTLDays: cfg.TokenTTLDays, trustForwardedIP: cfg.TrustForwardedIP,
		secureCookie: secure, limiter: newRateLimiter(4096), now: time.Now,
	}
	mux := http.NewServeMux()

	// Public.
	mux.HandleFunc("GET /healthz", s.handleHealth)
	mux.HandleFunc("GET /theme.css", s.handleThemeCSS)
	mux.HandleFunc("GET /p/{id}", s.handleServePage)
	mux.HandleFunc("GET /{$}", s.handleIndex)
	mux.HandleFunc("GET /.well-known/waymark", s.handleDiscovery)

	// Authenticated JSON API.
	mux.HandleFunc("GET /api/auth", s.auth("pages:read", s.handleAuthCheck))
	mux.HandleFunc("POST /api/pages", s.auth("pages:write", s.handleCreate))
	mux.HandleFunc("GET /api/pages", s.auth("pages:read", s.handleList))
	mux.HandleFunc("GET /api/pages/{id}", s.auth("pages:read", s.handleGetMeta))
	mux.HandleFunc("PUT /api/pages/{id}", s.auth("pages:write", s.handleUpdate))
	mux.HandleFunc("DELETE /api/pages/{id}", s.auth("pages:write", s.handleDelete))

	mux.HandleFunc("POST /api/auth/device/code", s.handleDeviceCode)
	mux.HandleFunc("POST /api/auth/device/token", s.handleDeviceToken)
	mux.HandleFunc("POST /api/auth/revoke", s.auth("pages:read", s.handleSelfRevoke))
	mux.HandleFunc("GET /activate", s.requireAdmin(s.handleActivate))
	mux.HandleFunc("POST /activate", s.requireAdmin(s.handleActivateDecision))
	mux.HandleFunc("GET /admin/login", s.handleAdminLogin)
	mux.HandleFunc("POST /admin/login", s.handleAdminLoginPost)
	mux.HandleFunc("POST /admin/logout", s.requireAdmin(s.handleAdminLogout))
	mux.HandleFunc("GET /admin/tokens", s.requireAdmin(s.handleAdminTokens))
	mux.HandleFunc("POST /admin/tokens/{id}/revoke", s.requireAdmin(s.handleAdminTokenRevoke))
	mux.HandleFunc("GET /admin/style.css", s.handleAdminCSS)

	s.mux = mux
	return s, nil
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	rec := &statusRecorder{ResponseWriter: w, status: 200}
	start := time.Now()
	if strings.EqualFold(r.Host, s.controlHost) {
		s.setControlHeaders(rec)
	}
	if !s.routeAllowed(r) {
		http.Error(rec, "misdirected request", http.StatusMisdirectedRequest)
	} else {
		s.mux.ServeHTTP(rec, r)
	}
	log.Printf("%s %s -> %d (%s)", r.Method, logPath(r.URL.Path), rec.status, time.Since(start).Round(time.Millisecond))
}

// --- public handlers -------------------------------------------------------

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	io.WriteString(w, "ok")
}

func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	io.WriteString(w, "Waymark\n")
}

// handleAuthCheck returns token metadata after auth() verifies the request.
func (s *Server) handleAuthCheck(w http.ResponseWriter, r *http.Request) {
	credential := credentialFromContext(r.Context())
	s.writeJSON(w, http.StatusOK, map[string]any{
		"ok": true, "credential_type": credential.Kind, "scopes": credential.Scopes,
		"label": credential.Label, "expires_at": credential.ExpiresAt,
	})
}

func (s *Server) handleThemeCSS(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/css; charset=utf-8")
	w.Header().Set("Cache-Control", "public, max-age=3600")
	io.WriteString(w, theme.CSS)
}

func (s *Server) handleServePage(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	p, err := s.store.Get(id)
	if errors.Is(err, store.ErrNotFound) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	if p.ExpiresAt != nil && !p.ExpiresAt.After(s.now()) {
		http.NotFound(w, r) // expired
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	if p.Raw {
		io.WriteString(w, p.HTML)
		return
	}
	io.WriteString(w, render.Hosted(p.Title, p.HTML))
}

// --- API: requests & responses ---------------------------------------------

type createReq struct {
	Title   string `json:"title"`
	Slug    string `json:"slug,omitempty"`
	HTML    string `json:"html"`
	Raw     bool   `json:"raw,omitempty"`
	TTLDays int    `json:"ttl_days,omitempty"`
}

type updateReq struct {
	Title       *string    `json:"title,omitempty"`
	Slug        *string    `json:"slug,omitempty"`
	HTML        *string    `json:"html,omitempty"`
	Raw         *bool      `json:"raw,omitempty"`
	TTLDays     *int       `json:"ttl_days,omitempty"` // >0 sets expiry; <=0 clears it
	IfUpdatedAt *time.Time `json:"if_updated_at,omitempty"`
}

type pageResp struct {
	ID        string     `json:"id"`
	URL       string     `json:"url"`
	Title     string     `json:"title"`
	Slug      string     `json:"slug,omitempty"`
	Raw       bool       `json:"raw"`
	CreatedAt time.Time  `json:"created_at"`
	UpdatedAt time.Time  `json:"updated_at"`
	ExpiresAt *time.Time `json:"expires_at,omitempty"`
	Size      int        `json:"size,omitempty"`
}

// --- API handlers ----------------------------------------------------------

func (s *Server) handleCreate(w http.ResponseWriter, r *http.Request) {
	var req createReq
	if !s.decode(w, r, &req) {
		return
	}
	req.Title = strings.TrimSpace(req.Title)
	if req.Title == "" {
		s.writeErr(w, http.StatusBadRequest, "title is required")
		return
	}
	if strings.TrimSpace(req.HTML) == "" {
		s.writeErr(w, http.StatusBadRequest, "html is required")
		return
	}

	now := s.now().UTC()
	p := &store.Page{
		Title:     req.Title,
		Slug:      strings.TrimSpace(req.Slug),
		HTML:      req.HTML,
		Raw:       req.Raw,
		CreatedAt: now,
		UpdatedAt: now,
		ExpiresAt: ttlToExpiry(now, req.TTLDays),
	}

	// Generate a unique id (collisions are astronomically unlikely; retry anyway).
	var createErr error
	for attempt := 0; attempt < 5; attempt++ {
		id, err := newID()
		if err != nil {
			s.writeErr(w, http.StatusInternalServerError, "could not generate id")
			return
		}
		p.ID = id
		if createErr = s.store.Create(p); createErr == nil {
			break
		}
	}
	if createErr != nil {
		s.writeErr(w, http.StatusInternalServerError, "could not save page")
		return
	}

	s.writeJSON(w, http.StatusCreated, s.toResp(p, len(p.HTML)))
}

func (s *Server) handleList(w http.ResponseWriter, r *http.Request) {
	limit := 50
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			limit = n
		}
	}
	metas, err := s.store.List(limit)
	if err != nil {
		s.writeErr(w, http.StatusInternalServerError, "could not list pages")
		return
	}
	out := make([]pageResp, 0, len(metas))
	for _, m := range metas {
		out = append(out, pageResp{
			ID:        m.ID,
			URL:       s.pageURL(m.ID),
			Title:     m.Title,
			Slug:      m.Slug,
			Raw:       m.Raw,
			CreatedAt: m.CreatedAt,
			UpdatedAt: m.UpdatedAt,
			ExpiresAt: m.ExpiresAt,
			Size:      m.Size,
		})
	}
	s.writeJSON(w, http.StatusOK, map[string]any{"pages": out})
}

func (s *Server) handleGetMeta(w http.ResponseWriter, r *http.Request) {
	p, err := s.store.Get(r.PathValue("id"))
	if errors.Is(err, store.ErrNotFound) {
		s.writeErr(w, http.StatusNotFound, "page not found")
		return
	}
	if err != nil {
		s.writeErr(w, http.StatusInternalServerError, "could not load page")
		return
	}
	s.writeJSON(w, http.StatusOK, s.toResp(p, len(p.HTML)))
}

func (s *Server) handleUpdate(w http.ResponseWriter, r *http.Request) {
	var req updateReq
	if !s.decode(w, r, &req) {
		return
	}
	p, err := s.store.Get(r.PathValue("id"))
	if errors.Is(err, store.ErrNotFound) {
		s.writeErr(w, http.StatusNotFound, "page not found")
		return
	}
	if err != nil {
		s.writeErr(w, http.StatusInternalServerError, "could not load page")
		return
	}

	if req.Title != nil {
		t := strings.TrimSpace(*req.Title)
		if t == "" {
			s.writeErr(w, http.StatusBadRequest, "title cannot be empty")
			return
		}
		p.Title = t
	}
	if req.Slug != nil {
		p.Slug = strings.TrimSpace(*req.Slug)
	}
	if req.HTML != nil {
		if strings.TrimSpace(*req.HTML) == "" {
			s.writeErr(w, http.StatusBadRequest, "html cannot be empty")
			return
		}
		p.HTML = *req.HTML
	}
	if req.Raw != nil {
		p.Raw = *req.Raw
	}
	now := s.now().UTC()
	if req.TTLDays != nil {
		p.ExpiresAt = ttlToExpiry(now, *req.TTLDays)
	}
	if !now.After(p.UpdatedAt) {
		now = p.UpdatedAt.UTC().Add(time.Nanosecond)
	}
	p.UpdatedAt = now

	var saveErr error
	if req.IfUpdatedAt != nil {
		saveErr = s.store.SaveIfUpdatedAt(p, *req.IfUpdatedAt)
	} else {
		saveErr = s.store.Save(p)
	}
	if errors.Is(saveErr, store.ErrConflict) {
		s.writeErr(w, http.StatusConflict, "page was updated by someone else")
		return
	}
	if errors.Is(saveErr, store.ErrNotFound) {
		s.writeErr(w, http.StatusNotFound, "page not found")
		return
	}
	if saveErr != nil {
		s.writeErr(w, http.StatusInternalServerError, "could not save page")
		return
	}
	s.writeJSON(w, http.StatusOK, s.toResp(p, len(p.HTML)))
}

func (s *Server) handleDelete(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	err := s.store.Delete(id)
	if errors.Is(err, store.ErrNotFound) {
		s.writeErr(w, http.StatusNotFound, "page not found")
		return
	}
	if err != nil {
		s.writeErr(w, http.StatusInternalServerError, "could not delete page")
		return
	}
	s.writeJSON(w, http.StatusOK, map[string]any{"id": id, "deleted": true})
}

// --- helpers ---------------------------------------------------------------

func bearerToken(r *http.Request) string {
	h := r.Header.Get("Authorization")
	if after, ok := strings.CutPrefix(h, "Bearer "); ok {
		return strings.TrimSpace(after)
	}
	return ""
}

func (s *Server) routeAllowed(r *http.Request) bool {
	if r.URL.Path == "/healthz" {
		return true
	}
	host := strings.ToLower(r.Host)
	if host == strings.ToLower(s.publicHost) {
		return r.URL.Path == "/" || r.URL.Path == "/theme.css" || r.URL.Path == "/.well-known/waymark" || strings.HasPrefix(r.URL.Path, "/p/")
	}
	if host == strings.ToLower(s.controlHost) {
		return r.URL.Path == "/.well-known/waymark" || strings.HasPrefix(r.URL.Path, "/api/") || r.URL.Path == "/activate" || strings.HasPrefix(r.URL.Path, "/admin/")
	}
	return false
}

func parseConfiguredOrigin(value string) (string, string, bool, error) {
	value = strings.TrimRight(strings.TrimSpace(value), "/")
	if value == "" {
		return "", "", false, nil
	}
	u, err := url.Parse(value)
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
		return "", "", false, errors.New("must be an absolute HTTP or HTTPS origin")
	}
	if u.User != nil || u.Path != "" || u.RawQuery != "" || u.Fragment != "" {
		return "", "", false, errors.New("must not contain credentials, a path, query, or fragment")
	}
	if u.Scheme == "http" && !isLoopbackHostname(u.Hostname()) {
		return "", "", false, errors.New("must use HTTPS outside loopback development")
	}
	hostname := strings.ToLower(u.Hostname())
	port := u.Port()
	if (u.Scheme == "https" && port == "443") || (u.Scheme == "http" && port == "80") {
		port = ""
	}
	host := hostname
	if port != "" {
		host = net.JoinHostPort(hostname, port)
	} else if strings.Contains(hostname, ":") {
		host = "[" + hostname + "]"
	}
	return u.Scheme + "://" + host, host, u.Scheme == "https", nil
}

func isLoopbackHostname(host string) bool {
	host = strings.ToLower(host)
	if host == "localhost" || strings.HasSuffix(host, ".localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func logPath(path string) string {
	if strings.HasPrefix(path, "/p/") {
		return "/p/[redacted]"
	}
	if strings.HasPrefix(path, "/api/pages/") {
		return "/api/pages/[redacted]"
	}
	return path
}

func (s *Server) decode(w http.ResponseWriter, r *http.Request, dst any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		s.writeErr(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return false
	}
	return true
}

func (s *Server) toResp(p *store.Page, size int) pageResp {
	return pageResp{
		ID:        p.ID,
		URL:       s.pageURL(p.ID),
		Title:     p.Title,
		Slug:      p.Slug,
		Raw:       p.Raw,
		CreatedAt: p.CreatedAt,
		UpdatedAt: p.UpdatedAt,
		ExpiresAt: p.ExpiresAt,
		Size:      size,
	}
}

func (s *Server) pageURL(id string) string {
	return s.baseURL + "/p/" + id
}

func (s *Server) writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		log.Printf("write json: %v", err)
	}
}

func (s *Server) writeErr(w http.ResponseWriter, status int, msg string) {
	s.writeJSON(w, status, map[string]string{"error": msg})
}

// ttlToExpiry converts a TTL in days to an absolute expiry, or nil if days <= 0.
func ttlToExpiry(now time.Time, days int) *time.Time {
	if days <= 0 {
		return nil
	}
	t := now.Add(time.Duration(days) * 24 * time.Hour)
	return &t
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(code int) {
	r.status = code
	r.ResponseWriter.WriteHeader(code)
}
