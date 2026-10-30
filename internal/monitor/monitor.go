package monitor

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/10ef/ABeast/internal/agent"
	"github.com/10ef/ABeast/internal/clash"
	"github.com/10ef/ABeast/internal/config"
	"github.com/10ef/ABeast/internal/probe"
)

// Route is the latest state of one target.
type Route struct {
	Target   config.Target `json:"target"`
	Trace    probe.Trace   `json:"trace"`
	Node     string        `json:"node,omitempty"`  // the proxy's outbound for the probe, e.g. DIRECT
	Chain    []string      `json:"chain,omitempty"` // the node, then the groups up to the rule's policy
	Geo      probe.Geo     `json:"geo"`
	Scores   []probe.Score `json:"scores,omitempty"` // the exit IP's reputation
	Err      string        `json:"error,omitempty"`
	Fails    int           `json:"fails"`
	Clean    int           `json:"clean"` // consecutive probes with no problem
	Problem  string        `json:"problem,omitempty"`
	Unsafe   bool          `json:"unsafe"`
	BaseIP   string        `json:"base_ip,omitempty"`
	BaseCC   string        `json:"base_country,omitempty"`
	Checked  time.Time     `json:"checked"`
	lastIP   string
	lastCC   string
	informed bool // shown only, never guarded or alerted on, like the direct route
	lowScore bool // a score was below Reputation.AlertBelow at the last round
}

// Snapshot is what the tray shows and status.json holds.
type Snapshot struct {
	Routes  []Route      `json:"routes"`
	Direct  *Route       `json:"direct,omitempty"`
	Block   BlockReport  `json:"block"`
	Guard   agent.Report `json:"guard"`
	Hold    string       `json:"hold,omitempty"` // why protection mode holds the agents
	Watch   WatchReport  `json:"watch"`
	Pin     PinReport    `json:"pin"`
	Risks   []Risk       `json:"risks,omitempty"`
	Protect bool         `json:"protect"`
	Checked time.Time    `json:"checked"`
}

func (s Snapshot) Unsafe() bool {
	for _, r := range s.Routes {
		if r.Unsafe {
			return true
		}
	}
	return false
}

type alert struct{ id, title, body string }

// Monitor probes every target in rounds and decides which routes are unsafe.
type Monitor struct {
	mu      sync.Mutex
	cfg     *config.Config
	routes  []*Route
	direct  *Route
	geo     *probe.GeoCache
	scores  *probe.ScoreCache
	blocker *Blocker
	pinner  *Pinner
	blocked map[string]bool // routes whose group rejects, by name
	// simulate makes routes unsafe on purpose, to test alerts and blocking.
	simulate map[string]bool
	// nodes is the exit country probes saw through each outbound.
	nodes   map[string]string
	watch   WatchReport
	alerted map[string]time.Time // last watcher alert of each route
	// pinnedNode is the node each route is pinned on.
	pinnedNode map[string]string
	// guard pauses the agents while protection mode holds them; see
	// SetGuard.
	guard *agent.Guard
	// What the last round told the user about, for changeAlerts.
	riskSeen map[string]bool
	apiOK    bool
	held     string
	kick     chan struct{}
	asleep   bool
	graceTo  time.Time

	OnSnapshot func(Snapshot)
	OnAlert    func(id, title, body string)
}

func New(cfg *config.Config) *Monitor {
	m := &Monitor{cfg: cfg, geo: probe.NewGeoCache(), scores: probe.NewScoreCache(), kick: make(chan struct{}, 1),
		blocked: map[string]bool{}, simulate: map[string]bool{}, nodes: map[string]string{}, alerted: map[string]time.Time{},
		pinnedNode: map[string]string{}, riskSeen: map[string]bool{}, apiOK: true}
	for _, t := range cfg.Targets {
		if t.BlockGroup != "" {
			m.blocker = newBlocker(clash.New(cfg.Mihomo), blockedPath())
			m.pinner = newPinner(m.blocker.api, pinnedPath())
			break
		}
	}
	for _, t := range cfg.Targets {
		m.routes = append(m.routes, &Route{Target: t})
	}
	if cfg.ShowDirect {
		m.direct = &Route{Target: config.Target{Name: "直连", Host: "www.cloudflare.com", Proxy: "direct"}, informed: true}
	}
	return m
}

func (m *Monitor) Run(ctx context.Context) {
	if m.blocker != nil {
		go m.watchConns(ctx)
	}
	ticker := time.NewTicker(m.cfg.Interval())
	defer ticker.Stop()
	for {
		m.round(ctx)
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		case <-m.kick:
		}
	}
}

// Kick starts a round now.
func (m *Monitor) Kick() {
	select {
	case m.kick <- struct{}{}:
	default:
	}
}

// Sleep stops probing while the computer sleeps; Wake resumes it, without
// counting the failures of the first seconds while the network comes back.
func (m *Monitor) Sleep() {
	m.mu.Lock()
	m.asleep = true
	m.mu.Unlock()
}

func (m *Monitor) Wake() {
	m.mu.Lock()
	m.asleep = false
	m.graceTo = time.Now().Add(20 * time.Second)
	m.mu.Unlock()
	time.AfterFunc(3*time.Second, m.Kick)
}

// AcceptCurrent makes each route's current exit its expected one.
func (m *Monitor) AcceptCurrent() {
	m.mu.Lock()
	for _, r := range m.routes {
		if r.Err == "" && r.Trace.IP != "" {
			r.BaseIP, r.BaseCC = r.Trace.IP, r.Trace.Country
		}
	}
	m.mu.Unlock()
	m.Kick()
}

// SetProtect turns protection mode on or off: pinning, blocking unsafe
// routes where the override script set up their groups, and cutting AI
// connections through a wrong exit.
func (m *Monitor) SetProtect(on bool) {
	m.mu.Lock()
	m.cfg.Protect = on
	err := config.Save(m.cfg)
	m.mu.Unlock()
	if err != nil {
		log.Println("config:", err)
	}
	m.Kick()
}

// Repin releases the pinned nodes, so the groups pick a node again, and
// pins what they pick at the next round.
func (m *Monitor) Repin(ctx context.Context) {
	if m.pinner == nil {
		return
	}
	m.pinner.RestoreAll(ctx)
	m.mu.Lock()
	m.pinnedNode = map[string]string{}
	m.mu.Unlock()
	time.AfterFunc(time.Second, m.Kick)
}

// SetTrayTitle saves the menu bar text template.
func (m *Monitor) SetTrayTitle(tpl string) {
	m.mu.Lock()
	m.cfg.TrayTitle = tpl
	err := config.Save(m.cfg)
	m.mu.Unlock()
	if err != nil {
		log.Println("config:", err)
	}
}

// Simulate makes a route unsafe, or stops doing so.
func (m *Monitor) Simulate(name string, on bool) {
	m.mu.Lock()
	m.simulate[name] = on
	m.mu.Unlock()
	m.Kick()
}

// SetGuard lets protection mode pause the agents through g. Without one it
// only reports when it would, as in tests.
func (m *Monitor) SetGuard(g *agent.Guard) { m.guard = g }

// HasController reports whether the targets name groups of a Clash API
// controller, without which nothing is pinned or blocked.
func (m *Monitor) HasController() bool { return m.blocker != nil }

// Record takes one probe result for r as a round does, for tools and tests
// that probe on their own.
func (m *Monitor) Record(r *Route, t probe.Trace, err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.evaluate(r, t, probe.Geo{}, err, false)
}

// ResumeAgents lets held agents go now, turning protection mode off so they
// are not held again at the next round.
func (m *Monitor) ResumeAgents() {
	m.SetProtect(false)
	if m.guard != nil {
		m.guard.ResumeAll()
	}
}

// Restore resumes held agents and puts back what the blocker and the pinner
// replaced, before quitting.
func (m *Monitor) Restore(ctx context.Context) {
	if m.guard != nil {
		m.guard.Close()
	}
	if m.blocker != nil {
		m.blocker.RestoreAll(ctx)
	}
	if m.pinner != nil {
		m.pinner.RestoreAll(ctx)
	}
}

func (m *Monitor) round(ctx context.Context) {
	m.mu.Lock()
	asleep := m.asleep
	m.mu.Unlock()
	if asleep {
		return
	}

	all := slices.Clone(m.routes)
	if m.direct != nil {
		all = append(all, m.direct)
	}
	type result struct {
		trace  probe.Trace
		geo    probe.Geo
		scores []probe.Score
		chain  []string
		err    error
	}
	results := make([]result, len(all))
	proxies := make([]string, len(all))
	m.mu.Lock()
	for i, r := range all {
		proxies[i] = r.Target.Proxy
		// A rejecting group refuses the probe too: probe the upstream the
		// group goes back to, to know when it can.
		if m.blocked[r.Target.Name] && r.Target.ProbeWhileBlocked != "" {
			proxies[i] = r.Target.ProbeWhileBlocked
		}
	}
	m.mu.Unlock()
	var wg sync.WaitGroup
	for i, r := range all {
		wg.Add(1)
		go func() {
			defer wg.Done()
			var chain []string
			var opened func(int)
			if m.blocker != nil {
				opened = func(port int) { chain = m.chainOf(ctx, r.Target.Host, port) }
			}
			t, err := probe.Exit(ctx, r.Target.Host, proxies[i], m.cfg.Timeout(), opened)
			res := result{trace: t, err: err, chain: chain}
			if err == nil {
				res.geo, _ = m.geo.Lookup(ctx, t.IP, m.cfg.Timeout())
				if !r.informed {
					res.scores = m.lookupScores(ctx, r.Target, t.IP)
				}
			}
			results[i] = res
		}()
	}
	wg.Wait()

	m.mu.Lock()
	inGrace := time.Now().Before(m.graceTo)
	var alerts []alert
	for i, r := range all {
		alerts = append(alerts, m.evaluate(r, results[i].trace, results[i].geo, results[i].err, inGrace)...)
		if !r.informed {
			alerts = append(alerts, m.score(r, results[i].scores)...)
		}
		r.Node, r.Chain = "", nil
		if res := results[i]; res.err == nil && len(res.chain) > 0 {
			r.Node, r.Chain = res.chain[0], res.chain
			m.nodes[r.Node] = res.trace.Country
		}
	}
	snap := Snapshot{Protect: m.cfg.Protect, Watch: m.watch, Checked: time.Now()}
	for _, r := range m.routes {
		snap.Routes = append(snap.Routes, *r)
	}
	if m.direct != nil {
		d := *m.direct
		snap.Direct = &d
	}
	m.mu.Unlock()

	if m.blocker != nil {
		// Without the override script's groups the blocker has nothing to
		// switch: it only reports them missing, and pinning protects alone.
		snap.Block = m.blocker.Apply(ctx, snap.Routes, snap.Protect)
		m.mu.Lock()
		for _, r := range m.routes {
			m.blocked[r.Target.Name] = snap.Block.Routes[r.Target.Name].Blocked
		}
		m.mu.Unlock()
	}
	if m.pinner != nil {
		snap.Pin = m.pinner.Apply(ctx, snap.Routes, snap.Protect)
		m.mu.Lock()
		m.pinnedNode = map[string]string{}
		for name, st := range snap.Pin.Routes {
			if st.Node != "" {
				m.pinnedNode[name] = st.Node
			}
		}
		m.mu.Unlock()
	}
	if m.blocker != nil {
		snap.Risks = m.selfCheck(ctx, snap.Routes, snap.Protect)
	}
	m.hold(&snap)
	m.mu.Lock()
	alerts = append(alerts, m.changeAlerts(snap)...)
	m.mu.Unlock()
	for i := range alerts {
		if !strings.HasPrefix(alerts[i].id, "unsafe-") {
			continue
		}
		name := strings.TrimPrefix(alerts[i].id, "unsafe-")
		if st := snap.Block.Routes[name]; st.Blocked {
			alerts[i].body += "\n已阻断：" + st.Group + " → REJECT"
		}
	}
	writeStatus(snap)
	if m.OnSnapshot != nil {
		m.OnSnapshot(snap)
	}
	if m.OnAlert != nil {
		for _, a := range alerts {
			m.OnAlert(a.id, a.title, a.body)
		}
	}
}

// evaluate updates a route with one probe's result and returns its alerts.
// Called with m.mu held.
func (m *Monitor) evaluate(r *Route, t probe.Trace, g probe.Geo, err error, inGrace bool) []alert {
	cfg := m.cfg
	r.Checked = time.Now()
	wasUnsafe := r.Unsafe
	var alerts []alert
	var problems []string

	if err != nil {
		r.Err = err.Error()
		r.Trace = probe.Trace{Via: t.Via}
		r.Geo = probe.Geo{}
		if !inGrace {
			r.Fails++
		}
		if r.Fails >= cfg.FailThreshold {
			problems = append(problems, fmt.Sprintf("连续 %d 次无法连接 %s", r.Fails, r.Target.Host))
		} else if r.Unsafe && r.Problem != "" {
			problems = append(problems, r.Problem) // keep the last reason while unreachable
		}
	} else {
		r.Err, r.Fails = "", 0
		r.Trace, r.Geo = t, g
		cc := t.Country
		if slices.Contains(cfg.BlockedCountries, cc) {
			problems = append(problems, fmt.Sprintf("出口在 %s%s（已屏蔽）", probe.FlagBefore(cc), cc))
		} else if len(cfg.AllowedCountries) > 0 && !slices.Contains(cfg.AllowedCountries, cc) {
			problems = append(problems, fmt.Sprintf("出口在 %s%s（不在允许列表）", probe.FlagBefore(cc), cc))
		}
		if r.BaseCC == "" && len(problems) == 0 {
			r.BaseIP, r.BaseCC = t.IP, cc
		}
		if r.BaseCC != "" && cc != r.BaseCC && cfg.GuardOnCountryChange {
			problems = append(problems, fmt.Sprintf("出口国家 %s→%s", r.BaseCC, cc))
		}
		if r.BaseIP != "" && t.IP != r.BaseIP && cfg.GuardOnIPChange {
			problems = append(problems, fmt.Sprintf("出口 IP %s→%s", r.BaseIP, t.IP))
		}
		switch {
		case r.informed:
		case r.lastCC != "" && cc != r.lastCC:
			alerts = append(alerts, alert{"change-" + r.Target.Name, r.Target.Name + " 出口国家变化",
				fmt.Sprintf("%s%s → %s%s  %s", probe.FlagBefore(r.lastCC), r.lastCC, probe.FlagBefore(cc), cc, t.IP)})
		case r.lastIP != "" && t.IP != r.lastIP:
			alerts = append(alerts, alert{"change-" + r.Target.Name, r.Target.Name + " 出口 IP 变化",
				fmt.Sprintf("%s → %s  %s", r.lastIP, t.IP, g.ISP)})
		}
		r.lastIP, r.lastCC = t.IP, cc
	}

	if m.simulate[r.Target.Name] {
		problems = append(problems, "模拟异常（测试）")
	}
	r.Problem = strings.Join(problems, "；")
	if r.informed {
		r.Unsafe = false
		return nil
	}
	if r.Problem != "" {
		r.Clean = 0
		r.Unsafe = true
	} else {
		r.Clean++
		// While the agents are held, one good probe is enough: every
		// minute frozen risks their prompt cache, and a relapse holds
		// them again at the next probe.
		need := cfg.RecoverThreshold
		if m.held != "" {
			need = 1
		}
		if r.Unsafe && r.Clean >= need {
			r.Unsafe = false
		}
	}
	switch {
	case r.Unsafe && !wasUnsafe:
		alerts = append(alerts, alert{"unsafe-" + r.Target.Name, "⚠️ " + r.Target.Name + " 出口异常", r.Problem})
	case !r.Unsafe && wasUnsafe:
		alerts = append(alerts, alert{"unsafe-" + r.Target.Name, "✅ " + r.Target.Name + " 出口已恢复",
			fmt.Sprintf("%s%s %s", probe.FlagBefore(r.Trace.Country), r.Trace.Country, r.Trace.IP)})
	}
	return alerts
}

// writeStatus saves the snapshot for other tools, such as an agent hook that
// refuses to start while a route is unsafe.
func writeStatus(s Snapshot) {
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return
	}
	path := filepath.Join(config.Dir(), "status.json")
	tmp := path + ".tmp"
	if os.WriteFile(tmp, data, 0o644) == nil {
		_ = os.Rename(tmp, path)
	}
}

// lookupScores asks every reputation source that scores the target about
// its exit IP, at once.
func (m *Monitor) lookupScores(ctx context.Context, t config.Target, ip string) []probe.Score {
	var srcs []config.ScoreSource
	for _, src := range m.cfg.Reputation.Sources {
		if src.URL != "" && (len(src.Targets) == 0 || slices.Contains(src.Targets, t.Name)) {
			srcs = append(srcs, src)
		}
	}
	scores := make([]probe.Score, len(srcs))
	var wg sync.WaitGroup
	for i, src := range srcs {
		wg.Add(1)
		go func() {
			defer wg.Done()
			scores[i] = m.scores.Lookup(ctx, src, ip, t.Proxy, m.cfg.Timeout())
		}()
	}
	wg.Wait()
	return scores
}

// score records a route's reputation and alerts when it turns low: a score
// below Reputation.AlertBelow, or a region the vendor does not serve. It
// only informs; protection mode does not act on it. Called with m.mu held.
func (m *Monitor) score(r *Route, scores []probe.Score) []alert {
	if r.Err != "" {
		return nil // keep the last scores while the route is down
	}
	r.Scores = scores
	below := m.cfg.Reputation.AlertBelow
	if below <= 0 {
		r.lowScore = false
		return nil
	}
	var low []string
	for _, s := range scores {
		switch {
		case s.Err != "":
		case s.Unsupported:
			low = append(low, fmt.Sprintf("%s 地区不支持（%s）", s.Source, s.Region))
		case s.Score < below:
			low = append(low, fmt.Sprintf("%s 信任评分 %d %s", s.Source, s.Score, s.Label))
		}
	}
	was := r.lowScore
	r.lowScore = len(low) > 0
	switch {
	case r.lowScore && !was:
		return []alert{{"score-" + r.Target.Name, "⚠️ " + r.Target.Name + " 出口 IP 信誉低",
			r.Trace.IP + "\n" + strings.Join(low, "；")}}
	case !r.lowScore && was:
		return []alert{{"score-" + r.Target.Name, "✅ " + r.Target.Name + " 出口 IP 信誉恢复", r.Trace.IP}}
	}
	return nil
}
