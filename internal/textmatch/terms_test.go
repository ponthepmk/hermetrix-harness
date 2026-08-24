package textmatch

import "testing"

func TestThaiTextProducesComparableUnits(t *testing.T) {
	_, grams := Terms("ปัดเศษเงินไทยเป็นสตางค์")
	if len(grams) == 0 {
		t.Fatal("Thai text produced no comparable units; a Thai query can only match a byte-identical string")
	}
	_, other := Terms("ปัดเศษสตางค์")
	if Overlap(grams, other) == 0 {
		t.Fatal("two Thai phrases about the same thing share nothing")
	}
	_, unrelated := Terms("จัดทำงบกระทบยอดเงินฝากธนาคาร")
	if Overlap(grams, unrelated) >= Overlap(grams, other) {
		t.Fatalf("unrelated Thai text overlaps as much as related text: %d vs %d",
			Overlap(grams, unrelated), Overlap(grams, other))
	}
}

func TestCompoundWordIsComparableByEitherHalf(t *testing.T) {
	words, _ := Terms("satang-rounding")
	for _, want := range []string{"satang-rounding", "satang", "rounding"} {
		if !words[want] {
			t.Fatalf("%q missing from %v", want, words)
		}
	}
}

// Units shorter than three characters are dropped: they are mostly noise and
// this matches what both original tokenizers did.
func TestUnitsShorterThanThreeCharactersAreDropped(t *testing.T) {
	words, _ := Terms("a an the vat")
	if words["a"] || words["an"] {
		t.Fatalf("one- and two-character words were kept: %v", words)
	}
	if !words["the"] || !words["vat"] {
		t.Fatalf("three-character words were dropped: %v", words)
	}
}

func TestASCIITextProducesNoGrams(t *testing.T) {
	_, grams := Terms("round money half up")
	if len(grams) != 0 {
		t.Fatalf("ASCII text produced grams: %v", grams)
	}
}
