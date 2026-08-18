package store

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

func TestStoreLifecycleAndPermissions(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "data")
	path := filepath.Join(dir, "pages.db")
	st, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })

	if runtime.GOOS != "windows" {
		assertStoreMode(t, dir, 0o700)
		assertStoreMode(t, path, 0o600)
	}

	now := time.Now().UTC().Truncate(time.Second)
	p := &Page{
		ID: "page1", Title: "First", HTML: "<p>Hello</p>",
		CreatedAt: now, UpdatedAt: now,
	}
	if err := st.Create(p); err != nil {
		t.Fatal(err)
	}

	got, err := st.Get(p.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Title != p.Title || got.HTML != p.HTML {
		t.Fatalf("Get() = %#v, want title %q and html %q", got, p.Title, p.HTML)
	}

	got.Title = "Updated"
	got.UpdatedAt = now.Add(time.Minute)
	if err := st.Save(got); err != nil {
		t.Fatal(err)
	}
	metas, err := st.List(10)
	if err != nil {
		t.Fatal(err)
	}
	if len(metas) != 1 || metas[0].Title != "Updated" {
		t.Fatalf("List() = %#v", metas)
	}

	if err := st.Delete(p.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := st.Get(p.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Get() after Delete() error = %v, want ErrNotFound", err)
	}
}

func TestSaveIfUpdatedAtIsAtomicAndRejectsStaleWriter(t *testing.T) {
	st, err := Open(filepath.Join(t.TempDir(), "pages.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })

	now := time.Now().UTC().Truncate(time.Second)
	p := &Page{ID: "page1", Title: "First", HTML: "x", CreatedAt: now, UpdatedAt: now}
	if err := st.Create(p); err != nil {
		t.Fatal(err)
	}
	p.Title = "Fresh"
	p.UpdatedAt = now.Add(time.Minute)
	if err := st.SaveIfUpdatedAt(p, now); err != nil {
		t.Fatal(err)
	}

	p.Title = "Stale"
	p.UpdatedAt = now.Add(2 * time.Minute)
	if err := st.SaveIfUpdatedAt(p, now); !errors.Is(err, ErrConflict) {
		t.Fatalf("stale SaveIfUpdatedAt() error = %v, want ErrConflict", err)
	}
	got, err := st.Get(p.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Title != "Fresh" {
		t.Fatalf("stale writer changed title to %q", got.Title)
	}
}

func TestDeleteExpired(t *testing.T) {
	st, err := Open(filepath.Join(t.TempDir(), "pages.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })

	now := time.Now().UTC()
	past := now.Add(-time.Minute)
	future := now.Add(time.Minute)
	for _, p := range []*Page{
		{ID: "past", Title: "Past", HTML: "x", CreatedAt: now, UpdatedAt: now, ExpiresAt: &past},
		{ID: "future", Title: "Future", HTML: "x", CreatedAt: now, UpdatedAt: now, ExpiresAt: &future},
	} {
		if err := st.Create(p); err != nil {
			t.Fatal(err)
		}
	}

	n, err := st.DeleteExpired(now)
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("DeleteExpired() = %d, want 1", n)
	}
	if _, err := st.Get("past"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expired page error = %v, want ErrNotFound", err)
	}
	if _, err := st.Get("future"); err != nil {
		t.Fatalf("future page missing: %v", err)
	}
}

func TestOpenDoesNotChmodExistingParent(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix permission bits are not enforced on Windows")
	}

	dir := filepath.Join(t.TempDir(), "shared")
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	st, err := Open(filepath.Join(dir, "pages.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })

	assertStoreMode(t, dir, 0o755)
	assertStoreMode(t, filepath.Join(dir, "pages.db"), 0o600)
}

func assertStoreMode(t *testing.T, path string, want os.FileMode) {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != want {
		t.Fatalf("%s mode = %04o, want %04o", path, got, want)
	}
}
