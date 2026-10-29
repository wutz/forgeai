package app

import (
	"context"
	"testing"

	"github.com/wailsapp/wails/v2/pkg/menu"
)

func helpMenu(t *testing.T, m *menu.Menu) *menu.MenuItem {
	t.Helper()
	last := m.Items[len(m.Items)-1]
	if last.SubMenu == nil {
		t.Fatalf("last menu item %q has no submenu", last.Label)
	}
	return last
}

func TestBuildMenuShowsVersionAndChecksForUpdates(t *testing.T) {
	prev := Version
	t.Cleanup(func() { Version = prev })
	Version = "1.2.3"

	var events []string
	a := &App{emit: func(_ context.Context, name string, _ ...interface{}) { events = append(events, name) }}
	help := helpMenu(t, a.buildMenu("linux", menuLabels{help: "Help", checkUpdate: "Check"}))

	if help.Label != "Help" {
		t.Errorf("menu title = %q", help.Label)
	}
	items := help.SubMenu.Items
	if len(items) != 3 {
		t.Fatalf("Help has %d items, want version, separator, update", len(items))
	}
	if items[0].Label != "forgeai v1.2.3" || !items[0].Disabled {
		t.Errorf("version item = %q disabled=%v", items[0].Label, items[0].Disabled)
	}
	if items[2].Label != "Check" {
		t.Errorf("update item = %q", items[2].Label)
	}
	items[2].Click(&menu.CallbackData{})
	if len(events) != 1 || events[0] != checkUpdateEvent {
		t.Errorf("events after click = %v", events)
	}
}

func TestBuildMenuDevVersionAndPlatforms(t *testing.T) {
	prev := Version
	t.Cleanup(func() { Version = prev })
	Version = devVersion

	a := &App{}
	mac := a.buildMenu("darwin", defaultMenuLabels)
	if len(mac.Items) != 4 {
		t.Errorf("macOS menu has %d top-level items, want app, edit, window, help", len(mac.Items))
	}
	other := a.buildMenu("windows", defaultMenuLabels)
	if len(other.Items) != 1 {
		t.Errorf("windows menu has %d top-level items, want help only", len(other.Items))
	}
	if got := helpMenu(t, other).SubMenu.Items[0].Label; got != "forgeai dev" {
		t.Errorf("dev version item = %q", got)
	}
}
