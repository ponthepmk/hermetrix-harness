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

// ChunkRunes and ChunkOverlap size the windows a long text is split into before
// embedding.
//
// A bi-encoder returns one vector for whatever it is given, so a fact inside a
// large fragment is averaged away. Measured with bge-m3 against a question the
// fact answers:
//
//	the fact on its own                     0.567
//	the fact inside ~470 runes of padding   0.406
//	the fact inside ~5,600 runes            0.338
//	pure padding of the same length         0.354
//
// Buried deeply enough, the fragment holding the answer scores below one
// holding nothing. That is not a threshold that can be tuned around: the vector
// no longer represents the fact. Chunking is the fix, and 500 runes is where
// the signal was still intact.
//
// The overlap exists so a fact spanning a boundary is whole in one window --
// without it, splitting can destroy exactly what splitting was meant to
// preserve.
const (
	ChunkRunes   = 500
	ChunkOverlap = 100
)

// Chunk splits text into overlapping windows for embedding. Short text is
// returned as a single chunk.
func Chunk(text string) []string {
	runes := []rune(text)
	if len(runes) <= ChunkRunes {
		return []string{text}
	}
	stride := ChunkRunes - ChunkOverlap
	var chunks []string
	for start := 0; start < len(runes); start += stride {
		end := start + ChunkRunes
		if end >= len(runes) {
			chunks = append(chunks, string(runes[start:]))
			break
		}
		chunks = append(chunks, string(runes[start:end]))
	}
	return chunks
}

// Best returns the highest cosine between the query and any of the vectors,
// which is how a chunked document is scored: a document is as relevant as its
// most relevant part, not as its average.
func Best(query []float32, vectors [][]float32) float64 {
	best := 0.0
	for _, vector := range vectors {
		if score := Cosine(query, vector); score > best {
			best = score
		}
	}
	return best
}

// ChunkSpan returns the rune range a chunk index covers, so a caller that knows
// which chunk matched also knows where in the text to look.
//
// This is what makes semantic ranking useful to a compactor rather than only to
// a search. Ranking alone put the right fragment in the checkpoint and the
// extract still cut the fact out, because nothing told it where inside the
// fragment to aim. The vectors already knew: the chunk that scored highest is
// the passage that matters.
func ChunkSpan(index, length int) (start, end int) {
	if index <= 0 {
		start = 0
	} else {
		start = index * (ChunkRunes - ChunkOverlap)
	}
	if start > length {
		start = length
	}
	end = start + ChunkRunes
	if end > length {
		end = length
	}
	return start, end
}
