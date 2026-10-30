//go:build !windows

package envcheck

import (
	"os"
	"os/exec"
	"regexp"
	"runtime"
	"strings"
	"time"
)

func read() Env {
	var e Env
	_, e.Offset = time.Now().Zone()
	// The agents' runtime honors TZ before the system's zone.
	if e.TimeZone = strings.TrimPrefix(os.Getenv("TZ"), ":"); e.TimeZone == "" {
		if target, err := os.Readlink("/etc/localtime"); err == nil {
			e.TimeZone = zoneFromLink(target)
		}
	}
	if runtime.GOOS == "darwin" {
		e.Languages = appleLanguages(defaults("AppleLanguages"))
		e.Locale = strings.TrimSpace(defaults("AppleLocale"))
	}
	langs, loc := envLanguages(os.Getenv)
	if len(e.Languages) == 0 {
		e.Languages = langs
	}
	if e.Locale == "" {
		e.Locale = loc
	}
	return e
}

// defaults reads a global macOS preference.
func defaults(key string) string {
	out, err := exec.Command("/usr/bin/defaults", "read", "-g", key).Output()
	if err != nil {
		return ""
	}
	return string(out)
}

var plistItem = regexp.MustCompile(`"?([A-Za-z0-9_-]+)"?,?`)

// appleLanguages parses `defaults read -g AppleLanguages`:
// ( "zh-Hans-CN", "en-CN" ).
func appleLanguages(out string) []string {
	var langs []string
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if line == "(" || line == ")" || line == "" {
			continue
		}
		if m := plistItem.FindStringSubmatch(line); m != nil {
			langs = append(langs, m[1])
		}
	}
	return langs
}
