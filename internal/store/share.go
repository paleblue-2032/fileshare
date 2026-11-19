package store

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"os"
	"path"
	"sort"
	"time"
)

type ShareItem struct {
	Token     string     `json:"token"`
	Path      string     `json:"path"`
	IsDir     bool       `json:"isDir"`
	CreatedAt time.Time  `json:"createdAt"`
	ExpiresAt *time.Time `json:"expiresAt,omitempty"`
}

func (s *Store) loadShares() error {
	var f struct {
		Items []ShareItem `json:"items"`
	}
	ok, err := s.readMeta("shares.json", &f)
	if err != nil {
		return fmt.Errorf("load share metadata: %w", err)
	}
	if ok {
		s.shareItems = f.Items
	}
	return nil
}

func (s *Store) saveSharesLocked() error {
	return s.writeMeta("shares.json", struct {
		Items []ShareItem `json:"items"`
	}{Items: s.shareItems})
}

func (s *Store) CreateShare(rel string, ttl time.Duration) (ShareItem, error) {
	rel, err := CleanRel(rel)
	if err != nil {
		return ShareItem{}, err
	}
	if rel == "" {
		return ShareItem{}, ErrRootOperation
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	info, err := s.root.Lstat(filesPath(rel))
	if err != nil {
		return ShareItem{}, err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return ShareItem{}, ErrInvalidPath
	}
	token := ""
	for i := 0; i < 5; i++ {
		token, err = newShareToken()
		if err != nil {
			return ShareItem{}, err
		}
		if !s.shareExistsLocked(token) {
			break
		}
	}
	item := ShareItem{
		Token:     token,
		Path:      rel,
		IsDir:     info.IsDir(),
		CreatedAt: s.now().UTC(),
	}
	if ttl > 0 {
		expires := s.now().UTC().Add(ttl)
		item.ExpiresAt = &expires
	}
	s.shareItems = append(s.shareItems, item)
	if err := s.saveSharesLocked(); err != nil {
		s.shareItems = s.shareItems[:len(s.shareItems)-1]
		return ShareItem{}, err
	}
	return item, nil
}

func (s *Store) Share(token string) (ShareItem, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	idx := -1
	for i := range s.shareItems {
		if s.shareItems[i].Token == token {
			idx = i
			break
		}
	}
	if idx < 0 {
		return ShareItem{}, ErrNotFound
	}
	item := s.shareItems[idx]
	if item.ExpiresAt != nil && !s.now().Before(*item.ExpiresAt) {
		return ShareItem{}, ErrNotFound
	}
	if _, err := s.root.Lstat(filesPath(item.Path)); err != nil {
		return ShareItem{}, ErrNotFound
	}
	return item, nil
}

func (s *Store) ListShares() []ShareItem {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]ShareItem, 0, len(s.shareItems))
	now := s.now()
	for _, item := range s.shareItems {
		if item.ExpiresAt != nil && !now.Before(*item.ExpiresAt) {
			continue
		}
		out = append(out, item)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.After(out[j].CreatedAt) })
	return out
}

func (s *Store) RevokeShare(token string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	idx := -1
	for i := range s.shareItems {
		if s.shareItems[i].Token == token {
			idx = i
			break
		}
	}
	if idx < 0 {
		return ErrNotFound
	}
	s.shareItems = append(s.shareItems[:idx], s.shareItems[idx+1:]...)
	return s.saveSharesLocked()
}

// ResolveShareSub resolves a path inside a shared file or folder.
func (s *Store) ResolveShareSub(item ShareItem, sub string) (string, error) {
	sub, err := CleanRel(sub)
	if err != nil {
		return "", err
	}
	if item.IsDir {
		if sub == "" {
			return item.Path, nil
		}
		return path.Join(item.Path, sub), nil
	}
	if sub != "" {
		return "", ErrNotFound
	}
	return item.Path, nil
}

func (s *Store) shareExistsLocked(token string) bool {
	for _, item := range s.shareItems {
		if item.Token == token {
			return true
		}
	}
	return false
}

func newShareToken() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}
