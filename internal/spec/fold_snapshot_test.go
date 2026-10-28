package spec

import (
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/zachpmanson/chainmail/internal/corpus"
)

func TestLoadKeepsBodiesProvenanceHostsAndFoldsInOneSnapshot(t *testing.T) {
	path := filepath.Join(t.TempDir(), "render.db")
	s, err := corpus.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	writer, err := corpus.Open(path + "?_pragma=busy_timeout(5000)")
	if err != nil {
		t.Fatal(err)
	}
	defer writer.Close()
	p, err := corpus.Resolve(s, corpus.KindEmail, "synthetic@render.example", "Synthetic")
	if err != nil {
		t.Fatal(err)
	}
	var ids []int64
	for i := 0; i < 3; i++ {
		res, err := s.Put(corpus.Entry{Source: corpus.SourceMail, ExtID: fmt.Sprint("mail:", i), PersonID: p, TS: time.Unix(1, 0), BodyText: "Opening\nRegards,\nSynthetic", BodyHTML: "<p>stateA</p>"}, &corpus.Mail{From: "synthetic@render.example"}, nil)
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, res.ID)
		if err = s.Sight(res.ID, 0, "direct", ""); err != nil {
			t.Fatal(err)
		}
	}
	host, err := s.Put(corpus.Entry{Source: corpus.SourceMail, ExtID: "mail:host", TS: time.Unix(1, 0), BodyHTML: "<p>stateB</p>"}, &corpus.Mail{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range ids {
		if err = s.Sight(id, host.ID, "quoted", ""); err != nil {
			t.Fatal(err)
		}
	}
	if _, err = s.WarmFolds(); err != nil {
		t.Fatal(err)
	}
	changes := make(chan error, 1)
	go func() {
		for i := 0; i < 40; i++ {
			tx, err := writer.DB().Begin()
			if err != nil {
				changes <- err
				return
			}
			direct := i%2 == 1
			text, html := "Unique single line", ""
			if direct {
				text = "Opening\nRegards,\nSynthetic"
				html = "<p>stateA</p>"
			}
			if _, err = tx.Exec(`update entries set body_text=?,body_html=? where id in (?,?,?)`, text, html, ids[0], ids[1], ids[2]); err == nil {
				if direct {
					_, err = tx.Exec(`insert into sightings(entry_id,kind) select id,'direct' from entries where id in (?,?,?) on conflict do nothing`, ids[0], ids[1], ids[2])
				} else {
					_, err = tx.Exec(`delete from sightings where kind='direct'`)
				}
			}
			if err != nil {
				tx.Rollback()
				changes <- err
				return
			}
			if err = tx.Commit(); err != nil {
				changes <- err
				return
			}
			time.Sleep(time.Millisecond)
		}
		changes <- nil
	}()
	for i := 0; i < 60; i++ {
		rows, err := load(s, ids)
		if err != nil {
			t.Fatal(err)
		}
		for _, r := range rows {
			if r.Direct {
				if r.BodyText != "Opening\nRegards,\nSynthetic" || r.BodyHTML != "<p>stateA</p>" || r.Fold.Lines != 2 || len(r.HostHTML) != 0 {
					t.Fatalf("mixed direct snapshot: %+v", r)
				}
			} else {
				if r.BodyText != "Unique single line" || r.BodyHTML != "" || r.Fold.Lines != 0 || len(r.HostHTML) != 1 || r.HostHTML[0] != "<p>stateB</p>" {
					t.Fatalf("mixed recovered snapshot: %+v", r)
				}
			}
		}
	}
	if err := <-changes; err != nil {
		t.Fatal(err)
	}
}
