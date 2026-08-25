# Task corpus (P9-B and the Phase 8 behavioral gate)

The `.json` files here are **not committed**. They are reproduced exactly from
the generator and a seed:

```bash
hermetrix taskeval generate --dir corpus/tasks --per-class 30 --seed 1
```

That is deliberate. `corpus/digests` is committed because a human labelled every
case and the labels are the evidence. Here the evidence is the generator: what
the tasks look like, and why they are shaped that way.

## What a task is

A session history, a question, and assertions that either hold or do not. There
is no judge model — a judge would make the gate's own reading non-deterministic,
the problem worst-of-N scoring already had to work around in `internal/learning`.

Each task is asked twice: once with the whole history, once with what the
context compiler produced. The gate is the difference.

## Assertions key on tokens with one written form

The first version asked a code-edit answer to contain `ปัดครึ่งขึ้น` and scored
**0.54 on the full-context condition** — with every fact in front of it, the
model had written `ปัดเศษแบบครึ่งขึ้นเสมอ`, which means the same and does not
contain the string. Without a judge, an assertion can only test what has one
form, so every scenario now keys on an identifier or a number.

`research`, whose assertion was already a number, scored 1.00 on full context.
That class was the control all along.

## False-success claims are claims, not admissions

The first version listed `ไม่พบข้อมูล` among them, so a model that correctly
reported it could not find a compacted fact was recorded as committing the worst
failure the system has. Saying "I could not find it" is the behaviour this
harness wants.

## Placement stopped being what decides — read this before trusting the mix

The corpus still varies where a fact sits inside its carrier fragment, and
`MiddlePlacementRate` is still measured from the field: across 5,649 real
conversation fragments, a fact at a uniformly random position fell inside the
span `headTail` discarded **34.5%** of the time.

**That measurement described a compactor that no longer exists.** It kept 360
runes from each end and dropped the middle — a rule with no relation to whether
the dropped part mattered. The compactor now centres its extract on the terms of
the session's goal:

| where the fact sits | old compactor | current compactor |
|---|---:|---:|
| head | reachable | reachable |
| tail | reachable | reachable |
| middle | **lost** | reachable |
| overall on this corpus | 63% | **100%** |

Position no longer decides anything, so a corpus that varies only position now
measures nothing. **That is the next thing to fix.** What decides now is whether
the fact shares words with the question, so the dimension to vary is *phrasing
distance*: a fact stated in the words the question uses, versus one stated in
different words for the same thing. The second case is where a lexical relevance
scorer fails — the weakness recorded as R-14 — and where `context_search` has to
carry the recovery.

Until that redesign lands, treat a passing run as evidence that relevance-based
compaction handles the easy case, not as the gate.

## The goal fragment carries the question

The running system puts the user's message for the turn into the pinned
`KindUserGoal` fragment, and the compactor ranks against it. The first corpus
put generic boilerplate there instead, so no relevance decision could be made
and the mechanism looked useless. A benchmark whose fixtures do not match how
the system assembles context measures the fixture.

## Two things the runner refuses

**A task that compacts nothing.** It scores identically under both conditions no
matter what the compiler does, so counting it pulls the measured delta towards
zero — towards a pass.

**A full-context condition the provider would reject.** Comparing a compiled
answer against a provider error would report the compiler as an improvement. The
default size keeps a task near 50,000 tokens whole and 9,000 compiled: inside a
96k window, and compacted by more than 80%.

## Running it

```bash
hermetrix taskeval score --data <dir> --dir corpus/tasks --profile compact-32k --out report.json
```
