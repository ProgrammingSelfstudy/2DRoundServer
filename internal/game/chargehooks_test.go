package game

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// The login service opens an order and the admin page confirms it, and neither runs the gateway's Register functions: a guard or a
// hook registered inside one is invisible to them, so the product could not be bought. They belong in init().
func TestChargeHooksAreRegisteredInInit(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil || len(files) == 0 {
		t.Fatal("no source files", err)
	}
	top := regexp.MustCompile(`^func (\([^)]*\) )?(\w+)`)
	call := regexp.MustCompile(`\bRegisterCharge(Hook|Guard)\(`)
	found := 0
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		src, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		fn := ""
		for i, line := range strings.Split(string(src), "\n") {
			if m := top.FindStringSubmatch(line); m != nil {
				fn = m[2]
				continue // the line of a function's own declaration is not a call
			}
			if strings.HasPrefix(strings.TrimSpace(line), "//") || !call.MatchString(line) {
				continue
			}
			found++
			if fn != "init" {
				t.Errorf("%s:%d registers a charge hook or guard in %s, not in init()", f, i+1, fn)
			}
		}
	}
	if found < 15 {
		t.Fatalf("only %d registrations found: the scan is broken", found)
	}
}
