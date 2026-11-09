package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zachpmanson/chainmail/internal/corpus"
)

func TestReactionInvalidManifestDoesNotOpenDatabase(t *testing.T) {
	for name, raw := range map[string]string{
		"version":  `{"version":2,"accountId":"A","items":[]}`,
		"account":  `{"version":1,"accountId":"","items":[]}`,
		"items":    `{"version":1,"accountId":"A","items":[{"gmailId":"x"}]}`,
		"unknown":  `{"version":1,"accountId":"A","items":[],"extra":true}`,
		"trailing": `{"version":1,"accountId":"A","items":[]} {}`,
		"summary":  `{"kind":"reaction-preview-summary","sources":1}`,
	} {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			manifest := filepath.Join(dir, "manifest.json")
			db := filepath.Join(dir, "must-not-open.db")
			if err := os.WriteFile(manifest, []byte(raw), 0600); err != nil {
				t.Fatal(err)
			}
			if err := runReactions(db, []string{"-apply", manifest}); err == nil {
				t.Fatal("invalid manifest accepted")
			}
			if _, err := os.Stat(db); !os.IsNotExist(err) {
				t.Fatalf("opened database for invalid envelope: %v", err)
			}
		})
	}
}

func TestReactionManifestReadCap(t *testing.T) {
	path := filepath.Join(t.TempDir(), "oversized.json")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	if err = f.Truncate((64 << 20) + 1); err != nil {
		t.Fatal(err)
	}
	f.Close()
	if _, err = readReactionManifest(path); err == nil || !strings.Contains(err.Error(), "64 MiB") {
		t.Fatalf("unbounded reader: %v", err)
	}
}

func TestReactionPreviewRedactsStdoutAndWritesExclusivePrivateManifest(t *testing.T) {
	m := corpus.ReactionManifest{Version: 1, AccountID: "private-account", Items: []corpus.ReactionPreviewItem{{GmailID: "private-id", Source: corpus.ReactionSource{From: "private@example.test", Body: "PRIVATE WORDS", BodyHTML: "<b>PRIVATE HTML</b>"}, Status: "ordinary-refused", Refusal: "private refusal detail"}}}
	var out bytes.Buffer
	path := filepath.Join(t.TempDir(), "PRIVATE.json")
	if err := writeReactionPreview(&out, m, path); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out.String(), "private-") || strings.Contains(out.String(), "PRIVATE") {
		t.Fatalf("stdout leaked source: %s", out.String())
	}
	var summary corpus.ReactionManifest
	if err := json.Unmarshal(out.Bytes(), &summary); err == nil && summary.Validate() == nil {
		t.Fatal("summary applies")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var exact corpus.ReactionManifest
	if err = json.Unmarshal(raw, &exact); err != nil || exact.Items[0].Source != m.Items[0].Source {
		t.Fatalf("private proof changed: %v", err)
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatalf("private file permissions: %v %v", info, err)
	}
	if err = writeReactionPreview(&out, m, path); err == nil {
		t.Fatal("overwrote private manifest")
	}
	unchanged, _ := os.ReadFile(path)
	if !bytes.Equal(raw, unchanged) {
		t.Fatal("existing file changed")
	}
}
