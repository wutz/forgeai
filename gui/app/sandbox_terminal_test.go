package app

import (
	"context"
	"reflect"
	"testing"

	"k8s.io/client-go/tools/remotecommand"
)

func TestCompleteLen(t *testing.T) {
	// "é" is two bytes, "中" three: a cut inside either must be held back.
	cases := []struct {
		in   string
		want int
	}{
		{"plain", 5},
		{"é", 2},
		{"é"[:1], 0},
		{"ok中"[:4], 2},
		{"ok中", 5},
		{"", 0},
	}
	for _, c := range cases {
		if got := completeLen([]byte(c.in)); got != c.want {
			t.Errorf("completeLen(%q) = %d, want %d", c.in, got, c.want)
		}
	}
}

func TestSizeQueueKeepsLatest(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	q := &sizeQueue{ctx: ctx, ch: make(chan remotecommand.TerminalSize, 1)}
	q.set(remotecommand.TerminalSize{Width: 80, Height: 24})
	q.set(remotecommand.TerminalSize{Width: 120, Height: 40})
	got := q.Next()
	if got == nil || got.Width != 120 || got.Height != 40 {
		t.Fatalf("Next() = %+v, want 120x40", got)
	}
	cancel()
	if q.Next() != nil {
		t.Fatal("Next() after cancel should return nil")
	}
}

func TestClampSize(t *testing.T) {
	if clampSize(0) != 1 || clampSize(-5) != 1 || clampSize(100) != 100 || clampSize(1<<20) != 65535 {
		t.Fatal("clampSize out of range")
	}
}

// TestEventWriterSequencesChunks checks that each emitted chunk gets the next
// sequence number, that a split UTF-8 character is held back until it is whole,
// and that the exit event follows the last chunk.
func TestEventWriterSequencesChunks(t *testing.T) {
	var got []sandboxOutput
	a := &App{emit: func(_ context.Context, _ string, data ...interface{}) {
		if out, ok := data[0].(sandboxOutput); ok {
			got = append(got, out)
		}
	}}
	w := &eventWriter{app: a, ctx: context.Background(), event: "sandbox-terminal:test"}

	for _, p := range []string{"a", "\xc3", "\xa9", "bc"} {
		if _, err := w.Write([]byte(p)); err != nil {
			t.Fatalf("Write(%q): %v", p, err)
		}
	}

	want := []sandboxOutput{{Seq: 1, Data: "a"}, {Seq: 2, Data: "é"}, {Seq: 3, Data: "bc"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("chunks = %+v, want %+v", got, want)
	}
	if next := w.nextSeq(); next != 4 {
		t.Fatalf("nextSeq() = %d, want 4", next)
	}
}
