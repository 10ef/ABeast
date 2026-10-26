package config

import "testing"

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
