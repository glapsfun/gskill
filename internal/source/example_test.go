package source_test

import (
	"fmt"

	"github.com/glapsfun/gskill/internal/source"
)

func ExampleParse() {
	ref, _ := source.Parse("github.com/acme/widgets/my-skill")
	fmt.Println(ref.Type, ref.Owner, ref.Repo, ref.Path)
	fmt.Println(ref.URL)
	// Output:
	// git acme widgets my-skill
	// https://github.com/acme/widgets.git
}
