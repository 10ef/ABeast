package monitor

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/10ef/ABeast/internal/clash"
	"github.com/10ef/ABeast/internal/config"
)

// WatchReport is what the connection watcher has done.
type WatchReport struct {
	Closed int    `json:"closed"`
	Last   string `json:"last,omitempty"` // "Claude · 走 DIRECT · 20:51:03"
	Err    string `json:"error,omitempty"`
}

// watchRoute is what the watcher needs of a route.
type watchRoute struct {
	name      string
	domains   []string
	simulated bool
}

// watchState is a copy of the monitor's state for one pass of the watcher.
type watchState struct {
	cfg    *config.Config
	routes []watchRoute
	nodes  map[string]string // outbound → exit country seen by a probe
	self   string            // this program's executable, whose probes are left alone
}

// check says whether an AI connection must be closed and why, or which
// outbound no probe has gone through yet, to probe it.
func (s *watchState) check(c clash.Conn) (route, why, unknown string) {
	if len(c.Chains) == 0 || (s.self != "" && strings.EqualFold(c.Metadata.Process, s.self)) {
		return "", "", ""
	}
	host := strings.ToLower(strings.TrimSuffix(c.Metadata.Host, "."))
	var r *watchRoute
	for i := range s.routes {
		if matchDomain(host, s.routes[i].domains) {
			r = &s.routes[i]
			break
		}
	}
	if r == nil {
		return "", "", ""
	}
	out := c.Chains[0]
	if out == "REJECT" || out == "REJECT-DROP" {
		return "", "", ""
	}
	if r.simulated {
		return r.name, "模拟异常（测试）", ""
	}
	cc, seen := s.nodes[out]
	switch {
	case out == "DIRECT" && !seen:
		// With TUN on, the probe without a proxy is routed by the rules too,
		// so where DIRECT leaves is unknown until a probe goes through it.
		return r.name, "走 DIRECT", ""
	case !seen:
		return "", "", out
	case !s.cfg.Acceptable(cc):
		return r.name, fmt.Sprintf("经 %s 出口在 %s", out, cc), ""
	}
	return "", "", ""
}

func matchDomain(host string, domains []string) bool {
	for _, d := range domains {
		if host == d || strings.HasSuffix(host, "."+d) {
			return true
		}
	}
	return false
}

// watchConns checks the proxy's connections twice a second, so an AI
// connection through an unsafe exit is closed as it opens, not at the next
// probe round.
func (m *Monitor) watchConns(ctx context.Context) {
	self := ""
	if exe, err := os.Executable(); err == nil {
		self = filepath.Base(exe)
	}
	asked := map[string]time.Time{}
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
		m.watchOnce(ctx, self, asked)
	}
}

func (m *Monitor) watchOnce(ctx context.Context, self string, asked map[string]time.Time) {
	m.mu.Lock()
	if !m.cfg.Protect || m.asleep {
		m.mu.Unlock()
		return
	}
	s := watchState{cfg: m.cfg, self: self,
		nodes: make(map[string]string, len(m.nodes))}
	for k, v := range m.nodes {
		s.nodes[k] = v
	}
	for _, r := range m.routes {
		s.routes = append(s.routes, watchRoute{name: r.Target.Name, domains: r.Target.DomainSuffixes(), simulated: m.simulate[r.Target.Name]})
	}
	m.mu.Unlock()

	conns, err := m.blocker.api.Connections(ctx)
	m.mu.Lock()
	lost := err != nil && m.watch.Err == ""
	m.watch.Err = ""
	if err != nil {
		m.watch.Err = err.Error()
	}
	m.mu.Unlock()
	if err != nil {
		if lost {
			// The Clash client just went away: check the exits now rather
			// than at the next round, to hold the agents at once if needed.
			m.Kick()
		}
		return
	}
	for _, c := range conns {
		route, why, unknown := s.check(c)
		if unknown != "" {
			// A node no probe has seen: probe now to learn where it exits.
			if time.Since(asked[unknown]) > 15*time.Second {
				asked[unknown] = time.Now()
				m.Kick()
			}
			continue
		}
		if why == "" {
			continue
		}
		if err := m.blocker.api.CloseConn(ctx, c.ID); err != nil {
			log.Printf("watch: close %s: %v", c.Metadata.Host, err)
			continue
		}
		m.closed(route, why, c)
	}
}

// closed records a closed connection and alerts, once a minute per route.
func (m *Monitor) closed(route, why string, c clash.Conn) {
	log.Printf("watch: closed %s %s (%s) %s", route, c.Metadata.Host, c.Metadata.Process, why)
	now := time.Now()
	m.mu.Lock()
	m.watch.Closed++
	m.watch.Last = route + " · " + why + " · " + now.Format("15:04:05")
	alert := now.Sub(m.alerted[route]) > time.Minute
	if alert {
		m.alerted[route] = now
	}
	m.mu.Unlock()
	if alert && m.OnAlert != nil {
		body := c.Metadata.Host
		if c.Metadata.Process != "" {
			body += " · " + c.Metadata.Process
		}
		m.OnAlert("watch-"+route, "⛔ 已断开 "+route+" 连接："+why, body)
	}
	m.Kick() // probe again, and show the count
}

// chainOf finds an open probe among the proxy's connections by its local
// port, and returns its chain: the outbound the rules picked, then the
// groups up to the rule's policy.
func (m *Monitor) chainOf(ctx context.Context, host string, port int) []string {
	conns, err := m.blocker.api.Connections(ctx)
	if err != nil {
		return nil
	}
	p := strconv.Itoa(port)
	for _, c := range conns {
		if c.Metadata.SourcePort == p && strings.EqualFold(c.Metadata.Host, host) && len(c.Chains) > 0 {
			return c.Chains
		}
	}
	return nil
}
