<h1><img src="resources/icon.png?v=cc65a0fb50d8" width="48" height="48" align="absmiddle" alt="ABeast 图标"> ABeast</h1>

<p>
  <a href="LICENSE"><img src="https://img.shields.io/badge/license-MIT-blue.svg" alt="MIT 许可证"></a>
  <img src="https://img.shields.io/badge/platform-macOS%20%7C%20Windows-lightgrey" alt="macOS 和 Windows">
  &nbsp; · &nbsp; <a href="README.md">English</a> · 简体中文
</p>

**小体积、低占用，专注 AI Agent 的出口监测与保护。**

macOS 应用不到 **25 MB**，Windows 可执行文件不到 **13 MB**；内存占用约 **11 至 28 MB**（因平台而异），平均约占单核 **0.4%**。ABeast 配合 Mihomo（Clash Meta），为 Claude Code、Codex 提供出口 IP 与地区监测、节点锁定、异常连接断开及通知。

## 功能

- **真实出口。** 经过与 Agent 相同的代理规则请求 `https://api.anthropic.com/cdn-cgi/trace` 和 `https://chatgpt.com/cdn-cgi/trace`，显示出口 IP、国家、城市、ISP、延迟和所用节点。
- **保护模式（一个开关）：**
  - **锁定节点（不泄漏）。** 在 Agent 规则经过的手动选择分组里选中当前节点。节点挂掉或抖动时，请求在代理里直接失败，不会换到别的节点、地区或直连。Agent 只会重试。
  - **拦住 Agent。** 代理拦不住的情况下——客户端退出或崩溃且出口异常、直连（direct）模式、TUN 没开且没有环境变量代理——约 1–2 秒内暂停 `claude` / `codex`（SIGSTOP / NtSuspendProcess）；出口恢复正常后几秒内自动继续，通常赶在提示缓存过期之前。会话完整保留，最多重试被打断的那一次请求。
  - **断开走错的连接。** 每 500 毫秒检查一次，断开经过 DIRECT 或屏蔽地区节点的 AI 连接。
  - **可选 REJECT。** 启用自带的[覆写脚本](clash/exitguard-override.js)后，异常路线还会在代理里切到 `REJECT`。
  - 代理客户端异常退出暂时无法拦截。
- **防泄漏自检。** 提示直连模式、TUN 没开、没开 strict-route、AI 分组可能落到 DIRECT。
- **提醒。** 出口变化、新的高风险项、连不上代理控制接口、Agent 暂停与恢复都会弹通知；托盘有六种状态。
- **原生面板。** 点图标打开面板：每条路线一张卡片、保护模式开关和环境检测；右键图标是完整菜单和高级设置。
- **环境检测。** 参考 [fuck-claude.com](https://fuck-claude.com/) 的浏览器检测，按本机时区、时区偏移、首选语言和区域格式打 0–100 分，估算在 Agent 看来是否偏中国化。只读本机设置，不上传。

## 性能与体积

| | |
|---|---|
| macOS（通用 DMG） | 下载约 10 MB · 安装后 < 25M |
| Windows x64 / ARM64（ZIP） | 下载约 5.7 / 5.4 MB · exe < 13M |
| 内存 | macOS 约 28 MB（内存占用）· Windows 约 11 MB（任务管理器） |
| CPU | 平均约占单核 0.4%（每 30 秒检测一轮），其余时间空闲 |

纯 Go、无 cgo，不内嵌 WebView 或 Electron，面板是原生界面。保护模式额外每 500 毫秒读取一次本机 Clash API。

## 使用

1. 从 [Releases](https://github.com/10ef/ABeast/releases) 下载。macOS：把 **ABeast.app** 拖进「应用程序」（macOS 12 及以上）。Windows：解压整个 ZIP，运行 **ABeast.exe**。安装包未公证、未做代码签名，需要在系统提示中手动允许。
2. 启动代理客户端，建议用 **TUN** 模式（Windows 上同时开 strict-route）。
3. 点小狗图标，在面板里确认 Claude、Codex 的出口；右键图标打开完整菜单。
4. 打开「保护模式」。它需要 Clash API 控制接口：Clash Verge Rev 和 mihomo 会自动找到；FlClash 需要打开「外部控制器」并重启一次。菜单会显示锁定的分组和节点。

配置文件在 `~/Library/Application Support/ExitGuard/config.json`（Windows：`%AppData%\ExitGuard\config.json`），可调整屏蔽 / 允许的国家、检测目标和控制接口，详见[配置说明](docs/configuration.md)。

## 构建

需要 [go.mod](go.mod) 中声明的 Go 版本。

```sh
git clone https://github.com/10ef/ABeast.git && cd ABeast
go test ./...
go tool mygo dev                                         # 从源码运行
go tool mygo build -platform darwin/universal            # macOS 应用和 DMG（需在 macOS 上）
go tool mygo build -platform windows/amd64,windows/arm64 # Windows
```

产物在 `build/`。发布打包见 [docs/releasing.md](docs/releasing.md)。

## 参与贡献

欢迎提 Issue 和 PR。提交前运行 `go test ./...`，写明在哪个系统、哪个代理客户端上测过，并去掉日志里的 IP、密钥和订阅地址。

## 许可证

[MIT](LICENSE)
