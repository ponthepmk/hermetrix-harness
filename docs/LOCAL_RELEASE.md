# Local Hermetrix release (Windows, 2026-09-23)

The integrated local pilot is on `main`, with the implementation recorded in commit `c462d43`. It combines the validated H2A read-only Platform adapter with the task-first UI, editable IDE, debugger, Discord bridge, and the 16k local-model profile. It is not a Pi-managed-write release or a production Pi deployment.

## Start and use

On the prepared PC, first double-click `C:\Users\ZP2E0\AppData\Local\Hermetrix\Start Bonsai.cmd` if the model server is not already running. Wait for it to finish loading, then double-click `C:\Users\ZP2E0\AppData\Local\Hermetrix\Start Hermetrix.cmd`. The second launcher checks model health, uses the installed executable and data under the same directory, registers `D:\Projects\harness\hermetrix-harness` as the workspace, and opens the local desktop window. Keep both launcher windows open while using Hermetrix. If the desktop window does not open, visit `http://127.0.0.1:7331/` in a browser. The API and UI bind to loopback only.

The local llama-compatible model server must be running at `http://127.0.0.1:8088/v1`. The saved **Bonsai Local** profile uses its actual 16,384-token context and needs no API key. Choose **Compact 16k** for new sessions. Existing sessions retain their frozen model/context contract. The local demo project is `C:\Users\ZP2E0\AppData\Local\Hermetrix\demo-project`; use it to try Code, Save and Test without changing the main source tree.

For source builds, run `go test -p 1 -count=1 ./...`, `go vet ./...`, and build `./cmd/hermetrix`. Frontend sources are in `internal/web/vendor-src`; `npm ci` and `npm run build` regenerate embedded IDE assets. The installed executable is a copy of the build, not the repository's output.

## Verified scope

- The installed API reports schema 49. The local UI created a session and Bonsai returned the requested Thai reply. The isolated demo ran `node --test` through the IDE and passed four tests.
- The frontend test suite passed 94 tests; the full uncached Go suite and `go vet ./...` passed again from `main` after integration. The H1 fixture acceptance demo passed with zero effects, attempts, task runs and real Pi contacts and an unchanged workspace digest.
- The browser integration suite passed after preferring installed Chrome over Edge for its managed DevTools test. The missing-key provider selection regression was reproduced before the fix and passes after it.
- The earlier uncommitted source state was saved as Git stash `1aa23973d1bdc71d868fcc6433a08f026ebf9c02` before the fast-forward. Its source files are represented in `main`, the 15 local attachments remain in place, and the original data directory remains separate from the installation. `main` is source authority for this local pilot, not proof of a production Pi deployment.

## Honest limits

H2A can probe Pi's read-only identity boundary, but live acceptance still needs a scoped Pi credential and a reachable Pi endpoint. This installation does not implement self-service Pi token issuance, managed write authority, or H2/H3 dispatch. Task completion that requires an independent post-review still needs a genuinely different ready model or endpoint; the single local Bonsai model cannot independently review itself. The saved 9arm remote profile has no usable key. Discord is deliberately paused in the installed copy; its stored token was not copied into source control. To resume it, check its allowlist and local-model settings first.

The IDE provides document editing, formatting, direct Run/Test, and Go/JavaScript debug paths, but it is not a full VS Code language-server replacement. Windows process jobs provide bounded lifetime, not a complete OS isolation profile. Do not treat the local pilot as an unattended coding or production-write system.

`go test -race` was not claimed for this release. Earlier Windows race runs were blocked by the host toolchain/ThreadSanitizer configuration.
