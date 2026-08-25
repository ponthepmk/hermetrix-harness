---
name: verify-and-report
description: Use when investigating, measuring, testing, or reporting engineering work in this repo — before claiming something works, before claiming something is broken, and before writing up what was done. Covers how to establish a claim and how to report it so the reader can follow it.
---

# Verify, then report

Two halves. The first is how to earn a claim. The second is how to deliver it.

Every rule below was paid for by getting it wrong in this project. The examples
are load-bearing — a rule without its failure reads as generic advice and gets
skipped.

---

## Part 1 — Verifying

### Measure. Do not reason your way to a conclusion you could check.

Reading code tells you what it should do. Running it tells you what it does.

Five consecutive wrong conclusions about the token error band all came from
reasoning over code and arithmetic. Each was corrected by measuring. The last
one nearly blamed the provider for a 35% billing change that turned out to be
our own bug.

Before writing "X happens", ask: did I observe X, or derive it? If derived,
go and observe it. It is usually cheap.

### Before building an instrument, prove it can register anything.

The most expensive failure mode here is not a wrong measurement. It is a
measurement that can only ever return one value, which reads as a pass forever.

Found five separate times in this codebase:

| instrument | why it could never register |
|---|---|
| skill analyzer threshold | set above every value in real data |
| fidelity corpus | 32 tokens, fit whole into the smallest profile — nothing was ever dropped |
| CI build | a `.gitignore` entry swallowed `cmd/`, so `go build ./...` compiled an empty project |
| essential retention | pinned fragments are retained or the compile errors — 1.00 across a 23,000× input sweep, no middle value |
| decision retention | the gate's subject was never produced by anything outside a test fixture |

**The check:** sweep the input across its real range. If the number is constant,
you are grading the fixture, not the system. Do this *before* investing in a
corpus, not after.

### Check the subject exists before measuring it.

A gate over something nothing produces measures nothing, no matter how many
cases you write. Census what the running system actually emits —
`scripts/fragment-census.py` exists because of this — and read both what
survived and what was discarded, so a kind that is emitted and then dropped
still shows up.

### Check reachability, not just correctness.

Code can be correct, tested, mutation-proven, and still never execute.

I added a producer for `open_task` in this session. It was correct and its
mutation passed. It could never fire, because raising an approval parks the
session holding its turn lease, so no compile runs while one is outstanding.
I removed it.

**A producer that cannot fire is worse than none** — it makes the area look
covered. Prove reachability by driving the real flow, not by reading it.

### A finding closes only when disabling its guard turns a test red.

Write the test. Then remove the guard it supposedly protects, confirm the test
goes red, and restore. Report the mutation alongside the finding.

A green test proves nothing on its own: it may be watching a different thing
that happens to be fine. The mutation is the only evidence that the test is
attached to the guard.

### Guard the premise in the test itself.

A test that measures degradation must fail loudly when the fixture was never
under pressure, or it will keep passing after someone trims the fixture.

```go
if kept == 2 {
    t.Fatal("the pair was never at risk; the premise is broken, not the guarantee")
}
if run.Metrics.CompressionRatio >= 1 {
    t.Fatalf("nothing was compacted: ratio=%.4f", run.Metrics.CompressionRatio)
}
```

Same shape when asserting a negative: prove the positive case still works, or
the negative assertion holds for the wrong reason.

### When a test fails, check whether the premise is wrong before editing code.

Several times here the code was right and my test's assumption was wrong —
fragments deduplicated because I gave them identical content, an assertion that
`"the"` would be dropped by a filter that by design does not drop it. Changing
the code would have broken working behaviour to satisfy a bad test.

### Look for the guard before declaring one absent.

Before reporting "nothing prevents X", search for the thing that prevents X.
`grep` for the field name, the error string, the table. I called the
qualification ceiling missing without having read `CreateSession`, which
refuses outright when the profile exceeds the declared window.

---

## Part 2 — Reporting

### Say what the part is and what it does, before saying what is wrong with it.

Name the component, what it does for the user, then the finding. Without that,
"A works like this, and it's broken" is unfollowable — the reader has to
reconstruct the subject before they can judge the claim.

Order: **what it is → why it exists → what I found → what it means for you →
what to do.**

### Separate "found a problem" from "added a test".

These are different kinds of result and most work is the second. A list that
mixes them reads as N problems when there is one.

State it explicitly: *this part was already correct, I only added a test that
watches it* versus *this part has a defect*.

### Lead with what changes for the reader.

Test counts, claim-registry entries, commit counts are ceremony. They go last,
or in a footer. Open with the finding or the decision they now have to make.

### Explain mechanisms, not vocabulary.

`mutation`, `instrument`, `reachability`, `premise` are shorthand for ideas the
reader may not be holding. Either unpack them or do not use them. If asked to
clarify once, do not re-explain in the same register — rebuild from the ground.

### Verify before defending.

When pushed back on, go and look. Twice in this project the pushback was right
and I had not checked the thing I was making claims about. Treat disagreement as
a pointer to something unchecked, not as something to argue against.

### Do not inflate evidence.

Ask what a number actually counts before it carries an argument.

"103 sessions downgraded to a smaller context window" was presented as evidence
of user behaviour. It was a `try/except` fallback in a script I wrote myself,
run 103 times. The number was real; the claim built on it was not.

Test: what would have to be true for this number to mean what I am saying? Then
check that.

### Correct plainly, in place.

State the correction, give the right answer, continue. No apology paragraph, no
tallying of past errors, no re-litigating how it happened. Combine multiple
corrections rather than enumerating them.

Correct in the artefact too, not only in chat — the doc, the test comment, the
finding severity.

### Report failures as plainly as successes.

If a run failed, say so with the output. If something was skipped, say it was
skipped. If a drive produced nothing, say that instead of describing what it
would have produced.

### Reply in the user's language.

Technical terms, code, API names, CLI commands and exact error strings stay
verbatim.

---

## Quick checklist

Before claiming something works:
- [ ] I observed it, not derived it
- [ ] the instrument can return more than one value
- [ ] the subject exists in the running system
- [ ] the code path is reachable in the real flow
- [ ] removing the guard turns a test red
- [ ] the test fails when its premise is broken

Before sending a report:
- [ ] the reader knows what the component is before hearing it is broken
- [ ] problems are separated from tests-added
- [ ] every number means what I say it means
- [ ] failures and skips are stated, not implied
- [ ] ceremony is at the bottom
