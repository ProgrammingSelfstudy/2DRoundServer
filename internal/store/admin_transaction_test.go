package store

import (
	"errors"
	"testing"
)

func TestAdminTransactionRollsBackMailStateAndAuditTogether(t *testing.T) {
	e := newMailEnv(t)
	s := e.s
	if err := s.SaveGlobal("symlf_1", "daysrank", map[string]bool{"Settled": false}); err != nil {
		t.Fatal(err)
	}
	for _, failAt := range []string{"mail", "state", "audit"} {
		t.Run(failAt, func(t *testing.T) {
			if failAt == "audit" {
				if _, err := s.db.Exec(`CREATE TRIGGER reject_admin_audit BEFORE INSERT ON audit_logs BEGIN SELECT RAISE(ABORT,'audit unavailable'); END`); err != nil {
					t.Fatal(err)
				}
				defer s.db.Exec(`DROP TRIGGER reject_admin_audit`)
			}
			_, before, _ := s.ListMails(100, 0)
			err := s.AdminTransaction(func(tx *Store) error {
				if _, err := tx.CreateMail(Mail{SrvID: "symlf_1", Subject: "settlement", CreatedBy: "test", Attachments: []Attachment{{Kind: AttachItem, ItemID: 1, Num: 1}}}, []int64{e.a.RID}); err != nil {
					return err
				}
				if failAt == "mail" {
					return errors.New("after mail")
				}
				if err := tx.SaveGlobal("symlf_1", "daysrank", map[string]bool{"Settled": true}); err != nil {
					return err
				}
				if failAt == "state" {
					return errors.New("after state")
				}
				return tx.WriteAudit(1, "super", "server.season.settle", "symlf_1", map[string]string{"reason": "test"}, "127.0.0.1")
			})
			if err == nil {
				t.Fatal("injected failure succeeded")
			}
			_, after, _ := s.ListMails(100, 0)
			raw, _, _ := s.GlobalModule("symlf_1", "daysrank")
			if before != after || string(raw) != `{"Settled":false}` {
				t.Fatal("failed admin transaction left mail or state")
			}
			_, audits, _ := s.ListAudit(100, 0)
			if audits != 0 {
				t.Fatal("failed admin transaction kept audit")
			}
		})
	}
	if err := s.AdminTransaction(func(tx *Store) error {
		if _, err := tx.CreateMail(Mail{SrvID: "symlf_1", Subject: "settlement", CreatedBy: "test"}, []int64{e.a.RID}); err != nil {
			return err
		}
		if err := tx.SaveGlobal("symlf_1", "daysrank", map[string]bool{"Settled": true}); err != nil {
			return err
		}
		return tx.WriteAudit(1, "super", "server.season.settle", "symlf_1", nil, "")
	}); err != nil {
		t.Fatal(err)
	}
	_, n, _ := s.ListMails(100, 0)
	_, a, _ := s.ListAudit(100, 0)
	if n != 1 || a != 1 {
		t.Fatalf("successful transaction: mails=%d audits=%d", n, a)
	}
}
