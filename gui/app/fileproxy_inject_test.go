package app

import (
	"compress/gzip"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// Dufs pages get the script wherever they are, including a folder reached
// through a breadcrumb with no trailing slash. An uploaded HTML file does not
// load dufs assets, so it is passed through unchanged. The upstream compresses
// when asked: the proxy must not pass the client's Accept-Encoding on.
func TestFileProxyInjectsScriptIntoDufsPagesOnly(t *testing.T) {
	const dufsPage = `<html><head><link rel="stylesheet" href="/__dufs_v0.46.0__/index.css"></head><body>listing</body></html>`
	const uploaded = `<html><head></head><body>uploaded</body></html>`
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		page := dufsPage
		switch r.URL.Path {
		case "/data/a.html":
			page = uploaded
		case "/data/big":
			page = dufsPage + strings.Repeat(" ", maxScriptedPage)
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		if strings.Contains(r.Header.Get("Accept-Encoding"), "gzip") {
			w.Header().Set("Content-Encoding", "gzip")
			zw := gzip.NewWriter(w)
			_, _ = io.WriteString(zw, page)
			_ = zw.Close()
			return
		}
		_, _ = io.WriteString(w, page)
	}))
	defer up.Close()
	origin := proxyFor(t, up.URL)

	want := `<script src="` + filesScriptPath + `"></script></head>`
	for _, path := range []string{"/data/", "/data"} {
		req := mustRequest(t, http.MethodGet, origin+path, nil)
		req.Header.Set("Accept-Encoding", "gzip, br")
		body := mustGet(t, req, http.StatusOK)
		if !strings.Contains(body, want) {
			t.Errorf("GET %s: no script tag before </head>: %s", path, body)
		}
	}

	body := mustGet(t, mustRequest(t, http.MethodGet, origin+"/data/a.html", nil), http.StatusOK)
	if strings.Contains(body, filesScriptPath) {
		t.Errorf("uploaded HTML file got the script: %s", body)
	}

	// A page too large to rewrite is passed through whole.
	big := mustGet(t, mustRequest(t, http.MethodGet, origin+"/data/big", nil), http.StatusOK)
	if strings.Contains(big, filesScriptPath) || len(big) != len(dufsPage)+maxScriptedPage {
		t.Errorf("large page: got %d bytes, script=%v; want it unchanged", len(big), strings.Contains(big, filesScriptPath))
	}
}

func TestFileProxyServesFilesScript(t *testing.T) {
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer up.Close()
	origin := proxyFor(t, up.URL)

	resp, err := http.Get(origin + filesScriptPath)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK || !strings.HasPrefix(resp.Header.Get("Content-Type"), "text/javascript") {
		t.Fatalf("status %d type %q", resp.StatusCode, resp.Header.Get("Content-Type"))
	}
	if !strings.Contains(string(body), "MKCOL") {
		t.Errorf("served script does not create folders through MKCOL")
	}
}

// The dufs listing page carries the script, and the requests the script makes
// (MKCOL for a folder, PUT for a file) succeed from the proxy origin.
func TestFileProxyCreatesFromListingWithDufs(t *testing.T) {
	origin, _ := startDufs(t)

	// A folder reached through a breadcrumb has no trailing slash and still gets the script.
	for _, path := range []string{"/data/", "/data"} {
		listing := mustGet(t, browserRequest(t, origin+path), http.StatusOK)
		if !strings.Contains(listing, filesScriptPath) {
			t.Errorf("GET %s: dufs listing does not load the script", path)
		}
	}

	mkcol := mustRequest(t, "MKCOL", origin+"/data/made-in-test", nil)
	mkcol.Header.Set("Origin", origin)
	mustGet(t, mkcol, http.StatusCreated)

	put := mustRequest(t, http.MethodPut, origin+"/data/made-in-test/new.txt", strings.NewReader(""))
	put.Header.Set("Origin", origin)
	mustGet(t, put, http.StatusCreated)
}

// browserRequest asks for the page the way a browser does, so dufs answers
// with its HTML listing rather than JSON or a bare directory.
func browserRequest(t *testing.T, u string) *http.Request {
	t.Helper()
	req := mustRequest(t, http.MethodGet, u, nil)
	req.Header.Set("Accept", "text/html")
	req.Header.Set("User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 14_0) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.0 Safari/605.1.15")
	return req
}
