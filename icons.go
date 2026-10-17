package main

import (
	"archive/zip"
	"encoding/json"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"

	"github.com/egoist/mygo/ui"
)

// maxIconSize is the largest icon the list reads, so that a manifest naming
// something huge cannot stall a scan.
const maxIconSize = 4 << 20

// iconCache keeps the icons already read, so that reading the machine again
// neither opens nor decodes them twice, and the renderer keeps their
// textures.
var iconCache = struct {
	sync.Mutex
	bitmaps map[string]*ui.Bitmap
}{bitmaps: map[string]*ui.Bitmap{}}

// packageIcon returns the icon a package draws, or nil when it has none
// or cannot be decoded.
func packageIcon(filePath string, kind Kind) *ui.Bitmap {
	if filePath == "" {
		return nil
	}
	iconCache.Lock()
	if bitmap, read := iconCache.bitmaps[filePath]; read {
		iconCache.Unlock()
		return bitmap
	}
	iconCache.Unlock()

	bitmap := decodePackageIcon(filePath, kind)
	if bitmap != nil {
		iconCache.Lock()
		iconCache.bitmaps[filePath] = bitmap
		iconCache.Unlock()
	}
	return bitmap
}

// decodePackageIcon extracts and decodes the icon from a package archive.
func decodePackageIcon(filePath string, kind Kind) *ui.Bitmap {
	archive, err := zip.OpenReader(filePath)
	if err != nil {
		return nil
	}
	defer archive.Close()

	marker := "manifest.json"
	if kind == KindCEP {
		marker = "csxs/manifest.xml"
	}

	var manifestEntry *zip.File
	var root string
	for _, entry := range archive.File {
		norm := zipEntryName(entry.Name)
		if strings.HasSuffix(norm, marker) {
			manifestEntry = entry
			if rel, ok := safeEntryPath(entry.Name); ok {
				r := rel
				for range strings.Split(marker, "/") {
					r = path.Dir(r)
				}
				if r != "." && r != "/" {
					root = r
				}
			}
			break
		}
	}
	if manifestEntry == nil || manifestEntry.UncompressedSize64 > manifestSizeLimit {
		return nil
	}

	reader, err := manifestEntry.Open()
	if err != nil {
		return nil
	}
	contents, err := io.ReadAll(io.LimitReader(reader, manifestSizeLimit+1))
	reader.Close()
	if err != nil {
		return nil
	}

	var iconPath string
	if kind == KindCEP {
		iconPath = cepIconPath(string(contents))
	} else {
		var parsed struct {
			Icons []uxpIcon `json:"icons"`
		}
		if err := json.Unmarshal(contents, &parsed); err == nil {
			iconPath = uxpIconPath(parsed.Icons)
		}
	}
	if iconPath == "" {
		return nil
	}
	iconPath = strings.TrimPrefix(strings.TrimPrefix(iconPath, "./"), "/")
	iconPath = strings.ReplaceAll(iconPath, "\\", "/")
	if iconPath == "" {
		return nil
	}

	target := iconPath
	if root != "" {
		target = root + "/" + iconPath
	}
	targetNorm := zipEntryName(target)
	suffixNorm := "/" + zipEntryName(iconPath)

	var iconEntry *zip.File
	for _, entry := range archive.File {
		norm := zipEntryName(entry.Name)
		if norm == targetNorm {
			iconEntry = entry
			break
		}
	}
	if iconEntry == nil {
		for _, entry := range archive.File {
			norm := zipEntryName(entry.Name)
			if norm == zipEntryName(iconPath) || strings.HasSuffix(norm, suffixNorm) {
				iconEntry = entry
				break
			}
		}
	}
	if iconEntry == nil || iconEntry.UncompressedSize64 > maxIconSize {
		return nil
	}

	r, err := iconEntry.Open()
	if err != nil {
		return nil
	}
	data, err := io.ReadAll(io.LimitReader(r, maxIconSize+1))
	r.Close()
	if err != nil {
		return nil
	}
	bitmap, err := ui.DecodeBitmap(data)
	if err != nil {
		return nil
	}
	return bitmap
}

// extensionIcon returns the icon an extension draws, or nil when it has none
// and the list should draw the default one. It reads files, so call it off
// the UI thread.
func extensionIcon(extension Installed) *ui.Bitmap {
	path := extensionIconPath(extension)
	if path == "" {
		return nil
	}
	iconCache.Lock()
	defer iconCache.Unlock()
	if bitmap, read := iconCache.bitmaps[path]; read {
		return bitmap
	}
	bitmap := decodeIcon(path)
	iconCache.bitmaps[path] = bitmap
	return bitmap
}

// decodeIcon reads and decodes an icon, and reports nil when it cannot.
func decodeIcon(path string) *ui.Bitmap {
	info, err := os.Stat(path)
	if err != nil || info.IsDir() || info.Size() > maxIconSize {
		return nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	bitmap, err := ui.DecodeBitmap(data)
	if err != nil {
		return nil
	}
	return bitmap
}

// extensionIconPath returns the file an extension draws, or "" when it names
// none that is there.
func extensionIconPath(extension Installed) string {
	if extension.Directory == "" {
		return ""
	}
	if extension.Kind == KindCEP {
		manifest, err := os.ReadFile(filepath.Join(extension.Directory, "CSXS", "manifest.xml"))
		if err != nil {
			return ""
		}
		return iconIn(extension.Directory, cepIconPath(string(manifest)))
	}
	manifest, err := os.ReadFile(filepath.Join(extension.Directory, "manifest.json"))
	if err != nil {
		return ""
	}
	var parsed struct {
		Icons []uxpIcon `json:"icons"`
	}
	if err := json.Unmarshal(manifest, &parsed); err != nil {
		return ""
	}
	return iconIn(extension.Directory, uxpIconPath(parsed.Icons))
}

// uxpIcon is one entry of the icon list of a UXP manifest.
type uxpIcon struct {
	Path    string   `json:"path"`
	Width   float64  `json:"width"`
	Theme   []string `json:"theme"`
	Species []string `json:"species"`
}

// uxpIconPath picks the icon a UXP manifest lists for the plugin list, drawn
// for a dark interface, which is the one this app shows.
func uxpIconPath(icons []uxpIcon) string {
	best, found := uxpIcon{}, false
	for _, icon := range icons {
		if icon.Path == "" {
			continue
		}
		if !found || betterUXPIcon(icon, best) {
			best, found = icon, true
		}
	}
	if !found {
		return ""
	}
	return best.Path
}

// betterUXPIcon reports whether an icon suits the list better than another:
// one the plugin list asks for, drawn for a dark interface, and the larger.
func betterUXPIcon(icon, than uxpIcon) bool {
	if iconList, thanList := contains(icon.Species, "pluginList"), contains(than.Species, "pluginList"); iconList != thanList {
		return iconList
	}
	if iconDark, thanDark := forDarkTheme(icon), forDarkTheme(than); iconDark != thanDark {
		return iconDark
	}
	return icon.Width > than.Width
}

// forDarkTheme reports whether an icon is drawn for a dark interface, whose
// artwork is the light one.
func forDarkTheme(icon uxpIcon) bool {
	return contains(icon.Theme, "dark") || contains(icon.Theme, "darkest")
}

// cepIcon is one icon a CEP manifest lists: the kind of interface it is
// drawn for, and the file.
type cepIcon struct {
	Type string
	Path string
}

// cepIconPath picks the icon to draw, preferring the one a manifest draws
// for a dark interface: its "Normal" icon is the one for a light one, whose
// artwork is dark and would be lost here.
func cepIconPath(manifest string) string {
	icons := cepIcons(manifest)
	for _, wanted := range []string{"DarkNormal", "Normal"} {
		for _, icon := range icons {
			if icon.Type == wanted && icon.Path != "" {
				return icon.Path
			}
		}
	}
	for _, icon := range icons {
		if icon.Path != "" {
			return icon.Path
		}
	}
	return ""
}

// cepIcons reads the icons a CEP manifest lists.
func cepIcons(manifest string) []cepIcon {
	open := strings.Index(manifest, "<Icons")
	if open < 0 {
		return nil
	}
	block := manifest[open:]
	// Past the opening tag, since "<Icons>" starts with "<Icon" too.
	openEnd := strings.Index(block, ">")
	if openEnd < 0 {
		return nil
	}
	block = block[openEnd+1:]
	if close := strings.Index(block, "</Icons>"); close >= 0 {
		block = block[:close]
	}
	icons := []cepIcon{}
	for {
		start := strings.Index(block, "<Icon")
		if start < 0 {
			return icons
		}
		block = block[start:]
		tagEnd := strings.Index(block, ">")
		if tagEnd < 0 {
			return icons
		}
		value := block[tagEnd+1:]
		valueEnd := strings.Index(value, "</Icon>")
		if valueEnd < 0 {
			return icons
		}
		icons = append(icons, cepIcon{
			Type: xmlAttribute(block[:tagEnd+1], "Type"),
			Path: strings.TrimSpace(value[:valueEnd]),
		})
		block = value[valueEnd:]
	}
}

// iconIn resolves an icon a manifest names beside it, and reports "" when
// nothing is there.
func iconIn(directory, named string) string {
	if named == "" {
		return ""
	}
	relative := strings.TrimPrefix(strings.TrimPrefix(named, "./"), "/")
	path := filepath.Join(directory, filepath.FromSlash(relative))
	if info, err := os.Stat(path); err != nil || info.IsDir() {
		return ""
	}
	return path
}
