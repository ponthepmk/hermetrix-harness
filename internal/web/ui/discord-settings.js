(() => {
  const endpoint = "/api/remote/discord";
  const idPattern = /^\d{17,20}$/;
  const stateLabels = {disabled:"ยังไม่เปิดใช้งาน",stopped:"หยุดเชื่อมต่อแล้ว",connecting:"กำลังเชื่อมต่อ…",ready:"เชื่อมต่อ Discord แล้ว",reconnecting:"กำลังเชื่อมต่อใหม่…",error:"เชื่อมต่อไม่สำเร็จ"};
  let snapshot = null, draft = null, root = null, busy = false, timer = null, generation = 0, statusVersion = 0;
  let feedback = "", failed = false;
  const localProviders = () => taskEligibleProviders({egress_policy:"local_only"});
  const profilesFor = providerID => {
    const provider = localProviders().find(item=>item.id===providerID);
    return provider ? availableProfiles(provider).filter(profile=>profileAdmission(provider,profile).admitted) : [];
  };
  function fromConfig(config = {}) {
    return {application_id:String(config.application_id || ""),guild_ids:(config.guild_ids || []).join("\n"),
      channel_ids:(config.channel_ids || []).join("\n"),user_ids:(config.user_ids || []).join("\n"),
      project_id:String(config.project_id || ""),provider_id:String(config.provider_id || ""),
      context_profile:String(config.context_profile || "compact-32k")};
  }
  function ids(value) { return [...new Set(String(value).trim().split(/[\s,]+/).filter(Boolean))]; }
  function configOf(value) {
    return {enabled:false,application_id:value.application_id.trim(),guild_ids:ids(value.guild_ids),channel_ids:ids(value.channel_ids),user_ids:ids(value.user_ids),
      project_id:value.project_id,provider_id:value.provider_id,context_profile:value.context_profile};
  }
  function validation(value) {
    const config = configOf(value);
    if (!idPattern.test(config.application_id)) return "Application ID ต้องเป็นตัวเลข 17–20 หลัก";
    for (const [name,label] of [["guild_ids","Server ID"],["channel_ids","Channel ID"],["user_ids","User ID"]]) {
      if (!config[name].length || config[name].length > 50 || config[name].some(id=>!idPattern.test(id))) return `${label} ต้องมี 1–50 รายการ เป็นตัวเลข 17–20 หลัก ไม่มี * หรือรายการว่าง`;
    }
    if (!state.projects.some(item=>item.id===config.project_id)) return "เลือกโปรเจกต์ที่จะให้ Discord เข้าถึง";
    if (!localProviders().some(item=>item.id===config.provider_id)) return "เลือกโมเดลบนเครื่องที่เปิดใช้งานและพร้อมเชื่อมต่อ";
    if (!profilesFor(config.provider_id).some(item=>item.name===config.context_profile)) return "เลือกขนาดบริบทที่โมเดลผ่านเงื่อนไขแล้ว ต้องตรวจสอบโมเดลใน Models ก่อนใช้ขนาดที่ใหญ่ขึ้น";
    return "";
  }
  function saved() {
    return snapshot && draft && JSON.stringify(configOf(draft)) === JSON.stringify(configOf(fromConfig(snapshot.config)));
  }
  function capture() {
    const form = root?.querySelector('[data-discord-scope]');
    if (!form || !draft) return;
    for (const name of Object.keys(draft)) {
      const field = form.elements.namedItem(name);
      if (field) draft[name] = field.value;
    }
  }
  function invite(value) {
    const id = String(value || "").trim();
    return idPattern.test(id) ? `https://discord.com/oauth2/authorize?client_id=${encodeURIComponent(id)}&scope=bot%20applications.commands&permissions=0&integration_type=0` : "";
  }
  function statusHTML() {
    const status = snapshot?.state;
    return `<strong>${escapeHTML(stateLabels[status] || "ยังตรวจสอบสถานะไม่ได้")}</strong>${snapshot?.last_error ? `<p class="form-note error">${escapeHTML(snapshot.last_error)}</p>` : ""}<small>Bot token: ${snapshot?.token_stored ? "บันทึกไว้แล้ว" : "ยังไม่บันทึก"}${snapshot?.bot_id ? ` · Bot ID ${escapeHTML(snapshot.bot_id)}` : ""} · งานที่กำลังทำ ${Number(snapshot?.active_requests) || 0}</small>`;
  }
  function update() {
    if (!root) return;
    const status = root.querySelector('[data-discord-status]');
    if (status) status.innerHTML = statusHTML();
    const notice = root.querySelector('[data-discord-feedback]');
    if (notice) { notice.textContent = feedback; notice.hidden = !feedback; notice.classList.toggle("error",failed); }
    const link = root.querySelector('[data-discord-invite]'), href = invite(draft?.application_id);
    if (link) { link.hidden = !href; if (href) link.href = href; else link.removeAttribute("href"); }
    const start = root.querySelector('[data-discord-start]');
    if (start) start.disabled = Boolean(busy || !snapshot?.token_stored || !draft || validation(draft) || !saved() || ["ready","connecting","reconnecting"].includes(snapshot?.state));
    const stop = root.querySelector('[data-discord-stop]');
    // A stale/unavailable status must never remove the user's disconnect action.
    if (stop) stop.disabled = Boolean(busy || !snapshot || (!snapshot.config?.enabled && snapshot.state === "disabled"));
    for (const button of root.querySelectorAll('[data-discord-save],[data-discord-save-token],[data-discord-refresh]')) button.disabled = busy;
  }
  function schedule() {
    clearTimeout(timer);
    if (!["connecting","ready","reconnecting"].includes(snapshot?.state)) return;
    timer = setTimeout(async()=>{
      if (!root?.isConnected || !root.getClientRects().length) return;
      if (!busy) {
        const version = statusVersion;
        try {
          const value = await api(endpoint);
          if (version !== statusVersion) return;
          snapshot = value;
        } catch {
          if (version !== statusVersion) return;
          feedback = "ตรวจสอบสถานะล่าสุดไม่ได้ กดตรวจสถานะอีกครั้ง"; failed = true; snapshot = {...snapshot,state:"unknown"};
        }
        update();
      }
      schedule();
    },3000);
  }
  async function saveScope(event) {
    event?.preventDefault();
    if (busy) return;
    capture();
    const problem = validation(draft);
    if (problem) { feedback = problem; failed = true; update(); return; }
    busy = true; ++statusVersion; feedback = "กำลังบันทึกขอบเขต…"; failed = false; update();
    try {
      await api(endpoint,{method:"PUT",body:JSON.stringify(configOf(draft))});
      snapshot = await api(endpoint);
      feedback = "บันทึกขอบเขตแล้ว การเชื่อมต่อยังหยุดอยู่ กดเชื่อมต่อเมื่อพร้อม";
    } catch (error) { feedback = `บันทึกขอบเขตไม่สำเร็จ: ${error.message} ข้อมูลที่กรอกยังอยู่`; failed = true; snapshot = {...snapshot,state:"unknown"}; }
    finally { busy = false; update(); schedule(); }
  }
  async function saveToken(event) {
    event?.preventDefault();
    if (busy) return;
    const field = root?.querySelector('[data-discord-token]');
    const token = field?.value.trim() || "";
    if (!token) { feedback = "กรอก Bot token แล้วกดบันทึก Token ช่องว่างจะไม่เปลี่ยน token เดิม"; failed = true; update(); return; }
    field.value = "";
    busy = true; ++statusVersion; feedback = "กำลังบันทึก Token…"; failed = false; update();
    try {
      await api(`${endpoint}/token`,{method:"PUT",body:JSON.stringify({token})});
      snapshot = await api(endpoint);
      feedback = "บันทึก Token แล้ว ช่องรหัสถูกล้าง การเชื่อมต่อยังหยุดอยู่";
    } catch { feedback = "บันทึก Bot token ไม่สำเร็จ ช่องรหัสถูกล้าง กรุณาตรวจ token แล้วลองใหม่"; failed = true; snapshot = {...snapshot,state:"unknown"}; }
    finally { busy = false; update(); schedule(); }
  }
  async function connection(action) {
    if (busy) return;
    capture();
    if (action === "start" && (validation(draft) || !snapshot?.token_stored || !saved())) {
      feedback = validation(draft) || "บันทึกขอบเขตและ Bot token ก่อนเชื่อมต่อ"; failed = true; update(); return;
    }
    busy = true; ++statusVersion; feedback = action === "start" ? "กำลังขอเชื่อมต่อ Discord…" : "กำลังหยุดเชื่อมต่อ…"; failed = false; update();
    try {
      await api(`${endpoint}/${action}`,{method:"POST",body:"{}"});
      snapshot = await api(endpoint);
      feedback = stateLabels[snapshot.state] || "ส่งคำขอแล้ว แต่ยังยืนยันสถานะไม่ได้";
    } catch (error) {
      feedback = `ดำเนินการไม่สำเร็จ: ${error.message}`; failed = true;
      try { snapshot = await api(endpoint); } catch { snapshot = {...snapshot,state:"unknown"}; }
    } finally { busy = false; update(); schedule(); }
  }
  function profileOptions() {
    return '<option value="">เลือกขนาดบริบท</option>'+profilesFor(draft.provider_id).map(item=>`<option value="${escapeHTML(item.name)}" ${item.name===draft.context_profile?"selected":""}>${escapeHTML(profileLabel(item))}</option>`).join("");
  }
  function paint() {
    const selectOptions = (items,selected,label) => `<option value="">${label}</option>`+items.map(item=>`<option value="${escapeHTML(item.id)}" ${item.id===selected?"selected":""}>${escapeHTML(item.name)}${item.model?` · ${escapeHTML(item.model)}`:""}</option>`).join("");
    root.innerHTML = `<section class="discord-settings"><header><p class="eyebrow">Remote access</p><h2>สั่งงานผ่าน Discord</h2><p>โมเดลประมวลผลบนเครื่อง แต่ข้อความ คำตอบ และตัวอย่างการแก้ไขที่ต้องอนุมัติจะส่งผ่าน Discord ใช้เฉพาะโปรเจกต์ ผู้ใช้ เซิร์ฟเวอร์ และช่องที่ระบุ คำตอบแสดงเฉพาะผู้สั่งคำสั่ง (ephemeral) ไม่รองรับ DM</p></header>
      <div class="discord-status" data-discord-status role="status"></div>
      <ol class="discord-steps"><li><strong>สร้างแอปและ Bot</strong><p>เปิด <a href="https://discord.com/developers/applications" target="_blank" rel="noopener noreferrer">Discord Developer Portal</a> → New Application คัดลอก Application ID จาก General Information แล้วเปิด Bot เพื่อสร้างหรือรีเซ็ต Bot token เก็บ token ไว้ในช่องด้านล่างเท่านั้น</p></li>
      <li><strong>กำหนดขอบเขตที่อนุญาต</strong><p>ใน Discord เปิด User Settings → Advanced → Developer Mode คลิกขวาเซิร์ฟเวอร์ ช่อง และบัญชีผู้ใช้เพื่อ Copy ID <a href="https://support.discord.com/hc/en-us/articles/206346498-Where-can-I-find-my-User-Server-Message-ID" target="_blank" rel="noopener noreferrer">ดูวิธีคัดลอก ID</a> ใส่หนึ่ง ID ต่อบรรทัดหรือคั่นด้วย comma ห้ามใช้ * เพื่อเปิดให้ทุกคน</p>
      <form class="discord-scope-form" data-discord-scope><label>Application ID<input name="application_id" inputmode="numeric" maxlength="20" required value="${escapeHTML(draft.application_id)}"></label>
      ${[["guild_ids","Server IDs ที่อนุญาต"],["channel_ids","Channel IDs ที่อนุญาต"],["user_ids","User IDs ที่อนุญาต"]].map(([name,label])=>`<label>${label}<textarea name="${name}" rows="2" required>${escapeHTML(draft[name])}</textarea></label>`).join("")}
      <label>โปรเจกต์<select name="project_id" required>${selectOptions(state.projects,draft.project_id,"เลือกโปรเจกต์")}</select></label>
      <label>โมเดลบนเครื่อง<select name="provider_id" required>${selectOptions(localProviders(),draft.provider_id,"เลือกโมเดลบนเครื่อง")}</select></label>
      <label>ขนาดบริบท<select name="context_profile" required>${profileOptions()}</select></label><p class="form-note neutral">แสดงเฉพาะโมเดล localhost ที่พร้อม และขนาดบริบทที่ผ่านเงื่อนไข หากไม่มีให้ตรวจโมเดลใน Models ก่อน</p><button type="submit" class="primary" data-discord-save>บันทึกขอบเขต</button></form></li>
      <li><strong>บันทึก Bot token</strong><form class="discord-token-form" data-discord-token-form autocomplete="off"><label>Bot token<input type="password" data-discord-token autocomplete="new-password" spellcheck="false" placeholder="ช่องว่างจะเก็บ token เดิมไว้"></label><button type="submit" class="ghost" data-discord-save-token>บันทึก Token</button></form><p class="form-note neutral">Token ไม่แสดงกลับในหน้านี้ การบันทึก token ใหม่จะหยุดการเชื่อมต่อเดิม</p></li>
      <li><strong>เชิญ Bot แล้วเชื่อมต่อ</strong><p><a data-discord-invite target="_blank" rel="noopener noreferrer" hidden>เปิดลิงก์เชิญ Bot เข้าเซิร์ฟเวอร์</a> ลิงก์ใช้สิทธิ์ bot และ applications.commands โดยไม่ขอสิทธิ์ Administrator</p><p>ใน Developer Portal เว้น Interactions Endpoint URL ว่าง เพื่อรับคำสั่งผ่าน Gateway ไม่ต้องเปิด Message Content Intent</p><p>หลังบันทึกครบและเชิญ Bot แล้ว กดเชื่อมต่อ รอให้สถานะเป็น “เชื่อมต่อ Discord แล้ว” จึงลองคำสั่ง <code>/hermetrix</code> ในช่องที่อนุญาต ต้องเปิด Hermetrix บนเครื่องนี้ไว้</p></li></ol>
      <p class="discord-feedback" data-discord-feedback role="status" hidden></p><div class="discord-actions action-row"><button class="primary" type="button" data-discord-start>เชื่อมต่อ Discord</button><button class="ghost" type="button" data-discord-stop>หยุดเชื่อมต่อ</button><button class="ghost" type="button" data-discord-refresh>ตรวจสถานะ</button></div></section>`;
    root.querySelector('[data-discord-scope]').addEventListener("submit",saveScope);
    root.querySelector('[data-discord-scope]').addEventListener("input",()=>{capture();update();});
    root.querySelector('[name="provider_id"]').addEventListener("change",()=>{
      capture(); const profiles = profilesFor(draft.provider_id);
      if (!profiles.some(item=>item.name===draft.context_profile)) draft.context_profile = profiles[0]?.name || "";
      root.querySelector('[name="context_profile"]').innerHTML = profileOptions();update();
    });
    root.querySelector('[data-discord-token-form]').addEventListener("submit",saveToken);
    root.querySelector('[data-discord-start]').addEventListener("click",()=>void connection("start"));
    root.querySelector('[data-discord-stop]').addEventListener("click",()=>void connection("stop"));
    root.querySelector('[data-discord-refresh]').addEventListener("click",()=>void render(root));
    update();
  }
  async function render(target) {
    capture(); root = target;
    const current = ++generation;
    const version = ++statusVersion;
    clearTimeout(timer);
    if (!draft) root.innerHTML = '<p class="form-note neutral" role="status">กำลังตรวจสอบการตั้งค่า Discord…</p>';
    try {
      const value = await api(endpoint);
      if (current !== generation || version !== statusVersion || root !== target) return;
      snapshot = value;
      feedback = ""; failed = false;
      if (!draft) draft = fromConfig(snapshot.config);
      paint();schedule();
    } catch (error) {
      if (current !== generation || version !== statusVersion) return;
      feedback = `โหลดสถานะ Discord ไม่สำเร็จ: ${error.message}`; failed = true;
      snapshot = {...snapshot,state:"unknown"};
      if (!draft) draft = fromConfig();
      paint();
    }
  }
  window.HermetrixDiscord = {render};
})();
