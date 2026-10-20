package app

import (
	"context"
	"fmt"
	"io"
	"unicode/utf8"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	kubescheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/remotecommand"

	"github.com/wailsapp/wails/v2/pkg/runtime"

	"github.com/wutz/forgeai/pkg/sandbox"
)

// shellCommand starts the best shell the sandbox image has, with a TERM the
// GUI's terminal emulator understands.
var shellCommand = []string{"sh", "-c", `export TERM=xterm-256color; exec "$(command -v bash || command -v sh)" -i`}

// sandboxTerminal is one interactive shell in a sandbox container.
type sandboxTerminal struct {
	input  *io.PipeWriter
	sizes  *sizeQueue
	cancel context.CancelFunc
}

// sizeQueue feeds terminal resizes to the exec stream. Only the latest size
// matters, so a pending size is replaced rather than queued behind.
type sizeQueue struct {
	ctx context.Context
	ch  chan remotecommand.TerminalSize
}

// Next implements remotecommand.TerminalSizeQueue.
func (q *sizeQueue) Next() *remotecommand.TerminalSize {
	select {
	case s := <-q.ch:
		return &s
	case <-q.ctx.Done():
		return nil
	}
}

// set replaces the pending size; it never blocks the caller.
func (q *sizeQueue) set(s remotecommand.TerminalSize) {
	select {
	case q.ch <- s:
	default:
		select {
		case <-q.ch:
		default:
		}
		select {
		case q.ch <- s:
		default:
		}
	}
}

// eventWriter emits what the shell writes as a Wails event. A multi-byte
// UTF-8 character split across two reads is held back until it is complete.
type eventWriter struct {
	ctx     context.Context
	event   string
	pending []byte
}

// Write implements io.Writer.
func (w *eventWriter) Write(p []byte) (int, error) {
	buf := append(w.pending, p...)
	cut := completeLen(buf)
	chunk := string(buf[:cut])
	w.pending = append(w.pending[:0], buf[cut:]...)
	if chunk != "" {
		runtime.EventsEmit(w.ctx, w.event, chunk)
	}
	return len(p), nil
}

// completeLen returns the length of the prefix of b that ends on a rune
// boundary.
func completeLen(b []byte) int {
	for i := len(b) - 1; i >= 0 && i >= len(b)-utf8.UTFMax; i-- {
		if utf8.RuneStart(b[i]) {
			if !utf8.FullRune(b[i:]) {
				return i
			}
			return len(b)
		}
	}
	return len(b)
}

// OpenSandboxTerminal opens a shell in the sandbox container. The caller
// names the session id so it can subscribe to its events before the shell
// prints anything: output arrives as "sandbox-terminal:<id>" events and the
// end of the session as "sandbox-terminal:<id>:exit" (with the error text,
// empty on a clean exit).
func (a *App) OpenSandboxTerminal(name, id string, cols, rows int) error {
	if id == "" {
		return fmt.Errorf("terminal session id is required")
	}
	if _, busy := a.terminals.Load(id); busy {
		return fmt.Errorf("terminal session %s is already open", id)
	}
	c, err := a.current()
	if err != nil {
		return err
	}
	ctx, cancel := a.call()
	defer cancel()
	u, err := getSandbox(ctx, c, name)
	if err != nil {
		return err
	}
	sb := &sandbox.Sandbox{}
	if err := sandbox.FromUnstructured(u.Object, sb); err != nil {
		return err
	}
	pod := &corev1.Pod{}
	if err := c.k8s.Get(ctx, types.NamespacedName{Namespace: c.namespace(), Name: sandbox.PodName(sb)}, pod); err != nil {
		if apierrors.IsNotFound(err) {
			return fmt.Errorf("sandbox %s has no pod: it is stopped or still starting", name)
		}
		return err
	}
	if pod.Status.Phase != corev1.PodRunning {
		return fmt.Errorf("sandbox %s is %s: start it before opening a terminal", name, pod.Status.Phase)
	}

	req := c.core.CoreV1().RESTClient().Post().Resource("pods").Namespace(c.namespace()).
		Name(pod.Name).SubResource("exec").VersionedParams(&corev1.PodExecOptions{
		Container: sandbox.ContainerName,
		Command:   shellCommand,
		Stdin:     true,
		Stdout:    true,
		TTY:       true,
	}, kubescheme.ParameterCodec)
	exec, err := remotecommand.NewSPDYExecutor(c.rest, "POST", req.URL())
	if err != nil {
		return err
	}

	// The session outlives this call: it is bound to the app, not the
	// request timeout.
	sctx, scancel := context.WithCancel(a.ctx)
	pr, pw := io.Pipe()
	t := &sandboxTerminal{
		input:  pw,
		sizes:  &sizeQueue{ctx: sctx, ch: make(chan remotecommand.TerminalSize, 1)},
		cancel: scancel,
	}
	t.sizes.set(remotecommand.TerminalSize{Width: clampSize(cols), Height: clampSize(rows)})
	a.terminals.Store(id, t)

	out := &eventWriter{ctx: a.ctx, event: "sandbox-terminal:" + id}
	go func() {
		err := exec.StreamWithContext(sctx, remotecommand.StreamOptions{
			Stdin: pr, Stdout: out, Tty: true, TerminalSizeQueue: t.sizes,
		})
		a.terminals.Delete(id)
		_ = pr.Close()
		scancel()
		msg := ""
		if err != nil {
			msg = err.Error()
		}
		runtime.EventsEmit(a.ctx, "sandbox-terminal:"+id+":exit", msg)
	}()
	return nil
}

// SandboxTerminalInput sends keystrokes to a terminal session.
func (a *App) SandboxTerminalInput(id, data string) error {
	t, err := a.terminal(id)
	if err != nil {
		return err
	}
	_, err = t.input.Write([]byte(data))
	return err
}

// ResizeSandboxTerminal tells the shell the size of the terminal emulator.
func (a *App) ResizeSandboxTerminal(id string, cols, rows int) error {
	t, err := a.terminal(id)
	if err != nil {
		return err
	}
	t.sizes.set(remotecommand.TerminalSize{Width: clampSize(cols), Height: clampSize(rows)})
	return nil
}

// CloseSandboxTerminal ends a terminal session. Closing an unknown or
// finished session is not an error.
func (a *App) CloseSandboxTerminal(id string) {
	v, ok := a.terminals.LoadAndDelete(id)
	if !ok {
		return
	}
	t := v.(*sandboxTerminal)
	t.cancel()
	_ = t.input.Close()
}

// terminal returns an open terminal session.
func (a *App) terminal(id string) (*sandboxTerminal, error) {
	v, ok := a.terminals.Load(id)
	if !ok {
		return nil, fmt.Errorf("terminal session %s is closed", id)
	}
	return v.(*sandboxTerminal), nil
}

// clampSize keeps a terminal dimension in the range the exec API accepts.
func clampSize(n int) uint16 {
	switch {
	case n < 1:
		return 1
	case n > 65535:
		return 65535
	}
	return uint16(n)
}
