package store

import (
	"sync"
	"time"
)

// Quota tracks committed bytes (files + trash) and in-flight bytes (temporary
// uploads and WebDAV writes), and refuses writes that would exhaust the quota
// or leave the underlying filesystem critically low.
type Quota struct {
	mu        sync.Mutex
	limit     int64
	minFree   int64
	used      int64
	inflight  int64
	dataDir   string
	lastCheck time.Time
	freeCache int64
	now       func() time.Time
	statFree  func(string) (int64, error)
}

func NewQuota(limit, minFree int64, dataDir string) *Quota {
	return &Quota{
		limit:    limit,
		minFree:  minFree,
		dataDir:  dataDir,
		now:      time.Now,
		statFree: diskFree,
	}
}

func (q *Quota) SetUsed(n int64) {
	if n < 0 {
		n = 0
	}
	q.mu.Lock()
	q.used = n
	q.mu.Unlock()
}

func (q *Quota) AddUsed(n int64) {
	if n <= 0 {
		return
	}
	q.mu.Lock()
	q.used += n
	q.mu.Unlock()
}

func (q *Quota) SubUsed(n int64) {
	if n <= 0 {
		return
	}
	q.mu.Lock()
	q.used -= n
	if q.used < 0 {
		q.used = 0
	}
	q.mu.Unlock()
}

func (q *Quota) Finish(written int64, commit bool) {
	if written <= 0 {
		return
	}
	q.mu.Lock()
	q.inflight -= written
	if q.inflight < 0 {
		q.inflight = 0
	}
	if commit {
		q.used += written
	}
	q.mu.Unlock()
}

func (q *Quota) AddInflight(n int64) {
	if n <= 0 {
		return
	}
	q.mu.Lock()
	q.inflight += n
	q.mu.Unlock()
}

func (q *Quota) CheckWrite(n int64) error {
	if n < 0 {
		return nil
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.used+q.inflight+n > q.limit {
		return ErrQuotaExceeded
	}
	if q.minFree > 0 {
		free, err := q.freeLocked()
		if err != nil {
			return err
		}
		if free-n < q.minFree {
			return ErrDiskLow
		}
	}
	return nil
}

func (q *Quota) freeLocked() (int64, error) {
	now := q.now()
	if !q.lastCheck.IsZero() && now.Sub(q.lastCheck) < 2*time.Second {
		return q.freeCache, nil
	}
	free, err := q.statFree(q.dataDir)
	if err != nil {
		return 0, err
	}
	q.lastCheck = now
	q.freeCache = free
	return free, nil
}

func (q *Quota) Snapshot() (used, limit int64) {
	q.mu.Lock()
	defer q.mu.Unlock()
	return q.used, q.limit
}

func (q *Quota) Remaining() int64 {
	q.mu.Lock()
	defer q.mu.Unlock()
	n := q.limit - q.used
	if n < 0 {
		return 0
	}
	return n
}

type quotaWriter struct {
	w        writer
	q        *Quota
	written  int64
	finished bool
}

type writer interface {
	Write([]byte) (int, error)
}

func newQuotaWriter(w writer, q *Quota) *quotaWriter {
	return &quotaWriter{w: w, q: q}
}

func (w *quotaWriter) Write(p []byte) (int, error) {
	if w.finished {
		return 0, ErrQuotaExceeded
	}
	if err := w.q.CheckWrite(int64(len(p))); err != nil {
		return 0, err
	}
	n, err := w.w.Write(p)
	if n > 0 {
		w.written += int64(n)
		w.q.AddInflight(int64(n))
	}
	return n, err
}

func (w *quotaWriter) finish(commit bool) {
	if w.finished {
		return
	}
	w.finished = true
	w.q.Finish(w.written, commit)
}
