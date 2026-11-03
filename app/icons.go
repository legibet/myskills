package main

import (
	"embed"

	"github.com/egoist/mygo/ui"
)

// Icons from Lucide (icons/LICENSE).
//
//go:embed icons/*.svg
var iconFiles embed.FS

func icon(name string) *ui.SVG {
	data, err := iconFiles.ReadFile("icons/" + name + ".svg")
	if err != nil {
		panic(err)
	}
	return ui.MustParseSVG(data)
}

var (
	iconSkills   = icon("book-open")
	iconProjects = icon("folder")
	iconFind     = icon("search")
	iconAgents   = icon("bot")
	iconGlobal   = icon("globe")
	iconProject  = icon("folder")
	iconMore     = icon("ellipsis")
	iconDisable  = icon("x")
	iconAdd      = icon("plus")
	iconRefresh  = icon("refresh-cw")
	iconExternal = icon("external-link")
	iconWarning  = icon("triangle-alert")
	iconFolder   = icon("folder-open")
	iconLink     = icon("link")
	iconChevron  = icon("chevron-down")
)
