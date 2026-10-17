package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// uxpFixture is a temporary home holding this user's Adobe directories, and
// the UXP directory inside it.
func uxpFixture(t *testing.T) (home, base string) {
	t.Helper()
	home = isolateUserDirs(t)
	base, err := uxpBaseDir()
	if err != nil {
		t.Fatalf("uxpBaseDir: %v", err)
	}
	return home, base
}

// writeUXPRegistry registers plugins for one Adobe application, the way the
// application itself does.
func writeUXPRegistry(t *testing.T, base, application string, plugins ...registryPlugin) {
	t.Helper()
	directory := filepath.Join(base, "PluginsInfo", "v1")
	if err := os.MkdirAll(directory, 0o755); err != nil {
		t.Fatal(err)
	}
	contents, err := json.MarshalIndent(map[string]any{"plugins": plugins}, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, application+".json"), contents, 0o644); err != nil {
		t.Fatal(err)
	}
}

// installUXPFixture creates the directory an UXP registration points at, and
// returns it.
func installUXPFixture(t *testing.T, base, folder string) string {
	t.Helper()
	directory := filepath.Join(base, "Plugins", "External", folder)
	if err := os.MkdirAll(directory, 0o755); err != nil {
		t.Fatal(err)
	}
	manifest := `{"id":"` + folder + `","version":"1.0.0","name":"Fixture"}`
	if err := os.WriteFile(filepath.Join(directory, "manifest.json"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
	return directory
}

// installCEPFixture creates a CEP extension of this user, and returns it.
func installCEPFixture(t *testing.T, directory, name, manifest string) string {
	t.Helper()
	extension := filepath.Join(directory, name)
	if err := os.MkdirAll(filepath.Join(extension, "CSXS"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(extension, "CSXS", "manifest.xml"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
	return extension
}

// userExtensions keeps the extensions of this user, so that a test reads the
// same on a machine that also has extensions for every user.
func userExtensions(found []Installed) []Installed {
	kept := []Installed{}
	for _, extension := range found {
		if extension.Source == sourceUser {
			kept = append(kept, extension)
		}
	}
	return kept
}

func TestInstalledReadsUXPRegistrations(t *testing.T) {
	_, base := uxpFixture(t)
	directory := installUXPFixture(t, base, "com.lanhu.ps_1.0.22")
	writeUXPRegistry(t, base, "PS", registryPlugin{
		PluginID:      "com.lanhu.ps",
		Name:          "蓝湖",
		VersionString: "1.0.22",
		Path:          "$localPlugins/External/com.lanhu.ps_1.0.22",
		Status:        "enabled",
	})

	found := userExtensions(installedExtensions())
	if len(found) != 1 {
		t.Fatalf("found %d extensions: %+v", len(found), found)
	}
	extension := found[0]
	if extension.Name != "蓝湖" || extension.Version != "1.0.22" || extension.ID != "com.lanhu.ps" {
		t.Errorf("the extension is %+v", extension)
	}
	if extension.Kind != KindUXP || extension.Source != sourceUser || !extension.Enabled {
		t.Errorf("the extension is %+v", extension)
	}
	if len(extension.Hosts) != 1 || extension.Hosts[0] != "Photoshop" {
		t.Errorf("its hosts are %v", extension.Hosts)
	}
	if extension.Directory != directory {
		t.Errorf("its directory is %q, want %q", extension.Directory, directory)
	}
	if !extension.Removable {
		t.Error("a plugin under Plugins/External is not removable")
	}
}

func TestInstalledMergesTheHostsOfOnePlugin(t *testing.T) {
	_, base := uxpFixture(t)
	installUXPFixture(t, base, "com.example.shared_1.0.0")
	plugin := registryPlugin{
		PluginID:      "com.example.shared",
		Name:          "Shared",
		VersionString: "1.0.0",
		Path:          "$localPlugins/External/com.example.shared_1.0.0",
		Status:        "enabled",
	}
	writeUXPRegistry(t, base, "PS", plugin)
	writeUXPRegistry(t, base, "XD", plugin)

	found := userExtensions(installedExtensions())
	if len(found) != 1 {
		t.Fatalf("one plugin registered twice became %d entries: %+v", len(found), found)
	}
	hosts := found[0].Hosts
	if len(hosts) != 2 || hosts[0] != "Photoshop" || hosts[1] != "Adobe XD" {
		t.Errorf("its hosts are %v", hosts)
	}
}

func TestInstalledReadsARegistryThatHoldsNoPlugins(t *testing.T) {
	_, base := uxpFixture(t)
	directory := filepath.Join(base, "PluginsInfo", "v1")
	if err := os.MkdirAll(directory, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, "XD.json"), []byte(`{"plugins":[]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	// A backup file is not a registry, and neither is a directory.
	if err := os.WriteFile(filepath.Join(directory, "PS.json.bak"), []byte(`{"plugins":[{"pluginId":"com.old"}]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if found := userExtensions(installedExtensions()); len(found) != 0 {
		t.Errorf("found %+v", found)
	}
}

func TestInstalledKeepsAPluginWithAnUnsafePath(t *testing.T) {
	_, base := uxpFixture(t)
	writeUXPRegistry(t, base, "PS", registryPlugin{
		PluginID:      "com.example.escape",
		Name:          "Escape",
		VersionString: "1.0.0",
		Path:          "$localPlugins/External/../../../../etc",
		Status:        "enabled",
	})

	found := userExtensions(installedExtensions())
	if len(found) != 1 {
		t.Fatalf("found %d extensions", len(found))
	}
	if found[0].Removable {
		t.Error("a plugin outside Plugins/External is removable")
	}
	if err := removeExtension(found[0]); err == nil {
		t.Error("removing it was allowed")
	}
}

func TestInstalledReadsCEPManifests(t *testing.T) {
	isolateUserDirs(t)
	directory, err := cepExtensionsDir()
	if err != nil {
		t.Fatal(err)
	}
	extension := installCEPFixture(t, directory, "com.moonvy.ps.cep", `<?xml version="1.0"?>
<ExtensionManifest Version="6.0" ExtensionBundleId="com.moonvy.ps.cep"
                   ExtensionBundleVersion="0.1.24" ExtensionBundleName="Moonvy 月维（经典）">
  <ExecutionEnvironment>
    <HostList>
      <Host Name="PHSP" Version="[16.0,99.9]"/>
      <Host Name="PHXS" Version="[16.0,99.9]"/>
    </HostList>
  </ExecutionEnvironment>
</ExtensionManifest>`)
	// Anything without a manifest is not an extension, and neither is a
	// hidden file.
	if err := os.MkdirAll(filepath.Join(directory, "not-an-extension"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, ".DS_Store"), []byte("junk"), 0o644); err != nil {
		t.Fatal(err)
	}

	found := userExtensions(installedExtensions())
	if len(found) != 1 {
		t.Fatalf("found %d extensions: %+v", len(found), found)
	}
	got := found[0]
	if got.Kind != KindCEP || got.ID != "com.moonvy.ps.cep" || got.Name != "Moonvy 月维（经典）" {
		t.Errorf("the extension is %+v", got)
	}
	if got.Version != "0.1.24" {
		t.Errorf("its version is %q", got.Version)
	}
	if len(got.Hosts) != 1 || got.Hosts[0] != "Photoshop" {
		t.Errorf("its hosts are %v", got.Hosts)
	}
	if got.Directory != extension || !got.Removable {
		t.Errorf("the extension is %+v", got)
	}
}

func TestRemoveUXPDropsTheRegistrationAndTheDirectory(t *testing.T) {
	_, base := uxpFixture(t)
	directory := installUXPFixture(t, base, "com.example.plugin_1.0.0")
	plugin := registryPlugin{
		PluginID:      "com.example.plugin",
		Name:          "Example",
		VersionString: "1.0.0",
		Path:          "$localPlugins/External/com.example.plugin_1.0.0",
		Status:        "enabled",
	}
	writeUXPRegistry(t, base, "PS", plugin)
	writeUXPRegistry(t, base, "XD", registryPlugin{
		PluginID: "com.example.other", Name: "Other", VersionString: "2.0.0",
		Path: "$localPlugins/External/com.example.other_2.0.0", Status: "enabled",
	}, plugin)

	found := userExtensions(installedExtensions())
	if len(found) != 2 {
		t.Fatalf("found %d extensions: %+v", len(found), found)
	}
	if err := removeExtension(found[0]); err != nil {
		t.Fatalf("removeExtension: %v", err)
	}
	if pathExists(directory) {
		t.Error("the plugin directory is still there")
	}
	registry := filepath.Join(base, "PluginsInfo", "v1")
	if entries := registryEntries(t, filepath.Join(registry, "PS.json")); len(entries) != 0 {
		t.Errorf("PS.json still lists %+v", entries)
	}
	entries := registryEntries(t, filepath.Join(registry, "XD.json"))
	if len(entries) != 1 || entries[0]["pluginId"] != "com.example.other" {
		t.Errorf("XD.json holds %+v, want only the other plugin", entries)
	}
}

func TestRemoveCEPRemovesALinkAndKeepsItsTarget(t *testing.T) {
	isolateUserDirs(t)
	directory, err := cepExtensionsDir()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(directory, 0o755); err != nil {
		t.Fatal(err)
	}
	// A working copy outside the extensions directory, which the extension
	// links to, as a development setup does.
	target := installCEPFixture(t, t.TempDir(), "dist", `<?xml version="1.0"?>
<ExtensionManifest ExtensionBundleId="com.example.dev" ExtensionBundleName="Dev" ExtensionBundleVersion="1.0.0"/>`)
	link := filepath.Join(directory, "com.example.dev")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlinks are unavailable here: %v", err)
	}

	found := userExtensions(installedExtensions())
	if len(found) != 1 || found[0].ID != "com.example.dev" {
		t.Fatalf("found %+v", found)
	}
	if err := removeExtension(found[0]); err != nil {
		t.Fatalf("removeExtension: %v", err)
	}
	if pathExists(link) {
		t.Error("the link is still there")
	}
	if !pathExists(filepath.Join(target, "CSXS", "manifest.xml")) {
		t.Error("the directory the link pointed at was removed")
	}
}

func TestRemoveRefusesAnExtensionOfEveryUser(t *testing.T) {
	isolateUserDirs(t)
	extension := Installed{
		ID:        "com.adobe.ccx.start",
		Name:      "Start",
		Kind:      KindCEP,
		Directory: "/Library/Application Support/Adobe/CEP/extensions/com.adobe.ccx.start-2.16.0",
		Source:    sourceSystem,
		Removable: false,
	}
	if err := removeExtension(extension); err == nil {
		t.Error("an extension of every user was removed")
	}
}

func TestInstalledSortsByName(t *testing.T) {
	_, base := uxpFixture(t)
	plugins := []registryPlugin{}
	for _, name := range []string{"Zed", "apple", "Moon"} {
		id := "com.example." + name
		installUXPFixture(t, base, id+"_1.0.0")
		plugins = append(plugins, registryPlugin{
			PluginID: id, Name: name, VersionString: "1.0.0",
			Path: "$localPlugins/External/" + id + "_1.0.0", Status: "enabled",
		})
	}
	writeUXPRegistry(t, base, "PS", plugins...)

	found := userExtensions(installedExtensions())
	if len(found) != 3 {
		t.Fatalf("found %d extensions", len(found))
	}
	if found[0].Name != "apple" || found[1].Name != "Moon" || found[2].Name != "Zed" {
		t.Errorf("the order is %q, %q, %q", found[0].Name, found[1].Name, found[2].Name)
	}
}

func TestRevealLabelNamesTheFileManagerOfThisSystem(t *testing.T) {
	label := revealLabel()
	if label == "" {
		t.Fatal("there is no label")
	}
	if runtime.GOOS == "windows" && label != "在资源管理器中显示" {
		t.Errorf("the label is %q", label)
	}
	if runtime.GOOS == "darwin" && label != "在访达中显示" {
		t.Errorf("the label is %q", label)
	}
}

func TestInstalledReadsCEPMenuNameWhenBundleNameMissing(t *testing.T) {
	isolateUserDirs(t)
	directory, err := cepExtensionsDir()
	if err != nil {
		t.Fatal(err)
	}
	installCEPFixture(t, directory, "com.lanhuapp.ps", `<?xml version="1.0" encoding="UTF-8"?>
<ExtensionManifest Version="6.0" ExtensionBundleId="com.lanhu.extension" ExtensionBundleVersion="1.0">
  <ExecutionEnvironment>
    <HostList><Host Name="PHSP" Version="11.0"/></HostList>
  </ExecutionEnvironment>
  <DispatchInfoList>
    <Extension Id="com.lanhu.extension">
      <DispatchInfo>
        <UI><Type>Panel</Type><Menu>蓝湖</Menu></UI>
      </DispatchInfo>
    </Extension>
  </DispatchInfoList>
</ExtensionManifest>`)

	found := userExtensions(installedExtensions())
	if len(found) != 1 {
		t.Fatalf("found %d extensions: %+v", len(found), found)
	}
	if got := found[0].Name; got != "蓝湖" {
		t.Errorf("got name %q, want %q", got, "蓝湖")
	}
}

func TestInstalledReadsCEPCDataAndHTMLEntities(t *testing.T) {
	isolateUserDirs(t)
	directory, err := cepExtensionsDir()
	if err != nil {
		t.Fatal(err)
	}
	installCEPFixture(t, directory, "com.codesign.ps", `<?xml version="1.0" encoding="UTF-8"?>
<ExtensionManifest Version="6.0" ExtensionBundleId="com.codesign.extension" ExtensionBundleVersion="1.0">
  <ExecutionEnvironment>
    <HostList><Host Name="PHSP" Version="11.0"/></HostList>
  </ExecutionEnvironment>
  <DispatchInfoList>
    <Extension Id="com.codesign.extension">
      <DispatchInfo>
        <UI><Type>Panel</Type><Menu><![CDATA[CoDesign &amp; Tool]]></Menu></UI>
      </DispatchInfo>
    </Extension>
  </DispatchInfoList>
</ExtensionManifest>`)

	found := userExtensions(installedExtensions())
	if len(found) != 1 {
		t.Fatalf("found %d extensions: %+v", len(found), found)
	}
	if got := found[0].Name; got != "CoDesign & Tool" {
		t.Errorf("got name %q, want %q", got, "CoDesign & Tool")
	}
}

func TestInstalledReadsCEPLocalizedProperties(t *testing.T) {
	isolateUserDirs(t)
	directory, err := cepExtensionsDir()
	if err != nil {
		t.Fatal(err)
	}
	extDir := installCEPFixture(t, directory, "com.example.localized", `<?xml version="1.0" encoding="UTF-8"?>
<ExtensionManifest Version="7.0" ExtensionBundleId="com.example.localized"
                   ExtensionBundleVersion="1.0.0" ExtensionBundleName="%plugin.title">
  <ExecutionEnvironment>
    <HostList><Host Name="PHSP" Version="20.0"/></HostList>
  </ExecutionEnvironment>
</ExtensionManifest>`)

	localeDir := filepath.Join(extDir, "locale", "zh_CN")
	if err := os.MkdirAll(localeDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(localeDir, "messages.properties"), []byte("plugin.title=\\u6708\\u7ef4\\u63d2\\u4ef6\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	found := userExtensions(installedExtensions())
	if len(found) != 1 {
		t.Fatalf("found %d extensions: %+v", len(found), found)
	}
	if got := found[0].Name; got != "月维插件" {
		t.Errorf("got name %q, want %q", got, "月维插件")
	}
}

func TestInstalledPrefersCEPMenuOverTechnicalBundleID(t *testing.T) {
	isolateUserDirs(t)
	directory, err := cepExtensionsDir()
	if err != nil {
		t.Fatal(err)
	}
	installCEPFixture(t, directory, "com.mycorp.plugin", `<?xml version="1.0" encoding="UTF-8"?>
<ExtensionManifest Version="7.0" ExtensionBundleId="com.mycorp.plugin"
                   ExtensionBundleVersion="1.0.0" ExtensionBundleName="com.mycorp.plugin">
  <ExecutionEnvironment>
    <HostList><Host Name="PHSP" Version="20.0"/></HostList>
  </ExecutionEnvironment>
  <DispatchInfoList>
    <Extension Id="com.mycorp.plugin">
      <DispatchInfo>
        <UI><Type>Panel</Type><Menu>企业实用工具</Menu></UI>
      </DispatchInfo>
    </Extension>
  </DispatchInfoList>
</ExtensionManifest>`)

	found := userExtensions(installedExtensions())
	if len(found) != 1 {
		t.Fatalf("found %d extensions", len(found))
	}
	if got := found[0].Name; got != "企业实用工具" {
		t.Errorf("got name %q, want %q", got, "企业实用工具")
	}
}

func TestInstalledReadsUXPNameFromLocalManifestWhenMissingInRegistry(t *testing.T) {
	_, base := uxpFixture(t)
	folder := "com.example.noname_1.0.0"
	pluginDir := filepath.Join(base, "Plugins", "External", folder)
	if err := os.MkdirAll(pluginDir, 0o755); err != nil {
		t.Fatal(err)
	}
	manifest := `{"id":"com.example.noname","version":"1.0.0","name":{"zh_CN":"本地清单插件","default":"Local Plugin"}}`
	if err := os.WriteFile(filepath.Join(pluginDir, "manifest.json"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}

	writeUXPRegistry(t, base, "PS", registryPlugin{
		PluginID:      "com.example.noname",
		Name:          "", // Empty in registry
		VersionString: "1.0.0",
		Path:          "$localPlugins/External/" + folder,
		Status:        "enabled",
	})

	found := userExtensions(installedExtensions())
	if len(found) != 1 {
		t.Fatalf("found %d extensions: %+v", len(found), found)
	}
	if got := found[0].Name; got != "本地清单插件" {
		t.Errorf("got name %q, want %q", got, "本地清单插件")
	}
}
