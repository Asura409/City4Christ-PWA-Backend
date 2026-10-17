package app

import (
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"time"

	"github.com/City4Christ/City4Christ-PWA-Backend/internal/auth"
)

func validOrigin(raw string) bool {
	u, err := url.Parse(raw)
	return err == nil && u.Scheme == "https" && u.Host != "" && u.User == nil && u.Path == "" && u.RawQuery == "" && u.Fragment == ""
}
func originAllowed(c Config, origin string) bool {
	if !validOrigin(origin) {
		return false
	}
	if origin == c.AppOrigin {
		return true
	}
	for _, allowed := range c.AllowedOrigins {
		if origin == allowed {
			return true
		}
	}
	return false
}
func accountLimit(w http.ResponseWriter, r *http.Request, limiter *auth.RateLimiter, scope, email string, limit int, window time.Duration) bool {
	if limiter == nil {
		return true
	}
	allowed, err := limiter.Allow(r.Context(), "account/"+scope, strings.ToLower(strings.TrimSpace(email)), limit, window)
	return limitResult(w, allowed, err)
}
func limitResult(w http.ResponseWriter, allowed bool, err error) bool {
	if err != nil {
		writeError(w, 503, "unavailable", "Service unavailable.", nil)
		return false
	}
	if !allowed {
		w.Header().Set("Retry-After", "60")
		writeError(w, 429, "rate_limited", "Too many attempts. Try again later.", nil)
		return false
	}
	return true
}
func clientIP(r *http.Request, c Config) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	addr, err := netip.ParseAddr(host)
	if err != nil {
		return "unknown"
	}
	addr = addr.Unmap()
	trusted := func(ip netip.Addr) bool {
		for _, raw := range c.TrustedProxyCIDRs {
			prefix, err := netip.ParsePrefix(raw)
			if err == nil && prefix.Contains(ip) {
				return true
			}
		}
		return false
	}
	if trusted(addr) {
		parts := strings.Split(r.Header.Get("X-Forwarded-For"), ",")
		for i := len(parts) - 1; i >= 0; i-- {
			next, err := netip.ParseAddr(strings.TrimSpace(parts[i]))
			if err != nil {
				return addr.String()
			}
			addr = next.Unmap()
			if !trusted(addr) {
				break
			}
		}
	}
	return addr.String()
}
func browserPolicy(next http.Handler, c Config, limiter *auth.RateLimiter) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Add("Vary", "Origin")
		if strings.HasPrefix(r.URL.Path, "/api/") {
			w.Header().Set("Cache-Control", "no-store")
		}
		origin := r.Header.Get("Origin")
		if origin != "" && validOrigin(c.AppOrigin) {
			if !originAllowed(c, origin) {
				writeError(w, 403, "origin_not_allowed", "Origin is not allowed.", nil)
				return
			}
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Access-Control-Allow-Credentials", "true")
		}
		if r.Method == "OPTIONS" && strings.HasPrefix(r.URL.Path, "/api/") {
			if !originAllowed(c, origin) {
				writeError(w, 403, "origin_not_allowed", "Origin is not allowed.", nil)
				return
			}
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PATCH, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type")
			w.Header().Set("Access-Control-Max-Age", "600")
			w.WriteHeader(204)
			return
		}
		if (r.Method == "POST" && strings.HasPrefix(r.URL.Path, "/api/v1/auth/")) || strings.HasPrefix(r.URL.Path, "/api/v1/auth/google/") {
			if r.Method == "POST" && validOrigin(c.AppOrigin) && !originAllowed(c, origin) {
				writeError(w, 403, "origin_not_allowed", "Supply an allowed Origin header.", nil)
				return
			}
			if limiter != nil {
				allowed, err := limiter.Allow(r.Context(), "ip/auth", clientIP(r, c), 60, time.Minute)
				if !limitResult(w, allowed, err) {
					return
				}
			}
		}
		next.ServeHTTP(w, r)
	})
}
