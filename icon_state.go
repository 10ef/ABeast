package main

import (
	"github.com/10ef/ABeast/internal/monitor"
)

// trayState is presentation only. It never changes the monitor's safety
// decisions, recovery thresholds, or protection switches.
type trayState uint8

const (
	trayChecking trayState = iota
	trayMonitoring
	trayProtected
	trayWarning
	trayPaused
	trayRecovering
)

func heldTraffic(s monitor.Snapshot) bool {
	for _, n := range s.Guard.Paused {
		if n > 0 {
			return true
		}
	}
	for _, r := range s.Block.Routes {
		if r.Blocked {
			return true
		}
	}
	return false
}

func stateForTray(s monitor.Snapshot) trayState {
	held := heldTraffic(s)
	// Do not show a healthy dog before the first completed probe.
	if s.Checked.IsZero() && !held {
		return trayChecking
	}
	// Failed protection must not look like successful protection, even if
	// another route was successfully paused or blocked.
	if len(s.Guard.Errors) > 0 || (s.Protect && (s.Watch.Err != "" || !s.Block.Available || s.Block.Err != "")) {
		return trayWarning
	}
	for _, risk := range s.Risks {
		if risk.High {
			return trayWarning
		}
	}
	unsafe, recovering, failed, pending := false, true, false, false
	for _, r := range s.Routes {
		failed = failed || r.Err != ""
		pending = pending || r.Checked.IsZero() || r.Trace.IP == ""
		if r.Unsafe {
			unsafe = true
			recovering = recovering && r.Clean > 0 && r.Err == "" && r.Problem == ""
		}
		if s.Protect {
			p := s.Pin.Routes[r.Target.Name]
			if p.Err != "" {
				return trayWarning
			}
			// Unsafe routes are deliberately not pinned. A healthy route
			// with no pin cannot substantiate a protection checkmark.
			if !r.Unsafe && r.Err == "" && !r.Checked.IsZero() && r.Trace.IP != "" && (p.Node == "" || p.Group == "") {
				return trayWarning
			}
		}
	}
	if unsafe && recovering && !failed && !pending {
		return trayRecovering
	}
	if held {
		return trayPaused
	}
	if unsafe || failed || len(s.Routes) == 0 {
		return trayWarning
	}
	if pending {
		return trayChecking
	}
	if s.Protect {
		return trayProtected
	}
	return trayMonitoring
}

func (state trayState) label(s monitor.Snapshot) string {
	switch state {
	case trayChecking:
		return "正在检测出口…"
	case trayMonitoring:
		return "出口正常 · 仅监控"
	case trayProtected:
		return "出口正常 · 保护已开启"
	case trayWarning:
		if s.Unsafe() {
			return "出口异常 · 请查看详情"
		}
		return "需要注意 · 请查看检测或保护详情"
	case trayPaused:
		return "已暂停进程或阻断流量 · 请查看详情"
	case trayRecovering:
		if heldTraffic(s) {
			return "恢复确认中 · 暂停或阻断尚未解除"
		}
		return "恢复确认中 · 等待连续正常检测"
	default:
		return "正在检测出口…"
	}
}
