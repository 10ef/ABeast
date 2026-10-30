package main

import (
	"fmt"
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

// scoreLine shows a route's reputation, the first source with a bar:
// "🟢 Claude 80 纯净 ▰▰▰▰▰▰▰▰▱▱ · 🟢 IP 80 · 支持地区 正常".
func scoreLine(r monitor.Route) string {
	var parts []string
	region := ""
	for i, s := range r.Scores {
		if s.Err != "" {
			parts = append(parts, s.Source+" 评分 ✕ "+s.Err)
			continue
		}
		p := strings.TrimSpace(fmt.Sprintf("%s %s %d %s", scoreDot(s.Score), s.Source, s.Score, s.Label))
		if i == 0 {
			p += " " + scoreBar(s.Score)
		}
		parts = append(parts, p)
		if region == "" && s.Region != "" {
			region = "支持地区 " + s.Region
			if s.Unsupported {
				region = "⛔ " + region
			}
		}
	}
	if region != "" {
		parts = append(parts, region)
	}
	return strings.Join(parts, " · ")
}

// scoreDot colors a score the way ip.net.coffee's scale does: 0 high risk
// to 100 trusted.
func scoreDot(n int) string {
	switch {
	case n < 25:
		return "🔴"
	case n < 50:
		return "🟠"
	case n < 75:
		return "🟡"
	}
	return "🟢"
}

// scoreBar draws a score out of ten blocks.
func scoreBar(n int) string {
	full := min(max((n+5)/10, 0), 10)
	return strings.Repeat("▰", full) + strings.Repeat("▱", 10-full)
}

// lowestScore is a route's worst score, for {score}; empty without one.
func lowestScore(r monitor.Route) string {
	low := -1
	for _, s := range r.Scores {
		if s.Err == "" && (low < 0 || s.Score < low) {
			low = s.Score
		}
	}
	if low < 0 {
		return ""
	}
	return fmt.Sprint(low)
}

// titlePresets are the menu's choices for the text beside the icon.
var titlePresets = []struct{ label, tpl string }{
	{"国旗 + 代码", "{flag} {code}"},
	{"仅国旗", "{flag}"},
	{"仅代码", "{code}"},
	{"国家", "{country}"},
	{"城市", "{city}"},
	{"国旗 + 延迟", "{flag} {ms}ms"},
	{"国旗 + 信任评分", "{flag} {score}"},
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
		"{score}", lowestScore(r),
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
