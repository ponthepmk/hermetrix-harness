package product

// orderManifestPrefixGroups groups ranked manifest files by the index of the
// longest matching directory prefix from manifestPrefixMatch, preserving the
// original rank order within each group. It emits one entry from each nonempty
// group in prefix order per round until all groups are empty, then appends any
// unmatched entries in their original order. Entries with score >= 10000 are
// treated as existing exact query path matches and are emitted before prefix
// groups, preserving their ranked order. The input slice is not modified and
// the result is always a new backing slice, even when prefixes is empty.
func orderManifestPrefixGroups(ranked []rankedManifestFile, prefixes []string) []rankedManifestFile {
	if len(ranked) == 0 {
		return []rankedManifestFile{}
	}

	// Partition direct exact-query matches (score >= 10000) before grouping.
	direct := []rankedManifestFile{}
	rest := []rankedManifestFile{}
	for _, entry := range ranked {
		if entry.score >= 10000 {
			direct = append(direct, entry)
		} else {
			rest = append(rest, entry)
		}
	}

	// Group indices by prefix match, preserving original rank order within each group.
	groups := make([][]rankedManifestFile, len(prefixes))
	unmatched := []rankedManifestFile{}

	for _, entry := range rest {
		idx := manifestPrefixMatch(entry.file.Path, prefixes)
		if idx >= 0 {
			groups[idx] = append(groups[idx], entry)
		} else {
			unmatched = append(unmatched, entry)
		}
	}

	// Emit one entry from each nonempty group in prefix order per round.
	result := []rankedManifestFile{}
	for len(groups) > 0 {
		anyEmit := false
		for i := range groups {
			if len(groups[i]) > 0 {
				result = append(result, groups[i][0])
				groups[i] = groups[i][1:]
				anyEmit = true
			}
		}
		if !anyEmit {
			break
		}
	}

	// Append unmatched entries in original order.
	result = append(result, unmatched...)

	// Emit direct exact-query matches before prefix groups and unmatched entries.
	return append(direct, result...)
}
