package main

import (
	"crypto/rand"
	"encoding/base32"
	"flag"
	"log"
	"net/http"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"

	"2droundserver/internal/admin"
	"2droundserver/internal/config"
	"2droundserver/internal/store"
)

func main() {
	cfgPath := flag.String("config", "configs/dev.yaml", "config file")
	flag.Parse()
	cfg, err := config.Load(*cfgPath)
	if err != nil {
		log.Fatal(err)
	}
	if len(cfg.Admin.JWTSecret) < 32 || strings.Contains(strings.ToLower(cfg.Admin.JWTSecret), "change-me") {
		log.Fatal("admin.jwt_secret must be a private random string of at least 32 characters (it signs every admin login)")
	}
	st, err := store.Open(cfg.DBPath)
	if err != nil {
		log.Fatal(err)
	}
	defer st.Close()
	logOut, closeLog, err := cfg.OpenLog("admin")
	if err != nil {
		log.Fatal(err)
	}
	defer closeLog()
	logger := log.New(logOut, "", log.LstdFlags|log.Lmicroseconds)
	bootstrapAdmin(st, logger)

	gw := cfg.Admin.GatewayURL
	if gw == "" && cfg.Gateway.AdminAddr != "" {
		gw = "http://" + cfg.Gateway.AdminAddr
	}
	s := &admin.Server{Store: st, Secret: []byte(cfg.Admin.JWTSecret), TTL: time.Duration(cfg.Admin.TTLHours) * time.Hour, GatewayURL: gw, Log: logger,
		BackupDir: cfg.BackupDirPath(), BackupKeep: cfg.Admin.BackupKeep}
	go autoBackup(st, cfg.BackupDirPath(), cfg.Admin.BackupKeep, time.Duration(cfg.Admin.BackupEveryHours)*time.Hour, logger)
	srv := &http.Server{
		Addr: cfg.Admin.Addr, Handler: s.Handler(), MaxHeaderBytes: 1 << 16,
		ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second, WriteTimeout: 30 * time.Second, IdleTimeout: 60 * time.Second,
	}
	logger.Printf("admin API listening on %s (gateway control: %q)", cfg.Admin.Addr, gw)
	log.Fatal(srv.ListenAndServe())
}

// bootstrapAdmin creates the first superadmin when there is none. The generated
// password is printed once and stored only as a bcrypt hash.
func bootstrapAdmin(st *store.Store, logger *log.Logger) {
	n, err := st.CountAdmins()
	if err != nil {
		log.Fatal(err)
	}
	if n > 0 {
		return
	}
	var b [10]byte
	if _, err := rand.Read(b[:]); err != nil {
		log.Fatal(err)
	}
	pw := strings.ToLower(base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(b[:]))
	h, err := bcrypt.GenerateFromPassword([]byte(pw), bcrypt.DefaultCost)
	if err != nil {
		log.Fatal(err)
	}
	if _, err := st.CreateAdmin("admin", string(h), "superadmin"); err != nil {
		log.Fatal(err)
	}
	logger.Printf("first start: created superadmin %q with password %q. It is shown only now; change it after logging in.", "admin", pw)
}

// autoBackup makes a database backup when the newest one is older than every (also right
// at the start), then keeps checking once a minute. every <= 0 turns it off.
func autoBackup(st *store.Store, dir string, keep int, every time.Duration, logger *log.Logger) {
	if every <= 0 {
		logger.Printf("automatic backups are off (admin.backup_every_hours is 0)")
		return
	}
	for {
		if store.BackupDue(dir, every, time.Now()) {
			path, pruned, err := st.BackupNow(dir, keep)
			if err != nil {
				logger.Printf("backup failed: %v", err)
			} else {
				logger.Printf("backup written: %s (%d old ones removed)", path, pruned)
			}
		}
		time.Sleep(time.Minute)
	}
}
