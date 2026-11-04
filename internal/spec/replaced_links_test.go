package spec

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/zachpmanson/chainmail/internal/corpus"
	"github.com/zachpmanson/chainmail/internal/mailingest"
)

func TestReplacedLinksExtractRepairAndRenderAsEdits(t *testing.T) {
	data, err := os.ReadFile("../../testdata/quotes/replaced-links.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases []struct {
		Name, Subject, Original, Quoted string
		OffsetMinutes                   int
	}
	if err := json.Unmarshal(data, &cases); err != nil {
		t.Fatal(err)
	}
	for _, tc := range cases {
		for _, originalFirst := range []bool{true, false} {
			t.Run(fmt.Sprintf("%s/originalFirst=%v", tc.Name, originalFirst), func(t *testing.T) {
				s := open(t)
				sent := time.Date(2026, 3, 4, 2, 23, 38, 0, time.UTC)
				wall := sent.Truncate(time.Minute).Add(time.Duration(tc.OffsetMinutes) * time.Minute)
				original := mailingest.Message{Envelope: mailingest.Envelope{
					ID: "original", MessageID: "<original@example.test>", ThreadID: "pilot",
					From: "Rowan Example <rowan@example.test>", Subject: tc.Subject, Date: sent.Format(time.RFC1123Z),
				}, Body: tc.Original + "\nOn Tue, 3 Mar 2026 at 09:00, Alex Example <alex@example.test> wrote:\nOlder background message."}
				reply := mailingest.Message{Envelope: mailingest.Envelope{
					ID: "reply", MessageID: "<reply@example.test>", ThreadID: "pilot", InReplyTo: "<original@example.test>",
					From: "Mira Example <mira@example.test>", Subject: "Re: " + tc.Subject, Date: sent.Add(24 * time.Hour).Format(time.RFC1123Z),
				}, Body: "Thanks for sending the sheet.\n\nOn " + wall.Format("Mon, 2 Jan 2006 at 15:04") + ", Rowan Example <rowan@example.test> wrote:\n" + tc.Quoted}
				messages := []mailingest.Message{original, reply}
				if !originalFirst {
					messages[0], messages[1] = messages[1], messages[0]
				}
				for _, m := range messages {
					if _, err := mailingest.Put(s, m); err != nil {
						t.Fatal(err)
					}
				}
				for i := 0; i < 2; i++ {
					if _, err := corpus.CollapseTwins(s, true); err != nil {
						t.Fatal(err)
					}
					if _, err := s.ResolveParents(); err != nil {
						t.Fatal(err)
					}
				}
				rows, err := s.DB().Query("select ext_id from entries where container='pilot'")
				if err != nil {
					t.Fatal(err)
				}
				var ids []string
				for rows.Next() {
					var id string
					if err := rows.Scan(&id); err != nil {
						t.Fatal(err)
					}
					ids = append(ids, id)
				}
				if err := rows.Close(); err != nil {
					t.Fatal(err)
				}
				rendered, err := RenderTrail(s, ids)
				if err != nil {
					t.Fatal(err)
				}
				host := rendered["mail:<reply@example.test>"]
				if len(host.Edits) != 1 {
					t.Fatalf("host edits=%+v, want exactly the replaced link quote", host.Edits)
				}
				edit := host.Edits[0]
				if edit.Base != "mail:<original@example.test>" || edit.ID == "" || edit.Body != tc.Quoted {
					t.Fatalf("edit=%+v", edit)
				}
				if _, ok := rendered[edit.ID]; !ok {
					t.Fatal("edited copy missing from render data")
				}
				sp := generate(t, s, Options{Title: tc.Subject, Containers: []string{"pilot"}})
				found := 0
				for _, m := range sp.Messages {
					if m.ExtID == "mail:<reply@example.test>" {
						found = len(m.Edits)
					}
				}
				if found != 1 {
					t.Fatal("page and thread disagree about host edit")
				}
				if !strings.Contains(edit.Body, "https://security.example.test/") {
					t.Fatal("modified destination lost")
				}
			})
		}
	}
}
