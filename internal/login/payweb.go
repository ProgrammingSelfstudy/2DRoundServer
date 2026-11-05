package login

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"2droundserver/internal/game"
	"2droundserver/internal/store"
)

// payWeb is the original's top-up call (capture 2026-11-05, testdata/captures/original-2026-11-05-charge): the client's
// iceBirdOnPay1 POSTs /api.php/pf/sygame/pay_web/?srv_id&role_id&money&name&productId to the server list's `host` and shows the
// answer as a toast (`message(response)`). On success the answer is the 快乐币 left, as plain text; the diamonds, the 快乐币 and
// the rest of the state arrive on the game connection a moment before the answer. Anything else is a message for the player.
//
// The original asks for nothing but names, so anyone who knew an account name could spend its 快乐币. Here the role must belong
// to the account in `name` and be online (a real payment is clicked inside the game), which the gateway says.
func (s *Server) payWeb(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=UTF-8")
	w.Header().Set("Cache-Control", "no-store, no-cache, must-revalidate")
	if r.Method != http.MethodPost {
		http.Error(w, "post only", http.StatusMethodNotAllowed)
		return
	}
	q := r.URL.Query()
	rid, err1 := strconv.ParseInt(q.Get("role_id"), 10, 64)
	money, err2 := strconv.ParseInt(q.Get("money"), 10, 64)
	pid, err3 := strconv.ParseInt(q.Get("productId"), 10, 64)
	if err1 != nil || err2 != nil || err3 != nil {
		fmt.Fprint(w, "充值参数错误")
		return
	}
	role, err := s.Store.RoleByID(rid)
	if errors.Is(err, store.ErrNotFound) {
		fmt.Fprint(w, "角色不存在")
		return
	}
	if err != nil {
		s.Log.Printf("pay_web: role %d: %v", rid, err)
		http.Error(w, "role", http.StatusInternalServerError)
		return
	}
	acc, err := s.Store.AccountByID(role.AccountID)
	if err != nil || role.SrvID != q.Get("srv_id") || acc.Account != q.Get("name") {
		fmt.Fprint(w, "账号与角色不符")
		return
	}
	if online, known := s.roleOnline(rid); known && !online {
		fmt.Fprint(w, "请在游戏内充值")
		return
	}
	res, err := game.PayWebCharge(s.Store, role, pid, money, time.Now())
	switch {
	case errors.Is(err, game.ErrNoKlb):
		fmt.Fprint(w, "快乐币不足")
		return
	case errors.Is(err, game.ErrNoProduct), errors.Is(err, game.ErrNotPayable), errors.Is(err, game.ErrPriceMoney):
		fmt.Fprint(w, "该商品暂时无法购买")
		return
	case err != nil && res == nil:
		s.Log.Printf("pay_web: role %d product %d: %v", rid, pid, err)
		http.Error(w, "pay", http.StatusInternalServerError)
		return
	case err != nil:
		s.Log.Printf("pay_web: role %d product %d delivered, but: %v", rid, pid, err) // e.g. only the 充值返利 mail failed
	}
	s.pushCharge(rid, res.Gold, res.MailID)
	fmt.Fprint(w, res.KlbLeft)
}

// gatewayURL is the gateway's loopback control endpoint ("" = none configured).
func (s *Server) gatewayURL() string {
	if s.Cfg == nil || s.Cfg.Gateway.AdminAddr == "" {
		return ""
	}
	return "http://" + s.Cfg.Gateway.AdminAddr
}

// roleOnline asks the gateway whether the role is connected; known is false when there is no gateway to ask.
func (s *Server) roleOnline(rid int64) (online, known bool) {
	base := s.gatewayURL()
	if base == "" {
		return false, false
	}
	var out struct {
		Online bool `json:"online"`
	}
	if err := s.gatewayCall("GET", fmt.Sprintf("%s/online?rid=%d", base, rid), &out); err != nil {
		s.Log.Printf("pay_web: gateway: %v", err)
		return false, false
	}
	return out.Online, true
}

// pushCharge has the gateway send an online role what the payment changed.
func (s *Server) pushCharge(rid, diamonds, mailID int64) {
	base := s.gatewayURL()
	if base == "" {
		return
	}
	v := url.Values{"rid": {strconv.FormatInt(rid, 10)}}
	if diamonds > 0 {
		v.Set("paid", strconv.FormatInt(diamonds, 10))
	}
	if mailID > 0 {
		v.Set("mail", strconv.FormatInt(mailID, 10))
	}
	if err := s.gatewayCall("POST", base+"/push/charge?"+v.Encode(), nil); err != nil {
		s.Log.Printf("pay_web: push to role %d: %v", rid, err) // delivered all the same: the role sees it when it logs in
	}
}

var gatewayClient = &http.Client{Timeout: 5 * time.Second}

func (s *Server) gatewayCall(method, u string, out any) error {
	req, err := http.NewRequest(method, u, nil)
	if err != nil {
		return err
	}
	resp, err := gatewayClient.Do(req)
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

// payHost is the `host` of the server list's entries: where the client's pay_web call goes, host:port of this service.
func (s *Server) payHost() string {
	b := strings.TrimRight(s.Cfg.LoginHTTP.PublicBase, "/")
	if i := strings.Index(b, "://"); i >= 0 {
		b = b[i+3:]
	}
	return b
}
