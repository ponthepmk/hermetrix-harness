package embedding

import (
	"encoding/json"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// A provider is allowed to return embeddings out of order -- the index field
// exists for exactly that -- and a vector attached to the wrong text is a
// silent, permanent mislabelling that no later check would catch.
func TestVectorsAreOrderedByTheReportedIndex(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		// Deliberately reversed.
		_ = json.NewEncoder(w).Encode(map[string]any{"data": []map[string]any{
			{"index": 1, "embedding": []float32{0, 1}},
			{"index": 0, "embedding": []float32{1, 0}},
		}})
	}))
	defer server.Close()

	embedder := NewOpenAIEmbedder(server.Client(), server.URL, "fake", "", 2)
	vectors, err := embedder.Embed(t.Context(), []string{"first", "second"})
	if err != nil {
		t.Fatal(err)
	}
	if vectors[0][0] != 1 || vectors[1][1] != 1 {
		t.Fatalf("vectors were taken in arrival order rather than by index: %v", vectors)
	}
}

// A model swapped behind the same endpoint returns a different width. Catching
// it on the first call is the difference between a clear error and a search
// that quietly returns nothing for the rest of the workspace's life.
func TestAWidthMismatchIsRefused(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"data": []map[string]any{
			{"index": 0, "embedding": []float32{1, 0, 0}},
		}})
	}))
	defer server.Close()

	embedder := NewOpenAIEmbedder(server.Client(), server.URL, "fake", "", 2)
	if _, err := embedder.Embed(t.Context(), []string{"one"}); err == nil {
		t.Fatal("a vector of the wrong width was accepted")
	}
}

// Stored vectors are normalised so one document cannot outrank another by
// being longer rather than by being more similar.
func TestNormaliseMakesLengthIrrelevant(t *testing.T) {
	short := Normalise([]float32{1, 1})
	long := Normalise([]float32{100, 100})
	if math.Abs(Cosine(short, long)-1) > 1e-6 {
		t.Fatalf("two vectors pointing the same way scored %.6f", Cosine(short, long))
	}
	if norm := Cosine(short, short); math.Abs(norm-1) > 1e-6 {
		t.Fatalf("a normalised vector is not unit length: %.6f", norm)
	}
}

// Vectors of different widths came from different models. Scoring them as if
// they shared a geometry would be worse than refusing.
func TestCosineRefusesMismatchedWidths(t *testing.T) {
	if score := Cosine([]float32{1, 0}, []float32{1, 0, 0}); score != 0 {
		t.Fatalf("mismatched widths scored %.3f", score)
	}
}

// A fact inside a long text is averaged out of that text's vector. Measured
// with bge-m3 against a question the fact answers: 0.567 for the fact alone,
// 0.406 inside ~470 runes of padding, 0.338 inside ~5,600 -- below the 0.354 of
// padding containing no fact at all. Chunking is what keeps the signal, and a
// chunk that loses a fact spanning its boundary would defeat the purpose.
func TestChunkingKeepsAFactWholeAcrossABoundary(t *testing.T) {
	const fact = "ROUND_HALF_UP_4096"
	pad := strings.Repeat("ก", ChunkRunes-len([]rune(fact))/2)
	text := pad + fact + pad

	chunks := Chunk(text)
	if len(chunks) < 2 {
		t.Fatalf("premise broken: the text fits in %d chunk(s)", len(chunks))
	}
	whole := 0
	for _, chunk := range chunks {
		if strings.Contains(chunk, fact) {
			whole++
		}
	}
	if whole == 0 {
		t.Fatal("the fact was split across every chunk, so no vector represents it")
	}
	// And short text is one chunk, not a needless split.
	if got := Chunk("สั้นมาก"); len(got) != 1 || got[0] != "สั้นมาก" {
		t.Fatalf("short text was chunked: %v", got)
	}
}

// A chunk index has to map back to where in the text it came from, or a scorer
// can say a fragment matters without being able to say where -- which is the
// case measured leaving reachability unchanged at 70 of 90.
func TestChunkSpanLocatesTheChunk(t *testing.T) {
	text := strings.Repeat("ก", 3*ChunkRunes)
	length := len([]rune(text))
	for index, chunk := range Chunk(text) {
		start, end := ChunkSpan(index, length)
		if start < 0 || end > length || start >= end {
			t.Fatalf("chunk %d spans [%d,%d) of %d", index, start, end, length)
		}
		if got := len([]rune(chunk)); index < 2 && got != ChunkRunes {
			t.Fatalf("chunk %d is %d runes, expected %d", index, got, ChunkRunes)
		}
	}
}
