package main

import (
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"testing"

	"github.com/egoist/mygo/ui"
)

// The view runs without a window in tests, which click and type as a user
// would.

// allowedPreflight is a sideload check that passed.
func allowedPreflight() SideloadPreflight {
	return SideloadPreflight{
		Supported:       true,
		Ready:           true,
		PluginDirectory: "/Users/test/Library/Application Support/Adobe/UXP/Plugins/External",
		RegistryPath:    "/Users/test/Library/Application Support/Adobe/UXP/PluginsInfo/v1/PS.json",
		RegistryPaths:   []string{"/Users/test/Library/Application Support/Adobe/UXP/PluginsInfo/v1/PS.json"},
		HostLabels:      []string{"Photoshop"},
		Kind:            KindUXP,
	}
}

// blockedPreflight is a sideload check that needs file permissions.
func blockedPreflight() SideloadPreflight {
	return SideloadPreflight{
		Supported:       true,
		PluginDirectory: "/Library/Application Support/Adobe/UXP/Plugins/External",
		HostLabels:      []string{"Photoshop"},
		Issues: []PermissionIssue{{
			Path:    "/Library/Application Support/Adobe/UXP/Plugins/External",
			Message: "插件目录权限不足：permission denied",
			Hint:    "请在访达中打开上级目录，为当前用户启用“读与写”，然后重试。",
		}},
		Kind: KindUXP,
	}
}

// samplePackage is the plugin the window shows in the tests.
func samplePackage(kind Kind) *Package {
	host := Host{App: "PS", Label: "Photoshop", MinVersion: "22.0.0", Icon: "photoshop"}
	if kind == KindCEP {
		host = Host{App: "PHSP", Label: "Photoshop", MinVersion: "23.0", Icon: "photoshop"}
	}
	pluginID := "com.moonvy.photoshop"
	if kind == KindCEP {
		pluginID = "com.example.cep"
	}
	return &Package{
		Path:            "/tmp/Moonvy-Photoshop-Plugin-v1.2.8.ccx",
		FileName:        "Moonvy-Photoshop-Plugin-v1.2.8.ccx",
		FileSize:        555965,
		Name:            "Moonvy 月维",
		Version:         "1.2.8",
		PluginID:        pluginID,
		Hosts:           []Host{host},
		ManifestVersion: 5,
		Kind:            kind,
	}
}

// readyApp is an app showing a package, with a fixed environment so that the
// tests read the same on every machine.
func readyApp(kind Kind, preflight SideloadPreflight) *app {
	a := newApp()
	a.environment = &Environment{
		Platform:           runtime.GOOS,
		InstallerFound:     true,
		InstallerPath:      "/Library/.../UnifiedPluginInstallerAgent",
		CreativeCloudFound: true,
	}
	a.pkg = samplePackage(kind)
	a.preflight = &preflight
	a.stage = stageReady
	return a
}

// tall is a tester big enough to show whole columns, so that a test reads
// every text rather than only the ones above the fold.
func tall(a *app) *ui.Tester {
	return ui.NewTester(a.view, 1120, 1400)
}

func TestDropZoneAsksForAPackage(t *testing.T) {
	a := newApp()
	tt := tall(a)
	if !tt.HasText("拖放 CCX / XDX / ZXP 文件到这里") {
		t.Errorf("the drop zone does not ask for a package: %q", tt.Texts())
	}
	if _, ok := tt.Find("选择文件"); !ok {
		t.Errorf("the drop zone has no button to browse: %q", tt.Texts())
	}
	if tt.HasText("立即安装") || tt.HasText("立即安装") {
		t.Error("the drop zone offers an install action")
	}
	if !tt.HasText("正在检测 Adobe 环境…") {
		t.Errorf("the drop zone does not report the environment: %q", tt.Texts())
	}
}

func TestPackageScreenShowsTheManifest(t *testing.T) {
	a := readyApp(KindUXP, allowedPreflight())
	tt := tall(a)
	for _, text := range []string{
		"准备安装",
		"Moonvy 月维",
		"Moonvy-Photoshop-Plugin-v1.2.8.ccx",
		"1.2.8",
		"UXP · Manifest v5",
		"com.moonvy.photoshop",
		"插件 ID",
		"兼容应用",
		"Photoshop",
		"≥ 22.0.0",
		"侧载安装",
		"官方安装",
		"立即安装",
	} {
		if !tt.HasText(text) {
			t.Errorf("%q is missing: %q", text, tt.Texts())
		}
	}
}

func TestSideloadPanelShowsWhereThePluginGoes(t *testing.T) {
	a := readyApp(KindUXP, allowedPreflight())
	tt := tall(a)
	if err := tt.Click("展开"); err != nil {
		t.Fatalf("clicking expand: %v", err)
	}
	tt.Frame()
	for _, text := range []string{
		"侧载环境已准备就绪",
		"插件目录",
		a.preflight.PluginDirectory,
		"注册文件",
		a.preflight.RegistryPaths[0],
		"侧载安装会创建可恢复的 Photoshop 注册文件备份",
	} {
		if !tt.HasText(text) {
			t.Errorf("%q is missing: %q", text, tt.Texts())
		}
	}
}

func TestBlockedSideloadListsThePermissionsToFix(t *testing.T) {
	a := readyApp(KindUXP, blockedPreflight())
	tt := tall(a)
	if !tt.HasText("侧载安装需要文件权限") {
		t.Errorf("the panel does not explain the block: %q", tt.Texts())
	}
	if !tt.HasText("插件目录权限不足：permission denied") {
		t.Errorf("the issue is missing: %q", tt.Texts())
	}
	if _, ok := tt.Find("重新检查权限"); !ok {
		t.Errorf("there is no way to check again: %q", tt.Texts())
	}
	if err := tt.Click("立即安装"); err != nil {
		t.Fatalf("clicking the action: %v", err)
	}
	tt.Frame()
	if a.stage != stageReady {
		t.Errorf("a blocked sideload started anyway, stage is %v", a.stage)
	}
}

func TestInstallModeSwitchesToAdobe(t *testing.T) {
	a := readyApp(KindUXP, allowedPreflight())
	tt := tall(a)
	if err := tt.Click("官方安装"); err != nil {
		t.Fatalf("clicking the official option: %v", err)
	}
	tt.Frame()
	if a.mode != modeOfficial {
		t.Fatalf("the mode is %v, want the official installer", a.mode)
	}
	// The action carries one name whichever method it uses.
	if !tt.HasText("立即安装") {
		t.Errorf("the action is missing: %q", tt.Texts())
	}
	if tt.HasText("侧载环境已准备就绪") {
		t.Error("the sideload panel is still shown")
	}
	if !tt.HasText("官方安装由 Adobe Unified Plugin Installer Agent 完成") {
		t.Errorf("the trust note is missing: %q", tt.Texts())
	}
	if err := tt.Click("侧载安装"); err != nil {
		t.Fatalf("clicking the sideload option: %v", err)
	}
	tt.Frame()
	if a.mode != modeSideload {
		t.Errorf("the mode is %v, want sideloading", a.mode)
	}
	if !tt.HasText("侧载安装会创建可恢复的 Photoshop 注册文件备份") {
		t.Errorf("the trust note did not follow the method: %q", tt.Texts())
	}
}

func TestSideloadingIsTheDefaultMethod(t *testing.T) {
	a := readyApp(KindUXP, allowedPreflight())
	if a.mode != modeSideload {
		t.Fatalf("a package opens in mode %v, want sideloading", a.mode)
	}
	tt := tall(a)
	if err := tt.Click("展开"); err != nil {
		t.Fatalf("clicking expand: %v", err)
	}
	tt.Frame()
	if !tt.HasText("侧载环境已准备就绪") {
		t.Errorf("the sideload panel is not shown when expanded: %q", tt.Texts())
	}
	a.reset()
	if a.mode != modeSideload {
		t.Errorf("the mode is %v after a reset, want sideloading", a.mode)
	}
}

func TestCEPPackageOffersOnlySideloading(t *testing.T) {
	preflight := allowedPreflight()
	preflight.Kind = KindCEP
	preflight.DebugModeKeys = []string{"CSXS.6", "CSXS.7"}
	preflight.RegistryPaths = nil
	a := readyApp(KindCEP, preflight)
	tt := tall(a)
	if !tt.HasText("CEP 侧载") || !tt.HasText("立即安装") {
		t.Errorf("a CEP package cannot be sideloaded: %q", tt.Texts())
	}
	if tt.HasText("官方安装") {
		t.Errorf("a CEP package offers the official installer: %q", tt.Texts())
	}
	if !tt.HasText("调试模式") {
		t.Errorf("the panel does not report the debug mode: %q", tt.Texts())
	}
	if a.mode != modeSideload {
		t.Errorf("a CEP package chose mode %v", a.mode)
	}
}

func TestInstallShowsTheResult(t *testing.T) {
	a := readyApp(KindUXP, allowedPreflight())
	called := 0
	a.install = func(path string, pkg Package) (InstallResult, error) {
		called++
		if path != pkg.Path || pkg.PluginID != "com.moonvy.photoshop" {
			t.Errorf("the action got %s and %+v", path, pkg)
		}
		return InstallResult{
			Success: true,
			Message: "插件已成功安装。",
			Details: "安装目录：/Users/test/Adobe/UXP/Plugins/External/com.example\n" +
				"注册文件：/Users/test/Adobe/UXP/PluginsInfo/v1/PS.json",
		}, nil
	}
	tt := tall(a)
	if err := tt.Click("官方安装"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if err := tt.Click("立即安装"); err != nil {
		t.Fatalf("clicking install: %v", err)
	}
	tt.Frame()
	if called != 1 {
		t.Fatalf("the install ran %d times", called)
	}
	if a.stage != stageSuccess {
		t.Fatalf("the stage is %v, want success", a.stage)
	}
	if !tt.HasText("安装完成") {
		t.Errorf("the screen does not report the install: %q", tt.Texts())
	}
	if !tt.HasText("已经安装到 Photoshop，打开 Photoshop 即可使用。") {
		t.Errorf("the screen does not say where it went: %q", tt.Texts())
	}
	// The install's own report is laid out as a label over its value, so
	// that a long path wraps rather than running off the panel.
	for _, text := range []string{"安装信息", "安装目录", "注册文件",
		"/Users/test/Adobe/UXP/Plugins/External/com.example"} {
		if !tt.HasText(text) {
			t.Errorf("%q is missing from the install information: %q", text, tt.Texts())
		}
	}
	if err := tt.Click("安装另一个插件"); err != nil {
		t.Fatalf("clicking reset: %v", err)
	}
	tt.Frame()
	if a.stage != stageEmpty || !tt.HasText("拖放 CCX / XDX / ZXP 文件到这里") {
		t.Errorf("the reset did not return to the drop zone: %q", tt.Texts())
	}
}

func TestSideloadAsksForARestart(t *testing.T) {
	a := readyApp(KindUXP, allowedPreflight())
	a.sideload = func(string, Package) (InstallResult, error) {
		return InstallResult{
			Success:           true,
			Message:           "插件已通过侧载安装到 Photoshop，请重启 Photoshop。",
			ActivationPending: true,
		}, nil
	}
	tt := tall(a)
	if err := tt.Click("立即安装"); err != nil {
		t.Fatalf("clicking sideload: %v", err)
	}
	tt.Frame()
	if a.stage != stageSuccess || !a.activationPending {
		t.Fatalf("the sideload ended in stage %v, activation pending %v", a.stage, a.activationPending)
	}
	if !tt.HasText("安装完成") {
		t.Errorf("the screen does not report the install: %q", tt.Texts())
	}
	if !tt.HasText("已经安装到 Photoshop，需要重新启动 Photoshop。") {
		t.Errorf("the screen does not ask for a restart: %q", tt.Texts())
	}
}

func TestFailedInstallOffersSideloading(t *testing.T) {
	isolateUserDirs(t)
	source := filepath.Join(t.TempDir(), "sample.ccx")
	writeZip(t, source, map[string]string{
		"manifest.json": `{"id":"com.example.test","version":"2.0.0","name":"Test","host":[{"app":"PS"}]}`,
		"index.html":    "<html></html>",
	})
	// A real package, so that the sideload check the failure triggers reads
	// something that is there.
	a := readyApp(KindUXP, allowedPreflight())
	pkg, err := readPackage(source)
	if err != nil {
		t.Fatalf("readPackage: %v", err)
	}
	a.pkg = &pkg
	a.install = func(string, Package) (InstallResult, error) {
		return InstallResult{
			Message:     "未找到 Adobe Unified Plugin Installer Agent。请先安装或更新 Creative Cloud Desktop。",
			CanSideLoad: true,
		}, nil
	}
	tt := tall(a)
	if err := tt.Click("官方安装"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if err := tt.Click("立即安装"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if a.stage != stageError {
		t.Fatalf("the stage is %v, want an error", a.stage)
	}
	if !tt.HasText("安装未完成") || !tt.HasText("未找到 Adobe Unified Plugin Installer Agent") {
		t.Errorf("the error is not shown: %q", tt.Texts())
	}
	// The failure left sideloading open, and its check has run.
	if a.preflight == nil || !a.preflight.Ready {
		t.Fatalf("sideloading is not offered after the failure: %+v", a.preflight)
	}
	if err := tt.Click("侧载安装"); err != nil {
		t.Fatalf("clicking the sideload option: %v", err)
	}
	tt.Frame()
	if !tt.HasText("侧载环境已准备就绪") {
		t.Errorf("the sideload panel is missing after the failure: %q", tt.Texts())
	}
	if err := tt.Click("立即安装"); err != nil {
		t.Fatalf("clicking the action: %v", err)
	}
	tt.Frame()
	if a.stage != stageSuccess {
		t.Errorf("the sideload ended in stage %v", a.stage)
	}
}

func TestUnreadablePackageShowsTheErrorScreen(t *testing.T) {
	a := newApp()
	a.inspect("/tmp/not-a-package.ccx")
	if a.stage != stageError || a.pkg != nil {
		t.Fatalf("reading a missing package ended in stage %v with %v", a.stage, a.pkg)
	}
	tt := tall(a)
	if !tt.HasText("无法读取安装包") || !tt.HasText("找不到所选的安装包。") || !tt.HasText("not-a-package.ccx") {
		t.Errorf("the error screen is incomplete: %q", tt.Texts())
	}
	if _, ok := tt.Find("重新选择"); !ok {
		t.Errorf("there is no way to start over: %q", tt.Texts())
	}
}

func TestDroppingSomethingElseAsksForAPackage(t *testing.T) {
	a := newApp()
	a.openDropped([]string{"/tmp/notes.txt"})
	if a.stage != stageError {
		t.Fatalf("the stage is %v", a.stage)
	}
	tt := tall(a)
	if !tt.HasText("请选择一个 .ccx、.xdx 或 .zxp 安装包") {
		t.Errorf("the drop is not explained: %q", tt.Texts())
	}
}

func TestDroppingAPackageReadsIt(t *testing.T) {
	isolateUserDirs(t)
	source := t.TempDir() + "/dropped.ccx"
	writeZip(t, source, map[string]string{
		"manifest.json": `{"id":"com.example.dropped","version":"3.1.0","name":"Dropped","host":[{"app":"PS"}]}`,
		"index.html":    "<html></html>",
	})
	a := newApp()
	a.openDropped([]string{"/tmp/notes.txt", source})
	if a.stage != stageReady || a.pkg == nil {
		t.Fatalf("the dropped package was not read: stage %v, package %v", a.stage, a.pkg)
	}
	if a.pkg.Name != "Dropped" || a.pkg.Version != "3.1.0" {
		t.Errorf("the package is %+v", a.pkg)
	}
	if a.preflight == nil || !a.preflight.Ready {
		t.Fatalf("the sideload check did not pass: %+v", a.preflight)
	}
	tt := tall(a)
	if !tt.HasText("Dropped") || !tt.HasText("立即安装") {
		t.Errorf("the package screen is incomplete: %q", tt.Texts())
	}
}

func TestDroppingZipContainingPackageExtractsAndOpensIt(t *testing.T) {
	isolateUserDirs(t)
	dir := t.TempDir()
	ccxPath := filepath.Join(dir, "nested.ccx")
	writeZip(t, ccxPath, map[string]string{
		"manifest.json": `{"id":"com.example.nested","version":"2.0.0","name":"Nested Plugin","host":[{"app":"PS"}]}`,
		"index.html":    "<html></html>",
	})
	ccxBytes, err := os.ReadFile(ccxPath)
	if err != nil {
		t.Fatal(err)
	}
	zipPath := filepath.Join(dir, "download.zip")
	writeZipArchive(t, zipPath, map[string]string{
		"readme.txt": "Instructions here",
	}, map[string][]byte{
		"nested.ccx": ccxBytes,
	})

	a := newApp()
	a.openDropped([]string{zipPath})
	if a.stage != stageReady || a.pkg == nil {
		t.Fatalf("the package inside zip was not read: stage %v, package %v", a.stage, a.pkg)
	}
	if a.pkg.Name != "Nested Plugin" || a.pkg.Version != "2.0.0" {
		t.Errorf("the package is %+v", a.pkg)
	}
}

func TestDroppingZipWithoutPackageShowsError(t *testing.T) {
	dir := t.TempDir()
	zipPath := filepath.Join(dir, "empty.zip")
	writeZip(t, zipPath, map[string]string{
		"readme.txt": "Just a text file",
	})
	a := newApp()
	a.openDropped([]string{zipPath})
	if a.stage != stageError {
		t.Fatalf("the stage is %v, want error", a.stage)
	}
	tt := tall(a)
	if !tt.HasText("压缩包中未找到 .ccx、.xdx 或 .zxp 安装包") {
		t.Errorf("error text missing: %q", tt.Texts())
	}
}

func TestOfficialInstallWarnsWithoutCreativeCloud(t *testing.T) {
	a := readyApp(KindUXP, allowedPreflight())
	a.environment = &Environment{Platform: runtime.GOOS}
	tt := tall(a)
	if err := tt.Click("官方安装"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !tt.HasText("需要 Creative Cloud Desktop") {
		t.Errorf("the missing Creative Cloud is not reported: %q", tt.Texts())
	}
	a.environment = &Environment{Platform: runtime.GOOS, CreativeCloudFound: true}
	tt.Frame()
	if !tt.HasText("未找到 Adobe 安装服务") {
		t.Errorf("the missing installer service is not reported: %q", tt.Texts())
	}
}

func TestUpdatePanelOffersTheNewVersion(t *testing.T) {
	a := readyApp(KindUXP, allowedPreflight())
	a.update = &UpdateInfo{Version: "1.2.0", Notes: "Fixes."}
	a.updateStage = updateAvailable
	tt := tall(a)
	if !tt.HasText("发现新版本 1.2.0") {
		t.Errorf("the update is not offered: %q", tt.Texts())
	}
	if _, ok := tt.Find("立即更新"); !ok {
		t.Errorf("there is no way to update: %q", tt.Texts())
	}
	a.updateStage = updateDownloading
	a.updateProgress = 0.42
	tt.Frame()
	if !tt.HasText("正在下载并安装… 42%") {
		t.Errorf("the download progress is not shown: %q", tt.Texts())
	}
	a.updateStage = updateFailed
	a.updateMessage = "网络不可用。"
	tt.Frame()
	if !tt.HasText("更新失败") || !tt.HasText("网络不可用。") {
		t.Errorf("the update failure is not shown: %q", tt.Texts())
	}
	if _, ok := tt.Find("重试"); !ok {
		t.Errorf("there is no way to retry: %q", tt.Texts())
	}
	a.updateStage = updateHidden
	tt.Frame()
	if tt.HasText("发现新版本 1.2.0") || tt.HasText("更新失败") {
		t.Errorf("a hidden update panel is still shown: %q", tt.Texts())
	}
}

func TestCheckEnvironmentDescribesThisMachine(t *testing.T) {
	environment := checkEnvironment()
	if environment.Platform != runtime.GOOS {
		t.Errorf("the platform is %q", environment.Platform)
	}
	if environment.InstallerFound != (environment.InstallerPath != "") {
		t.Error("the installer path and the flag disagree")
	}
}

func TestIsPackagePath(t *testing.T) {
	for _, path := range []string{"/a/Plugin.CCX", "b.xdx", "c.zxp"} {
		if !isPackagePath(path) {
			t.Errorf("%q is not recognized", path)
		}
	}
	for _, path := range []string{"/a/notes.txt", "archive.zip", ""} {
		if isPackagePath(path) {
			t.Errorf("%q is recognized as a package", path)
		}
	}
}

// installedApp is an app whose right column lists the extensions given.
func installedApp(extensions ...Installed) *app {
	a := newApp()
	a.environment = &Environment{
		Platform: runtime.GOOS, InstallerFound: true, CreativeCloudFound: true,
	}
	a.installed = extensions
	a.installedLoaded = true
	return a
}

// extension is one extension of this user, to show in the tests.
func sampleExtension(id, name string, kind Kind, hosts ...string) Installed {
	return Installed{
		ID: id, Name: name, Version: "1.0.0", Kind: kind, Hosts: hosts,
		Directory: "/tmp/" + id, Source: sourceUser, Enabled: true, Removable: true,
	}
}

func TestInstalledPaneListsTheExtensions(t *testing.T) {
	a := installedApp(
		sampleExtension("com.lanhu.ps", "蓝湖", KindUXP, "Photoshop"),
		sampleExtension("com.moonvy.ps.cep", "Moonvy 月维（经典）", KindCEP, "Photoshop", "Illustrator"),
	)
	tt := tall(a)
	for _, text := range []string{
		"已安装", "蓝湖", "v1.0.0", "com.lanhu.ps", "Moonvy 月维（经典）",
		"UXP", "CEP", "Photoshop", "Illustrator",
	} {
		if !tt.HasText(text) {
			t.Errorf("%q is missing: %q", text, tt.Texts())
		}
	}
	if _, ok := tt.Find("刷新"); !ok {
		t.Errorf("there is no way to read the machine again: %q", tt.Texts())
	}
}

func TestInstalledPaneLoadsInTheBackground(t *testing.T) {
	tt := tall(newApp())
	if !tt.HasText("正在读取本机扩展…") {
		t.Errorf("the column does not say it is reading: %q", tt.Texts())
	}
}

func TestInstalledPaneSaysWhenThereIsNothing(t *testing.T) {
	tt := tall(installedApp())
	if !tt.HasText("没有找到已安装的扩展") {
		t.Errorf("the column does not explain itself: %q", tt.Texts())
	}
}

func TestInstalledPaneMarksStoppedAndSharedExtensions(t *testing.T) {
	stopped := sampleExtension("com.example.stopped", "Stopped", KindUXP, "Photoshop")
	stopped.Enabled = false
	shared := sampleExtension("com.adobe.ccx.start", "Start", KindCEP, "Photoshop")
	shared.Source, shared.Removable = sourceSystem, false

	tt := tall(installedApp(stopped, shared))
	if !tt.HasText("已停用") {
		t.Errorf("a stopped extension is not marked: %q", tt.Texts())
	}
	if !tt.HasText("其他用户") {
		t.Errorf("an extension of every user is not marked: %q", tt.Texts())
	}
	if err := tt.Click("更多操作 Start"); err == nil {
		if tt.Menu() != nil && slices.Contains(tt.Menu(), "移除 Start") {
			t.Errorf("an extension of every user offers removal in menu: %q", tt.Menu())
		}
	}
}

func TestCopyExtensionIDFromMenu(t *testing.T) {
	ext := sampleExtension("com.test.copyid", "Test Copy", KindUXP, "Photoshop")
	a := installedApp(ext)
	tt := tall(a)
	if err := tt.Click("更多操作 Test Copy"); err != nil {
		t.Fatalf("clicking more: %v", err)
	}
	tt.Frame()
	if err := tt.ChooseMenuItem("复制扩展 ID"); err != nil {
		t.Fatalf("choosing copy: %v", err)
	}
	tt.Frame()
	if got := tt.Clipboard(); got != "com.test.copyid" {
		t.Errorf("clipboard has %q, want %q", got, "com.test.copyid")
	}
}

func TestRemovingAnExtensionFromTheList(t *testing.T) {
	_, base := uxpFixture(t)
	directory := installUXPFixture(t, base, "com.example.plugin_1.0.0")
	writeUXPRegistry(t, base, "PS", registryPlugin{
		PluginID: "com.example.plugin", Name: "示例插件", VersionString: "1.0.0",
		Path: "$localPlugins/External/com.example.plugin_1.0.0", Status: "enabled",
	})
	a := installedApp(userExtensions(installedExtensions())...)
	tt := tall(a)
	if !tt.HasText("示例插件") {
		t.Fatalf("the extension is not listed: %q", tt.Texts())
	}

	if err := tt.Click("更多操作 示例插件"); err != nil {
		t.Fatalf("clicking more: %v", err)
	}
	if err := tt.ChooseMenuItem("移除 示例插件"); err != nil {
		t.Fatalf("choosing remove: %v", err)
	}
	tt.Frame()
	if !tt.HasText("确认移除") {
		t.Fatalf("the row does not ask to confirm: %q", tt.Texts())
	}

	if err := tt.Click("确认移除"); err != nil {
		t.Fatalf("confirming the removal: %v", err)
	}
	tt.Frame()
	if pathExists(directory) {
		t.Error("the plugin directory is still there")
	}
	registry := filepath.Join(base, "PluginsInfo", "v1", "PS.json")
	if entries := registryEntries(t, registry); len(entries) != 0 {
		t.Errorf("the registry still lists %+v", entries)
	}
	if !tt.HasText("已移除 示例插件。") {
		t.Errorf("the removal is not reported: %q", tt.Texts())
	}
	if _, ok := tt.Find("更多操作 示例插件"); ok {
		t.Errorf("the row still exists after removal: %q", tt.Texts())
	}
}

func TestCancelingARemoval(t *testing.T) {
	_, base := uxpFixture(t)
	directory := installUXPFixture(t, base, "com.example.plugin_1.0.0")
	writeUXPRegistry(t, base, "PS", registryPlugin{
		PluginID: "com.example.plugin", Name: "示例插件", VersionString: "1.0.0",
		Path: "$localPlugins/External/com.example.plugin_1.0.0", Status: "enabled",
	})
	a := installedApp(userExtensions(installedExtensions())...)
	tt := tall(a)

	if err := tt.Click("更多操作 示例插件"); err != nil {
		t.Fatalf("clicking more: %v", err)
	}
	if err := tt.ChooseMenuItem("移除 示例插件"); err != nil {
		t.Fatalf("choosing remove: %v", err)
	}
	tt.Frame()
	if err := tt.Click("取消"); err != nil {
		t.Fatalf("clicking cancel: %v", err)
	}
	tt.Frame()
	if !pathExists(directory) {
		t.Error("canceling removed the plugin anyway")
	}
	if _, ok := tt.Find("更多操作 示例插件"); !ok {
		t.Errorf("the row does not offer more actions again: %q", tt.Texts())
	}
}
