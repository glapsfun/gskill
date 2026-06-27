package errs_test

import (
	"errors"
	"fmt"

	"github.com/glapsfun/gskill/internal/errs"
)

func ExampleExitCode() {
	// A wrapped sentinel keeps its exit code through %w wrapping.
	err := fmt.Errorf("git fetch: %w", errs.ErrAuth)
	fmt.Println(errs.ExitCode(err))
	// Output: 11
}

func ExampleExitCode_unknown() {
	fmt.Println(errs.ExitCode(errors.New("something unexpected")))
	// Output: 1
}
