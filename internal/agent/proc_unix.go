//go:build !windows

package agent

import (
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
)

// NormName keeps case: on macOS "claude" is the Claude Code CLI while
// "Claude" is the desktop app.
func NormName(name string) string { return name }

func listProcs() ([]proc, error) {
	out, err := exec.Command("ps", "-axo", "pid=,ppid=,comm=").Output()
	if err != nil {
		return nil, err
	}
	var procs []proc
	for _, line := range strings.Split(string(out), "\n") {
		f := strings.Fields(line)
		if len(f) < 3 {
			continue
		}
		pid, err1 := strconv.Atoi(f[0])
		ppid, err2 := strconv.Atoi(f[1])
		if err1 != nil || err2 != nil {
			continue
		}
		// comm is the executable path, which may contain spaces.
		comm := strings.Join(f[2:], " ")
		procs = append(procs, proc{pid: pid, ppid: ppid, name: NormName(filepath.Base(comm))})
	}
	return procs, nil
}

func suspendProc(pid int) error { return syscall.Kill(pid, syscall.SIGSTOP) }
func resumeProc(pid int) error  { return syscall.Kill(pid, syscall.SIGCONT) }
