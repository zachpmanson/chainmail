package eval

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/zachpmanson/chainmail/internal/corpus"
	"github.com/zachpmanson/chainmail/internal/embed"
)

// DefaultCutoffs are the depths recall and nDCG are reported at. 1 is "was the
// best answer first", 10 is roughly a screenful, and 5 is where the difference
// between the two usually lives.
var DefaultCutoffs = []int{1, 5, 10}

// Config is one retrieval configuration: a corpus, a model, and how they are
// asked. Two of these run against one judged set, which is the whole point —
// a retrieval number in isolation says nothing, and the only honest claim is a
// delta between two configurations over the same queries.
type Config struct {
	// Name labels the configuration in the report.
	Name string
	// DB is the corpus to search. Two configurations may name different
	// corpora, which is how a change to *stored* vectors is compared at all:
	// prefixed and unprefixed documents cannot coexist under one model name.
	DB string

	Mode  string // lexical | semantic | hybrid
	Model string
	URL   string
	Dim   int
	TopK  int

	// MinSim overrides the model's calibrated floor when non-nil. A pointer
	// because 0 — no floor at all — is a configuration worth measuring, and is
	// how the floor was chosen in the first place.
	MinSim *float64

	// NoPrefix strips the model's task prefixes from the query side.
	//
	// It exists for exactly one measurement: scoring a corpus embedded before
	// prefixes existed. A prefixed query against unprefixed documents is the
	// mismatch under test, so the two sides have to be settable independently
	// even though production only ever wants them to agree.
	NoPrefix bool
}

// ParseConfig reads a configuration from a comma-separated key=value spec, so
// that two of them fit on one command line.
func ParseConfig(spec string) (Config, error) {
	c := Config{}
	for _, part := range strings.Split(spec, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		k, v, ok := strings.Cut(part, "=")
		if !ok {
			return c, fmt.Errorf("%q: want key=value", part)
		}
		k, v = strings.TrimSpace(k), strings.TrimSpace(v)
		var err error
		switch k {
		case "name":
			c.Name = v
		case "db":
			c.DB = v
		case "mode":
			c.Mode = v
		case "model":
			c.Model = v
		case "url":
			c.URL = v
		case "dim":
			c.Dim, err = strconv.Atoi(v)
		case "topk":
			c.TopK, err = strconv.Atoi(v)
		case "minsim":
			var f float64
			f, err = strconv.ParseFloat(v, 64)
			c.MinSim = &f
		case "noprefix":
			c.NoPrefix, err = strconv.ParseBool(v)
		default:
			return c, fmt.Errorf("unknown key %q: want name, db, mode, model, url, dim, topk, minsim or noprefix", k)
		}
		if err != nil {
			return c, fmt.Errorf("%s=%s: %w", k, v, err)
		}
	}
	if c.Mode == "" {
		c.Mode = "hybrid"
	}
	if c.Mode != "lexical" && c.Mode != "semantic" && c.Mode != "hybrid" {
		return c, fmt.Errorf("mode %q: want lexical, semantic or hybrid", c.Mode)
	}
	if c.Model == "" {
		c.Model = embed.DefaultModel
	}
	if c.Dim <= 0 {
		c.Dim = embed.DefaultDim
	}
	if c.URL == "" {
		c.URL = embed.DefaultBaseURL
	}
	if c.Name == "" {
		c.Name = c.Mode
	}
	return c, nil
}

// Target is a Config made concrete: an open corpus and a model to ask.
type Target struct {
	Cfg   Config
	Store *corpus.Store
	// Model is the embedder. Nil is legal for a lexical configuration, and for
	// nothing else.
	Model embed.Embedder

	owns bool
}

// unprefixed reports a model as wanting no task prefixes, whatever its traits
// say. Wrapping rather than editing the traits keeps the override to one
// configuration instead of to the process.
type unprefixed struct{ embed.Embedder }

func (unprefixed) Prefix(embed.Task) string { return "" }

// OpenTarget opens the corpus a configuration names and builds the client for
// its model. Close it when done.
func OpenTarget(cfg Config, timeout time.Duration) (*Target, error) {
	s, err := corpus.Open(cfg.DB)
	if err != nil {
		return nil, err
	}
	t := &Target{Cfg: cfg, Store: s, owns: true}
	if cfg.Mode != "lexical" {
		var e embed.Embedder = &embed.Ollama{BaseURL: cfg.URL, Name: cfg.Model,
			Dimension: cfg.Dim, Client: &http.Client{Timeout: timeout}}
		if cfg.NoPrefix {
			e = unprefixed{e}
		}
		t.Model = e
	}
	return t, nil
}

// Close releases the corpus, if this Target opened it.
func (t *Target) Close() error {
	if t == nil || !t.owns || t.Store == nil {
		return nil
	}
	return t.Store.Close()
}

// CaseResult is what one configuration did with one query.
type CaseResult struct {
	Case   Case
	Ranked []string  // ext_ids, best first
	Sims   []float64 // cosine per ranked result, 0 where the vectors did not rank it

	Recall    map[int]float64
	NDCG      map[int]float64
	RR        float64
	FirstRank int
	Returned  int
}

// Report is one configuration's scores over a whole set.
type Report struct {
	Cfg     Config
	Level   string
	Cutoffs []int
	Cases   []CaseResult

	// Judged aggregates, macro-averaged over the cases that have judgements.
	Judged int
	Recall map[int]float64
	NDCG   map[int]float64
	MRR    float64

	// Negative cases: how many there were, how many came back clean, and how
	// many results leaked through in total. Leaked is the number to watch when
	// lowering a floor: Clean can stay flat while every absurd query gains a
	// result.
	Negative int
	Clean    int
	Leaked   int
	// TopNoise is the highest cosine any negative case scored, which is the
	// measurement a floor is chosen from.
	TopNoise float64

	// HitSims and NoiseSims are the cosines of, respectively, every judged-
	// relevant result that was returned and every result a negative case
	// returned. They are the two distributions a similarity floor has to
	// separate, and keeping them is what makes the choice of floor a
	// measurement rather than a preference. Both are empty for a lexical
	// configuration, which has no cosines.
	HitSims   []float64
	NoiseSims []float64
}

// Run scores a set against a target.
//
// Every query goes through the same public search path the CLI uses, embedder
// included. A harness that reimplemented ranking to make scoring convenient
// would measure the harness.
func (t *Target) Run(ctx context.Context, set Set, cutoffs []int) (Report, error) {
	if len(cutoffs) == 0 {
		cutoffs = DefaultCutoffs
	}
	depth := 0
	for _, k := range cutoffs {
		depth = max(depth, k)
	}
	rep := Report{Cfg: t.Cfg, Level: set.Level, Cutoffs: cutoffs,
		Recall: map[int]float64{}, NDCG: map[int]float64{}}
	if rep.Level == "" {
		rep.Level = LevelEntry
	}

	perK := map[int][]float64{}
	perKN := map[int][]float64{}
	var rrs []float64

	for _, c := range set.Cases {
		ranked, sims, err := t.rank(ctx, c.Query, rep.Level, depth)
		if err != nil {
			return rep, fmt.Errorf("query %q: %w", c.Query, err)
		}
		res := CaseResult{Case: c, Ranked: ranked, Sims: sims,
			Returned: len(ranked), Recall: map[int]float64{}, NDCG: map[int]float64{}}
		if c.ExpectEmpty {
			rep.Negative++
			rep.Leaked += len(ranked)
			if len(ranked) == 0 {
				rep.Clean++
			}
			for _, s := range sims {
				rep.TopNoise = max(rep.TopNoise, s)
			}
			rep.NoiseSims = append(rep.NoiseSims, sims...)
			rep.Cases = append(rep.Cases, res)
			continue
		}
		rel := relevantOf(c)
		for i, id := range ranked {
			if rel[id] && i < len(sims) {
				rep.HitSims = append(rep.HitSims, sims[i])
			}
		}
		for _, k := range cutoffs {
			res.Recall[k] = RecallAt(ranked, rel, k)
			res.NDCG[k] = NDCGAt(ranked, rel, k)
			perK[k] = append(perK[k], res.Recall[k])
			perKN[k] = append(perKN[k], res.NDCG[k])
		}
		res.RR = ReciprocalRank(ranked, rel)
		res.FirstRank = FirstRelevantRank(ranked, rel)
		rrs = append(rrs, res.RR)
		rep.Judged++
		rep.Cases = append(rep.Cases, res)
	}

	for _, k := range cutoffs {
		rep.Recall[k] = mean(perK[k])
		rep.NDCG[k] = mean(perKN[k])
	}
	rep.MRR = mean(rrs)
	return rep, nil
}

// rank runs one query and returns the ranked ids at the set's level.
func (t *Target) rank(ctx context.Context, text, level string, depth int) ([]string, []float64, error) {
	q := corpus.Query{Text: text, Limit: depth}
	if t.Cfg.Mode != "lexical" {
		if t.Model == nil {
			return nil, nil, fmt.Errorf("mode %s needs a model", t.Cfg.Mode)
		}
		sem, err := corpus.SemanticFor(ctx, t.Model, text, corpus.SemanticOptions{
			Only:          t.Cfg.Mode == "semantic",
			TopK:          t.Cfg.TopK,
			MinSimilarity: t.Cfg.MinSim,
		})
		if err != nil {
			return nil, nil, err
		}
		q.Semantic = sem
	}
	if level == LevelChain {
		chains, err := t.Store.SearchChains(q)
		if err != nil {
			return nil, nil, err
		}
		ids := make([]string, 0, len(chains))
		sims := make([]float64, 0, len(chains))
		for _, c := range chains {
			ids = append(ids, c.RootExtID)
			// A chain has no cosine of its own; the best of its members stands
			// in, since that is the score that put it here.
			best := 0.0
			for _, h := range c.Best {
				best = max(best, h.Similarity)
			}
			sims = append(sims, best)
		}
		return ids, sims, nil
	}
	hits, err := t.Store.SearchEntries(q)
	if err != nil {
		return nil, nil, err
	}
	ids := make([]string, 0, len(hits))
	sims := make([]float64, 0, len(hits))
	for _, h := range hits {
		ids = append(ids, h.ExtID)
		sims = append(sims, h.Similarity)
	}
	return ids, sims, nil
}
