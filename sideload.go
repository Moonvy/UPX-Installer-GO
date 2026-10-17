package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// PermissionIssue explains why a sideload target cannot be written.
type PermissionIssue struct {
	Path    string `json:"path"`
	Message string `json:"message"`
	Hint    string `json:"hint"`
}

// SideloadPreflight reports whether a package can be sideloaded, and where.
type SideloadPreflight struct {
	Supported        bool              `json:"supported"`
	Ready            bool              `json:"ready"`
	PluginDirectory  string            `json:"pluginDirectory"`
	RegistryPath     string            `json:"registryPath"`
	RegistryPaths    []string          `json:"registryPaths"`
	HostLabels       []string          `json:"hostLabels"`
	Issues           []PermissionIssue `json:"issues"`
	Kind             Kind              `json:"kind"`
	DebugModeEnabled bool              `json:"debugModeEnabled"`
	DebugModeKeys    []string          `json:"debugModeKeys"`
}

// sideloadApp is an Adobe application whose user level UXP plugin directory
// the app writes into directly.
type sideloadApp string

const (
	sideloadPhotoshop   sideloadApp = "Photoshop"
	sideloadXD          sideloadApp = "Adobe XD"
	sideloadIllustrator sideloadApp = "Illustrator"
)

// sideloadApps lists the hosts that support sideloading, in report order.
var sideloadApps = []sideloadApp{sideloadPhotoshop, sideloadXD, sideloadIllustrator}

func (a sideloadApp) manifestCode() string {
	switch a {
	case sideloadPhotoshop:
		return "PS"
	case sideloadXD:
		return "XD"
	}
	return "AI"
}

func (a sideloadApp) registryFile() string {
	switch a {
	case sideloadPhotoshop:
		return "PS.json"
	case sideloadXD:
		return "XD.json"
	}
	return "AI.json"
}

func (a sideloadApp) defaultMinVersion() string {
	switch a {
	case sideloadPhotoshop:
		return "22.0.0"
	case sideloadXD:
		return "36.0.0"
	}
	return "26.0.0"
}

// matches reports whether a manifest host code belongs to this app.
func (a sideloadApp) matches(app string) bool {
	switch a {
	case sideloadPhotoshop:
		return strings.EqualFold(app, "PS") || strings.EqualFold(app, "PHSP")
	case sideloadXD:
		return strings.EqualFold(app, "XD")
	}
	return strings.EqualFold(app, "AI") || strings.EqualFold(app, "ILST")
}

func (a sideloadApp) isRunning() bool {
	switch a {
	case sideloadPhotoshop:
		return photoshopRunning()
	case sideloadXD:
		return xdRunning()
	}
	return illustratorRunning()
}

// packageSideloadApps returns the apps a package can be sideloaded into.
func packageSideloadApps(pkg Package) []sideloadApp {
	apps := []sideloadApp{}
	for _, app := range sideloadApps {
		for _, host := range pkg.Hosts {
			if app.matches(host.App) {
				apps = append(apps, app)
				break
			}
		}
	}
	return apps
}

func packageHostLabels(pkg Package) []string {
	return hostLabels(pkg.Hosts)
}

func contains(values []string, value string) bool {
	for _, item := range values {
		if item == value {
			return true
		}
	}
	return false
}

// joinAnd lists labels the way a sentence does, with 和 before the last.
func joinAnd(labels []string) string {
	switch len(labels) {
	case 0:
		return "兼容的 Adobe 应用"
	case 1:
		return labels[0]
	case 2:
		return labels[0] + " 和 " + labels[1]
	}
	return strings.Join(labels[:len(labels)-1], "、") + " 和 " + labels[len(labels)-1]
}

// joinOr lists labels with 或 before the last.
func joinOr(labels []string) string {
	switch len(labels) {
	case 0:
		return "兼容的 Adobe 应用"
	case 1:
		return labels[0]
	case 2:
		return labels[0] + " 或 " + labels[1]
	}
	return strings.Join(labels[:len(labels)-1], "、") + " 或 " + labels[len(labels)-1]
}

func sideloadAppLabels() []string {
	labels := make([]string, 0, len(sideloadApps))
	for _, app := range sideloadApps {
		labels = append(labels, string(app))
	}
	return labels
}

func sideloadUnsupportedMessage() string {
	return fmt.Sprintf("该安装包未声明支持 %s。", joinOr(sideloadAppLabels()))
}

func sideloadUnsupportedHint() string {
	details := make([]string, 0, len(sideloadApps))
	for _, app := range sideloadApps {
		details = append(details, fmt.Sprintf("%s（%s）", app, app.manifestCode()))
	}
	return fmt.Sprintf("侧载目前仅支持 manifest 中宿主为 %s 的插件。", joinOr(details))
}

func sideloadPlatformHint() string {
	return fmt.Sprintf("侧载目前仅支持 macOS 和 Windows 上的 %s。", joinAnd(sideloadAppLabels()))
}

// permissionHint tells the user how to grant write access on their system.
func permissionHint() string {
	if runtime.GOOS == "windows" {
		return "请打开文件或文件夹的“属性”→“安全”→“编辑”，为当前用户授予“修改”和“写入”权限，然后重试。"
	}
	return "请在访达中打开上级目录，选择“显示简介”→“共享与权限”，为当前用户启用“读与写”，然后重试。"
}

// adobeUserDir returns a directory below the current user's Adobe folder.
func adobeUserDir(parts ...string) (string, error) {
	var root string
	switch runtime.GOOS {
	case "darwin":
		home, err := os.UserHomeDir()
		if err != nil {
			return "", errors.New("无法确定当前用户的主目录。")
		}
		root = filepath.Join(home, "Library", "Application Support")
	case "windows":
		root = os.Getenv("APPDATA")
		if root == "" {
			return "", errors.New("无法确定当前用户的 AppData 目录。")
		}
	default:
		return "", errors.New("侧载目前仅支持 macOS 和 Windows。")
	}
	return filepath.Join(append([]string{root, "Adobe"}, parts...)...), nil
}

// cepExtensionsDir is where CEP loads this user's extensions from.
func cepExtensionsDir() (string, error) {
	return adobeUserDir("CEP", "extensions")
}

// uxpBaseDir is the root of the user level UXP plugin directories.
func uxpBaseDir() (string, error) {
	return adobeUserDir("UXP")
}

// invalidIDIssue reports an ID that cannot safely name a directory.
func invalidIDIssue(id, subject, missing, missingHint string) *PermissionIssue {
	if id == "" {
		return &PermissionIssue{Message: missing, Hint: missingHint}
	}
	if safeComponent(id) == id {
		return nil
	}
	return &PermissionIssue{
		Message: subject + "包含不安全的路径字符。",
		Hint:    "为避免目录冲突，侧载仅接受字母、数字、点、连字符和下划线组成的" + subject + "。",
	}
}

// issuesDetails renders preflight issues for the technical details box.
func issuesDetails(issues []PermissionIssue) string {
	parts := []string{}
	for _, issue := range issues {
		if issue.Path == "" {
			parts = append(parts, issue.Message)
			continue
		}
		parts = append(parts, issue.Message+"\n"+issue.Path)
	}
	return strings.Join(parts, "\n\n")
}

// failedSideload is a sideload that stopped before changing anything.
func failedSideload(message, details string) InstallResult {
	return InstallResult{Message: message, Details: details, CanSideLoad: true}
}

// buildSideloadPreflight reports whether a package can be sideloaded.
func buildSideloadPreflight(pkg Package) SideloadPreflight {
	if pkg.Kind == KindCEP {
		return buildCEPPreflight(pkg)
	}
	return buildUXPPreflight(pkg)
}

// checkSideload reads a package and reports whether it can be sideloaded.
func checkSideload(filePath string) (SideloadPreflight, error) {
	pkg, err := readPackage(filePath)
	if err != nil {
		return SideloadPreflight{}, err
	}
	return buildSideloadPreflight(pkg), nil
}

func buildCEPPreflight(pkg Package) SideloadPreflight {
	issues := []PermissionIssue{}
	hostLabels := packageHostLabels(pkg)
	debugModeEnabled, debugModeKeys := cepDebugStatus()
	directory, err := cepExtensionsDir()
	if err != nil {
		issues = append(issues, PermissionIssue{Message: err.Error(), Hint: "CEP 侧载目前仅支持 macOS 和 Windows。"})
		return SideloadPreflight{
			HostLabels:       hostLabels,
			Issues:           issues,
			Kind:             KindCEP,
			DebugModeEnabled: debugModeEnabled,
			DebugModeKeys:    debugModeKeys,
		}
	}
	if issue := invalidIDIssue(pkg.PluginID, "扩展 ID",
		"CEP 清单缺少有效的 ExtensionBundleId。",
		"侧载需要 CSXS/manifest.xml 中存在非空 ExtensionBundleId。"); issue != nil {
		issues = append(issues, *issue)
	}
	if err := probeDirectory(directory); err != nil {
		issues = append(issues, PermissionIssue{
			Path:    directory,
			Message: "CEP 扩展目录权限不足：" + err.Error(),
			Hint:    permissionHint(),
		})
	}
	return SideloadPreflight{
		Supported:        true,
		Ready:            len(issues) == 0,
		PluginDirectory:  directory,
		HostLabels:       hostLabels,
		Issues:           issues,
		Kind:             KindCEP,
		DebugModeEnabled: debugModeEnabled,
		DebugModeKeys:    debugModeKeys,
	}
}

func buildUXPPreflight(pkg Package) SideloadPreflight {
	issues := []PermissionIssue{}
	apps := packageSideloadApps(pkg)
	hostLabels := []string{}
	for _, app := range apps {
		hostLabels = append(hostLabels, string(app))
	}
	base, err := uxpBaseDir()
	if err != nil {
		issues = append(issues, PermissionIssue{Message: err.Error(), Hint: sideloadPlatformHint()})
		return SideloadPreflight{
			HostLabels: hostLabels,
			Issues:     issues,
			Kind:       KindUXP,
		}
	}
	pluginDirectory := filepath.Join(base, "Plugins", "External")
	registryDirectory := filepath.Join(base, "PluginsInfo", "v1")
	registryPaths := []string{}
	for _, app := range apps {
		registryPaths = append(registryPaths, filepath.Join(registryDirectory, app.registryFile()))
	}
	supported := len(apps) > 0
	if !supported {
		issues = append(issues, PermissionIssue{
			Message: sideloadUnsupportedMessage(),
			Hint:    sideloadUnsupportedHint(),
		})
	}
	if issue := invalidIDIssue(pkg.PluginID, "插件 ID",
		"插件清单缺少有效的插件 ID。",
		"侧载需要 manifest.json 中存在非空 id。"); issue != nil {
		issues = append(issues, *issue)
	}
	if pkg.Version == "" || pkg.Version == "未知" {
		issues = append(issues, PermissionIssue{
			Message: "插件清单缺少有效版本号。",
			Hint:    "侧载需要 manifest.json 中存在非空 version。",
		})
	}
	if err := probeDirectory(pluginDirectory); err != nil {
		issues = append(issues, PermissionIssue{
			Path:    pluginDirectory,
			Message: "插件目录权限不足：" + err.Error(),
			Hint:    permissionHint(),
		})
	}
	if err := probeDirectory(registryDirectory); err != nil {
		issues = append(issues, PermissionIssue{
			Path:    registryDirectory,
			Message: "注册目录权限不足：" + err.Error(),
			Hint:    permissionHint(),
		})
	}
	for _, registryPath := range registryPaths {
		if _, err := os.Stat(registryPath); err != nil {
			continue
		}
		file, err := os.OpenFile(registryPath, os.O_RDWR, 0)
		if err != nil {
			issues = append(issues, PermissionIssue{
				Path:    registryPath,
				Message: filepath.Base(registryPath) + " 权限不足：" + err.Error(),
				Hint:    permissionHint(),
			})
			continue
		}
		file.Close()
	}
	preflight := SideloadPreflight{
		Supported:       supported,
		Ready:           supported && len(issues) == 0,
		PluginDirectory: pluginDirectory,
		RegistryPaths:   registryPaths,
		HostLabels:      hostLabels,
		Issues:          issues,
		Kind:            KindUXP,
	}
	if len(registryPaths) > 0 {
		preflight.RegistryPath = registryPaths[0]
	}
	return preflight
}

// registryUpdate is a pending change to a PluginsInfo registry file.
type registryUpdate struct {
	path        string
	fileName    string
	previous    []byte
	hadPrevious bool
	contents    []byte
	backup      string
}

// prepareRegistryUpdate upserts one plugin entry into a registry file.
func prepareRegistryUpdate(registryPath, pluginID string, entry map[string]any) (registryUpdate, error) {
	fileName := filepath.Base(registryPath)
	update := registryUpdate{path: registryPath, fileName: fileName}
	previous, err := os.ReadFile(registryPath)
	switch {
	case err == nil:
		update.previous, update.hadPrevious = previous, true
	case os.IsNotExist(err):
	default:
		return registryUpdate{}, fmt.Errorf("无法读取 %s：%w", fileName, err)
	}
	registry := map[string]any{}
	loaded := false
	if update.hadPrevious && len(update.previous) > 0 {
		if err := json.Unmarshal(update.previous, &registry); err != nil {
			return registryUpdate{}, fmt.Errorf("%s 格式无效，未执行侧载：%w", fileName, err)
		}
		loaded = true
	}
	plugins := []any{}
	if loaded {
		existing, ok := registry["plugins"].([]any)
		if !ok {
			return registryUpdate{}, fmt.Errorf("%s 缺少 plugins 数组，未执行侧载。", fileName)
		}
		plugins = existing
	}
	kept := []any{}
	for _, item := range plugins {
		if existing, ok := item.(map[string]any); ok && existing["pluginId"] == pluginID {
			continue
		}
		kept = append(kept, item)
	}
	registry["plugins"] = append(kept, entry)

	var buffer bytes.Buffer
	encoder := json.NewEncoder(&buffer)
	encoder.SetEscapeHTML(false)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(registry); err != nil {
		return registryUpdate{}, fmt.Errorf("无法生成 %s 注册信息：%w", fileName, err)
	}
	update.contents = bytes.TrimRight(buffer.Bytes(), "\n")
	return update, nil
}

func restoreRegistry(update registryUpdate) {
	if update.hadPrevious {
		writeFileAtomically(update.path, update.previous)
		return
	}
	os.Remove(update.path)
}

func hostMinVersion(pkg Package, app sideloadApp) string {
	for _, host := range pkg.Hosts {
		if app.matches(host.App) && host.MinVersion != "" {
			return host.MinVersion
		}
	}
	return app.defaultMinVersion()
}

func pathExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// stagingDir returns a fresh directory beside the installed plugin, so that
// enabling it is a rename within one filesystem.
func stagingDir(directory string) (string, error) {
	staging, err := os.MkdirTemp(directory, ".openuxp-staging-*")
	if err != nil {
		return "", err
	}
	if err := os.Chmod(staging, 0o755); err != nil {
		os.RemoveAll(staging)
		return "", err
	}
	return staging, nil
}

// sideloadPackage installs a package into the current user's Adobe
// directories, without going through Creative Cloud.
func sideloadPackage(filePath string, pkg Package) (InstallResult, error) {
	if pkg.Kind == KindCEP {
		return sideloadCEP(filePath, pkg)
	}
	return sideloadUXP(filePath, pkg)
}

func sideloadCEP(filePath string, pkg Package) (InstallResult, error) {
	hostSummary := joinAnd(packageHostLabels(pkg))
	preflight := buildCEPPreflight(pkg)
	if !preflight.Ready {
		return failedSideload("侧载前权限检查未通过，请修改权限后重试。", issuesDetails(preflight.Issues)), nil
	}
	pluginID := pkg.PluginID
	directory := preflight.PluginDirectory
	if err := os.MkdirAll(directory, 0o755); err != nil {
		return InstallResult{}, fmt.Errorf("无法创建 CEP 扩展目录：%w", err)
	}
	staging, err := stagingDir(directory)
	if err != nil {
		return InstallResult{}, fmt.Errorf("无法创建临时扩展目录：%w", err)
	}
	if err := extractCEP(filePath, staging); err != nil {
		os.RemoveAll(staging)
		return failedSideload("无法安全解压 CEP 安装包。", err.Error()), nil
	}
	contents, err := os.ReadFile(filepath.Join(staging, "CSXS", "manifest.xml"))
	if err != nil {
		os.RemoveAll(staging)
		return InstallResult{}, fmt.Errorf("无法复核解压后的 CEP 清单：%w", err)
	}
	if xmlAttribute(string(contents), "ExtensionBundleId") != pluginID {
		os.RemoveAll(staging)
		return InstallResult{}, errors.New("安装包在读取过程中发生变化，已停止侧载。")
	}

	folder := safeComponent(pluginID)
	target := filepath.Join(directory, folder)
	nonce := time.Now().UnixNano()
	previous := filepath.Join(directory, fmt.Sprintf(".openuxp-previous-%s-%d", folder, nonce))
	hadTarget := pathExists(target)
	if hadTarget {
		if err := os.Rename(target, previous); err != nil {
			os.RemoveAll(staging)
			return InstallResult{
				Message:     fmt.Sprintf("无法替换现有扩展，请关闭 %s 后重试。", hostSummary),
				Details:     err.Error(),
				CanSideLoad: true,
			}, nil
		}
	}
	if err := os.Rename(staging, target); err != nil {
		if hadTarget {
			os.Rename(previous, target)
		}
		os.RemoveAll(staging)
		return InstallResult{}, fmt.Errorf("无法启用已解压的 CEP 扩展：%w", err)
	}
	if hadTarget {
		os.RemoveAll(previous)
	}

	details := []string{fmt.Sprintf("安装目录：%s", target)}
	keys, debugErr := enableCEPDebugMode()
	if debugErr != nil {
		details = append(details, fmt.Sprintf("扩展文件已安装，但未能完全开启 CEP 调试模式：%v", debugErr))
	} else {
		details = append(details, fmt.Sprintf("已开启 CEP 调试模式：%s", strings.Join(keys, "、")))
	}
	message := fmt.Sprintf("CEP 扩展已安装到用户目录，并已开启调试模式。请重启 %s。", hostSummary)
	if debugErr != nil {
		message = fmt.Sprintf("CEP 扩展已安装，但调试模式未能完全开启。未签名扩展可能无法加载，请重启 %s 后再试。", hostSummary)
	}
	return InstallResult{
		Success:           true,
		Message:           message,
		Details:           strings.Join(details, "\n"),
		ActivationPending: true,
	}, nil
}

func sideloadUXP(filePath string, pkg Package) (InstallResult, error) {
	apps := packageSideloadApps(pkg)
	labels := make([]string, 0, len(apps))
	for _, app := range apps {
		labels = append(labels, string(app))
	}
	hostSummary := joinAnd(labels)
	preflight := buildUXPPreflight(pkg)
	if !preflight.Ready {
		return failedSideload("侧载前权限检查未通过，请修改权限后重试。", issuesDetails(preflight.Issues)), nil
	}
	if len(preflight.RegistryPaths) == 0 {
		return InstallResult{}, errors.New("没有可用于侧载的注册文件。")
	}
	pluginID := pkg.PluginID
	pluginDirectory := preflight.PluginDirectory
	registryDirectory := filepath.Dir(preflight.RegistryPaths[0])
	if err := os.MkdirAll(pluginDirectory, 0o755); err != nil {
		return InstallResult{}, fmt.Errorf("无法创建插件目录：%w", err)
	}
	if err := os.MkdirAll(registryDirectory, 0o755); err != nil {
		return InstallResult{}, fmt.Errorf("无法创建注册目录：%w", err)
	}
	staging, err := stagingDir(pluginDirectory)
	if err != nil {
		return InstallResult{}, fmt.Errorf("无法创建临时插件目录：%w", err)
	}
	if err := extractCCX(filePath, staging); err != nil {
		os.RemoveAll(staging)
		return failedSideload("无法安全解压 CCX 安装包。", err.Error()), nil
	}
	contents, err := os.ReadFile(filepath.Join(staging, "manifest.json"))
	if err != nil {
		os.RemoveAll(staging)
		return InstallResult{}, fmt.Errorf("无法复核解压后的插件清单：%w", err)
	}
	var manifest map[string]any
	if err := json.Unmarshal(bytes.TrimPrefix(contents, []byte("\uFEFF")), &manifest); err != nil {
		os.RemoveAll(staging)
		return InstallResult{}, fmt.Errorf("解压后的插件清单无效：%w", err)
	}
	if valueAsText(manifest["id"]) != pluginID || valueAsText(manifest["version"]) != pkg.Version {
		os.RemoveAll(staging)
		return InstallResult{}, errors.New("安装包在读取过程中发生变化，已停止侧载。")
	}

	folder := safeComponent(pluginID) + "_" + safeComponent(pkg.Version)
	registryRelativePath := "$localPlugins/External/" + folder
	if runtime.GOOS == "windows" {
		registryRelativePath = `$localPlugins\External\` + folder
	}
	nonce := time.Now().UnixNano()
	updates := []registryUpdate{}
	for _, app := range apps {
		entry := map[string]any{
			"hostMinVersion": hostMinVersion(pkg, app),
			"name":           pkg.Name,
			"path":           registryRelativePath,
			"pluginId":       pluginID,
			"status":         "enabled",
			"type":           "uxp",
			"versionString":  pkg.Version,
		}
		update, err := prepareRegistryUpdate(filepath.Join(registryDirectory, app.registryFile()), pluginID, entry)
		if err != nil {
			os.RemoveAll(staging)
			return InstallResult{}, err
		}
		if update.hadPrevious {
			update.backup = filepath.Join(registryDirectory, fmt.Sprintf("%s.openuxp-backup-%d", update.fileName, nonce))
		}
		updates = append(updates, update)
	}

	target := filepath.Join(pluginDirectory, folder)
	previous := filepath.Join(pluginDirectory, fmt.Sprintf(".openuxp-previous-%s-%d", folder, nonce))
	hadTarget := pathExists(target)
	restorePlugin := func() {
		os.RemoveAll(target)
		if hadTarget {
			os.Rename(previous, target)
		}
	}
	if hadTarget {
		if err := os.Rename(target, previous); err != nil {
			os.RemoveAll(staging)
			return InstallResult{
				Message:     fmt.Sprintf("无法替换现有插件，请关闭 %s 后重试。", hostSummary),
				Details:     err.Error(),
				CanSideLoad: true,
			}, nil
		}
	}
	if err := os.Rename(staging, target); err != nil {
		if hadTarget {
			os.Rename(previous, target)
		}
		os.RemoveAll(staging)
		return InstallResult{}, fmt.Errorf("无法启用已解压的插件：%w", err)
	}

	for _, update := range updates {
		if update.backup == "" {
			continue
		}
		if err := writeFileAtomically(update.backup, update.previous); err != nil {
			restorePlugin()
			return InstallResult{}, fmt.Errorf("无法备份 %s，已取消侧载：%w", update.fileName, err)
		}
	}
	for index, update := range updates {
		if err := writeFileAtomically(update.path, update.contents); err != nil {
			for _, written := range updates[:index+1] {
				restoreRegistry(written)
			}
			restorePlugin()
			return InstallResult{
				Message:     fmt.Sprintf("写入 %s 失败，已回滚插件文件。", update.fileName),
				Details:     err.Error(),
				CanSideLoad: true,
			}, nil
		}
	}
	if hadTarget {
		os.RemoveAll(previous)
	}

	details := []string{fmt.Sprintf("安装目录：%s", target)}
	for _, update := range updates {
		details = append(details, fmt.Sprintf("注册文件：%s", update.path))
		if update.backup != "" {
			details = append(details, fmt.Sprintf("注册文件备份：%s", update.backup))
		}
	}
	return InstallResult{
		Success:           true,
		Message:           fmt.Sprintf("插件已通过侧载安装到 %s，请重启 %s。", hostSummary, hostSummary),
		Details:           strings.Join(details, "\n"),
		ActivationPending: true,
	}, nil
}
