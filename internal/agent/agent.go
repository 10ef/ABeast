package agent

import (
	"log"
	"os"
	"sort"
	"sync"
)

type proc struct {
	pid, ppid int
	name      string // normalized with normName
}

// Guard pauses agent processes while their route is unsafe, so they send no
// further requests through the wrong exit, and resumes them afterwards.
type Guard struct {
	mu     sync.Mutex
	paused map[int]string // pid → name
	closed bool
}

// New returns a guard that has paused nothing.
func New() *Guard { return &Guard{paused: map[int]string{}} }

// Report is what Apply found and did.
type Report struct {
	Running map[string]int `json:"running"` // watched name → process count
	Paused  map[string]int `json:"paused"`
	Errors  []string       `json:"errors,omitempty"`
}

// Apply makes the paused set match: processes named in pause are paused when
// enabled, every other process it paused is resumed. watched names are only
// counted. ExitGuard's own process and its ancestors are never touched, so a
// guard started from an agent's terminal does not freeze that agent.
func (g *Guard) Apply(watched []string, pause map[string]bool, enabled bool) Report {
	g.mu.Lock()
	defer g.mu.Unlock()
	rep := Report{Running: map[string]int{}, Paused: map[string]int{}}
	if g.closed {
		return rep
	}
	procs, err := listProcs()
	if err != nil {
		rep.Errors = append(rep.Errors, "列出进程失败: "+err.Error())
		return rep
	}
	want := map[string]bool{}
	for _, n := range watched {
		want[NormName(n)] = true
	}
	stop := map[string]bool{}
	for n, v := range pause {
		if v {
			stop[NormName(n)] = true
		}
	}
	protected := ancestors(procs, os.Getpid())
	alive := map[int]string{}
	for _, p := range procs {
		if !want[p.name] || protected[p.pid] {
			continue
		}
		alive[p.pid] = p.name
		rep.Running[p.name]++
		if !enabled || !stop[p.name] {
			continue
		}
		if _, done := g.paused[p.pid]; !done {
			if err := suspendProc(p.pid); err != nil {
				rep.Errors = append(rep.Errors, p.name+": "+err.Error())
				continue
			}
			log.Printf("guard: paused %s (%d)", p.name, p.pid)
			g.paused[p.pid] = p.name
		}
	}
	for pid, name := range g.paused {
		if alive[pid] != name {
			delete(g.paused, pid) // exited, or the PID was reused
			continue
		}
		if !enabled || !stop[name] {
			if err := resumeProc(pid); err != nil {
				rep.Errors = append(rep.Errors, name+": "+err.Error())
			}
			log.Printf("guard: resumed %s (%d)", name, pid)
			delete(g.paused, pid)
		}
	}
	for _, name := range g.paused {
		rep.Paused[name]++
	}
	return rep
}

// Close resumes every paused process and stops pausing, before quitting.
func (g *Guard) Close() {
	g.mu.Lock()
	g.closed = true
	g.mu.Unlock()
	g.ResumeAll()
}

// ResumeAll resumes every process the guard paused.
func (g *Guard) ResumeAll() {
	g.mu.Lock()
	defer g.mu.Unlock()
	for pid, name := range g.paused {
		_ = resumeProc(pid)
		log.Printf("guard: resumed %s (%d)", name, pid)
		delete(g.paused, pid)
	}
}

func ancestors(procs []proc, pid int) map[int]bool {
	parent := map[int]int{}
	for _, p := range procs {
		parent[p.pid] = p.ppid
	}
	seen := map[int]bool{}
	for pid > 1 && !seen[pid] {
		seen[pid] = true
		pid = parent[pid]
	}
	return seen
}

func sortedKeys(m map[string]int) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
