package store

import (
	"archive/zip"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

type Store struct {
	dataDir string
	root    *os.Root
	quota   *Quota

	mu         sync.Mutex
	trashItems []TrashItem
	shareItems []ShareItem
	now        func() time.Time
}

type Entry struct {
	Name    string    `json:"name"`
	Path    string    `json:"path"`
	IsDir   bool      `json:"isDir"`
	Size    int64     `json:"size"`
	ModTime time.Time `json:"modTime"`
	Kind    string    `json:"kind"`
}

func Open(dataDir string, quotaLimit, minFree int64) (*Store, error) {
	abs, err := filepath.Abs(dataDir)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(abs, 0o750); err != nil {
		return nil, err
	}
	for _, name := range []string{"files", ".trash", ".meta", ".tmp"} {
		if err := os.MkdirAll(filepath.Join(abs, name), 0o750); err != nil {
			return nil, err
		}
	}
	root, err := os.OpenRoot(abs)
	if err != nil {
		return nil, err
	}
	s := &Store{
		dataDir: abs,
		root:    root,
		quota:   NewQuota(quotaLimit, minFree, abs),
		now:     time.Now,
	}
	if err := s.loadTrash(); err != nil {
		_ = root.Close()
		return nil, err
	}
	if err := s.loadShares(); err != nil {
		_ = root.Close()
		return nil, err
	}
	used, err := s.diskUsed()
	if err != nil {
		_ = root.Close()
		return nil, fmt.Errorf("scan used space: %w", err)
	}
	s.quota.SetUsed(used)
	s.cleanupTemps(24 * time.Hour)
	return s, nil
}

func (s *Store) Close() error {
	return s.root.Close()
}

func (s *Store) DataDir() string {
	return s.dataDir
}

func (s *Store) QuotaSnapshot() (used, limit int64) {
	return s.quota.Snapshot()
}

func (s *Store) List(rel string) ([]Entry, error) {
	rel, err := CleanRel(rel)
	if err != nil {
		return nil, err
	}
	p := filesPath(rel)
	info, err := s.root.Lstat(p)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		return nil, ErrNotDir
	}
	f, err := s.root.Open(p)
	if err != nil {
		return nil, err
	}
	infos, err := f.Readdir(-1)
	_ = f.Close()
	if err != nil {
		return nil, err
	}
	entries := make([]Entry, 0, len(infos))
	for _, fi := range infos {
		if fi.Mode()&os.ModeSymlink != 0 {
			continue
		}
		if !fi.IsDir() && !fi.Mode().IsRegular() {
			continue
		}
		entries = append(entries, entryFrom(joinRel(rel, fi.Name()), fi))
	}
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].IsDir != entries[j].IsDir {
			return entries[i].IsDir
		}
		return strings.ToLower(entries[i].Name) < strings.ToLower(entries[j].Name)
	})
	return entries, nil
}

func (s *Store) Stat(rel string) (Entry, error) {
	rel, err := CleanRel(rel)
	if err != nil {
		return Entry{}, err
	}
	info, err := s.root.Lstat(filesPath(rel))
	if err != nil {
		return Entry{}, err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return Entry{}, ErrInvalidPath
	}
	if !info.IsDir() && !info.Mode().IsRegular() {
		return Entry{}, ErrInvalidPath
	}
	return entryFrom(rel, info), nil
}

// Open returns a regular file for download. The caller must close it.
func (s *Store) Open(rel string) (*os.File, os.FileInfo, error) {
	rel, err := CleanRel(rel)
	if err != nil {
		return nil, nil, err
	}
	p := filesPath(rel)
	info, err := s.root.Lstat(p)
	if err != nil {
		return nil, nil, err
	}
	if info.IsDir() {
		return nil, nil, ErrIsDir
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return nil, nil, ErrInvalidPath
	}
	f, err := s.root.Open(p)
	if err != nil {
		return nil, nil, err
	}
	return f, info, nil
}

func (s *Store) ReadText(rel string, max int64) ([]byte, error) {
	f, info, err := s.Open(rel)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	if info.Size() > max {
		return nil, ErrTooLarge
	}
	data, err := io.ReadAll(io.LimitReader(f, max+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > max {
		return nil, ErrTooLarge
	}
	return data, nil
}

func (s *Store) Mkdir(dirRel, name string) (Entry, error) {
	dirRel, err := CleanRel(dirRel)
	if err != nil {
		return Entry{}, err
	}
	if err := ValidName(name); err != nil {
		return Entry{}, err
	}
	if dirRel != "" {
		info, err := s.root.Lstat(filesPath(dirRel))
		if err != nil {
			return Entry{}, err
		}
		if !info.IsDir() {
			return Entry{}, ErrNotDir
		}
	}
	target := joinRel(dirRel, name)
	if err := s.root.Mkdir(filesPath(target), 0o750); err != nil {
		return Entry{}, err
	}
	return s.Stat(target)
}

func (s *Store) Rename(rel, newName string) (Entry, error) {
	rel, err := CleanRel(rel)
	if err != nil || rel == "" {
		return Entry{}, ErrInvalidPath
	}
	if err := ValidName(newName); err != nil {
		return Entry{}, err
	}
	newRel := joinRel(cleanParent(rel), newName)
	if newRel == rel {
		return s.Stat(rel)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, err := s.root.Lstat(filesPath(newRel)); err == nil {
		return Entry{}, ErrExists
	} else if !errors.Is(err, fs.ErrNotExist) {
		return Entry{}, err
	}
	if err := s.root.Rename(filesPath(rel), filesPath(newRel)); err != nil {
		return Entry{}, err
	}
	if s.revokeSharesForPathLocked(rel) {
		_ = s.saveSharesLocked()
	}
	return s.statLocked(newRel)
}

func (s *Store) Move(rel, destDir string) (Entry, error) {
	rel, err := CleanRel(rel)
	if err != nil || rel == "" {
		return Entry{}, ErrInvalidPath
	}
	destDir, err = CleanRel(destDir)
	if err != nil {
		return Entry{}, err
	}
	info, err := s.root.Lstat(filesPath(rel))
	if err != nil {
		return Entry{}, err
	}
	destInfo, err := s.root.Lstat(filesPath(destDir))
	if err != nil {
		return Entry{}, err
	}
	if !destInfo.IsDir() {
		return Entry{}, ErrNotDir
	}
	if info.IsDir() && (destDir == rel || strings.HasPrefix(destDir, rel+"/")) {
		return Entry{}, ErrInvalidPath
	}
	newRel := joinRel(destDir, path.Base(rel))
	if newRel == rel {
		return entryFrom(rel, info), nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, err := s.root.Lstat(filesPath(newRel)); err == nil {
		return Entry{}, ErrExists
	} else if !errors.Is(err, fs.ErrNotExist) {
		return Entry{}, err
	}
	if err := s.root.Rename(filesPath(rel), filesPath(newRel)); err != nil {
		return Entry{}, err
	}
	if s.revokeSharesForPathLocked(rel) {
		_ = s.saveSharesLocked()
	}
	return s.statLocked(newRel)
}

func (s *Store) Delete(rel string) (TrashItem, error) {
	return s.deleteToTrash(rel, "manual")
}

func (s *Store) deleteToTrash(rel, reason string) (TrashItem, error) {
	rel, err := CleanRel(rel)
	if err != nil {
		return TrashItem{}, err
	}
	if rel == "" {
		return TrashItem{}, ErrRootOperation
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.moveToTrashLocked(rel, reason)
}

func (s *Store) Search(dirRel, query string, limit int) ([]Entry, error) {
	dirRel, err := CleanRel(dirRel)
	if err != nil {
		return nil, err
	}
	if limit <= 0 || limit > 500 {
		limit = 200
	}
	q := strings.ToLower(strings.TrimSpace(query))
	if q == "" {
		return []Entry{}, nil
	}
	rootPath := filesPath(dirRel)
	info, err := s.root.Lstat(rootPath)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		return nil, ErrNotDir
	}
	results := make([]Entry, 0, 32)
	err = fs.WalkDir(s.root.FS(), rootPath, func(p string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if p == rootPath {
			return nil
		}
		if d.Type()&fs.ModeSymlink != 0 {
			if d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		if d.IsDir() && strings.Count(strings.TrimPrefix(p, "files"), "/") > 32 {
			return fs.SkipDir
		}
		fi, err := d.Info()
		if err != nil {
			return nil
		}
		if !fi.IsDir() && !fi.Mode().IsRegular() {
			return nil
		}
		if strings.Contains(strings.ToLower(fi.Name()), q) {
			rel := strings.TrimPrefix(p, "files/")
			results = append(results, entryFrom(rel, fi))
			if len(results) >= limit {
				return fs.SkipAll
			}
		}
		return nil
	})
	if err != nil && !errors.Is(err, fs.SkipAll) {
		return nil, err
	}
	return results, nil
}

func (s *Store) WriteZip(rel string, zw *zip.Writer) error {
	rel, err := CleanRel(rel)
	if err != nil {
		return err
	}
	p := filesPath(rel)
	info, err := s.root.Lstat(p)
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return ErrInvalidPath
	}
	if info.Mode().IsRegular() {
		return s.zipFile(zw, p, path.Base(rel), info)
	}
	if !info.IsDir() {
		return ErrInvalidPath
	}
	base := ""
	if rel != "" {
		base = path.Base(rel)
	}
	err = fs.WalkDir(s.root.FS(), p, func(name string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if name == p {
			return nil
		}
		if d.Type()&fs.ModeSymlink != 0 {
			if d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		fi, err := d.Info()
		if err != nil {
			return err
		}
		relative := strings.TrimPrefix(name, p)
		relative = strings.TrimPrefix(relative, "/")
		if relative == "" {
			return nil
		}
		zipName := relative
		if base != "" {
			zipName = path.Join(base, relative)
		}
		if fi.IsDir() {
			h, err := zip.FileInfoHeader(fi)
			if err != nil {
				return err
			}
			h.Name = strings.TrimSuffix(zipName, "/") + "/"
			h.Method = zip.Store
			_, err = zw.CreateHeader(h)
			return err
		}
		if !fi.Mode().IsRegular() {
			return nil
		}
		return s.zipFile(zw, name, zipName, fi)
	})
	return err
}

func (s *Store) zipFile(zw *zip.Writer, name, zipName string, info os.FileInfo) error {
	h, err := zip.FileInfoHeader(info)
	if err != nil {
		return err
	}
	h.Name = zipName
	h.Method = zip.Store
	w, err := zw.CreateHeader(h)
	if err != nil {
		return err
	}
	f, err := s.root.Open(name)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	_, err = io.Copy(w, f)
	return err
}

func (s *Store) statLocked(rel string) (Entry, error) {
	info, err := s.root.Lstat(filesPath(rel))
	if err != nil {
		return Entry{}, err
	}
	return entryFrom(rel, info), nil
}

func (s *Store) treeSize(p string) (int64, error) {
	var total int64
	err := fs.WalkDir(s.root.FS(), p, func(_ string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.Type()&fs.ModeSymlink != 0 {
			if d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		if d.IsDir() {
			return nil
		}
		fi, err := d.Info()
		if err != nil {
			return err
		}
		if fi.Mode().IsRegular() {
			total += fi.Size()
		}
		return nil
	})
	return total, err
}

func (s *Store) diskUsed() (int64, error) {
	var total int64
	for _, dir := range []string{"files", ".trash"} {
		err := fs.WalkDir(s.root.FS(), dir, func(_ string, d fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if d.Type()&fs.ModeSymlink != 0 {
				if d.IsDir() {
					return fs.SkipDir
				}
				return nil
			}
			if d.IsDir() {
				return nil
			}
			fi, err := d.Info()
			if err != nil {
				return err
			}
			if fi.Mode().IsRegular() {
				total += fi.Size()
			}
			return nil
		})
		if err != nil {
			return 0, err
		}
	}
	return total, nil
}

func (s *Store) cleanupTemps(maxAge time.Duration) {
	cutoff := s.now().Add(-maxAge)
	_ = fs.WalkDir(s.root.FS(), ".tmp", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		fi, err := d.Info()
		if err != nil {
			return nil
		}
		if fi.ModTime().Before(cutoff) {
			_ = s.root.Remove(p)
		}
		return nil
	})
}

func (s *Store) readMeta(name string, v any) (bool, error) {
	data, err := s.root.ReadFile(".meta/" + name)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return false, nil
		}
		return false, err
	}
	if len(data) == 0 {
		return false, nil
	}
	if err := json.Unmarshal(data, v); err != nil {
		return false, err
	}
	return true, nil
}

func (s *Store) writeMeta(name string, v any) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	tmp := ".meta/.tmp-" + randomHex(8)
	f, err := s.root.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	if _, err := f.Write(data); err != nil {
		_ = f.Close()
		_ = s.root.Remove(tmp)
		return err
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		_ = s.root.Remove(tmp)
		return err
	}
	if err := f.Close(); err != nil {
		_ = s.root.Remove(tmp)
		return err
	}
	if err := s.root.Rename(tmp, ".meta/"+name); err != nil {
		_ = s.root.Remove(tmp)
		return err
	}
	return nil
}

func entryFrom(rel string, info os.FileInfo) Entry {
	return Entry{
		Name:    info.Name(),
		Path:    rel,
		IsDir:   info.IsDir(),
		Size:    info.Size(),
		ModTime: info.ModTime(),
		Kind:    classify(info.Name(), info.IsDir()),
	}
}

func classify(name string, isDir bool) string {
	if isDir {
		return "folder"
	}
	switch strings.ToLower(path.Ext(name)) {
	case ".jpg", ".jpeg", ".png", ".gif", ".webp", ".avif", ".bmp", ".heic":
		return "image"
	case ".mp4", ".webm", ".mov", ".mkv", ".m4v":
		return "video"
	case ".mp3", ".wav", ".ogg", ".flac", ".m4a", ".aac":
		return "audio"
	case ".pdf":
		return "pdf"
	case ".txt", ".md", ".markdown", ".json", ".yaml", ".yml", ".toml", ".ini",
		".conf", ".csv", ".log", ".go", ".js", ".ts", ".tsx", ".jsx", ".css",
		".html", ".htm", ".xml", ".sh", ".rs", ".c", ".h", ".cpp", ".hpp",
		".py", ".java", ".scala", ".nix":
		return "text"
	case ".zip", ".tar", ".gz", ".tgz", ".bz2", ".xz", ".7z", ".rar":
		return "archive"
	default:
		return "file"
	}
}

func randomHex(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic("crypto/rand failed: " + err.Error())
	}
	return hex.EncodeToString(b)
}
