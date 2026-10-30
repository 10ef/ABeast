package monitor

import (
	"errors"
	"testing"

	"github.com/10ef/ABeast/internal/config"
	"github.com/10ef/ABeast/internal/probe"
)

func testMonitor() (*Monitor, *Route) {
	cfg := config.Default()
	cfg.ShowDirect = false
	m := New(cfg)
	return m, m.routes[0]
}

func TestBlockedCountryIsUnsafeAtOnce(t *testing.T) {
	m, r := testMonitor()
	alerts := m.evaluate(r, probe.Trace{IP: "1.2.3.4", Country: "CN"}, probe.Geo{}, nil, false)
	if !r.Unsafe || len(alerts) != 1 {
		t.Fatalf("unsafe=%v alerts=%v", r.Unsafe, alerts)
	}
	if r.BaseCC != "" {
		t.Fatalf("a blocked exit must not become the baseline, got %q", r.BaseCC)
	}
}

func TestCountryChangeNeedsAccepting(t *testing.T) {
	m, r := testMonitor()
	m.evaluate(r, probe.Trace{IP: "1.1.1.1", Country: "JP"}, probe.Geo{}, nil, false)
	m.evaluate(r, probe.Trace{IP: "2.2.2.2", Country: "US"}, probe.Geo{}, nil, false)
	if !r.Unsafe {
		t.Fatal("country change should make the route unsafe")
	}
	for range 5 {
		m.evaluate(r, probe.Trace{IP: "2.2.2.2", Country: "US"}, probe.Geo{}, nil, false)
	}
	if !r.Unsafe {
		t.Fatal("the new country stays unsafe until accepted")
	}
	m.AcceptCurrent()
	for range m.cfg.RecoverThreshold {
		m.evaluate(r, probe.Trace{IP: "2.2.2.2", Country: "US"}, probe.Geo{}, nil, false)
	}
	if r.Unsafe {
		t.Fatalf("accepted exit should recover, problem=%q", r.Problem)
	}
}

func TestFailuresNeedThresholdAndRecover(t *testing.T) {
	m, r := testMonitor()
	ok := probe.Trace{IP: "1.1.1.1", Country: "JP"}
	m.evaluate(r, ok, probe.Geo{}, nil, false)
	down := errors.New("超时")
	for i := 1; i < m.cfg.FailThreshold; i++ {
		m.evaluate(r, probe.Trace{}, probe.Geo{}, down, false)
		if r.Unsafe {
			t.Fatalf("unsafe after %d failures", i)
		}
	}
	m.evaluate(r, probe.Trace{}, probe.Geo{}, down, false)
	if !r.Unsafe {
		t.Fatal("should be unsafe at the threshold")
	}
	m.evaluate(r, probe.Trace{}, probe.Geo{}, down, true) // grace after wake: not counted
	if r.Fails != m.cfg.FailThreshold {
		t.Fatalf("fails=%d", r.Fails)
	}
	m.evaluate(r, ok, probe.Geo{}, nil, false)
	if !r.Unsafe {
		t.Fatal("one clean probe is not enough to recover")
	}
	m.evaluate(r, ok, probe.Geo{}, nil, false)
	if r.Unsafe {
		t.Fatal("should recover after RecoverThreshold clean probes")
	}
}

func TestHeldRouteRecoversAfterOneProbe(t *testing.T) {
	for _, held := range []bool{false, true} {
		m, r := testMonitor()
		m.evaluate(r, probe.Trace{IP: "1.2.3.4", Country: "CN"}, probe.Geo{}, nil, false)
		if held {
			m.held = "连不上 Clash 客户端，且出口异常"
		}
		m.evaluate(r, probe.Trace{IP: "5.6.7.8", Country: "JP"}, probe.Geo{}, nil, false)
		if r.Unsafe == held {
			t.Fatalf("held=%v: unsafe=%v after one good probe", held, r.Unsafe)
		}
	}
}

func TestLowScoreAlertsOnce(t *testing.T) {
	m, r := testMonitor()
	m.evaluate(r, probe.Trace{IP: "1.1.1.1", Country: "JP"}, probe.Geo{}, nil, false)
	good := []probe.Score{{Source: "Claude", Score: 80}}
	low := []probe.Score{{Source: "Claude", Score: 20, Label: "高危"}}
	if a := m.score(r, good); len(a) != 0 {
		t.Fatalf("a good score does not alert: %v", a)
	}
	if a := m.score(r, low); len(a) != 1 || r.Unsafe {
		t.Fatalf("a low score alerts, and only informs: %v unsafe=%v", a, r.Unsafe)
	}
	if a := m.score(r, low); len(a) != 0 {
		t.Fatalf("a low score alerts once: %v", a)
	}
	if a := m.score(r, []probe.Score{{Source: "Claude", Score: 90, Region: "不支持", Unsupported: true}}); len(a) != 0 {
		t.Fatalf("still low while the region is unsupported: %v", a)
	}
	if a := m.score(r, good); len(a) != 1 {
		t.Fatalf("recovery alerts: %v", a)
	}
}
