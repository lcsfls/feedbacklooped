package main

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"math/big"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Proof-of-work captcha, wire-compatible with ALTCHA (MIT, altcha.org): the
// server picks a random number n and sends sha256(salt+n) with an HMAC
// signature; the browser searches 0..maxnumber for n. No cookies, no third
// party, nothing about the visitor is stored.

const challengeTTL = 20 * time.Minute

type challenge struct {
	Algorithm string `json:"algorithm"`
	Challenge string `json:"challenge"`
	MaxNumber int    `json:"maxnumber"`
	Salt      string `json:"salt"`
	Signature string `json:"signature"`
}

type powSolution struct {
	Algorithm string `json:"algorithm"`
	Challenge string `json:"challenge"`
	Number    int    `json:"number"`
	Salt      string `json:"salt"`
	Signature string `json:"signature"`
}

// usedChallenges blocks replays of a solved challenge until it expires.
type usedChallenges struct {
	mu   sync.Mutex
	seen map[string]time.Time
}

func newUsedChallenges() *usedChallenges {
	u := &usedChallenges{seen: map[string]time.Time{}}
	go func() {
		for range time.Tick(5 * time.Minute) {
			u.mu.Lock()
			now := time.Now()
			for k, exp := range u.seen {
				if now.After(exp) {
					delete(u.seen, k)
				}
			}
			u.mu.Unlock()
		}
	}()
	return u
}

func (u *usedChallenges) use(c string, exp time.Time) bool {
	u.mu.Lock()
	defer u.mu.Unlock()
	if _, ok := u.seen[c]; ok {
		return false
	}
	u.seen[c] = exp
	return true
}

func (a *App) powHMAC(s string) string {
	m := hmac.New(sha256.New, []byte(a.store.Settings().Secret+"|pow"))
	m.Write([]byte(s))
	return hex.EncodeToString(m.Sum(nil))
}

func sha256hex(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:])
}

func (a *App) handleChallenge(w http.ResponseWriter, r *http.Request) {
	st := a.store.Settings()
	max := st.CaptchaDifficulty
	if max < 1000 {
		max = 1000
	}
	n, err := rand.Int(rand.Reader, big.NewInt(int64(max)+1))
	if err != nil {
		http.Error(w, "rng", 500)
		return
	}
	salt := randomToken(12) + "?expires=" + strconv.FormatInt(time.Now().Add(challengeTTL).Unix(), 10)
	ch := sha256hex(salt + n.String())
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, 200, challenge{Algorithm: "SHA-256", Challenge: ch, MaxNumber: max, Salt: salt, Signature: a.powHMAC(ch)})
}

// checkPoW verifies the "altcha" form field. It always passes when the
// captcha is switched off.
func (a *App) checkPoW(r *http.Request) bool {
	if !a.store.Settings().CaptchaEnabled {
		return true
	}
	raw, err := base64.StdEncoding.DecodeString(r.FormValue("altcha"))
	if err != nil {
		return false
	}
	var s powSolution
	if json.Unmarshal(raw, &s) != nil || s.Algorithm != "SHA-256" || s.Number < 0 {
		return false
	}
	if !hmac.Equal([]byte(s.Signature), []byte(a.powHMAC(s.Challenge))) {
		return false
	}
	_, q, _ := strings.Cut(s.Salt, "?")
	params, _ := url.ParseQuery(q)
	unix, err := strconv.ParseInt(params.Get("expires"), 10, 64)
	if err != nil || time.Now().Unix() > unix {
		return false
	}
	if sha256hex(s.Salt+strconv.Itoa(s.Number)) != s.Challenge {
		return false
	}
	return a.usedPoW.use(s.Challenge, time.Unix(unix, 0))
}

// --- data protection --------------------------------------------------------

// retentionLoop deletes Done/Declined cards (and their files) once they have
// been closed for longer than the configured number of days.
func (a *App) retentionLoop() {
	for {
		a.applyRetention()
		time.Sleep(time.Hour)
	}
}

func (a *App) applyRetention() {
	days := a.store.Settings().RetentionDays
	if days <= 0 {
		return
	}
	cut := time.Now().Add(-time.Duration(days) * 24 * time.Hour)
	var removed []Attachment
	a.store.Update(func(db *dbFile) error {
		db.Tickets = slices.DeleteFunc(db.Tickets, func(t *Ticket) bool {
			if (t.Status == "done" || t.Status == "rejected") && t.UpdatedAt.Before(cut) {
				removed = append(removed, t.Attachments...)
				return true
			}
			return false
		})
		return nil
	})
	for _, att := range removed {
		os.Remove(filepath.Join(a.store.Uploads, att.File))
	}
}

// handleSelfDelete lets a submitter delete their own feedback (right to erasure).
func (a *App) handleSelfDelete(w http.ResponseWriter, r *http.Request) {
	if !a.sameOrigin(r) || !a.store.Settings().AllowSelfDelete {
		a.notFound(w, r)
		return
	}
	t := a.store.TicketByToken(r.PathValue("token"))
	if t == nil {
		a.notFound(w, r)
		return
	}
	var removed []Attachment
	a.store.Update(func(db *dbFile) error {
		db.Tickets = slices.DeleteFunc(db.Tickets, func(x *Ticket) bool {
			if x.ID == t.ID {
				removed = x.Attachments
				return true
			}
			return false
		})
		return nil
	})
	a.removeFiles(removed)
	a.render(w, r, 200, "error", pageData{"Title": "Deleted", "Message": "Your feedback, all messages and all attachments have been deleted."})
}
