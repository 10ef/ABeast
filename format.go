package main

import (
	"fmt"
	"github.com/10ef/ABeast/internal/envcheck"
	"github.com/10ef/ABeast/internal/monitor"
	"github.com/10ef/ABeast/internal/probe"
	"strings"
)

func place(g probe.Geo) string {
	parts := []string{g.Country}
	if g.Region != "" && g.Region != g.City {
		parts = append(parts, g.Region)
	}
	if g.City != "" {
		parts = append(parts, g.City)
	}
	return strings.Join(parts, " ")
}

func routeLine(r monitor.Route) string {
	prefix := ""
	if r.Unsafe {
		prefix = "⚠️ "
	}
	if r.Err != "" {
		return fmt.Sprintf("%s%s  ✕ %s", prefix, r.Target.Name, r.Err)
	}
	loc := place(r.Geo)
	if strings.TrimSpace(loc) == "" {
		loc = r.Trace.Country
	}
	return fmt.Sprintf("%s%s  %s%s · %dms", prefix, r.Target.Name, probe.FlagBefore(r.Trace.Country), loc, r.Trace.Latency.Milliseconds())
}

func routeDetail(r monitor.Route) string {
	parts := []string{r.Target.Host}
	if r.Trace.IP != "" {
		parts = append(parts, r.Trace.IP)
	}
	if r.Geo.ISP != "" {
		parts = append(parts, r.Geo.ISP)
	}
	if r.Trace.Via != "" {
		parts = append(parts, "经 "+r.Trace.Via)
	}
	if r.Node != "" {
		parts = append(parts, "节点 "+r.Node)
	}
	if r.Err != "" && r.Fails > 0 {
		parts = append(parts, fmt.Sprintf("失败 %d 次", r.Fails))
	}
	return strings.Join(parts, " · ")
}

// envLine is the environment check in one line, with the signals it hit:
// "🟡 环境检测 45/100 中等风险 · 系统时区 · 时区偏移".
func envLine(r envcheck.Report) string {
	dot := map[string]string{"low": "🟢", "medium": "🟡", "high": "🔴"}[r.Band]
	parts := []string{fmt.Sprintf("%s 环境检测 %d/100 %s", dot, r.Total, r.BandName())}
	for _, s := range r.Signals {
		if s.Hit() {
			parts = append(parts, s.Name)
		}
	}
	return strings.Join(parts, " · ")
}

// signalLine is one signal of the environment check:
// "⚠️ 系统时区  Asia/Shanghai  +26/26".
func signalLine(s envcheck.Signal) string {
	mark := "✓"
	if s.Hit() {
		mark = "⚠️"
	}
	return fmt.Sprintf("%s %s  %s  +%d/%d", mark, s.Name, s.Raw, s.Points, s.Weight)
}

// titlePresets are the menu's choices for the text beside the icon.
var titlePresets = []struct{ label, tpl string }{
	{"国旗 + 代码", "{flag} {code}"},
	{"仅国旗", "{flag}"},
	{"仅代码", "{code}"},
	{"国家", "{country}"},
	{"城市", "{city}"},
	{"国旗 + 延迟", "{flag} {ms}ms"},
	{"仅图标", ""},
}

// formatTitle fills a TrayTitle template with one route's exit.
func formatTitle(tpl string, r monitor.Route) string {
	if r.Err != "" {
		return "✕"
	}
	city := r.Geo.City
	if city == "" {
		city = r.Geo.Region
	}
	country := r.Geo.Country
	if country == "" {
		country = r.Trace.Country
	}
	return strings.TrimSpace(strings.NewReplacer(
		"{flag}", probe.Flag(r.Trace.Country),
		"{code}", r.Trace.Country,
		"{country}", country,
		"{region}", r.Geo.Region,
		"{city}", city,
		"{ip}", r.Trace.IP,
		"{isp}", r.Geo.ISP,
		"{ms}", fmt.Sprint(r.Trace.Latency.Milliseconds()),
	).Replace(tpl))
}

// trayTitle is the text beside the icon (macOS): the template filled once
// per distinct exit, so routes leaving through different places all show.
func trayTitle(s monitor.Snapshot, tpl string) string {
	if strings.TrimSpace(tpl) == "" {
		return ""
	}
	var parts []string
	seen := map[string]bool{}
	for _, r := range s.Routes {
		if t := formatTitle(tpl, r); t != "" && !seen[t] {
			seen[t] = true
			parts = append(parts, t)
		}
	}
	return strings.Join(parts, "  ")
}

func tooltip(s monitor.Snapshot) string {
	lines := []string{"ABeast", stateForTray(s).label(s)}
	for _, r := range s.Routes {
		lines = append(lines, routeLine(r))
	}
	return strings.Join(lines, "\n")
}
