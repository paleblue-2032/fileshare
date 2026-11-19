package store

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"sort"
	"strings"
	"time"
)

type TrashItem struct {
	ID           string    `json:"id"`
	OriginalPath string    `json:"originalPath"`
	Name         string    `json:"name"`
	IsDir        bool      `json:"isDir"`
	Size         int64     `json:"size"`
	DeletedAt    time.Time `json:"deletedAt"`
	Reason       string    `json:"reason"`
}

func (s *Store) loadTrash() error {
	var f struct {
		Items []TrashItem `json:"items"`
	}
	ok, err := s.readMeta("trash.json", &f)
	if err != nil {
		return fmt.Errorf("load trash metadata: %w", err)
	}
	if ok {
		s.trashItems = f.Items
	}
	return nil
}

func (s *Store) saveTrashLocked() error {
	return s.writeMeta("trash.json", struct {
		Items []TrashItem `json:"items"`
	}{Items: s.trashItems})
}

func (s *Store) TrashList() []TrashItem {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]TrashItem, len(s.trashItems))
	copy(out, s.trashItems)
	sort.Slice(out, func(i, j int) bool { return out[i].DeletedAt.After(out[j].DeletedAt) })
	return out
}

func (s *Store) TrashTotalBytes() int64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	var total int64
	for _, item := range s.trashItems {
		total += item.Size
	}
	return total
}

func (s *Store) moveToTrashLocked(rel, reason string) (TrashItem, error) {
	p := filesPath(rel)
	info, err := s.root.Lstat(p)
	if err != nil {
		return TrashItem{}, err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return TrashItem{}, ErrInvalidPath
	}
	size := info.Size()
	if info.IsDir() {
		size, err = s.treeSize(p)
		if err != nil {
			return TrashItem{}, err
		}
	}
	id := s.newTrashIDLocked()
	if err := s.root.Rename(p, ".trash/"+id); err != nil {
		return TrashItem{}, err
	}
	item := TrashItem{
		ID:           id,
		OriginalPath: rel,
		Name:         path.Base(rel),
		IsDir:        info.IsDir(),
		Size:         size,
		DeletedAt:    s.now().UTC(),
		Reason:       reason,
	}
	s.trashItems = append(s.trashItems, item)
	if err := s.saveTrashLocked(); err != nil {
		s.trashItems = s.trashItems[:len(s.trashItems)-1]
		_ = s.root.Rename(".trash/"+id, p)
		return TrashItem{}, err
	}
	return item, nil
}

func (s *Store) newTrashIDLocked() string {
	base := s.now().UTC().Format("20060102T150405") + "-" + randomHex(6)
	id := base
	for i := 0; i < 8; i++ {
		if _, err := s.root.Lstat(".trash/" + id); errors.Is(err, fs.ErrNotExist) {
			return id
		}
		id = base + "-" + randomHex(2)
	}
	return base + "-" + randomHex(8)
}

func (s *Store) Restore(id string) (Entry, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	idx := -1
	for i := range s.trashItems {
		if s.trashItems[i].ID == id {
			idx = i
			break
		}
	}
	if idx < 0 {
		return Entry{}, ErrNotFound
	}
	item := s.trashItems[idx]
	if _, err := s.root.Lstat(".trash/" + id); err != nil {
		return Entry{}, err
	}
	dirRel := cleanParent(item.OriginalPath)
	targetRel, err := s.uniqueNameLocked(dirRel, path.Base(item.OriginalPath))
	if err != nil {
		return Entry{}, err
	}
	if err := s.root.MkdirAll(filesPath(dirRel), 0o750); err != nil {
		return Entry{}, err
	}
	if err := s.root.Rename(".trash/"+id, filesPath(targetRel)); err != nil {
		return Entry{}, err
	}
	s.trashItems = append(s.trashItems[:idx], s.trashItems[idx+1:]...)
	if err := s.saveTrashLocked(); err != nil {
		_ = s.root.Rename(filesPath(targetRel), ".trash/"+id)
		s.trashItems = append(s.trashItems, item)
		return Entry{}, err
	}
	return s.statLocked(targetRel)
}

func (s *Store) Purge(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	idx := -1
	for i := range s.trashItems {
		if s.trashItems[i].ID == id {
			idx = i
			break
		}
	}
	if idx < 0 {
		return ErrNotFound
	}
	item := s.trashItems[idx]
	size := item.Size
	if actual, err := s.treeSize(".trash/" + id); err == nil {
		size = actual
	}
	if err := s.root.RemoveAll(".trash/" + id); err != nil {
		return err
	}
	s.quota.SubUsed(size)
	s.trashItems = append(s.trashItems[:idx], s.trashItems[idx+1:]...)
	return s.saveTrashLocked()
}

func (s *Store) EmptyTrash() (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.trashItems) == 0 {
		return 0, nil
	}
	var freed int64
	count := 0
	for _, item := range s.trashItems {
		size := item.Size
		if actual, err := s.treeSize(".trash/" + item.ID); err == nil {
			size = actual
		}
		if err := s.root.RemoveAll(".trash/" + item.ID); err != nil {
			return count, err
		}
		freed += size
		count++
	}
	s.quota.SubUsed(freed)
	s.trashItems = nil
	if err := s.saveTrashLocked(); err != nil {
		return count, err
	}
	return count, nil
}

// uniqueNameLocked returns a path in dirRel that does not collide with an
// existing entry, adding " (1)", " (2)" and so on before the extension.
func (s *Store) uniqueNameLocked(dirRel, name string) (string, error) {
	try := func(candidate string) (string, error) {
		rel := joinRel(dirRel, candidate)
		_, err := s.root.Lstat(filesPath(rel))
		if errors.Is(err, fs.ErrNotExist) {
			return rel, nil
		}
		if err != nil {
			return "", err
		}
		return "", nil
	}
	if rel, err := try(name); err != nil || rel != "" {
		return rel, err
	}
	ext := path.Ext(name)
	stem := strings.TrimSuffix(name, ext)
	for i := 1; i < 1000; i++ {
		candidate := fmt.Sprintf("%s (%d)%s", stem, i, ext)
		rel, err := try(candidate)
		if err != nil {
			return "", err
		}
		if rel != "" {
			return rel, nil
		}
	}
	return "", ErrExists
}
