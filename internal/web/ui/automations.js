/* Scheduled intake creates reviewable tasks. It never grants tool authority. */
(() => {
  async function mount(root) {
    root.querySelector('[data-automations]')?.remove();
    const section = document.createElement('section');
    section.className = 'panel'; section.dataset.automations = '';
    section.innerHTML = `<h3>งานตามกำหนดเวลา</h3><p>สร้างงานรอตรวจแผนในโครงการที่เลือก · ไม่เริ่มแก้ไฟล์เอง</p>
      <p data-auto-status role="status" aria-live="polite">กำลังโหลด…</p>
      <form data-auto-form><label>ชื่อกำหนดการ<input name="name" maxlength="120" required></label>
      <label>โครงการ<select name="project" required><option value="">เลือกโครงการ</option></select></label>
      <label>งานที่ต้องการ<textarea name="objective" maxlength="6000" rows="3" required></textarea></label>
      <label>ผลที่ต้องตรวจยืนยัน<input name="criterion" maxlength="1000" required></label>
      <label>รอบเวลา<select name="interval"><option value="86400">ทุก 24 ชั่วโมง</option><option value="604800">ทุก 7 วัน</option><option value="3600">ทุกชั่วโมง</option><option value="0">เมื่อกดสร้างงานหรือรับเหตุการณ์เท่านั้น</option></select></label>
      <p>แสดงเวลาตามกรุงเทพฯ · หากงานก่อนหน้ายังไม่จบ ระบบจะข้ามรอบนั้น</p>
      <button type="submit" class="primary">บันทึกกำหนดการ</button></form><div data-auto-list class="card-list"></div>`;
    root.prepend(section);
    const status = section.querySelector('[data-auto-status]');
    const list = section.querySelector('[data-auto-list]');
    const form = section.querySelector('form');
    let loading = false;
    const tell = text => { if(section.isConnected) status.textContent = text; };
    const request = (path, body) => api(path, {method:'POST', body:JSON.stringify(body)});
    async function refresh() {
      if(loading || !section.isConnected) return;
      loading = true;
      try {
        const schedules = await api('/api/automations');
        if(!section.isConnected) return;
        list.replaceChildren();
        for(const schedule of schedules) {
          const card=document.createElement('article'); card.className='provider-card';
          const title=document.createElement('h4'); title.textContent=schedule.name;
          const description=document.createElement('p');
          description.textContent=schedule.enabled ? (schedule.interval_seconds ? `รอบถัดไป ${new Date(schedule.next_run_at).toLocaleString('th-TH',{timeZone:'Asia/Bangkok'})}` : 'รอรับเหตุการณ์') : 'พักกำหนดการแล้ว';
          const actions=document.createElement('div'); actions.className='action-row';
          const toggle=document.createElement('button'); toggle.type='button'; toggle.className='ghost'; toggle.textContent=schedule.enabled?'พักกำหนดการ':'เปิดกำหนดการ';
          toggle.onclick=async()=>{toggle.disabled=true;try{await request('/api/automations',{id:schedule.id,revision:schedule.revision,name:schedule.name,template:schedule.template,interval_seconds:schedule.interval_seconds,timezone:schedule.timezone,enabled:!schedule.enabled});await refresh();tell('บันทึกสถานะแล้ว');}catch(error){tell(error.message);toggle.disabled=false;}};
          const trigger=document.createElement('button'); trigger.type='button';trigger.className='ghost';trigger.textContent='สร้างงานตอนนี้';trigger.disabled=!schedule.enabled;
          // An uncertain response retains the same request ID; explicit retry
          // cannot accidentally create another task for that click.
          let requestID=null;
          trigger.onclick=async()=>{trigger.disabled=true;requestID ||= crypto.randomUUID();try{const run=await request(`/api/automations/${encodeURIComponent(schedule.id)}/trigger`,{request_id:requestID});tell(run.state==='skipped_overlap'?'ข้ามรอบนี้ เพราะงานก่อนหน้ายังไม่จบ':'สร้างงานแล้ว · เปิดหน้าแผนเพื่อตรวจและดำเนินการ');requestID=null;trigger.disabled=false;}catch(error){tell(`ยังยืนยันผลไม่ได้: ${error.message} · กดอีกครั้งเพื่อตรวจคำขอเดิม`);trigger.disabled=false;}};
          const history=document.createElement('button');history.type='button';history.className='ghost';history.textContent='ดูประวัติ';
          const results=document.createElement('div');
          history.onclick=async()=>{history.disabled=true;try{const runs=await api(`/api/automations/${encodeURIComponent(schedule.id)}/runs`);results.replaceChildren();for(const run of runs){const row=document.createElement('p');row.textContent=`${new Date(run.created_at).toLocaleString('th-TH',{timeZone:'Asia/Bangkok'})} · ${run.state==='skipped_overlap'?'ข้ามงานซ้อน':'รอตรวจแผน'}`;results.append(row);}if(!runs.length)results.textContent='ยังไม่มีการสร้างงาน';}catch(error){tell(error.message);}finally{history.disabled=false;}};
          actions.append(trigger,toggle,history);card.append(title,description,actions,results);list.append(card);
        }
        tell(schedules.length?'โหลดกำหนดการแล้ว':'ยังไม่มีกำหนดการ สร้างรายการแรกด้านบนได้');
      } catch(error){tell(`โหลดไม่ได้: ${error.message}`);} finally{loading=false;}
    }
    try {
      const projects=await api('/api/projects');if(!section.isConnected)return;
      for(const project of projects){const option=document.createElement('option');option.value=project.id;option.textContent=project.name;form.elements.project.append(option);}
      form.querySelector('button').disabled=!projects.length;
      if(!projects.length)tell('เพิ่มโครงการก่อนสร้างกำหนดการ');
    }catch(error){tell(error.message);form.querySelector('button').disabled=true;}
    let createRequest=null;
    form.onsubmit=async event=>{event.preventDefault();const button=form.querySelector('button');button.disabled=true;const data=new FormData(form);
      try{const objective=String(data.get('objective')).trim();createRequest ||= {request_id:crypto.randomUUID(),name:data.get('name'),enabled:true,interval_seconds:Number(data.get('interval')),timezone:'Asia/Bangkok',template:{project_id:data.get('project'),title:data.get('name'),objective,original_request:objective,criteria:[{id:'result',description:data.get('criterion')}]}};await request('/api/automations',createRequest);createRequest=null;form.reset();await refresh();tell('บันทึกแล้ว · งานที่สร้างจะรอตรวจแผน');}catch(error){tell(`${error.message} · ส่งซ้ำจะตรวจคำขอเดิม`);}finally{button.disabled=false;}};
    await refresh();
  }
  window.HermetrixAutomations={mount};
})();
