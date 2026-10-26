package config

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

// Target is one route an AI agent takes to its API. The host must be behind
// Cloudflare so that https://<host>/cdn-cgi/trace reports the exit IP and
// country the proxy's rules pick for that very host.
type Target struct {
	Name string `json:"name"`
	Host string `json:"host"`
	// Proxy is how the probe connects, matching how the agent connects:
	// "auto" (HTTPS_PROXY if set, else the system proxy, else direct),
	// "env", "system", "direct", or a proxy URL such as
	// "http://127.0.0.1:7890" or "socks5://127.0.0.1:7890".
	Proxy string `json:"proxy"`
	// Processes are the agents' executable names: protection mode pauses
	// them while the proxy cannot keep their traffic in.
	Processes []string `json:"processes"`
	// Domains are the vendor's domains, with their subdomains, whose
	// connections the watcher checks. Empty is the host's last two labels.
	Domains []string `json:"domains,omitempty"`
	// BlockGroup is the Mihomo Selector group the host's rule picks; it is
	// switched to REJECT while the route is unsafe.
	BlockGroup string `json:"block_group,omitempty"`
	// ProbeWhileBlocked is a local proxy that reaches the group's upstream
	// without the REJECT, to probe the exit the route would have.
	ProbeWhileBlocked string `json:"probe_while_blocked,omitempty"`
}

// Mihomo points at a Clash API controller: Mihomo (Clash Meta) and the
// clients that run it, such as FlClash, Clash Verge Rev and mihomo-party, or
// sing-box with clash_api.
type Mihomo struct {
	// Controller is "host:port", a Windows named pipe (\\.\pipe\name) or
	// a Unix socket (unix:///path). Empty finds it: in the configs of known
	// clients, then on the default ports.
	Controller string `json:"controller"`
	Secret     string `json:"secret"`
}

// Config is the settings in config.json.
type Config struct {
	IntervalSeconds int `json:"interval_seconds"`
	TimeoutSeconds  int `json:"timeout_seconds"`
	// FailThreshold consecutive failed probes make a route unsafe.
	FailThreshold int `json:"fail_threshold"`
	// RecoverThreshold consecutive clean probes make it safe again.
	RecoverThreshold int `json:"recover_threshold"`

	// AllowedCountries, when not empty, is the only exits accepted.
	AllowedCountries []string `json:"allowed_countries"`
	// BlockedCountries are exits that are always unsafe, e.g. the home
	// country, which means the VPN is off or a rule sent the host DIRECT.
	BlockedCountries []string `json:"blocked_countries"`

	GuardOnCountryChange bool `json:"guard_on_country_change"`
	GuardOnIPChange      bool `json:"guard_on_ip_change"`
	// Protect is the menu's protection mode: it holds each route on the node
	// it leaves through, so a node going down fails its requests instead of
	// moving them; closes AI connections that leave through DIRECT or a node
	// a probe saw exit where it must not; and blocks unsafe routes where the
	// override script set up their groups.
	Protect bool   `json:"protect"`
	Mihomo  Mihomo `json:"mihomo"`

	// ShowDirect also probes without any proxy, to show where traffic of
	// programs that ignore the proxy settings exits.
	ShowDirect bool `json:"show_direct"`
	// TrayTitle is the text beside the menu bar icon (macOS), once per
	// distinct exit: {flag} {code} {country} {region} {city} {ip} {isp}
	// {ms}. Empty shows the icon alone.
	TrayTitle string   `json:"tray_title"`
	Targets   []Target `json:"targets"`
}

// Default is the config of a first run.
func Default() *Config {
	return &Config{
		IntervalSeconds:      30,
		TimeoutSeconds:       8,
		FailThreshold:        3,
		RecoverThreshold:     2,
		BlockedCountries:     []string{"CN", "HK", "MO", "RU", "BY", "IR", "KP", "CU", "SY"},
		GuardOnCountryChange: true,
		GuardOnIPChange:      false,
		ShowDirect:           true,
		TrayTitle:            "{flag} {code}",
		Targets: []Target{
			{Name: "Claude", Host: "api.anthropic.com", Proxy: "auto", Processes: []string{"claude"},
				Domains:    []string{"anthropic.com", "claude.ai", "claude.com"},
				BlockGroup: "🛡 EG-Anthropic", ProbeWhileBlocked: "http://127.0.0.1:17892"},
			{Name: "Codex", Host: "chatgpt.com", Proxy: "auto", Processes: []string{"codex"},
				Domains:    []string{"openai.com", "chatgpt.com", "oaistatic.com", "oaiusercontent.com"},
				BlockGroup: "🛡 EG-OpenAI", ProbeWhileBlocked: "http://127.0.0.1:17891"},
		},
	}
}

// Acceptable reports whether an exit country is allowed.
func (c *Config) Acceptable(cc string) bool {
	if slices.Contains(c.BlockedCountries, cc) {
		return false
	}
	return len(c.AllowedCountries) == 0 || slices.Contains(c.AllowedCountries, cc)
}

// DomainSuffixes are the vendor domains a connection's host is checked against.
func (t Target) DomainSuffixes() []string {
	if len(t.Domains) > 0 {
		return t.Domains
	}
	labels := strings.Split(t.Host, ".")
	return []string{strings.Join(labels[max(len(labels)-2, 0):], ".")}
}

// Agents are the executables of all targets' agents.
func (c *Config) Agents() []string {
	var names []string
	for _, t := range c.Targets {
		for _, p := range t.Processes {
			if !slices.Contains(names, p) {
				names = append(names, p)
			}
		}
	}
	return names
}

// Interval is the time between probe rounds.
func (c *Config) Interval() time.Duration {
	return time.Duration(max(c.IntervalSeconds, 5)) * time.Second
}

// Timeout bounds one probe.
func (c *Config) Timeout() time.Duration {
	return time.Duration(max(c.TimeoutSeconds, 2)) * time.Second
}

// Dir is where the config, status and restoration records live. It keeps
// the ExitGuard name of earlier versions.
func Dir() string {
	dir, err := os.UserConfigDir()
	if err != nil {
		dir = os.TempDir()
	}
	return filepath.Join(dir, "ExitGuard")
}

// Path is the config file.
func Path() string { return filepath.Join(Dir(), "config.json") }

// Load reads the config file, writing the defaults on first run.
func Load() (*Config, error) {
	cfg := Default()
	data, err := os.ReadFile(Path())
	if errors.Is(err, os.ErrNotExist) {
		return cfg, Save(cfg)
	}
	if err != nil {
		return cfg, err
	}
	if err := json.Unmarshal(data, cfg); err != nil {
		return Default(), err
	}
	migrate(cfg, data)
	// Targets saved before a setting existed get its default.
	for i, t := range cfg.Targets {
		for _, d := range Default().Targets {
			if t.Name == d.Name && t.BlockGroup == "" && t.ProbeWhileBlocked == "" {
				cfg.Targets[i].BlockGroup, cfg.Targets[i].ProbeWhileBlocked = d.BlockGroup, d.ProbeWhileBlocked
			}
			if t.Name == d.Name && len(t.Domains) == 0 {
				cfg.Targets[i].Domains = d.Domains
			}
		}
	}
	cfg.FailThreshold = max(cfg.FailThreshold, 1)
	cfg.RecoverThreshold = max(cfg.RecoverThreshold, 1)
	// Write back, so settings added since show up in the file to edit.
	return cfg, Save(cfg)
}

// migrate carries settings of older versions over: pin_enabled,
// watch_enabled and block_enabled, before protection mode joined them.
func migrate(cfg *Config, data []byte) {
	var old struct {
		Pin   bool `json:"pin_enabled"`
		Watch bool `json:"watch_enabled"`
		Block bool `json:"block_enabled"`
	}
	if json.Unmarshal(data, &old) == nil && (old.Pin || old.Watch || old.Block) {
		cfg.Protect = true
	}
}

// Save writes the config file.
func Save(cfg *Config) error {
	if err := os.MkdirAll(Dir(), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(Path(), append(data, '\n'), 0o644)
}
