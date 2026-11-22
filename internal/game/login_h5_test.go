package game

import (
	"testing"

	"2droundserver/internal/store"
)

func TestEnteredSessionRejectsAccountSwitch(t *testing.T) {
	for _, test := range []struct{ name, platform, otherPlatform string }{
		{"H5 to native", store.PlatformH5, "symlf"},
		{"native to H5", "symlf", store.PlatformH5},
		{"H5 to other H5", store.PlatformH5, store.PlatformH5},
	} {
		t.Run(test.name, func(t *testing.T) {
			env := newDungeonEnv(t)
			client, role := enterDungeonRole(env, test.platform, "original", "Original")
			client.settle()
			session := env.gw.Hub.Online(role.RID)
			other, err := env.st.FindOrCreateAccount(test.otherPlatform, "other")
			if err != nil {
				t.Fatal(err)
			}
			token, err := env.st.IssueToken(other.ID, 3600)
			if err != nil {
				t.Fatal(err)
			}
			client.send(1164, loginArgs(other.Account, token))
			if reply := client.recv(1164); reply.Int("code") != codeFail || reply.Str("msg") == "" || len(reply.Arr("roles")) != 0 {
				t.Fatalf("account switch did not fail normally: %v", reply)
			}
			if session == nil || env.gw.Hub.Online(role.RID) != session || session.Account().ID != role.AccountID || session.Role().RID != role.RID {
				t.Fatal("rejected login changed the entered session")
			}
			freshToken, err := env.st.IssueToken(role.AccountID, 3600)
			if err != nil {
				t.Fatal(err)
			}
			client.send(1164, loginArgs("original", freshToken))
			reply := client.recv(1164)
			roles := reply.Arr("roles")
			if reply.Int("code") != codeOK || len(roles) != 1 || roles[0].Int("rid") != role.RID || roles[0].Int("is_online") != 1 {
				t.Fatalf("same-account re-login lost the entered role: %v", reply)
			}
		})
	}
}

func TestH5GameplayChecksEnteredRoleOwnership(t *testing.T) {
	env := newDungeonEnv(t)
	client, role := enterDungeonRole(env, "symlf", "native", "Native")
	client.settle()
	other, err := env.st.FindOrCreateAccount(store.PlatformH5, "other")
	if err != nil {
		t.Fatal(err)
	}
	session := env.gw.Hub.Online(role.RID)
	session.SetAccount(other)
	client.send(11000, nil)
	heroes := client.recv(11000).Arr("partners")
	if len(heroes) != 1 || heroes[0].Int("bid") != starterMale || heroes[0].Int("star") >= h5MinHeroStar {
		t.Fatalf("an unrelated H5 account activated H5 roster filtering: %v", heroes)
	}
}
