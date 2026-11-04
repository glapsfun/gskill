package selfupdate_test

import (
	"os"
	"testing"

	"github.com/glapsfun/gskill/internal/selfupdate/selfupdatetest"
)

func TestMain(m *testing.M) {
	code := m.Run()
	selfupdatetest.Cleanup()
	os.Exit(code)
}
