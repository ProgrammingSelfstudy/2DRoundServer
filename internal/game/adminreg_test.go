package game

import (
	"encoding/json"
	"reflect"
	"testing"
)

func TestAdminModuleConstructorsAndCopies(t *testing.T) {
	for _, m := range AdminModules() {
		a, b := reflect.ValueOf(m.New()), reflect.ValueOf(m.New())
		if a.IsNil() || b.IsNil() || a.Type() != b.Type() || (a.Elem().Type().Size() > 0 && a.Pointer() == b.Pointer()) {
			t.Fatalf("constructor does not return fresh state: %s/%s", m.Scope, m.Key)
		}
		if _, err := json.Marshal(a.Interface()); err != nil {
			t.Fatalf("state not JSON serializable: %s/%s: %v", m.Scope, m.Key, err)
		}
	}
	a := AdminModules()
	a[0].Key = "mutated"
	if AdminModules()[0].Key == "mutated" {
		t.Fatal("caller mutated registry")
	}
}

func TestAdminModuleConcreteKeyValidation(t *testing.T) {
	for _, tc := range []struct{ scope, key string }{
		{"role", "monthcard"}, {"role", "firstcharge_first"}, {"role", "firstcharge_first_new"},
		{"role", "firstcharge_first_lwt"}, {"role", "firstcharge_first_3"},
		{"global", "guildmarket:1"}, {"global", "monopoly_g9223372036854775807"},
		{"role", "crosschamp"}, {"global", "crosschamp"},
	} {
		if _, ok := FindAdminModule(tc.scope, tc.key); !ok {
			t.Errorf("valid concrete key rejected: %s/%s", tc.scope, tc.key)
		}
	}
	for _, tc := range []struct{ scope, key string }{
		{"role", "unknown"}, {"role", "firstcharge_*"}, {"role", "firstcharge_arbitrary"},
		{"role", "firstcharge_"}, {"role", "guildmarket:1"}, {"global", "monthcard"},
		{"global", "guildmarket:0"}, {"global", "guildmarket:01"}, {"global", "guildmarket:-1"},
		{"global", "monopoly_g9223372036854775808"}, {"global", "monopoly_g"},
		{"role", "../../monthcard"}, {"role", "monthcard\x00"},
	} {
		if _, ok := FindAdminModule(tc.scope, tc.key); ok {
			t.Errorf("invalid concrete key accepted: %s/%s", tc.scope, tc.key)
		}
	}
}
