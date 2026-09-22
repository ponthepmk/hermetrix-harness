# Decision engine, measured admission, and read-only selection

Hermetrix derives each recommendation from the exact durable task revision. `RuleDecision` is the deterministic baseline. `BonsaiDecision` first runs in shadow mode and can be admitted only after the fixed corpus passes. The admitted path receives read-only, policy-free candidates and returns a recommendation; it has no execution or task-mutation capability.

## API flow

1. Read `GET /api/tasks/{task_id}/next-action?revision=N` for the deterministic baseline.
2. Run `POST /api/tasks/{task_id}/decision-shadow` with JSON:

   ```json
   {
     "expected_task_revision": 2,
     "provider_id": "provider_local"
   }
   ```

3. Inspect `GET /api/tasks/{task_id}/decision-shadows?limit=100` for immutable receipts.
4. Inspect `GET /api/tasks/{task_id}/decision-shadow-metrics?provider_id=provider_local` for total, valid, agreement, invalid-rate and average-latency metrics.
5. Run `POST /api/decision/benchmarks` with `provider_id`. The corpus contains six boundary cases.
6. Inspect `GET /api/decision/benchmarks` and enable the exact passing provider revision through `PUT /api/decision/admission`.
7. Call `POST /api/tasks/{task_id}/decision-read-only` with the exact task revision. The response is a recommendation only.
8. Read `GET /api/tasks/{task_id}/progress?revision=N` for the receipt-derived attempt/escalation budget and `GET /api/tasks/{task_id}/knowledge?revision=N` for bounded deterministic retrieval.

The POST accepts an enabled loopback provider only. It rejects stale task revisions before inference and revalidates the provider revision, enabled state and credential readiness immediately before dispatch. The model must return exactly one `submit_decision` tool call whose `action_id` is in the request's candidate enum. Revision `bonsai-decision-v2` uses temperature 0, disables supported local reasoning transport, applies the deterministic safety precedence in the selector contract, and retains a 256-token output ceiling and 64 KiB input ceiling.

## Durable evidence

Schema 47 stores shadow receipts. Schema 48 adds immutable benchmark receipts and admission policies. Provider or response failures are stored as observations; they do not replace or invalidate the rule result.

## Admission boundary

Admission requires all six fixed cases, at least 80% accuracy, zero invalid responses and average latency no greater than 10 seconds. Enabling one provider disables any previously admitted provider. A provider revision change invalidates active use until the corpus is rerun. Test execution, planning, reconciliation, completion and every write or transition remain behind their existing deterministic policy gates.

The Task cockpit exposes shadow comparison, corpus results, admission state, loop budgets and retrieved active user memory. Retrieval is deterministic, project scoped, limited to eight matches and 1,200 characters per snippet. A verified task completion also queues a bounded learning digest with validation and review receipts; the existing reviewer and Skill admission gates still control whether any reusable knowledge is promoted.

The opt-in real-model check is:

```powershell
$env:HERMETRIX_RUN_BONSAI_DECISION_E2E='1'
go test -count=1 -run TestBonsaiDecisionAgainstLocalServer -v ./internal/taskcoord
```

It uses a temporary data root and expects an OpenAI-compatible Bonsai server on `127.0.0.1:8088`.
