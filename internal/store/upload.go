package store

import (
	"errors"
	"io/fs"
	"os"
	"sync"
)

type Upload struct {
	store   *Store
	dirRel  string
	name    string
	tempRel string
	file    *os.File
	qw      *quotaWriter
	mu      sync.Mutex
	done    bool
}

func (s *Store) BeginUpload(dirRel, name string) (*Upload, error) {
	dirRel, err := CleanRel(dirRel)
	if err != nil {
		return nil, err
	}
	name, err = CleanUploadName(name)
	if err != nil {
		return nil, err
	}
	info, err := s.root.Lstat(filesPath(dirRel))
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		return nil, ErrNotDir
	}
	tempRel := ".tmp/upload-" + randomHex(16)
	f, err := s.root.OpenFile(tempRel, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return nil, err
	}
	return &Upload{
		store:   s,
		dirRel:  dirRel,
		name:    name,
		tempRel: tempRel,
		file:    f,
		qw:      newQuotaWriter(f, s.quota),
	}, nil
}

func (u *Upload) Write(p []byte) (int, error) {
	u.mu.Lock()
	defer u.mu.Unlock()
	if u.done {
		return 0, fs.ErrClosed
	}
	return u.qw.Write(p)
}

func (u *Upload) Commit() (Entry, error) {
	u.mu.Lock()
	defer u.mu.Unlock()
	if u.done {
		return Entry{}, fs.ErrClosed
	}
	u.done = true

	if err := u.file.Sync(); err != nil {
		_ = u.file.Close()
		_ = u.store.root.Remove(u.tempRel)
		u.qw.finish(false)
		return Entry{}, err
	}
	if err := u.file.Close(); err != nil {
		_ = u.store.root.Remove(u.tempRel)
		u.qw.finish(false)
		return Entry{}, err
	}

	targetRel, err := u.store.commitTemp(u.tempRel, u.dirRel, u.name)
	if err != nil {
		_ = u.store.root.Remove(u.tempRel)
		u.qw.finish(false)
		return Entry{}, err
	}
	u.qw.finish(true)
	return u.store.Stat(targetRel)
}

func (u *Upload) Abort() {
	u.mu.Lock()
	defer u.mu.Unlock()
	if u.done {
		return
	}
	u.done = true
	_ = u.file.Close()
	_ = u.store.root.Remove(u.tempRel)
	u.qw.finish(false)
}

// commitTemp links a temporary file into the share tree without overwriting an
// existing name. Hard links are used because rename(2) silently replaces files.
func (s *Store) commitTemp(tempRel, dirRel, name string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for attempt := 0; attempt < 8; attempt++ {
		targetRel, err := s.uniqueNameLocked(dirRel, name)
		if err != nil {
			return "", err
		}
		err = s.root.Link(tempRel, filesPath(targetRel))
		if err == nil {
			_ = s.root.Remove(tempRel)
			return targetRel, nil
		}
		if errors.Is(err, fs.ErrExist) {
			continue
		}
		// Some filesystems may not support hard links; fall back to an
		// existence-checked rename while still holding the mutation lock.
		if _, statErr := s.root.Lstat(filesPath(targetRel)); errors.Is(statErr, fs.ErrNotExist) {
			if renameErr := s.root.Rename(tempRel, filesPath(targetRel)); renameErr == nil {
				return targetRel, nil
			}
		}
		return "", err
	}
	return "", ErrExists
}
