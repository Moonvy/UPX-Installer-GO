package main

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

// stage is which screen the window shows.
type stage int

// The screens the window switches between.
const (
	stageEmpty stage = iota
	stageReady
	stageInstalling
	stageSuccess
	stageError
)

// installMode is how a package is installed.
type installMode int

// The two ways a package can be installed.
const (
	modeOfficial installMode = iota
	modeSideload
)

// updateStage is what the update panel shows.
type updateStage int

// The states of the update panel.
const (
	updateHidden updateStage = iota
	updateAvailable
	updateDownloading
	updateRestarting
	updateFailed
)

// unknownVersion is what a package without a usable version is called.
const unknownVersion = "未知"

// app is the window's state, which its view is a function of.
type app struct {
	win   *mygo.Window
	dirty bool

	stage       stage
	pkg         *Package
	environment *Environment
	preflight   *SideloadPreflight
	mode        installMode

	message           string
	details           string
	errorFile         string
	detailsExpanded   bool
	installingMessage string
	activationPending bool

	update         *UpdateInfo
	updateStage    updateStage
	updateProgress float64
	updateMessage  string

	// The extensions this machine already has, on the right. The generation
	// numbers the scans, so that a slow one does not answer for a newer one.
	installed           []Installed
	installedLoaded     bool
	installedNotice     string
	installedGeneration int
	confirming          string
	removing            string

	// install and sideload are the actions the buttons run. Tests replace
	// them so that a click never touches the machine.
	install  func(string, Package) (InstallResult, error)
	sideload func(string, Package) (InstallResult, error)
}

func newApp() *app {
	return &app{
		mode:     modeSideload,
		install:  installPackage,
		sideload: sideloadPackage,
	}
}

// attach gives the app the window it publishes background results through.
func (a *app) attach(win *mygo.Window) {
	a.win = win
	a.environment = ptr(checkEnvironment())
	a.dirty = true
	a.scanInstalled()
	go a.checkForUpdate()
}

// emit runs fn on the UI thread and draws a frame. Without a window, as in
// tests, it runs fn right away.
func (a *app) emit(fn func()) {
	if a.win == nil {
		fn()
		return
	}
	a.win.Update(fn)
}

// background runs work off the UI thread and applies what it returns on it.
// The work returns the function that changes the state, or nil to change
// nothing, e.g. when the user canceled a dialog.
func (a *app) background(work func() func()) {
	if a.win == nil {
		if apply := work(); apply != nil {
			apply()
		}
		return
	}
	go func() {
		apply := work()
		a.emit(func() {
			if apply != nil {
				apply()
			}
		})
	}()
}

// view builds the window's interface from the state, for every frame.
func (a *app) view(c *ui.Context) {
	c.SetTheme(appTheme())
	if a.dirty {
		a.dirty = false
		c.Invalidate()
	}
	// The material shows wherever the view draws no background. The scrim
	// over it keeps the window dark, so that what is behind tints it rather
	// than showing through it. A window without a material, such as one on
	// Windows 10, gets the opaque background instead.
	if c.Vibrancy() {
		c.Root().Background(colScrim)
	} else {
		c.Root().Background(colBackground)
	}
	ui.Column(c).Fill().Children(func() {
		a.topBar(c)
		ui.Row(c).Grow(1).AlignItems(ui.Stretch).Children(func() {
			a.installPane(c)
			ui.Box(c).Width(1).Shrink(0).Background(colHairline)
			a.installedPane(c)
		})
		a.footer(c)
	})
}

const (
	// installPaneWidth is the width of the column that installs a package.
	installPaneWidth = 460
)

// installPane is the left column: the package to install, and what installing
// it will do. Both columns share a unified 48px header line for perfect grid symmetry.
func (a *app) installPane(c *ui.Context) {
	ui.Column(c).Width(installPaneWidth).Shrink(0).Children(func() {
		a.installHeader(c)
		if a.stage == stageEmpty {
			ui.Column(c).Grow(1).Padding(16, 20, 20, 20).Children(func() {
				a.screen(c)
			})
			return
		}
		ui.Scroll(c).Grow(1).Padding(12, 20, 16, 20).Children(func() {
			ui.Column(c).FillWidth().Gap(14).Children(func() {
				a.screen(c)
			})
		})
	})
}

// installHeader displays the phase title in prominent bold text.
func (a *app) installHeader(c *ui.Context) {
	ui.Row(c).FillWidth().Height(48).Padding(0, 20).Gap(10).AlignItems(ui.Center).
		Background(colBar).BorderWidth(0, 0, 1, 0).BorderColor(colHairline).Children(func() {
		if a.pkg == nil {
			ui.Text(c, "安装扩展").FontSize(14).Bold().TextColor(colText)
			return
		}
		ui.Text(c, "准备安装").FontSize(14).Bold().TextColor(colText)
		ui.Spacer(c)
		cancel := ui.ButtonBase(c).Height(28).Padding(0, 12).Radius(6).Center()
		if cancel.Hovered() && a.stage != stageInstalling {
			cancel.Background(ui.RGBA(255, 255, 255, 0.08))
		}
		cancel.Label("取消")
		cancel.Tooltip("取消并返回")
		cancel.Children(func() {
			ui.Text(c, "取消").FontSize(12).
				TextColor(choose(a.stage == stageInstalling, colTextFaint, choose(cancel.Hovered(), colText, colTextMuted)))
		})
		cancel.Disabled(a.stage == stageInstalling).OnClick(func() {
			if a.stage != stageInstalling {
				a.reset()
			}
		})
	})
}

// screen builds whichever screen the state calls for.
func (a *app) screen(c *ui.Context) {
	switch {
	case a.stage == stageSuccess && a.pkg != nil:
		a.successCard(c)
	case a.stage == stageError && a.pkg == nil:
		a.errorCard(c)
	case a.pkg != nil:
		a.packageCard(c)
	default:
		a.dropZone(c)
	}
	a.updatePanel(c)
}

// titleBarHeight is how tall the view draws its own title bar, which is at
// least as tall as the room the window controls take.
const titleBarHeight = 52

// topBar draws the title bar: the masthead on the left, clear of the window
// controls, and the state of the Adobe tooling on the right. Dragging it
// moves the window, and a double click zooms it, as a title bar does.
func (a *app) topBar(c *ui.Context) {
	bar := c.TitleBar()
	ui.Row(c).FillWidth().Height(max(bar.Height, titleBarHeight)).
		Padding(0, bar.Right+18, 0, bar.Left+18).Gap(12).AlignItems(ui.Center).
		Background(colBar).BorderWidth(0, 0, 1, 0).BorderColor(colHairline).
		DragWindow().Children(func() {
		ui.Image(c, draw().app).Size(30, 30).Radius(7)
		ui.Column(c).Gap(1).Justify(ui.Center).Children(func() {
			ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
				ui.Text(c, "OpenUXP").FontSize(14).Bold().TextColor(colText)
				ui.Row(c).Padding(1, 5).Radius(4).Background(colAccentSoft).Children(func() {
					ui.Text(c, "INSTALLER").FontSize(9).Font("monospace").
						Bold().LetterSpacing(1.2).TextColor(colAccentHover)
				})
			})
		})
		ui.Spacer(c)
		tone, text := a.environmentStatus()
		if tone != colSuccess {
			statusBg := colWarningSoft
			statusLine := colWarningLine
			ui.Row(c).Padding(4, 11).Radius(12).Gap(7).AlignItems(ui.Center).
				Background(statusBg).Border(1, statusLine).Children(func() {
				ui.Box(c).Size(6, 6).Radius(3).Background(tone)
				ui.Text(c, text).FontSize(11).Bold().TextColor(tone).TextAlign(ui.End)
			})
		}
	})
}

// environmentStatus names the state of the Adobe tooling, and its color.
func (a *app) environmentStatus() (ui.Color, string) {
	if a.environment == nil {
		return colTextFaint, "正在检测 Adobe 环境…"
	}
	if a.environment.InstallerFound {
		return colSuccess, "Adobe 安装服务已就绪"
	}
	if a.environment.CreativeCloudFound {
		return colWarning, "Creative Cloud 已安装，但未找到安装服务"
	}
	return colWarning, "未找到 Creative Cloud Desktop"
}

// footer draws the version and the links along the bottom of the window.
func (a *app) footer(c *ui.Context) {
	ui.Row(c).FillWidth().Height(44).Padding(0, 24).AlignItems(ui.Center).
		Background(colBar).BorderWidth(1, 0, 0, 0).BorderColor(colHairline).Children(func() {
		ui.Link(c, "Moonvy.com", moonvyURL).FontSize(12)
		ui.Textf(c, " · v%s", valueOr(mygo.App.Version(), "—")).FontSize(12).Font("monospace").TextColor(colTextMuted)
		ui.Spacer(c)
		ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
			ui.Icon(c, interfaceIcon("github")).FontSize(14).TextColor(colTextMuted)
			ui.Link(c, "GitHub", githubURL).FontSize(12)
		})
	})
}

// dropZone is the screen that asks for a package: a click target that also
// takes the files dropped on it.
func (a *app) dropZone(c *ui.Context) {
	zone := ui.Column(c).FillWidth().MinHeight(290).Grow(1).Padding(28, 24).Gap(8).
		Justify(ui.Center).AlignItems(ui.Center).Radius(18).
		Background(colCard).Border(1, colBorder).BorderStyle(ui.BorderDashed).
		Focusable().Cursor(ui.CursorPointer)

	if paths := zone.DroppedFiles(); len(paths) > 0 {
		a.openDropped(paths)
	}
	hovering := zone.FileDragOver()
	switch {
	case hovering:
		zone.Background(colAccentPanel).Border(1, colAccent)
	case zone.Hovered():
		zone.Border(1, colAccentHover)
	}
	zone.OnClick(a.pickPackage)
	zone.Children(func() {
		iconBg := choose(hovering, colAccentSoft, ui.RGBA(255, 255, 255, 0.03))
		iconBorder := choose(hovering, colAccentLine, colBorderSubtle)
		ui.Column(c).Size(96, 96).Radius(22).Center().
			Background(iconBg).Border(1, iconBorder).Margin(0, 0, 10, 0).
			Children(func() {
				ui.Image(c, draw().ccx).Size(68, 68)
			})

		title := "拖放 CCX / XDX / ZXP 文件到这里"
		if hovering {
			title = "松开以载入安装包"
		}
		ui.Text(c, title).FontSize(17).Bold().TextColor(colText).TextAlign(ui.Center)
		ui.Text(c, "或点击从电脑中选择").FontSize(13).TextColor(colTextMuted).Margin(0, 0, 6, 0)
		iconTextButton(c, "选择文件", "folder-open").OnClick(a.pickPackage)
		ui.Row(c).Margin(14, 0, 0, 0).Padding(4, 12).Radius(12).
			Background(colTagHostBg).Children(func() {
			ui.Text(c, "支持 UXP .ccx、Adobe XD .xdx 与 CEP .zxp").FontSize(11).TextColor(colTextMuted)
		})
	})
}

// packageCard is the screen that installs the package the user chose.
func (a *app) packageCard(c *ui.Context) {
	ui.Column(c).FillWidth().Padding(18).Gap(14).Radius(18).
		Background(colCard).Border(1, colBorder).Children(func() {
		a.packageHeading(c)
		a.metadata(c)
		a.installPicker(c)
		a.primaryAction(c)
		a.detailsSection(c)
	})
}

// packageHeading names the package and its file.
func (a *app) packageHeading(c *ui.Context) {
	pkg := a.pkg
	ui.Row(c).FillWidth().Gap(14).AlignItems(ui.Center).Children(func() {
		ui.Column(c).Size(48, 48).Radius(12).Center().Clip().
			Background(colSurfaceHover).Children(func() {
			if pkg.Icon != nil {
				ui.Image(c, pkg.Icon).Size(48, 48).Fit(ui.Contain)
			} else {
				ui.Image(c, draw().ccx).Size(40, 40)
			}
		})
		ui.Column(c).Grow(1).Gap(3).Children(func() {
			ui.Text(c, pkg.Name).FontSize(17).Bold().TextColor(colText).SingleLine().Ellipsis("…").Selectable()
			ui.Textf(c, "%s · %s", pkg.FileName, formatBytes(pkg.FileSize)).
				FontSize(11).Font("monospace").TextColor(colTextMuted).SingleLine().Ellipsis("…").Selectable()
		})
	})
}

// metadata is the version, manifest and ID of the package.
func (a *app) metadata(c *ui.Context) {
	pkg := a.pkg
	ui.Column(c).FillWidth().Radius(10).Background(colInset).Border(1, colBorder).Clip().Children(func() {
		ui.Row(c).FillWidth().Children(func() {
			metadataCell(c, "版本", func() { a.versionValue(c, *pkg) }).Grow(1)
			ui.Box(c).Width(1).Shrink(0).Background(colBorderSubtle)
			metadataCell(c, "清单", func() { metadataValue(c, manifestLabel(pkg)) }).Grow(1)
		})
		ui.Box(c).Height(1).Shrink(0).Background(colBorderSubtle)
		metadataCell(c, "插件 ID", func() {
			metadataValue(c, valueOr(pkg.PluginID, "未提供"))
		})
	})
}

// versionValue draws the version of a package: what it brings, and what it
// replaces when the machine already has the extension.
func (a *app) versionValue(c *ui.Context, pkg Package) {
	current := a.installedVersion(pkg)
	if current == "" || current == pkg.Version {
		metadataValue(c, valueOr(pkg.Version, unknownVersion))
		return
	}
	ui.Row(c).Gap(4).AlignItems(ui.Center).Children(func() {
		ui.Text(c, current).FontSize(12).Font("monospace").
			TextColor(colTextFaint).Strikethrough().SingleLine().Selectable()
		ui.Icon(c, interfaceIcon("chevron-right")).FontSize(12).TextColor(colTextFaint)
		ui.Text(c, pkg.Version).FontSize(12).Font("monospace").
			Bold().TextColor(colAccentHover).SingleLine().Selectable()
	})
}

// installedVersion returns the version of the extension a package would
// replace, or "" when the machine does not have it.
func (a *app) installedVersion(pkg Package) string {
	if pkg.PluginID == "" {
		return ""
	}
	for _, extension := range a.installed {
		if extension.ID == pkg.PluginID && extension.Kind == pkg.Kind {
			return extension.Version
		}
	}
	return ""
}

// metadataCell is one field of the metadata, which fills the width it is
// given.
func metadataCell(c *ui.Context, label string, value func()) ui.Element {
	return ui.Column(c).MinWidth(0).Padding(8, 11).Gap(4).Children(func() {
		ui.Text(c, label).FontSize(11).TextColor(colTextFaint).Selectable()
		value()
	})
}

// metadataValue is a plain value in the metadata's monospace.
func metadataValue(c *ui.Context, text string) {
	ui.Text(c, text).FontSize(12).Font("monospace").SingleLine().Ellipsis("…").Selectable()
}

// installPicker chooses between Adobe's installer service and sideloading.
func (a *app) installPicker(c *ui.Context) {
	sectionLabel(c, "安装方式")
	ui.Row(c).FillWidth().Gap(8).Children(func() {
		cep := a.pkg.Kind == KindCEP
		a.installOption(c, modeSideload, choose(cep, "CEP 侧载", "侧载安装"),
			choose(cep, "安装到用户扩展目录", "直接写入用户目录"), true)
		if !cep {
			a.installOption(c, modeOfficial, "官方安装", "通过 Creative Cloud", true)
		}
	})
}

// installOptionHeight is how tall each install method is drawn, so that the
// methods are the same size whatever their wording.
const installOptionHeight = 58

// installOption is one of the install methods, which the user picks.
func (a *app) installOption(c *ui.Context, mode installMode, title, subtitle string, enabled bool) {
	active := a.mode == mode
	option := ui.Column(c).Grow(1).Height(installOptionHeight).Justify(ui.Center).
		Padding(9, 12).Gap(3).Radius(9).
		Background(colInset).Border(1, colBorder)
	if !enabled {
		option.Disabled(true).Opacity(0.55)
		return
	}
	switch {
	case active:
		option.Background(colAccentSoft).Border(1, colAccent)
	case option.Hovered():
		option.Background(colHover).Border(1, colAccentHover)
	}
	option.Focusable().Cursor(ui.CursorPointer)
	option.Children(func() {
		ui.Text(c, title).FontSize(13).Bold().SingleLine().Ellipsis("…")
		ui.Text(c, subtitle).FontSize(11).TextColor(colTextMuted).SingleLine().Ellipsis("…")
	})
	option.OnClick(func() { a.chooseMode(mode) })
}

// hostBadge is a compact chip for a host Adobe application.
func (a *app) hostBadge(c *ui.Context, host Host) {
	ui.Row(c).Gap(5).AlignItems(ui.Center).Padding(2, 6).Radius(5).
		Background(colSurfaceHover).Children(func() {
		ui.Image(c, hostIcon(host.Icon)).Size(15, 15).Radius(3)
		ui.Text(c, host.Label).FontSize(11).Bold().TextColor(colText).Selectable()
		if host.MinVersion != "" {
			ui.Textf(c, "≥ %s", host.MinVersion).FontSize(10).Font("monospace").TextColor(colTextMuted).Selectable()
		}
	})
}

// detailsSection shows the compatible apps, install paths and requirements.
func (a *app) detailsSection(c *ui.Context) {
	showDetails := a.detailsExpanded ||
		(a.mode == modeSideload && a.preflight != nil && !a.preflight.Ready) ||
		(a.mode == modeOfficial && a.environment != nil && !a.environment.InstallerFound) ||
		a.stage == stageError

	ui.Row(c).FillWidth().AlignItems(ui.Center).Children(func() {
		sectionLabel(c, "详细信息")
		ui.Spacer(c)
		toggle := ui.ButtonBase(c).Size(22, 22).Radius(5).Center()
		if toggle.Hovered() {
			toggle.Background(colSurfaceHover)
		}
		toggleText := choose(showDetails, "收起", "展开")
		toggleLabel := choose(showDetails, "收起详细信息", "展开详细信息")
		toggle.Label(toggleText)
		toggle.Tooltip(toggleLabel)
		toggle.Children(func() {
			ui.Icon(c, choose(showDetails, interfaceIcon("chevron-up"), interfaceIcon("chevron-down"))).
				FontSize(14).TextColor(choose(toggle.Hovered(), colText, colTextMuted))
		})
		toggle.OnClick(func() {
			a.detailsExpanded = !showDetails
		})
	})

	ui.Column(c).FillWidth().Padding(12, 14).Gap(8).Radius(10).
		Background(colInset).Border(1, colBorderSubtle).Children(func() {
		// 1. 兼容应用
		ui.Row(c).FillWidth().Gap(8).AlignItems(ui.Center).Children(func() {
			ui.Text(c, "兼容应用").FontSize(11).Width(64).TextColor(colTextFaint).Selectable()
			if len(a.pkg.Hosts) == 0 {
				ui.Text(c, "未声明宿主应用").FontSize(11).TextColor(colTextMuted).Selectable()
			} else {
				ui.Row(c).Gap(6).AlignItems(ui.Center).Wrap().Grow(1).Children(func() {
					for _, host := range a.pkg.Hosts {
						a.hostBadge(c, host)
					}
				})
			}
		})

		if !showDetails {
			return
		}

		ui.Box(c).Height(1).FillWidth().Background(colBorderSubtle).Margin(2, 0)

		// 2. 侧载环境核心信息
		if a.mode == modeSideload && a.preflight != nil {
			ready := a.preflight.Ready
			ui.Row(c).FillWidth().Gap(8).AlignItems(ui.Center).Children(func() {
				ui.Text(c, "侧载状态").FontSize(11).Width(64).TextColor(colTextFaint).Selectable()
				ui.Row(c).Gap(5).AlignItems(ui.Center).Children(func() {
					ui.Icon(c, choose(ready, interfaceIcon("shield-check"), interfaceIcon("alert-circle"))).
						FontSize(13).TextColor(choose(ready, colAccentHover, ui.Hex("#c2934a")))
					ui.Text(c, a.sideloadHeadline(a.preflight)).FontSize(11).Bold().
						TextColor(choose(ready, colText, ui.Hex("#c2934a"))).Selectable()
				})
			})

			if !ready {
				a.permissionIssues(c, a.preflight)
			} else {
				// 路径核心信息
				isCEP := a.pkg.Kind == KindCEP
				pathRow(c, choose(isCEP, "扩展目录", "插件目录"), a.preflight.PluginDirectory, true)
				if isCEP {
					pathRow(c, "调试模式", choose(a.preflight.DebugModeEnabled,
						"已开启 "+strings.Join(a.preflight.DebugModeKeys, "、"),
						"安装时自动开启 "+strings.Join(a.preflight.DebugModeKeys, "、")), false)
				} else {
					for _, path := range a.preflight.RegistryPaths {
						pathRow(c, "注册文件", path, true)
					}
				}

				// 柔和警告提示
				ui.Box(c).Height(1).FillWidth().Background(colBorderSubtle).Margin(2, 0)
				ui.Row(c).FillWidth().Gap(7).AlignItems(ui.Start).Children(func() {
					ui.Icon(c, interfaceIcon("triangle-alert")).FontSize(13).
						TextColor(ui.Hex("#c2934a")).Margin(1, 0, 0, 0)
					ui.Text(c, a.sideloadWarningTitle()).FontSize(11).
						TextColor(colTextMuted).Grow(1).Selectable()
				})
			}
		}

		// 官方安装信息
		if a.mode == modeOfficial {
			pathRow(c, "安装方式", "官方安装 · 通过 Creative Cloud Desktop", false)
			if !a.environment.InstallerFound {
				ui.Box(c).Height(1).FillWidth().Background(colBorderSubtle).Margin(2, 0)
				title, body := "需要 Creative Cloud Desktop", "安装前请先安装或更新 Adobe Creative Cloud Desktop。"
				if a.environment.CreativeCloudFound {
					title = "未找到 Adobe 安装服务"
					body = "请重新启动或更新 Creative Cloud Desktop 后重试。"
				}
				a.inlineNotice(c, noticeWarning, "triangle-alert", title, body)
			}
		}

		// 异步检查 / 安装中 / 报错状态
		if a.mode == modeSideload && a.preflight == nil && a.stage != stageInstalling {
			ui.Box(c).Height(1).FillWidth().Background(colBorderSubtle).Margin(2, 0)
			a.inlineNotice(c, noticeBusy, "loader-circle", "正在检查侧载环境", "正在检查插件目录读写权限…")
		}
		if a.stage == stageInstalling {
			ui.Box(c).Height(1).FillWidth().Background(colBorderSubtle).Margin(2, 0)
			a.inlineNotice(c, noticeBusy, "loader-circle",
				choose(a.mode == modeSideload, "正在侧载插件", "正在安装插件"), a.installingMessage)
		}
		if a.stage == stageError && a.message != "" {
			ui.Box(c).Height(1).FillWidth().Background(colBorderSubtle).Margin(2, 0)
			a.inlineNotice(c, noticeError, "triangle-alert", "安装未完成", a.message)
			a.detailsPanel(c)
		}
	})
}

// notice kinds.
const (
	noticeWarning = iota
	noticeError
	noticeBusy
)

// inlineNotice is a subtle message inside the details container.
func (a *app) inlineNotice(c *ui.Context, kind int, icon, title, body string) {
	var (
		iconColor  = ui.Hex("#c2934a")
		titleColor = colText
	)
	switch kind {
	case noticeError:
		iconColor = colDanger
		titleColor = colDanger
	case noticeBusy:
		iconColor = colAccentHover
	}
	body = strings.TrimRight(body, "\n")
	ui.Row(c).FillWidth().Gap(8).AlignItems(ui.Start).Children(func() {
		ui.Icon(c, interfaceIcon(icon)).FontSize(14).TextColor(iconColor).Margin(1, 0, 0, 0)
		ui.Column(c).Grow(1).Gap(2).Children(func() {
			ui.Text(c, title).FontSize(11).Bold().TextColor(titleColor)
			if body != "" {
				ui.Text(c, body).FontSize(11).TextColor(colTextMuted)
			}
		})
	})
}

func (a *app) sideloadHeadline(preflight *SideloadPreflight) string {
	switch {
	case preflight.Ready:
		return "侧载环境已准备就绪"
	case preflight.Supported:
		return "侧载安装需要文件权限"
	}
	return "当前安装包不支持侧载"
}

func (a *app) sideloadDescription(preflight *SideloadPreflight) string {
	switch {
	case preflight.Ready && a.pkg.Kind == KindCEP:
		return "扩展将安装到用户级 CEP 目录。安装时会自动开启调试模式，未签名扩展才能加载。"
	case preflight.Ready:
		return "插件将安装到 " + a.sideloadSummary() + " 的用户级 UXP 目录，不经过 Creative Cloud。"
	case preflight.Supported:
		return "OpenUXP Installer 当前无法安全写入以下位置。"
	}
	return "侧载目前仅支持 manifest 中声明 Photoshop（PS）、Adobe XD 或 Illustrator（AI）的插件。"
}

func (a *app) sideloadWarningTitle() string {
	if a.pkg.Kind == KindCEP {
		return "未签名 CEP 扩展需要 PlayerDebugMode。安装会写入当前用户的 CSXS 调试偏好，并在重启 Adobe 应用后生效。"
	}
	return "侧载会绕过 Creative Cloud 的安装确认，且插件不会由 Creative Cloud 管理。"
}

// permissionIssues lists what stops a sideload, with a button to check again.
func (a *app) permissionIssues(c *ui.Context, preflight *SideloadPreflight) {
	ui.Column(c).FillWidth().Gap(8).Children(func() {
		for _, issue := range preflight.Issues {
			ui.Column(c).FillWidth().Padding(8, 10).Gap(4).Radius(8).
				Background(colSurfaceHover).Children(func() {
				ui.Text(c, issue.Message).FontSize(12).Bold().TextColor(colText)
				if issue.Path != "" {
					ui.Text(c, issue.Path).FontSize(11).Font("monospace").TextColor(colTextMuted)
				}
				if issue.Hint != "" {
					ui.Text(c, issue.Hint).FontSize(11).TextColor(colTextFaint)
				}
			})
		}
		iconTextButton(c, "重新检查权限", "rotate-ccw").AlignSelf(ui.Start).OnClick(a.recheckPermissions)
	})
}

// sideloadPaths names where the plugin and its registration files go.
func (a *app) sideloadPaths(c *ui.Context, preflight *SideloadPreflight) {
	isCEP := a.pkg.Kind == KindCEP
	ui.Column(c).FillWidth().Gap(6).Children(func() {
		pathRow(c, choose(isCEP, "扩展目录", "插件目录"), preflight.PluginDirectory, true)
		if isCEP {
			pathRow(c, "调试模式", choose(preflight.DebugModeEnabled,
				"已开启 "+strings.Join(preflight.DebugModeKeys, "、"),
				"安装时自动开启 "+strings.Join(preflight.DebugModeKeys, "、")), false)
			return
		}
		for _, path := range preflight.RegistryPaths {
			pathRow(c, "注册文件", path, true)
		}
	})
}

// pathRow is one label and value. If reveal is true, the value is rendered
// in a read-only text input so the user can drag through the full path, and
// carries a button to show it in the system file manager.
func pathRow(c *ui.Context, label, value string, reveal bool) {
	ui.Row(c).FillWidth().Gap(6).AlignItems(ui.Center).Children(func() {
		ui.Text(c, label).FontSize(11).Width(64).TextColor(colTextFaint).Shrink(0)
		if reveal {
			pathVal := value
			ui.TextInputBase(c, &pathVal).Label(value).ReadOnly(true).
				FontSize(11).Font("monospace").
				Height(24).Padding(2, 6).Radius(5).
				Background(colSurface).
				TextColor(colTextMuted).Grow(1)

			btn := ui.ButtonBase(c).Size(22, 22).Radius(5).Center().Shrink(0)
			if btn.Hovered() {
				btn.Background(ui.RGBA(255, 255, 255, 0.12))
			}
			btn.Tooltip(revealLabel())
			btn.Children(func() {
				ui.Icon(c, interfaceIcon("folder-open")).FontSize(13).
					TextColor(choose(btn.Hovered(), colText, colTextMuted))
			})
			btn.OnClick(func() {
				target := value
				for target != "" && target != "." && target != "/" {
					if _, err := os.Stat(target); err == nil {
						break
					}
					target = filepath.Dir(target)
				}
				if target != "" && target != "." {
					mygo.Shell.ShowItemInFolder(target)
				}
			})
		} else {
			ui.Text(c, value).FontSize(11).Font("monospace").Grow(1).
				TextColor(colTextMuted).SingleLine().Ellipsis("…")
		}
	})
}

// primaryAction installs the package, and says what that will do. It carries
// one name whichever method it uses.
func (a *app) primaryAction(c *ui.Context) {
	sideloading := a.mode == modeSideload
	installing := a.stage == stageInstalling
	label := "立即安装"
	if installing {
		label = "正在安装…"
	}
	disabled := installing || (sideloading && (a.preflight == nil || !a.preflight.Ready))
	ui.PrimaryButton(c, label).FillWidth().Height(42).FontSize(14).
		Disabled(disabled).OnClick(a.startInstall)
	ui.Row(c).FillWidth().Gap(7).Justify(ui.Center).Children(func() {
		ui.Icon(c, interfaceIcon("shield-check")).FontSize(14).TextColor(colTextFaint)
		ui.Text(c, a.trustNote()).FontSize(11).TextColor(colTextFaint).TextAlign(ui.Center)
	})
}

// trustNote says what the chosen install does to the machine.
func (a *app) trustNote() string {
	if a.mode != modeSideload {
		return "官方安装由 Adobe Unified Plugin Installer Agent 完成"
	}
	if a.pkg.Kind == KindCEP {
		return "安装时会自动开启 CEP 调试模式（PlayerDebugMode）"
	}
	return "侧载安装会创建可恢复的 " + a.sideloadSummary() + " 注册文件备份"
}

// successCard is the screen shown once the package is installed.
func (a *app) successCard(c *ui.Context) {
	pkg := a.pkg
	ui.Column(c).FillWidth().Padding(28, 26).Gap(11).Radius(18).
		Background(colCard).Border(1, colBorder).AlignItems(ui.Center).Children(func() {
		if pkg != nil && pkg.Icon != nil {
			ui.Image(c, pkg.Icon).Size(76, 76).Fit(ui.Contain).Radius(16)
		} else {
			ui.Image(c, draw().app).Size(76, 76)
		}
		sectionLabel(c, "安装完成")
		ui.Text(c, pkg.Name).FontSize(20).Bold().TextAlign(ui.Center)
		a.installedMessage(c)
		a.detailsPanel(c)
		iconTextButton(c, "安装另一个插件", "package-open").OnClick(a.reset)
	})
}

// installedMessage says where the package went, and what the user has to do
// before it loads.
func (a *app) installedMessage(c *ui.Context) {
	summary := a.activeHostSummary()
	message := fmt.Sprintf("已经安装到 %s，打开 %s 即可使用。", summary, summary)
	color := colTextMuted
	if a.activationPending {
		message = fmt.Sprintf("已经安装到 %s，需要重新启动 %s。", summary, summary)
		color = colWarning
	}
	ui.Text(c, message).FontSize(13).TextColor(color).TextAlign(ui.Center)
}

// detailLine is one fact an install reported: what it is, and its value.
type detailLine struct {
	label string
	value string
}

// detailLines splits what an install reported into the facts the panel
// shows, each written under the label it was reported with.
func detailLines(details string) []detailLine {
	lines := []detailLine{}
	for _, raw := range strings.Split(details, "\n") {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}
		label, value, found := strings.Cut(raw, "：")
		if !found {
			lines = append(lines, detailLine{value: raw})
			continue
		}
		lines = append(lines, detailLine{label: label, value: value})
	}
	return lines
}

// detailsPanel shows what an install wrote: where the files went, and which
// registrations or settings it changed. Each fact is a label over its value,
// so that a long path wraps instead of running off the panel, and can be
// copied out of it.
func (a *app) detailsPanel(c *ui.Context) {
	if a.details == "" {
		return
	}
	lines := detailLines(a.details)
	if len(lines) == 0 {
		return
	}
	ui.Column(c).FillWidth().Padding(12).Gap(8).Radius(9).
		Background(colInset).Border(1, colBorder).Children(func() {
		ui.Text(c, "安装信息").FontSize(11).TextColor(colTextFaint)
		for _, line := range lines {
			ui.Column(c).FillWidth().Gap(2).Children(func() {
				if line.label != "" {
					ui.Text(c, line.label).FontSize(11).TextColor(colTextFaint)
				}
				ui.Text(c, line.value).FontSize(11).Font("monospace").
					TextColor(colTextMuted).Wrap().Selectable()
			})
		}
	})
}

// errorCard is the screen shown when no package could be read.
func (a *app) errorCard(c *ui.Context) {
	ui.Column(c).FillWidth().Padding(36, 30).Gap(12).Radius(18).
		Background(colCard).Border(1, colBorder).AlignItems(ui.Center).Children(func() {
		ui.Icon(c, interfaceIcon("triangle-alert")).FontSize(34).TextColor(colDanger)
		ui.Text(c, "无法读取安装包").FontSize(22).Bold()
		if a.errorFile != "" {
			ui.Row(c).Gap(6).AlignItems(ui.Center).Padding(4, 10).Radius(6).
				Background(colSurfaceHover).Children(func() {
				ui.Icon(c, interfaceIcon("package")).FontSize(14).TextColor(colTextMuted)
				ui.Text(c, a.errorFile).FontSize(12).Font("monospace").
					Bold().TextColor(colText).SingleLine().Ellipsis("…").Selectable()
			})
		}
		ui.Text(c, a.message).FontSize(12).TextColor(colTextMuted).TextAlign(ui.Center)
		iconTextButton(c, "重新选择", "rotate-ccw").OnClick(a.reset)
	})
}

// updatePanel offers a newer version of the app itself.
func (a *app) updatePanel(c *ui.Context) {
	if a.update == nil || a.updateStage == updateHidden {
		return
	}
	background, border, color := colAccentPanel, colAccentLine, colAccentHover
	title := "发现新版本 " + a.update.Version
	description := "新版本已通过签名验证，可以立即更新。"
	switch a.updateStage {
	case updateDownloading:
		description = fmt.Sprintf("正在下载并安装… %.0f%%", a.updateProgress*100)
	case updateRestarting:
		description = "更新完成，正在重新启动…"
	case updateFailed:
		background, border, color = colDangerSoft, colDangerLine, colDanger
		title = "更新失败"
		description = valueOr(a.updateMessage, "暂时无法完成更新，请稍后重试。")
	}
	ui.Row(c).FillWidth().Padding(11, 12).Gap(11).Radius(12).
		Background(background).Border(1, border).Children(func() {
		ui.Column(c).Gap(4).Children(func() {
			ui.Icon(c, interfaceIcon("download")).FontSize(17).TextColor(color)
		})
		ui.Column(c).Grow(1).Gap(5).Children(func() {
			ui.Text(c, title).FontSize(12).Bold().TextColor(color)
			ui.Text(c, description).FontSize(12).TextColor(colTextMuted).SingleLine().Ellipsis("…")
			if a.updateStage == updateDownloading {
				ui.Progress(c, a.updateProgress).FillWidth().Height(3)
			}
		})
		switch a.updateStage {
		case updateAvailable:
			ui.Button(c, "立即更新").OnClick(a.startUpdate)
			ui.Button(c, "").Label("稍后更新").Children(func() {
				ui.Icon(c, interfaceIcon("x")).FontSize(14)
			}).OnClick(func() { a.updateStage = updateHidden })
		case updateFailed:
			ui.Button(c, "重试").OnClick(func() { a.updateStage = updateAvailable })
		}
	})
}

// installedPane is the right column: the extensions this machine already has,
// and what can be done with them.
func (a *app) installedPane(c *ui.Context) {
	ui.Column(c).Grow(1).MinWidth(0).Children(func() {
		a.installedHeader(c)
		ui.Scroll(c).Grow(1).Padding(16, 20, 20, 20).Children(func() {
			a.installedList(c)
		})
	})
}

// installedHeader names the column and offers to read the machine again. It
// carries its own background, so that its text does not sit on the material.
func (a *app) installedHeader(c *ui.Context) {
	ui.Row(c).FillWidth().Height(48).Padding(0, 20).Gap(10).AlignItems(ui.Center).
		Background(colBar).BorderWidth(0, 0, 1, 0).BorderColor(colHairline).Children(func() {
		ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
			sectionLabel(c, "已安装")
			if a.installedLoaded && len(a.installed) > 0 {
				ui.Row(c).Padding(1, 7).Radius(10).Background(colSurfaceHover).Children(func() {
					ui.Textf(c, "%d", len(a.installed)).FontSize(10).Bold().TextColor(colAccentHover)
				})
			}
		})
		ui.Spacer(c)
		if a.installedNotice != "" {
			ui.Text(c, a.installedNotice).FontSize(11).
				TextColor(choose(a.removing != "", colWarning, colSuccess)).
				SingleLine().Ellipsis("…")
		}
		refresh := ui.ButtonBase(c).Size(28, 28).Radius(6).Center()
		if refresh.Hovered() && a.installedLoaded {
			refresh.Background(ui.RGBA(255, 255, 255, 0.14))
		}
		refresh.Label("刷新")
		refresh.Tooltip("刷新已安装列表")
		refresh.Children(func() {
			ui.Icon(c, interfaceIcon("refresh-cw")).FontSize(14).
				TextColor(choose(a.installedLoaded, choose(refresh.Hovered(), colText, colTextMuted), colTextFaint))
		})
		refresh.Disabled(!a.installedLoaded).OnClick(a.scanInstalled)
	})
}

// installedList is the extensions found, or why none are shown.
func (a *app) installedList(c *ui.Context) {
	if !a.installedLoaded {
		ui.Row(c).Gap(9).AlignItems(ui.Center).Children(func() {
			ui.Spinner(c).Size(15, 15)
			ui.Text(c, "正在读取本机扩展…").FontSize(12).TextColor(colTextMuted)
		})
		return
	}
	if len(a.installed) == 0 {
		ui.Column(c).Padding(32, 24).Gap(8).AlignItems(ui.Center).Children(func() {
			ui.Icon(c, interfaceIcon("package-open")).FontSize(32).TextColor(colTextFaint)
			ui.Text(c, "没有找到已安装的扩展").FontSize(14).Bold().TextColor(colTextMuted)
			ui.Text(c, "安装一个插件后，它会出现在这里。").
				FontSize(12).TextColor(colTextFaint).TextAlign(ui.Center)
		})
		return
	}
	ui.Column(c).FillWidth().Gap(20).Children(func() {
		for _, group := range groupInstalled(a.installed) {
			a.installedGroup(c, group)
		}
	})
}

// installedGroup is the extensions of one application, under its name and
// icon.
func (a *app) installedGroup(c *ui.Context, group installedGroup) {
	ui.Column(c).FillWidth().Gap(8).Children(func() {
		ui.Row(c).FillWidth().Gap(8).AlignItems(ui.Center).Margin(4, 0, 2, 0).Children(func() {
			if group.Icon != nil {
				ui.Image(c, group.Icon).Size(18, 18).Radius(4).Shrink(0)
			}
			ui.Text(c, group.Name).FontSize(13).Bold().TextColor(colText)
			ui.Row(c).Padding(1, 6).Radius(8).Background(colTagHostBg).Children(func() {
				ui.Textf(c, "%d", len(group.Extensions)).FontSize(10).Bold().TextColor(colTextMuted)
			})
		})
		for _, extension := range group.Extensions {
			a.installedRow(c, extension)
		}
	})
}

// installedRow is one extension: what it is, who loads it, and what can be
// done with it.
func (a *app) installedRow(c *ui.Context, extension Installed) {
	removing := a.removing == extension.ID
	row := ui.Column(c).FillWidth().Padding(12, 14).Gap(10).Radius(12).
		Background(colInset).Border(1, colBorder)
	if removing {
		row.Opacity(0.55)
	}
	if row.Hovered() {
		row.Background(colHover).Border(1, colBorderSubtle)
	}
	row.ContextMenu(func(m *ui.Menu) {
		if extension.ID != "" && m.Item("复制扩展 ID").Chosen() {
			c.WriteClipboard(extension.ID)
		}
		if m.Item(revealLabel()).Chosen() {
			a.revealInstalled(extension)
		}
		if extension.Removable {
			m.Separator()
			if m.Item("移除 " + extension.Name).Chosen() {
				a.askToRemove(extension)
			}
		}
	})
	row.Children(func() {
		ui.Row(c).FillWidth().Gap(12).AlignItems(ui.Center).Children(func() {
			a.installedIcon(c, extension)
			ui.Column(c).Grow(1).MinWidth(0).Gap(3).Children(func() {
				ui.Row(c).FillWidth().Gap(7).AlignItems(ui.Center).Children(func() {
					ui.Text(c, extension.Name).FontSize(13).Bold().TextColor(colText).SingleLine().Ellipsis("…")
					if extension.Version != "" {
						ui.Row(c).Shrink(0).Padding(1, 6).Radius(5).
							Background(colSurfaceHover).Border(1, colBorder).Children(func() {
							ui.Text(c, "v"+strings.TrimPrefix(extension.Version, "v")).
								FontSize(11).Font("monospace").Bold().TextColor(colText)
						})
					}
				})
				pathVal := extension.Directory
				if pathVal == "" {
					pathVal = extension.ID
				}
				ui.TextInputBase(c, &pathVal).Label(pathVal).ReadOnly(true).
					FontSize(11).Font("monospace").
					Height(18).TextColor(colTextMuted).FillWidth()
			})
			a.installedActions(c, extension)
		})
		ui.Row(c).FillWidth().GapX(6).GapY(4).PaddingStart(44).AlignItems(ui.Center).Wrap().Children(func() {
			if extension.Kind == KindCEP {
				a.tag(c, "CEP", colCEPText, colCEPBg)
			} else {
				a.tag(c, "UXP", colUXPText, colUXPBg)
			}
			a.hostTags(c, extension.Hosts)
			if !extension.Enabled {
				a.tag(c, "已停用", colWarning, colWarningSoft)
			}
			if extension.Source == sourceSystem {
				a.tag(c, "其他用户", colTextFaint, colSurfaceHover)
			}
		})
	})
}

// installedIcon draws the picture an extension brought, and the default one
// when it brought none.
func (a *app) installedIcon(c *ui.Context, extension Installed) {
	ui.Column(c).Size(32, 32).Shrink(0).Radius(8).Clip().Center().
		Background(colSurfaceHover).Border(1, colBorderSubtle).Children(func() {
		if extension.Icon != nil {
			ui.Image(c, extension.Icon).Size(32, 32).Fit(ui.Contain)
			return
		}
		ui.Icon(c, interfaceIcon("package")).FontSize(16).TextColor(colTextMuted)
	})
}

// hostAppIcon returns the icon of an Adobe host application by name or code.
func hostAppIcon(name string) *ui.SVG {
	if icon := hostIconName(name); icon != "" {
		return hostIcon(icon)
	}
	return draw().host["others"]
}

// hostTags displays compact application icons for the hosts an extension declares.
func (a *app) hostTags(c *ui.Context, hosts []string) {
	const shown = 5
	for index, host := range hosts {
		if index == shown {
			remaining := hosts[shown:]
			a.tagTooltip(c, fmt.Sprintf("+%d", len(remaining)), colTextFaint, colTagHostBg, strings.Join(remaining, "、"))
			return
		}
		ui.Image(c, hostAppIcon(host)).Size(16, 16).Radius(3).Shrink(0).
			Label(host).Tooltip(host)
	}
}

// installedActions offers to remove an extension or reveal it via a dropdown menu.
func (a *app) installedActions(c *ui.Context, extension Installed) {
	if a.confirming == extension.ID {
		ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
			ui.Button(c, "确认移除").OnClick(func() { a.removeInstalled(extension) })
			ui.Button(c, "取消").OnClick(a.cancelRemove)
		})
		return
	}
	more := ui.ButtonBase(c).Size(28, 28).Radius(6).Center()
	if more.Hovered() {
		more.Background(ui.RGBA(255, 255, 255, 0.14))
	}
	more.Label("更多操作 " + extension.Name)
	more.Tooltip("更多选项")
	more.Children(func() {
		ui.Icon(c, interfaceIcon("more-horizontal")).FontSize(16).
			TextColor(choose(more.Hovered(), colText, colTextMuted))
	})
	more.Menu(func(m *ui.Menu) {
		if extension.ID != "" && m.Item("复制扩展 ID").Chosen() {
			c.WriteClipboard(extension.ID)
		}
		if m.Item(revealLabel()).Chosen() {
			a.revealInstalled(extension)
		}
		if extension.Removable {
			m.Separator()
			if m.Item("移除 " + extension.Name).Chosen() {
				a.askToRemove(extension)
			}
		}
	})
}

// tagTooltip is a short label on a pill with an optional tooltip.
func (a *app) tagTooltip(c *ui.Context, text string, color ui.Color, background ui.Color, tooltip string) {
	pill := ui.Row(c).Shrink(0).Padding(2, 7).Radius(6).Background(background)
	if tooltip != "" {
		pill.Tooltip(tooltip)
	}
	pill.Children(func() {
		ui.Text(c, text).FontSize(10).Bold().TextColor(color)
	})
}

// tag is a short label on a pill, such as an extension's host or format.
func (a *app) tag(c *ui.Context, text string, color ui.Color, background ui.Color) {
	a.tagTooltip(c, text, color, background, "")
}

// revealLabel names the action that shows a file in the file manager.
func revealLabel() string {
	if runtime.GOOS == "windows" {
		return "在资源管理器中显示"
	}
	return "在访达中显示"
}

// sectionLabel is the small capital label above a group of controls.
func sectionLabel(c *ui.Context, text string) {
	ui.Text(c, text).FontSize(11).Font("monospace").LetterSpacing(1.4).
		TextColor(colTextMuted).Margin(0, 0, 2, 0)
}

// iconTextButton is a button showing an icon beside its label. The label
// names it for the reader and for tests.
func iconTextButton(c *ui.Context, label, icon string) ui.Element {
	button := ui.Button(c, "").Label(label)
	button.Children(func() {
		ui.Row(c).Gap(6).AlignItems(ui.Center).Padding(4, 10).Children(func() {
			ui.Icon(c, interfaceIcon(icon)).FontSize(15).TextColor(colText)
			ui.Text(c, label).FontSize(13).Bold().TextColor(colText)
		})
	})
	return button
}

// manifestLabel names the manifest a package carries.
func manifestLabel(pkg *Package) string {
	switch pkg.Kind {
	case KindCEP:
		if pkg.ManifestVersion > 0 {
			return fmt.Sprintf("CEP v%d", pkg.ManifestVersion)
		}
		return "CEP"
	case KindXDX:
		if pkg.ManifestVersion > 0 {
			return fmt.Sprintf("XDX · Manifest v%d", pkg.ManifestVersion)
		}
		return "XDX"
	}
	if pkg.ManifestVersion > 0 {
		return fmt.Sprintf("UXP · Manifest v%d", pkg.ManifestVersion)
	}
	return "UXP"
}

// formatBytes renders a size the way a person reads it.
func formatBytes(bytes int64) string {
	if bytes <= 0 {
		return "—"
	}
	units := []string{"B", "KB", "MB", "GB"}
	value, unit := float64(bytes), 0
	for value >= 1024 && unit < len(units)-1 {
		value /= 1024
		unit++
	}
	if unit == 0 {
		return fmt.Sprintf("%.0f %s", value, units[unit])
	}
	return fmt.Sprintf("%.1f %s", value, units[unit])
}

// choose returns first when cond is true, and second otherwise.
func choose[T any](cond bool, first, second T) T {
	if cond {
		return first
	}
	return second
}

// valueOr returns value, or fallback when it is empty.
func valueOr(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}

// ptr returns a pointer to a copy of value.
func ptr[T any](value T) *T {
	return &value
}
