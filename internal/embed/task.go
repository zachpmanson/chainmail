package embed

import (
	"context"
	"strings"
)

// Task is what a piece of text is being embedded for.
//
// It exists because some models are trained asymmetrically: a question and the
// passage answering it are not the same kind of text, and such a model expects
// to be told which it is looking at. A model trained symmetrically ignores the
// distinction, so a caller states the task unconditionally and the model's own
// traits decide whether it means anything.
type Task int

const (
	// Document is stored text: the thing that will be searched.
	Document Task = iota
	// Query is a question asked of the stored text.
	Query
)

func (t Task) String() string {
	if t == Query {
		return "query"
	}
	return "document"
}

// Prefixer is implemented by an Embedder that knows its model's task prefixes.
// An Embedder that does not implement it is treated as wanting none, which is
// the right default: a prefix a model was not trained with is just two tokens
// of noise at the front of every vector.
type Prefixer interface {
	// Prefix is the string to prepend for a task, or "" for none.
	Prefix(Task) string
}

// Vectors embeds texts for a task, applying whatever prefix the model wants.
//
// This is the entry point every caller should use; Embedder.Embed is the raw
// primitive underneath it. The split is deliberate: a caller knows whether it
// holds a question or a corpus body, and that is all it should have to know.
// Which strings implement that distinction is the model's business, and putting
// them here — rather than in the Embedder interface — is what keeps a model
// swap from silently prefixing text no model asked to be prefixed.
//
// The prefix is applied on the way to the model and is not visible in anything
// stored, which is why prep in the corpus versions this: the stored text and its
// hash are unchanged while the vector is not.
func Vectors(ctx context.Context, e Embedder, t Task, texts []string) ([][]float32, error) {
	if len(texts) == 0 {
		return nil, nil
	}
	prefix := ""
	if p, ok := e.(Prefixer); ok {
		prefix = p.Prefix(t)
	}
	if prefix == "" {
		return e.Embed(ctx, texts)
	}
	pre := make([]string, len(texts))
	for i, s := range texts {
		pre[i] = prefix + s
	}
	return e.Embed(ctx, pre)
}

// Traits are the per-model facts a model cannot report about itself: the task
// prefixes it was trained with, and the cosine below which its scores stop
// meaning anything.
//
// Keyed by model name because that is where model identity already lives. The
// alternative — one constant for the corpus — is wrong in both directions: the
// prefixes are ignored or actively harmful on a model not trained with them,
// and a floor calibrated on one model's score distribution has no bearing on
// another's, since two embedding models do not agree on what 0.6 means.
type Traits struct {
	// QueryPrefix and DocumentPrefix are prepended before embedding. Both empty
	// means a symmetric model.
	QueryPrefix    string
	DocumentPrefix string

	// MinSimilarity is the cosine floor at which results stop being worth
	// showing, measured against this model's own scores. Zero means the model
	// has not been calibrated and no floor should be assumed.
	MinSimilarity float64
}

// Prefix is the prefix for a task.
func (tr Traits) Prefix(t Task) string {
	if t == Query {
		return tr.QueryPrefix
	}
	return tr.DocumentPrefix
}

// traits holds every model that has been characterised. An unlisted model gets
// the zero Traits: no prefixes, no floor. That is the safe default — it is the
// behaviour of a model nobody has measured, and it never invents a cutoff on a
// score distribution nobody has looked at.
var traits = map[string]Traits{
	// nomic-embed-text is trained with these exact prefixes, trailing space
	// included, and its published retrieval numbers are all measured with them.
	// The floor comes from measuring this corpus: an absurd query's best match
	// sits around 0.5 and a real query's true answers around 0.7 and up, so the
	// cut goes between them, nearer the noise than the signal because losing a
	// true answer costs more than showing a weak one.
	"nomic-embed-text": {
		QueryPrefix:    "search_query: ",
		DocumentPrefix: "search_document: ",
		MinSimilarity:  0.6,
	},
}

// TraitsFor returns what is known about a model, by name.
//
// An ollama tag is stripped first: "nomic-embed-text:latest" and
// "nomic-embed-text" are the same weights, and a user who pinned a tag should
// not silently lose the prefixes the model needs. A tag that is genuinely a
// different model — a quantisation, a version bump — shares the base model's
// training and therefore its prefixes; if it ever needs a different floor, it
// earns its own entry here.
func TraitsFor(model string) Traits {
	if t, ok := traits[model]; ok {
		return t
	}
	if base, _, tagged := strings.Cut(model, ":"); tagged {
		return traits[base]
	}
	return Traits{}
}
