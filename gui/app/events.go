package app

import (
	"context"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// emitEvent sends an event to the UI. The Wails runtime aborts when it is
// called outside an app context, so tests set App.emit to capture the events.
func (a *App) emitEvent(ctx context.Context, name string, data ...interface{}) {
	if a.emit != nil {
		a.emit(ctx, name, data...)
		return
	}
	runtime.EventsEmit(ctx, name, data...)
}
