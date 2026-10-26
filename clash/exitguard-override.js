// ABeast override script for Mihomo (Clash Meta) clients: FlClash (配置 →
// 右上角「脚本」), Clash Verge Rev (全局扩展脚本), mihomo-party (覆写 → JavaScript).
//
// For each AI vendor it adds:
//   - a Selector group "🛡 EG-<vendor>" = [the policy the vendor's domains
//     already used, REJECT], and rules in front sending the vendor's domains
//     to it. ABeast switches the group to REJECT while the exit is unsafe.
//   - a listener on 127.0.0.1 that goes straight to that policy, bypassing the
//     group, for ABeast to probe the exit while the group rejects.
// It also turns on the external controller when the config has none.

const VENDORS = [
  {
    group: "🛡 EG-OpenAI",
    port: 17891,
    domains: ["openai.com", "chatgpt.com", "oaistatic.com", "oaiusercontent.com"],
  },
  {
    group: "🛡 EG-Anthropic",
    port: 17892,
    domains: ["anthropic.com", "claude.ai", "claude.com"],
  },
];
const CONTROLLER = "127.0.0.1:9090";

// policyFor returns the policy of the first rule that matches one of the
// domains, or the MATCH rule's. Rule sets and GeoSite are not evaluated.
function policyFor(rules, domains) {
  for (const rule of rules) {
    const [type, value, policy] = String(rule).split(",").map((s) => s.trim());
    if (type === "MATCH") return value;
    if ((type === "DOMAIN-SUFFIX" || type === "DOMAIN") &&
        domains.some((d) => value === d || value.endsWith("." + d))) {
      return policy;
    }
  }
  return null;
}

function main(config) {
  // Never ship a shared controller password. Keep the client's own secret.
  if (!config["external-controller"] && !config.secret) {
    throw new Error("Set a private Mihomo secret before enabling the ABeast override.");
  }
  const ours = (name) => VENDORS.some((v) => v.group === name);
  const groups = (config["proxy-groups"] || []).filter((g) => !ours(g.name));
  const rules = (config.rules || []).filter((r) => !ours(String(r).split(",")[2]?.trim()));
  const listeners = (config.listeners || []).filter((l) => !String(l.name).startsWith("exitguard-"));

  const head = [];
  for (const v of VENDORS) {
    const upstream = policyFor(rules, v.domains) || (groups[0] && groups[0].name) || "DIRECT";
    groups.push({ name: v.group, type: "select", proxies: [upstream, "REJECT"] });
    for (const d of v.domains) head.push(`DOMAIN-SUFFIX,${d},${v.group}`);
    listeners.push({
      name: "exitguard-" + v.port,
      type: "mixed",
      listen: "127.0.0.1",
      port: v.port,
      proxy: upstream,
    });
  }

  config["proxy-groups"] = groups;
  config.rules = head.concat(rules);
  config.listeners = listeners;
  if (!config["external-controller"]) {
    config["external-controller"] = CONTROLLER;
  }
  return config;
}
