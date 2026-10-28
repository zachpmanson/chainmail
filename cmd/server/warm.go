package main

import (
	"log"
	"time"
)

// warmFolds publishes durable evidence at startup and after ingest.
// Failure is logged; selected readers independently retry or fail closed.
func (s *server) warmFolds() {
	w, err := s.store.WarmFolds()
	if err != nil {
		log.Printf("warm: folding the corpus: %v", err)
		return
	}
	log.Printf("warm: normalized=%d lines=%d memberships=%d tails=%d counters=%d generation=%d rebuilt=%t lock=%s took=%s",
		w.Bodies, w.Lines, w.Memberships, w.Tails, w.CounterKeys, w.Generation, w.Rebuilt,
		w.LockWait.Round(time.Millisecond), w.Took.Round(time.Millisecond))
}
