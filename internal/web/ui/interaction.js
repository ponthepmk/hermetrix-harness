(function exposeInteraction(root, factory) {
  const ui = factory(root);
  if (typeof module !== "undefined" && module.exports) module.exports = ui;
  root.HermetrixInteraction = ui;
  if (root.document) ui.install(root.document);
})(typeof globalThis !== "undefined" ? globalThis : this, function buildInteraction(root) {
  "use strict";

  function requestLabel(path, method = "GET") {
    if (/\/providers\/[^/]+\/test/.test(path)) return "กำลังทดสอบการเชื่อมต่อโมเดล";
    if (/post-review/.test(path)) return "กำลังตรวจทานผลการแก้ไข";
    if (/verify/.test(path)) return "กำลังตรวจผลการทำงาน";
    if (/auto-plan/.test(path)) return "กำลังสร้างแผนงาน";
    if (/proposals/.test(path) && method !== "GET") return "กำลังดำเนินการแก้ไข";
    if (/\/tasks/.test(path)) return method === "GET" ? "กำลังโหลดแผนและหลักฐาน" : "กำลังบันทึกงาน";
    if (/\/files/.test(path)) return "กำลังโหลดไฟล์";
    if (/\/projects/.test(path)) return "กำลังโหลดพื้นที่ทำงาน";
    if (/\/providers|\/profiles/.test(path)) return "กำลังโหลดโมเดล";
    return method === "GET" ? "กำลังโหลดข้อมูล" : "กำลังบันทึกข้อมูล";
  }

  // Only explicitly initiated work enters this ledger. Background polling must
  // not make a ready interface appear perpetually busy.
  function createTracker({now = () => Date.now(), changed = () => {}} = {}) {
    const requests = new Map();
    let sequence = 0, last = null;
    function snapshot() {
      const items = [...requests.values()];
      const latest = items[items.length - 1];
      return latest ? {state:"loading", label:latest.label, count:items.length,
        elapsed:Math.floor((now() - Math.min(...items.map(item => item.started))) / 1000)} : last || {state:"idle"};
    }
    return {
      snapshot,
      pending:control => [...requests.values()].some(item => item.control === control),
      begin({label, control = null}) {
        const id = ++sequence;
        requests.set(id, {label, control, started:now()});
        // Retain a failure until the whole overlapping group settles.
        if (requests.size === 1) last = null;
        changed(snapshot());
        let finished = false;
        return error => {
          if (finished) return;
          finished = true;
          requests.delete(id);
          if (error) last = {state:"error", label:"ทำรายการไม่สำเร็จ", control};
          else if (!last) last = {state:"success", label:"อัปเดตแล้ว"};
          changed(snapshot());
        };
      },
      dismiss() { if (!requests.size) { last = null; changed(snapshot()); } },
    };
  }

  let documentRef, status, intent = null, hideTimer, tickTimer;
  const controls = new Map();
  const tracker = createTracker({changed:render});
  function render(value) {
    if (!status) return;
    clearTimeout(hideTimer);
    clearInterval(tickTimer);
    status.dataset.state = value.state;
    status.hidden = value.state === "idle";
    const label = status.querySelector("[data-activity-label]");
    if (label.textContent !== value.label) label.textContent = value.label || "";
    const detail = status.querySelector("[data-activity-detail]");
    const updateTime = () => {
      const current = tracker.snapshot();
      detail.textContent = current.state === "loading"
        ? current.elapsed >= 8 ? `ยังทำงานอยู่ · ${current.elapsed} วินาที` : "กรุณารอสักครู่"
        : value.state === "error" ? "ข้อมูลเดิมยังอยู่ ลองทำรายการอีกครั้ง" : "";
    };
    updateTime();
    if (value.state === "loading") tickTimer = setInterval(updateTime, 1000);
    if (value.state === "success") hideTimer = setTimeout(() => tracker.dismiss(), 1400);
    status.querySelector("[data-activity-dismiss]").hidden = value.state === "loading";
    // Do not set disabled: existing handlers own permission/validation state.
    for (const [control, ownsBusy] of controls) {
      if (tracker.pending(control)) continue;
      control.classList.remove("is-pending");
      if (ownsBusy) control.removeAttribute("aria-busy");
      controls.delete(control);
    }
  }

  function beginRequest(path, options = {}) {
    if (!intent || Date.now() - intent.at > 1200 || options.uiSilent) return null;
    const control = intent.control;
    const ownsBusy = !control.hasAttribute("aria-busy");
    if (!controls.has(control)) controls.set(control, ownsBusy);
    control.classList.add("is-pending");
    if (ownsBusy) control.setAttribute("aria-busy", "true");
    const end = tracker.begin({control, label:requestLabel(path, options.method || "GET")});
    return error => {
      end(error);
    };
  }

  function install(doc) {
    if (documentRef) return;
    documentRef = doc;
    status = doc.querySelector("#interactionStatus");
    status?.querySelector("[data-activity-dismiss]").addEventListener("click", () => tracker.dismiss());
    const capture = event => {
      const control = event.type === "submit" ? event.submitter : event.target.closest?.("button, a, summary");
      if (!control || control.closest("#interactionStatus")) return;
      if (tracker.pending(control)) {
        event.preventDefault(); event.stopImmediatePropagation(); return;
      }
      intent = {control, at:Date.now()};
      control.classList.remove("is-pressed");
      control.classList.add("is-pressed");
      setTimeout(() => control.classList.remove("is-pressed"), 240);
    };
    doc.addEventListener("click", capture, true);
    doc.addEventListener("submit", capture, true);
    // Page teardown naturally discards this local ledger; no server operation
    // or approval is retried, cancelled or marked successful by this module.
  }
  function beginOperation(label) { return tracker.begin({label}); }
  return {createTracker, requestLabel, beginRequest, beginOperation, install};
});
