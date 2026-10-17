package main

import (
	"archive/zip"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

const (
	githubURL = "https://github.com/yArna/OpenUPX-Installer"
	moonvyURL = "https://moonvy.com/?homepage"
)

// packageExtensions are the files the app installs.
var packageExtensions = []string{".ccx", ".xdx", ".zxp"}

// isPackagePath reports whether a path names an installable package.
func isPackagePath(path string) bool {
	lower := strings.ToLower(path)
	for _, extension := range packageExtensions {
		if strings.HasSuffix(lower, extension) {
			return true
		}
	}
	return false
}

// openDropped reads the first installable file out of a drop, and says so
// when the drop held none. If a zip archive is dropped, it inspects inside
// for a .ccx, .xdx or .zxp package to extract and open.
func (a *app) openDropped(paths []string) {
	for _, path := range paths {
		if isPackagePath(path) {
			a.inspect(path)
			return
		}
	}
	for _, path := range paths {
		if strings.EqualFold(filepath.Ext(path), ".zip") {
			if target, err := extractPackageFromZip(path); err == nil {
				a.inspect(target)
				return
			}
		}
	}
	a.pkg = nil
	a.preflight = nil
	a.stage = stageError
	a.message = "请选择一个 .ccx、.xdx 或 .zxp 安装包"
	if len(paths) > 0 {
		a.errorFile = filepath.Base(paths[0])
	}
	for _, path := range paths {
		if strings.EqualFold(filepath.Ext(path), ".zip") {
			a.message = "压缩包中未找到 .ccx、.xdx 或 .zxp 安装包"
			a.errorFile = filepath.Base(path)
			break
		}
	}
	a.details = ""
	a.dirty = true
}

// extractPackageFromZip inspects a zip archive for a .ccx, .xdx or .zxp package
// (or an archive that directly packages a plugin manifest), and extracts it to
// a temporary file ready for installation.
func extractPackageFromZip(zipPath string) (string, error) {
	archive, err := zip.OpenReader(zipPath)
	if err != nil {
		return "", fmt.Errorf("无法打开压缩包：%w", err)
	}
	defer archive.Close()

	var candidates []*zip.File
	var hasUXPManifest, hasCEPManifest bool
	for _, entry := range archive.File {
		if entry.FileInfo().IsDir() {
			continue
		}
		name := entry.Name
		base := filepath.Base(name)
		if strings.HasPrefix(base, "._") || strings.Contains(name, "__MACOSX") {
			continue
		}
		ext := strings.ToLower(filepath.Ext(name))
		if ext == ".ccx" || ext == ".xdx" || ext == ".zxp" {
			candidates = append(candidates, entry)
		}
		norm := zipEntryName(name)
		if strings.HasSuffix(norm, "manifest.json") {
			hasUXPManifest = true
		} else if strings.HasSuffix(norm, "csxs/manifest.xml") {
			hasCEPManifest = true
		}
	}

	if len(candidates) > 0 {
		best := candidates[0]
		for _, c := range candidates {
			if strings.EqualFold(filepath.Ext(c.Name), ".ccx") {
				best = c
				break
			}
		}
		tempDir, err := os.MkdirTemp("", "openuxp-unzipped-*")
		if err != nil {
			return "", fmt.Errorf("无法创建临时目录：%w", err)
		}
		target := filepath.Join(tempDir, filepath.Base(best.Name))
		if err := copyZipEntryToFile(best, target); err != nil {
			os.RemoveAll(tempDir)
			return "", err
		}
		return target, nil
	}

	// If the zip itself directly contains a plugin manifest, wrap it as a package.
	if hasUXPManifest || hasCEPManifest {
		targetExt := ".ccx"
		if hasCEPManifest && !hasUXPManifest {
			targetExt = ".zxp"
		}
		tempDir, err := os.MkdirTemp("", "openuxp-repackaged-*")
		if err != nil {
			return "", fmt.Errorf("无法创建临时目录：%w", err)
		}
		baseName := strings.TrimSuffix(filepath.Base(zipPath), filepath.Ext(zipPath))
		if baseName == "" {
			baseName = "plugin"
		}
		target := filepath.Join(tempDir, baseName+targetExt)
		if err := copyFile(zipPath, target); err != nil {
			os.RemoveAll(tempDir)
			return "", err
		}
		return target, nil
	}

	return "", errors.New("压缩包中未找到 .ccx、.xdx 或 .zxp 安装包。")
}

func copyZipEntryToFile(entry *zip.File, target string) error {
	if entry.UncompressedSize64 > maxExtractedFileSize {
		return fmt.Errorf("安装包文件过大（%s）。", formatBytes(int64(entry.UncompressedSize64)))
	}
	r, err := entry.Open()
	if err != nil {
		return fmt.Errorf("无法读取压缩包内文件：%w", err)
	}
	defer r.Close()

	w, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return fmt.Errorf("无法写入临时安装包：%w", err)
	}
	defer w.Close()

	if _, err := io.Copy(w, r); err != nil {
		return fmt.Errorf("提取安装包失败：%w", err)
	}
	return nil
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return err
	}
	defer out.Close()

	_, err = io.Copy(out, in)
	return err
}

// pickPackage asks the user for a package and reads it.
func (a *app) pickPackage() {
	a.background(func() func() {
		paths, err := mygo.Dialog.Open(mygo.OpenDialogOptions{
			Parent: a.win,
			Title:  "选择 Adobe 插件安装包",
			Filters: []mygo.FileFilter{
				{Name: "Adobe 插件安装包", Extensions: []string{"ccx", "xdx", "zxp", "zip"}},
				{Name: "Adobe UXP 安装包", Extensions: []string{"ccx"}},
				{Name: "Adobe XD 安装包", Extensions: []string{"xdx"}},
				{Name: "Adobe CEP 安装包", Extensions: []string{"zxp"}},
				{Name: "ZIP 压缩包", Extensions: []string{"zip"}},
			},
		})
		if err != nil || len(paths) == 0 {
			return nil
		}
		path := paths[0]
		if strings.EqualFold(filepath.Ext(path), ".zip") {
			if extracted, err := extractPackageFromZip(path); err == nil {
				path = extracted
			}
		}
		return func() { a.inspect(path) }
	})
}

// inspect reads a package and checks whether it can be sideloaded.
func (a *app) inspect(filePath string) {
	mode := a.mode
	a.background(func() func() {
		pkg, err := readPackage(filePath)
		if err != nil {
			return func() {
				a.pkg = nil
				a.preflight = nil
				a.stage = stageError
				a.message = err.Error()
				a.errorFile = filepath.Base(filePath)
				a.details = ""
				a.dirty = true
			}
		}
		var preflight *SideloadPreflight
		if pkg.Kind == KindCEP || mode == modeSideload {
			if checked, err := checkSideload(filePath); err == nil {
				preflight = &checked
			}
		}
		return func() {
			a.pkg = &pkg
			a.preflight = preflight
			a.stage = stageReady
			if pkg.Kind == KindCEP {
				a.mode = modeSideload
			}
			a.message = ""
			a.errorFile = ""
			a.details = ""
			a.activationPending = false
			a.dirty = true
		}
	})
}

// loadPreflight checks whether a package can be sideloaded.
func (a *app) loadPreflight(filePath string) {
	a.background(func() func() {
		preflight, err := checkSideload(filePath)
		return func() {
			if err != nil {
				failed := failedPreflight(err.Error())
				a.preflight = &failed
				return
			}
			a.preflight = &preflight
			a.dirty = true
		}
	})
}

// chooseMode switches between the official installer and sideloading.
func (a *app) chooseMode(mode installMode) {
	if a.mode == mode {
		return
	}
	a.mode = mode
	a.message = ""
	a.details = ""
	a.dirty = true
	if mode == modeOfficial || a.pkg == nil {
		a.preflight = nil
		return
	}
	a.preflight = nil
	a.loadPreflight(a.pkg.Path)
}

// recheckPermissions checks the sideload targets again, after the user
// changed their permissions.
func (a *app) recheckPermissions() {
	if a.pkg == nil {
		return
	}
	a.loadPreflight(a.pkg.Path)
}

// reset returns to the screen that asks for a package.
func (a *app) reset() {
	a.pkg = nil
	a.preflight = nil
	a.stage = stageEmpty
	a.mode = modeSideload
	a.message = ""
	a.errorFile = ""
	a.details = ""
	a.detailsExpanded = false
	a.installingMessage = ""
	a.activationPending = false
	a.dirty = true
}

// startInstall runs the chosen install off the UI thread.
func (a *app) startInstall() {
	if a.pkg == nil || a.stage == stageInstalling {
		return
	}
	if a.mode == modeSideload && (a.preflight == nil || !a.preflight.Ready) {
		return
	}
	pkg := *a.pkg
	action := a.install
	if a.mode == modeSideload {
		action = a.sideload
		a.installingMessage = a.sideloadMessage(pkg)
	} else {
		a.installingMessage = "正在等待 Adobe 官方安装服务完成，最长可能需要 5 分钟…"
	}
	a.stage = stageInstalling
	a.message = ""
	a.details = ""
	a.preflight = nil
	a.activationPending = false
	a.dirty = true

	a.background(func() func() {
		result, err := action(pkg.Path, pkg)
		if err != nil {
			return func() {
				a.stage = stageError
				a.message = err.Error()
				a.details = ""
				a.installingMessage = ""
				a.dirty = true
			}
		}
		var preflight *SideloadPreflight
		if !result.Success && result.CanSideLoad {
			if checked, err := checkSideload(pkg.Path); err == nil {
				preflight = &checked
			}
		}
		return func() {
			if preflight != nil {
				a.preflight = preflight
			}
			a.finish(result)
		}
	})
}

// finish applies the outcome of an install to the screen.
func (a *app) finish(result InstallResult) {
	a.stage = choose(result.Success, stageSuccess, stageError)
	a.message = result.Message
	a.details = result.Details
	a.activationPending = result.ActivationPending
	a.installingMessage = ""
	a.dirty = true
	if result.Success {
		// What the install just wrote belongs in the other column.
		a.scanInstalled()
	}
}

// sideloadMessage says what the sideload is doing while it runs.
func (a *app) sideloadMessage(pkg Package) string {
	if pkg.Kind == KindCEP {
		return "正在解压 CEP 扩展并开启调试模式…"
	}
	return "正在安全解压插件并更新 " + a.sideloadSummary() + " 注册信息…"
}

// sideloadSummary names the applications a sideload installs the plugin for.
func (a *app) sideloadSummary() string {
	if a.preflight != nil && len(a.preflight.HostLabels) > 0 {
		return joinAnd(a.preflight.HostLabels)
	}
	if a.pkg == nil {
		return sideloadAllApps
	}
	if a.pkg.Kind == KindCEP {
		return valueOr(joinAnd(packageHostLabels(*a.pkg)), "兼容的 Adobe 应用")
	}
	return valueOr(joinAnd(sideloadHostLabels(*a.pkg)), sideloadAllApps)
}

// activeHostSummary names the applications the finished install serves.
func (a *app) activeHostSummary() string {
	if a.mode == modeSideload {
		return a.sideloadSummary()
	}
	if a.pkg == nil {
		return "兼容的 Adobe 应用"
	}
	return valueOr(joinAnd(packageHostLabels(*a.pkg)), "兼容的 Adobe 应用")
}

// sideloadAllApps names the applications sideloading supports at all.
const sideloadAllApps = "Photoshop、Adobe XD 或 Illustrator"

// sideloadHostLabels names the applications out of a manifest that support
// sideloading, in the order the manifest declares them.
func sideloadHostLabels(pkg Package) []string {
	labels := []string{}
	seen := map[sideloadApp]bool{}
	for _, host := range pkg.Hosts {
		for _, app := range sideloadApps {
			if !app.matches(host.App) {
				continue
			}
			if !seen[app] {
				seen[app] = true
				labels = append(labels, host.Label)
			}
			break
		}
	}
	return labels
}

// failedPreflight describes a sideload check that could not run.
func failedPreflight(message string) SideloadPreflight {
	return SideloadPreflight{
		Kind:   KindUXP,
		Issues: []PermissionIssue{{Message: message, Hint: "请检查安装包后重试。"}},
	}
}

// scanInstalled reads the extensions on this machine, off the UI thread, so
// that reading them never delays the window or an install. It keeps the list
// it shows while reading, so that a refresh does not flash an empty column.
func (a *app) scanInstalled() {
	a.installedGeneration++
	generation := a.installedGeneration
	a.background(func() func() {
		extensions := installedExtensions()
		return func() {
			if a.installedGeneration != generation {
				return
			}
			a.installed = extensions
			a.installedLoaded = true
			a.installedNotice = ""
			a.dirty = true
			// The icons are read after the list is shown, so that a slow or
			// large one never delays it.
			a.loadInstalledIcons(generation, extensions)
		}
	})
}

// loadInstalledIcons reads the icon of each extension listed, and shows the
// list again with them. It runs off the UI thread, and drops what it read
// when a newer scan has answered in the meantime.
func (a *app) loadInstalledIcons(generation int, extensions []Installed) {
	a.background(func() func() {
		icons := make([]*ui.Bitmap, len(extensions))
		for index, extension := range extensions {
			icons[index] = extensionIcon(extension)
		}
		return func() {
			if a.installedGeneration != generation || len(a.installed) != len(icons) {
				return
			}
			for index := range a.installed {
				a.installed[index].Icon = icons[index]
			}
			a.dirty = true
		}
	})
}

// askToRemove marks an extension for removal, so that its row asks to
// confirm first.
func (a *app) askToRemove(extension Installed) {
	a.confirming = extension.ID
	a.installedNotice = ""
	a.dirty = true
}

// cancelRemove forgets the extension that was to be removed.
func (a *app) cancelRemove() {
	a.confirming = ""
	a.dirty = true
}

// removeInstalled removes an extension, and reads the machine again.
func (a *app) removeInstalled(extension Installed) {
	a.confirming = ""
	a.removing = extension.ID
	a.installedNotice = "正在移除 " + extension.Name + "…"
	a.dirty = true
	a.background(func() func() {
		err := removeExtension(extension)
		extensions := installedExtensions()
		return func() {
			a.removing = ""
			a.installed = extensions
			a.installedLoaded = true
			if err != nil {
				a.installedNotice = err.Error()
			} else {
				a.installedNotice = "已移除 " + extension.Name + "。"
			}
			a.dirty = true
		}
	})
}

// revealInstalled shows an extension in the file manager.
func (a *app) revealInstalled(extension Installed) {
	mygo.Shell.ShowItemInFolder(extension.Directory)
}
