// Command app is the myskills desktop app (design §10).
package main

import (
	"fmt"
	"os"

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
		win = mygo.NewWindow(mygo.WindowOptions{
			Title:          "MySkills",
			Width:          1120,
			Height:         720,
			MinWidth:       840,
			MinHeight:      520,
			TitleBarStyle:  mygo.TitleBarHiddenInset,
			TitleBarHeight: titleBarHeight,
			Vibrancy:       mygo.VibrancySidebar,
			StateKey:       "main",
			Content:        ui.View(a.view),
		})
		mygo.App.OnDidBecomeActive(func() { win.Update(a.reload) })
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
