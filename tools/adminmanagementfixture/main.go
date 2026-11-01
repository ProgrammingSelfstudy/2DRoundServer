// Adds repeatable example management records to a marked disposable admin sandbox.
package main

import (
	"2droundserver/internal/config"
	"2droundserver/internal/store"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"
)

func must(e error) {
	if e != nil {
		log.Fatal(e)
	}
}
func main() {
	dir := flag.String("dir", "", "marked disposable directory")
	flag.Parse()
	root, e := filepath.Abs(*dir)
	must(e)
	b, e := os.ReadFile(filepath.Join(root, ".admin-ui-sandbox"))
	must(e)
	if string(b) != "2dround-admin-sandbox\n" {
		log.Fatal("sandbox marker required")
	}
	cfg, e := config.Load(filepath.Join(root, "config.yaml"))
	must(e)
	if filepath.Clean(cfg.DBPath) != filepath.Join(root, "admin.db") {
		log.Fatal("database must be isolated")
	}
	st, e := store.Open(cfg.DBPath)
	must(e)
	defer st.Close()
	b, e = os.ReadFile(filepath.Join(root, "fixture.json"))
	must(e)
	var fixture map[string]any
	must(json.Unmarshal(b, &fixture))
	now := time.Now().Unix()
	ids := map[string]int64{}
	for i := 0; i < 2; i++ {
		id, e := st.FileTicket(store.Ticket{RID: int64(i + 1), SrvID: "uitest_1", IssueType: 1, Title: fmt.Sprintf("隔离测试工单%d", i+1), Phone: "测试未提供", Email: "", PhoneInfo: "测试设备"}, "测试问题：请协助核对奖励，不涉及真实玩家", now)
		must(e)
		if i == 0 {
			must(st.AnswerTicket(id, "历史回复：正在核对", now))
			must(st.AskTicket(int64(i+1), id, "追问：请告知处理进展", now+1))
		}
		ids[fmt.Sprintf("ticket%d", i+1)] = id
	}
	for i := 0; i < 2; i++ {
		rid := int64(25 + i)
		g, _, e := st.GuildOfRole(rid)
		if e == store.ErrNotFound {
			gid, err := st.CreateGuild(store.Guild{SrvID: "uitest_1", Name: fmt.Sprintf("隔离测试公会%d", i+1), LeaderRID: rid, ApplyLev: 1}, now, nil, nil)
			must(err)
			ids[fmt.Sprintf("guild%d", i+1)] = gid
		} else {
			must(e)
			ids[fmt.Sprintf("guild%d", i+1)] = g.GID
		}
	}
	must(st.AddReport(store.Report{RID: 1, TargetRID: 2, TargetSrvID: "uitest_1", Kind: 1, Msg: "隔离测试举报：展示完整举报正文，不涉及真实玩家"}))
	fixture["management"] = ids
	b, e = json.Marshal(fixture)
	must(e)
	must(os.WriteFile(filepath.Join(root, "fixture.json"), b, 0600))
	fmt.Println("Seeded isolated tickets, follow-up, report and two guilds; credentials unchanged")
}
