package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/zachpmanson/chainmail/internal/corpus"
	"github.com/zachpmanson/chainmail/internal/gmailclient"
	"github.com/zachpmanson/chainmail/internal/mailingest"
)

func runReactions(path string, args []string) error {
	fs := flag.NewFlagSet("reactions", flag.ContinueOnError)
	account := fs.String("account", "", "explicit account for bounded preview")
	ids := fs.String("ids", "", "1..20 existing Gmail source IDs, comma-separated")
	apply := fs.String("apply", "", "apply an exact separately reviewed manifest file, without provider reads")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return errors.New("unexpected reactions arguments")
	}
	if *apply != "" {
		if *ids != "" || *account != "" {
			return errors.New("-apply cannot be combined with preview flags")
		}
		raw, err := os.ReadFile(*apply)
		if err != nil {
			return err
		}
		var manifest corpus.ReactionManifest
		decoder := json.NewDecoder(strings.NewReader(string(raw)))
		decoder.DisallowUnknownFields()
		if err = decoder.Decode(&manifest); err != nil {
			return err
		}
		s, err := corpus.Open(path)
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
		return errors.New("reactions -account <account> -ids <source,...> > reviewed.json; separately: reactions -apply reviewed.json")
	}
	// Unlike ordinary ingest, preview must not even migrate an old corpus.
	s, err := corpus.OpenReadOnly(path)
	if err != nil {
		return err
	}
	defer s.Close()
	sourceIDs := strings.Split(*ids, ",")
	if len(sourceIDs) > corpus.ReactionPreviewLimit {
		return errors.New("at most 20 explicit sources per preview")
	}
	// Resolve every local copy before opening the mailbox boundary.
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
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	return encoder.Encode(manifest)
}
