package main

import (
	"log"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

func main() {
	// The interface is dark whatever the desktop's appearance is, and so is
	// the material behind it.
	mygo.Theme.SetSource(mygo.ThemeDark)
	application := newApp()
	mygo.App.WhenReady(func() {
		window := mygo.NewWindow(mygo.WindowOptions{
			Title:     "OpenUXP Installer",
			Width:     1080,
			Height:    800,
			MinWidth:  940,
			MinHeight: 620,
			// The view draws its own title bar under the window controls, so
			// the traffic lights and the window buttons sit over it.
			TitleBarStyle:  mygo.TitleBarHiddenInset,
			TitleBarHeight: titleBarHeight,
			// The material blurs what is behind the window; the translucent
			// bars and panels let it show. Windows 11 shows Mica here, and
			// Windows 10 shows none, where the view draws its own background.
			Vibrancy: mygo.VibrancyUnderWindow,
			Content:  ui.View(application.view),
		})
		application.attach(window)
	})
	if err := mygo.App.Run(); err != nil {
		log.Fatal(err)
	}
}
