package httpapi

import (
	"net"
	"net/http"
	"sync"

	"golang.org/x/time/rate"
)

// Per-IP token bucket. Generous for a human clicking around, tight enough to
// blunt a scripted flood — this is MVP abuse protection, not DDoS defense.
const (
	rateLimitPerSecond = 10
	rateLimitBurst     = 30
	// Crude memory bound: when the map accumulates this many IPs, it resets.
	// Resetting refills everyone's bucket, which is acceptable at this scale.
	rateLimitMaxIPs = 10_000
)

type ipRateLimiter struct {
	mu      sync.Mutex
	perIP   map[string]*rate.Limiter
	limit   rate.Limit
	burst   int
	maxSize int
}

func newIPRateLimiter() *ipRateLimiter {
	return &ipRateLimiter{
		perIP:   map[string]*rate.Limiter{},
		limit:   rateLimitPerSecond,
		burst:   rateLimitBurst,
		maxSize: rateLimitMaxIPs,
	}
}

func (rl *ipRateLimiter) allow(ip string) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	if len(rl.perIP) >= rl.maxSize {
		rl.perIP = map[string]*rate.Limiter{}
	}
	limiter, ok := rl.perIP[ip]
	if !ok {
		limiter = rate.NewLimiter(rl.limit, rl.burst)
		rl.perIP[ip] = limiter
	}
	return limiter.Allow()
}

// withRateLimit rejects clients that exceed the per-IP budget. /healthz is
// exempt so liveness probes never trip it.
func withRateLimit(rl *ipRateLimiter, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/healthz" {
			next.ServeHTTP(w, r)
			return
		}
		ip, _, err := net.SplitHostPort(r.RemoteAddr)
		if err != nil {
			ip = r.RemoteAddr
		}
		if !rl.allow(ip) {
			w.Header().Set("Retry-After", "1")
			writeError(w, http.StatusTooManyRequests, "too many requests — slow down")
			return
		}
		next.ServeHTTP(w, r)
	})
}
