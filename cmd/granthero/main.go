// granthero gives heroes to a role straight from the command line, for development:
// it is for trying fights and hero screens with more than the starter hero when the
// summon screen cannot be used. It goes through Store.Summon with nothing to pay, so
// the heroes are made exactly like summoned ones. The gateway does not need to be
// stopped; a role that is online sees them after it reopens the hero list or logs in again.
//
//	granthero -config configs/lan.yaml -rid 1 -hero 10501:5 -hero 20402:5 -hero 30506:5
package main

import (
	"flag"
	"fmt"
	"log"
	"strconv"
	"strings"

	"2droundserver/internal/config"
	"2droundserver/internal/store"
)

type multi []string

func (m *multi) String() string     { return strings.Join(*m, ",") }
func (m *multi) Set(v string) error { *m = append(*m, v); return nil }

func main() {
	cfgPath := flag.String("config", "configs/dev.yaml", "config file")
	rid := flag.Int64("rid", 0, "role id (required)")
	var heroes multi
	flag.Var(&heroes, "hero", "bid:star, e.g. 10501:5 (repeatable)")
	flag.Parse()
	if *rid == 0 || len(heroes) == 0 {
		log.Fatal("-rid and at least one -hero bid:star are required")
	}
	var seeds []store.PartnerSeed
	for _, h := range heroes {
		parts := strings.Split(h, ":")
		if len(parts) != 2 {
			log.Fatalf("bad -hero %q, want bid:star", h)
		}
		bid, err1 := strconv.ParseInt(parts[0], 10, 64)
		star, err2 := strconv.Atoi(parts[1])
		if err1 != nil || err2 != nil || bid <= 0 || star < 1 {
			log.Fatalf("bad -hero %q, want bid:star", h)
		}
		seeds = append(seeds, store.PartnerSeed{BID: bid, Star: star})
	}
	cfg, err := config.Load(*cfgPath)
	if err != nil {
		log.Fatal(err)
	}
	st, err := store.Open(cfg.DBPath)
	if err != nil {
		log.Fatal(err)
	}
	defer st.Close()
	if _, err := st.RoleByID(*rid); err != nil {
		log.Fatalf("role %d: %v", *rid, err)
	}
	got, err := st.Summon(*rid, store.SummonResult{Seeds: seeds})
	if err != nil {
		log.Fatal(err)
	}
	for _, p := range got {
		fmt.Printf("hero %d: bid %d star %d\n", p.PartnerID, p.BID, p.Star)
	}
}
