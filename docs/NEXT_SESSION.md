# Current state — 2026-09-23

The integrated local pilot lives on `codex/hermetrix-usable-release`, based on H2A (`8b5b9ca`), with a separate Windows installation at `C:\Users\ZP2E0\AppData\Local\Hermetrix`. See [LOCAL_RELEASE.md](LOCAL_RELEASE.md) for the launch path, verified behavior and limits. The original dirty `main` checkout at `D:\Projects\harness\hermetrix-harness` was preserved and is not this release's source authority.

The local UI, Bonsai chat at a truthful 16k context, editable IDE, direct Test, and H1 fixture proof have passed. The full Go suite, frontend suite and vet passed in the integrated checkout. The stable installation uses copied data at schema 49. Discord is paused. A model with no credential no longer gets chosen as the background reviewer.

Do not claim full Platform managed execution. H2A's real Pi read-only proof still needs a scoped credential/live Pi acceptance. Self-service token issuance is not implemented, and managed write authority is not implemented. A second independent model/endpoint is also needed to close a coding task that requires post-review. Do not use the local pilot for unattended production writes.

Next engineering milestone: provide an intentional Pi token pairing flow and run the H2A read-only acceptance with a scoped credential; then add a second qualified local reviewer and complete one end-to-end coding task on an isolated fixture. Keep all authority, effect no-replay and independent-review gates intact. Do not merge the integrated branch over the dirty original tree without a separate source review.
