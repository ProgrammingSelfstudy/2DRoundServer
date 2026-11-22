package store

import (
	"errors"
	"testing"
	"time"
)

func TestFriendRequestRejectsConcurrentBlacklist(t *testing.T) {
	for _, pair := range []struct{ sender, recipient string }{
		{sender: "symlf", recipient: "symlf"},
		{sender: "symlf", recipient: PlatformH5},
		{sender: PlatformH5, recipient: "symlf"},
		{sender: PlatformH5, recipient: PlatformH5},
	} {
		for _, direction := range []string{"sender blocks", "recipient blocks"} {
			t.Run(pair.sender+"/"+pair.recipient+"/"+direction, func(t *testing.T) {
				s := open(t)
				const server = "symlf_1"
				if err := s.EnsureServer(srv(server)); err != nil {
					t.Fatal(err)
				}
				createRole := func(platform, name string) int64 {
					acct, err := s.FindOrCreateAccount(platform, name)
					if err != nil {
						t.Fatal(err)
					}
					role, err := s.CreateRole(acct.ID, server, name, 1, 1, 0)
					if err != nil {
						t.Fatal(err)
					}
					return role.RID
				}
				from, to := createRole(pair.sender, "sender"), createRole(pair.recipient, "recipient")
				atInsert, resumeInsert := make(chan struct{}), make(chan struct{})
				requestStore := &Store{db: s.db, now: func() time.Time {
					close(atInsert)
					<-resumeInsert
					return time.Now()
				}}
				result := make(chan error, 1)
				go func() { result <- requestStore.RequestFriend(from, to) }()
				select {
				case <-atInsert:
				case err := <-result:
					t.Fatalf("request finished before insert could be interleaved: %v", err)
				}
				blocker, target := from, to
				if direction == "recipient blocks" {
					blocker, target = to, from
				}
				_, blockErr := s.AddBlack(blocker, target, 50)
				close(resumeInsert)
				requestErr := <-result
				if blockErr != nil {
					t.Fatal(blockErr)
				}
				if !errors.Is(requestErr, ErrSocialBlocked) {
					t.Errorf("friend request error = %v, want %v", requestErr, ErrSocialBlocked)
				}
				if pending, err := s.HasFriendRequest(from, to); err != nil || pending {
					t.Errorf("pending request = %t, err = %v; want no request", pending, err)
				}
				wantDMBlock := pair.sender == PlatformH5 && pair.recipient == PlatformH5
				if blocked, err := s.PrivateChatBlocked(from, to); err != nil || blocked != wantDMBlock {
					t.Errorf("private chat blocked = %t, err = %v; want %t", blocked, err, wantDMBlock)
				}
			})
		}
	}
}
