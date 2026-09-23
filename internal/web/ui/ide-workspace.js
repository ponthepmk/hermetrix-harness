/* Project-scoped IDE actions. Formatting changes a buffer; only Save writes it.
 * Run/Test use bounded command jobs, independently of the interactive terminal. */
(() => {
  const runs = new Map(), debuggers = new Map(), breakpoints = new Map(), capabilities = new Map();
  const polling = new Map(), busy = new Set();
  const restoredOutput = new Set();
  let formatterLoading;
  const extension = path => path.split(".").pop().toLowerCase();
  const projectKey = () => state.currentProject?.id;
  const relativeDir = path => path.includes("/") ? path.slice(0, path.lastIndexOf("/")) : ".";
  const live = value => ["queued", "starting", "running", "paused"].includes(value?.state);
  const pane = kind => workspacePaneVisible(kind) ? document.querySelector(`.pane:not(.pane-hidden) .pane-body[data-pane-kind="${kind}"]`) : null;
  const canFormat = path => /\.(go|[cm]?[jt]sx?|json5?|html?|vue|css|scss|less|md|markdown|mdx|ya?ml)$/i.test(path);
  function supports(action, path) {
    if (action === "format") return canFormat(path);
    if (action === "debug") return /\.(go|[cm]?js)$/i.test(path);
    return /\.(go|[cm]?js|py|mod)$/i.test(path);
  }
  function feedback(message, error = false) {
    const box = document.querySelector("#codeFeedback");
    if (box) { box.textContent = message; box.hidden = !message; box.classList.toggle("error", error); }
  }
  function dirty() {
    const doc = state.projectFile;
    if (!doc) return;
    const changed = doc.content !== doc.originalContent;
    const label = document.querySelector("#codeSaveState");
    if (label) label.textContent = changed ? "Unsaved" : "Saved";
    const tab = [...document.querySelectorAll("[data-code-tab]")].find(button => button.dataset.codeTab === doc.path);
    if (tab) { tab.querySelector("i")?.remove(); if (changed) { const dot = document.createElement("i"); dot.textContent = "•"; dot.setAttribute("aria-label", "Unsaved"); tab.append(dot); } }
  }
  function open(consoleKind) {
    captureCodeDraft();
    if (state.view !== "code") switchView("code");
    collapseZone("rail", true);
    const side = ["files", "chat", "git", "environment"].includes(state.panes[1]) ? state.panes[1] : "files";
    state.panes = ["editor", side];
    if (consoleKind) state.panes.push(consoleKind);
    state.paneLayout = "ide";
    state.maximisedPane = null;
    state.compactPane = "editor";
    renderPanes(); saveLayout();
  }
  function panel(kind) {
    if (["files", "chat", "git", "environment"].includes(kind)) {
      if (state.view !== "code" || state.paneLayout !== "ide") open();
      state.panes[1] = kind;
      state.compactPane = window.innerWidth <= 900 ? kind : "editor";
      renderPanes(); saveLayout();
    } else if (["output", "debug", "terminal"].includes(kind)) open(kind);
    else openContentPane(kind);
  }
  async function ensureFormatter() {
    if (window.HermetrixFormatter) return window.HermetrixFormatter;
    if (!formatterLoading) formatterLoading = new Promise((resolve, reject) => {
      const script = document.createElement("script"); script.src = "/vendor/formatter.js";
      script.onload = () => resolve(window.HermetrixFormatter);
      script.onerror = () => { script.remove(); formatterLoading = null; reject(new Error("Cannot load formatter. Try again.")); };
      document.head.append(script);
    });
    return formatterLoading;
  }
  async function format(doc) {
    const editor = activeCodeEditor, key = codeDraftKey(doc.projectID, doc.path), before = editor.getValue();
    feedback("Formatting…");
    const content = extension(doc.path) === "go"
      ? (await api(`/api/projects/${encodeURIComponent(doc.projectID)}/ide/format`, {method:"POST", body:JSON.stringify({path:doc.path, content:before})})).content
      : await (await ensureFormatter()).format(before, doc.path);
    if (activeCodeEditor !== editor || editor.documentKey !== key || editor.getValue() !== before) {
      feedback("The buffer changed while formatting. Format again to keep your latest edits.", true); return false;
    }
    if (!editor.setValue) throw new Error("Reload the rich editor before formatting.");
    editor.setValue(content);
    captureCodeDraft(); dirty();
    feedback(content === before ? "Already formatted." : "Formatted · Ctrl+Z to undo · Save to write to disk.");
    return true;
  }
  function command(action, doc, tools = {}) {
    const ext = extension(doc.path), dir = relativeDir(doc.path);
    if (ext === "go" || ext === "mod") return {executable:"go", arguments: action === "test" ? ["test", "./..."] : ext === "mod" ? ["build", "./..."] : /^\s*package\s+main\b/m.test(doc.content) ? ["run", dir === "." ? "." : `./${dir}`] : ["test", dir === "." ? "." : `./${dir}`]};
    if (["js", "mjs", "cjs"].includes(ext)) return {executable:"node", arguments:action === "test" ? ["--test"] : [`./${doc.path}`]};
    if (ext === "py") return {executable:tools.python3 ? "python3" : "python", arguments:action === "test" ? ["-m", "unittest", "discover"] : [`./${doc.path}`]};
    throw new Error("Use Terminal for this project's custom build toolchain.");
  }
  async function action(kind) {
    captureCodeDraft();
    const doc = state.projectFile;
    if (!doc || doc.projectID !== projectKey()) return;
    const key = `${doc.projectID}:${["run", "test"].includes(kind) ? "command" : kind}`;
    if (busy.has(key)) return;
    busy.add(key);
    try {
      if (kind === "format") { await format(doc); return; }
      if (kind === "debug") { panel("debug"); return; }
      if (live(runs.get(doc.projectID)?.job)) throw new Error("A command is already running. Stop it or wait for its result.");
      if (doc.content !== doc.originalContent && !await saveWorkbenchFile()) return;
      // Saving can finish after a project/file change or more typing.
      captureCodeDraft();
      if (projectKey() !== doc.projectID || state.projectFile?.path !== doc.path || state.projectFile.content !== state.projectFile.originalContent) {
        throw new Error("The editor changed while saving. Run again after saving the latest buffer.");
      }
      const caps = await api(`/api/projects/${encodeURIComponent(doc.projectID)}/ide`);
      const input = command(kind, state.projectFile, caps.tools);
      if (!caps.tools?.[input.executable]) throw new Error(`${input.executable} is not installed or is missing from the server PATH. Install it and restart Hermetrix.`);
      const job = await api(`/api/projects/${encodeURIComponent(doc.projectID)}/commands`, {method:"POST", body:JSON.stringify({...input, actor:currentActor(), working_dir:".", timeout_seconds:300})});
      runs.set(doc.projectID, {job, label:[input.executable, ...input.arguments].join(" "), action:kind, path:doc.path});
      if (projectKey() === doc.projectID) panel("output");
    } catch (error) { feedback(error.message, true); toast(error.message, true); }
    finally { busy.delete(key); }
  }
  function schedule(key, callback, delay = 700) {
    clearTimeout(polling.get(key));
    polling.set(key, setTimeout(() => { polling.delete(key); void callback(); }, delay));
  }
  function resultText(job) {
    return String(job?.result?.output || "") + (job?.error ? `\n${job.error}` : "");
  }
  function diagnostics(output) {
    const result = [], seen = new Set();
    for (const line of output.split("\n")) {
      const match = line.match(/(?:^|\s)(?:\.\/)?([^\s():]+\.(?:go|[cm]?[jt]sx?|py)):(\d+)(?::(\d+))?(.*)/);
      if (!match || result.length >= 80 || seen.has(match[0])) continue;
      seen.add(match[0]); result.push({path:match[1].replaceAll("\\", "/"), line:Number(match[2]), text:match[0].trim()});
    }
    return result;
  }
  async function goTo(path, line) {
    const projectID=projectKey();
    const root = state.currentProject?.root_path?.replaceAll("\\", "/");
    if (root && path.toLowerCase().startsWith(`${root.toLowerCase()}/`)) path = path.slice(root.length + 1);
    await openWorkbenchFile(path);
    if (projectKey()===projectID && state.projectFile?.path===path) activeCodeEditor?.goToLine(line);
  }
  function renderOutput(body) {
    const id = projectKey(), run = runs.get(id);
    if (!run) { body.innerHTML = `<div class="ide-empty"><h3>Run &amp; test</h3><p>Open a Go, JavaScript or Python file and choose Run or Test. Output, errors and the exit code appear here.</p><p>Run saves this file first. Save other edited files before project-wide tests. Use Terminal for custom commands.</p></div>`;
      if (!restoredOutput.has(id)) { restoredOutput.add(id); void api('/api/jobs').then(jobs=>{
        if(runs.has(id)) return;
        const own=jobs.filter(job=>job.kind==='command' && job.payload?.project_id===id);
        const job=own.find(live) || own[0];
        if(job) { runs.set(id,{job,label:[job.payload.executable,...(job.payload.arguments || [])].join(' ')}); if(body.isConnected && projectKey()===id) renderOutput(body); }
      }).catch(error=>{ if(body.isConnected) body.querySelector('.ide-empty').append(document.createTextNode(` Could not restore previous output: ${error.message}`)); }); }
      return; }
    const job = run.job, output = resultText(job), problems = diagnostics(output.slice(-131072));
    body.innerHTML = `<div class="ide-output"><div class="ide-console-toolbar"><code>${escapeHTML(run.label)}</code><strong data-job-state>${escapeHTML(job.state)}</strong>${live(job) ? `<button class="danger" data-job-stop>Stop</button>` : `<span>Exit ${escapeHTML(String(job.result?.exit_code ?? "—"))}</span>${run.path ? `<button class="ghost" data-job-again>Run again</button>` : ""}`}</div><div data-job-problems>${problems.map((problem, index) => `<button class="ide-problem" data-problem="${index}">${escapeHTML(problem.text)}</button>`).join("")}</div><pre class="ide-console-output" tabindex="0" aria-label="Command output">${escapeHTML(stripANSI(output.slice(-131072)) || (live(job) ? "Waiting for output…" : "No output."))}</pre>${output.length > 131072 ? `<p>Showing the last 128 KiB. Full captured output is retained in the job receipt, up to 2 MiB.</p>` : ""}${job.result?.truncated ? `<p>Process output exceeded the 2 MiB capture limit.</p>` : ""}<div class="ide-console-footer"><span>Project: ${escapeHTML(state.currentProject?.name || "")}</span><button class="ghost" data-explain-output>Ask local AI about this result</button></div></div>`;
    body.querySelector("[data-job-stop]")?.addEventListener("click", async () => {
      try { await api(`/api/jobs/${encodeURIComponent(job.id)}/cancel`, {method:"POST", body:JSON.stringify({actor:currentActor()})}); await pollOutput(id); } catch (error) { toast(error.message,true); }
    });
    body.querySelector("[data-job-again]")?.addEventListener("click", async () => { if (state.projectFile?.path !== run.path) await openWorkbenchFile(run.path); if (projectKey() === id && state.projectFile?.path === run.path) await action(run.action); });
    body.querySelector("[data-explain-output]")?.addEventListener("click", () => window.HermetrixAssistant?.action("output", `${run.label}\nState: ${job.state}\n${output.slice(-16000)}`));
    body.querySelectorAll("[data-problem]").forEach(button => button.addEventListener("click", () => { const problem = problems[Number(button.dataset.problem)]; void goTo(problem.path, problem.line); }));
    if (live(job)) schedule(`job:${id}`, () => pollOutput(id));
  }
  async function pollOutput(id) {
    if (projectKey() !== id || !pane("output") || document.hidden) return;
    const run = runs.get(id); if (!run || !live(run.job)) return;
    try {
      const updated = await api(`/api/projects/${encodeURIComponent(id)}/ide/jobs/${encodeURIComponent(run.job.id)}`);
      if (runs.get(id) !== run) return;
      const changed = updated.state !== run.job.state || resultText(updated) !== resultText(run.job);
      run.job = updated;
      const body = projectKey() === id && pane("output");
      if (body && changed) { const scroll = body.querySelector("pre")?.scrollTop || 0; renderOutput(body); const pre = body.querySelector("pre"); if (pre) pre.scrollTop = scroll; }
      if (live(updated) && body) schedule(`job:${id}`, () => pollOutput(id), changed ? 700 : 1800);
    } catch (error) { const body = pane("output"); if (body && projectKey() === id) { const status=body.querySelector("[data-job-state]"); if (status) status.textContent=`Connection error: ${error.message} · retrying`; schedule(`job:${id}`, () => pollOutput(id), 3000); } }
  }
  function allBreakpoints(id) {
    const result = [];
    for (const [key, lines] of breakpoints) { const [projectID, path] = JSON.parse(key); if (projectID === id) for (const line of lines) result.push({path, line}); }
    return result;
  }
  function editorMounted(doc) {
    activeCodeEditor?.setBreakpoints?.([...(breakpoints.get(codeDraftKey(doc.projectID, doc.path)) || [])]);
    const debug = debuggers.get(doc.projectID)?.session;
    const frame = debug?.state === "paused" ? debug.stack?.[0] : null;
    activeCodeEditor?.setExecutionLine?.(frame?.path === doc.path ? frame.line : null);
  }
  async function breakpoint(doc, line, enabled) {
    const key=codeDraftKey(doc.projectID, doc.path), lines=breakpoints.get(key) || new Set();
    if (enabled) lines.add(line); else lines.delete(line);
    breakpoints.set(key, lines);
    const record = debuggers.get(doc.projectID);
    if (live(record?.session)) {
      const sessionID = record.session.id;
      try { const updated = await api(`/api/debug/sessions/${encodeURIComponent(sessionID)}/breakpoints`, {method:"PUT", body:JSON.stringify({breakpoints:allBreakpoints(doc.projectID)})}); if (record.session.id === sessionID) record.session.breakpoints = updated.breakpoints; }
      catch (error) { record.error = error.message; toast(error.message,true); }
    }
    if (pane("debug")) renderDebug(pane("debug"));
  }
  async function loadDebugCapabilities(id, body) {
    if (capabilities.has(id)) return;
    capabilities.set(id, {loading:true});
    try {
      capabilities.set(id, await api("/api/debug/capabilities"));
      const sessions=await api(`/api/debug/sessions?project_id=${encodeURIComponent(id)}`);
      const restored=sessions.find(live);
      if(restored && !debuggers.get(id)?.busy && !debuggers.get(id)?.session) debuggers.set(id,{session:restored,frame:"",variables:[]});
    }
    catch (error) { capabilities.set(id, {error:error.message}); }
    if (body.isConnected && projectKey() === id) renderDebug(body);
  }
  async function startDebug(body) {
    captureCodeDraft();
    const doc = state.projectFile, id = projectKey();
    if (!doc || doc.projectID !== id) { toast("Open the saved program you want to debug first.", true); return; }
    const record = debuggers.get(id) || {};
    if (record.busy || live(record.session)) return;
    record.busy=true; record.error=""; debuggers.set(id,record);
    try {
      if (doc.content !== doc.originalContent && !await saveWorkbenchFile()) return;
      captureCodeDraft();
      if (projectKey() !== id || state.projectFile?.path !== doc.path || state.projectFile.content !== state.projectFile.originalContent) throw new Error("Save the latest buffer before debugging.");
      const runtime=extension(doc.path)==="go" ? "go" : "node";
      if (!capabilities.get(id)?.runtimes?.some(item=>item.id===runtime && item.available)) throw new Error(`${runtime} debugging is unavailable. See runtime setup below.`);
      record.session=await api("/api/debug/sessions", {method:"POST", body:JSON.stringify({project_id:id, runtime, program:doc.path, args:[], breakpoints:allBreakpoints(id)})});
      record.frame=""; record.variables=[];
    } catch(error) { record.error=error.message; }
    finally { record.busy=false; if (body.isConnected && projectKey()===id) renderDebug(body); }
  }
  async function control(action, body) {
    const id=projectKey(), record=debuggers.get(id); if (!record?.session || record.busy) return;
    record.busy=true; record.error="";
    const sessionID=record.session.id;
    try { const updated=await api(`/api/debug/sessions/${encodeURIComponent(sessionID)}/control`, {method:"POST",body:JSON.stringify({action})}); if (record.session.id === sessionID) { record.session=updated; record.frame=""; record.variables=[]; } }
    catch(error) { record.error=error.message; }
    finally { record.busy=false; if (body.isConnected && projectKey()===id) renderDebug(body); }
  }
  async function selectFrame(id, frame, navigate = true) {
    const record = debuggers.get(id), session=record?.session;
    if (!session || session.state !== "paused") return;
    record.frame=frame.id;
    const pauseVersion=session.updated_at;
    try {
      const result=await api(`/api/debug/sessions/${encodeURIComponent(session.id)}/variables?frame_id=${encodeURIComponent(frame.id)}`);
      if (record.session.id !== session.id || record.session.updated_at !== pauseVersion || record.frame !== frame.id || record.session.state !== "paused") return;
      record.variables=Array.isArray(result) ? result : result.variables || [];
      if (projectKey()===id && navigate && frame.path) { await goTo(frame.path,frame.line); if(projectKey()===id && state.projectFile?.path===frame.path && record.session.id===session.id) activeCodeEditor?.setExecutionLine?.(frame.line); }
      if (projectKey()===id && pane("debug")) renderDebug(pane("debug"));
    } catch(error) { record.error=error.message; if (pane("debug")) renderDebug(pane("debug")); }
  }
  function renderDebug(body) {
    const id=projectKey(), caps=capabilities.get(id), record=debuggers.get(id) || {}, session=record.session;
    const paused=session?.state==="paused", running=session?.state==="running", isLive=live(session);
    const doc=state.projectFile, points=allBreakpoints(id);
    const buttons=[["continue","Continue",paused],["pause","Pause",running],["step_over","Step over",paused],["step_in","Step in",paused],["step_out","Step out",paused],["stop","Stop",isLive]];
    body.innerHTML=`<div class="ide-debug"><div class="ide-console-toolbar"><strong>${escapeHTML(session?.state || "Ready to debug")}</strong><button class="primary" data-debug-start ${isLive || record.busy || !doc || !supports("debug",doc.path) ? "disabled" : ""}>Start debugging</button>${buttons.map(([action,label,enabled])=>`<button class="ghost" data-debug-action="${action}" ${enabled && !record.busy ? "" : "disabled"}>${label}</button>`).join("")}</div><p class="ide-debug-program">${escapeHTML(session?.program || doc?.path || "Open a Go or JavaScript program first.")}</p>${record.error || session?.error ? `<p class="session-error" role="alert">${escapeHTML(record.error || session.error)}</p>` : ""}${session?.reason ? `<p role="status">${escapeHTML(session.reason)}</p>` : ""}<div class="ide-debug-grid"><section><h4>Call stack</h4>${(session?.stack || []).map((frame,index)=>`<button class="ide-stack-frame ${record.frame===frame.id ? "active" : ""}" data-debug-frame="${index}"><strong>${escapeHTML(frame.name || "anonymous")}</strong><small>${escapeHTML(frame.path)}:${frame.line}</small></button>`).join("") || `<p>${isLive ? "Waiting for a pause…" : "Start to stop at the entry point or a breakpoint."}</p>`}</section><section><h4>Variables</h4>${paused && record.variables?.length ? `<dl class="ide-variables">${record.variables.map(variable=>`<dt title="${escapeHTML(variable.type || "")}">${escapeHTML(variable.name)}</dt><dd>${escapeHTML(String(variable.value))}</dd>`).join("")}</dl>` : `<p>${paused ? "Select a stack frame to inspect its local variables." : "Values appear when execution pauses."}</p>`}</section></div><details><summary>Breakpoints (${points.length}) · click the gutter beside a line number</summary>${points.map(point=>`<div>${escapeHTML(point.path)}:${point.line}${session?.breakpoints?.some(item=>item.path===point.path && item.line===point.line && item.verified) ? " ✓" : ""}</div>`).join("") || `<p>No breakpoints set.</p>`}</details><details ${session?.output ? "open" : ""}><summary>Program output</summary><pre class="ide-console-output">${escapeHTML(stripANSI(session?.output || "No output yet."))}</pre></details><details ${!session ? "open" : ""}><summary>Debugger setup</summary>${caps?.error ? `<p class="session-error">${escapeHTML(caps.error)}</p><button class="ghost" data-debug-retry>Retry</button>` : (caps?.runtimes || []).map(runtime=>`<p><strong>${escapeHTML(runtime.label || runtime.id)}</strong> · ${runtime.available ? "Ready" : escapeHTML(runtime.reason || "Not installed")}</p>`).join("") || `<p>Checking installed runtimes…</p>`}<p>Runs saved local code. Save other edited files first. Program writes and network requests run with your local account permissions.</p>${(caps?.limitations || []).map(value=>`<p>${escapeHTML(value)}</p>`).join("")}</details></div>`;
    body.querySelector("[data-debug-start]")?.addEventListener("click",()=>startDebug(body));
    body.querySelector("[data-debug-retry]")?.addEventListener("click",()=>{capabilities.delete(id); void loadDebugCapabilities(id,body);});
    body.querySelectorAll("[data-debug-action]").forEach(button=>button.addEventListener("click",()=>control(button.dataset.debugAction,body)));
    body.querySelectorAll("[data-debug-frame]").forEach(button=>button.addEventListener("click",()=>selectFrame(id,session.stack[Number(button.dataset.debugFrame)])));
    if (!caps) void loadDebugCapabilities(id,body);
    if (isLive) schedule(`debug:${id}`,()=>pollDebug(id),paused ? 900 : 450);
    if (doc) editorMounted(doc);
    if (paused && session.stack?.length && !record.frame) void selectFrame(id,session.stack[0],false);
  }
  async function pollDebug(id) {
    if (projectKey()!==id || !pane("debug") || document.hidden) return;
    const record=debuggers.get(id); if (!record?.session || !live(record.session)) return;
    const requestedSession=record.session;
    const sessionID=record.session.id;
    try {
      const session=await api(`/api/debug/sessions/${encodeURIComponent(sessionID)}`);
      if (record.session !== requestedSession || record.session.id !== sessionID) return;
      if (record.busy) { schedule(`debug:${id}`,()=>pollDebug(id)); return; }
      const changed=JSON.stringify(session)!==JSON.stringify(record.session);
      if (session.state!=="paused" || JSON.stringify(session.stack)!==JSON.stringify(record.session.stack)) { record.frame=""; record.variables=[]; }
      record.session=session;
      if (projectKey()===id && pane("debug")) { if (changed) renderDebug(pane("debug")); else if(live(session)) schedule(`debug:${id}`,()=>pollDebug(id),session.state==="paused" ? 900 : 450); }
    } catch(error) { record.error=error.message; if (projectKey()===id && pane("debug")) { renderDebug(pane("debug")); schedule(`debug:${id}`,()=>pollDebug(id),3000); } }
  }
  function resume() { if (!document.hidden) { const id=projectKey(); if(pane("output")) schedule(`job:${id}`,()=>pollOutput(id),50); if(pane("debug")) schedule(`debug:${id}`,()=>pollDebug(id),50); } }
  document.addEventListener("visibilitychange",resume);
  window.addEventListener("beforeunload",event=>{ captureCodeDraft(); if([...codeDrafts.values()].some(doc=>doc.content!==doc.originalContent)) { event.preventDefault(); event.returnValue=""; } });
  document.addEventListener("keydown",event=>{
    if (event.defaultPrevented || !state.projectFile || !document.querySelector("#workbenchFileForm")) return;
    if (event.altKey && event.shiftKey && event.key.toLowerCase()==="f") { event.preventDefault(); void action("format"); }
    if (event.key==="F5") { event.preventDefault(); panel("debug"); }
  });
  window.HermetrixWorkspace={supports,action,format,open,panel,feedback,dirty,renderOutput,renderDebug,breakpoint,editorMounted,command,diagnostics,resume};
})();
