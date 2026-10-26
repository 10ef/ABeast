//go:build !darwin && !windows

package probe

import "net/url"

func systemProxy() *url.URL { return nil }
