package game

import (
	"math/rand"
	"strconv"

	"2droundserver/internal/store"
)

// The events of the growth way (growway.go) the game fires. The ids and sentences are the client's (room_grow_data.lua). The original
// picks one of an event's variants at random (capture original-2026-10-27: 109004 of four, 102004 of four, 108003 of three,
// 115001 of three, 101303 of three), so growPick does too; the thresholds some of them name are not in the client, so the numbers of
// the sentences are used (OPEN_QUESTIONS.md).

// growPick is a random variant of an event: base, base+1 ... base+n-1 (the variants of one event are numbered in turn).
func growPick(base int64, n int) int64 { return base + int64(rand.Intn(n)) }

// growHasAny tells whether the role has a line of any variant of the event.
func growHasAny(st *store.Store, rid, base int64, n int) (bool, error) {
	for i := 0; i < n; i++ {
		if had, err := growHas(st, rid, base+int64(i)); err != nil || had {
			return had, err
		}
	}
	return false, nil
}

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
		_ = RecordGrowth(st, rid, growPick(103001, 4), now, friend)
	case 100:
		_ = RecordGrowth(st, rid, growPick(104001, 4), now, friend)
	}
}

// growGuild: a role created (create) or joined a guild.
func growGuild(st *store.Store, rid int64, guild string, leaderRID int64, create bool, now int64) {
	if create {
		_ = RecordGrowth(st, rid, growPick(110001, 4), now, guild)
		return
	}
	leader := ""
	if lr, err := st.RoleByID(leaderRID); err == nil {
		leader = lr.Name
	}
	_ = RecordGrowth(st, rid, growPick(109001, 4), now, guild, leader)
}

// growWorshipped: a role was worshipped by another; the milestones 1, 10, 50 and 100.
func growWorshipped(st *store.Store, rid int64, by string, total, now int64) {
	switch total {
	case 1, 10, 50:
		_ = RecordGrowth(st, rid, growPick(107001, 3), now, by, growNum(total))
	case 100:
		_ = RecordGrowth(st, rid, 107003, now, by)
	}
}

// growArtifact: a hero got a super (10454) or legend (10455) skill book. The capture (original-2026-10-27) has a line for
// the first super one worn and none for a mid-tier one; whether later ones get a line too is not known, so it is the first only.
func growArtifact(st *store.Store, rid int64, name string, base, now int64) {
	id, n := int64(125001), 2
	if base == 10455 {
		id, n = 126001, 3
	} else if base != 10454 {
		return
	}
	if had, err := growHasAny(st, rid, id, n); err == nil && !had {
		_ = RecordGrowth(st, rid, growPick(id, n), now, name)
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
	first := func(base int64, n int) {
		if had, err := growHasAny(st, rid, base, n); err == nil && !had {
			_ = RecordGrowth(st, rid, growPick(base, n), now, name)
		}
	}
	switch star {
	case 5:
		if atLeast(5) == 5 { // the fifth hero of five stars
			_ = RecordGrowth(st, rid, growPick(108001, 3), now, name)
		}
	case 6:
		first(102001, 4)
	case 7:
		first(115001, 3)
	case 8:
		first(115101, 3)
	case 9:
		first(115201, 3)
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
		_ = RecordGrowth(st, rid, growPick(id, 2), now, name)
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
