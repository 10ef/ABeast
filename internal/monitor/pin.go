package monitor

import (
	"context"
	"encoding/json"
	"log"
	"os"
	"path/filepath"
	"slices"
	"sync"

	"github.com/10ef/ABeast/internal/clash"
	"github.com/10ef/ABeast/internal/config"
)

// PinState is a route's pinned node.
type PinState struct {
	Group string `json:"group,omitempty"` // the group switched to the node
	Node  string `json:"node,omitempty"`
	Fixed bool   `json:"fixed,omitempty"` // a url-test or fallback group, which may still move off a dead node
	Err   string `json:"error,omitempty"`
}

type PinReport struct {
	Routes map[string]PinState `json:"routes,omitempty"` // by target name
}

// pin is a group the pinner switched, with what to put back.
type pin struct {
	Node string `json:"node"`
	Prev string `json:"prev,omitempty"` // a selector's selection before
	Type string `json:"type"`
}

// Pinner holds each AI route on the node it leaves through, by selecting that
// node in a group of the route's chain, so the proxy cannot move the route to
// another node, and with it another IP or country, when the node jitters.
// The selections it replaced are saved to disk and put back when pinning is
// turned off or ABeast quits, or at the next start after a crash.
type Pinner struct {
	mu    sync.Mutex
	api   *clash.Client
	file  string
	saved map[string]pin // by group
}

func pinnedPath() string { return filepath.Join(config.Dir(), "pinned.json") }

func newPinner(api *clash.Client, file string) *Pinner {
	p := &Pinner{api: api, file: file, saved: map[string]pin{}}
	if data, err := os.ReadFile(file); err == nil {
		_ = json.Unmarshal(data, &p.saved)
	}
	return p
}

func (p *Pinner) persist() {
	if len(p.saved) == 0 {
		_ = os.Remove(p.file)
		return
	}
	data, _ := json.MarshalIndent(p.saved, "", "  ")
	_ = os.WriteFile(p.file, data, 0o644)
}

// Apply pins the node of each safe route when enabled, and unpins all when
// not. A route whose node is unsafe is left alone: pinning would hold it on
// the wrong exit.
func (p *Pinner) Apply(ctx context.Context, routes []Route, enabled bool) PinReport {
	p.mu.Lock()
	defer p.mu.Unlock()
	rep := PinReport{Routes: map[string]PinState{}}
	if !enabled {
		p.restoreLocked(ctx)
		return rep
	}
	proxies, err := p.api.Proxies(ctx)
	if err != nil {
		for _, r := range routes {
			rep.Routes[r.Target.Name] = PinState{Err: "控制接口不可用：" + err.Error()}
		}
		return rep
	}
	// Selected by hand since: the user's choice wins, and is not undone.
	for g, s := range p.saved {
		info, ok := proxies[g]
		now := info.Now
		if s.Type != "Selector" {
			now = info.Fixed
		}
		if !ok || (now != s.Node && now != "REJECT") {
			log.Printf("pin: %s moved to %q by hand, released", g, now)
			delete(p.saved, g)
		}
	}
	for _, r := range routes {
		st := PinState{}
		if len(r.Chain) < 2 {
			if r.Err == "" && r.Node != "" {
				st.Err = "这条路线直接走 " + r.Node + "，没有可锁定的分组"
			}
			rep.Routes[r.Target.Name] = st
			continue
		}
		node := r.Chain[0]
		if g, ok := p.pinnedIn(r.Chain); ok {
			s := p.saved[g]
			st.Group, st.Node, st.Fixed = g, s.Node, s.Type != "Selector"
			rep.Routes[r.Target.Name] = st
			continue
		}
		if r.Unsafe || node == "DIRECT" || node == "REJECT" {
			rep.Routes[r.Target.Name] = st
			continue
		}
		g, typ := pinPoint(proxies, r.Chain)
		if g == "" {
			st.Err = "链路里没有能直接选到 " + node + " 的分组"
			rep.Routes[r.Target.Name] = st
			continue
		}
		info := proxies[g]
		if err := p.api.Select(ctx, g, node); err != nil {
			st.Err = "锁定失败：" + err.Error()
			rep.Routes[r.Target.Name] = st
			continue
		}
		s := pin{Node: node, Type: typ}
		if typ == "Selector" {
			s.Prev = info.Now
		}
		p.saved[g] = s
		p.persist()
		info.Now = node
		proxies[g] = info
		log.Printf("pin: %s → %s (was %s)", g, node, s.Prev)
		st.Group, st.Node, st.Fixed = g, node, typ != "Selector"
		rep.Routes[r.Target.Name] = st
	}
	return rep
}

// pinnedIn finds a group of the chain the pinner holds on the chain's node.
func (p *Pinner) pinnedIn(chain []string) (string, bool) {
	for _, g := range chain[1:] {
		if s, ok := p.saved[g]; ok && s.Node == chain[0] {
			return g, true
		}
	}
	return "", false
}

// pinPoint picks the group of a chain (node first, the rule's policy last)
// to select the node in: the selector nearest the rule that lists the node
// itself, else a url-test or fallback group listing it, which the core can
// fix on a node too.
func pinPoint(proxies map[string]clash.ProxyInfo, chain []string) (group, typ string) {
	node := chain[0]
	for _, types := range [][]string{{"Selector"}, {"URLTest", "Fallback"}} {
		for i := len(chain) - 1; i >= 1; i-- {
			info, ok := proxies[chain[i]]
			if ok && slices.Contains(types, info.Type) && slices.Contains(info.All, node) {
				return chain[i], info.Type
			}
		}
	}
	return "", ""
}

// RestoreAll unpins every group, before quitting.
func (p *Pinner) RestoreAll(ctx context.Context) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.restoreLocked(ctx)
}

func (p *Pinner) restoreLocked(ctx context.Context) {
	for g, s := range p.saved {
		var err error
		switch {
		case s.Type != "Selector":
			err = p.api.Unfix(ctx, g)
		case s.Prev != "" && s.Prev != s.Node:
			// Only while still on the pinned node: one picked by hand stays.
			if info, gerr := p.api.Group(ctx, g); gerr == nil && info.Now == s.Node {
				err = p.api.Select(ctx, g, s.Prev)
			}
		}
		if err != nil {
			log.Printf("pin: restore %s: %v", g, err)
			continue
		}
		log.Printf("pin: %s released (back to %s)", g, s.Prev)
		delete(p.saved, g)
	}
	p.persist()
}
