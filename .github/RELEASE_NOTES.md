ABeast v0.1.1 makes held agents resume faster, so an interrupted Claude Code or Codex session can continue while its prompt cache is still warm.

1. **Immediate re-check when the proxy returns:** when the Mihomo / Clash controller comes back after being unreachable, ABeast probes the exits at once instead of waiting for the next 30-second round.
2. **One clean probe resumes held agents:** while protection mode holds the agents, a route recovers after a single good probe instead of `recover_threshold` consecutive ones. Routes without held agents keep the configured threshold.

Paused agents keep their whole session; at most the request interrupted by the network change is retried.

## Downloads

- **macOS 12 or later:** download `ABeast-0.1.1-darwin-universal.dmg` for Apple silicon or Intel, then drag ABeast.app into Applications.
- **Windows x64:** download `ABeast-0.1.1-windows-amd64.zip`, extract the entire archive, and run ABeast.exe.
- **Windows ARM64:** download `ABeast-0.1.1-windows-arm64.zip`, extract the entire archive, and run ABeast.exe.
- **SHA256SUMS.txt:** SHA-256 checksums for the three packages.

The macOS app is ad hoc signed and not notarized. Windows packages are not code signed. The app interface is currently Chinese. Protection is off by default; review the country policy and proxy setup before enabling it.
