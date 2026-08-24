package context

import (
	"math"
	"strings"
	"sync"
	"unicode"
)

type Estimator interface{ Count(text string) int }

// AdaptiveEstimator is conservative for Thai/non-ASCII text and learns a
// bounded correction multiplier from provider-reported prompt usage.
type AdaptiveEstimator struct {
	mu         sync.RWMutex
	multiplier float64
}

func NewAdaptiveEstimator() *AdaptiveEstimator { return &AdaptiveEstimator{multiplier: 1} }

func (e *AdaptiveEstimator) Count(text string) int {
	base := heuristicTokens(text)
	e.mu.RLock()
	multiplier := e.multiplier
	e.mu.RUnlock()
	return int(math.Ceil(float64(base) * multiplier))
}

func (e *AdaptiveEstimator) Observe(predicted, actual int) {
	if predicted <= 0 || actual <= 0 {
		return
	}
	ratio := float64(actual) / float64(predicted)
	if ratio < 0.70 {
		ratio = 0.70
	}
	if ratio > 2.50 {
		ratio = 2.50
	}
	e.mu.Lock()
	e.multiplier = 0.8*e.multiplier + 0.2*ratio
	e.mu.Unlock()
}

func (e *AdaptiveEstimator) Multiplier() float64 {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.multiplier
}

func heuristicTokens(text string) int {
	asciiLetters, whitespace, punctuation, nonASCII := 0, 0, 0, 0
	for _, r := range text {
		switch {
		case r > 127:
			nonASCII++
		case unicode.IsSpace(r):
			whitespace++
		case strings.ContainsRune("{}[]():,;\"'`=<>/\\|+-*", r):
			punctuation++
		default:
			asciiLetters++
		}
	}
	count := (asciiLetters+whitespace+3)/4 + nonASCII + (punctuation+2)/3
	if count == 0 && text != "" {
		return 1
	}
	return count
}

// ScaledEstimator applies a fixed multiplier to the same heuristic. It exists so
// a compile can use a calibration that belongs to one provider and does not move
// while the compile is running.
//
// AdaptiveEstimator could not do that. It is one mutable float shared by every
// session, and Observe writes to it between model steps -- measured inside a
// single turn, step two predicted 3,632 tokens for strictly more context than
// step one's 3,677, because the multiplier had moved underneath it. A context
// budget that changes while the context is being built is not a budget.
type ScaledEstimator float64

func (e ScaledEstimator) Count(text string) int {
	multiplier := float64(e)
	if multiplier <= 0 {
		multiplier = 1
	}
	return int(math.Ceil(float64(heuristicTokens(text)) * multiplier))
}
