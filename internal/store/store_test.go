package store

import (
	"archive/zip"
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func newTestStore(t *testing.T, quota int64) *Store {
	t.Helper()
	s, err := Open(t.TempDir(), quota, 0)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func upload(t *testing.T, s *Store, dir, name, content string) Entry {
	t.Helper()
	up, err := s.BeginUpload(dir, name)
	if err != nil {
		t.Fatalf("BeginUpload: %v", err)
	}
	if _, err := io.WriteString(up, content); err != nil {
		up.Abort()
		t.Fatalf("Write: %v", err)
	}
	entry, err := up.Commit()
	if err != nil {
		t.Fatalf("Commit: %v", err)
	}
	return entry
}

func readFile(t *testing.T, s *Store, path string) string {
	t.Helper()
	f, _, err := s.Open(path)
	if err != nil {
		t.Fatalf("Open(%q): %v", path, err)
	}
	defer func() { _ = f.Close() }()
	data, err := io.ReadAll(f)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	return string(data)
}

func TestUploadListDownloadAndCollisions(t *testing.T) {
	s := newTestStore(t, 1<<30)
	upload(t, s, "", "hello.txt", "hello world")
	upload(t, s, "", "hello.txt", "second")

	entries, err := s.List("")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("got %d entries, want 2", len(entries))
	}
	names := []string{entries[0].Name, entries[1].Name}
	if !strings.Contains(strings.Join(names, ","), "hello (1).txt") {
		t.Fatalf("collision suffix missing: %v", names)
	}
	if got := readFile(t, s, "hello.txt"); got != "hello world" {
		t.Fatalf("hello.txt = %q", got)
	}
	used, _ := s.QuotaSnapshot()
	if used != int64(len("hello world")+len("second")) {
		t.Fatalf("used = %d", used)
	}
}

func TestPathTraversalRejected(t *testing.T) {
	s := newTestStore(t, 1<<30)
	upload(t, s, "", "safe.txt", "ok")

	bad := []string{"../etc/passwd", "..", "/../x", "a/../../b"}
	for _, p := range bad {
		if _, err := s.List(p); err == nil {
			t.Fatalf("List(%q) should fail", p)
		}
		if _, err := s.Stat(p); err == nil {
			t.Fatalf("Stat(%q) should fail", p)
		}
		if _, _, err := s.Open(p); err == nil {
			t.Fatalf("Open(%q) should fail", p)
		}
		if _, err := s.BeginUpload(p, "x.txt"); err == nil {
			t.Fatalf("BeginUpload(%q) should fail", p)
		}
	}
	if _, err := s.CreateShare("../safe.txt", 0); err == nil {
		t.Fatal("CreateShare traversal should fail")
	}
}

func TestSymlinksAreNotFollowed(t *testing.T) {
	s := newTestStore(t, 1<<30)
	link := filepath.Join(s.DataDir(), "files", "escape")
	if err := os.Symlink("/etc/passwd", link); err != nil {
		t.Fatal(err)
	}
	entries, err := s.List("")
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if entry.Name == "escape" {
			t.Fatal("symlink appeared in listing")
		}
	}
	if _, err := s.Stat("escape"); !errors.Is(err, ErrInvalidPath) {
		t.Fatalf("Stat symlink error = %v", err)
	}
	if _, _, err := s.Open("escape"); !errors.Is(err, ErrInvalidPath) {
		t.Fatalf("Open symlink error = %v", err)
	}
}

func TestMkdirRenameMove(t *testing.T) {
	s := newTestStore(t, 1<<30)
	if _, err := s.Mkdir("", "docs"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Mkdir("docs", "sub"); err != nil {
		t.Fatal(err)
	}
	upload(t, s, "", "a.txt", "A")
	if _, err := s.Move("a.txt", "docs"); err != nil {
		t.Fatal(err)
	}
	if got := readFile(t, s, "docs/a.txt"); got != "A" {
		t.Fatalf("moved content = %q", got)
	}
	if _, err := s.Rename("docs/a.txt", "b.txt"); err != nil {
		t.Fatal(err)
	}
	if got := readFile(t, s, "docs/b.txt"); got != "A" {
		t.Fatalf("renamed content = %q", got)
	}
	if _, err := s.Move("docs", "docs"); err == nil {
		t.Fatal("moving a directory into itself should fail")
	}
	if _, err := s.Move("docs", "docs/sub"); err == nil {
		t.Fatal("moving a directory into its descendant should fail")
	}
}

func TestTrashRestoreAndPurge(t *testing.T) {
	s := newTestStore(t, 1<<30)
	upload(t, s, "", "note.txt", "keep me")
	usedBefore, _ := s.QuotaSnapshot()

	if _, err := s.Delete("note.txt"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Stat("note.txt"); err == nil {
		t.Fatal("file should be gone from files tree")
	}
	usedInTrash, _ := s.QuotaSnapshot()
	if usedInTrash != usedBefore {
		t.Fatalf("trash should still count toward quota: %d != %d", usedInTrash, usedBefore)
	}
	items := s.TrashList()
	if len(items) != 1 || items[0].OriginalPath != "note.txt" {
		t.Fatalf("unexpected trash items: %+v", items)
	}
	if _, err := s.Restore(items[0].ID); err != nil {
		t.Fatal(err)
	}
	if got := readFile(t, s, "note.txt"); got != "keep me" {
		t.Fatalf("restored content = %q", got)
	}

	if _, err := s.Delete("note.txt"); err != nil {
		t.Fatal(err)
	}
	items = s.TrashList()
	if err := s.Purge(items[0].ID); err != nil {
		t.Fatal(err)
	}
	usedAfter, _ := s.QuotaSnapshot()
	if usedAfter != 0 {
		t.Fatalf("quota after purge = %d, want 0", usedAfter)
	}
	if len(s.TrashList()) != 0 {
		t.Fatal("trash should be empty")
	}
}

func TestRestoreConflictGetsSuffix(t *testing.T) {
	s := newTestStore(t, 1<<30)
	upload(t, s, "", "same.txt", "old")
	if _, err := s.Delete("same.txt"); err != nil {
		t.Fatal(err)
	}
	upload(t, s, "", "same.txt", "new")
	item := s.TrashList()[0]
	if _, err := s.Restore(item.ID); err != nil {
		t.Fatal(err)
	}
	if got := readFile(t, s, "same (1).txt"); got != "old" {
		t.Fatalf("restored conflict content = %q", got)
	}
}

func TestQuota(t *testing.T) {
	s := newTestStore(t, 10)
	up, err := s.BeginUpload("", "small.txt")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := up.Write([]byte("123456789")); err != nil {
		t.Fatal(err)
	}
	if _, err := up.Write([]byte("01")); !errors.Is(err, ErrQuotaExceeded) {
		t.Fatalf("write error = %v, want quota exceeded", err)
	}
	up.Abort()
	used, _ := s.QuotaSnapshot()
	if used != 0 {
		t.Fatalf("used after abort = %d", used)
	}
	entries, _ := s.List("")
	if len(entries) != 0 {
		t.Fatalf("aborted upload left entries: %+v", entries)
	}

	up, err = s.BeginUpload("", "ok.txt")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := up.Write([]byte("12345")); err != nil {
		t.Fatal(err)
	}
	if _, err := up.Commit(); err != nil {
		t.Fatal(err)
	}
	up, err = s.BeginUpload("", "too-big.txt")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := up.Write([]byte("123456")); !errors.Is(err, ErrQuotaExceeded) {
		t.Fatalf("second upload error = %v", err)
	}
	up.Abort()
}

func TestRetentionSweep(t *testing.T) {
	s := newTestStore(t, 1<<30)
	base := time.Unix(1700000000, 0)
	s.now = func() time.Time { return base }
	upload(t, s, "", "old.txt", "old data")
	upload(t, s, "", "new.txt", "new data")
	old := base.AddDate(0, 0, -60)
	if err := s.root.Chtimes(filesPath("old.txt"), old, old); err != nil {
		t.Fatal(err)
	}

	res := s.Sweep(base, 30, 30, 100)
	if res.FilesExpired != 1 {
		t.Fatalf("FilesExpired = %d, want 1", res.FilesExpired)
	}
	if _, err := s.Stat("old.txt"); err == nil {
		t.Fatal("old.txt should have been moved to trash")
	}
	if _, err := s.Stat("new.txt"); err != nil {
		t.Fatalf("new.txt should remain: %v", err)
	}
	items := s.TrashList()
	if len(items) != 1 || items[0].Reason != "retention" {
		t.Fatalf("unexpected trash: %+v", items)
	}
	usedBeforePurge, _ := s.QuotaSnapshot()
	if usedBeforePurge == 0 {
		t.Fatal("retention trash should still count toward quota")
	}

	res = s.Sweep(base.AddDate(0, 0, 31), 0, 30, 100)
	if res.TrashPurged != 1 {
		t.Fatalf("TrashPurged = %d, want 1", res.TrashPurged)
	}
	if len(s.TrashList()) != 0 {
		t.Fatal("trash should be empty after retention purge")
	}
}

func TestSharesAndExpiry(t *testing.T) {
	s := newTestStore(t, 1<<30)
	base := time.Unix(1700000000, 0)
	s.now = func() time.Time { return base }
	upload(t, s, "", "shared.txt", "hello")

	item, err := s.CreateShare("shared.txt", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if item.Token == "" || item.IsDir {
		t.Fatalf("unexpected share: %+v", item)
	}
	got, err := s.Share(item.Token)
	if err != nil {
		t.Fatal(err)
	}
	if got.Path != "shared.txt" {
		t.Fatalf("share path = %q", got.Path)
	}
	if resolved, err := s.ResolveShareSub(got, ""); err != nil || resolved != "shared.txt" {
		t.Fatalf("resolve = %q, %v", resolved, err)
	}

	s.now = func() time.Time { return base.Add(2 * time.Hour) }
	if _, err := s.Share(item.Token); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expired share error = %v", err)
	}
	s.Sweep(base.Add(2*time.Hour), 0, 0, 100)
	if len(s.ListShares()) != 0 {
		t.Fatal("expired share should be pruned")
	}
}

func TestZip(t *testing.T) {
	s := newTestStore(t, 1<<30)
	if _, err := s.Mkdir("", "folder"); err != nil {
		t.Fatal(err)
	}
	upload(t, s, "folder", "a.txt", "A")
	upload(t, s, "folder", "b.txt", "B")
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	if err := s.WriteZip("folder", zw); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	zr, err := zip.NewReader(bytes.NewReader(buf.Bytes()), int64(buf.Len()))
	if err != nil {
		t.Fatal(err)
	}
	names := make([]string, 0, len(zr.File))
	for _, f := range zr.File {
		names = append(names, f.Name)
	}
	joined := strings.Join(names, ",")
	if !strings.Contains(joined, "folder/a.txt") || !strings.Contains(joined, "folder/b.txt") {
		t.Fatalf("zip names = %v", names)
	}
}

func TestDAVOperations(t *testing.T) {
	s := newTestStore(t, 1<<30)
	if err := s.DAVMkdir("/davdir", 0o755); err != nil {
		t.Fatal(err)
	}
	f, err := s.DAVOpen("/davdir/file.txt", os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.Write([]byte("dav data")); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	if got := readFile(t, s, "davdir/file.txt"); got != "dav data" {
		t.Fatalf("dav content = %q", got)
	}
	if _, err := s.DAVStat("/davdir/file.txt"); err != nil {
		t.Fatal(err)
	}
	if err := s.DAVRename("/davdir/file.txt", "/davdir/renamed.txt"); err != nil {
		t.Fatal(err)
	}
	if got := readFile(t, s, "davdir/renamed.txt"); got != "dav data" {
		t.Fatalf("renamed dav content = %q", got)
	}
	if err := s.DAVRemove("/davdir/renamed.txt"); err != nil {
		t.Fatal(err)
	}
	if len(s.TrashList()) != 1 {
		t.Fatal("DAV delete should move the file to trash")
	}
	if _, err := s.DAVStat("/../escape"); err == nil {
		t.Fatal("DAV traversal should fail")
	}
}

func TestSearch(t *testing.T) {
	s := newTestStore(t, 1<<30)
	if _, err := s.Mkdir("", "photos"); err != nil {
		t.Fatal(err)
	}
	upload(t, s, "photos", "summer.jpg", "x")
	upload(t, s, "", "notes.txt", "y")
	results, err := s.Search("", "summer", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 || results[0].Path != "photos/summer.jpg" {
		t.Fatalf("search results = %+v", results)
	}
}
