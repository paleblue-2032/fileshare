package auth

import (
	"crypto/rand"
	"encoding/base64"
	"sync"
	"time"
)

const CookieName = "fileshare_session"

type SessionManager struct {
	mu       sync.Mutex
	sessions map[string]time.Time
	ttl      time.Duration
	now      func() time.Time
}

func NewSessionManager(ttl time.Duration) *SessionManager {
	return &SessionManager{
		sessions: make(map[string]time.Time),
		ttl:      ttl,
		now:      time.Now,
	}
}

func (m *SessionManager) Create() (string, time.Time, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", time.Time{}, err
	}
	token := base64.RawURLEncoding.EncodeToString(buf)
	expires := m.now().Add(m.ttl)

	m.mu.Lock()
	defer m.mu.Unlock()
	m.cleanupLocked()
	m.sessions[token] = expires
	return token, expires, nil
}

// Touch validates a token and extends its lifetime while the device is active.
func (m *SessionManager) Touch(token string) bool {
	if token == "" {
		return false
	}
	now := m.now()
	m.mu.Lock()
	defer m.mu.Unlock()
	expires, ok := m.sessions[token]
	if !ok {
		return false
	}
	if !now.Before(expires) {
		delete(m.sessions, token)
		return false
	}
	if expires.Sub(now) < m.ttl/2 {
		m.sessions[token] = now.Add(m.ttl)
	}
	return true
}

func (m *SessionManager) Revoke(token string) {
	if token == "" {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.sessions, token)
}

func (m *SessionManager) Count() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.sessions)
}

func (m *SessionManager) cleanupLocked() {
	if len(m.sessions) < 1000 {
		return
	}
	now := m.now()
	for token, expires := range m.sessions {
		if !now.Before(expires) {
			delete(m.sessions, token)
		}
	}
}
