// Package login serves the HTTP side of the client's login flow: the remote
// url_config.lua, the server list and the token endpoint. Response shapes are
// taken from the recovered Lua (login_model.lua, login_controller.lua,
// file_update.lua); anything the client asks for beyond that is logged and
// answered 404 so the gap is visible.
package login

import (
	"errors"
	"fmt"
	"log"
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"golang.org/x/crypto/bcrypt"

	"2droundserver/internal/config"
	"2droundserver/internal/store"
)

type Server struct {
	Store *store.Store
	Cfg   *config.Config
	Log   *log.Logger
}

const platform = "symlf" // PLATFORM_NAME in the client's config.lua

func (s *Server) Handler() http.Handler {
	// A plain HandlerFunc (not ServeMux) so odd client paths are not redirected.
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.Log.Printf("http %s %s from %s", r.Method, r.URL.RequestURI(), r.RemoteAddr)
		// The client's URLs were patched in place at equal length, so they may
		// carry filler such as "////log" or "?&&&username=": match loosely.
		p := r.URL.Path
		switch {
		case strings.Contains(p, "/update/config/"):
			s.urlConfig(w, r)
		case p == "/api/serverlist":
			s.serverList(w, r)
		case strings.HasSuffix(p, "/userlogin/yxmmdasba"):
			s.makeToken(w, r)
		case strings.HasSuffix(p, "/userlogin/userLogins2"), strings.HasSuffix(p, "/userlogin/userLogins"):
			s.userLogin(w, r)
		case strings.HasSuffix(p, "/userlogin/userRegs"):
			s.userReg(w, r)
		case strings.HasSuffix(p, "/userlogin/ageverify"):
			fmt.Fprint(w, `{msg="200"}`)
		case strings.HasSuffix(p, "/userLogin"):
			fmt.Fprint(w, "mail binding is not available on this server")
		case strings.HasSuffix(p, "/log") || p == "/index.php" || p == "/" || p == "":
			// "/" answers the native XMLHttpRequest reachability probe
			fmt.Fprint(w, "ok")
		default:
			http.NotFound(w, r)
		}
	})
}

func luaStr(s string) string { return strconv.Quote(s) } // Go escapes are valid Lua for plain text

// urlConfig is loaded by the client with loadstring(...)() and must define
// URL_PATH_ALL (file_update.lua) and get_servers_url (login_model.lua).
func (s *Server) urlConfig(w http.ResponseWriter, r *http.Request) {
	base := s.Cfg.LoginHTTP.PublicBase
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	fmt.Fprintf(w, `URL_PATH_ALL = {
	get = function(_)
		return {update = %s, voice = %s}
	end
}
function get_servers_url(account, platform, channel, local_srv_id)
	return string.format("%s/api/serverlist?account=%%s&platform=%%s&channel=%%s&srv=%%s",
		tostring(account), tostring(platform), tostring(channel), tostring(local_srv_id)), nil
end
`, luaStr(base+"/update"), luaStr(base+"/voice"), base)
}

func luaBool(b bool) string {
	if b {
		return "true"
	}
	return "false"
}

// serverEntry renders one server in the shape login_model.lua reads
// (srv_id, zone_id, name, ip/host, port, is_maintain, is_new, is_recomed,
// open_time, group_id, main_*).
func serverEntry(v store.Server) string {
	main := v.MainSrvID
	if main == "" {
		main = v.SrvID
	}
	mainZone := v.ZoneID
	if i := strings.LastIndex(main, "_"); i >= 0 {
		if n, err := strconv.Atoi(main[i+1:]); err == nil {
			mainZone = n
		}
	}
	return fmt.Sprintf(`{srv_id=%s, platform=%s, zone_id=%d, name=%s, ip=%s, host=%s, port=%d, main_platform=%s, main_zone_id=%d, `+
		`is_maintain=%s, is_new=%s, is_recomed=%s, open_time=%d, game_status=0, group_id=%d}`,
		luaStr(v.SrvID), luaStr(v.Platform), v.ZoneID, luaStr(v.Name), luaStr(v.Host), luaStr(v.Host), v.Port,
		luaStr(v.Platform), mainZone, luaBool(v.Status == 0), luaBool(v.IsNew), luaBool(v.IsRecommend), v.OpenTime, v.GroupID)
}

// serverList answers in Lua-table syntax: the client runs
// loadstring("return " .. body) when NEW_SERVER_LIST is not set. The list comes
// from the servers table, so admin edits take effect immediately.
func (s *Server) serverList(w http.ResponseWriter, r *http.Request) {
	list, err := s.Store.ListServers()
	if err != nil {
		http.Error(w, "servers", http.StatusInternalServerError)
		return
	}
	if len(list) == 0 {
		list = []store.Server{s.Cfg.DefaultServer()}
	}
	def := list[0]
	for _, v := range list {
		if v.IsRecommend && v.Status == 1 {
			def = v
			break
		}
	}
	entries := make([]string, len(list))
	for i, v := range list {
		entries[i] = serverEntry(v)
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	fmt.Fprintf(w, `{code=200, data={default_zone=%s, server_list={%s}, roles={}, isWhite=false, sign=""}}`,
		serverEntry(def), strings.Join(entries, ", "))
}

// makeToken issues the token the client later puts into the 1164 args.
// The reply is a Lua table whose msg field is the token (login_controller.lua).
func (s *Server) makeToken(w http.ResponseWriter, r *http.Request) {
	user := r.URL.Query().Get("username")
	if user == "" {
		fmt.Fprint(w, `{code=0, msg="missing username"}`)
		return
	}
	acc, err := s.Store.FindOrCreateAccount(platform, user)
	if err != nil || acc.Status != 1 {
		fmt.Fprint(w, `{code=0, msg="account unavailable"}`)
		return
	}
	ttl := int64(s.Cfg.LoginHTTP.TokenTTLHours) * 3600
	tok, err := s.Store.IssueToken(acc.ID, ttl)
	if err != nil {
		http.Error(w, "token", http.StatusInternalServerError)
		return
	}
	fmt.Fprintf(w, `{code=1, msg=%s}`, luaStr(tok))
}

// userLogin implements the platform login the client's UserPanel calls before
// anything else. Reply codes come from user_panel.lua: msg "206" account does
// not exist, "207" wrong password, "208" contact support; anything else is
// success, and msg is then the login token. With auto_register on, an unknown
// account is created with the password it presented (development convenience).
func (s *Server) userLogin(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	user, pass := q.Get("username"), q.Get("password")
	acc, err := s.Store.AccountByName(platform, user)
	switch {
	case errors.Is(err, store.ErrNotFound):
		if !s.Cfg.LoginHTTP.AutoRegister || user == "" {
			fmt.Fprint(w, `{msg="206"}`)
			return
		}
		h, _ := bcrypt.GenerateFromPassword([]byte(pass), bcrypt.DefaultCost)
		if acc, err = s.Store.CreateAccount(platform, user, string(h), ""); err != nil {
			http.Error(w, "create", http.StatusInternalServerError)
			return
		}
	case err != nil:
		http.Error(w, "lookup", http.StatusInternalServerError)
		return
	case acc.Status != 1:
		fmt.Fprint(w, `{msg="208"}`)
		return
	case acc.PasswordHash == "":
		// created by the token endpoint without a password: adopt this one
		h, _ := bcrypt.GenerateFromPassword([]byte(pass), bcrypt.DefaultCost)
		s.Store.SetAccountPassword(acc.ID, string(h))
	case bcrypt.CompareHashAndPassword([]byte(acc.PasswordHash), []byte(pass)) != nil:
		fmt.Fprint(w, `{msg="207"}`)
		return
	}
	// The client keeps this reply's msg in a global and later sends it as the
	// "token" arg of 1164 (confirmed on device), so msg carries the real token.
	tok, err := s.Store.IssueToken(acc.ID, int64(s.Cfg.LoginHTTP.TokenTTLHours)*3600)
	if err != nil {
		http.Error(w, "token", http.StatusInternalServerError)
		return
	}
	fmt.Fprintf(w, `{code=1, msg=%s}`, luaStr(tok))
}

var (
	reUser = regexp.MustCompile(`^[A-Za-z0-9]{6,12}$`)
)

// userReg: "205" registered, "200" already exists, "203" bad name, "204" bad password.
func (s *Server) userReg(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	user, pass := q.Get("username"), q.Get("password")
	if !reUser.MatchString(user) {
		fmt.Fprint(w, `{msg="203"}`)
		return
	}
	if n := len(pass); n < 6 || n > 16 {
		fmt.Fprint(w, `{msg="204"}`)
		return
	}
	h, _ := bcrypt.GenerateFromPassword([]byte(pass), bcrypt.DefaultCost)
	if _, err := s.Store.CreateAccount(platform, user, string(h), ""); errors.Is(err, store.ErrConflict) {
		fmt.Fprint(w, `{msg="200"}`)
		return
	} else if err != nil {
		http.Error(w, "create", http.StatusInternalServerError)
		return
	}
	fmt.Fprint(w, `{msg="205"}`)
}
