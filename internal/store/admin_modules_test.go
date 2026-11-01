package store

import (
	"bytes"
	"encoding/json"
	"errors"
	"testing"
)

func moduleRole(t *testing.T, s *Store) *Role {
	t.Helper()
	if err := s.EnsureServer(srv("symlf_1")); err != nil {
		t.Fatal(err)
	}
	a, err := s.FindOrCreateAccount("symlf", "module-test")
	if err != nil {
		t.Fatal(err)
	}
	r, err := s.CreateRole(a.ID, "symlf_1", "模块事务", 0, 1, 0)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func TestRoleModuleResetTransactionAndCompareBefore(t *testing.T) {
	s := open(t)
	role := moduleRole(t, s)
	if err := s.SaveState(role.RID, "test_reset", map[string]int{"count": 9}); err != nil {
		t.Fatal(err)
	}
	before, ok, err := s.RoleModule(role.RID, "test_reset")
	if err != nil || !ok {
		t.Fatal("missing seed")
	}
	after := json.RawMessage(`{"count":0}`)
	audit := AuditLog{AdminID: 1, AdminName: "test", Action: "role.module.reset", Target: role.Name, DetailJSON: `{"reason":"核对"}`}
	if err := s.CommitRoleModuleReset(role.RID, "test_reset", before, after, audit); err != nil {
		t.Fatal(err)
	}
	if err := s.CommitRoleModuleReset(role.RID, "test_reset", before, nil, audit); !errors.Is(err, ErrConflict) {
		t.Fatalf("stale before not rejected: %v", err)
	}
	current, _, _ := s.RoleModule(role.RID, "test_reset")
	if !bytes.Equal(current, after) {
		t.Fatal("stale delete changed state")
	}
	_, total, err := s.ListAudit(10, 0)
	if err != nil || total != 1 {
		t.Fatalf("wrong audit count: %d %v", total, err)
	}
	if err := s.CommitRoleModuleReset(role.RID, "test_reset", after, nil, audit); err != nil {
		t.Fatal(err)
	}
	if _, exists, _ := s.RoleModule(role.RID, "test_reset"); exists {
		t.Fatal("delete did not commit")
	}
}

func TestRoleModuleResetRollsBackWhenAuditFails(t *testing.T) {
	s := open(t)
	role := moduleRole(t, s)
	if err := s.SaveState(role.RID, "test_reset", map[string]int{"count": 9}); err != nil {
		t.Fatal(err)
	}
	before, _, _ := s.RoleModule(role.RID, "test_reset")
	if _, err := s.db.Exec(`CREATE TRIGGER reject_module_audit BEFORE INSERT ON audit_logs BEGIN SELECT RAISE(ABORT,'audit unavailable'); END`); err != nil {
		t.Fatal(err)
	}
	audit := AuditLog{AdminID: 1, AdminName: "test", Action: "role.module.reset", DetailJSON: `{}`}
	for _, after := range []json.RawMessage{json.RawMessage(`{"count":0}`), nil} {
		if err := s.CommitRoleModuleReset(role.RID, "test_reset", before, after, audit); err == nil {
			t.Fatal("audit failure did not fail reset")
		}
		current, exists, err := s.RoleModule(role.RID, "test_reset")
		if err != nil || !exists || !bytes.Equal(before, current) {
			t.Fatal("write survived audit failure")
		}
	}
	_, total, err := s.ListAudit(10, 0)
	if err != nil || total != 0 {
		t.Fatal("failed transaction left audit")
	}
}
