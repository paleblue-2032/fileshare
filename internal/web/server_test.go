package web

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/paleblue-2032/fileshare/internal/auth"
	"github.com/paleblue-2032/fileshare/internal/config"
	"github.com/paleblue-2032/fileshare/internal/store"
)

const testPassword = "test-password"

type sessionResponse struct {
	Authenticated bool  `json:"authenticated"`
	QuotaBytes    int64 `json:"quotaBytes"`
}

type listResponse struct {
	Path    string        `json:"path"`
	Entries []store.Entry `json:"entries"`
}

type shareCreateResponse struct {
	Share store.ShareItem `json:"share"`
	URL   string          `json:"url"`
}

type trashResponse struct {
	Items []store.TrashItem `json:"items"`
}

func newTestServer(t *testing.T) (*httptest.Server, *http.Client, *store.Store) {
	t.Helper()
	hash, err := auth.HashPassword(testPassword)
	if err != nil {
		t.Fatal(err)
	}
	st, err := store.Open(t.TempDir(), 1<<30, 0)
	if err != nil {
		t.Fatal(err)
	}
	cfg := config.Config{
		Addr:           "127.0.0.1:0",
		DataDir:        st.DataDir(),
		PasswordHash:   hash,
		QuotaBytes:     1 << 30,
		MaxUploadBytes: 1 << 20,
		MinFreeBytes:   0,
		RetentionDays:  30,
		TrashDays:      30,
		SweepInterval:  time.Hour,
		WebDAV:         true,
		SessionTTL:     time.Hour,
	}
	app, err := New(cfg, st, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(app.Handler())
	jar, _ := cookiejar.New(nil)
	client := &http.Client{Jar: jar}
	t.Cleanup(func() {
		ts.Close()
		_ = st.Close()
	})
	return ts, client, st
}

func jsonRequest(t *testing.T, method, endpoint string, body any) *http.Request {
	t.Helper()
	var reader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		reader = bytes.NewReader(data)
	}
	req, err := http.NewRequest(method, endpoint, reader)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("X-Fileshare", "1")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	return req
}

func doJSON(t *testing.T, client *http.Client, method, endpoint string, body any) *http.Response {
	t.Helper()
	resp, err := client.Do(jsonRequest(t, method, endpoint, body))
	if err != nil {
		t.Fatal(err)
	}
	return resp
}

func login(t *testing.T, ts *httptest.Server, client *http.Client) {
	t.Helper()
	resp := doJSON(t, client, http.MethodPost, ts.URL+"/api/login", map[string]string{"password": testPassword})
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("login status = %d: %s", resp.StatusCode, body)
	}
}

func uploadFile(t *testing.T, ts *httptest.Server, client *http.Client, dir, name, content string) *http.Response {
	t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	part, err := mw.CreateFormFile("file", name)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.WriteString(part, content); err != nil {
		t.Fatal(err)
	}
	if err := mw.Close(); err != nil {
		t.Fatal(err)
	}
	params := url.Values{"path": {dir}}
	req, err := http.NewRequest(http.MethodPost, ts.URL+"/api/upload?"+params.Encode(), &buf)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.Header.Set("X-Fileshare", "1")
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	return resp
}

func decode[T any](t *testing.T, resp *http.Response) T {
	t.Helper()
	defer func() { _ = resp.Body.Close() }()
	var out T
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	return out
}

func TestLoginAndCSRF(t *testing.T) {
	ts, _, _ := newTestServer(t)
	jar, _ := cookiejar.New(nil)
	client := &http.Client{Jar: jar}
	noHeader, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/login", strings.NewReader(`{"password":"test-password"}`))
	noHeader.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(noHeader)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("missing CSRF header status = %d, want 403", resp.StatusCode)
	}

	login(t, ts, client)
	resp = doJSON(t, client, http.MethodGet, ts.URL+"/api/session", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("session status = %d", resp.StatusCode)
	}
	session := decode[sessionResponse](t, resp)
	if !session.Authenticated || session.QuotaBytes == 0 {
		t.Fatalf("unexpected session: %+v", session)
	}
}

func TestUploadListDownloadRangeAndHTMLSafety(t *testing.T) {
	ts, client, _ := newTestServer(t)
	login(t, ts, client)

	resp := uploadFile(t, ts, client, "", "hello.txt", "hello world")
	if resp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("upload status = %d: %s", resp.StatusCode, body)
	}
	_ = resp.Body.Close()

	params := url.Values{"path": {""}}
	resp = doJSON(t, client, http.MethodGet, ts.URL+"/api/list?"+params.Encode(), nil)
	listing := decode[listResponse](t, resp)
	if len(listing.Entries) != 1 || listing.Entries[0].Name != "hello.txt" {
		t.Fatalf("listing = %+v", listing)
	}

	req := jsonRequest(t, http.MethodGet, ts.URL+"/api/download?path=hello.txt", nil)
	req.Header.Set("Range", "bytes=0-4")
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusPartialContent || string(body) != "hello" {
		t.Fatalf("range = %d %q", resp.StatusCode, body)
	}

	resp = uploadFile(t, ts, client, "", "x.html", "<script>alert(1)</script>")
	_ = resp.Body.Close()
	resp = doJSON(t, client, http.MethodGet, ts.URL+"/api/download?path=x.html&inline=1", nil)
	body, _ = io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if !strings.HasPrefix(resp.Header.Get("Content-Disposition"), "attachment") {
		t.Fatalf("HTML disposition = %q", resp.Header.Get("Content-Disposition"))
	}
	if !strings.Contains(resp.Header.Get("Content-Type"), "text/html") {
		t.Fatalf("HTML content type = %q", resp.Header.Get("Content-Type"))
	}
	if !strings.Contains(string(body), "script") {
		t.Fatalf("download body = %q", body)
	}
}

func TestPathTraversalAPI(t *testing.T) {
	ts, client, _ := newTestServer(t)
	login(t, ts, client)
	params := url.Values{"path": {"../../etc/passwd"}}
	resp := doJSON(t, client, http.MethodGet, ts.URL+"/api/list?"+params.Encode(), nil)
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("traversal status = %d, want 400", resp.StatusCode)
	}
}

func TestShareLifecycle(t *testing.T) {
	ts, client, _ := newTestServer(t)
	login(t, ts, client)
	resp := uploadFile(t, ts, client, "", "shared.txt", "shared data")
	_ = resp.Body.Close()

	resp = doJSON(t, client, http.MethodPost, ts.URL+"/api/shares", map[string]any{
		"path": "shared.txt", "expiresInHours": 1,
	})
	created := decode[shareCreateResponse](t, resp)
	if created.URL == "" || created.Share.Token == "" {
		t.Fatalf("share response = %+v", created)
	}

	public := &http.Client{}
	page, err := public.Get(created.URL)
	if err != nil {
		t.Fatal(err)
	}
	pageBody, _ := io.ReadAll(page.Body)
	_ = page.Body.Close()
	if page.StatusCode != http.StatusOK || !strings.Contains(string(pageBody), "shared.txt") {
		t.Fatalf("share page = %d %q", page.StatusCode, pageBody)
	}

	dl, err := public.Get(created.URL + "/download")
	if err != nil {
		t.Fatal(err)
	}
	dlBody, _ := io.ReadAll(dl.Body)
	_ = dl.Body.Close()
	if dl.StatusCode != http.StatusOK || string(dlBody) != "shared data" {
		t.Fatalf("share download = %d %q", dl.StatusCode, dlBody)
	}

	resp = doJSON(t, client, http.MethodDelete, ts.URL+"/api/shares/"+created.Share.Token, nil)
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("revoke status = %d", resp.StatusCode)
	}
	gone, err := public.Get(created.URL)
	if err != nil {
		t.Fatal(err)
	}
	_ = gone.Body.Close()
	if gone.StatusCode != http.StatusNotFound {
		t.Fatalf("revoked share status = %d, want 404", gone.StatusCode)
	}
}

func TestFolderShareZip(t *testing.T) {
	ts, client, _ := newTestServer(t)
	login(t, ts, client)
	resp := doJSON(t, client, http.MethodPost, ts.URL+"/api/mkdir", map[string]string{"path": "", "name": "folder"})
	_ = resp.Body.Close()
	_ = uploadFile(t, ts, client, "folder", "inner.txt", "inner data").Body.Close()

	resp = doJSON(t, client, http.MethodPost, ts.URL+"/api/shares", map[string]any{
		"path": "folder", "expiresInHours": 0,
	})
	created := decode[struct {
		URL string `json:"url"`
	}](t, resp)
	public := &http.Client{}
	page, err := public.Get(created.URL)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(page.Body)
	_ = page.Body.Close()
	if page.StatusCode != http.StatusOK || !strings.Contains(string(body), "inner.txt") {
		t.Fatalf("folder share page = %d %q", page.StatusCode, body)
	}
	zipResp, err := public.Get(created.URL + "/download?zip=1")
	if err != nil {
		t.Fatal(err)
	}
	zipBody, _ := io.ReadAll(zipResp.Body)
	_ = zipResp.Body.Close()
	if zipResp.StatusCode != http.StatusOK || len(zipBody) == 0 {
		t.Fatalf("folder zip = %d len=%d", zipResp.StatusCode, len(zipBody))
	}
	if ct := zipResp.Header.Get("Content-Type"); ct != "application/zip" {
		t.Fatalf("zip content type = %q", ct)
	}
}

func TestTrashAPI(t *testing.T) {
	ts, client, _ := newTestServer(t)
	login(t, ts, client)
	_ = uploadFile(t, ts, client, "", "gone.txt", "bye").Body.Close()

	resp := doJSON(t, client, http.MethodPost, ts.URL+"/api/delete", map[string]any{"paths": []string{"gone.txt"}})
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("delete status = %d", resp.StatusCode)
	}
	resp = doJSON(t, client, http.MethodGet, ts.URL+"/api/trash", nil)
	trash := decode[trashResponse](t, resp)
	if len(trash.Items) != 1 {
		t.Fatalf("trash items = %+v", trash.Items)
	}
	resp = doJSON(t, client, http.MethodPost, ts.URL+"/api/trash/restore", map[string]string{"id": trash.Items[0].ID})
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("restore status = %d", resp.StatusCode)
	}
	params := url.Values{"path": {""}}
	resp = doJSON(t, client, http.MethodGet, ts.URL+"/api/list?"+params.Encode(), nil)
	listing := decode[listResponse](t, resp)
	if len(listing.Entries) != 1 || listing.Entries[0].Name != "gone.txt" {
		t.Fatalf("listing after restore = %+v", listing)
	}
}

func TestSecureCookieBehindLoopbackProxy(t *testing.T) {
	ts, _, _ := newTestServer(t)
	client := &http.Client{}
	req := jsonRequest(t, http.MethodPost, ts.URL+"/api/login", map[string]string{"password": testPassword})
	req.Header.Set("X-Forwarded-Proto", "https")
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("login status = %d", resp.StatusCode)
	}
	found := false
	for _, cookie := range resp.Cookies() {
		if cookie.Name == auth.CookieName {
			found = true
			if !cookie.Secure {
				t.Fatal("session cookie is not Secure when the loopback proxy reports HTTPS")
			}
		}
	}
	if !found {
		t.Fatal("session cookie was not set")
	}
}

func TestRevokeAllSessions(t *testing.T) {
	ts, first, _ := newTestServer(t)
	login(t, ts, first)
	jar, _ := cookiejar.New(nil)
	second := &http.Client{Jar: jar}
	login(t, ts, second)

	resp := doJSON(t, first, http.MethodPost, ts.URL+"/api/sessions/revoke-all", map[string]any{})
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("revoke-all status = %d", resp.StatusCode)
	}

	resp = doJSON(t, second, http.MethodGet, ts.URL+"/api/session", nil)
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("second session after revoke-all = %d, want 401", resp.StatusCode)
	}
}

func TestWebDAVOptionsWithoutAuth(t *testing.T) {
	ts, _, _ := newTestServer(t)
	for _, path := range []string{"/dav", "/dav/"} {
		req, err := http.NewRequest(http.MethodOptions, ts.URL+path, nil)
		if err != nil {
			t.Fatal(err)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("OPTIONS %s status = %d, want 200", path, resp.StatusCode)
		}
		if !strings.Contains(resp.Header.Get("DAV"), "1") {
			t.Fatalf("OPTIONS %s DAV header = %q", path, resp.Header.Get("DAV"))
		}
	}

	req, err := http.NewRequest("PROPFIND", ts.URL+"/dav/", nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("unauthenticated PROPFIND status = %d, want 401", resp.StatusCode)
	}
	if resp.Header.Get("WWW-Authenticate") == "" {
		t.Fatal("missing WWW-Authenticate challenge")
	}

	req, err = http.NewRequest(http.MethodOptions, ts.URL+"/dav/secret", nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("OPTIONS on a child path status = %d, want 401", resp.StatusCode)
	}
}

func TestWebDAV(t *testing.T) {
	ts, client, _ := newTestServer(t)
	login(t, ts, client)
	req, _ := http.NewRequest("PROPFIND", ts.URL+"/dav/", nil)
	req.SetBasicAuth("fileshare", testPassword)
	req.Header.Set("Depth", "0")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusMultiStatus {
		t.Fatalf("PROPFIND status = %d", resp.StatusCode)
	}

	req, _ = http.NewRequest(http.MethodPut, ts.URL+"/dav/from-dav.txt", strings.NewReader("dav body"))
	req.SetBasicAuth("fileshare", testPassword)
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusNoContent {
		t.Fatalf("PUT status = %d", resp.StatusCode)
	}

	resp = doJSON(t, client, http.MethodGet, ts.URL+"/api/download?path=from-dav.txt", nil)
	body, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK || string(body) != "dav body" {
		t.Fatalf("DAV download = %d %q", resp.StatusCode, body)
	}

	req, _ = http.NewRequest(http.MethodDelete, ts.URL+"/dav/from-dav.txt", nil)
	req.SetBasicAuth("fileshare", testPassword)
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("DELETE status = %d", resp.StatusCode)
	}

	req, _ = http.NewRequest("PROPFIND", ts.URL+"/dav/", nil)
	req.SetBasicAuth("fileshare", "wrong-password")
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("wrong DAV password status = %d", resp.StatusCode)
	}
}
