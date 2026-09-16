#!/usr/bin/env bash
# seed-starter-skills.sh -- create two starter Skill candidates over the API.
# Usage: ./scripts/seed-starter-skills.sh [base-url]   (default http://127.0.0.1:7331)
# Candidate-only: nothing becomes active until a human promotes it in Skill Studio.
# Safe to re-run: names that already exist (skill or candidate) are skipped.
set -euo pipefail

BASE="${1:-http://127.0.0.1:7331}"

exists() {
  local name="$1"
  curl -sf --max-time 5 "$BASE/api/skills" | grep -q "\"canonical_name\":\"$name\"" && return 0
  curl -sf --max-time 5 "$BASE/api/candidates" | grep -q "\"canonical_name\":\"$name\"" && return 0
  return 1
}

post_candidate() {
  local name="$1" reason="$2" file="$3"
  python3 - "$BASE" "$name" "$reason" "$file" <<'EOF'
import json, sys, urllib.request
base, name, reason, path = sys.argv[1:5]
with open(path, encoding="utf-8") as f:
    markdown = f.read()
body = json.dumps({
    "canonical_name": name,
    "scope_kind": "user",
    "scope_ref": "",
    "reason": reason,
    "evidence_refs": ["seed:starter-skills"],
    "markdown": markdown,
}).encode()
req = urllib.request.Request(base + "/api/skills/custom", data=body,
                             headers={"Content-Type": "application/json"})
with urllib.request.urlopen(req, timeout=10) as r:
    print(name, "->", r.status, r.read()[:80].decode("utf-8", "replace"))
EOF
}

DIR="$(mktemp -d)"
trap 'rm -rf "$DIR"' EXIT

cat > "$DIR/frontend-design.md" <<'MD'
# frontend-design

Production-grade UI direction for anything user-facing. Use when building web
components, pages, dashboards, or styling any web UI.

## Avoid generic AI aesthetics

- No Inter/Roboto/system-font default; pick a typeface that fits the subject.
- No purple-gradient-on-white, no numbered `01/02/03` markers unless the
  content is a real sequence.
- No grid-of-identical-cards as the first idea. Vary layout per context.

## Process

1. State the aesthetic direction in one line (e.g. "quiet editorial, warm
   paper, serif display") before writing code.
2. Tokens first: palette (≤5), spacing scale, type scale. Then components.
3. Motion only where it serves the subject: one orchestrated moment beats
   scattered effects. Minimal directions need restraint and precise spacing.
4. Match complexity to the vision: maximalist needs elaborate execution,
   minimal needs precision.

## Check before done

- Typography, spacing, and palette trace back to the direction line.
- No element exists only because "landing pages have those".
MD

cat > "$DIR/project-guide.md" <<'MD'
# project-guide (Hermetrix-harness)

How to run and verify this harness. Use when working inside the
Hermetrix-harness project itself.

## Everyday commands (Makefile)

- `make run` — serve on 127.0.0.1:7331 with `./.hermetrix`
- `make smoke` — read-only liveness check (health, bootstrap, providers, UI)
- `make test` — `go test ./...` + UI runtime tests + `node --check` + doc-truth
- `make backup` — timestamped SQLite backup into `backups/`

## First-run order

1. Models screen: connect an OpenAI-compatible endpoint (key takes effect
   immediately, stored owner-only in `secrets.json`).
2. Chat: New session, compact-32k envelope first.
3. Skill Studio: promote one candidate before relying on `skill_search`.

## Docs that matter

- `docs/HANDOVER.md` — start here on a new machine
- `docs/FUTURE-ARCHITECTURE-PLAN.md` — forward source of truth
- Runtime beats docs: when they disagree, the runtime is the truth.

## Sandbox lesson (measured 2026-09-09)

- Agent background jobs run under Seatbelt (macOS): loopback is denied, so
  `go test` suites needing localhost FAIL there with exit 1. Run loopback
  tests in the Terminal pane, not via agent jobs.
- Vague repo-wide asks burn the 12 model-step budget. Ask one narrow thing
  per turn instead.
MD

if exists "frontend-design"; then
  echo "skip frontend-design (already present)"
else
  post_candidate "frontend-design" "Starter UI-direction skill so generated interfaces stop looking generic." "$DIR/frontend-design.md"
fi

if exists "project-guide"; then
  echo "skip project-guide (already present)"
else
  post_candidate "project-guide" "Starter operator skill for running and verifying this harness." "$DIR/project-guide.md"
fi

echo "seed: done — review and promote in Skill Studio."
