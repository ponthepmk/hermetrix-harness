# Voice capture reliability — 2026-11-01

Scope: development checkout only; no deployed binary, Pi service, credentials or runtime configuration changed. Original unwanted-English incident remains unreproduced. The previous real Whisper silence probe produced an empty transcription.

## Implemented

- Match streaming partial/final/no-speech/error events to the active capture epoch; terminal events close that epoch. Both Windows streaming transports now include epochs on no-speech/error receipts.
- Reject old browser sockets and aborted/replaced Windows capture callbacks. An old capture's cleanup cannot deactivate a replacement.
- Invalidate pending fallback transcription when discarding capture.
- Apply a conservative PCM signal gate before native ASR and after HTTP audio normalization. Silence, DC, low-level signal and isolated clicks are rejected. This is not semantic VAD: sustained noise and speaker echo can still pass. Very short or very quiet speech may need another utterance.
- Preserve Windows playback-tail suppression when explicitly stopping output. Browser echo cancellation remains browser-managed; no claim of acoustic echo classification.
- Bump the voice asset URL so updated clients load the matching event contract.

## Evidence

The extracted real browser event handler and existing turn controller pass 12 Node tests, including stale/duplicate final, stale partial, no-speech followed by a fabricated final, hidden-panel rejection and bilingual text preservation. New Go signal tests pass for silence, DC, isolated transient, quiet input, sustained signal and malformed WAV. Build passes.

These checks do not prove real microphone, Thai/English recognition accuracy, loudspeaker echo rejection or repeated full-duplex acoustic interruption. Those require recorded fixtures or the user's later live reproduction. Do not deploy as an acoustic-VAD completion claim. Keep test files local per the user's commit scope.

Rollback: revert only the voice capture changes as one client/server unit. No schema migration or production rollback is currently needed.
