package main

import (
	"os"
	"path/filepath"
	"testing"
)

const cepManifest = `<?xml version="1.0" encoding="UTF-8"?>
<ExtensionManifest Version="11.0" ExtensionBundleId="com.example.cep"
                   ExtensionBundleVersion="2.1.0"
                   ExtensionBundleName="Example CEP">
  <ExecutionEnvironment>
    <HostList>
      <Host Name="PHSP" Version="[23.0,99.9]"/>
      <Host Name="ILST" Version="[26.0,99.9]"/>
      <Host Name="PPRO" Version="[22.0,99.9]"/>
    </HostList>
  </ExecutionEnvironment>
</ExtensionManifest>`

func TestReadsCEPManifest(t *testing.T) {
	if got := xmlAttribute(cepManifest, "ExtensionBundleId"); got != "com.example.cep" {
		t.Errorf("ExtensionBundleId is %q", got)
	}
	if got := xmlAttribute(cepManifest, "ExtensionBundleName"); got != "Example CEP" {
		t.Errorf("ExtensionBundleName is %q", got)
	}
	if got := parseCEPMinVersion("[23.0,99.9]"); got != "23.0" {
		t.Errorf("parseCEPMinVersion is %q", got)
	}
	hosts := parseCEPHosts(cepManifest)
	if len(hosts) != 3 {
		t.Fatalf("found %d hosts", len(hosts))
	}
	if hosts[0].App != "PHSP" || hosts[0].Label != "Photoshop" || hosts[0].MinVersion != "23.0" {
		t.Errorf("first host is %+v", hosts[0])
	}
	if hosts[1].App != "ILST" || hosts[1].Label != "Illustrator" {
		t.Errorf("second host is %+v", hosts[1])
	}
	if hosts[2].Label != "Premiere Pro" {
		t.Errorf("third host is %+v", hosts[2])
	}
}

func TestReadsUXPManifest(t *testing.T) {
	source := filepath.Join(t.TempDir(), "Sample Plugin.ccx")
	writeZip(t, source, map[string]string{
		"manifest.json": `{
			"manifestVersion": 5,
			"id": "com.example.test",
			"name": {"default": "示例插件", "en_US": "Sample Plugin"},
			"version": "1.2.3",
			"host": [{"app": "PS", "minVersion": "25.0.0"}, {"app": "XD"}]
			}`,
		"index.html": "<html></html>",
	})
	pkg, err := readPackage(source)
	if err != nil {
		t.Fatalf("readPackage: %v", err)
	}
	if pkg.Kind != KindUXP {
		t.Errorf("kind is %q", pkg.Kind)
	}
	if pkg.Name != "示例插件" {
		t.Errorf("name is %q", pkg.Name)
	}
	if pkg.Version != "1.2.3" || pkg.PluginID != "com.example.test" {
		t.Errorf("version is %q and id is %q", pkg.Version, pkg.PluginID)
	}
	if pkg.ManifestVersion != 5 {
		t.Errorf("manifest version is %d", pkg.ManifestVersion)
	}
	if len(pkg.Hosts) != 2 || pkg.Hosts[0].Label != "Photoshop" || pkg.Hosts[1].Label != "Adobe XD" {
		t.Errorf("hosts are %+v", pkg.Hosts)
	}
	if pkg.FileName != "Sample Plugin.ccx" || pkg.FileSize == 0 {
		t.Errorf("file is %q of %d bytes", pkg.FileName, pkg.FileSize)
	}
}

func TestReadPackageReadsXDXAsUXP(t *testing.T) {
	source := filepath.Join(t.TempDir(), "plugin.xdx")
	writeZip(t, source, map[string]string{
		"manifest.json": `{"id":"com.example.xd","version":"3.0.0","name":"XD Plugin","host":{"app":"XD"}}`,
	})
	pkg, err := readPackage(source)
	if err != nil {
		t.Fatalf("readPackage: %v", err)
	}
	if pkg.Kind != KindXDX {
		t.Errorf("kind is %q", pkg.Kind)
	}
	if len(pkg.Hosts) != 1 || pkg.Hosts[0].App != "XD" {
		t.Errorf("hosts are %+v", pkg.Hosts)
	}
}

func TestReadPackageReadsZXP(t *testing.T) {
	source := filepath.Join(t.TempDir(), "extension.zxp")
	writeZip(t, source, map[string]string{"CSXS/manifest.xml": cepManifest})
	pkg, err := readPackage(source)
	if err != nil {
		t.Fatalf("readPackage: %v", err)
	}
	if pkg.Kind != KindCEP {
		t.Errorf("kind is %q", pkg.Kind)
	}
	if pkg.Name != "Example CEP" || pkg.Version != "2.1.0" {
		t.Errorf("package is %+v", pkg)
	}
	if pkg.ManifestVersion != 11 {
		t.Errorf("manifest version is %d", pkg.ManifestVersion)
	}
}

func TestReadPackageRejectsUnknownFormats(t *testing.T) {
	source := filepath.Join(t.TempDir(), "plugin.txt")
	writeZip(t, source, map[string]string{"manifest.json": `{}`})
	if _, err := readPackage(source); err == nil {
		t.Fatal("an unsupported extension was accepted")
	}
	if _, err := readPackage(filepath.Join(t.TempDir(), "missing.ccx")); err == nil {
		t.Fatal("a missing file was accepted")
	}
}

func TestReadPackageFallsBackToTheFileName(t *testing.T) {
	source := filepath.Join(t.TempDir(), "Fallback Name.ccx")
	writeZip(t, source, map[string]string{"manifest.json": `{"id":"com.example.test"}`})
	pkg, err := readPackage(source)
	if err != nil {
		t.Fatalf("readPackage: %v", err)
	}
	if pkg.Name != "Fallback Name" {
		t.Errorf("name is %q", pkg.Name)
	}
	if pkg.Version != "未知" {
		t.Errorf("version is %q", pkg.Version)
	}
}

// TestReadsRealPackage exercises a real plugin, which is worth running
// against the packages users actually ship. Point OPENUXP_SAMPLE_CCX at a
// .ccx, .xdx or .zxp to run it.
func TestReadsRealPackage(t *testing.T) {
	source := os.Getenv("OPENUXP_SAMPLE_CCX")
	if source == "" {
		t.Skip("set OPENUXP_SAMPLE_CCX to read a real package")
	}
	pkg, err := readPackage(source)
	if err != nil {
		t.Fatalf("readPackage: %v", err)
	}
	if pkg.Name == "" || pkg.Name == "未知" {
		t.Errorf("the package has no name: %+v", pkg)
	}
	if pkg.Version == "" || pkg.Version == "未知" {
		t.Errorf("the package has no version: %+v", pkg)
	}
	if pkg.PluginID == "" {
		t.Errorf("the package has no plugin ID: %+v", pkg)
	}
	if len(pkg.Hosts) == 0 {
		t.Errorf("the package declares no host: %+v", pkg)
	}
	if pkg.FileSize == 0 {
		t.Error("the package has no size")
	}
	t.Logf("read %s %s (%s) for %+v", pkg.Name, pkg.Version, pkg.Kind, pkg.Hosts)

	isolateUserDirs(t)
	preflight := buildSideloadPreflight(pkg)
	t.Logf("preflight: supported=%v ready=%v issues=%+v", preflight.Supported, preflight.Ready, preflight.Issues)
	if !preflight.Supported {
		t.Errorf("a Photoshop plugin cannot be sideloaded: %+v", preflight.Issues)
	}
	result, err := sideloadPackage(source, pkg)
	if err != nil {
		t.Fatalf("sideloadPackage: %v", err)
	}
	if !result.Success {
		t.Fatalf("sideload failed: %s", result.Message)
	}
	if !pathExists(filepath.Join(preflight.PluginDirectory, safeComponent(pkg.PluginID)+"_"+safeComponent(pkg.Version))) {
		t.Errorf("the plugin was not installed into %s", preflight.PluginDirectory)
	}
	if len(preflight.RegistryPaths) == 0 {
		t.Fatal("the package has no registry file")
	}
	entries := registryEntries(t, preflight.RegistryPaths[0])
	if len(entries) == 0 || entries[len(entries)-1]["pluginId"] != pkg.PluginID {
		t.Errorf("the registry holds %+v", entries)
	}
}
