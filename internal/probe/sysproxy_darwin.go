package probe

import (
	"net/url"
	"os/exec"
	"strings"
	"sync"
	"time"
)

var sysProxyCache struct {
	sync.Mutex
	u       *url.URL
	expires time.Time
}

// systemProxy reads the proxy of System Settings › Network, which GUI apps
// and Electron-based agents follow. PAC files are not evaluated.
func systemProxy() *url.URL {
	sysProxyCache.Lock()
	defer sysProxyCache.Unlock()
	if time.Now().Before(sysProxyCache.expires) {
		return sysProxyCache.u
	}
	sysProxyCache.u = readScutilProxy()
	sysProxyCache.expires = time.Now().Add(10 * time.Second)
	return sysProxyCache.u
}

func readScutilProxy() *url.URL {
	out, err := exec.Command("scutil", "--proxy").Output()
	if err != nil {
		return nil
	}
	kv := map[string]string{}
	for _, line := range strings.Split(string(out), "\n") {
		k, v, ok := strings.Cut(line, " : ")
		if ok {
			kv[strings.TrimSpace(k)] = strings.TrimSpace(v)
		}
	}
	for _, p := range []struct{ prefix, scheme string }{
		{"HTTPS", "http"}, {"HTTP", "http"}, {"SOCKS", "socks5"},
	} {
		if kv[p.prefix+"Enable"] == "1" && kv[p.prefix+"Proxy"] != "" {
			host := kv[p.prefix+"Proxy"]
			if port := kv[p.prefix+"Port"]; port != "" {
				host += ":" + port
			}
			return &url.URL{Scheme: p.scheme, Host: host}
		}
	}
	return nil
}
