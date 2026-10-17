package main

import (
	"embed"
	"fmt"
	"io/fs"
	"strings"
	"sync"

	"github.com/egoist/mygo/ui"
)

// resources holds the pictures the window draws: the app icon, the package
// artwork, the Adobe host icons and the interface's own icons. The first is
// also the icon MyGo gives the app when it packages it.
//
//go:embed resources/icon.png resources/ccx-icon.png resources/apps resources/icons
var resources embed.FS

// pictures holds the decoded images, which are read once rather than on
// every frame.
type pictures struct {
	app  *ui.Bitmap
	ccx  *ui.Bitmap
	host map[string]*ui.SVG
	icon map[string]*ui.SVG
}

// draw returns the decoded images, decoding them the first time.
var draw = sync.OnceValue(func() *pictures {
	load := func(name string) []byte {
		data, err := resources.ReadFile("resources/" + name)
		if err != nil {
			panic(fmt.Sprintf("openuxp: reading %s: %v", name, err))
		}
		return data
	}
	bitmap := func(name string) *ui.Bitmap {
		image, err := ui.DecodeBitmap(load(name))
		if err != nil {
			panic(fmt.Sprintf("openuxp: decoding %s: %v", name, err))
		}
		return image
	}
	svgs := func(directory string) map[string]*ui.SVG {
		entries, err := fs.ReadDir(resources, "resources/"+directory)
		if err != nil {
			panic(fmt.Sprintf("openuxp: reading %s: %v", directory, err))
		}
		found := make(map[string]*ui.SVG, len(entries))
		for _, entry := range entries {
			name := strings.TrimSuffix(entry.Name(), ".svg")
			found[name] = ui.MustParseSVG(load(directory + "/" + entry.Name()))
		}
		return found
	}
	return &pictures{
		app:  bitmap("icon.png"),
		ccx:  bitmap("ccx-icon.png"),
		host: svgs("apps"),
		icon: svgs("icons"),
	}
})

// hostIcon returns the icon of an Adobe host, falling back to the generic
// one for a host the app does not know.
func hostIcon(name string) *ui.SVG {
	if icon, ok := draw().host[name]; ok {
		return icon
	}
	return draw().host["others"]
}

// interfaceIcon returns one of the interface's own icons.
func interfaceIcon(name string) *ui.SVG {
	return draw().icon[name]
}
