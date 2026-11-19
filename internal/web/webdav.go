package web

import (
	"context"
	"net/http"
	"os"

	"github.com/paleblue-2032/fileshare/internal/auth"
	"github.com/paleblue-2032/fileshare/internal/store"
	"golang.org/x/net/webdav"
)

type davFS struct {
	store *store.Store
}

func (f *davFS) Mkdir(ctx context.Context, name string, perm os.FileMode) error {
	return f.store.DAVMkdir(name, perm)
}

func (f *davFS) OpenFile(ctx context.Context, name string, flag int, perm os.FileMode) (webdav.File, error) {
	return f.store.DAVOpen(name, flag, perm)
}

func (f *davFS) RemoveAll(ctx context.Context, name string) error {
	return f.store.DAVRemove(name)
}

func (f *davFS) Rename(ctx context.Context, oldName, newName string) error {
	return f.store.DAVRename(oldName, newName)
}

func (f *davFS) Stat(ctx context.Context, name string) (os.FileInfo, error) {
	return f.store.DAVStat(name)
}

func (s *Server) davAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, password, ok := r.BasicAuth()
		if !ok {
			s.davChallenge(w)
			return
		}
		digest := auth.Digest(password)
		if !s.davCache.Allowed(digest) {
			ip := s.clientIP(r)
			if !s.davLimiter.Allow(ip) {
				http.Error(w, "too many attempts", http.StatusTooManyRequests)
				return
			}
			hash := s.cfg.PasswordHash
			if s.cfg.DAVPasswordHash != "" {
				hash = s.cfg.DAVPasswordHash
			}
			if !auth.VerifyPassword(hash, password) {
				s.logger.Warn("webdav authentication failed", "ip", ip)
				s.davChallenge(w)
				return
			}
			s.davLimiter.Reset(ip)
			s.davCache.Remember(digest)
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) davChallenge(w http.ResponseWriter) {
	w.Header().Set("WWW-Authenticate", `Basic realm="Fileshare", charset="UTF-8"`)
	http.Error(w, "authentication required", http.StatusUnauthorized)
}
