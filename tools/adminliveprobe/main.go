// Real TCP + HTTP management probe. Only accepts tools/admin-live-sandbox.sh data.
package main

import (
	"2droundserver/internal/config"
	"2droundserver/internal/proto"
	"bufio"
	"bytes"
	"crypto/md5"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"
	"time"
)

type user struct {
	Username string `json:"username"`
	Password string `json:"password"`
	ID       int64  `json:"id"`
}
type fixture struct {
	Users map[string]user `json:"users"`
	API   string          `json:"api"`
	Live  *liveUser       `json:"live,omitempty"`
}
type liveUser struct {
	Username  string  `json:"username"`
	Password  string  `json:"password"`
	AccountID int64   `json:"account_id"`
	RoleIDs   []int64 `json:"role_ids"`
	Server    string  `json:"server"`
}
type runner struct {
	root       string
	cfg        *config.Config
	f          fixture
	schema     *proto.Schema
	codec      proto.Options
	adminToken string
	events     []map[string]any
}

func must(e error) {
	if e != nil {
		log.Fatal(e)
	}
}
func check(ok bool, what string) {
	if !ok {
		log.Fatal("FAIL " + what)
	}
}
func (r *runner) event(name string, detail map[string]any) {
	if detail == nil {
		detail = map[string]any{}
	}
	detail["event"] = name
	detail["at"] = time.Now().UTC().Format(time.RFC3339)
	r.events = append(r.events, detail)
	b, e := json.MarshalIndent(r.events, "", "  ")
	must(e)
	must(os.WriteFile(filepath.Join(r.root, "evidence.json"), b, 0600))
	fmt.Println("PASS " + name)
}
func random(n int) string {
	b := make([]byte, n)
	must(func() error { _, e := rand.Read(b); return e }())
	return hex.EncodeToString(b)
}
func loopback(addr string) {
	host, _, e := net.SplitHostPort(addr)
	must(e)
	check(net.ParseIP(host) != nil && net.ParseIP(host).IsLoopback(), "loopback address required")
}
func (r *runner) api(method, path string, body any, status int) proto.Message {
	var input io.Reader
	if body != nil {
		b, e := json.Marshal(body)
		must(e)
		input = bytes.NewReader(b)
	}
	req, e := http.NewRequest(method, r.f.API+"/api"+path, input)
	must(e)
	req.Header.Set("Content-Type", "application/json")
	if r.adminToken != "" {
		req.Header.Set("Authorization", "Bearer "+r.adminToken)
	}
	res, e := (&http.Client{Timeout: 10 * time.Second}).Do(req)
	must(e)
	defer res.Body.Close()
	check(res.StatusCode == status, fmt.Sprintf("%s %s HTTP %d want %d", method, path, res.StatusCode, status))
	var out any
	decoder := json.NewDecoder(res.Body)
	decoder.UseNumber()
	must(decoder.Decode(&out))
	return normalize(out).(proto.Message)
}
func (r *runner) token() (string, bool) {
	q := url.Values{"username": {r.f.Live.Username}, "password": {r.f.Live.Password}}
	res, e := (&http.Client{Timeout: 10 * time.Second}).Get("http://" + r.cfg.LoginHTTP.Addr + "/userlogin/userLogins2?" + q.Encode())
	if e != nil {
		log.Fatal("game HTTP login unavailable")
	}
	defer res.Body.Close()
	b, e := io.ReadAll(res.Body)
	must(e)
	ok := strings.Contains(string(b), "code=1")
	if !ok {
		return "", false
	}
	m := regexp.MustCompile(`msg="([^"]*)"`).FindSubmatch(b)
	check(m != nil, "game login token")
	return string(m[1]), true
}

type client struct {
	r   *runner
	c   net.Conn
	br  *bufio.Reader
	idx int64
	rid int64
}

func (r *runner) connect(token string) *client {
	c, e := net.DialTimeout("tcp", r.cfg.Gateway.TCPAddr, 5*time.Second)
	must(e)
	_, e = c.Write([]byte(proto.Handshake))
	must(e)
	b := &client{r: r, c: c, br: bufio.NewReader(c)}
	b.send(1164, proto.Message{"args": []proto.Message{{"key": "account", "val": r.f.Live.Username}, {"key": "token", "val": token}, {"key": "enter_srv_id", "val": r.cfg.Gateway.SrvID}, {"key": "platform", "val": "symlf"}}})
	return b
}
func (b *client) send(cmd uint16, m proto.Message) {
	out := []byte{}
	if !(cmd == 10300 || (cmd >= 16601 && cmd <= 16603) || cmd/100 == 111 || cmd/100 == 200) {
		b.idx++
		now := time.Now().Unix()
		sum := md5.Sum([]byte(fmt.Sprintf("%d%d%d__sszg666__", b.idx, now, cmd)))
		p, e := proto.Encode(b.r.codec, b.r.schema.Send[1197], proto.Message{"idx": b.idx, "time": now, "code": int64(cmd) + (b.idx+5)%16, "sign": hex.EncodeToString(sum[:])})
		must(e)
		f, e := (proto.Frame{Command: 1197, Payload: p}).Encode()
		must(e)
		out = append(out, f...)
	}
	p, e := proto.Encode(b.r.codec, b.r.schema.Send[cmd], m)
	must(e)
	f, e := (proto.Frame{Command: cmd, Payload: p}).Encode()
	must(e)
	_, e = b.c.Write(append(out, f...))
	must(e)
}
func (b *client) recv(cmd uint16) proto.Message {
	must(b.c.SetReadDeadline(time.Now().Add(6 * time.Second)))
	for {
		f, e := proto.ReadFrame(b.br)
		must(e)
		if f.Command != cmd {
			continue
		}
		m, e := proto.Decode(b.r.codec, b.r.schema.Recv[cmd], f.Payload)
		must(e)
		return m
	}
}
func (b *client) step(cmd uint16, m proto.Message) proto.Message { b.send(cmd, m); return b.recv(cmd) }
func (b *client) enter(rid int64) proto.Message {
	b.rid = rid
	return b.step(10192, proto.Message{"rid": rid, "srv_id": b.r.cfg.Gateway.SrvID})
}
func (b *client) disconnected(label string) {
	msg := b.recv(10310)
	check(msg.Int("is_show") == 1, label+" kick notification")
	must(b.c.SetReadDeadline(time.Now().Add(5 * time.Second)))
	for {
		_, e := proto.ReadFrame(b.br)
		if e != nil {
			check(errors.Is(e, io.EOF) || errors.Is(e, io.ErrUnexpectedEOF) || errors.Is(e, net.ErrClosed) || errors.Is(e, syscall.ECONNRESET), label+" actual socket close, not timeout")
			break
		}
	}
	b.c.Close()
	b.r.event(label, map[string]any{"rid": b.rid, "cmd": 10310, "socket_closed": true})
}
func has(list []proto.Message, id int64) bool {
	for _, m := range list {
		if m.Int("id") == id {
			return true
		}
	}
	return false
}
func (r *runner) openRole(token string, rid int64) *client {
	b := r.connect(token)
	check(b.recv(1164).Int("code") == 1, "role list success")
	check(b.enter(rid).Int("code") == 1, "enter role success")
	return b
}
func (r *runner) save() {
	raw := map[string]any{}
	old, e := os.ReadFile(filepath.Join(r.root, "fixture.json"))
	must(e)
	must(json.Unmarshal(old, &raw))
	raw["live"] = r.f.Live
	raw["users"] = r.f.Users
	b, e := json.MarshalIndent(raw, "", "  ")
	must(e)
	must(os.WriteFile(filepath.Join(r.root, "fixture.json"), b, 0600))
}
func (r *runner) initLive() {
	if r.f.Live == nil {
		r.f.Live = &liveUser{Username: "live" + random(3), Password: random(8), Server: r.cfg.Gateway.SrvID}
	}
	tok, ok := r.token()
	check(ok, "game HTTP login")
	b := r.connect(tok)
	list := b.recv(1164)
	check(list.Int("code") == 1, "1164 authenticated")
	roles := list.Arr("roles")
	for len(roles) < 2 {
		cr := b.step(10191, proto.Message{"sex": 1, "name": "", "career": 2, "playform": "symlf"})
		check(cr.Int("code") == 1, "create actual protocol role")
		roles = append(roles, proto.Message{"rid": cr.Int("rid")})
	}
	r.f.Live.RoleIDs = []int64{roles[0].Int("rid"), roles[1].Int("rid")}
	b.c.Close()
	acc := r.api("GET", "/accounts?q="+url.QueryEscape(r.f.Live.Username), nil, 200).Arr("items")
	check(len(acc) == 1, "game account lookup")
	r.f.Live.AccountID = acc[0].Int("id")
	r.save()
	r.event("authenticated game HTTP + TCP 1164/10191", map[string]any{"account_id": r.f.Live.AccountID, "role_ids": r.f.Live.RoleIDs})
}
func (r *runner) verify() {
	r.initLive()
	tok, ok := r.token()
	check(ok, "game login")
	id1, id2 := r.f.Live.RoleIDs[0], r.f.Live.RoleIDs[1]
	a, b := r.openRole(tok, id1), r.openRole(tok, id2)
	defer a.c.Close()
	defer b.c.Close()
	r.event("two roles online on real cmd/gateway", map[string]any{"roles": []int64{id1, id2}})
	before := r.api("GET", fmt.Sprintf("/roles/%d", id1), nil, 200)
	gold := func(m proto.Message) int64 {
		for _, v := range m.Arr("assets") {
			if v.Int("label") == 3 {
				return v.Int("val")
			}
		}
		return 0
	}
	mail := r.api("POST", "/mails", proto.Message{"scope": "direct", "role_ids": []int64{id1}, "subject": "真实TCP邮件验证", "content": "邮件正文与附件隔离验证", "from_name": "本机测试", "type": 1, "expire_days": 1, "attachments": []proto.Message{{"kind": "asset", "asset": "gold", "num": 7}, {"kind": "item", "item_id": 10034, "num": 1}}}, 201)
	mid := mail["mail"].(proto.Message).Int("id")
	check(mail.Int("pushed") == 1 && mail["push_error"] == nil, "mail pushed count 1")
	check(has(a.recv(10803).Arr("mail"), mid), "actual 10803 arrival")
	check(has(a.step(10800, proto.Message{}).Arr("mail"), mid), "mail list")
	detail := a.step(10806, proto.Message{"id": mid, "srv_id": r.cfg.Gateway.SrvID})
	check(detail.Str("content") == "邮件正文与附件隔离验证" && len(detail.Arr("assets")) == 1 && len(detail.Arr("items")) == 1, "mail detail attachments")
	r.event("mail 10803 push + 10800 list + 10806 detail", map[string]any{"mail_id": mid, "rid": id1, "assets": detail.Arr("assets"), "items": detail.Arr("items")})
	claim := a.step(10801, proto.Message{"id": mid, "srv_id": r.cfg.Gateway.SrvID})
	check(claim.Int("code") == 1, "claim attachments")
	after := r.api("GET", fmt.Sprintf("/roles/%d", id1), nil, 200)
	check(gold(after) == gold(before)+7, "gold delivery exactly once")
	itemCount := func(m proto.Message) int64 {
		for _, v := range m.Arr("items") {
			if v.Int("base_id") == 10034 {
				return v.Int("num")
			}
		}
		return 0
	}
	check(itemCount(after) == itemCount(before)+1, "item attachment delivery exactly once")
	check(len(a.step(10806, proto.Message{"id": mid, "srv_id": r.cfg.Gateway.SrvID}).Arr("items")) == 0, "claimed attachment removed from detail")

	dup := a.step(10801, proto.Message{"id": mid, "srv_id": r.cfg.Gateway.SrvID})
	check(dup.Int("code") == 0, "duplicate mail claim refused")
	check(gold(r.api("GET", fmt.Sprintf("/roles/%d", id1), nil, 200)) == gold(after), "duplicate claim no extra gold")
	check(itemCount(r.api("GET", fmt.Sprintf("/roles/%d", id1), nil, 200)) == itemCount(after), "duplicate claim no extra item")
	r.event("10801 attachment claim and duplicate refusal", map[string]any{"mail_id": mid, "gold_delta": 7, "duplicate_code": dup.Int("code")})
	nb := proto.Message{"title": "真实TCP公告", "content": "公告第一版", "srv_id": r.cfg.Gateway.SrvID, "status": 1, "start_at": 0, "end_at": 0}
	n := r.api("POST", "/notices", nb, 201)
	nid := n.Int("id")
	check(n.Int("pushed") == 2, "notice notify two roles")
	for _, c := range []*client{a, b} {
		check(has(c.recv(10950).Arr("board_list"), nid), "notice create TCP")
		check(has(c.step(10950, proto.Message{}).Arr("board_list"), nid), "notice list TCP")
	}
	check(a.step(10952, proto.Message{"id": nid}).Int("code") == 1, "notice detail open")
	check(has(a.recv(10950).Arr("board_list"), nid), "notice read state push")
	nb["title"], nb["content"] = "真实TCP公告已编辑", "公告第二版"
	r.api("PUT", fmt.Sprintf("/notices/%d", nid), nb, 200)
	for _, c := range []*client{a, b} {
		rows := c.recv(10950).Arr("board_list")
		check(len(rows) == 1 && rows[0].Str("content") == "公告第二版", "notice update TCP content")
	}
	r.api("DELETE", fmt.Sprintf("/notices/%d", nid), nil, 200)
	for _, c := range []*client{a, b} {
		check(c.recv(10951).Int("id") == nid, "notice delete TCP")
		check(!has(c.step(10950, proto.Message{}).Arr("board_list"), nid), "deleted notice absent")
	}
	r.event("notice create/edit/delete actual 10950/10952/10951", map[string]any{"notice_id": nid, "online_roles": 2})
	r.api("POST", fmt.Sprintf("/roles/%d/ban", id1), proto.Message{}, 200)
	a.disconnected("role ban disconnect")
	denied := r.connect(tok)
	check(denied.recv(1164).Int("code") == 1, "account remains permitted")
	check(denied.enter(id1).Int("code") == 0, "banned role reenter refused")
	denied.c.Close()
	b.step(10800, proto.Message{})
	r.event("role ban scope + reconnect refused", map[string]any{"blocked_role": id1, "other_role_alive": id2})
	r.api("POST", fmt.Sprintf("/roles/%d/unban", id1), nil, 200)
	a = r.openRole(tok, id1)
	defer a.c.Close()
	kick := r.api("POST", fmt.Sprintf("/roles/%d/kick", id1), nil, 200)
	check(kick["kicked"] == true, "kick reports online")
	a.disconnected("explicit kick disconnect")
	a = r.openRole(tok, id1)
	defer a.c.Close()
	r.event("unban + reconnect after ordinary kick", map[string]any{"rid": id1})
	r.api("POST", fmt.Sprintf("/accounts/%d/ban", r.f.Live.AccountID), proto.Message{"reason": "真实TCP隔离封禁测试"}, 200)
	a.disconnected("account ban first role disconnect")
	b.disconnected("account ban second role disconnect")
	_, ok = r.token()
	check(!ok, "HTTP login forbidden for banned account")
	denied = r.connect(tok)
	check(denied.recv(1164).Int("code") != 1, "old game token rejected")
	denied.c.Close()
	r.api("POST", fmt.Sprintf("/accounts/%d/unban", r.f.Live.AccountID), nil, 200)
	tok, ok = r.token()
	check(ok, "unbanned HTTP login")
	a = r.openRole(tok, id1)
	defer a.c.Close()
	b = r.openRole(tok, id2)
	defer b.c.Close()
	r.event("account ban covers both roles + login denied + unban restored", map[string]any{"account_id": r.f.Live.AccountID, "roles": r.f.Live.RoleIDs})
	r.event("real stack verification complete", nil)
}
func (r *runner) watch() {
	r.initLive()
	tok, ok := r.token()
	check(ok, "watch login")
	c := r.openRole(tok, r.f.Live.RoleIDs[0])
	defer c.c.Close()
	r.event("browser watch ready", map[string]any{"rid": c.rid})
	for {
		c.c.SetReadDeadline(time.Now().Add(14 * time.Minute))
		f, e := proto.ReadFrame(c.br)
		if e != nil {
			check(errors.Is(e, io.EOF) || errors.Is(e, io.ErrUnexpectedEOF) || errors.Is(e, net.ErrClosed) || errors.Is(e, syscall.ECONNRESET), "watch socket truly disconnected")
			r.event("browser watch socket closed", map[string]any{"rid": c.rid, "closed": true})
			return
		}
		if f.Command != 10803 && f.Command != 10950 && f.Command != 10951 && f.Command != 10310 {
			continue
		}
		m, e := proto.Decode(r.codec, r.schema.Recv[f.Command], f.Payload)
		must(e)
		r.event("browser action TCP frame", map[string]any{"rid": c.rid, "cmd": f.Command, "payload": m})
		if f.Command == 10803 {
			for _, v := range m.Arr("mail") {
				detail := c.step(10806, proto.Message{"id": v.Int("id"), "srv_id": r.cfg.Gateway.SrvID})
				r.event("browser mail TCP detail", map[string]any{"mail_id": v.Int("id"), "content": detail.Str("content"), "assets": detail.Arr("assets"), "items": detail.Arr("items")})
			}
		}
	}
}
func main() {
	dir := flag.String("dir", "", "marked isolated root")
	mode := flag.String("mode", "verify", "verify or watch")
	flag.Parse()
	root, e := filepath.Abs(*dir)
	must(e)
	marker, e := os.ReadFile(filepath.Join(root, ".admin-live-sandbox"))
	must(e)
	check(string(marker) == "2dround-admin-live\n", "live sandbox marker")
	cfg, e := config.Load(filepath.Join(root, "config.yaml"))
	must(e)
	check(filepath.Clean(cfg.DBPath) == filepath.Join(root, "admin.db"), "isolated DB path")
	for _, addr := range []string{cfg.Admin.Addr, cfg.LoginHTTP.Addr, cfg.Gateway.TCPAddr, cfg.Gateway.AdminAddr} {
		loopback(addr)
	}
	check(cfg.Admin.GatewayURL == "http://"+cfg.Gateway.AdminAddr, "gateway control must match the isolated loopback address")
	r := &runner{root: root, cfg: cfg}
	b, e := os.ReadFile(filepath.Join(root, "fixture.json"))
	must(e)
	must(json.Unmarshal(b, &r.f))
	check(r.f.API == "http://"+cfg.Admin.Addr, "fixture API matches isolated config")
	r.schema, e = proto.LoadSchema()
	must(e)
	r.codec, e = cfg.CodecOptions()
	must(e)
	u := r.f.Users["operator"]
	r.adminToken = r.api("POST", "/login", proto.Message{"username": u.Username, "password": u.Password}, 200).Str("token")
	if b, e := os.ReadFile(filepath.Join(root, "evidence.json")); e == nil {
		must(json.Unmarshal(b, &r.events))
	}
	switch *mode {
	case "watch":
		r.watch()
	case "verify":
		r.verify()
	case "recover":
		r.recoverOffline()
	case "offline":
		r.offline()
	default:
		log.Fatal("mode must be verify, watch, offline or recover")
	}
}

func normalize(v any) any {
	switch x := v.(type) {
	case json.Number:
		n, e := x.Int64()
		must(e)
		return n
	case map[string]any:
		out := proto.Message{}
		for k, v := range x {
			out[k] = normalize(v)
		}
		return out
	case []any:
		out := []proto.Message{}
		for _, v := range x {
			out = append(out, normalize(v).(proto.Message))
		}
		return out
	default:
		return v
	}
}

func (r *runner) recoverOffline() {
	b, e := os.ReadFile(filepath.Join(r.root, "offline-evidence.json"))
	must(e)
	var ids struct {
		MailID   int64 `json:"mail_id"`
		NoticeID int64 `json:"notice_id"`
	}
	must(json.Unmarshal(b, &ids))
	tok, ok := r.token()
	check(ok, "recovery HTTP login")
	c := r.openRole(tok, r.f.Live.RoleIDs[0])
	defer c.c.Close()
	check(has(c.step(10800, proto.Message{}).Arr("mail"), ids.MailID), "stored offline mail visible after recovery")
	d := c.step(10806, proto.Message{"id": ids.MailID, "srv_id": r.cfg.Gateway.SrvID})
	check(d.Str("content") == "离线期间保存，重连后列表应可见", "offline mail detail")
	check(has(c.step(10950, proto.Message{}).Arr("board_list"), ids.NoticeID), "stored offline notice visible after recovery")
	r.event("real gateway stopped/restarted: offline mail/notice available over TCP", map[string]any{"mail_id": ids.MailID, "notice_id": ids.NoticeID})
}

func (r *runner) offline() {
	c, e := net.DialTimeout("tcp", r.cfg.Gateway.AdminAddr, time.Second)
	if e == nil {
		c.Close()
		log.Fatal("offline test requires the owned isolated gateway to be stopped")
	}
	check(r.f.Live != nil, "initialize real game roles first")
	m := r.api("POST", "/mails", proto.Message{"scope": "direct", "role_ids": []int64{r.f.Live.RoleIDs[0]}, "subject": "网关离线保存验证", "content": "离线期间保存，重连后列表应可见", "type": 1}, 201)
	check(m["pushed"] == nil && m.Str("push_error") != "", "offline mail stored without push")
	n := r.api("POST", "/notices", proto.Message{"title": "网关离线公告验证", "content": "离线期间保存，重连后公告应可见", "status": 1, "start_at": 0, "end_at": 0}, 201)
	check(n["pushed"] == nil && n.Str("push_error") != "", "offline notice stored without push")
	r.api("POST", fmt.Sprintf("/roles/%d/kick", r.f.Live.RoleIDs[0]), proto.Message{}, 502)
	out := map[string]any{"mail_id": m["mail"].(proto.Message).Int("id"), "notice_id": n.Int("id"), "mail_stored": true, "mail_pushed": nil, "notice_stored": true, "notice_pushed": nil, "kick_status": 502}
	b, e := json.MarshalIndent(out, "", "  ")
	must(e)
	must(os.WriteFile(filepath.Join(r.root, "offline-evidence.json"), b, 0600))
	r.event("stopped real gateway: saved/unpushed mail/notice, kick 502", out)
}
