package login

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"2droundserver/internal/store"
)

// clientConfig answers the new clients' first request: where the resource CDN is, which release of the resources to load, whether
// the app must be updated and which switches are on. Everything comes from client_config and client_release, so the admin page
// changes it without a deploy. Settings named "switch.*" are returned as the switches map; the rest of the known keys are fixed fields.
func (s *Server) clientConfig(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	plat := q.Get("platform")
	if plat == "" {
		http.Error(w, "platform is required", http.StatusBadRequest)
		return
	}
	cfg, err := s.Store.ClientConfig(plat)
	if err != nil {
		http.Error(w, "config", http.StatusInternalServerError)
		return
	}
	switches := map[string]string{}
	for k, v := range cfg {
		if strings.HasPrefix(k, "switch.") {
			switches[strings.TrimPrefix(k, "switch.")] = v
		}
	}
	out := map[string]any{
		"cdn_base":     cfg["cdn_base"],
		"notice":       cfg["notice"],
		"min_version":  cfg["min_version"],
		"update_url":   cfg["update_url"],
		"force_update": cfg["min_version"] != "" && versionLess(q.Get("ver"), cfg["min_version"]),
		"switches":     switches,
		"release":      nil,
	}
	rel, err := s.Store.LiveClientRelease(plat, q.Get("account"))
	switch {
	case err == nil:
		out["release"] = map[string]any{"version": rel.Version, "manifest_url": rel.ManifestURL, "manifest_hash": rel.ManifestHash}
	case err != store.ErrNotFound:
		http.Error(w, "release", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	json.NewEncoder(w).Encode(out)
}

// versionLess compares dotted numbers ("1.2.10" > "1.2.9"); an empty or unparsable client version counts as older.
func versionLess(a, b string) bool {
	pa, pb := strings.Split(a, "."), strings.Split(b, ".")
	for i := 0; i < len(pa) || i < len(pb); i++ {
		var x, y int
		if i < len(pa) {
			x, _ = strconv.Atoi(pa[i])
		}
		if i < len(pb) {
			y, _ = strconv.Atoi(pb[i])
		}
		if x != y {
			return x < y
		}
	}
	return false
}
