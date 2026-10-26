package main

import (
	"crypto/hmac"
	"crypto/pbkdf2"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	pbkdfIter     = 600_000
	sessionCookie = "fl_admin"
	sessionTTL    = 14 * 24 * time.Hour
)

func hashPassword(pw string) (string, error) {
	salt := []byte(randomToken(16))
	key, err := pbkdf2.Key(sha256.New, pw, salt, pbkdfIter, 32)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("pbkdf2-sha256$%d$%s$%s", pbkdfIter,
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(key)), nil
}

func checkPassword(stored, pw string) bool {
	parts := strings.Split(stored, "$")
	if len(parts) != 4 || parts[0] != "pbkdf2-sha256" {
		return false
	}
	iter, err := strconv.Atoi(parts[1])
	if err != nil || iter < 1 {
		return false
	}
	salt, err1 := base64.RawStdEncoding.DecodeString(parts[2])
	want, err2 := base64.RawStdEncoding.DecodeString(parts[3])
	if err1 != nil || err2 != nil {
		return false
	}
	got, err := pbkdf2.Key(sha256.New, pw, salt, iter, len(want))
	if err != nil {
		return false
	}
	return subtle.ConstantTimeCompare(got, want) == 1
}

func (a *App) sign(msg string) string {
	m := hmac.New(sha256.New, []byte(a.store.Settings().Secret))
	m.Write([]byte(msg))
	return base64.RawURLEncoding.EncodeToString(m.Sum(nil))
}

func (a *App) verify(msg, sig string) bool {
	return hmac.Equal([]byte(a.sign(msg)), []byte(sig))
}

// Session cookie: "<uid>.<expiry>.<pwfingerprint>.<sig>". The fingerprint of
// the user's password hash makes a password change end that user's sessions;
// deleting the user ends them as well.
func (a *App) sessionValue(u *User, exp time.Time) string {
	msg := strconv.Itoa(u.ID) + "." + strconv.FormatInt(exp.Unix(), 10) + "." + u.fingerprint()
	return msg + "." + a.sign("session:"+msg)
}

// fingerprint changes when the password changes or the user logs out
// everywhere, which invalidates all existing session cookies of that user.
func (u *User) fingerprint() string {
	h := sha256.Sum256([]byte(u.PassHash + "|" + strconv.Itoa(u.SessionEpoch)))
	return base64.RawURLEncoding.EncodeToString(h[:8])
}

func (a *App) setSession(w http.ResponseWriter, r *http.Request, u *User) {
	exp := time.Now().Add(sessionTTL)
	http.SetCookie(w, &http.Cookie{
		Name: sessionCookie, Value: a.sessionValue(u, exp), Path: "/",
		Expires: exp, HttpOnly: true, Secure: a.isHTTPS(r), SameSite: http.SameSiteStrictMode,
	})
}

func (a *App) clearSession(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{Name: sessionCookie, Value: "", Path: "/", MaxAge: -1,
		HttpOnly: true, Secure: a.isHTTPS(r), SameSite: http.SameSiteStrictMode})
}

// currentUser returns the logged-in admin, or nil.
func (a *App) currentUser(r *http.Request) *User {
	c, err := r.Cookie(sessionCookie)
	if err != nil {
		return nil
	}
	parts := strings.Split(c.Value, ".")
	if len(parts) != 4 {
		return nil
	}
	msg := parts[0] + "." + parts[1] + "." + parts[2]
	if !a.verify("session:"+msg, parts[3]) {
		return nil
	}
	unix, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil || time.Now().Unix() >= unix {
		return nil
	}
	id, _ := strconv.Atoi(parts[0])
	u := a.store.User(id)
	if u == nil || parts[2] != u.fingerprint() {
		return nil
	}
	return u
}

func (a *App) isAdmin(r *http.Request) bool { return a.currentUser(r) != nil }

func (a *App) isHTTPS(r *http.Request) bool {
	if r.TLS != nil {
		return true
	}
	if a.cfg.TrustProxy && strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https") {
		return true
	}
	return strings.HasPrefix(a.store.Settings().BaseURL, "https://")
}

// sameOrigin protects all state-changing requests against CSRF.
func (a *App) sameOrigin(r *http.Request) bool {
	if site := r.Header.Get("Sec-Fetch-Site"); site != "" {
		return site == "same-origin" || site == "none"
	}
	origin := r.Header.Get("Origin")
	if origin == "" {
		origin = r.Header.Get("Referer")
	}
	if origin == "" {
		return true // older clients without headers; SameSite=Strict still applies
	}
	u, err := url.Parse(origin)
	if err != nil {
		return false
	}
	host := r.Host
	if fh := r.Header.Get("X-Forwarded-Host"); fh != "" && a.cfg.TrustProxy {
		host = fh
	}
	return strings.EqualFold(u.Host, host)
}

// clientKey is only used for in-memory rate limits and never persisted.
func (a *App) clientKey(r *http.Request) string {
	ip := r.RemoteAddr
	if a.cfg.TrustProxy {
		if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
			ip = strings.TrimSpace(strings.Split(xff, ",")[0])
		} else if xr := r.Header.Get("X-Real-IP"); xr != "" {
			ip = xr
		}
	}
	if h, _, err := net.SplitHostPort(ip); err == nil {
		ip = h
	}
	sum := sha256.Sum256([]byte(a.store.Settings().Secret + "|" + ip))
	return base64.RawURLEncoding.EncodeToString(sum[:12])
}

type limiter struct {
	mu     sync.Mutex
	hits   map[string][]time.Time
	limit  int
	window time.Duration
}

func newLimiter(limit int, window time.Duration) *limiter {
	l := &limiter{hits: map[string][]time.Time{}, limit: limit, window: window}
	go func() {
		for range time.Tick(10 * time.Minute) {
			l.mu.Lock()
			cut := time.Now().Add(-l.window)
			for k, v := range l.hits {
				if len(v) == 0 || v[len(v)-1].Before(cut) {
					delete(l.hits, k)
				}
			}
			l.mu.Unlock()
		}
	}()
	return l
}

func (l *limiter) Allow(key string) bool { return l.AllowN(key, l.limit) }

// Check reports whether key is still below the limit, without counting a hit.
func (l *limiter) Check(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	cut := time.Now().Add(-l.window)
	n := 0
	for _, t := range l.hits[key] {
		if t.After(cut) {
			n++
		}
	}
	return n < l.limit
}

// AllowN applies a limit that may change at runtime (from the settings).
func (l *limiter) AllowN(key string, limit int) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := time.Now()
	cut := now.Add(-l.window)
	h := l.hits[key]
	i := 0
	for i < len(h) && h[i].Before(cut) {
		i++
	}
	h = h[i:]
	if len(h) >= limit {
		l.hits[key] = h
		return false
	}
	l.hits[key] = append(h, now)
	return true
}

// Form timestamp against bots: must be signed and at least 3 s old.
func (a *App) formStamp() string {
	ts := strconv.FormatInt(time.Now().Unix(), 10)
	return ts + "." + a.sign("form:"+ts)
}

func (a *App) checkFormStamp(v string) bool {
	parts := strings.Split(v, ".")
	if len(parts) != 2 || !a.verify("form:"+parts[0], parts[1]) {
		return false
	}
	unix, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		return false
	}
	age := time.Since(time.Unix(unix, 0))
	return age >= 3*time.Second && age <= 24*time.Hour
}
