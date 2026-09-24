(() => {
  // This is an owner-operated export of one completed task, not an automatic
  // learning trigger. Candidate IDs in localStorage are only status bookmarks;
  // approval, credentials and evidence bytes remain on their owning services.
  const records = new Map();
  const endpoint = "/api/project-brain/candidates";
  // The app passes a small facade at the render boundary. The module does not
  // depend on top-level lexical declarations from a differently loaded script.
  let api, askAction, load, currentActor, escapeHTML, formatDate;
  function configure(deps) {
    for (const key of ["api","askAction","load","currentActor","escapeHTML","formatDate"]) {
      if (typeof deps?.[key] !== "function") throw Error(`Project Brain UI is missing ${key}`);
    }
    ({api,askAction,load,currentActor,escapeHTML,formatDate} = deps);
  }
  const statusKey = taskID => `hermetrix-project-brain-candidate:${taskID}`;
  const statusLabels = {
    pending: ["รอส่งไป Pi", "amber"], sending: ["กำลังส่งไป Pi", "blue"],
    submitted: ["Pi รับข้อเสนอแล้ว", "green"], blocked: ["ส่งไม่ได้", "red"],
    conflict: ["ข้อมูลที่ส่งขัดแย้ง", "red"]
  };
  const reasonLabels = {
    "Task is not completed": "งานยังไม่เสร็จ",
    "Project is not active": "โปรเจกต์ไม่ได้เปิดใช้งาน",
    "Project export is disabled": "โปรเจกต์ยังไม่อนุญาตให้เลือกข้อมูลส่งออก",
    "Task export is disabled": "งานนี้ยังเป็นส่วนตัว",
    "Task has no active requirement revision": "งานไม่มีเกณฑ์ตรวจฉบับปัจจุบัน"
  };
  function recordFor(task) {
    if (!records.has(task.id)) {
      let candidateID = "";
      try { candidateID = localStorage.getItem(statusKey(task.id)) || ""; } catch {}
      records.set(task.id, {task, open:false, root:null, eligibility:null, loading:false,
        error:"", busy:"", validationID:"", artifactIDs:new Set(), solution:"",
        kind:"test", formRevision:0, preview:null, candidateID, delivery:null, statusLoading:false});
    }
    const record = records.get(task.id);
    record.task = task;
    return record;
  }
  function selectedValidation(record) {
    return (record.eligibility?.validations || []).find(item => item.id === record.validationID) || null;
  }
  function selectedArtifacts(record) {
    return (selectedValidation(record)?.artifacts || []).filter(item => record.artifactIDs.has(item.id) && item.eligible);
  }
  function readyForPreview(record) {
    return Boolean(record.validationID && record.solution.trim() &&
      selectedArtifacts(record).length > 0 && selectedArtifacts(record).length <= 8);
  }
  function deliveryHTML(record) {
    const delivery = record.delivery;
    if (!record.candidateID && !delivery) return "";
    const label = delivery?.state === "pending" && delivery.attempts > 0
      ? ["รอส่งอีกครั้ง", "amber"] : statusLabels[delivery?.state] || ["กำลังตรวจสถานะ", "amber"];
    return `<div class="brain-delivery" role="status"><div><strong>${escapeHTML(label[0])}</strong>
      <p>${delivery?.state === "submitted" ? "ข้อเสนอนี้รอผู้ดูแล Pi ตรวจหลักฐานและรับรอง จึงจะปรากฏในการค้นหาปกติ" :
        delivery?.state === "blocked" || delivery?.state === "conflict" ? "ข้อมูลหรือสิทธิ์แชร์อาจเปลี่ยนไป ตรวจสาเหตุและทำตัวอย่างใหม่ก่อนส่ง" :
        "ข้อเสนอเก็บในคิวของเครื่องนี้อย่างทนทาน การเชื่อมต่อ Pi ขัดข้องไม่ทำให้งานที่เสร็จแล้วล้มเหลว"}</p>
      ${delivery?.last_error ? `<small>สาเหตุ: ${escapeHTML(delivery.last_error)}</small>` : ""}
      ${record.candidateID ? `<small>รหัสข้อเสนอ: <code>${escapeHTML(record.candidateID.slice(0, 18))}…</code></small>` : ""}</div>
      <button type="button" class="ghost" data-brain-status ${record.statusLoading ? "disabled" : ""}>${record.statusLoading ? "กำลังตรวจ…" : "ตรวจสถานะ"}</button></div>`;
  }
  function policyHTML(record) {
    const reason = record.eligibility?.reason || "";
    const text = reasonLabels[reason] || reason || "ยังเลือกข้อมูลส่งออกไม่ได้";
    let action = "";
    if (reason === "Project export is disabled") action = `<button type="button" class="ghost" data-brain-share="project">อนุญาตแชร์โปรเจกต์นี้</button>`;
    else if (reason === "Task export is disabled") action = `<button type="button" class="ghost" data-brain-share="task">อนุญาตแชร์งานนี้</button>`;
    return `<div class="brain-empty"><p>${escapeHTML(text)}</p>${action}
      <small>การอนุญาตให้เลือกส่งออกยังไม่ส่งข้อความหรือหลักฐานไป Pi</small></div>`;
  }
  function evidenceHTML(record) {
    const validation = selectedValidation(record);
    if (!validation) return "";
    const artifacts = validation.artifacts || [];
    return `<fieldset class="brain-evidence"><legend>หลักฐานที่ต้องการอ้างอิง</legend>
      ${artifacts.length ? artifacts.map(item => `<div class="brain-evidence-row">
        <label><input type="checkbox" name="artifact_ids" value="${escapeHTML(item.id)}"
          ${record.artifactIDs.has(item.id) ? "checked" : ""} ${item.eligible && !record.busy ? "" : "disabled"}>
          <span><strong>${escapeHTML(item.name)}</strong><small>${escapeHTML(item.content_digest.slice(0, 24))}… · ${item.eligible ? "เลือกส่งได้" : "ยังเป็นส่วนตัว"}</small></span></label>
        ${item.eligible ? "" : `<button type="button" class="ghost" data-brain-share-artifact="${escapeHTML(item.id)}" ${record.busy ? "disabled" : ""}>อนุญาตหลักฐานนี้</button>`}
      </div>`).join("") : `<p>ผลตรวจนี้ไม่มี artifact ที่อ้างอิงได้ บันทึกหลักฐานแบบ immutable ก่อนแชร์วิธีแก้</p>`}
      <small>เลือกได้ 1–8 รายการ Pi จะได้รับรหัสและ digest ของหลักฐาน ไม่ได้รับไฟล์ต้นฉบับโดยอัตโนมัติ</small>
    </fieldset>`;
  }
  function previewHTML(record) {
    const candidate = record.preview?.candidate;
    if (!candidate) return "";
    return `<section class="brain-preview" aria-label="ตัวอย่างข้อมูลที่จะส่ง"><h4>ตรวจข้อมูลที่จะส่ง</h4>
      <dl><dt>หัวข้อ</dt><dd>${escapeHTML(candidate.title)}</dd>
      <dt>ปัญหา</dt><dd class="brain-prose">${escapeHTML(candidate.problem)}</dd>
      <dt>วิธีแก้</dt><dd class="brain-prose">${escapeHTML(candidate.solution)}</dd>
      <dt>ผลตรวจ</dt><dd>${escapeHTML(candidate.verification?.kind || "")} · ${escapeHTML(candidate.verification?.scope || "")}</dd>
      <dt>หลักฐาน</dt><dd><ul class="brain-preview-evidence">${selectedArtifacts(record).map(item =>
        `<li>${escapeHTML(item.name)} · <code>${escapeHTML(item.content_digest)}</code></li>`).join("")}</ul></dd></dl>
      <p>การส่งนี้เป็นเพียงข้อเสนอ ผู้ดูแล Pi ต้องตรวจและรับรองก่อน agent อื่นนำไปใช้</p>
      <button type="button" class="primary" data-brain-submit ${record.busy ? "disabled" : ""}>ยืนยันส่งข้อเสนอนี้ให้ Pi ตรวจ</button>
    </section>`;
  }
  function formHTML(record) {
    const checks = record.eligibility?.validations || [];
    if (!checks.length) return `<div class="brain-empty"><p>ยังไม่พบผลตรวจผ่านของเกณฑ์งานฉบับปัจจุบันที่มีหลักฐาน</p>
      <small>กลับไปตรวจงานและบันทึก artifact ให้ผลตรวจ จากนั้นกด “โหลดผลตรวจใหม่”</small></div>`;
    return `<form class="brain-form" data-brain-form><label>ผลตรวจที่ผ่าน
      <select name="validation_id" required ${record.busy ? "disabled" : ""}>${checks.map(item => `<option value="${escapeHTML(item.id)}" ${record.validationID === item.id ? "selected" : ""}>${escapeHTML(item.check_id)} · ${escapeHTML(item.requirement_id)} · ${formatDate(item.observed_at)}</option>`).join("")}</select></label>
      ${evidenceHTML(record)}
      <label>วิธีแก้ที่นำกลับมาใช้ได้<textarea name="solution" rows="5" maxlength="6000" required ${record.busy ? "disabled" : ""} placeholder="อธิบายวิธีแก้ที่ผ่านการตรวจแล้ว พร้อมข้อจำกัดที่ควรรู้">${escapeHTML(record.solution)}</textarea></label>
      <label>วิธีที่ใช้ตรวจ<select name="verification_kind" ${record.busy ? "disabled" : ""}>${[["test","ทดสอบ"],["build","บิลด์"],["manual","ตรวจด้วยคน"],["review","รีวิว"],["reproduction","ทดสอบซ้ำ"]].map(([value,label]) => `<option value="${value}" ${record.kind === value ? "selected" : ""}>${label}</option>`).join("")}</select></label>
      <div class="action-row"><button type="submit" class="ghost" data-brain-preview ${readyForPreview(record) && !record.busy ? "" : "disabled"}>${record.busy === "preview" ? "กำลังสร้างตัวอย่าง…" : "ตรวจสิ่งที่จะส่ง"}</button></div>
    </form>${previewHTML(record)}`;
  }
  function paint(record) {
    const root = record.root;
    if (!root || !root.isConnected) return;
    const delivery = record.delivery;
    const badge = delivery?.state === "pending" && delivery.attempts > 0
      ? ["รอส่งอีกครั้ง", "amber"] : delivery ? statusLabels[delivery.state] : null;
    root.innerHTML = `<section class="inspect-section brain-share"><div class="brain-share-head"><div>
      <p class="eyebrow">Project Brain</p><h3>แชร์วิธีแก้ที่ตรวจแล้ว</h3>
      <p>ส่งเฉพาะข้อความและหลักฐานที่คุณเลือกจากงานนี้ เพื่อให้ Pi ตรวจรับรองก่อนใช้ร่วมกัน</p></div>
      ${badge ? `<span class="pill ${badge[1]}">${escapeHTML(badge[0])}</span>` : ""}</div>
      <button type="button" class="ghost brain-toggle" data-brain-toggle aria-expanded="${record.open}">${record.open ? "ปิดรายละเอียด" : "ตรวจและเลือกข้อมูลที่จะแชร์"}</button>
      ${record.open ? `<div class="brain-share-body">${deliveryHTML(record)}
        ${record.loading ? `<p role="status">กำลังโหลดผลตรวจและหลักฐาน…</p>` : ""}
        ${record.error ? `<div class="task-feedback" role="alert">${escapeHTML(record.error)}</div>` : ""}
        ${record.eligibility && !record.eligibility.eligible ? policyHTML(record) : ""}
        ${record.eligibility?.eligible && (!delivery || delivery.state === "blocked" || delivery.state === "conflict") ? formHTML(record) : ""}
        <button type="button" class="ghost" data-brain-reload ${record.loading || record.busy ? "disabled" : ""}>โหลดผลตรวจใหม่</button>
      </div>` : ""}</section>`;
    root.querySelector("[data-brain-toggle]")?.addEventListener("click", () => {
      record.open = !record.open; paint(record);
      if (record.open && !record.eligibility && !record.loading) void loadEligibility(record);
      if (record.open && record.candidateID && !record.delivery) void refreshStatus(record);
    });
    root.querySelector("[data-brain-reload]")?.addEventListener("click", () => void loadEligibility(record));
    root.querySelector("[data-brain-status]")?.addEventListener("click", () => void refreshStatus(record));
    root.querySelector('[data-brain-share="project"]')?.addEventListener("click", () => void allowSharing(record,"project"));
    root.querySelector('[data-brain-share="task"]')?.addEventListener("click", () => void allowSharing(record,"task"));
    root.querySelectorAll("[data-brain-share-artifact]").forEach(button => button.addEventListener("click", () => void allowSharing(record,"artifact",button.dataset.brainShareArtifact)));
    const form = root.querySelector("[data-brain-form]");
    form?.addEventListener("submit", event => { event.preventDefault(); void preview(record); });
    form?.addEventListener("input", event => {
      const values = new FormData(form);
      record.formRevision++;
      record.solution = String(values.get("solution") || "");
      record.kind = String(values.get("verification_kind") || "test");
      if (event.target.name === "validation_id") {
        record.validationID = String(values.get("validation_id") || "");
        record.artifactIDs.clear(); record.preview = null; paint(record); return;
      }
      record.artifactIDs = new Set(values.getAll("artifact_ids").map(String));
      record.preview = null;
      root.querySelector(".brain-preview")?.remove();
      const button = root.querySelector("[data-brain-preview]");
      if (button) button.disabled = !readyForPreview(record) || Boolean(record.busy);
    });
    root.querySelector("[data-brain-submit]")?.addEventListener("click", () => void submit(record));
  }
  async function loadEligibility(record) {
    if (record.loading) return;
    record.loading = true; record.error = ""; record.preview = null; paint(record);
    try {
      record.eligibility = await api(`/api/tasks/${encodeURIComponent(record.task.id)}/project-brain-eligibility`);
      if (!record.eligibility?.task_id || !Array.isArray(record.eligibility.validations)) throw Error("ผลตรวจที่ได้รับไม่ครบ");
      const checks = record.eligibility.validations;
      if (!checks.some(item => item.id === record.validationID)) {
        record.validationID = checks[0]?.id || ""; record.artifactIDs.clear();
      }
    } catch (error) { record.error = `โหลดผลตรวจไม่ได้: ${error.message}`; record.eligibility = null; }
    finally { record.loading = false; paint(record); }
  }
  async function allowSharing(record, kind, artifactID = "") {
    const choice = kind === "artifact" ? selectedValidation(record)?.artifacts?.find(item => item.id === artifactID) : null;
    const revision = kind === "project" ? record.project?.sharing_revision : kind === "task" ? record.task.sharing_revision : choice?.sharing_revision;
    const objectID = kind === "project" ? record.project?.id : kind === "task" ? record.task.id : artifactID;
    if (!objectID || !Number.isInteger(revision)) { record.error = "ข้อมูลสิทธิ์แชร์ไม่ครบ กรุณาโหลดหน้าใหม่"; paint(record); return; }
    const label = kind === "project" ? "โปรเจกต์" : kind === "task" ? "งาน" : "หลักฐาน";
    const confirmed = await askAction({title:`อนุญาตแชร์${label}นี้?`,
      message:"ขั้นตอนนี้เปิดทางให้เลือกข้อมูลส่งออกเท่านั้น ยังไม่มีข้อความหรือไฟล์ถูกส่งไป Pi คุณจะได้ตรวจตัวอย่างและกดยืนยันอีกครั้ง",
      confirmLabel:"อนุญาตให้เลือกส่งออก"});
    if (!confirmed) return;
    record.busy = "share"; record.error = ""; paint(record);
    try {
      const payload = {visibility:"project_shared", export_policy:"explicit_selection", expected_revision:revision,
        actor:currentActor(), reason:"owner enabled explicit Project Brain candidate sharing"};
      const path = kind === "project" ? "/api/sharing/visibility" :
        kind === "task" ? `/api/tasks/${encodeURIComponent(objectID)}/sharing` :
        `/api/artifacts/${encodeURIComponent(objectID)}/sharing`;
      if (kind === "project") { payload.object_kind = "project"; payload.object_id = objectID; }
      await api(path,{method:kind === "project" ? "POST" : "PATCH",body:JSON.stringify(payload)});
      await load(); // refresh sharing revisions in every open surface
      await loadEligibility(record);
    } catch (error) { record.error = `อนุญาตแชร์ไม่สำเร็จ: ${error.message}`; }
    finally { record.busy = ""; paint(record); }
  }
  function selection(record) {
    return {task_id:record.task.id,validation_id:record.validationID,
      selected_artifact_ids:selectedArtifacts(record).map(item => item.id),
      solution:record.solution.trim(),verification_kind:record.kind};
  }
  async function preview(record) {
    if (!readyForPreview(record) || record.busy) return;
    const chosen = selection(record);
    const revision = record.formRevision;
    record.busy = "preview"; record.error = ""; record.preview = null; paint(record);
    try {
      const result = await api(`${endpoint}/preview`,{method:"POST",body:JSON.stringify(chosen)});
      if (!result?.candidate?.candidate_id || !result.approval) throw Error("ตัวอย่างที่ได้รับไม่ครบ");
      if (record.formRevision !== revision) throw Error("ข้อมูลที่เลือกเปลี่ยนแล้ว กรุณาตรวจตัวอย่างอีกครั้ง");
      record.preview = result;
    } catch (error) { record.error = `สร้างตัวอย่างไม่ได้: ${error.message}`; }
    finally { record.busy = ""; paint(record); }
  }
  async function submit(record) {
    if (!record.preview || record.busy) return;
    const previewed = record.preview;
    const chosen = selection(record);
    record.busy = "submit"; record.error = ""; paint(record);
    try {
      const result = await api(endpoint,{method:"POST",body:JSON.stringify({...chosen,approval:previewed.approval})});
      if (!result?.candidate_id || !result.state || result.candidate_id !== previewed.candidate.candidate_id)
        throw Error("Pi submission receipt does not match the reviewed candidate");
      record.candidateID = result.candidate_id; record.delivery = result;
      try { localStorage.setItem(statusKey(record.task.id),record.candidateID); } catch {}
    } catch (error) {
      // The response may be lost after a durable queue write. Check the exact
      // previewed candidate ID before offering a second send.
      const id = previewed.candidate?.candidate_id;
      if (id) {
        try {
          const found = await api(`${endpoint}/${encodeURIComponent(id)}`);
          if (found?.candidate_id === id) {
            record.candidateID = id; record.delivery = found;
            try { localStorage.setItem(statusKey(record.task.id),id); } catch {}
          } else throw Error("unexpected candidate status");
        } catch { record.error = `ยังยืนยันสถานะการส่งไม่ได้: ${error.message} ตรวจสถานะก่อนส่งซ้ำ`; }
      } else record.error = `ส่งข้อเสนอไม่ได้: ${error.message}`;
    } finally { record.busy = ""; paint(record); }
  }
  async function refreshStatus(record) {
    if (!record.candidateID || record.statusLoading) return;
    record.statusLoading = true; record.error = ""; paint(record);
    try {
      const result = await api(`${endpoint}/${encodeURIComponent(record.candidateID)}`);
      if (result?.candidate_id !== record.candidateID) throw Error("candidate status does not match the selected task");
      record.delivery = result;
    }
    catch (error) { record.error = `ตรวจสถานะไม่ได้: ${error.message}`; }
    finally { record.statusLoading = false; paint(record); }
  }
  function render(root, task, project, deps) {
    if (deps) configure(deps);
    if (!root || !task || task.state !== "completed") return;
    const record = recordFor(task);
    record.root = root; record.project = project;
    paint(record);
    if (record.candidateID && !record.delivery && !record.statusLoading) void refreshStatus(record);
  }
  window.HermetrixProjectBrain = {render};
})();
