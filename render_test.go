package main

import (
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"github.com/egoist/mygo/ui"
)

// windowControls is the room a window's controls take, as a window with a
// hidden title bar reports it: the traffic lights on macOS, and the window
// buttons on Windows.
var windowControls = ui.TitleBar{Height: 52, Left: 78, Right: 138}

// TestRenderScreens writes each screen of the window to a PNG, for looking
// at the interface without opening it, and composites the translucent ones
// over a backdrop so that the material can be judged. Point
// OPENUXP_RENDER_DIR at a directory to run it.
func TestRenderScreens(t *testing.T) {
	dir := os.Getenv("OPENUXP_RENDER_DIR")
	if dir == "" {
		t.Skip("set OPENUXP_RENDER_DIR to write the screens as PNGs")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	const width, height, scale = 1080, 800, 2

	for name, screen := range screens() {
		write := func(tt *ui.Tester, suffix string, image *image.RGBA) {
			t.Helper()
			file, err := os.Create(filepath.Join(dir, name+suffix+".png"))
			if err != nil {
				t.Fatal(err)
			}
			defer file.Close()
			if err := png.Encode(file, image); err != nil {
				t.Fatal(err)
			}
		}
		// A window without a material, as on Windows 10: the root draws the
		// background, and the screens hold no translucency.
		plain := ui.NewTester(screen.view, width, height)
		plain.SetScale(scale)
		plain.SetTitleBar(windowControls)
		write(plain, "", plain.Image())

		// A window with one: the panels and bars are translucent over the
		// desktop, which the composite over a backdrop stands in for.
		vibrant := ui.NewTester(screen.view, width, height)
		vibrant.SetScale(scale)
		vibrant.SetTitleBar(windowControls)
		vibrant.SetVibrancy(true)
		write(vibrant, "-vibrant", overDesktop(vibrant.Image()))
		t.Logf("wrote %s.png and %s-vibrant.png", name, name)
	}
}

// screens is every screen of the window, as the state that shows it.
func screens() map[string]*app {
	found := map[string]*app{}

	empty := newApp()
	empty.environment = &Environment{InstallerFound: true, CreativeCloudFound: true}
	found["1-empty"] = empty

	found["2-ready"] = readyApp(KindUXP, allowedPreflight())
	found["3-blocked"] = readyApp(KindUXP, blockedPreflight())

	cep := allowedPreflight()
	cep.Kind = KindCEP
	cep.DebugModeKeys = []string{"CSXS.6", "CSXS.7", "CSXS.8"}
	cep.RegistryPaths = nil
	found["4-cep"] = readyApp(KindCEP, cep)

	success := readyApp(KindUXP, allowedPreflight())
	success.stage = stageSuccess
	success.activationPending = true
	success.details = "安装目录：/Users/yarna/Library/Application Support/Adobe/UXP/Plugins/External/moonvy.ps.plugin\n" +
		"注册文件：/Users/yarna/Library/Application Support/Adobe/UXP/PluginsInfo/v1/PS.json\n" +
		"注册文件备份：/Users/yarna/Library/Application Support/Adobe/UXP/PluginsInfo/v1/PS.json.openuxp-backup-1759000000"
	found["5-success"] = success

	failed := newApp()
	failed.environment = &Environment{InstallerFound: true, CreativeCloudFound: true}
	failed.stage = stageError
	failed.errorFile = "unsupported-extension.rar"
	failed.message = "文件格式不受支持，请选择扩展名为 .ccx、.xdx 或 .zxp 的安装包。"
	found["6-error"] = failed

	update := readyApp(KindUXP, allowedPreflight())
	update.update = &UpdateInfo{Version: "1.2.0"}
	update.updateStage = updateDownloading
	update.updateProgress = 0.42
	found["7-update"] = update

	confirming := readyApp(KindUXP, allowedPreflight())
	confirming.confirming = "com.lanhu.ps"
	found["8-removing"] = confirming

	// A package the machine already has, which the version shows as an
	// upgrade.
	upgrade := readyApp(KindUXP, allowedPreflight())
	upgrade.pkg.PluginID = "moonvy.ps.plugin"
	upgrade.pkg.Version = "1.2.27"
	found["9-upgrading"] = upgrade

	for _, screen := range found {
		screen.installed = installedSample()
		screen.installedLoaded = true
	}
	return found
}

// installedSample is what the right column lists in the renders: a sideloaded
// plugin with an icon, one without, a CEP extension, one that has stopped,
// and one of every user.
func installedSample() []Installed {
	return []Installed{
		{ID: "moonvy.ps.plugin", Name: "Moonvy 月维", Version: "1.2.26", Kind: KindUXP,
			Hosts: []string{"Photoshop"}, Directory: "/tmp/moonvy", Source: sourceUser,
			Enabled: true, Removable: true, Icon: draw().ccx},
		{ID: "com.lanhu.ps", Name: "蓝湖", Version: "1.0.22", Kind: KindUXP,
			Hosts: []string{"Photoshop"}, Directory: "/tmp/lanhu", Source: sourceUser,
			Enabled: true, Removable: true},
		{ID: "com.moonvy.ps.cep", Name: "Moonvy 月维（经典）", Version: "0.1.24", Kind: KindCEP,
			Hosts: []string{"Photoshop", "Illustrator"}, Directory: "/tmp/cep", Source: sourceUser,
			Enabled: true, Removable: true, Icon: draw().app},
		{ID: "com.example.stopped", Name: "已停用的插件", Version: "0.9.0", Kind: KindUXP,
			Hosts: []string{"Adobe XD"}, Directory: "/tmp/stopped", Source: sourceUser,
			Enabled: false, Removable: true},
		{ID: "com.adobe.ccx.start", Name: "Adobe CCX-Start", Version: "2.16.0.8", Kind: KindCEP,
			Hosts:     []string{"After Effects", "InDesign", "Illustrator", "Photoshop", "Premiere Pro"},
			Directory: "/tmp/start", Source: sourceSystem, Enabled: true},
	}
}

// overDesktop composites a frame over a stand-in for the desktop, so that
// what the material would blur can be seen through the translucent panels.
func overDesktop(frame *image.RGBA) *image.RGBA {
	bounds := frame.Bounds()
	out := image.NewRGBA(bounds)
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			backdrop := desktopAt(x, y, bounds.Dx(), bounds.Dy())
			out.SetRGBA(x, y, composite(frame.RGBAAt(x, y), backdrop))
		}
	}
	return out
}

// desktopAt is a wallpaper-like gradient, so that a blurred backdrop reads
// as one rather than as a flat color.
func desktopAt(x, y, width, height int) color.RGBA {
	fx, fy := float64(x)/float64(width), float64(y)/float64(height)
	r := 0.25 + 0.55*fx + 0.20*fy
	g := 0.30 + 0.25*fx
	b := 0.75 - 0.25*fy
	return color.RGBA{
		R: uint8(clamp01(r) * 255),
		G: uint8(clamp01(g) * 255),
		B: uint8(clamp01(b) * 255),
		A: 255,
	}
}

// composite puts a premultiplied frame pixel over an opaque backdrop.
func composite(top, bottom color.RGBA) color.RGBA {
	alpha := float64(top.A) / 255
	blend := func(a, b uint8) uint8 {
		return uint8(float64(a)*alpha + float64(b)*(1-alpha))
	}
	return color.RGBA{R: blend(top.R, bottom.R), G: blend(top.G, bottom.G), B: blend(top.B, bottom.B), A: 255}
}

func clamp01(v float64) float64 {
	return min(max(v, 0), 1)
}
