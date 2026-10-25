package store

import (
	"errors"
	"testing"
	"time"
)

func open(t *testing.T) *Store {
	t.Helper()
	s, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func srv(id string) Server {
	return Server{SrvID: id, Name: id, Platform: "symlf", ZoneID: 1, Host: "127.0.0.1", Port: 8800, Status: 1, GroupID: 1}
}

func TestAccountsAndTokens(t *testing.T) {
	s := open(t)
	a, err := s.FindOrCreateAccount("symlf", "u1")
	if err != nil {
		t.Fatal(err)
	}
	if b, _ := s.FindOrCreateAccount("symlf", "u1"); b.ID != a.ID {
		t.Fatal("FindOrCreate must be idempotent")
	}
	tok, _ := s.IssueToken(a.ID, 60)
	if got, err := s.AccountByToken(tok); err != nil || got.ID != a.ID {
		t.Fatalf("token lookup: %v %v", got, err)
	}
	if _, err := s.AccountByToken("nope"); !errors.Is(err, ErrNotFound) {
		t.Fatal("unknown token must be ErrNotFound")
	}
	expired, _ := s.IssueToken(a.ID, -1)
	if _, err := s.AccountByToken(expired); !errors.Is(err, ErrNotFound) {
		t.Fatal("expired token must not validate")
	}
	if err := s.SetAccountStatus(a.ID, 0, "test"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.AccountByToken(tok); !errors.Is(err, ErrNotFound) {
		t.Fatal("banning must revoke tokens")
	}
}

func TestServersAndRoles(t *testing.T) {
	s := open(t)
	if err := s.EnsureServer(srv("symlf_1")); err != nil {
		t.Fatal(err)
	}
	edited := srv("symlf_1")
	edited.Name = "renamed"
	if err := s.UpsertServer(edited); err != nil {
		t.Fatal(err)
	}
	if err := s.EnsureServer(srv("symlf_1")); err != nil { // must not overwrite
		t.Fatal(err)
	}
	if v, _ := s.Server("symlf_1"); v.Name != "renamed" {
		t.Fatalf("EnsureServer overwrote an edit: %q", v.Name)
	}
	a, _ := s.FindOrCreateAccount("symlf", "u1")
	r, err := s.CreateRole(a.ID, "symlf_1", "路飞", 1, 2, 0)
	if err != nil || r.RID == 0 || r.Lev != 1 {
		t.Fatalf("create: %+v %v", r, err)
	}
	if _, err := s.CreateRole(a.ID, "symlf_1", "路飞", 1, 2, 0); !errors.Is(err, ErrConflict) {
		t.Fatalf("duplicate name: %v", err)
	}
	if err := s.EnsureServer(srv("symlf_2")); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateRole(a.ID, "symlf_2", "路飞", 1, 2, 0); err != nil {
		t.Fatalf("same name on another server must be allowed: %v", err)
	}
	if rs, _ := s.RolesByAccount(a.ID, "symlf_1"); len(rs) != 1 {
		t.Fatalf("roles by server: %d", len(rs))
	}
	if err := s.DeleteServer("symlf_1"); !errors.Is(err, ErrConflict) {
		t.Fatalf("deleting a server with roles must conflict: %v", err)
	}
	if _, err := s.CreateRole(a.ID, "no_such_srv", "x", 0, 0, 0); err == nil {
		t.Fatal("role on unknown server must fail (foreign key)")
	}
}

func TestMigrationsIdempotent(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(dir + "/a.db")
	if err != nil {
		t.Fatal(err)
	}
	s.Close()
	s, err = Open(dir + "/a.db") // reopening must not re-run migrations
	if err != nil {
		t.Fatal(err)
	}
	s.Close()
}

func TestAdminsAndAudit(t *testing.T) {
	s := open(t)
	if _, err := s.CreateAdmin("root", "hash", "superadmin"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateAdmin("root", "hash", "superadmin"); !errors.Is(err, ErrConflict) {
		t.Fatal("duplicate admin")
	}
	if err := s.WriteAudit(1, "root", "server.create", "symlf_1", map[string]any{"a": 1}, "127.0.0.1"); err != nil {
		t.Fatal(err)
	}
	rows, total, err := s.ListAudit(10, 0)
	if err != nil || total != 1 || rows[0].Action != "server.create" {
		t.Fatalf("%v %d %v", rows, total, err)
	}
}

func TestAdminListsSearchTreatsWildcardsLiterally(t *testing.T) {
	s := open(t)
	s.EnsureServer(srv("symlf_1"))
	for _, n := range []string{"a_b", "axb", "50%off", "plain"} {
		a, _ := s.FindOrCreateAccount("symlf", n)
		s.CreateRole(a.ID, "symlf_1", "r-"+n, 0, 1, 0)
	}
	for q, want := range map[string]int{"a_b": 1, "50%": 1, "%": 1, "_": 1, "": 4, "plain": 1, "nothing": 0} {
		rows, total, err := s.ListAccountsAdmin(q, 50, 0)
		if err != nil || total != want || len(rows) != want {
			t.Errorf("accounts q=%q: total=%d rows=%d err=%v, want %d", q, total, len(rows), err, want)
		}
		rr, rt, err := s.ListRolesAdmin(q, "", 50, 0)
		if err != nil || rt != want || len(rr) != want {
			t.Errorf("roles q=%q: total=%d rows=%d err=%v, want %d", q, rt, len(rr), err, want)
		}
	}
	rows, _, _ := s.ListAccountsAdmin("1", 50, 0) // numeric query also matches the id
	if len(rows) == 0 || rows[0].RoleCount != 1 {
		t.Errorf("account row: %+v", rows)
	}
	if rr, _, _ := s.ListRolesAdmin("", "symlf_2", 50, 0); len(rr) != 0 {
		t.Errorf("server filter: %+v", rr)
	}
}

func TestNoticesForRoleVisibilityAndReadState(t *testing.T) {
	s := open(t)
	now := time.Unix(1_800_000_000, 0)
	s.now = func() time.Time { return now }
	s.EnsureServer(srv("symlf_1"))
	s.EnsureServer(srv("symlf_2"))
	a, _ := s.FindOrCreateAccount("symlf", "u")
	role, _ := s.CreateRole(a.ID, "symlf_1", "甲", 0, 1, 0)
	mk := func(n Notice) int64 {
		c, err := s.CreateNotice(n)
		if err != nil {
			t.Fatal(err)
		}
		return c.ID
	}
	t0 := now.Unix()
	all := mk(Notice{Title: "all", Content: "c", Status: 1})
	own := mk(Notice{Title: "own", Content: "c", Status: 1, SrvID: "symlf_1"})
	other := mk(Notice{Title: "other server", Content: "c", Status: 1, SrvID: "symlf_2"})
	hidden := mk(Notice{Title: "hidden", Content: "c", Status: 0})
	future := mk(Notice{Title: "future", Content: "c", Status: 1, StartAt: t0 + 100})
	ended := mk(Notice{Title: "ended", Content: "c", Status: 1, StartAt: 1, EndAt: t0 - 1})
	window := mk(Notice{Title: "window", Content: "c", Status: 1, StartAt: t0 - 10, EndAt: t0 + 10})

	ids := func() map[int64]bool {
		l, err := s.NoticesForRole(role)
		if err != nil {
			t.Fatal(err)
		}
		m := map[int64]bool{}
		for _, v := range l {
			m[v.ID] = true
		}
		return m
	}
	got := ids()
	for id, want := range map[int64]bool{all: true, own: true, window: true, other: false, hidden: false, future: false, ended: false} {
		if got[id] != want {
			t.Errorf("notice %d visible=%v, want %v", id, got[id], want)
		}
	}
	// the boundaries: a notice starting exactly now is visible, one ending exactly now is not
	now = now.Add(10 * time.Second)
	if got = ids(); got[window] {
		t.Error("a notice whose end time is now must be gone")
	}
	now = now.Add(100 * time.Second) // the future one has started
	if got = ids(); !got[future] {
		t.Error("a notice that has started must appear")
	}
	if _, err := s.NoticeForRole(role, other); !errors.Is(err, ErrNotFound) {
		t.Errorf("another server's notice: %v", err)
	}
	if _, err := s.NoticeForRole(role, hidden); !errors.Is(err, ErrNotFound) {
		t.Errorf("hidden notice: %v", err)
	}
	// read state is per role and sticks
	b, _ := s.FindOrCreateAccount("symlf", "u2")
	role2, _ := s.CreateRole(b.ID, "symlf_1", "乙", 0, 1, 0)
	if err := s.MarkNoticeRead(role.RID, all); err != nil {
		t.Fatal(err)
	}
	s.MarkNoticeRead(role.RID, all) // idempotent
	if v, _ := s.NoticeForRole(role, all); !v.Read {
		t.Error("read flag not stored")
	}
	if v, _ := s.NoticeForRole(role2, all); v.Read {
		t.Error("another role must not see it as read")
	}
	// deleting a notice removes its read rows (foreign key cascade)
	if err := s.DeleteNotice(all); err != nil {
		t.Fatal(err)
	}
	var n int
	s.db.QueryRow(`SELECT COUNT(*) FROM role_notice_reads`).Scan(&n)
	if n != 0 {
		t.Errorf("read rows left behind: %d", n)
	}
}

func TestModuleStateRoundTrip(t *testing.T) {
	st, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	type doc struct {
		N int
		S []string
	}
	var got doc
	if ok, err := st.LoadGlobal("s1", "m", &got); ok || err != nil {
		t.Fatalf("empty global: %v %v", ok, err)
	}
	if err := st.SaveGlobal("s1", "m", doc{3, []string{"a"}}); err != nil {
		t.Fatal(err)
	}
	if err := st.SaveGlobal("s1", "m", doc{4, []string{"b", "c"}}); err != nil {
		t.Fatal(err)
	}
	if ok, err := st.LoadGlobal("s1", "m", &got); !ok || err != nil || got.N != 4 || len(got.S) != 2 {
		t.Fatalf("global: %v %v %+v", ok, err, got)
	}
}
