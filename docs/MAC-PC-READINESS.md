# MacBook M3/M4 and PC — readiness contract

User clarification, 2026-11-02: MacBooks are Apple Silicon M3/M4. Bonsai is a separate provider hosted only on the PC, not a dependency of the Mac application or the platform core. Godot remains deferred.

Required topology: a native darwin/arm64 Harness on Mac and windows/amd64 Harness on PC; separately configured model profiles including Bonsai PC; scoped shared Pi/Project Brain. Opening the Mac interface must work without a Bonsai process on that Mac. A Mac request to the PC provider must retain explicit remote egress consent and authenticated transport; a tunnel must not silently imply that remote processing became local/private.

Development work now includes a non-migrating POSIX launcher and a native Windows/macOS CI workflow. The Apple Silicon binary cross-compiles on the PC; this is compile evidence only. Hosted CI and real M3/M4 hardware acceptance have not run from this checkout. CI was not pushed or dispatched automatically.

Initial inspection found Windows-only voice paths. The browser PCM/WebSocket capture and ASR implementation have now been moved into shared files. Windows native-device capture remains platform-specific; the Mac uses browser microphone capture. macOS TTS now calls the native say command with a private UTF-8 input file and bounded WAV output, without a shell. Windows voice regressions pass. Native Mac speech tests are supplied, including an explicit real-synthesis opt-in; they have not run on an actual Mac. Do not advertise acoustic parity from compilation alone. Native macOS command sandbox code also needs Mac execution evidence.

Next work: implement/qualify macOS speech and capture paths; verify provider identity/egress routing for the PC provider; create native Mac packaging and run the actual chat/files/commands/Brain/recovery workflows on M3/M4. Obtain the Mac SSH host or native acceptance receipts before claiming hardware readiness. Existing Windows and production gates remain unchanged.
