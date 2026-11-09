package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/zachpmanson/chainmail/internal/corpus"
)

func runReparseQuotes(ctx context.Context, path string, args []string, out io.Writer) error {
	fs := flag.NewFlagSet("reparse-quotes", flag.ContinueOnError)
	entry := fs.String("entry", "", "one existing external ID")
	container := fs.String("container", "", "one local container")
	after := fs.Int64("after-id", 0, "resume after this entry ID")
	limit := fs.Int("limit", 100, "selected entries, maximum 100")
	hosts := fs.Int("max-hosts", 1000, "complete evidence hosts, maximum 1000")
	byteLimit := fs.Int64("max-bytes", 32<<20, "whole-host source byte budget, maximum 33554432")
	output := fs.String("out", "", "write preview JSON here instead of stdout")
	planPath := fs.String("plan", "", "exact reviewed JSON plan to apply")
	apply := fs.Bool("apply", false, "authorise only the reviewed plan's quote metadata changes")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return errors.New("reparse-quotes takes flags, not positional arguments")
	}
	if *apply || *planPath != "" {
		previewOption := false
		fs.Visit(func(f *flag.Flag) {
			if f.Name != "apply" && f.Name != "plan" {
				previewOption = true
			}
		})
		if previewOption || !*apply || *planPath == "" || *entry != "" || *container != "" || *output != "" || *after != 0 || *limit != 100 || *hosts != 1000 || *byteLimit != 32<<20 {
			return errors.New("apply requires -apply -plan <reviewed.json>, without selectors or preview options")
		}
		f, err := os.Open(*planPath)
		if err != nil {
			return err
		}
		defer f.Close()
		data, err := io.ReadAll(io.LimitReader(f, (64<<20)+1))
		if err != nil {
			return err
		}
		if len(data) > 64<<20 {
			return errors.New("quote plan exceeds 64 MiB")
		}
		var p corpus.QuoteMetadataPlan
		// Refuse unknown fields and trailing JSON values.
		dec := json.NewDecoder(bytes.NewReader(data))
		dec.DisallowUnknownFields()
		if err = dec.Decode(&p); err != nil {
			return err
		}
		var extra any
		if dec.Decode(&extra) != io.EOF {
			return errors.New("quote plan has trailing data")
		}
		if err = p.Validate(); err != nil {
			return err
		}
		s, err := corpus.Open(path)
		if err != nil {
			return err
		}
		defer s.Close()
		n, err := s.ApplyQuotedMetadata(ctx, p)
		if err != nil {
			return err
		}
		_, err = fmt.Fprintf(out, "applied %d quoted metadata changes; no mailbox/content/provenance repair\n", n)
		return err
	}
	if (*entry == "") == (*container == "") {
		return errors.New("preview requires exactly one -entry <ext-id> or -container <id>")
	}
	s, err := corpus.OpenReadOnly(path)
	if err != nil {
		return err
	}
	defer s.Close()
	p, err := s.PlanQuotedMetadata(ctx, corpus.QuoteSelection{ExtID: *entry, Container: *container, AfterID: *after}, corpus.QuoteLimits{Entries: *limit, Hosts: *hosts, Bytes: *byteLimit})
	if err != nil {
		return err
	}
	if *output != "" {
		f, err := os.OpenFile(*output, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if err != nil {
			return err
		}
		defer f.Close()
		out = f
	}
	enc := json.NewEncoder(out)
	enc.SetIndent("", "  ")
	return enc.Encode(p)
}
