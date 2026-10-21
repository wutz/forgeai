package app

import (
	"log"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
	"sync"
	"time"
)

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
	rp := &httputil.ReverseProxy{
		Rewrite: func(pr *httputil.ProxyRequest) {
			pr.SetURL(t.base)
			pr.Out.SetBasicAuth(t.user, t.pass)
		},
		// Stream uploads and downloads instead of buffering whole files.
		FlushInterval: -1,
		ModifyResponse: func(resp *http.Response) error {
			for k := range resp.Header {
				if strings.HasPrefix(http.CanonicalHeaderKey(k), "Access-Control-") {
					resp.Header.Del(k)
				}
			}
			return nil
		},
	}
	rp.ServeHTTP(w, r)
}
