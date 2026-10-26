//go:build !windows

package clash

import (
	"context"
	"errors"
	"net"
)

func mihomoPipes() []string { return nil }

// dialPipe fails: named pipes are Windows'.
func dialPipe(context.Context, string) (net.Conn, error) {
	return nil, errors.New("named pipes need Windows")
}
