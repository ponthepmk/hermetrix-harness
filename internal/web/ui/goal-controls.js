/* Goal mutations use an exact retained request. Lost responses trigger reads,
   never a new Goal ID or an automatic mutation retry. */
(() => {
  "use strict";
  function create({task,api,actor,notify = () => {},changed = () => {},storage,makeID = () => `goal_ui_${crypto.randomUUID().replace(/-/g, "")}`}) {
    let disposed = false, readSequence = 0, reading = null;
    const key = `hermetrix.goal.pending.${task.id}`;
    const state = {goal:null,receipts:[],loading:true,pending:false,uncertain:false,error:""};
    let request = null;
    try { request = JSON.parse(storage?.getItem(key) || "null"); } catch { /* A broken client draft is not execution authority. */ }
    if (request) {
      let body;
      try { body=JSON.parse(request.body); } catch { /* Invalid cached drafts are never sent. */ }
      const expected=request.kind === "start" ? `/api/tasks/${encodeURIComponent(task.id)}/goals` : `/api/goals/${encodeURIComponent(request.id)}/${request.kind}`;
      if (request.taskID !== task.id || request.path !== expected || !body || !["start","stop","resume"].includes(request.kind) || request.kind === "start" && body.goal_id !== request.id) request=null;
    }
    if (request) state.uncertain = true;
    const snapshot = () => ({...state, goal:state.goal && {...state.goal}, receipts:state.receipts.slice()});
    const publish = () => { if (!disposed) notify(snapshot()); };
    function keep(value) {
      request = value && Object.freeze({...value,taskID:task.id});
      try { if (request) storage?.setItem(key,JSON.stringify(request)); else storage?.removeItem(key); } catch { /* The in-memory exact request is still retained. */ }
    }
    const knownOutcome = (candidate, value) => candidate && candidate.id === value.id && (
      value.kind === "start" ||
      value.kind === "stop" && ["stopped","stopping"].includes(candidate.state) ||
      value.kind === "resume" && candidate.state === "active" && candidate.expected_task_revision === JSON.parse(value.body).expected_task_revision);
    async function readStatus() {
      const list = await api(`/api/tasks/${encodeURIComponent(task.id)}/goals`);
      const goals = Array.isArray(list) ? list : [];
      const goal = request && goals.find(item => item.id === request.id) || goals.find(item => ["active","waiting","stopping"].includes(item.state)) || goals[0];
      if (!goal) return {goal:null,receipts:[]};
      const detail = await api(`/api/goals/${encodeURIComponent(goal.id)}`);
      if (detail.goal?.task_id !== task.id) throw new Error("ผลสถานะไม่ตรงกับงานนี้");
      return {goal:detail.goal,receipts:detail.actions || []};
    }
    async function refresh() {
      if (disposed || state.pending) return;
      if (reading) return reading;
      const sequence = ++readSequence;
      reading = (async () => {
        try {
          const observed = await readStatus();
          if (disposed || sequence !== readSequence) return;
          const previous = state.goal;
          Object.assign(state,observed,{loading:false,error:""});
          if (request && knownOutcome(state.goal,request)) { keep(null); state.uncertain=false; }
          if (state.uncertain) state.error = "คำขอเดิมยังไม่มีผลที่ยืนยันได้";
          publish();
          if (previous && state.goal && (previous.expected_task_revision !== state.goal.expected_task_revision || previous.state !== state.goal.state)) changed();
        } catch (error) {
          if (!disposed && sequence === readSequence) { state.loading=false;state.error=error.message;publish(); }
        } finally { reading = null; }
      })();
      return reading;
    }
    async function dispatch(value) {
      if (disposed || state.pending) return;
      state.pending=true;state.error="";state.uncertain=false;state.loading=false;
      ++readSequence;keep(value);publish();
      try {
        const goal = await api(value.path,{method:"POST",body:value.body});
        if (disposed) return;
        state.goal=goal;keep(null);state.uncertain=false;
      } catch (error) {
        if (disposed) return;
        state.error=error.message;state.uncertain=true;
        try {
          const detail = await api(`/api/goals/${encodeURIComponent(value.id)}`);
          if (!disposed && detail.goal?.task_id === task.id && knownOutcome(detail.goal,value)) {
            state.goal=detail.goal;state.receipts=detail.actions || [];keep(null);state.uncertain=false;state.error="";
          }
        } catch { /* Keep the exact request; the user may inspect or explicitly retry it. */ }
      } finally {
        state.pending=false;
        if (!disposed) { publish(); if (!state.uncertain) { await refresh(); changed(); } }
      }
    }
    return Object.freeze({snapshot,refresh,
      start(values) {
        if (state.pending || state.uncertain || !values.provider_id || !values.reviewer_provider_id || values.provider_id === values.reviewer_provider_id) return Promise.resolve(false);
        const id=makeID();
        return dispatch({kind:"start",id,path:`/api/tasks/${encodeURIComponent(task.id)}/goals`,body:JSON.stringify({goal_id:id,expected_task_revision:task.revision,
          actor:actor(),provider_id:values.provider_id,reviewer_provider_id:values.reviewer_provider_id,
          apply_changes:Boolean(values.apply_changes),max_actions:100,max_attempts:8,max_plans:3,max_seconds:3600,verification_timeout_seconds:120})});
      },
      stop() { if (!state.goal || state.uncertain) return Promise.resolve(false);return dispatch({kind:"stop",id:state.goal.id,path:`/api/goals/${encodeURIComponent(state.goal.id)}/stop`,body:"{}"}); },
      resume() { if (!state.goal || state.uncertain) return Promise.resolve(false);return dispatch({kind:"resume",id:state.goal.id,path:`/api/goals/${encodeURIComponent(state.goal.id)}/resume`,body:JSON.stringify({expected_task_revision:task.revision})}); },
      retry() { return request ? dispatch(request) : Promise.resolve(false); },
      destroy() { disposed=true;++readSequence; },
    });
  }

  function mount(host,{task,providers,api,actor,onChange}) {
    if (!host) return;
    host.__goalCleanup?.();
    const client = create({task,api,actor,storage:window.sessionStorage,changed:onChange,notify:render});
    function render(state) {
      if (!host.isConnected) return;
      host.innerHTML = window.HermetrixGoalView.render({task,providers,...state});
      host.querySelector("[data-goal-start]")?.addEventListener("submit",event => {
        event.preventDefault();const data=new FormData(event.currentTarget);
        const reviewer=event.currentTarget.elements.namedItem("reviewer_provider_id");
        reviewer.setCustomValidity(data.get("provider_id")===data.get("reviewer_provider_id") ? "เลือกโปรไฟล์ตรวจผลคนละโปรไฟล์กับโมเดลทำงาน" : "");
        if (!event.currentTarget.reportValidity()) return;
        void client.start({provider_id:data.get("provider_id"),reviewer_provider_id:data.get("reviewer_provider_id"),apply_changes:data.has("apply_changes")});
      });
      host.querySelector("[data-goal-start]")?.addEventListener("change",event => event.currentTarget.elements.namedItem("reviewer_provider_id").setCustomValidity(""));
      host.querySelectorAll("[data-goal-action]").forEach(button => button.addEventListener("click",() => {
        void client[button.dataset.goalAction === "refresh" ? "refresh" : button.dataset.goalAction]?.();
      }));
    }
    const timer=setInterval(() => {
      if (!host.isConnected) { cleanup();return; }
      if (["active","waiting","stopping"].includes(client.snapshot().goal?.state) || client.snapshot().uncertain) void client.refresh();
    },2000);
    function cleanup() { clearInterval(timer);client.destroy(); }
    host.__goalCleanup=cleanup;
    render(client.snapshot());void client.refresh();
  }
  const exported = Object.freeze({create,mount});
  if (typeof module !== "undefined" && module.exports) module.exports=exported;
  if (typeof window !== "undefined") window.HermetrixGoals=exported;
})();
