package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// isolateUserDirs points the user level Adobe directories at a fresh
// directory, so tests never touch the real ones.
func isolateUserDirs(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	if runtime.GOOS == "windows" {
		t.Setenv("APPDATA", home)
	} else {
		t.Setenv("HOME", home)
	}
	return home
}

func hosts(codes ...string) []Host {
	list := make([]Host, 0, len(codes))
	for _, code := range codes {
		label, icon := hostMetadata(code)
		list = append(list, Host{App: code, Label: label, Icon: icon})
	}
	return list
}

func TestPackageSideloadAppsFindsSupportedHosts(t *testing.T) {
	cases := []struct {
		codes []string
		want  []sideloadApp
	}{
		{[]string{"PS"}, []sideloadApp{sideloadPhotoshop}},
		{[]string{"PHSP"}, []sideloadApp{sideloadPhotoshop}},
		{[]string{"XD"}, []sideloadApp{sideloadXD}},
		{[]string{"AI"}, []sideloadApp{sideloadIllustrator}},
		{[]string{"ILST"}, []sideloadApp{sideloadIllustrator}},
		{[]string{"XD", "PHSP", "AI"}, []sideloadApp{sideloadPhotoshop, sideloadXD, sideloadIllustrator}},
		{[]string{"PR", "ID"}, nil},
	}
	for _, testCase := range cases {
		got := packageSideloadApps(Package{Hosts: hosts(testCase.codes...)})
		if len(got) != len(testCase.want) {
			t.Errorf("%v gave %v, want %v", testCase.codes, got, testCase.want)
			continue
		}
		for i := range got {
			if got[i] != testCase.want[i] {
				t.Errorf("%v gave %v, want %v", testCase.codes, got, testCase.want)
				break
			}
		}
	}
}

func TestRegistryFilesAndMinVersions(t *testing.T) {
	if got := sideloadPhotoshop.registryFile(); got != "PS.json" {
		t.Errorf("Photoshop registry file is %q", got)
	}
	if got := sideloadXD.registryFile(); got != "XD.json" {
		t.Errorf("Adobe XD registry file is %q", got)
	}
	if got := sideloadIllustrator.registryFile(); got != "AI.json" {
		t.Errorf("Illustrator registry file is %q", got)
	}
	pkg := Package{Hosts: hosts("PS")}
	pkg.Hosts[0].MinVersion = "25.0.0"
	if got := hostMinVersion(pkg, sideloadPhotoshop); got != "25.0.0" {
		t.Errorf("hostMinVersion is %q", got)
	}
	if got := hostMinVersion(Package{}, sideloadXD); got != "36.0.0" {
		t.Errorf("default XD min version is %q", got)
	}
}

func TestJoinsHostLabels(t *testing.T) {
	if got := joinAnd(nil); got != "兼容的 Adobe 应用" {
		t.Errorf("joinAnd(nil) is %q", got)
	}
	if got := joinAnd([]string{"Adobe XD"}); got != "Adobe XD" {
		t.Errorf("joinAnd of one is %q", got)
	}
	if got := joinAnd([]string{"Photoshop", "Adobe XD"}); got != "Photoshop 和 Adobe XD" {
		t.Errorf("joinAnd of two is %q", got)
	}
	if got := joinOr([]string{"Photoshop", "Adobe XD", "Illustrator"}); got != "Photoshop、Adobe XD 或 Illustrator" {
		t.Errorf("joinOr of three is %q", got)
	}
}

func TestPrepareRegistryUpdateUpsertsEntries(t *testing.T) {
	root := t.TempDir()
	registry := filepath.Join(root, "XD.json")
	if err := os.WriteFile(registry, []byte(`{"plugins":[{"name":"Old","pluginId":"com.old"}]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	update, err := prepareRegistryUpdate(registry, "com.example.test", map[string]any{
		"pluginId": "com.example.test",
		"name":     "Test",
		"status":   "enabled",
	})
	if err != nil {
		t.Fatalf("prepareRegistryUpdate: %v", err)
	}
	if !update.hadPrevious {
		t.Error("the existing registry file was not recorded for rollback")
	}
	if err := writeFileAtomically(update.path, update.contents); err != nil {
		t.Fatalf("writeFileAtomically: %v", err)
	}
	contents, err := os.ReadFile(registry)
	if err != nil {
		t.Fatal(err)
	}
	var parsed struct {
		Plugins []map[string]any `json:"plugins"`
	}
	if err := json.Unmarshal(contents, &parsed); err != nil {
		t.Fatalf("the registry file is not valid JSON: %v", err)
	}
	if len(parsed.Plugins) != 2 {
		t.Fatalf("registry holds %d plugins", len(parsed.Plugins))
	}
	if parsed.Plugins[1]["pluginId"] != "com.example.test" {
		t.Errorf("the new entry is %+v", parsed.Plugins[1])
	}
}

func TestPrepareRegistryUpdateRejectsBrokenRegistries(t *testing.T) {
	root := t.TempDir()
	registry := filepath.Join(root, "PS.json")
	if err := os.WriteFile(registry, []byte(`not json`), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := prepareRegistryUpdate(registry, "com.example.test", map[string]any{}); err == nil {
		t.Fatal("a malformed registry file was accepted")
	}
}

func TestPrepareRegistryUpdateStartsFromEmptyFiles(t *testing.T) {
	root := t.TempDir()
	entry := map[string]any{"pluginId": "com.example.test"}

	empty := filepath.Join(root, "PS.json")
	if err := os.WriteFile(empty, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	update, err := prepareRegistryUpdate(empty, "com.example.test", entry)
	if err != nil {
		t.Fatalf("an empty registry file was rejected: %v", err)
	}
	if !update.hadPrevious {
		t.Error("an empty registry file was not recorded for rollback")
	}
	if err := writeFileAtomically(update.path, update.contents); err != nil {
		t.Fatal(err)
	}
	if entries := registryEntries(t, empty); len(entries) != 1 {
		t.Errorf("registry holds %d plugins", len(entries))
	}

	missing := filepath.Join(root, "XD.json")
	update, err = prepareRegistryUpdate(missing, "com.example.test", entry)
	if err != nil {
		t.Fatalf("a missing registry file was rejected: %v", err)
	}
	if update.hadPrevious {
		t.Error("a missing registry file was recorded for rollback")
	}
	if err := writeFileAtomically(update.path, update.contents); err != nil {
		t.Fatal(err)
	}
	if entries := registryEntries(t, missing); len(entries) != 1 {
		t.Errorf("registry holds %d plugins", len(entries))
	}

	broken := filepath.Join(root, "AI.json")
	if err := os.WriteFile(broken, []byte(`{"version":1}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := prepareRegistryUpdate(broken, "com.example.test", entry); err == nil {
		t.Error("a registry file without a plugins array was accepted")
	}
}

func TestPreflightRejectsPackagesWithoutASupportedHost(t *testing.T) {
	isolateUserDirs(t)
	preflight := buildSideloadPreflight(Package{
		PluginID: "com.example.test",
		Version:  "1.0.0",
		Hosts:    hosts("PPRO"),
		Kind:     KindUXP,
	})
	if preflight.Supported || preflight.Ready {
		t.Errorf("preflight accepted an unsupported host: %+v", preflight)
	}
	if len(preflight.Issues) == 0 {
		t.Error("preflight reported no issue")
	}
}

func TestPreflightRejectsUnsafePluginIDs(t *testing.T) {
	isolateUserDirs(t)
	preflight := buildSideloadPreflight(Package{
		PluginID: "../escape",
		Version:  "1.0.0",
		Hosts:    hosts("PS"),
		Kind:     KindUXP,
	})
	if preflight.Ready {
		t.Errorf("preflight accepted an unsafe plugin ID: %+v", preflight)
	}
}

func TestPreflightRejectsPackagesWithoutAVersion(t *testing.T) {
	isolateUserDirs(t)
	preflight := buildSideloadPreflight(Package{
		PluginID: "com.example.test",
		Version:  "未知",
		Hosts:    hosts("XD"),
		Kind:     KindUXP,
	})
	if preflight.Ready {
		t.Errorf("preflight accepted a package without a version: %+v", preflight)
	}
}

func TestSideloadInstallsPluginAndRegistry(t *testing.T) {
	isolateUserDirs(t)
	source := filepath.Join(t.TempDir(), "sample.ccx")
	writeZip(t, source, map[string]string{
		"manifest.json": `{"id":"com.example.test","version":"2.0.0","name":"Test","host":[{"app":"PS","minVersion":"25.0.0"}]}`,
		"index.html":    "<html></html>",
	})
	pkg, err := readPackage(source)
	if err != nil {
		t.Fatalf("readPackage: %v", err)
	}
	base, err := uxpBaseDir()
	if err != nil {
		t.Fatalf("uxpBaseDir: %v", err)
	}
	registry := filepath.Join(base, "PluginsInfo", "v1", "PS.json")
	if err := os.MkdirAll(filepath.Dir(registry), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(registry, []byte(`{"plugins":[{"name":"Old","pluginId":"com.old"}]}`), 0o644); err != nil {
		t.Fatal(err)
	}

	if preflight := buildSideloadPreflight(pkg); !preflight.Ready {
		t.Fatalf("preflight is not ready: %+v", preflight.Issues)
	}
	result, err := sideloadPackage(source, pkg)
	if err != nil {
		t.Fatalf("sideloadPackage: %v", err)
	}
	if !result.Success {
		t.Fatalf("sideload failed: %s", result.Message)
	}
	if !result.ActivationPending {
		t.Error("the result does not ask for a restart")
	}
	installed := filepath.Join(base, "Plugins", "External", "com.example.test_2.0.0")
	if !pathExists(filepath.Join(installed, "index.html")) {
		t.Error("the plugin files were not installed")
	}

	entries := registryEntries(t, registry)
	if len(entries) != 2 {
		t.Fatalf("registry holds %d plugins", len(entries))
	}
	entry := entries[1]
	if entry["pluginId"] != "com.example.test" || entry["status"] != "enabled" || entry["type"] != "uxp" {
		t.Errorf("the new entry is %+v", entry)
	}
	if entry["hostMinVersion"] != "25.0.0" || entry["versionString"] != "2.0.0" || entry["name"] != "Test" {
		t.Errorf("the new entry is %+v", entry)
	}
	wantPath := "$localPlugins/External/com.example.test_2.0.0"
	if runtime.GOOS == "windows" {
		wantPath = `$localPlugins\External\com.example.test_2.0.0`
	}
	if entry["path"] != wantPath {
		t.Errorf("the entry path is %v, want %v", entry["path"], wantPath)
	}

	if result, err = sideloadPackage(source, pkg); err != nil || !result.Success {
		t.Fatalf("second sideload: %v %s", err, result.Message)
	}
	if entries = registryEntries(t, registry); len(entries) != 2 {
		t.Errorf("registry holds %d plugins after the second sideload", len(entries))
	}
	leftovers, err := os.ReadDir(filepath.Join(base, "Plugins", "External"))
	if err != nil {
		t.Fatal(err)
	}
	for _, leftover := range leftovers {
		if strings.Contains(leftover.Name(), "openuxp-") {
			t.Errorf("a staging directory was left behind: %s", leftover.Name())
		}
	}
}

func TestSideloadWritesOneRegistryFilePerHost(t *testing.T) {
	isolateUserDirs(t)
	source := filepath.Join(t.TempDir(), "multi.ccx")
	writeZip(t, source, map[string]string{
		"manifest.json": `{"id":"com.example.multi","version":"1.0.0","name":"Multi","host":[{"app":"PS"},{"app":"XD"},{"app":"AI"}]}`,
	})
	pkg, err := readPackage(source)
	if err != nil {
		t.Fatalf("readPackage: %v", err)
	}
	if result, err := sideloadPackage(source, pkg); err != nil || !result.Success {
		t.Fatalf("sideloadPackage: %v %s", err, result.Message)
	}
	base, err := uxpBaseDir()
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"PS.json", "XD.json", "AI.json"} {
		entries := registryEntries(t, filepath.Join(base, "PluginsInfo", "v1", name))
		if len(entries) != 1 || entries[0]["pluginId"] != "com.example.multi" {
			t.Errorf("%s holds %+v", name, entries)
		}
	}
	installed, err := os.ReadDir(filepath.Join(base, "Plugins", "External"))
	if err != nil {
		t.Fatal(err)
	}
	if len(installed) != 1 {
		t.Errorf("the plugin was installed %d times", len(installed))
	}
}

func TestOfficialInstallRedirectsCEPToSideload(t *testing.T) {
	result, err := installPackage("extension.zxp", Package{Kind: KindCEP})
	if err != nil {
		t.Fatalf("installPackage: %v", err)
	}
	if result.Success || !result.CanSideLoad {
		t.Errorf("result is %+v", result)
	}
}

func registryEntries(t *testing.T, path string) []map[string]any {
	t.Helper()
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading %s: %v", path, err)
	}
	var parsed struct {
		Plugins []map[string]any `json:"plugins"`
	}
	if err := json.Unmarshal(contents, &parsed); err != nil {
		t.Fatalf("%s is not valid JSON: %v", path, err)
	}
	return parsed.Plugins
}
