package main

import (
	"archive/zip"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeZip creates a zip archive holding entries, whose names are file paths.
func writeZip(t *testing.T, path string, entries map[string]string) {
	t.Helper()
	file, err := os.Create(path)
	if err != nil {
		t.Fatalf("creating %s: %v", path, err)
	}
	archive := zip.NewWriter(file)
	for name, contents := range entries {
		writer, err := archive.Create(name)
		if err != nil {
			t.Fatalf("adding %s: %v", name, err)
		}
		if _, err := writer.Write([]byte(contents)); err != nil {
			t.Fatalf("writing %s: %v", name, err)
		}
	}
	if err := archive.Close(); err != nil {
		t.Fatalf("closing %s: %v", path, err)
	}
	if err := file.Close(); err != nil {
		t.Fatalf("closing %s: %v", path, err)
	}
}

func TestExtractCCXTakesOnlyThePluginRoot(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "sample.ccx")
	writeZip(t, source, map[string]string{
		"wrapper/manifest.json": `{"id":"com.example.test","version":"1.0.0"}`,
		"wrapper/index.js":      "console.log('ok')",
		"package-metadata.txt":  "ignored",
	})
	output := filepath.Join(root, "output")
	if err := extractCCX(source, output); err != nil {
		t.Fatalf("extractCCX: %v", err)
	}
	for _, name := range []string{"manifest.json", "index.js"} {
		if !pathExists(filepath.Join(output, name)) {
			t.Errorf("%s was not extracted", name)
		}
	}
	if pathExists(filepath.Join(output, "package-metadata.txt")) {
		t.Error("a file outside the plugin root was extracted")
	}
}

func TestExtractCEPTakesOnlyThePluginRoot(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "sample.zxp")
	writeZip(t, source, map[string]string{
		"CSXS/manifest.xml":        `<ExtensionManifest ExtensionBundleId="com.example.cep" ExtensionBundleVersion="1.0.0" Version="11.0"/>`,
		"index.html":               "<html></html>",
		"META-INF/signatures.xml":  "<signatures/>",
		"CSXS/icons/extension.svg": "<svg/>",
	})
	output := filepath.Join(root, "output")
	if err := extractCEP(source, output); err != nil {
		t.Fatalf("extractCEP: %v", err)
	}
	for _, name := range []string{"CSXS/manifest.xml", "index.html", "CSXS/icons/extension.svg"} {
		if !pathExists(filepath.Join(output, filepath.FromSlash(name))) {
			t.Errorf("%s was not extracted", name)
		}
	}
}

func TestExtractRejectsEscapingEntries(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "escape.ccx")
	writeZip(t, source, map[string]string{
		"manifest.json":     `{"id":"com.example.test","version":"1.0.0"}`,
		"../../escaped.txt": "no",
	})
	output := filepath.Join(root, "output")
	if err := extractCCX(source, output); err == nil {
		t.Fatal("an archive with an escaping path was accepted")
	}
	if pathExists(filepath.Join(root, "escaped.txt")) {
		t.Error("an entry was written outside the destination")
	}
}

func TestExtractRequiresTheManifest(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "empty.ccx")
	writeZip(t, source, map[string]string{"readme.txt": "nothing here"})
	if err := extractCCX(source, filepath.Join(root, "output")); err == nil {
		t.Fatal("an archive without a manifest was accepted")
	}
}

func TestSafeEntryPath(t *testing.T) {
	for _, name := range []string{"../escape", "/absolute", "..", ""} {
		if _, ok := safeEntryPath(name); ok {
			t.Errorf("safeEntryPath(%q) accepted an unsafe name", name)
		}
	}
	if got, ok := safeEntryPath(`wrapper\manifest.json`); !ok || got != "wrapper/manifest.json" {
		t.Errorf("safeEntryPath normalized to %q, ok=%v", got, ok)
	}
}

func TestSanitizesDirectoryComponents(t *testing.T) {
	if got := safeComponent("com.example_plugin-1"); got != "com.example_plugin-1" {
		t.Errorf("safeComponent kept a safe ID as %q", got)
	}
	if got := safeComponent("../../escape"); got != ".._.._escape" {
		t.Errorf("safeComponent sanitized to %q", got)
	}
}

func TestWriteFileAtomically(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "PS.json")
	if err := os.WriteFile(target, []byte(`{"plugins":[]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	contents := []byte(`{"plugins":[{"pluginId":"com.example"}]}`)
	if err := writeFileAtomically(target, contents); err != nil {
		t.Fatalf("writeFileAtomically: %v", err)
	}
	written, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if string(written) != string(contents) {
		t.Errorf("file holds %q", written)
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.Contains(entry.Name(), "openuxp-temp") {
			t.Errorf("a temporary file was left behind: %s", entry.Name())
		}
	}
}

func TestProbeDirectoryFindsAnExistingParent(t *testing.T) {
	root := t.TempDir()
	if err := probeDirectory(filepath.Join(root, "missing", "deeper")); err != nil {
		t.Fatalf("probeDirectory: %v", err)
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Errorf("probeDirectory left %d entries behind", len(entries))
	}
}
