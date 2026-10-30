package probe

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/10ef/ABeast/internal/config"
)

// Score is what one reputation source says of an exit IP.
type Score struct {
	Source string `json:"source"`
	Score  int    `json:"score"` // 0 high risk … 100 trusted
	Label  string `json:"label,omitempty"`
	Region string `json:"region,omitempty"` // "正常", or why the region is not supported
	// Unsupported is a region the vendor does not serve.
	Unsupported bool   `json:"unsupported,omitempty"`
	Page        string `json:"page,omitempty"`
	Err         string `json:"error,omitempty"`
}

// ScoreCache asks each source once per exit IP, then keeps the answer for
// hours: scores change slowly, and the probe round must not wait on them.
type ScoreCache struct {
	mu      sync.Mutex
	entries map[string]scoreEntry
}

type scoreEntry struct {
	score   Score
	expires time.Time
}

// NewScoreCache returns an empty cache.
func NewScoreCache() *ScoreCache { return &ScoreCache{entries: map[string]scoreEntry{}} }

// Lookup returns src's score of ip. proxyMode is the route's proxy, used
// when the URL has no {ip} and so scores whatever exit it is asked through.
func (c *ScoreCache) Lookup(ctx context.Context, src config.ScoreSource, ip, proxyMode string, timeout time.Duration) Score {
	key := src.Name + " " + ip
	c.mu.Lock()
	e, hit := c.entries[key]
	c.mu.Unlock()
	if hit && time.Now().Before(e.expires) {
		return e.score
	}
	s, err := fetchScore(ctx, src, ip, proxyMode, timeout)
	e = scoreEntry{score: s, expires: time.Now().Add(6 * time.Hour)}
	if err != nil {
		e.score = Score{Source: src.Name, Page: s.Page, Err: shortErr(err).Error()}
		e.expires = time.Now().Add(10 * time.Minute)
	}
	c.mu.Lock()
	c.entries[key] = e
	c.mu.Unlock()
	return e.score
}

func fetchScore(ctx context.Context, src config.ScoreSource, ip, proxyMode string, timeout time.Duration) (Score, error) {
	fill := strings.NewReplacer("{ip}", url.PathEscape(ip))
	s := Score{Source: src.Name, Page: fill.Replace(src.Page)}
	// An API told the IP may go any way, like the geo lookup.
	if strings.Contains(src.URL, "{ip}") {
		proxyMode = "auto"
	}
	proxy, _, err := resolveProxy(proxyMode)
	if err != nil {
		return s, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fill.Replace(src.URL), nil)
	if err != nil {
		return s, err
	}
	req.Header.Set("Accept", "application/json")
	resp, err := newClient(proxy, timeout).Do(req)
	if err != nil {
		return s, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return s, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	var doc any
	if err := json.NewDecoder(io.LimitReader(resp.Body, 64<<10)).Decode(&doc); err != nil {
		return s, err
	}
	return parseScore(s, src, doc)
}

// parseScore takes the source's fields out of its JSON answer.
func parseScore(s Score, src config.ScoreSource, doc any) (Score, error) {
	v, ok := jsonPath(doc, src.Score)
	if !ok {
		return s, fmt.Errorf("没有 %s 字段", src.Score)
	}
	n, err := number(v)
	if err != nil {
		return s, fmt.Errorf("%s 不是分数：%v", src.Score, v)
	}
	s.Score = min(max(int(n+0.5), 0), 100)
	if src.Label != "" {
		if v, ok := jsonPath(doc, src.Label); ok {
			s.Label = text(v)
		}
	}
	if src.Region != "" {
		if v, ok := jsonPath(doc, src.Region); ok {
			switch v := v.(type) {
			case bool:
				s.Region, s.Unsupported = "正常", !v
				if !v {
					s.Region = "不支持"
				}
			default:
				s.Region = text(v)
				s.Unsupported = s.Region != "" && !supported(s.Region)
			}
		}
	}
	return s, nil
}

// jsonPath follows a dotted path, with numbers indexing arrays.
func jsonPath(doc any, path string) (any, bool) {
	if path == "" {
		return nil, false
	}
	for _, k := range strings.Split(path, ".") {
		switch d := doc.(type) {
		case map[string]any:
			v, ok := d[k]
			if !ok {
				return nil, false
			}
			doc = v
		case []any:
			i, err := strconv.Atoi(k)
			if err != nil || i < 0 || i >= len(d) {
				return nil, false
			}
			doc = d[i]
		default:
			return nil, false
		}
	}
	return doc, doc != nil
}

func number(v any) (float64, error) {
	switch v := v.(type) {
	case float64:
		return v, nil
	case string:
		return strconv.ParseFloat(strings.TrimSuffix(strings.TrimSpace(v), "%"), 64)
	}
	return 0, fmt.Errorf("not a number")
}

func text(v any) string {
	if s, ok := v.(string); ok {
		return strings.TrimSpace(s)
	}
	return fmt.Sprint(v)
}

// supported reads a region's text, which says either that the vendor
// serves it or why it does not.
func supported(region string) bool {
	switch strings.ToLower(region) {
	case "正常", "支持", "可用", "ok", "true", "yes", "supported", "available":
		return true
	}
	return false
}
