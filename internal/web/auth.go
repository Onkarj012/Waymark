package web

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/Onkarj012/Waymark/internal/store"
)

const (
	deviceGrantLifetime = 10 * time.Minute
	initialPollInterval = 5
)

type credential struct {
	Kind      string
	TokenID   string
	Label     string
	Scopes    []string
	ExpiresAt *time.Time
}

type credentialContextKey struct{}

func credentialFromContext(ctx context.Context) credential {
	value, _ := ctx.Value(credentialContextKey{}).(credential)
	return value
}

func (s *Server) auth(scope string, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		credential, err := s.authenticate(r)
		if err != nil {
			s.writeErr(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		if !hasScope(credential.Scopes, scope) {
			s.writeErr(w, http.StatusForbidden, "insufficient_scope")
			return
		}
		next(w, r.WithContext(context.WithValue(r.Context(), credentialContextKey{}, credential)))
	}
}

func (s *Server) authenticate(r *http.Request) (credential, error) {
	token := bearerToken(r)
	if token == "" {
		return credential{}, errors.New("missing bearer token")
	}
	if !strings.EqualFold(r.Host, s.controlHost) {
		return credential{}, errors.New("device tokens require control origin")
	}
	now := s.now()
	stored, err := s.store.APITokenByHash(hashHighEntropy(token), now)
	if err != nil {
		return credential{}, err
	}
	if stored.LastUsedAt == nil || stored.LastUsedAt.Before(now.Add(-5*time.Minute)) {
		_ = s.store.TouchAPIToken(stored.ID, now)
	}
	expires := stored.ExpiresAt
	return credential{Kind: "device_token", TokenID: stored.ID, Label: stored.DeviceLabel, Scopes: splitScopes(stored.Scopes), ExpiresAt: &expires}, nil
}

func (s *Server) handleDiscovery(w http.ResponseWriter, r *http.Request) {
	s.writeJSON(w, http.StatusOK, map[string]any{
		"control_url": s.controlURL, "content_url": s.baseURL, "device_authorization": true,
	})
}

type deviceCodeRequest struct {
	DeviceSecret string   `json:"device_secret"`
	DeviceLabel  string   `json:"device_label"`
	Scopes       []string `json:"scopes"`
}

func (s *Server) handleDeviceCode(w http.ResponseWriter, r *http.Request) {
	sourceKey, sourceHint := s.requestSource(r)
	if !s.limiter.allow("create:"+sourceKey, 5, 10*time.Minute, s.now()) {
		s.writeErr(w, http.StatusTooManyRequests, "rate_limited")
		return
	}
	var req deviceCodeRequest
	if !s.decode(w, r, &req) {
		return
	}
	secretBytes, err := base64.RawURLEncoding.DecodeString(req.DeviceSecret)
	if err != nil || len(secretBytes) != 32 {
		s.writeErr(w, http.StatusBadRequest, "device_secret must be 32 random bytes encoded as base64url")
		return
	}
	label := strings.TrimSpace(req.DeviceLabel)
	if label == "" || len(label) > 120 {
		s.writeErr(w, http.StatusBadRequest, "device_label must be between 1 and 120 characters")
		return
	}
	scopes, ok := validateScopes(req.Scopes)
	if !ok {
		s.writeErr(w, http.StatusBadRequest, "scopes must contain pages:read and optionally pages:write")
		return
	}
	deviceCode, err := randomBase64(32)
	if err != nil {
		s.writeErr(w, http.StatusInternalServerError, "could not create device authorization")
		return
	}
	userCode, err := randomUserCode()
	if err != nil {
		s.writeErr(w, http.StatusInternalServerError, "could not create device authorization")
		return
	}
	id, err := randomBase64(12)
	if err != nil {
		s.writeErr(w, http.StatusInternalServerError, "could not create device authorization")
		return
	}
	now := s.now().UTC()
	grant := store.DeviceAuthorization{
		ID: id, DeviceCodeHash: hashHighEntropy(deviceCode), DeviceSecretHash: hashHighEntropy(req.DeviceSecret),
		UserCodeHash: s.hashLowEntropy(normalizeUserCode(userCode)), DeviceLabel: label, Scopes: strings.Join(scopes, " "),
		SourceKey: sourceKey, SourceHint: sourceHint, CreatedAt: now, ExpiresAt: now.Add(deviceGrantLifetime), PollIntervalSeconds: initialPollInterval,
	}
	if err := s.store.CreateDeviceAuthorization(grant, 5, 50); err != nil {
		if errors.Is(err, store.ErrLimitReached) {
			s.writeErr(w, http.StatusTooManyRequests, "too_many_pending_authorizations")
			return
		}
		s.writeErr(w, http.StatusInternalServerError, "could not create device authorization")
		return
	}
	verification := s.controlURL + "/activate"
	s.writeJSON(w, http.StatusCreated, map[string]any{
		"device_code": deviceCode, "user_code": userCode, "verification_uri": verification,
		"verification_uri_complete": verification + "?code=" + userCode, "expires_in": int(deviceGrantLifetime.Seconds()), "interval": initialPollInterval,
	})
}

type deviceTokenRequest struct {
	DeviceCode   string `json:"device_code"`
	DeviceSecret string `json:"device_secret"`
}

func (s *Server) handleDeviceToken(w http.ResponseWriter, r *http.Request) {
	sourceKey, _ := s.requestSource(r)
	if !s.limiter.allow("poll:"+sourceKey, 120, 10*time.Minute, s.now()) {
		s.writeErr(w, http.StatusTooManyRequests, "rate_limited")
		return
	}
	var req deviceTokenRequest
	if !s.decode(w, r, &req) {
		return
	}
	rawToken, tokenID, displayPrefix, err := newAPIToken()
	if err != nil {
		s.writeErr(w, http.StatusInternalServerError, "could not issue token")
		return
	}
	now := s.now().UTC()
	grant, grantErr := s.store.DeviceAuthorizationByDeviceCode(hashHighEntropy(req.DeviceCode), hashHighEntropy(req.DeviceSecret), now)
	if grantErr != nil {
		if errors.Is(grantErr, store.ErrGrantExpired) || errors.Is(grantErr, store.ErrGrantNotFound) {
			s.writeDeviceError(w, http.StatusBadRequest, "expired_token", 0)
		} else {
			s.writeErr(w, http.StatusInternalServerError, "could not complete device authorization")
		}
		return
	}
	token := store.APIToken{
		ID: tokenID, TokenHash: hashHighEntropy(rawToken), DisplayPrefix: displayPrefix, DeviceLabel: grant.DeviceLabel,
		Scopes: grant.Scopes, CreatedAt: now, ExpiresAt: now.Add(time.Duration(s.tokenTTLDays) * 24 * time.Hour),
	}
	interval, err := s.store.PollDeviceAuthorization(hashHighEntropy(req.DeviceCode), hashHighEntropy(req.DeviceSecret), now, token)
	switch {
	case err == nil:
		s.writeJSON(w, http.StatusOK, map[string]any{
			"access_token": rawToken, "token_type": "Bearer", "expires_in": s.tokenTTLDays * 86400, "scope": grant.Scopes,
		})
	case errors.Is(err, store.ErrGrantPending):
		s.writeDeviceError(w, http.StatusBadRequest, "authorization_pending", interval)
	case errors.Is(err, store.ErrSlowDown):
		s.writeDeviceError(w, http.StatusBadRequest, "slow_down", interval)
	case errors.Is(err, store.ErrGrantDenied):
		s.writeDeviceError(w, http.StatusBadRequest, "access_denied", 0)
	case errors.Is(err, store.ErrGrantConsumed):
		// A consumed grant is terminal; replay must never reveal or mint a token.
		s.writeDeviceError(w, http.StatusBadRequest, "expired_token", 0)
	case errors.Is(err, store.ErrGrantExpired), errors.Is(err, store.ErrGrantNotFound):
		s.writeDeviceError(w, http.StatusBadRequest, "expired_token", 0)
	default:
		s.writeErr(w, http.StatusInternalServerError, "could not complete device authorization")
	}
}

func (s *Server) handleSelfRevoke(w http.ResponseWriter, r *http.Request) {
	credential := credentialFromContext(r.Context())
	if credential.Kind != "device_token" {
		s.writeErr(w, http.StatusBadRequest, "only device tokens can revoke themselves")
		return
	}
	if err := s.store.RevokeAPIToken(credential.TokenID, s.now()); err != nil {
		s.writeErr(w, http.StatusInternalServerError, "could not revoke token")
		return
	}
	s.writeJSON(w, http.StatusOK, map[string]bool{"revoked": true})
}

func (s *Server) writeDeviceError(w http.ResponseWriter, status int, code string, retry int) {
	if retry > 0 {
		w.Header().Set("Retry-After", fmt.Sprint(retry))
	}
	s.writeJSON(w, status, map[string]string{"error": code})
}

func validateScopes(input []string) ([]string, bool) {
	if len(input) == 0 {
		return []string{"pages:read", "pages:write"}, true
	}
	seen := map[string]bool{}
	for _, scope := range input {
		if scope != "pages:read" && scope != "pages:write" {
			return nil, false
		}
		seen[scope] = true
	}
	if !seen["pages:read"] {
		return nil, false
	}
	out := []string{"pages:read"}
	if seen["pages:write"] {
		out = append(out, "pages:write")
	}
	return out, true
}

func hasScope(scopes []string, want string) bool {
	for _, scope := range scopes {
		if scope == want {
			return true
		}
	}
	return false
}

func splitScopes(value string) []string { return strings.Fields(value) }

func randomBase64(size int) (string, error) {
	b := make([]byte, size)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

func randomUserCode() (string, error) {
	const alphabet = "23456789ABCDEFGHJKLMNPQRSTUVWXYZ"
	b := make([]byte, 8)
	random := make([]byte, 8)
	if _, err := rand.Read(random); err != nil {
		return "", err
	}
	for i := range b {
		b[i] = alphabet[int(random[i])%len(alphabet)]
	}
	return string(b[:4]) + "-" + string(b[4:]), nil
}

func newAPIToken() (string, string, string, error) {
	id, err := randomBase64(9)
	if err != nil {
		return "", "", "", err
	}
	secret, err := randomBase64(32)
	if err != nil {
		return "", "", "", err
	}
	prefix := "waymark_" + id
	return prefix + "." + secret, id, prefix, nil
}

func hashHighEntropy(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

func constantTimeSecretEqual(left, right string) bool {
	leftHash := sha256.Sum256([]byte(left))
	rightHash := sha256.Sum256([]byte(right))
	return subtle.ConstantTimeCompare(leftHash[:], rightHash[:]) == 1
}

func (s *Server) hashLowEntropy(value string) string {
	h := hmac.New(sha256.New, []byte(s.adminPasscode))
	h.Write([]byte(value))
	return hex.EncodeToString(h.Sum(nil))
}

func normalizeUserCode(value string) string {
	return strings.ToUpper(strings.ReplaceAll(strings.TrimSpace(value), "-", ""))
}

func (s *Server) requestSource(r *http.Request) (string, string) {
	host := ""
	if s.trustForwardedIP {
		forwarded := strings.TrimSpace(r.Header.Get("X-Real-IP"))
		if net.ParseIP(forwarded) != nil {
			host = forwarded
		}
	}
	if host == "" {
		host, _, _ = net.SplitHostPort(r.RemoteAddr)
		if host == "" {
			host = strings.TrimSpace(r.RemoteAddr)
		}
	}
	if host == "" {
		host = "unknown"
	}
	hint := host
	if ip := net.ParseIP(host); ip != nil && !ip.IsLoopback() {
		if v4 := ip.To4(); v4 != nil {
			hint = fmt.Sprintf("%d.%d.%d.x", v4[0], v4[1], v4[2])
		} else {
			hint = "IPv6 address"
		}
	}
	return s.hashLowEntropy(host), hint
}

type rateBucket struct {
	Start time.Time
	Count int
}

type rateLimiter struct {
	mu    sync.Mutex
	max   int
	items map[string]rateBucket
}

func newRateLimiter(max int) *rateLimiter {
	return &rateLimiter{max: max, items: make(map[string]rateBucket)}
}

func (l *rateLimiter) allow(key string, limit int, window time.Duration, now time.Time) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	if len(l.items) >= l.max {
		for k, bucket := range l.items {
			if now.Sub(bucket.Start) >= window {
				delete(l.items, k)
			}
		}
		if len(l.items) >= l.max {
			return false
		}
	}
	bucket := l.items[key]
	if bucket.Start.IsZero() || now.Sub(bucket.Start) >= window {
		l.items[key] = rateBucket{Start: now, Count: 1}
		return true
	}
	if bucket.Count >= limit {
		return false
	}
	bucket.Count++
	l.items[key] = bucket
	return true
}
