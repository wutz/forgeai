package app

import (
	goruntime "runtime"
	"sync"

	"github.com/wailsapp/wails/v2/pkg/menu"
	"github.com/wailsapp/wails/v2/pkg/menu/keys"
	wailsruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

// The application menu carries the version and the update check. On macOS they
// are in the first menu, named after the app: "About forgeai" shows the
// version and "Check for Updates…" follows it. The other platforms have no such
// menu, so they get a Help menu with the same two items. "Check for Updates…"
// asks the UI to run the check, so the result and the install confirmation
// appear in the window like those of the sidebar button.

// checkUpdateEvent asks the UI to check for a newer release and report the
// outcome, whether or not one is out.
const checkUpdateEvent = "update:check"

// menuLabels are the menu texts the UI owns, because the UI owns the language.
type menuLabels struct {
	// about is the text of the item that shows the version.
	about string
	// checkUpdate is the text of the item that checks for a newer release.
	checkUpdate string
	// help is the title of the Help menu outside macOS.
	help string
	// hide and quit are the macOS app menu's items that Wails fills in
	// itself only in English.
	hide, quit string
}

// defaultMenuLabels are used until the UI reports its language.
var defaultMenuLabels = menuLabels{
	about:       "About forgeai",
	checkUpdate: "Check for Updates…",
	help:        "Help",
	hide:        "Hide forgeai",
	quit:        "Quit forgeai",
}

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

// SetMenuLabels rebuilds the application menu in the UI's language. The keys
// are about, checkUpdate, help, hide and quit; a missing one keeps its English
// text.
func (a *App) SetMenuLabels(texts map[string]string) {
	pick := func(key, fallback string) string {
		if v := texts[key]; v != "" {
			return v
		}
		return fallback
	}
	d := defaultMenuLabels
	labels := menuLabels{
		about:       pick("about", d.about),
		checkUpdate: pick("checkUpdate", d.checkUpdate),
		help:        pick("help", d.help),
		hide:        pick("hide", d.hide),
		quit:        pick("quit", d.quit),
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
// Edit and Window menus, which the system fills in. Its first menu cannot be
// the standard one, since that has no place for the update item: it is built
// here with the items the standard one has that a Wails app can run.
func (a *App) buildMenu(goos string, labels menuLabels) *menu.Menu {
	check := func(*menu.CallbackData) { a.emitEvent(a.ctx, checkUpdateEvent) }
	about := func(*menu.CallbackData) {
		wailsruntime.MessageDialog(a.ctx, wailsruntime.MessageDialogOptions{
			Type:    wailsruntime.InfoDialog,
			Title:   "forgeai",
			Message: "forgeai " + versionLabel(Version),
		})
	}

	m := menu.NewMenu()
	if goos == "darwin" {
		app := m.AddSubmenu("forgeai")
		app.AddText(labels.about, nil, about)
		app.AddText(labels.checkUpdate, nil, check)
		app.AddSeparator()
		app.AddText(labels.hide, keys.CmdOrCtrl("h"), func(*menu.CallbackData) { wailsruntime.Hide(a.ctx) })
		app.AddSeparator()
		app.AddText(labels.quit, keys.CmdOrCtrl("q"), func(*menu.CallbackData) { wailsruntime.Quit(a.ctx) })
		m.Append(menu.EditMenu())
		m.Append(menu.WindowMenu())
		return m
	}
	help := m.AddSubmenu(labels.help)
	help.AddText(labels.about, nil, about)
	help.AddText(labels.checkUpdate, nil, check)
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
