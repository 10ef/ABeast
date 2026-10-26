package monitor

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"sync"

	"github.com/10ef/ABeast/internal/agent"
	"github.com/10ef/ABeast/internal/clash"
	"github.com/10ef/ABeast/internal/config"
)

// BlockState is one route's group in Mihomo.
type BlockState struct {
	Group   string   `json:"group"`
	Path    []string `json:"path,omitempty"` // the group's selection, down to the node
	Blocked bool     `json:"blocked"`
	Agents  []string `json:"agents,omitempty"` // agent connections, e.g. "codex ×3 → 日本 03"
	Err     string   `json:"error,omitempty"`
}

type BlockReport struct {
	Available bool                  `json:"available"`
	Mode      string                `json:"mode,omitempty"`
	Err       string                `json:"error,omitempty"`
	Routes    map[string]BlockState `json:"routes,omitempty"` // by target name
	Closed    int                   `json:"closed"`
}

// Blocker switches the Mihomo group of an unsafe route to REJECT, so no
// request of any program reaches that API through the wrong exit, and
// switches it back once the route is safe. The selection it replaced is
// saved to disk, so a crash cannot leave a group rejecting for good.
type Blocker struct {
	mu    sync.Mutex
	api   *clash.Client
	file  string            // where saved is kept
	saved map[string]string // group → selection to restore
}

func blockedPath() string { return filepath.Join(config.Dir(), "blocked.json") }

func newBlocker(api *clash.Client, file string) *Blocker {
	b := &Blocker{api: api, file: file, saved: map[string]string{}}
	if data, err := os.ReadFile(file); err == nil {
		_ = json.Unmarshal(data, &b.saved)
	}
	return b
}

func (b *Blocker) persist() {
	if len(b.saved) == 0 {
		_ = os.Remove(b.file)
		return
	}
	data, _ := json.MarshalIndent(b.saved, "", "  ")
	_ = os.WriteFile(b.file, data, 0o644)
}

// Apply rejects the groups of unsafe routes when enabled and restores the
// others.
func (b *Blocker) Apply(ctx context.Context, routes []Route, enabled bool) BlockReport {
	b.mu.Lock()
	defer b.mu.Unlock()
	rep := BlockReport{Routes: map[string]BlockState{}}
	mode, err := b.api.Mode(ctx)
	if err != nil {
		rep.Err = "控制接口不可用：" + err.Error()
		return rep
	}
	rep.Available, rep.Mode = true, mode
	proxies, err := b.api.Proxies(ctx)
	if err != nil {
		rep.Err = err.Error()
		return rep
	}
	conns, _ := b.api.Connections(ctx)
	wantBlock := false
	for _, r := range routes {
		g := r.Target.BlockGroup
		if g == "" {
			continue
		}
		st := BlockState{Group: g}
		info, ok := proxies[g]
		switch {
		case !ok:
			st.Err = "找不到策略组，先在 Clash 客户端里启用 ABeast 覆写脚本"
		case r.Unsafe && enabled:
			wantBlock = true
			if info.Now != "REJECT" {
				if !slices.Contains(info.All, "REJECT") {
					st.Err = "策略组没有 REJECT 选项"
					break
				}
				b.saved[g] = info.Now
				b.persist()
				if err := b.api.Select(ctx, g, "REJECT"); err != nil {
					st.Err = err.Error()
					break
				}
				log.Printf("block: %s %s → REJECT", g, info.Now)
				info.Now = "REJECT"
				proxies[g] = info
			}
			st.Blocked = true
			// Cut what was opened before the switch: an agent's open
			// stream would otherwise go on through the old exit.
			for _, c := range conns {
				if slices.Contains(c.Chains, g) && len(c.Chains) > 0 && c.Chains[0] != "REJECT" {
					if b.api.CloseConn(ctx, c.ID) == nil {
						rep.Closed++
					}
				}
			}
		default:
			prev, saved := b.saved[g]
			if !saved {
				break
			}
			if info.Now == "REJECT" {
				if err := b.api.Select(ctx, g, prev); err != nil {
					st.Err = err.Error()
					break
				}
				log.Printf("block: %s REJECT → %s", g, prev)
				info.Now = prev
				proxies[g] = info
			}
			delete(b.saved, g)
			b.persist()
		}
		st.Path = clash.Resolve(proxies, g)
		st.Agents = agentConns(conns, r.Target)
		rep.Routes[r.Target.Name] = st
	}
	if wantBlock && mode != "rule" {
		rep.Err = fmt.Sprintf("Mihomo 处于 %s 模式，策略组阻断不生效", mode)
	}
	return rep
}

// RestoreAll puts back every selection the blocker replaced, before quitting.
func (b *Blocker) RestoreAll(ctx context.Context) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for g, prev := range b.saved {
		if info, err := b.api.Group(ctx, g); err == nil && info.Now == "REJECT" {
			if err := b.api.Select(ctx, g, prev); err != nil {
				log.Printf("block: restore %s: %v", g, err)
				continue
			}
			log.Printf("block: %s REJECT → %s", g, prev)
		}
		delete(b.saved, g)
	}
	b.persist()
}

// agentConns summarizes the connections the target's agents have open, by
// process and the node they leave through.
func agentConns(conns []clash.Conn, t config.Target) []string {
	count := map[string]int{}
	for _, c := range conns {
		name := agent.NormName(c.Metadata.Process)
		if name == "" && c.Metadata.ProcessPath != "" {
			name = agent.NormName(filepath.Base(c.Metadata.ProcessPath))
		}
		if !slices.ContainsFunc(t.Processes, func(p string) bool { return agent.NormName(p) == name }) {
			continue
		}
		node := "?"
		if len(c.Chains) > 0 {
			node = c.Chains[0]
		}
		count[name+" → "+node]++
	}
	keys := make([]string, 0, len(count))
	for k := range count {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := make([]string, 0, len(keys))
	for _, k := range keys {
		name, node, _ := strings.Cut(k, " → ")
		out = append(out, fmt.Sprintf("%s ×%d → %s", name, count[k], node))
	}
	return out
}
