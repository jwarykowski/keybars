package main

import (
	"os"
	"testing"
)

// TestMain is keybars itself when start runs this binary in the background.
func TestMain(m *testing.M) {
	if os.Getenv("KEYBARS_TEST_MAIN") == "1" {
		main()
		os.Exit(0)
	}
	os.Exit(m.Run())
}
