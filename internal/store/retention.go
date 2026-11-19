package store

import (
	"errors"
	"io/fs"
	"strings"
	"time"
)

type SweepResult struct {
	FilesExpired int
	TrashPurged  int
	SharesPruned int
	Errors       int
}

// Sweep applies the retention policy: old files move to Trash, and old Trash
// items are permanently deleted. It also drops expired share links.
func (s *Store) Sweep(now time.Time, retentionDays, trashDays, maxFiles int) SweepResult {
	var res SweepResult

	if trashDays > 0 {
		cutoff := now.AddDate(0, 0, -trashDays)
		s.mu.Lock()
		ids := make([]string, 0)
		for _, item := range s.trashItems {
			if item.DeletedAt.Before(cutoff) {
				ids = append(ids, item.ID)
			}
		}
		s.mu.Unlock()
		for _, id := range ids {
			err := s.Purge(id)
			if err == nil {
				res.TrashPurged++
			} else if !errors.Is(err, ErrNotFound) {
				res.Errors++
			}
		}
	}

	s.mu.Lock()
	kept := s.shareItems[:0]
	for _, item := range s.shareItems {
		if item.ExpiresAt != nil && !now.Before(*item.ExpiresAt) {
			res.SharesPruned++
			continue
		}
		kept = append(kept, item)
	}
	if res.SharesPruned > 0 {
		s.shareItems = kept
		if err := s.saveSharesLocked(); err != nil {
			res.Errors++
		}
	}
	s.mu.Unlock()

	if retentionDays <= 0 {
		return res
	}
	if maxFiles <= 0 {
		maxFiles = 500
	}
	cutoff := now.AddDate(0, 0, -retentionDays)
	candidates := make([]string, 0, 64)
	err := fs.WalkDir(s.root.FS(), "files", func(p string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return nil
		}
		if p == "files" {
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
			return nil
		}
		if fi.IsDir() || !fi.Mode().IsRegular() {
			return nil
		}
		if fi.ModTime().Before(cutoff) {
			candidates = append(candidates, strings.TrimPrefix(p, "files/"))
			if len(candidates) >= maxFiles {
				return fs.SkipAll
			}
		}
		return nil
	})
	if err != nil && !errors.Is(err, fs.SkipAll) {
		res.Errors++
		return res
	}
	for _, rel := range candidates {
		if _, err := s.deleteToTrash(rel, "retention"); err != nil {
			if !errors.Is(err, fs.ErrNotExist) {
				res.Errors++
			}
		} else {
			res.FilesExpired++
		}
	}
	return res
}
