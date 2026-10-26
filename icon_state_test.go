package main

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/10ef/ABeast/internal/config"
	"github.com/10ef/ABeast/internal/monitor"
	"github.com/10ef/ABeast/internal/probe"
)

// Exercise the actual failure and recovery thresholds, including the
// interval where a good probe must not yet display a healthy icon.
func TestTrayFailureAndRecoveryLifecycle(t *testing.T) {
	cfg := config.Default()
	m := monitor.New(cfg)
	r := &monitor.Route{Target: config.Target{Name: "Claude"}}
	s := monitor.Snapshot{Block: monitor.BlockReport{Available: true}}
	check := func(want trayState) {
		t.Helper()
		if got := stateForTray(s); got != want {
			t.Fatalf("state=%v want=%v, snapshot=%+v", got, want, s)
		}
	}
	probe := func(err error) {
		m.Record(r, probe.Trace{IP: "1.1.1.1", Country: "JP"}, err)
		s.Checked = time.Now()
		s.Routes = []monitor.Route{*r}
	}
	check(trayChecking)
	probe(nil)
	check(trayMonitoring)
	s.Protect = true
	s.Pin.Routes = map[string]monitor.PinState{"Claude": {Node: "JP", Group: "select"}}
	check(trayProtected)
	probe(errors.New("timeout"))
	check(trayWarning) // not yet unsafe, but cannot claim a healthy exit
	for i := 1; i < cfg.FailThreshold; i++ {
		probe(errors.New("timeout"))
	}
	s.Block.Routes = map[string]monitor.BlockState{"Claude": {Blocked: true}}
	check(trayPaused)
	probe(nil)
	check(trayRecovering)
	if !strings.Contains(trayRecovering.label(s), "尚未解除") {
		t.Fatal("recovery must disclose that traffic is still held")
	}
	probe(nil)
	s.Block.Routes = nil
	check(trayProtected)
}

func TestTrayDoesNotOverstateProtection(t *testing.T) {
	ready := func() monitor.Snapshot {
		return monitor.Snapshot{Checked: time.Now(), Routes: []monitor.Route{{
			Target: config.Target{Name: "Claude", BlockGroup: "guard"},
			Trace:  probe.Trace{IP: "1.1.1.1", Country: "JP"}, Checked: time.Now(),
		}}}
	}
	for _, tc := range []struct {
		name string
		edit func(*monitor.Snapshot)
		want trayState
	}{
		{"switch on without a pin", func(s *monitor.Snapshot) { s.Protect = true }, trayWarning},
		{"pin verified", func(s *monitor.Snapshot) {
			s.Protect = true
			s.Block.Available = true
			s.Pin.Routes = map[string]monitor.PinState{"Claude": {Node: "JP", Group: "select"}}
		}, trayProtected},
		{"watcher failure despite pin", func(s *monitor.Snapshot) {
			s.Protect = true
			s.Pin.Routes = map[string]monitor.PinState{"Claude": {Node: "JP", Group: "select"}}
			s.Watch.Err = "controller unavailable"
		}, trayWarning},
		{"pinned but the Clash API is gone", func(s *monitor.Snapshot) {
			s.Protect = true
			s.Pin.Routes = map[string]monitor.PinState{"Claude": {Node: "JP", Group: "select"}}
		}, trayWarning},
		{"disabled blocker unavailable is irrelevant", func(s *monitor.Snapshot) {
			s.Block.Err = "controller unavailable"
		}, trayMonitoring},
		{"historical disconnect count is not a current block", func(s *monitor.Snapshot) {
			s.Watch.Closed = 12
			s.Block.Closed = 5
		}, trayMonitoring},
		{"still blocked after disabling switch", func(s *monitor.Snapshot) {
			s.Block.Routes = map[string]monitor.BlockState{"Claude": {Blocked: true}}
		}, trayPaused},
		{"agents paused", func(s *monitor.Snapshot) {
			s.Guard.Paused = map[string]int{"claude": 1}
		}, trayPaused},
		{"partial protection failure overrides pause", func(s *monitor.Snapshot) {
			s.Guard.Paused = map[string]int{"claude": 1}
			s.Guard.Errors = []string{"codex: permission denied"}
		}, trayWarning},
		{"high leak risk overrides healthy route", func(s *monitor.Snapshot) {
			s.Risks = []monitor.Risk{{High: true, Text: "TUN off"}}
		}, trayWarning},
		{"hint does not override healthy route", func(s *monitor.Snapshot) {
			s.Risks = []monitor.Risk{{Text: "Consider protection"}}
		}, trayMonitoring},
		{"direct display route does not change agent status", func(s *monitor.Snapshot) {
			s.Direct = &monitor.Route{Err: "timeout"}
		}, trayMonitoring},
		{"one recovering route does not hide another failure", func(s *monitor.Snapshot) {
			s.Routes[0].Unsafe, s.Routes[0].Clean = true, 1
			s.Routes = append(s.Routes, monitor.Route{Unsafe: true, Err: "timeout"})
		}, trayWarning},
		{"empty targets is not healthy", func(s *monitor.Snapshot) { s.Routes = nil }, trayWarning},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := ready()
			tc.edit(&s)
			if got := stateForTray(s); got != tc.want {
				t.Fatalf("state=%v want=%v", got, tc.want)
			}
		})
	}
}
