package monitor

import (
	"context"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/10ef/ABeast/internal/clash"
	"github.com/10ef/ABeast/internal/config"
)

func TestPinPoint(t *testing.T) {
	proxies := map[string]clash.ProxyInfo{
		"🤖 AI":   {Type: "Selector", Now: "🚀 节点选择", All: []string{"🚀 节点选择", "DIRECT"}},
		"🚀 节点选择": {Type: "Selector", Now: "♻️ 自动", All: []string{"♻️ 自动", "日本 02", "香港 01"}},
		"♻️ 自动":  {Type: "URLTest", Now: "日本 02", All: []string{"日本 02", "香港 01"}},
	}
	chain := []string{"日本 02", "♻️ 自动", "🚀 节点选择", "🤖 AI"}
	if g, typ := pinPoint(proxies, chain); g != "🚀 节点选择" || typ != "Selector" {
		t.Fatal(g, typ)
	}
	// No selector lists the node: fix the url-test group on it.
	delete(proxies, "🚀 节点选择")
	if g, typ := pinPoint(proxies, chain); g != "♻️ 自动" || typ != "URLTest" {
		t.Fatal(g, typ)
	}
	if g, _ := pinPoint(map[string]clash.ProxyInfo{}, chain); g != "" {
		t.Fatal(g)
	}
}

func pinFake() *fakeClash {
	f := newFakeClash()
	f.proxies["🌏 ChatGPT/Claude"] = clash.ProxyInfo{Name: "🌏 ChatGPT/Claude", Type: "Selector", Now: "♻️ 自动", All: []string{"♻️ 自动", "日本 03", "香港 01"}}
	f.proxies["♻️ 自动"] = clash.ProxyInfo{Name: "♻️ 自动", Type: "URLTest", Now: "日本 03", All: []string{"日本 03", "香港 01"}}
	f.proxies["香港 01"] = clash.ProxyInfo{Name: "香港 01", Type: "Trojan"}
	return f
}

func TestPinnerPinsAndRestores(t *testing.T) {
	fake := pinFake()
	srv := httptest.NewServer(fake)
	defer srv.Close()
	file := filepath.Join(t.TempDir(), "pinned.json")
	p := newPinner(clash.New(config.Mihomo{Controller: srv.URL, Secret: "s3cret"}), file)
	ctx := context.Background()
	codex := Route{Target: config.Target{Name: "Codex"}, Node: "日本 03", Chain: []string{"日本 03", "♻️ 自动", "🌏 ChatGPT/Claude"}}
	claude := Route{Target: config.Target{Name: "Claude"}, Node: "日本 03", Chain: []string{"日本 03", "♻️ 自动", "🌏 ChatGPT/Claude"}}

	rep := p.Apply(ctx, []Route{codex, claude}, true)
	if fake.now("🌏 ChatGPT/Claude") != "日本 03" {
		t.Fatal(fake.now("🌏 ChatGPT/Claude"))
	}
	if st := rep.Routes["Claude"]; st.Group != "🌏 ChatGPT/Claude" || st.Node != "日本 03" || st.Fixed {
		t.Fatalf("%+v", st)
	}
	if _, err := os.Stat(file); err != nil {
		t.Fatal("the replaced selection must be saved:", err)
	}
	// A crash: a new pinner restores from the file.
	p = newPinner(clash.New(config.Mihomo{Controller: srv.URL, Secret: "s3cret"}), file)
	p.Apply(ctx, []Route{codex}, false)
	if fake.now("🌏 ChatGPT/Claude") != "♻️ 自动" {
		t.Fatal(fake.now("🌏 ChatGPT/Claude"))
	}
	if _, err := os.Stat(file); !os.IsNotExist(err) {
		t.Fatal("restored pins must leave no file")
	}

	// Picked by hand: released, not undone, and the new pick pinned.
	p.Apply(ctx, []Route{codex}, true)
	fake.mu.Lock()
	g := fake.proxies["🌏 ChatGPT/Claude"]
	g.Now = "香港 01"
	fake.proxies["🌏 ChatGPT/Claude"] = g
	fake.mu.Unlock()
	hk := Route{Target: config.Target{Name: "Codex"}, Node: "香港 01", Chain: []string{"香港 01", "🌏 ChatGPT/Claude"}}
	rep = p.Apply(ctx, []Route{hk}, true)
	if st := rep.Routes["Codex"]; st.Node != "香港 01" {
		t.Fatalf("%+v", st)
	}
	p.RestoreAll(ctx)
	if fake.now("🌏 ChatGPT/Claude") != "香港 01" {
		t.Fatal("a selection made by hand must stay:", fake.now("🌏 ChatGPT/Claude"))
	}

	// An unsafe route is not pinned on its bad node.
	bad := codex
	bad.Unsafe = true
	if st := p.Apply(ctx, []Route{bad}, true).Routes["Codex"]; st.Node != "" {
		t.Fatalf("%+v", st)
	}
}

func TestPinnerFixesURLTest(t *testing.T) {
	fake := pinFake()
	delete(fake.proxies, "🌏 ChatGPT/Claude")
	srv := httptest.NewServer(fake)
	defer srv.Close()
	p := newPinner(clash.New(config.Mihomo{Controller: srv.URL, Secret: "s3cret"}), filepath.Join(t.TempDir(), "pinned.json"))
	ctx := context.Background()
	r := Route{Target: config.Target{Name: "Codex"}, Node: "日本 03", Chain: []string{"日本 03", "♻️ 自动"}}
	if st := p.Apply(ctx, []Route{r}, true).Routes["Codex"]; st.Group != "♻️ 自动" || !st.Fixed {
		t.Fatalf("%+v", st)
	}
	p.RestoreAll(ctx)
	fake.mu.Lock()
	defer fake.mu.Unlock()
	if f := fake.proxies["♻️ 自动"].Fixed; f != "" {
		t.Fatal("unpinning must unfix the group:", f)
	}
}
