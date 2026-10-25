package admin

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"golang.org/x/crypto/bcrypt"

	"2droundserver/internal/game"
	"2droundserver/internal/store"
)

// ---- dashboard ----

func (s *Server) stats(w http.ResponseWriter, r *http.Request, p *principal) {
	st, err := s.Store.Stats()
	if err != nil {
		internalErr(w, s, err)
		return
	}
	writeJSON(w, 200, map[string]int{"accounts": st.Accounts, "roles": st.Roles, "servers": st.Servers, "mails": st.Mails, "notices": st.Notices})
}

func (s *Server) online(w http.ResponseWriter, r *http.Request, p *principal) {
	var out struct {
		Online int `json:"online"`
	}
	if err := s.gateway("GET", "/online", &out); err != nil {
		writeJSON(w, 200, map[string]any{"online": nil, "error": err.Error()}) // the gateway being down is data, not a failure
		return
	}
	writeJSON(w, 200, out)
}

// meta gives the forms what they need to offer choices.
func (s *Server) meta(w http.ResponseWriter, r *http.Request, p *principal) {
	names := game.AssetLabelNames()
	sort.Strings(names)
	assets := make([]map[string]any, 0, len(names))
	for _, n := range names {
		id, _ := game.AssetLabelID(n)
		assets = append(assets, map[string]any{"name": n, "label": id})
	}
	writeJSON(w, 200, map[string]any{"assets": assets, "heroes": game.HeroOptions(), "skins": game.SkinChoices(), "roles": []string{"viewer", "operator", "superadmin"}, "mail_types": []int{1, 3}})
}

// ---- admins ----

func (s *Server) listAdmins(w http.ResponseWriter, r *http.Request, p *principal) {
	list, err := s.Store.ListAdmins()
	if err != nil {
		internalErr(w, s, err)
		return
	}
	writeJSON(w, 200, map[string]any{"items": nonNil(list)})
}

func (s *Server) createAdmin(w http.ResponseWriter, r *http.Request, p *principal) {
	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
		Role     string `json:"role"`
	}
	if !decode(w, r, &req) {
		return
	}
	if !reAdminName.MatchString(req.Username) {
		fail(w, 422, "username must be 3 to 32 characters of letters, digits, _ . -")
		return
	}
	if !validPassword(req.Password) {
		fail(w, 422, "the password must be 8 to 72 characters")
		return
	}
	if _, ok := roleLevel[req.Role]; !ok {
		fail(w, 422, "role must be viewer, operator or superadmin")
		return
	}
	h, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		internalErr(w, s, err)
		return
	}
	a, err := s.Store.CreateAdmin(req.Username, string(h), req.Role)
	if errors.Is(err, store.ErrConflict) {
		fail(w, 409, "that username is taken")
		return
	}
	if err != nil {
		internalErr(w, s, err)
		return
	}
	s.audit(r, p, "admin.create", a.Username, map[string]any{"id": a.ID, "role": a.Role})
	writeJSON(w, 201, a)
}

func (s *Server) updateAdmin(w http.ResponseWriter, r *http.Request, p *principal) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	var req struct {
		Role     *string `json:"role"`
		Status   *int    `json:"status"`
		Password *string `json:"password"`
	}
	if !decode(w, r, &req) {
		return
	}
	target, err := s.Store.AdminByID(id)
	if errors.Is(err, store.ErrNotFound) {
		fail(w, 404, "no such admin")
		return
	}
	if err != nil {
		internalErr(w, s, err)
		return
	}
	role, status := "", -1
	if req.Role != nil {
		if _, ok := roleLevel[*req.Role]; !ok {
			fail(w, 422, "role must be viewer, operator or superadmin")
			return
		}
		role = *req.Role
	}
	if req.Status != nil {
		if *req.Status != 0 && *req.Status != 1 {
			fail(w, 422, "status must be 0 or 1")
			return
		}
		status = *req.Status
	}
	if req.Password != nil && !validPassword(*req.Password) {
		fail(w, 422, "the password must be 8 to 72 characters")
		return
	}
	demotes := (role != "" && role != "superadmin") || status == 0
	if id == p.Admin.ID && demotes {
		fail(w, 409, "you cannot lower or disable your own account")
		return
	}
	if target.Role == "superadmin" && target.Status == 1 && demotes {
		if n, err := s.Store.CountActiveSuperadmins(); err != nil {
			internalErr(w, s, err)
			return
		} else if n <= 1 {
			fail(w, 409, "at least one active superadmin must remain")
			return
		}
	}
	if err := s.Store.UpdateAdmin(id, role, status); err != nil {
		internalErr(w, s, err)
		return
	}
	detail := map[string]any{"id": id}
	if role != "" {
		detail["role"] = role
	}
	if status >= 0 {
		detail["status"] = status
	}
	if req.Password != nil {
		h, err := bcrypt.GenerateFromPassword([]byte(*req.Password), bcrypt.DefaultCost)
		if err != nil {
			internalErr(w, s, err)
			return
		}
		if err := s.Store.SetAdminPassword(id, string(h)); err != nil {
			internalErr(w, s, err)
			return
		}
		detail["password_reset"] = true // the password itself is never logged
	}
	s.audit(r, p, "admin.update", target.Username, detail)
	a, _ := s.Store.AdminByID(id)
	writeJSON(w, 200, a)
}

// ---- servers ----

var reSrvID = regexp.MustCompile(`^[A-Za-z0-9]{1,16}_[0-9]{1,6}$`)

type serverReq struct {
	SrvID       string `json:"srv_id"`
	Name        string `json:"name"`
	Platform    string `json:"platform"`
	ZoneID      int    `json:"zone_id"`
	Host        string `json:"host"`
	Port        int    `json:"port"`
	WSURL       string `json:"ws_url"`
	Status      int    `json:"status"`
	IsNew       bool   `json:"is_new"`
	IsRecommend bool   `json:"is_recommend"`
	OpenTime    int64  `json:"open_time"`
	MainSrvID   string `json:"main_srv_id"`
	GroupID     int    `json:"group_id"`
}

// validate returns the problem with the request, or "".
func (s *Server) validateServer(q *serverReq) string {
	switch {
	case !reSrvID.MatchString(q.SrvID):
		return "srv_id must look like symlf_1"
	case strings.TrimSpace(q.Name) == "" || utf8.RuneCountInString(q.Name) > 32:
		return "name is required, at most 32 characters"
	case q.Platform == "" || len(q.Platform) > 16:
		return "platform is required"
	case strings.TrimSpace(q.Host) == "" || len(q.Host) > 255:
		return "host is required"
	case q.Port < 1 || q.Port > 65535:
		return "port must be 1 to 65535"
	case q.Status != 0 && q.Status != 1:
		return "status must be 0 (maintenance) or 1 (open)"
	case q.OpenTime < 0:
		return "open_time cannot be negative"
	case q.GroupID < 1:
		return "group_id must be at least 1"
	case q.ZoneID < 1:
		return "zone_id must be at least 1"
	}
	if q.MainSrvID != "" && q.MainSrvID != q.SrvID {
		if _, err := s.Store.Server(q.MainSrvID); err != nil {
			return "main_srv_id must be an existing server"
		}
	}
	return ""
}

func (q *serverReq) store(created int64) store.Server {
	return store.Server{SrvID: q.SrvID, Name: q.Name, Platform: q.Platform, ZoneID: q.ZoneID, Host: q.Host, Port: q.Port, WSURL: q.WSURL,
		Status: q.Status, IsNew: q.IsNew, IsRecommend: q.IsRecommend, OpenTime: q.OpenTime, MainSrvID: q.MainSrvID, GroupID: q.GroupID, CreatedAt: created}
}

func (s *Server) listServers(w http.ResponseWriter, r *http.Request, p *principal) {
	list, err := s.Store.ListServers()
	if err != nil {
		internalErr(w, s, err)
		return
	}
	writeJSON(w, 200, map[string]any{"items": nonNil(list)})
}

func (s *Server) createServer(w http.ResponseWriter, r *http.Request, p *principal) {
	var q serverReq
	if !decode(w, r, &q) {
		return
	}
	if msg := s.validateServer(&q); msg != "" {
		fail(w, 422, msg)
		return
	}
	if _, err := s.Store.Server(q.SrvID); err == nil {
		fail(w, 409, "that srv_id already exists")
		return
	}
	if err := s.Store.UpsertServer(q.store(0)); err != nil {
		internalErr(w, s, err)
		return
	}
	s.audit(r, p, "server.create", q.SrvID, q)
	v, _ := s.Store.Server(q.SrvID)
	writeJSON(w, 201, v)
}

func (s *Server) updateServer(w http.ResponseWriter, r *http.Request, p *principal) {
	var q serverReq
	if !decode(w, r, &q) {
		return
	}
	if q.SrvID == "" {
		q.SrvID = r.PathValue("id")
	}
	if q.SrvID != r.PathValue("id") {
		fail(w, 422, "srv_id in the body does not match the URL")
		return
	}
	old, err := s.Store.Server(q.SrvID)
	if errors.Is(err, store.ErrNotFound) {
		fail(w, 404, "no such server")
		return
	}
	if err != nil {
		internalErr(w, s, err)
		return
	}
	if msg := s.validateServer(&q); msg != "" {
		fail(w, 422, msg)
		return
	}
	if err := s.Store.UpsertServer(q.store(old.CreatedAt)); err != nil {
		internalErr(w, s, err)
		return
	}
	s.audit(r, p, "server.update", q.SrvID, map[string]any{"before": old, "after": q})
	v, _ := s.Store.Server(q.SrvID)
	writeJSON(w, 200, v)
}

func (s *Server) deleteServer(w http.ResponseWriter, r *http.Request, p *principal) {
	id := r.PathValue("id")
	err := s.Store.DeleteServer(id)
	switch {
	case errors.Is(err, store.ErrNotFound):
		fail(w, 404, "no such server")
	case errors.Is(err, store.ErrConflict):
		fail(w, 409, "the server still has roles; put it into maintenance instead")
	case err != nil:
		internalErr(w, s, err)
	default:
		s.audit(r, p, "server.delete", id, nil)
		writeJSON(w, 200, map[string]bool{"ok": true})
	}
}

// ---- accounts and roles ----

func (s *Server) listAccounts(w http.ResponseWriter, r *http.Request, p *principal) {
	limit, offset := page(r)
	rows, total, err := s.Store.ListAccountsAdmin(r.URL.Query().Get("q"), limit, offset)
	if err != nil {
		internalErr(w, s, err)
		return
	}
	writeJSON(w, 200, map[string]any{"items": nonNil(rows), "total": total})
}

func (s *Server) kick(rid int64) {
	if err := s.gateway("POST", "/kick?rid="+strconv.FormatInt(rid, 10), nil); err != nil {
		s.Log.Printf("admin: kick of role %d failed (the ban holds, the player stays online until they reconnect): %v", rid, err)
	}
}

func (s *Server) banAccount(w http.ResponseWriter, r *http.Request, p *principal) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	var req struct {
		Reason string `json:"reason"`
	}
	if !decode(w, r, &req) {
		return
	}
	if utf8.RuneCountInString(req.Reason) > 200 {
		fail(w, 422, "reason is at most 200 characters")
		return
	}
	acc, err := s.Store.AccountByID(id)
	if errors.Is(err, store.ErrNotFound) {
		fail(w, 404, "no such account")
		return
	}
	if err != nil {
		internalErr(w, s, err)
		return
	}
	if err := s.Store.SetAccountStatus(id, 0, req.Reason); err != nil {
		internalErr(w, s, err)
		return
	}
	rids, _ := s.Store.RoleIDsOfAccount(id)
	for _, rid := range rids {
		s.kick(rid)
	}
	s.audit(r, p, "account.ban", acc.Account, map[string]any{"id": id, "reason": req.Reason, "roles_kicked": len(rids)})
	writeJSON(w, 200, map[string]bool{"ok": true})
}

func (s *Server) unbanAccount(w http.ResponseWriter, r *http.Request, p *principal) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	acc, err := s.Store.AccountByID(id)
	if errors.Is(err, store.ErrNotFound) {
		fail(w, 404, "no such account")
		return
	}
	if err != nil {
		internalErr(w, s, err)
		return
	}
	if err := s.Store.SetAccountStatus(id, 1, ""); err != nil {
		internalErr(w, s, err)
		return
	}
	s.audit(r, p, "account.unban", acc.Account, map[string]any{"id": id})
	writeJSON(w, 200, map[string]bool{"ok": true})
}

func (s *Server) listRoles(w http.ResponseWriter, r *http.Request, p *principal) {
	limit, offset := page(r)
	rows, total, err := s.Store.ListRolesAdmin(r.URL.Query().Get("q"), r.URL.Query().Get("srv_id"), limit, offset)
	if err != nil {
		internalErr(w, s, err)
		return
	}
	writeJSON(w, 200, map[string]any{"items": nonNil(rows), "total": total})
}

// searchItems finds items of the client's item table by name or by id prefix, so the
// mail form can offer them by name.
func (s *Server) searchItems(w http.ResponseWriter, r *http.Request, p *principal) {
	limit := 30
	if n, err := strconv.Atoi(r.URL.Query().Get("limit")); err == nil && n > 0 && n <= 100 {
		limit = n
	}
	writeJSON(w, 200, map[string]any{"items": game.SearchItems(r.URL.Query().Get("q"), limit)})
}

// roleDetail is one role with everything the game holds for it: who owns it,
// whether it is online, its currencies and its items. Items are the ledger, so
// they include ones the game's backpack does not list (equipment, ...);
// "listed" says which are shown there.
func (s *Server) roleDetail(w http.ResponseWriter, r *http.Request, p *principal) {
	rid, ok := pathID(w, r)
	if !ok {
		return
	}
	role, err := s.Store.RoleByID(rid)
	if errors.Is(err, store.ErrNotFound) {
		fail(w, 404, "no such role")
		return
	}
	if err != nil {
		internalErr(w, s, err)
		return
	}
	acc, err := s.Store.AccountByID(role.AccountID)
	if err != nil {
		internalErr(w, s, err)
		return
	}
	ledger, err := s.Store.RoleAssets(rid)
	if err != nil {
		internalErr(w, s, err)
		return
	}
	held, err := s.Store.RoleItems(rid)
	if err != nil {
		internalErr(w, s, err)
		return
	}
	assets := []map[string]any{}
	for _, label := range sortedKeys(ledger) {
		name, _ := game.AssetName(label)
		assets = append(assets, map[string]any{"label": label, "name": name, "val": ledger[label]})
	}
	items := []map[string]any{}
	for _, id := range sortedKeys(held) {
		name, _ := game.ItemName(id)
		items = append(items, map[string]any{"base_id": id, "name": name, "num": held[id], "listed": game.ItemListedInBackpack(id)})
	}
	out := map[string]any{"role": role, "account": acc.Account, "banned": role.Status != 1, "assets": assets, "items": items, "online": nil}
	var on struct {
		Online bool `json:"online"`
	}
	if err := s.gateway("GET", "/online?rid="+strconv.FormatInt(rid, 10), &on); err != nil {
		out["online_error"] = err.Error() // the gateway being down is data, not a failure
	} else {
		out["online"] = on.Online
	}
	writeJSON(w, 200, out)
}

func (s *Server) listBackups(w http.ResponseWriter, r *http.Request, p *principal) {
	if s.BackupDir == "" {
		fail(w, http.StatusConflict, "backups are not configured")
		return
	}
	files, err := store.ListBackups(s.BackupDir)
	if err != nil {
		internalErr(w, s, err)
		return
	}
	writeJSON(w, 200, map[string]any{"items": nonNil(files), "keep": s.BackupKeep})
}

// makeBackup copies the live database into the backup directory and prunes old copies.
// The path is never taken from the request: a superadmin cannot make it write elsewhere.
func (s *Server) makeBackup(w http.ResponseWriter, r *http.Request, p *principal) {
	if s.BackupDir == "" {
		fail(w, http.StatusConflict, "backups are not configured")
		return
	}
	path, pruned, err := s.Store.BackupNow(s.BackupDir, s.BackupKeep)
	if err != nil {
		internalErr(w, s, err)
		return
	}
	info, _ := os.Stat(path)
	size := int64(0)
	if info != nil {
		size = info.Size()
	}
	s.audit(r, p, "backup.create", filepath.Base(path), map[string]any{"size": size, "pruned": pruned})
	writeJSON(w, 200, map[string]any{"ok": true, "name": filepath.Base(path), "size": size, "pruned": pruned})
}

// broadcast scrolls a message across the top of every online player's screen.
func (s *Server) broadcast(w http.ResponseWriter, r *http.Request, p *principal) {
	var q struct {
		Msg string `json:"msg"`
	}
	if !decode(w, r, &q) {
		return
	}
	msg := strings.TrimSpace(q.Msg)
	if msg == "" || utf8.RuneCountInString(msg) > 100 || strings.ContainsFunc(msg, unicode.IsControl) {
		fail(w, http.StatusUnprocessableEntity, "msg must be 1 to 100 characters without control characters")
		return
	}
	var out struct {
		Pushed int `json:"pushed"`
	}
	if err := s.gateway("POST", "/push/broadcast?msg="+url.QueryEscape(msg), &out); err != nil {
		fail(w, http.StatusBadGateway, "the gateway could not be reached: "+err.Error())
		return
	}
	s.audit(r, p, "broadcast", "", map[string]any{"msg": msg, "pushed": out.Pushed})
	writeJSON(w, 200, map[string]any{"ok": true, "pushed": out.Pushed})
}

// setRoleLevel sets a role's level. The client reads the level when it logs in (10301), so an
// online role only sees it after it reconnects; "kick" disconnects it right away for that.
func (s *Server) setRoleLevel(w http.ResponseWriter, r *http.Request, p *principal) {
	rid, ok := pathID(w, r)
	if !ok {
		return
	}
	var q struct {
		Lev  int  `json:"lev"`
		Kick bool `json:"kick"`
	}
	if !decode(w, r, &q) {
		return
	}
	if q.Lev < 1 || q.Lev > game.MaxRoleLevel() {
		fail(w, http.StatusUnprocessableEntity, fmt.Sprintf("lev must be 1 to %d", game.MaxRoleLevel()))
		return
	}
	role, err := s.Store.RoleByID(rid)
	if errors.Is(err, store.ErrNotFound) {
		fail(w, 404, "no such role")
		return
	}
	if err != nil {
		internalErr(w, s, err)
		return
	}
	if err := s.Store.SetRoleLevel(rid, q.Lev); err != nil {
		internalErr(w, s, err)
		return
	}
	if q.Kick {
		s.kick(rid)
	}
	s.audit(r, p, "role.level", role.Name, map[string]any{"rid": rid, "srv_id": role.SrvID, "from": role.Lev, "to": q.Lev, "kick": q.Kick})
	writeJSON(w, 200, map[string]any{"ok": true, "lev": q.Lev, "was": role.Lev})
}

// setRoleVIP sets the VIP level of a role (0 to game.MaxVIPLevel). The client reads it at
// login, like the level, so an online role sees the change after it reconnects.
func (s *Server) setRoleVIP(w http.ResponseWriter, r *http.Request, p *principal) {
	rid, ok := pathID(w, r)
	if !ok {
		return
	}
	var q struct {
		VIP  int  `json:"vip_lev"`
		Kick bool `json:"kick"`
	}
	if !decode(w, r, &q) {
		return
	}
	if q.VIP < 0 || q.VIP > game.MaxVIPLevel {
		fail(w, http.StatusUnprocessableEntity, fmt.Sprintf("vip_lev must be 0 to %d", game.MaxVIPLevel))
		return
	}
	role, err := s.Store.RoleByID(rid)
	if errors.Is(err, store.ErrNotFound) {
		fail(w, 404, "no such role")
		return
	}
	if err != nil {
		internalErr(w, s, err)
		return
	}
	if err := s.Store.SetRoleVIP(rid, q.VIP); err != nil {
		internalErr(w, s, err)
		return
	}
	if q.Kick {
		s.kick(rid)
	}
	s.audit(r, p, "role.vip", role.Name, map[string]any{"rid": rid, "srv_id": role.SrvID, "from": role.VIPLev, "to": q.VIP, "kick": q.Kick})
	writeJSON(w, 200, map[string]any{"ok": true, "vip_lev": q.VIP, "was": role.VIPLev})
}

// grantRole puts currencies and items straight into a role's ledgers, the same ones a claimed
// mail fills, without a mail. An online role gets its currencies and backpack resent at once.
func (s *Server) grantRole(w http.ResponseWriter, r *http.Request, p *principal) {
	rid, ok := pathID(w, r)
	if !ok {
		return
	}
	var q struct {
		Grants []attachmentReq `json:"grants"`
	}
	if !decode(w, r, &q) {
		return
	}
	if len(q.Grants) == 0 || len(q.Grants) > maxGrantItems {
		fail(w, http.StatusUnprocessableEntity, fmt.Sprintf("give 1 to %d grants", maxGrantItems))
		return
	}
	atts, msg := buildAttachments(q.Grants)
	if msg != "" {
		fail(w, http.StatusUnprocessableEntity, msg)
		return
	}
	role, err := s.Store.RoleByID(rid)
	if errors.Is(err, store.ErrNotFound) {
		fail(w, 404, "no such role")
		return
	}
	if err != nil {
		internalErr(w, s, err)
		return
	}
	assets, items := map[int64]int64{}, map[int64]int64{}
	for _, a := range atts {
		if a.Kind == store.AttachAsset {
			assets[a.ItemID] += a.Num
		} else {
			items[a.ItemID] += a.Num
		}
	}
	if err := s.Store.Exchange(rid, nil, nil, assets, items); err != nil {
		internalErr(w, s, err)
		return
	}
	resp := map[string]any{"ok": true, "granted": atts, "refreshed": nil}
	var out struct {
		Refreshed bool `json:"refreshed"`
	}
	if err := s.gateway("POST", fmt.Sprintf("/push/refresh?rid=%d", rid), &out); err != nil {
		resp["push_error"] = err.Error() // stored: the role sees it when it logs in
	} else {
		resp["refreshed"] = out.Refreshed
	}
	s.audit(r, p, "role.grant", role.Name, map[string]any{"rid": rid, "srv_id": role.SrvID, "grants": atts})
	writeJSON(w, 200, resp)
}

// grantHeroes gives a role new heroes straight away, made like summoned ones (level 1,
// the given star). An online role is sent them at once (11001).
func (s *Server) grantHeroes(w http.ResponseWriter, r *http.Request, p *principal) {
	rid, ok := pathID(w, r)
	if !ok {
		return
	}
	var q struct {
		Heroes []struct {
			BID  int64 `json:"bid"`
			Star int   `json:"star"`
			Num  int   `json:"num"`
		} `json:"heroes"`
	}
	if !decode(w, r, &q) {
		return
	}
	var seeds []store.PartnerSeed
	for _, h := range q.Heroes {
		if h.Num == 0 {
			h.Num = 1
		}
		if h.Num < 1 || h.Num > maxGrantHeroes {
			fail(w, http.StatusUnprocessableEntity, fmt.Sprintf("num must be 1 to %d", maxGrantHeroes))
			return
		}
		if !game.HeroStarKnown(h.BID, h.Star) {
			fail(w, http.StatusUnprocessableEntity, fmt.Sprintf("hero %d has no star %d", h.BID, h.Star))
			return
		}
		for i := 0; i < h.Num; i++ {
			seeds = append(seeds, store.PartnerSeed{BID: h.BID, Star: h.Star})
		}
	}
	if len(seeds) == 0 || len(seeds) > maxGrantHeroes {
		fail(w, http.StatusUnprocessableEntity, fmt.Sprintf("give 1 to %d heroes", maxGrantHeroes))
		return
	}
	role, err := s.Store.RoleByID(rid)
	if errors.Is(err, store.ErrNotFound) {
		fail(w, 404, "no such role")
		return
	}
	if err != nil {
		internalErr(w, s, err)
		return
	}
	got, err := s.Store.Summon(rid, store.SummonResult{Seeds: seeds})
	if err != nil {
		internalErr(w, s, err)
		return
	}
	ids := make([]string, 0, len(got))
	granted := make([]map[string]any, 0, len(got))
	for _, h := range got {
		ids = append(ids, strconv.FormatInt(h.PartnerID, 10))
		granted = append(granted, map[string]any{"partner_id": h.PartnerID, "bid": h.BID, "star": h.Star})
	}
	resp := map[string]any{"ok": true, "granted": granted, "refreshed": nil}
	var out struct {
		Refreshed bool `json:"refreshed"`
	}
	if err := s.gateway("POST", fmt.Sprintf("/push/refresh?rid=%d&heroes=%s", rid, strings.Join(ids, ",")), &out); err != nil {
		resp["push_error"] = err.Error() // stored: the role sees them when it logs in
	} else {
		resp["refreshed"] = out.Refreshed
	}
	s.audit(r, p, "role.heroes", role.Name, map[string]any{"rid": rid, "srv_id": role.SrvID, "heroes": granted})
	writeJSON(w, 200, resp)
}

// grantSkin gives a role a hero skin, for good or for `days` days. An online role is sent its
// skin list (11020) at once, which also opens the client's "new skin" window.
func (s *Server) grantSkin(w http.ResponseWriter, r *http.Request, p *principal) {
	rid, ok := pathID(w, r)
	if !ok {
		return
	}
	var q struct {
		SkinID int64 `json:"skin_id"`
		Days   int   `json:"days"`
	}
	if !decode(w, r, &q) {
		return
	}
	if !game.SkinKnown(q.SkinID) {
		fail(w, http.StatusUnprocessableEntity, fmt.Sprintf("no skin %d", q.SkinID))
		return
	}
	if q.Days < 0 || q.Days > maxSkinDays {
		fail(w, http.StatusUnprocessableEntity, fmt.Sprintf("days must be 0 (for good) to %d", maxSkinDays))
		return
	}
	role, err := s.Store.RoleByID(rid)
	if errors.Is(err, store.ErrNotFound) {
		fail(w, 404, "no such role")
		return
	}
	if err != nil {
		internalErr(w, s, err)
		return
	}
	var end int64
	if q.Days > 0 {
		end = time.Now().Add(time.Duration(q.Days) * 24 * time.Hour).Unix()
	}
	if err := s.Store.GrantSkin(rid, q.SkinID, end); err != nil {
		internalErr(w, s, err)
		return
	}
	resp := map[string]any{"ok": true, "skin_id": q.SkinID, "end_time": end, "refreshed": nil}
	var out struct {
		Refreshed bool `json:"refreshed"`
	}
	if err := s.gateway("POST", fmt.Sprintf("/push/refresh?rid=%d&skins=1", rid), &out); err != nil {
		resp["push_error"] = err.Error() // stored: the role sees it when it logs in
	} else {
		resp["refreshed"] = out.Refreshed
	}
	s.audit(r, p, "role.skin", role.Name, map[string]any{"rid": rid, "srv_id": role.SrvID, "skin_id": q.SkinID, "days": q.Days})
	writeJSON(w, 200, resp)
}

// kickRole disconnects an online role without banning it.
func (s *Server) kickRole(w http.ResponseWriter, r *http.Request, p *principal) {
	rid, ok := pathID(w, r)
	if !ok {
		return
	}
	role, err := s.Store.RoleByID(rid)
	if errors.Is(err, store.ErrNotFound) {
		fail(w, 404, "no such role")
		return
	}
	if err != nil {
		internalErr(w, s, err)
		return
	}
	var res struct {
		Kicked bool `json:"kicked"`
	}
	if err := s.gateway("POST", "/kick?rid="+strconv.FormatInt(rid, 10), &res); err != nil {
		fail(w, http.StatusBadGateway, "the gateway could not be reached: "+err.Error())
		return
	}
	s.audit(r, p, "role.kick", role.Name, map[string]any{"rid": rid, "srv_id": role.SrvID, "was_online": res.Kicked})
	writeJSON(w, 200, map[string]bool{"ok": true, "kicked": res.Kicked})
}

func sortedKeys(m map[int64]int64) []int64 {
	out := make([]int64, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

func (s *Server) setRole(w http.ResponseWriter, r *http.Request, p *principal, status int, action string) {
	rid, ok := pathID(w, r)
	if !ok {
		return
	}
	role, err := s.Store.RoleByID(rid)
	if errors.Is(err, store.ErrNotFound) {
		fail(w, 404, "no such role")
		return
	}
	if err != nil {
		internalErr(w, s, err)
		return
	}
	if err := s.Store.SetRoleStatus(rid, status); err != nil {
		internalErr(w, s, err)
		return
	}
	if status == 0 {
		s.kick(rid)
	}
	s.audit(r, p, action, role.Name, map[string]any{"rid": rid, "srv_id": role.SrvID})
	writeJSON(w, 200, map[string]bool{"ok": true})
}

func (s *Server) banRole(w http.ResponseWriter, r *http.Request, p *principal) {
	s.setRole(w, r, p, 0, "role.ban")
}
func (s *Server) unbanRole(w http.ResponseWriter, r *http.Request, p *principal) {
	s.setRole(w, r, p, 1, "role.unban")
}

// ---- audit ----

func (s *Server) listAudit(w http.ResponseWriter, r *http.Request, p *principal) {
	limit, offset := page(r)
	rows, total, err := s.Store.ListAudit(limit, offset)
	if err != nil {
		internalErr(w, s, err)
		return
	}
	writeJSON(w, 200, map[string]any{"items": nonNil(rows), "total": total})
}
