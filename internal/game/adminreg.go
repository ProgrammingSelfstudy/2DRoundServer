package game

import (
	"fmt"
	"reflect"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"2droundserver/internal/store"
)

// AdminModule describes persisted data, not a gameplay registration or a new rule.
// Only explicitly registered charge resets are enabled; JSON editing stays off.
type AdminModule struct {
	Key, Title, Window string
	Scope              string
	New                func() any
	View               func(*store.Store, string, string, time.Time) (any, error) // role/server id, validated concrete module key
	Resets             []AdminReset
	Rank               func(*store.Store, string, int) ([]AdminRankRow, error)
	Editable           bool
	Match              func(string) bool // required for concrete keys of a prefix family
}

type AdminRankRow struct {
	Rank int64 `json:"rank"`
	Data any   `json:"data"`
}

// Apply only changes the freshly decoded state in memory. Persistence and audit
// are owned by the admin handler. "all" must be explicitly whitelisted and have
// no Apply function; it requests deletion. Items are enumerated from each module's
// configuration, never accepted as arbitrary strings by the admin handler.
type AdminReset struct {
	Item, Title string
	Apply       func(*store.Store, int64, any, time.Time) error
}

var adminRegistry = map[string]AdminModule{}
var adminKeyPattern = regexp.MustCompile(`^[a-z][a-z0-9_:]*\*?$`)

func adminState[T any]() any { return new(T) }

func registerAdminModule(m AdminModule) {
	if !adminKeyPattern.MatchString(m.Key) || m.Title == "" || m.Window == "" || (m.Scope != "role" && m.Scope != "global") || m.New == nil {
		panic(fmt.Sprintf("game: invalid admin module %s/%s", m.Scope, m.Key))
	}
	v := reflect.ValueOf(m.New())
	if !v.IsValid() || v.Kind() != reflect.Pointer || v.IsNil() {
		panic("game: admin module constructor must return a non-nil state pointer: " + m.Key)
	}
	if strings.HasSuffix(m.Key, "*") && m.Match == nil {
		panic("game: admin prefix requires a suffix validator: " + m.Key)
	}
	items := map[string]bool{}
	for _, reset := range m.Resets {
		if reset.Item == "" || reset.Title == "" || items[reset.Item] || (reset.Item == "all") != (reset.Apply == nil) {
			panic("game: invalid admin reset: " + m.Key + "/" + reset.Item)
		}
		items[reset.Item] = true
	}
	id := m.Scope + ":" + m.Key
	if _, exists := adminRegistry[id]; exists {
		panic("game: duplicate admin module " + id)
	}
	m.Resets = slices.Clone(m.Resets)
	adminRegistry[id] = m
}

// AdminModules returns copies in stable scope/key order. Callers cannot mutate
// the initialization-only registry by changing the returned slice or Resets.
func AdminModules() []AdminModule {
	out := make([]AdminModule, 0, len(adminRegistry))
	for _, m := range adminRegistry {
		m.Resets = slices.Clone(m.Resets)
		out = append(out, m)
	}
	slices.SortFunc(out, func(a, b AdminModule) int { return strings.Compare(a.Scope+":"+a.Key, b.Scope+":"+b.Key) })
	return out
}

// FindAdminModule accepts concrete persisted keys, never the wildcard itself.
// Prefix matches validate known first-charge windows or canonical positive GIDs.
func FindAdminModule(scope, key string) (AdminModule, bool) {
	if len(key) > 128 || strings.Contains(key, "*") || !adminKeyPattern.MatchString(key) {
		return AdminModule{}, false
	}
	if m, ok := adminRegistry[scope+":"+key]; ok {
		m.Resets = slices.Clone(m.Resets)
		return m, true
	}
	for _, m := range AdminModules() {
		if m.Scope == scope && strings.HasSuffix(m.Key, "*") && strings.HasPrefix(key, strings.TrimSuffix(m.Key, "*")) && m.Match(key) {
			return m, true
		}
	}
	return AdminModule{}, false
}

func adminPositiveSuffix(prefix string) func(string) bool {
	return func(key string) bool {
		if !strings.HasPrefix(key, prefix) {
			return false
		}
		suffix := strings.TrimPrefix(key, prefix)
		id, err := strconv.ParseInt(suffix, 10, 64)
		return err == nil && id > 0 && strconv.FormatInt(id, 10) == suffix
	}
}
