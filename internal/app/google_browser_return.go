package app

import (
	"net/url"
	"strings"
)

// googleReturnAllowed limits navigation to explicitly trusted frontend sign-in pages.
func googleReturnAllowed(c Config, raw string) bool {
	u, err := url.Parse(raw)
	return err == nil && len(raw) <= 2048 && u.Scheme == "https" && u.User == nil && u.RawQuery == "" && u.Fragment == "" && u.RawPath == "" && strings.HasSuffix(u.Path, "/auth.html") && originAllowed(c, u.Scheme+"://"+u.Host)
}
