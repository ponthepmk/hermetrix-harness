# P8-A — digest corpus

A hundred cases behind the Phase 8 semantic-reviewer gate: at least 60% of
digests holding a real procedure produce a candidate that passes checks, at
most 10% false proposals, and no proposal citing evidence it was never given.

## Where these came from

Driven, not harvested. The first attempt exported 127 digests from real use and
every one was unusable: a single trigger family, every tool receipt read-only,
not one user correction. Nothing had been done, only explained. The four trigger
families are conditions on what happened in a turn, so they had to be caused:

| family | how it was caused |
|---|---|
| `successful_milestone` | a fix performed and written to a file, with approval |
| `repeated_correction` | the same point corrected twice in one session |
| `explicit_learn` | the user asking, in so many words, to keep a rule |
| `skill_failure` | a Skill active and the turn running out of its tool-call budget |

The work is real: the bugs fixed are genuine defects in the workspace files --
`order_total` dividing satang integers into floats against its own docstring,
`summarise` dividing by `len(orders)` with no empty guard.

## How the hundred were chosen

A hundred and eighty-two milestone digests collapsed to twelve distinct
*shapes* -- which tools ran, whether the user pushed back, whether a Skill was
in play -- and one shape held a hundred and seven of them. That is one case
repeated. Taken whole it would put most of the corpus on a single easy negative
and make the false-proposal ceiling trivially satisfiable.

Selection draws round-robin across each family's shapes and caps the family:

    hermetrix corpus export --data DIR --out corpus/digests --labeller NAME --per-family 25

That yields exactly a hundred cases across four families and thirty-one shapes.

## Labels

Labels are the judgement the gate's denominator rests on. They were assigned
from the digests alone, deliberately blind to what the reviewer decided --
reading its answers first would make the gate measure the reviewer against
itself.

Cases resting on an open question carry `ASSUMPTION:constraint-counts` in the
rationale: whether a rule the user asked to keep, with no steps performed, is a
Skill. The reviewer's position is that it is not. Twenty-five cases rest on it
and a single grep flips them.

Labelled 55 positive, 45 negative.

## Audit

Twenty cases, drawn evenly across the four families, were read back by the
project owner. Agreement was twenty of twenty. The threshold agreed in advance
was that disagreement above twenty percent would stop the labelling and reopen
the standard; it did not.

The sample included cases resting on `ASSUMPTION:constraint-counts`, so agreeing
to it settles that question: a rule the user asked to keep is a Skill even
when no steps were performed. The reviewer's instruction currently disagrees,
and that disagreement is now a defect in the instruction rather than an open
question.

## Reading it

    hermetrix corpus score --data DIR --dir corpus/digests --repeats 3

`--repeats` matters. The reviewer is not deterministic: temperature is already
zero, and five readings of twelve cases changed three of them, every one a
positive, while negatives never moved. Three scorings of these hundred cases
returned 31, 34 and 30 proposals -- recall 0.55 to 0.62 against a floor of 0.60.
One reading cannot settle a threshold the reviewer's own variance straddles, so
the gate reports the worst of N.

## Result

Read twice against the live gateway, judged on the worse reading:

```
100 cases · positives 55 · proposed 52 · recall 0.94   floor 0.60   pass
false proposals 4 / 45 = 0.09                          ceiling 0.10 pass
invented evidence 0                                    no tolerance pass
reviewer errors 0
recall ranged 0.94 to 0.96 · 5 cases did not answer the same way twice
```

The reviewer's instruction was revised once on this corpus and measured once.
Before and after, by family:

| family | recall before | recall after | false |
|---|---:|---:|---:|
| explicit_learn | 0.64 | 1.00 | 0 |
| repeated_correction | 0.56 | 0.96 | 0 |
| successful_milestone | 0.20 | 0.60 | 2 |
| skill_failure | — (no positives) | — | 2 |

It moved exactly where it was aimed. It also moved past where it was aimed:
false proposals went from none to four, which is 0.089 against a ceiling of
0.100. One more would fail the gate. The margin on precision is one case.

All four are the same shape from two families -- evidence that *describes*
steps without performing them. Two are questions about a domain procedure
("explain the steps for verifying a full-form tax invoice"), which the
instruction explicitly tells the reviewer to decline. Two are the failed
exploration in `skill_failure`, where the digest carries a goal, some reads and
an outcome of failure, and no method that worked.

That is the mirror image of the failure the revision fixed. The corpus is now
the record of a reviewer that is slightly too eager rather than slightly too
strict, and the number that says so is one case from failing.
