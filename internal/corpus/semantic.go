package corpus

import (
	"context"
	"errors"
	"strings"

	"github.com/zachpmanson/chainmail/internal/embed"
)

// SemanticOptions are the knobs a caller has over a vector ranking, above and
// beyond the query text itself.
type SemanticOptions struct {
	// Only makes the vector ranking the sole one, instead of a third voter
	// beside the lexical indexes.
	Only bool
	// TopK is how deep the vector ranking votes; zero means the default.
	TopK int
	// MinSimilarity overrides the model's calibrated floor when non-nil. A
	// pointer, not a float, because 0 is a meaningful value — "no floor at all"
	// — and is exactly what someone measuring the floor needs to be able to ask
	// for.
	MinSimilarity *float64
}

// SemanticFor embeds text with e and returns the vector ranking it describes.
//
// The embedding happens here, in the caller's call stack, rather than inside
// search: a down daemon then surfaces against whatever the user asked for,
// instead of as an empty result set from a function whose job is to return few
// results.
//
// The floor defaults to the model's own, from embed.Traits, and this is the only
// place that lookup happens. A caller that reaches past this and builds a
// SemanticQuery by hand gets no floor, which is the honest default for a model
// nobody has calibrated.
func SemanticFor(ctx context.Context, e embed.Embedder, text string, opt SemanticOptions) (*SemanticQuery, error) {
	if strings.TrimSpace(text) == "" {
		return nil, errors.New("a semantic search needs query text: there is nothing to be similar to")
	}
	vs, err := embed.Vectors(ctx, e, embed.Query, []string{text})
	if err != nil {
		return nil, err
	}
	floor := embed.TraitsFor(e.Model()).MinSimilarity
	if opt.MinSimilarity != nil {
		floor = *opt.MinSimilarity
	}
	return &SemanticQuery{
		Vector:        vs[0],
		Model:         e.Model(),
		Only:          opt.Only,
		TopK:          opt.TopK,
		MinSimilarity: floor,
	}, nil
}
