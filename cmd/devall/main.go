// devall starts loginhttp, gateway and admin together for local development.
// Each child's output is prefixed with its name; Ctrl+C stops all of them.
package main

import (
	"bufio"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"os/signal"
	"sync"
	"syscall"
)

func main() {
	cfgPath := flag.String("config", "configs/dev.yaml", "config file passed to every service")
	flag.Parse()

	// Build once so the children start fast and a compile error shows up once.
	tmp, err := os.MkdirTemp("", "2droundserver-devall-")
	if err != nil {
		log.Fatal(err)
	}
	defer os.RemoveAll(tmp)

	names := []string{"loginhttp", "gateway", "admin"}
	var cmds []*exec.Cmd
	for _, n := range names {
		bin := tmp + "/" + n
		build := exec.Command("go", "build", "-o", bin, "./cmd/"+n)
		build.Stdout, build.Stderr = os.Stdout, os.Stderr
		if err := build.Run(); err != nil {
			log.Fatalf("build %s: %v", n, err)
		}
		cmds = append(cmds, exec.Command(bin, "-config", *cfgPath))
	}

	var wg sync.WaitGroup
	done := make(chan string, len(cmds))
	for i, c := range cmds {
		name := names[i]
		out, _ := c.StdoutPipe()
		c.Stderr = c.Stdout
		if err := c.Start(); err != nil {
			log.Fatalf("start %s: %v", name, err)
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			prefix(name, out)
			c.Wait()
			done <- name
		}()
	}

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, syscall.SIGTERM)
	select {
	case s := <-sig:
		fmt.Printf("[devall] %v, stopping\n", s)
	case n := <-done:
		fmt.Printf("[devall] %s exited, stopping the rest\n", n)
	}
	for _, c := range cmds {
		if c.Process != nil {
			c.Process.Signal(syscall.SIGTERM)
		}
	}
	wg.Wait()
}

func prefix(name string, r io.Reader) {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 64<<10), 1<<20)
	for sc.Scan() {
		fmt.Printf("[%s] %s\n", name, sc.Text())
	}
}
