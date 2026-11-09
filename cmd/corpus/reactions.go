package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/zachpmanson/chainmail/internal/corpus"
	"github.com/zachpmanson/chainmail/internal/gmailclient"
	"github.com/zachpmanson/chainmail/internal/mailingest"
)

func readReactionManifest(path string) (corpus.ReactionManifest, error) {
	var m corpus.ReactionManifest
	f, err := os.Open(path)
	if err != nil {
		return m, err
	}
	defer f.Close()
	raw, err := io.ReadAll(io.LimitReader(f, (64<<20)+1))
	if err != nil {
		return m, err
	}
	if len(raw) > 64<<20 {
		return m, errors.New("reaction manifest exceeds 64 MiB")
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if err = d.Decode(&m); err != nil {
		return m, err
	}
	var extra any
	if d.Decode(&extra) != io.EOF {
		return m, errors.New("reaction manifest has trailing data")
	}
	return m, m.Validate()
}

func writeReactionPreview(out io.Writer, m corpus.ReactionManifest, path string) error {
	if path != "" {
		f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if err != nil {
			return err
		}
		err = json.NewEncoder(f).Encode(m)
		closeErr := f.Close()
		if err != nil {
			return err
		}
		if closeErr != nil {
			return closeErr
		}
	}
	// Deliberately not an apply envelope: no source prose, identities or mailbox IDs.
	summary := struct {
		Kind                   string         `json:"kind"`
		Sources                int            `json:"sources"`
		Refused                int            `json:"refused"`
		Statuses               map[string]int `json:"statuses"`
		PrivateManifestWritten bool           `json:"privateManifestWritten"`
	}{Kind: "reaction-preview-summary", Sources: len(m.Items), Statuses: map[string]int{}, PrivateManifestWritten: path != ""}
	for _, item := range m.Items {
		summary.Statuses[item.Status]++
		if item.Refusal != "" {
			summary.Refused++
		}
	}
	return json.NewEncoder(out).Encode(summary)
}

func runReactions(path string, args []string) error {
	fs := flag.NewFlagSet("reactions", flag.ContinueOnError)
	account := fs.String("account", "", "explicit account for bounded preview")
	ids := fs.String("ids", "", "1..20 existing Gmail source IDs, comma-separated")
	output := fs.String("out", "", "exclusive 0600 PRIVATE exact apply manifest (contains source text/HTML)")
	apply := fs.String("apply", "", "apply an exact separately reviewed PRIVATE manifest file, without provider reads")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return errors.New("unexpected reactions arguments")
	}
	if *apply != "" {
		if *ids != "" || *account != "" || *output != "" {
			return errors.New("-apply cannot be combined with preview flags")
		}
		manifest, err := readReactionManifest(*apply)
		if err != nil {
			return err
		}
		s, err := corpus.OpenForMetadata(path)
		if err != nil {
			return err
		}
		defer s.Close()
		changed, err := s.ApplyReactionManifest(manifest)
		if err != nil {
			return err
		}
		fmt.Printf("reaction manifest applied: %t (exact replay is a no-op)\n", changed)
		return nil
	}
	if *account == "" || *ids == "" {
		return errors.New("reactions -account <account> -ids <source,...> [-out PRIVATE.json]; stdout is a redacted summary, not an apply manifest")
	}
	s, err := corpus.OpenReadOnly(path)
	if err != nil {
		return err
	}
	defer s.Close()
	sourceIDs := strings.Split(*ids, ",")
	if err = s.CheckReactionPreviewSources(*account, sourceIDs); err != nil {
		return err
	}
	// Resolve all local preconditions before opening the mailbox boundary.
	for _, id := range sourceIDs {
		if _, err = s.PreviewReactionSource(*account, id); err != nil {
			return err
		}
	}
	token, err := gmailclient.AccountTokenPath(*account)
	if err != nil {
		return err
	}
	mailbox, err := gmailclient.NewForTokenPath(token)
	if err != nil {
		return err
	}
	manifest, err := mailingest.PreviewReactions(s, mailbox, *account, sourceIDs)
	if err != nil {
		return err
	}
	return writeReactionPreview(os.Stdout, manifest, *output)
}
