package game

import "testing"

func TestRoleExtraRegistered(t *testing.T) {
	e := newEnv(t, 0)
	for _, c := range []uint16{10330, 10332, 10333, 10907, 25813, 25814, 25815, 25816, 25817, 25818, 25819, 25820, 25830, 25831, 25832, 25840} {
		if !e.gw.Router.Has(c) {
			t.Errorf("%d has no handler", c)
		}
	}
}

func TestBattleLeftoversRegistered(t *testing.T) {
	e := newEnv(t, 0)
	for _, c := range []uint16{10958, 20008, 20009, 20014, 20015, 20016, 20022, 20026, 20028, 20034, 20036} {
		if !e.gw.Router.Has(c) {
			t.Errorf("%d has no handler", c)
		}
	}
}

func TestHeroStubsRegistered(t *testing.T) {
	e := newEnv(t, 0)
	for _, c := range []uint16{11018, 11030, 11086, 11401, 25220, 26420, 26432} {
		if !e.gw.Router.Has(c) {
			t.Errorf("%d has no handler", c)
		}
	}
}
