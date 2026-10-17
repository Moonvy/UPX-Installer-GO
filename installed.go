package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"

	"github.com/egoist/mygo/ui"
)

// Installed is an Adobe extension found on this machine.
type Installed struct {
	ID        string   `json:"id"`
	Name      string   `json:"name"`
	Version   string   `json:"version"`
	Kind      Kind     `json:"kind"`
	Hosts     []string `json:"hosts"`
	Directory string   `json:"directory"`
	Source    string   `json:"source"`
	Enabled   bool     `json:"enabled"`
	Removable bool     `json:"removable"`
	// Icon is the picture the extension draws, read after the list is
	// shown. It is nil when the extension has none.
	Icon *ui.Bitmap `json:"-"`
}

// Where an extension was installed.
const (
	sourceUser   = "user"
	sourceSystem = "system"
)

// installedExtensions returns the Adobe extensions on this machine, by name:
// the UXP plugins each application registered for this user, and the CEP
// extensions of this user and of everyone.
func installedExtensions() []Installed {
	found := append(uxpExtensions(), cepExtensions()...)
	sort.SliceStable(found, func(i, j int) bool {
		first, second := strings.ToLower(found[i].Name), strings.ToLower(found[j].Name)
		if first != second {
			return first < second
		}
		return found[i].ID < found[j].ID
	})
	return found
}

// registryPlugin is one entry of the registry an application keeps of the
// plugins it loads.
type registryPlugin struct {
	PluginID      string `json:"pluginId"`
	Name          string `json:"name"`
	VersionString string `json:"versionString"`
	Path          string `json:"path"`
	Status        string `json:"status"`
}

// uxpExtensions reads what each Adobe application registered, merging the
// plugins several applications share into one entry.
func uxpExtensions() []Installed {
	base, err := uxpBaseDir()
	if err != nil {
		return nil
	}
	registryDirectory := filepath.Join(base, "PluginsInfo", "v1")
	files, err := os.ReadDir(registryDirectory)
	if err != nil {
		return nil
	}
	found := []Installed{}
	at := map[string]int{}
	for _, file := range files {
		if file.IsDir() || filepath.Ext(file.Name()) != ".json" {
			continue
		}
		label, _ := hostMetadata(strings.TrimSuffix(file.Name(), ".json"))
		for _, plugin := range readRegistry(filepath.Join(registryDirectory, file.Name())) {
			if plugin.PluginID == "" {
				continue
			}
			if index, seen := at[plugin.PluginID]; seen {
				found[index] = mergeRegistration(found[index], label, plugin)
				continue
			}
			directory := resolvePluginPath(base, plugin.Path)
			at[plugin.PluginID] = len(found)
			name := resolveUXPInstalledName(plugin, directory)
			found = append(found, Installed{
				ID:        plugin.PluginID,
				Name:      name,
				Version:   plugin.VersionString,
				Kind:      KindUXP,
				Hosts:     []string{label},
				Directory: directory,
				Source:    sourceUser,
				Enabled:   plugin.Status != "disabled",
				Removable: within(filepath.Join(base, "Plugins", "External"), directory),
			})
		}
	}
	return found
}

// resolveUXPInstalledName resolves the user-facing name for an installed UXP plugin.
func resolveUXPInstalledName(plugin registryPlugin, directory string) string {
	var manifestName string
	if directory != "" {
		if data, err := os.ReadFile(filepath.Join(directory, "manifest.json")); err == nil {
			var parsed struct {
				Name        any `json:"name"`
				Entrypoints []struct {
					Label any `json:"label"`
				} `json:"entrypoints"`
			}
			if err := json.Unmarshal(data, &parsed); err == nil {
				manifestName = valueAsText(parsed.Name)
				if manifestName == "" && len(parsed.Entrypoints) > 0 {
					manifestName = valueAsText(parsed.Entrypoints[0].Label)
				}
			}
		}
	}

	name := strings.TrimSpace(plugin.Name)
	if name == "" || name == plugin.PluginID || isTechnicalID(name) {
		if manifestName != "" {
			return manifestName
		}
		if name != "" {
			return name
		}
		return plugin.PluginID
	}

	if manifestName != "" && hasNonASCII(manifestName) && !hasNonASCII(name) {
		return manifestName
	}

	return name
}

func hasNonASCII(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] >= 128 {
			return true
		}
	}
	return false
}

// mergeRegistration adds an application to a plugin several of them load.
func mergeRegistration(extension Installed, label string, plugin registryPlugin) Installed {
	if !contains(extension.Hosts, label) {
		extension.Hosts = append(extension.Hosts, label)
	}
	extension.Enabled = extension.Enabled || plugin.Status != "disabled"
	if (extension.Name == "" || extension.Name == extension.ID || isTechnicalID(extension.Name)) &&
		plugin.Name != "" && plugin.Name != plugin.PluginID && !isTechnicalID(plugin.Name) {
		extension.Name = strings.TrimSpace(plugin.Name)
	}
	return extension
}

// readRegistry reads the plugins an application registered, or none when the
// file is missing or malformed.
func readRegistry(path string) []registryPlugin {
	contents, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var registry struct {
		Plugins []registryPlugin `json:"plugins"`
	}
	if err := json.Unmarshal(contents, &registry); err != nil {
		return nil
	}
	return registry.Plugins
}

// resolvePluginPath turns the path an application registered, such as
// "$localPlugins/External/com.example_1.0.0", into a directory.
func resolvePluginPath(base, registered string) string {
	relative := strings.ReplaceAll(registered, "\\", "/")
	relative = strings.TrimPrefix(relative, "$localPlugins")
	relative = strings.TrimPrefix(relative, "/")
	if relative == "" {
		return ""
	}
	return filepath.Join(base, "Plugins", filepath.FromSlash(relative))
}

// cepExtensions reads the extensions CEP loads: the ones of this user, which
// this app may manage, and the ones installed for everyone.
func cepExtensions() []Installed {
	locations := []struct{ directory, source string }{}
	if directory, err := cepExtensionsDir(); err == nil {
		locations = append(locations, struct{ directory, source string }{directory, sourceUser})
	}
	if directory := cepSystemExtensionsDir(); directory != "" {
		locations = append(locations, struct{ directory, source string }{directory, sourceSystem})
	}
	found := []Installed{}
	for _, location := range locations {
		entries, err := os.ReadDir(location.directory)
		if err != nil {
			continue
		}
		for _, entry := range entries {
			if strings.HasPrefix(entry.Name(), ".") {
				continue
			}
			directory := filepath.Join(location.directory, entry.Name())
			manifest, err := os.ReadFile(filepath.Join(directory, "CSXS", "manifest.xml"))
			if err != nil {
				continue
			}
			manifestStr := string(manifest)
			hosts := parseCEPHosts(manifestStr)
			name := resolveCEPName(manifestStr, directory, entry.Name())
			found = append(found, Installed{
				ID:        valueOr(xmlAttribute(manifestStr, "ExtensionBundleId"), entry.Name()),
				Name:      name,
				Version:   valueOr(xmlAttribute(manifestStr, "ExtensionBundleVersion"), unknownVersion),
				Kind:      KindCEP,
				Hosts:     hostLabels(hosts),
				Directory: directory,
				Source:    location.source,
				Enabled:   true,
				Removable: location.source == sourceUser,
			})
		}
	}
	return found
}

// cepSystemExtensionsDir is where the extensions of every user live, which
// this app shows but does not manage.
func cepSystemExtensionsDir() string {
	switch runtime.GOOS {
	case "darwin":
		return "/Library/Application Support/Adobe/CEP/extensions"
	case "windows":
		if root := os.Getenv("CommonProgramFiles"); root != "" {
			return filepath.Join(root, "Adobe", "CEP", "extensions")
		}
	}
	return ""
}

// hostLabels names the applications of a parsed host list, once each.
func hostLabels(hosts []Host) []string {
	labels := []string{}
	for _, host := range hosts {
		if !contains(labels, host.Label) {
			labels = append(labels, host.Label)
		}
	}
	return labels
}

// installedGroup is the extensions one application loads, or the ones that
// several of them share.
type installedGroup struct {
	Name       string
	Extensions []Installed
	// Icon is the picture of the application the group is named after, or
	// nil for the groups that name none.
	Icon *ui.SVG
}

// groupOrder names the applications the list puts first, before the rest.
var groupOrder = []string{"Photoshop", "Adobe XD", "Illustrator", "After Effects"}

// The groups of the extensions no single application loads.
const (
	sharedGroup  = "多应用"
	unknownGroup = "未知宿主"
)

// groupInstalled puts the extensions in the groups the list shows, by the
// application that loads them.
func groupInstalled(extensions []Installed) []installedGroup {
	byName := map[string]*installedGroup{}
	names := []string{}
	for _, extension := range extensions {
		name := groupName(extension)
		group, ok := byName[name]
		if !ok {
			group = &installedGroup{Name: name, Icon: hostIconOf(name)}
			byName[name] = group
			names = append(names, name)
		}
		group.Extensions = append(group.Extensions, extension)
	}
	sort.SliceStable(names, func(i, j int) bool { return lessGroup(names[i], names[j]) })
	groups := make([]installedGroup, 0, len(names))
	for _, name := range names {
		groups = append(groups, *byName[name])
	}
	return groups
}

// groupName is the group an extension belongs to: the one application that
// loads it, or the ones that share it.
func groupName(extension Installed) string {
	switch len(extension.Hosts) {
	case 0:
		return unknownGroup
	case 1:
		return extension.Hosts[0]
	}
	return sharedGroup
}

// lessGroup orders the groups: the applications this app installs for, then
// the other applications by name, then the shared and unknown extensions.
func lessGroup(first, second string) bool {
	firstRank, secondRank := groupRank(first), groupRank(second)
	if firstRank != secondRank {
		return firstRank < secondRank
	}
	return first < second
}

// groupRank is where a group sits among the others.
func groupRank(name string) int {
	for index, known := range groupOrder {
		if name == known {
			return index
		}
	}
	switch name {
	case sharedGroup:
		return 1000
	case unknownGroup:
		return 1001
	}
	return 100
}

// hostIconOf returns the icon of the application a group is named after, or
// nil for the groups that name no single application.
func hostIconOf(name string) *ui.SVG {
	if icon := hostIconName(name); icon != "" {
		return hostIcon(icon)
	}
	return nil
}

// removeExtension deletes an extension this app manages, together with the
// registrations it wrote for it.
func removeExtension(extension Installed) error {
	if !extension.Removable {
		return errors.New("扩展不在当前用户的 Adobe 目录中，无法移除。")
	}
	if extension.Kind == KindCEP {
		return removeCEP(extension)
	}
	return removeUXP(extension)
}

// removeCEP removes a CEP extension. A link is unlinked on its own, so the
// directory it points at, such as a working copy, is left alone.
func removeCEP(extension Installed) error {
	info, err := os.Lstat(extension.Directory)
	if err != nil {
		return fmt.Errorf("无法找到扩展目录：%w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		if err := os.Remove(extension.Directory); err != nil {
			return fmt.Errorf("无法删除扩展链接：%w", err)
		}
		return nil
	}
	if err := os.RemoveAll(extension.Directory); err != nil {
		return fmt.Errorf("无法删除扩展目录：%w", err)
	}
	return nil
}

// removeUXP drops a plugin from every application's registry, then deletes
// the directory it was installed into.
func removeUXP(extension Installed) error {
	base, err := uxpBaseDir()
	if err != nil {
		return err
	}
	registryDirectory := filepath.Join(base, "PluginsInfo", "v1")
	files, err := os.ReadDir(registryDirectory)
	if err != nil {
		return fmt.Errorf("无法读取插件注册目录：%w", err)
	}
	for _, file := range files {
		if file.IsDir() || filepath.Ext(file.Name()) != ".json" {
			continue
		}
		if err := dropRegistration(filepath.Join(registryDirectory, file.Name()), extension.ID); err != nil {
			return err
		}
	}
	if err := os.RemoveAll(extension.Directory); err != nil {
		return fmt.Errorf("无法删除插件目录：%w", err)
	}
	return nil
}

// dropRegistration removes a plugin from the registry of one application,
// leaving the file alone when it does not list it.
func dropRegistration(registryPath, pluginID string) error {
	fileName := filepath.Base(registryPath)
	contents, err := os.ReadFile(registryPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("无法读取 %s：%w", fileName, err)
	}
	var registry map[string]any
	if err := json.Unmarshal(contents, &registry); err != nil {
		return fmt.Errorf("%s 格式无效：%w", fileName, err)
	}
	plugins, ok := registry["plugins"].([]any)
	if !ok {
		return nil
	}
	kept, removed := []any{}, false
	for _, item := range plugins {
		if entry, ok := item.(map[string]any); ok && entry["pluginId"] == pluginID {
			removed = true
			continue
		}
		kept = append(kept, item)
	}
	if !removed {
		return nil
	}
	registry["plugins"] = kept
	var buffer bytes.Buffer
	encoder := json.NewEncoder(&buffer)
	encoder.SetEscapeHTML(false)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(registry); err != nil {
		return fmt.Errorf("无法生成 %s：%w", fileName, err)
	}
	return writeFileAtomically(registryPath, bytes.TrimRight(buffer.Bytes(), "\n"))
}
