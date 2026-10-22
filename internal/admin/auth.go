package admin

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/crypto/bcrypt"

	"2droundserver/internal/store"
)

const (
	maxFailures = 5
	lockout     = 15 * time.Minute
	minPassword = 8
)

var reAdminName = regexp.MustCompile(`^[A-Za-z0-9_.-]{3,32}$`)

// loginLimiter locks a (username, IP) pair after repeated failures.
type loginLimiter struct {
	mu  sync.Mutex
	now func() time.Time
	m   map[string]*attempt
}

type attempt struct {
	fails int
	until time.Time
}

func newLoginLimiter(now func() time.Time) *loginLimiter {
	return &loginLimiter{now: now, m: map[string]*attempt{}}
}

func (l *loginLimiter) key(user, ip string) string { return strings.ToLower(user) + "|" + ip }

// blocked reports whether the pair is locked, and for how long.
func (l *loginLimiter) blocked(user, ip string) (bool, time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()
	a := l.m[l.key(user, ip)]
	if a == nil || !l.now().Before(a.until) {
		return false, 0
	}
	return true, a.until.Sub(l.now())
}

func (l *loginLimiter) fail(user, ip string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if len(l.m) > 10000 { // forget stale entries so an attacker cannot grow the map without bound
		for k, a := range l.m {
			if !l.now().Before(a.until) && a.fails < maxFailures {
				delete(l.m, k)
			}
		}
	}
	k := l.key(user, ip)
	a := l.m[k]
	if a == nil {
		a = &attempt{}
		l.m[k] = a
	}
	if !a.until.IsZero() && !l.now().Before(a.until) { // a lock that has run out starts over
		a.fails, a.until = 0, time.Time{}
	}
	a.fails++
	if a.fails >= maxFailures {
		a.until = l.now().Add(lockout)
	}
}

func (l *loginLimiter) reset(user, ip string) {
	l.mu.Lock()
	delete(l.m, l.key(user, ip))
	l.mu.Unlock()
}

// dummyHash is compared against when the username is unknown, so that response
// time does not reveal which usernames exist.
var dummyHash, _ = bcrypt.GenerateFromPassword([]byte("not a real password"), bcrypt.DefaultCost)

// stamp ties a token to the current password: changing the password changes the
// stamp, which invalidates every token issued before.
func stamp(hash string) string {
	sum := sha256.Sum256([]byte(hash))
	return hex.EncodeToString(sum[:8])
}

type claims struct {
	PV string `json:"pv"`
	jwt.RegisteredClaims
}

func (s *Server) issue(a *store.Admin) (string, time.Time, error) {
	exp := s.Now().Add(s.TTL)
	t := jwt.NewWithClaims(jwt.SigningMethodHS256, claims{
		PV: stamp(a.PasswordHash),
		RegisteredClaims: jwt.RegisteredClaims{
			Subject: strconv.FormatInt(a.ID, 10), IssuedAt: jwt.NewNumericDate(s.Now()), ExpiresAt: jwt.NewNumericDate(exp),
		},
	})
	str, err := t.SignedString(s.Secret)
	return str, exp, err
}

// authenticate checks the bearer token and loads the admin from the database, so a
// disabled admin, a changed role or a changed password takes effect on the next request.
func (s *Server) authenticate(w http.ResponseWriter, r *http.Request) (*principal, bool) {
	h := r.Header.Get("Authorization")
	tok, ok := strings.CutPrefix(h, "Bearer ")
	if !ok || tok == "" {
		fail(w, http.StatusUnauthorized, "login required")
		return nil, false
	}
	var c claims
	_, err := jwt.ParseWithClaims(tok, &c, func(*jwt.Token) (any, error) { return s.Secret, nil },
		jwt.WithValidMethods([]string{"HS256"}), jwt.WithExpirationRequired(), jwt.WithTimeFunc(s.Now))
	if err != nil {
		fail(w, http.StatusUnauthorized, "invalid or expired token")
		return nil, false
	}
	id, err := strconv.ParseInt(c.Subject, 10, 64)
	if err != nil {
		fail(w, http.StatusUnauthorized, "invalid token")
		return nil, false
	}
	a, err := s.Store.AdminByID(id)
	if err != nil || a.Status != 1 || c.PV != stamp(a.PasswordHash) {
		fail(w, http.StatusUnauthorized, "invalid or expired token")
		return nil, false
	}
	return &principal{Admin: a}, true
}

func (s *Server) login(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if !decode(w, r, &req) {
		return
	}
	ip := clientIP(r)
	if blocked, wait := s.limiter.blocked(req.Username, ip); blocked {
		w.Header().Set("Retry-After", strconv.Itoa(int(wait.Seconds())+1))
		fail(w, http.StatusTooManyRequests, "too many failed attempts, try again later")
		return
	}
	a, err := s.Store.AdminByUsername(req.Username)
	hash := string(dummyHash)
	if err == nil {
		hash = a.PasswordHash
	} else if !errors.Is(err, store.ErrNotFound) {
		internalErr(w, s, err)
		return
	}
	pwOK := bcrypt.CompareHashAndPassword([]byte(hash), []byte(req.Password)) == nil
	if err != nil || !pwOK || a.Status != 1 {
		s.limiter.fail(req.Username, ip)
		fail(w, http.StatusUnauthorized, "wrong username or password") // the same text for every cause
		return
	}
	s.limiter.reset(req.Username, ip)
	tok, exp, err := s.issue(a)
	if err != nil {
		internalErr(w, s, err)
		return
	}
	s.Store.TouchAdminLogin(a.ID)
	p := &principal{Admin: a}
	s.audit(r, p, "admin.login", a.Username, nil)
	writeJSON(w, 200, map[string]any{"token": tok, "expires_at": exp.Unix(), "admin": a})
}

func (s *Server) me(w http.ResponseWriter, r *http.Request, p *principal) {
	writeJSON(w, 200, p.Admin)
}

func validPassword(pw string) bool { return len(pw) >= minPassword && len(pw) <= 72 } // 72: bcrypt's limit

func (s *Server) changeOwnPassword(w http.ResponseWriter, r *http.Request, p *principal) {
	var req struct {
		Old string `json:"old"`
		New string `json:"new"`
	}
	if !decode(w, r, &req) {
		return
	}
	if bcrypt.CompareHashAndPassword([]byte(p.Admin.PasswordHash), []byte(req.Old)) != nil {
		fail(w, http.StatusForbidden, "the current password is wrong")
		return
	}
	if !validPassword(req.New) {
		fail(w, http.StatusUnprocessableEntity, "the new password must be 8 to 72 characters")
		return
	}
	h, err := bcrypt.GenerateFromPassword([]byte(req.New), bcrypt.DefaultCost)
	if err != nil {
		internalErr(w, s, err)
		return
	}
	if err := s.Store.SetAdminPassword(p.Admin.ID, string(h)); err != nil {
		internalErr(w, s, err)
		return
	}
	s.audit(r, p, "admin.password.change", p.Admin.Username, nil)
	writeJSON(w, 200, map[string]bool{"ok": true}) // every earlier token is now invalid; log in again
}
