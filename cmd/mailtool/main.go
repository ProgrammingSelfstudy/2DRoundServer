// mailtool sends a mail from the command line (until the admin web exists).
//
//	mailtool -rid 1,2 -subject Hi -content Welcome -asset gold=100 -item 40112=2
//	mailtool -all -subject Maintenance -content "..." -expire-days 7
//	mailtool -srv symlf_1 -subject ... -content ...
package main

import (
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"2droundserver/internal/config"
	"2droundserver/internal/game"
	"2droundserver/internal/store"
)

type multi []string

func (m *multi) String() string     { return strings.Join(*m, ",") }
func (m *multi) Set(v string) error { *m = append(*m, v); return nil }

func main() {
	cfgPath := flag.String("config", "configs/dev.yaml", "config file")
	rids := flag.String("rid", "", "comma separated role ids (direct mail)")
	all := flag.Bool("all", false, "broadcast to every server")
	srv := flag.String("srv", "", "broadcast to one server (srv_id)")
	subject := flag.String("subject", "", "subject (required)")
	content := flag.String("content", "", "content")
	from := flag.String("from", "GM", "sender name")
	typ := flag.Int("type", 1, "client mail type (the client lists 1 and 3)")
	expire := flag.Int("expire-days", 0, "days until the mail expires (0 = never)")
	push := flag.String("push", "", "gateway control URL to push to online roles (default: from the config)")
	var assets, items multi
	flag.Var(&assets, "asset", "currency attachment name=amount, e.g. gold=100 (repeatable)")
	flag.Var(&items, "item", "item attachment baseId=count (repeatable)")
	flag.Parse()
	if *subject == "" {
		log.Fatal("-subject is required")
	}
	cfg, err := config.Load(*cfgPath)
	if err != nil {
		log.Fatal(err)
	}
	m := store.Mail{Subject: *subject, Content: *content, FromName: *from, Type: *typ, CreatedBy: "mailtool"}
	if *expire > 0 {
		m.ExpireAt = time.Now().Add(time.Duration(*expire) * 24 * time.Hour).Unix()
	}
	for _, a := range assets {
		name, num, ok := split(a)
		id, known := game.AssetLabelID(name)
		if !ok || !known {
			names := game.AssetLabelNames()
			sort.Strings(names)
			log.Fatalf("bad -asset %q; names: %s", a, strings.Join(names, " "))
		}
		m.Attachments = append(m.Attachments, store.Attachment{Kind: store.AttachAsset, ItemID: id, Num: num})
	}
	for _, it := range items {
		idStr, num, ok := split(it)
		id, err := strconv.ParseInt(idStr, 10, 64)
		if !ok || err != nil {
			log.Fatalf("bad -item %q (want baseId=count)", it)
		}
		m.Attachments = append(m.Attachments, store.Attachment{Kind: store.AttachItem, ItemID: id, Num: num})
	}
	var recipients []int64
	switch {
	case *rids != "":
		for _, f := range strings.Split(*rids, ",") {
			n, err := strconv.ParseInt(strings.TrimSpace(f), 10, 64)
			if err != nil {
				log.Fatalf("bad role id %q", f)
			}
			recipients = append(recipients, n)
		}
	case *all || *srv != "":
		m.Scope, m.SrvID = store.MailBroadcast, *srv
	default:
		log.Fatal("choose recipients: -rid, -srv or -all")
	}
	st, err := store.Open(cfg.DBPath)
	if err != nil {
		log.Fatal(err)
	}
	defer st.Close()
	created, err := st.CreateMail(m, recipients)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("mail %d created (%s)\n", created.ID, created.Scope)
	url := *push
	if url == "" && cfg.Gateway.AdminAddr != "" {
		url = "http://" + cfg.Gateway.AdminAddr
	}
	if url == "" {
		return
	}
	resp, err := http.Post(fmt.Sprintf("%s/push/mail?id=%d", url, created.ID), "", nil)
	if err != nil {
		fmt.Fprintf(os.Stderr, "stored, but the gateway could not be reached for the live push: %v\n", err)
		return
	}
	defer resp.Body.Close()
	fmt.Println("live push:", resp.Status)
}

func split(s string) (string, int64, bool) {
	k, v, ok := strings.Cut(s, "=")
	n, err := strconv.ParseInt(v, 10, 64)
	return k, n, ok && err == nil && n > 0
}
