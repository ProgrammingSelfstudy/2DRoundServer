package login

import (
	"encoding/json"
	"errors"
	"html/template"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"2droundserver/internal/game"
	"2droundserver/internal/store"
)

// The payment pages. The client's pay call (sdkOnPay) hands the product to the phone's browser, which opens /pay/start; that opens an
// order for the role and shows /pay/order/<no>: the amount, the order number and the QR codes the operator put in the admin page.
// The player pays and the operator confirms the order in the admin page, which delivers it. The page asks again every few
// seconds and tells the player when the order is done.

var payPage = template.Must(template.New("pay").Parse(`<!doctype html>
<html lang="zh"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1">
<title>充值</title>
<style>
body{margin:0;background:#f4f1ea;color:#2b2b2b;font:16px/1.5 -apple-system,"PingFang SC",sans-serif}
main{max-width:460px;margin:0 auto;padding:16px}
.card{background:#fff;border-radius:12px;padding:16px;margin-bottom:12px;box-shadow:0 1px 3px rgba(0,0,0,.08)}
h1{font-size:20px;margin:0 0 8px}.amount{font-size:34px;font-weight:700;color:#c0392b}
.qr{display:flex;gap:12px;flex-wrap:wrap;justify-content:center}.qr figure{margin:0;text-align:center}
.qr img{width:200px;height:200px;object-fit:contain;border:1px solid #ddd;border-radius:8px}
.no{font-family:monospace;word-break:break-all;background:#f0ece2;padding:4px 8px;border-radius:6px}
.state{padding:10px 12px;border-radius:8px;margin-top:8px}.wait{background:#fff6dd}.done{background:#e3f6e3}.cancel{background:#eee}
small{color:#777}
</style></head><body><main>
<div class="card"><h1>{{.Name}}</h1>
<div class="amount">¥{{.Yuan}}</div>
<p>角色：{{.Role}}（{{.RID}}）</p>
<p>订单号：<span class="no">{{.No}}</span></p>
<div id="state" class="state {{.Class}}">{{.Text}}</div></div>
{{if .Waiting}}
<div class="card"><b>请扫码付款，金额与上面一致</b>
{{if .QRs}}<div class="qr">{{range .QRs}}<figure><img src="{{.Src}}" alt="{{.Name}}"><figcaption>{{.Name}}</figcaption></figure>{{end}}</div>
{{else}}<p>收款码还没有配置，请联系客服。</p>{{end}}
{{if .Notice}}<p>{{.Notice}}</p>{{end}}
<small>付款后请保持本页打开，到账后会自动显示。客服确认后到账，通常需要几分钟。</small></div>
<script>
(function(){var el=document.getElementById('state');
function tick(){fetch('/pay/status/{{.No}}').then(function(r){return r.json()}).then(function(d){
if(d.status==1){el.className='state done';el.textContent='已到账，请回到游戏查看';return}
if(d.status==2){el.className='state cancel';el.textContent='订单已取消';return}
setTimeout(tick,4000)}).catch(function(){setTimeout(tick,8000)})}
setTimeout(tick,4000)})();
</script>
{{end}}
</main></body></html>`))

type payQR struct {
	Name string
	Src  template.URL
}

var payChannels = []struct{ Key, Name string }{{"qr_wechat", "微信"}, {"qr_alipay", "支付宝"}}

func (s *Server) payOrderPage(w http.ResponseWriter, r *http.Request, no string) {
	o, err := s.Store.PayOrderByNo(no)
	if errors.Is(err, store.ErrNotFound) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		http.Error(w, "order", http.StatusInternalServerError)
		return
	}
	p, _ := game.ChargeProductByID(o.ChargeID)
	role, _ := s.Store.RoleByID(o.RID)
	cfg, err := s.Store.PayConfig()
	if err != nil {
		http.Error(w, "config", http.StatusInternalServerError)
		return
	}
	data := struct {
		Name, Yuan, Role, No, Class, Text, Notice string
		RID                                       int64
		Waiting                                   bool
		QRs                                       []payQR
	}{Name: p.Name, Yuan: strconv.FormatFloat(float64(o.AmountCents)/100, 'f', -1, 64), No: o.OrderNo, RID: o.RID, Notice: cfg["notice"]}
	if role != nil {
		data.Role = role.Name
	}
	switch o.Status {
	case store.PayDone:
		data.Class, data.Text = "done", "已到账，请回到游戏查看"
	case store.PayCancelled:
		data.Class, data.Text = "cancel", "订单已取消"
	default:
		data.Class, data.Text, data.Waiting = "wait", "等待付款", true
		for _, ch := range payChannels {
			if v := cfg[ch.Key]; v != "" {
				data.QRs = append(data.QRs, payQR{ch.Name, template.URL(v)})
			}
		}
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	if err := payPage.Execute(w, data); err != nil {
		s.Log.Printf("pay page: %v", err)
	}
}

func (s *Server) payStatus(w http.ResponseWriter, r *http.Request, no string) {
	o, err := s.Store.PayOrderByNo(no)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	json.NewEncoder(w).Encode(map[string]int{"status": o.Status})
}

// payStart opens an order from the call the client's pay function makes (the JSON of sdkOnPay: roleId, productId, price, ...)
// and sends the browser to its page. The price comes from the product table, not from the call.
func (s *Server) payStart(w http.ResponseWriter, r *http.Request) {
	var info struct {
		RoleID    json.Number `json:"roleId"`
		ProductID json.Number `json:"productId"`
	}
	raw := r.URL.Query().Get("info")
	if err := json.Unmarshal([]byte(raw), &info); err != nil {
		http.Error(w, "bad pay request", http.StatusBadRequest)
		return
	}
	rid, err1 := info.RoleID.Int64()
	cid, err2 := info.ProductID.Int64()
	if err1 != nil || err2 != nil {
		http.Error(w, "bad pay request", http.StatusBadRequest)
		return
	}
	role, err := s.Store.RoleByID(rid)
	if errors.Is(err, store.ErrNotFound) {
		http.Error(w, "no such role", http.StatusNotFound)
		return
	}
	if err != nil {
		http.Error(w, "role", http.StatusInternalServerError)
		return
	}
	o, err := game.CreateChargeOrder(s.Store, role, cid, time.Now())
	switch {
	case errors.Is(err, game.ErrNoProduct), errors.Is(err, game.ErrNotPayable):
		http.Error(w, "this product cannot be paid for here yet", http.StatusUnprocessableEntity)
		return
	case errors.Is(err, game.ErrTooManyOpen):
		http.Error(w, "too many orders wait for payment; pay or cancel them first", http.StatusTooManyRequests)
		return
	case err != nil:
		http.Error(w, "order", http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, "/pay/order/"+url.PathEscape(o.OrderNo), http.StatusFound)
}

// payRoute handles the paths under /pay/.
func (s *Server) payRoute(w http.ResponseWriter, r *http.Request) bool {
	p := r.URL.Path
	switch {
	case p == "/pay/start":
		s.payStart(w, r)
	case strings.HasPrefix(p, "/pay/order/"):
		s.payOrderPage(w, r, strings.TrimPrefix(p, "/pay/order/"))
	case strings.HasPrefix(p, "/pay/status/"):
		s.payStatus(w, r, strings.TrimPrefix(p, "/pay/status/"))
	default:
		return false
	}
	return true
}
