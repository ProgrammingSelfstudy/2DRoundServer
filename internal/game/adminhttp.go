package game

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"2droundserver/internal/proto"
)

// AssetLabelID resolves a currency name of the client's asset table (gold, coin, ...).
func AssetLabelID(name string) (int64, bool) { id, ok := assetLabelByName[name]; return id, ok }

// AssetName is the reverse lookup: the currency name of a label id.
func AssetName(label int64) (string, bool) { n, ok := assetNameByLabel[label]; return n, ok }

// AssetLabelNames lists the known currency names.
func AssetLabelNames() []string {
	out := make([]string, 0, len(assetLabelByName))
	for n := range assetLabelByName {
		out = append(out, n)
	}
	return out
}

// AdminHandler is the gateway's control endpoint. It has no authentication and
// must only be bound to a loopback address (gateway.admin_addr); the admin
// service in front of it is what authenticates people.
//
//	POST /push/mail?id=N   push mail N (10803) to the online roles that are due it
//	POST /push/notice?id=N         show (10950) or drop (10951) notice N for the online roles
//	POST /push/notice/remove?id=N  tell the online roles notice N was deleted (10951)
//	POST /push/broadcast?msg=TEXT  scroll TEXT across the top of every online player's screen
//	POST /push/refresh?rid=N[&heroes=1,2][&skins=1]  resend the currencies (10302) and backpack (10500) of an online role, the heroes with those partner ids (11001) and, with skins=1, its skins (11020)
//	POST /push/charge?rid=N  resend the diamonds, backpack, VIP (10301) and the top-up page (16700) of an online role after an order was paid
//	POST /kick?rid=N       disconnect an online role (used after a ban)
//	GET  /online           number of roles online
//	GET  /online?rid=N     whether role N is online
func (m *MailService) AdminHandler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /push/mail", func(w http.ResponseWriter, r *http.Request) {
		id, err := strconv.ParseInt(r.URL.Query().Get("id"), 10, 64)
		if err != nil || id <= 0 {
			http.Error(w, "id is required", http.StatusBadRequest)
			return
		}
		n, err := m.PushMail(id)
		if err != nil {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}
		json.NewEncoder(w).Encode(map[string]int{"pushed": n})
	})
	if m.Notices != nil {
		mux.HandleFunc("POST /push/notice", func(w http.ResponseWriter, r *http.Request) {
			id, err := strconv.ParseInt(r.URL.Query().Get("id"), 10, 64)
			if err != nil || id <= 0 {
				http.Error(w, "id is required", http.StatusBadRequest)
				return
			}
			shown, dropped, err := m.Notices.PushNotice(id)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			json.NewEncoder(w).Encode(map[string]int{"pushed": shown, "dropped": dropped})
		})
		mux.HandleFunc("POST /push/notice/remove", func(w http.ResponseWriter, r *http.Request) {
			id, err := strconv.ParseInt(r.URL.Query().Get("id"), 10, 64)
			if err != nil || id <= 0 {
				http.Error(w, "id is required", http.StatusBadRequest)
				return
			}
			dropped, err := m.Notices.DropNotice(id)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			json.NewEncoder(w).Encode(map[string]int{"dropped": dropped})
		})
	}
	mux.HandleFunc("POST /push/broadcast", func(w http.ResponseWriter, r *http.Request) {
		msg := r.URL.Query().Get("msg")
		if !validChatMsg(msg) {
			http.Error(w, "msg is required (1 to 100 characters, no control characters)", http.StatusBadRequest)
			return
		}
		json.NewEncoder(w).Encode(map[string]int{"pushed": m.Broadcast(msg)})
	})
	mux.HandleFunc("POST /push/refresh", func(w http.ResponseWriter, r *http.Request) {
		rid, err := strconv.ParseInt(r.URL.Query().Get("rid"), 10, 64)
		if err != nil || rid <= 0 {
			http.Error(w, "rid is required", http.StatusBadRequest)
			return
		}
		sess := m.Hub.Online(rid)
		if sess != nil {
			if err := sendAssets(sess, m.Store, rid); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			if err := sendBackpack(sess, m.Store); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			var heroes []proto.Message
			for _, f := range strings.Split(r.URL.Query().Get("heroes"), ",") {
				pid, err := strconv.ParseInt(f, 10, 64)
				if err != nil {
					continue
				}
				if p, err := m.Store.PartnerByID(rid, pid); err == nil {
					heroes = append(heroes, partnerMessage(*p))
				}
			}
			if r.URL.Query().Get("skins") == "1" {
				if err := sendSkins(sess, m.Store, rid); err != nil {
					http.Error(w, err.Error(), http.StatusInternalServerError)
					return
				}
			}
			if len(heroes) > 0 {
				if err := sess.Send(11001, proto.Message{"partners": heroes}); err != nil {
					http.Error(w, err.Error(), http.StatusInternalServerError)
					return
				}
			}
		}
		json.NewEncoder(w).Encode(map[string]bool{"refreshed": sess != nil})
	})
	mux.HandleFunc("POST /push/charge", func(w http.ResponseWriter, r *http.Request) {
		rid, err := strconv.ParseInt(r.URL.Query().Get("rid"), 10, 64)
		if err != nil || rid <= 0 {
			http.Error(w, "rid is required", http.StatusBadRequest)
			return
		}
		sess := m.Hub.Online(rid)
		if sess != nil {
			if err := SendChargeState(sess, m.Store, rid); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
		}
		json.NewEncoder(w).Encode(map[string]bool{"refreshed": sess != nil})
	})
	mux.HandleFunc("POST /kick", func(w http.ResponseWriter, r *http.Request) {
		rid, err := strconv.ParseInt(r.URL.Query().Get("rid"), 10, 64)
		if err != nil || rid <= 0 {
			http.Error(w, "rid is required", http.StatusBadRequest)
			return
		}
		sess := m.Hub.Online(rid)
		if sess != nil {
			sess.Send(10310, proto.Message{"is_show": 1, "msg": "账号已被封禁或被管理员下线"}) // 10310: the client's "you were disconnected" message
			sess.Close()
		}
		json.NewEncoder(w).Encode(map[string]bool{"kicked": sess != nil})
	})
	mux.HandleFunc("GET /online", func(w http.ResponseWriter, r *http.Request) {
		if q := r.URL.Query().Get("rid"); q != "" {
			rid, err := strconv.ParseInt(q, 10, 64)
			if err != nil || rid <= 0 {
				http.Error(w, "bad rid", http.StatusBadRequest)
				return
			}
			json.NewEncoder(w).Encode(map[string]bool{"online": m.Hub.Online(rid) != nil})
			return
		}
		json.NewEncoder(w).Encode(map[string]int{"online": m.Hub.Count()})
	})
	return mux
}
