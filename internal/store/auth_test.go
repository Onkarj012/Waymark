package store

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestDeviceAuthorizationStoresHashesAndConsumesOnce(t *testing.T) {
	st, err := Open(filepath.Join(t.TempDir(), "pages.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	now := time.Now().UTC().Truncate(time.Second)
	deviceCodeHash := testHash("raw-device-code")
	deviceSecretHash := testHash("raw-device-secret")
	userCodeHash := testHash("ABCD-EFGH")
	grant := DeviceAuthorization{
		ID: "grant", DeviceCodeHash: deviceCodeHash, DeviceSecretHash: deviceSecretHash, UserCodeHash: userCodeHash,
		DeviceLabel: "test device", Scopes: "pages:read pages:write", SourceKey: "source-hash", SourceHint: "127.0.0.1",
		CreatedAt: now, ExpiresAt: now.Add(10 * time.Minute), PollIntervalSeconds: 5,
	}
	if err := st.CreateDeviceAuthorization(grant, 5, 50); err != nil {
		t.Fatal(err)
	}
	if err := st.DecideDeviceAuthorization(grant.UserCodeHash, "approved", now); err != nil {
		t.Fatal(err)
	}

	token := APIToken{ID: "token", TokenHash: testHash("waymark_public.raw-token-secret"), DisplayPrefix: "waymark_public", DeviceLabel: grant.DeviceLabel, Scopes: grant.Scopes, CreatedAt: now, ExpiresAt: now.Add(24 * time.Hour)}
	if _, err := st.PollDeviceAuthorization(grant.DeviceCodeHash, grant.DeviceSecretHash, now, token); err != nil {
		t.Fatal(err)
	}
	if _, err := st.PollDeviceAuthorization(grant.DeviceCodeHash, grant.DeviceSecretHash, now.Add(6*time.Second), APIToken{ID: "second"}); !errors.Is(err, ErrGrantConsumed) {
		t.Fatalf("second consume error = %v, want ErrGrantConsumed", err)
	}

	for _, raw := range []string{"raw-device-code", "raw-device-secret", "ABCD-EFGH", "waymark_public.raw-token-secret"} {
		var count int
		if err := st.db.QueryRow(`SELECT
			(SELECT count(*) FROM device_authorizations WHERE device_code_hash = ? OR device_secret_hash = ? OR user_code_hash = ?) +
			(SELECT count(*) FROM api_tokens WHERE token_hash = ?)`, raw, raw, raw, raw).Scan(&count); err != nil {
			t.Fatal(err)
		}
		if count != 0 {
			t.Fatalf("raw secret %q was stored", raw)
		}
	}
}

func testHash(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

func TestConcurrentDeviceConsumptionIssuesOneToken(t *testing.T) {
	st, err := Open(filepath.Join(t.TempDir(), "pages.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	now := time.Now().UTC()
	grant := DeviceAuthorization{ID: "grant", DeviceCodeHash: "code", DeviceSecretHash: "secret", UserCodeHash: "user", DeviceLabel: "device", Scopes: "pages:read", SourceKey: "source", SourceHint: "local", CreatedAt: now, ExpiresAt: now.Add(time.Minute), PollIntervalSeconds: 5}
	if err := st.CreateDeviceAuthorization(grant, 5, 50); err != nil {
		t.Fatal(err)
	}
	if err := st.DecideDeviceAuthorization("user", "approved", now); err != nil {
		t.Fatal(err)
	}

	var wg sync.WaitGroup
	results := make(chan error, 2)
	for _, id := range []string{"one", "two"} {
		wg.Add(1)
		go func(id string) {
			defer wg.Done()
			_, err := st.PollDeviceAuthorization("code", "secret", now, APIToken{ID: id, TokenHash: id + "-hash", DisplayPrefix: id, DeviceLabel: "device", Scopes: "pages:read", CreatedAt: now, ExpiresAt: now.Add(time.Hour)})
			results <- err
		}(id)
	}
	wg.Wait()
	close(results)
	successes := 0
	for err := range results {
		if err == nil {
			successes++
		} else if !errors.Is(err, ErrGrantConsumed) {
			t.Fatalf("unexpected poll error: %v", err)
		}
	}
	if successes != 1 {
		t.Fatalf("successful consumptions = %d, want 1", successes)
	}
}

func TestPollingSlowDownAndExpiry(t *testing.T) {
	st, err := Open(filepath.Join(t.TempDir(), "pages.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	now := time.Now().UTC()
	grant := DeviceAuthorization{ID: "grant", DeviceCodeHash: "code", DeviceSecretHash: "secret", UserCodeHash: "user", DeviceLabel: "device", Scopes: "pages:read", SourceKey: "source", SourceHint: "local", CreatedAt: now, ExpiresAt: now.Add(10 * time.Second), PollIntervalSeconds: 5}
	if err := st.CreateDeviceAuthorization(grant, 5, 50); err != nil {
		t.Fatal(err)
	}
	if _, err := st.PollDeviceAuthorization("code", "secret", now, APIToken{}); !errors.Is(err, ErrGrantPending) {
		t.Fatalf("first poll error = %v", err)
	}
	if interval, err := st.PollDeviceAuthorization("code", "secret", now.Add(time.Second), APIToken{}); !errors.Is(err, ErrSlowDown) || interval != 10 {
		t.Fatalf("fast poll = (%d, %v), want (10, ErrSlowDown)", interval, err)
	}
	if _, err := st.PollDeviceAuthorization("code", "secret", now.Add(11*time.Second), APIToken{}); !errors.Is(err, ErrGrantExpired) {
		t.Fatalf("expired poll error = %v", err)
	}
}

func TestAdminSessionCreationPrunesExpiredAndCapsActiveRows(t *testing.T) {
	st, err := Open(filepath.Join(t.TempDir(), "pages.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	now := time.Now().UTC()
	if err := st.CreateAdminSession(AdminSession{ID: "expired", SessionHash: "expired-hash", CreatedAt: now.Add(-time.Hour), ExpiresAt: now.Add(-time.Minute)}); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < maxAdminSessions; i++ {
		id := fmt.Sprintf("session-%d", i)
		if err := st.CreateAdminSession(AdminSession{ID: id, SessionHash: id + "-hash", CreatedAt: now, ExpiresAt: now.Add(time.Hour)}); err != nil {
			t.Fatalf("create active session %d: %v", i, err)
		}
	}
	if err := st.CreateAdminSession(AdminSession{ID: "overflow", SessionHash: "overflow-hash", CreatedAt: now, ExpiresAt: now.Add(time.Hour)}); !errors.Is(err, ErrSessionLimit) {
		t.Fatalf("overflow error = %v, want ErrSessionLimit", err)
	}
	var expired int
	if err := st.db.QueryRow(`SELECT count(*) FROM admin_sessions WHERE id = 'expired'`).Scan(&expired); err != nil {
		t.Fatal(err)
	}
	if expired != 0 {
		t.Fatal("expired anonymous session was not pruned")
	}
}

func TestAuthScannersRejectMalformedTimestamps(t *testing.T) {
	st, err := Open(filepath.Join(t.TempDir(), "pages.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	now := time.Now().UTC()
	future := now.Add(time.Hour).Format(rfc)

	t.Run("device authorization", func(t *testing.T) {
		_, err := st.db.Exec(`INSERT INTO device_authorizations
			(id, device_code_hash, device_secret_hash, user_code_hash, device_label, scopes, source_key, source_hint, status, created_at, expires_at, poll_interval_seconds)
			VALUES ('bad-device-time', 'code-time', 'secret-time', 'user-time', 'device', 'pages:read', 'source', 'local', 'pending', 'not-a-time', ?, 5)`, future)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := st.DeviceAuthorizationByUserCode("user-time", now); err == nil || !strings.Contains(err.Error(), "parse device authorization created_at") {
			t.Fatalf("malformed device timestamp error = %v", err)
		}
	})

	t.Run("api token", func(t *testing.T) {
		_, err := st.db.Exec(`INSERT INTO api_tokens (id, token_hash, display_prefix, device_label, scopes, created_at, expires_at)
			VALUES ('bad-token-time', 'token-time', 'waymark_bad', 'device', 'pages:read', 'not-a-time', ?)`, future)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := st.ListAPITokens(); err == nil || !strings.Contains(err.Error(), "parse api token created_at") {
			t.Fatalf("malformed token timestamp error = %v", err)
		}
	})

	t.Run("admin session", func(t *testing.T) {
		_, err := st.db.Exec(`INSERT INTO admin_sessions (id, session_hash, authenticated, created_at, expires_at)
			VALUES ('bad-session-time', 'session-time', 0, 'not-a-time', ?)`, future)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := st.AdminSessionByHash("session-time", now); err == nil || !strings.Contains(err.Error(), "parse admin session created_at") {
			t.Fatalf("malformed session timestamp error = %v", err)
		}
	})
}
