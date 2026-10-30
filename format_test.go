package main

import (
	"testing"
	"time"

	"github.com/10ef/ABeast/internal/monitor"
	"github.com/10ef/ABeast/internal/probe"
)

func TestTrayTitle(t *testing.T) {
	jp := monitor.Route{Trace: probe.Trace{IP: "1.1.1.1", Country: "JP", Latency: 120 * time.Millisecond}, Geo: probe.Geo{Country: "日本", Region: "東京都", City: "東京都"}}
	us := monitor.Route{Trace: probe.Trace{IP: "2.2.2.2", Country: "US"}, Geo: probe.Geo{Country: "美国", Region: "加州"}}
	down := monitor.Route{Err: "超时"}
	for _, c := range []struct {
		tpl    string
		routes []monitor.Route
		want   string
	}{
		{"{flag} {code}", []monitor.Route{jp, jp}, "🇯🇵 JP"},
		{"{code}", []monitor.Route{jp, us}, "JP  US"},
		{"{flag}", []monitor.Route{jp, down}, "🇯🇵  ✕"},
		{"{country}·{city}", []monitor.Route{jp}, "日本·東京都"},
		{"{city}", []monitor.Route{us}, "加州"},
		{"{flag} {ms}ms", []monitor.Route{jp}, "🇯🇵 120ms"},
		{"", []monitor.Route{jp}, ""},
	} {
		if got := trayTitle(monitor.Snapshot{Routes: c.routes}, c.tpl); got != c.want {
			t.Errorf("%q: got %q, want %q", c.tpl, got, c.want)
		}
	}
}

func TestScoreLine(t *testing.T) {
	r := monitor.Route{Scores: []probe.Score{
		{Source: "Claude", Score: 80, Label: "纯净", Region: "正常"},
		{Source: "IP", Score: 42},
	}}
	if got, want := scoreLine(r), "🟢 Claude 80 纯净 ▰▰▰▰▰▰▰▰▱▱ · 🟠 IP 42 · 支持地区 正常"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
	r.Scores[0].Region, r.Scores[0].Unsupported = "不支持", true
	r.Scores[1].Err = "超时"
	if got, want := scoreLine(r), "🟢 Claude 80 纯净 ▰▰▰▰▰▰▰▰▱▱ · IP 评分 ✕ 超时 · ⛔ 支持地区 不支持"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
	if got := formatTitle("{flag} {score}", monitor.Route{Trace: probe.Trace{Country: "JP"}, Scores: r.Scores}); got != "🇯🇵 80" {
		t.Errorf("{score}: got %q", got)
	}
}
