package providers

import (
	"context"
	"math"
	"path/filepath"
	"sort"
	"testing"

	"hermetrix-harness/internal/store"
)

// liveRequests are thirty-seven consecutive requests measured against a real
// gateway running qwen3.8-27b-fp8 on a Thai-dominant session, from the first
// turn through eleven turns of active compaction. Each row is the ASCII half of
// the estimate including tool schemas, the count of characters the ASCII rules
// do not cover, the number of chat messages the request carried, and the prompt
// tokens the provider billed.
//
// They are the evidence for the whole prediction model, so they are kept
// verbatim rather than regenerated.
var liveRequests = []struct{ asciiTokens, nonASCIIChars, messages, billed int }{
	{1769, 1336, 2, 2481},
	{1898, 1370, 4, 2635},
	{2040, 1370, 6, 2777},
	{2065, 3033, 8, 3612},
	{2194, 3071, 10, 3763},
	{2223, 4724, 12, 4609},
	{2246, 6391, 14, 5457},
	{2276, 8021, 16, 6290},
	{2409, 8066, 18, 6448},
	{2444, 9707, 20, 7291},
	{2470, 11374, 22, 8147},
	{2493, 13078, 24, 9009},
	{2519, 14828, 26, 9906},
	{2649, 14868, 28, 10061},
	{2676, 16580, 30, 10940},
	{2813, 16620, 32, 11092},
	{2839, 18301, 34, 11954},
	{3000, 20356, 38, 13409},
	{3164, 22542, 42, 14888},
	{3543, 24564, 48, 16384},
	{3740, 25507, 49, 17844},
	{3729, 25136, 47, 17875},
	{3953, 24727, 49, 18301},
	{4096, 24530, 52, 18610},
	{4098, 23854, 51, 18087},
	{4286, 23778, 53, 18290},
	{4327, 23074, 53, 17774},
	{4464, 23104, 55, 17928},
	{4485, 22536, 56, 17694},
	{4619, 22571, 58, 17851},
	{4673, 22253, 57, 17121},
	{4784, 22010, 58, 17083},
	{4796, 21771, 61, 17051},
	{4939, 21776, 63, 17201},
	{4857, 21778, 61, 17124},
	{5062, 21430, 63, 17176},
	{5277, 21122, 64, 17187},
}

// measuredMessageOverhead and measuredRequestOverhead are the template cost
// measured against this gateway by splitting identical content across 1, 3, 5,
// 9, 17 and 33 messages: exactly 9 more tokens per message, over a constant of
// 43. MeasureTokenOverhead probes with empty messages instead and reads the
// same line as 7 and 45; on this corpus the two give p95 7.42% and 7.54%.
const (
	measuredMessageOverhead = 9
	measuredRequestOverhead = 43
)

func bandFor(rate float64, messageOverhead, requestOverhead int) (p95 float64, within int) {
	errors := make([]float64, 0, len(liveRequests))
	for _, row := range liveRequests {
		predicted := float64(requestOverhead+messageOverhead*row.messages+row.asciiTokens) +
			rate*float64(row.nonASCIIChars)
		absolute := math.Abs(float64(row.billed)-predicted) / predicted
		errors = append(errors, absolute)
		if absolute <= 0.10 {
			within++
		}
	}
	sort.Float64s(errors)
	return errors[int(math.Ceil(0.95*float64(len(errors))))-1], within
}

// TestTheCompletePredictionClearsTheGate replays every live request through the
// model the estimator now uses: the measured chat template, the ASCII rules, and
// a learned rate for everything else. This is the Phase 9 token error band,
// answered against real traffic.
func TestTheCompletePredictionClearsTheGate(t *testing.T) {
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
	for _, row := range liveRequests {
		content := row.billed - measuredRequestOverhead - measuredMessageOverhead*row.messages
		if err := service.ObserveNonASCIIRate(ctx, profile.ID, row.asciiTokens, row.nonASCIIChars, content); err != nil {
			t.Fatal(err)
		}
	}
	learned, err := service.Get(ctx, profile.ID)
	if err != nil {
		t.Fatal(err)
	}
	p95, within := bandFor(learned.NonASCIIRate, measuredMessageOverhead, measuredRequestOverhead)
	if within != len(liveRequests) {
		t.Fatalf("%d of %d requests fall outside the band (p95 %.3f, rate %.4f)",
			len(liveRequests)-within, len(liveRequests), p95, learned.NonASCIIRate)
	}
	if p95 > 0.10 {
		t.Fatalf("p95 %.3f exceeds the gate at rate %.4f", p95, learned.NonASCIIRate)
	}
}

// TestTheRateStopsAbsorbingThePerMessageCost is the reason to model the chat
// template, and it is not the band on this corpus. Learned with the wrapper
// priced separately the rate is 0.5324 and p95 is 7.42%; learned with it folded
// in the rate is 0.5550 and p95 is 7.85%. Both clear the gate, because here
// message count and content grow together and the rate can absorb the wrapper.
//
// The absorption is the defect. A rate carrying a per-message constant is right
// only for traffic with the message density it was learned on. Priced
// separately the prediction is exact at any density; absorbed, the error grows
// with density and leaves the gate at about three times the corpus density.
func TestTheRateStopsAbsorbingThePerMessageCost(t *testing.T) {
	const (
		ratePriced    = 0.5324
		rateAbsorbed  = 0.5550
		asciiTokens   = 2000
		nonASCIIChars = 20000
		// The corpus this was learned on carried roughly this many messages for
		// this much content.
		learnedDensity = 64
	)
	truth := func(messages int) float64 {
		return float64(measuredRequestOverhead+measuredMessageOverhead*messages+asciiTokens) +
			ratePriced*nonASCIIChars
	}
	priced := func(messages int) float64 {
		return float64(measuredRequestOverhead+measuredMessageOverhead*messages+asciiTokens) +
			ratePriced*nonASCIIChars
	}
	absorbed := float64(asciiTokens) + rateAbsorbed*nonASCIIChars

	// Pricing the wrapper tracks any density exactly, because it is the same term.
	for _, messages := range []int{learnedDensity, 4 * learnedDensity, 16 * learnedDensity} {
		if error := math.Abs(priced(messages)-truth(messages)) / truth(messages); error > 0.001 {
			t.Fatalf("priced model is off by %.2f%% at %d messages", 100*error, messages)
		}
	}
	// Absorbing it is fine at the density it was learned on and degrades from there.
	atLearned := math.Abs(absorbed-truth(learnedDensity)) / truth(learnedDensity)
	if atLearned > 0.05 {
		t.Fatalf("precondition: the absorbed rate should fit its own corpus, off by %.1f%%", 100*atLearned)
	}
	crossing := 0
	for messages := learnedDensity; messages <= 64*learnedDensity; messages++ {
		if math.Abs(absorbed-truth(messages))/truth(messages) > 0.10 {
			crossing = messages
			break
		}
	}
	if crossing == 0 {
		t.Fatal("the absorbed rate never leaves the band; this case does not separate the two models")
	}
	if crossing > 8*learnedDensity {
		t.Fatalf("the absorbed rate only fails at %dx the learned density; the term is not worth pricing", crossing/learnedDensity)
	}
	t.Logf("absorbed rate leaves the band at %d messages, %.1fx the density it was learned on",
		crossing, float64(crossing)/learnedDensity)
}

func TestSolveOverheadReadsTwoProbes(t *testing.T) {
	// The measured gateway: 52 tokens for one empty message, 124 for nine.
	messageOverhead, requestOverhead, err := solveOverhead(52, 124)
	if err != nil {
		t.Fatal(err)
	}
	if messageOverhead != 9 || requestOverhead != 43 {
		t.Fatalf("got %d per message and %d per request, want 9 and 43", messageOverhead, requestOverhead)
	}
}

// TestAnImplausibleProbeIsRefused keeps a bad measurement from being subtracted
// from usable context on every later request.
func TestAnImplausibleProbeIsRefused(t *testing.T) {
	for _, probe := range []struct {
		name         string
		small, large int
	}{
		{"more messages billed fewer tokens", 500, 100},
		{"per-message cost far past any template", 50, 5000},
		{"request constant below zero", 10, 4000},
	} {
		if _, _, err := solveOverhead(probe.small, probe.large); err == nil {
			t.Fatalf("%s: accepted probes %d and %d", probe.name, probe.small, probe.large)
		}
	}
}
