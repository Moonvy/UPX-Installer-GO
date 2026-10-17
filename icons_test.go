package main

import (
	"archive/zip"
	"bytes"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"testing"
)

// writeIconPNG writes a PNG of the given size, to tell icons apart.
func writeIconPNG(t *testing.T, path string, size int) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	picture := image.NewRGBA(image.Rect(0, 0, size, size))
	picture.SetRGBA(0, 0, color.RGBA{R: 0xff, A: 0xff})
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	if err := png.Encode(file, picture); err != nil {
		t.Fatal(err)
	}
}

// writeUXPManifest replaces the manifest of a UXP fixture.
func writeUXPManifest(t *testing.T, directory, manifest string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(directory, "manifest.json"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
}

// iconSize is the size of an icon, or zero when there is none.
func iconSize(icon interface {
	Size() (int, int)
}) (int, int) {
	if icon == nil {
		return 0, 0
	}
	return icon.Size()
}

func TestExtensionIconPicksTheLargestPluginListIcon(t *testing.T) {
	_, base := uxpFixture(t)
	directory := installUXPFixture(t, base, "com.example.plugin_1.0.0")
	writeIconPNG(t, filepath.Join(directory, "icons", "small.png"), 4)
	writeIconPNG(t, filepath.Join(directory, "icons", "large.png"), 8)
	writeIconPNG(t, filepath.Join(directory, "icons", "toolbar.png"), 16)
	writeUXPManifest(t, directory, `{
		"id":"com.example.plugin","version":"1.0.0","name":"Example",
		"icons":[
			{"path":"icons/toolbar.png","width":16,"species":["toolbar"]},
			{"path":"icons/small.png","width":16,"species":["pluginList"]},
			{"path":"icons/large.png","width":48,"species":["pluginList"]}
		]}`)

	icon := extensionIcon(Installed{Kind: KindUXP, Directory: directory})
	if icon == nil {
		t.Fatal("the icon was not read")
	}
	if width, height := iconSize(icon); width != 8 || height != 8 {
		t.Errorf("the icon is %dx%d, want the largest plugin list one", width, height)
	}
}

func TestExtensionIconFallsBackWhenTheFileIsMissing(t *testing.T) {
	_, base := uxpFixture(t)
	directory := installUXPFixture(t, base, "com.lanhu.ps_1.0.22")
	// A manifest naming an icon the package does not carry, as plugins built
	// from source often do.
	writeUXPManifest(t, directory, `{
		"id":"com.lanhu.ps","version":"1.0.22","name":"蓝湖",
		"icons":[{"path":"icons/plugin.png","width":48,"species":["pluginList"]}]}`)

	if icon := extensionIcon(Installed{Kind: KindUXP, Directory: directory}); icon != nil {
		t.Errorf("a missing icon was read as %v", icon)
	}
}

func TestExtensionIconFallsBackWithoutAnIconList(t *testing.T) {
	_, base := uxpFixture(t)
	directory := installUXPFixture(t, base, "com.example.plugin_1.0.0")
	writeUXPManifest(t, directory, `{"id":"com.example.plugin","version":"1.0.0","name":"Example"}`)
	if icon := extensionIcon(Installed{Kind: KindUXP, Directory: directory}); icon != nil {
		t.Errorf("an icon was read from a manifest without one: %v", icon)
	}
}

func TestExtensionIconFallsBackWithoutADirectory(t *testing.T) {
	if icon := extensionIcon(Installed{Kind: KindUXP}); icon != nil {
		t.Errorf("an icon was read without a directory: %v", icon)
	}
}

func TestExtensionIconReadsTheDarkInterfaceCEPIcon(t *testing.T) {
	directory := filepath.Join(t.TempDir(), "com.example.panel")
	writeIconPNG(t, filepath.Join(directory, "roll.png"), 4)
	writeIconPNG(t, filepath.Join(directory, "light.png"), 8)
	writeIconPNG(t, filepath.Join(directory, "dark.png"), 12)
	if err := os.MkdirAll(filepath.Join(directory, "CSXS"), 0o755); err != nil {
		t.Fatal(err)
	}
	// What a CEP manifest really carries: the artwork for a light interface
	// under "Normal", and the one for a dark interface under "DarkNormal".
	manifest := `<?xml version="1.0"?>
<ExtensionManifest ExtensionBundleId="com.example.panel" ExtensionBundleName="Panel" ExtensionBundleVersion="1.0.0">
  <DispatchInfoList>
    <DispatchInfo>
      <UI>
        <Icons>
          <Icon Type="RollOver">./roll.png</Icon>
          <Icon Type="Normal">./light.png</Icon>
          <Icon Type="DarkNormal">./dark.png</Icon>
        </Icons>
      </UI>
    </DispatchInfo>
  </DispatchInfoList>
</ExtensionManifest>`
	if err := os.WriteFile(filepath.Join(directory, "CSXS", "manifest.xml"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}

	icon := extensionIcon(Installed{Kind: KindCEP, Directory: directory})
	if icon == nil {
		t.Fatal("the icon was not read")
	}
	if width, _ := iconSize(icon); width != 12 {
		t.Errorf("the icon is %d wide, want the one drawn for a dark interface", width)
	}
}

func TestCEPIconPathPrefersTheDarkInterfaceIcon(t *testing.T) {
	// The interface is dark, so the icon drawn for it is the "Dark" one; the
	// "Normal" one is drawn for a light interface, and is dark artwork.
	manifest := `<Icons><Icon Type="Normal">./light.png</Icon><Icon Type="DarkNormal">./dark.png</Icon></Icons>`
	if got := cepIconPath(manifest); got != "./dark.png" {
		t.Errorf("cepIconPath is %q", got)
	}
	// Without one, the normal icon is better than none, and the rolled over
	// one better than nothing.
	if got := cepIconPath(`<Icons><Icon Type="Normal">./n.png</Icon></Icons>`); got != "./n.png" {
		t.Errorf("cepIconPath is %q", got)
	}
	if got := cepIconPath(`<Icons><Icon Type="RollOver">./r.png</Icon></Icons>`); got != "./r.png" {
		t.Errorf("cepIconPath is %q", got)
	}
	if got := cepIconPath(`<ExtensionManifest/>`); got != "" {
		t.Errorf("cepIconPath is %q, want none", got)
	}
}

func TestUXPIconPathPrefersTheDarkInterfaceIcon(t *testing.T) {
	// Adobe names the artwork after the interface it is for: the icon drawn
	// for a dark interface is the light one.
	icons := []uxpIcon{
		{Path: "dark.png", Width: 23, Theme: []string{"lightest", "light"}, Species: []string{"pluginList"}},
		{Path: "light.png", Width: 23, Theme: []string{"darkest", "dark", "medium"}, Species: []string{"pluginList"}},
	}
	if got := uxpIconPath(icons); got != "light.png" {
		t.Errorf("uxpIconPath is %q, want the icon drawn for a dark interface", got)
	}
}

func TestUXPIconPathPrefersAPluginListIcon(t *testing.T) {
	icons := []uxpIcon{
		{Path: "big.png", Width: 96},
		{Path: "list.png", Width: 16, Species: []string{"pluginList"}},
	}
	if got := uxpIconPath(icons); got != "list.png" {
		t.Errorf("uxpIconPath is %q", got)
	}
}

func TestUXPIconPathFallsBackWithoutAPluginListIcon(t *testing.T) {
	// Some manifests name no species at all, as 摹客's does.
	icons := []uxpIcon{
		{Width: 96}, // no path to read
		{Path: "a.png", Width: 16, Species: []string{"toolbar"}},
	}
	if got := uxpIconPath(icons); got != "a.png" {
		t.Errorf("uxpIconPath is %q, want the only icon with a path", got)
	}
	if got := uxpIconPath([]uxpIcon{{Width: 96}}); got != "" {
		t.Errorf("uxpIconPath is %q, want none", got)
	}
}

func TestScanInstalledReadsIcons(t *testing.T) {
	_, base := uxpFixture(t)
	directory := installUXPFixture(t, base, "com.example.plugin_1.0.0")
	writeIconPNG(t, filepath.Join(directory, "icons", "plugin.png"), 6)
	writeUXPManifest(t, directory, `{
		"id":"com.example.plugin","version":"1.0.0","name":"Example",
		"icons":[{"path":"icons/plugin.png","width":48,"species":["pluginList"]}]}`)
	writeUXPRegistry(t, base, "PS", registryPlugin{
		PluginID: "com.example.plugin", Name: "Example", VersionString: "1.0.0",
		Path: "$localPlugins/External/com.example.plugin_1.0.0", Status: "enabled",
	})

	a := newApp()
	a.scanInstalled()
	if !a.installedLoaded {
		t.Fatal("the scan did not finish")
	}
	found := userExtensions(a.installed)
	if len(found) != 1 {
		t.Fatalf("found %d extensions of this user: %+v", len(found), found)
	}
	icon := found[0].Icon
	if icon == nil {
		t.Fatal("the scan did not read the icon")
	}
	if width, _ := iconSize(icon); width != 6 {
		t.Errorf("the icon is %d wide", width)
	}
}

func TestLoadInstalledIconsDropsWhatANewerScanOvertook(t *testing.T) {
	a := newApp()
	a.installed = []Installed{sampleExtension("com.a", "A", KindUXP, "Photoshop")}
	a.installedLoaded = true
	stale := a.installedGeneration
	a.installedGeneration++ // a newer scan answered in the meantime
	a.loadInstalledIcons(stale, []Installed{{ID: "com.stale"}})

	if a.installed[0].Icon != nil {
		t.Error("an overtaken scan attached its icon")
	}
	if a.installed[0].ID != "com.a" {
		t.Errorf("an overtaken scan replaced the list: %+v", a.installed)
	}
}

func TestLoadInstalledIconsIgnoresAListThatChangedSize(t *testing.T) {
	a := newApp()
	a.installed = []Installed{
		sampleExtension("com.a", "A", KindUXP, "Photoshop"),
		sampleExtension("com.b", "B", KindUXP, "Photoshop"),
	}
	a.installedLoaded = true
	a.loadInstalledIcons(a.installedGeneration, []Installed{{ID: "com.a"}})

	if a.installed[0].Icon != nil {
		t.Error("a shorter list attached its icons")
	}
}

func TestGroupInstalledPutsExtensionsUnderTheirApplication(t *testing.T) {
	groups := groupInstalled([]Installed{
		sampleExtension("com.a", "A", KindUXP, "Photoshop"),
		sampleExtension("com.b", "B", KindUXP, "Illustrator"),
		sampleExtension("com.c", "C", KindCEP, "Photoshop"),
		sampleExtension("com.d", "D", KindUXP, "Photoshop", "Illustrator"),
		sampleExtension("com.e", "E", KindUXP, "Adobe XD"),
		sampleExtension("com.f", "F", KindUXP),
	})

	want := []struct {
		name  string
		count int
	}{
		{"Photoshop", 2}, {"Adobe XD", 1}, {"Illustrator", 1},
		{sharedGroup, 1}, {unknownGroup, 1},
	}
	if len(groups) != len(want) {
		t.Fatalf("the groups are %+v", groups)
	}
	for index, expected := range want {
		if groups[index].Name != expected.name || len(groups[index].Extensions) != expected.count {
			t.Errorf("group %d is %q with %d extensions, want %q with %d",
				index, groups[index].Name, len(groups[index].Extensions), expected.name, expected.count)
		}
	}
}

func TestGroupInstalledNamesEachApplicationIcon(t *testing.T) {
	groups := groupInstalled([]Installed{
		sampleExtension("com.a", "A", KindUXP, "Photoshop"),
		sampleExtension("com.b", "B", KindUXP, "After Effects"),
		sampleExtension("com.c", "C", KindUXP, "Photoshop", "Illustrator"),
	})
	for _, group := range groups {
		switch group.Name {
		case "Photoshop", "After Effects":
			if group.Icon == nil {
				t.Errorf("the group %q has no icon", group.Name)
			}
		default:
			if group.Icon != nil {
				t.Errorf("the group %q has an icon", group.Name)
			}
		}
	}
}

func TestHostIconNameFindsEveryApplicationTheAppKnows(t *testing.T) {
	for _, code := range hostCodes {
		label, icon := hostMetadata(code)
		if label == code {
			t.Errorf("hostCodes names %q, which hostMetadata does not know", code)
			continue
		}
		if got := hostIconName(label); got != icon {
			t.Errorf("hostIconName(%q) is %q, want %q", label, got, icon)
		}
	}
	for _, label := range []string{sharedGroup, unknownGroup, "Nonexistent"} {
		if got := hostIconName(label); got != "" {
			t.Errorf("hostIconName(%q) is %q, want none", label, got)
		}
	}
}

func TestGroupInstalledOrdersApplications(t *testing.T) {
	groups := groupInstalled([]Installed{
		sampleExtension("com.z", "Z", KindCEP, "InDesign"),
		sampleExtension("com.m", "M", KindUXP, "Photoshop", "After Effects"),
		sampleExtension("com.a", "A", KindUXP, "After Effects"),
		sampleExtension("com.p", "P", KindUXP, "Premiere Pro"),
	})
	names := []string{}
	for _, group := range groups {
		names = append(names, group.Name)
	}
	want := []string{"After Effects", "InDesign", "Premiere Pro", sharedGroup}
	if len(names) != len(want) {
		t.Fatalf("the groups are %v", names)
	}
	for index := range want {
		if names[index] != want[index] {
			t.Fatalf("the groups are %v, want %v", names, want)
		}
	}
}

func TestInstalledPaneShowsTheGroups(t *testing.T) {
	a := installedApp(
		sampleExtension("com.a", "A", KindUXP, "Photoshop"),
		sampleExtension("com.b", "B", KindCEP, "Illustrator"),
		sampleExtension("com.c", "C", KindUXP, "Photoshop", "Illustrator"),
	)
	tt := tall(a)
	for _, text := range []string{"Photoshop", "Illustrator", sharedGroup} {
		if !tt.HasText(text) {
			t.Errorf("the group %q is missing: %q", text, tt.Texts())
		}
	}
}

func pngBytes(t *testing.T, size int) []byte {
	t.Helper()
	picture := image.NewRGBA(image.Rect(0, 0, size, size))
	picture.SetRGBA(0, 0, color.RGBA{R: 0xff, A: 0xff})
	var buf bytes.Buffer
	if err := png.Encode(&buf, picture); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func writeZipArchive(t *testing.T, target string, textFiles map[string]string, binaryFiles map[string][]byte) {
	t.Helper()
	out, err := os.Create(target)
	if err != nil {
		t.Fatal(err)
	}
	defer out.Close()
	archive := zip.NewWriter(out)
	for name, content := range textFiles {
		writer, err := archive.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := writer.Write([]byte(content)); err != nil {
			t.Fatal(err)
		}
	}
	for name, data := range binaryFiles {
		writer, err := archive.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := writer.Write(data); err != nil {
			t.Fatal(err)
		}
	}
	if err := archive.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestPackageIconReadsUXPIcon(t *testing.T) {
	pkgPath := filepath.Join(t.TempDir(), "plugin.ccx")
	writeZipArchive(t, pkgPath, map[string]string{
		"manifest.json": `{
			"manifestVersion": 5,
			"id": "com.test.icon",
			"name": "Icon Test",
			"version": "1.0.0",
			"icons": [{"width": 48, "path": "icons/logo.png"}]
		}`,
	}, map[string][]byte{
		"icons/logo.png": pngBytes(t, 48),
	})

	pkg, err := readPackage(pkgPath)
	if err != nil {
		t.Fatalf("readPackage failed: %v", err)
	}
	if pkg.Icon == nil {
		t.Fatal("expected package icon to be loaded, got nil")
	}
	width, height := pkg.Icon.Size()
	if width != 48 || height != 48 {
		t.Errorf("expected icon size 48x48, got %dx%d", width, height)
	}
}

func TestPackageIconReadsCEPIcon(t *testing.T) {
	pkgPath := filepath.Join(t.TempDir(), "plugin.zxp")
	writeZipArchive(t, pkgPath, map[string]string{
		"CSXS/manifest.xml": `<?xml version="1.0" encoding="UTF-8"?>
<ExtensionManifest Version="7.0" ExtensionBundleId="com.test.cep" ExtensionBundleVersion="1.0.0">
  <Icons>
    <Icon Type="Normal">./res/icon.png</Icon>
  </Icons>
</ExtensionManifest>`,
	}, map[string][]byte{
		"res/icon.png": pngBytes(t, 32),
	})

	pkg, err := readPackage(pkgPath)
	if err != nil {
		t.Fatalf("readPackage failed: %v", err)
	}
	if pkg.Icon == nil {
		t.Fatal("expected package icon to be loaded, got nil")
	}
	width, height := pkg.Icon.Size()
	if width != 32 || height != 32 {
		t.Errorf("expected icon size 32x32, got %dx%d", width, height)
	}
}

