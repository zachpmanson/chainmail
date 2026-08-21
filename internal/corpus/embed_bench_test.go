package corpus

import (
	"fmt"
	"math/rand/v2"
	"testing"

	"github.com/zachpmanson/chainmail/internal/embed"
)

// benchEntries and benchDim are the shape of the real thing: this corpus holds
// ~29k entries, of which the ones with enough text to embed are ~20k, and
// nomic-embed-text is 768-dimensional.
const (
	benchEntries = 30000
	benchDim     = 768
)

// BenchmarkSemanticScan is the number the "no vector index" decision rests on.
// It is the whole path — sqlite reading 30k blobs, decoding each into floats,
// one dot product each, and a sort — not an in-memory loop, because the blob
// read is the part an ANN index would remove and the part a microbenchmark
// hides.
//
// modernc.org/sqlite cannot load native extensions, so sqlite-vec is not an
// option regardless. This says what that costs.
func BenchmarkSemanticScan(b *testing.B) {
	s := benchStore(b)
	q := randomUnit(benchDim, rand.New(rand.NewPCG(99, 99)))
	sem := SemanticQuery{Vector: q, Model: "bench", Only: true}
	query := Query{}.withDefaults()

	b.ResetTimer()
	for b.Loop() {
		rows, err := s.semanticRank(query, sem, "1", nil)
		if err != nil {
			b.Fatal(err)
		}
		if len(rows) == 0 {
			b.Fatal("scan returned nothing")
		}
	}
}

// BenchmarkDotProduct isolates the arithmetic, so the scan's cost can be
// attributed between sqlite and the maths.
func BenchmarkDotProduct(b *testing.B) {
	r := rand.New(rand.NewPCG(1, 2))
	a, c := randomUnit(benchDim, r), randomUnit(benchDim, r)
	b.ResetTimer()
	for b.Loop() {
		if _, err := embed.Dot(a, c); err != nil {
			b.Fatal(err)
		}
	}
}

// benchStore writes benchEntries random vectors straight into the embedding
// table. The entries themselves are minimal: the scan reads only the vector
// blob and the join columns, so a realistic body would inflate the fixture
// without changing what is measured.
func benchStore(b *testing.B) *Store {
	b.Helper()
	s, err := Open(":memory:")
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(func() { s.Close() })

	tx, err := s.db.Begin()
	if err != nil {
		b.Fatal(err)
	}
	ins, err := tx.Prepare(`
		insert into entries (source, ext_id, ts, body_text, body_sha, ingested_at)
		values ('mail', ?, 0, 'x', 'x', 0) returning id`)
	if err != nil {
		b.Fatal(err)
	}
	vec, err := tx.Prepare(`
		insert into entry_embeddings (entry_id, model, dim, body_sha, prep, vec, embedded_at)
		values (?, 'bench', ?, 'x', ?, ?, 0)`)
	if err != nil {
		b.Fatal(err)
	}
	r := rand.New(rand.NewPCG(7, 11))
	for i := range benchEntries {
		var id int64
		if err := ins.QueryRow(fmt.Sprintf("mail:bench-%d", i)).Scan(&id); err != nil {
			b.Fatal(err)
		}
		if _, err := vec.Exec(id, benchDim, prepVersion, encodeVector(randomUnit(benchDim, r))); err != nil {
			b.Fatal(err)
		}
	}
	if err := tx.Commit(); err != nil {
		b.Fatal(err)
	}
	return s
}

func randomUnit(dim int, r *rand.Rand) []float32 {
	v := make([]float32, dim)
	for i := range v {
		v[i] = float32(r.NormFloat64())
	}
	if err := embed.Normalise(v); err != nil {
		panic(err)
	}
	return v
}
