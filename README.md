# OpenUXP Installer

一个使用 [MyGo](https://mygo.egoist.dev/) 构建的轻量级 Adobe 插件安装器，支持 macOS 和 Windows 上的 CEP（`.zxp`）、UXP（`.ccx`）与 XDX（`.xdx`）安装包。

界面是 **MyGo 原生界面**：用 Go 写成，由 MyGo 在 GPU 上绘制（macOS 用 Metal，Windows 用 Direct3D 11），不启动 WebView，没有 HTML、没有前端构建、也没有内置浏览器。冷启动即刻出窗口，空闲时 CPU 占用 0%，窗口打开时常驻内存约 90 MB（macOS arm64 实测）。

本项目是 Tauri 版本 [OpenUPX Installer](https://github.com/yArna/OpenUPX-Installer) 的重写：功能与界面布局一致，后端由 Rust 改为 Go，界面由 React 改为 MyGo 原生控件。

## 窗口样式

- **没有系统标题栏。** 窗口用 `TitleBarHiddenInset`，由视图自己画标题栏：标题栏区域可以拖动窗口、双击缩放，左侧的品牌标识会避开 macOS 的红绿灯（Windows 上则避开最小化/最大化/关闭按钮），这些位置由 `ui.Context.TitleBar()` 给出，不写死。
- **活力效果（vibrancy）。** 窗口声明 `VibrancyUnderWindow`：macOS 使用 `NSVisualEffectView` 材质，Windows 11 22H2 及以后使用 Mica。窗口盖着一层黑色半透明（`colScrim`，六成不透明），所以背后的桌面只是给窗口染上一层色调，而不是直接透出来；标题栏、页脚与各个面板再各自半透明，叠在这层之上。
- **没有材质的系统上自动退回不透明。** `ui.Context.Vibrancy()` 告知材质是否生效；不生效时（Windows 10、Linux）视图改为绘制不透明背景，半透明面板叠在上面，观感几乎一致。
- **应用固定为暗色外观**（`mygo.Theme.SetSource(mygo.ThemeDark)`），因此材质也是暗色版本，半透明面板上的文字始终有足够对比度。

想预览这些效果而不打开窗口：

```bash
npm run screens
```

每个界面会输出两张图：`<名字>.png` 是没有材质时的样子，`<名字>-vibrant.png` 是窗口带材质、叠在一张模拟桌面背景上的合成结果。

## 功能

窗口分成两栏：左边「新安装」放安装包与安装方式，右边「已安装」列出这台机器上已有的扩展。

**安装**

- 拖放或选择 `.ccx` / `.xdx` / `.zxp` 文件
- 在安装前读取并展示 UXP / XDX / CEP 清单、版本和兼容宿主；若这台机器已经装了同一个扩展，版本显示成 `1.2.26 → 1.2.27`，旧的划掉、新的高亮
- 安装方式默认「侧载安装」，两个方式等高等宽，可随时切换；动作按钮在安装方式之下，两种方式共用同一个名字「立即安装」
- 自动探测 Creative Cloud Desktop 附带的 Unified Plugin Installer Agent（UPIA）
- 使用 Adobe 官方安装服务完成安装，并展示错误代码与诊断输出
- 可将 Photoshop、Adobe XD 或 Illustrator 的 UXP 插件侧载到用户级 UXP 目录
- 可将 CEP 扩展侧载到用户级 `Adobe/CEP/extensions` 目录，并自动开启 `PlayerDebugMode`
- 侧载前检查插件目录、注册目录及 `PS.json` / `XD.json` / `AI.json` 的读写权限
- 安全解压安装包、备份注册文件、去重更新，并在写入失败时自动回滚

**安装完成**

- 标题固定为「安装完成」，正文是一句确定的文案：`已经安装到 Photoshop，打开 Photoshop 即可使用。`，需要重启时换成 `已经安装到 Photoshop，需要重新启动 Photoshop。` 并用警示色标出
- 「安装信息」按行拆开：每条一个标签、下面是它的值，长路径在面板内换行而不是横穿出去，并且可以选中复制

**已安装**

- 读取这台机器上的 UXP 插件（各 Adobe 应用的 `PluginsInfo/v1/*.json` 注册表）与 CEP 扩展（用户级与他人安装的 `Adobe/CEP/extensions`），显示名称、ID、版本、格式、宿主与启用状态
- 按应用分组，每组带该应用的图标：Photoshop、Adobe XD、Illustrator、After Effects 依次在前，其他应用按名称在后，被多个应用加载的归入「多应用」
- 显示扩展自带的图标（UXP 用 `manifest.json` 的 `icons`，CEP 用 `CSXS/manifest.xml` 的 `<Icons>`），没有或读不到时用缺省图标；界面是暗色的，因此取的是**为暗色界面绘制的那一份**（CEP 的 `DarkNormal`，UXP 中 `theme` 含 `dark` 的条目），否则会在深色面板上看不清
- 可移除自己安装的扩展（连同注册信息，链接只删链接），或在访达 / 资源管理器中显示；移除前在行内确认一次

**其他**

- 通过 GitHub Releases 静默检查并安装签名更新

## 开发

只需要 Go 1.27+（MyGo 会在首次构建时自动获取所需工具链版本）。没有前端、没有 Bun；`package.json` 只是命令的快捷方式，没有任何依赖，也不需要 `npm install`。

```bash
npm run dev     # 运行开发版，改动 Go 代码后自动重建重启
npm test        # 视图与安装逻辑的测试，不需要窗口
npm run check   # go vet
npm run build   # 打包当前平台
```

| 脚本 | 做什么 |
|---|---|
| `npm run dev` | `go tool mygo dev`：构建开发版并启动，监视 Go 代码、配置、图标与资源的变化 |
| `npm run build` | `go tool mygo build`：打包当前平台 |
| `npm run build:macos` | 打包 macOS Universal，签名并公证 |
| `npm run build:windows` | 打包 Windows x64 与 NSIS 安装包 |
| `npm test` | `go test ./...` |
| `npm run check` | `go vet ./...` |
| `npm run format` | `gofmt -w .` |
| `npm run screens` | 把每个界面渲染成 `build/screens/*.png` |
| `npm run doctor` | `go tool mygo doctor`：检查这台机器缺什么 |
| `npm run release` | 打包两个平台并上传到 GitHub Release 草稿 |

脚本里的 `go tool mygo` 来自 `go.mod` 声明的 [tool](https://go.dev/doc/modules/managing-dependencies#tools)，版本随 `go.mod` 固定，不需要全局安装。`go get -tool github.com/egoist/mygo/cmd/mygo@latest` 升级它；用 `go install github.com/egoist/mygo/cmd/mygo@latest` 装到 PATH 后，也可以直接敲 `mygo dev`。

`mygo.json` 同时是应用名称、标识、版本与打包设置的唯一来源。`package.json` 刻意**没有** `version` 字段，避免出现第二个版本号，所以不再需要以前那套 `version:sync` / `version:check`：改版本只改 `mygo.json` 一处，页脚显示的版本号与打包用的版本号都来自它。

### 看界面

```bash
npm run screens
```

视图离屏渲染成 PNG，不必打开窗口：每个界面（空态、待安装、权限不足、CEP、完成、错误、更新）各写两张 `build/screens/*.png`，一张没有材质、一张叠在模拟桌面背景上，用来检查布局与半透明效果。它的底层是 `OPENUXP_RENDER_DIR=… go test -run TestRenderScreens`。

### 测试真实安装包

```bash
OPENUXP_SAMPLE_CCX=/path/to/plugin.ccx npm test
```

除了读取清单，该测试还会把插件侧载到临时目录，并校验注册文件写入结果。

## 代码结构

| 文件 | 内容 |
|---|---|
| `main.go` | 创建窗口，把视图接到 MyGo |
| `app.go` | 窗口状态与界面（顶栏、页脚、各个界面） |
| `actions.go` | 用户操作：选择文件、拖放、切换安装方式、开始安装 |
| `update.go` | 自动更新的检查与安装 |
| `theme.go` | 配色，沿用 Web 版本的设计 |
| `assets.go` | 嵌入并解码图标（应用图标、安装包图形、宿主图标、界面图标） |
| `package.go` | 解析 UXP / XDX / CEP 清单 |
| `installed.go` | 读取本机已安装的扩展、按应用分组、移除扩展 |
| `icons.go` | 读取扩展自带图标，带缓存与缺省回退 |
| `environment.go` | 探测 UPIA 与 Creative Cloud，调用官方安装服务 |
| `sideload.go` | 侧载前检查、注册文件更新、备份与回滚 |
| `cep.go` | CEP 的 `PlayerDebugMode` |
| `archive.go` | 安全解压与原子写入 |

## 构建

```bash
npm run build:macos      # macOS Universal，签名与公证
npm run build:windows    # Windows x64 与 NSIS 安装包
npm run build            # 当前平台，不发布
```

两个平台一起打包（签名与安装包都会产出，但不上传）：

```bash
go tool mygo build -platform darwin/universal,windows/amd64
```

产物位于 `build/<os>-<arch>/`：

| 平台 | 产物 |
|---|---|
| macOS | `OpenUXP Installer.app`、`OpenUXP Installer 1.0.5.dmg` |
| Windows | `OpenUXP Installer.exe`、`OpenUXP Installer Setup 1.0.5 amd64.exe` |

MyGo 不依赖 cgo，因此任意机器都能为两个平台打包。Windows 安装包由 [NSIS](https://nsis.sourceforge.io) 生成：Windows 上缺少时会自动下载，macOS 上可 `brew install makensis`，未安装时会跳过安装包而只产出可执行文件。

## macOS 签名与公证

在钥匙串中安装 `Developer ID Application` 证书后，在 `mygo.json` 里填写身份与公证配置：

```json
{
  "macos": {
    "signingIdentity": "Developer ID Application: Example Inc (TEAMID)",
    "notarize": { "keychainProfile": "notary" }
  }
}
```

公证凭据只需在钥匙串里保存一次：

```bash
xcrun notarytool store-credentials notary
```

`go tool mygo build` 会使用 Hardened Runtime 签名、提交 DMG 公证，并把票据装订到 DMG 与 `.app` 上。

## Windows 签名

```json
{
  "windows": { "certificate": "certs/code-signing.pfx" }
}
```

证书密码来自环境变量 `MYGO_WINDOWS_CERTIFICATE_PASSWORD`。使用硬件令牌或云签名服务时改用 `windows.signCommand`。没有证书时构建照样完成，只是 SmartScreen 会提示未知发布者。

## 自动更新

更新包通过 MyGo 的独立签名校验，使用 `mygo keygen` 生成的密钥对：

```bash
go tool mygo keygen -o secret
```

公钥写入 `mygo.json` 的 `updates.publicKey`，私钥保存在 `secret/mygo-update.key`（已被 Git 忽略）。请妥善备份私钥：丢失后已安装的旧版本将无法验证后续更新。

应用启动后会静默检查 GitHub Releases，发现新版本时在窗口底部显示版本、下载进度与「立即更新」，安装完成后自动重启。构建时提供私钥即可产出签名更新包：

```bash
MYGO_UPDATER_PRIVATE_KEY="$(cat secret/mygo-update.key)" go tool mygo build -platform darwin/universal,windows/amd64
```

`build/<os>-<arch>/update-<os>-<arch>.json` 是该平台的更新清单；`CHANGELOG.md` 中 `## <版本>` 一节会成为发行说明。

## 发布

```bash
npm run release
```

它执行 `go tool mygo build -platform darwin/universal,windows/amd64 -upload`：打包两个平台，并把 DMG、安装包与更新文件上传到该版本的草稿 Release。上传需要已登录的 [GitHub CLI](https://cli.github.com)；在 GitHub 上发布那个草稿，应用就能看到新版本。

想先在本机确认产物再上传，用 `npm run build:macos` 与 `npm run build:windows`，它们不碰网络。

## 安装原理

应用调用 Creative Cloud Desktop 自带的 UPIA：

- macOS：`UnifiedPluginInstallerAgent --install /path/to/plugin.ccx`
- Windows：`UnifiedPluginInstallerAgent.exe /install C:\path\to\plugin.ccx`

UPIA 不能单独下载；若应用提示未找到安装服务，请安装或更新 Adobe Creative Cloud Desktop。CEP 的 `.zxp` 不走官方服务，只提供侧载。

## 侧载安装

UXP 侧载支持 manifest 宿主为 Photoshop（`PS` / `PHSP`）、Adobe XD（`XD`）或 Illustrator（`AI` / `ILST`）的插件。应用会把插件解压到当前用户的 `Adobe/UXP/Plugins/External` 目录，并按宿主更新 `PluginsInfo/v1/PS.json`、`XD.json` 和/或 `AI.json`。同时声明多个宿主时写入多份注册文件，插件文件只安装一份。

侧载会先把插件解压到目标目录中的临时文件夹、校验清单未在读取过程中变化，再用一次重命名替换旧版本；注册文件写入前会先备份，任一步骤失败都会回滚插件与注册文件。侧载插件不由 Creative Cloud 管理，使用前请确认安装包来源可信。

## CEP 扩展

`.zxp` 会安装到当前用户的 `Adobe/CEP/extensions` 目录。未签名的 CEP 扩展需要调试模式才能加载，因此安装时会自动为 `CSXS.6` 到 `CSXS.14` 写入 `PlayerDebugMode=1`（macOS 使用 `defaults write`，Windows 写入当前用户注册表）。完成后请完全退出并重新打开对应的 Adobe 应用。

## XDX 插件

`.xdx` 是 Adobe XD 的插件包。应用会按 UXP 清单读取名称、版本和宿主，既可以通过官方安装服务安装，也可以侧载到 Adobe XD 的用户级 UXP 目录。

## 已安装扩展

右栏读取两个地方：

- **UXP**：各 Adobe 应用写下的注册表 `Adobe/UXP/PluginsInfo/v1/<应用>.json`。同一个 `pluginId` 出现在多个应用的注册表里时合并成一条，宿主取并集；插件目录由注册表里的 `$localPlugins/External/...` 还原。
- **CEP**：`Adobe/CEP/extensions`（当前用户，可管理）与 `/Library/Application Support/Adobe/CEP/extensions`（所有用户，只读）。只认带有 `CSXS/manifest.xml` 的目录，跳过隐藏文件；符号链接按链接本身读取。

**读取是异步的，分两步。** 第一步扫描注册表与清单，得出列表后立刻显示；第二步才去读图标，读完再刷新一次。两步都在后台 goroutine 里跑，窗口启动与安装流程完全不等待它们，所以扩展很多或某个图标很大时，界面照样即时可用。两次扫描之间用一个世代号区分：较慢的一次回来时若已有更新的结果，就丢弃自己（`installedGeneration`）。

**图标分明暗。** Adobe 把两套图都放进包里，靠名字或属性区分给哪种界面用，而本应用的界面是暗色的，必须取对，否则深色图标落在深色面板上会看不清：

- CEP：`CSXS/manifest.xml` 的 `<Icons>` 里，`Normal` 是给浅色界面的（画面本身是深色的），`DarkNormal` 才是给暗色界面的。取 `DarkNormal`，没有才退到 `Normal`，再退到列出的第一个。
- UXP：`manifest.json` 的 `icons` 数组按 `theme` 标注用途，取 `theme` 含 `dark` / `darkest` 的条目（它的画面是浅色的）。同时优先 `species` 为 `pluginList` 的条目，再优先尺寸大的；有些清单（如摹客）既不写 `species` 也不写 `pluginList`，此时退到列出的第一个而不是放弃。

图标按路径缓存，重新扫描不会重复读取或解码，渲染器也不会因为换了 `*ui.Bitmap` 而反复重建纹理。

**面板不滚动。** 左栏只有装着安装包时才需要滚动；空态下的拖放面板直接填满整栏，因此永远不会出现「无从滚起的滚动条」。

**管理范围。** 只有当前用户 Adobe 目录里的扩展可以移除：UXP 会先删掉各应用注册表里的条目，再删插件目录；CEP 若是指向工作副本的符号链接，只删链接、保留目标。他人安装的扩展只列出、只能「显示」。移除是破坏性操作，因此先在行内确认一次。

## 与 Tauri 版本的差异

| | Tauri 版本 | 本版本 |
|---|---|---|
| 后端 | Rust + `zip` / `serde_json` | Go + `archive/zip` / `encoding/json` |
| 界面 | React 18 + Vite，在 WebView 中 | MyGo 原生界面，Go 编写，GPU 绘制 |
| 界面代码 | `App.tsx` + `styles.css` + 图标字体 | `app.go`，每个界面一个方法 |
| 前后端通信 | `invoke` 命令、`@tauri-apps/api` | 无，视图直接调用 Go 函数 |
| 拖放 | `onDragDropEvent` 事件 | `Element.DroppedFiles` / `Element.FileDragOver` |
| 图标 | SVG 文件由浏览器加载 | 编译进二进制的 SVG 与 PNG |
| 自动更新 | Tauri Updater + `latest.json` | `mygo.Updater` + `update-<平台>.json` |
| 打包 | `tauri build`、cargo-xwin、自建发布脚本 | `go tool mygo build -platform ...` |
| 窗口装饰 | 系统标题栏 | 无标题栏，视图自绘；活力效果（macOS 材质 / Windows 11 Mica） |
| 常用命令 | `package.json` 脚本 + `scripts/*.mjs`、`*.sh` | `package.json` 脚本，只调用 MyGo 与 Go，无依赖 |
| 版本来源 | `package.json` + 同步脚本 | `mygo.json`（`package.json` 无 `version`） |
| 最低系统 | macOS 10.15 | macOS 12.0（MyGo 默认） |
| 发布上传 | QRls（GitHub + 蓝奏云） | `mygo build -upload`（GitHub） |
| 暗色外观 | 固定暗色 | 固定暗色，沿用同一套配色 |
