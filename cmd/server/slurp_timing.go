package main

import (
	"bytes"
	"log"
	"regexp"
	"time"
)

var slurpTimingLine = regexp.MustCompile(`^slurp timing: phase=(slack|mail|twins|repair|dedupe|unread|embed|status|total) outcome=(started|complete|INCOMPLETE|skipped|FAILED) elapsed=([^ ]+)$`)

// slurpTimingWriter retains the response transcript while forwarding only fixed
// timing metadata to the journal as it arrives. A killed subprocess leaves its
// last started phase visible rather than losing all progress in CombinedOutput.
// exec.Cmd serializes writes when stdout and stderr share this writer.
type slurpTimingWriter struct {
	transcript bytes.Buffer
	source     string
	line       []byte
	overflow   bool
}

func (w *slurpTimingWriter) Write(p []byte) (int, error) {
	w.transcript.Write(p)
	for _, b := range p {
		if b == '\n' {
			if !w.overflow {
				if m := slurpTimingLine.FindSubmatch(w.line); m != nil {
					if elapsed, err := time.ParseDuration(string(m[3])); err == nil && elapsed >= 0 {
						log.Printf("%s slurp timing: phase=%s outcome=%s elapsed=%s",
							w.source, m[1], m[2], elapsed)
					}
				}
			}
			w.line, w.overflow = w.line[:0], false
		} else if len(w.line) < 256 && !w.overflow {
			w.line = append(w.line, b)
		} else {
			w.overflow = true
		}
	}
	return len(p), nil
}
