package game

import (
	"testing"

	"2droundserver/internal/store"
)

func TestClearedMaxIsWhatTheClientIsTold(t *testing.T) {
	first, second := dramaChain.Dungeons[0], dramaChain.Dungeons[1]
	last := dramaChain.Dungeons[len(dramaChain.Dungeons)-1]
	mk := func(dun, max int64, status int) store.Drama {
		return store.Drama{DunID: dun, MaxDunID: max, Status: status}
	}
	for _, c := range []struct {
		name string
		d    store.Drama
		want int64
	}{
		{"new role at the first dungeon", mk(first.ID, first.ID, dungeonNotCleared), 0},
		{"after the first win", mk(second.ID, second.ID, dungeonNotCleared), first.ID},
		{"replaying an earlier dungeon", mk(first.ID, second.ID, dungeonCleared), first.ID},
		{"the last dungeon not yet cleared", mk(last.ID, last.ID, dungeonNotCleared), dramaChain.Dungeons[len(dramaChain.Dungeons)-2].ID},
		{"the last dungeon cleared", mk(last.ID, last.ID, dungeonCleared), last.ID},
	} {
		if got := clearedMax(c.d); got != c.want {
			t.Errorf("%s: clearedMax = %d, want %d", c.name, got, c.want)
		}
	}
}
