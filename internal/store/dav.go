package store

import (
	"errors"
	"io/fs"
	"os"
	"sync"
)

// DAVFile adapts *os.File for webdav.File while charging bytes written to the
// quota. The embedded file supplies Read, Seek, Readdir and Stat.
type DAVFile struct {
	*os.File
	qw       *quotaWriter
	once     sync.Once
	closeErr error
}

func (f *DAVFile) Write(p []byte) (int, error) {
	if f.qw == nil {
		return f.File.Write(p)
	}
	return f.qw.Write(p)
}

func (f *DAVFile) Close() error {
	f.once.Do(func() {
		if f.qw != nil {
			f.qw.finish(true)
		}
		f.closeErr = f.File.Close()
	})
	return f.closeErr
}

func (s *Store) DAVStat(name string) (os.FileInfo, error) {
	rel, err := CleanRel(name)
	if err != nil {
		return nil, fs.ErrNotExist
	}
	info, err := s.root.Lstat(filesPath(rel))
	if err != nil {
		return nil, err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return nil, fs.ErrNotExist
	}
	if !info.IsDir() && !info.Mode().IsRegular() {
		return nil, fs.ErrNotExist
	}
	return info, nil
}

func (s *Store) DAVMkdir(name string, perm os.FileMode) error {
	rel, err := CleanRel(name)
	if err != nil {
		return err
	}
	if rel == "" {
		return fs.ErrExist
	}
	return s.root.Mkdir(filesPath(rel), perm)
}

func (s *Store) DAVOpen(name string, flag int, perm os.FileMode) (*DAVFile, error) {
	rel, err := CleanRel(name)
	if err != nil {
		return nil, fs.ErrNotExist
	}
	p := filesPath(rel)
	var oldSize int64
	subtracted := false
	if info, statErr := s.root.Lstat(p); statErr == nil {
		if info.Mode()&os.ModeSymlink != 0 {
			return nil, fs.ErrNotExist
		}
		if info.Mode().IsRegular() && flag&os.O_TRUNC != 0 && flag&(os.O_WRONLY|os.O_RDWR) != 0 {
			oldSize = info.Size()
			s.quota.SubUsed(oldSize)
			subtracted = true
		}
	}
	f, err := s.root.OpenFile(p, flag, perm)
	if err != nil {
		if subtracted {
			s.quota.AddUsed(oldSize)
		}
		return nil, err
	}
	var qw *quotaWriter
	if flag&(os.O_WRONLY|os.O_RDWR) != 0 {
		qw = newQuotaWriter(f, s.quota)
	}
	return &DAVFile{File: f, qw: qw}, nil
}

func (s *Store) DAVRename(oldName, newName string) error {
	oldRel, err := CleanRel(oldName)
	if err != nil {
		return err
	}
	newRel, err := CleanRel(newName)
	if err != nil {
		return err
	}
	if oldRel == "" || newRel == "" {
		return ErrRootOperation
	}
	var replacedSize int64
	if info, statErr := s.root.Lstat(filesPath(newRel)); statErr == nil {
		if info.IsDir() {
			return fs.ErrExist
		}
		replacedSize = info.Size()
	} else if !errors.Is(statErr, fs.ErrNotExist) {
		return statErr
	}
	if err := s.root.Rename(filesPath(oldRel), filesPath(newRel)); err != nil {
		return err
	}
	if replacedSize > 0 {
		s.quota.SubUsed(replacedSize)
	}
	return nil
}

func (s *Store) DAVRemove(name string) error {
	rel, err := CleanRel(name)
	if err != nil {
		return err
	}
	if rel == "" {
		return ErrRootOperation
	}
	_, err = s.deleteToTrash(rel, "webdav")
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	return err
}
