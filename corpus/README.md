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

## Reading it

    hermetrix corpus score --data DIR --dir corpus/digests --repeats 3

`--repeats` matters. The reviewer is not deterministic: temperature is already
zero, and five readings of twelve cases changed three of them, every one a
positive, while negatives never moved. Three scorings of these hundred cases
returned 31, 34 and 30 proposals -- recall 0.55 to 0.62 against a floor of 0.60.
One reading cannot settle a threshold the reviewer's own variance straddles, so
the gate reports the worst of N.
