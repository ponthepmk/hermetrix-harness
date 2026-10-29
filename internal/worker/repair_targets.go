package worker

import (
	"encoding/json"
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"
)

// RepairTargetVersion pins the derivation recipe. Zero preserves historical
// tasks and hashes; a new recipe must not reinterpret an old frozen proposal.
const RepairTargetVersion = 1

const maxRepairTargets = 32
const maxRepairTargetWireBytes = 8192

// RepairTarget describes syntax, not a proven cause or additional write scope.
// Preview can be incomplete; replacement always covers the full host span.
type RepairTarget struct {
	ID               string `json:"id"`
	Kind             string `json:"kind"`
	Symbol           string `json:"symbol"`
	Start            int    `json:"start"`
	End              int    `json:"end"`
	Preview          string `json:"preview"`
	PreviewTruncated bool   `json:"preview_truncated,omitempty"`
}

func buildRepairTargets(task Task) (map[string][]RepairTarget, error) {
	if task.RepairTargetVersion == 0 {
		return nil, nil
	}
	if task.RepairTargetVersion != RepairTargetVersion || len(task.RepairDrafts) == 0 {
		return nil, errors.New("unsupported repair-target recipe or missing drafts")
	}
	if err := validateRepairDrafts(task); err != nil {
		return nil, err
	}
	type candidate struct {
		path     string
		target   RepairTarget
		priority int
	}
	var candidates []candidate
	paths := make([]string, 0, len(task.RepairDrafts))
	for path := range task.RepairDrafts {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	for _, path := range paths {
		draft := task.RepairDrafts[path]
		add := func(kind, symbol string, start, end, priority int) {
			if start < 0 || end <= start || end > len(draft.Content) {
				return
			}
			text := draft.Content[start:end]
			n := min(len(text), 192)
			for n < len(text) && n > 0 && !utf8.RuneStart(text[n]) {
				n--
			}
			binding := struct {
				Version                       int
				Path, SourceSHA, Kind, Symbol string
				Start, End                    int
			}{task.RepairTargetVersion, path, draft.SourceSHA256, kind, symbol, start, end}
			b, _ := json.Marshal(binding)
			candidates = append(candidates, candidate{path, RepairTarget{ID: Hash(string(b)), Kind: kind, Symbol: symbol, Start: start, End: end, Preview: text[:n], PreviewTruncated: n < len(text)}, priority})
		}
		if strings.HasSuffix(path, ".go") {
			set := token.NewFileSet()
			file, _ := parser.ParseFile(set, path, draft.Content, parser.SkipObjectResolution)
			if file != nil {
				offset := func(pos token.Pos) int {
					if pos == token.NoPos {
						return -1
					}
					return set.PositionFor(pos, false).Offset
				}
				for _, decl := range file.Decls {
					switch node := decl.(type) {
					case *ast.FuncDecl:
						add("go_function", node.Name.Name, offset(node.Pos()), offset(node.End()), 1)
						if node.Body != nil {
							ast.Inspect(node.Body, func(n ast.Node) bool {
								literal, ok := n.(*ast.CompositeLit)
								if !ok {
									return true
								}
								for _, element := range literal.Elts {
									pair, ok := element.(*ast.KeyValueExpr)
									if !ok || pair.Value == nil {
										continue
									}
									key, ok := pair.Key.(*ast.Ident)
									if !ok {
										continue
									}
									add("go_keyed_value", node.Name.Name+"."+key.Name, offset(pair.Value.Pos()), offset(pair.Value.End()), 0)
								}
								return true
							})
						}
					case *ast.GenDecl:
						for _, spec := range node.Specs {
							switch s := spec.(type) {
							case *ast.TypeSpec:
								add("go_type", s.Name.Name, offset(s.Pos()), offset(s.End()), 2)
							case *ast.ValueSpec:
								if len(s.Names) > 0 {
									add("go_value_spec", s.Names[0].Name, offset(s.Pos()), offset(s.End()), 2)
								}
							}
						}
					}
				}
			}
		}
		// Generic spans do not claim a language-level symbol or causal meaning.
		start, line := 0, 1
		for _, text := range strings.SplitAfter(draft.Content, "\n") {
			end := start + len(text)
			if strings.TrimSpace(text) != "" {
				add("text_line", "line "+strconv.Itoa(line), start, end, 3)
			}
			start = end
			line++
		}
		add("whole_file", "complete historical file", 0, len(draft.Content), 4)
	}
	// Prefer precise syntax spans, then generic text. Never offer a parent and
	// its child simultaneously: a model cannot use both in one replacement.
	// Broader changes remain possible via the existing exact-text edit form.
	sort.SliceStable(candidates, func(i, j int) bool {
		a, b := candidates[i], candidates[j]
		if (a.priority < 3) != (b.priority < 3) {
			return a.priority < 3
		}
		if a.priority >= 3 && a.priority != b.priority {
			return a.priority < b.priority
		}
		if a.path != b.path {
			return a.path < b.path
		}
		if a.target.End-a.target.Start != b.target.End-b.target.Start {
			return a.target.End-a.target.Start < b.target.End-b.target.Start
		}
		return a.target.Start < b.target.Start
	})
	flat := make([]candidate, 0, len(candidates))
	for _, c := range candidates {
		overlaps := false
		for _, chosen := range flat {
			if c.path == chosen.path && c.target.Start < chosen.target.End && chosen.target.Start < c.target.End {
				overlaps = true
				break
			}
		}
		if !overlaps {
			flat = append(flat, c)
		}
	}
	candidates = flat
	sort.SliceStable(candidates, func(i, j int) bool {
		a, b := candidates[i], candidates[j]
		if a.priority != b.priority {
			return a.priority < b.priority
		}
		if a.path != b.path {
			return a.path < b.path
		}
		if a.target.Start != b.target.Start {
			return a.target.Start < b.target.Start
		}
		return a.target.End < b.target.End
	})
	result := map[string][]RepairTarget{}
	remaining, count := maxRepairTargetWireBytes-2, 0
	for _, c := range candidates {
		b, _ := json.Marshal(c.target)
		cost := len(b) + 1
		if len(result[c.path]) == 0 {
			key, _ := json.Marshal(c.path)
			cost += len(key) + 4
		}
		if cost > remaining {
			continue
		}
		if count == maxRepairTargets {
			break
		}
		result[c.path] = append(result[c.path], c.target)
		remaining -= cost
		count++
	}
	if count == 0 {
		return nil, errors.New("repair target catalog does not fit its bounds")
	}
	return result, nil
}

func requiredRepairPaths(task Task) []string {
	if task.RepairTargetVersion != RepairTargetVersion {
		return nil
	}
	var paths []string
	for _, path := range task.AbsentFiles {
		if _, historical := task.RepairDrafts[path]; historical {
			paths = append(paths, path)
		}
	}
	sort.Strings(paths)
	return paths
}

func validateRepairTargetCompleteness(task Task, result Result) error {
	seen := map[string]bool{}
	for _, change := range result.Changes {
		seen[change.Path] = true
	}
	for _, path := range requiredRepairPaths(task) {
		if !seen[path] {
			return errors.New("proposal omits a missing historical companion; explicitly restore every repair_required_paths entry")
		}
	}
	return nil
}
