# Changelog

## 1.0.5

- 使用 [MyGo](https://mygo.egoist.dev/) 重写桌面应用，不再使用 WebView：界面由 MyGo 在 GPU 上绘制，用 Go 编写，没有 HTML、前端构建与内置浏览器。
- 功能与此前的 Tauri 版本一致：读取并展示 UXP / XDX / CEP 清单，探测 Creative Cloud 与 UPIA，官方安装、UXP / CEP 侧载、自动开启 `PlayerDebugMode`、侧载前权限检查、备份与失败回滚。
- 支持 macOS（Universal，签名与公证）与 Windows（x64，NSIS 安装包）构建。
- 更新包使用 MyGo 的独立签名校验，替代 Tauri 的更新签名。
