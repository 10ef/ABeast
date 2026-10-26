package agent

import (
	"fmt"
	"strings"
	"syscall"
	"unsafe"
)

var (
	ntdll                = syscall.NewLazyDLL("ntdll.dll")
	procNtSuspendProcess = ntdll.NewProc("NtSuspendProcess")
	procNtResumeProcess  = ntdll.NewProc("NtResumeProcess")
)

// NormName ignores case and the .exe suffix, as Windows does.
func NormName(name string) string {
	return strings.TrimSuffix(strings.ToLower(name), ".exe")
}

func listProcs() ([]proc, error) {
	snap, err := syscall.CreateToolhelp32Snapshot(syscall.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return nil, err
	}
	defer syscall.CloseHandle(snap)
	var e syscall.ProcessEntry32
	e.Size = uint32(unsafe.Sizeof(e))
	var procs []proc
	for err = syscall.Process32First(snap, &e); err == nil; err = syscall.Process32Next(snap, &e) {
		procs = append(procs, proc{
			pid:  int(e.ProcessID),
			ppid: int(e.ParentProcessID),
			name: NormName(syscall.UTF16ToString(e.ExeFile[:])),
		})
	}
	return procs, nil
}

func callOnProcess(pid int, fn *syscall.LazyProc) error {
	const processSuspendResume = 0x0800
	h, err := syscall.OpenProcess(processSuspendResume, false, uint32(pid))
	if err != nil {
		return err
	}
	defer syscall.CloseHandle(h)
	if status, _, _ := fn.Call(uintptr(h)); status != 0 {
		return fmt.Errorf("NTSTATUS 0x%x", status)
	}
	return nil
}

func suspendProc(pid int) error { return callOnProcess(pid, procNtSuspendProcess) }
func resumeProc(pid int) error  { return callOnProcess(pid, procNtResumeProcess) }
