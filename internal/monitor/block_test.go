package monitor

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/10ef/ABeast/internal/clash"
	"github.com/10ef/ABeast/internal/config"
)

// fakeClash is a Clash API with the groups of the ExitGuard override.
type fakeClash struct {
	mu      sync.Mutex
	proxies map[string]clash.ProxyInfo
	conns   []clash.Conn
	closed  []string
	mode    string
	delay   map[string]int // a proxy's delay; missing times out
	tun     map[string]any
}

func newFakeClash() *fakeClash {
	return &fakeClash{mode: "rule", proxies: map[string]clash.ProxyInfo{
		"🛡 EG-OpenAI":      {Name: "🛡 EG-OpenAI", Type: "Selector", Now: "🌏 ChatGPT/Claude", All: []string{"🌏 ChatGPT/Claude", "REJECT"}},
		"🛡 EG-Anthropic":   {Name: "🛡 EG-Anthropic", Type: "Selector", Now: "🌏 ChatGPT/Claude", All: []string{"🌏 ChatGPT/Claude", "REJECT"}},
		"🌏 ChatGPT/Claude": {Name: "🌏 ChatGPT/Claude", Type: "Selector", Now: "日本 03", All: []string{"日本 03"}},
		"NoReject":         {Name: "NoReject", Type: "Selector", Now: "日本 03", All: []string{"日本 03"}},
		"日本 03":            {Name: "日本 03", Type: "Trojan"},
	}}
}

func (f *fakeClash) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("Authorization") != "Bearer s3cret" {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	path := r.URL.Path
	switch {
	case path == "/version":
		json.NewEncoder(w).Encode(map[string]any{"meta": true, "version": "test"})
	case path == "/configs":
		json.NewEncoder(w).Encode(map[string]any{"mode": f.mode, "tun": f.tun})
	case path == "/proxies":
		json.NewEncoder(w).Encode(map[string]any{"proxies": f.proxies})
	case strings.HasPrefix(path, "/proxies/") && strings.HasSuffix(path, "/delay"):
		d, ok := f.delay[strings.TrimSuffix(strings.TrimPrefix(path, "/proxies/"), "/delay")]
		if !ok {
			w.WriteHeader(http.StatusRequestTimeout)
			return
		}
		json.NewEncoder(w).Encode(map[string]int{"delay": d})
	case strings.HasPrefix(path, "/proxies/"):
		name := strings.TrimPrefix(path, "/proxies/")
		p, ok := f.proxies[name]
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		if r.Method == http.MethodPut {
			var body struct{ Name string }
			json.NewDecoder(r.Body).Decode(&body)
			p.Now = body.Name
			if p.Type != "Selector" {
				p.Fixed = body.Name
			}
			f.proxies[name] = p
			w.WriteHeader(http.StatusNoContent)
			return
		}
		if r.Method == http.MethodDelete {
			p.Fixed = ""
			f.proxies[name] = p
			w.WriteHeader(http.StatusNoContent)
			return
		}
		json.NewEncoder(w).Encode(p)
	case path == "/connections":
		json.NewEncoder(w).Encode(map[string]any{"connections": f.conns})
	case strings.HasPrefix(path, "/connections/") && r.Method == http.MethodDelete:
		f.closed = append(f.closed, strings.TrimPrefix(path, "/connections/"))
		w.WriteHeader(http.StatusNoContent)
	default:
		w.WriteHeader(http.StatusNotFound)
	}
}

func (f *fakeClash) now(group string) string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.proxies[group].Now
}

func codexConn(id string) clash.Conn {
	var c clash.Conn
	c.ID = id
	c.Metadata.Host = "chatgpt.com"
	c.Metadata.Process = "codex"
	c.Chains = []string{"日本 03", "🌏 ChatGPT/Claude", "🛡 EG-OpenAI"}
	return c
}

func TestBlockerRejectsAndRestores(t *testing.T) {
	fake := newFakeClash()
	fake.conns = []clash.Conn{codexConn("c1")}
	srv := httptest.NewServer(fake)
	defer srv.Close()
	file := filepath.Join(t.TempDir(), "blocked.json")
	b := newBlocker(clash.New(config.Mihomo{Controller: srv.URL, Secret: "s3cret"}), file)
	ctx := context.Background()
	codex := Route{Target: config.Target{Name: "Codex", BlockGroup: "🛡 EG-OpenAI", Processes: []string{"codex"}}}
	claude := Route{Target: config.Target{Name: "Claude", BlockGroup: "🛡 EG-Anthropic"}}

	rep := b.Apply(ctx, []Route{codex, claude}, true)
	if !rep.Available || rep.Routes["Codex"].Blocked || fake.now("🛡 EG-OpenAI") != "🌏 ChatGPT/Claude" {
		t.Fatalf("safe routes must not be blocked: %+v", rep)
	}
	if got := strings.Join(rep.Routes["Codex"].Path, " → "); got != "🛡 EG-OpenAI → 🌏 ChatGPT/Claude → 日本 03" {
		t.Fatalf("path %q", got)
	}
	if got := rep.Routes["Codex"].Agents; len(got) != 1 || got[0] != "codex ×1 → 日本 03" {
		t.Fatalf("agents %v", got)
	}

	codex.Unsafe = true
	rep = b.Apply(ctx, []Route{codex, claude}, false)
	if fake.now("🛡 EG-OpenAI") == "REJECT" {
		t.Fatal("disabled blocking must not switch groups")
	}
	rep = b.Apply(ctx, []Route{codex, claude}, true)
	if !rep.Routes["Codex"].Blocked || fake.now("🛡 EG-OpenAI") != "REJECT" {
		t.Fatalf("not blocked: %+v", rep.Routes["Codex"])
	}
	if fake.now("🛡 EG-Anthropic") != "🌏 ChatGPT/Claude" {
		t.Fatal("blocking Codex must leave Claude alone")
	}
	if rep.Closed != 1 || len(fake.closed) != 1 || fake.closed[0] != "c1" {
		t.Fatalf("open connections must be cut: %v", fake.closed)
	}
	if data, err := os.ReadFile(file); err != nil || !strings.Contains(string(data), "🌏 ChatGPT/Claude") {
		t.Fatalf("selection not saved: %s %v", data, err)
	}

	// A restart while blocked restores from the file once the route is safe.
	b2 := newBlocker(clash.New(config.Mihomo{Controller: srv.URL, Secret: "s3cret"}), file)
	codex.Unsafe = false
	b2.Apply(ctx, []Route{codex, claude}, true)
	if fake.now("🛡 EG-OpenAI") != "🌏 ChatGPT/Claude" {
		t.Fatal("not restored after recovery")
	}
	if _, err := os.Stat(file); !os.IsNotExist(err) {
		t.Fatal("blocked.json must be removed once nothing is blocked")
	}

	// Quitting while blocked restores too.
	codex.Unsafe = true
	b2.Apply(ctx, []Route{codex}, true)
	b2.RestoreAll(ctx)
	if fake.now("🛡 EG-OpenAI") != "🌏 ChatGPT/Claude" {
		t.Fatal("RestoreAll did not restore")
	}
}

func TestBlockerReportsGroupsItCannotBlock(t *testing.T) {
	fake := newFakeClash()
	srv := httptest.NewServer(fake)
	defer srv.Close()
	b := newBlocker(clash.New(config.Mihomo{Controller: srv.URL, Secret: "s3cret"}), filepath.Join(t.TempDir(), "b.json"))
	routes := []Route{
		{Target: config.Target{Name: "A", BlockGroup: "NoReject"}, Unsafe: true},
		{Target: config.Target{Name: "B", BlockGroup: "Missing"}, Unsafe: true},
	}
	rep := b.Apply(context.Background(), routes, true)
	if !strings.Contains(rep.Routes["A"].Err, "REJECT") || rep.Routes["A"].Blocked {
		t.Fatalf("A: %+v", rep.Routes["A"])
	}
	if rep.Routes["B"].Err == "" {
		t.Fatalf("B: %+v", rep.Routes["B"])
	}
	fake.mode = "global"
	rep = b.Apply(context.Background(), routes, true)
	if !strings.Contains(rep.Err, "global") {
		t.Fatalf("global mode must be reported: %q", rep.Err)
	}
}

func TestBlockerWithoutController(t *testing.T) {
	b := newBlocker(clash.NewFixed(config.Mihomo{Controller: "127.0.0.1:1", Secret: "x"}), filepath.Join(t.TempDir(), "b.json"))
	rep := b.Apply(context.Background(), nil, true)
	if rep.Available || rep.Err == "" {
		t.Fatalf("%+v", rep)
	}
}
