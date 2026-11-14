package monitor

import (
	"context"
	"fmt"
	"runtime"
	"slices"
	"strings"

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
	g, err := m.blocker.api.General(ctx)
	if err != nil {
		add(false, "无法读取 Clash 运行配置，TUN、DNS 和 IPv6 设置未确认")
	} else {
		var unknown []string
		switch {
		case g.Mode == "direct":
			addRisk(Risk{High: true, Hold: true, Text: "Clash 处于直连（direct）模式，流量都不走代理"})
		case g.Tun.Enable == nil:
			unknown = append(unknown, "TUN")
		case !*g.Tun.Enable:
			addRisk(Risk{High: true, Hold: true, Text: "TUN 未开启，无法确认各 Agent 均走代理"})
		default:
			switch {
			case g.Tun.AutoRoute == nil:
				unknown = append(unknown, "TUN auto-route")
			case !*g.Tun.AutoRoute:
				add(true, "TUN auto-route 已关闭，请核对手动路由是否接管 Agent 流量")
			}
			if runtime.GOOS != "darwin" {
				if g.Tun.StrictRoute == nil {
					unknown = append(unknown, "TUN strict-route")
				} else if !*g.Tun.StrictRoute {
					add(false, "TUN strict-route 已关闭，部分流量可能绕过 TUN")
				}
			}
			if g.Tun.DNSHijack == nil {
				unknown = append(unknown, "TUN DNS 劫持")
			} else if len(*g.Tun.DNSHijack) == 0 {
				add(false, "TUN DNS 劫持列表为空")
			}
		}
		if g.IPv6 == nil {
			unknown = append(unknown, "Clash IPv6")
		} else if *g.IPv6 {
			add(false, "Clash IPv6 已开启，请核对系统 IPv6 路由")
		}
		if len(unknown) > 0 {
			add(false, "Clash 未报告：%s", strings.Join(unknown, "、"))
		}
	}

	proxies, err := m.blocker.api.Proxies(ctx)
	for _, r := range routes {
		if len(r.Chain) == 0 {
			continue
		}
		outbound := r.Chain[0]
		if isDirect(proxies, outbound) {
			addRisk(Risk{High: true, Hold: true, Text: fmt.Sprintf("%s 探测经 %s 直连", r.Target.Name, outbound)})
		}
		if err != nil {
			continue
		}
		for _, grp := range r.Chain[1:] {
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

func isDirect(proxies map[string]clash.ProxyInfo, name string) bool {
	return name == "DIRECT" || strings.EqualFold(proxies[name].Type, "Direct")
}

// reachesDirect reports whether an automatic group can end up on DIRECT
// directly or through a member group.
func reachesDirect(proxies map[string]clash.ProxyInfo, group string, depth int) bool {
	if depth > 5 {
		return false
	}
	info := proxies[group]
	switch {
	case info.Type == "Selector":
		return isDirect(proxies, info.Now) || (info.Now != "" && reachesDirect(proxies, info.Now, depth+1))
	case !slices.Contains(autoGroups, info.Type):
		return false
	}
	for _, member := range info.All {
		if isDirect(proxies, member) || reachesDirect(proxies, member, depth+1) {
			return true
		}
	}
	return false
}
