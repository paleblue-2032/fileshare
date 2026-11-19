package web

import (
	"archive/zip"
	"fmt"
	"html/template"
	"net/http"
	"net/url"
	"path"
	"strings"
	"time"

	"github.com/paleblue-2032/fileshare/internal/store"
)

type shareCrumb struct {
	Name string
	Href string
}

type shareRow struct {
	Name     string
	Href     string
	IsDir    bool
	Kind     string
	Size     string
	Modified string
}

type sharePageData struct {
	Title        string
	Entry        store.Entry
	Rows         []shareRow
	Breadcrumbs  []shareCrumb
	Expires      string
	CanZip       bool
	ZipHref      string
	DownloadHref string
	SizeText     string
	Modified     string
}

var shareTemplate = template.Must(template.New("share").Funcs(template.FuncMap{
	"humanSize": humanSize,
}).Parse(sharePageHTML))

type shareAPIItem struct {
	store.ShareItem
	URL string `json:"url"`
}

func (s *Server) handleSharesList(w http.ResponseWriter, r *http.Request) {
	items := s.store.ListShares()
	out := make([]shareAPIItem, 0, len(items))
	for _, item := range items {
		out = append(out, shareAPIItem{ShareItem: item, URL: s.shareURL(r, item.Token)})
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": out})
}

func (s *Server) handleShareCreate(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Path           string `json:"path"`
		ExpiresInHours int    `json:"expiresInHours"`
	}
	if err := readJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request")
		return
	}
	if req.ExpiresInHours < 0 || req.ExpiresInHours > 24*365 {
		writeError(w, http.StatusBadRequest, "invalid expiry")
		return
	}
	ttl := time.Duration(req.ExpiresInHours) * time.Hour
	item, err := s.store.CreateShare(req.Path, ttl)
	if err != nil {
		s.handleStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{
		"share": item,
		"url":   s.shareURL(r, item.Token),
	})
}

func (s *Server) handleShareRevoke(w http.ResponseWriter, r *http.Request) {
	token := r.PathValue("token")
	if token == "" {
		writeError(w, http.StatusBadRequest, "missing token")
		return
	}
	if err := s.store.RevokeShare(token); err != nil {
		s.handleStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) handleSharePage(w http.ResponseWriter, r *http.Request) {
	token := r.PathValue("token")
	item, err := s.store.Share(token)
	if err != nil {
		shareNotFound(w)
		return
	}
	sub, err := store.CleanRel(r.URL.Query().Get("p"))
	if err != nil {
		shareNotFound(w)
		return
	}
	rel, err := s.store.ResolveShareSub(item, sub)
	if err != nil {
		shareNotFound(w)
		return
	}
	entry, err := s.store.Stat(rel)
	if err != nil {
		shareNotFound(w)
		return
	}

	data := sharePageData{
		Title:    entry.Name,
		Entry:    entry,
		Expires:  shareExpiry(item),
		SizeText: humanSize(entry.Size),
		Modified: entry.ModTime.Format("Jan 2, 2006 15:04"),
	}
	if entry.IsDir && entry.Name == "" {
		data.Title = "Shared files"
	}
	data.Breadcrumbs = shareBreadcrumbs(token, sub)
	if entry.IsDir {
		entries, err := s.store.List(rel)
		if err != nil {
			shareNotFound(w)
			return
		}
		data.Rows = make([]shareRow, 0, len(entries))
		for _, e := range entries {
			child := strings.TrimPrefix(path.Join(sub, e.Name), "/")
			q := url.Values{"p": {child}}.Encode()
			href := "/s/" + token + "?" + q
			if !e.IsDir {
				href = "/s/" + token + "/download?" + q
			}
			size := "—"
			if !e.IsDir {
				size = humanSize(e.Size)
			}
			data.Rows = append(data.Rows, shareRow{
				Name:     e.Name,
				Href:     href,
				IsDir:    e.IsDir,
				Kind:     e.Kind,
				Size:     size,
				Modified: e.ModTime.Format("Jan 2, 2006 15:04"),
			})
		}
		q := url.Values{"p": {sub}, "zip": {"1"}}.Encode()
		data.CanZip = true
		data.ZipHref = "/s/" + token + "/download?" + q
	} else {
		q := url.Values{"p": {sub}}.Encode()
		data.DownloadHref = "/s/" + token + "/download?" + q
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Robots-Tag", "noindex")
	if err := shareTemplate.Execute(w, data); err != nil {
		s.logger.Error("render share page", "error", err)
	}
}

func (s *Server) handleShareDownload(w http.ResponseWriter, r *http.Request) {
	token := r.PathValue("token")
	item, err := s.store.Share(token)
	if err != nil {
		shareNotFound(w)
		return
	}
	sub, err := store.CleanRel(r.URL.Query().Get("p"))
	if err != nil {
		shareNotFound(w)
		return
	}
	rel, err := s.store.ResolveShareSub(item, sub)
	if err != nil {
		shareNotFound(w)
		return
	}
	entry, err := s.store.Stat(rel)
	if err != nil {
		shareNotFound(w)
		return
	}
	if entry.IsDir {
		if r.URL.Query().Get("zip") != "1" {
			http.Error(w, "Use zip=1 to download a folder.", http.StatusBadRequest)
			return
		}
		name := entry.Name + ".zip"
		if entry.Name == "" {
			name = "files.zip"
		}
		w.Header().Set("Content-Type", "application/zip")
		w.Header().Set("Content-Disposition", contentDisposition("attachment", name))
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Robots-Tag", "noindex")
		zw := zip.NewWriter(w)
		if err := s.store.WriteZip(rel, zw); err != nil {
			s.logger.Error("share zip failed", "error", err)
		}
		_ = zw.Close()
		return
	}

	f, info, err := s.store.Open(rel)
	if err != nil {
		shareNotFound(w)
		return
	}
	defer func() { _ = f.Close() }()
	contentType, err := detectContentType(f, info.Name())
	if err != nil {
		shareNotFound(w)
		return
	}
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Content-Disposition", contentDisposition("attachment", info.Name()))
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Robots-Tag", "noindex")
	w.Header().Set("Content-Security-Policy", "frame-ancestors 'self'")
	http.ServeContent(w, r, info.Name(), info.ModTime(), f)
}

func shareNotFound(w http.ResponseWriter) {
	http.Error(w, "This share link is invalid or has expired.", http.StatusNotFound)
}

func shareExpiry(item store.ShareItem) string {
	if item.ExpiresAt == nil {
		return ""
	}
	return item.ExpiresAt.Local().Format("Jan 2, 2006 15:04")
}

func shareBreadcrumbs(token, sub string) []shareCrumb {
	crumbs := []shareCrumb{{Name: "Shared", Href: "/s/" + token}}
	if sub == "" {
		return crumbs
	}
	parts := strings.Split(sub, "/")
	for i, part := range parts {
		prefix := strings.Join(parts[:i+1], "/")
		crumbs = append(crumbs, shareCrumb{
			Name: part,
			Href: "/s/" + token + "?" + url.Values{"p": {prefix}}.Encode(),
		})
	}
	return crumbs
}

func humanSize(n int64) string {
	if n < 0 {
		return "—"
	}
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for m := n / unit; m >= unit; m /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(n)/float64(div), "KMGTPE"[exp])
}

const sharePageHTML = `<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<meta name="robots" content="noindex">
<title>{{.Title}} · Fileshare</title>
<style>
:root{color-scheme:dark;--bg:#0b0d12;--surface:#141821;--border:#242b3a;--text:#e8ecf4;--muted:#98a3b8;--accent:#7c9cff;--accent2:#8b5cf6}
*{box-sizing:border-box}
body{margin:0;background:radial-gradient(1200px 600px at 80% -10%,rgba(139,92,246,.16),transparent),radial-gradient(900px 500px at 0% 0%,rgba(124,156,255,.12),transparent),var(--bg);color:var(--text);font:15px/1.55 system-ui,-apple-system,"Segoe UI",Roboto,"Helvetica Neue",Arial,sans-serif;min-height:100vh}
.wrap{max-width:900px;margin:0 auto;padding:32px 20px 48px}
.top{display:flex;align-items:center;justify-content:space-between;margin-bottom:22px}
.brand{font-weight:700;letter-spacing:.2px;font-size:18px}
.brand span{color:var(--accent)}
.badge{border:1px solid var(--border);color:var(--muted);font-size:12px;padding:4px 10px;border-radius:999px;background:rgba(20,24,33,.7)}
.card{background:linear-gradient(180deg,rgba(255,255,255,.03),transparent),var(--surface);border:1px solid var(--border);border-radius:16px;padding:24px;box-shadow:0 20px 60px rgba(0,0,0,.35)}
h1{font-size:24px;margin:0 0 4px;word-break:break-word}
.muted{color:var(--muted);margin:0}
.crumbs{display:flex;flex-wrap:wrap;gap:8px;align-items:center;margin:18px 0 8px;font-size:13px}
.crumbs a{color:var(--accent);text-decoration:none}
.crumbs a:hover{text-decoration:underline}
.sep{color:#46506a}
.actions{display:flex;gap:10px;margin:16px 0}
.btn{display:inline-flex;align-items:center;justify-content:center;min-height:42px;padding:0 16px;border-radius:10px;border:1px solid var(--border);background:#1b2130;color:var(--text);text-decoration:none;font-weight:600;font-size:14px}
.btn.primary{background:linear-gradient(135deg,var(--accent),var(--accent2));border-color:transparent;color:white}
.btn:hover{filter:brightness(1.08)}
.files{list-style:none;margin:12px 0 0;padding:0;display:flex;flex-direction:column;gap:2px}
.files li{border-top:1px solid rgba(36,43,58,.65)}
.files li:first-child{border-top:0}
.file{display:grid;grid-template-columns:42px minmax(0,1fr) auto;gap:12px;align-items:center;padding:12px 8px;border-radius:10px;color:var(--text);text-decoration:none}
.file:hover{background:rgba(124,156,255,.08)}
.ico{display:inline-flex;align-items:center;justify-content:center;width:42px;height:30px;border-radius:8px;font-size:10px;font-weight:800;letter-spacing:.06em;color:#cdd6f4;background:#222a3b;border:1px solid #303a4f}
.ico.folder{background:rgba(124,156,255,.14);border-color:rgba(124,156,255,.35);color:#aebfff}
.ico.image{background:rgba(62,207,142,.12);border-color:rgba(62,207,142,.3);color:#7de3b6}
.ico.video{background:rgba(244,107,107,.12);border-color:rgba(244,107,107,.3);color:#ff9c9c}
.ico.audio{background:rgba(139,92,246,.14);border-color:rgba(139,92,246,.35);color:#c6b2ff}
.ico.pdf{background:rgba(255,159,67,.12);border-color:rgba(255,159,67,.32);color:#ffc38a}
.ico.text,.ico.file,.ico.archive{background:#222a3b;border-color:#303a4f}
.name{font-weight:600;overflow:hidden;text-overflow:ellipsis;white-space:nowrap}
.meta{font-size:12px;color:var(--muted);white-space:nowrap}
.filehero{display:flex;align-items:center;gap:14px;margin:18px 0}
.filehero .ico{width:56px;height:44px;font-size:11px}
.filehero .name{font-size:18px}
footer{margin-top:18px;font-size:12px;color:var(--muted);text-align:center}
@media(max-width:560px){.card{padding:18px}.wrap{padding:20px 14px 36px}.file{grid-template-columns:38px minmax(0,1fr)}.meta{grid-column:2;font-size:11px}.filehero .name{font-size:16px}}
</style>
</head>
<body>
<main class="wrap">
  <div class="top">
    <div class="brand">File<span>share</span></div>
    <div class="badge">Read-only share</div>
  </div>
  <section class="card">
    <h1>{{.Title}}</h1>
    <p class="muted">{{if .Entry.IsDir}}Folder{{else}}{{.Entry.Kind}}{{end}}{{if .Expires}} · Expires {{.Expires}}{{end}}</p>
    <nav class="crumbs" aria-label="Breadcrumb">
      {{range .Breadcrumbs}}<a href="{{.Href}}">{{.Name}}</a><span class="sep">/</span>{{end}}
    </nav>
    {{if .Entry.IsDir}}
      <div class="actions">
        {{if .CanZip}}<a class="btn primary" href="{{.ZipHref}}">Download folder (.zip)</a>{{end}}
      </div>
      {{if .Rows}}
      <ul class="files">
        {{range .Rows}}
        <li>
          <a class="file" href="{{.Href}}">
            <span class="ico {{.Kind}}">{{if .IsDir}}DIR{{else}}FILE{{end}}</span>
            <span class="name">{{.Name}}</span>
            <span class="meta">{{.Size}} · {{.Modified}}</span>
          </a>
        </li>
        {{end}}
      </ul>
      {{else}}<p class="muted" style="margin-top:18px">This folder is empty.</p>{{end}}
    {{else}}
      <div class="filehero">
        <span class="ico {{.Entry.Kind}}">FILE</span>
        <div>
          <div class="name">{{.Entry.Name}}</div>
          <div class="meta">{{.SizeText}} · {{.Modified}}</div>
        </div>
      </div>
      <div class="actions"><a class="btn primary" href="{{.DownloadHref}}">Download</a></div>
    {{end}}
  </section>
  <footer>Read-only link · Powered by Fileshare</footer>
</main>
</body>
</html>`
