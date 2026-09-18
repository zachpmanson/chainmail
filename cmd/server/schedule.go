package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strconv"
	"strings"
	"time"

	"github.com/zachpmanson/chainmail/internal/corpus"
)

// The cadence this server sweeps the mailbox on: how often `corpus slurp` runs
// on its own, and the bounds a reader may set it within.
//
// The cadence is a stored setting rather than a line in the host's config
// because it is a decision about a corpus rather than about a machine: it is
// read and changed on the services page (/settings), and changing it there should
// not cost a deploy — nor leave a second copy of the number in the unit file,
// which is what the timer this replaced had, along with a Go function that
// hardcoded the same hour and a comment admitting it had to follow the timer.
//
// Ten minutes is the default because the unread badge is what goes stale: the
// ingest reads a message's labels once and never again, so a thread read on a
// phone still reads unread here until something reconciles it (the `unread`
// phase), and the badge is then only as current as this cadence.
const (
	DefaultSlurpEvery = 10 * time.Minute

	// slurpEveryOff is the word for "sweep never": the mailbox is then read only
	// when someone asks — POST /v1/slurp, and the ingest a page's refresh
	// triggers.
	slurpEveryOff = "off"

	// The bounds a caller may choose within. The floor is what keeps a mistyped
	// value from turning the corpus into a crawl of the mailbox (a sweep is a
	// full `in:anywhere` walk, and it reaches Google), and the ceiling keeps a
	// value meant as "off" from being stored as a wait nobody expected.
	minSlurpEvery = 1 * time.Minute
	maxSlurpEvery = 24 * time.Hour

	// sweepTick is how often the loop looks at the clock. Short enough that a
	// cadence is honoured within half a minute, which for a schedule measured in
	// minutes is the difference between "every ten minutes" and "every ten
	// minutes, plus however long the last one took".
	sweepTick = 30 * time.Second
)

// parseSlurpEvery reads a cadence as it travels: a duration word such as `10m`
// or `1h`, or the word `off`. It returns the duration (0 for off) and the
// canonical spelling the setting is stored and served in.
//
// Canonical because `600s`, `10m` and `1h0m` are one cadence, and a stored
// spelling the services page does not offer would leave its control showing
// nothing at all — the value it was handed and the values it can render have to
// be the same vocabulary.
func parseSlurpEvery(word string) (time.Duration, string, error) {
	w := strings.ToLower(strings.TrimSpace(word))
	if w == slurpEveryOff || w == "never" {
		return 0, slurpEveryOff, nil
	}
	d, err := time.ParseDuration(w)
	if err != nil {
		return 0, "", fmt.Errorf(
			"a cadence is a duration such as 10m or 1h, or %q to sweep never: %q is not one",
			slurpEveryOff, word)
	}
	if d < minSlurpEvery {
		return 0, "", fmt.Errorf(
			"a cadence below %s walks the whole mailbox more often than anyone reads it: %s is too short",
			minSlurpEvery, d)
	}
	if d > maxSlurpEvery {
		return 0, "", fmt.Errorf(
			"a cadence beyond %s is a sweep that never happens: use %q instead",
			maxSlurpEvery, slurpEveryOff)
	}
	if d%time.Minute != 0 {
		return 0, "", fmt.Errorf(
			"a cadence is a whole number of minutes or hours: %s does not divide into one", d)
	}
	return d, everyWord(d), nil
}

// everyWord is a cadence as it is written: whole hours in hours, whole minutes
// in minutes, and anything else as hours and minutes together. Duration.String
// would print 10m0s, which is true and unreadable on a page that offers "10
// minutes".
func everyWord(d time.Duration) string {
	if d%time.Hour == 0 {
		return strconv.Itoa(int(d/time.Hour)) + "h"
	}
	if d < time.Hour {
		return strconv.Itoa(int(d/time.Minute)) + "m"
	}
	return strconv.Itoa(int(d/time.Hour)) + "h" + strconv.Itoa(int(d%time.Hour/time.Minute)) + "m"
}

// slurpEvery is the cadence in force: the stored setting, or the default when
// nobody has chosen one.
//
// A stored value that no longer parses is logged and treated as the default
// rather than as "off": the setting is the reader's, but a corpus that stops
// being swept because a row was hand-edited would be a silent failure of the
// thing this schedule exists for.
func (s *server) slurpEvery() time.Duration {
	word, ok, err := s.store.Setting(corpus.SettingSlurpEvery)
	if err != nil {
		log.Printf("slurp cadence: %v", err)
		return DefaultSlurpEvery
	}
	if !ok {
		return DefaultSlurpEvery
	}
	d, _, err := parseSlurpEvery(word)
	if err != nil {
		log.Printf("slurp cadence %q is not one: %v — sweeping every %s", word, err, DefaultSlurpEvery)
		return DefaultSlurpEvery
	}
	return d
}

// slurpEveryWord is the cadence as the settings API serves it. Unlike the
// reader's other choices it is always present: a host sweeps at some cadence
// whether or not anyone has chosen one, and an absent field would have the page
// guessing what the server will actually do — which is the one thing the control
// exists to say.
func (s *server) slurpEveryWord() string {
	d := s.slurpEvery()
	if d <= 0 {
		return slurpEveryOff
	}
	return everyWord(d)
}

// lastSlurp is when this server last reached for the mailbox, by the stamp the
// ingest writes as it starts. Zero when it never has, which is a real state: a
// corpus that has just been built has nothing to count from.
func (s *server) lastSlurp() time.Time {
	v, ok, err := s.store.Setting(corpus.SettingSlurpAt)
	if err != nil || !ok {
		if err != nil {
			log.Printf("last sweep: %v", err)
		}
		return time.Time{}
	}
	at, err := time.Parse(time.RFC3339, v)
	if err != nil {
		log.Printf("last sweep %q is not a timestamp: %v", v, err)
		return time.Time{}
	}
	return at
}

// recordSlurp stamps when an ingest started. Written before the walk rather than
// after it, because what the cadence counts from is when the server last reached
// for the mailbox — and a run that fails must still hold the next one off for a
// whole cadence. A mailbox refusing logins is not fixed by asking every thirty
// seconds.
func (s *server) recordSlurp(at time.Time) {
	if err := s.store.PutSetting(corpus.SettingSlurpAt, at.UTC().Format(time.RFC3339)); err != nil {
		log.Printf("recording the sweep: %v", err)
	}
}

// dueAt is when the next sweep may run: the cadence measured from the last time
// the mailbox was reached, or from when this process started on a corpus that
// has never been swept.
//
// One rule, and the loop and the page both ask it. They are the same question —
// "when does this sweep next?" — and two answers to it are two answers that
// drift: the page would name a time the loop did not honour, for as long as it
// took the loop to catch up with its own base. A fresh corpus counts from the
// server's own start, which is the base that makes a corpus swept a cadence after
// it came up rather than the moment it boots.
func (s *server) dueAt() time.Time {
	base := s.lastSlurp()
	if base.IsZero() {
		base = s.startedAt
	}
	return base.Add(s.slurpEvery())
}

// nextSlurpAt is when the next sweep is due, as a UTC RFC3339 stamp, or "" when
// nothing will sweep at all — no -slurp grant on this host, or a cadence of off.
//
// Due at, not started at: the loop reads the clock every sweepTick, so the run
// itself lands within half a minute of this.
func (s *server) nextSlurpAt() string {
	if !s.slurpEnabled {
		return ""
	}
	if s.slurpEvery() <= 0 {
		return ""
	}
	return s.dueAt().UTC().Format(time.RFC3339)
}

// slurpOnce runs one ingest: the whole of what the manual button and the
// schedule have in common.
//
// One at a time, because both callers share the corpus and the mailbox and two
// ingests overlapping is two processes walking the same query into the same
// tables. The latch is held across the run and cleared however it ends. The
// stamp is written first for the reason recordSlurp gives — a failure is still
// an attempt — and the fold cache is reduced on the way out, since the ingest
// changed exactly the bodies it is evidence about (see warmFolds).
func (s *server) slurpOnce(ctx context.Context, run func(context.Context, string) ([]byte, error)) ([]byte, error) {
	if run == nil {
		return nil, errors.New("this server has no ingest to run")
	}
	if !s.sweeping.CompareAndSwap(false, true) {
		return nil, errSweepRunning
	}
	defer s.sweeping.Store(false)
	s.recordSlurp(time.Now())
	runCtx, cancel := context.WithTimeout(ctx, s.slurpTimeout)
	defer cancel()
	out, err := run(runCtx, s.corpusPath)
	if err != nil {
		return out, err
	}
	// Doing it here means the thing that filled the mailbox does not hand the
	// next reader the cost of reading it.
	s.warmFolds()
	return out, nil
}

// errSweepRunning is the latch refusing a second ingest. It is its own error
// rather than a sentence at the call site because both callers report it
// differently: the handler as a 409, the loop as nothing at all.
var errSweepRunning = errors.New("a sweep is already running")

// sweepLoop is the schedule: it reads the clock every sweepTick and runs the
// ingest when the cadence says it is due.
//
// Nothing here is a systemd timer. The cadence is a setting, so the process that
// reads it owns it — one place to look, one place to change, and no unit file
// whose OnCalendar has to be kept in step with a number a page can edit.
func (s *server) sweepLoop(ctx context.Context) {
	// A host that has not granted -slurp cannot reach the mailbox at all, so
	// there is nothing to schedule; the same goes for a harness with no ingest
	// injected. Said here rather than logged on every tick.
	if !s.slurpEnabled || s.runSweep == nil {
		return
	}
	if every := s.slurpEvery(); every <= 0 {
		log.Printf("sweeping is off: the mailbox is read when asked, not on a schedule")
	}
	t := time.NewTicker(sweepTick)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			s.sweepIfDue(ctx)
		}
	}
}

// sweepIfDue runs the ingest if a cadence has elapsed since the last one.
//
// A tick that lands while an ingest is already running is dropped rather than
// queued: the running one has already stamped its start, so the tick after it
// decides on its own, and a queued sweep would only be a second walk of the same
// mailbox that the first has just finished.
func (s *server) sweepIfDue(ctx context.Context) {
	if s.slurpEvery() <= 0 {
		return
	}
	if time.Now().Before(s.dueAt()) {
		return
	}
	out, err := s.slurpOnce(ctx, s.runSweep)
	switch {
	case errors.Is(err, errSweepRunning):
		return
	case err != nil:
		log.Printf("sweep: %v", err)
	default:
		// The transcript goes to the journal, as the timer's log got it: what a
		// phase found is the reason to read this line at all, and "swept" alone
		// would be the one thing already known.
		log.Printf("sweep: %s", strings.TrimSpace(string(out)))
	}
}
