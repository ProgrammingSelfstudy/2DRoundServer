package game

import (
	"2droundserver/internal/gateway"
	"2droundserver/internal/store"
)

func isH5RoleSession(s *gateway.Session) bool {
	account, role := s.Account(), s.Role()
	return account != nil && role != nil && account.ID == role.AccountID && account.Platform == store.PlatformH5
}
