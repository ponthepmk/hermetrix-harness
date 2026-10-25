package taskscope

import (
	"errors"
	"strings"
)

const (
	maxWritableCount     = 512
	maxAggregateBytes    = 32768
	maxPathBytes         = 512
	maxOutputCount       = 32
	maxLegacyWritableCnt = 32
)

// ValidateWritablePaths checks global bounds and canonical form of a writable
// path list. It rejects invalid input rather than cleaning it.
func ValidateWritablePaths(paths []string) error {
	if len(paths) > maxWritableCount {
		return errors.New("taskscope: writable path count exceeds limit")
	}
	seen := make(map[string]struct{}, len(paths))
	aggregate := 0
	for _, p := range paths {
		if err := validateCanonical(p); err != nil {
			return err
		}
		if len(p) > maxPathBytes {
			return errors.New("taskscope: path byte length exceeds limit")
		}
		aggregate += len(p) + 1
		if _, dup := seen[p]; dup {
			return errors.New("taskscope: duplicate writable path")
		}
		seen[p] = struct{}{}
	}
	if aggregate > maxAggregateBytes {
		return errors.New("taskscope: aggregate path bytes exceed limit")
	}
	return nil
}

// ValidateOutputPaths validates the writable scope globally and then checks
// that outputs (when non-empty) form a canonical distinct subset of at most 32
// explicit writable paths. Blank outputs require legacy writable <= 32.
func ValidateOutputPaths(outputs, writable []string) error {
	if err := ValidateWritablePaths(writable); err != nil {
		return err
	}
	if len(outputs) == 0 {
		if len(writable) > maxLegacyWritableCnt {
			return errors.New("taskscope: legacy writable count exceeds limit")
		}
		return nil
	}
	if len(writable) == 0 {
		return errors.New("taskscope: explicit outputs require non-empty writable scope")
	}
	if len(outputs) > maxOutputCount {
		return errors.New("taskscope: output path count exceeds limit")
	}
	writableSet := make(map[string]struct{}, len(writable))
	for _, w := range writable {
		writableSet[w] = struct{}{}
	}
	seen := make(map[string]struct{}, len(outputs))
	for _, o := range outputs {
		if err := validateCanonical(o); err != nil {
			return err
		}
		if _, dup := seen[o]; dup {
			return errors.New("taskscope: duplicate output path")
		}
		seen[o] = struct{}{}
		if _, ok := writableSet[o]; !ok {
			return errors.New("taskscope: output path not in writable scope")
		}
	}
	return nil
}

// EffectiveWritablePaths returns a defensive copy of outputs when non-empty,
// otherwise a defensive copy of writable. Nil is preserved when the result
// would be nil. Arguments are never mutated.
func EffectiveWritablePaths(outputs, writable []string) ([]string, error) {
	if err := ValidateOutputPaths(outputs, writable); err != nil {
		return nil, err
	}
	if len(outputs) > 0 {
		out := make([]string, len(outputs))
		copy(out, outputs)
		return out, nil
	}
	if writable == nil {
		return nil, nil
	}
	out := make([]string, len(writable))
	copy(out, writable)
	return out, nil
}

func validateCanonical(p string) error {
	if p == "" {
		return errors.New("taskscope: empty path")
	}
	if p != strings.TrimSpace(p) {
		return errors.New("taskscope: path has outer whitespace")
	}
	if strings.HasPrefix(p, "/") {
		return errors.New("taskscope: absolute path not allowed")
	}
	for _, r := range p {
		switch r {
		case '\\', ':', '*', '?', '\r', '\n', '\x00':
			return errors.New("taskscope: path contains forbidden character")
		}
	}
	for _, seg := range strings.Split(p, "/") {
		if seg == "" || seg == "." || seg == ".." {
			return errors.New("taskscope: invalid path segment")
		}
	}
	return nil
}
