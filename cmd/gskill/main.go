// Command gskill is the GSKILL command-line entrypoint.
package main

import (
	"fmt"

	"github.com/glapsfun/gskill/internal/version"
)

func main() {
	fmt.Println(version.String())
}
