package login

import (
	"io"
	"mime"
	"net/http"
	"net/url"
	"slices"
	"unicode/utf8"

	"2droundserver/internal/store"
)

const accountFormLimit = 8 << 10

// The native client uses only the query, even on POST. An H5 selector in
// either source opts into validation without merging the two sets of fields.
func accountForm(w http.ResponseWriter, r *http.Request) (query, form url.Values, h5, ok bool) {
	w.Header().Set("Cache-Control", "no-store")
	query, queryErr := url.ParseQuery(r.URL.RawQuery)
	mediaType, _, mediaErr := mime.ParseMediaType(r.Header.Get("Content-Type"))
	var body []byte
	var formErr, readErr error
	if r.Method == http.MethodPost && mediaType == "application/x-www-form-urlencoded" && r.Body != nil {
		body, readErr = io.ReadAll(io.LimitReader(r.Body, accountFormLimit+1))
		// Parse any prefix received so a declared H5 request still gets a
		// client error when a later value is malformed or exceeds the limit.
		form, formErr = url.ParseQuery(string(body))
	}
	if !slices.Contains(query["client"], "h5") && !slices.Contains(form["client"], "h5") {
		return query, nil, false, true
	}
	if len(body) > accountFormLimit || len(r.URL.RawQuery) > accountFormLimit {
		http.Error(w, "account form too large", http.StatusRequestEntityTooLarge)
		return nil, nil, false, false
	}
	if queryErr != nil || readErr != nil || formErr != nil || (form != nil && mediaErr != nil) ||
		!validH5Selector(query["client"]) || !validH5Selector(form["client"]) {
		http.Error(w, "invalid account form", http.StatusBadRequest)
		return nil, nil, false, false
	}
	return query, form, true, true
}

func validH5Selector(values []string) bool {
	return len(values) == 0 || (len(values) == 1 && values[0] == "h5")
}

func accountCredentials(w http.ResponseWriter, r *http.Request) (plat, user, pass string, ok bool) {
	query, form, h5, ok := accountForm(w, r)
	if !ok {
		return "", "", "", false
	}
	if !h5 {
		return platform, query.Get("username"), query.Get("password"), true
	}
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		http.Error(w, "H5 credentials require POST", http.StatusMethodNotAllowed)
		return "", "", "", false
	}
	user, pass = form.Get("username"), form.Get("password")
	if query.Has("username") || query.Has("password") || len(form["username"]) != 1 || len(form["password"]) != 1 ||
		user == "" || !utf8.ValidString(user) || pass == "" || len(pass) > 72 {
		http.Error(w, "invalid account form", http.StatusBadRequest)
		return "", "", "", false
	}
	return store.PlatformH5, user, pass, true
}

// The H5 token call can still send its username in the query. It carries
// no password and must name exactly one account in either source.
func tokenUsername(w http.ResponseWriter, r *http.Request) (plat, user string, ok bool) {
	query, form, h5, ok := accountForm(w, r)
	if !ok {
		return "", "", false
	}
	if !h5 {
		return platform, query.Get("username"), true
	}
	if r.Method != http.MethodGet && r.Method != http.MethodPost {
		w.Header().Set("Allow", "GET, POST")
		http.Error(w, "H5 token requests require GET or POST", http.StatusMethodNotAllowed)
		return "", "", false
	}
	if query.Has("password") || form.Has("password") || len(query["username"])+len(form["username"]) > 1 {
		http.Error(w, "invalid account form", http.StatusBadRequest)
		return "", "", false
	}
	user = query.Get("username")
	if form.Has("username") {
		user = form.Get("username")
	}
	if !utf8.ValidString(user) {
		http.Error(w, "invalid account form", http.StatusBadRequest)
		return "", "", false
	}
	return store.PlatformH5, user, true
}
