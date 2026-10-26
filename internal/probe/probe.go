package probe

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptrace"
	"net/url"
	"os"
	"runtime"
	"strings"
	"sync"
	"time"
)

// Trace is what Cloudflare saw of one request: the exit IP and its country.
type Trace struct {
	IP      string        `json:"ip"`
	Country string        `json:"country"` // ISO code, e.g. "JP"
	Colo    string        `json:"colo"`    // Cloudflare data center, e.g. "NRT"
	Latency time.Duration `json:"-"`
	Ms      int64         `json:"latency_ms"`
	Via     string        `json:"via"` // the proxy used, or "direct"
}

type proxyFunc = func(*http.Request) (*url.URL, error)

// resolveProxy turns a Target.Proxy setting into the proxy the probe uses.
func resolveProxy(mode string) (proxyFunc, string, error) {
	switch mode {
	case "direct":
		return nil, "direct", nil
	case "env":
		return http.ProxyFromEnvironment, "env", nil
	case "system":
		if u := systemProxy(); u != nil {
			return http.ProxyURL(u), u.Host, nil
		}
		return nil, "direct", nil
	case "", "auto":
		for _, k := range []string{"HTTPS_PROXY", "https_proxy", "ALL_PROXY", "all_proxy"} {
			if os.Getenv(k) != "" {
				return http.ProxyFromEnvironment, "env", nil
			}
		}
		return resolveProxy("system")
	}
	u, err := url.Parse(mode)
	if err != nil || u.Host == "" {
		return nil, "", fmt.Errorf("bad proxy %q", mode)
	}
	return http.ProxyURL(u), u.Host, nil
}

// newClient returns a client that opens a new connection per request, so
// each probe goes through the proxy's rules again rather than reusing a
// connection to a node that has since changed.
func newClient(proxy proxyFunc, timeout time.Duration) *http.Client {
	return &http.Client{
		Timeout: timeout,
		Transport: &http.Transport{
			Proxy:               proxy,
			DisableKeepAlives:   true,
			TLSHandshakeTimeout: timeout,
		},
	}
}

// Exit asks the host's /cdn-cgi/trace where the request left. opened,
// if set, gets the probe's local port while the response is still open, to
// find the probe in the proxy's connections.
func Exit(ctx context.Context, host, proxyMode string, timeout time.Duration, opened func(port int)) (Trace, error) {
	proxy, via, err := resolveProxy(proxyMode)
	if err != nil {
		return Trace{}, err
	}
	var port int
	ctx = httptrace.WithClientTrace(ctx, &httptrace.ClientTrace{GotConn: func(i httptrace.GotConnInfo) {
		if a, ok := i.Conn.LocalAddr().(*net.TCPAddr); ok {
			port = a.Port
		}
	}})
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://"+host+"/cdn-cgi/trace", nil)
	if err != nil {
		return Trace{}, err
	}
	start := time.Now()
	// A transport of its own opens a new connection, through the proxy's
	// rules again. Unlike newClient it keeps the connection alive, so the
	// server does not close it before opened has found it among the proxy's.
	tr := &http.Transport{Proxy: proxy, TLSHandshakeTimeout: timeout}
	defer tr.CloseIdleConnections()
	resp, err := (&http.Client{Timeout: timeout, Transport: tr}).Do(req)
	if err != nil {
		return Trace{Via: via}, shortErr(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return Trace{Via: via}, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	if opened != nil && port != 0 {
		opened(port)
	}
	t := Trace{Via: via}
	sc := bufio.NewScanner(io.LimitReader(resp.Body, 4096))
	for sc.Scan() {
		k, v, _ := strings.Cut(sc.Text(), "=")
		switch k {
		case "ip":
			t.IP = v
		case "loc":
			t.Country = strings.ToUpper(v)
		case "colo":
			t.Colo = v
		}
	}
	t.Latency = time.Since(start)
	t.Ms = t.Latency.Milliseconds()
	if t.IP == "" || t.Country == "" {
		return t, fmt.Errorf("%s 不支持 /cdn-cgi/trace", host)
	}
	return t, nil
}

// shortErr drops the URL that net/http puts in front of every error.
func shortErr(err error) error {
	if ue, ok := err.(*url.Error); ok {
		if ue.Timeout() {
			return fmt.Errorf("超时")
		}
		return ue.Err
	}
	return err
}

// Geo describes an IP address.
type Geo struct {
	Country  string `json:"country"`
	Region   string `json:"region"`
	City     string `json:"city"`
	ISP      string `json:"isp"`
	ASN      string `json:"asn"`
	Timezone string `json:"timezone"`
}

// GeoCache looks IPs up once, then keeps them for hours: exits rarely move.
type GeoCache struct {
	mu      sync.Mutex
	entries map[string]geoEntry
}

type geoEntry struct {
	geo     Geo
	ok      bool
	expires time.Time
}

// NewGeoCache returns an empty cache.
func NewGeoCache() *GeoCache { return &GeoCache{entries: map[string]geoEntry{}} }

// Lookup returns where an IP is, from ipwho.is, else ipinfo.io.
func (c *GeoCache) Lookup(ctx context.Context, ip string, timeout time.Duration) (Geo, bool) {
	c.mu.Lock()
	e, hit := c.entries[ip]
	c.mu.Unlock()
	if hit && time.Now().Before(e.expires) {
		return e.geo, e.ok
	}
	g, err := lookupIPWhois(ctx, ip, timeout)
	if err != nil {
		g, err = lookupIPInfo(ctx, ip, timeout)
	}
	e = geoEntry{geo: g, ok: err == nil, expires: time.Now().Add(6 * time.Hour)}
	if err != nil {
		e.expires = time.Now().Add(5 * time.Minute)
	}
	c.mu.Lock()
	c.entries[ip] = e
	c.mu.Unlock()
	return e.geo, e.ok
}

func getJSON(ctx context.Context, rawURL string, timeout time.Duration, v any) error {
	proxy, _, _ := resolveProxy("auto")
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return err
	}
	resp, err := newClient(proxy, timeout).Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	return json.NewDecoder(io.LimitReader(resp.Body, 64<<10)).Decode(v)
}

func lookupIPWhois(ctx context.Context, ip string, timeout time.Duration) (Geo, error) {
	var r struct {
		Success    bool   `json:"success"`
		Message    string `json:"message"`
		Country    string `json:"country"`
		Region     string `json:"region"`
		City       string `json:"city"`
		Connection struct {
			ASN int    `json:"asn"`
			ISP string `json:"isp"`
			Org string `json:"org"`
		} `json:"connection"`
		Timezone struct {
			ID string `json:"id"`
		} `json:"timezone"`
	}
	if err := getJSON(ctx, "https://ipwho.is/"+url.PathEscape(ip)+"?lang=zh-CN", timeout, &r); err != nil {
		return Geo{}, err
	}
	if !r.Success {
		return Geo{}, fmt.Errorf("ipwho.is: %s", r.Message)
	}
	g := Geo{Country: r.Country, Region: r.Region, City: r.City, ISP: r.Connection.ISP, Timezone: r.Timezone.ID}
	if g.ISP == "" {
		g.ISP = r.Connection.Org
	}
	if r.Connection.ASN != 0 {
		g.ASN = fmt.Sprintf("AS%d", r.Connection.ASN)
	}
	return g, nil
}

func lookupIPInfo(ctx context.Context, ip string, timeout time.Duration) (Geo, error) {
	var r struct {
		City     string `json:"city"`
		Region   string `json:"region"`
		Country  string `json:"country"`
		Org      string `json:"org"`
		Timezone string `json:"timezone"`
	}
	if err := getJSON(ctx, "https://ipinfo.io/"+url.PathEscape(ip)+"/json", timeout, &r); err != nil {
		return Geo{}, err
	}
	asn, isp, _ := strings.Cut(r.Org, " ")
	return Geo{Country: r.Country, Region: r.Region, City: r.City, ISP: isp, ASN: asn, Timezone: r.Timezone}, nil
}

// Flag returns the emoji flag of an ISO country code.
func Flag(code string) string {
	if len(code) != 2 {
		return "🏳"
	}
	r := []rune(strings.ToUpper(code))
	if r[0] < 'A' || r[0] > 'Z' || r[1] < 'A' || r[1] > 'Z' {
		return "🏳"
	}
	return string([]rune{0x1F1E6 + r[0] - 'A', 0x1F1E6 + r[1] - 'A'})
}

// FlagBefore is a country's flag and a space, to put before its name or
// code; none on Windows, whose fonts draw a flag as its two letters.
func FlagBefore(code string) string {
	if runtime.GOOS == "windows" {
		return ""
	}
	return Flag(code) + " "
}
