ABeast v0.2.2 extends Claude connection monitoring and optional proxy rules to its user-content domain.

1. **Content-domain coverage:** `claudeusercontent.com` and its subdomains now join the existing Claude connection checks and override rules. No additional periodic probes are added.
2. **Existing configurations:** the old default Claude domain list is extended automatically when it still belongs to the default Claude API target. Custom domain lists are preserved.

Update the [Clash override script](https://github.com/10ef/ABeast/blob/v0.2.2/clash/exitguard-override.js) separately to apply the new routing rules. If your profile routes this domain differently from the other Claude domains, explicitly set `UPSTREAMS.anthropic` in your copy of the script.

## Downloads

- **macOS 12 or later:** download `ABeast-0.2.2-darwin-universal.dmg` for Apple silicon or Intel, then drag ABeast.app into Applications.
- **Windows x64:** download `ABeast-0.2.2-windows-amd64.zip`, extract the entire archive, and run ABeast.exe.
- **Windows ARM64:** download `ABeast-0.2.2-windows-arm64.zip`, extract the entire archive, and run ABeast.exe.
- **SHA256SUMS.txt:** SHA-256 checksums for the three packages.

The macOS app is ad hoc signed and not notarized. Windows packages are not code signed. The app interface is currently Chinese. Protection is off by default.
