// Package admincoverage inventories persistence names for the admin coverage gate.
// It is used by development tools and tests, never by gameplay or admin handlers.
package admincoverage

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

type Entry struct {
	Scope   string   `json:"scope"`
	Key     string   `json:"key"`
	Sources []string `json:"sources"`
}

type Inventory struct {
	Modules      []Entry `json:"modules"`
	Tables       []Entry `json:"tables"`
	Declarations []Entry `json:"declarations"`
	StateCalls   int     `json:"state_calls"`
}

type scanner struct {
	fset      *token.FileSet
	constants map[string]ast.Expr
	objects   map[*ast.Object]ast.Expr
	functions map[string]*ast.FuncDecl
	fields    map[string][]ast.Expr
}

// Scan includes non-test game and store files, including persistence wrappers,
// even files excluded by today's build tags. Packages have separate namespaces.
// Unknown dynamic persistence expressions fail instead of silently disappearing.
func Scan(root string) (Inventory, error) {
	var out Inventory
	for _, dir := range []string{"internal/game", "internal/store"} {
		part, err := scanPackage(root, dir)
		if err != nil {
			return Inventory{}, err
		}
		out.Modules = append(out.Modules, part.Modules...)
		out.Declarations = append(out.Declarations, part.Declarations...)
		out.StateCalls += part.StateCalls
	}
	// A module can be accessed both by gameplay and by a store wrapper.
	merged := map[string]*Entry{}
	for _, e := range out.Modules {
		id := e.Scope + ":" + e.Key
		if merged[id] == nil {
			merged[id] = &Entry{Scope: e.Scope, Key: e.Key}
		}
		for _, source := range e.Sources {
			if !slices.Contains(merged[id].Sources, source) {
				merged[id].Sources = append(merged[id].Sources, source)
			}
		}
	}
	out.Modules = nil
	for _, e := range merged {
		slices.Sort(e.Sources)
		out.Modules = append(out.Modules, *e)
	}
	sortEntries(out.Modules)
	sortEntries(out.Declarations)
	var err error
	out.Tables, err = scanTables(root)
	return out, err
}

func scanPackage(root, dir string) (Inventory, error) {
	s := &scanner{
		fset: token.NewFileSet(), constants: map[string]ast.Expr{}, objects: map[*ast.Object]ast.Expr{},
		functions: map[string]*ast.FuncDecl{}, fields: map[string][]ast.Expr{},
	}
	files, err := filepath.Glob(filepath.Join(root, dir, "*.go"))
	if err != nil {
		return Inventory{}, err
	}
	if len(files) == 0 && dir == "internal/game" {
		return Inventory{}, fmt.Errorf("no game sources under %s", root)
	}
	var parsed []*ast.File
	for _, path := range files {
		if strings.HasSuffix(path, "_test.go") {
			continue
		}
		f, err := parser.ParseFile(s.fset, path, nil, 0)
		if err != nil {
			return Inventory{}, err
		}
		parsed = append(parsed, f)
		for _, d := range f.Decls {
			switch d := d.(type) {
			case *ast.FuncDecl:
				if d.Recv == nil {
					s.functions[d.Name.Name] = d
				}
			case *ast.GenDecl:
				if d.Tok != token.CONST {
					continue
				}
				var values []ast.Expr
				for _, spec := range d.Specs {
					v := spec.(*ast.ValueSpec)
					if len(v.Values) > 0 {
						values = v.Values
					}
					for i, name := range v.Names {
						if i < len(values) {
							s.constants[name.Name] = values[i]
						}
					}
				}
			}
		}
	}
	var moduleNames []*ast.Ident
	for _, f := range parsed {
		ast.Inspect(f, func(n ast.Node) bool {
			if decl, ok := n.(*ast.GenDecl); ok && decl.Tok == token.CONST {
				var values []ast.Expr
				for _, spec := range decl.Specs {
					v := spec.(*ast.ValueSpec)
					if len(v.Values) > 0 {
						values = v.Values
					}
					for i, name := range v.Names {
						if i < len(values) {
							s.objects[name.Obj] = values[i]
						}
						if strings.HasSuffix(name.Name, "Module") || strings.HasSuffix(name.Name, "Mod") {
							moduleNames = append(moduleNames, name)
						}
					}
				}
			}
			if a, ok := n.(*ast.AssignStmt); ok && len(a.Lhs) == len(a.Rhs) {
				for i, lhs := range a.Lhs {
					if field, ok := lhs.(*ast.SelectorExpr); ok {
						if typ := s.typeName(field.X, 0); typ != "" {
							key := typ + "." + field.Sel.Name
							s.fields[key] = append(s.fields[key], a.Rhs[i])
						}
					}
				}
			}
			if c, ok := n.(*ast.CompositeLit); ok {
				for _, elt := range c.Elts {
					if kv, ok := elt.(*ast.KeyValueExpr); ok {
						if name, ok := kv.Key.(*ast.Ident); ok {
							key := s.typeName(c.Type, 0) + "." + name.Name
							if !strings.HasPrefix(key, ".") {
								s.fields[key] = append(s.fields[key], kv.Value)
							}
						}
					}
				}
			}
			return true
		})
	}
	var out Inventory
	entries := map[string]*Entry{}
	add := func(scope, key string, pos token.Pos) {
		id := scope + ":" + key
		if entries[id] == nil {
			entries[id] = &Entry{Scope: scope, Key: key}
		}
		rel, _ := filepath.Rel(root, s.fset.Position(pos).Filename)
		if !slices.Contains(entries[id].Sources, filepath.ToSlash(rel)) {
			entries[id].Sources = append(entries[id].Sources, filepath.ToSlash(rel))
		}
	}
	for _, f := range parsed {
		var scanErr error
		ast.Inspect(f, func(n ast.Node) bool {
			if scanErr != nil {
				return false
			}
			c, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			method, ok := c.Fun.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			scope := ""
			switch method.Sel.Name {
			case "LoadState", "SaveState":
				scope = "role"
			case "LoadGlobal", "SaveGlobal":
				scope = "global"
			default:
				return true
			}
			if len(c.Args) != 3 {
				scanErr = fmt.Errorf("%s: unsupported persistence call", s.fset.Position(c.Pos()))
				return false
			}
			keys, err := s.keys(c.Args[1], 0)
			if err != nil {
				scanErr = fmt.Errorf("%s: %s module: %w", s.fset.Position(c.Pos()), method.Sel.Name, err)
				return false
			}
			out.StateCalls++
			for _, key := range keys {
				add(scope, key, c.Pos())
			}
			return true
		})
		if scanErr != nil {
			return Inventory{}, scanErr
		}
	}
	for _, name := range moduleNames {
		keys, err := s.keys(name, 0)
		if err != nil {
			return Inventory{}, fmt.Errorf("module constant %s: %w", name.Name, err)
		}
		for _, key := range keys {
			rel, _ := filepath.Rel(root, s.fset.Position(name.Pos()).Filename)
			out.Declarations = append(out.Declarations, Entry{"declaration", key, []string{filepath.ToSlash(rel)}})
		}
	}
	for _, e := range entries {
		slices.Sort(e.Sources)
		out.Modules = append(out.Modules, *e)
	}
	sortEntries(out.Modules)
	sortEntries(out.Declarations)
	return out, nil
}

func sortEntries(entries []Entry) {
	slices.SortFunc(entries, func(a, b Entry) int { return strings.Compare(a.Scope+":"+a.Key, b.Scope+":"+b.Key) })
}

func (s *scanner) typeName(e ast.Expr, depth int) string {
	if depth > 30 {
		return ""
	}
	switch e := e.(type) {
	case *ast.StarExpr:
		return s.typeName(e.X, depth+1)
	case *ast.UnaryExpr:
		return s.typeName(e.X, depth+1)
	case *ast.CompositeLit:
		return s.typeName(e.Type, depth+1)
	case *ast.Ident:
		if e.Obj == nil || e.Obj.Kind == ast.Typ {
			return e.Name
		}
		switch d := e.Obj.Decl.(type) {
		case *ast.Field:
			return s.typeName(d.Type, depth+1)
		case *ast.AssignStmt:
			for i, lhs := range d.Lhs {
				if id, ok := lhs.(*ast.Ident); ok && id.Name == e.Name && i < len(d.Rhs) {
					return s.typeName(d.Rhs[i], depth+1)
				}
			}
		case *ast.ValueSpec:
			if d.Type != nil {
				return s.typeName(d.Type, depth+1)
			}
		}
	case *ast.CallExpr:
		if id, ok := e.Fun.(*ast.Ident); ok {
			if f := s.functions[id.Name]; f != nil && f.Type.Results != nil && len(f.Type.Results.List) == 1 {
				return s.typeName(f.Type.Results.List[0].Type, depth+1)
			}
		}
	}
	return ""
}

func (s *scanner) keys(e ast.Expr, depth int) ([]string, error) {
	if depth > 30 {
		return nil, fmt.Errorf("cyclic or deeply nested module expression")
	}
	unknown := fmt.Errorf("cannot resolve %T; extend the inventory scanner explicitly", e)
	switch e := e.(type) {
	case *ast.ParenExpr:
		return s.keys(e.X, depth+1)
	case *ast.BasicLit:
		if e.Kind == token.STRING {
			value, err := strconv.Unquote(e.Value)
			if err == nil && value != "" && !strings.Contains(value, "*") {
				return []string{value}, nil
			}
		}
	case *ast.Ident:
		if e.Obj != nil {
			if value := s.objects[e.Obj]; value != nil {
				return s.keys(value, depth+1)
			}
			// A local variable must not accidentally resolve to a shadowed package constant.
			if e.Obj.Kind != ast.Con {
				return nil, unknown
			}
		}
		if value := s.constants[e.Name]; value != nil {
			return s.keys(value, depth+1)
		}
	case *ast.BinaryExpr:
		if e.Op == token.ADD {
			left, err := s.keys(e.X, depth+1)
			if err != nil {
				return nil, err
			}
			right, err := s.keys(e.Y, depth+1)
			var result []string
			for _, prefix := range left {
				if strings.Contains(prefix, "*") {
					return nil, fmt.Errorf("wildcard in middle of module name")
				}
				if err != nil {
					result = append(result, prefix+"*")
				} else {
					for _, suffix := range right {
						result = append(result, prefix+suffix)
					}
				}
			}
			return result, nil
		}
	case *ast.SelectorExpr:
		field := s.typeName(e.X, 0) + "." + e.Sel.Name
		if bindings := s.fields[field]; len(bindings) > 0 {
			var keys []string
			for _, value := range bindings {
				resolved, err := s.keys(value, depth+1)
				if err != nil {
					return nil, fmt.Errorf("%s: %w", field, err)
				}
				keys = append(keys, resolved...)
			}
			slices.Sort(keys)
			return slices.Compact(keys), nil
		}
	case *ast.CallExpr:
		if id, ok := e.Fun.(*ast.Ident); ok {
			if f := s.functions[id.Name]; f != nil {
				var keys []string
				var resolveErr error
				ast.Inspect(f.Body, func(n ast.Node) bool {
					if _, nested := n.(*ast.FuncLit); nested {
						return false
					}
					if ret, ok := n.(*ast.ReturnStmt); ok && len(ret.Results) == 1 {
						resolved, err := s.keys(ret.Results[0], depth+1)
						if err != nil {
							resolveErr = err
						}
						keys = append(keys, resolved...)
					}
					return true
				})
				if resolveErr == nil && len(keys) > 0 {
					slices.Sort(keys)
					return slices.Compact(keys), nil
				}
			}
		}
	}
	return nil, unknown
}

var createTable = regexp.MustCompile("(?i)\\bCREATE\\s+TABLE\\s+(?:IF\\s+NOT\\s+EXISTS\\s+)?[\"`\\[]?([a-zA-Z_][a-zA-Z0-9_]*)")

func scanTables(root string) ([]Entry, error) {
	paths, err := filepath.Glob(filepath.Join(root, "internal/store/migrations/*.sql"))
	if err != nil {
		return nil, err
	}
	goFiles, _ := filepath.Glob(filepath.Join(root, "internal/store/*.go"))
	paths = append(paths, goFiles...)
	entries := map[string]*Entry{}
	for _, path := range paths {
		if strings.HasSuffix(path, "_test.go") {
			continue
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		var sql []string
		if strings.HasSuffix(path, ".sql") {
			sql = append(sql, string(data))
		} else {
			f, err := parser.ParseFile(token.NewFileSet(), path, data, 0)
			if err != nil {
				return nil, err
			}
			ast.Inspect(f, func(n ast.Node) bool {
				if lit, ok := n.(*ast.BasicLit); ok && lit.Kind == token.STRING {
					value, _ := strconv.Unquote(lit.Value)
					sql = append(sql, value)
				}
				return true
			})
		}
		for _, text := range sql {
			text = regexp.MustCompile(`(?m)--[^\n]*|/\*(?s:.*?)\*/`).ReplaceAllString(text, " ")
			for _, match := range createTable.FindAllStringSubmatch(text, -1) {
				key := strings.ToLower(match[1])
				if entries[key] == nil {
					entries[key] = &Entry{Scope: "table", Key: key}
				}
				rel, _ := filepath.Rel(root, path)
				if !slices.Contains(entries[key].Sources, filepath.ToSlash(rel)) {
					entries[key].Sources = append(entries[key].Sources, filepath.ToSlash(rel))
				}
			}
		}
	}
	var out []Entry
	for _, e := range entries {
		slices.Sort(e.Sources)
		out = append(out, *e)
	}
	sortEntries(out)
	return out, nil
}
