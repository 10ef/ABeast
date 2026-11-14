// ABeast override script for Mihomo (Clash Meta) clients.

const VENDORS = [
  {
    id: "openai",
    group: "🛡 EG-OpenAI",
    port: 17891,
    domains: ["openai.com", "chatgpt.com", "oaistatic.com", "oaiusercontent.com"],
  },
  {
    id: "anthropic",
    group: "🛡 EG-Anthropic",
    port: 17892,
    domains: ["anthropic.com", "claude.ai", "claude.com"],
  },
  {
    id: "cursor",
    group: "🛡 EG-Cursor",
    domains: ["cursor.com", "cursor.sh"],
  },
  {
    id: "copilot",
    group: "🛡 EG-Copilot",
    domains: ["githubcopilot.com", "copilot-proxy.githubusercontent.com"],
  },
];

// Set a proxy or proxy-group name to resolve ambiguous rules. Setting a
// name for cursor or copilot opts in to its domain rules (no probe listener).
const UPSTREAMS = {
  openai: "",
  anthropic: "",
  cursor: "",
  copilot: "",
};

const CONTROLLER = "127.0.0.1:9090";
const within = (host, suffix) => host === suffix || host.endsWith("." + suffix);

// Each vendor subtree must have one known upstream. Stop at an unresolved
// rule that may match; later MATCH or DOMAIN rules cannot decide its policy.
function policyFor(rules, domains) {
  const policies = new Set();
  for (const domain of domains) {
    let covered = false;
    for (const rule of rules) {
      const [type, value, policy] = String(rule).split(",").map((s) => s.trim());
      if (type === "MATCH") {
        policies.add(value);
        covered = true;
        break;
      }
      if (type !== "DOMAIN-SUFFIX" && type !== "DOMAIN") return null;
      if (type === "DOMAIN-SUFFIX" && within(domain, value)) {
        policies.add(policy);
        covered = true;
        break;
      }
      if (within(value, domain)) policies.add(policy);
    }
    if (!covered) return null;
  }
  return policies.size === 1 ? policies.values().next().value : null;
}

function main(config) {
  if (!config["external-controller"] && !config.secret) {
    throw new Error("Set a private Mihomo secret before enabling the ABeast override.");
  }
  const ours = (name) => VENDORS.some((v) => v.group === name);
  const groups = (config["proxy-groups"] || []).filter((g) => !ours(g.name));
  const rules = (config.rules || []).filter((r) => !ours(String(r).split(",")[2]?.trim()));
  const listeners = (config.listeners || []).filter((l) => !VENDORS.some((v) => v.port && l.name === "exitguard-" + v.port));
  const outbounds = new Set([...groups, ...(config.proxies || [])]
    .filter((p) => !["direct", "reject", "pass", "compatible"].includes(String(p.type).toLowerCase()))
    .map((p) => p.name));

  const head = [];
  for (const v of VENDORS) {
    const explicit = UPSTREAMS[v.id].trim();
    if (!v.port && !explicit) continue;
    const upstream = explicit || policyFor(rules, v.domains);
    if (!outbounds.has(upstream) || /^(DIRECT|REJECT|REJECT-DROP|PASS|COMPATIBLE)$/i.test(upstream)) {
      throw new Error(`Cannot determine a proxy upstream for ${v.id}. Set UPSTREAMS.${v.id} to an existing proxy or proxy-group name.`);
    }
    groups.push({ name: v.group, type: "select", proxies: [upstream, "REJECT"] });
    for (const d of v.domains) head.push(`DOMAIN-SUFFIX,${d},${v.group}`);
    if (v.port) {
      listeners.push({
        name: "exitguard-" + v.port,
        type: "mixed",
        listen: "127.0.0.1",
        port: v.port,
        proxy: upstream,
      });
    }
  }

  config["proxy-groups"] = groups;
  config.rules = head.concat(rules);
  config.listeners = listeners;
  if (!config["external-controller"]) {
    config["external-controller"] = CONTROLLER;
  }
  return config;
}
