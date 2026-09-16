package main

import (
	"log"
	"time"
)

// warmFolds fills the fold cache before anyone asks a verdict of it.
//
// The cache lives in the store and starts empty, so the first chain read after a
// deploy used to reduce every body in scope itself: 1.802 s for a 29-entry chain
// against 0.399 s once warm. Warming runs the same pass with nobody waiting on
// it, at the two moments the cache's evidence changes wholesale: at boot, and
// after an ingest.
//
// A failure is logged and survived, not returned. A warm that cannot read the
// corpus leaves every request exactly as fast as it was before this existed, and
// refusing to serve over that would cost more than the seconds it saves.
func (s *server) warmFolds() {
	w, err := s.store.WarmFolds()
	if err != nil {
		log.Printf("warm: folding the corpus: %v", err)
		return
	}
	log.Printf("warm: %d bodies folded to %d lines in %s", w.Bodies, w.Lines,
		w.Took.Round(time.Millisecond))
}
