ABeast v0.2.1 makes proxy configuration checks clearer and removes unsafe defaults from the optional Clash override.

1. **Safer override:** ambiguous or direct upstream policies now require an explicit proxy selection. Optional Cursor and Copilot presets add basic domain rules without changing the default monitored targets. Update your override script separately from the app.
2. **Network checks:** report TUN routing settings, an empty DNS hijack list, enabled Clash IPv6 and observed DIRECT paths. Missing controller fields are shown as unknown. These checks do not measure DNS leaks or system IPv6 egress.
3. **Clearer environment score:** the score describes local settings, not account-ban probability or network safety.

**Protection behavior change:** when protection is enabled, explicitly disabled TUN pauses configured agents even if ABeast itself uses an environment proxy. ABeast cannot establish that other processes inherited the same proxy settings. Review your setup before enabling protection.

## Downloads

- **macOS 12 or later:** download `ABeast-0.2.1-darwin-universal.dmg` for Apple silicon or Intel, then drag ABeast.app into Applications.
- **Windows x64:** download `ABeast-0.2.1-windows-amd64.zip`, extract the entire archive, and run ABeast.exe.
- **Windows ARM64:** download `ABeast-0.2.1-windows-arm64.zip`, extract the entire archive, and run ABeast.exe.
- **SHA256SUMS.txt:** SHA-256 checksums for the three packages.

The macOS app is ad hoc signed and not notarized. Windows packages are not code signed. The app interface is currently Chinese. Protection is off by default.
