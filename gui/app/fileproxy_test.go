package app

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	v1 "github.com/wutz/forgeai/api/v1alpha1"
	"github.com/wutz/forgeai/pkg/naming"
)

// proxyFor starts a proxy in front of target and returns its origin.
func proxyFor(t *testing.T, target, user, pass string) string {
	t.Helper()
	base, err := url.Parse(target)
	if err != nil {
		t.Fatal(err)
	}
	p := &fileProxy{}
	p.SetTarget(&fileTarget{base: base, user: user, pass: pass})
	origin, err := p.Origin()
	if err != nil {
		t.Fatal(err)
	}
	return origin
}

func TestFileProxyAddsCredentialsAndDropsCORS(t *testing.T) {
	seen := make(chan *http.Request, 1)
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen <- r
		w.Header().Set("Access-Control-Allow-Origin", "*")
		_, _ = io.WriteString(w, "listing")
	}))
	defer up.Close()
	origin := proxyFor(t, up.URL, "forgeai", "s3cret")

	resp, err := http.Get(origin + "/data/a.txt?json")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK || string(body) != "listing" {
		t.Fatalf("got %d %q, want 200 \"listing\"", resp.StatusCode, body)
	}
	if acao := resp.Header.Get("Access-Control-Allow-Origin"); acao != "" {
		t.Errorf("CORS header reached the browser: %q", acao)
	}

	got := <-seen
	want := "Basic " + base64.StdEncoding.EncodeToString([]byte("forgeai:s3cret"))
	if got.Header.Get("Authorization") != want {
		t.Errorf("upstream Authorization = %q, want %q", got.Header.Get("Authorization"), want)
	}
	if got.URL.Path != "/data/a.txt" || got.URL.RawQuery != "json" {
		t.Errorf("upstream path = %q?%q, want /data/a.txt?json", got.URL.Path, got.URL.RawQuery)
	}
}

func TestFileProxyRefusesForeignOriginAndHost(t *testing.T) {
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, "ok")
	}))
	defer up.Close()
	origin := proxyFor(t, up.URL, "forgeai", "s3cret")

	cases := []struct {
		name   string
		host   string
		origin string
		want   int
	}{
		{name: "own origin", origin: origin, want: http.StatusOK},
		{name: "no origin", want: http.StatusOK},
		{name: "page in another origin", origin: "http://evil.example", want: http.StatusForbidden},
		{name: "rebound host name", host: "evil.example", want: http.StatusForbidden},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			req, err := http.NewRequest(http.MethodGet, origin+"/", nil)
			if err != nil {
				t.Fatal(err)
			}
			if c.host != "" {
				req.Host = c.host
			}
			if c.origin != "" {
				req.Header.Set("Origin", c.origin)
			}
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = resp.Body.Close() }()
			if resp.StatusCode != c.want {
				t.Errorf("status = %d, want %d", resp.StatusCode, c.want)
			}
		})
	}
}

func TestFileProxyWithoutTargetIsUnavailable(t *testing.T) {
	p := &fileProxy{}
	origin, err := p.Origin()
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.Get(origin + "/")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503", resp.StatusCode)
	}
}

// Files must hand the webview a loopback address only: the password stays in
// the backend, and the address goes away when the server is not ready.
func TestFilesAddressIsLoopbackAndHidesPassword(t *testing.T) {
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if u, p, ok := r.BasicAuth(); !ok || u != "forgeai" || p != "s3cret" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		_, _ = io.WriteString(w, "listing")
	}))
	defer up.Close()

	fs := &v1.FileServer{
		ObjectMeta: metav1.ObjectMeta{Name: naming.FileServerName, Namespace: testNamespace},
		Status:     v1.FileServerStatus{Phase: v1.FileServerReady, URL: up.URL, Volumes: []string{"data"}},
	}
	sec := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: naming.FileServerSecretName, Namespace: testNamespace},
		Data: map[string][]byte{
			corev1.BasicAuthUsernameKey: []byte("forgeai"),
			corev1.BasicAuthPasswordKey: []byte("s3cret"),
		},
	}
	a := testApp(t, fs, sec)
	out, err := a.Files()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(out.Address, "http://127.0.0.1:") {
		t.Fatalf("address = %q, want a loopback URL", out.Address)
	}
	js, _ := json.Marshal(out)
	if strings.Contains(string(js), "s3cret") {
		t.Errorf("password reached the webview: %s", js)
	}
	resp, err := http.Get(out.Address + "data/")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK || string(body) != "listing" {
		t.Fatalf("through the address: %d %q, want 200 \"listing\"", resp.StatusCode, body)
	}

	// The server goes idle: the address is withdrawn and the proxy stops
	// forwarding to the old pod.
	cur := &v1.FileServer{}
	key := client.ObjectKey{Namespace: testNamespace, Name: naming.FileServerName}
	if err := a.conn.k8s.Get(context.Background(), key, cur); err != nil {
		t.Fatal(err)
	}
	cur.Status.Phase = v1.FileServerIdle
	// The fake client has no status subresource, so a plain update sets status.
	if err := a.conn.k8s.Update(context.Background(), cur); err != nil {
		t.Fatal(err)
	}
	out, err = a.Files()
	if err != nil {
		t.Fatal(err)
	}
	if out.Address != "" {
		t.Errorf("idle server still has address %q", out.Address)
	}
	resp, err = http.Get(appProxyOrigin(t, a) + "/data/")
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("idle proxy status = %d, want 503", resp.StatusCode)
	}
}

// appProxyOrigin returns the proxy origin of an app that already started it.
func appProxyOrigin(t *testing.T, a *App) string {
	t.Helper()
	o, err := a.files.Origin()
	if err != nil {
		t.Fatal(err)
	}
	return o
}

// The proxy in front of the real dufs binary the operator runs, with the
// same auth rules: the browser UI and its absolute asset paths, the JSON
// listing, uploads into a volume, and the read-only root all go through.
func TestFileProxyWithDufs(t *testing.T) {
	bin, err := exec.LookPath("dufs")
	if err != nil {
		t.Skip("dufs is not on PATH")
	}
	root := t.TempDir()
	if err := os.Mkdir(root+"/data", 0o755); err != nil {
		t.Fatal(err)
	}
	port := freePort(t)
	cmd := exec.Command(bin, root, "--bind", "127.0.0.1", "--port", strconv.Itoa(port),
		"--allow-all", "--enable-cors", "--auth", "forgeai:s3cret@/:ro,/data:rw")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cmd.Process.Kill(); _ = cmd.Wait() })
	upstream := fmt.Sprintf("http://127.0.0.1:%d", port)
	waitForHTTP(t, upstream+"/__dufs__/health")

	// Direct access needs the password; the proxy supplies it.
	if resp, err := http.Get(upstream + "/data/"); err != nil {
		t.Fatal(err)
	} else {
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusUnauthorized {
			t.Fatalf("direct status = %d, want 401", resp.StatusCode)
		}
	}
	origin := proxyFor(t, upstream, "forgeai", "s3cret")

	// The browser UI loads from the root with absolute asset paths.
	req, _ := http.NewRequest(http.MethodGet, origin+"/data/", nil)
	req.Header.Set("Accept", "text/html")
	req.Header.Set("User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 14_0) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.0 Safari/605.1.15")
	page := mustGet(t, req, http.StatusOK)
	asset := regexp.MustCompile(`/__dufs_[^"']*/index\.js`).FindString(page)
	if asset == "" {
		t.Fatalf("browser UI does not reference its script:\n%.300s", page)
	}
	mustGet(t, mustRequest(t, http.MethodGet, origin+asset, nil), http.StatusOK)

	mustGet(t, mustRequest(t, http.MethodGet, origin+"/data/?json", nil), http.StatusOK)

	// Uploads from the browser carry the proxy origin, and land in the volume.
	put := mustRequest(t, http.MethodPut, origin+"/data/new.txt", strings.NewReader("hi"))
	put.Header.Set("Origin", origin)
	mustGet(t, put, http.StatusCreated)
	if body := mustGet(t, mustRequest(t, http.MethodGet, origin+"/data/new.txt", nil), http.StatusOK); body != "hi" {
		t.Errorf("uploaded file reads %q, want \"hi\"", body)
	}

	// Only the volume is writable; the root stays read-only.
	mustGet(t, mustRequest(t, http.MethodPut, origin+"/root.txt", strings.NewReader("x")), http.StatusForbidden)
}

func freePort(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ln.Close() }()
	return ln.Addr().(*net.TCPAddr).Port
}

func waitForHTTP(t *testing.T, u string) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		resp, err := http.Get(u)
		if err == nil {
			_ = resp.Body.Close()
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("server at %s did not start: %v", u, err)
		}
		time.Sleep(100 * time.Millisecond)
	}
}

func mustRequest(t *testing.T, method, u string, body io.Reader) *http.Request {
	t.Helper()
	req, err := http.NewRequest(method, u, body)
	if err != nil {
		t.Fatal(err)
	}
	return req
}

// mustGet sends req and fails unless the status matches; it returns the body.
func mustGet(t *testing.T, req *http.Request, want int) string {
	t.Helper()
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != want {
		t.Fatalf("%s %s: status %d, want %d (%.200s)", req.Method, req.URL, resp.StatusCode, want, body)
	}
	return string(body)
}
