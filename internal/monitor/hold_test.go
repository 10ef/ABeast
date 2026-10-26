package monitor

import (
	"context"
	"net/http/httptest"
	"slices"
	"testing"

	"github.com/10ef/ABeast/internal/config"
	"github.com/10ef/ABeast/internal/probe"
)

func TestHoldReason(t *testing.T) {
	unsafe := []Route{{Target: config.Target{Name: "Claude"}, Unsafe: true}}
	safe := []Route{{Target: config.Target{Name: "Claude"}}}
	for _, tc := range []struct {
		name string
		s    Snapshot
		want bool
	}{
		{"off never holds", Snapshot{Routes: unsafe, Risks: []Risk{{High: true, Hold: true}}}, false},
		{"client gone, exit wrong", Snapshot{Protect: true, Routes: unsafe}, true},
		{"client gone, exit fine", Snapshot{Protect: true, Routes: safe}, false},
		{"node down is left to the pin", Snapshot{Protect: true, Routes: unsafe, Block: BlockReport{Available: true}}, false},
		{"agents sent around the proxy", Snapshot{Protect: true, Routes: safe, Block: BlockReport{Available: true},
			Risks: []Risk{{High: true, Hold: true, Text: "TUN 未开启"}}}, true},
		{"a high risk that does not leak now", Snapshot{Protect: true, Routes: safe, Block: BlockReport{Available: true},
			Risks: []Risk{{High: true, Text: "可能选到 DIRECT"}}}, false},
	} {
		if got := holdReason(tc.s) != ""; got != tc.want {
			t.Errorf("%s: %v", tc.name, got)
		}
	}
}

func TestChangeAlerts(t *testing.T) {
	m := New(config.Default())
	ids := func(as []alert) []string {
		var out []string
		for _, a := range as {
			out = append(out, a.id)
		}
		return out
	}
	tun := Risk{High: true, Hold: true, Text: "TUN 未开启"}
	s := Snapshot{Protect: true, Block: BlockReport{Available: true}, Risks: []Risk{tun, {Text: "提示"}}}
	s.Hold = holdReason(s)
	if got := ids(m.changeAlerts(s)); !slices.Equal(got, []string{"risk-TUN 未开启", "hold"}) {
		t.Fatal(got)
	}
	if got := m.changeAlerts(s); len(got) != 0 {
		t.Fatalf("nothing changed, yet %v", ids(got))
	}
	// Fixed, then the Clash client goes away.
	s = Snapshot{Protect: true, Routes: []Route{{Target: config.Target{Name: "Claude"}}}}
	if got := ids(m.changeAlerts(s)); !slices.Equal(got, []string{"clash-api", "hold"}) {
		t.Fatal(got) // the client lost, and the agents let go
	}
	if got := m.changeAlerts(s); len(got) != 0 {
		t.Fatalf("told once, yet %v", ids(got))
	}
	// Without protection mode a missing client is nothing to tell.
	m = New(config.Default())
	if got := m.changeAlerts(Snapshot{}); len(got) != 0 {
		t.Fatal(ids(got))
	}
}

func TestSelfCheckHoldsWithoutTUN(t *testing.T) {
	fake := newFakeClash()
	fake.tun = map[string]any{"enable": false}
	srv := httptest.NewServer(fake)
	defer srv.Close()
	cfg := config.Default()
	cfg.Mihomo = config.Mihomo{Controller: srv.URL, Secret: "s3cret"}
	m := New(cfg)
	ctx := context.Background()
	hold := func(via string) bool {
		routes := []Route{{Target: config.Target{Name: "Claude"}, Trace: probe.Trace{Via: via}}}
		return slices.ContainsFunc(m.selfCheck(ctx, routes, true), func(r Risk) bool { return r.Hold })
	}
	if !hold("127.0.0.1:7890") {
		t.Fatal("agents that ignore the system proxy go around it without TUN")
	}
	if hold("env") {
		t.Fatal("with HTTPS_PROXY in the environment the agents reach the proxy")
	}
	fake.mode = "direct"
	if !hold("env") {
		t.Fatal("direct mode sends everything around the proxy")
	}
}
