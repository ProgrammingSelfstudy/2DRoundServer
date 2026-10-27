// Package admin is the management HTTP API: admin login, RBAC, auditing, and the
// resources an operator manages (servers, accounts, roles, notices, mail).
package admin

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"reflect"
	"strconv"
	"time"

	"2droundserver/internal/store"
)

// Permission levels, lowest first. A role holds one level.
type level int

const (
	levRead   level = iota + 1 // viewer: look at everything except admin management and audit
	levWrite                   // operator: mail, notices, bans
	levManage                  // superadmin: servers, admins, audit log
)

var roleLevel = map[string]level{"viewer": levRead, "operator": levWrite, "superadmin": levManage}

type Server struct {
	Store *store.Store
	// Secret signs the login tokens; it must be long and private.
	Secret []byte
	TTL    time.Duration
	// GatewayURL is the gateway's loopback control endpoint ("" = none): live mail
	// push, kicking banned roles, online count.
	GatewayURL string
	// BackupDir and BackupKeep: where POST /api/backup writes and how many backups stay
	// (BackupDir "" = backups are not configured and the endpoints answer 409).
	BackupDir  string
	BackupKeep int
	HTTP       *http.Client
	Log        *log.Logger
	Now        func() time.Time

	limiter *loginLimiter
}

func (s *Server) init() {
	if s.Now == nil {
		s.Now = time.Now
	}
	if s.Log == nil {
		s.Log = log.Default()
	}
	if s.TTL == 0 {
		s.TTL = 12 * time.Hour
	}
	if s.HTTP == nil {
		s.HTTP = &http.Client{Timeout: 3 * time.Second}
	}
	if s.limiter == nil {
		s.limiter = newLoginLimiter(s.Now)
	}
}

type principal struct{ Admin *store.Admin }

type handler func(w http.ResponseWriter, r *http.Request, p *principal)

// Handler returns the API; every route except login and health needs a token.
func (s *Server) Handler() http.Handler {
	s.init()
	mux := http.NewServeMux()
	route := func(pattern string, min level, h handler) {
		mux.HandleFunc(pattern, func(w http.ResponseWriter, r *http.Request) {
			p, ok := s.authenticate(w, r)
			if !ok {
				return
			}
			if roleLevel[p.Admin.Role] < min {
				fail(w, http.StatusForbidden, "your role may not do this")
				return
			}
			h(w, r, p)
		})
	}
	mux.HandleFunc("GET /api/healthz", func(w http.ResponseWriter, r *http.Request) { writeJSON(w, 200, map[string]bool{"ok": true}) })
	mux.HandleFunc("POST /api/login", s.login)

	route("GET /api/me", levRead, s.me)
	route("POST /api/me/password", levRead, s.changeOwnPassword)
	route("GET /api/stats", levRead, s.stats)
	route("GET /api/online", levRead, s.online)
	route("GET /api/meta", levRead, s.meta)
	route("GET /api/items", levRead, s.searchItems)
	route("POST /api/broadcast", levWrite, s.broadcast)
	route("GET /api/backups", levManage, s.listBackups)
	route("POST /api/backups", levManage, s.makeBackup)

	route("GET /api/admins", levManage, s.listAdmins)
	route("POST /api/admins", levManage, s.createAdmin)
	route("PATCH /api/admins/{id}", levManage, s.updateAdmin)

	route("GET /api/servers", levRead, s.listServers)
	route("POST /api/servers", levManage, s.createServer)
	route("PUT /api/servers/{id}", levManage, s.updateServer)
	route("DELETE /api/servers/{id}", levManage, s.deleteServer)

	route("GET /api/accounts", levRead, s.listAccounts)
	route("POST /api/accounts/{id}/ban", levWrite, s.banAccount)
	route("POST /api/accounts/{id}/unban", levWrite, s.unbanAccount)

	route("GET /api/roles", levRead, s.listRoles)
	route("GET /api/roles/{id}", levRead, s.roleDetail)
	route("POST /api/roles/{id}/kick", levWrite, s.kickRole)
	route("POST /api/roles/{id}/level", levWrite, s.setRoleLevel)
	route("POST /api/roles/{id}/vip", levWrite, s.setRoleVIP)
	route("POST /api/roles/{id}/grant", levWrite, s.grantRole)
	route("POST /api/roles/{id}/heroes", levWrite, s.grantHeroes)
	route("POST /api/roles/{id}/skins", levWrite, s.grantSkin)
	route("POST /api/roles/{id}/looks", levWrite, s.grantLook)
	route("POST /api/roles/{id}/ban", levWrite, s.banRole)
	route("POST /api/roles/{id}/unban", levWrite, s.unbanRole)

	route("GET /api/redeem-codes", levRead, s.listRedeemCodes)
	route("POST /api/redeem-codes", levWrite, s.createRedeemCode)
	route("DELETE /api/redeem-codes/{code}", levWrite, s.deleteRedeemCode)
	route("GET /api/reports", levRead, s.listReports)
	route("GET /api/guilds", levRead, s.listGuilds)
	route("POST /api/guilds/{id}/disband", levWrite, s.disbandGuild)

	route("GET /api/notices", levRead, s.listNotices)
	route("POST /api/notices", levWrite, s.createNotice)
	route("PUT /api/notices/{id}", levWrite, s.updateNotice)
	route("DELETE /api/notices/{id}", levWrite, s.deleteNotice)

	route("GET /api/activities", levRead, s.listActivities)
	route("POST /api/activities", levWrite, s.createActivity)
	route("PUT /api/activities/{id}", levWrite, s.updateActivity)
	route("DELETE /api/activities/{id}", levWrite, s.deleteActivity)

	route("GET /api/tickets", levRead, s.listTickets)
	route("POST /api/tickets/{id}/reply", levWrite, s.replyTicket)

	route("GET /api/mails", levRead, s.listMails)
	route("POST /api/mails", levWrite, s.sendMail)

	route("GET /api/pay/config", levRead, s.payConfig)
	route("PUT /api/pay/config", levManage, s.setPayConfig)
	route("GET /api/pay/products", levRead, s.payProducts)
	route("GET /api/pay/orders", levRead, s.listPayOrders)
	route("POST /api/pay/orders", levManage, s.createPayOrder)
	route("POST /api/pay/orders/{no}/confirm", levManage, s.confirmPayOrder)
	route("POST /api/pay/orders/{no}/cancel", levManage, s.cancelPayOrder)

	route("GET /api/audit", levManage, s.listAudit)

	mux.Handle("/", staticHandler()) // the web UI; /api/ paths that match nothing above are a JSON 404

	return secure(mux)
}

// secure adds headers that keep API responses out of caches and stop sniffing, and
// turns a handler panic into a 500 instead of a dropped connection.
func secure(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("Cache-Control", "no-store")
		h.Set("X-Content-Type-Options", "nosniff")
		defer func() {
			if v := recover(); v != nil {
				log.Printf("admin: panic in %s %s: %v", r.Method, r.URL.Path, v)
				fail(w, http.StatusInternalServerError, "internal error")
			}
		}()
		next.ServeHTTP(w, r)
	})
}

const maxBody = 1 << 20

// decode reads a JSON body strictly: size limited, unknown fields and trailing data rejected.
func decode(w http.ResponseWriter, r *http.Request, v any) bool {
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxBody))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		var tooBig *http.MaxBytesError
		if errors.As(err, &tooBig) {
			fail(w, http.StatusRequestEntityTooLarge, "request body too large")
		} else {
			fail(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		}
		return false
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		fail(w, http.StatusBadRequest, "invalid JSON: trailing data")
		return false
	}
	return true
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func fail(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

func internalErr(w http.ResponseWriter, s *Server, err error) {
	s.Log.Printf("admin: %v", err)
	fail(w, http.StatusInternalServerError, "internal error")
}

func pathID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		fail(w, http.StatusBadRequest, "bad id")
		return 0, false
	}
	return id, true
}

// page reads limit/offset: default 50, at most 200.
func page(r *http.Request) (limit, offset int) {
	limit, offset = 50, 0
	if n, err := strconv.Atoi(r.URL.Query().Get("limit")); err == nil && n > 0 {
		limit = min(n, 200)
	}
	if n, err := strconv.Atoi(r.URL.Query().Get("offset")); err == nil && n > 0 {
		offset = n
	}
	return
}

func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// audit records a state-changing action. detail must never contain secrets.
func (s *Server) audit(r *http.Request, p *principal, action, target string, detail any) {
	if err := s.Store.WriteAudit(p.Admin.ID, p.Admin.Username, action, target, detail, clientIP(r)); err != nil {
		s.Log.Printf("admin: audit write failed for %s: %v", action, err)
	}
}

// gateway calls the loopback control endpoint; the error says why a live action did not happen.
func (s *Server) gateway(method, path string, out any) error {
	if s.GatewayURL == "" {
		return errors.New("no gateway configured")
	}
	req, err := http.NewRequest(method, s.GatewayURL+path, nil)
	if err != nil {
		return err
	}
	resp, err := s.HTTP.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 200))
		return fmt.Errorf("gateway answered %d: %s", resp.StatusCode, b)
	}
	if out != nil {
		return json.NewDecoder(resp.Body).Decode(out)
	}
	return nil
}

// nonNil turns a nil slice into an empty one: a nil slice is encoded as JSON null, and
// the web pages call .map on every list.
func nonNil(v any) any {
	if rv := reflect.ValueOf(v); rv.Kind() == reflect.Slice && rv.IsNil() {
		return reflect.MakeSlice(rv.Type(), 0, 0).Interface()
	}
	return v
}
