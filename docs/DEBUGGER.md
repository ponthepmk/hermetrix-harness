# Local code debugger

Hermetrix can launch saved **Go** programs through Delve DAP and saved **JavaScript** programs through Node Inspector. These are actual debugger connections: breakpoints stop the program, the stack identifies the current source location, and the Variables view reads the paused program's local values. Running a shell command is a separate feature.

## Windows setup

Node.js must be on the server's `PATH` for JavaScript. Go and Delve must be available for Go. Restart Hermetrix after changing its environment.

To install Delve inside this repository from PowerShell:

```powershell
$env:GOBIN = Join-Path (Get-Location) '.hermetrix-tools'
go install github.com/go-delve/delve/cmd/dlv@v1.27.2
```

The `.hermetrix-tools` directory is ignored by Git. Hermetrix finds Delve on `PATH`, in `.hermetrix-tools` next to the Hermetrix executable or its working directory, or at an explicitly configured absolute path:

```powershell
$env:HERMETRIX_DLV_PATH = 'D:\Projects\harness\hermetrix-harness\.hermetrix-tools\dlv.exe'
```

Delve **v1.27.2** and the installed Node.js were tested on native Windows with real temporary programs. Capability detection checks whether tools are present; the actual launch also checks whether the program builds and the debugger can start. An unsupported compiler/debugger combination or missing module dependency appears as a launch error with compiler details.

## Usage

1. Save the source file in a saved project.
2. Choose Go or JavaScript and the project-relative program. Go accepts a `.go` file or package directory such as `.` or `cmd/server`. JavaScript accepts `.js`, `.cjs`, and `.mjs` files.
3. Add a breakpoint using a project-relative source path and a one-based line number. A verified breakpoint is one resolved by the actual debugger; requested breakpoints are not automatically marked verified.
4. Start debugging. The program initially pauses at entry. Go may initially stop in runtime code, in which case the project stack remains empty until execution reaches project code.
5. Continue to a breakpoint, inspect the stack and local variables, or step over, into, or out of a function. Pause interrupts a running program; Stop terminates the launched process tree.

Code changes apply on the next launch. The debugger does not save an unsaved editor buffer or replace a running program.

## Current limits

- Sessions are local and temporary. They end when Hermetrix stops and are not automatically resumed after restart.
- At most four sessions can run simultaneously. The default deadline is 30 minutes, configurable from one second to one hour through the API. Finished history retains at most 32 sessions.
- Output retains its most recent 128 KiB and reports truncation. Each pause exposes up to 32 project stack frames and up to 100 local variables per inspected frame. Long variable values are truncated to 2 KiB.
- Variables are read only. There is no arbitrary expression evaluation, conditional breakpoint, remote attach, browser debugger, TypeScript source map, or expandable object tree support.
- Go's stack follows the stopped goroutine. There is no goroutine picker yet. Package launch uses debug mode, not `go test` mode.
- The debugger interface binds only to an ephemeral loopback port and is never returned to the browser. Browser requests use Hermetrix's normal authentication and project ownership checks. Program and breakpoint paths must resolve inside the chosen project.
- Project binding restricts the files selected by the debugger API; it is not an operating-system sandbox for the running program. Programs retain local process permissions and may write files or use the network. The environment is reduced and excludes inherited provider credentials. Debugging fails closed when `HERMETRIX_REQUIRE_OS_SANDBOX=1` is configured.
- Unexpected loss of the adapter connection stops the launched program. Nonzero program exits and launch/compiler errors remain failures, rather than appearing as successful checks.

## HTTP contract

All routes are under normal API authentication. No Inspector or DAP connection is exposed to the browser.

| Method | Route | Request or result |
| --- | --- | --- |
| GET | `/api/debug/capabilities` | Available runtimes and current limitations |
| POST | `/api/debug/sessions` | `{project_id,runtime,program,args,breakpoints,timeout_seconds?}`; returns a session |
| GET | `/api/debug/sessions?project_id=…` | Sessions for the owned project |
| GET | `/api/debug/sessions/{id}` | Current session, output, stack, and breakpoints |
| PUT | `/api/debug/sessions/{id}/breakpoints` | `{breakpoints:[{path,line}]}` replaces the breakpoint set |
| POST | `/api/debug/sessions/{id}/control` | `{action:"continue"\|"pause"\|"step_over"\|"step_in"\|"step_out"\|"stop"}` |
| GET | `/api/debug/sessions/{id}/variables?frame_id=…` | `[{name,type,value}]` for a current paused project frame |

Session states are `starting`, `running`, `paused`, `exited`, `stopped`, and `failed`. `stack` and `breakpoints` are arrays. Frames contain `{id,name,path,line,column}`; breakpoints contain `{id,path,line,verified}`. State changes arrive asynchronously, so callers poll after controls instead of assuming the returned state has already reached the next pause. Expiration, bounded output, process scope, and exit code are exposed in `expires_at`, `output_truncated`, `sandbox`, and `exit_code`.

## Verification

The product integration tests launch real Node and Delve processes in temporary projects. They verify requested breakpoints, local values `total=5` and `doubled=10`, all execution controls, program output, termination, timeout, bounded partial output, compile failure details, and nonzero exit status. The HTTP integration test reads `answer=42` from a real paused Node program. Negative tests cover authentication, foreign principals, escaping paths, unsupported operations, and strict sandbox mode. Tests requiring a runtime skip explicitly if that runtime is absent.

References: [Delve DAP lifecycle](https://github.com/go-delve/delve/blob/v1.27.2/Documentation/api/dap/README.md), [Node.js debugger](https://nodejs.org/api/debugger.html).
