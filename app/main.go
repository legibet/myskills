// Command app is the myskills desktop app (design §10).
package main

import (
	"fmt"
	"os"
	"runtime"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"github.com/legibet/myskills/internal/core"
)

func main() {
	svc, err := core.Open()
	if err != nil {
		fmt.Fprintln(os.Stderr, "myskills:", err)
		os.Exit(1)
	}
	var a *app
	mygo.App.WhenReady(func() {
		mygo.App.SetMenu(mygo.NewMenu([]*mygo.MenuItem{
			{Role: mygo.RoleAppMenu},
			{Role: mygo.RoleFileMenu}, // Close on macOS, Quit elsewhere
			{Role: mygo.RoleEditMenu},
			{Role: mygo.RoleWindowMenu},
		}))
		var win *mygo.Window
		desk := desktop{
			chooseFolder: func(title string) string {
				paths, err := mygo.Dialog.Open(mygo.OpenDialogOptions{Parent: win, Title: title, Directory: true, CreateDirectories: true})
				if err != nil || len(paths) == 0 {
					return ""
				}
				return paths[0]
			},
			reveal: mygo.Shell.ShowItemInFolder,
			open:   func(path string) { _ = mygo.Shell.OpenPath(path) },
		}
		post := func(fn func()) { win.Update(fn) }
		a = newApp(svc, desk, post)
		opts := mygo.WindowOptions{
			Title:           "MySkills",
			Width:           1120,
			Height:          720,
			MinWidth:        840,
			MinHeight:       520,
			TitleBarStyle:   mygo.TitleBarHiddenInset,
			TitleBarHeight:  barHeight,
			Vibrancy:        mygo.VibrancySidebar,
			BackgroundColor: "light-dark(#ffffff, #1e1e20)",
			StateKey:        "main",
			Content:         ui.View(a.view),
		}
		if runtime.GOOS == "darwin" {
			// Centered in the bars at the top of the panes.
			opts.TrafficLightPosition = &mygo.Point{X: 19, Y: 19}
		}
		win = mygo.NewWindow(opts)
		a.reload() // after win is set: the read posts to it
		mygo.App.OnDidBecomeActive(func() { win.Update(func() { a.reload() }) })
	})
	err = mygo.App.Run()
	if a != nil {
		a.close()
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "myskills:", err)
		os.Exit(1)
	}
}
