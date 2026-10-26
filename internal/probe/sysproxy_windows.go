package probe

import (
	"net/url"
	"os/exec"
	"strings"
	"syscall"
)

// systemProxy reads the WinINet proxy of Settings › Network › Proxy.
func systemProxy() *url.URL {
	cmd := exec.Command("reg", "query", `HKCU\Software\Microsoft\Windows\CurrentVersion\Internet Settings`)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000} // CREATE_NO_WINDOW
	out, err := cmd.Output()
	if err != nil {
		return nil
	}
	var enabled bool
	var server string
	for _, line := range strings.Split(string(out), "\n") {
		f := strings.Fields(line)
		if len(f) < 3 {
			continue
		}
		switch f[0] {
		case "ProxyEnable":
			enabled = f[2] == "0x1"
		case "ProxyServer":
			server = f[2]
		}
	}
	if !enabled || server == "" {
		return nil
	}
	// "host:port", or per scheme: "http=host:port;https=host:port".
	if strings.Contains(server, "=") {
		for _, part := range strings.Split(server, ";") {
			k, v, _ := strings.Cut(part, "=")
			if k == "https" || k == "http" {
				server = v
				break
			}
		}
	}
	return &url.URL{Scheme: "http", Host: server}
}
