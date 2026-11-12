package admin

import (
	"errors"
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"2droundserver/internal/store"
)

var (
	clientPlatforms = map[string]bool{"*": true, "wechat": true, "douyin": true, "h5": true, "android": true, "ios": true}
	reVersion       = regexp.MustCompile(`^[0-9A-Za-z._-]{1,32}$`)
	reCfgKey        = regexp.MustCompile(`^(cdn_base|ws_url|notice|min_version|update_url|switch\.[a-z0-9_]{1,32})$`)
	reWSURL         = regexp.MustCompile(`^wss?://[^\s/]+(/\S*)?$`)
)

// clientSettings lists the settings of one platform ("*" when none is given) and its releases.
func (s *Server) clientSettings(w http.ResponseWriter, r *http.Request, p *principal) {
	plat := r.URL.Query().Get("platform")
	if plat == "" {
		plat = "*"
	}
	cfg, err := s.Store.ClientConfig(plat)
	if err != nil {
		internalErr(w, s, err)
		return
	}
	rels, err := s.Store.ClientReleases(r.URL.Query().Get("platform"), 100)
	if err != nil {
		internalErr(w, s, err)
		return
	}
	writeJSON(w, 200, map[string]any{"config": cfg, "releases": rels})
}

// setClientSetting writes one setting; an empty value removes it. Keys are fixed so a typo cannot create a setting nothing reads.
func (s *Server) setClientSetting(w http.ResponseWriter, r *http.Request, p *principal) {
	var q struct {
		Platform string `json:"platform"`
		Key      string `json:"key"`
		Value    string `json:"value"`
	}
	if !decode(w, r, &q) {
		return
	}
	if !clientPlatforms[q.Platform] || !reCfgKey.MatchString(q.Key) || len([]rune(q.Value)) > 500 {
		fail(w, http.StatusUnprocessableEntity, "platform or key is not known, or the value is longer than 500 characters")
		return
	}
	if q.Key == "ws_url" && q.Value != "" && !reWSURL.MatchString(q.Value) {
		fail(w, http.StatusUnprocessableEntity, "ws_url must be a ws:// or wss:// address")
		return
	}
	if err := s.Store.SetClientConfig(q.Platform, q.Key, q.Value); err != nil {
		internalErr(w, s, err)
		return
	}
	s.audit(r, p, "client.config", q.Platform+"/"+q.Key, nil)
	writeJSON(w, 200, map[string]bool{"ok": true})
}

// createClientRelease registers a draft release for a manifest the pipeline uploaded; it is not served until published.
func (s *Server) createClientRelease(w http.ResponseWriter, r *http.Request, p *principal) {
	var q store.ClientRelease
	if !decode(w, r, &q) {
		return
	}
	if !clientPlatforms[q.Platform] || q.Platform == "*" || !reVersion.MatchString(q.Version) ||
		!(strings.HasPrefix(q.ManifestURL, "https://") || strings.HasPrefix(q.ManifestURL, "http://")) || len([]rune(q.Note)) > 200 {
		fail(w, http.StatusUnprocessableEntity, "platform, version or manifest_url is invalid")
		return
	}
	rel, err := s.Store.CreateClientRelease(q, s.Now().Unix())
	if errors.Is(err, store.ErrConflict) {
		fail(w, http.StatusConflict, "that platform already has this version")
		return
	}
	if err != nil {
		internalErr(w, s, err)
		return
	}
	s.audit(r, p, "client.release.create", q.Platform+"/"+q.Version, nil)
	writeJSON(w, 200, rel)
}

// setClientReleaseState publishes or rolls back a release and may set its tester whitelist in the same call.
func (s *Server) setClientReleaseState(w http.ResponseWriter, r *http.Request, p *principal) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	var q struct {
		Status    int     `json:"status"`
		Whitelist *string `json:"whitelist"`
	}
	if !decode(w, r, &q) {
		return
	}
	if q.Status != store.ReleasePublished && q.Status != store.ReleaseRolledBack && q.Status != store.ReleaseDraft {
		fail(w, http.StatusUnprocessableEntity, "status must be 0 draft, 1 published or 2 rolled back")
		return
	}
	var err error
	if q.Whitelist != nil {
		err = s.Store.SetClientReleaseWhitelist(id, *q.Whitelist)
	}
	if err == nil {
		err = s.Store.SetClientReleaseStatus(id, q.Status, s.Now().Unix())
	}
	if errors.Is(err, store.ErrNotFound) {
		fail(w, 404, "no such release")
		return
	}
	if err != nil {
		internalErr(w, s, err)
		return
	}
	s.audit(r, p, "client.release.state", strconv.FormatInt(id, 10), q)
	writeJSON(w, 200, map[string]bool{"ok": true})
}
