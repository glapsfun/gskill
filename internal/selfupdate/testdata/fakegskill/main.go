// Command fakegskill stands in for a gskill release binary in selfupdate tests.
// Its version and failure mode are set at build time with
// -ldflags "-X main.version=... -X main.mode=...".
package main

import (
	"fmt"
	"os"
	"time"
)

var (
	version = "0.0.0"
	mode    = "ok"
)

func main() {
	if len(os.Args) != 3 || os.Args[1] != "version" || os.Args[2] != "--json" {
		os.Exit(2)
	}
	switch mode {
	case "ok":
		fmt.Printf("{\"version\":%q,\"commit\":\"fake\"}\n", version)
	case "badjson":
		fmt.Println("not json")
	case "hang":
		time.Sleep(60 * time.Second)
	case "fail":
		os.Exit(3)
	}
}
