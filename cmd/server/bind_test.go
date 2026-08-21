package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The bind is the whole security model. There is no authentication, and a spec
// response carries unsanitised sender HTML (#14), so an address that reaches
// the network hands the corpus to anyone who can route to it.
func TestANonLoopbackBindIsRefused(t *testing.T) {
	refused := []string{
		":8765",           // every interface, including ones acquired later
		"0.0.0.0:8765",    //
		"[::]:8765",       //
		"192.0.2.10:8765", // a documentation address, standing in for a LAN one
	}
	for _, addr := range refused {
		err := checkBind(addr, false)
		if err == nil {
			t.Errorf("%s was accepted", addr)
			continue
		}
		// The message has to say what to do, or the next step is to go looking
		// for the flag and pass it without reading why it exists.
		if !strings.Contains(err.Error(), unsafeBindFlag) {
			t.Errorf("%s: %q does not name the override", addr, err)
		}
	}
}

func TestLoopbackBindsAreAccepted(t *testing.T) {
	for _, addr := range []string{"127.0.0.1:8765", "127.0.0.1:0", "[::1]:8765", "localhost:8765"} {
		if err := checkBind(addr, false); err != nil {
			t.Errorf("%s: %v", addr, err)
		}
	}
}

// Widening the bind is possible, because refusing outright would only be routed
// around with a tunnel, which is worse: at least this prints what it costs.
func TestTheOverrideIsWhatMakesANonLoopbackBindPossible(t *testing.T) {
	if err := checkBind("192.0.2.10:8765", true); err != nil {
		t.Errorf("the override did not permit it: %v", err)
	}
}

func TestAnAddressThatIsNotHostPortIsRefused(t *testing.T) {
	for _, addr := range []string{"8765", "127.0.0.1", ""} {
		if err := checkBind(addr, false); err == nil {
			t.Errorf("%q was accepted", addr)
		}
	}
}

// Checked before the corpus is opened, so a refused address cannot half-start
// the server or migrate a database it will not serve.
func TestARefusedBindNeverTouchesTheCorpus(t *testing.T) {
	path := filepath.Join(t.TempDir(), "corpus.db")
	err := run([]string{"-addr", "0.0.0.0:0", "-corpus", path})
	if err == nil {
		t.Fatal("run accepted a wildcard bind")
	}
	if _, statErr := os.Stat(path); statErr == nil {
		t.Error("the corpus was opened despite the bind being refused")
	}
}
