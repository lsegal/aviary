package channels

import (
	"os"
	"testing"

	"github.com/lsegal/aviary/internal/store"
)

func TestMain(m *testing.M) {
	dataDir, err := os.MkdirTemp("", "aviary-channels-test-*")
	if err != nil {
		panic(err)
	}
	store.SetDataDir(dataDir)
	code := m.Run()
	store.SetDataDir("")
	_ = os.RemoveAll(dataDir)
	os.Exit(code)
}
