package probe

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/10ef/ABeast/internal/config"
)

func TestScoreLookup(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.URL.Path != "/score/23.187.72.246" {
			http.NotFound(w, r)
			return
		}
		w.Write([]byte(`{"data":{"score":"80","level":"纯净","region":{"ok":true}}}`))
	}))
	defer srv.Close()
	src := config.ScoreSource{Name: "Claude", URL: srv.URL + "/score/{ip}", Score: "data.score",
		Label: "data.level", Region: "data.region.ok", Page: "https://ip.net.coffee/claude/?ip={ip}"}
	c := NewScoreCache()
	for range 2 {
		s := c.Lookup(context.Background(), src, "23.187.72.246", "direct", 2*time.Second)
		want := Score{Source: "Claude", Score: 80, Label: "纯净", Region: "正常",
			Page: "https://ip.net.coffee/claude/?ip=23.187.72.246"}
		if s != want {
			t.Fatalf("got %+v, want %+v", s, want)
		}
	}
	if calls != 1 {
		t.Fatalf("an IP's score is cached, got %d calls", calls)
	}
	if s := c.Lookup(context.Background(), src, "1.1.1.1", "direct", 2*time.Second); s.Err == "" {
		t.Fatalf("a failed lookup reports its error, got %+v", s)
	}
}

func TestParseScore(t *testing.T) {
	doc := map[string]any{"list": []any{map[string]any{"risk": 12.6, "area": "不支持的地区"}}}
	s, err := parseScore(Score{}, config.ScoreSource{Score: "list.0.risk", Region: "list.0.area"}, doc)
	if err != nil || s.Score != 13 || !s.Unsupported || s.Region != "不支持的地区" {
		t.Fatalf("got %+v, %v", s, err)
	}
	if _, err := parseScore(Score{}, config.ScoreSource{Score: "missing"}, doc); err == nil {
		t.Fatal("a missing score is an error")
	}
}
