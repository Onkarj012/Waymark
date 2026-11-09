package store

import (
	"errors"
	"path/filepath"
	"reflect"
	"testing"
)

func TestSourceSnapshotPreservesWorkersTimestampsAndStoredBytes(t *testing.T) {
	st, err := Open(filepath.Join(t.TempDir(), "pages.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	created := "2026-11-09T09:00:00.120Z"
	updated := "2026-11-09T09:01:00.120Z"
	expires := "2100-11-09T09:00:00.120Z"
	html := "\ufeff<p>café 🌍 e\u0301</p>\r\n\x00"
	if _, err := st.db.Exec(`INSERT INTO pages
		(id, title, slug, html, raw, created_at, updated_at, expires_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`, "workers-page", "Report", "", html, 0, created, updated, expires); err != nil {
		t.Fatal(err)
	}
	want := &SourceSnapshot{
		ID: "workers-page", Title: "Report", Slug: "", HTML: html, Raw: false,
		CreatedAt: created, UpdatedAt: updated, ExpiresAt: &expires,
	}
	for attempt := 0; attempt < 2; attempt++ {
		got, err := st.GetSource(want.ID)
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("source snapshot = %#v, want %#v", got, want)
		}
	}
	if _, err := st.db.Exec(`UPDATE pages SET expires_at = NULL WHERE id = ?`, want.ID); err != nil {
		t.Fatal(err)
	}
	snapshot, err := st.GetSource(want.ID)
	if err != nil || snapshot.ExpiresAt != nil {
		t.Fatalf("permanent snapshot = %#v, error = %v", snapshot, err)
	}
	if err := st.Delete(want.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := st.GetSource(want.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("deleted source error = %v, want ErrNotFound", err)
	}
}
