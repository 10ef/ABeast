package clash

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/10ef/ABeast/internal/config"
)

// Client is a client of the Clash RESTful API.
type Client struct {
	cfg config.Mihomo
	hc  *http.Client
	// only keeps to the configured controller, without looking for the
	// clients installed here: tests must not reach the machine's own.
	only bool

	mu  sync.Mutex
	api *controller // the controller found, nil until then
}

// controller is a Clash API controller and the client that reaches it.
type controller struct {
	addr, base, secret string
	hc                 *http.Client
}

// NewFixed is New keeping to the configured controller, without looking for
// the clients installed here: tests must not reach the machine's own.
func NewFixed(c config.Mihomo) *Client {
	m := New(c)
	m.only = true
	return m
}

// New returns a client of the configured controller, or of one found in the
// configs of known clients, on their named pipes, or on the default ports.
func New(c config.Mihomo) *Client {
	return &Client{
		cfg: c,
		// Never through a proxy: the controller is local.
		hc: &http.Client{Timeout: 5 * time.Second, Transport: &http.Transport{Proxy: nil}},
	}
}

// Addr is the controller in use, or "" before one answered.
func (m *Client) Addr() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.api == nil {
		return ""
	}
	return m.api.addr
}

// connect finds a controller that answers /version with its secret.
func (m *Client) connect(ctx context.Context) (*controller, error) {
	m.mu.Lock()
	api := m.api
	m.mu.Unlock()
	if api != nil {
		return api, nil
	}
	tried := map[string]bool{}
	candidates := controllerCandidates(m.cfg)
	if m.only {
		candidates = []config.Mihomo{m.cfg}
	}
	for _, c := range candidates {
		api := m.dial(c)
		key := api.addr + " " + api.base + " " + api.secret
		if tried[key] {
			continue
		}
		tried[key] = true
		if api.request(ctx, http.MethodGet, "/version", nil, nil) == nil {
			m.mu.Lock()
			m.api = api
			m.mu.Unlock()
			return api, nil
		}
	}
	return nil, fmt.Errorf("找不到 Clash API 控制接口")
}

// dial makes the client of a controller: TCP ones share hc, a named pipe or
// Unix socket gets a client that dials it.
func (m *Client) dial(c config.Mihomo) *controller {
	if path, ok := socketPath(c.Controller); ok {
		return &controller{addr: c.Controller, base: "http://localhost", secret: c.Secret, hc: &http.Client{
			Timeout: m.hc.Timeout,
			Transport: &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
				if strings.HasPrefix(path, `\\.\pipe\`) {
					return dialPipe(ctx, path)
				}
				var d net.Dialer
				return d.DialContext(ctx, "unix", path)
			}},
		}}
	}
	base := c.Controller
	if !strings.Contains(base, "://") {
		base = "http://" + base
	}
	return &controller{addr: c.Controller, base: strings.TrimRight(base, "/"), secret: c.Secret, hc: m.hc}
}

// socketPath is the named pipe or Unix socket a controller names, if it is
// not a TCP address.
func socketPath(addr string) (string, bool) {
	switch {
	case strings.HasPrefix(addr, `\\.\pipe\`):
		return addr, true
	case strings.HasPrefix(addr, "unix://"):
		return strings.TrimPrefix(addr, "unix://"), true
	}
	return "", false
}

// controllerCandidates lists the configured controller, those written in the
// configs of known clients, the named pipes of running cores, then the
// default ports.
func controllerCandidates(c config.Mihomo) []config.Mihomo {
	var out []config.Mihomo
	if c.Controller != "" {
		out = append(out, c)
	}
	for _, path := range clientConfigs() {
		out = append(out, readController(path)...)
	}
	// Clash Verge Rev's service starts the core on a pipe its config does
	// not name: try the pipes that are there.
	for _, pipe := range mihomoPipes() {
		out = append(out, config.Mihomo{Controller: pipe})
	}
	for _, port := range []string{"9090", "9097"} {
		out = append(out, config.Mihomo{Controller: "127.0.0.1:" + port, Secret: c.Secret})
	}
	return out
}

// clientConfigs are the config files that Clash clients hand their core.
func clientConfigs() []string {
	home, _ := os.UserHomeDir()
	var bases []string
	switch runtime.GOOS {
	case "darwin":
		bases = []string{filepath.Join(home, "Library", "Application Support")}
	case "windows":
		bases = []string{os.Getenv("APPDATA"), os.Getenv("LOCALAPPDATA")}
	default:
		bases = []string{filepath.Join(home, ".config"), filepath.Join(home, ".local", "share")}
	}
	rel := []string{
		"com.follow.clash/config.yaml",                                // FlClash
		"io.github.clash-verge-rev.clash-verge-rev/clash-verge.yaml",  // Clash Verge Rev (runtime)
		"io.github.clash-verge-rev.clash-verge-rev/config.yaml",       // Clash Verge Rev
		"mihomo-party/work/config.yaml",                               // mihomo-party
		"io.github.clash-nyanpasu.clash-nyanpasu/clash-nyanpasu.yaml", // Clash Nyanpasu
		"mihomo/config.yaml",                                          // mihomo
		"clash/config.yaml",                                           // clash
	}
	var paths []string
	for _, b := range bases {
		if b == "" {
			continue
		}
		for _, r := range rel {
			paths = append(paths, filepath.Join(b, filepath.FromSlash(r)))
		}
	}
	return paths
}

// readController reads the controllers in the top-level keys of a config,
// with its secret. The named pipe and Unix socket come first: Clash Verge
// Rev serves the API only there, leaving external-controller empty.
func readController(path string) []config.Mihomo {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var addr, pipe, sock, secret string
	for _, line := range strings.Split(string(data), "\n") {
		k, v, ok := strings.Cut(strings.TrimRight(line, "\r"), ":")
		if !ok {
			continue
		}
		raw := strings.TrimSpace(v)
		v = strings.Trim(raw, `"'`)
		if strings.HasPrefix(raw, `"`) {
			v = strings.ReplaceAll(v, `\\`, `\`) // "\\\\.\\pipe\\name"
		}
		switch k {
		case "external-controller":
			addr = v
		case "external-controller-pipe":
			pipe = v
		case "external-controller-unix":
			sock = v
		case "secret":
			secret = v
		}
	}
	var out []config.Mihomo
	if pipe != "" && runtime.GOOS == "windows" {
		out = append(out, config.Mihomo{Controller: pipe, Secret: secret})
	}
	if sock != "" {
		// A relative path is in the core's home, where its config is.
		if !filepath.IsAbs(sock) {
			sock = filepath.Join(filepath.Dir(path), sock)
		}
		out = append(out, config.Mihomo{Controller: "unix://" + sock, Secret: secret})
	}
	if addr != "" {
		// ":9090" and "0.0.0.0:9090" listen on every interface; dial loopback.
		if host, port, ok := strings.Cut(addr, ":"); ok && (host == "" || host == "0.0.0.0") {
			addr = "127.0.0.1:" + port
		}
		out = append(out, config.Mihomo{Controller: addr, Secret: secret})
	}
	return out
}

func (m *Client) do(ctx context.Context, method, path string, body, out any) error {
	api, err := m.connect(ctx)
	if err != nil {
		return err
	}
	err = api.request(ctx, method, path, body, out)
	if err != nil && !strings.Contains(err.Error(), "HTTP ") {
		// Unreachable: find the controller again next time, e.g. after
		// the client restarted on another port.
		m.mu.Lock()
		if m.api == api {
			m.api = nil
		}
		m.mu.Unlock()
	}
	return err
}

func (c *controller) request(ctx context.Context, method, path string, body, out any) error {
	var rd io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return err
		}
		rd = bytes.NewReader(data)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.base+path, rd)
	if err != nil {
		return err
	}
	if c.secret != "" {
		req.Header.Set("Authorization", "Bearer "+c.secret)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.hc.Do(req)
	if err != nil {
		if ue, ok := err.(*url.Error); ok {
			return ue.Err // without the URL net/http puts in front
		}
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return fmt.Errorf("%s %s: HTTP %d %s", method, path, resp.StatusCode, strings.TrimSpace(string(msg)))
	}
	if out == nil {
		return nil
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

// ProxyInfo is a proxy or a group; groups have Now and All.
type ProxyInfo struct {
	Name string   `json:"name"`
	Type string   `json:"type"`
	Now  string   `json:"now"`
	All  []string `json:"all"`
	// Fixed is the node a url-test or fallback group was fixed on; the core
	// still moves off it while it is dead.
	Fixed string `json:"fixed,omitempty"`
}

func (m *Client) Proxies(ctx context.Context) (map[string]ProxyInfo, error) {
	var r struct {
		Proxies map[string]ProxyInfo `json:"proxies"`
	}
	err := m.do(ctx, http.MethodGet, "/proxies", nil, &r)
	return r.Proxies, err
}

func (m *Client) Group(ctx context.Context, name string) (ProxyInfo, error) {
	var p ProxyInfo
	err := m.do(ctx, http.MethodGet, "/proxies/"+url.PathEscape(name), nil, &p)
	return p, err
}

// Select picks a member of a Selector group.
func (m *Client) Select(ctx context.Context, group, name string) error {
	return m.do(ctx, http.MethodPut, "/proxies/"+url.PathEscape(group), map[string]string{"name": name}, nil)
}

// Unfix lets a url-test or fallback group pick its node again.
func (m *Client) Unfix(ctx context.Context, group string) error {
	return m.do(ctx, http.MethodDelete, "/proxies/"+url.PathEscape(group), nil, nil)
}

// Delay has the core fetch testURL through one proxy and returns how long it
// took, or an error when it timed out.
func (m *Client) Delay(ctx context.Context, proxy, testURL string, timeout time.Duration) (int, error) {
	var r struct {
		Delay int `json:"delay"`
	}
	q := url.Values{"url": {testURL}, "timeout": {fmt.Sprint(timeout.Milliseconds())}}
	err := m.do(ctx, http.MethodGet, "/proxies/"+url.PathEscape(proxy)+"/delay?"+q.Encode(), nil, &r)
	if err == nil && r.Delay <= 0 {
		err = fmt.Errorf("超时")
	}
	return r.Delay, err
}

// General is the part of the core's running config the self-check reads.
type General struct {
	Mode string `json:"mode"`
	Tun  struct {
		Enable      bool `json:"enable"`
		AutoRoute   bool `json:"auto-route"`
		StrictRoute bool `json:"strict-route"`
	} `json:"tun"`
}

func (m *Client) General(ctx context.Context) (General, error) {
	var g General
	err := m.do(ctx, http.MethodGet, "/configs", nil, &g)
	g.Mode = strings.ToLower(g.Mode)
	return g, err
}

func (m *Client) Mode(ctx context.Context) (string, error) {
	var r struct {
		Mode string `json:"mode"`
	}
	err := m.do(ctx, http.MethodGet, "/configs", nil, &r)
	return strings.ToLower(r.Mode), err
}

// Conn is an open connection through Mihomo.
type Conn struct {
	ID       string `json:"id"`
	Metadata struct {
		Host        string `json:"host"`
		Process     string `json:"process"`
		ProcessPath string `json:"processPath"`
		SourcePort  string `json:"sourcePort"`
	} `json:"metadata"`
	// Chains go from the outbound proxy to the group the rule picked.
	Chains      []string `json:"chains"`
	Rule        string   `json:"rule"`
	RulePayload string   `json:"rulePayload"`
}

func (m *Client) Connections(ctx context.Context) ([]Conn, error) {
	var r struct {
		Connections []Conn `json:"connections"`
	}
	err := m.do(ctx, http.MethodGet, "/connections", nil, &r)
	return r.Connections, err
}

func (m *Client) CloseConn(ctx context.Context, id string) error {
	return m.do(ctx, http.MethodDelete, "/connections/"+url.PathEscape(id), nil, nil)
}

// Resolve follows a group's selection down to the outbound proxy:
// "🛡 EG-OpenAI → 🌏 ChatGPT/Claude → 🔰 节点选择 → 日本 03".
func Resolve(proxies map[string]ProxyInfo, name string) []string {
	path := []string{name}
	for range 10 {
		p, ok := proxies[name]
		if !ok || p.Now == "" {
			break
		}
		name = p.Now
		path = append(path, name)
	}
	return path
}
