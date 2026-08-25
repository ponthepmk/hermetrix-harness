# Task corpus (P9-B and the Phase 8 behavioral gate)

This directory holds the generated task corpus. The `.json` files are **not
committed** — they are reproduced exactly from the generator and a seed:

```bash
hermetrix taskeval generate --dir corpus/tasks --per-class 30 --seed 1
```

That is deliberate. `corpus/digests` is committed because a human labelled every
case and the labels are the evidence. Here the evidence is the generator: what
the tasks look like, and why the placement mix is what it is.

## What a task is

Each task carries a session history, a question, and assertions that either hold
or do not. There is no judge model — a judge would make the gate's own reading
non-deterministic, which is the problem worst-of-N scoring already had to work
around in `internal/learning`.

Every task is asked twice: once with the whole history, once with what the
context compiler produced for a profile. The gate is the difference.

## Why the placement mix is 34.5% and not a round number

A task only measures anything if its answer is at risk. Measured on
compact-32k, with the priorities `compileTurn` really assigns, a planted fact
behaves in exactly three ways:

| where the fact sits | what compaction does |
|---|---|
| a short fragment | kept verbatim — `selectWithin` keeps scanning past a fragment that does not fit, so small ones still land |
| head or tail of a long fragment | survives as summary text; the checkpoint extract keeps both ends |
| **middle of a long fragment** | **gone at every size tried** — `headTail` keeps 360 runes of head and tail and drops what is between |

So the corpus's answer depends entirely on how often it hides facts in that
middle. That number is measured rather than chosen: across 5,649 real
conversation fragments in the driven corpus, a fact at a uniformly random
position lands in the discarded span **34.5%** of the time — median length 431
runes against a 360-rune cap, with 58.2% of fragments over it. Weighted across
every kind the field produces it is 14.4%; conversation is higher because it is
the longest thing a session writes, and a stated decision lives in conversation.

Re-measure with `scripts/fragment-census.py` before changing
`MiddlePlacementRate`. Picking the number that makes the gate pass would make
the gate meaningless.

The corpus deliberately contains tasks that *should* survive compaction as well
as tasks that should not. A corpus made only of losable tasks would condemn a
compiler that is behaving correctly.

## Two things the runner refuses

**A task that compacts nothing.** It scores identically under both conditions no
matter what the compiler does, so counting it pulls the measured delta towards
zero — towards a pass.

**A full-context condition the provider would reject.** Comparing a compiled
answer against a provider error would report the compiler as an improvement.
The default size keeps a task near 50,000 tokens whole and 9,000 compiled: inside
a 96k window, and compacted by more than 80%.

## Running it

```bash
hermetrix taskeval score --data <dir> --dir corpus/tasks --profile compact-32k --out report.json
```
