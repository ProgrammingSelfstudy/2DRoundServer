package login

import (
	"bytes"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"testing"

	"2droundserver/internal/game"
)

func TestPayPages(t *testing.T) {
	s, ts := newTestServer(t)
	if err := s.Store.EnsureServer(s.Cfg.DefaultServer()); err != nil {
		t.Fatal(err)
	}
	acc, err := s.Store.CreateAccount("symlf", "payer", "x", "")
	if err != nil {
		t.Fatal(err)
	}
	role, err := s.Store.CreateRole(acc.ID, "symlf_1", "付款人", 1, 1, 10401)
	if err != nil {
		t.Fatal(err)
	}
	png := "data:image/png;base64,iVBORw0KGgo="
	s.Store.SetPayConfig("qr_wechat", png)
	s.Store.SetPayConfig("notice", "备注订单号")

	// the call the client's pay function makes
	info := `{"roleId":"` + itoa(role.RID) + `","productId":4,"price":30,"amount":30}`
	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	resp, err := client.Get(ts.URL + "/pay/start?info=" + url.QueryEscape(info))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	loc := resp.Header.Get("Location")
	if resp.StatusCode != http.StatusFound || !strings.HasPrefix(loc, "/pay/order/P") {
		t.Fatalf("start: %d %q", resp.StatusCode, loc)
	}
	code, body := get(t, "GET", ts.URL+loc)
	if code != 200 || !strings.Contains(body, "¥30") || !strings.Contains(body, "普通充值30") || !strings.Contains(body, "付款人") || !strings.Contains(body, png) || !strings.Contains(body, "备注订单号") || !strings.Contains(body, "等待付款") {
		t.Fatalf("order page: %d %s", code, body)
	}
	no := strings.TrimPrefix(loc, "/pay/order/")
	if _, body := get(t, "GET", ts.URL+"/pay/status/"+no); !strings.Contains(body, `"status":0`) {
		t.Fatalf("status: %s", body)
	}
	// the price is the table's, not the call's; a product nobody delivers, a missing role and junk are refused
	for _, bad := range []string{`{"roleId":"` + itoa(role.RID) + `","productId":101}`, `{"roleId":"999999","productId":4}`, `nonsense`, `{"roleId":"x","productId":4}`} {
		r2, err := client.Get(ts.URL + "/pay/start?info=" + url.QueryEscape(bad))
		if err != nil {
			t.Fatal(err)
		}
		io.Copy(io.Discard, r2.Body)
		r2.Body.Close()
		if r2.StatusCode < 400 {
			t.Fatalf("%s: %d", bad, r2.StatusCode)
		}
	}
	// a payment screenshot: only images, size capped, kept for the operator, and refused once the order is done
	pngBytes := []byte("\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR")
	post := func(body []byte) int {
		resp, err := http.Post(ts.URL+"/pay/proof/"+no, "application/octet-stream", bytes.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
		return resp.StatusCode
	}
	if c := post([]byte("<html>not an image</html>")); c != http.StatusUnsupportedMediaType {
		t.Fatalf("junk proof: %d", c)
	}
	if c := post(append(pngBytes, make([]byte, maxProofBytes)...)); c != http.StatusRequestEntityTooLarge {
		t.Fatalf("big proof: %d", c)
	}
	if c := post(pngBytes); c != 200 {
		t.Fatalf("proof: %d", c)
	}
	if mime, data, err := s.Store.PayProof(no); err != nil || mime != "image/png" || !bytes.Equal(data, pngBytes) {
		t.Fatalf("stored proof: %q %v", mime, err)
	}
	if o, _ := s.Store.PayOrderByNo(no); o.ProofAt == 0 {
		t.Fatal("proof_at not set")
	}
	if _, body := get(t, "GET", ts.URL+loc); !strings.Contains(body, "已上传，等待客服核对") {
		t.Fatalf("page does not show the proof: %s", body)
	}
	// once confirmed the page and the status say so, and the QR codes are gone
	if _, err := game.ConfirmChargeOrder(s.Store, no, "t", ""); err != nil {
		t.Fatal(err)
	}
	if _, body := get(t, "GET", ts.URL+"/pay/status/"+no); !strings.Contains(body, `"status":1`) {
		t.Fatalf("status after: %s", body)
	}
	if _, body := get(t, "GET", ts.URL+loc); !strings.Contains(body, "已到账") || strings.Contains(body, png) {
		t.Fatalf("page after: %s", body)
	}
	if c := post(pngBytes); c != http.StatusConflict {
		t.Fatalf("proof after confirm: %d", c)
	}
	if code, _ := get(t, "GET", ts.URL+"/pay/order/nope"); code != 404 {
		t.Fatalf("unknown order: %d", code)
	}
}

func itoa(n int64) string { return strconv.FormatInt(n, 10) }
