package main

import (
	"archive/zip"
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/egoist/mygo/ui"
)

// Kind names the format of an Adobe package.
type Kind string

// The package formats the app installs.
const (
	KindUXP Kind = "uxp"
	KindCEP Kind = "cep"
	KindXDX Kind = "xdx"
)

// Host is an Adobe application a package declares support for.
type Host struct {
	App        string `json:"app"`
	Label      string `json:"label"`
	MinVersion string `json:"minVersion,omitempty"`
	Icon       string `json:"icon"`
}

// Package is a .ccx, .xdx or .zxp package the user picked.
type Package struct {
	Path            string     `json:"path"`
	FileName        string     `json:"fileName"`
	FileSize        int64      `json:"fileSize"`
	Name            string     `json:"name"`
	Version         string     `json:"version"`
	PluginID        string     `json:"pluginId,omitempty"`
	Hosts           []Host     `json:"hosts"`
	ManifestVersion int64      `json:"manifestVersion,omitempty"`
	Kind            Kind       `json:"kind"`
	Icon            *ui.Bitmap `json:"-"`
}

const manifestSizeLimit = 2 * 1024 * 1024

// asciiLower lowercases ASCII letters and leaves every other byte alone, so
// that offsets in the result still point into the input.
func asciiLower(s string) string {
	b := []byte(s)
	for i, c := range b {
		if c >= 'A' && c <= 'Z' {
			b[i] = c + ('a' - 'A')
		}
	}
	return string(b)
}

// zipEntryName normalizes an archive entry name for suffix matching.
func zipEntryName(name string) string {
	return asciiLower(strings.ReplaceAll(name, "\\", "/"))
}

// hostMetadata returns the label and icon of an Adobe host code. The icon
// names a picture in resources/apps, which the view draws.
func hostMetadata(app string) (string, string) {
	switch strings.ToUpper(app) {
	case "PS", "PHSP", "PHXS":
		return "Photoshop", "photoshop"
	case "ID", "IDSN":
		return "InDesign", "indesign"
	case "PR", "PPRO":
		return "Premiere Pro", "premiere pro"
	case "XD":
		return "Adobe XD", "xd"
	case "AI", "ILST":
		return "Illustrator", "illustrator"
	case "AE", "AEFT":
		return "After Effects", "after effects"
	case "AU", "AUDT":
		return "Audition", "audition"
	case "BR", "KBRG":
		return "Bridge", "bridge"
	case "ACROBAT", "ACRO":
		return "Acrobat Pro", "Acrobat Pro"
	case "AICY":
		return "InCopy", "incopy"
	case "LTRM":
		return "Lightroom", "lightroom"
	case "MUSE":
		return "Muse", "others"
	case "PRLD":
		return "Prelude", "others"
	case "DRWV":
		return "Dreamweaver", "dreamweaver"
	case "FLPR":
		return "Animate", "animate"
	}
	return app, "others"
}

// hostCodes are the application codes hostMetadata names, so that an
// application can be looked up by the label it is shown with.
var hostCodes = []string{
	"PS", "ID", "PR", "XD", "AI", "AE", "AU", "BR", "ACROBAT", "AICY",
	"LTRM", "MUSE", "PRLD", "DRWV", "FLPR",
}

// hostIconName returns the icon of the application shown with a label, or ""
// when the label names none this app knows.
func hostIconName(label string) string {
	for _, code := range hostCodes {
		if name, icon := hostMetadata(code); name == label {
			return icon
		}
	}
	return ""
}

// cleanXMLText unescapes XML/HTML entities, strips CDATA markers and trims whitespace.
func cleanXMLText(s string) string {
	s = strings.TrimSpace(s)
	if strings.HasPrefix(s, "<![CDATA[") && strings.HasSuffix(s, "]]>") {
		s = strings.TrimSuffix(strings.TrimPrefix(s, "<![CDATA["), "]]>")
		s = strings.TrimSpace(s)
	}
	s = html.UnescapeString(s)
	return strings.TrimSpace(s)
}

// preferredLanguageKeys returns prioritized locale codes for localized text resolution.
func preferredLanguageKeys() []string {
	langEnv := os.Getenv("LC_ALL")
	if langEnv == "" {
		langEnv = os.Getenv("LC_MESSAGES")
	}
	if langEnv == "" {
		langEnv = os.Getenv("LANG")
	}
	isChinese := true
	if langEnv != "" {
		lower := strings.ToLower(langEnv)
		if !strings.HasPrefix(lower, "zh") && !strings.Contains(lower, "c.utf-8") && lower != "c" {
			isChinese = false
		}
	}
	if isChinese {
		return []string{"zh_CN", "zh-CN", "zh", "zh_Hans", "zh-Hans", "zh_Hans_CN", "zh_TW", "zh-TW", "default", "en_US", "en-US", "en"}
	}
	return []string{"en_US", "en-US", "en", "default", "zh_CN", "zh-CN", "zh"}
}

// valueAsText reads a manifest value that is either text or a map of
// localized texts, selecting the best matching language.
func valueAsText(value any) string {
	switch v := value.(type) {
	case string:
		return cleanXMLText(v)
	case map[string]any:
		for _, key := range preferredLanguageKeys() {
			if text, ok := v[key].(string); ok && strings.TrimSpace(text) != "" {
				return cleanXMLText(text)
			}
		}
		keys := make([]string, 0, len(v))
		for key := range v {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			if text, ok := v[key].(string); ok && strings.TrimSpace(text) != "" {
				return cleanXMLText(text)
			}
		}
	}
	return ""
}

func stringField(item map[string]any, keys ...string) string {
	for _, key := range keys {
		if text, ok := item[key].(string); ok {
			return text
		}
	}
	return ""
}

// parseHosts reads the host applications out of a UXP manifest.
func parseHosts(manifest map[string]any) []Host {
	value, ok := manifest["host"]
	if !ok {
		value = manifest["hosts"]
	}
	entries := []any{}
	switch v := value.(type) {
	case []any:
		entries = v
	case map[string]any:
		entries = []any{v}
	}
	hosts := []Host{}
	for _, entry := range entries {
		item, ok := entry.(map[string]any)
		if !ok {
			continue
		}
		app := stringField(item, "app", "name")
		if app == "" {
			continue
		}
		label, icon := hostMetadata(app)
		hosts = append(hosts, Host{
			App:        app,
			Label:      label,
			MinVersion: stringField(item, "minVersion", "min_version"),
			Icon:       icon,
		})
	}
	return hosts
}

// xmlAttribute reads the first name="value" of an XML tag, case-insensitively,
// with unescaped XML entities, skipping <?xml ... ?> declarations.
func xmlAttribute(source, name string) string {
	searchSource := source
	if xmlDeclStart := strings.Index(strings.ToLower(source), "<?xml"); xmlDeclStart >= 0 {
		if xmlDeclEnd := strings.Index(source[xmlDeclStart:], "?>"); xmlDeclEnd >= 0 {
			searchSource = source[xmlDeclStart+xmlDeclEnd+2:]
		}
	}

	lowerSource := strings.ToLower(searchSource)
	lowerName := strings.ToLower(name)
	nameLen := len(lowerName)

	pos := 0
	for {
		idx := strings.Index(lowerSource[pos:], lowerName)
		if idx < 0 {
			break
		}
		matchStart := pos + idx
		matchEnd := matchStart + nameLen
		pos = matchEnd

		// Verify word boundary before match
		if matchStart > 0 {
			prev := searchSource[matchStart-1]
			if prev != ' ' && prev != '\t' && prev != '\n' && prev != '\r' && prev != '<' {
				continue
			}
		}

		// Look ahead for '=' with optional whitespace
		rest := strings.TrimLeft(searchSource[matchEnd:], " \t\r\n")
		if !strings.HasPrefix(rest, "=") {
			continue
		}
		rest = strings.TrimLeft(rest[1:], " \t\r\n")
		if len(rest) == 0 {
			continue
		}

		quote := rest[0]
		if quote != '"' && quote != '\'' {
			continue
		}
		rest = rest[1:]
		endQuote := strings.IndexByte(rest, quote)
		if endQuote < 0 {
			continue
		}
		return cleanXMLText(rest[:endQuote])
	}
	return ""
}

// firstAttribute reads the first of several spellings of an XML attribute.
func firstAttribute(tag string, names ...string) string {
	for _, name := range names {
		if value := xmlAttribute(tag, name); value != "" {
			return value
		}
	}
	return ""
}

// parseCEPMinVersion reduces a CEP host version range to its lower bound.
func parseCEPMinVersion(version string) string {
	trimmed := strings.TrimSpace(version)
	trimmed = strings.TrimLeft(trimmed, "[(")
	trimmed = strings.TrimRight(trimmed, "])")
	min, _, _ := strings.Cut(trimmed, ",")
	return strings.TrimSpace(min)
}

// parseCEPHosts reads the host list of a CEP CSXS/manifest.xml.
func parseCEPHosts(manifest string) []Host {
	hosts := []Host{}
	lower := asciiLower(manifest)
	from := 0
	for {
		relative := strings.Index(lower[from:], "<host")
		if relative < 0 {
			break
		}
		start := from + relative
		tag := manifest[start:]
		if end := strings.Index(tag, ">"); end >= 0 {
			tag = tag[:end]
		}
		if app := firstAttribute(tag, "Name", "name"); app != "" {
			if findHost(hosts, app) == nil {
				label, icon := hostMetadata(app)
				hosts = append(hosts, Host{
					App:        app,
					Label:      label,
					MinVersion: parseCEPMinVersion(firstAttribute(tag, "Version", "version")),
					Icon:       icon,
				})
			}
		}
		from = start + 1
	}
	return hosts
}

func findHost(hosts []Host, app string) *Host {
	for i := range hosts {
		if strings.EqualFold(hosts[i].App, app) {
			return &hosts[i]
		}
	}
	return nil
}

// readZipEntryText returns the size of the package and the text of its first
// entry whose name ends with suffix.
func readZipEntryText(filePath, suffix string) (int64, string, error) {
	info, err := os.Stat(filePath)
	if err != nil {
		return 0, "", fmt.Errorf("无法打开安装包：%w", err)
	}
	archive, err := zip.OpenReader(filePath)
	if err != nil {
		return 0, "", errors.New("安装包无效或已损坏。")
	}
	defer archive.Close()

	for _, entry := range archive.File {
		if !strings.HasSuffix(zipEntryName(entry.Name), suffix) {
			continue
		}
		if entry.UncompressedSize64 > manifestSizeLimit {
			return 0, "", errors.New("插件清单异常过大，已停止读取。")
		}
		reader, err := entry.Open()
		if err != nil {
			return 0, "", errors.New("无法读取插件清单。")
		}
		contents, err := io.ReadAll(io.LimitReader(reader, manifestSizeLimit+1))
		reader.Close()
		if err != nil {
			return 0, "", errors.New("插件清单不是有效的 UTF-8 文本。")
		}
		return info.Size(), strings.TrimPrefix(string(contents), "\uFEFF"), nil
	}
	return 0, "", fmt.Errorf("安装包中没有找到 %s。", suffix)
}

// readUXPPackage reads a UXP manifest.json, which .ccx and .xdx packages hold.
func readUXPPackage(filePath string) (Package, error) {
	size, contents, err := readZipEntryText(filePath, "manifest.json")
	if err != nil {
		return Package{}, err
	}
	var manifest map[string]any
	if err := json.Unmarshal([]byte(contents), &manifest); err != nil {
		return Package{}, fmt.Errorf("插件清单格式无效：%w", err)
	}
	version := valueAsText(manifest["version"])
	if version == "" {
		version = "未知"
	}
	name := valueAsText(manifest["name"])
	if name == "" {
		name = packageStem(filePath, "UXP 插件")
	}
	pluginID := valueAsText(manifest["id"])
	manifestVersion, _ := manifest["manifestVersion"].(float64)
	return Package{
		Path:            filePath,
		FileName:        filepath.Base(filePath),
		FileSize:        size,
		Name:            name,
		Version:         version,
		PluginID:        pluginID,
		Hosts:           parseHosts(manifest),
		ManifestVersion: int64(manifestVersion),
		Kind:            KindUXP,
	}, nil
}

// readCEPPackage reads the CSXS/manifest.xml of a .zxp package.
func readCEPPackage(filePath string) (Package, error) {
	size, contents, err := readZipEntryText(filePath, "csxs/manifest.xml")
	if err != nil {
		return Package{}, err
	}
	name := resolveCEPName(contents, "", "")
	if strings.TrimSpace(name) == "" {
		name = packageStem(filePath, "CEP 扩展")
	}
	version := xmlAttribute(contents, "ExtensionBundleVersion")
	if strings.TrimSpace(version) == "" {
		version = "未知"
	}
	manifestVersion := int64(0)
	if major, _, _ := strings.Cut(xmlAttribute(contents, "Version"), "."); major != "" {
		fmt.Sscanf(major, "%d", &manifestVersion)
	}
	return Package{
		Path:            filePath,
		FileName:        filepath.Base(filePath),
		FileSize:        size,
		Name:            name,
		Version:         version,
		PluginID:        xmlAttribute(contents, "ExtensionBundleId"),
		Hosts:           parseCEPHosts(contents),
		ManifestVersion: manifestVersion,
		Kind:            KindCEP,
	}, nil
}

// decodeUnicodeEscape decodes \uXXXX escape sequences in strings.
func decodeUnicodeEscape(s string) string {
	if !strings.Contains(s, `\u`) && !strings.Contains(s, `\U`) {
		return s
	}
	var buf strings.Builder
	for i := 0; i < len(s); {
		if s[i] == '\\' && i+5 < len(s) && (s[i+1] == 'u' || s[i+1] == 'U') {
			r, err := strconv.ParseInt(s[i+2:i+6], 16, 32)
			if err == nil {
				buf.WriteRune(rune(r))
				i += 6
				continue
			}
		}
		buf.WriteByte(s[i])
		i++
	}
	return buf.String()
}

// parseProperties parses a Java .properties format file content.
func parseProperties(content []byte) map[string]string {
	props := make(map[string]string)
	scanner := bufio.NewScanner(bytes.NewReader(content))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, "!") {
			continue
		}
		eqIdx := strings.IndexAny(line, "=:")
		var key, val string
		if eqIdx >= 0 {
			key = strings.TrimSpace(line[:eqIdx])
			val = strings.TrimSpace(line[eqIdx+1:])
		} else {
			key = line
			val = ""
		}
		val = decodeUnicodeEscape(val)
		props[key] = val
	}
	return props
}

// resolveCEPLocalization looks up a localized string key in the extension's locale directory.
func resolveCEPLocalization(directory, key string) string {
	key = strings.TrimPrefix(key, "%")
	if key == "" || directory == "" {
		return key
	}
	searchLangs := preferredLanguageKeys()
	checkedFiles := make(map[string]bool)

	checkFile := func(path string) (string, bool) {
		cleanPath := filepath.Clean(path)
		if checkedFiles[cleanPath] {
			return "", false
		}
		checkedFiles[cleanPath] = true
		data, err := os.ReadFile(cleanPath)
		if err != nil {
			return "", false
		}
		props := parseProperties(data)
		if val, ok := props[key]; ok && strings.TrimSpace(val) != "" {
			return strings.TrimSpace(val), true
		}
		return "", false
	}

	for _, lang := range searchLangs {
		for _, name := range []string{"messages.properties", "manifest.properties"} {
			if val, ok := checkFile(filepath.Join(directory, "locale", lang, name)); ok {
				return val
			}
			if val, ok := checkFile(filepath.Join(directory, "locales", lang, name)); ok {
				return val
			}
		}
	}
	for _, p := range []string{
		filepath.Join(directory, "locale", "messages.properties"),
		filepath.Join(directory, "locales", "messages.properties"),
		filepath.Join(directory, "CSXS", "messages.properties"),
		filepath.Join(directory, "CSXS", "manifest.properties"),
		filepath.Join(directory, "messages.properties"),
	} {
		if val, ok := checkFile(p); ok {
			return val
		}
	}

	localeDir := filepath.Join(directory, "locale")
	if entries, err := os.ReadDir(localeDir); err == nil {
		for _, entry := range entries {
			if entry.IsDir() {
				if val, ok := checkFile(filepath.Join(localeDir, entry.Name(), "messages.properties")); ok {
					return val
				}
			}
		}
	}

	return key
}

// parseCEPMenuNames collects text from all <Menu>...</Menu> elements in manifest.xml.
func parseCEPMenuNames(manifestXML string) []string {
	var menus []string
	lower := strings.ToLower(manifestXML)
	pos := 0
	for {
		start := strings.Index(lower[pos:], "<menu")
		if start < 0 {
			break
		}
		start += pos
		tagClose := strings.Index(manifestXML[start:], ">")
		if tagClose < 0 {
			break
		}
		contentStart := start + tagClose + 1
		end := strings.Index(lower[contentStart:], "</menu>")
		if end < 0 {
			break
		}
		raw := manifestXML[contentStart : contentStart+end]
		cleaned := cleanXMLText(raw)
		if cleaned != "" {
			menus = append(menus, cleaned)
		}
		pos = contentStart + end + 7
	}
	return menus
}

// isTechnicalID reports whether a name looks like an internal technical identifier (e.g. reverse domain).
func isTechnicalID(name string) bool {
	if strings.Contains(name, " ") {
		return false
	}
	lower := strings.ToLower(name)
	for _, p := range []string{"com.", "net.", "org.", "cn.", "io.", "adobe."} {
		if strings.HasPrefix(lower, p) && strings.Count(lower, ".") >= 2 {
			return true
		}
	}
	return false
}

// readPackageJSONName reads the display name or name from a package.json file if present.
func readPackageJSONName(directory string) string {
	data, err := os.ReadFile(filepath.Join(directory, "package.json"))
	if err != nil {
		return ""
	}
	var pkg struct {
		DisplayName string `json:"displayName"`
		Name        string `json:"name"`
	}
	if err := json.Unmarshal(data, &pkg); err != nil {
		return ""
	}
	if strings.TrimSpace(pkg.DisplayName) != "" {
		return strings.TrimSpace(pkg.DisplayName)
	}
	return strings.TrimSpace(pkg.Name)
}

// resolveCEPName resolves the user-facing name of a CEP extension from its manifest and directory.
func resolveCEPName(manifestXML, directory, entryName string) string {
	bundleID := xmlAttribute(manifestXML, "ExtensionBundleId")
	bundleName := xmlAttribute(manifestXML, "ExtensionBundleName")
	menus := parseCEPMenuNames(manifestXML)

	if strings.HasPrefix(bundleName, "%") {
		resolved := resolveCEPLocalization(directory, bundleName)
		if resolved != "" && resolved != strings.TrimPrefix(bundleName, "%") {
			bundleName = resolved
		} else if len(menus) > 0 {
			bundleName = ""
		} else {
			bundleName = strings.TrimPrefix(bundleName, "%")
		}
	}

	var validMenus []string
	for _, m := range menus {
		if strings.HasPrefix(m, "%") {
			m = resolveCEPLocalization(directory, m)
		}
		if m != "" && !strings.HasPrefix(m, "%") {
			validMenus = append(validMenus, m)
		}
	}

	if bundleName != "" && !strings.EqualFold(bundleName, bundleID) && !isTechnicalID(bundleName) {
		return bundleName
	}
	if len(validMenus) > 0 {
		return validMenus[0]
	}
	if bundleName != "" {
		return bundleName
	}
	if directory != "" {
		if pkgName := readPackageJSONName(directory); pkgName != "" {
			return pkgName
		}
	}
	if entryName != "" {
		return entryName
	}
	return bundleID
}

func packageStem(filePath, fallback string) string {
	stem := strings.TrimSuffix(filepath.Base(filePath), filepath.Ext(filePath))
	if stem == "" {
		return fallback
	}
	return stem
}

// readPackage reads the manifest of a package, by its extension.
func readPackage(filePath string) (Package, error) {
	if info, err := os.Stat(filePath); err != nil || info.IsDir() {
		return Package{}, errors.New("找不到所选的安装包。")
	}
	var pkg Package
	var err error
	switch strings.ToLower(filepath.Ext(filePath)) {
	case ".ccx":
		pkg, err = readUXPPackage(filePath)
	case ".xdx":
		pkg, err = readUXPPackage(filePath)
		if err == nil {
			pkg.Kind = KindXDX
		}
	case ".zxp":
		pkg, err = readCEPPackage(filePath)
	default:
		return Package{}, errors.New("文件格式不受支持，请选择扩展名为 .ccx、.xdx 或 .zxp 的安装包。")
	}
	if err != nil {
		return Package{}, err
	}
	pkg.Icon = packageIcon(filePath, pkg.Kind)
	return pkg, nil
}
