package app

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
)

// A file server behind a private CA answers the health check only when the
// connection skips verification, the same setting the registry uses.
func TestFileServerServingHonoursPrivateCA(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/__dufs__/health" {
			http.NotFound(w, r)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()
	base, err := url.Parse(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if fileServerServing(ctx, base, false) {
		t.Error("serving with verification on and a self-signed certificate")
	}
	if !fileServerServing(ctx, base, true) {
		t.Error("not serving with verification skipped")
	}
}
