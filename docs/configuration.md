# Configuration and protection

[English README](../README.md) · [中文首页](../README.zh-CN.md)

Open the config from **高级 → 打开配置文件**, edit `config.json`, and restart ABeast. First launch writes the defaults. The interface is currently Chinese; this page uses the visible labels where needed.

## Files and compatibility

| File | Purpose |
| --- | --- |
| `config.json` | Preferences, targets, and optional controller credentials |
| `status.json` | Latest monitoring snapshot, suitable for reading from a local agent hook |
| `pinned.json` | Original proxy choices to restore after pinning |
| `blocked.json` | Original proxy choices to restore after blocking |

Files live in `~/Library/Application Support/ExitGuard/` on macOS or `%AppData%\ExitGuard\` on Windows. ABeast previously used the names ExitGuard and ExitPin. The directory, bundle identifier `dev.cy.exitguard`, override filename, and `EG-*` groups remain unchanged for compatibility. `ABEAST_SIMULATE` is the current simulation variable; `EXITPIN_SIMULATE` and `EXITGUARD_SIMULATE` remain accepted as fallbacks.

## Monitoring defaults

| Setting | Default | Meaning |
| --- | --- | --- |
| `interval_seconds` | `30` | Probe interval, minimum 5 seconds |
| `timeout_seconds` | `8` | Probe timeout, minimum 2 seconds |
| `fail_threshold` | `3` | Consecutive failures before a route is unsafe |
| `recover_threshold` | `2` | Consecutive clean probes required for recovery; one is enough while agents are held |
| `blocked_countries` | `CN, HK, MO, RU, BY, IR, KP, CU, SY` | Always rejected, even if also allowed |
| `allowed_countries` | Empty | When nonempty, only these country codes are accepted |
| `guard_on_country_change` | `true` | A country change from the baseline makes a route unsafe |
| `guard_on_ip_change` | `false` | IP changes notify; set true to also make them unsafe |
| `protect` | `false` | Enables node pinning, connection watching, and applicable blocking/holds |
| `show_direct` | `true` | Also display a direct probe, separate from target route decisions |
| `tray_title` | `"{flag} {code}"` | macOS menu bar text; empty means icon only |

Country codes use uppercase ISO codes. A blocked or non-allowed country is unsafe immediately; connection failures use the threshold. After system wake, a 20-second grace period avoids counting transient failures. To accept an intentional route change, use **高级 → 把当前出口设为正常基线**. This does not override the country policy.

Menu bar text supports `{flag}`, `{code}`, `{country}`, `{region}`, `{city}`, `{ip}`, `{isp}`, and `{ms}`. Windows displays details in the tray menu and tooltip instead.

## Targets and proxy paths

Default targets are Claude (`api.anthropic.com`, process `claude`) and Codex (`chatgpt.com`, process `codex`). Each target has:

- `name` and `host`: display name and a host supporting Cloudflare's `/cdn-cgi/trace`.
- `proxy`: `auto`, `env`, `system`, `direct`, or an HTTP/SOCKS proxy URL. `auto` tries environment configuration, then the system proxy, then direct access. PAC scripts are not evaluated.
- `processes`: executable names that may be paused when a hold is necessary. Check actual process names on your machine.
- `domains`: vendor domains, including their subdomains, watched through the proxy controller. Defaults cover Anthropic/Claude and OpenAI/ChatGPT domains.
- `block_group` and `probe_while_blocked`: optional blocking group and the local probe proxy that bypasses it.

A probe is not packet inspection of the agent process. Different environment variables, PAC routing, application-specific proxies, rules, DNS behavior, or an agent running elsewhere can produce different paths. Apps opened from Finder may not inherit proxy variables from your shell. Verify the agent's own setup as well as the displayed probe.

## Controller setup

ABeast can discover controller settings from supported client configuration locations, including FlClash, Clash Verge Rev, and mihomo-party. It also tries local default ports `9090` and `9097`. You can explicitly set:

```json
{
  "mihomo": {
    "controller": "127.0.0.1:9090",
    "secret": "YOUR_PRIVATE_CONTROLLER_SECRET"
  }
}
```

Merge this into the generated configuration, using the same private secret as your proxy client. A Windows named pipe or a `unix:///path/to/socket` controller address is also supported. Keep controller access local.

Protection is designed around Mihomo's Clash API. Other implementations, including sing-box's Clash API, may expose only a subset of the required endpoints or group behavior. Surge and Stash use different APIs and are not supported by this integration. Monitoring can still work without a controller, but pinning and connection inspection cannot.

Self-checks look for direct mode, missing TUN, missing strict-route on Windows, and automatic groups that can select DIRECT or switch nodes. These are configuration checks, not proof that all network traffic is contained. Explicit proxy configuration can work without TUN, but the current hold decision recognizes environment-proxy probes; a per-target proxy URL alone does not suppress the missing-TUN hold.

## What protection changes

1. **Pin nodes.** Prefer a manual Selector directly containing the observed node. URLTest/Fallback selections are a weaker fallback because the core may still switch on failure. Shared groups affect every route using that group. Manual changes are respected; **换节点（解锁后重新选择）** releases the pin so another node can be selected.
2. **Watch connections.** Every 500 ms, inspect matching vendor-domain connections and close unacceptable exits. An unknown outbound prompts a fresh probe. A direct connection is allowed only after its exit country has been verified as acceptable. Detection is reactive and cannot prevent the first bytes from leaving.
3. **Block routes, when configured.** With the override below, switch unsafe vendor groups to `REJECT`. Normal probes must succeed through the bypass listener before recovery can be confirmed. Without these groups, the other protection mechanisms remain available.
4. **Hold processes when necessary.** Pause configured agent names if the controller is unavailable while an exit is unsafe, or when self-checks require it, such as direct mode or missing TUN without environment-proxy evidence. This can affect all matching configured agents, not just one request. A node failure alone does not pause them. ABeast excludes itself and its ancestor processes. To resume quickly — ideally while the agents' prompt cache is still warm — a reconnecting controller triggers an immediate probe, and a held route recovers after a single clean probe instead of `recover_threshold`.

Normal shutdown restores recorded selections and resumes held processes; after a crash, restoration records are used at the next start. Do not delete them while protection is active. A forced crash can leave a process suspended until recovery or manual intervention. **立即恢复 Agent（关闭保护模式）** resumes held agents and disables protection.

The icon's warning state takes priority over a success checkmark when protection reports errors or high-risk configuration. A past connection-close count does not mean traffic is currently blocked. [Icon states and previews](../resources/tray/README.md).

## Optional per-vendor blocking

Use [clash/exitguard-override.js](../clash/exitguard-override.js) in your Mihomo client's JavaScript override facility. It creates `🛡 EG-OpenAI` and `🛡 EG-Anthropic`, each with the original upstream and `REJECT`, then adds domain rules pointing at them. Rule mode is required.

It also creates loopback-only mixed listeners: `17891` for OpenAI and `17892` for Anthropic. These bypass the blocking group so ABeast can test recovery. Keep those ports free. If no TCP controller exists, the script opens `127.0.0.1:9090` and requires your own existing `secret`; it never supplies a shared password. Existing controller settings are preserved. If you used an older override with a bundled shared secret, replace that secret in your client and ABeast configuration.

The script only recognizes explicit DOMAIN, DOMAIN-SUFFIX, and MATCH rules. It does not evaluate rule sets or GeoSite entries and may fall back to the first group, or DIRECT if no group exists. Inspect the generated groups and listeners before enabling protection, especially with rule-provider configurations.

To remove the integration, turn protection off and quit ABeast first, then remove the override and reload your proxy configuration. This lets ABeast restore selections before the groups disappear.

For a controlled test, use **高级 → 测试 → 模拟 Codex 出口异常**. Simulation exercises real protection actions when protection is on, so it can interrupt configured agents. An environment-based equivalent on macOS is:

```sh
open --env ABEAST_SIMULATE=Codex build/darwin-universal/ABeast.app
```
