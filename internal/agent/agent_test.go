//go:build !windows

package agent

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// TestMain lets a copy of the test binary stand in for an agent process.
func TestMain(m *testing.M) {
	if os.Getenv("EXITGUARD_FAKE_AGENT") == "1" {
		time.Sleep(30 * time.Second)
		os.Exit(0)
	}
	os.Exit(m.Run())
}

// TestGuardPausesAndResumes pauses a copy of the test binary named
// fakeagent, never a real agent.
func TestGuardPausesAndResumes(t *testing.T) {
	bin := filepath.Join(t.TempDir(), "fakeagent")
	self, err := os.Executable()
	if err != nil {
		t.Skip(err)
	}
	data, err := os.ReadFile(self)
	if err != nil {
		t.Skip(err)
	}
	if err := os.WriteFile(bin, data, 0o755); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(bin)
	cmd.Env = append(os.Environ(), "EXITGUARD_FAKE_AGENT=1")
	if err := cmd.Start(); err != nil {
		t.Skip(err)
	}
	defer cmd.Process.Kill()
	state := func() string {
		out, _ := exec.Command("ps", "-o", "stat=", "-p", strconv.Itoa(cmd.Process.Pid)).Output()
		return strings.TrimSpace(string(out))
	}
	g := New()
	watched := []string{"fakeagent"}

	rep := g.Apply(watched, map[string]bool{"fakeagent": true}, false)
	if rep.Running["fakeagent"] != 1 || strings.HasPrefix(state(), "T") {
		t.Fatalf("disabled guard must only count: %+v %s", rep, state())
	}
	rep = g.Apply(watched, map[string]bool{"fakeagent": true}, true)
	if rep.Paused["fakeagent"] != 1 || !strings.HasPrefix(state(), "T") {
		t.Fatalf("not paused: %+v %s", rep, state())
	}
	rep = g.Apply(watched, map[string]bool{}, true)
	if rep.Paused["fakeagent"] != 0 || strings.HasPrefix(state(), "T") {
		t.Fatalf("not resumed: %+v %s", rep, state())
	}
	g.Apply(watched, map[string]bool{"fakeagent": true}, true)
	g.Close()
	if strings.HasPrefix(state(), "T") {
		t.Fatal("Close must resume")
	}
	if rep := g.Apply(watched, map[string]bool{"fakeagent": true}, true); len(rep.Paused) != 0 {
		t.Fatal("a closed guard must not pause")
	}
}
