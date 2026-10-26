package monitor

import (
	"context"
	"net/http/httptest"
	"slices"
	"testing"
	"time"

	"github.com/10ef/ABeast/internal/clash"
	"github.com/10ef/ABeast/internal/config"
)

func conn(id, host, process string, chains ...string) clash.Conn {
	var c clash.Conn
	c.ID = id
	c.Metadata.Host = host
	c.Metadata.Process = process
	c.Chains = chains
	return c
}

func TestWatchCheck(t *testing.T) {
	s := watchState{
		cfg:  config.Default(),
		self: "ExitPin.exe",
		routes: []watchRoute{
			{name: "Claude", domains: []string{"anthropic.com", "claude.ai"}},
			{name: "Codex", domains: []string{"chatgpt.com"}, simulated: false},
		},
		nodes: map[string]string{"日本 03": "JP", "香港 01": "HK"},
	}
	cases := []struct {
		c                   clash.Conn
		route, why, unknown string
	}{
		{conn("1", "api.anthropic.com", "claude", "日本 03", "🛡 EG-Anthropic"), "", "", ""},
		{conn("2", "api.anthropic.com", "claude", "香港 01"), "Claude", "经 香港 01 出口在 HK", ""},
		{conn("3", "claude.ai", "chrome.exe", "DIRECT"), "Claude", "走 DIRECT", ""},
		{conn("4", "chatgpt.com", "codex", "美国 02"), "", "", "美国 02"},
		{conn("5", "notanthropic.com", "x", "DIRECT"), "", "", ""},
		{conn("6", "api.anthropic.com", "exitpin.exe", "DIRECT"), "", "", ""},
		{conn("7", "chatgpt.com", "codex", "REJECT"), "", "", ""},
		{conn("8", "ab.chatgpt.com.", "codex", "香港 01"), "Codex", "经 香港 01 出口在 HK", ""},
	}
	for _, tc := range cases {
		route, why, unknown := s.check(tc.c)
		if route != tc.route || why != tc.why || unknown != tc.unknown {
			t.Errorf("%s: got %q %q %q", tc.c.ID, route, why, unknown)
		}
	}

	// DIRECT a probe went through and saw an allowed country is left alone.
	s.nodes["DIRECT"] = "JP"
	if _, why, _ := s.check(conn("9", "claude.ai", "x", "DIRECT")); why != "" {
		t.Fatal(why)
	}
	s.nodes["DIRECT"] = "CN"
	if _, why, _ := s.check(conn("10", "claude.ai", "x", "DIRECT")); why != "经 DIRECT 出口在 CN" {
		t.Fatal(why)
	}
	// A simulated route has every connection closed, whatever the node.
	s.routes[1].simulated = true
	if _, why, _ := s.check(conn("11", "chatgpt.com", "codex", "日本 03")); why != "模拟异常（测试）" {
		t.Fatal(why)
	}
}

func TestWatchOnceClosesBadConns(t *testing.T) {
	fake := newFakeClash()
	fake.conns = []clash.Conn{
		conn("ok", "chatgpt.com", "codex", "日本 03"),
		conn("hk", "api.anthropic.com", "claude", "香港 01"),
		conn("direct", "claude.ai", "chrome", "DIRECT"),
		conn("new", "chatgpt.com", "codex", "美国 02"),
		conn("other", "example.com", "chrome", "DIRECT"),
	}
	srv := httptest.NewServer(fake)
	defer srv.Close()
	cfg := config.Default()
	cfg.Protect = true
	cfg.Mihomo = config.Mihomo{Controller: srv.URL, Secret: "s3cret"}
	m := New(cfg)
	m.nodes = map[string]string{"日本 03": "JP", "香港 01": "HK"}
	var alerts []string
	m.OnAlert = func(id, title, body string) { alerts = append(alerts, id) }

	m.watchOnce(context.Background(), "ExitPin", map[string]time.Time{})
	if !slices.Equal(fake.closed, []string{"hk", "direct"}) {
		t.Fatalf("closed %v", fake.closed)
	}
	if m.watch.Closed != 2 || m.watch.Last == "" {
		t.Fatalf("%+v", m.watch)
	}
	if !slices.Equal(alerts, []string{"watch-Claude"}) { // once a minute per route
		t.Fatalf("alerts %v", alerts)
	}
	select {
	case <-m.kick:
	default:
		t.Fatal("an unknown node must start a probe")
	}

	// Off, it leaves everything alone.
	fake.closed = nil
	m.cfg.Protect = false // not SetProtect, which saves the config file
	m.watchOnce(context.Background(), "ExitPin", map[string]time.Time{})
	if len(fake.closed) != 0 {
		t.Fatalf("closed %v", fake.closed)
	}
}

func TestNodeOf(t *testing.T) {
	fake := newFakeClash()
	a := conn("a", "api.anthropic.com", "claude", "日本 03")
	a.Metadata.SourcePort = "50001"
	b := conn("b", "api.anthropic.com", "ExitPin", "香港 01")
	b.Metadata.SourcePort = "50002"
	fake.conns = []clash.Conn{a, b}
	srv := httptest.NewServer(fake)
	defer srv.Close()
	cfg := config.Default()
	cfg.Mihomo = config.Mihomo{Controller: srv.URL, Secret: "s3cret"}
	m := New(cfg)
	if c := m.chainOf(context.Background(), "api.anthropic.com", 50002); !slices.Equal(c, []string{"香港 01"}) {
		t.Fatal(c)
	}
	if c := m.chainOf(context.Background(), "api.anthropic.com", 1); c != nil {
		t.Fatal(c)
	}
}
