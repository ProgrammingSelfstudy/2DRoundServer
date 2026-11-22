package store

import (
	"errors"
	"maps"
	"testing"
)

func TestAscendH5PartnerRollsBackWearableRefunds(t *testing.T) {
	s := open(t)
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	must(s.EnsureServer(srv("symlf_1")))
	account, err := s.CreateAccount(PlatformH5, "growth", "", "")
	must(err)
	role, err := s.CreateRole(account.ID, "symlf_1", "H5", 0, 1, 0)
	must(err)
	rid := role.RID
	const equip, art, holy, soul = 40101, 10451, 1010201, 90503
	heroes, err := s.Summon(rid, SummonResult{Seeds: []PartnerSeed{{BID: 30501, Star: 5}, {BID: 30502, Star: 5}}})
	must(err)
	target, fodder := heroes[0].PartnerID, heroes[1].PartnerID
	must(s.Exchange(rid, nil, nil, nil, map[int64]int64{equip: 1, soul: 1}))
	must(s.EquipPiece(rid, fodder, 1, equip))
	must(s.SaveArtifacts(rid, &ArtifactDoc{Pieces: []ArtPiece{{ID: 1, Base: art, Partner: fodder, Pos: 1, Star: 2}}}))
	must(s.SaveHoly(rid, &HolyDoc{Pieces: []HolyPiece{{ID: 2, Base: holy, Partner: fodder, Slot: 1, Washes: 3}}}))
	beforeAssets, err := s.RoleAssets(rid)
	must(err)
	beforeItems, err := s.RoleItems(rid)
	must(err)

	roster := map[int64]Partner{target: heroes[0], fodder: heroes[1]}
	ascend := func() (map[int64]int64, error) {
		return s.AscendH5Partner(rid, target, 5, []int64{fodder}, nil, map[int64]int64{soul: 1},
			map[int64]int64{1: 123}, map[int64]int64{10001: 5}, roster)
	}
	// Fail the last document write, after the hero, ledgers and artifact were updated.
	_, err = s.db.Exec(`CREATE TRIGGER fail_holy_refund BEFORE UPDATE ON module_state
		WHEN NEW.module='holyeqm' BEGIN SELECT RAISE(ABORT, 'test holy refund failure'); END`)
	must(err)
	if worn, err := ascend(); err == nil || worn != nil {
		t.Fatalf("ascension with a failed refund: %v, %v", worn, err)
	}
	stored, err := s.PartnersOf(rid)
	must(err)
	if len(stored) != 2 || stored[0].Star != 5 || len(stored[1].Equips) != 1 ||
		len(stored[1].Artifacts) != 1 || len(stored[1].Holy) != 1 {
		t.Fatalf("failed ascension changed the roster: %v", stored)
	}
	assets, err := s.RoleAssets(rid)
	must(err)
	items, err := s.RoleItems(rid)
	must(err)
	if !maps.Equal(assets, beforeAssets) || !maps.Equal(items, beforeItems) {
		t.Fatalf("failed ascension changed the ledgers: %v, %v", assets, items)
	}
	_, err = s.db.Exec(`DROP TRIGGER fail_holy_refund`)
	must(err)

	worn, err := ascend()
	must(err)
	if !maps.Equal(worn, map[int64]int64{art: 1, holy: 1}) {
		t.Fatalf("wearable refund: %v", worn)
	}
	if _, err := ascend(); !errors.Is(err, ErrConflict) {
		t.Fatalf("replayed ascension: %v", err)
	}
	stored, err = s.PartnersOf(rid)
	must(err)
	assets, err = s.RoleAssets(rid)
	must(err)
	items, err = s.RoleItems(rid)
	must(err)
	if len(stored) != 1 || stored[0].Star != 6 || assets[1] != beforeAssets[1]+123 ||
		!maps.Equal(items, map[int64]int64{equip: 1, art: 1, holy: 1, 10001: 5}) {
		t.Fatalf("retried ascension: %v, %v, %v", stored, assets, items)
	}
	artifacts, err := s.ArtifactsOf(rid)
	must(err)
	holyDoc, err := s.HolyOf(rid)
	must(err)
	if len(artifacts.Pieces) != 1 || artifacts.Pieces[0].ID != 1 || artifacts.Pieces[0].Partner != 0 ||
		artifacts.Pieces[0].Pos != 0 || artifacts.Pieces[0].Star != 2 ||
		len(holyDoc.Pieces) != 1 || holyDoc.Pieces[0].ID != 2 || holyDoc.Pieces[0].Partner != 0 ||
		holyDoc.Pieces[0].Slot != 0 || holyDoc.Pieces[0].Washes != 3 {
		t.Fatalf("refunded items lost instance data: %v, %v", artifacts, holyDoc)
	}
}
