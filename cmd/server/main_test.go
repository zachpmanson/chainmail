package main

import (
	"flag"
	"strings"
	"testing"
)

func TestHasslesGrantIsRegisteredOnEachRunFlagSet(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	for i := 0; i < 2; i++ {
		// Parsing must reach the loopback safety check, without opening a corpus,
		// binding a socket, or registering a duplicate process-global flag.
		err := run([]string{"-hassles", "-addr", "0.0.0.0:8765"})
		if err == nil || strings.Contains(err.Error(), "flag provided but not defined") || !strings.Contains(err.Error(), unsafeBindFlag) {
			t.Fatalf("run did not parse its Hassles grant: %v", err)
		}
	}
	if flag.CommandLine.Lookup("hassles") != nil {
		t.Fatal("Hassles grant leaked onto the process-global flag set")
	}
}
