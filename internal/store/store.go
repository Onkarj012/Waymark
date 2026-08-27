// Package store is the SQLite-backed persistence layer for Waymark.
// HTML is stored inline in the database (pages are small text documents). Back
// up the main database together with its WAL state during a write pause.
package store

import (
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite" // pure-Go SQLite driver (no cgo), registers "sqlite"
)

// ErrNotFound is returned when a page does not exist.
var ErrNotFound = errors.New("page not found")

// ErrConflict is returned when a conditional page update has a stale
// expected updated_at value.
var ErrConflict = errors.New("page update conflict")

// Page is a stored page. New pages are raw: HTML is a complete document served
// verbatim. Legacy themed pages store body content that the server still wraps
// in the house theme.
type Page struct {
	ID        string
	Title     string
	Slug      string
	HTML      string
	Raw       bool
	CreatedAt time.Time
	UpdatedAt time.Time
	ExpiresAt *time.Time // nil = never expires
}

// Meta is page metadata without the (potentially large) HTML body, for listings.
type Meta struct {
	ID        string
	Title     string
	Slug      string
	Raw       bool
	CreatedAt time.Time
	UpdatedAt time.Time
	ExpiresAt *time.Time
	Size      int // bytes of HTML
}

// Store wraps the database handle.
type Store struct {
	db *sql.DB
}

// Open opens (creating if needed) the SQLite database at path, runs migrations,
// and returns a ready Store. The parent directory is created if missing.
func Open(path string) (*Store, error) {
	if dir := filepath.Dir(path); dir != "" && dir != "." {
		if _, err := os.Stat(dir); errors.Is(err, os.ErrNotExist) {
			if err := os.MkdirAll(dir, 0o700); err != nil {
				return nil, fmt.Errorf("create db dir: %w", err)
			}
		} else if err != nil {
			return nil, fmt.Errorf("inspect db dir: %w", err)
		}
	}

	// WAL + a generous busy timeout keep the single-user workload contention-free.
	dsn := "file:" + path + "?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=foreign_keys(on)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open db: %w", err)
	}
	// One connection sidesteps SQLite write-locking entirely; ample for one user.
	db.SetMaxOpenConns(1)

	s := &Store{db: db}
	if err := s.migrate(); err != nil {
		db.Close()
		return nil, err
	}
	if err := os.Chmod(path, 0o600); err != nil {
		db.Close()
		return nil, fmt.Errorf("secure db: %w", err)
	}
	return s, nil
}

// Close closes the underlying database.
func (s *Store) Close() error { return s.db.Close() }

func (s *Store) migrate() error {
	const schema = `
CREATE TABLE IF NOT EXISTS pages (
  id         TEXT PRIMARY KEY,
  title      TEXT NOT NULL,
  slug       TEXT NOT NULL DEFAULT '',
  html       TEXT NOT NULL,
  raw        INTEGER NOT NULL DEFAULT 0,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  expires_at TEXT
);
CREATE INDEX IF NOT EXISTS idx_pages_created ON pages(created_at);
CREATE INDEX IF NOT EXISTS idx_pages_expires ON pages(expires_at);

CREATE TABLE IF NOT EXISTS device_authorizations (
  id                    TEXT PRIMARY KEY,
  device_code_hash      TEXT NOT NULL UNIQUE,
  device_secret_hash    TEXT NOT NULL,
  user_code_hash        TEXT NOT NULL UNIQUE,
  device_label          TEXT NOT NULL,
  scopes                 TEXT NOT NULL,
  source_key             TEXT NOT NULL,
  source_hint            TEXT NOT NULL,
  status                 TEXT NOT NULL,
  created_at             TEXT NOT NULL,
  expires_at             TEXT NOT NULL,
  approved_at            TEXT,
  denied_at              TEXT,
  last_poll_at           TEXT,
  poll_interval_seconds  INTEGER NOT NULL,
  consumed_at            TEXT
);
CREATE INDEX IF NOT EXISTS idx_device_authorizations_user_code ON device_authorizations(user_code_hash);
CREATE INDEX IF NOT EXISTS idx_device_authorizations_source ON device_authorizations(source_key, status);
CREATE INDEX IF NOT EXISTS idx_device_authorizations_expires ON device_authorizations(expires_at);

CREATE TABLE IF NOT EXISTS api_tokens (
  id              TEXT PRIMARY KEY,
  token_hash      TEXT NOT NULL UNIQUE,
  display_prefix  TEXT NOT NULL,
  device_label    TEXT NOT NULL,
  scopes          TEXT NOT NULL,
  created_at      TEXT NOT NULL,
  expires_at      TEXT NOT NULL,
  last_used_at    TEXT,
  revoked_at      TEXT
);
CREATE INDEX IF NOT EXISTS idx_api_tokens_hash ON api_tokens(token_hash);
CREATE INDEX IF NOT EXISTS idx_api_tokens_expires ON api_tokens(expires_at);

CREATE TABLE IF NOT EXISTS admin_sessions (
  id            TEXT PRIMARY KEY,
  session_hash  TEXT NOT NULL UNIQUE,
  authenticated INTEGER NOT NULL DEFAULT 0,
  created_at    TEXT NOT NULL,
  expires_at    TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_admin_sessions_hash ON admin_sessions(session_hash);
CREATE INDEX IF NOT EXISTS idx_admin_sessions_expires ON admin_sessions(expires_at);`
	_, err := s.db.Exec(schema)
	if err != nil {
		return fmt.Errorf("migrate: %w", err)
	}
	return nil
}

const rfc = time.RFC3339Nano

func nullTime(t *time.Time) any {
	if t == nil {
		return nil
	}
	return t.UTC().Format(rfc)
}

func parseTime(s string) (time.Time, error) { return time.Parse(rfc, s) }

// Create inserts a new page.
func (s *Store) Create(p *Page) error {
	_, err := s.db.Exec(
		`INSERT INTO pages (id, title, slug, html, raw, created_at, updated_at, expires_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		p.ID, p.Title, p.Slug, p.HTML, boolToInt(p.Raw),
		p.CreatedAt.UTC().Format(rfc), p.UpdatedAt.UTC().Format(rfc), nullTime(p.ExpiresAt),
	)
	if err != nil {
		return fmt.Errorf("create page: %w", err)
	}
	return nil
}

// Get returns the full page (including HTML) regardless of expiry. Callers that
// serve pages publicly should check ExpiresAt themselves.
func (s *Store) Get(id string) (*Page, error) {
	row := s.db.QueryRow(
		`SELECT id, title, slug, html, raw, created_at, updated_at, expires_at
		 FROM pages WHERE id = ?`, id)

	var p Page
	var raw int
	var created, updated string
	var expires sql.NullString
	switch err := row.Scan(&p.ID, &p.Title, &p.Slug, &p.HTML, &raw, &created, &updated, &expires); {
	case errors.Is(err, sql.ErrNoRows):
		return nil, ErrNotFound
	case err != nil:
		return nil, fmt.Errorf("get page: %w", err)
	}
	p.Raw = raw != 0
	if t, err := parseTime(created); err == nil {
		p.CreatedAt = t
	}
	if t, err := parseTime(updated); err == nil {
		p.UpdatedAt = t
	}
	if expires.Valid {
		if t, err := parseTime(expires.String); err == nil {
			p.ExpiresAt = &t
		}
	}
	return &p, nil
}

// Save overwrites an existing page's mutable fields. Returns ErrNotFound if the
// id does not exist.
func (s *Store) Save(p *Page) error {
	res, err := s.db.Exec(
		`UPDATE pages SET title = ?, slug = ?, html = ?, raw = ?, updated_at = ?, expires_at = ?
		 WHERE id = ?`,
		p.Title, p.Slug, p.HTML, boolToInt(p.Raw),
		p.UpdatedAt.UTC().Format(rfc), nullTime(p.ExpiresAt), p.ID,
	)
	if err != nil {
		return fmt.Errorf("save page: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// SaveIfUpdatedAt overwrites an existing page only when its updated_at still
// matches expected. The comparison and write are one SQLite UPDATE statement,
// so a concurrent writer cannot pass a separate read check and then be
// overwritten. It returns ErrNotFound when the page is absent and ErrConflict
// when the page exists with a different updated_at value.
func (s *Store) SaveIfUpdatedAt(p *Page, expected time.Time) error {
	res, err := s.db.Exec(
		`UPDATE pages SET title = ?, slug = ?, html = ?, raw = ?, updated_at = ?, expires_at = ?
		 WHERE id = ? AND updated_at = ?`,
		p.Title, p.Slug, p.HTML, boolToInt(p.Raw),
		p.UpdatedAt.UTC().Format(rfc), nullTime(p.ExpiresAt), p.ID,
		expected.UTC().Format(rfc),
	)
	if err != nil {
		return fmt.Errorf("save page conditionally: %w", err)
	}
	if n, _ := res.RowsAffected(); n > 0 {
		return nil
	}

	var exists int
	if err := s.db.QueryRow(`SELECT 1 FROM pages WHERE id = ?`, p.ID).Scan(&exists); errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	} else if err != nil {
		return fmt.Errorf("check page after conditional save: %w", err)
	}
	return ErrConflict
}

// Delete removes a page. Returns ErrNotFound if it did not exist.
func (s *Store) Delete(id string) error {
	res, err := s.db.Exec(`DELETE FROM pages WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("delete page: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// List returns page metadata, newest first, up to limit (limit <= 0 means all).
func (s *Store) List(limit int) ([]Meta, error) {
	q := `SELECT id, title, slug, raw, created_at, updated_at, expires_at, length(html)
	      FROM pages ORDER BY created_at DESC`
	args := []any{}
	if limit > 0 {
		q += " LIMIT ?"
		args = append(args, limit)
	}
	rows, err := s.db.Query(q, args...)
	if err != nil {
		return nil, fmt.Errorf("list pages: %w", err)
	}
	defer rows.Close()

	var out []Meta
	for rows.Next() {
		var m Meta
		var raw int
		var created, updated string
		var expires sql.NullString
		if err := rows.Scan(&m.ID, &m.Title, &m.Slug, &raw, &created, &updated, &expires, &m.Size); err != nil {
			return nil, fmt.Errorf("scan page: %w", err)
		}
		m.Raw = raw != 0
		if t, err := parseTime(created); err == nil {
			m.CreatedAt = t
		}
		if t, err := parseTime(updated); err == nil {
			m.UpdatedAt = t
		}
		if expires.Valid {
			if t, err := parseTime(expires.String); err == nil {
				m.ExpiresAt = &t
			}
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// DeleteExpired removes all pages whose expiry is at or before now. Returns the
// number deleted.
func (s *Store) DeleteExpired(now time.Time) (int, error) {
	res, err := s.db.Exec(
		`DELETE FROM pages WHERE expires_at IS NOT NULL AND expires_at <= ?`,
		now.UTC().Format(rfc))
	if err != nil {
		return 0, fmt.Errorf("delete expired: %w", err)
	}
	n, _ := res.RowsAffected()
	return int(n), nil
}

// DeleteExpiredAuth removes expired device grants, tokens, and admin sessions.
func (s *Store) DeleteExpiredAuth(now time.Time) (int, error) {
	cutoff := now.UTC().Format(rfc)
	tx, err := s.db.Begin()
	if err != nil {
		return 0, fmt.Errorf("begin auth cleanup: %w", err)
	}
	defer tx.Rollback()

	total := int64(0)
	for _, q := range []string{
		`DELETE FROM device_authorizations WHERE expires_at <= ?`,
		`DELETE FROM api_tokens WHERE expires_at <= ?`,
		`DELETE FROM admin_sessions WHERE expires_at <= ?`,
	} {
		res, err := tx.Exec(q, cutoff)
		if err != nil {
			return 0, fmt.Errorf("delete expired auth state: %w", err)
		}
		n, _ := res.RowsAffected()
		total += n
	}
	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("commit auth cleanup: %w", err)
	}
	return int(total), nil
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
