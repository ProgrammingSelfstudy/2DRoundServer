package game

import (
	"time"

	"2droundserver/internal/gateway"
	"2droundserver/internal/store"
)

var festivalIconIDs = [...]int64{13, 15, 801, 806, 908, 909, 1101, 1102, 609, 610, 611, 612, 613, 614, 615, 616}

var festivalLoginIcons = map[int64]int64{991011: 908, 1011: 909, 991024: 1101, 991025: 1102}

type festivalSchedule struct {
	icons      map[int64]bool
	returnCamp *store.Activity
}

func festivalScheduleOf(entries []actEntry) festivalSchedule {
	schedule := festivalSchedule{icons: make(map[int64]bool), returnCamp: activityForCamp(entries, retCamp)}
	for _, entry := range entries {
		switch icon := entry.Spec.CliType; icon {
		case 13, 15, 908, 909, 1101, 1102:
			schedule.icons[icon] = true
		case 0:
			if id, ok := festivalLoginIcons[entry.Row.BID]; ok {
				schedule.icons[id] = true
			}
		}
	}
	return schedule
}

func (schedule festivalSchedule) returning(state *retRole, now time.Time) bool {
	if schedule.returnCamp == nil || state == nil {
		return false
	}
	_, _, returning := state.availability(schedule.returnCamp.StartAt, schedule.returnCamp.EndAt, now)
	return returning
}

func warIconPeriod(now time.Time) int64 {
	period, _, _, _ := warNow(now)
	if warOrderCfg[period] == nil {
		return 0
	}
	return period
}

type festivalIcons struct {
	schedule  festivalSchedule
	returning bool
	war       int64
}

func (icons festivalIcons) open(id int64) bool {
	switch {
	case id == 801 || id == 806:
		return icons.returning
	case id >= 609 && id <= 616:
		return icons.war == id-606
	default:
		return icons.schedule.icons[id]
	}
}

func readFestivalIcons(st *store.Store, role *store.Role, now time.Time) (festivalIcons, error) {
	entries, err := openActivities(st, role.SrvID, now)
	if err != nil {
		return festivalIcons{}, err
	}
	schedule := festivalScheduleOf(entries)
	icons := festivalIcons{schedule: schedule, war: warIconPeriod(now)}
	if schedule.returnCamp != nil {
		state, err := loadReturnRole(st, role.RID)
		if err != nil {
			return festivalIcons{}, err
		}
		icons.returning = schedule.returning(state, now)
	}
	return icons, nil
}

func pushFestivalIconChanges(session *gateway.Session, previous, current festivalIcons) error {
	for _, open := range []bool{false, true} {
		for _, id := range festivalIconIDs {
			if current.open(id) == open && previous.open(id) != open {
				if err := session.Send(10923, activityIcon(id, b2i(open))); err != nil {
					return err
				}
			}
		}
	}
	return nil
}
