// Package envcheck estimates how China-like this computer's settings look to
// the agents, after fuck-claude.com's browser check: the system timezone the
// agents read, its UTC offset, the preferred languages and the locale. The
// browser-only signals of that check (fonts, browser, device, emoji) have no
// counterpart here, so the score is out of the weights that remain.
package envcheck

import (
	"fmt"
	"math"
	"slices"
	"strings"
	"time"
)

// Env is what the system says about where its user is.
type Env struct {
	TimeZone  string   // IANA name, or a Windows time zone key
	Offset    int      // seconds east of UTC, now
	Languages []string // preferred, first first, e.g. zh-Hans-CN
	Locale    string   // region format, e.g. zh-CN
}

// Signal is one check and how China-like it found the system.
type Signal struct {
	ID     string  `json:"id"`
	Name   string  `json:"name"`
	Weight int     `json:"weight"`
	Raw    string  `json:"raw"`
	Score  float64 `json:"score"` // 0 … 1
	Points int     `json:"points"`
}

// Hit is a signal that counts, as on the site: a score of 0.25 or more.
func (s Signal) Hit() bool { return s.Score >= 0.25 }

// Report is a check's score: 0 … 100, higher looking more China-like.
type Report struct {
	Signals []Signal  `json:"signals"`
	Total   int       `json:"total"`
	Band    string    `json:"band"` // low, medium or high
	Checked time.Time `json:"checked"`
}

// BandName is the band in words.
func (r Report) BandName() string {
	return map[string]string{"low": "低风险", "medium": "中等风险", "high": "高风险"}[r.Band]
}

// Check reads the system's settings and scores them.
func Check() Report { return Score(read()) }

var (
	chinaZones  = []string{"Asia/Shanghai", "Asia/Urumqi", "Asia/Chongqing", "Asia/Chungking", "Asia/Harbin", "Asia/Kashgar", "PRC"}
	nearbyZones = []string{"Asia/Hong_Kong", "Asia/Macau", "Asia/Taipei", "Hongkong", "ROC"}
	// Windows names zones by their keys. China Standard Time covers Hong
	// Kong and Macau too, so it scores as the mainland does.
	windowsZones = map[string]float64{"China Standard Time": 1, "Taipei Standard Time": 0.6}
)

// Score weighs an Env's signals the way the site does.
func Score(e Env) Report {
	r := Report{Checked: time.Now(), Signals: []Signal{
		timeZone(e.TimeZone),
		languages(e.Languages),
		locale(e.Locale),
		offset(e.Offset),
	}}
	points, weight := 0, 0
	for i := range r.Signals {
		s := &r.Signals[i]
		s.Points = int(math.Round(s.Score * float64(s.Weight)))
		points += s.Points
		weight += s.Weight
	}
	r.Total = min(int(math.Round(float64(points)*100/float64(weight))), 100)
	switch {
	case r.Total <= 30:
		r.Band = "low"
	case r.Total <= 60:
		r.Band = "medium"
	default:
		r.Band = "high"
	}
	return r
}

func timeZone(tz string) Signal {
	s := Signal{ID: "timezone", Name: "系统时区", Weight: 26, Raw: tz}
	switch {
	case slices.Contains(chinaZones, tz):
		s.Score = 1
	case slices.Contains(nearbyZones, tz):
		s.Score = 0.6
	default:
		s.Score = windowsZones[tz]
	}
	if tz == "" {
		s.Raw = "未知"
	}
	return s
}

func offset(sec int) Signal {
	s := Signal{ID: "offset", Name: "时区偏移", Weight: 7}
	h := float64(sec) / 3600
	s.Raw = fmt.Sprintf("UTC%+g", h)
	if sec == 8*3600 {
		s.Score = 0.7
	}
	return s
}

func simplified(l string) bool {
	return strings.HasPrefix(l, "zh-cn") || strings.Contains(l, "hans") || l == "zh"
}

func traditional(l string) bool {
	return strings.HasPrefix(l, "zh-tw") || strings.HasPrefix(l, "zh-hk") ||
		strings.HasPrefix(l, "zh-mo") || strings.Contains(l, "hant")
}

func languages(langs []string) Signal {
	s := Signal{ID: "language", Name: "系统语言", Weight: 20, Raw: strings.Join(langs, ", ")}
	if s.Raw == "" {
		s.Raw = "未知"
	}
	norm := make([]string, len(langs))
	for i, l := range langs {
		norm[i] = normTag(l)
	}
	first := ""
	if len(norm) > 0 {
		first = norm[0]
	}
	switch {
	case simplified(first):
		s.Score = 1
	case traditional(first):
		s.Score = 0.5
	case slices.ContainsFunc(norm, simplified):
		s.Score = 0.7
	case slices.ContainsFunc(norm, func(l string) bool { return strings.HasPrefix(l, "zh") }):
		s.Score = 0.4
	}
	return s
}

func locale(loc string) Signal {
	s := Signal{ID: "locale", Name: "区域格式", Weight: 9, Raw: loc}
	l := normTag(loc)
	switch {
	case simplified(l):
		s.Score = 1
	case strings.HasPrefix(l, "zh"):
		s.Score = 0.5
	}
	if loc == "" {
		s.Raw = "未知"
	}
	return s
}

// normTag lowercases a language tag and writes zh_CN.UTF-8 as zh-cn.
func normTag(t string) string {
	t, _, _ = strings.Cut(t, ".")
	t, _, _ = strings.Cut(t, "@")
	return strings.ToLower(strings.ReplaceAll(strings.TrimSpace(t), "_", "-"))
}

// envLanguages are the languages the POSIX locale variables name.
func envLanguages(getenv func(string) string) (langs []string, loc string) {
	for _, l := range strings.Split(getenv("LANGUAGE"), ":") {
		if l != "" {
			langs = append(langs, l)
		}
	}
	for _, k := range []string{"LC_ALL", "LC_MESSAGES", "LANG"} {
		if v := getenv(k); v != "" && v != "C" && v != "POSIX" {
			if len(langs) == 0 {
				langs = append(langs, v)
			}
			break
		}
	}
	for _, k := range []string{"LC_ALL", "LC_TIME", "LANG"} {
		if v := getenv(k); v != "" && v != "C" && v != "POSIX" {
			return langs, v
		}
	}
	return langs, ""
}

// zoneFromLink takes the IANA name out of /etc/localtime's target, e.g.
// /var/db/timezone/zoneinfo/Asia/Shanghai.
func zoneFromLink(target string) string {
	if _, name, ok := strings.Cut(target, "zoneinfo/"); ok {
		return name
	}
	return ""
}
