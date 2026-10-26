ABeast v0.1.0 is the first public release: a lightweight menu bar and tray app for monitoring Claude Code and Codex exit routes with a Mihomo (Clash Meta) proxy.

1. **Exit monitoring:** see the exit IP, country, location, latency, and proxy node for each API route, with notifications when conditions change.
2. **Protection mode:** pin proxy nodes, close abnormal AI connections, and optionally block unsafe routes through the bundled override script.
3. **Tray status:** six dog icons show checking, monitoring, protected, warning, paused, and recovering states on macOS and Windows.

## Downloads

- **macOS 12 or later:** download `ABeast-0.1.0-darwin-universal.dmg` for Apple silicon or Intel, then drag ABeast.app into Applications.
- **Windows x64:** download `ABeast-0.1.0-windows-amd64.zip`, extract the entire archive, and run ABeast.exe.
- **Windows ARM64:** download `ABeast-0.1.0-windows-arm64.zip`, extract the entire archive, and run ABeast.exe.
- **SHA256SUMS.txt:** SHA-256 checksums for the three packages.

The macOS app is ad hoc signed and not notarized. Windows packages are not code signed. The app interface is currently Chinese. Protection is off by default; review the country policy and proxy setup before enabling it. ABeast cannot currently intercept traffic if the proxy client exits unexpectedly.
