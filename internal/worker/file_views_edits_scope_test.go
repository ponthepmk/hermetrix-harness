package worker

import (
	"testing"
	"unicode/utf8"
)

func TestMaterializeBoundedChangeScopeSplit(t *testing.T) {
	var cases []struct {
		id          string
		source      string
		sourceBytes []byte
		ranges      []struct {
			start int
			end   int
		}
		old []byte
		new string
	}
	cases = append(cases, struct {
		id          string
		source      string
		sourceBytes []byte
		ranges      []struct {
			start int
			end   int
		}
		old []byte
		new string
	}{
		id:     "globally_ambiguous",
		source: "one|one",
		ranges: []struct {
			start int
			end   int
		}{{0, 3}},
		old: []byte("one"),
		new: "x",
	})
	cases = append(cases, struct {
		id          string
		source      string
		sourceBytes []byte
		ranges      []struct {
			start int
			end   int
		}
		old []byte
		new string
	}{
		id:     "overlapping_occurrences",
		source: "aaaa",
		ranges: []struct {
			start int
			end   int
		}{{0, 4}},
		old: []byte("aaa"),
		new: "x",
	})
	cases = append(cases, struct {
		id          string
		source      string
		sourceBytes []byte
		ranges      []struct {
			start int
			end   int
		}
		old []byte
		new string
	}{
		id:     "hidden_only_match",
		source: "hidden|visible",
		ranges: []struct {
			start int
			end   int
		}{{7, 14}},
		old: []byte("hidden"),
		new: "x",
	})
	cases = append(cases, struct {
		id          string
		source      string
		sourceBytes []byte
		ranges      []struct {
			start int
			end   int
		}
		old []byte
		new string
	}{
		id:     "cross_adjacent_views",
		source: "alphaBeta",
		ranges: []struct {
			start int
			end   int
		}{{0, 5}, {5, 9}},
		old: []byte("haBe"),
		new: "x",
	})
	cases = append(cases, struct {
		id          string
		source      string
		sourceBytes []byte
		ranges      []struct {
			start int
			end   int
		}
		old []byte
		new string
	}{
		id:          "split_crlf_end",
		source:      string([]byte{97, 13, 10, 98}),
		sourceBytes: []byte{97, 13, 10, 98},
		ranges: []struct {
			start int
			end   int
		}{{0, 4}},
		old: []byte{13},
		new: "x",
	})
	cases = append(cases, struct {
		id          string
		source      string
		sourceBytes []byte
		ranges      []struct {
			start int
			end   int
		}
		old []byte
		new string
	}{
		id:          "split_crlf_start",
		source:      string([]byte{97, 13, 10, 98}),
		sourceBytes: []byte{97, 13, 10, 98},
		ranges: []struct {
			start int
			end   int
		}{{0, 4}},
		old: []byte{10},
		new: "x",
	})
	for _, c := range cases {
		t.Run(c.id, func(t *testing.T) {
			fullSource := c.source
			if c.sourceBytes != nil {
				fullSource = string(c.sourceBytes)
			}
			fullSHA := Hash(fullSource)
			var views []BoundedFileView
			for _, r := range c.ranges {
				view, err := NewBoundedFileView("test", fullSource, fullSHA, r.start, r.end)
				if err != nil {
					t.Fatalf("NewBoundedFileView %s: %v", c.id, err)
				}
				views = append(views, view)
			}
			edits := []TextEdit{{Old: string(c.old), New: c.new}}
			full, strategy, err := MaterializeBoundedChange("test", fullSource, fullSHA, views, nil, edits)
			if err == nil {
				t.Fatalf("MaterializeBoundedChange %s: expected error, got full=%q strategy=%q", c.id, full, strategy)
			}
			if full != "" {
				t.Fatalf("MaterializeBoundedChange %s: expected empty full, got %q", c.id, full)
			}
			if strategy != "" {
				t.Fatalf("MaterializeBoundedChange %s: expected empty strategy, got %q", c.id, strategy)
			}
			if !utf8.ValidString(fullSource) {
				t.Fatalf("MaterializeBoundedChange %s: expected valid UTF-8 source", c.id)
			}
			if len(views) != len(c.ranges) {
				t.Fatalf("MaterializeBoundedChange %s: expected %d views, got %d", c.id, len(c.ranges), len(views))
			}
			for i, v := range views {
				if v.Path != "test" {
					t.Fatalf("MaterializeBoundedChange %s: view %d path mismatch", c.id, i)
				}
				if v.FullSHA256 != fullSHA {
					t.Fatalf("MaterializeBoundedChange %s: view %d sha mismatch", c.id, i)
				}
				if v.Start != c.ranges[i].start || v.End != c.ranges[i].end {
					t.Fatalf("MaterializeBoundedChange %s: view %d range mismatch", c.id, i)
				}
				if v.Content != fullSource[c.ranges[i].start:c.ranges[i].end] {
					t.Fatalf("MaterializeBoundedChange %s: view %d content mismatch", c.id, i)
				}
				if v.RegionID == "" {
					t.Fatalf("MaterializeBoundedChange %s: view %d empty region id", c.id, i)
				}
			}
			if Hash(fullSource) == "" {
				t.Fatalf("MaterializeBoundedChange %s: expected non-empty hash", c.id)
			}
			if Hash(fullSource) != fullSHA {
				t.Fatalf("MaterializeBoundedChange %s: hash mismatch", c.id)
			}
		})
	}
}
