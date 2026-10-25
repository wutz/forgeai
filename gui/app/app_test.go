package app

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"

	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

// TestNewSetsControllerRuntimeLogger checks that controller-runtime logs reach
// the application's slog handler. Without a logger it prints a stack trace and
// drops its logs.
func TestNewSetsControllerRuntimeLogger(t *testing.T) {
	var buf bytes.Buffer
	prev := slog.Default()
	t.Cleanup(func() { slog.SetDefault(prev) })
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, nil)))

	New()
	logf.Log.Info("Probe logged")

	if !strings.Contains(buf.String(), `msg="Probe logged"`) {
		t.Fatalf("controller-runtime log did not reach slog: %q", buf.String())
	}
}
