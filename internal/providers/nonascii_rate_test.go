package providers

import (
	"context"
	"math"
	"path/filepath"
	"sort"
	"testing"

	"hermetrix-harness/internal/store"
)

// liveThaiRequests are twenty-three consecutive requests measured against a real
// gateway running qwen3.8-27b-fp8, on a session whose context is roughly ninety
// percent Thai. Each row is the ASCII half of the estimate, the number of
// characters the ASCII rules do not cover, and the prompt tokens the provider
// billed for that exact request.
//
// They are kept verbatim because they are the evidence for the change: the
// estimator charged one token per non-ASCII character, and the truth is about a
// half.
var liveThaiRequests = []struct{ asciiTokens, nonASCIIChars, billed int }{
	{1767, 1348, 2483},
	{1785, 2904, 3253},
	{1800, 4434, 4014},
	{1815, 5970, 4773},
	{1831, 7594, 5589},
	{1846, 9080, 6324},
	{1864, 10645, 7127},
	{1952, 10680, 7282},
	{1970, 12245, 8075},
	{1989, 13895, 8921},
	{2007, 15409, 9673},
	{2091, 15440, 9824},
	{2113, 17101, 10684},
	{2131, 18693, 11512},
	{2147, 20238, 12319},
	{2167, 21796, 13125},
	{2184, 23335, 13888},
	{2263, 22877, 13879},
	{2309, 23416, 14257},
	{2343, 23930, 14835},
	{2345, 24236, 15276},
	{2342, 24240, 15200},
	{2408, 24100, 15695},
}

func bandOf(rate float64) (p95 float64, within int) {
	errors := make([]float64, 0, len(liveThaiRequests))
	for _, row := range liveThaiRequests {
		predicted := float64(row.asciiTokens) + rate*float64(row.nonASCIIChars)
		absolute := math.Abs(float64(row.billed)-predicted) / predicted
		errors = append(errors, absolute)
		if absolute <= 0.10 {
			within++
		}
	}
	sort.Float64s(errors)
	return errors[int(math.Ceil(0.95*float64(len(errors))))-1], within
}

// TestLearningTheScriptRateBringsTheBandInside is the whole justification for
// learning this number instead of assuming it. Replayed against the requests
// that exposed the problem, the assumed rate of one token per character misses
// the +/-10% gate on all but one request; the learned rate clears it on every
// one.
func TestLearningTheScriptRateBringsTheBandInside(t *testing.T) {
	assumedP95, assumedWithin := bandOf(1.0)
	if assumedWithin > 2 || assumedP95 < 0.30 {
		t.Fatalf("precondition: the assumed rate should fail badly, got p95 %.3f with %d of %d inside",
			assumedP95, assumedWithin, len(liveThaiRequests))
	}
	ctx := context.Background()
	dataStore, err := store.Open(ctx, filepath.Join(t.TempDir(), "data"))
	if err != nil {
		t.Fatal(err)
	}
	defer dataStore.Close()
	service := NewService(dataStore, nil)
	profile, err := service.Save(ctx, SaveInput{Name: "gateway", BaseURL: "https://models.example/v1",
		Model: "qwen-test", ContextWindow: 131072, MaxOutputTokens: 4096})
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range liveThaiRequests {
		if err := service.ObserveNonASCIIRate(ctx, profile.ID, row.asciiTokens, row.nonASCIIChars, row.billed); err != nil {
			t.Fatal(err)
		}
	}
	learned, err := service.Get(ctx, profile.ID)
	if err != nil {
		t.Fatal(err)
	}
	p95, within := bandOf(learned.NonASCIIRate)
	if within != len(liveThaiRequests) {
		t.Fatalf("learned rate %.4f leaves %d of %d requests outside the band (p95 %.3f)",
			learned.NonASCIIRate, len(liveThaiRequests)-within, len(liveThaiRequests), p95)
	}
	if p95 > 0.10 {
		t.Fatalf("learned rate %.4f gives p95 %.3f, outside the +/-10%% gate", learned.NonASCIIRate, p95)
	}
	if learned.NonASCIISample != len(liveThaiRequests) {
		t.Fatalf("sample = %d, want %d", learned.NonASCIISample, len(liveThaiRequests))
	}
}

// TestASCIIOnlyRequestsTeachNothingAboutAScriptRate keeps English traffic from
// dragging the rate around. Those requests carry no information about what a
// non-ASCII character costs.
func TestASCIIOnlyRequestsTeachNothingAboutAScriptRate(t *testing.T) {
	ctx := context.Background()
	dataStore, err := store.Open(ctx, filepath.Join(t.TempDir(), "data"))
	if err != nil {
		t.Fatal(err)
	}
	defer dataStore.Close()
	service := NewService(dataStore, nil)
	profile, err := service.Save(ctx, SaveInput{Name: "gateway", BaseURL: "https://models.example/v1",
		Model: "qwen-test", ContextWindow: 131072, MaxOutputTokens: 4096})
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 5; i++ {
		if err := service.ObserveNonASCIIRate(ctx, profile.ID, 1000, 0, 980); err != nil {
			t.Fatal(err)
		}
	}
	after, err := service.Get(ctx, profile.ID)
	if err != nil {
		t.Fatal(err)
	}
	if after.NonASCIISample != 0 || after.NonASCIIRate != 1 {
		t.Fatalf("ASCII-only traffic moved the script rate to %v after %d samples",
			after.NonASCIIRate, after.NonASCIISample)
	}
}

// TestOneImpossibleSampleCannotSetTheScriptRate bounds a single observation the
// same way the overall scale is bounded.
func TestOneImpossibleSampleCannotSetTheScriptRate(t *testing.T) {
	ctx := context.Background()
	dataStore, err := store.Open(ctx, filepath.Join(t.TempDir(), "data"))
	if err != nil {
		t.Fatal(err)
	}
	defer dataStore.Close()
	service := NewService(dataStore, nil)
	profile, err := service.Save(ctx, SaveInput{Name: "gateway", BaseURL: "https://models.example/v1",
		Model: "qwen-test", ContextWindow: 131072, MaxOutputTokens: 4096})
	if err != nil {
		t.Fatal(err)
	}
	// A provider reporting fewer tokens than the ASCII half alone implies a
	// negative rate, which is not a thing.
	if err := service.ObserveNonASCIIRate(ctx, profile.ID, 5000, 100, 1000); err != nil {
		t.Fatal(err)
	}
	after, _ := service.Get(ctx, profile.ID)
	if after.NonASCIIRate < nonASCIIRateFloor {
		t.Fatalf("rate fell to %v, below the %v floor", after.NonASCIIRate, nonASCIIRateFloor)
	}
	if err := service.ObserveNonASCIIRate(ctx, profile.ID, 0, 10, 100000); err != nil {
		t.Fatal(err)
	}
	after, _ = service.Get(ctx, profile.ID)
	if after.NonASCIIRate > nonASCIIRateCeiling {
		t.Fatalf("rate rose to %v, above the %v ceiling", after.NonASCIIRate, nonASCIIRateCeiling)
	}
}
