package main

import (
	"embed"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/options/mac"

	"github.com/wutz/forgeai/gui/app"
)

//go:embed all:frontend/dist
var assets embed.FS

func main() {
	a := app.New()
	err := wails.Run(&options.App{
		Title:     "forgeai",
		Width:     1280,
		Height:    840,
		MinWidth:  960,
		MinHeight: 620,
		AssetServer: &assetserver.Options{
			Assets: assets,
		},
		BackgroundColour: &options.RGBA{R: 250, G: 250, B: 250, A: 1},
		// Hide the macOS title bar: the app header is the drag region and
		// leaves room for the window controls.
		Mac: &mac.Options{
			TitleBar: mac.TitleBarHiddenInset(),
		},
		Menu:      app.ApplicationMenu(a),
		OnStartup: a.Startup,
		Bind: []any{
			a,
		},
	})
	if err != nil {
		println("Error:", err.Error())
	}
}
