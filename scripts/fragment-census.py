#!/usr/bin/env python3
"""fragment-census.py -- what kinds of context fragment does the running system
actually produce?

The Phase 9 gate reads "retention of goal/constraint/decision = 100%". Two of
those three subjects turned out never to be emitted at all. This script is how
that was established, and how to re-establish it after the producers are built.

Usage:  scripts/fragment-census.py <path-to-hermetrix.db>

It reads compiled_json (what survived) and report_json (what was dropped), so a
kind that is emitted and then discarded still shows up. A kind reported as
absent here is absent from the pipeline, not merely from the output.
"""
import collections
import json
import sqlite3
import sys

WATCHED = ("user_goal", "acceptance_criteria", "decision", "open_task", "artifact_receipt")


def main(path: str) -> int:
    db = sqlite3.connect(path)
    kinds = collections.Counter()
    per_snapshot = []
    dropped_prefixes = collections.Counter()
    pairs_seen = half_pairs = 0

    for compiled_json, report_json in db.execute(
        "SELECT compiled_json, report_json FROM context_snapshots"
    ):
        try:
            compiled = json.loads(compiled_json)
        except (TypeError, ValueError):
            continue
        fragments = compiled if isinstance(compiled, list) else (compiled.get("fragments") or [])
        counts = collections.Counter(f.get("kind") for f in fragments if isinstance(f, dict))
        kinds.update(counts)
        per_snapshot.append(counts)

        by_pair = collections.defaultdict(list)
        for fragment in fragments:
            if isinstance(fragment, dict) and fragment.get("pair_id"):
                by_pair[fragment["pair_id"]].append(fragment.get("kind"))
        pairs_seen += len(by_pair)
        half_pairs += sum(1 for members in by_pair.values() if len(set(members)) < 2)

        try:
            report = json.loads(report_json)
        except (TypeError, ValueError):
            continue
        for dropped in report.get("dropped_ids") or []:
            dropped_prefixes[str(dropped).split(":")[0]] += 1

    if not per_snapshot:
        print(f"no context snapshots in {path}")
        return 1

    print(f"snapshots: {len(per_snapshot)}")
    print("kinds produced:", dict(kinds.most_common()))
    print("dropped id prefixes:", dict(dropped_prefixes.most_common(8)))
    print(f"causal pairs: {pairs_seen} seen, {half_pairs} split")
    print()
    absent = []
    for kind in WATCHED:
        values = sorted(counts[kind] for counts in per_snapshot)
        percentile = lambda q: values[min(len(values) - 1, int(q * len(values)))]
        print(f"  {kind:22s} median={percentile(0.5):3d} p90={percentile(0.9):3d} "
              f"p99={percentile(0.99):3d} max={values[-1]:3d}")
        if values[-1] == 0:
            absent.append(kind)
    if absent:
        print()
        print("NEVER PRODUCED: " + ", ".join(absent))
        print("A retention gate over these kinds measures nothing until something emits them.")
    return 0


if __name__ == "__main__":
    if len(sys.argv) != 2:
        print(__doc__.strip())
        raise SystemExit(2)
    raise SystemExit(main(sys.argv[1]))
