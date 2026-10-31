/* Pure presentation for the persisted Goal lifecycle; no request dispatch. */
(() => {
  "use strict";
  const escape = value => String(value ?? "").replace(/[&<>"']/g, ch => ({"&":"&amp;","<":"&lt;",">":"&gt;",'"':"&quot;","'":"&#39;"}[ch]));
  const labels = {active:"กำลังดำเนินงาน",waiting:"รอให้ตรวจสอบ",stopping:"กำลังหยุด",stopped:"หยุดแล้ว",completed:"รอบทำงานเสร็จแล้ว",budget_exhausted:"ถึงขีดจำกัดรอบนี้"};
  const stateLabel = value => labels[value] || "ไม่ทราบสถานะ";
  const count = value => Number.isFinite(value) && value >= 0 ? value.toLocaleString("th-TH") : "—";
  const time = value => value && Number.isFinite(Date.parse(value)) ? new Date(value).toLocaleString("th-TH") : "ยังไม่มีเวลาอัปเดต";
  const actions = {plan:"สร้างแผน",start_run:"เริ่มตามแผน",select_files:"เลือกไฟล์",propose:"เสนอการแก้ไข",apply:"แก้ไฟล์",verify:"ทดสอบ",verify_observe:"ติดตามผลทดสอบ",review:"ตรวจผล",complete:"ตรวจเกณฑ์สำเร็จ"};

  function render({task,goal,providers = [],receipts = [],loading = false,error = "",pending = false,uncertain = false} = {}) {
    const disabled = pending || loading || Boolean(error) || uncertain;
    const terminalTask = ["completed","cancelled"].includes(task?.state);
    const expired = goal?.deadline && Date.parse(goal.deadline) <= Date.now();
    const revised = goal && task?.active_requirement_revision && goal.requirement_revision !== task.active_requirement_revision;
    const canStart = !terminalTask && (!goal || ["budget_exhausted","completed","stopped"].includes(goal.state));
    const canResume = goal && goal.state === "waiting" && !expired && !revised && !terminalTask;
    const canStop = goal && ["active","waiting"].includes(goal.state);
    const writable = task?.requirement?.contract?.writable_paths || [];
    const providerOptions = selected => providers.map(item => `<option value="${escape(item.id)}" ${item.id === selected ? "selected" : ""}>${escape(item.name)}</option>`).join("");
    const writer = providers[0], reviewer = providers.find(item => item.id !== writer?.id);
    const sameRuntime = writer && reviewer && writer.model === reviewer.model && writer.base_url === reviewer.base_url;
    const form = canStart ? `<form data-goal-start><p>ให้ระบบทำตามแผนต่อจนจบ หรือหยุดเมื่อมีเรื่องที่ต้องตรวจ</p>
      <div class="goal-provider-fields"><label>โมเดลทำงาน<select name="provider_id" required ${disabled ? "disabled" : ""}>${providerOptions(writer?.id)}</select></label>
      <label>โมเดลตรวจผล<select name="reviewer_provider_id" required ${disabled ? "disabled" : ""}>${providerOptions(reviewer?.id)}</select></label></div>
      ${providers.length < 2 ? '<p class="form-note">เพิ่มโปรไฟล์ทำงานและตรวจผลใน Models ก่อนเริ่ม</p>' : ""}
      ${sameRuntime ? '<p class="form-note">สองโปรไฟล์นี้ใช้โมเดลเดียวกัน การตรวจเป็นอีกรอบหนึ่ง</p>' : ""}
      <label class="goal-write-consent"><input type="checkbox" name="apply_changes" ${!writable.length || disabled ? "disabled" : ""}> อนุญาตแก้ไฟล์ตามขอบเขตงานนี้</label>
      <p class="form-note">${writable.length ? "หากไม่เลือก ข้อเสนอแก้ไฟล์จะรอให้คุณตรวจ" : "ยังไม่ได้กำหนดไฟล์ที่แก้ได้ ข้อเสนอจะรอให้คุณตรวจ"} · สูงสุด 1 ชั่วโมง · แก้ไขได้ 8 รอบ</p>
      <button class="primary" type="submit" ${disabled || providers.length < 2 ? "disabled" : ""}>${goal ? "เริ่มรอบใหม่" : "เริ่มทำงานต่อเนื่อง"}</button></form>` : "";
    const detail = goal ? `<details><summary>เหตุผลและความคืบหน้า</summary>
      ${goal.reason ? `<p class="goal-reason">${escape(goal.reason)}</p>` : '<p>รายละเอียดจะอัปเดตเมื่อมีผลของขั้นถัดไป</p>'}
      ${expired ? '<p>เวลาของรอบนี้หมดแล้ว การเริ่มรอบใหม่จะเก็บประวัติเดิมไว้</p>' : ""}
      ${revised ? '<p>เป้าหมายงานเปลี่ยนแล้ว ต้องเริ่มรอบใหม่ตามขอบเขตล่าสุด</p>' : ""}
      ${goal.state === "stopped" ? '<p>รอบนี้หยุดแล้ว เริ่มรอบใหม่ได้โดยเก็บประวัติและขีดจำกัดของรอบเดิมไว้</p>' : ""}
      <ol class="goal-receipts">${receipts.slice(-5).map(item => `<li>${escape(actions[item.action] || item.action)} · ${escape({planned:"เตรียมแล้ว",dispatched:"กำลังดำเนินการ",observed:"มีผลที่บันทึกแล้ว",failed:"ต้องตรวจผล",completed:"มีผลที่บันทึกแล้ว"}[item.state] || item.state)}</li>`).join("")}</ol>
      <small>รอบ ${escape(goal.id)} · หมดเวลา ${escape(time(goal.deadline))}</small></details>` : "";
    return `<section class="task-goal-runner" aria-labelledby="goalRunnerTitle">
      <div class="goal-runner-heading"><h4 id="goalRunnerTitle">ทำงานต่อเนื่อง</h4><span class="goal-state" data-goal-state="${escape(goal?.state || "idle")}" role="status">${loading ? "กำลังตรวจสถานะ…" : goal ? escape(stateLabel(goal.state)) : terminalTask ? "งานนี้ปิดแล้ว" : "พร้อมตั้งค่ารอบทำงาน"}</span></div>
      ${goal ? `<p class="goal-runner-counts">ใช้ ${count(goal.actions_used)} / ${count(goal.config?.max_actions)} ขั้น · แก้ไข ${count(goal.attempts_used)} / ${count(goal.config?.max_attempts)} รอบ</p><p class="form-note">อัปเดต ${escape(time(goal.updated_at))}</p>` : ""}
      ${error ? `<div class="goal-error" role="alert"><p>${uncertain ? "ยังยืนยันผลคำขอไม่ได้ ตรวจสถานะก่อนส่งใหม่" : "อ่านสถานะล่าสุดไม่ได้ ข้อมูลที่แสดงอาจยังไม่อัปเดต"}</p><details><summary>รายละเอียดการเชื่อมต่อ</summary><p>${escape(error)}</p></details></div>` : ""}
      <div class="goal-runner-actions">${canResume ? `<button type="button" class="primary" data-goal-action="resume" ${disabled ? "disabled" : ""}>ทำต่อจากงานเดิม</button>` : ""}
      ${canStop ? `<button type="button" class="ghost" data-goal-action="stop" ${disabled ? "disabled" : ""}>หยุดงาน</button>` : ""}
      <button type="button" class="ghost" data-goal-action="refresh" ${pending || loading ? "disabled" : ""}>ตรวจสถานะล่าสุด</button>
      ${uncertain ? '<button type="button" class="ghost" data-goal-action="retry">ส่งคำขอเดิมอีกครั้ง</button>' : ""}</div>
      ${pending ? '<p role="status">กำลังส่งคำขอ…</p>' : ""}${form}${detail}
    </section>`;
  }

  const api = Object.freeze({render,stateLabel});
  if (typeof module !== "undefined" && module.exports) module.exports = api;
  if (typeof window !== "undefined") window.HermetrixGoalView = api;
})();
