package main

import (
	"embed"
	"fmt"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"

	"mmcli/internal/app"
)

//go:embed all:frontend/dist
var assets embed.FS

func main() {
	service := app.NewService()

	err := wails.Run(&options.App{
		Title:            "mmcli",
		Width:            980,
		Height:           680,
		MinWidth:         760,
		MinHeight:        560,
		Assets:           assets,
		BackgroundColour: &options.RGBA{R: 245, G: 247, B: 250, A: 255},
		Bind: []interface{}{
			service,
		},
	})
	if err != nil {
		fmt.Println("mmcli UI failed:", err)
	}
}
