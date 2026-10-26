package clash

import (
	"context"
	"errors"
	"net"
	"os"
	"strings"
	"syscall"
	"time"
)

const errorPipeBusy = syscall.Errno(231)

// dialPipe opens a named pipe, waiting while all its instances are busy.
// Opened for overlapped I/O, the file goes through the runtime's poller, so
// deadlines work and closing it ends a pending read.
func dialPipe(ctx context.Context, path string) (net.Conn, error) {
	name, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return nil, err
	}
	for {
		h, err := syscall.CreateFile(name, syscall.GENERIC_READ|syscall.GENERIC_WRITE, 0, nil,
			syscall.OPEN_EXISTING, syscall.FILE_FLAG_OVERLAPPED, 0)
		if err == nil {
			return pipeConn{os.NewFile(uintptr(h), path)}, nil
		}
		if !errors.Is(err, errorPipeBusy) {
			return nil, &os.PathError{Op: "open", Path: path, Err: err}
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(50 * time.Millisecond):
		}
	}
}

// mihomoPipes lists the named pipes with mihomo in their name, such as
// Clash Verge Rev's verge-mihomo-production-<hash>.
func mihomoPipes() []string {
	entries, err := os.ReadDir(`\\.\pipe\`)
	if err != nil {
		return nil
	}
	var pipes []string
	for _, e := range entries {
		if strings.Contains(strings.ToLower(e.Name()), "mihomo") {
			pipes = append(pipes, `\\.\pipe\`+e.Name())
		}
	}
	return pipes
}

// pipeConn is a named pipe as the net.Conn an HTTP client wants.
type pipeConn struct{ *os.File }

func (pipeConn) LocalAddr() net.Addr  { return pipeAddr{} }
func (pipeConn) RemoteAddr() net.Addr { return pipeAddr{} }

type pipeAddr struct{}

func (pipeAddr) Network() string { return "pipe" }
func (pipeAddr) String() string  { return "pipe" }
