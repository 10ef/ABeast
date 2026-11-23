package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"testing"
)

func TestMigrateProtect(t *testing.T) {
	for data, want := range map[string]bool{
		`{"pin_enabled": true}`:   true,
		`{"watch_enabled": true}`: true,
		`{"block_enabled": true}`: true,
		`{"webrtc_guard": true}`:  false, // it was on by default
		`{}`:                      false,
	} {
		cfg := Default()
		migrate(cfg, []byte(data))
		if cfg.Protect != want {
			t.Errorf("%s: %v", data, cfg.Protect)
		}
	}
}

func TestMigrateClaudeDomains(t *testing.T) {
	claude := func(domains []string) Target {
		return Target{Name: "Claude", Host: "api.anthropic.com", Domains: domains}
	}
	withContent := func(domains []string) []string {
		return append(slices.Clone(domains), "claudeusercontent.com")
	}
	legacy := []string{"anthropic.com", "claude.ai", "claude.com"}
	reordered := []string{"claude.com", "anthropic.com", "claude.ai"}
	duplicates := []string{"anthropic.com", "claude.ai", "claude.com", "claude.com"}
	custom := []string{"anthropic.com", "claude.ai", "claude.com", "example.org"}
	for _, tc := range []struct {
		name    string
		target  Target
		domains []string
	}{
		{"legacy", claude(legacy), withContent(legacy)},
		{"reordered", claude(reordered), withContent(reordered)},
		{"duplicate", claude(duplicates), withContent(duplicates)},
		{"custom domains", claude(custom), custom},
		{"partial domains", claude(legacy[:2]), legacy[:2]},
		{"custom host", Target{Name: "Claude", Host: "example.org", Domains: legacy}, legacy},
		{"custom name", Target{Name: "Other", Host: "api.anthropic.com", Domains: legacy}, legacy},
	} {
		t.Run(tc.name, func(t *testing.T) {
			data, err := json.Marshal(Config{Targets: []Target{tc.target}})
			if err != nil {
				t.Fatal(err)
			}
			for load := range 2 {
				cfg := Default()
				if err := json.Unmarshal(data, cfg); err != nil {
					t.Fatal(err)
				}
				migrate(cfg, data)
				if got := cfg.Targets[0].Domains; !slices.Equal(got, tc.domains) {
					t.Fatalf("load %d: domains = %v, want %v", load+1, got, tc.domains)
				}
				data, err = json.Marshal(cfg)
				if err != nil {
					t.Fatal(err)
				}
			}
		})
	}
}

func TestClaudeDomainsMatchClashOverride(t *testing.T) {
	source, err := os.ReadFile(filepath.Join("..", "..", "clash", "exitguard-override.js"))
	if err != nil {
		t.Fatal(err)
	}
	match := regexp.MustCompile(`\bid:\s*"anthropic",[^}]*\bdomains:\s*(\[[^\]]*\])`).FindSubmatch(source)
	if match == nil {
		t.Fatal("Anthropic domain list is missing from the Clash override")
	}
	var overrideDomains []string
	if err := json.Unmarshal(match[1], &overrideDomains); err != nil {
		t.Fatal(err)
	}
	for _, target := range Default().Targets {
		if target.Name == "Claude" {
			if !slices.Equal(overrideDomains, target.Domains) {
				t.Errorf("Clash override domains = %v, want default Claude domains %v", overrideDomains, target.Domains)
			}
			return
		}
	}
	t.Fatal("default Claude target is missing")
}
