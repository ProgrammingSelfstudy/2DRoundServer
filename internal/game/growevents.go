package game

import (
	"strconv"

	"2droundserver/internal/store"
)

// The events of the growth way (growway.go) the game fires. The ids and sentences are the client's (room_grow_data.lua); which variant
// of an id the original picks, and the thresholds some of them name, are not in the client, so the first variant and the
// numbers of the sentences are used (OPEN_QUESTIONS.md).

func growNum(n int64) string { return strconv.FormatInt(n, 10) }

// growCreated: the first lines of a new role.
func growCreated(st *store.Store, rid, now int64) {
	_ = RecordGrowth(st, rid, 100001, now)
}

// growFriend: after a friendship was made; the first friend and the hundredth.
func growFriend(st *store.Store, rid int64, friend string, now int64) {
	n, err := st.CountFriends(rid)
	if err != nil {
		return
	}
	switch n {
	case 1:
		_ = RecordGrowth(st, rid, 103001, now, friend)
	case 100:
		_ = RecordGrowth(st, rid, 104001, now, friend)
	}
}

// growGuild: a role created (create) or joined a guild.
func growGuild(st *store.Store, rid int64, guild string, leaderRID int64, create bool, now int64) {
	if create {
		_ = RecordGrowth(st, rid, 110001, now, guild)
		return
	}
	leader := ""
	if lr, err := st.RoleByID(leaderRID); err == nil {
		leader = lr.Name
	}
	_ = RecordGrowth(st, rid, 109001, now, guild, leader)
}

// growWorshipped: a role was worshipped by another; the milestones 1, 10, 50 and 100.
func growWorshipped(st *store.Store, rid int64, by string, total, now int64) {
	switch total {
	case 1, 10, 50:
		_ = RecordGrowth(st, rid, 107001, now, by, growNum(total))
	case 100:
		_ = RecordGrowth(st, rid, 107003, now, by)
	}
}

// growStar: a hero reached a star. The first hero of a star and the 5th, 10th and 15th hero at 10, 11 and 12 stars get a line.
func growStar(st *store.Store, rid int64, name string, star int, now int64) {
	ps, err := st.PartnersOf(rid)
	if err != nil {
		return
	}
	atLeast := func(s int) int {
		n := 0
		for _, p := range ps {
			if p.Star >= s {
				n++
			}
		}
		return n
	}
	first := func(id int64) {
		if had, err := growHas(st, rid, id); err == nil && !had {
			_ = RecordGrowth(st, rid, id, now, name)
		}
	}
	switch star {
	case 6:
		first(102001)
	case 7:
		first(115002)
	case 8:
		first(115102)
	case 9:
		first(115202)
	}
	if star < 10 || star > 12 {
		return
	}
	n := atLeast(star)
	ids := map[int]map[int]int64{ // star -> count -> event
		10: {1: 121001, 5: 121101},
		11: {1: 122001, 5: 122101, 10: 122201, 15: 122301},
		12: {1: 123001, 5: 123101, 10: 123201, 15: 123301},
	}
	if id, ok := ids[star][n]; ok {
		_ = RecordGrowth(st, rid, id, now, name)
	}
}

// growChampion: the rank a role ended the champion match with (the server's own match).
func growChampion(st *store.Store, rid, rank, now int64) {
	switch {
	case rank == 1:
		_ = RecordGrowth(st, rid, 118201, now)
	case rank <= 8:
		_ = RecordGrowth(st, rid, 118101, now, growNum(rank))
	case rank <= 32:
		_ = RecordGrowth(st, rid, 118001, now, growNum(rank))
	}
}
