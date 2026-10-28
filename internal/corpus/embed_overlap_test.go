package corpus

import (
	"context"
	"fmt"
	"reflect"
	"testing"
	"time"

	"github.com/zachpmanson/chainmail/internal/embed"
)

type pausedEmbedder struct {
	embed.Embedder
	entered chan struct{}
	release chan struct{}
}

func (p *pausedEmbedder) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	select {
	case p.entered <- struct{}{}:
	default:
	}
	select {
	case <-p.release:
		return p.Embedder.Embed(ctx, texts)
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func awaitEmbed(t *testing.T, ch <-chan struct{}) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(5 * time.Second):
		t.Fatal("embedding barrier timed out")
	}
}

func TestEmbeddingPublishesPendingBeforeInference(t *testing.T) {
	s := embedStore(t)
	put(t, s, msg{id: "initial-progress@example.test", body: "The synthetic ledger needs checking against the current schedule."})
	ctx, cancel := context.WithCancel(context.Background())
	p := &pausedEmbedder{Embedder: embed.NewFake(8), entered: make(chan struct{}, 1), release: make(chan struct{})}
	progress := make(chan BackfillProgress, 2)
	finished := make(chan struct{})
	done := make(chan error, 1)
	go func() {
		defer close(finished)
		_, err := s.BackfillEmbeddings(ctx, p, BackfillOptions{Progress: func(v BackfillProgress) { progress <- v }})
		done <- err
	}()
	defer func() { cancel(); awaitEmbed(t, finished) }()
	awaitEmbed(t, p.entered)
	select {
	case got := <-progress:
		if got.Pending != 1 || got.Done != 0 || got.Embedded != 0 || got.Skipped != 0 {
			t.Fatalf("initial progress = %+v, want 0/1 before inference", got)
		}
	default:
		t.Fatal("pending baseline was not published before inference")
	}
	close(p.release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if got := <-progress; got.Pending != 1 || got.Done != 1 || got.Embedded != 1 {
		t.Fatalf("committed progress = %+v, want 1/1", got)
	}
}

func TestEmbeddingDiscardsChangedSourceAndContinuesPastAllStaleBatch(t *testing.T) {
	for _, change := range []string{"body", "subject", "source", "delete"} {
		t.Run(change, func(t *testing.T) {
			s := embedStore(t)
			ids := make([]int64, 3)
			for i := range ids {
				ids[i] = put(t, s, msg{id: fmt.Sprintf("overlap-%d@example.test", i), subject: "Ledger", body: "The ledger needs checking against this month's schedule."})
			}
			f := embed.NewFake(8)
			p := &pausedEmbedder{Embedder: f, entered: make(chan struct{}, 1), release: make(chan struct{})}
			type result struct {
				rep BackfillReport
				err error
			}
			done := make(chan result, 1)
			go func() {
				rep, err := s.BackfillEmbeddings(context.Background(), p, BackfillOptions{Batch: 2})
				done <- result{rep, err}
			}()
			awaitEmbed(t, p.entered)
			// The entire first batch goes stale; a later stable ID must still be visited.
			for _, id := range ids[:2] {
				var err error
				switch change {
				case "body":
					_, err = s.db.Exec("update entries set body_text=?,body_sha=? where id=?", "A changed invoice needs reviewing before Friday.", BodySHA("Ledger", "A changed invoice needs reviewing before Friday."), id)
				case "subject":
					_, err = s.db.Exec("update entries set subject=?,body_sha=? where id=?", "Revised ledger", BodySHA("Revised ledger", "The ledger needs checking against this month's schedule."), id)
				case "source":
					_, err = s.db.Exec("update entries set source='slack' where id=?", id)
				case "delete":
					// No vector existed at selection, so canonical deletion is valid.
					_, err = s.db.Exec("delete from mail_detail where entry_id=?", id)
					if err == nil {
						_, err = s.db.Exec("delete from sightings where entry_id=?", id)
					}
					if err == nil {
						_, err = s.db.Exec("delete from entries where id=?", id)
					}
				}
				if err != nil {
					t.Fatal(err)
				}
			}
			close(p.release)
			var got result
			select {
			case got = <-done:
			case <-time.After(5 * time.Second):
				t.Fatal("pass spun on stale rows")
			}
			if got.err != nil || got.rep.Embedded != 1 {
				t.Fatalf("pass = %+v, %v", got.rep, got.err)
			}
			for _, id := range ids[:2] {
				v, err := s.Embedding(id, f.Model())
				if err != nil || v != nil {
					t.Fatalf("obsolete entry %d published: %v %v", id, v, err)
				}
			}
			if v, err := s.Embedding(ids[2], f.Model()); err != nil || v == nil {
				t.Fatalf("later stable ID lost: %v %v", v, err)
			}
			if change != "delete" {
				if rep := backfill(t, s, embed.NewFake(8), BackfillOptions{}); rep.Embedded != 2 {
					t.Fatalf("fresh retry = %+v", rep)
				}
			}
		})
	}
}

func TestEmbeddingDoesNotOverwriteNewerVectorOrLoseStableBatchRows(t *testing.T) {
	s := embedStore(t)
	id := put(t, s, msg{id: "newer@example.test", body: "An invented message about the current ledger and schedule."})
	stable := put(t, s, msg{id: "stable@example.test", body: "Another invented message that must survive the entire batch."})
	p := &pausedEmbedder{Embedder: embed.NewFake(8), entered: make(chan struct{}, 1), release: make(chan struct{})}
	done := make(chan error, 1)
	go func() { _, err := s.BackfillEmbeddings(context.Background(), p, BackfillOptions{}); done <- err }()
	awaitEmbed(t, p.entered)
	backfill(t, s, embed.NewFake(8), BackfillOptions{Limit: 1})
	// Make the newer winner visibly different from the old inference.
	winner := []float32{1, 0, 0, 0, 0, 0, 0, 0}
	if _, err := s.db.Exec("update entry_embeddings set vec=? where entry_id=?", encodeVector(winner), id); err != nil {
		t.Fatal(err)
	}
	close(p.release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	got, err := s.Embedding(id, p.Model())
	if err != nil || !reflect.DeepEqual(got, winner) {
		t.Fatalf("newer vector overwritten: %v %v", got, err)
	}
	if v, err := s.Embedding(stable, p.Model()); err != nil || v == nil {
		t.Fatalf("stable batch row lost: %v %v", v, err)
	}
}

func TestSemanticReadExcludesChangedHashPreparationAndSource(t *testing.T) {
	for _, change := range []string{"body", "prep", "source"} {
		t.Run(change, func(t *testing.T) {
			s := embedStore(t)
			id := put(t, s, msg{id: "read-guard@example.test", body: "The invoice needs a careful review against the current ledger."})
			f := embed.NewFake(8)
			backfill(t, s, f, BackfillOptions{})
			query := Query{Semantic: &SemanticQuery{Model: f.Model(), Vector: unitQuery(t, 8), Only: true}}
			check := func(want int) {
				t.Helper()
				hits, err := s.SearchEntries(query)
				if err != nil || len(hits) != want {
					t.Fatalf("semantic hits = %d, %v; want %d", len(hits), err, want)
				}
			}
			check(1)
			var err error
			switch change {
			case "body":
				_, err = s.db.Exec("update entries set body_text=?,body_sha=? where id=?", "The replacement ledger arrived from a different supplier.", BodySHA("", "The replacement ledger arrived from a different supplier."), id)
			case "prep":
				_, err = s.db.Exec("update entry_embeddings set prep=0 where entry_id=?", id)
			case "source":
				_, err = s.db.Exec("update entries set source='slack' where id=?", id)
			}
			if err != nil {
				t.Fatal(err)
			}
			check(0)
			backfill(t, s, embed.NewFake(8), BackfillOptions{})
			check(1)
		})
	}
}

func TestEmbeddingCancellationWhileInferencePausedHasNoLateWrite(t *testing.T) {
	s := embedStore(t)
	id := put(t, s, msg{id: "cancel-overlap@example.test", body: "The ledger should never receive a late vector after cancellation."})
	p := &pausedEmbedder{Embedder: embed.NewFake(8), entered: make(chan struct{}, 1), release: make(chan struct{})}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { _, err := s.BackfillEmbeddings(ctx, p, BackfillOptions{}); done <- err }()
	awaitEmbed(t, p.entered)
	cancel()
	if err := <-done; err == nil {
		t.Fatal("cancelled run succeeded")
	}
	close(p.release)
	if v, err := s.Embedding(id, p.Model()); err != nil || v != nil {
		t.Fatalf("late write: %v %v", v, err)
	}
}

func TestEmbeddingDeletedRowDoesNotAbortOtherRowsInSameBatch(t *testing.T) {
	s := embedStore(t)
	gone := put(t, s, msg{id: "gone@example.test", body: "This invented message will be deleted during inference."})
	stable := put(t, s, msg{id: "survivor@example.test", body: "This invented stable message must still get its own vector."})
	p := &pausedEmbedder{Embedder: embed.NewFake(8), entered: make(chan struct{}, 1), release: make(chan struct{})}
	done := make(chan error, 1)
	go func() { _, err := s.BackfillEmbeddings(context.Background(), p, BackfillOptions{}); done <- err }()
	awaitEmbed(t, p.entered)
	foldExec(t, s, "delete from mail_detail where entry_id=?", gone)
	foldExec(t, s, "delete from sightings where entry_id=?", gone)
	foldExec(t, s, "delete from entries where id=?", gone)
	close(p.release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if v, err := s.Embedding(stable, p.Model()); err != nil || v == nil {
		t.Fatalf("stable batch row lost: %v %v", v, err)
	}
}

func TestEmbeddingCommitsDoNotPublishOrConsumeFoldWork(t *testing.T) {
	s := embedStore(t)
	id := put(t, s, msg{id: "fold-embedding@example.test", body: "An invented initial message with enough words for embedding."})
	if _, err := s.WarmFolds(); err != nil {
		t.Fatal(err)
	}
	var generation int64
	if err := s.db.QueryRow("select generation from fold_state").Scan(&generation); err != nil {
		t.Fatal(err)
	}
	foldExec(t, s, "update entries set body_text=?,body_sha=? where id=?", "An invented changed message that dirties the fold cache.", BodySHA("An invented changed message that dirties the fold cache."), id)
	backfill(t, s, embed.NewFake(8), BackfillOptions{})
	var got int64
	var dirty int
	if err := s.db.QueryRow("select generation,(select count(*) from fold_dirty_entries) from fold_state").Scan(&got, &dirty); err != nil {
		t.Fatal(err)
	}
	if got != generation || dirty != 1 {
		t.Fatalf("embedding changed fold publication: generation %d -> %d dirty=%d", generation, got, dirty)
	}
}
