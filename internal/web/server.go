package web

import (
	"embed"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/paleblue-2032/fileshare/internal/auth"
	"github.com/paleblue-2032/fileshare/internal/config"
	"github.com/paleblue-2032/fileshare/internal/store"
	"golang.org/x/net/webdav"
)

//go:embed assets/*
var assetFS embed.FS

type Server struct {
	cfg    config.Config
	store  *store.Store
	logger *slog.Logger

	sessions           *auth.SessionManager
	loginLimiter       *auth.RateLimiter
	globalLoginLimiter *auth.RateLimiter
	davLimiter         *auth.RateLimiter
	davCache           *auth.AuthCache

	dav    *webdav.Handler
	assets http.Handler
	now    func() time.Time
}

func New(cfg config.Config, st *store.Store, logger *slog.Logger) (*Server, error) {
	sub, err := fs.Sub(assetFS, "assets")
	if err != nil {
		return nil, err
	}
	s := &Server{
		cfg:                cfg,
		store:              st,
		logger:             logger,
		sessions:           auth.NewSessionManager(cfg.SessionTTL),
		loginLimiter:       auth.NewRateLimiter(10, 10*time.Minute),
		globalLoginLimiter: auth.NewRateLimiter(300, 10*time.Minute),
		davLimiter:         auth.NewRateLimiter(30, 10*time.Minute),
		davCache:           auth.NewAuthCache(5 * time.Minute),
		assets:             http.FileServer(http.FS(sub)),
		now:                time.Now,
	}
	s.dav = &webdav.Handler{
		Prefix:     "/dav",
		FileSystem: &davFS{store: st},
		LockSystem: webdav.NewMemLS(),
		Logger: func(r *http.Request, err error) {
			if err != nil {
				logger.Warn("webdav request failed", "method", r.Method, "path", r.URL.Path, "error", err)
			}
		},
	}
	return s, nil
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /healthz", s.handleHealth)
	mux.HandleFunc("POST /api/login", s.handleLogin)
	mux.HandleFunc("POST /api/logout", s.requireAuth(s.handleLogout))
	mux.HandleFunc("GET /api/session", s.requireAuth(s.handleSession))

	mux.HandleFunc("GET /api/list", s.requireAuth(s.handleList))
	mux.HandleFunc("GET /api/search", s.requireAuth(s.handleSearch))
	mux.HandleFunc("POST /api/upload", s.requireAuth(s.handleUpload))
	mux.HandleFunc("GET /api/download", s.requireAuth(s.handleDownload))
	mux.HandleFunc("GET /api/zip", s.requireAuth(s.handleZip))
	mux.HandleFunc("POST /api/mkdir", s.requireAuth(s.handleMkdir))
	mux.HandleFunc("POST /api/rename", s.requireAuth(s.handleRename))
	mux.HandleFunc("POST /api/move", s.requireAuth(s.handleMove))
	mux.HandleFunc("POST /api/delete", s.requireAuth(s.handleDelete))

	mux.HandleFunc("GET /api/trash", s.requireAuth(s.handleTrashList))
	mux.HandleFunc("POST /api/trash/restore", s.requireAuth(s.handleTrashRestore))
	mux.HandleFunc("POST /api/trash/purge", s.requireAuth(s.handleTrashPurge))
	mux.HandleFunc("POST /api/trash/empty", s.requireAuth(s.handleTrashEmpty))

	mux.HandleFunc("GET /api/shares", s.requireAuth(s.handleSharesList))
	mux.HandleFunc("POST /api/shares", s.requireAuth(s.handleShareCreate))
	mux.HandleFunc("DELETE /api/shares/{token}", s.requireAuth(s.handleShareRevoke))

	mux.HandleFunc("GET /s/{token}", s.handleSharePage)
	mux.HandleFunc("GET /s/{token}/download", s.handleShareDownload)

	if s.cfg.WebDAV {
		mux.Handle("/dav/", s.davAuth(s.dav))
		mux.HandleFunc("/dav", func(w http.ResponseWriter, r *http.Request) {
			http.Redirect(w, r, "/dav/", http.StatusMovedPermanently)
		})
	}

	mux.Handle("GET /assets/", http.StripPrefix("/assets/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-cache")
		s.assets.ServeHTTP(w, r)
	})))
	mux.HandleFunc("GET /login", s.handleLoginPage)
	mux.HandleFunc("GET /{$}", s.handleIndex)
	mux.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) {
		writeError(w, http.StatusNotFound, "not found")
	})

	return s.securityHeaders(s.logRequests(s.recoverPanic(s.csrf(mux))))
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = io.WriteString(w, "ok\n")
}

func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	if !s.sessions.Touch(s.sessionToken(r)) {
		http.Redirect(w, r, "/login", http.StatusFound)
		return
	}
	s.serveAsset(w, "index.html")
}

func (s *Server) handleLoginPage(w http.ResponseWriter, r *http.Request) {
	if s.sessions.Touch(s.sessionToken(r)) {
		http.Redirect(w, r, "/", http.StatusFound)
		return
	}
	s.serveAsset(w, "login.html")
}

func (s *Server) serveAsset(w http.ResponseWriter, name string) {
	data, err := assetFS.ReadFile("assets/" + name)
	if err != nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write(data)
}

func (s *Server) requireAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !s.sessions.Touch(s.sessionToken(r)) {
			writeError(w, http.StatusUnauthorized, "unauthenticated")
			return
		}
		next(w, r)
	}
}

func (s *Server) sessionToken(r *http.Request) string {
	cookie, err := r.Cookie(auth.CookieName)
	if err != nil {
		return ""
	}
	return cookie.Value
}

func (s *Server) setSessionCookie(w http.ResponseWriter, r *http.Request, token string) {
	http.SetCookie(w, &http.Cookie{
		Name:     auth.CookieName,
		Value:    token,
		Path:     "/",
		MaxAge:   int(s.cfg.SessionTTL.Seconds()),
		HttpOnly: true,
		Secure:   isHTTPS(r),
		SameSite: http.SameSiteStrictMode,
	})
}

func (s *Server) clearSessionCookie(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{
		Name:     auth.CookieName,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   isHTTPS(r),
		SameSite: http.SameSiteStrictMode,
	})
}

func (s *Server) clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	if host == "127.0.0.1" || host == "::1" {
		if forwarded := r.Header.Get("X-Forwarded-For"); forwarded != "" {
			if first, _, ok := strings.Cut(forwarded, ","); ok {
				forwarded = first
			}
			if strings.TrimSpace(forwarded) != "" {
				return strings.TrimSpace(forwarded)
			}
		}
	}
	return host
}

func isHTTPS(r *http.Request) bool {
	return r.TLS != nil || strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https")
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}

func readJSON(w http.ResponseWriter, r *http.Request, dst any) error {
	body := http.MaxBytesReader(w, r.Body, 64<<10)
	dec := json.NewDecoder(body)
	if err := dec.Decode(dst); err != nil {
		return err
	}
	if err := dec.Decode(&struct{}{}); err != io.EOF {
		return errors.New("unexpected trailing data")
	}
	return nil
}

func mapStoreError(err error) (int, string) {
	switch {
	case err == nil:
		return http.StatusOK, ""
	case errors.Is(err, store.ErrNotFound), errors.Is(err, fs.ErrNotExist):
		return http.StatusNotFound, "not found"
	case errors.Is(err, store.ErrExists), errors.Is(err, fs.ErrExist):
		return http.StatusConflict, "already exists"
	case errors.Is(err, store.ErrInvalidPath), errors.Is(err, store.ErrNotDir),
		errors.Is(err, store.ErrIsDir), errors.Is(err, store.ErrRootOperation):
		return http.StatusBadRequest, err.Error()
	case errors.Is(err, store.ErrQuotaExceeded):
		return http.StatusInsufficientStorage, "storage quota exceeded"
	case errors.Is(err, store.ErrDiskLow):
		return http.StatusInsufficientStorage, "server storage is low"
	case errors.Is(err, store.ErrTooLarge):
		return http.StatusRequestEntityTooLarge, "file is too large"
	default:
		return http.StatusInternalServerError, "internal error"
	}
}

func (s *Server) handleStoreError(w http.ResponseWriter, err error) {
	status, message := mapStoreError(err)
	if status >= 500 {
		s.logger.Error("request failed", "error", err)
	}
	writeError(w, status, message)
}

func (s *Server) shareURL(r *http.Request, token string) string {
	scheme := "http"
	if isHTTPS(r) {
		scheme = "https"
	}
	return scheme + "://" + r.Host + "/s/" + token
}
