// adminfixture seeds only a disposable directory created by admin-api-sandbox.sh.
package main

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"

	"2droundserver/internal/config"
	"2droundserver/internal/store"
	"golang.org/x/crypto/bcrypt"
)

func main() {
	dir := flag.String("dir", "", "disposable sandbox directory")
	flag.Parse()
	root, err := filepath.Abs(*dir)
	must(err)
	marker, err := os.ReadFile(filepath.Join(root, ".admin-ui-sandbox"))
	must(err)
	if string(marker) != "2dround-admin-sandbox\n" {
		log.Fatal("not an admin UI sandbox")
	}
	cfg, err := config.Load(filepath.Join(root, "config.yaml"))
	must(err)
	if filepath.Clean(cfg.DBPath) != filepath.Join(root, "admin.db") {
		log.Fatal("database must be inside the sandbox")
	}
	st, err := store.Open(cfg.DBPath)
	must(err)
	defer st.Close()
	count, err := st.CountAdmins()
	must(err)
	if count != 0 {
		log.Fatal("fixture requires a new, empty admin database")
	}
	users := map[string]map[string]any{}
	for _, role := range []string{"viewer", "operator", "superadmin"} {
		b := make([]byte, 18)
		_, err := rand.Read(b)
		must(err)
		password := hex.EncodeToString(b)
		hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
		must(err)
		a, err := st.CreateAdmin("ui_"+role, string(hash), role)
		must(err)
		users[role] = map[string]any{"username": a.Username, "password": password, "id": a.ID}
	}
	for i := 1; i <= 2; i++ {
		row := store.Server{SrvID: fmt.Sprintf("uitest_%d", i), Name: fmt.Sprintf("后台测试服%d", i), Platform: "uitest", ZoneID: i, Host: "127.0.0.1", Port: 18800 + i, Status: 1, GroupID: 1}
		if i == 1 && cfg.Gateway.SrvID == row.SrvID {
			live := cfg.DefaultServer()
			live.Name = row.Name
			live.Platform = row.Platform
			row = live
		}
		must(st.UpsertServer(row))
	}
	var rid int64
	for i := 1; i <= 31; i++ {
		a, err := st.CreateAccount("uitest", fmt.Sprintf("ui_account_%02d", i), "", "")
		must(err)
		srv := "uitest_1"
		if i > 28 {
			srv = "uitest_2"
		}
		r, err := st.CreateRole(a.ID, srv, fmt.Sprintf("测试角色%02d", i), 1, 1, 10401)
		must(err)
		if i == 1 {
			rid = r.RID
		}
	}
	out, err := json.Marshal(map[string]any{"users": users, "rid": rid, "api": "http://" + cfg.Admin.Addr})
	must(err)
	must(os.WriteFile(filepath.Join(root, "fixture.json"), out, 0600))
	fmt.Printf("Seeded disposable fixtures: three admin roles and 31 test roles; credentials file: %s\n", filepath.Join(root, "fixture.json"))
}
func must(err error) {
	if err != nil {
		log.Fatal(err)
	}
}
