package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/zachpmanson/chainmail/internal/corpus"
	"github.com/zachpmanson/chainmail/internal/unnest"
)

func TestReparseQuotesCLIReadonlyPreviewReviewedApply(t *testing.T) {
	path := filepath.Join(t.TempDir(), "corpus.db")
	s, err := corpus.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	address, _ := corpus.ParseAddress("Alex <alex@example.test>")
	pid, err := corpus.ResolveAddress(s, address, "synthetic")
	if err != nil {
		t.Fatal(err)
	}
	body := "Hello, here is the community garden bulletin."
	hostText := "Da: Alex <alex@example.test>\nDate: lun 5 ott 2026, 22:07\nSubject: Monthly bulletin\nTo: Robin <robin@example.test>\n\n" + body
	host, err := s.Put(corpus.Entry{Source: corpus.SourceMail, ExtID: "mail:synthetic-cli-host", TS: time.Date(2026, 10, 7, 0, 0, 0, 0, time.UTC), BodyText: hostText}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	// Simulate the old row directly, not through current quote extraction.
	old, err := s.Put(corpus.Entry{Source: corpus.SourceMail, ExtID: unnest.TextKey(body), TS: time.Date(2026, 10, 7, 0, 0, 0, 0, time.UTC), PersonID: pid, BodyText: body}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.DB().Exec(`update entries set quoted=1 where id=?`, old.ID); err != nil {
		t.Fatal(err)
	}
	if err = s.Sight(old.ID, host.ID, "quoted", "depth 0, time inferred from host"); err != nil {
		t.Fatal(err)
	}
	s.Close()
	before, _ := os.ReadFile(path)
	var out bytes.Buffer
	if err = runReparseQuotes(context.Background(), path, []string{"-entry", unnest.TextKey(body)}, &out); err != nil {
		t.Fatal(err)
	}
	after, _ := os.ReadFile(path)
	if !bytes.Equal(before, after) {
		t.Fatal("preview wrote database")
	}
	var plan corpus.QuoteMetadataPlan
	if err = json.Unmarshal(out.Bytes(), &plan); err != nil || len(plan.Changes) != 1 {
		t.Fatal(out.String(), err)
	}
	if strings.Contains(out.String(), body) || strings.Contains(out.String(), "alex@example.test") {
		t.Fatal("private text leaked")
	}
	file := filepath.Join(t.TempDir(), "reviewed.json")
	if err = os.WriteFile(file, out.Bytes(), 0600); err != nil {
		t.Fatal(err)
	}
	out.Reset()
	if err = runReparseQuotes(context.Background(), path, []string{"-apply", "-plan", file}, &out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "applied 1") {
		t.Fatal(out.String())
	}
	if err = runReparseQuotes(context.Background(), path, []string{"-apply", "-plan", file}, &out); err == nil {
		t.Fatal("repeat stale plan accepted")
	}
	out.Reset()
	if err = runReparseQuotes(context.Background(), path, []string{"-entry", unnest.TextKey(body)}, &out); err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal(out.Bytes(), &plan); err != nil || len(plan.Changes) != 0 {
		t.Fatal(out.String(), err)
	}
}
func TestReparseQuotesCLIRefusesImplicitApplyAndUnboundedPreview(t *testing.T) {
	path := filepath.Join(t.TempDir(), "missing", "corpus.db")
	for _, args := range [][]string{{}, {"-apply"}, {"-plan", "missing.json"}, {"-apply", "-plan", "missing.json", "-entry", "x"}, {"-entry", "a", "-container", "b"}, {"positional"}} {
		if err := runReparseQuotes(context.Background(), path, args, &bytes.Buffer{}); err == nil {
			t.Fatal("accepted", args)
		}
	}
	t.Setenv("CHAINMAIL_CORPUS", path)
	if err := run([]string{"reparse-quotes", "-entry", "synthetic"}); err == nil {
		t.Fatal("nonexistent database preview accepted")
	}
	if _, err := os.Stat(filepath.Dir(path)); !os.IsNotExist(err) {
		t.Fatal("preview created a directory", err)
	}
	if !strings.Contains(usage, "reparse-quotes") {
		t.Fatal("command undocumented")
	}
}

func TestReparseQuotesInvalidEnvelopeDoesNotOpenWritableDatabase(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "not-created.db")
	plan := filepath.Join(dir, "invalid.json")
	if err := os.WriteFile(plan, []byte(`{"Version":1,"ParserVersion":1,"Digest":"invalid"}`), 0600); err != nil {
		t.Fatal(err)
	}
	if err := runReparseQuotes(context.Background(), path, []string{"-apply", "-plan", plan}, &bytes.Buffer{}); err == nil {
		t.Fatal("invalid envelope accepted")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("invalid plan opened writable database", err)
	}
}
