package web

import (
	"archive/zip"
	"errors"
	"io"
	"mime"
	"net/http"
	"os"
	"path"
	"strconv"
	"strings"

	"github.com/paleblue-2032/fileshare/internal/store"
)

func (s *Server) handleList(w http.ResponseWriter, r *http.Request) {
	rel, err := store.CleanRel(r.URL.Query().Get("path"))
	if err != nil {
		s.handleStoreError(w, err)
		return
	}
	entries, err := s.store.List(rel)
	if err != nil {
		s.handleStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"path": rel, "entries": entries})
}

func (s *Server) handleSearch(w http.ResponseWriter, r *http.Request) {
	rel, err := store.CleanRel(r.URL.Query().Get("path"))
	if err != nil {
		s.handleStoreError(w, err)
		return
	}
	limit := 200
	if raw := r.URL.Query().Get("limit"); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil {
			limit = n
		}
	}
	entries, err := s.store.Search(rel, r.URL.Query().Get("q"), limit)
	if err != nil {
		s.handleStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"entries": entries})
}

func (s *Server) handleUpload(w http.ResponseWriter, r *http.Request) {
	dir := r.URL.Query().Get("path")
	r.Body = http.MaxBytesReader(w, r.Body, s.cfg.MaxUploadBytes+(1<<20))
	mr, err := r.MultipartReader()
	if err != nil {
		writeError(w, http.StatusBadRequest, "expected a multipart upload")
		return
	}
	uploaded := make([]store.Entry, 0, 4)
	buf := make([]byte, 64<<10)
	for {
		part, err := mr.NextPart()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			var maxErr *http.MaxBytesError
			if errors.As(err, &maxErr) {
				writeError(w, http.StatusRequestEntityTooLarge, "file is too large")
			} else {
				writeError(w, http.StatusBadRequest, "invalid upload")
			}
			return
		}
		if part.FileName() == "" {
			_ = part.Close()
			continue
		}
		if len(uploaded) >= 50 {
			_ = part.Close()
			writeError(w, http.StatusBadRequest, "too many files in one request")
			return
		}
		up, err := s.store.BeginUpload(dir, part.FileName())
		if err != nil {
			_ = part.Close()
			s.handleStoreError(w, err)
			return
		}
		_, copyErr := io.CopyBuffer(up, part, buf)
		_ = part.Close()
		if copyErr != nil {
			up.Abort()
			var maxErr *http.MaxBytesError
			if errors.As(copyErr, &maxErr) {
				writeError(w, http.StatusRequestEntityTooLarge, "file is too large")
				return
			}
			s.handleStoreError(w, copyErr)
			return
		}
		entry, err := up.Commit()
		if err != nil {
			s.handleStoreError(w, err)
			return
		}
		uploaded = append(uploaded, entry)
	}
	if len(uploaded) == 0 {
		writeError(w, http.StatusBadRequest, "no files supplied")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"uploaded": uploaded})
}

func (s *Server) handleDownload(w http.ResponseWriter, r *http.Request) {
	rel := r.URL.Query().Get("path")
	f, info, err := s.store.Open(rel)
	if err != nil {
		s.handleStoreError(w, err)
		return
	}
	defer func() { _ = f.Close() }()

	contentType, err := detectContentType(f, info.Name())
	if err != nil {
		s.handleStoreError(w, err)
		return
	}
	disposition := "attachment"
	if r.URL.Query().Get("inline") == "1" && inlineAllowed(contentType) {
		disposition = "inline"
	}
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Content-Disposition", contentDisposition(disposition, info.Name()))
	w.Header().Set("Cache-Control", "private, max-age=0, must-revalidate")
	w.Header().Set("Content-Security-Policy", "frame-ancestors 'self'")
	http.ServeContent(w, r, info.Name(), info.ModTime(), f)
}

func (s *Server) handleZip(w http.ResponseWriter, r *http.Request) {
	rel, err := store.CleanRel(r.URL.Query().Get("path"))
	if err != nil {
		s.handleStoreError(w, err)
		return
	}
	entry, err := s.store.Stat(rel)
	if err != nil {
		s.handleStoreError(w, err)
		return
	}
	name := "files.zip"
	if entry.Name != "" && entry.Name != "." {
		name = entry.Name + ".zip"
	}
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", contentDisposition("attachment", name))
	w.Header().Set("Cache-Control", "no-store")
	zw := zip.NewWriter(w)
	if err := s.store.WriteZip(rel, zw); err != nil {
		s.logger.Error("zip stream failed", "path", rel, "error", err)
	}
	if err := zw.Close(); err != nil {
		s.logger.Error("zip close failed", "path", rel, "error", err)
	}
}

func (s *Server) handleMkdir(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Path string `json:"path"`
		Name string `json:"name"`
	}
	if err := readJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request")
		return
	}
	entry, err := s.store.Mkdir(req.Path, req.Name)
	if err != nil {
		s.handleStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, entry)
}

func (s *Server) handleRename(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Path string `json:"path"`
		Name string `json:"name"`
	}
	if err := readJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request")
		return
	}
	entry, err := s.store.Rename(req.Path, req.Name)
	if err != nil {
		s.handleStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, entry)
}

func (s *Server) handleMove(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Path string `json:"path"`
		Dest string `json:"dest"`
	}
	if err := readJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request")
		return
	}
	entry, err := s.store.Move(req.Path, req.Dest)
	if err != nil {
		s.handleStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, entry)
}

func (s *Server) handleDelete(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Paths []string `json:"paths"`
	}
	if err := readJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request")
		return
	}
	if len(req.Paths) == 0 || len(req.Paths) > 100 {
		writeError(w, http.StatusBadRequest, "invalid path count")
		return
	}
	deleted := 0
	failed := make([]string, 0)
	for _, p := range req.Paths {
		if _, err := s.store.Delete(p); err != nil {
			failed = append(failed, p)
			continue
		}
		deleted++
	}
	writeJSON(w, http.StatusOK, map[string]any{"deleted": deleted, "failed": failed})
}

func detectContentType(f *os.File, name string) (string, error) {
	ext := strings.ToLower(path.Ext(name))
	switch ext {
	case ".md", ".markdown", ".log", ".conf", ".ini", ".toml", ".yaml", ".yml",
		".json", ".csv", ".go", ".js", ".ts", ".tsx", ".jsx", ".css", ".sh",
		".rs", ".c", ".h", ".cpp", ".hpp", ".py", ".java", ".scala", ".nix":
		return "text/plain; charset=utf-8", nil
	}
	if ct := mime.TypeByExtension(ext); ct != "" {
		return ct, nil
	}
	buf := make([]byte, 512)
	n, err := f.Read(buf)
	if err != nil && !errors.Is(err, io.EOF) {
		return "", err
	}
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return "", err
	}
	return http.DetectContentType(buf[:n]), nil
}

func inlineAllowed(contentType string) bool {
	base := strings.ToLower(strings.TrimSpace(strings.Split(contentType, ";")[0]))
	switch base {
	case "image/jpeg", "image/png", "image/gif", "image/webp", "image/avif",
		"image/bmp", "video/mp4", "video/webm", "video/quicktime",
		"audio/mpeg", "audio/ogg", "audio/wav", "audio/flac", "audio/mp4",
		"application/pdf", "text/plain":
		return true
	default:
		return false
	}
}

func contentDisposition(disposition, name string) string {
	return mime.FormatMediaType(disposition, map[string]string{"filename": name})
}
