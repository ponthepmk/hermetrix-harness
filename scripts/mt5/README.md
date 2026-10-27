# MT5 compile/backtest adapter

Source implementation; actual MetaTrader qualification is pending. MT4 is not
implemented. This adapter runs one deterministic test on a **dedicated portable
MT5 installation on Windows**, already configured by its owner with the required
history/account access. It does not install MT5 or manage account credentials.

Copy `cli.mjs` and `runner.mjs` into the EA project's `scripts/mt5/` directory.
The project should already contain the EA source and its declared dependencies.
Create `mt5.local.json` in that project's root, adapting this example explicitly
to the desired experiment. These are sample test inputs, not recommended targets:

```json
{
  "terminal_root": "D:/MT5-Backtest",
  "dedicated_terminal": true,
  "source": "EA.mq5",
  "inputs": ["lib.mqh"],
  "run_revision": "dataset-version-1",
  "timeout_seconds": 90,
  "tester": {
    "symbol": "EURUSD",
    "period": "H1",
    "from": "2025.01.01",
    "to": "2026.01.01",
    "deposit": 10000,
    "currency": "USD",
    "leverage": 100,
    "model": 4
  },
  "criteria": {
    "min_trades": 80,
    "min_profit_factor": 1.3,
    "max_equity_drawdown_pct": 10,
    "min_net_profit": 0
  }
}
```

Run/freeze the exact acceptance command:

```powershell
node scripts/mt5/cli.mjs run mt5.local.json
```

Exit 0 means all declared report thresholds passed. Any missing report,
unsupported metric, compile error/warning, timeout or unmet threshold returns
nonzero. The stdout JSON includes metrics, failures and evidence paths; the goal
runner's existing job log makes this available as failed-check evidence.

The adapter snapshots up to 32 declared input files (2 MiB each), records their
SHA-256s, compiles the snapshot, installs the resulting EX5 into a unique
`MQL5/Experts/Hermetrix/{operation}` folder and writes a fresh report to
`Reports/Hermetrix/{operation}/report.htm`. Evidence is retained under
`.mt5-runs/{operation}/` in the project, including source snapshot, compile log,
tester configuration, report and terminal receipt. Acceptance requires one
English error/warning summary, new nonempty EX5 bytes and one unambiguous English
report value for every required metric. Localized reports and ambiguous decimal
formats fail pending a separately qualified parser.
Report expert, symbol, timeframe and exact date range must also match the request;
a profitable report for another experiment cannot pass the goal.

The generated configuration runs the Strategy Tester, disables live automated
trading/DLL import/cloud and remote tester agents, selects a fixed date/symbol/
tick model, and requests shutdown on test completion. This does not replace OS
isolation or inspect arbitrary EA source for every possible side effect. Use a
dedicated terminal with the intended testing data. Compilation includes standard
headers from that terminal's `MQL5` folder; declare all project includes/resources
in `inputs` so the snapshot is complete. Custom `.set` parameters, optimization
and forward reports are not supported in this initial adapter.

The operation digest binds configuration and declared source inputs. Identical
inputs read the saved receipt, verify saved report/binary hashes and reevaluate
report metrics without executing again. Change `run_revision` when deliberately
testing a new history/environment snapshot. If interruption leaves no terminal
receipt, replay is refused and `.hermetrix-mt5.lock` is retained. Inspect the
exact terminal process, files and reports first; only its owner should clear the
stale lock and authorize a new run revision after determining the outcome.
The adapter never stops a pre-existing terminal process. A terminal instance
already using the installation may prevent a new tester run, which must fail
rather than reuse an old report.

Compile plus backtest accepts 1–86,380 seconds. For a longer experiment, freeze
the goal runner's `verification_timeout_seconds` at least 20 seconds above this
adapter timeout and give the total `max_seconds` budget enough time for planning,
compilation, testing and review. For example, adapter `timeout_seconds: 1800`,
runner `verification_timeout_seconds: 1820` and `max_seconds: 3600`.
These are example budgets, not measured MT5 performance targets. The source goal
runner dispatches a durable job, releases its worker, observes the existing job,
renews the run lease and cancels its process tree on Stop/deadline. At most two
verification commands run concurrently; others wait without consuming attempts.
Interrupted or unknown operations still require inspection instead of relaunch.
Ordinary manual VerifyFrozen calls retain their 120-second limit. Actual MT5
long-run qualification and optimization/forward-report support remain pending.

Remote operation uses the harness running on the target Windows host and its
authenticated HTTPS API; see [goal runner control](../../docs/GOAL-RUNNER.md).
No target has been connected and no real EA result has been claimed.

Protocol references: [MetaEditor external compilation](https://www.metatrader5.com/en/metaeditor/help/beginning/integration_ide)
and [MT5 startup/tester configuration](https://www.metatrader5.com/en/terminal/help/start_advanced/start).
The implementation sends literal argv through native process APIs, without a shell.
