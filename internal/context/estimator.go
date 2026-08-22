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
