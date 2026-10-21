package app

import (
	"bytes"
	_ "embed"
	"io"
	"log"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
)

// filesScriptPath serves filesScript to the directory listing. The name cannot
// clash with a volume: Kubernetes names cannot start with an underscore.
const filesScriptPath = "/_forgeai/files.js"

// dufsAssetPrefix is the path every dufs page loads its scripts and styles from.
const dufsAssetPrefix = "/__dufs_"

// maxScriptedPage bounds the pages read into memory for the script tag.
const maxScriptedPage = 1 << 20

//go:embed files_inject.js
var filesScript []byte

// fileTarget is the file server the proxy forwards to, with the basic-auth
// credentials that the browser never sees.
type fileTarget struct {
	base *url.URL
	user string
	pass string
}

// fileProxy is a reverse proxy on the loopback interface. The file browser
// (dufs) asks for basic auth, and the webview cannot pass credentials to an
// embedded frame. The frame loads the proxy instead, and the proxy adds the
// Authorization header on the way to the file server.
//
// The listener is random-port and loopback only. Requests are refused unless
// they carry the proxy's own Host, and any request with an Origin other than
// the proxy's own is refused too, so a web page in another tab cannot use the
// proxy. CORS headers from the file server are dropped for the same reason.
type fileProxy struct {
	mu     sync.RWMutex
	origin string // http://127.0.0.1:<port>, set once the listener is up
	target *fileTarget
}

// Origin returns the proxy's origin, starting the listener on first use.
func (p *fileProxy) Origin() (string, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.origin != "" {
		return p.origin, nil
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return "", err
	}
	p.origin = "http://" + ln.Addr().String()
	srv := &http.Server{Handler: p, ReadHeaderTimeout: 10 * time.Second}
	go func() {
		if err := srv.Serve(ln); err != nil && err != http.ErrServerClosed {
			log.Printf("file proxy stopped: %v", err)
		}
	}()
	return p.origin, nil
}

// SetTarget points the proxy at a file server, or at nothing (nil) when the
// server is not ready.
func (p *fileProxy) SetTarget(t *fileTarget) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.target = t
}

func (p *fileProxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	p.mu.RLock()
	origin, t := p.origin, p.target
	p.mu.RUnlock()

	if r.Host != strings.TrimPrefix(origin, "http://") {
		http.Error(w, "bad host", http.StatusForbidden)
		return
	}
	if o := r.Header.Get("Origin"); o != "" && o != origin {
		http.Error(w, "cross-origin request refused", http.StatusForbidden)
		return
	}
	if t == nil {
		http.Error(w, "file server is not running", http.StatusServiceUnavailable)
		return
	}
	if r.URL.Path == filesScriptPath {
		w.Header().Set("Content-Type", "text/javascript; charset=utf-8")
		_, _ = w.Write(filesScript)
		return
	}
	// Pages are rewritten below, so they must arrive uncompressed.
	rp := &httputil.ReverseProxy{
		Rewrite: func(pr *httputil.ProxyRequest) {
			pr.SetURL(t.base)
			pr.Out.SetBasicAuth(t.user, t.pass)
			pr.Out.Header.Del("Accept-Encoding")
		},
		// Stream uploads and downloads instead of buffering whole files.
		FlushInterval: -1,
		ModifyResponse: func(resp *http.Response) error {
			for k := range resp.Header {
				if strings.HasPrefix(http.CanonicalHeaderKey(k), "Access-Control-") {
					resp.Header.Del(k)
				}
			}
			return injectFilesScript(resp)
		},
	}
	rp.ServeHTTP(w, r)
}

// injectFilesScript adds the script tag to a dufs page, just before </head> so
// it runs before dufs' own script. Pages are recognised by the dufs asset path
// they load, not by their URL: a folder reached through a breadcrumb has no
// trailing slash but is still a listing. An uploaded HTML file does not load
// dufs assets and is passed through unchanged. Dufs pages are a few KB, so a
// page over maxScriptedPage is passed through without the script.
func injectFilesScript(resp *http.Response) error {
	if resp.StatusCode != http.StatusOK || !strings.HasPrefix(resp.Header.Get("Content-Type"), "text/html") {
		return nil
	}
	// Read one byte past the limit to tell a large page from a small one. The
	// length is not used: a decompressed response has none.
	head, err := io.ReadAll(io.LimitReader(resp.Body, maxScriptedPage+1))
	if err != nil {
		_ = resp.Body.Close()
		return err
	}
	if len(head) > maxScriptedPage {
		resp.Body = readCloser{io.MultiReader(bytes.NewReader(head), resp.Body), resp.Body}
		return nil
	}
	_ = resp.Body.Close()
	body := head
	if bytes.Contains(body, []byte(dufsAssetPrefix)) {
		tag := []byte(`<script src="` + filesScriptPath + `"></script>`)
		if i := bytes.Index(body, []byte("</head>")); i >= 0 {
			body = slices.Concat(body[:i], tag, body[i:])
		} else {
			body = slices.Concat(tag, body)
		}
	}
	resp.Body = io.NopCloser(bytes.NewReader(body))
	resp.ContentLength = int64(len(body))
	resp.Header.Set("Content-Length", strconv.Itoa(len(body)))
	return nil
}

// readCloser reads from r and closes the upstream body.
type readCloser struct {
	io.Reader
	io.Closer
}
