package game

import (
	"sort"
	"time"

	"2droundserver/internal/store"
)

type eliteWorld struct {
	Done int64 // half seasons settled
}

// settleSeasons pays the half seasons that have ended since the last time and starts the new season after the second half. A server
// seen for the first time has no history to pay: it only notes where it stands.
func (e *eliteSvc) settleSeasons(srv string, now time.Time) error {
	period, phase, _, _, _ := eliteSeason(now, eliteOpenAt(e.st, srv, now))
	ended := 2 * (period - 1)
	if phase >= 2 {
		ended++
	}
	w := &eliteWorld{}
	found, err := e.st.LoadGlobal(srv, eliteModule, w)
	if err != nil {
		return err
	}
	if !found {
		w.Done = ended
		return e.st.SaveGlobal(srv, eliteModule, w)
	}
	if w.Done >= ended {
		return nil
	}
	for w.Done < ended {
		w.Done++
		if err := e.settleHalf(srv, w.Done); err != nil {
			return err
		}
	}
	return e.st.SaveGlobal(srv, eliteModule, w)
}

// settleHalf pays the rank rewards of the k-th half season (the odd are first halves) to the roles of the server that stand
// in the first places, by rank (higher rank first, then score) and with the rank the table asks; at the end of a season
// (an even k) every rank goes back to the one the table maps it to, with the rewards of the old one open again.
func (e *eliteSvc) settleHalf(srv string, k int64) error {
	roles, err := e.st.RankRoles(srv)
	if err != nil {
		return err
	}
	type row struct {
		rid int64
		er  *eliteRole
	}
	var rows []row
	for _, r := range roles {
		er := &eliteRole{}
		found, err := e.st.LoadState(r.RID, eliteModule, er)
		if err != nil {
			return err
		}
		if found && er.Lev > 0 {
			rows = append(rows, row{r.RID, er})
		}
	}
	sort.SliceStable(rows, func(i, j int) bool {
		a, b := rows[i].er, rows[j].er
		if a.Lev != b.Lev {
			return a.Lev > b.Lev
		}
		if a.Score != b.Score {
			return a.Score > b.Score
		}
		return rows[i].rid < rows[j].rid
	})
	season := (k + 1) / 2
	half := "上半赛季"
	if k%2 == 0 {
		half = "下半赛季"
	}
	for i, x := range rows {
		place := int64(i + 1)
		for _, t := range eliteCfg.RankReward {
			if place >= t.Min && place <= t.Max && x.er.Lev >= t.Limit {
				m := store.Mail{SrvID: srv, Subject: "超凡段位赛" + half + "结算奖励", FromName: "超凡段位赛", CreatedBy: "elitematch",
					Content:     "超凡段位赛第" + itoa64(season) + "赛季" + half + "已结束，你排在第" + itoa64(place) + "名，这是排名奖励。",
					Attachments: attachmentsOf(t.Award)}
				if _, err := e.st.CreateMail(m, []int64{x.rid}); err != nil {
					return err
				}
				break
			}
		}
	}
	if k%2 != 0 {
		return nil
	}
	for _, x := range rows {
		next := eliteCfg.Reset[itoa64(x.er.Lev)]
		if next == 0 {
			next = 1
		}
		x.er.Lev, x.er.Score = next, eliteCfg.level(next).InitExp
		x.er.Claimed, x.er.Streak, x.er.Period = nil, 0, season+1
		if err := e.st.SaveState(x.rid, eliteModule, x.er); err != nil {
			return err
		}
	}
	return nil
}
