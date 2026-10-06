package server

import (
	"net"
	"sync"
	"time"
)

// limiter caps the connections that have not finished login (server wide and per address) and
// the login attempts per address per minute. Loopback addresses only count toward the server
// wide cap.
type limiter struct {
	maxPending, maxPerIP int // negative: no cap
	loginsPerMinute      int // negative: no cap

	mu        sync.Mutex
	pending   int
	ips       map[string]*ipState
	lastSweep time.Time
	now       func() time.Time // tests
}

type ipState struct {
	pending int
	logins  windowCounter
}

func newLimiter(maxPending, maxPerIP, loginsPerMinute int) *limiter {
	return &limiter{maxPending: maxPending, maxPerIP: maxPerIP, loginsPerMinute: loginsPerMinute,
		ips: map[string]*ipState{}, now: time.Now}
}

// acquire counts a new connection from ip, or reports false if it is over a cap.
func (l *limiter) acquire(ip string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.sweep()
	if l.maxPending >= 0 && l.pending >= l.maxPending {
		return false
	}
	st := l.ips[ip]
	if st == nil {
		max := l.loginsPerMinute
		if isLoopback(ip) {
			max = -1
		}
		st = &ipState{logins: windowCounter{max: max, window: time.Minute}}
		l.ips[ip] = st
	}
	if l.maxPerIP >= 0 && st.pending >= l.maxPerIP && !isLoopback(ip) {
		return false
	}
	st.pending++
	l.pending++
	return true
}

// release ends a connection counted by acquire (it finished login or failed).
func (l *limiter) release(ip string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if st := l.ips[ip]; st != nil && st.pending > 0 {
		st.pending--
		l.pending--
	}
}

// loginAttempt counts a login attempt from ip and reports whether it is within the per-minute cap.
func (l *limiter) loginAttempt(ip string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	st := l.ips[ip]
	if st == nil {
		return true // not counted by acquire (cannot happen): let it through
	}
	return st.logins.allowAt(l.now())
}

// sweep forgets addresses with no pending connection and no recent login, at most once a minute,
// so the table does not grow with every address that ever connected. l.mu is held.
func (l *limiter) sweep() {
	now := l.now()
	if now.Sub(l.lastSweep) < time.Minute {
		return
	}
	l.lastSweep = now
	for ip, st := range l.ips {
		if st.pending == 0 && st.logins.idleAt(now) {
			delete(l.ips, ip)
		}
	}
}

// windowCounter allows at most max events in any window (a sliding log of event times; max is
// small). A negative max allows everything.
type windowCounter struct {
	mu     sync.Mutex
	max    int
	window time.Duration
	times  []time.Time
}

func newWindowCounter(max int, window time.Duration) *windowCounter {
	return &windowCounter{max: max, window: window}
}

// allow records an event now if it is within the cap.
func (w *windowCounter) allow() bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.allowAt(time.Now())
}

// allowAt is allow at a given time. w.mu is held (or w is only used under another lock).
func (w *windowCounter) allowAt(now time.Time) bool {
	if w.max < 0 {
		return true
	}
	w.expire(now)
	if len(w.times) >= w.max {
		return false
	}
	w.times = append(w.times, now)
	return true
}

func (w *windowCounter) expire(now time.Time) {
	i := 0
	for i < len(w.times) && now.Sub(w.times[i]) >= w.window {
		i++
	}
	if i > 0 {
		w.times = append(w.times[:0], w.times[i:]...)
	}
}

// idleAt reports whether no event is inside the window.
func (w *windowCounter) idleAt(now time.Time) bool {
	w.expire(now)
	return len(w.times) == 0
}

// isLoopback reports whether ip (an ipKey) is this machine. Per-address limits do not apply to
// it: a proxy or relay on the same host brings every player from there.
func isLoopback(ip string) bool {
	p := net.ParseIP(ip)
	return p != nil && p.IsLoopback()
}

// ipKey is the address the per-address limits count by: the IP, with IPv6 cut to its /64 (one
// host usually has a whole /64 to pick addresses from).
func ipKey(a net.Addr) string {
	var ip net.IP
	switch a := a.(type) {
	case *net.TCPAddr:
		ip = a.IP
	default:
		host, _, err := net.SplitHostPort(a.String())
		if err != nil {
			return a.String()
		}
		ip = net.ParseIP(host)
		if ip == nil {
			return host
		}
	}
	if v4 := ip.To4(); v4 != nil {
		return v4.String()
	}
	if ip.IsLoopback() {
		return ip.String()
	}
	return ip.Mask(net.CIDRMask(64, 128)).String() + "/64"
}
