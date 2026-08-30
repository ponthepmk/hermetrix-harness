const state = { skills: [], candidates: [], archives: [], relations: [], reviews: [], curator_runs: [], profiles: [], providers: [], mcp_servers: [], capability_summary: { total:0, by_source:{}, by_readiness:{} }, capabilityResults: [], selectedCapability: null, sessions: [], projects: [], projectFiles: [], jobs: [], artifacts: [], settings: [], memories: [], backups: [], usage: {}, fidelityCases: [], fidelityRuns: [], qualifications: [], curatorFindings: [], schedules: [], gcRuns: [], skillAuthority:null, authorityActions:[], activeTab: "chat", selectedSkill: null, selectedSession: null, selectedProject: null, sessionDetail: null, contextResult: null, modelProbe: null, sending: false, draftQualificationReason:"", sessionError:"" };
const $ = (selector, root = document) => root.querySelector(selector);
const $$ = (selector, root = document) => [...root.querySelectorAll(selector)];

async function api(path, options = {}) {
  const response = await fetch(path, { headers: { "Content-Type": "application/json", ...(options.headers || {}) }, ...options });
  const body = await response.json().catch(() => ({}));
  if (!response.ok) throw new Error(body.error || `Request failed (${response.status})`);
  return body;
}

function escapeHTML(value = "") {
  return String(value).replace(/[&<>'"]/g, char => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", "'": "&#39;", '"': "&quot;" })[char]);
}

function shortHash(value = "") { return value ? `${value.slice(0, 10)}…` : "—"; }
function formatDate(value) { return value ? new Intl.DateTimeFormat("th-TH", { dateStyle: "medium", timeStyle: "short" }).format(new Date(value)) : "—"; }
function pill(text, tone = "") { return `<span class="pill ${tone}">${escapeHTML(text)}</span>`; }

let toastTimer;
function toast(message, error = false) {
  const node = $("#toast");
  node.textContent = message;
  // Class, not node.style: the server sends `style-src 'self'`, so every inline
  // style assignment in this file was being blocked and the error tint never
  // actually appeared.
  node.classList.toggle("error", error);
  node.classList.add("show");
  clearTimeout(toastTimer);
  toastTimer = setTimeout(() => node.classList.remove("show"), 2600);
}

function askAction({ title, message, confirmLabel = "Confirm", reasonLabel = "", danger = false }) {
  const dialog = $("#actionDialog");
  const form = $("#actionForm");
  const input = $("#actionInput");
  $("#actionTitle").textContent = title;
  $("#actionMessage").textContent = message;
  $("#actionConfirm").textContent = confirmLabel;
  $("#actionConfirm").className = danger ? "danger" : "primary";
  $("#actionInputLabel").hidden = !reasonLabel;
  $("#actionInputLabel").firstChild.textContent = reasonLabel || "Reason";
  input.value = "";
  input.required = Boolean(reasonLabel);
  return new Promise(resolve => {
    let settled = false;
    const finish = value => {
      if (settled) return;
      settled = true;
      dialog.close();
      resolve(value);
    };
    form.onsubmit = event => { event.preventDefault(); finish(reasonLabel ? input.value.trim() : true); };
    $("#actionCancel").onclick = () => finish(null);
    $("#actionClose").onclick = () => finish(null);
    dialog.oncancel = event => { event.preventDefault(); finish(null); };
    dialog.showModal();
    if (reasonLabel) input.focus(); else $("#actionConfirm").focus();
  });
}

async function load() {
  try {
    const [data, projects, jobs, artifacts, settings, memories, backups, usage, fidelityCases, fidelityRuns, qualifications, curatorFindings, schedules, gcRuns, skillAuthority, authorityActions] = await Promise.all([
      api("/api/bootstrap"), api("/api/projects"), api("/api/jobs"), api("/api/artifacts"), api("/api/settings"),
      api("/api/memories"), api("/api/backups"), api("/api/usage"), api("/api/fidelity/cases"), api("/api/fidelity/runs"),
      api("/api/qualifications"), api("/api/curator/findings"), api("/api/maintenance/schedules"), api("/api/maintenance/gc"),
      api("/api/skill-authority"), api("/api/skill-authority/actions")
    ]);
    Object.assign(state, data);
    Object.assign(state, { projects, jobs, artifacts, settings, memories, backups, usage, fidelityCases, fidelityRuns, qualifications, curatorFindings, schedules, gcRuns, skillAuthority, authorityActions });
    if (!state.selectedProject && projects.length) state.selectedProject = projects[0].id;
    if (state.selectedProject) state.projectFiles = await api(`/api/projects/${encodeURIComponent(state.selectedProject)}/files?path=`);
    renderAll();
  } catch (error) { toast(error.message, true); }
}

function renderAll() {
  // Isolate each panel: one renderer throwing on an unexpected payload must not
  // leave every panel after it in the list blank.
  for (const render of [renderChat, renderProviders, renderMCP, renderStats, renderLibrary,
    renderProposals, renderLearning, renderInsights, renderArchive, renderContext,
    renderProjects, renderOffice, renderArtifacts, renderFidelity, renderMaintenance]) {
    try { render(); } catch (error) { console.error(`${render.name} failed`, error); }
  }
  const waiting = state.candidates.filter(item => ["needs_review", "quarantined"].includes(item.state)).length;
  $("#proposalBadge").hidden = waiting === 0;
  $("#proposalBadge").textContent = waiting;
  $("#tabProposalCount").textContent = waiting;
  const queuedReviews = state.reviews.filter(item => item.state === "queued" || item.state === "running").length;
  $("#tabReviewCount").textContent = queuedReviews;
  switchTab(state.activeTab, false);
}

function renderStats() {
  const active = state.skills.filter(item => item.state === "active").length;
  const waiting = state.candidates.filter(item => ["needs_review", "quarantined"].includes(item.state)).length;
  const queued = state.reviews.filter(item => item.state === "queued" || item.state === "running").length;
  $("#stats").innerHTML = [
    [active, "Active skills"], [waiting, "Skill proposals"], [queued, "Background reviews"], [state.relations.length, "Open relations"]
  ].map(([value, label]) => `<div class="stat"><strong>${value}</strong><span>${label}</span></div>`).join("");
}

function renderLibrary() {
  const query = $("#searchInput").value.trim().toLowerCase();
  const filter = $("#stateFilter").value;
  const items = state.skills.filter(item => {
    const haystack = `${item.canonical_name} ${item.summary} ${item.origin} ${item.owner}`.toLowerCase();
    return (!query || haystack.includes(query)) && (!filter || item.state === filter);
  });
  const root = $("#view-library");
  const policy = state.skillAuthority;
  const policyPanel = policy ? `<div class="panel authority-panel"><div class="proposal-head"><div><p class="eyebrow">Skill authority</p><h3>${policy.mode === "manual" ? "Manual review" : "Gated automation"}</h3><p>Automation can promote only trusted agent/reviewer candidates that pass checks, replay, scope and token gates. Capability widening always remains manual.</p></div>${pill(`policy r${policy.revision}`, policy.mode === "manual" ? "blue" : "amber")}</div><form id="authorityForm"><div class="form-grid"><label>Mode<select name="mode"><option value="manual" ${policy.mode === "manual" ? "selected" : ""}>Manual · safest default</option><option value="gated_automation" ${policy.mode === "gated_automation" ? "selected" : ""}>Gated automation</option></select></label><label>Candidate token ceiling<input name="max_candidate_tokens" type="number" min="256" max="16384" value="${policy.max_candidate_tokens}"></label></div><div class="authority-checks"><label class="check-label"><input name="auto_create" type="checkbox" ${policy.auto_promote_agent_create ? "checked" : ""}> Auto-promote trusted agent-created Skills</label><label class="check-label"><input name="auto_improve" type="checkbox" ${policy.auto_promote_agent_improve ? "checked" : ""}> Auto-promote no-regression improvements</label><label class="check-label"><input name="auto_archive" type="checkbox" ${policy.auto_archive_agent_skills ? "checked" : ""}> Let curator archive stale agent Skills with undo</label></div><fieldset><legend>Allowed scopes</legend>${["user","workspace","agent"].map(scope => `<label class="check-label"><input name="scope" value="${scope}" type="checkbox" ${(policy.allowed_scopes || []).includes(scope) ? "checked" : ""}> ${scope}</label>`).join("")}</fieldset><label>Change reason<input name="reason" required maxlength="1000" placeholder="Why this authority policy is appropriate"></label><div class="action-row"><button class="primary">Save authority policy</button><button class="ghost" type="button" id="runAuthorityButton">Evaluate pending candidates</button></div></form>${state.authorityActions.length ? `<div class="authority-actions"><h4>Recent automated decisions</h4>${state.authorityActions.slice(0,5).map(action => `<article><div>${pill(action.state,action.state === "completed" ? "green" : action.state === "failed" ? "red" : "amber")}<strong>${escapeHTML(action.action_kind)}</strong><small>policy r${action.policy_revision} · ${formatDate(action.created_at)}</small></div>${action.error ? `<p>${escapeHTML(action.error)}</p>` : ""}${action.state === "completed" && !action.rollback_candidate_id ? `<button class="ghost" data-authority-rollback="${escapeHTML(action.id)}">Create rollback</button>` : ""}</article>`).join("")}</div>` : ""}</div>` : "";
  const list = items.length ? `<div class="skill-list">${items.map(item => `
    <article class="skill-row ${state.selectedSkill === item.id ? "selected" : ""}" data-skill-id="${escapeHTML(item.id)}" tabindex="0">
      <div><div class="row-title"><h3>${escapeHTML(item.canonical_name)}</h3>${pill(item.state, item.state === "active" ? "green" : "amber")}${item.pinned ? pill("pinned", "blue") : ""}</div>
      <p>${escapeHTML(item.summary || "No summary in the active manifest")}</p>
      <div class="meta">${pill(item.scope_kind)}${pill(item.origin)}${pill(item.owner)}</div></div>
      <div class="metric"><strong>${item.injected_count}</strong>injected · ${item.success_count} success</div>
    </article>`).join("")}</div>` : `<div class="empty"><h3>${state.skills.length ? "No matching skills" : "No active skills yet"}</h3><p>Use + New proposal to create a custom Skill. It enters the candidate workspace first and never bypasses checks.</p></div>`;
  root.innerHTML = `${policyPanel}${list}`;
  $("#authorityForm")?.addEventListener("submit", saveAuthorityPolicy);
  $("#runAuthorityButton")?.addEventListener("click", runAuthorityPolicy);
  $$('[data-authority-rollback]', root).forEach(button => button.addEventListener("click", () => rollbackAuthorityAction(button.dataset.authorityRollback)));
  $$("[data-skill-id]", root).forEach(node => {
    const open = () => inspectSkill(node.dataset.skillId);
    node.addEventListener("click", open);
    node.addEventListener("keydown", event => { if (event.key === "Enter" || event.key === " ") open(); });
  });
}

async function inspectSkill(id) {
  try {
    const data = await api(`/api/skills/${encodeURIComponent(id)}`);
    state.selectedSkill = id;
    renderLibrary();
    const { skill, version } = data;
    const attempts = skill.success_count + skill.failure_count;
    const observed = attempts ? `${skill.success_count}/${attempts} observed success` : "No explicit outcomes yet";
    $("#inspector").innerHTML = `
      <div class="inspect-head"><p class="eyebrow">Active capability</p><h2>${escapeHTML(skill.canonical_name)}</h2><div class="meta">${pill(skill.state,"green")}${pill(skill.scope_kind)}${pill(skill.origin)}</div></div>
      <section class="inspect-section"><h3>Provenance</h3><div class="kv"><span>Owner</span><strong>${escapeHTML(skill.owner)}</strong><span>Version</span><span class="hash">${escapeHTML(version.id)}</span><span>Content</span><span class="hash">${escapeHTML(version.content_hash)}</span><span>Author</span><span>${escapeHTML(version.author_actor)}</span><span>Changed</span><span>${formatDate(version.created_at)}</span></div></section>
      <section class="inspect-section"><h3>Usage evidence</h3><div class="kv"><span>Selected</span><strong>${skill.selected_count}</strong><span>Injected</span><strong>${skill.injected_count}</strong><span>Outcome</span><span>${observed}</span><span>Last used</span><span>${formatDate(skill.last_used_at)}</span></div></section>
      <section class="inspect-section"><h3>Current SKILL.md</h3><pre>${escapeHTML(version.markdown)}</pre></section>
      <section class="inspect-section"><h3>Selection controls</h3><p>Changes affect new sessions only. Existing Session Contracts keep their exact Skill version and cache prefix.</p><div class="action-row"><button class="ghost" id="toggleEnabled">${skill.enabled ? "Disable for new sessions" : "Enable for new sessions"}</button><button class="ghost" id="togglePinned">${skill.pinned ? "Unpin" : "Pin"}</button></div></section>
      <section class="inspect-section"><h3>Reversible actions</h3><p>Editing starts from this exact version as a proposal. Forking creates a user-owned custom Skill. Archiving preserves the snapshot and history.</p><div class="action-row"><button class="primary" id="improveSelected">Propose improvement</button><button class="ghost" id="forkSelected">Fork as custom</button><button class="danger" id="archiveSelected">Archive skill</button></div></section>`;
    $("#inspector").classList.add("open");
    $("#improveSelected").addEventListener("click", () => proposeImprovement(skill));
    $("#forkSelected").addEventListener("click", () => forkSkill(skill));
    $("#archiveSelected").addEventListener("click", () => archiveSkill(skill));
    $("#toggleEnabled").addEventListener("click", () => updateSkillControl(skill, "enabled", !skill.enabled));
    $("#togglePinned").addEventListener("click", () => updateSkillControl(skill, "pinned", !skill.pinned));
  } catch (error) { toast(error.message, true); }
}

async function proposeImprovement(skill) {
  const reason = await askAction({ title:`Improve ${skill.canonical_name}?`, message:"Hermetrix will clone the active immutable version into a candidate workspace. The active skill remains unchanged until a later promotion.", confirmLabel:"Create improvement proposal", reasonLabel:"Improvement goal" });
  if (!reason) return;
  try {
    const candidate = await api(`/api/skills/${encodeURIComponent(skill.id)}/improvements`, { method:"POST", body:JSON.stringify({ actor:"user", reason }) });
    toast("Improvement proposal created — active version unchanged");
    await load();
    switchTab("proposals");
    await inspectCandidate(candidate.id);
  } catch (error) { toast(error.message, true); }
}

async function archiveSkill(skill) {
  const reason = await askAction({ title:`Archive ${skill.canonical_name}?`, message:"The skill will stop being selected. Its current version, provenance, usage, and blob remain recoverable.", confirmLabel:"Archive safely", reasonLabel:"Archive reason", danger:true });
  if (!reason) return;
  try {
    await api(`/api/skills/${encodeURIComponent(skill.id)}/archive`, { method: "POST", body: JSON.stringify({ actor: "user", reason }) });
    state.selectedSkill = null;
    $("#inspector").innerHTML = `<div class="empty-inspector"><span class="orb">✓</span><h2>Archived safely</h2><p>The exact version remains available in Archive and restore creates a new proposal.</p></div>`;
    toast("Skill archived — snapshot retained");
    await load();
  } catch (error) { toast(error.message, true); }
}

async function updateSkillControl(skill, field, value) {
  const label = field === "enabled" ? (value ? "enable" : "disable") : (value ? "pin" : "unpin");
  const approved = await askAction({
    title:`${label[0].toUpperCase()}${label.slice(1)} ${skill.canonical_name}?`,
    message:"The change applies to future Session Contracts only. Running sessions keep their immutable Skill selection.",
    confirmLabel:`${label[0].toUpperCase()}${label.slice(1)} Skill`
  });
  if (!approved) return;
  try {
    await api(`/api/skills/${encodeURIComponent(skill.id)}`, { method:"PATCH", body:JSON.stringify({
      actor:"local-user", expected_version_id:skill.current_version_id, [field]:value
    }) });
    toast(`Skill ${label}d for future sessions`);
    await load();
    await inspectSkill(skill.id);
  } catch (error) { toast(error.message, true); }
}

async function forkSkill(skill) {
  const name = await askAction({
    title:`Fork ${skill.canonical_name}?`,
    message:"Hermetrix will create a user-owned candidate from the exact immutable version. Enter a new kebab-case name; the original remains unchanged.",
    confirmLabel:"Create custom fork", reasonLabel:"New Skill name"
  });
  if (!name) return;
  const canonicalName = name.trim().toLowerCase();
  if (!/^[a-z0-9][a-z0-9-]{1,62}$/.test(canonicalName)) {
    toast("Skill name must be 2–63 lowercase letters, digits or hyphens", true);
    return;
  }
  try {
    const candidate = await api(`/api/skills/${encodeURIComponent(skill.id)}/fork`, { method:"POST", body:JSON.stringify({
      canonical_name:canonicalName, actor:"local-user", reason:`User-created fork of ${skill.canonical_name}`
    }) });
    toast("Custom fork created as a reviewable candidate");
    await load();
    switchTab("proposals");
    await inspectCandidate(candidate.id);
  } catch (error) { toast(error.message, true); }
}

async function saveAuthorityPolicy(event) {
  event.preventDefault();
  const form = new FormData(event.currentTarget);
  const scopes = form.getAll("scope");
  if (!scopes.length) { toast("Select at least one allowed scope", true); return; }
  try {
    await api("/api/skill-authority", { method:"PUT", body:JSON.stringify({
      mode:form.get("mode"), auto_promote_agent_create:form.get("auto_create") === "on",
      auto_promote_agent_improve:form.get("auto_improve") === "on",
      auto_archive_agent_skills:form.get("auto_archive") === "on", allowed_scopes:scopes,
      max_candidate_tokens:Number(form.get("max_candidate_tokens")), actor:"local-user",
      reason:form.get("reason"), expected_revision:state.skillAuthority.revision
    }) });
    toast("Skill authority policy saved with a new revision");
    await load();
    switchTab("library");
  } catch (error) { toast(error.message, true); }
}

async function runAuthorityPolicy() {
  try {
    const actions = await api("/api/skill-authority/run", { method:"POST", body:"{}" });
    toast(actions.length ? `Completed ${actions.length} gated Skill decisions` : "No pending candidates met every authority gate");
    await load();
    switchTab("library");
  } catch (error) { toast(error.message, true); }
}

async function rollbackAuthorityAction(id) {
  const reason = await askAction({ title:"Undo automated Skill decision?", message:"Auto-created Skills are archived immediately. Improvements create a rollback candidate so the previous immutable version must pass the normal review gate.", confirmLabel:"Create safe rollback", reasonLabel:"Rollback reason", danger:true });
  if (!reason) return;
  try {
    const candidate = await api(`/api/skill-authority/actions/${encodeURIComponent(id)}/rollback`, { method:"POST", body:JSON.stringify({ actor:"local-user", reason }) });
    toast(candidate.id ? "Rollback candidate created for review" : "Auto-created Skill archived and retained for restore");
    await load();
    switchTab(candidate.id ? "proposals" : "archive");
  } catch (error) { toast(error.message, true); }
}

function renderProposals() {
  const items = state.candidates.filter(item => ["needs_review", "quarantined"].includes(item.state));
  const root = $("#view-proposals");
  if (!items.length) { root.innerHTML = `<div class="empty"><h3>No proposals waiting</h3><p>Background and agent learning can write only here—not into the active skill store.</p></div>`; return; }
  root.innerHTML = `<div class="card-list">${items.map(item => {
    const errors = (item.checks.findings || []).filter(finding => finding.level === "error");
    return `<article class="proposal-card"><div class="proposal-head"><div><div class="row-title"><h3>${escapeHTML(item.canonical_name)}</h3>${pill(item.change_kind,"blue")}${pill(item.state,item.state === "needs_review" ? "green" : "red")}</div><p>${escapeHTML(item.reason || "No rationale supplied")}</p></div><span class="hash">r${item.revision} · ${shortHash(item.candidate_hash)}</span></div>
      <div class="meta">${pill(`by ${item.created_by}`)}${pill(item.trigger_kind)}${pill(`${item.checks.token_estimate} tokens`)}</div>
      ${(item.checks.findings || []).length ? `<ul class="findings">${item.checks.findings.map(f => `<li class="${f.level}"><strong>${escapeHTML(f.code)}</strong> — ${escapeHTML(f.message)}</li>`).join("")}</ul>` : ""}
      <div class="action-row"><button class="ghost" data-review="${escapeHTML(item.id)}">Inspect content</button><button class="primary" data-promote="${escapeHTML(item.id)}" ${errors.length || !item.checks.passed ? "disabled" : ""}>Approve & promote</button><button class="danger" data-reject="${escapeHTML(item.id)}">Reject</button></div></article>`;
  }).join("")}</div>`;
  $$('[data-review]', root).forEach(button => button.addEventListener("click", () => inspectCandidate(button.dataset.review)));
  $$('[data-promote]', root).forEach(button => button.addEventListener("click", () => promoteCandidate(button.dataset.promote)));
  $$('[data-reject]', root).forEach(button => button.addEventListener("click", () => rejectCandidate(button.dataset.reject)));
}

function renderLearning() {
  const root = $("#view-learning");
  const queued = state.reviews.filter(item => item.state === "queued").length;
  root.innerHTML = `<div class="panel"><div class="proposal-head"><div><h3>Background review queue</h3><p>Jobs persist across restart, use structured digests, yield to foreground inference, and can create only checked candidates.</p></div><button class="primary" id="runReviewButton" ${queued ? "" : "disabled"}>Run next review</button></div></div>
    <div class="card-list spaced">${state.reviews.length ? state.reviews.map(item => `<article class="proposal-card"><div class="proposal-head"><div><div class="row-title"><h3>${escapeHTML(item.trigger_kind)}</h3>${pill(item.state, item.state === "completed" ? "green" : item.state === "failed" ? "red" : "amber")}</div><p>${escapeHTML(item.digest.goal_and_constraints || "Structured milestone digest")}</p></div><span class="hash">${escapeHTML(item.reviewer_revision)}</span></div><div class="meta">${pill(`session ${item.session_id}`)}${pill(`${item.attempts} attempts`)}${item.decision?.kind ? pill(item.decision.kind,"blue") : ""}${item.candidate_id ? pill("candidate created","green") : ""}</div>${item.error ? `<ul class="findings"><li class="error">${escapeHTML(item.error)}</li></ul>` : ""}</article>`).join("") : `<div class="empty"><h3>No learning reviews yet</h3><p>The agent runtime will enqueue successful milestones, repeated corrections, explicit learn requests, and skill-related failures. Empty is a valid state.</p></div>`}</div>`;
  $("#runReviewButton").addEventListener("click", runNextReview);
}

async function runNextReview() {
  try {
    const job = await api("/api/reviews/run-next", { method:"POST", body:"{}" });
    toast(job.candidate_id ? "Review completed — candidate created" : "Review completed — no durable change needed");
    await load();
    if (job.candidate_id) switchTab("proposals");
  } catch (error) { toast(error.message, true); }
}

async function inspectCandidate(id) {
  try {
    const [item, replays] = await Promise.all([api(`/api/candidates/${encodeURIComponent(id)}`), api(`/api/candidates/${encodeURIComponent(id)}/replays`)]);
    const replay = replays[0];
    const addedTools = replay?.summary?.added_tools || [];
    $("#inspector").innerHTML = `<div class="inspect-head"><p class="eyebrow">Untrusted candidate</p><h2>${escapeHTML(item.canonical_name)}</h2><div class="meta">${pill(item.state,item.checks.passed ? "green" : "red")}${pill(`revision ${item.revision}`)}</div></div>
      <section class="inspect-section"><h3>Evidence</h3><p>${escapeHTML(item.reason)}</p><div class="meta">${(item.evidence_refs || []).map(ref => pill(ref)).join("") || pill("manual")}</div></section>
      <section class="inspect-section"><h3>Candidate SKILL.md</h3><textarea id="candidateEditor" rows="18">${escapeHTML(item.markdown)}</textarea><div class="action-row"><button class="primary" id="saveCandidateEdit">Save & re-run checks</button></div></section>
      <section class="inspect-section"><h3>Checks</h3><div class="kv"><span>Lint</span><strong>${item.checks.lint_passed ? "pass" : "fail"}</strong><span>Security</span><strong>${item.checks.security_passed ? "pass" : "fail"}</strong><span>Replay</span><strong>${item.checks.replay_required ? (item.checks.replay_passed ? "pass" : "required") : "not required"}</strong><span>Footprint</span><span>${item.checks.token_estimate} tokens</span></div></section>
      <section class="inspect-section"><h3>Deterministic replay & bounded diff</h3>${replay ? `<div class="kv"><span>Runner</span><strong>${escapeHTML(replay.runner_revision)}</strong><span>Binding</span><span>r${replay.candidate_revision} · ${shortHash(replay.candidate_hash)}</span><span>Fixtures</span><strong>${replay.candidate_passed}/${replay.fixtures_total}</strong><span>Regressions</span><strong>${replay.regressions}</strong></div>${addedTools.length ? `<p class="form-note">Capability widening: ${addedTools.map(escapeHTML).join(", ")}</p>` : ""}<ul class="findings">${(replay.cases || []).map(test => `<li class="${test.candidate_passed ? "" : "error"}"><strong>${escapeHTML(test.id)}</strong> — baseline ${test.baseline_passed ? "pass" : "fail"}, candidate ${test.candidate_passed ? "pass" : "fail"}</li>`).join("")}</ul><pre>${escapeHTML(replay.diff || "No line changes")}</pre>` : `<p>No replay has been recorded for this revision.</p>`}<div class="action-row"><button class="ghost" id="runCandidateReplay">Run exact replay</button>${addedTools.length ? `<button class="primary" id="approveCandidateTools">Approve widened tools</button>` : ""}</div></section>`;
    $("#inspector").classList.add("open");
    $("#saveCandidateEdit").addEventListener("click", () => saveCandidateEdit(item));
    $("#runCandidateReplay").addEventListener("click", () => runCandidateReplay(item));
    $("#approveCandidateTools")?.addEventListener("click", () => reviewCandidateTools(item));
  } catch (error) { toast(error.message, true); }
}

async function runCandidateReplay(item) {
  try {
    await api(`/api/candidates/${encodeURIComponent(item.id)}/replays`, { method:"POST", body:"{}" });
    toast("Replay completed against the exact candidate revision");
    await load();
    await inspectCandidate(item.id);
  } catch (error) { toast(error.message, true); await inspectCandidate(item.id); }
}

async function reviewCandidateTools(item) {
  const approved = await askAction({ title:"Approve widened tool declaration?", message:"This approval is bound only to the exact candidate revision and added tool list shown in the replay report.", confirmLabel:"Approve exact revision" });
  if (!approved) return;
  try {
    await api(`/api/candidates/${encodeURIComponent(item.id)}/capability-review`, { method:"POST", body:JSON.stringify({ actor:"user", decision:"approve", expected_revision:item.revision }) });
    toast("Capability widening approved for this exact revision");
  } catch (error) { toast(error.message, true); }
}

async function saveCandidateEdit(item) {
  try {
    const updated = await api(`/api/candidates/${encodeURIComponent(item.id)}`, { method:"PATCH", body:JSON.stringify({ markdown:$("#candidateEditor").value, actor:"user", expected_revision:item.revision }) });
    toast(`Candidate revision ${updated.revision} saved; checks re-run`);
    await load();
    await inspectCandidate(updated.id);
  } catch (error) { toast(error.message, true); }
}

async function promoteCandidate(id) {
  const item = state.candidates.find(candidate => candidate.id === id);
  if (!item) return;
  const approved = await askAction({ title:`Promote ${item.canonical_name}?`, message:"This immutable version will become eligible for context selection. The proposal, checks, actor, and evidence remain in the audit history.", confirmLabel:"Approve & promote" });
  if (!approved) return;
  try {
    await api(`/api/candidates/${encodeURIComponent(id)}/promote`, { method: "POST", body: JSON.stringify({ actor: "user", expected_revision: item.revision }) });
    toast("Candidate promoted as an immutable skill version");
    await load();
    switchTab("library");
  } catch (error) { toast(error.message, true); }
}

async function rejectCandidate(id) {
  const item = state.candidates.find(candidate => candidate.id === id);
  const reason = await askAction({ title:`Reject ${item?.canonical_name || "this proposal"}?`, message:"The proposal will stay in history and cannot become active.", confirmLabel:"Reject proposal", reasonLabel:"Rejection reason", danger:true });
  if (!reason) return;
  try {
    await api(`/api/candidates/${encodeURIComponent(id)}/reject`, { method: "POST", body: JSON.stringify({ actor: "user", reason, expected_revision: item.revision }) });
    toast("Proposal rejected with an audit reason");
    await load();
  } catch (error) { toast(error.message, true); }
}

function renderInsights() {
  const root = $("#view-insights");
  const lastRun = state.curator_runs[0];
  const archiveEnabled = state.skillAuthority?.mode === "gated_automation" && state.skillAuthority?.auto_archive_agent_skills;
  root.innerHTML = `<div class="panel"><div class="proposal-head"><div><h3>Curator · ${archiveEnabled ? "gated archive enabled" : "report-only"}</h3><p>Deterministic retrieval runs first. Duplicate and merge findings always remain proposals. ${archiveEnabled ? "Only high-confidence stale agent-created Skills may be archived under the current versioned policy; every action has a restore path." : "The current authority policy forbids curator mutation."}</p><div class="meta">${lastRun ? `${pill(`last ${formatDate(lastRun.completed_at || lastRun.started_at)}`)}${pill(`${lastRun.findings_count} findings`)}${pill(lastRun.analyzer_revision)}` : pill("not run yet")}</div></div><button class="primary" id="analyzeButton">Analyze now</button></div></div>
    <div class="card-list spaced">${state.curatorFindings.length ? state.curatorFindings.map(item => `<article class="insight-card"><div class="insight-head"><div><h3>${escapeHTML(item.finding_kind)} · ${Math.round(item.score*100)}%</h3><p>${escapeHTML((item.evidence?.reasons || []).join("; ") || item.evidence?.note || "Version-bound human review required")}</p></div>${pill(item.severity,item.severity === "warning" ? "amber" : "blue")}</div><div class="kv"><span>Left skill</span><code>${escapeHTML(item.left_skill_id || "—")}</code><span>Right skill</span><code>${escapeHTML(item.right_skill_id || "—")}</code><span>Action</span><strong>${escapeHTML(item.proposal?.action || "report only")}</strong><span>Auto mutation</span><strong>${item.proposal?.automatic_mutation === false ? "forbidden" : "none"}</strong></div>${item.proposal?.review_steps ? `<ol class="review-steps">${item.proposal.review_steps.map(step => `<li>${escapeHTML(step)}</li>`).join("")}</ol>` : ""}</article>`).join("") : `<div class="empty"><h3>No curator findings</h3><p>Run analysis to score duplicates, overlaps and stale skills without changing active state.</p></div>`}</div>`;
  $("#analyzeButton").addEventListener("click", analyzeRelations);
}

async function analyzeRelations() {
  try { const result = await api("/api/curator/run", { method: "POST", body: "{}" }); const count = result.run?.findings_count || 0; const automated = result.authority_actions?.length || 0; toast(`Analysis complete — ${count} findings${automated ? ` · ${automated} gated actions` : ""}`); await load(); } catch (error) { toast(error.message, true); }
}

function renderArchive() {
  const root = $("#view-archive");
  if (!state.archives.length) { root.innerHTML = `<div class="empty"><h3>Archive is empty</h3><p>Archive is recoverable storage, not deletion.</p></div>`; return; }
  root.innerHTML = `<div class="card-list">${state.archives.map(item => `<article class="archive-card"><div class="archive-head"><div><h3>${escapeHTML(item.skill_name)}</h3><p>${escapeHTML(item.reason)} · ${formatDate(item.created_at)}</p></div>${pill(item.restored_candidate_id ? "restore proposed" : "archived", item.restored_candidate_id ? "blue" : "amber")}</div><div class="meta">${pill(item.actor_kind)}${pill(shortHash(item.archived_version_id))}</div><div class="action-row"><button class="ghost" data-restore="${escapeHTML(item.id)}" ${item.restored_candidate_id ? "disabled" : ""}>Restore as proposal</button></div></article>`).join("")}</div>`;
  $$('[data-restore]', root).forEach(button => button.addEventListener("click", () => restoreArchive(button.dataset.restore)));
}

async function restoreArchive(id) {
  const reason = await askAction({ title:"Restore archived version?", message:"Restore creates a proposal from the exact archived blob. It does not reactivate the skill until a separate promotion decision.", confirmLabel:"Create restore proposal", reasonLabel:"Restore reason" });
  if (!reason) return;
  try { await api(`/api/archives/${encodeURIComponent(id)}/restore`, { method: "POST", body: JSON.stringify({ actor: "user", reason }) }); toast("Restore proposal created — active state is unchanged"); await load(); switchTab("proposals"); } catch (error) { toast(error.message, true); }
}

function profileLabel(profile) {
  const labels = { "compact-32k":"Compact 32k", "certified-64k":"Certified 64k", "extended-128k":"Extended 128k", "extended-256k":"Extended 256k", "ultra-1m":"Ultra 1M" };
  return labels[profile.name] || profile.name;
}

function availableProfiles(provider) {
  return state.profiles.filter(profile => provider && profile.total <= provider.context_window);
}

function exactQualification(provider, profile) {
  if (!provider || !profile) return null;
  return state.qualifications.find(run => run.provider_id === provider.id && run.model === provider.model &&
    run.provider_revision === provider.revision && run.requested_profile === profile.name && run.state === "completed" && run.eligible) || null;
}

// MINIMUM_ANSWER_BUDGET mirrors minimumAnswerBudget in internal/agent/service.go.
// The server refuses to open a session below it, so the panel has to apply the
// same rule -- otherwise it offers a profile the server will reject, and the
// only feedback is a 500 that disappears with the toast.
const MINIMUM_ANSWER_BUDGET = 512;

// answerBudget mirrors answerBudget() in internal/agent/service.go: what is left
// of the output reserve once this model has done the reasoning it usually does.
function answerBudget(provider, profile) {
  if (!provider || !profile) return 0;
  const ratio = Number(provider.reasoning_ratio) || 0;
  if (ratio <= 0) return profile.output_reserve;
  if (ratio >= 1) return 0;
  return Math.floor(profile.output_reserve * (1 - ratio));
}

// profileAdmission answers one question the panel needs before it enables the
// button: can THIS provider open a session on THIS profile, and at what cost.
// Two independent gates apply, and both were previously invisible here.
function profileAdmission(provider, profile) {
  if (!profile) return { admitted:false, mode:"unavailable", budget:0 };
  const budget = answerBudget(provider, profile);
  // Gate 1: the answer budget. A reasoning model can spend the whole reserve
  // thinking; the server refuses such a session outright, whatever the
  // qualification says, so this check comes first.
  if (budget < MINIMUM_ANSWER_BUDGET) {
    return { admitted:false, mode:"answer_budget", budget, blocking:true };
  }
  // Gate 2: qualification. compact-32k is the declared compatibility floor and
  // needs none; every larger envelope needs exact evidence or a reviewed override.
  if (profile.name === "compact-32k") return { admitted:true, mode:"compatibility", budget };
  const qualification = exactQualification(provider, profile);
  return qualification
    ? { admitted:true, mode:"qualified", qualification, budget }
    : { admitted:false, mode:"override_required", budget };
}

// bestProfileFor picks the smallest envelope this provider can actually open a
// session on. Defaulting to a fixed name meant a reasoning model landed on an
// option its own output ratio had already ruled out, with a disabled button and
// no stated reason.
function bestProfileFor(provider, profiles) {
  const ordered = [...profiles].sort((a, b) => a.total - b.total);
  return ordered.find(profile => profileAdmission(provider, profile).admitted)
    || ordered.find(profile => answerBudget(provider, profile) >= MINIMUM_ANSWER_BUDGET)
    || ordered[0] || null;
}

// suggestedOverrideReason writes the reason from the evidence already on screen
// so an override that is the only available path does not also demand that the
// operator invent prose for it. It stays editable: the audit record keeps
// whatever is actually submitted.
function suggestedOverrideReason(provider, profile) {
  if (!provider || !profile) return "";
  const run = state.qualifications.find(item => item.provider_id === provider.id &&
    item.provider_revision === provider.revision && item.state === "completed");
  const parts = [`${profileLabel(profile)} on ${provider.model} via ${provider.name}`];
  if (run) {
    parts.push(`qualification grade ${run.capability_grade || "?"}, context tier ${run.context_tier || "?"}`);
    const notRun = (run.results?.checks || []).filter(check => check.state === "not_run").map(check => check.name);
    if (notRun.includes("runtime_allocation")) parts.push("local runtime allocation cannot be probed on a remote endpoint");
  } else {
    parts.push("no qualification run recorded for this provider revision");
  }
  parts.push(`accepted for local single-operator use with ~${answerBudget(provider, profile).toLocaleString()} answer tokens`);
  return parts.join("; ") + ".";
}

function renderTimelineEvent(event) {
  if (event.event_kind === "message" && ["user", "assistant"].includes(event.role)) {
    return `<article class="chat-message ${event.role}"><div class="message-role">${event.role === "user" ? "You" : "Hermetrix"}</div><div class="message-body">${escapeHTML(event.content)}</div>${event.role === "assistant" && event.metadata?.step_binding_id ? `<div class="message-proof">bound ${escapeHTML(shortHash(event.metadata.step_binding_id))} · ${event.metadata.usage?.total_tokens || 0} tokens</div>` : ""}</article>`;
  }
  if (event.event_kind === "tool_call") {
    let argumentsPreview = String(event.content || "");
    if (event.metadata?.tool_name === "workspace.write_file") {
      try {
        const args = JSON.parse(argumentsPreview);
        argumentsPreview = JSON.stringify({ path:args.path, content:`[${String(args.content || "").length} characters; inspect approval preview]`, expected_sha256:args.expected_sha256 });
      } catch {}
    } else if (argumentsPreview.length > 900) {
      argumentsPreview = `${argumentsPreview.slice(0,900)}…`;
    }
    return `<article class="tool-receipt request"><div>${pill("tool request","blue")}<strong>${escapeHTML(event.metadata?.tool_name || "unknown tool")}</strong></div><code>${escapeHTML(argumentsPreview)}</code><small>bound ${escapeHTML(shortHash(event.metadata?.step_binding_id))}</small></article>`;
  }
  if (event.event_kind === "tool_result") {
    let receipt = {};
    try { receipt = JSON.parse(event.content); } catch {}
    const preview = String(receipt.output || receipt.error || "No inline output");
    return `<article class="tool-receipt result"><div>${pill(receipt.status || event.metadata?.tool_status || "receipt", receipt.status === "succeeded" ? "green" : "red")}<strong>${escapeHTML(receipt.name || event.metadata?.tool_name || "tool")}</strong><span>${Number(receipt.duration_ms || 0)}ms</span></div><pre>${escapeHTML(preview.length > 900 ? `${preview.slice(0,900)}…` : preview)}</pre><small>call ${escapeHTML(shortHash(event.metadata?.tool_call_id))}</small></article>`;
  }
  if (event.event_kind === "approval_required") {
    const approval = (state.sessionDetail?.approvals || []).find(item => item.id === event.metadata?.approval_id);
    if (!approval) return "";
    const tone = approval.state === "pending" ? "amber" : approval.state === "executed" ? "green" : "red";
    return `<article class="approval-card"><div class="approval-head"><div>${pill("approval required", tone)}<strong>${escapeHTML(approval.tool_name)}</strong></div>${pill(approval.state, tone)}</div><p>${escapeHTML(approval.summary)}</p><pre>${escapeHTML(approval.preview || "No content preview")}</pre><small>effect ${escapeHTML(approval.effect)} · exact call ${escapeHTML(shortHash(approval.arguments_hash))}</small>${approval.state === "pending" ? `<div class="action-row"><button class="primary" data-approve-tool="${escapeHTML(approval.id)}">Approve once</button><button class="danger" data-deny-tool="${escapeHTML(approval.id)}">Deny</button></div>` : ""}</article>`;
  }
  if (event.event_kind === "approval_decision") {
    const approved = event.metadata?.decision === "approve";
    return `<article class="approval-decision">${pill(approved ? "approved once" : "denied", approved ? "green" : "red")}<span>${escapeHTML(event.metadata?.reason || "No reason supplied")}</span><small>${escapeHTML(event.metadata?.actor || "user")}</small></article>`;
  }
  return "";
}

function renderChat() {
  const root = $("#view-chat");
  if (!root) return;
  const enabledProviders = state.providers.filter(provider => provider.enabled);
  if (!state.draftProviderID || !enabledProviders.some(provider => provider.id === state.draftProviderID)) {
    state.draftProviderID = enabledProviders[0]?.id || null;
  }
  const draftProvider = enabledProviders.find(provider => provider.id === state.draftProviderID);
  const compatibleProfiles = availableProfiles(draftProvider);
  if (!compatibleProfiles.some(profile => profile.name === state.draftProfileName)) {
    state.draftProfileName = bestProfileFor(draftProvider, compatibleProfiles)?.name || "";
  }
  // Bind to the workspace project by default. An unbound session cannot read a
  // file, which is what most sessions here are opened to do.
  if (state.draftProjectID === undefined) state.draftProjectID = state.projects[0]?.id || "";
  const draftProfile = compatibleProfiles.find(profile => profile.name === state.draftProfileName);
  const admission = profileAdmission(draftProvider, draftProfile);
  const needsOverride = admission.mode === "override_required";
  const overrideReason = state.draftQualificationReason.trim() || suggestedOverrideReason(draftProvider, draftProfile);
  const canStart = Boolean(draftProvider && draftProfile && draftProvider.credential_ready &&
    (admission.admitted || (needsOverride && overrideReason.length >= 8)));
  const selectedID = state.sessionDetail?.session?.id || state.selectedSession;
  const timeline = (state.sessionDetail?.events || []).filter(event => ["message", "tool_call", "tool_result", "approval_required", "approval_decision"].includes(event.event_kind));
  const session = state.sessionDetail?.session;
  root.innerHTML = `<div class="chat-layout">
    <aside class="session-panel">
      <div class="session-create">
        <p class="eyebrow">New session</p>
        <label>Provider<select id="chatProviderSelect" ${enabledProviders.length ? "" : "disabled"}>${enabledProviders.length ? enabledProviders.map(provider => `<option value="${escapeHTML(provider.id)}" ${provider.id === state.draftProviderID ? "selected" : ""}>${escapeHTML(provider.name)} · ${escapeHTML(provider.model)}</option>`).join("") : `<option>No provider</option>`}</select></label>
        <label>Project<select id="chatProjectSelect">${state.projects.map(project => `<option value="${escapeHTML(project.id)}" ${project.id === state.draftProjectID ? "selected" : ""}>${escapeHTML(project.name)}</option>`).join("")}<option value="" ${state.draftProjectID ? "" : "selected"}>No project · chat only</option></select></label>
        <label>Context<select id="chatProfileSelect" ${compatibleProfiles.length ? "" : "disabled"}>${compatibleProfiles.map(profile => {
          const status = profileAdmission(draftProvider, profile);
          const note = status.blocking ? "too small for this model"
            : status.mode === "qualified" ? "qualified"
            : status.mode === "compatibility" ? "ready"
            : "one-click override";
          return `<option value="${profile.name}" ${profile.name === state.draftProfileName ? "selected" : ""} ${status.blocking ? "disabled" : ""}>${profileLabel(profile)} · ${status.budget.toLocaleString()} answer tokens · ${note}</option>`;
        }).join("")}</select></label>
        ${draftProfile ? `<div class="session-readiness ${admission.admitted ? "ready" : needsOverride ? "review" : "blocked"}">
          <p class="readiness-line">${admission.admitted
            ? (admission.mode === "qualified" ? `Bound to qualification ${escapeHTML(shortHash(admission.qualification.id))}.` : `Ready. ${admission.budget.toLocaleString()} tokens for the answer.`)
            : needsOverride ? `No local qualification can exist for a remote endpoint, so this envelope opens under a reviewed 24-hour override. The reason below is recorded with the session.`
            : `${escapeHTML(profileLabel(draftProfile))} leaves only ${admission.budget.toLocaleString()} answer tokens because ${escapeHTML(draftProvider.model)} spends about ${Math.round((draftProvider.reasoning_ratio || 0) * 100)}% of its output reasoning. The server refuses anything under ${MINIMUM_ANSWER_BUDGET}. Choose a larger envelope.`}</p>
          ${needsOverride ? `<details class="override-details"><summary>Override reason (edit if you want)</summary><textarea id="chatQualificationReason" rows="4" minlength="8">${escapeHTML(overrideReason)}</textarea></details>` : ""}
        </div>` : ""}
        <button class="primary" id="newSessionButton" ${canStart ? "" : "disabled"}>${needsOverride ? "Start session with override" : "+ Start session"}</button>
        ${state.sessionError ? `<p class="session-error" role="alert">${escapeHTML(state.sessionError)}</p>` : ""}
        ${draftProvider && !draftProvider.credential_ready ? `<p class="form-note">Set server environment variable <code>${escapeHTML(draftProvider.api_key_env)}</code> before starting a session.</p>` : ""}
      </div>
      <div class="session-list">${state.sessions.length ? state.sessions.map(item => `<button class="session-item ${item.id === selectedID ? "active" : ""}" data-session-id="${escapeHTML(item.id)}"><strong>${escapeHTML(item.title)}</strong><span>${escapeHTML(item.model)} · ${escapeHTML(item.context_profile)}</span></button>`).join("") : `<div class="session-empty">No sessions yet</div>`}</div>
    </aside>
    <section class="chat-stage">
      ${session ? `<header class="chat-head"><div><p class="eyebrow">${escapeHTML(session.provider_name)} / ${escapeHTML(session.context_profile)}</p><h2>${escapeHTML(session.title)}</h2><small>contract ${escapeHTML(shortHash(session.contract_revision))} · cache epoch ${session.cache_epoch} · ${escapeHTML(session.contract?.qualification?.mode || "unbound")}</small></div><div class="chat-state">${pill(session.state, session.state === "active" ? "green" : "amber")}${pill(session.model,"blue")}</div></header>
        <div class="message-list" id="messageList">${timeline.length ? timeline.map(renderTimelineEvent).join("") : `<div class="chat-welcome"><img src="/assets/brand/hermetrix-engine-v3-512.png" alt=""><h3>Hermetrix is ready</h3><p>Each turn freezes its provider, model, context snapshot, capability revision and policy revision before sampling.</p></div>`}<article class="chat-message assistant streaming ${state.sending ? "" : "hidden"}" id="streamingAssistant"><div class="message-role">Hermetrix</div><div class="message-body"></div><div class="message-proof" id="streamStatus">waiting for provider…</div></article></div>
        <form class="composer" id="chatForm"><textarea id="chatInput" rows="3" maxlength="1048576" placeholder="ส่งข้อความถึง Hermetrix…" ${state.sending ? "disabled" : ""}></textarea><button class="primary" ${state.sending ? "disabled" : ""}>${state.sending ? "Running…" : "Send"}</button></form>` : `<div class="chat-welcome standalone"><img src="/assets/brand/hermetrix-engine-v3-512.png" alt=""><h3>${enabledProviders.length ? "Start an agent session" : "Configure a provider first"}</h3><p>${enabledProviders.length ? "Choose a provider and a context envelope. The selected envelope cannot exceed the provider declaration." : "Provider profiles keep endpoint and model metadata only. Credentials stay in server environment variables."}</p>${enabledProviders.length ? "" : `<button class="primary" id="openProvidersButton">Open providers</button>`}</div>`}
    </section>
  </div>`;
  $("#chatProviderSelect")?.addEventListener("change", event => { state.draftProviderID = event.target.value; state.draftProfileName = ""; state.draftQualificationReason=""; state.sessionError=""; renderChat(); });
  $("#chatProjectSelect")?.addEventListener("change", event => { state.draftProjectID = event.target.value; });
  $("#chatProfileSelect")?.addEventListener("change", event => { state.draftProfileName = event.target.value; state.draftQualificationReason=""; state.sessionError=""; renderChat(); });
  // Update state without re-rendering: the textarea lives inside an open
  // <details>, and a re-render would collapse it and take the caret with it.
  $("#chatQualificationReason")?.addEventListener("input", event => { state.draftQualificationReason=event.target.value; $("#newSessionButton").disabled=event.target.value.trim().length < 8; });
  $("#newSessionButton")?.addEventListener("click", createAgentSession);
  $$("[data-session-id]", root).forEach(button => button.addEventListener("click", () => selectSession(button.dataset.sessionId)));
  $$("[data-approve-tool]", root).forEach(button => button.addEventListener("click", () => decideToolApproval(button.dataset.approveTool, "approve")));
  $$("[data-deny-tool]", root).forEach(button => button.addEventListener("click", () => decideToolApproval(button.dataset.denyTool, "deny")));
  $("#chatForm")?.addEventListener("submit", sendTurn);
  $("#openProvidersButton")?.addEventListener("click", () => switchTab("providers"));
  requestAnimationFrame(() => { const list = $("#messageList"); if (list) list.scrollTop = list.scrollHeight; });
}

async function createAgentSession() {
  if (!state.draftProviderID || !state.draftProfileName) return;
  const provider = state.providers.find(item => item.id === state.draftProviderID);
  const profile = state.profiles.find(item => item.name === state.draftProfileName);
  const admission = profileAdmission(provider, profile);
  const project = state.projects.find(item => item.id === state.draftProjectID);
  try {
    state.sessionError = "";
    const body = {
      provider_id: state.draftProviderID,
      project_id: state.draftProjectID || "",
      context_profile: state.draftProfileName,
      // Name the session after what it is bound to, so the list is readable
      // once more than one session exists.
      title: project ? `${project.name} · ${profileLabel(profile)}` : `Chat · ${profileLabel(profile)}`
    };
    if (admission.mode === "override_required") {
      const reason = (state.draftQualificationReason.trim() || suggestedOverrideReason(provider, profile));
      body.qualification_override = { actor:"local-user", reason };
    }
    const session = await api("/api/sessions", { method:"POST", body:JSON.stringify(body) });
    state.draftQualificationReason = "";
    await load();
    await selectSession(session.id);
    switchTab("chat");
    $("#chatInput")?.focus();
  } catch (error) {
    // Keep the failure on screen. A 2.6-second toast was the only report that
    // the server had refused the session, and it was routinely missed.
    state.sessionError = error.message;
    renderChat();
  }
}

async function selectSession(id) {
  try {
    state.selectedSession = id;
    state.sessionDetail = await api(`/api/sessions/${encodeURIComponent(id)}`);
    renderChat();
  } catch (error) { toast(error.message, true); }
}

async function sendTurn(event) {
  event.preventDefault();
  if (state.sending || !state.sessionDetail?.session) return;
  const input = $("#chatInput");
  const content = input.value.trim();
  if (!content) return;
  state.sending = true;
  renderChat();
  const sessionID = state.sessionDetail.session.id;
  try {
    const response = await fetch(`/api/sessions/${encodeURIComponent(sessionID)}/turns`, { method:"POST", headers:{"Content-Type":"application/json"}, body:JSON.stringify({ content }) });
    if (!response.ok) {
      const body = await response.json().catch(() => ({}));
      throw new Error(body.error || `Turn failed (${response.status})`);
    }
    await consumeAgentStream(response);
    await selectSession(sessionID);
  } catch (error) {
    toast(error.message, true);
    await selectSession(sessionID).catch(() => {});
  } finally {
    state.sending = false;
    renderChat();
  }
}

async function decideToolApproval(id, decision) {
  if (state.sending) return;
  const approval = (state.sessionDetail?.approvals || []).find(item => item.id === id);
  if (!approval || approval.state !== "pending") return;
  const response = await askAction({
    title: decision === "approve" ? "Approve this write once?" : "Deny this write?",
    message: `${approval.summary}. The grant is bound to this exact tool call and content hash; it does not authorize later writes.`,
    confirmLabel: decision === "approve" ? "Approve exact write" : "Deny write",
    reasonLabel: decision === "deny" ? "Reason" : "",
    danger: decision === "deny"
  });
  if (!response) return;
  const sessionID = approval.session_id;
  state.sending = true;
  renderChat();
  try {
    const stream = await fetch(`/api/approvals/${encodeURIComponent(id)}/decisions`, {
      method:"POST", headers:{"Content-Type":"application/json"},
      body:JSON.stringify({ actor:"user", decision, reason:decision === "deny" ? response : "approved after preview" })
    });
    if (!stream.ok) {
      const body = await stream.json().catch(() => ({}));
      throw new Error(body.error || `Approval failed (${stream.status})`);
    }
    await consumeAgentStream(stream);
    await selectSession(sessionID);
    toast(decision === "approve" ? "Exact write approved and receipt committed" : "Write denied; Hermetrix continued without mutation");
  } catch (error) {
    toast(error.message, true);
    await selectSession(sessionID).catch(() => {});
  } finally {
    state.sending = false;
    renderChat();
  }
}

async function consumeAgentStream(response) {
  if (!response.body) throw new Error("Streaming response is unavailable");
  const reader = response.body.getReader();
  const decoder = new TextDecoder();
  let buffer = "";
  let failedMessage = "";
  while (true) {
    const { value, done } = await reader.read();
    buffer += decoder.decode(value || new Uint8Array(), { stream: !done });
    const lines = buffer.split("\n");
    buffer = lines.pop() || "";
    for (const line of lines) {
      if (!line.trim()) continue;
      const item = JSON.parse(line);
      if (item.type === "user_committed" && item.event) {
        state.sessionDetail.events.push(item.event);
        renderChat();
      } else if (item.type === "step_bound") {
        const status = $("#streamStatus");
        if (status) status.textContent = `${item.context_report?.profile || "context"} · immutable step ${shortHash(item.binding?.id)}`;
      } else if (item.type === "delta" && item.delta?.content) {
        const target = $("#streamingAssistant .message-body");
        if (target) target.textContent += item.delta.content;
        const list = $("#messageList");
        if (list) list.scrollTop = list.scrollHeight;
      } else if (item.type === "tool_call") {
        const status = $("#streamStatus");
        if (status) status.textContent = `requesting ${item.event?.metadata?.tool_name || "bound tool"}…`;
      } else if (item.type === "approval_required") {
        const status = $("#streamStatus");
        if (status) status.textContent = "paused safely · waiting for your approval";
      } else if (item.type === "approval_decision") {
        const status = $("#streamStatus");
        if (status) status.textContent = `${item.event?.metadata?.decision || "decision"} · exact approval recorded`;
      } else if (item.type === "tool_result") {
        const status = $("#streamStatus");
        if (status) status.textContent = `${item.event?.metadata?.tool_name || "tool"} · ${item.event?.metadata?.tool_status || "receipt committed"}`;
      } else if (item.type === "failed") {
        failedMessage = item.error || "Turn failed";
      }
    }
    if (done) break;
  }
  if (failedMessage) throw new Error(failedMessage);
}

function renderProviders() {
  const root = $("#view-providers");
  if (!root) return;
  root.innerHTML = `<div class="provider-grid"><div class="panel"><p class="eyebrow">Provider registry</p><h3>Add OpenAI-compatible provider</h3><form id="providerForm">
      <label>Name<input name="name" required maxlength="80" placeholder="My local gateway"></label>
      <label>Base URL<input name="base_url" required type="url" placeholder="https://host.example/v1"></label>
      <label>Model<input name="model" required maxlength="240" placeholder="qwen model ID"></label>
      <label>API key environment variable<input name="api_key_env" pattern="[A-Z][A-Z0-9_]{1,126}" placeholder="HERMETRIX_PROVIDER_API_KEY"></label>
      <div class="form-grid"><label>Context window<input name="context_window" type="number" min="4096" max="2097152" value="131072" required></label><label>Max output<input name="max_output_tokens" type="number" min="128" value="8192" required></label></div>
      <p class="form-note neutral">Hermetrix stores only the environment variable name. Secret values never enter the browser, API payload, SQLite or logs.</p>
      <button class="primary">Save provider</button>
    </form><section class="inspect-section"><h3>Qualification controls</h3><label>Requested profile<select id="qualificationProfile">${state.profiles.map(profile => `<option value="${profile.name}" ${profile.name === "certified-64k" ? "selected" : ""}>${profileLabel(profile)}</option>`).join("")}</select></label><label>Optional local runtime<select id="qualificationRuntime"><option value="">None · behavioral only</option><option value="ollama">Ollama</option><option value="lmstudio">LM Studio</option><option value="vllm">vLLM</option><option value="llamacpp">llama.cpp</option></select></label><label>Runtime endpoint<input id="qualificationEndpoint" value="http://127.0.0.1:11434"></label><p class="form-note neutral">Eligibility is never silently downgraded. Missing allocation evidence remains limited.</p></section></div>
    <div><div class="card-list">${state.providers.length ? state.providers.map(provider => `<article class="provider-card"><div class="provider-head"><div><div class="row-title"><h3>${escapeHTML(provider.name)}</h3>${pill(provider.enabled ? "enabled" : "disabled", provider.enabled ? "green" : "amber")}${pill(provider.context_evidence, provider.context_evidence === "qualified" ? "green" : "amber")}</div><p>${escapeHTML(provider.base_url)}</p></div>${pill(provider.credential_ready ? "credential ready" : "credential missing", provider.credential_ready ? "green" : "red")}</div><div class="kv"><span>Adapter</span><strong>${escapeHTML(provider.adapter_kind)}</strong><span>Model</span><span>${escapeHTML(provider.model)}</span><span>Context</span><strong>${provider.context_window.toLocaleString()}</strong><span>Output</span><span>${provider.max_output_tokens.toLocaleString()}</span><span>Secret ref</span><code>${escapeHTML(provider.api_key_env || "none")}</code></div><div class="action-row"><button class="ghost" data-test-provider="${escapeHTML(provider.id)}" ${provider.credential_ready ? "" : "disabled"}>Connectivity</button><button class="primary" data-qualify-provider="${escapeHTML(provider.id)}" ${provider.credential_ready ? "" : "disabled"}>Full qualification</button></div></article>`).join("") : `<div class="empty"><h3>No providers configured</h3><p>Add a local or remote HTTPS OpenAI-compatible endpoint. More adapter families can implement the same contract later.</p></div>`}</div><div class="card-list qualification-list">${state.qualifications.map(run => `<article class="provider-card"><div class="provider-head"><div><h3>${escapeHTML(run.provider_name)} · ${escapeHTML(run.model)}</h3><p>${formatDate(run.completed_at || run.started_at)}</p></div>${pill(`grade ${run.capability_grade}`, run.capability_grade === "A" ? "green" : run.capability_grade === "B" ? "amber" : "red")}</div><div class="kv"><span>Context tier</span><strong>${escapeHTML(run.context_tier)}</strong><span>Allocated</span><strong>${Number(run.allocated_context || 0).toLocaleString()}</strong><span>Requested</span><span>${escapeHTML(run.requested_profile)}</span><span>Eligibility</span><strong>${run.eligible ? "eligible" : "explicit decision required"}</strong><span>TTFT</span><span>${run.results.ttft_milliseconds || 0} ms</span><span>Throughput</span><span>${Number(run.results.tokens_per_second || 0).toFixed(1)} tok/s</span></div>${(run.remediation || []).length ? `<ul class="findings">${run.remediation.map(item => `<li>${escapeHTML(item)}</li>`).join("")}</ul>` : ""}</article>`).join("")}</div></div></div>`;
  $("#providerForm")?.addEventListener("submit", saveProvider);
  $$("[data-test-provider]", root).forEach(button => button.addEventListener("click", () => testProvider(button.dataset.testProvider)));
  $$("[data-qualify-provider]", root).forEach(button => button.addEventListener("click", () => qualifyProvider(button.dataset.qualifyProvider)));
}

async function qualifyProvider(id) {
  const provider = state.providers.find(item => item.id === id);
  const runtime = $("#qualificationRuntime").value;
  const input = { provider_id:id, requested_profile:$("#qualificationProfile").value };
  if (runtime) input.runtime_probe = { runtime, endpoint:$("#qualificationEndpoint").value.trim(), model:provider.model };
  const button = $(`[data-qualify-provider="${CSS.escape(id)}"]`);
  if (button) { button.disabled = true; button.textContent = "Qualifying…"; }
  try {
    const run = await api("/api/qualifications", { method:"POST", body:JSON.stringify(input) });
    toast(run.eligible ? `Certified ${run.context_tier} · grade ${run.capability_grade}` : "Qualification complete — explicit decision required");
    await load(); switchTab("providers");
  } catch (error) { toast(error.message, true); renderProviders(); }
}

async function saveProvider(event) {
  event.preventDefault();
  const form = event.currentTarget;
  const values = new FormData(form);
  try {
    await api("/api/providers", { method:"POST", body:JSON.stringify({ name:values.get("name"), adapter_kind:"openai-compatible", base_url:values.get("base_url"), model:values.get("model"), api_key_env:values.get("api_key_env"), context_window:Number(values.get("context_window")), context_evidence:"declared", max_output_tokens:Number(values.get("max_output_tokens")) }) });
    form.reset();
    toast("Provider profile saved without credential material");
    await load();
    switchTab("providers");
  } catch (error) { toast(error.message, true); }
}

async function testProvider(id) {
  const button = $(`[data-test-provider="${CSS.escape(id)}"]`);
  if (button) { button.disabled = true; button.textContent = "Testing…"; }
  try {
    const result = await api(`/api/providers/${encodeURIComponent(id)}/test`, { method:"POST", body:"{}" });
    toast(`Provider replied ${result.sample} in ${result.latency_ms}ms`);
  } catch (error) { toast(error.message, true); }
  finally { renderProviders(); }
}

function renderMCP() {
  const root = $("#view-mcp");
  if (!root) return;
  const summary = state.capability_summary || { total:0, by_source:{}, by_readiness:{} };
  const selected = state.selectedCapability ? `<article class="provider-card capability-detail"><div class="provider-head"><div><div class="row-title"><h3>${escapeHTML(state.selectedCapability.title || state.selectedCapability.name)}</h3>${pill(state.selectedCapability.effect, state.selectedCapability.requires_approval ? "amber" : "green")}${pill(state.selectedCapability.readiness, state.selectedCapability.readiness === "ready" ? "green" : "red")}</div><p>${escapeHTML(state.selectedCapability.description || "No description supplied by MCP server")}</p></div>${pill(state.selectedCapability.source)}</div><div class="kv"><span>Capability ID</span><code>${escapeHTML(state.selectedCapability.id)}</code><span>Revision</span><code>${escapeHTML(state.selectedCapability.revision)}</code><span>Source ref</span><code>${escapeHTML(state.selectedCapability.source_ref)}</code><span>Approval</span><strong>${state.selectedCapability.requires_approval ? "required" : "not required"}</strong></div><section class="inspect-section"><h3>Exact input schema</h3><pre>${escapeHTML(JSON.stringify(state.selectedCapability.input_schema, null, 2))}</pre></section><p class="form-note neutral">This schema is loaded on demand. It is not part of the direct model prompt until tool_describe is called.</p></article>` : `<div class="probe-empty">Select a search result to inspect its exact revision and schema.</div>`;
  root.innerHTML = `<div class="mcp-overview"><div class="panel"><p class="eyebrow">MCP connection registry</p><h3>Add Streamable HTTP server</h3><form id="mcpForm">
      <label>Name<input name="name" required maxlength="80" placeholder="Local knowledge tools"></label>
      <label>MCP endpoint<input name="endpoint" required type="url" placeholder="https://host.example/mcp"></label>
      <label>Bearer token environment variable<input name="api_key_env" pattern="[A-Z][A-Z0-9_]{1,126}" placeholder="HERMETRIX_MCP_API_KEY"></label>
      <div class="form-grid"><label>Protocol<select name="protocol_mode"><option value="auto">Auto · current then legacy</option><option value="2026-07-28">2026-07-28 · stateless</option><option value="2025-11-25">2025-11-25 · session</option></select></label><label>Timeout ms<input name="request_timeout_ms" type="number" min="1000" max="120000" value="15000" required></label></div>
      <label class="check-label"><input name="trust_annotations" type="checkbox"> Trust this server's risk annotations</label>
      <p class="form-note neutral">Default is fail-closed: annotations are untrusted and every remote call requires approval. Only the environment variable name is persisted.</p>
      <button class="primary">Save connection</button>
    </form></div>
    <div class="panel"><div class="provider-head"><div><p class="eyebrow">Deferred capability graph</p><h3>${Number(summary.total || 0).toLocaleString()} indexed tools</h3><p>Direct prompt remains fixed at 6 schemas.</p></div>${pill(`${summary.by_readiness?.ready || 0} ready`, "green")}</div>
      <form id="capabilitySearchForm" class="capability-search"><label>Search catalog<input id="capabilityQuery" required placeholder="calendar, repository search, database…"></label><button class="ghost">Search</button></form>
      <div id="capabilityResults">${state.capabilityResults.length ? state.capabilityResults.map(item => `<button class="capability-result" data-capability-id="${escapeHTML(item.id)}"><span><strong>${escapeHTML(item.title || item.name)}</strong><small>${escapeHTML(item.description || "No description")}</small></span><span>${pill(item.effect, item.requires_approval ? "amber" : "green")}${pill(item.readiness, item.readiness === "ready" ? "green" : "red")}</span></button>`).join("") : `<div class="probe-empty">Search returns bounded metadata only—never the complete catalog schemas.</div>`}</div>
      ${selected}
    </div></div>
    <div class="card-list mcp-server-list">${state.mcp_servers.length ? state.mcp_servers.map(server => `<article class="provider-card"><div class="provider-head"><div><div class="row-title"><h3>${escapeHTML(server.name)}</h3>${pill(server.status, server.status === "ready" ? "green" : server.status === "error" ? "red" : "amber")}${pill(server.last_protocol || server.protocol_mode, "blue")}</div><p>${escapeHTML(server.endpoint)}</p></div>${pill(server.credential_ready ? "credential ready" : "credential missing", server.credential_ready ? "green" : "red")}</div><div class="kv"><span>Transport</span><strong>${escapeHTML(server.transport_kind)}</strong><span>Tools</span><strong>${Number(server.tool_count).toLocaleString()}</strong><span>Timeout</span><span>${server.request_timeout_ms.toLocaleString()} ms</span><span>Risk hints</span><strong>${server.trust_annotations ? "trusted by user" : "untrusted · approval default"}</strong><span>Secret ref</span><code>${escapeHTML(server.api_key_env || "none")}</code><span>Discovered</span><span>${formatDate(server.last_discovered_at)}</span></div>${server.last_error ? `<ul class="findings"><li class="error">${escapeHTML(server.last_error)}</li></ul>` : ""}<div class="action-row"><button class="ghost" data-discover-mcp="${escapeHTML(server.id)}" ${server.enabled && server.credential_ready ? "" : "disabled"}>Discover atomically</button></div></article>`).join("") : `<div class="empty"><h3>No MCP connections</h3><p>Add a Streamable HTTP endpoint. Discovery is explicit and replaces each server snapshot atomically.</p></div>`}</div>`;
  $("#mcpForm")?.addEventListener("submit", saveMCPServer);
  $("#capabilitySearchForm")?.addEventListener("submit", searchCapabilities);
  $$('[data-discover-mcp]', root).forEach(button => button.addEventListener("click", () => discoverMCPServer(button.dataset.discoverMcp)));
  $$('[data-capability-id]', root).forEach(button => button.addEventListener("click", () => inspectCapability(button.dataset.capabilityId)));
}

async function saveMCPServer(event) {
  event.preventDefault();
  const form = event.currentTarget;
  const values = new FormData(form);
  try {
    const saved = await api("/api/mcp/servers", { method:"POST", body:JSON.stringify({ name:values.get("name"), transport_kind:"streamable-http", endpoint:values.get("endpoint"), api_key_env:values.get("api_key_env"), protocol_mode:values.get("protocol_mode"), trust_annotations:values.get("trust_annotations") === "on", request_timeout_ms:Number(values.get("request_timeout_ms")) }) });
    form.reset();
    toast(`MCP connection ${saved.name} saved — run discovery next`);
    await load();
    switchTab("mcp");
  } catch (error) { toast(error.message, true); }
}

async function discoverMCPServer(id) {
  const button = $(`[data-discover-mcp="${CSS.escape(id)}"]`);
  if (button) { button.disabled = true; button.textContent = "Discovering…"; }
  try {
    const result = await api(`/api/mcp/servers/${encodeURIComponent(id)}/discover`, { method:"POST", body:"{}" });
    toast(`Indexed ${result.tools} tools via MCP ${result.protocol}${result.rejected ? ` · rejected ${result.rejected}` : ""}`);
    await load();
    switchTab("mcp");
  } catch (error) { toast(error.message, true); renderMCP(); }
}

async function searchCapabilities(event) {
  event.preventDefault();
  const query = $("#capabilityQuery").value.trim();
  if (!query) return;
  try {
    const result = await api(`/api/capabilities?query=${encodeURIComponent(query)}&limit=20`);
    state.capabilityResults = result.results || [];
    state.selectedCapability = null;
    renderMCP();
  } catch (error) { toast(error.message, true); }
}

async function inspectCapability(id) {
  try {
    state.selectedCapability = await api(`/api/capabilities/${encodeURIComponent(id)}`);
    renderMCP();
  } catch (error) { toast(error.message, true); }
}

function renderContext() {
  const root = $("#view-context");
  const profiles = state.profiles.map(profile => `<option value="${profile.name}" ${profile.name === "compact-32k" ? "selected" : ""}>${profileLabel(profile)} · ${profile.total.toLocaleString()} tokens</option>`).join("");
  root.innerHTML = `<div class="context-grid"><div class="panel-stack"><div class="panel"><p class="eyebrow">Runtime truth</p><h3>Verify local model context</h3><div class="form-grid model-grid"><label>Runtime<select id="runtimeSelect"><option value="ollama">Ollama</option><option value="lmstudio">LM Studio</option><option value="vllm">vLLM</option><option value="llamacpp">llama.cpp</option></select></label><label>Model<input id="modelInput" placeholder="qwen3-coder:30b" required></label></div><label>Local endpoint<input id="endpointInput" value="http://127.0.0.1:11434" spellcheck="false"></label><button class="ghost" id="probeButton">Probe allocation</button><div id="probeResult">${renderProbeResult(state.modelProbe)}</div><p class="form-note neutral">Certification here covers allocated context only—not tool calling, structured output, or task quality.</p></div><div class="panel"><p class="eyebrow">Compression laboratory</p><h3>Compile a diagnostic context</h3><label>Runtime profile<select id="profileSelect">${profiles}</select></label><label>Conversation sample<textarea id="contextSample" rows="12">ผู้ใช้ต้องการสร้างระบบ Skill lifecycle ที่ทุกการเปลี่ยนแปลงตรวจสอบและย้อนกลับได้\n\nWe inspected the repository, ran tests, and recorded tool receipts. Preserve the exact goal, acceptance criteria, current decisions, and unresolved failures while compressing older narrative.</textarea></label><label>Worst-case next tool burst<input id="burstInput" type="number" min="0" max="8192" value="1024"></label><button class="primary" id="compileButton">Compile & inspect</button></div></div><div class="panel" id="contextResult">${state.contextResult ? renderContextResult(state.contextResult) : `<div class="empty"><h3>No compilation yet</h3><p>The compiler will fail rather than silently drop pinned goals or overflow direct-tool schemas.</p></div>`}</div></div>`;
  $("#compileButton").addEventListener("click", compileDiagnostic);
  $("#probeButton").addEventListener("click", probeLocalModel);
  $("#runtimeSelect").addEventListener("change", event => {
    const defaults = { ollama:"http://127.0.0.1:11434", lmstudio:"http://127.0.0.1:1234", vllm:"http://127.0.0.1:8000", llamacpp:"http://127.0.0.1:8080" };
    $("#endpointInput").value = defaults[event.target.value];
  });
  applyDataFills(root);
}

// applyDataFills sizes every [data-fill] bar through CSSOM, which the Content
// Security Policy permits, unlike the style attribute these used to carry.
function applyDataFills(root) {
  $$("[data-fill]", root).forEach(node => { node.style.width = `${node.dataset.fill}%`; });
}

function renderProbeResult(result) {
  if (!result) return `<div class="probe-empty">No runtime allocation verified.</div>`;
  const tone = result.mode === "certified-context" ? "green" : result.mode === "compact-context" ? "amber" : "red";
  const warnings = (result.warnings || []).map(item => `<li>${escapeHTML(item)}</li>`).join("");
  return `<div class="probe-result"><div class="probe-title">${pill(result.mode, tone)}<strong>${result.verified ? "runtime verified" : "metadata only"}</strong></div><div class="kv"><span>Allocated</span><strong>${result.allocated_context ? result.allocated_context.toLocaleString() : "unverified"}</strong><span>Configured</span><strong>${result.configured_context ? result.configured_context.toLocaleString() : "not reported"}</strong><span>Training max</span><strong>${result.training_context ? result.training_context.toLocaleString() : "not reported"}</strong><span>Source</span><span class="hash">${escapeHTML(result.context_source)}</span></div>${warnings ? `<ul class="findings">${warnings}</ul>` : ""}</div>`;
}

async function probeLocalModel() {
  const model = $("#modelInput").value.trim();
  if (!model) { toast("Enter the exact loaded model name", true); return; }
  try {
    state.modelProbe = await api("/api/local-model/probe", { method:"POST", body:JSON.stringify({ runtime:$("#runtimeSelect").value, endpoint:$("#endpointInput").value.trim(), model }) });
    renderContext();
    toast(state.modelProbe.mode === "certified-context" ? "64k+ runtime context verified" : "Runtime probed — compact mode may be required");
  } catch (error) { toast(error.message, true); }
}

function renderContextResult(result) {
  const report = result.report;
  // The fill carries its percentage as a data attribute and is sized through
  // CSSOM once the markup is in the document. A `style` attribute is blocked by
  // `style-src 'self'`, which left every bar at full width regardless of usage.
  const bars = Object.entries(report.slices).map(([name, slice]) => `<div class="budget-row"><div class="budget-label"><span>${escapeHTML(name)}</span><span>${slice.used.toLocaleString()} / ${slice.budget.toLocaleString()}</span></div><div class="bar"><i data-fill="${Math.min(100, slice.budget ? slice.used / slice.budget * 100 : 0).toFixed(2)}"></i></div></div>`).join("");
  const preview = result.fragments.map(item => {
    const content = item.content.length > 3600 ? `${item.content.slice(0,2600)}\n… preview clipped in UI …\n${item.content.slice(-700)}` : item.content;
    return `[${item.kind}:${item.id}]\n${content}`;
  }).join("\n\n");
  return `<h3>${escapeHTML(report.profile)}</h3><div class="kv"><span>Input</span><strong>${report.predicted_input.toLocaleString()}</strong><span>Original</span><strong>${report.original_tokens.toLocaleString()}</strong><span>Selected</span><strong>${report.selected_tokens.toLocaleString()}</strong><span>Dropped</span><strong>${report.dropped_tokens.toLocaleString()}</strong><span>Checkpoint</span><strong>${report.compacted_tokens.toLocaleString()}</strong><span>Output reserve</span><strong>${report.output_reserve.toLocaleString()}</strong><span>Free</span><strong>${report.free.toLocaleString()}</strong><span>Ratio</span><strong>${(report.compression_ratio*100).toFixed(1)}%</strong><span>Essentials</span><strong>${(report.integrity.essential_retention*100).toFixed(0)}%</strong><span>Causal pairs</span><strong>${report.integrity.causal_pairs_selected} live · ${report.integrity.causal_pairs_compacted} compact · ${report.integrity.causal_pairs_omitted} omitted</strong><span>Spilled</span><strong>${report.spilled.length}</strong></div><section class="inspect-section"><h3>Slice usage</h3>${bars}</section><section class="inspect-section"><h3>Selected context preview</h3><pre>${escapeHTML(preview)}</pre></section>`;
}

async function compileDiagnostic() {
  const sample = $("#contextSample").value;
  const now = new Date();
  const fragments = [
    { id:"policy:learning", kind:"policy", scope:"runtime", provenance:"hermetrix", trust:"system", version:"v1", priority:100, pinned:false, cache_class:"stable", content:"Skill changes are proposal-only. Never promote, merge, archive, or widen authority without an approval event.", created_at:now },
    { id:"goal:current", kind:"user_goal", scope:"session", provenance:"user", trust:"user", version:"v1", priority:100, pinned:true, cache_class:"session", content:sample.split("\n")[0] || sample, created_at:now },
    { id:"criteria:current", kind:"acceptance_criteria", scope:"session", provenance:"user", trust:"user", version:"v1", priority:98, pinned:true, cache_class:"session", content:"Preserve goal, reversible skill mutations, explicit provenance, output reserve, and causal tool pairs.", created_at:now },
    { id:"conversation:sample", kind:"conversation", scope:"session", provenance:"session", trust:"user", version:"v1", priority:70, pinned:false, cache_class:"rolling", content:(sample+"\n").repeat(160), created_at:now },
    { id:"tool:call", kind:"tool_call", scope:"session", provenance:"assistant", trust:"assistant", version:"v1", priority:60, pinned:false, cache_class:"rolling", pair_id:"demo-tool", content:'filesystem.read {"path":"review.log"}', created_at:now },
    { id:"tool:large", kind:"tool_result", scope:"session", provenance:"tool:test", trust:"tool", version:"v1", priority:60, pinned:false, cache_class:"rolling", pair_id:"demo-tool", content:("test output line — ผลการทดสอบยังคงอ้างอิงได้\n").repeat(800), created_at:now }
  ];
  const direct_tools = [
    { name:"filesystem.read", revision:"v1", source:"core", schema:'{"type":"object","properties":{"path":{"type":"string"}},"required":["path"]}' },
    { name:"tool_search", revision:"v1", source:"core", schema:'{"type":"object","properties":{"query":{"type":"string"}},"required":["query"]}' }
  ];
  try {
    const result = await api("/api/context/compile", { method:"POST", body:JSON.stringify({ profile_name:$("#profileSelect").value, fragments, direct_tools, worst_case_tool_burst:Number($("#burstInput").value) || 0 }) });
    state.contextResult = result;
    renderContext();
    toast("Context compiled within reserve");
  } catch (error) { toast(error.message, true); }
}

function renderProjects() {
  const root = $("#view-projects");
  if (!root) return;
  const project = state.projects.find(item => item.id === state.selectedProject);
  root.innerHTML = `<div class="workbench-grid"><div class="panel"><p class="eyebrow">Bounded workspace registry</p><h3>Add project</h3><form id="projectForm"><label>Name<input name="name" required maxlength="100" placeholder="My workspace"></label><label>Existing local root<input name="root_path" required placeholder="/absolute/path"></label><button class="primary">Register project</button></form><section class="inspect-section"><h3>Projects</h3><div class="project-list">${state.projects.map(item => `<button class="session-item ${item.id === state.selectedProject ? "active" : ""}" data-project-id="${escapeHTML(item.id)}"><strong>${escapeHTML(item.name)}</strong><span>${escapeHTML(item.root_path)}</span></button>`).join("")}</div></section></div>
    <div class="panel"><div class="provider-head"><div><p class="eyebrow">Project workbench</p><h3>${escapeHTML(project?.name || "Select a project")}</h3><p>${escapeHTML(project?.root_path || "")}</p></div>${project ? pill(project.state,"green") : ""}</div>${project ? `<div class="file-browser"><div class="file-path"><code>${escapeHTML(state.projectPath || ".")}</code>${state.projectPath ? `<button class="ghost" id="projectUpButton">Up</button>` : ""}</div>${state.projectFiles.map(item => `<button class="file-row" data-file-path="${escapeHTML(item.path)}" data-directory="${item.directory}"><span>${item.directory ? "◇" : "·"}</span><strong>${escapeHTML(item.name)}</strong><small>${item.directory ? "directory" : `${Number(item.bytes).toLocaleString()} bytes`}</small></button>`).join("") || `<div class="probe-empty">Directory is empty.</div>`}</div><section class="inspect-section"><h3>Direct background command</h3><form id="commandForm"><div class="form-grid"><label>Executable<select name="executable"><option>go</option><option>git</option><option>node</option><option>npm</option><option>python3</option><option>rg</option><option>ls</option></select></label><label>Timeout seconds<input name="timeout" type="number" min="1" max="120" value="30"></label></div><label>Arguments as JSON array<textarea name="arguments" rows="3">["test", "./..."]</textarea></label><label>Working directory<input name="working_dir" value="${escapeHTML(state.projectPath || ".")}"></label><p class="form-note neutral">No shell is involved. Executable allowlist, root boundary, minimal environment, timeout, output limit and process-group cancellation are enforced server-side.</p><button class="primary">Start background job</button></form></section>` : `<div class="empty"><h3>No project selected</h3><p>Register an existing local directory to create a bounded workbench.</p></div>`}</div></div>`;
  $("#projectForm")?.addEventListener("submit", createProject);
  $$('[data-project-id]', root).forEach(button => button.addEventListener("click", () => selectProject(button.dataset.projectId, "")));
  $$('[data-file-path]', root).forEach(button => button.addEventListener("click", () => { if (button.dataset.directory === "true") selectProject(state.selectedProject, button.dataset.filePath); }));
  $("#projectUpButton")?.addEventListener("click", () => selectProject(state.selectedProject, (state.projectPath || "").split("/").slice(0,-1).join("/")));
  $("#commandForm")?.addEventListener("submit", startCommand);
}

async function createProject(event) {
  event.preventDefault();
  const form = new FormData(event.currentTarget);
  try {
    const project = await api("/api/projects", { method:"POST", body:JSON.stringify({ name:form.get("name"), root_path:form.get("root_path") }) });
    state.selectedProject = project.id; state.projectPath = ""; toast("Project registered with a bounded root"); await load(); switchTab("projects");
  } catch (error) { toast(error.message, true); }
}

async function selectProject(id, path = "") {
  try {
    state.selectedProject = id; state.projectPath = path;
    state.projectFiles = await api(`/api/projects/${encodeURIComponent(id)}/files?path=${encodeURIComponent(path)}`);
    renderProjects();
  } catch (error) { toast(error.message, true); }
}

async function startCommand(event) {
  event.preventDefault();
  const form = new FormData(event.currentTarget);
  let args;
  try { args = JSON.parse(form.get("arguments")); if (!Array.isArray(args) || !args.every(item => typeof item === "string")) throw new Error(); }
  catch { toast("Arguments must be a JSON array of strings", true); return; }
  try {
    await api(`/api/projects/${encodeURIComponent(state.selectedProject)}/commands`, { method:"POST", body:JSON.stringify({ actor:"user", executable:form.get("executable"), arguments:args, working_dir:form.get("working_dir"), timeout_seconds:Number(form.get("timeout")) }) });
    toast("Background job started with a direct process binding"); await load(); switchTab("office");
  } catch (error) { toast(error.message, true); }
}

function renderOffice() {
  const root = $("#view-office");
  if (!root) return;
  root.innerHTML = `<div class="panel"><div class="provider-head"><div><p class="eyebrow">Background execution office</p><h3>Durable jobs and learning reviews</h3><p>Commands are never silently retried after restart; uncertain effects remain visible.</p></div>${pill(`${state.jobs.filter(item => ["queued","running"].includes(item.state)).length} active`,"blue")}</div></div><div class="office-grid"><div class="card-list">${state.jobs.map(job => `<article class="provider-card"><div class="provider-head"><div><h3>${escapeHTML(job.payload.executable || job.kind)}</h3><p>${formatDate(job.started_at || job.created_at)}</p></div>${pill(job.state, job.state === "completed" ? "green" : job.state === "failed" ? "red" : "amber")}</div><div class="kv"><span>Job</span><code>${escapeHTML(job.id)}</code><span>Arguments</span><code>${escapeHTML(JSON.stringify(job.payload.arguments || []))}</code><span>Exit</span><strong>${job.result.exit_code ?? "—"}</strong><span>Duration</span><span>${job.result.duration_ms ?? 0} ms</span><span>Artifact</span><code>${escapeHTML(job.result.artifact_id || "pending")}</code></div>${job.error ? `<ul class="findings"><li class="error">${escapeHTML(job.error)}</li></ul>` : ""}${["queued","running"].includes(job.state) ? `<div class="action-row"><button class="danger" data-cancel-job="${escapeHTML(job.id)}">Cancel process group</button></div>` : ""}</article>`).join("") || `<div class="empty"><h3>No command jobs</h3><p>Start an allowlisted command from a project workbench.</p></div>`}</div><div class="card-list">${state.reviews.map(item => `<article class="provider-card"><div class="provider-head"><div><h3>${escapeHTML(item.trigger_kind)}</h3><p>${escapeHTML(item.digest.goal_and_constraints || "Structured digest")}</p></div>${pill(item.state,item.state === "completed" ? "green" : "amber")}</div><div class="meta">${pill(item.reviewer_revision)}${item.candidate_id ? pill("candidate","blue") : ""}</div></article>`).join("") || `<div class="empty"><h3>No learning reviews</h3><p>Only valid lifecycle triggers create review jobs.</p></div>`}</div></div>`;
  $$('[data-cancel-job]', root).forEach(button => button.addEventListener("click", () => cancelBackgroundJob(button.dataset.cancelJob)));
}

async function cancelBackgroundJob(id) {
  try { await api(`/api/jobs/${encodeURIComponent(id)}/cancel`, { method:"POST", body:"{}" }); toast("Cancellation requested; process group will be terminated"); await load(); switchTab("office"); }
  catch (error) { toast(error.message, true); }
}

function renderArtifacts() {
  const root = $("#view-artifacts");
  if (!root) return;
  root.innerHTML = `<div class="workbench-grid"><div class="panel"><p class="eyebrow">Content-addressed outputs</p><h3>Create artifact</h3><form id="artifactForm"><label>Project<select name="project_id"><option value="">Global</option>${state.projects.map(item => `<option value="${item.id}">${escapeHTML(item.name)}</option>`).join("")}</select></label><label>Name<input name="name" required placeholder="report.md"></label><div class="form-grid"><label>Kind<input name="kind" value="report" required></label><label>MIME type<input name="mime_type" value="text/markdown" required></label></div><label>Content<textarea name="content" rows="10" required></textarea></label><button class="primary">Save immutable artifact</button></form></div><div class="card-list">${state.artifacts.map(item => `<article class="provider-card"><div class="provider-head"><div><h3>${escapeHTML(item.name)}</h3><p>${escapeHTML(item.mime_type)} · ${Number(item.byte_size).toLocaleString()} bytes</p></div>${pill(item.kind,"blue")}</div><div class="kv"><span>Checksum</span><code>${escapeHTML(item.checksum)}</code><span>Project</span><span>${escapeHTML(state.projects.find(project => project.id === item.project_id)?.name || "global")}</span><span>Created</span><span>${formatDate(item.created_at)}</span></div><div class="action-row"><a class="button-link" href="/api/artifacts/${encodeURIComponent(item.id)}/content" target="_blank" rel="noreferrer">Open verified content</a></div></article>`).join("") || `<div class="empty"><h3>No artifacts</h3><p>Command logs and generated outputs will appear here.</p></div>`}</div></div>`;
  $("#artifactForm")?.addEventListener("submit", createArtifact);
}

async function createArtifact(event) {
  event.preventDefault(); const form = new FormData(event.currentTarget);
  try { await api("/api/artifacts", { method:"POST", body:JSON.stringify({ project_id:form.get("project_id"), name:form.get("name"), kind:form.get("kind"), mime_type:form.get("mime_type"), content:form.get("content"), metadata:{ created_by:"user" } }) }); toast("Artifact stored by checksum"); await load(); switchTab("artifacts"); }
  catch (error) { toast(error.message, true); }
}

function renderFidelity() {
  const root = $("#view-fidelity");
  if (!root) return;
  root.innerHTML = `<div class="panel"><div class="provider-head"><div><p class="eyebrow">Full-context vs compiled-context evidence</p><h3>Bilingual context fidelity laboratory</h3><p>Measures exact essentials, decisions, open tasks, file state, causal pairs, task/patch delta, hallucination and fallback.</p></div>${pill(`${state.fidelityCases.length} cases`,"blue")}</div><label>Run profile<select id="fidelityProfile">${state.profiles.map(profile => `<option value="${profile.name}" ${profile.name === "compact-32k" ? "selected" : ""}>${profileLabel(profile)}</option>`).join("")}</select></label></div><div class="fidelity-grid"><div class="card-list">${state.fidelityCases.map(item => `<article class="provider-card"><div class="provider-head"><div><h3>${escapeHTML(item.name)}</h3><p>${escapeHTML(item.benchmark_class)} · ${escapeHTML(item.language)}</p></div>${pill(`${item.fragments.length} fragments`)}</div><div class="action-row"><button class="primary" data-run-fidelity="${escapeHTML(item.id)}">Force compaction test</button></div></article>`).join("")}</div><div class="card-list">${state.fidelityRuns.map(run => `<article class="provider-card"><div class="provider-head"><div><h3>${escapeHTML(run.case_name)}</h3><p>${escapeHTML(run.profile_name)} · ${formatDate(run.completed_at || run.started_at)}</p></div>${pill(run.metrics.passed ? "passed" : "failed",run.metrics.passed ? "green" : "red")}</div><div class="kv"><span>Essentials</span><strong>${Math.round(run.metrics.essential_exact_retention*100)}%</strong><span>Decisions</span><strong>${Math.round(run.metrics.decision_recall*100)}%</strong><span>Open tasks</span><strong>${Math.round(run.metrics.open_task_recall*100)}%</strong><span>Causal splits</span><strong>${run.metrics.causal_pair_splits}</strong><span>Tokens saved</span><strong>${Number(run.metrics.tokens_saved).toLocaleString()}</strong><span>Hallucinations</span><strong>${run.metrics.hallucination_count}</strong><span>Fallback</span><strong>${run.metrics.fallback_used ? "verified fallback" : "not used"}</strong></div></article>`).join("") || `<div class="empty"><h3>No fidelity runs</h3><p>Run a seeded Thai or English forced-compaction case.</p></div>`}</div></div>`;
  $$('[data-run-fidelity]', root).forEach(button => button.addEventListener("click", () => runFidelity(button.dataset.runFidelity)));
}

async function runFidelity(id) {
  try { const run = await api(`/api/fidelity/cases/${encodeURIComponent(id)}/run`, { method:"POST", body:JSON.stringify({ profile_name:$("#fidelityProfile").value }) }); toast(run.metrics.passed ? "Fidelity evidence passed" : "Fidelity regression detected", !run.metrics.passed); await load(); switchTab("fidelity"); }
  catch (error) { toast(error.message, true); }
}

function renderMaintenance() {
  const root = $("#view-maintenance");
  if (!root) return;
  const usage = state.usage || {};
  root.innerHTML = `<div class="maintenance-grid"><div class="panel-stack"><div class="panel"><p class="eyebrow">Usage & provenance</p><h3>Derived runtime totals</h3><div class="kv"><span>Sessions</span><strong>${usage.sessions || 0}</strong><span>Model steps</span><strong>${usage.model_steps || 0}</strong><span>Tool calls</span><strong>${usage.tool_calls || 0}</strong><span>Tool success</span><strong>${usage.tool_succeeded || 0}</strong><span>Total tokens</span><strong>${Number(usage.total_tokens || 0).toLocaleString()}</strong></div></div><div class="panel"><p class="eyebrow">Safe settings</p><h3>Non-secret JSON settings</h3><form id="settingForm"><label>Key<input name="key" required value="retention.skill_activation_days"></label><label>JSON value<input name="value" required value="365"></label><button class="ghost">Save setting</button></form><div class="meta">${state.settings.map(item => pill(`${item.key}=${JSON.stringify(item.value)}`)).join("")}</div></div><div class="panel"><p class="eyebrow">Explicit memory</p><h3>User-controlled memory</h3><form id="memoryForm"><div class="form-grid"><label>Scope<select name="scope_kind"><option value="user">User</option><option value="project">Project</option></select></label><label>Project<select name="scope_ref"><option value="">None</option>${state.projects.map(item => `<option value="${item.id}">${escapeHTML(item.name)}</option>`).join("")}</select></label></div><label>Kind<input name="memory_kind" value="preference" required></label><label>Content<textarea name="content" rows="3" required></textarea></label><button class="ghost">Save explicit memory</button></form><div class="card-list">${state.memories.map(item => `<article class="memory-row"><div><strong>${escapeHTML(item.memory_kind)}</strong><p>${escapeHTML(item.content)}</p></div>${pill(item.state,item.state === "active" ? "green" : "amber")}${item.state === "active" ? `<button class="danger" data-archive-memory="${item.id}">Archive</button>` : ""}</article>`).join("")}</div></div></div>
    <div class="panel-stack"><div class="panel"><div class="provider-head"><div><p class="eyebrow">Verified backup</p><h3>Export / preview / candidate-only import</h3></div><button class="primary" id="exportBackupButton">Export now</button></div><label>Import backup<input id="importBackupFile" type="file" accept="application/json,.json"></label><button class="ghost" id="previewImportButton">Verify & preview import</button><div class="card-list">${state.backups.map(item => `<article class="provider-card"><div class="provider-head"><div><h3>${escapeHTML(item.kind)}</h3><p>${formatDate(item.created_at)}</p></div>${pill(item.state,item.state === "completed" || item.state === "imported" ? "green" : "amber")}</div><div class="kv"><span>Checksum</span><code>${escapeHTML(item.checksum || "pending")}</code><span>Skills</span><strong>${item.counts.skills || 0}</strong><span>Conflicts</span><strong>${item.counts.skill_conflicts || 0}</strong></div><div class="action-row">${item.kind === "export" && item.state === "completed" ? `<a class="button-link" href="/api/backups/${item.id}/download">Download</a>` : ""}${item.kind === "import_preview" && item.state === "awaiting_apply" ? `<button class="primary" data-apply-import="${item.id}">Apply as candidates</button>` : ""}</div></article>`).join("")}</div></div>
      <div class="panel"><div class="provider-head"><div><p class="eyebrow">Background policy</p><h3>Maintenance schedules</h3></div><button class="ghost" id="runDueButton">Run due when policy allows</button></div><form id="scheduleForm"><label>Name<input name="name" value="Weekly curator" required></label><div class="form-grid"><label>Task<select name="task_kind"><option value="curator">Curator report</option><option value="gc_dry_run">GC dry-run</option></select></label><label>Interval seconds<input name="interval_seconds" type="number" min="300" value="604800"></label></div><label class="check-label"><input name="enabled" type="checkbox"> Enabled</label><label class="check-label"><input name="require_idle" type="checkbox" checked> Require 5-minute idle</label><label class="check-label"><input name="require_ac_power" type="checkbox" checked> Require AC power</label><button class="ghost">Save schedule</button></form><div class="meta">${state.schedules.map(item => pill(`${item.name} · ${item.enabled ? "enabled" : "disabled"} · next ${formatDate(item.next_run_at)}`)).join("")}</div></div>
      <div class="panel"><div class="provider-head"><div><p class="eyebrow">Recoverable CAS GC</p><h3>Dry-run → exact snapshot → quarantine</h3></div><button class="primary" id="dryRunGCButton">New dry-run</button></div><div class="card-list">${state.gcRuns.map(run => `<article class="provider-card"><div class="provider-head"><div><h3>${run.unreachable_count} unreachable · ${Number(run.reclaimable_bytes).toLocaleString()} bytes</h3><p>${formatDate(run.created_at)} · ${shortHash(run.snapshot_revision)}</p></div>${pill(run.state,run.state === "restored" ? "green" : "amber")}</div><div class="action-row">${run.state === "planned" ? `<button class="danger" data-apply-gc="${run.id}">Quarantine exact set</button>` : ""}${["quarantined","partial_quarantine"].includes(run.state) ? `<button class="ghost" data-restore-gc="${run.id}">Restore quarantine</button>` : ""}</div></article>`).join("")}</div></div></div></div>`;
  $("#settingForm")?.addEventListener("submit", saveSetting);
  $("#memoryForm")?.addEventListener("submit", saveMemory);
  $$('[data-archive-memory]', root).forEach(button => button.addEventListener("click", () => archiveMemory(button.dataset.archiveMemory)));
  $("#exportBackupButton")?.addEventListener("click", exportBackup);
  $("#previewImportButton")?.addEventListener("click", previewImport);
  $$('[data-apply-import]', root).forEach(button => button.addEventListener("click", () => applyImport(button.dataset.applyImport)));
  $("#scheduleForm")?.addEventListener("submit", saveSchedule);
  $("#runDueButton")?.addEventListener("click", runDueMaintenance);
  $("#dryRunGCButton")?.addEventListener("click", dryRunGC);
  $$('[data-apply-gc]', root).forEach(button => button.addEventListener("click", () => applyGC(button.dataset.applyGc)));
  $$('[data-restore-gc]', root).forEach(button => button.addEventListener("click", () => restoreGC(button.dataset.restoreGc)));
}

async function saveSetting(event) { event.preventDefault(); const form = new FormData(event.currentTarget); let value; try { value=JSON.parse(form.get("value")); } catch { toast("Setting value must be valid JSON",true); return; } try { await api("/api/settings",{method:"PUT",body:JSON.stringify({key:form.get("key"),value})}); toast("Non-secret setting saved"); await load(); switchTab("maintenance"); } catch(error){toast(error.message,true);} }
async function saveMemory(event) { event.preventDefault(); const form=new FormData(event.currentTarget); const scope=form.get("scope_kind"); try { await api("/api/memories",{method:"POST",body:JSON.stringify({scope_kind:scope,scope_ref:scope === "project" ? form.get("scope_ref") : "",memory_kind:form.get("memory_kind"),content:form.get("content"),source:"user"})}); toast("Explicit user memory saved"); await load(); switchTab("maintenance"); } catch(error){toast(error.message,true);} }
async function archiveMemory(id) { try { await api(`/api/memories/${encodeURIComponent(id)}/archive`,{method:"POST",body:"{}"}); toast("Memory archived"); await load(); switchTab("maintenance"); } catch(error){toast(error.message,true);} }
async function exportBackup() { try { const run=await api("/api/backups",{method:"POST",body:JSON.stringify({actor:"user"})}); toast("Verified backup completed"); await load(); window.location.href=`/api/backups/${encodeURIComponent(run.id)}/download`; } catch(error){toast(error.message,true);} }
async function previewImport() { const file=$("#importBackupFile").files[0]; if(!file){toast("Choose a backup file",true);return;} try { const response=await fetch("/api/imports/preview?actor=user",{method:"POST",headers:{"Content-Type":"application/vnd.hermetrix.backup+json"},body:file}); const body=await response.json(); if(!response.ok)throw new Error(body.error); toast(`Verified import preview · ${body.skill_conflicts} skill conflicts`); await load(); switchTab("maintenance"); } catch(error){toast(error.message,true);} }
async function applyImport(id) { const approved=await askAction({title:"Restore backup as candidates?",message:"Blobs are checksum-verified. Skills become reviewable candidates only; active skills are never overwritten.",confirmLabel:"Create candidates"}); if(!approved)return; try { const result=await api(`/api/imports/${encodeURIComponent(id)}/apply`,{method:"POST",body:JSON.stringify({actor:"user"})}); toast(`Created ${result.candidate_ids.length} candidates · ${result.conflicts} conflicts`); await load(); switchTab("proposals"); } catch(error){toast(error.message,true);} }
async function saveSchedule(event) { event.preventDefault(); const form=new FormData(event.currentTarget); try { await api("/api/maintenance/schedules",{method:"POST",body:JSON.stringify({name:form.get("name"),task_kind:form.get("task_kind"),interval_seconds:Number(form.get("interval_seconds")),enabled:form.get("enabled")==="on",require_idle:form.get("require_idle")==="on",require_ac_power:form.get("require_ac_power")==="on"})}); toast("Maintenance schedule saved"); await load(); switchTab("maintenance"); } catch(error){toast(error.message,true);} }
async function runDueMaintenance() { try { const detected=await api("/api/maintenance/system-state"); const runs=await api("/api/maintenance/run-due",{method:"POST",body:JSON.stringify(detected)}); toast(runs.length ? `Evaluated ${runs.length} due schedules` : "No schedules are due"); await load(); switchTab("maintenance"); } catch(error){toast(error.message,true);} }
async function dryRunGC() { try { const run=await api("/api/maintenance/gc/dry-run",{method:"POST",body:"{}"}); toast(`GC dry-run found ${run.unreachable_count} unreachable objects`); await load(); switchTab("maintenance"); } catch(error){toast(error.message,true);} }
async function applyGC(id) { const approved=await askAction({title:"Quarantine exact GC snapshot?",message:"The CAS set must still match the dry-run. Objects are moved to recoverable quarantine, never deleted.",confirmLabel:"Quarantine exact set",danger:true}); if(!approved)return; try { await api(`/api/maintenance/gc/${encodeURIComponent(id)}/apply`,{method:"POST",body:JSON.stringify({actor:"user"})}); toast("Exact snapshot moved to recoverable quarantine"); await load(); switchTab("maintenance"); } catch(error){toast(error.message,true);} }
async function restoreGC(id) { try { await api(`/api/maintenance/gc/${encodeURIComponent(id)}/restore`,{method:"POST",body:JSON.stringify({actor:"user"})}); toast("Quarantined CAS objects restored after integrity verification"); await load(); switchTab("maintenance"); } catch(error){toast(error.message,true);} }

// TAB_GROUPS keeps the tab strip contextual. Every one of the fourteen views
// used to be listed on every screen, next to a sidebar that already named ten of
// them, so the strip carried no information about where you were. A view now
// shows only its siblings, and a view with no siblings shows no strip at all.
const TAB_GROUPS = [
  ["library", "proposals", "learning", "insights", "archive"],
  ["projects", "office", "artifacts"],
  ["providers", "mcp"],
  ["context", "fidelity"]
];

function switchTab(tab) {
  state.activeTab = tab;
  const group = TAB_GROUPS.find(names => names.includes(tab)) || [];
  $$(".tab").forEach(node => {
    node.classList.toggle("active", node.dataset.tab === tab);
    node.hidden = !group.includes(node.dataset.tab);
  });
  $(".tabbar").hidden = group.length === 0;
  $$(".nav-item[data-tab]").forEach(node => node.classList.toggle("active", node.dataset.tab === tab || (node.dataset.tab === "library" && ["proposals","learning","insights","archive"].includes(tab))));
  $$(".view").forEach(node => node.classList.toggle("active", node.id === `view-${tab}`));
  $("#libraryToolbar").hidden = tab !== "library";
  const skillTabs = ["library", "proposals", "learning", "insights", "archive"];
  $("#stats").hidden = !skillTabs.includes(tab);
  $("#createButton").hidden = !skillTabs.includes(tab);
  $(".shell").classList.toggle("focus-mode", !skillTabs.includes(tab));
  const pages = {
    chat:["Hermetrix Engine / Agent runtime", "Agent Workspace"], library:["Hermetrix Engine / Skills", "Skill Control Center"],
    proposals:["Hermetrix Engine / Review gate", "Skill Proposals"], learning:["Hermetrix Engine / Background review", "Learning Queue"],
    insights:["Hermetrix Engine / Curator", "Skill Insights"], archive:["Hermetrix Engine / Reversible state", "Skill Archive"],
    context:["Hermetrix Engine / Context laboratory", "Context Diagnostics"], providers:["Hermetrix Engine / Model adapters", "Provider Registry"],
    mcp:["Hermetrix Engine / Capability graph", "MCP Control Center"], projects:["Hermetrix Engine / Bounded workspaces", "Project Workbench"],
    office:["Hermetrix Engine / Background execution", "Agent Office"], artifacts:["Hermetrix Engine / Content-addressed outputs", "Artifact Registry"],
    fidelity:["Hermetrix Engine / Context evidence", "Fidelity Laboratory"], maintenance:["Hermetrix Engine / Recoverable operations", "Maintenance & Settings"]
  };
  const page = pages[tab] || pages.library;
  $("#pageEyebrow").textContent = page[0];
  $("#pageTitle").textContent = page[1];
}

function openCandidateDialog() { $("#candidateDialog").showModal(); }

async function submitCandidate(event) {
  event.preventDefault();
  const formElement = event.currentTarget;
  const form = new FormData(formElement);
  const name = form.get("name").trim();
  const description = form.get("description").trim();
  const markdown = `---\nname: ${name}\ndescription: "${description.replaceAll('"', "'")}"\ntags: []\ntools: []\n---\n\n${form.get("body").trim()}\n`;
  try {
    await api("/api/skills/custom", { method:"POST", body:JSON.stringify({ canonical_name:name, scope_kind:form.get("scope"), reason:form.get("reason"), evidence_refs:["manual:user"], markdown }) });
    $("#candidateDialog").close();
    formElement.reset();
    toast("Proposal created — active skills unchanged");
    await load();
    switchTab("proposals");
  } catch (error) { toast(error.message, true); }
}

document.addEventListener("DOMContentLoaded", () => {
  $$(".tab, .nav-item[data-tab]").forEach(node => node.addEventListener("click", () => switchTab(node.dataset.tab)));
  $("#refreshButton").addEventListener("click", load);
  $("#createButton").addEventListener("click", openCandidateDialog);
  $("#candidateForm").addEventListener("submit", submitCandidate);
  $("#searchInput").addEventListener("input", renderLibrary);
  $("#stateFilter").addEventListener("change", renderLibrary);
  load();
});
