# ADR-003: Fresh ranked source discovery with bounded hashing

Date: 2026-10-23 · Status: Accepted

Context: Goal planning/selection used lexical 512/2000-file prefixes. A relevant
late source file could be absent from context; a writable pin was later sorted
behind filler files and lost during byte fitting. Planner hashing read candidate
contents before fitting the prompt, with a database/root lookup for every file.
The existing Project Brain retrieves curated external knowledge and cannot prove
the current local filesystem. No hard repository-size/latency target was supplied.

Decision: Rank a fresh metadata walk against requirements/step/check paths using
a bounded heap, retain exact owner-writable pins first, then fit serialized entries
to both wire and selected-preset input ceilings. Hash only fitted planner files
under one canonical-root binding and an 8 MiB total read budget. Preserve explicit
truncation/hash-availability flags. Do not add a second persistent source index or
a TTL cache whose freshness cannot yet be established after external edits.

Consequences: Relevant late paths and writable pins survive the measured fixtures;
source reads and per-file database lookups drop. Snapshot hashes remain fresh and
unknown hashes are omitted. File contents do not participate in lexical ranking,
so this is not dependency/semantic search. Walk callbacks stop after 100000 entries;
`filepath.WalkDir` still enumerates each directory in memory, so huge flat-directory
memory and million-file throughput are not qualified. A future source index needs
explicit freshness, root/inode identity, invalidation and measured load acceptance.
Filesystem changes still invalidate snapshots at apply; this design does not
grant additional write paths or weaken evidence/completion gates.
