package app

import (
	goruntime "runtime"
	"sync"

	"github.com/wailsapp/wails/v2/pkg/menu"
	wailsruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

// The application menu carries the version and the update check. Its Help
// menu shows the running version and a "Check for Updates…" item; the item
// asks the UI to run the check, so the result and the install confirmation
// appear in the window like those of the top bar button.

// checkUpdateEvent asks the UI to check for a newer release and report the
// outcome, whether or not one is out.
const checkUpdateEvent = "update:check"

// menuLabels are the menu texts the UI owns, because the UI owns the language.
type menuLabels struct {
	// help is the title of the menu holding the version and the update check.
	help string
	// checkUpdate is the text of the item that checks for a newer release.
	checkUpdate string
}

// defaultMenuLabels are used until the UI reports its language.
var defaultMenuLabels = menuLabels{help: "Help", checkUpdate: "Check for Updates…"}

// menuState is the language of the menu; the zero value means the defaults.
type menuState struct {
	mu     sync.Mutex
	labels menuLabels
}

// ApplicationMenu is the application menu in the language last reported by the
// UI. It is a function rather than a method so that Wails does not bind it.
func ApplicationMenu(a *App) *menu.Menu {
	a.menu.mu.Lock()
	labels := a.menu.labels
	a.menu.mu.Unlock()
	if labels == (menuLabels{}) {
		labels = defaultMenuLabels
	}
	return a.buildMenu(goruntime.GOOS, labels)
}

// SetMenuLabels rebuilds the application menu in the UI's language: help is
// the title of the Help menu and checkUpdate the text of its update item.
func (a *App) SetMenuLabels(help, checkUpdate string) {
	labels := menuLabels{help: help, checkUpdate: checkUpdate}
	if labels.help == "" {
		labels.help = defaultMenuLabels.help
	}
	if labels.checkUpdate == "" {
		labels.checkUpdate = defaultMenuLabels.checkUpdate
	}
	a.menu.mu.Lock()
	a.menu.labels = labels
	a.menu.mu.Unlock()
	if a.ctx == nil {
		return
	}
	wailsruntime.MenuSetApplicationMenu(a.ctx, ApplicationMenu(a))
}

// buildMenu lays out the application menu for goos. macOS keeps the standard
// app, Edit and Window menus, which the system fills in; the other platforms
// have no such menus to keep, so they get the Help menu alone.
func (a *App) buildMenu(goos string, labels menuLabels) *menu.Menu {
	m := menu.NewMenu()
	if goos == "darwin" {
		m.Append(menu.AppMenu())
		m.Append(menu.EditMenu())
		m.Append(menu.WindowMenu())
	}
	help := m.AddSubmenu(labels.help)
	help.Append(menu.Label("forgeai " + versionLabel(Version)).Disable())
	help.AddSeparator()
	help.AddText(labels.checkUpdate, nil, func(*menu.CallbackData) {
		a.emitEvent(a.ctx, checkUpdateEvent)
	})
	return m
}

// versionLabel is how a version is shown: a release number with a leading v,
// and a development build as it is.
func versionLabel(version string) string {
	if version == devVersion {
		return devVersion
	}
	return "v" + version
}
