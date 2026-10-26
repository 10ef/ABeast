package monitor

import (
	"context"
	"net/http/httptest"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/10ef/ABeast/internal/clash"
	"github.com/10ef/ABeast/internal/config"
)

func TestReachesDirect(t *testing.T) {
	proxies := map[string]clash.ProxyInfo{
		"自动":    {Type: "URLTest", All: []string{"日本 02", "DIRECT"}},
		"故障转移":  {Type: "Fallback", All: []string{"日本 02", "选择"}},
		"选择":    {Type: "Selector", Now: "DIRECT", All: []string{"DIRECT", "日本 02"}},
		"稳定":    {Type: "URLTest", All: []string{"日本 02", "香港 01"}},
		"手动含直连": {Type: "Selector", Now: "日本 02", All: []string{"日本 02", "DIRECT"}},
	}
	for group, want := range map[string]bool{"自动": true, "故障转移": true, "稳定": false, "手动含直连": false} {
		if got := reachesDirect(proxies, group, 0); got != want {
			t.Errorf("%s: %v", group, got)
		}
	}
}

func TestSelfCheck(t *testing.T) {
	fake := newFakeClash()
	fake.tun = map[string]any{"enable": false}
	fake.proxies["♻️ 自动"] = clash.ProxyInfo{Name: "♻️ 自动", Type: "URLTest", Now: "日本 03", All: []string{"日本 03", "DIRECT"}}
	fake.proxies["⚡ 测速"] = clash.ProxyInfo{Name: "⚡ 测速", Type: "URLTest", Now: "日本 03", All: []string{"日本 03", "香港 01"}}
	srv := httptest.NewServer(fake)
	defer srv.Close()
	cfg := config.Default()
	cfg.Mihomo = config.Mihomo{Controller: srv.URL, Secret: "s3cret"}
	m := New(cfg)
	routes := []Route{
		{Target: config.Target{Name: "Claude"}, Chain: []string{"日本 03", "♻️ 自动", "🌏 ChatGPT/Claude"}},
		{Target: config.Target{Name: "Codex"}, Chain: []string{"日本 03", "⚡ 测速"}},
	}
	ctx := context.Background()

	risks := m.selfCheck(ctx, routes, false)
	var texts []string
	for _, r := range risks {
		texts = append(texts, r.Text)
	}
	want := []string{"TUN 未开启", "♻️ 自动 会自动选择，且可能选到 DIRECT", "⚡ 测速 会自动换节点"}
	if len(risks) != len(want) {
		t.Fatalf("%q", texts)
	}
	for i, w := range want {
		if !strings.Contains(texts[i], w) {
			t.Fatalf("%d: %q", i, texts)
		}
	}
	if !risks[0].High || !risks[1].High || risks[2].High {
		t.Fatalf("%+v", risks)
	}

	// TUN on without strict-route, pinned: only the hints that still hold.
	fake.tun = map[string]any{"enable": true, "auto-route": true}
	risks = m.selfCheck(ctx, routes, true)
	texts = texts[:0]
	for _, r := range risks {
		texts = append(texts, r.Text)
	}
	strict := slices.ContainsFunc(texts, func(s string) bool { return strings.Contains(s, "strict-route") })
	if strict != (runtime.GOOS != "darwin") {
		t.Fatalf("%q", texts)
	}
	if slices.ContainsFunc(texts, func(s string) bool { return strings.Contains(s, "自动换节点") }) {
		t.Fatalf("pinned routes do not move: %q", texts)
	}

	fake.mode = "direct"
	if risks := m.selfCheck(ctx, routes, true); !strings.Contains(risks[0].Text, "direct") {
		t.Fatalf("%+v", risks)
	}
}
