// Command compat-api serves the scc-k8s-v2 compatible job API.
package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	// Timezone database for createTime formatting in minimal images.
	_ "time/tzdata"

	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	v1 "github.com/scclabs/forgeai/api/v1alpha1"
	"github.com/scclabs/forgeai/compat-api/internal/server"
	"github.com/scclabs/forgeai/pkg/jobs"
	"github.com/scclabs/forgeai/pkg/platform"
)

func env(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func main() {
	log := slog.New(slog.NewJSONHandler(os.Stdout, nil))

	scheme := runtime.NewScheme()
	if err := clientgoscheme.AddToScheme(scheme); err != nil {
		panic(err)
	}
	if err := v1.AddToScheme(scheme); err != nil {
		panic(err)
	}
	k8s, err := client.New(ctrl.GetConfigOrDie(), client.Options{Scheme: scheme})
	if err != nil {
		log.Error("create kubernetes client", "error", err)
		os.Exit(1)
	}

	keys := server.APIKeysFromEnv("APIKEY")
	if len(keys) == 0 {
		log.Error("no API keys configured: set APIKEY (or any APIKEY* env var)")
		os.Exit(1)
	}

	s := &server.Server{
		Jobs: &jobs.Client{
			K8s:             k8s,
			Namespace:       env("NAMESPACE", "pod"),
			ConfigNamespace: platform.Namespace(),
		},
		APIKeys: keys,
		Log:     log,
	}
	srv := &http.Server{
		Addr:              ":" + env("PORT", "8000"),
		Handler:           s.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	go func() {
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdown)
	}()

	log.Info("listening", "addr", srv.Addr, "namespace", s.Jobs.Namespace)
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Error("serve", "error", err)
		os.Exit(1)
	}
}
