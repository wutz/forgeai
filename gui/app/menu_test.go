package app

import (
	"context"
	"testing"

	"github.com/wailsapp/wails/v2/pkg/menu"
)

func labelsOf(m *menu.Menu) []string {
	var out []string
	for _, it := range m.Items {
		if it.IsSeparator() {
			out = append(out, "-")
		} else {
			out = append(out, it.Label)
		}
	}
	return out
}

func equal(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestBuildMenuMacOSPutsTheUpdateCheckUnderAbout(t *testing.T) {
	var events []string
	a := &App{emit: func(_ context.Context, name string, _ ...interface{}) { events = append(events, name) }}
	m := a.buildMenu("darwin", defaultMenuLabels)

	if len(m.Items) != 3 {
		t.Fatalf("macOS menu has %d top-level items, want app, edit, window", len(m.Items))
	}
	appMenu := m.Items[0].SubMenu
	want := []string{"About forgeai", "Check for Updates…", "-", "Hide forgeai", "-", "Quit forgeai"}
	if got := labelsOf(appMenu); !equal(got, want) {
		t.Fatalf("app menu = %q, want %q", got, want)
	}
	appMenu.Items[1].Click(&menu.CallbackData{})
	if len(events) != 1 || events[0] != checkUpdateEvent {
		t.Errorf("events after click = %v", events)
	}
}

func TestBuildMenuOtherPlatformsHaveAHelpMenu(t *testing.T) {
	a := &App{}
	m := a.buildMenu("windows", menuLabels{about: "关于", checkUpdate: "检查更新", help: "帮助"})
	if len(m.Items) != 1 || m.Items[0].Label != "帮助" {
		t.Fatalf("menu = %q, want a lone 帮助 menu", labelsOf(m))
	}
	if got := labelsOf(m.Items[0].SubMenu); !equal(got, []string{"关于", "检查更新"}) {
		t.Errorf("help menu = %q", got)
	}
}

func TestSetMenuLabelsKeepsEnglishForMissingKeys(t *testing.T) {
	a := &App{}
	a.SetMenuLabels(map[string]string{"help": "帮助"})
	if a.menu.labels.help != "帮助" || a.menu.labels.about != defaultMenuLabels.about {
		t.Errorf("labels = %+v", a.menu.labels)
	}
}

func TestVersionLabel(t *testing.T) {
	if got := versionLabel("1.2.3"); got != "v1.2.3" {
		t.Errorf("release = %q", got)
	}
	if got := versionLabel(devVersion); got != "dev" {
		t.Errorf("dev = %q", got)
	}
}
