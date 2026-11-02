# Local architecture pilot

Current operational update (2026-11-01): this pilot was explicitly authorized, updated and verified running at the same port with its existing schema82 data. See [runtime reliability release](../reviews/2026-11-01-runtime-reliability.md). The startup-pending statements below describe the original preparation checkpoint; separate Brain/physical-voice acceptance remains open.


The prepared pilot uses a separate database and executable. It does not replace
the installed application or run historical marketplace Goals. Current evidence
and limitations are in the [checkpoint](../reviews/2026-10-29-interaction-and-learning.md).

Preflight, without starting an application:

```powershell
powershell -NoProfile -File scripts/start-architecture-pilot.ps1 `
  -ConfigPath .tmp-ui/architecture-20261029/pilot.json -CheckOnly
```

The concrete startup, pending the recorded serving approval boundary:

```powershell
powershell -NoProfile -File scripts/start-architecture-pilot.ps1 `
  -ConfigPath .tmp-ui/architecture-20261029/pilot.json -Open
```

Expected URL: `http://127.0.0.1:18816/`. The launcher refuses an occupied port
belonging to another executable, data directory or reviewer. It does not find
another port. Launch logs and PID are printed. Startup itself is not yet proven
by the preflight test.

The actual Bonsai service is on port 8088 with 65,536 allocated context. The
tested release Whisper CUDA worker is on port 8082. Its path/model are pinned in
the local config; that config contains no credential. Cold GPU initialization
needs warmup before judging ASR latency. The launcher configures the application
child, but does not install or silently restart either model service.

Once the pilot is open:

1. In Chat choose the local Bonsai work profile. Use **พูด** beside the composer.
   Allow the browser microphone; leave automatic send on for a continuous
   conversation. **หยุดคำตอบ** stops playback and requests the owned generation.
   Closing voice preserves unsent text in the chat draft.
2. In Plans open a Task and use **ทำงานต่อเนื่อง**. Inspect a wait before resuming.
   A stopped/exhausted round requires an explicit new round. File-write consent
   starts unchecked and cannot expand the Task's writable paths.
3. Open **บริบทที่ส่งให้โมเดล** under results to inspect exact included Skill and
   Project Brain identities. This is inclusion evidence, not a quality score.
4. In MCP Settings select `project-brain-read`. Enter the separately granted
   read credential there, enable the profile and discover it. Never paste the
   token into chat. The startup binding is project `hermetrix-harness`.
5. Test actual Thai/English speech, speaker playback and repeated interruptions
   on the owner's device. Synthetic tests do not replace this acoustic check.

Learning candidates remain reviewable in this fresh pilot; none were activated
by the inconclusive experiment. Same-model review is another pass on Bonsai.

To stop the pilot, stop only the PID printed by its launcher after checking its
executable/data identity. Existing installed services have different ownership
and are left alone. Retain the pilot data for history. Schema 82 is additive;
do not manually downgrade its version or open it with an older executable.
The preserved original schema-81 runtime remains separate.
