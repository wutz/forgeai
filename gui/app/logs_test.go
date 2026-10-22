package app

import (
	"strings"
	"testing"
)

// TestReadTailKeepsNewestOutput checks a log longer than the limit keeps its
// end, not its start, and that the line cut at the front is dropped.
func TestReadTailKeepsNewestOutput(t *testing.T) {
	var b strings.Builder
	for i := range 2000 {
		b.WriteString("line " + strings.Repeat("x", 50) + " ")
		b.WriteString(string(rune('a' + i%26)))
		b.WriteString("\n")
	}
	b.WriteString("crash: last line\n")
	got, err := readTail(strings.NewReader(b.String()), 4096)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) > 4096 {
		t.Fatalf("returned %d bytes, want at most 4096", len(got))
	}
	if !strings.HasSuffix(got, "crash: last line\n") {
		t.Fatalf("newest line missing from tail: %q", got[len(got)-40:])
	}
	if !strings.HasPrefix(got, "line ") {
		t.Fatalf("tail should start on a whole line, got %q", got[:20])
	}
}

// TestReadTailShortLogUnchanged checks a log within the limit is returned as
// it was written, first line included.
func TestReadTailShortLogUnchanged(t *testing.T) {
	const in = "first\nsecond\n"
	got, err := readTail(strings.NewReader(in), 4096)
	if err != nil {
		t.Fatal(err)
	}
	if got != in {
		t.Fatalf("got %q, want %q", got, in)
	}
}
