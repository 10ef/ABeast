package monitor

import (
	"context"
	"fmt"
	"runtime"
	"slices"

	"github.com/10ef/ABeast/internal/clash"
)

// Risk is one way the real IP can leak that the self-check found.
type Risk struct {
	High bool   `json:"high"`           // leaking, or will on the next hiccup; else a hint
	Text string `json:"text"`           // what and why, for the menu
	Hold bool   `json:"hold,omitempty"` // agents leak now: protection mode pauses them
}

// autoGroups pick a member on their own, so they can move a route.
var autoGroups = []string{"URLTest", "Fallback", "LoadBalance"}

// selfCheck looks at how the proxy is set up, once a round, for what lets
// the real IP out: traffic that skips the proxy, and AI routes whose groups
// fall to DIRECT or move to another node on their own. It needs nothing set
// up: everything comes from the Clash API and the probes' chains.
func (m *Monitor) selfCheck(ctx context.Context, routes []Route, pinOn bool) []Risk {
	var risks []Risk
	addRisk := func(r Risk) {
		if !slices.Contains(risks, r) {
			risks = append(risks, r)
		}
	}
	add := func(high bool, format string, a ...any) {
		addRisk(Risk{High: high, Text: fmt.Sprintf(format, a...)})
	}
	// Probes that found HTTPS_PROXY in the environment suggest the agents
	// have it too, and reach the proxy without TUN.
	viaEnv := slices.ContainsFunc(routes, func(r Route) bool { return r.Trace.Via == "env" })
	g, err := m.blocker.api.General(ctx)
	if err == nil {
		switch {
		case g.Mode == "direct":
			addRisk(Risk{High: true, Hold: true, Text: "Clash 处于直连（direct）模式，流量都不走代理"})
		case !g.Tun.Enable:
			addRisk(Risk{High: true, Hold: !viaEnv, Text: "TUN 未开启：不认系统代理的程序（如命令行 Agent）和浏览器 WebRTC 会直连"})
		case !g.Tun.StrictRoute && runtime.GOOS != "darwin":
			add(false, "TUN 未开启 strict-route：部分流量（如 DNS、IPv6）可能绕过 TUN")
		}
		if proxies, err := m.blocker.api.Proxies(ctx); err == nil {
			for _, r := range routes {
				for _, grp := range r.Chain[min(1, len(r.Chain)):] {
					info := proxies[grp]
					if !slices.Contains(autoGroups, info.Type) {
						continue
					}
					if reachesDirect(proxies, grp, 0) {
						add(true, "%s 经过的 %s 会自动选择，且可能选到 DIRECT：节点失效时会直连", r.Target.Name, grp)
					} else if !pinOn && len(info.All) > 1 {
						add(false, "%s 经过的 %s 会自动换节点，可能跳区：建议打开「保护模式」", r.Target.Name, grp)
					}
				}
			}
		}
	}
	slices.SortStableFunc(risks, func(a, b Risk) int {
		switch {
		case a.High && !b.High:
			return -1
		case b.High && !a.High:
			return 1
		}
		return 0
	})
	return risks
}

// reachesDirect reports whether an automatic group can end up on DIRECT:
// listing it, or through a member group that can.
func reachesDirect(proxies map[string]clash.ProxyInfo, group string, depth int) bool {
	if depth > 5 {
		return false
	}
	info := proxies[group]
	switch {
	case info.Type == "Selector":
		return info.Now == "DIRECT" || (info.Now != "" && reachesDirect(proxies, info.Now, depth+1))
	case !slices.Contains(autoGroups, info.Type):
		return false
	}
	for _, member := range info.All {
		if member == "DIRECT" || reachesDirect(proxies, member, depth+1) {
			return true
		}
	}
	return false
}
