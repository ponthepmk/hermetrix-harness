package embedding

import (
	"encoding/json"
	"math"
	"net/http"
	"net/http/httptest"
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
