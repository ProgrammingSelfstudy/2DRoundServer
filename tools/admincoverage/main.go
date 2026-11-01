// Command admincoverage emits the source-derived inventory, without opening a DB.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"

	"2droundserver/internal/admincoverage"
)

func main() {
	root := flag.String("root", ".", "repository root")
	flag.Parse()
	inventory, err := admincoverage.Scan(*root)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(inventory); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
