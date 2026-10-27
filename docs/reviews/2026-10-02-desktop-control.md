# Hermetrix Windows computer control — 2026-10-02

## Outcome and opening the feature

Implemented and exercised physical Windows mouse control and screenshot evidence.
The running desktop build is **http://127.0.0.1:18806/**, executable
`.tmp-ui/hermetrix-desktop-final.exe`, data
`.tmp-ui/desktop-preview-data-20261002` (schema74).
Use `scripts/start-desktop-harness.ps1` to start/reuse this instance. It builds
when stopped, opens a hidden local server and never stops another port/process.
Original18801/Easiiot, Delivery18804 and Deliveryapp18920 are preserved; this
feature has **not** replaced their executables or migrated their databases.

In the UI: choose Workspace **hermetrix-harness**, select **เบราว์เซอร์** in a
pane, then **ควบคุมเครื่อง**. Load windows, explicitly choose the target, and
capture its latest image. Clicking the image selects a point without dispatching
input. Review the marker/coordinates, press **ตรวจและคลิกจุดนี้**, then confirm
the exact command. The panel shows actual pending/error/cancelled/uncertain
states and before/after images. **ตรวจผลคำสั่งล่าสุด** recovers the persisted
receipt after reload/restart. **หยุดรอผล** stops waiting; it does not undo input
already dispatched. Native/UI actions are never automatically retried.

## What was added

| Capability | Evidence and limits |
| --- | --- |
| Window inventory, selected-window screenshots and UI Automation labels | Existing native capture is now exposed through the project-chat desktop tool and a dedicated control panel. No automatic first-window capture. |
| Physical left click at a point | Windowsamd64 SendInput with window-relative coordinates, DPI/virtual-desktop mapping, exact process/window/bounds and occlusion checks. Real Paint test passed. |
| Button invocation and setting text | Existing UI Automation path retains exact-target validation and approvals; native test fixture invokes a button and fills its text field. No general keyboard shortcut/drag/scroll/right-click support is claimed. |
| Chat tool | `desktop` actions `list`, `observe`, `view`, `click`, `invoke`, `set_value`. Read-only list/observe need no mutation approval; input and bitmap sharing require exact approvals. New project-scoped chats receive this tool. Personal chat and previously frozen tool contracts are unchanged. |
| Model receives actual pixels | Explicit `view` approval, qualified image-capable provider, unchanged profile revision/destination, owned bounded image artifact and fresh frame. PNG bytes verified over the actual provider HTTP/SSE adapter using a local deterministic model fixture. **No real vision-model reasoning was tested.** |
| Recovery and durable evidence | Additive schema74 stores both image IDs and observation JSON. One-use native receipt is prepared before chat approval; denial cancels it. Stale/expired/replayed approval is rejected. Restart cannot replay a dispatched action. |

The image is a timestamped observation, **not live video or an unattended
whole-machine agent**. UIA text and image content are untrusted tool observations.
Text-only models can consume UIA text; they cannot see a screenshot merely
because its artifact ID was included. The latest failed/uncertain desktop result
invalidates older approved pixels. Image context reserves are conservative
estimates and do not contaminate text-token calibration.

## Reproduced issues and fixes

- Loading render erased the text about to be entered: capture the intended value
  before rendering pending state; regression verifies both prepared and confirmed
  text match.
- Windows rejected background foreground activation: raise only the selected
  window, then validate the window at the exact point and its bounds before
  injecting input. No input-queue attachment or activation-policy bypass.
- UI result could arrive after selecting another window: generation and target
  fences prevent the old observation replacing the newly selected one.
- Screenshot selection needed keyboard/touch alternatives: coordinate inputs,
  visible point marker, minimum44px controls and separate confirmation.
- Long window options overflowed a390px screen: remove intrinsic select minimum
  width and constrain it to the panel. Explicit selection also prevents capturing
  an unrelated first window.
- Observation text clipping could discard the approval hash: compact binding,
  hash and screenshot metadata precede the UIA tree.
- Adding the desktop schema exceeded the smallest frozen tool budget: reduce
  the new tool's schema prose; keep the existing budget unchanged.

## Actual validation

Opened a test-owned blank Paint window. The agent's direct computer-use baseline
selected Pencil. **Hermetrix UI** physically clicked Fill at(309,97), then Pencil
at(270,96). Downloaded native after-images show the selected toolbar outlines.
On the final build, a390px browser cancelled a prepared Fill click (persisted
`cancelled`, no after-image), then approved a new one-use command and selected
Fill. Canvas remained blank; no file was saved and unrelated apps were not
operated.

First two completed receipts:

- `desktopaction_c4175dfcb2a2950e98c35d195f9c8394`
- `desktopaction_072fe4a5775cfc90abb84367391856ac`

Current final after-image: `artifact_089ebc8251118bbd1f1819541f43f6fa`.
Final390px cancelled receipt: `desktopaction_7020bad0c117149fb4f8699da6c53b45`;
separately approved receipt: `desktopaction_5f6dd9ead887f6e7a1048ec6daebf5be`.
`.tmp-ui/desktop-final-receipts.json` proves all four receipts remain present,
with states completed/completed/cancelled/completed and schema74.
An actual HTTP replay and stale-frame preparation were both rejected; database
action count stayed unchanged. These currently return HTTP500 with an explicit
validation message (status-code classification remains an improvement).
The restart/read-back recovered the original before/after artifact IDs.

- Full agent suite:45.812s; tools:1.455s; store:5.646s.
- Native exact-approval/no-replay/physical-pointer fixture:18.266s.
- Full required-Chrome web suite:81.347s. After final small UI changes, actual
  browser inspection and relevant UI contracts passed again:6.929s.
- UI JavaScript:135/135. Vet for all changed Go packages and final build pass.
- Real browser375/390/414/768/1024/1440px checks: no page/panel horizontal
  overflow, window selection inside panel, primary controls44px or more.
  No browser console warning/error was recorded. Browser viewport restored.

Evidence: `.tmp-ui/desktop-control-final.jpg`,
`.tmp-ui/desktop-after-pencil.png`, `.tmp-ui/desktop-after-fill.png`,
`.tmp-ui/desktop-final-runtime-proof.json`, `.tmp-ui/desktop-responsive-proof.json`,
and `.tmp-ui/desktop-*-tests*.txt`. Native fixture is separate from the live Paint
exercise. Provider-wire fixture is separate from actual Bonsai execution.

## Remaining limits

Bonsai on8088 is currently stopped, and this isolated instance has no configured
model. Therefore an actual natural-language→live-model→desktop task is **not
verified**. Existing model-restoration approval remains pending; no rejected
startup was replayed. To use chat, connect a qualified model in Models and start
a new project chat; image understanding additionally requires verified image
capability. No claim of complete Codex parity, public remote desktop, unattended
operation, all-screen recording or Discord desktop roundtrip is made.

No commit/PR/public deployment was requested or performed. Earlier dirty changes,
credentials, Delivery orders/acceptance gate and `.tmp-ui` evidence are retained.
