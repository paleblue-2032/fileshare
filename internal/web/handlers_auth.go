package web

import (
	"net/http"

	"github.com/paleblue-2032/fileshare/internal/auth"
)

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	ip := s.clientIP(r)
	if !s.loginLimiter.Allow(ip) || !s.globalLoginLimiter.Allow("global") {
		writeError(w, http.StatusTooManyRequests, "too many attempts, try again later")
		return
	}
	var req struct {
		Password string `json:"password"`
	}
	if err := readJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request")
		return
	}
	if req.Password == "" || !auth.VerifyPassword(s.cfg.PasswordHash, req.Password) {
		s.logger.Warn("login failed", "ip", ip)
		writeError(w, http.StatusUnauthorized, "invalid password")
		return
	}
	s.loginLimiter.Reset(ip)
	s.globalLoginLimiter.Reset("global")
	token, _, err := s.sessions.Create()
	if err != nil {
		s.logger.Error("create session", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	s.setSessionCookie(w, r, token)
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	s.sessions.Revoke(s.sessionToken(r))
	s.clearSessionCookie(w, r)
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) handleSession(w http.ResponseWriter, r *http.Request) {
	used, limit := s.store.QuotaSnapshot()
	writeJSON(w, http.StatusOK, map[string]any{
		"authenticated":  true,
		"usedBytes":      used,
		"quotaBytes":     limit,
		"trashBytes":     s.store.TrashTotalBytes(),
		"retentionDays":  s.cfg.RetentionDays,
		"trashDays":      s.cfg.TrashDays,
		"maxUploadBytes": s.cfg.MaxUploadBytes,
		"webdav":         s.cfg.WebDAV,
	})
}
