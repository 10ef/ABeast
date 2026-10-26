package clash

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"testing"

	"github.com/10ef/ABeast/internal/config"
)

// fakeMode is a controller that answers /version and /configs to the secret
// s3cret.
func fakeMode(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("Authorization") != "Bearer s3cret" {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	switch r.URL.Path {
	case "/version":
		json.NewEncoder(w).Encode(map[string]any{"meta": true, "version": "test"})
	case "/configs":
		json.NewEncoder(w).Encode(map[string]string{"mode": "rule"})
	default:
		w.WriteHeader(http.StatusNotFound)
	}
}

func TestReadController(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	os.WriteFile(path, []byte("mixed-port: 7890\nexternal-controller: ':9097'\nsecret: \"abc\"\ndns:\n  secret: nested\n"), 0o644)
	got := readController(path)
	if len(got) != 1 || got[0] != (config.Mihomo{Controller: "127.0.0.1:9097", Secret: "abc"}) {
		t.Fatalf("%+v", got)
	}
}

// Clash Verge Rev's runtime config: no TCP controller, a pipe and a socket.
func TestReadControllerSockets(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "clash-verge.yaml")
	os.WriteFile(path, []byte("external-controller: ''\nsecret: set-your-secret\n"+
		"external-controller-pipe: \\\\.\\pipe\\verge-mihomo-sidecar-release-1b10\n"+
		"external-controller-unix: verge.sock\n"), 0o644)
	want := []config.Mihomo{{Controller: "unix://" + filepath.Join(dir, "verge.sock"), Secret: "set-your-secret"}}
	if runtime.GOOS == "windows" {
		want = append([]config.Mihomo{{Controller: `\\.\pipe\verge-mihomo-sidecar-release-1b10`, Secret: "set-your-secret"}}, want...)
	}
	if got := readController(path); !slices.Equal(got, want) {
		t.Fatalf("%+v", got)
	}
}

func TestMihomoOverUnixSocket(t *testing.T) {
	dir, err := os.MkdirTemp("", "eg") // short: socket paths have a length limit
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	sock := filepath.Join(dir, "c.sock")
	l, err := net.Listen("unix", sock)
	if err != nil {
		t.Skip("no unix sockets:", err)
	}
	srv := httptest.NewUnstartedServer(http.HandlerFunc(fakeMode))
	srv.Listener = l
	srv.Start()
	defer srv.Close()

	m := New(config.Mihomo{Controller: "unix://" + sock, Secret: "s3cret"})
	if mode, err := m.Mode(context.Background()); err != nil || mode != "rule" {
		t.Fatalf("%q %v", mode, err)
	}
	if m.Addr() != "unix://"+sock {
		t.Fatal(m.Addr())
	}
}
