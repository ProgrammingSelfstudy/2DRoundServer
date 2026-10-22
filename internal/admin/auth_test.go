package admin

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

func TestLoginReturnsATokenAndNoSecrets(t *testing.T) {
	e := newEnv(t)
	e.admin("boss", "superadmin", "correct-horse")
	r := e.do("", "POST", "/api/login", map[string]string{"username": "boss", "password": "correct-horse"})
	if r.Status != 200 {
		t.Fatalf("%d %s", r.Status, r.Body)
	}
	j := r.JSON()
	if j["token"] == "" || j["expires_at"] == nil {
		t.Fatalf("%v", j)
	}
	if strings.Contains(string(r.Body), "password") || strings.Contains(string(r.Body), "$2a$") {
		t.Fatalf("the response leaks password material: %s", r.Body)
	}
	me := e.do(j["token"].(string), "GET", "/api/me", nil)
	if me.Status != 200 || me.JSON()["username"] != "boss" || me.JSON()["role"] != "superadmin" {
		t.Fatalf("me: %d %s", me.Status, me.Body)
	}
}

func TestLoginFailuresLookTheSame(t *testing.T) {
	e := newEnv(t)
	e.admin("boss", "superadmin", "correct-horse")
	id := e.admin("off", "operator", "disabled-pass1")
	e.st.SetAdminStatus(id, 0)
	var msgs []string
	for _, c := range [][2]string{{"boss", "wrong"}, {"nobody", "whatever"}, {"off", "disabled-pass1"}, {"", ""}} {
		r := e.do("", "POST", "/api/login", map[string]string{"username": c[0], "password": c[1]})
		if r.Status != 401 {
			t.Fatalf("%v -> %d", c, r.Status)
		}
		msgs = append(msgs, r.Err())
	}
	for _, m := range msgs {
		if m != msgs[0] {
			t.Fatalf("different messages reveal which usernames exist: %q", msgs)
		}
	}
}

func TestLoginLockoutAndRecovery(t *testing.T) {
	e := newEnv(t)
	e.admin("boss", "superadmin", "correct-horse")
	e.admin("other", "viewer", "other-pass-123")
	for i := 0; i < maxFailures; i++ {
		if r := e.do("", "POST", "/api/login", map[string]string{"username": "boss", "password": "bad"}); r.Status != 401 {
			t.Fatalf("attempt %d: %d", i, r.Status)
		}
	}
	r := e.do("", "POST", "/api/login", map[string]string{"username": "boss", "password": "correct-horse"})
	if r.Status != 429 || r.Header.Get("Retry-After") == "" {
		t.Fatalf("a locked pair must be refused even with the right password: %d %v", r.Status, r.Header)
	}
	if r := e.do("", "POST", "/api/login", map[string]string{"username": "BOSS", "password": "correct-horse"}); r.Status != 429 {
		t.Fatalf("the lock must not depend on letter case: %d", r.Status)
	}
	if r := e.do("", "POST", "/api/login", map[string]string{"username": "other", "password": "other-pass-123"}); r.Status != 200 {
		t.Fatalf("another username is unaffected: %d", r.Status)
	}
	e.advance(lockout + time.Second)
	if r := e.do("", "POST", "/api/login", map[string]string{"username": "boss", "password": "correct-horse"}); r.Status != 200 {
		t.Fatalf("after the lockout the login works again: %d %s", r.Status, r.Body)
	}
	// a success clears the failure count
	for i := 0; i < maxFailures-1; i++ {
		e.do("", "POST", "/api/login", map[string]string{"username": "boss", "password": "bad"})
	}
	e.login("boss", "correct-horse")
	for i := 0; i < maxFailures-1; i++ {
		e.do("", "POST", "/api/login", map[string]string{"username": "boss", "password": "bad"})
	}
	if r := e.do("", "POST", "/api/login", map[string]string{"username": "boss", "password": "correct-horse"}); r.Status != 200 {
		t.Fatalf("failures before a success must not count afterwards: %d", r.Status)
	}
}

func TestTokenValidation(t *testing.T) {
	e := newEnv(t)
	id := e.admin("boss", "superadmin", "correct-horse")
	good := e.login("boss", "correct-horse")

	if r := e.do("", "GET", "/api/me", nil); r.Status != 401 {
		t.Fatalf("no token: %d", r.Status)
	}
	for name, tok := range map[string]string{
		"garbage":   "not-a-token",
		"tampered":  good[:len(good)-3] + "AAA",
		"empty":     "x",
		"other key": signWith(t, []byte("a different secret entirely, long enough......"), jwt.SigningMethodHS256, id, "", e.clock.Load()),
	} {
		if r := e.do(tok, "GET", "/api/me", nil); r.Status != 401 {
			t.Errorf("%s: %d", name, r.Status)
		}
	}
	// the right key and the right password stamp, but another HMAC algorithm: only HS256 is ours
	a, _ := e.st.AdminByID(id)
	hs512 := signWith(t, e.s.Secret, jwt.SigningMethodHS512, id, stamp(a.PasswordHash), e.clock.Load())
	if r := e.do(hs512, "GET", "/api/me", nil); r.Status != 401 {
		t.Fatalf("a token signed with HS512 was accepted: %d", r.Status)
	}
	hs256 := signWith(t, e.s.Secret, jwt.SigningMethodHS256, id, stamp(a.PasswordHash), e.clock.Load())
	if r := e.do(hs256, "GET", "/api/me", nil); r.Status != 200 {
		t.Fatalf("the same token signed with HS256 must work (the test token itself is valid): %d", r.Status)
	}
	// alg=none must never be accepted
	none := jwt.NewWithClaims(jwt.SigningMethodNone, claims{RegisteredClaims: jwt.RegisteredClaims{Subject: fmt.Sprint(id), ExpiresAt: jwt.NewNumericDate(time.Unix(0, e.clock.Load()).Add(time.Hour))}})
	s, _ := none.SignedString(jwt.UnsafeAllowNoneSignatureType)
	if r := e.do(s, "GET", "/api/me", nil); r.Status != 401 {
		t.Fatalf("alg none accepted: %d", r.Status)
	}
	// no expiry claim at all
	noExp := jwt.NewWithClaims(jwt.SigningMethodHS256, claims{PV: "x", RegisteredClaims: jwt.RegisteredClaims{Subject: fmt.Sprint(id)}})
	s, _ = noExp.SignedString(e.s.Secret)
	if r := e.do(s, "GET", "/api/me", nil); r.Status != 401 {
		t.Fatalf("token without exp accepted: %d", r.Status)
	}
	// expiry
	if r := e.do(good, "GET", "/api/me", nil); r.Status != 200 {
		t.Fatalf("fresh token: %d", r.Status)
	}
	e.advance(e.s.TTL + time.Second)
	if r := e.do(good, "GET", "/api/me", nil); r.Status != 401 {
		t.Fatalf("expired token: %d", r.Status)
	}
}

// signWith builds a structurally valid token with the given key and password stamp.
func signWith(t *testing.T, key []byte, m jwt.SigningMethod, id int64, pv string, nowNanos int64) string {
	t.Helper()
	tok := jwt.NewWithClaims(m, claims{PV: pv, RegisteredClaims: jwt.RegisteredClaims{Subject: fmt.Sprint(id), ExpiresAt: jwt.NewNumericDate(time.Unix(0, nowNanos).Add(time.Hour))}})
	s, err := tok.SignedString(key)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestTokenFollowsTheAdminRecord(t *testing.T) {
	e := newEnv(t)
	e.admin("boss", "superadmin", "correct-horse")
	id := e.admin("op", "operator", "operator-pass1")
	tok := e.login("op", "operator-pass1")
	body := map[string]any{"scope": "broadcast", "subject": "x"}

	if r := e.do(tok, "POST", "/api/mails", body); r.Status != 201 {
		t.Fatalf("operator may send mail: %d %s", r.Status, r.Body)
	}
	e.st.UpdateAdmin(id, "viewer", -1) // demoted: the very next request sees it
	if r := e.do(tok, "POST", "/api/mails", body); r.Status != 403 {
		t.Fatalf("a demoted admin keeps the old role: %d", r.Status)
	}
	e.st.UpdateAdmin(id, "", 0) // disabled
	if r := e.do(tok, "GET", "/api/me", nil); r.Status != 401 {
		t.Fatalf("a disabled admin keeps working: %d", r.Status)
	}
}

func TestChangingThePasswordInvalidatesOldTokens(t *testing.T) {
	e := newEnv(t)
	e.admin("boss", "superadmin", "correct-horse")
	old := e.login("boss", "correct-horse")
	other := e.login("boss", "correct-horse") // a second session

	if r := e.do(old, "POST", "/api/me/password", map[string]string{"old": "wrong", "new": "brand-new-pass"}); r.Status != 403 {
		t.Fatalf("wrong current password: %d", r.Status)
	}
	if r := e.do(old, "POST", "/api/me/password", map[string]string{"old": "correct-horse", "new": "short"}); r.Status != 422 {
		t.Fatalf("short new password: %d", r.Status)
	}
	if r := e.do(old, "POST", "/api/me/password", map[string]string{"old": "correct-horse", "new": "brand-new-pass"}); r.Status != 200 {
		t.Fatalf("change: %d %s", r.Status, r.Body)
	}
	for name, tok := range map[string]string{"the session that changed it": old, "another session": other} {
		if r := e.do(tok, "GET", "/api/me", nil); r.Status != 401 {
			t.Errorf("%s still works after a password change: %d", name, r.Status)
		}
	}
	if r := e.do("", "POST", "/api/login", map[string]string{"username": "boss", "password": "correct-horse"}); r.Status != 401 {
		t.Fatalf("old password still works: %d", r.Status)
	}
	if e.login("boss", "brand-new-pass") == "" {
		t.Fatal("new password does not work")
	}
	for _, a := range e.auditActions() {
		if strings.Contains(a, "brand-new") {
			t.Fatal("password in audit")
		}
	}
}

func TestRequestBodyRules(t *testing.T) {
	e := newEnv(t)
	_, _, super := e.tokens()
	if r := e.do(super, "POST", "/api/notices", `{"title":"t","content":"c","status":1,"surprise":true}`); r.Status != 400 {
		t.Fatalf("unknown field accepted: %d %s", r.Status, r.Body)
	}
	if r := e.do(super, "POST", "/api/notices", `{"title":"t","content":"c","status":1} {"x":1}`); r.Status != 400 {
		t.Fatalf("trailing data accepted: %d", r.Status)
	}
	if r := e.do(super, "POST", "/api/notices", `{not json`); r.Status != 400 {
		t.Fatalf("bad json: %d", r.Status)
	}
	big := `{"title":"t","content":"` + strings.Repeat("x", 2<<20) + `","status":1}`
	if r := e.do(super, "POST", "/api/notices", big); r.Status != 413 {
		t.Fatalf("oversized body: %d", r.Status)
	}
	r := e.do(super, "GET", "/api/me", nil)
	if r.Header.Get("Cache-Control") != "no-store" || r.Header.Get("X-Content-Type-Options") != "nosniff" {
		t.Fatalf("security headers: %v", r.Header)
	}
}
