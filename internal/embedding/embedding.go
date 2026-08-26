// Package embedding turns text into vectors so retrieval can cross a
// paraphrase.
//
// It exists because of a measurement. On the phrasing corpus, when a fact was
// stated in words the question did not use and sat where compaction discards
// content, the model searched for it 18 times out of 19 -- it knew something
// was missing and reached for the tool -- and the tool found it 3 times. The
// model queried in the question's words; the fact was written in different
// ones; and that is the same mismatch that made the compactor drop it. A
// lexical retriever cannot rescue what a lexical ranker discarded, because both
// fail on the same input for the same reason (O-44).
//
// Two things this package is careful about.
//
// It does not replace lexical matching. Where the wording did match, lexical
// retrieval was perfect -- far/head searched 7 and found 7 -- and an exact
// identifier like ROUND_HALF_UP_1024 is something a vector is worse at than a
// substring. The two are combined, not swapped.
//
// It is optional. Hermetrix is local-first and an embedder is a second model to
// run; with none configured every caller falls back to the lexical path it used
// before. Nothing here may become a hard dependency of answering a turn.
package embedding

import (
	"context"
	"errors"
	"math"
)

// ErrNoEmbedder means no embedder is configured. Callers treat it as "fall back
// to lexical", never as a failure worth surfacing: a workspace with no
// embedding model is a supported configuration, not a broken one.
var ErrNoEmbedder = errors.New("no embedder is configured")

// Embedder turns text into vectors.
//
// Implementations must be safe for concurrent use: a compile embeds a batch of
// fragments while a search may be embedding a query.
type Embedder interface {
	// Embed returns one vector per input, in the same order.
	Embed(ctx context.Context, texts []string) ([][]float32, error)
	// Revision identifies the model and its parameters. Vectors from different
	// revisions are not comparable -- their coordinates mean different things --
	// so stored vectors carry it and a mismatch invalidates rather than
	// silently mixing two geometries.
	Revision() string
	// Dimensions is the vector width, used to validate what comes back from a
	// provider before it is stored.
	Dimensions() int
}

// Cosine returns the cosine similarity of two vectors, in [-1, 1].
//
// Zero when the widths disagree rather than an error: a mismatch means the
// vectors came from different models, which is a fact about the store rather
// than about this pair, and the caller has already been told by the revision.
func Cosine(left, right []float32) float64 {
	if len(left) != len(right) || len(left) == 0 {
		return 0
	}
	var dot, leftNorm, rightNorm float64
	for index := range left {
		l, r := float64(left[index]), float64(right[index])
		dot += l * r
		leftNorm += l * l
		rightNorm += r * r
	}
	if leftNorm == 0 || rightNorm == 0 {
		return 0
	}
	return dot / (math.Sqrt(leftNorm) * math.Sqrt(rightNorm))
}

// Normalise scales a vector to unit length in place and returns it.
//
// Stored vectors are normalised so a later cosine is a dot product, and so a
// provider that returns unnormalised vectors cannot make one document
// systematically outrank another by being longer.
func Normalise(vector []float32) []float32 {
	var norm float64
	for _, value := range vector {
		norm += float64(value) * float64(value)
	}
	if norm == 0 {
		return vector
	}
	scale := 1 / math.Sqrt(norm)
	for index := range vector {
		vector[index] = float32(float64(vector[index]) * scale)
	}
	return vector
}
