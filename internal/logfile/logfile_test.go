package logfile

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func read(t *testing.T, p string) string {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestRollsOverAndKeepsTheNewest(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "logs")
	w, err := Open(dir, "gw", 30, 2) // 30 bytes, keep 2 rolled files
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	for i := 1; i <= 7; i++ {
		fmt.Fprintf(w, "line-%02d-xxxxxxxxxxx\n", i) // 20 bytes: one line per file fits, two do not
	}
	// the newest line is in the current file; .1 and .2 hold the two before it; the rest are gone
	if got := read(t, filepath.Join(dir, "gw.log")); got != "line-07-xxxxxxxxxxx\n" {
		t.Fatalf("current: %q", got)
	}
	if got := read(t, filepath.Join(dir, "gw.log.1")); got != "line-06-xxxxxxxxxxx\n" {
		t.Fatalf(".1: %q", got)
	}
	if got := read(t, filepath.Join(dir, "gw.log.2")); got != "line-05-xxxxxxxxxxx\n" {
		t.Fatalf(".2: %q", got)
	}
	if _, err := os.Stat(filepath.Join(dir, "gw.log.3")); !os.IsNotExist(err) {
		t.Fatalf(".3 must not exist: %v", err)
	}
}

func TestNoLineIsSplitAndFilesArePrivate(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "logs")
	w, _ := Open(dir, "x", 50, 3)
	defer w.Close()
	line := strings.Repeat("a", 30) + "\n" // 31 bytes
	for i := 0; i < 6; i++ {
		w.Write([]byte(line))
	}
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		got := read(t, filepath.Join(dir, e.Name()))
		if len(got) == 0 || len(got)%len(line) != 0 || strings.Count(got, "\n") != len(got)/len(line) {
			t.Fatalf("%s does not hold whole lines: %d bytes", e.Name(), len(got))
		}
		info, _ := e.Info()
		if info.Mode().Perm() != 0o600 {
			t.Fatalf("%s mode %v, want 0600", e.Name(), info.Mode().Perm())
		}
	}
	if d, _ := os.Stat(dir); d.Mode().Perm() != 0o700 {
		t.Fatalf("directory mode %v, want 0700", d.Mode().Perm())
	}
}

func TestAppendsToAnExistingFileAndCountsItsSize(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "g.log"), []byte(strings.Repeat("o", 40)), 0o600) // a log from an earlier run
	w, err := Open(dir, "g", 50, 2)
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	w.Write([]byte("new-1\n")) // 40 + 6 fits
	if got := read(t, filepath.Join(dir, "g.log")); !strings.HasPrefix(got, "ooo") || !strings.HasSuffix(got, "new-1\n") {
		t.Fatalf("an earlier log must be appended to, not replaced: %q", got)
	}
	w.Write([]byte("new-2-12345\n")) // 46 + 12 > 50: roll first
	if got := read(t, filepath.Join(dir, "g.log")); got != "new-2-12345\n" {
		t.Fatalf("after the roll: %q", got)
	}
	if got := read(t, filepath.Join(dir, "g.log.1")); !strings.HasSuffix(got, "new-1\n") || len(got) != 46 {
		t.Fatalf(".1: %q", got)
	}
}

func TestNeverRollsWhenUnlimitedAndKeepsAtLeastOne(t *testing.T) {
	dir := t.TempDir()
	w, _ := Open(dir, "u", 0, 0) // no limit; keep 0 is treated as 1
	defer w.Close()
	for i := 0; i < 100; i++ {
		w.Write([]byte(strings.Repeat("z", 100) + "\n"))
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 1 {
		t.Fatalf("an unlimited log rolled: %d files", len(entries))
	}
	dir2 := t.TempDir()
	w2, _ := Open(dir2, "k", 10, 0)
	defer w2.Close()
	for i := 0; i < 5; i++ {
		w2.Write([]byte("12345678\n"))
	}
	if entries, _ := os.ReadDir(dir2); len(entries) != 2 { // the current file and one rolled
		t.Fatalf("keep 0 must keep one: %d files", len(entries))
	}
}

func TestConcurrentWritesLoseNothingAndTearNothing(t *testing.T) {
	dir := t.TempDir()
	w, _ := Open(dir, "c", 2000, 100)
	defer w.Close()
	var wg sync.WaitGroup
	for g := 0; g < 8; g++ {
		wg.Add(1)
		go func(g int) {
			defer wg.Done()
			for i := 0; i < 200; i++ {
				fmt.Fprintf(w, "g%d-%03d-%s\n", g, i, strings.Repeat("q", 20))
			}
		}(g)
	}
	wg.Wait()
	total := 0
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		for _, line := range strings.Split(strings.TrimSuffix(read(t, filepath.Join(dir, e.Name())), "\n"), "\n") {
			if len(line) != len("g0-000-")+20 {
				t.Fatalf("a torn line in %s: %q", e.Name(), line)
			}
			total++
		}
	}
	if total != 8*200 {
		t.Fatalf("%d lines survived, want %d", total, 8*200)
	}
}
