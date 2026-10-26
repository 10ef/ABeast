package monitor

import (
	"fmt"
)

// holdReason is why protection mode holds the agents now, or "" to let them
// run. It holds them only where pinning cannot keep their traffic in: the
// Clash client is gone while an exit is wrong, or the setup sends agents
// around the proxy. A node going down is left to the pin, which fails the
// requests without freezing anything.
func holdReason(s Snapshot) string {
	if !s.Protect {
		return ""
	}
	if !s.Block.Available && s.Unsafe() {
		return "连不上 Clash 客户端，且出口异常"
	}
	for _, r := range s.Risks {
		if r.Hold {
			return r.Text
		}
	}
	return ""
}

// hold pauses the agents while holdReason says so, and resumes them when it
// no longer does. Without a guard, as in tests, it only reports the reason.
func (m *Monitor) hold(s *Snapshot) {
	s.Hold = holdReason(*s)
	if m.guard == nil {
		return
	}
	names := m.cfg.Agents()
	pause := map[string]bool{}
	for _, n := range names {
		pause[n] = s.Hold != ""
	}
	s.Guard = m.guard.Apply(names, pause, s.Hold != "")
}

// changeAlerts tells about what changed since the last round that the user
// must know now: a high risk appearing, the Clash client lost while
// protection mode needs it, and agents held or let go. Called with m.mu
// held.
func (m *Monitor) changeAlerts(s Snapshot) []alert {
	var alerts []alert
	seen := map[string]bool{}
	for _, r := range s.Risks {
		if !r.High {
			continue
		}
		seen[r.Text] = true
		if !m.riskSeen[r.Text] {
			alerts = append(alerts, alert{"risk-" + r.Text, "⚠️ 存在泄漏风险", r.Text})
		}
	}
	m.riskSeen = seen

	api := !s.Protect || s.Block.Available
	if !api && m.apiOK {
		alerts = append(alerts, alert{"clash-api", "⚠️ 连不上 Clash 客户端",
			"保护模式无法锁定节点；出口一旦异常，会暂停 Agent 进程"})
	}
	m.apiOK = api

	paused := 0
	for _, n := range s.Guard.Paused {
		paused += n
	}
	switch {
	case s.Hold != "" && m.held == "":
		body := s.Hold
		if paused > 0 {
			body += fmt.Sprintf("\n已暂停 %d 个 Agent 进程，恢复后自动继续", paused)
		}
		alerts = append(alerts, alert{"hold", "⏸ 保护模式已拦住 Agent", body})
	case s.Hold == "" && m.held != "":
		alerts = append(alerts, alert{"hold", "▶️ Agent 已恢复", "已恢复安全，暂停的 Agent 进程已继续"})
	}
	m.held = s.Hold
	return alerts
}
