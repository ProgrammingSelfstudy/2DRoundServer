package main

import (
	"flag"
	"log"
	"net/http"

	"2droundserver/internal/config"
	"2droundserver/internal/login"
	"2droundserver/internal/store"
)

func main() {
	cfgPath := flag.String("config", "configs/dev.yaml", "config file")
	flag.Parse()
	cfg, err := config.Load(*cfgPath)
	if err != nil {
		log.Fatal(err)
	}
	st, err := store.Open(cfg.DBPath)
	if err != nil {
		log.Fatal(err)
	}
	defer st.Close()
	if err := st.EnsureServer(cfg.DefaultServer()); err != nil {
		log.Fatal(err)
	}
	logOut, closeLog, err := cfg.OpenLog("loginhttp")
	if err != nil {
		log.Fatal(err)
	}
	defer closeLog()
	s := &login.Server{Store: st, Cfg: cfg, Log: log.New(logOut, "", log.LstdFlags|log.Lmicroseconds)}
	s.Log.Printf("login http listening on %s", cfg.LoginHTTP.Addr)
	log.Fatal(http.ListenAndServe(cfg.LoginHTTP.Addr, s.Handler()))
}
