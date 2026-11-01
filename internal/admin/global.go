package admin

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strconv"

	"2droundserver/internal/game"
	"2droundserver/internal/store"
)

// No global-state reads/writes here. The gateway resolves the server and owns
// service locking; transport/unexpected upstream failures never fall back to SQL.
func (s *Server) globalProxy(w http.ResponseWriter, r *http.Request, path string, body any) (json.RawMessage, bool) {
	var reader io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			internalErr(w, s, err)
			return nil, false
		}
		reader = bytes.NewReader(b)
	}
	if s.GatewayURL == "" {
		fail(w, 502, "gateway unavailable")
		return nil, false
	}
	req, err := http.NewRequestWithContext(r.Context(), r.Method, s.GatewayURL+path, reader)
	if err != nil {
		fail(w, 502, "gateway unavailable")
		return nil, false
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := s.HTTP.Do(req)
	if err != nil {
		fail(w, 502, "gateway unavailable")
		return nil, false
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
	if err != nil || !json.Valid(raw) {
		fail(w, 502, "invalid gateway response")
		return nil, false
	}
	if resp.StatusCode != 200 {
		status := 502
		switch resp.StatusCode {
		case 404, 405, 409, 422:
			status = resp.StatusCode
		}
		var out any
		json.Unmarshal(raw, &out)
		writeJSON(w, status, out)
		return nil, false
	}
	return raw, true
}
func (s *Server) globalServer(w http.ResponseWriter, r *http.Request) (string, bool) {
	id := r.PathValue("id")
	_, err := s.Store.Server(id)
	if errors.Is(err, store.ErrNotFound) {
		fail(w, 404, "no such server")
		return "", false
	}
	if err != nil {
		internalErr(w, s, err)
		return "", false
	}
	return id, true
}
func (s *Server) serverModules(w http.ResponseWriter, r *http.Request, p *principal) {
	s.globalRead(w, r, "/ops/global", false)
}
func (s *Server) serverModule(w http.ResponseWriter, r *http.Request, p *principal) {
	s.globalRead(w, r, "/ops/global", true)
}
func (s *Server) serverRank(w http.ResponseWriter, r *http.Request, p *principal) {
	s.globalRead(w, r, "/ops/rank", true)
}
func (s *Server) serverSeason(w http.ResponseWriter, r *http.Request, p *principal) {
	s.globalRead(w, r, "/ops/season", true)
}
func (s *Server) serverOverview(w http.ResponseWriter, r *http.Request, p *principal) {
	s.globalRead(w, r, "/ops/overview", false)
}
func (s *Server) globalRead(w http.ResponseWriter, r *http.Request, path string, keyed bool) {
	srv, ok := s.globalServer(w, r)
	if !ok {
		return
	}
	q := url.Values{"srv": {srv}}
	if keyed {
		key := r.PathValue("key")
		if _, ok := game.FindAdminModule("global", key); !ok {
			fail(w, 404, "no such global module")
			return
		}
		q.Set("key", key)
	}
	if path == "/ops/rank" {
		limit, _ := page(r)
		q.Set("limit", strconv.Itoa(limit))
	}
	raw, ok := s.globalProxy(w, r, path+"?"+q.Encode(), nil)
	if !ok {
		return
	}
	writeJSON(w, 200, raw)
}
