<h1><img src="resources/icon.png?v=cc65a0fb50d8" width="48" height="48" align="absmiddle" alt="ABeast icon"> ABeast</h1>

<p>
  <a href="LICENSE"><img src="https://img.shields.io/badge/license-MIT-blue.svg" alt="MIT license"></a>
  <img src="https://img.shields.io/badge/platform-macOS%20%7C%20Windows-lightgrey" alt="macOS and Windows">
  &nbsp; · &nbsp; English · <a href="README.zh-CN.md">简体中文</a>
</p>

**Lightweight exit monitoring and protection for AI agents.**

The macOS app is under **20 MB** and the Windows executable under **10 MB**, with roughly **11 to 28 MB of memory** depending on platform, and **0.4% of one CPU core** on average. ABeast works with Mihomo (Clash Meta) to monitor exit IPs and countries for Claude Code and Codex, pin nodes, close abnormal connections, and send alerts.

## Features

- **Real exit per API.** Probes `https://api.anthropic.com/cdn-cgi/trace` and `https://chatgpt.com/cdn-cgi/trace` through the same proxy rules as the agents: exit IP, country, city, ISP, latency and the node in use.
- **Protection mode — one switch:**
  - **Pin the node (no leak).** Selects the agents' current node in the manual group their rule goes through. When that node dies or jitters, requests fail inside the proxy — they never move to another node, region, or DIRECT. Agents just retry.
  - **Hold the agents.** When the proxy cannot contain the traffic — client quit or crashed while the exit is wrong, `direct` mode, TUN off without an env proxy — ABeast pauses `claude` / `codex` (SIGSTOP / NtSuspendProcess) within about 1–2 s and resumes them when it is safe.
  - **Cut stray connections.** Every 500 ms, closes AI connections going through DIRECT or a node in a blocked region.
  - **Optional REJECT.** With the bundled [override script](clash/exitguard-override.js), unsafe routes are also switched to `REJECT` in the proxy.
  - ABeast cannot currently intercept traffic if the proxy client exits unexpectedly.
- **Leak self-check.** Flags `direct` mode, TUN off, missing strict-route, and AI groups that can fall back to DIRECT.
- **Alerts.** Notifications on exit changes, new high risks, lost proxy controller, and paused/resumed agents; six tray states.

## Performance and size

| | |
|---|---|
| macOS (universal DMG) | 7.6 MB download · < 20M installed |
| Windows x64 / ARM64 (ZIP) | 4.3 / 4.0 MB download · < 10M exe |
| Memory | ≈ 28 MB on macOS (memory footprint) · ≈ 11 MB on Windows (Task Manager) |
| CPU | ≈ 0.4% of one core on average (one probe round every 30 s), idle in between |

Pure Go, no cgo, no WebView or Electron. Protection mode adds a local Clash API poll every 500 ms.

## Usage

1. Download from [Releases](https://github.com/10ef/ABeast/releases). macOS: drag **ABeast.app** to Applications (macOS 12+). Windows: unzip the whole archive and run **ABeast.exe**. Builds are not notarized/code-signed; approve them through the system prompt.
2. Start your proxy client, preferably in **TUN** mode (plus strict-route on Windows).
3. Click the dog icon: check the Claude and Codex exits.
4. Turn on **Protection mode**. It requires the Clash API controller. Clash Verge Rev and mihomo are detected automatically; in FlClash, enable **External controller** and restart the client. The menu shows the pinned group and node.

Settings live in `~/Library/Application Support/ExitGuard/config.json` (Windows: `%AppData%\ExitGuard\config.json`): blocked/allowed countries, targets, controller. See [configuration](docs/configuration.md).

## Build

Requires the Go version in [go.mod](go.mod).

```sh
git clone https://github.com/10ef/ABeast.git && cd ABeast
go test ./...
go tool mygo dev                                         # run from source
go tool mygo build -platform darwin/universal            # macOS app + DMG (on macOS)
go tool mygo build -platform windows/amd64,windows/arm64 # Windows
```

Output goes to `build/`. Release packaging: [docs/releasing.md](docs/releasing.md).

## Contributing

Issues and pull requests are welcome. Run `go test ./...` before submitting, say what you tested on which OS and proxy client, and strip IPs, secrets and subscription URLs from logs.

## License

[MIT](LICENSE)
