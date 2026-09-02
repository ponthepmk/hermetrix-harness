const state = { skills: [], candidates: [], archives: [], relations: [], reviews: [], curator_runs: [], profiles: [], providers: [], mcp_servers: [], capability_summary: { total:0, by_source:{}, by_readiness:{} }, capabilityResults: [], capabilityPickerResults: [], capabilityPickerFilter:"all", selectedCapability: null, sessions: [], projects: [], projectFiles: [], jobs: [], artifacts: [], terminals: [], browserTabs: [], teams: [], teamRuns: [], settings: [], memories: [], backups: [], usage: {}, fidelityCases: [], fidelityRuns: [], qualifications: [], curatorFindings: [], schedules: [], gcRuns: [], skillAuthority:null, authorityActions:[], activeTab: "chat", view: "chat", workbenchTab:"review", selectedSkill: null, selectedSkillDetail:null, selectedSession: null, selectedProject: null, currentProject: null, selectedTerminal:null, selectedBrowserTab:null, selectedTeam:null, teamDraft:null, projectFile:null, projectFileDiff:"", sessionDetail: null, contextResult: null, modelProbe: null, sending: false, draftQualificationReason:"", sessionError:"", commandItems: [], commandMatches: [], commandIndex: 0, capabilityPickerSearching: false, density: "comfortable", sessionOptionsOpen: false, sessionReady: false, elicitations: [], folderListing: null, draftMessage: "", composerFocused: false, composerCaret: 0, zoneWidths: {}, panes: [], maximisedPane: null };
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
let capabilitySearchTimer;
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

function activateWorkbenchChrome(name) {
  state.workbenchTab = name;
  $$(".workbench-tab").forEach(node => node.classList.toggle("active", node.dataset.workbench === name));
  // Opening a room from elsewhere (a skill row, a candidate) has to reveal the
  // side zone even if the user had collapsed it earlier.
  collapseZone("side", false);
}

// The chip names whichever project the picker opened -- currentProject, not
// selectedProject, because selectedProject also drifts to whatever the
// workbench is browsing (a different project's files can be opened from the
// Projects room). The chip's click handler, wired in DOMContentLoaded, is what
// turns it into a switcher back to the picker.
function renderProjectChip() {
  const project = state.currentProject;
  $("#projectName").textContent = project ? project.name : "No project";
  $("#projectChip").title = project ? (project.root_path || "No code folder") : "No project registered yet";
}

// The picker is the first screen because a project is the root of everything:
// work, notes, chat and code all hang off one. It draws only the groups that
// have something in them -- an empty heading reads as a place you can go.
function renderPicker() {
  const query = ($("#pickerSearch")?.value || "").trim().toLowerCase();
  const matches = state.projects.filter(item =>
    !query || `${item.name} ${item.root_path}`.toLowerCase().includes(query));
  const card = item => {
    const where = item.root_path
      ? `<div class="picker-path">${escapeHTML(item.root_path)}</div>`
      : `<div class="picker-path none">ไม่มีโฟลเดอร์โค้ด</div>`;
    // Only the count that has a system behind it. Tasks and notes arrive with
    // their own specs; a zero here would be a claim, not a fact.
    return `<button class="picker-card" data-open-project="${escapeHTML(item.id)}">
      <strong>${escapeHTML(item.name)}</strong>${where}
      <span class="picker-stat">${Number(item.session_count || 0).toLocaleString()} แชท</span></button>`;
  };
  const group = (title, items) => items.length
    ? `<p class="picker-group">${title}</p><div class="picker-grid">${items.map(card).join("")}</div>` : "";
  $("#pickerPinned").innerHTML = group("ปักหมุด", matches.filter(item => item.pinned));
  $("#pickerRecent").innerHTML = group("ล่าสุด", matches.filter(item => !item.pinned));
  const exact = state.projects.some(item => item.name.toLowerCase() === query);
  const create = $("#pickerCreate");
  create.hidden = !query || exact;
  create.textContent = `สร้างโปรเจค “${query}”`;
  $$("[data-open-project]").forEach(button =>
    button.addEventListener("click", () => openProject(button.dataset.openProject)));
}

// Opening records that someone worked here, which is what "recent" is ordered
// by, and then hands the screen to the shell.
async function openProject(id) {
  try {
    state.currentProject = await api(`/api/projects/${encodeURIComponent(id)}/open`, { method:"POST", body:"{}" });
    state.projects = await api("/api/projects");
    state.selectedProject = id;
    showShell();
    await load();
  } catch (error) { toast(error.message, true); }
}

async function createProjectFromPicker() {
  const name = ($("#pickerSearch")?.value || "").trim();
  if (!name) return;
  await runAction($("#pickerCreate"), {
    working: `กำลังสร้าง “${name}”`,
    done: "สร้างแล้ว",
    run: async () => {
      // A project created from the picker has no code folder yet. Adding one is
      // a separate, deliberate step: guessing a path here would bind a scope to
      // a directory the user never named.
      const created = await api("/api/projects", { method:"POST", body:JSON.stringify({ name, root_path:"" }) });
      state.projects = await api("/api/projects");
      $("#pickerSearch").value = "";
      renderPicker();
      await openProject(created.id);
    }
  });
}

// showShell and showPicker are the only two things that decide which of the two
// top-level screens is on.
function showShell() {
  $("#projectPicker").hidden = true;
  $("#appShell").hidden = false;
  $("#projectName").textContent = state.currentProject?.name || "—";
}

function showPicker() {
  $("#appShell").hidden = true;
  $("#projectPicker").hidden = false;
  renderPicker();
  $("#pickerSearch")?.focus();
}

// The app opens on the picker rather than the workspace bootstrap: a project
// is the root of everything, so nothing else is worth fetching until one is
// chosen. Opening or creating a project (see openProject) is what triggers the
// full load().
async function initPicker() {
  try {
    state.projects = await api("/api/projects");
  } catch (error) { toast(error.message, true); }
  showPicker();
}

function askAction({ title, message, confirmLabel = "Confirm", reasonLabel = "", danger = false, eyebrow = "Review decision" }) {
  const dialog = $("#actionDialog");
  const form = $("#actionForm");
  const input = $("#actionInput");
  $("#actionEyebrow").textContent = eyebrow;
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
    // The reason field is a textarea, so a bare Enter has to stay a newline.
    // Without this the only way to confirm was to reach for the mouse.
    input.onkeydown = event => {
      if ((event.metaKey || event.ctrlKey) && event.key === "Enter") { event.preventDefault(); form.requestSubmit(); }
    };
    $("#actionCancel").onclick = () => finish(null);
    $("#actionClose").onclick = () => finish(null);
    dialog.oncancel = event => { event.preventDefault(); finish(null); };
    dialog.showModal();
    if (reasonLabel) input.focus(); else $("#actionConfirm").focus();
  });
}

async function load() {
  try {
    const [data, projects, jobs, artifacts, terminals, browserTabs, teams, teamRuns, settings, memories, backups, usage, fidelityCases, fidelityRuns, qualifications, curatorFindings, schedules, gcRuns, skillAuthority, authorityActions] = await Promise.all([
      api("/api/bootstrap"), api("/api/projects"), api("/api/jobs"), api("/api/artifacts"),
      api("/api/terminals"), api("/api/browser/tabs"), api("/api/teams"), api("/api/team-runs"), api("/api/settings"),
      api("/api/memories"), api("/api/backups"), api("/api/usage"), api("/api/fidelity/cases"), api("/api/fidelity/runs"),
      api("/api/qualifications"), api("/api/curator/findings"), api("/api/maintenance/schedules"), api("/api/maintenance/gc"),
      api("/api/skill-authority"), api("/api/skill-authority/actions")
    ]);
    Object.assign(state, data);
    // Belt to the server's braces: one endpoint answering null instead of []
    // used to throw here and leave every panel in the cockpit unrendered.
    const list = value => (Array.isArray(value) ? value : []);
    Object.assign(state, { projects: list(projects), jobs: list(jobs), artifacts: list(artifacts),
      terminals: list(terminals), browserTabs: list(browserTabs), teams: list(teams), teamRuns: list(teamRuns),
      settings: list(settings), memories: list(memories), backups: list(backups), usage: usage || {},
      fidelityCases: list(fidelityCases), fidelityRuns: list(fidelityRuns), qualifications: list(qualifications),
      curatorFindings: list(curatorFindings), schedules: list(schedules), gcRuns: list(gcRuns),
      skillAuthority, authorityActions: list(authorityActions) });
    if (!state.selectedProject && state.projects.length) state.selectedProject = state.projects[0].id;
    if (!state.selectedTerminal && state.terminals.length) state.selectedTerminal = state.terminals.find(item => item.state === "running")?.id || state.terminals[0].id;
    if (!state.selectedBrowserTab && state.browserTabs.length) state.selectedBrowserTab = state.browserTabs.find(item => item.state === "ready")?.id || state.browserTabs[0].id;
    if (!state.selectedTeam && state.teams.length) state.selectedTeam = state.teams[0].id;
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
  state.pendingProposals = state.candidates.filter(item => ["needs_review", "quarantined"].includes(item.state)).length;
  state.pendingReviews = state.reviews.filter(item => item.state === "queued" || item.state === "running").length;
  const waiting = state.pendingProposals + state.pendingReviews;
  $("#proposalBadge").hidden = waiting === 0;
  $("#proposalBadge").textContent = waiting;
  renderProjectChip();
  switchTab(state.activeTab);
  if (!$("#zones").classList.contains("side-hidden")) renderCurrentWorkbench();
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
  const policyPanel = policy ? `<details class="panel authority-panel"><summary><div><p class="eyebrow">Skill authority</p><h3>${policy.mode === "manual" ? "Manual review" : "Gated automation"}</h3><p>Automation can promote only trusted agent/reviewer candidates that pass checks, replay, scope and token gates. Capability widening always remains manual.</p></div>${pill(`policy r${policy.revision}`, policy.mode === "manual" ? "blue" : "amber")}</summary><div class="authority-panel-body"><form id="authorityForm"><div class="form-grid"><label>Mode<select name="mode"><option value="manual" ${policy.mode === "manual" ? "selected" : ""}>Manual · safest default</option><option value="gated_automation" ${policy.mode === "gated_automation" ? "selected" : ""}>Gated automation</option></select></label><label>Candidate token ceiling<input name="max_candidate_tokens" type="number" min="256" max="16384" value="${policy.max_candidate_tokens}"></label></div><div class="authority-checks"><label class="check-label"><input name="auto_create" type="checkbox" ${policy.auto_promote_agent_create ? "checked" : ""}> Auto-promote trusted agent-created Skills</label><label class="check-label"><input name="auto_improve" type="checkbox" ${policy.auto_promote_agent_improve ? "checked" : ""}> Auto-promote no-regression improvements</label><label class="check-label"><input name="auto_archive" type="checkbox" ${policy.auto_archive_agent_skills ? "checked" : ""}> Let curator archive stale agent Skills with undo</label></div><fieldset><legend>Allowed scopes</legend>${["user","workspace","agent"].map(scope => `<label class="check-label"><input name="scope" value="${scope}" type="checkbox" ${(policy.allowed_scopes || []).includes(scope) ? "checked" : ""}> ${scope}</label>`).join("")}</fieldset><label>Change reason<input name="reason" required maxlength="1000" placeholder="Why this authority policy is appropriate"></label><div class="action-row"><button class="primary">Save authority policy</button><button class="ghost" type="button" id="runAuthorityButton">Evaluate pending candidates</button></div></form>${state.authorityActions.length ? `<div class="authority-actions"><h4>Recent automated decisions</h4>${state.authorityActions.slice(0,5).map(action => `<article><div>${pill(action.state,action.state === "completed" ? "green" : action.state === "failed" ? "red" : "amber")}<strong>${escapeHTML(action.action_kind)}</strong><small>policy r${action.policy_revision} · ${formatDate(action.created_at)}</small></div>${action.error ? `<p>${escapeHTML(action.error)}</p>` : ""}${action.state === "completed" && !action.rollback_candidate_id ? `<button class="ghost" data-authority-rollback="${escapeHTML(action.id)}">Create rollback</button>` : ""}</article>`).join("")}</div>` : ""}</div></details>` : "";
  // A Skill the agent promoted on its own has to be findable and undoable, or
  // "you can review it afterwards" is not a real offer. The action that
  // promoted it is the thing that can be rolled back, so the row carries it.
  const promotionBySkill = new Map();
  for (const action of state.authorityActions) {
    if (action.action_kind === "auto_promote" && action.state === "completed" && action.skill_id
        && !action.rollback_candidate_id && !promotionBySkill.has(action.skill_id)) {
      promotionBySkill.set(action.skill_id, action);
    }
  }
  const list = items.length ? `<div class="skill-list">${items.map(item => {
    const promotion = promotionBySkill.get(item.id);
    return `<article class="skill-row ${state.selectedSkill === item.id ? "selected" : ""}" data-skill-id="${escapeHTML(item.id)}" tabindex="0">
      <div><div class="row-title"><h3>${escapeHTML(item.canonical_name)}</h3>${pill(item.state, item.state === "active" ? "green" : "amber")}${item.pinned ? pill("pinned", "blue") : ""}${promotion ? pill("promoted by agent", "amber") : ""}</div>
      <p>${escapeHTML(item.summary || "No summary in the active manifest")}</p>
      <div class="meta">${pill(item.scope_kind)}${pill(item.origin)}${pill(item.owner)}</div>
      ${promotion ? `<p class="skill-promotion">Hermetrix promoted this on ${escapeHTML(formatDate(promotion.completed_at || promotion.created_at))} under policy r${promotion.policy_revision}. Open it to edit, or undo it here.</p><div class="action-row"><button class="ghost" data-revert-promotion="${escapeHTML(promotion.id)}">Undo this promotion</button></div>` : ""}</div>
      <div class="skill-row-actions"><div class="metric"><strong>${item.injected_count}</strong>injected · ${item.success_count} success</div><button class="ghost" type="button" data-mention-skill="${escapeHTML(item.id)}">Use in chat</button></div>
    </article>`;
  }).join("")}</div>` : `<div class="empty"><h3>${state.skills.length ? "No matching skills" : "No active skills yet"}</h3><p>Use + New Skill to write one yourself. Hermetrix also writes Skills as it works; those appear here marked as promoted by agent.</p></div>`;
  // Skill Studio opens on what a person came to do -- read the library, add a
  // Skill -- with the authority policy folded away behind its own summary
  // rather than sitting above the list it governs.
  const intro = `<section class="skill-studio-intro"><div><p class="eyebrow">Skill Studio</p><h3>${state.skills.filter(item => item.state === "active").length} active Skills, ${state.candidates.filter(item => ["needs_review","quarantined"].includes(item.state)).length} waiting on you</h3><p>Agent and curator changes start as candidates. Manual mode requires your promotion; gated automation may promote only inside the policy below, records provenance, and always leaves a rollback path. Capability widening remains manual.</p></div><div class="action-row"><button class="ghost" type="button" id="studioReviewButton">Review proposals</button><button class="primary" type="button" id="studioCreateButton">+ New proposal</button></div></section>`;
  root.innerHTML = `${intro}${policyPanel}${list}`;
  $("#studioReviewButton")?.addEventListener("click", () => switchTab("proposals"));
  $("#studioCreateButton")?.addEventListener("click", openCandidateDialog);
  $("#authorityForm")?.addEventListener("submit", saveAuthorityPolicy);
  $("#runAuthorityButton")?.addEventListener("click", runAuthorityPolicy);
  $$('[data-authority-rollback]', root).forEach(button => button.addEventListener("click", () => rollbackAuthorityAction(button.dataset.authorityRollback)));
  $$('[data-mention-skill]', root).forEach(button => button.addEventListener("click", event => {
    event.stopPropagation();
    const skill = state.skills.find(item => item.id === button.dataset.mentionSkill);
    if (skill) mentionSkill(skill);
  }));
  $$("[data-revert-promotion]", root).forEach(button => button.addEventListener("click", event => {
    // The row itself opens the Skill, so an action inside it must not also.
    event.stopPropagation();
    rollbackAuthorityAction(button.dataset.revertPromotion);
  }));
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
    state.selectedSkillDetail = data;
    activateWorkbenchChrome("review");
    const attempts = skill.success_count + skill.failure_count;
    const observed = attempts ? `${skill.success_count}/${attempts} observed success` : "No explicit outcomes yet";
    $("#workbenchContent").innerHTML = `
      <div class="inspect-head"><div class="provider-head"><div><p class="eyebrow">Active capability</p><h2>${escapeHTML(skill.canonical_name)}</h2></div><button class="ghost" id="closeSkillInspect">Session review</button></div><div class="meta">${pill(skill.state,"green")}${pill(skill.scope_kind)}${pill(skill.origin)}</div></div>
      <section class="inspect-section"><h3>Provenance</h3><div class="kv"><span>Owner</span><strong>${escapeHTML(skill.owner)}</strong><span>Version</span><span class="hash">${escapeHTML(version.id)}</span><span>Content</span><span class="hash">${escapeHTML(version.content_hash)}</span><span>Author</span><span>${escapeHTML(version.author_actor)}</span><span>Changed</span><span>${formatDate(version.created_at)}</span></div></section>
      <section class="inspect-section"><h3>Usage evidence</h3><div class="kv"><span>Selected</span><strong>${skill.selected_count}</strong><span>Injected</span><strong>${skill.injected_count}</strong><span>Outcome</span><span>${observed}</span><span>Last used</span><span>${formatDate(skill.last_used_at)}</span></div></section>
      <section class="inspect-section"><h3>Current SKILL.md</h3><pre>${escapeHTML(version.markdown)}</pre></section>
      <section class="inspect-section"><h3>Selection controls</h3><p>Changes affect new sessions only. Existing Session Contracts keep their exact Skill version and cache prefix.</p><div class="action-row"><button class="primary" id="mentionSelectedSkill">Use in chat</button><button class="ghost" id="toggleEnabled">${skill.enabled ? "Disable for new sessions" : "Enable for new sessions"}</button><button class="ghost" id="togglePinned">${skill.pinned ? "Unpin" : "Pin"}</button></div></section>
      <section class="inspect-section"><h3>Reversible actions</h3><p>Editing starts from this exact version as a proposal. Forking creates a user-owned custom Skill. Archiving preserves the snapshot and history.</p><div class="action-row"><button class="primary" id="improveSelected">Propose improvement</button><button class="ghost" id="forkSelected">Fork as custom</button><button class="danger" id="archiveSelected">Archive skill</button></div></section>`;
    $("#improveSelected").addEventListener("click", () => proposeImprovement(skill));
    $("#forkSelected").addEventListener("click", () => forkSkill(skill));
    $("#archiveSelected").addEventListener("click", () => archiveSkill(skill));
    $("#toggleEnabled").addEventListener("click", () => updateSkillControl(skill, "enabled", !skill.enabled));
    $("#togglePinned").addEventListener("click", () => updateSkillControl(skill, "pinned", !skill.pinned));
    $("#mentionSelectedSkill").addEventListener("click", () => mentionSkill(skill));
    $("#closeSkillInspect").addEventListener("click", () => {
      state.selectedSkillDetail = null;
      state.selectedSkill = null;
      renderLibrary();
      renderWorkbenchReview();
    });
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
    $("#workbenchContent").innerHTML = `<div class="empty-inspector"><span class="orb">✓</span><h2>Archived safely</h2><p>The exact version remains available in Archive and restore creates a new proposal.</p></div>`;
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
    activateWorkbenchChrome("review");
    $("#workbenchContent").innerHTML = `<div class="inspect-head"><p class="eyebrow">Untrusted candidate</p><h2>${escapeHTML(item.canonical_name)}</h2><div class="meta">${pill(item.state,item.checks.passed ? "green" : "red")}${pill(`revision ${item.revision}`)}</div></div>
      <section class="inspect-section"><h3>Evidence</h3><p>${escapeHTML(item.reason)}</p><div class="meta">${(item.evidence_refs || []).map(ref => pill(ref)).join("") || pill("manual")}</div></section>
      <section class="inspect-section"><h3>Candidate SKILL.md</h3><textarea id="candidateEditor" rows="18">${escapeHTML(item.markdown)}</textarea><div class="action-row"><button class="primary" id="saveCandidateEdit">Save & re-run checks</button></div></section>
      <section class="inspect-section"><h3>Checks</h3><div class="kv"><span>Lint</span><strong>${item.checks.lint_passed ? "pass" : "fail"}</strong><span>Security</span><strong>${item.checks.security_passed ? "pass" : "fail"}</strong><span>Replay</span><strong>${item.checks.replay_required ? (item.checks.replay_passed ? "pass" : "required") : "not required"}</strong><span>Footprint</span><span>${item.checks.token_estimate} tokens</span></div></section>
      <section class="inspect-section"><h3>Deterministic replay & bounded diff</h3>${replay ? `<div class="kv"><span>Runner</span><strong>${escapeHTML(replay.runner_revision)}</strong><span>Binding</span><span>r${replay.candidate_revision} · ${shortHash(replay.candidate_hash)}</span><span>Fixtures</span><strong>${replay.candidate_passed}/${replay.fixtures_total}</strong><span>Regressions</span><strong>${replay.regressions}</strong></div>${addedTools.length ? `<p class="form-note">Capability widening: ${addedTools.map(escapeHTML).join(", ")}</p>` : ""}<ul class="findings">${(replay.cases || []).map(test => `<li class="${test.candidate_passed ? "" : "error"}"><strong>${escapeHTML(test.id)}</strong> — baseline ${test.baseline_passed ? "pass" : "fail"}, candidate ${test.candidate_passed ? "pass" : "fail"}</li>`).join("")}</ul><pre>${escapeHTML(replay.diff || "No line changes")}</pre>` : `<p>No replay has been recorded for this revision.</p>`}<div class="action-row"><button class="ghost" id="runCandidateReplay">Run exact replay</button>${addedTools.length ? `<button class="primary" id="approveCandidateTools">Approve widened tools</button>` : ""}</div></section>`;
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

// A tool call and its receipt are one action, and reading them meant expanding
// two separate rows that named the same tool. toolArgumentsPreview,
// toolReceiptOf and toolOutputPreview are shared by the paired card below and
// by the unpaired fallbacks above, which are what a call still in flight or a
// receipt whose call has been compacted away renders as.
function toolArgumentsPreview(event) {
  let preview = String(event.content || "");
  if (event.metadata?.tool_name === "workspace.write_file") {
    // A whole file body in the transcript buries the path and the expected
    // hash, which are the two things worth reading before an approval.
    try {
      const args = JSON.parse(preview);
      preview = JSON.stringify({ path:args.path, content:`[${String(args.content || "").length} characters; inspect approval preview]`, expected_sha256:args.expected_sha256 });
    } catch {}
  } else if (preview.length > 900) {
    preview = `${preview.slice(0,900)}…`;
  }
  return preview;
}

function toolReceiptOf(event) {
  try { return JSON.parse(event.content); } catch { return {}; }
}

function toolOutputPreview(receipt) {
  const preview = String(receipt.output || receipt.error || "No inline output");
  return preview.length > 900 ? `${preview.slice(0,900)}…` : preview;
}

// groupTimeline pairs each tool_call with the tool_result carrying the same
// tool_call_id. Events keep their original order, so an unpaired call still
// appears exactly where it happened.
function groupTimeline(events) {
  const resultByCall = new Map();
  for (const event of events) {
    const callID = event.event_kind === "tool_result" ? event.metadata?.tool_call_id : "";
    if (callID) resultByCall.set(callID, event);
  }
  const paired = new Set();
  const items = [];
  for (const event of events) {
    if (event.event_kind === "tool_call") {
      const callID = event.metadata?.tool_call_id;
      const result = callID ? resultByCall.get(callID) : null;
      if (result) {
        paired.add(callID);
        items.push({ kind:"tool_step", call:event, result });
        continue;
      }
    } else if (event.event_kind === "tool_result" && paired.has(event.metadata?.tool_call_id)) {
      continue;
    }
    items.push({ kind:"event", event });
  }
  return items;
}

function renderTimelineItem(item) {
  if (item.kind !== "tool_step") return renderTimelineEvent(item.event);
  const receipt = toolReceiptOf(item.result);
  const status = receipt.status || item.result.metadata?.tool_status || "receipt";
  const succeeded = status === "succeeded";
  const name = receipt.name || item.call.metadata?.tool_name || "tool";
  return `<details class="tool-receipt step"><summary>${pill(status, succeeded ? "green" : "red")}<strong>${escapeHTML(name)}</strong><span>${Number(receipt.duration_ms || 0)}ms</span></summary><div class="tool-detail"><p class="tool-step-label">Arguments</p><code>${escapeHTML(toolArgumentsPreview(item.call))}</code><p class="tool-step-label">Result</p><pre>${escapeHTML(toolOutputPreview(receipt))}</pre><small>call ${escapeHTML(shortHash(item.result.metadata?.tool_call_id))} · bound ${escapeHTML(shortHash(item.call.metadata?.step_binding_id))}</small></div></details>`;
}

function renderTimelineEvent(event) {
  if (event.event_kind === "message" && ["user", "assistant"].includes(event.role)) {
    return `<article class="chat-message ${event.role}"><div class="message-role">${event.role === "user" ? "You" : "Hermetrix"}</div><div class="message-body">${escapeHTML(event.content)}</div>${event.role === "assistant" && event.metadata?.step_binding_id ? `<div class="message-proof">bound ${escapeHTML(shortHash(event.metadata.step_binding_id))} · ${event.metadata.usage?.total_tokens || 0} tokens</div>` : ""}</article>`;
  }
  if (event.event_kind === "tool_call") {
    return `<details class="tool-receipt request"><summary>${pill("tool request","blue")}<strong>${escapeHTML(event.metadata?.tool_name || "unknown tool")}</strong><span>running…</span></summary><div class="tool-detail"><p class="tool-step-label">Arguments</p><code>${escapeHTML(toolArgumentsPreview(event))}</code><small>bound ${escapeHTML(shortHash(event.metadata?.step_binding_id))}</small></div></details>`;
  }
  if (event.event_kind === "tool_result") {
    const receipt = toolReceiptOf(event);
    return `<details class="tool-receipt result"><summary>${pill(receipt.status || event.metadata?.tool_status || "receipt", receipt.status === "succeeded" ? "green" : "red")}<strong>${escapeHTML(receipt.name || event.metadata?.tool_name || "tool")}</strong><span>${Number(receipt.duration_ms || 0)}ms</span></summary><div class="tool-detail"><p class="tool-step-label">Result</p><pre>${escapeHTML(toolOutputPreview(receipt))}</pre><small>call ${escapeHTML(shortHash(event.metadata?.tool_call_id))}</small></div></details>`;
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

// An elicitation is a remote server asking the person at the keyboard a
// question, in the server's own words. Those words are untrusted content, so
// the card says which server is speaking and never renders them as if they came
// from Hermetrix. The schema the server asked for drives the fields; with no
// schema it is one free-text answer.
function elicitationCardHTML(item) {
  let properties = {};
  let required = [];
  try {
    const schema = item.schema ? JSON.parse(item.schema) : null;
    if (schema && schema.type === "object" && schema.properties) {
      properties = schema.properties;
      required = Array.isArray(schema.required) ? schema.required : [];
    }
  } catch {}
  const names = Object.keys(properties).slice(0, 12);
  const fields = names.length
    ? names.map(name => {
        const field = properties[name] || {};
        const label = escapeHTML(field.title || name);
        const help = field.description ? `<small>${escapeHTML(field.description)}</small>` : "";
        const need = required.includes(name) ? "required" : "";
        if (Array.isArray(field.enum) && field.enum.length) {
          return `<label>${label}${help}<select name="${escapeHTML(name)}" ${need}>${field.enum.slice(0, 40).map(value => `<option value="${escapeHTML(String(value))}">${escapeHTML(String(value))}</option>`).join("")}</select></label>`;
        }
        if (field.type === "boolean") {
          return `<label class="check-label"><input type="checkbox" name="${escapeHTML(name)}"> ${label}</label>${help}`;
        }
        const kind = field.type === "number" || field.type === "integer" ? "number" : "text";
        return `<label>${label}${help}<input type="${kind}" name="${escapeHTML(name)}" ${need}></label>`;
      }).join("")
    : `<label>Your answer<input type="text" name="answer" required></label>`;
  return `<article class="elicitation-card">
    <div class="elicitation-head">${pill("question from a tool server", "amber")}<strong>${escapeHTML(item.server_name)}</strong></div>
    <p class="elicitation-message">${escapeHTML(item.message)}</p>
    <form data-elicit-accept="${escapeHTML(item.id)}">${fields}
      <div class="action-row"><button class="ghost" type="button" data-elicit-decline="${escapeHTML(item.id)}">Decline</button><button class="primary" type="submit">Send answer</button></div>
    </form>
    <small>Waiting since ${escapeHTML(formatDate(item.asked_at))}. Unanswered questions are cancelled automatically, and the server is told so.</small>
  </article>`;
}

async function answerElicitation(event) {
  event.preventDefault();
  const form = event.currentTarget;
  const content = {};
  for (const [key, value] of new FormData(form).entries()) content[key] = value;
  for (const box of $$('input[type="checkbox"]', form)) content[box.name] = box.checked;
  await sendElicitationAnswer(form.dataset.elicitAccept, { accept: true, content });
}

async function declineElicitation(id) { await sendElicitationAnswer(id, { accept: false }); }

async function sendElicitationAnswer(id, body) {
  try {
    await api(`/api/elicitations/${encodeURIComponent(id)}/answer`, { method:"POST", body:JSON.stringify(body) });
    state.elicitations = state.elicitations.filter(item => item.id !== id);
    toast(body.accept ? "Answer sent to the tool server" : "Declined; the server was told");
    renderChat();
  } catch (error) { toast(error.message, true); }
}

// pollElicitations runs only while a turn is in flight, because that is the
// only time a server can be waiting on one.
async function pollElicitations() {
  if (!state.sending || !state.selectedSession) return;
  try {
    const items = await api(`/api/elicitations?session_id=${encodeURIComponent(state.selectedSession)}`);
    const changed = JSON.stringify(items) !== JSON.stringify(state.elicitations);
    state.elicitations = Array.isArray(items) ? items : [];
    if (changed) renderChat();
  } catch {}
  if (state.sending) setTimeout(pollElicitations, 1200);
}

function renderChat() {
  const root = $("#view-chat");
  if (!root) return;
  // Streaming re-renders this whole view on every delta. Take the composer's
  // draft, caret and focus before the markup is replaced so they can be put
  // back afterwards; without this, typing while a turn streams loses a
  // character every time a token arrives.
  captureComposer();
  const enabledProviders = state.providers.filter(provider => provider.enabled);
  if (!state.draftProviderID || !enabledProviders.some(provider => provider.id === state.draftProviderID)) {
    state.draftProviderID = enabledProviders[0]?.id || null;
  }
  const draftProvider = enabledProviders.find(provider => provider.id === state.draftProviderID);
  const compatibleProfiles = availableProfiles(draftProvider);
  if (!compatibleProfiles.some(profile => profile.name === state.draftProfileName)) {
    state.draftProfileName = bestProfileFor(draftProvider, compatibleProfiles)?.name || "";
  }
  // Bind to the open project by default, not merely the first one in the
  // list: the picker is what put you here, and a session started from this
  // rail belongs where you actually are.
  if (state.draftProjectID === undefined) state.draftProjectID = state.currentProject?.id || state.projects[0]?.id || "";
  const draftProfile = compatibleProfiles.find(profile => profile.name === state.draftProfileName);
  const admission = profileAdmission(draftProvider, draftProfile);
  const needsOverride = admission.mode === "override_required";
  const overrideReason = state.draftQualificationReason.trim() || suggestedOverrideReason(draftProvider, draftProfile);
  const canStart = Boolean(draftProvider && draftProfile && draftProvider.credential_ready &&
    (admission.admitted || (needsOverride && overrideReason.length >= 8)));
  const selectedID = state.sessionDetail?.session?.id || state.selectedSession;
  const timeline = (state.sessionDetail?.events || []).filter(event => ["message", "tool_call", "tool_result", "approval_required", "approval_decision"].includes(event.event_kind));
  // An MCP server can stop mid tool call to ask a question. It is waiting on
  // the answer right now, so it is rendered after the transcript rather than
  // inside it: it is not history yet.
  const questions = state.elicitations.filter(item => item.session_id === selectedID);
  const session = state.sessionDetail?.session;
  const contract = session?.contract || {};
  const skillCatalog = contract.skill_catalog || [];
  const selectedSkills = contract.selected_skills || [];
  const directTools = contract.tool_bindings || [];
  const readyMCPTools = Number(state.capability_summary?.by_readiness?.ready || 0);
  const projectName = state.projects.find(item => item.id === session?.project_id)?.name || "No project";
  // Starting a session is one button. Provider, project and context are a
  // remembered default shown as a single line, and the three selects that used
  // to greet every new session live behind Options. Choosing a model is
  // configuration, not something to redo before each conversation.
  state.sessionReady = canStart;
  const draftProjectName = state.projects.find(item => item.id === state.draftProjectID)?.name || "No project";
  // Options stays shut unless the user opened it. It used to spring open
  // whenever an override was needed, which is most remote endpoints, and the
  // three selects plus a full explanation then filled the rail from the brand
  // to the settings row. The summary line carries the fact; the explanation
  // lives behind Options where someone can go and read it.
  const optionsOpen = state.sessionOptionsOpen;
  const dock = $("#sessionDock");
  dock.innerHTML = `<div class="session-create">${enabledProviders.length ? `
        <p class="session-summary" title="Change these under Options">${escapeHTML(draftProvider ? `${draftProvider.name} · ${draftProvider.model}` : "No model")}<span>${escapeHTML(draftProfile ? profileLabel(draftProfile) : "no envelope")} · ${escapeHTML(draftProjectName)}</span></p>
        ${draftProvider && !draftProvider.credential_ready ? `<p class="session-error" role="alert">${escapeHTML(draftProvider.name)} has no API key. Open Models and paste one — it takes effect immediately.</p>` : ""}
        ${draftProfile && !admission.admitted && !needsOverride ? `<p class="session-error" role="alert">Only ${admission.budget.toLocaleString()} answer tokens left. Choose a larger envelope under Options.</p>` : ""}
        ${needsOverride ? `<p class="session-note">Opens under a reviewed 24-hour override. The reason is recorded with the session and editable under Options.</p>` : ""}
        ${state.sessionError ? `<p class="session-error" role="alert">${escapeHTML(state.sessionError)}</p>` : ""}
        <details class="session-options" id="sessionOptions" ${optionsOpen ? "open" : ""}><summary>Options</summary><div class="session-options-body">
          <label>Model<select id="chatProviderSelect">${enabledProviders.map(provider => `<option value="${escapeHTML(provider.id)}" ${provider.id === state.draftProviderID ? "selected" : ""}>${escapeHTML(provider.name)} · ${escapeHTML(provider.model)}</option>`).join("")}</select></label>
          <!-- No "no project" option: a project is the root of everything now, so
               every session belongs to one. This picks which project among the
               known ones, never none. -->
          <label>Project<select id="chatProjectSelect">${state.projects.map(project => `<option value="${escapeHTML(project.id)}" ${project.id === state.draftProjectID ? "selected" : ""}>${escapeHTML(project.name)}</option>`).join("")}</select></label>
          <label>Context<select id="chatProfileSelect" ${compatibleProfiles.length ? "" : "disabled"}>${compatibleProfiles.map(profile => {
            const status = profileAdmission(draftProvider, profile);
            const note = status.blocking ? "too small for this model"
              : status.mode === "qualified" ? "qualified"
              : status.mode === "compatibility" ? "ready"
              : "one-click override";
            return `<option value="${profile.name}" ${profile.name === state.draftProfileName ? "selected" : ""} ${status.blocking ? "disabled" : ""}>${profileLabel(profile)} · ${status.budget.toLocaleString()} answer tokens · ${note}</option>`;
          }).join("")}</select></label>
          ${draftProfile && admission.admitted ? `<p class="readiness-line">${admission.mode === "qualified" ? `Bound to qualification ${escapeHTML(shortHash(admission.qualification.id))}.` : `${admission.budget.toLocaleString()} tokens for the answer.`}</p>` : ""}
          ${needsOverride ? `<div class="session-readiness review"><p class="readiness-line">No local qualification can exist for a remote endpoint, so this envelope opens under a reviewed 24-hour override.</p><textarea id="chatQualificationReason" rows="3" minlength="8">${escapeHTML(overrideReason)}</textarea></div>` : ""}
        </div></details>` : `
        <div class="session-needs-model"><p>No model connected yet. Connect one and every session picks it up — no environment variable, no restart.</p><button class="primary" id="openProvidersFromDock">Connect a model</button></div>`}
      </div><div class="session-list">${state.sessions.length ? state.sessions.map((item, index) => {
        // The model and envelope repeat down the whole list when every session
        // uses the same ones, which is the common case. Drawing them once per
        // run of identical sessions halves the height of the list and loses
        // nothing: a row with no meta line has the same meta as the row above.
        const meta = `${item.model} · ${item.context_profile}`;
        const previous = state.sessions[index - 1];
        const repeated = previous && `${previous.model} · ${previous.context_profile}` === meta;
        return `<div class="session-row"><button class="session-item ${item.id === selectedID ? "active" : ""} ${repeated ? "terse" : ""}" data-session-id="${escapeHTML(item.id)}" title="${escapeHTML(`${item.title} — ${meta}`)}"><strong>${escapeHTML(item.title)}</strong>${repeated ? "" : `<span>${escapeHTML(meta)}</span>`}</button><button class="session-delete" data-delete-session="${escapeHTML(item.id)}" title="Delete this session" aria-label="Delete ${escapeHTML(item.title)}">×</button></div>`;
      }).join("") : `<div class="session-empty">No sessions yet</div>`}</div>`;
  const railStart = $("#railNewSession");
  railStart.disabled = enabledProviders.length > 0 && !canStart;
  // One short label on one line. Which envelope it opens under is the summary
  // line's job, not the button's.
  railStart.textContent = "＋ New session";
  root.innerHTML = `<div class="chat-layout"><section class="chat-stage">
      ${session ? `<header class="chat-head"><div><p class="eyebrow">${escapeHTML(session.provider_name)} / ${escapeHTML(session.context_profile)}</p><h2>${escapeHTML(session.title)}</h2><small>contract ${escapeHTML(shortHash(session.contract_revision))} · cache epoch ${session.cache_epoch} · ${escapeHTML(session.contract?.qualification?.mode || "unbound")}</small><div class="session-capabilities"><button class="capability-chip" data-open-capabilities="skills">Skills <strong>${selectedSkills.length}/${skillCatalog.length}</strong></button><button class="capability-chip" data-open-capabilities="tools">Direct tools <strong>${directTools.length}</strong></button><button class="capability-chip" data-open-capabilities="mcp">MCP ready <strong>${readyMCPTools}</strong></button></div></div><div class="chat-state">${pill(session.state, session.state === "active" ? "green" : "amber")}${pill(session.model,"blue")}</div></header>
        <div class="message-list" id="messageList">${timeline.length ? groupTimeline(timeline).map(renderTimelineItem).join("") : `<div class="chat-welcome"><img src="/assets/brand/hermetrix-engine-v3-512.png" alt=""><h3>Hermetrix is ready</h3><p>Each turn freezes its provider, model, context snapshot, capability revision and policy revision before sampling.</p></div>`}${questions.map(elicitationCardHTML).join("")}<article class="chat-message assistant streaming ${state.sending ? "" : "hidden"}" id="streamingAssistant"><div class="message-role">Hermetrix</div><div class="message-body"></div><div class="message-proof" id="streamStatus">waiting for provider…</div></article></div>
        <form class="composer" id="chatForm"><div class="composer-tools"><button type="button" class="composer-tool-button" id="composerCapabilityButton">＋ Skills & tools</button><button type="button" class="composer-tool-button" id="composerFilesButton">Files</button><button type="button" class="composer-tool-button" id="composerTerminalButton">Terminal</button><span class="composer-context">${escapeHTML(projectName)} · ${escapeHTML(session.context_profile)}</span></div><textarea id="chatInput" rows="2" maxlength="1048576" placeholder="Ask Hermetrix to work…  Enter sends, Shift+Enter adds a line, @ picks a Skill or tool" ${state.sending ? "disabled" : ""}></textarea><button class="primary" ${state.sending ? "disabled" : ""}>${state.sending ? "Running…" : "Send"}</button></form>` : `<div class="chat-welcome standalone"><img src="/assets/brand/hermetrix-engine-v3-512.png" alt=""><h3>${enabledProviders.length ? "Ready when you are" : "Connect a model first"}</h3><p>${enabledProviders.length ? "Press ＋ New session in the sidebar. It uses the model and context envelope shown there; change them under Options whenever you want." : "Add any OpenAI-compatible endpoint and paste its API key. It takes effect immediately — there is nothing to set in your shell and nothing to restart."}</p>${enabledProviders.length ? "" : `<button class="primary" id="openProvidersButton">Connect a model</button>`}</div>`}
    </section></div>`;
  $("#chatProviderSelect")?.addEventListener("change", event => { state.draftProviderID = event.target.value; state.draftProfileName = ""; state.draftQualificationReason=""; state.sessionError=""; renderChat(); });
  $("#chatProjectSelect")?.addEventListener("change", event => { state.draftProjectID = event.target.value; });
  $("#chatProfileSelect")?.addEventListener("change", event => { state.draftProfileName = event.target.value; state.draftQualificationReason=""; state.sessionError=""; renderChat(); });
  // Update state without re-rendering: the textarea lives inside an open
  // <details>, and a re-render would collapse it and take the caret with it.
  $("#chatQualificationReason")?.addEventListener("input", event => { state.draftQualificationReason=event.target.value; $("#railNewSession").disabled=event.target.value.trim().length < 8; });
  // Remember whether Options is open across the re-render each select triggers.
  $("#sessionOptions")?.addEventListener("toggle", event => { state.sessionOptionsOpen = event.target.open; });
  $("#openProvidersFromDock")?.addEventListener("click", () => switchTab("providers"));
  $$("[data-session-id]", dock).forEach(button => button.addEventListener("click", () => selectSession(button.dataset.sessionId)));
  $$("[data-delete-session]", dock).forEach(button => button.addEventListener("click", event => {
    event.stopPropagation();
    deleteSession(button.dataset.deleteSession);
  }));
  $$("[data-elicit-accept]", root).forEach(form => form.addEventListener("submit", answerElicitation));
  $$("[data-elicit-decline]", root).forEach(button => button.addEventListener("click", () => declineElicitation(button.dataset.elicitDecline)));
  $$("[data-approve-tool]", root).forEach(button => button.addEventListener("click", () => decideToolApproval(button.dataset.approveTool, "approve")));
  $$("[data-deny-tool]", root).forEach(button => button.addEventListener("click", () => decideToolApproval(button.dataset.denyTool, "deny")));
  $$("[data-open-capabilities]", root).forEach(button => button.addEventListener("click", () => openCapabilityPicker(button.dataset.openCapabilities)));
  $("#composerCapabilityButton")?.addEventListener("click", () => openCapabilityPicker("all"));
  $("#composerFilesButton")?.addEventListener("click", () => switchWorkbench("files"));
  // Terminal no longer has a side-strip room to switch to; this opens (or
  // reveals) a terminal pane in Code instead, so the quick button still
  // works without reviving the second door the redesign closed.
  $("#composerTerminalButton")?.addEventListener("click", () => openContentPane("terminal"));
  bindComposer();
  $("#chatForm")?.addEventListener("submit", sendTurn);
  $("#openProvidersButton")?.addEventListener("click", () => switchTab("providers"));
  requestAnimationFrame(() => {
    const list = $("#messageList");
    if (!list) return;
    // A pending approval is the one thing the turn is waiting on, so put it on
    // screen rather than the bottom of a transcript that has scrolled past it.
    const pending = $(".approval-card [data-approve-tool]", list);
    if (pending) pending.closest(".approval-card").scrollIntoView({ block: "nearest" });
    else list.scrollTop = list.scrollHeight;
  });
}

// renderChatRail/Main/Side are the static shell those three zones carry in
// index.html at first paint -- the dock, the transcript section, the
// workbench strip -- reproduced here so switchView can put it back after
// showing one of the other views. They hand back an empty session dock, an
// empty transcript section and an empty workbench pane on purpose: filling
// them is renderChat's and renderCurrentWorkbench's job, not this one's, so
// chat's live markup is computed in exactly the one place it always was.
function renderChatRail() {
  return `<button class="new-chat" id="railNewSession">＋ New session</button>
    <div class="session-dock" id="sessionDock" aria-label="Agent sessions"></div>
    <div class="rail-footer">
      <div class="runtime-card">
        <img class="runtime-mark" src="/assets/brand/hermetrix-icon-v3-192.png" alt="">
        <div><strong>Hermetrix Engine</strong><small><span class="status-dot"></span> Local-first · authority gated</small></div>
      </div>
    </div>`;
}

function renderChatMain() {
  return `<section class="view active" id="view-chat"></section>`;
}

// The active class comes from state rather than being hard-coded to "review"
// so that leaving chat for another view and coming back does not silently
// snap the workbench back to the first tab.
function renderChatSide() {
  // Terminal and browser left this 320px strip for Code's panes -- both need
  // room a fixed-width column cannot give them -- so only the rooms that
  // still fit one stay here.
  const rooms = [["review", "Review"], ["files", "Files"], ["artifacts", "Office"], ["team", "Team"]];
  return `<nav class="workbench-tabs" aria-label="Workbench rooms">${rooms.map(([id, label]) =>
    `<button class="workbench-tab ${state.workbenchTab === id ? "active" : ""}" data-workbench="${id}">${escapeHTML(label)}</button>`).join("")}</nav>
    <div class="workbench-content" id="workbenchContent"></div>`;
}

// Each view fills the same three zones. What changes is the content; what
// each zone means does not, which is what makes moving between them
// predictable.
//
// Three of the four are specs, not code, yet. They say so and name the spec:
// a view that opens onto nothing is worse than one that explains itself.
const VIEWS = {
  chat: {
    label: "Chat",
    rail: () => renderChatRail(),
    main: () => renderChatMain(),
    side: () => renderChatSide()
  },
  work: {
    label: "Work",
    rail: () => unbuilt("Boards and tasks", "spec 3"),
    main: () => unbuilt("Work — kanban, backlog, linking work to chat", "spec 3"),
    side: () => ""
  },
  code: {
    label: "Code",
    rail: () => unbuilt("Files and diffs", "spec 2"),
    // Code is the one view whose main area is a split rather than a single
    // surface -- the spec's own table gives only this row more than one
    // pane -- so main() hands back an empty host and renderPanes() fills it,
    // the same division of labour renderChatMain()/renderChat() already use.
    main: () => "",
    side: () => ""
  },
  knowledge: {
    label: "Knowledge",
    rail: () => unbuilt("Library and sources", "spec 4"),
    main: () => unbuilt("Knowledge — notes, semantic search", "spec 4"),
    side: () => ""
  }
};

function unbuilt(what, spec) {
  return `<div class="unbuilt"><h3>${escapeHTML(what)}</h3>
    <p>Not built yet. Waiting on <strong>${escapeHTML(spec)}</strong> of the redesign.</p></div>`;
}

// The rail's New session button and the workbench tab strip are recreated
// every time switchView rebuilds chat's skeleton, so a listener bound to them
// once at load time would not survive a trip through another view. Wiring
// lives here, called at startup and again on every return to chat, so both
// stay exactly as functional as the first paint.
function wireChatSkeleton() {
  $("#railNewSession").addEventListener("click", () => {
    switchTab("chat");
    if (!state.providers.some(provider => provider.enabled)) { switchTab("providers"); return; }
    if (state.sessionReady) { createAgentSession(); return; }
    state.sessionOptionsOpen = true;
    state.sessionDetail = null;
    state.selectedSession = null;
    renderChat();
    $("#chatProviderSelect")?.focus();
  });
  $$(".workbench-tab").forEach(node => node.addEventListener("click", () => switchWorkbench(node.dataset.workbench)));
}

// switchView is the one place a top-level view becomes visible. Chat is the
// only one with real state behind it -- sessions, polling, a composer that
// has to keep its caret across renders -- so its zones are rebuilt from the
// skeleton above and handed straight back to the renderChat/renderCurrentWorkbench
// pipeline that already knows how to keep that state alive; duplicating
// chat's live markup into this registry would give the transcript two places
// to be computed; the one that ran second would win. The other three views
// have no such state yet, so their rail and main just say what they will be,
// and a side with nothing to put in it is hidden rather than drawn empty.
function switchView(name) {
  const view = VIEWS[name] ? name : "chat";
  $$("#viewSwitch [data-view]").forEach(button => button.classList.toggle("on", button.dataset.view === view));
  if (view === state.view) return;
  state.view = view;
  // A terminal or team run polls on its own timer independent of any render
  // call. Leaving chat tears down the workbench pane those polls write into,
  // so the timer is cancelled here rather than left to throw on a pane that
  // no longer exists.
  clearTimeout(workbenchPollTimer);
  $("#zoneRail").innerHTML = VIEWS[view].rail();
  $("#zoneMain").innerHTML = VIEWS[view].main();
  const side = VIEWS[view].side();
  $("#zoneSide").innerHTML = side;
  collapseZone("side", !side);
  if (view === "chat") {
    wireChatSkeleton();
    renderChat();
    if (!$("#zones").classList.contains("side-hidden")) renderCurrentWorkbench();
  }
  // main() above hands Code an empty host on purpose; this is what actually
  // fills it, the same way renderChat() fills the empty <section> chat's own
  // main() returns.
  if (view === "code") renderPanes();
}

// Every action answers two questions and no others: did the press register,
// and what is it doing now. The answer belongs on the control that was pressed
// -- a toast is for something that happened somewhere else, and it is gone in
// under three seconds, which is the wrong place for a failure.
//
// The label while working names the work. "Discovering this server's tools" is
// an answer; "Loading" is a word that fills the same space and says nothing.
async function runAction(button, { working, done, run }) {
  // "done" is not idle: the button still shows the finished label and is
  // still holding for --dur-hold-done. A caller that starts a new run during
  // that hold would capture "done" as its own idle text, and the first run's
  // still-pending timeout would later stomp the second run's state.
  if (!button || button.dataset.actionState === "working" || button.dataset.actionState === "done") return;
  // Any hold timer left over from a previous run must die here rather than
  // fire later and overwrite whatever this run is about to set.
  clearTimeout(button._runActionHold);
  const idle = button.textContent;
  const previousError = button.parentElement?.querySelector(".action-error");
  previousError?.remove();
  button.dataset.actionState = "working";
  button.setAttribute("aria-busy", "true");
  button.disabled = true;
  button.textContent = working;
  try {
    const result = await run();
    button.dataset.actionState = "done";
    button.textContent = done;
    // Work that finished says so and holds long enough to read, rather than
    // snapping back as though nothing happened. The timer is kept on the
    // button itself so a later run (or this same run, if it were ever
    // re-entered) can cancel it instead of racing it.
    button._runActionHold = setTimeout(() => {
      button.removeAttribute("data-action-state");
      button.removeAttribute("aria-busy");
      button.disabled = false;
      button.textContent = idle;
    }, motionMS("--dur-hold-done"));
    return result;
  } catch (error) {
    button.dataset.actionState = "failed";
    button.removeAttribute("aria-busy");
    button.disabled = false;
    button.textContent = idle;
    // The failure stays next to the button until the next attempt.
    const note = document.createElement("p");
    note.className = "action-error";
    note.textContent = error.message;
    button.parentElement?.appendChild(note);
    return undefined;
  }
}

// motionMS reads a duration token so JavaScript timing and CSS timing cannot
// drift apart. A number typed here would be the second answer this file spent
// a whole rule avoiding.
function motionMS(token) {
  const raw = getComputedStyle(document.documentElement).getPropertyValue(token).trim();
  const value = parseFloat(raw) || 0;
  return raw.endsWith("ms") ? value : value * 1000;
}

/* --- Composer keys ---------------------------------------------------------
   Sending took Cmd-Enter and nothing else, which is not what a message box
   does anywhere else: Enter sends, Shift-Enter writes a second line. The rest
   of these exist because the box is re-rendered on every streamed token, and
   an input that loses your draft or your cursor while you type into it is
   worse than one with no shortcuts at all. */
function bindComposer() {
  const input = $("#chatInput");
  if (!input) return;

  // The draft survives the re-render that each streamed delta triggers.
  input.value = state.draftMessage || "";
  input.addEventListener("input", event => {
    if (event.target.value.endsWith("@")) {
      event.target.value = event.target.value.slice(0, -1);
      state.draftMessage = event.target.value;
      openCapabilityPicker("all");
      return;
    }
    state.draftMessage = event.target.value;
  });

  input.addEventListener("keydown", event => {
    if (event.key === "Enter" && !event.shiftKey && !event.altKey && !event.isComposing) {
      // Cmd/Ctrl-Enter keeps working: it was the only way to send for a while
      // and fingers remember it.
      event.preventDefault();
      $("#chatForm")?.requestSubmit();
      return;
    }
    if (event.key === "Escape" && event.target.value) {
      // Clear the draft, keep the caret here. Escape with an empty box does
      // nothing so it can still reach whatever is behind the composer.
      event.preventDefault();
      event.target.value = "";
      state.draftMessage = "";
      return;
    }
    if (event.key === "ArrowUp" && !event.target.value && !state.sending) {
      // An empty box plus Up recalls what you last said, which is how you fix
      // a typo in a message you have already sent without retyping it.
      const previous = [...(state.sessionDetail?.events || [])]
        .reverse().find(item => item.event_kind === "message" && item.role === "user");
      if (!previous) return;
      event.preventDefault();
      event.target.value = previous.content;
      state.draftMessage = previous.content;
      event.target.setSelectionRange(previous.content.length, previous.content.length);
    }
  });

  // Restore the caret and the focus the re-render just took away, but only if
  // the person was already typing here.
  if (state.composerFocused && !state.sending) {
    input.focus();
    const caret = Math.min(state.composerCaret ?? input.value.length, input.value.length);
    input.setSelectionRange(caret, caret);
  }
}

// captureComposer reads where the caret is and whether the box has focus, and
// it runs before the re-render rather than from a blur handler: replacing the
// element does not reliably fire blur, and by the time the new one is bound the
// old one is already gone.
function captureComposer() {
  const input = $("#chatInput");
  if (!input) return;
  state.composerFocused = document.activeElement === input;
  state.composerCaret = input.selectionStart;
  state.draftMessage = input.value;
}

async function createAgentSession() {
  if (!state.draftProviderID || !state.draftProfileName) return;
  // A session has to bind to a project -- Task 2's Inbox migration swept up
  // every session that once didn't. Trusting every future caller to reach
  // this function only after a project is open is exactly the assumption
  // that already produced one silent orphan; check it here instead, at the
  // one place the value is actually used, and fail on screen rather than
  // throwing past the button that was pressed.
  const projectID = state.draftProjectID || state.currentProject?.id;
  if (!projectID) {
    state.sessionError = "No project is open. Choose one from the project picker before starting a session.";
    renderChat();
    return;
  }
  const provider = state.providers.find(item => item.id === state.draftProviderID);
  const profile = state.profiles.find(item => item.name === state.draftProfileName);
  const admission = profileAdmission(provider, profile);
  const project = state.projects.find(item => item.id === state.draftProjectID);
  try {
    state.sessionError = "";
    const body = {
      provider_id: state.draftProviderID,
      project_id: projectID,
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

// Deleting a session removes the conversation. The dialog says what survives,
// because "delete" on a harness that keeps provenance means something narrower
// than it does elsewhere, and a user who assumes otherwise is being misled.
async function deleteSession(id) {
  const session = state.sessions.find(item => item.id === id);
  if (!session) return;
  const approved = await askAction({
    eyebrow: "Session",
    title: `Delete "${session.title}"?`,
    message: "The transcript, its context snapshots and any pending approvals are removed. What was learned stays: Skills, reviews and usage evidence are kept, and files this session produced are kept and detached from it.",
    confirmLabel: "Delete session",
    danger: true
  });
  if (!approved) return;
  try {
    await api(`/api/sessions/${encodeURIComponent(id)}`, { method:"DELETE" });
    if (state.selectedSession === id) {
      state.selectedSession = null;
      state.sessionDetail = null;
    }
    toast("Session deleted");
    await load();
    switchTab("chat");
  } catch (error) { toast(error.message, true); }
}

async function selectSession(id) {
  try {
    state.selectedSession = id;
    state.selectedSkillDetail = null;
    state.sessionDetail = await api(`/api/sessions/${encodeURIComponent(id)}`);
    renderChat();
    if (state.workbenchTab === "review" && !$("#zones").classList.contains("side-hidden")) renderWorkbenchReview();
  } catch (error) { toast(error.message, true); }
}

async function sendTurn(event) {
  event.preventDefault();
  if (state.sending || !state.sessionDetail?.session) return;
  const input = $("#chatInput");
  const content = input.value.trim();
  if (!content) return;
  state.draftMessage = "";
  state.composerCaret = 0;
  state.composerFocused = true;
  state.sending = true;
  renderChat();
  setTimeout(pollElicitations, 600);
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
    state.elicitations = [];
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

// Connecting a model is four fields and a key, so those come first and the
// registry's tuning knobs go behind Advanced. The API key is a plain field:
// requiring an environment variable and a server restart to try a model was
// the single biggest reason this page could not be used.
const MODEL_FLOW = [
  ["Connect", "Name the endpoint, paste the API key. It is saved to this machine only — never to the database, a backup or a log."],
  ["Test", "One cheap request proves the endpoint, the model name and the key actually work together."],
  ["Qualify", "A full run measures real capacity so a context envelope is admitted on evidence, not on a declared number."],
  ["Use", "Start a session. Provider, model, context snapshot and policy are frozen for every turn."]
];

function renderProviders() {
  const root = $("#view-providers");
  if (!root) return;
  const list = state.providers;
  const qualified = list.filter(item => item.context_evidence === "qualified").length;
  const metrics = [
    [list.length.toLocaleString(), "connected"],
    [list.filter(item => item.credential_stored).length.toLocaleString(), "key saved"],
    [qualified.toLocaleString(), "qualified"]
  ];
  const hero = `<section class="capability-hero"><div class="capability-hero-head"><div><p class="eyebrow">Models</p><h3>${list.length ? `${list.length} model${list.length === 1 ? "" : "s"} connected` : "No model connected yet"}</h3><p>Any OpenAI-compatible endpoint works — a hosted API or a local runtime. Paste the key here; there is nothing to put in your shell and nothing to restart.</p></div><div class="capability-hero-metrics">${metrics.map(([value, label]) => `<div class="capability-metric"><strong>${escapeHTML(value)}</strong><span>${escapeHTML(label)}</span></div>`).join("")}</div></div></section>`;
  const cards = list.length ? list.map(provider => `<article class="provider-card"><div class="provider-head"><div><div class="row-title"><h3>${escapeHTML(provider.name)}</h3>${pill(provider.enabled ? "enabled" : "disabled", provider.enabled ? "green" : "amber")}${pill(provider.context_evidence, provider.context_evidence === "qualified" ? "green" : "amber")}</div><p>${escapeHTML(provider.base_url)}</p></div>${pill(provider.credential_stored ? "key saved" : provider.api_key_env ? "key from environment" : "no key set", provider.credential_stored || provider.api_key_env ? "green" : "amber")}</div><div class="kv"><span>Model</span><strong>${escapeHTML(provider.model)}</strong><span>Context</span><strong>${provider.context_window.toLocaleString()}</strong><span>Output</span><span>${provider.max_output_tokens.toLocaleString()}</span><span>Key source</span><span>${provider.credential_stored ? "saved on this machine" : provider.api_key_env ? `environment · ${escapeHTML(provider.api_key_env)}` : "none required"}</span></div><div class="action-row"><button class="ghost" data-provider-key="${escapeHTML(provider.id)}">${provider.credential_stored ? "Replace API key" : "Set API key"}</button><button class="ghost" data-test-provider="${escapeHTML(provider.id)}" ${provider.credential_ready ? "" : "disabled"}>Test connection</button><button class="primary" data-qualify-provider="${escapeHTML(provider.id)}" ${provider.credential_ready ? "" : "disabled"}>Full qualification</button></div></article>`).join("") : `<div class="empty"><h3>No model connected</h3><p>Open the Model registry panel and connect one. A hosted endpoint needs its API key; a local runtime usually needs none.</p></div>`;
  const flow = `<div class="panel"><p class="eyebrow">How connecting works</p><div class="tool-flow">${MODEL_FLOW.map(([title, detail], index) => `<article><b>${index + 1}</b><div><strong>${escapeHTML(title)}</strong><small>${escapeHTML(detail)}</small></div></article>`).join("")}</div></div>`;
  const setup = `<details class="panel connection-setup" ${list.length ? "" : "open"}><summary><div><p class="eyebrow">Model registry</p><h3>Connect a model</h3></div></summary><div class="connection-setup-body"><form id="providerForm">
      <label>Name<input name="name" required maxlength="80" placeholder="OpenAI, my local gateway…"></label>
      <label>Base URL<input name="base_url" required type="url" placeholder="https://api.openai.com/v1"></label>
      <label>Model<input name="model" required maxlength="240" placeholder="model ID from your provider"></label>
      <label>API key<input name="api_key" type="password" autocomplete="off" spellcheck="false" placeholder="Paste the key — leave empty for a local model"></label>
      <p class="form-note neutral">The key is written to <code>secrets.json</code> in your data directory with owner-only permissions. It never enters the database, a backup export, a log line or any API response.</p>
      <details class="advanced-fields"><summary>Advanced</summary><div class="advanced-fields-body">
        <div class="form-grid"><label>Context window<input name="context_window" type="number" min="4096" max="2097152" value="131072" required></label><label>Max output<input name="max_output_tokens" type="number" min="128" value="8192" required></label></div>
        <label>Read the key from an environment variable instead<input name="api_key_env" pattern="[A-Z][A-Z0-9_]{1,126}" placeholder="HERMETRIX_PROVIDER_API_KEY"></label>
        <p class="form-note neutral">A saved key wins over the variable. Use the variable when a process manager or secret manager injects it.</p>
      </div></details>
      <button class="primary">Connect model</button>
    </form></div></details>
    <details class="panel connection-setup"><summary><div><p class="eyebrow">Qualification</p><h3>Measurement controls</h3></div></summary><div class="connection-setup-body"><label>Requested profile<select id="qualificationProfile">${state.profiles.map(profile => `<option value="${profile.name}" ${profile.name === "certified-64k" ? "selected" : ""}>${profileLabel(profile)}</option>`).join("")}</select></label><label>Optional local runtime<select id="qualificationRuntime"><option value="">None · behavioral only</option><option value="ollama">Ollama</option><option value="lmstudio">LM Studio</option><option value="vllm">vLLM</option><option value="llamacpp">llama.cpp</option></select></label><label>Runtime endpoint<input id="qualificationEndpoint" value="http://127.0.0.1:11434"></label><p class="form-note neutral">Eligibility is never silently downgraded. Missing allocation evidence remains limited.</p></div></details>`;
  const runs = state.qualifications.length ? `<div class="mcp-connection-head"><h3>Qualification runs</h3>${pill(`${state.qualifications.length} recorded`, "blue")}</div><div class="card-list qualification-list">${state.qualifications.map(run => `<article class="provider-card"><div class="provider-head"><div><h3>${escapeHTML(run.provider_name)} · ${escapeHTML(run.model)}</h3><p>${formatDate(run.completed_at || run.started_at)}</p></div>${pill(`grade ${run.capability_grade}`, run.capability_grade === "A" ? "green" : run.capability_grade === "B" ? "amber" : "red")}</div><div class="kv"><span>Context tier</span><strong>${escapeHTML(run.context_tier)}</strong><span>Allocated</span><strong>${Number(run.allocated_context || 0).toLocaleString()}</strong><span>Requested</span><span>${escapeHTML(run.requested_profile)}</span><span>Eligibility</span><strong>${run.eligible ? "eligible" : "explicit decision required"}</strong><span>TTFT</span><span>${run.results.ttft_milliseconds || 0} ms</span><span>Throughput</span><span>${Number(run.results.tokens_per_second || 0).toFixed(1)} tok/s</span></div>${(run.remediation || []).length ? `<ul class="findings">${run.remediation.map(item => `<li>${escapeHTML(item)}</li>`).join("")}</ul>` : ""}</article>`).join("")}</div>` : "";
  root.innerHTML = `${hero}<div class="tool-center-grid"><div><div class="card-list">${cards}</div>${runs}</div><div class="tool-center-aside">${flow}${setup}</div></div>`;
  $("#providerForm")?.addEventListener("submit", saveProvider);
  $$("[data-test-provider]", root).forEach(button => button.addEventListener("click", () => testProvider(button.dataset.testProvider)));
  $$("[data-qualify-provider]", root).forEach(button => button.addEventListener("click", () => qualifyProvider(button.dataset.qualifyProvider)));
  $$("[data-provider-key]", root).forEach(button => button.addEventListener("click", () => setProviderCredential(button.dataset.providerKey)));
}

// setProviderCredential is the "I already connected this, the key changed"
// path. An empty answer clears the key, which is the only way to remove one.
async function setProviderCredential(id) {
  const provider = state.providers.find(item => item.id === id);
  if (!provider) return;
  const token = await askAction({
    eyebrow: "Credential",
    title: `API key for ${provider.name}`,
    message: "The key is written to this machine only — never to the database, a backup export, a log line or any API response. Leave it empty to remove the saved key.",
    confirmLabel: "Save key",
    reasonLabel: "API key"
  });
  if (token === null) return;
  try {
    await api(`/api/providers/${encodeURIComponent(id)}/credential`, { method:"PUT", body:JSON.stringify({ api_key: token }) });
    toast(token.trim() ? "API key saved on this machine" : "Saved API key removed");
    await load();
    switchTab("providers");
  } catch (error) { toast(error.message, true); }
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
    const key = String(values.get("api_key") || "");
    await api("/api/providers", { method:"POST", body:JSON.stringify({ name:values.get("name"), adapter_kind:"openai-compatible", base_url:values.get("base_url"), model:values.get("model"), api_key:key, api_key_env:values.get("api_key_env"), context_window:Number(values.get("context_window")), context_evidence:"declared", max_output_tokens:Number(values.get("max_output_tokens")) }) });
    form.reset();
    toast(key.trim() ? "Model connected and API key saved on this machine" : "Model connected");
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

// The Tool Center answers "what can this thing do, and how do I let it" before
// it asks for an endpoint. The connection form used to be the first thing on
// the page, which put the one step that needs a decision ahead of the four that
// explain it.
// A capability name carries its kind: resources and prompts share the catalog
// with tools, and a row that does not say which is which reads as one list of
// things that all behave the same way, which they do not.
function capabilityKind(item) {
  const name = String(item?.name || "");
  if (name.startsWith("resource:")) return "resource";
  if (name.startsWith("prompt:")) return "prompt";
  return "tool";
}

// describeCatalog says what one server published, drawing only the kinds it
// actually has: a "0 prompts" on every tools-only server is noise. Resource and
// prompt counts come from whatever the current search has surfaced, because the
// catalog is searched rather than listed whole.
function describeCatalog(serverID) {
  const server = state.mcp_servers.find(item => item.id === serverID);
  const counts = { tool: Number(server?.tool_count || 0), resource: 0, prompt: 0 };
  for (const item of state.capabilityResults) {
    if (item.source_ref !== serverID) continue;
    const kind = capabilityKind(item);
    if (kind !== "tool") counts[kind] += 1;
  }
  const parts = [];
  if (counts.tool) parts.push(`${counts.tool} tools`);
  if (counts.resource) parts.push(`${counts.resource} resources`);
  if (counts.prompt) parts.push(`${counts.prompt} prompts`);
  return parts.length ? parts.join(" · ") : "nothing yet";
}

const TOOL_FLOW = [
  ["Connect", "Point Hermetrix at a program on this machine or at a Streamable HTTP URL. A token you paste is saved to this machine only."],
  ["Discover", "Hermetrix replaces that server's snapshot atomically and indexes every tool at an exact revision."],
  ["Search", "The model asks by intent and finds tools, resources and prompt templates alike. Search returns bounded metadata; a full schema loads only when one is opened."],
  ["Approve", "Remote calls are fail-closed. You see the exact arguments and their hash before anything runs."]
];

function renderMCP() {
  const root = $("#view-mcp");
  if (!root) return;
  const summary = state.capability_summary || { total:0, by_source:{}, by_readiness:{} };
  const servers = state.mcp_servers;
  const directCount = Number(state.sessionDetail?.session?.contract?.tool_bindings?.length || 0);
  const directSummary = directCount ? `${directCount} direct tools in this session` : "a bounded direct-tool set";
  const untrusted = servers.filter(server => !server.trust_annotations).length;
  const metrics = [
    [Number(summary.total || 0).toLocaleString(), "indexed capabilities"],
    [Number(summary.by_readiness?.ready || 0).toLocaleString(), "ready"],
    [servers.length.toLocaleString(), "connections"],
    [untrusted.toLocaleString(), "approval by default"]
  ];
  const hero = `<section class="capability-hero"><div class="capability-hero-head"><div><p class="eyebrow">Tool Center</p><h3>${Number(summary.total || 0).toLocaleString()} deferred capabilities reachable</h3><p>Hermetrix keeps ${escapeHTML(directSummary)} in the model prompt. Tools, resources and prompt templates published by MCP servers stay deferred: indexed here, searched on demand, and loaded only when the model describes one.</p></div><div class="capability-hero-metrics">${metrics.map(([value, label]) => `<div class="capability-metric"><strong>${escapeHTML(value)}</strong><span>${escapeHTML(label)}</span></div>`).join("")}</div></div></section>`;
  const selected = state.selectedCapability ? `<article class="provider-card capability-detail"><div class="provider-head"><div><div class="row-title"><h3>${escapeHTML(state.selectedCapability.title || state.selectedCapability.name)}</h3>${pill(state.selectedCapability.effect, state.selectedCapability.requires_approval ? "amber" : "green")}${pill(state.selectedCapability.readiness, state.selectedCapability.readiness === "ready" ? "green" : "red")}</div><p>${escapeHTML(state.selectedCapability.description || "No description supplied by MCP server")}</p></div>${pill(state.selectedCapability.source)}</div><div class="kv"><span>Capability ID</span><code>${escapeHTML(state.selectedCapability.id)}</code><span>Revision</span><code>${escapeHTML(state.selectedCapability.revision)}</code><span>Source ref</span><code>${escapeHTML(state.selectedCapability.source_ref)}</code><span>Approval</span><strong>${state.selectedCapability.requires_approval ? "required" : "not required"}</strong></div><section class="inspect-section"><h3>Exact input schema</h3><pre>${escapeHTML(JSON.stringify(state.selectedCapability.input_schema, null, 2))}</pre></section><p class="form-note neutral">This schema is loaded on demand. It is not part of the direct model prompt until tool_describe is called.</p><div class="action-row"><button class="primary" type="button" data-use-capability="${escapeHTML(state.selectedCapability.id)}">Use in chat</button></div></article>` : `<div class="probe-empty">Search the catalog, then open a result to read its exact revision and schema.</div>`;
  const search = `<div class="panel"><div class="provider-head"><div><p class="eyebrow">Deferred capability graph</p><h3>Find a tool by what it does</h3></div>${pill(`${summary.by_readiness?.ready || 0} ready`, "green")}</div>
      <form id="capabilitySearchForm" class="capability-search"><label>Search catalog<input id="capabilityQuery" required placeholder="calendar, repository search, database…"></label><button class="ghost">Search</button></form>
      <div id="capabilityResults">${state.capabilityResults.length ? state.capabilityResults.map(item => `<button class="capability-result" data-capability-id="${escapeHTML(item.id)}"><span><strong>${escapeHTML(item.title || item.name)}</strong><small>${escapeHTML(item.description || "No description")}</small></span><span>${pill(capabilityKind(item), "blue")}${pill(item.effect, item.requires_approval ? "amber" : "green")}${pill(item.readiness, item.readiness === "ready" ? "green" : "red")}</span></button>`).join("") : `<div class="probe-empty">Search returns bounded metadata only—never the complete catalog schemas.</div>`}</div>
      ${selected}</div>`;
  const serverList = `<div class="mcp-connection-head"><h3>Connections</h3>${pill(`${servers.filter(server => server.status === "ready").length}/${servers.length} ready`, servers.length && servers.every(server => server.status === "ready") ? "green" : "amber")}</div>
    <div class="card-list mcp-server-list">${servers.length ? servers.map(server => `<article class="provider-card"><div class="provider-head"><div><div class="row-title"><h3>${escapeHTML(server.name)}</h3>${pill(server.status, server.status === "ready" ? "green" : server.status === "error" ? "red" : "amber")}${pill(server.last_protocol || server.protocol_mode, "blue")}</div><p><code>${escapeHTML(server.endpoint)}</code></p></div>${pill(server.credential_stored ? "token saved" : server.api_key_env ? "token from environment" : "no token set", server.credential_stored || server.api_key_env ? "green" : "amber")}</div><div class="kv"><span>Runs as</span><strong>${server.transport_kind === "stdio" ? "local program" : "remote URL"}</strong><span>Publishes</span><strong>${escapeHTML(describeCatalog(server.id))}</strong><span>Timeout</span><span>${server.request_timeout_ms.toLocaleString()} ms</span><span>Risk hints</span><strong>${server.trust_annotations ? "trusted by user" : "untrusted · approval default"}</strong><span>Token source</span><span>${server.credential_stored ? "saved on this machine" : server.api_key_env ? `environment · ${escapeHTML(server.api_key_env)}` : "none required"}</span><span>Discovered</span><span>${formatDate(server.last_discovered_at)}</span></div>${server.last_error ? `<ul class="findings"><li class="error">${escapeHTML(server.last_error)}</li></ul>` : ""}<div class="action-row"><button class="ghost" data-mcp-key="${escapeHTML(server.id)}">${server.credential_stored ? "Replace token" : "Set token"}</button><button class="primary" data-discover-mcp="${escapeHTML(server.id)}" ${server.enabled && server.credential_ready ? "" : "disabled"}>Discover catalog</button></div></article>`).join("") : `<div class="empty"><h3>No MCP connections yet</h3><p>Connect one in the registry panel: most published MCP servers are a program you launch, such as <code>npx -y @modelcontextprotocol/server-everything</code>. Nothing reaches the model until you run discovery.</p></div>`}</div>`;
  const flow = `<div class="panel"><p class="eyebrow">How a tool call happens</p><div class="tool-flow">${TOOL_FLOW.map(([title, detail], index) => `<article><b>${index + 1}</b><div><strong>${escapeHTML(title)}</strong><small>${escapeHTML(detail)}</small></div></article>`).join("")}</div></div>`;
  const setup = `<details class="panel connection-setup" ${servers.length ? "" : "open"}><summary><div><p class="eyebrow">MCP connection registry</p><h3>Connect a tool server</h3></div></summary><div class="connection-setup-body"><form id="mcpForm">
      <label>Name<input name="name" required maxlength="80" placeholder="Local knowledge tools"></label>
      <label>How does this server run?<select name="transport_kind" id="mcpTransport"><option value="stdio">A program on this machine · stdio</option><option value="streamable-http">A URL · Streamable HTTP</option></select></label>
      <label id="mcpEndpointLabel">Command that starts it<input name="endpoint" id="mcpEndpoint" required placeholder="npx -y @modelcontextprotocol/server-everything"></label>
      <p class="form-note neutral" id="mcpEndpointNote">The program runs on this machine with only PATH, HOME and its own token in its environment, and it is started directly rather than through a shell. Allowed launchers: npx, node, bun, deno, uv, uvx, python, python3, docker, go.</p>
      <label>Bearer token<input name="api_key" type="password" autocomplete="off" spellcheck="false" placeholder="Paste the token — leave empty if the server needs none"></label>
      <p class="form-note neutral">The token is written to <code>secrets.json</code> in your data directory with owner-only permissions. It never enters the database, a backup export, a log line or any API response.</p>
      <label class="check-label"><input name="trust_annotations" type="checkbox"> Trust this server's risk annotations</label>
      <p class="form-note neutral">Default is fail-closed: annotations are untrusted and every remote call requires your approval.</p>
      <details class="advanced-fields"><summary>Advanced</summary><div class="advanced-fields-body">
        <div class="form-grid"><label>Protocol<select name="protocol_mode"><option value="auto">Auto · current then legacy</option><option value="2026-07-28">2026-07-28 · stateless</option><option value="2025-11-25">2025-11-25 · session</option></select></label><label>Timeout ms<input name="request_timeout_ms" type="number" min="1000" max="120000" value="15000" required></label></div>
        <label>Read the token from an environment variable instead<input name="api_key_env" pattern="[A-Z][A-Z0-9_]{1,126}" placeholder="HERMETRIX_MCP_API_KEY"></label>
      </div></details>
      <button class="primary">Connect server</button>
    </form></div></details>`;
  root.innerHTML = `${hero}<div class="tool-center-grid"><div>${search}${serverList}</div><div class="tool-center-aside">${flow}${setup}</div></div>`;
  $("#mcpForm")?.addEventListener("submit", saveMCPServer);
  // One question at a time: the endpoint field is a command or a URL depending
  // on the answer above it, so it renames itself rather than showing both.
  $("#mcpTransport")?.addEventListener("change", event => {
    const stdio = event.target.value === "stdio";
    const field = $("#mcpEndpoint");
    $("#mcpEndpointLabel").firstChild.textContent = stdio ? "Command that starts it" : "MCP endpoint";
    field.placeholder = stdio ? "npx -y @modelcontextprotocol/server-everything" : "https://host.example/mcp";
    field.type = stdio ? "text" : "url";
    field.value = "";
    $("#mcpEndpointNote").textContent = stdio
      ? "The program runs on this machine with only PATH, HOME and its own token in its environment, and it is started directly rather than through a shell. Allowed launchers: npx, node, bun, deno, uv, uvx, python, python3, docker, go."
      : "Remote endpoints must use https. Plain http is accepted only on loopback.";
  });
  $("#capabilitySearchForm")?.addEventListener("submit", searchCapabilities);
  $$('[data-discover-mcp]', root).forEach(button => button.addEventListener("click", () => discoverMCPServer(button.dataset.discoverMcp)));
  $$('[data-capability-id]', root).forEach(button => button.addEventListener("click", () => inspectCapability(button.dataset.capabilityId)));
  $$('[data-use-capability]', root).forEach(button => button.addEventListener("click", () => {
    const capability = state.selectedCapability;
    if (capability) mentionCapability({ kind:"mcp", name:capability.title || capability.name, id:capability.id });
  }));
  $$('[data-mcp-key]', root).forEach(button => button.addEventListener("click", () => setMCPCredential(button.dataset.mcpKey)));
}

async function setMCPCredential(id) {
  const server = state.mcp_servers.find(item => item.id === id);
  if (!server) return;
  const token = await askAction({
    eyebrow: "Credential",
    title: `Bearer token for ${server.name}`,
    message: "The token is written to this machine only — never to the database, a backup export, a log line or any API response. Leave it empty to remove the saved token.",
    confirmLabel: "Save token",
    reasonLabel: "Bearer token"
  });
  if (token === null) return;
  try {
    await api(`/api/mcp/servers/${encodeURIComponent(id)}/credential`, { method:"PUT", body:JSON.stringify({ api_key: token }) });
    toast(token.trim() ? "Bearer token saved on this machine" : "Saved bearer token removed");
    await load();
    switchTab("mcp");
  } catch (error) { toast(error.message, true); }
}

async function saveMCPServer(event) {
  event.preventDefault();
  const form = event.currentTarget;
  const values = new FormData(form);
  try {
    const saved = await api("/api/mcp/servers", { method:"POST", body:JSON.stringify({ name:values.get("name"), transport_kind:values.get("transport_kind") || "stdio", endpoint:values.get("endpoint"), api_key:String(values.get("api_key") || ""), api_key_env:values.get("api_key_env"), protocol_mode:values.get("protocol_mode"), trust_annotations:values.get("trust_annotations") === "on", request_timeout_ms:Number(values.get("request_timeout_ms")) }) });
    form.reset();
    toast(`Connected ${saved.name} — run discovery next`);
    await load();
    switchTab("mcp");
  } catch (error) { toast(error.message, true); }
}

async function discoverMCPServer(id) {
  const button = $(`[data-discover-mcp="${CSS.escape(id)}"]`);
  if (button) { button.disabled = true; button.textContent = "Discovering…"; }
  try {
    const result = await api(`/api/mcp/servers/${encodeURIComponent(id)}/discover`, { method:"POST", body:"{}" });
    const counted = [`${result.tools} tools`];
    if (result.resources) counted.push(`${result.resources} resources`);
    if (result.prompts) counted.push(`${result.prompts} prompts`);
    toast(`Indexed ${counted.join(" · ")} via MCP ${result.protocol}${result.rejected ? ` · rejected ${result.rejected}` : ""}`);
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

// Bucketed classes keep progress bars compatible with style-src 'self'.
function applyDataFills(root) {
  $$("[data-fill]", root).forEach(node => node.classList.add(`fill-${Math.round(Number(node.dataset.fill || 0) / 5) * 5}`));
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

/* --- Folder picker ---------------------------------------------------------
   A browser will not tell a web page where a folder is: a file input reports
   names, never paths. So the walk happens on the server and this renders what
   it answers. Typing a path by hand still works; this is the way that does not
   require knowing it. */

async function openFolderPicker(startPath = "") {
  const dialog = $("#folderDialog");
  if (!dialog) return;
  if (!dialog.open) dialog.showModal();
  await loadFolder(startPath);
}

async function loadFolder(path) {
  const body = $("#folderBody");
  if (!body) return;
  body.innerHTML = `<p class="command-empty">Reading ${escapeHTML(path || "your home folder")}…</p>`;
  try {
    state.folderListing = await api(`/api/filesystem/directories?path=${encodeURIComponent(path || "")}`);
  } catch (error) {
    body.innerHTML = `<p class="command-empty">${escapeHTML(error.message)}</p>`;
    return;
  }
  renderFolderPicker();
}

function renderFolderPicker() {
  const listing = state.folderListing;
  const body = $("#folderBody");
  if (!listing || !body) return;
  $("#folderPath").textContent = listing.path;
  const rows = listing.entries.map(entry =>
    `<button type="button" class="folder-row" data-folder="${escapeHTML(entry.path)}"><span>▸</span><span>${escapeHTML(entry.name)}</span></button>`).join("");
  body.innerHTML = `${listing.parent ? `<button type="button" class="folder-row up" data-folder="${escapeHTML(listing.parent)}"><span>↑</span><span>Up to ${escapeHTML(listing.parent)}</span></button>` : ""}
    ${listing.unreadable ? `<p class="command-empty">This folder cannot be read. Go back up and try another.</p>`
      : rows || `<p class="command-empty">No folders inside this one. You can still choose it.</p>`}
    ${listing.truncated ? `<p class="command-empty">Only the first 500 folders are listed. Type the path if the one you want is not here.</p>` : ""}`;
  $$("[data-folder]", body).forEach(button => button.addEventListener("click", () => loadFolder(button.dataset.folder)));
}

function bindFolderPicker() {
  $("#folderClose")?.addEventListener("click", () => $("#folderDialog").close());
  $("#folderHome")?.addEventListener("click", () => loadFolder(state.folderListing?.home || ""));
  $("#folderChoose")?.addEventListener("click", () => {
    const chosen = state.folderListing?.path;
    if (!chosen) return;
    $("#folderDialog").close();
    const field = $("#projectRoot");
    if (!field) return;
    field.value = chosen;
    // Name the project after the folder unless the user already named it.
    const name = $('#projectForm input[name="name"]');
    if (name && !name.value.trim()) name.value = chosen.split("/").filter(Boolean).pop() || "";
    field.focus();
  });
}

function renderProjects() {
  const root = $("#view-projects");
  if (!root) return;
  const project = state.projects.find(item => item.id === state.selectedProject);
  root.innerHTML = `<div class="workbench-grid"><div class="panel"><p class="eyebrow">Bounded workspace registry</p><h3>Add project</h3><form id="projectForm"><label>Name<input name="name" required maxlength="100" placeholder="My workspace"></label><label>Existing local root<span class="path-field"><input name="root_path" id="projectRoot" required placeholder="/absolute/path"><button type="button" class="ghost" id="browseRoot">Browse…</button></span></label><button class="primary">Register project</button></form><section class="inspect-section"><h3>Projects</h3><div class="project-list">${state.projects.map(item => `<button class="session-item ${item.id === state.selectedProject ? "active" : ""}" data-project-id="${escapeHTML(item.id)}"><strong>${escapeHTML(item.name)}</strong><span>${escapeHTML(item.root_path)}</span></button>`).join("")}</div></section></div>
    <div class="panel"><div class="provider-head"><div><p class="eyebrow">Project workbench</p><h3>${escapeHTML(project?.name || "Select a project")}</h3><p>${escapeHTML(project?.root_path || "")}</p></div>${project ? pill(project.state,"green") : ""}</div>${project ? `<div class="file-browser"><div class="file-path"><code>${escapeHTML(state.projectPath || ".")}</code>${state.projectPath ? `<button class="ghost" id="projectUpButton">Up</button>` : ""}</div>${state.projectFiles.map(item => `<button class="file-row" data-file-path="${escapeHTML(item.path)}" data-directory="${item.directory}"><span>${item.directory ? "◇" : "·"}</span><strong>${escapeHTML(item.name)}</strong><small>${item.directory ? "directory" : `${Number(item.bytes).toLocaleString()} bytes`}</small></button>`).join("") || `<div class="probe-empty">Directory is empty.</div>`}</div><section class="inspect-section"><h3>Direct background command</h3><form id="commandForm"><div class="form-grid"><label>Executable<select name="executable"><option>go</option><option>git</option><option>node</option><option>npm</option><option>python3</option><option>rg</option><option>ls</option></select></label><label>Timeout seconds<input name="timeout" type="number" min="1" max="120" value="30"></label></div><label>Arguments as JSON array<textarea name="arguments" rows="3">["test", "./..."]</textarea></label><label>Working directory<input name="working_dir" value="${escapeHTML(state.projectPath || ".")}"></label><p class="form-note neutral">No shell is involved. Executable allowlist, root boundary, minimal environment, timeout, output limit and process-group cancellation are enforced server-side.</p><button class="primary">Start background job</button></form></section>` : `<div class="empty"><h3>No project selected</h3><p>Register an existing local directory to create a bounded workbench.</p></div>`}</div></div>`;
  $("#projectForm")?.addEventListener("submit", createProject);
  $("#browseRoot")?.addEventListener("click", () => openFolderPicker($("#projectRoot")?.value || ""));
  $$('[data-project-id]', root).forEach(button => button.addEventListener("click", () => selectProject(button.dataset.projectId, "")));
  $$('[data-file-path]', root).forEach(button => button.addEventListener("click", () => button.dataset.directory === "true" ? selectProject(state.selectedProject, button.dataset.filePath) : (activateWorkbenchChrome("files"), openWorkbenchFile(button.dataset.filePath))));
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
    // Files now has two possible homes; refreshWorkbenchSurface asks which
    // one (if either) is actually showing it rather than assuming the
    // chat-side tab.
    refreshWorkbenchSurface("files");
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

let workbenchPollTimer;

function switchWorkbench(tab) {
  clearTimeout(workbenchPollTimer);
  activateWorkbenchChrome(tab);
  renderCurrentWorkbench();
}

function renderCurrentWorkbench() {
  const tab = state.workbenchTab;
  if (tab === "review") renderWorkbenchReview();
  if (tab === "files") renderWorkbenchFiles();
  if (tab === "artifacts") renderWorkbenchArtifacts();
  if (tab === "team") renderWorkbenchTeam();
}

// Terminal has exactly one home now: a Code pane. Its poll loop and the
// output fetch it drives both have to ask whether that pane is actually open
// rather than checking a chat-side tab that no longer exists -- checking the
// old tab would silently stop a running terminal's output the moment
// anything else became the active workbench tab.
function terminalPaneOpen() {
  return state.view === "code" && state.panes.includes("terminal");
}

function scheduleWorkbenchPoll(callback, delay = 700) {
  clearTimeout(workbenchPollTimer);
  if (terminalPaneOpen() || state.workbenchTab === "team") {
    workbenchPollTimer = setTimeout(callback, delay);
  }
}

function renderWorkbenchReview() {
  if (state.selectedSkillDetail?.skill) {
    inspectSkill(state.selectedSkillDetail.skill.id);
    return;
  }
  const queued = state.reviews.filter(item => ["queued","running"].includes(item.state));
  const session = state.sessionDetail?.session;
  const contract = session?.contract || {};
  const selectedSkills = contract.selected_skills || [];
  const pendingApprovals = (state.sessionDetail?.approvals || []).filter(item => item.state === "pending");
  const sessionPanel = session ? `<div class="panel session-contract-panel"><div class="provider-head"><div><p class="eyebrow">Current Session Contract</p><h3>${escapeHTML(session.title)}</h3></div>${pill(session.state, session.state === "active" ? "green" : "amber")}</div><div class="kv"><span>Model</span><strong>${escapeHTML(session.model)}</strong><span>Context</span><strong>${escapeHTML(session.context_profile)}</strong><span>Project</span><strong>${escapeHTML(state.projects.find(item => item.id === session.project_id)?.name || "chat only")}</strong><span>Skills in context</span><strong>${selectedSkills.length}</strong><span>Direct tools</span><strong>${(contract.tool_bindings || []).length}</strong><span>Pending approvals</span><strong>${pendingApprovals.length}</strong><span>Contract</span><code>${escapeHTML(shortHash(session.contract_revision))}</code><span>Capability revision</span><code>${escapeHTML(shortHash(contract.capability_revision))}</code></div>${selectedSkills.length ? `<div class="meta session-skill-list">${selectedSkills.map(item => pill(item.canonical_name,"blue")).join("")}</div>` : `<p class="form-note neutral">No Skill body is injected yet; the session can still retrieve a frozen Skill with skill_search and skill_view.</p>`}<div class="action-row"><button class="primary" id="reviewOpenCapabilities">Skills & tools</button><button class="ghost" id="reviewOpenTools">Tool Center</button></div></div>` : `<div class="panel"><p class="eyebrow">Session review</p><h3>Start or select a session</h3><p class="dialog-message">Its immutable model, context envelope, Skill catalog, direct tools and approval state will appear here beside the conversation.</p></div>`;
  $("#workbenchContent").innerHTML = `${sessionPanel}<div class="panel"><p class="eyebrow">Authority & background work</p><h3>Evidence before authority</h3><p class="dialog-message">Skill candidates, write approvals, background reviews and command receipts stay inspectable here. Agents cannot widen authority through this room.</p><div class="kv"><span>Proposals</span><strong>${state.candidates.filter(item => ["needs_review","quarantined"].includes(item.state)).length}</strong><span>Review jobs</span><strong>${queued.length}</strong><span>Policy</span><strong>${escapeHTML(state.skillAuthority?.mode || "manual")}</strong></div><div class="action-row"><button class="primary" id="reviewOpenSkills">Open Skill Studio</button><button class="ghost" id="reviewRunNext" ${queued.length ? "" : "disabled"}>Run next review</button></div></div>
  <div class="card-list spaced">${state.jobs.slice(0,5).map(job => `<article class="artifact-mini"><div class="provider-head"><strong>${escapeHTML(job.payload?.executable || job.kind)}</strong>${pill(job.state,job.state === "completed" ? "green" : job.state === "failed" ? "red" : "amber")}</div><small>${formatDate(job.created_at)} · ${escapeHTML(job.result?.artifact_id || "receipt pending")}</small></article>`).join("") || `<div class="probe-empty">No recent execution receipts.</div>`}</div>`;
  $("#reviewOpenCapabilities")?.addEventListener("click", () => openCapabilityPicker("all"));
  $("#reviewOpenTools")?.addEventListener("click", () => switchTab("mcp"));
  $("#reviewOpenSkills")?.addEventListener("click", () => switchTab("library"));
  $("#reviewRunNext")?.addEventListener("click", runNextReview);
}

// renderWorkbenchFilesHTML returns markup rather than writing it, because
// this same markup now has two possible homes: the chat-side workbench room
// and a Code-view pane. A pane and a room rendering from two copies of this
// function would drift the moment one of them changed; returning a string
// keeps there being exactly one place the files room is actually built.
function renderWorkbenchFilesHTML() {
  const project = state.projects.find(item => item.id === state.selectedProject);
  const document = state.projectFile;
  return `<div class="panel"><div class="provider-head"><div><p class="eyebrow">Bounded file room</p><h3>${escapeHTML(project?.name || "Select a project")}</h3></div><button class="ghost" id="newWorkbenchFile" ${project ? "" : "disabled"}>New file</button></div>
    <label>Project<select id="workbenchProject">${state.projects.map(item => `<option value="${escapeHTML(item.id)}" ${item.id === state.selectedProject ? "selected" : ""}>${escapeHTML(item.name)}</option>`).join("")}</select></label>
    ${project ? `<div class="file-path"><code>${escapeHTML(state.projectPath || ".")}</code>${state.projectPath ? `<button class="ghost" id="workbenchFileUp">Up</button>` : ""}</div><div class="file-browser">${state.projectFiles.map(item => `<button class="file-row" data-workbench-file="${escapeHTML(item.path)}" data-directory="${item.directory}"><span>${item.directory ? "◇" : "·"}</span><strong>${escapeHTML(item.name)}</strong><small>${item.directory ? "folder" : `${Number(item.bytes).toLocaleString()} B`}</small></button>`).join("") || `<div class="probe-empty">Directory is empty.</div>`}</div>` : `<div class="probe-empty">Register a project from Control Center → Projects.</div>`}
    ${document ? `<form class="file-editor" id="workbenchFileForm"><div class="provider-head"><div><strong>${escapeHTML(document.path)}</strong><p>SHA ${escapeHTML(shortHash(document.sha256))} · optimistic save</p></div>${pill(document.mode || "new","blue")}</div><textarea id="workbenchFileContent" spellcheck="false">${escapeHTML(document.content)}</textarea><div class="action-row"><button class="primary">Save exact revision</button><button class="ghost" type="button" id="closeFileEditor">Close</button></div></form>${state.projectFileDiff ? `<section class="inspect-section"><h3>Committed diff</h3><pre class="diff-view">${escapeHTML(state.projectFileDiff)}</pre></section>` : ""}` : ""}
  </div>`;
}

// The listeners below used to be attached at the bottom of the function that
// wrote the markup into the DOM. Now that the HTML can land in either of two
// hosts (#workbenchContent or a pane's .pane-body), binding has to happen
// after whichever host actually received it -- there is nothing on the page
// to attach to before that -- so it is a separate step both callers run
// right after they set innerHTML.
function bindWorkbenchFilesEvents() {
  $("#workbenchProject")?.addEventListener("change", event => selectProject(event.target.value, ""));
  $("#workbenchFileUp")?.addEventListener("click", () => selectProject(state.selectedProject, (state.projectPath || "").split("/").slice(0,-1).join("/")));
  $$('[data-workbench-file]').forEach(button => button.addEventListener("click", () => button.dataset.directory === "true" ? selectProject(state.selectedProject, button.dataset.workbenchFile) : openWorkbenchFile(button.dataset.workbenchFile)));
  $("#workbenchFileForm")?.addEventListener("submit", saveWorkbenchFile);
  $("#closeFileEditor")?.addEventListener("click", () => { state.projectFile=null; state.projectFileDiff=""; refreshWorkbenchSurface("files"); });
  $("#newWorkbenchFile")?.addEventListener("click", newWorkbenchFile);
}

// The chat-side workbench room's own entry point: still exactly what callers
// elsewhere in this file expect a "render the files room" call to do.
function renderWorkbenchFiles() {
  $("#workbenchContent").innerHTML = renderWorkbenchFilesHTML();
  bindWorkbenchFilesEvents();
}

async function openWorkbenchFile(path) {
  try {
    state.projectFile = await api(`/api/projects/${encodeURIComponent(state.selectedProject)}/file?path=${encodeURIComponent(path)}`);
    state.projectFileDiff = "";
    refreshWorkbenchSurface("files");
  } catch (error) { toast(error.message, true); }
}

async function newWorkbenchFile() {
  const path = await askAction({title:"Create a project file",message:"Enter a path relative to the bounded project root. The server rejects symlinks and traversal.",confirmLabel:"Open editor",reasonLabel:"Relative path"});
  if (!path) return;
  state.projectFile = {path:path.trim(),content:"",sha256:"",mode:"0644",bytes:0};
  state.projectFileDiff = "";
  refreshWorkbenchSurface("files");
}

async function saveWorkbenchFile(event) {
  event.preventDefault();
  try {
    const result = await api(`/api/projects/${encodeURIComponent(state.selectedProject)}/file`, {method:"PUT",body:JSON.stringify({path:state.projectFile.path,content:$("#workbenchFileContent").value,expected_sha256:state.projectFile.sha256 || "",actor:"local-user"})});
    state.projectFile = result.document;
    state.projectFileDiff = result.diff;
    toast(`File committed · receipt ${shortHash(result.receipt_artifact.id)}`);
    state.artifacts = await api("/api/artifacts");
    refreshWorkbenchSurface("files");
  } catch (error) { toast(error.message, true); }
}

function stripANSI(value="") { return String(value).replace(/\x1B(?:[@-_][0-?]*[ -\/]*[@-~]|\][^\x07]*(?:\x07|\x1B\\))/g, ""); }

function renderWorkbenchTerminalHTML() {
  const terminal = state.terminals.find(item => item.id === state.selectedTerminal);
  return `<div class="panel"><div class="provider-head"><div><p class="eyebrow">Real PTY room</p><h3>${terminal ? escapeHTML(`${terminal.shell} · ${terminal.working_dir}`) : "Start terminal"}</h3></div>${terminal ? pill(terminal.state,terminal.state === "running" ? "green" : "amber") : ""}</div>
    <form id="terminalStartForm"><div class="form-grid"><label>Project<select name="project_id">${state.projects.map(item => `<option value="${escapeHTML(item.id)}" ${item.id === state.selectedProject ? "selected" : ""}>${escapeHTML(item.name)}</option>`).join("")}</select></label><label>Shell<select name="shell"><option>zsh</option><option>bash</option><option>sh</option></select></label></div><label>Working directory<input name="working_dir" value="${escapeHTML(state.projectPath || ".")}"></label><div class="form-grid"><label>Columns<input name="columns" type="number" min="20" max="400" value="100"></label><label>Rows<input name="rows" type="number" min="5" max="200" value="30"></label></div><button class="primary">New PTY tab</button></form>
    <div class="meta">${state.terminals.map(item => `<button class="ghost" data-terminal-id="${escapeHTML(item.id)}">${escapeHTML(item.shell)} · ${escapeHTML(item.state)}</button>`).join("")}</div>
    ${terminal ? `<pre class="terminal-screen" id="terminalScreen">${escapeHTML(stripANSI(state.terminalOutput || "waiting for output…"))}</pre>${terminal.state === "running" ? `<form class="terminal-command" id="terminalInputForm"><input name="input" autocomplete="off" placeholder="Command or interactive input"><button class="primary">Send ↵</button></form><form class="terminal-resize" id="terminalResizeForm"><input name="columns" type="number" min="20" max="400" value="100" aria-label="Terminal columns"><span>×</span><input name="rows" type="number" min="5" max="200" value="30" aria-label="Terminal rows"><button class="ghost">Resize PTY</button></form><div class="action-row"><button class="ghost" id="terminalInterrupt">Ctrl-C</button><button class="danger" id="terminalClose">Close PTY</button></div>` : `<p class="form-note neutral">Exit ${terminal.exit_code ?? "—"} · ${escapeHTML(terminal.error || "terminal is no longer live")}</p>`}` : `<div class="probe-empty">PTY output is streamed from a real shell process and bounded to a 1 MiB tail.</div>`}
  </div>`;
}

function bindWorkbenchTerminalEvents() {
  const terminal = state.terminals.find(item => item.id === state.selectedTerminal);
  $("#terminalStartForm")?.addEventListener("submit", startWorkbenchTerminal);
  $$('[data-terminal-id]').forEach(button => button.addEventListener("click", () => { state.selectedTerminal=button.dataset.terminalId; state.terminalOutput=""; refreshWorkbenchSurface("terminal"); }));
  $("#terminalInputForm")?.addEventListener("submit", sendTerminalInput);
  $("#terminalResizeForm")?.addEventListener("submit", resizeWorkbenchTerminal);
  $("#terminalInterrupt")?.addEventListener("click", () => sendRawTerminalInput("\x03"));
  $("#terminalClose")?.addEventListener("click", closeWorkbenchTerminal);
  // Polling has to (re)start from wherever the markup just landed, which is
  // exactly why this lives in the bind step rather than back in the HTML
  // builder: it needs the terminal to already exist in the DOM to write into.
  if (terminal) pollTerminal();
}

function renderWorkbenchTerminal() {
  $("#workbenchContent").innerHTML = renderWorkbenchTerminalHTML();
  bindWorkbenchTerminalEvents();
}

async function startWorkbenchTerminal(event) {
  event.preventDefault(); const form=new FormData(event.currentTarget);
  try {
    const terminal=await api("/api/terminals",{method:"POST",body:JSON.stringify({project_id:form.get("project_id"),shell:form.get("shell"),working_dir:form.get("working_dir"),actor:"local-user",columns:Number(form.get("columns")),rows:Number(form.get("rows"))})});
    state.selectedProject=form.get("project_id"); state.selectedTerminal=terminal.id; state.terminalOutput=""; state.terminals=await api("/api/terminals"); refreshWorkbenchSurface("terminal");
  } catch(error){toast(error.message,true);}
}

async function pollTerminal() {
  const id=state.selectedTerminal;
  if (!id || !terminalPaneOpen()) return;
  try {
    const output=await api(`/api/terminals/${encodeURIComponent(id)}/output?cursor=0`);
    state.terminalOutput=output.output;
    const terminal=state.terminals.find(item => item.id===id);
    if (terminal) Object.assign(terminal,{state:output.state,exit_code:output.exit_code,error:output.error,cursor:output.cursor});
    const screen=$("#terminalScreen"); if(screen){screen.textContent=stripANSI(state.terminalOutput);screen.scrollTop=screen.scrollHeight;}
    scheduleWorkbenchPoll(pollTerminal,500);
  } catch(error){toast(error.message,true);}
}

async function sendRawTerminalInput(input) { try { await api(`/api/terminals/${encodeURIComponent(state.selectedTerminal)}/input`,{method:"POST",body:JSON.stringify({input})}); scheduleWorkbenchPoll(pollTerminal,80); } catch(error){toast(error.message,true);} }
async function sendTerminalInput(event) { event.preventDefault(); const input=new FormData(event.currentTarget).get("input"); if(!input)return; event.currentTarget.reset(); await sendRawTerminalInput(input+"\n"); }
async function resizeWorkbenchTerminal(event) { event.preventDefault(); const form=new FormData(event.currentTarget); try { await api(`/api/terminals/${encodeURIComponent(state.selectedTerminal)}/resize`,{method:"POST",body:JSON.stringify({columns:Number(form.get("columns")),rows:Number(form.get("rows"))})}); toast(`PTY resized to ${form.get("columns")} × ${form.get("rows")}`); } catch(error){toast(error.message,true);} }
async function closeWorkbenchTerminal() { try { await api(`/api/terminals/${encodeURIComponent(state.selectedTerminal)}/close`,{method:"POST",body:"{}"}); state.terminals=await api("/api/terminals"); refreshWorkbenchSurface("terminal"); } catch(error){toast(error.message,true);} }

function renderWorkbenchBrowserHTML() {
  const tab=state.browserTabs.find(item => item.id===state.selectedBrowserTab);
  return `<div class="panel"><div class="provider-head"><div><p class="eyebrow">Managed browser · untrusted web</p><h3>${escapeHTML(tab?.title || "Open a browser tab")}</h3></div>${tab ? pill(tab.state,tab.state==="ready"?"green":"amber") : ""}</div>
    <form id="browserOpenForm"><label>Address<input name="url" type="url" value="${escapeHTML(tab?.url || "https://")}" required></label><label>Bound project<select name="project_id"><option value="">None</option>${state.projects.map(item=>`<option value="${escapeHTML(item.id)}" ${item.id===state.selectedProject?"selected":""}>${escapeHTML(item.name)}</option>`).join("")}</select></label><label class="check-label"><input name="allow_private" type="checkbox" ${tab?.allow_private?"checked":""}> Allow local/private addresses for this tab</label><div class="action-row"><button class="primary">Open isolated tab</button>${tab?`<button class="ghost" type="button" data-browser-action="navigate">Navigate current tab</button><button class="ghost" type="button" data-browser-action="back">Back</button><button class="ghost" type="button" data-browser-action="capture">Capture</button><button class="danger" type="button" data-browser-action="close">Close</button>`:""}</div></form>
    <div class="meta">${state.browserTabs.map(item=>`<button class="ghost" data-browser-id="${escapeHTML(item.id)}">${escapeHTML(item.title||item.url)} · ${escapeHTML(item.state)}</button>`).join("")}</div>
    ${tab?`${tab.screenshot_artifact_id?`<img class="browser-shot" src="/api/artifacts/${encodeURIComponent(tab.screenshot_artifact_id)}/content" alt="Managed browser screenshot">`:""}<section class="inspect-section"><h3>Readable snapshot · untrusted content</h3><pre>${escapeHTML((tab.text_snapshot||"").slice(0,8000))}</pre></section><div class="browser-elements">${(tab.elements||[]).map(element=>`<article class="browser-element"><strong>${element.ref}</strong><span><strong>${escapeHTML(element.text||element.placeholder||element.tag)}</strong><small>${escapeHTML(element.tag)} ${escapeHTML(element.role||"")}</small></span><span><button class="ghost" data-browser-click="${element.ref}">Click</button>${["input","textarea"].includes(element.tag)?`<button class="ghost" data-browser-type="${element.ref}">Type</button>`:""}</span></article>`).join("")}</div>`:`<div class="probe-empty">This room drives Chrome through DevTools; it is not an iframe. Private URLs require an explicit per-tab opt-in.</div>`}
  </div>`;
}

function bindWorkbenchBrowserEvents() {
  $("#browserOpenForm")?.addEventListener("submit",openWorkbenchBrowser);
  $$('[data-browser-id]').forEach(button=>button.addEventListener("click",()=>{state.selectedBrowserTab=button.dataset.browserId;refreshWorkbenchSurface("browser");}));
  $$('[data-browser-action]').forEach(button=>button.addEventListener("click",()=>browserWorkbenchAction(button.dataset.browserAction)));
  $$('[data-browser-click]').forEach(button=>button.addEventListener("click",()=>browserWorkbenchAction("click",Number(button.dataset.browserClick))));
  $$('[data-browser-type]').forEach(button=>button.addEventListener("click",()=>typeBrowserElement(Number(button.dataset.browserType))));
}

function renderWorkbenchBrowser() {
  $("#workbenchContent").innerHTML = renderWorkbenchBrowserHTML();
  bindWorkbenchBrowserEvents();
}

async function openWorkbenchBrowser(event){event.preventDefault();const form=new FormData(event.currentTarget);try{const tab=await api("/api/browser/tabs",{method:"POST",body:JSON.stringify({project_id:form.get("project_id"),url:form.get("url"),allow_private:form.get("allow_private")==="on",actor:"local-user"})});state.browserTabs.unshift(tab);state.selectedBrowserTab=tab.id;refreshWorkbenchSurface("browser");}catch(error){toast(error.message,true);}}
async function browserWorkbenchAction(action,ref=0,text=""){const tab=state.browserTabs.find(item=>item.id===state.selectedBrowserTab);if(!tab)return;const url=action==="navigate"?new FormData($("#browserOpenForm")).get("url"):"";try{const updated=await api(`/api/browser/tabs/${encodeURIComponent(tab.id)}/actions`,{method:"POST",body:JSON.stringify({action,url,ref,text,actor:"local-user"})});state.browserTabs=state.browserTabs.map(item=>item.id===updated.id?updated:item);state.selectedBrowserTab=updated.id;refreshWorkbenchSurface("browser");}catch(error){toast(error.message,true);}}
async function typeBrowserElement(ref){const text=await askAction({title:`Type into browser element ${ref}`,message:"The value is sent only to this exact element reference on the active managed tab.",confirmLabel:"Type value",reasonLabel:"Text"});if(text===null)return;await browserWorkbenchAction("type",ref,text);}

// Files, terminal and browser can each be showing in one of two places (a
// chat-side workbench room, or a Code pane) or neither. A callback bound
// inside their markup has no way to know which one raised it -- it is a
// listener on a DOM node, not a closure over the render call that made that
// node -- so it asks state instead of assuming a fixed target, and does
// nothing if the content in question is not actually on screen anywhere.
function refreshWorkbenchSurface(id) {
  if (state.view === "code" && state.panes.includes(id)) { renderPanes(); return; }
  if (state.view === "chat" && state.workbenchTab === id) {
    if (id === "files") renderWorkbenchFiles();
    if (id === "terminal") renderWorkbenchTerminal();
    if (id === "browser") renderWorkbenchBrowser();
  }
}

// Terminal and browser now live only as pane content in Code. Anything that
// used to jump straight to their old side-strip room -- the composer's quick
// button, the command palette -- opens or reveals a pane instead, so there
// is exactly one door into either room rather than two.
function openContentPane(id) {
  if (!state.panes.length) state.panes = ["files"];
  if (!state.panes.includes(id)) {
    if (state.panes.length < MAX_PANES) state.panes.push(id);
    else state.panes[state.panes.length - 1] = id;
  }
  state.maximisedPane = state.panes.indexOf(id);
  if (state.view === "code") renderPanes();
  else switchView("code");
}

// The Output pane reads the same state.jobs the Review room's receipt list
// already draws from -- a second store here would be a second place that
// list could disagree with itself.
function renderPaneOutputHTML() {
  const jobs = state.jobs.slice(0, 12);
  return `<div class="panel"><p class="eyebrow">Background jobs</p><h3>Recent runs for this project</h3>
    <div class="card-list spaced">${jobs.map(job => `<article class="artifact-mini"><div class="provider-head"><strong>${escapeHTML(job.payload?.executable || job.kind)}</strong>${pill(job.state, job.state === "completed" ? "green" : job.state === "failed" ? "red" : "amber")}</div><small>${formatDate(job.started_at || job.created_at)} · ${escapeHTML(job.result?.artifact_id || "receipt pending")}</small></article>`).join("") || `<div class="probe-empty">No background jobs yet.</div>`}</div>
  </div>`;
}

// Four is the ceiling because a fifth pane on one screen is smaller than the
// thing inside it, and because a bounded number is a number that can be
// tested. This is a split, not a tiling manager.
const MAX_PANES = 4;

// Content is not tied to a position: any pane may show any of these.
// Terminal and browser are here rather than in the side strip because both
// need room, which is the whole reason the panes exist.
const PANE_CONTENT = [
  { id: "files", label: "Files", render: () => renderWorkbenchFilesHTML() },
  { id: "terminal", label: "Terminal", render: () => renderWorkbenchTerminalHTML() },
  { id: "browser", label: "Browser", render: () => renderWorkbenchBrowserHTML() },
  { id: "output", label: "Output", render: () => renderPaneOutputHTML() }
];

function paneContent(id) {
  return PANE_CONTENT.find(item => item.id === id) || PANE_CONTENT[0];
}

function renderPanes() {
  const host = $("#zoneMain");
  if (!state.panes.length) state.panes = ["files"];
  const columns = state.panes.length > 1 ? 2 : 1;
  const rows = state.panes.length > 2 ? 2 : 1;
  document.documentElement.style.setProperty("--pane-columns", String(columns));
  document.documentElement.style.setProperty("--pane-rows", String(rows));
  host.innerHTML = `<div class="pane-grid ${state.maximisedPane === null ? "" : "one-up"}">${
    state.panes.map((id, index) => {
      const hidden = state.maximisedPane !== null && state.maximisedPane !== index;
      const item = paneContent(id);
      return `<section class="pane ${hidden ? "pane-hidden" : ""}" data-pane="${index}">
        <header class="pane-head">
          <select data-pane-content="${index}" aria-label="Pane content">${
            PANE_CONTENT.map(option =>
              `<option value="${option.id}" ${option.id === id ? "selected" : ""}>${escapeHTML(option.label)}</option>`).join("")
          }</select>
          <button class="ghost compact" data-pane-max="${index}" aria-label="Maximise this pane">${
            state.maximisedPane === index ? "▪" : "▫"}</button>
          ${state.panes.length > 1
            ? `<button class="ghost compact" data-pane-close="${index}" aria-label="Close this pane">×</button>` : ""}
        </header>
        <div class="pane-body">${item.render()}</div>
      </section>`;
    }).join("")
  }</div>
  ${state.panes.length < MAX_PANES ? `<button class="ghost compact pane-add" id="paneAdd">＋ แบ่งช่อง</button>` : ""}`;
  bindPaneControls();
}

function splitPane() {
  if (state.panes.length >= MAX_PANES) return;
  // A new pane starts on something other than what is already open, because
  // splitting to see the same thing twice is not why anyone splits.
  const unused = PANE_CONTENT.find(item => !state.panes.includes(item.id));
  state.panes.push((unused || PANE_CONTENT[0]).id);
  state.maximisedPane = null;
  renderPanes();
  saveLayout();
}

function closePane(index) {
  if (state.panes.length <= 1) return;
  state.panes.splice(index, 1);
  state.maximisedPane = null;
  renderPanes();
  saveLayout();
}

function setPaneContent(index, id) {
  state.panes[index] = paneContent(id).id;
  renderPanes();
  saveLayout();
}

function maximisePane(index) {
  state.maximisedPane = state.maximisedPane === index ? null : index;
  renderPanes();
  saveLayout();
}

function bindPaneControls() {
  $("#paneAdd")?.addEventListener("click", splitPane);
  $$("[data-pane-content]").forEach(select =>
    select.addEventListener("change", event => setPaneContent(Number(select.dataset.paneContent), event.target.value)));
  $$("[data-pane-max]").forEach(button =>
    button.addEventListener("click", () => maximisePane(Number(button.dataset.paneMax))));
  $$("[data-pane-close]").forEach(button =>
    button.addEventListener("click", () => closePane(Number(button.dataset.paneClose))));
  // Each *HTML() renderer above returns a string before renderPanes() ever
  // inserts it into the document, so any listener it used to attach to
  // itself would have had nothing to attach to. Binding happens here
  // instead, once per pane, keyed off what that pane is actually showing --
  // the same bind functions the workbench room calls, so a pane and a room
  // never end up with two different behaviours for one control.
  state.panes.forEach(id => {
    if (id === "files") bindWorkbenchFilesEvents();
    if (id === "terminal") bindWorkbenchTerminalEvents();
    if (id === "browser") bindWorkbenchBrowserEvents();
  });
}

function renderWorkbenchArtifacts(){
  $("#workbenchContent").innerHTML=`<div class="panel"><p class="eyebrow">Office deliverables</p><h3>Create a real editable file</h3><form id="deliverableForm"><label>Project<select name="project_id"><option value="">Global</option>${state.projects.map(item=>`<option value="${escapeHTML(item.id)}" ${item.id===state.selectedProject?"selected":""}>${escapeHTML(item.name)}</option>`).join("")}</select></label><div class="form-grid"><label>Format<select name="format"><option>docx</option><option>xlsx</option><option>pptx</option><option>pdf</option></select></label><label>Title<input name="title" required value="Hermetrix report"></label></div><label>Content<textarea name="content" rows="8" required placeholder="Paragraphs; use tab-separated rows for XLSX or --- between PPTX slides"></textarea></label><p class="form-note neutral">DOCX/XLSX/PPTX support Unicode. Native PDF currently fails closed for non-Basic-Latin text instead of generating missing glyphs.</p><button class="primary">Build immutable deliverable</button></form></div><section class="office-preview" id="deliverablePreview"></section><div class="card-list spaced">${state.artifacts.slice(0,30).map(item=>`<article class="artifact-mini"><div class="provider-head"><div><strong>${escapeHTML(item.name)}</strong><p>${escapeHTML(item.mime_type)} · ${Number(item.byte_size).toLocaleString()} B</p></div>${pill(item.kind,"blue")}</div><div class="action-row"><a class="button-link" href="/api/artifacts/${encodeURIComponent(item.id)}/content" target="_blank" rel="noreferrer">Open / download</a></div></article>`).join("")||`<div class="probe-empty">No artifacts yet.</div>`}</div>`;
  $("#deliverableForm")?.addEventListener("submit",createDeliverable);
  $("#deliverableForm")?.addEventListener("input",renderDeliverableDraftPreview);
  renderDeliverableDraftPreview();
}

function renderDeliverableDraftPreview(){
  const form=$("#deliverableForm"),root=$("#deliverablePreview");if(!form||!root)return;const data=new FormData(form),format=data.get("format"),title=String(data.get("title")||"Untitled"),content=String(data.get("content")||"");
  if(format==="xlsx"){const rows=content.split("\n").filter(Boolean).slice(0,20).map(row=>`<tr>${row.split("\t").slice(0,10).map(cell=>`<td>${escapeHTML(cell)}</td>`).join("")}</tr>`).join("");root.innerHTML=`<p class="eyebrow">Structured preview · first 20 rows</p><div class="sheet-preview"><table>${rows||`<tr><td>Tab-separated cells appear here</td></tr>`}</table></div>`;return;}
  if(format==="pptx"){const slides=content.split(/\n---\n/).slice(0,8);root.innerHTML=`<p class="eyebrow">Structured preview · first 8 slides</p><div class="slide-preview-list">${slides.map((block,index)=>{const lines=block.split("\n").filter(Boolean);return`<article class="slide-preview"><small>${index+1}</small><h4>${escapeHTML(lines.shift()||title)}</h4><ul>${lines.map(line=>`<li>${escapeHTML(line)}</li>`).join("")}</ul></article>`;}).join("")}</div>`;return;}
  root.innerHTML=`<p class="eyebrow">Structured preview · ${escapeHTML(format.toUpperCase())}</p><article class="page-preview"><h4>${escapeHTML(title)}</h4>${content.split(/\n+/).filter(Boolean).slice(0,30).map(paragraph=>`<p>${escapeHTML(paragraph)}</p>`).join("")||`<p class="preview-placeholder">Document paragraphs appear here</p>`}</article>`;
}

async function createDeliverable(event){event.preventDefault();const form=new FormData(event.currentTarget);const format=form.get("format");const content=String(form.get("content")||"");const body={project_id:form.get("project_id"),format,title:form.get("title"),actor:"local-user",paragraphs:content.split(/\n+/).filter(Boolean)};if(format==="xlsx")body.rows=content.split("\n").map(row=>row.split("\t"));if(format==="pptx")body.slides=content.split(/\n---\n/).map(block=>{const lines=block.split("\n").filter(Boolean);return{title:lines.shift()||form.get("title"),bullets:lines};});try{const artifact=await api("/api/deliverables",{method:"POST",body:JSON.stringify(body)});state.artifacts=await api("/api/artifacts");toast(`Created ${artifact.name} · ${shortHash(artifact.checksum)}`);renderWorkbenchArtifacts();}catch(error){toast(error.message,true);}}

function newTeamDraft(team=null){
  return team ? {sourceID:team.id,id:team.id,expected_revision:team.revision,name:team.name,instructions:team.instructions,members:team.members.map(member=>({...member}))} :
    {sourceID:"new",id:"",expected_revision:0,name:"Evidence Team",instructions:"Verify evidence, surface disagreement, and never widen another agent's authority.",members:[
      {name:"Researcher",role:"research",instructions:"Collect primary evidence and note uncertainty.",is_lead:false},
      {name:"Reviewer",role:"review",instructions:"Challenge unsupported claims and identify risk.",is_lead:false},
      {name:"Lead",role:"synthesis",instructions:"Synthesize the final answer from labelled peer evidence.",is_lead:true}
    ]};
}

function captureTeamDraft(){
  const form=$("#teamCreateForm"); if(!form)return;
  const data=new FormData(form); const lead=Number(data.get("lead_index"));
  state.teamDraft={...(state.teamDraft||newTeamDraft()),name:data.get("name"),instructions:data.get("instructions"),members:$$('[data-team-member]',form).map((row,index)=>({id:row.dataset.memberId||"",name:data.get(`member_name_${index}`),role:data.get(`member_role_${index}`),instructions:data.get(`member_instructions_${index}`),is_lead:index===lead}))};
}

function renderTeamMemberRows(draft){
  return draft.members.map((member,index)=>`<article class="team-member-editor" data-team-member data-member-id="${escapeHTML(member.id||"")}"><div class="form-grid"><label>Name<input name="member_name_${index}" required value="${escapeHTML(member.name||"")}"></label><label>Role<input name="member_role_${index}" required value="${escapeHTML(member.role||"")}"></label></div><label>Instructions<textarea name="member_instructions_${index}" rows="2" required>${escapeHTML(member.instructions||"")}</textarea></label><div class="action-row"><label class="check-label"><input type="radio" name="lead_index" value="${index}" ${member.is_lead?"checked":""} required> Team lead</label><button type="button" class="danger" data-remove-team-member="${index}" ${draft.members.length===1?"disabled":""}>Remove</button></div></article>`).join("");
}

function teamTaskRowHTML(team,index){
  const id=`task-${Date.now().toString(36)}-${index+1}`;
  return `<article class="team-task-editor" data-team-task><div class="form-grid"><label>Task ID<input data-task-field="id" required value="${id}"></label><label>Member<select data-task-field="member_id" required>${team.members.map(member=>`<option value="${escapeHTML(member.id)}">${escapeHTML(member.name)} · ${escapeHTML(member.role)}</option>`).join("")}</select></label></div><label>Title<input data-task-field="title" required placeholder="Independent review"></label><label>Depends on task IDs<input data-task-field="depends" placeholder="task-a, task-b"></label><label>Task prompt<textarea data-task-field="prompt" rows="2" required></textarea></label><button type="button" class="danger" data-remove-team-task>Remove task</button></article>`;
}

function renderWorkbenchTeam(){
  const team=state.teams.find(item=>item.id===state.selectedTeam);
  if(!state.teamDraft || state.teamDraft.sourceID!==(team?.id||"new"))state.teamDraft=newTeamDraft(team||null);
  const draft=state.teamDraft;
  const provider=state.providers.find(item=>item.enabled);
  const profiles=availableProfiles(provider);
  const profile=bestProfileFor(provider,profiles);
  $("#workbenchContent").innerHTML=`<div class="panel"><div class="provider-head"><div><p class="eyebrow">Reusable roster · explicit authority</p><h3>${draft.id?"Edit":"Create"} Agent Team</h3></div>${draft.id?pill(`revision ${draft.expected_revision}`,"blue"):pill("new roster","green")}</div><label>Roster<select id="teamSelect"><option value="">＋ New team</option>${state.teams.map(item=>`<option value="${escapeHTML(item.id)}" ${item.id===state.selectedTeam?"selected":""}>${escapeHTML(item.name)}</option>`).join("")}</select></label><form id="teamCreateForm"><label>Team name<input name="name" required value="${escapeHTML(draft.name)}"></label><label>Unit rules<textarea name="instructions" rows="3" required>${escapeHTML(draft.instructions)}</textarea></label><section class="team-editor-list">${renderTeamMemberRows(draft)}</section><div class="action-row"><button type="button" class="ghost" id="addTeamMember" ${draft.members.length>=12?"disabled":""}>＋ Add member</button><button class="primary">${draft.id?"Save exact revision":"Save reusable team"}</button></div></form></div>
  <div class="panel spaced">${team?`<div class="meta">${team.members.map(member=>pill(`${member.is_lead?"lead · ":""}${member.name} / ${member.role}`,member.is_lead?"green":"blue")).join("")}</div><form id="teamRunForm"><label>Objective<textarea name="objective" rows="4" required placeholder="What should this team solve?"></textarea></label><div class="form-grid"><label>Provider<select name="provider_id" required>${state.providers.filter(item=>item.enabled).map(item=>`<option value="${escapeHTML(item.id)}" ${item.id===provider?.id?"selected":""}>${escapeHTML(item.name)} · ${escapeHTML(item.model)}</option>`).join("")}</select></label><label>Context<select name="context_profile" required>${profiles.map(item=>`<option value="${escapeHTML(item.name)}" ${item.name===profile?.name?"selected":""}>${escapeHTML(profileLabel(item))}</option>`).join("")}</select></label></div><label>Remote qualification reason<input name="qualification_reason" value="User-approved team run against the configured remote provider"></label><label>Parallel children<input name="max_parallel" type="number" min="1" max="4" value="3"></label><details class="team-graph"><summary>Custom task DAG · optional</summary><p class="form-note neutral">Leave empty for automatic specialist fan-out and lead synthesis. Dependencies refer to exact Task IDs.</p><div id="teamTaskRows"></div><button class="ghost" type="button" id="addTeamTask">＋ Add task</button></details><button class="primary">Start team run</button></form>`:`<div class="probe-empty">Save or select a team before starting a run.</div>`}</div>
  <div class="card-list spaced">${state.teamRuns.slice(0,20).map(run=>`<article class="provider-card"><div class="provider-head"><div><h3>${escapeHTML(state.teams.find(item=>item.id===run.team_id)?.name||run.team_name||"Team run")}</h3><p>${escapeHTML(run.objective)} · ${formatDate(run.created_at)}</p></div>${pill(run.state,run.state==="completed"?"green":["failed","cancelled"].includes(run.state)?"red":"amber")}</div><div class="kv"><span>Parallel</span><strong>${run.max_parallel}</strong><span>Tokens</span><strong>${Number((run.prompt_tokens||0)+(run.completion_tokens||0)).toLocaleString()}</strong></div>${["queued","running","awaiting_approval"].includes(run.state)?`<div class="action-row"><button class="danger" data-cancel-team-run="${escapeHTML(run.id)}">Cancel team and children</button></div>`:""}${(run.tasks||[]).map(task=>`<section class="team-task"><div class="provider-head"><div><strong>${escapeHTML(task.title)}</strong><small>${escapeHTML(task.member_name||"")} · ${escapeHTML(task.member_role||"")}</small></div>${pill(task.state,task.state==="completed"?"green":["failed","cancelled"].includes(task.state)?"red":"amber")}</div>${task.state==="awaiting_approval"?`<article class="team-approval"><strong>${escapeHTML(task.approval_summary||"Child requests an exact effect")}</strong><p>${escapeHTML(task.approval_effect||"effect")}</p><pre>${escapeHTML(task.approval_preview||"No preview supplied")}</pre><div class="action-row"><button class="primary" data-team-approval="approve" data-run-id="${escapeHTML(run.id)}" data-task-id="${escapeHTML(task.id)}">Approve exact effect</button><button class="danger" data-team-approval="deny" data-run-id="${escapeHTML(run.id)}" data-task-id="${escapeHTML(task.id)}">Deny</button></div></article>`:""}${task.result?`<p>${escapeHTML(task.result.slice(0,900))}</p>`:""}${task.error?`<p class="form-note">${escapeHTML(task.error)}</p>`:""}${task.session_id?`<button class="ghost" data-team-session="${escapeHTML(task.session_id)}">Open child session</button>`:""}</section>`).join("")}</article>`).join("")||`<div class="probe-empty">No team runs yet. Default runs create parallel specialist tasks and a dependent lead synthesis.</div>`}</div>`;
  $("#teamCreateForm")?.addEventListener("submit",saveWorkbenchTeam);
  $("#teamSelect")?.addEventListener("change",event=>{state.selectedTeam=event.target.value||null;state.teamDraft=null;renderWorkbenchTeam();});
  $("#addTeamMember")?.addEventListener("click",()=>{captureTeamDraft();state.teamDraft.members.push({name:"",role:"specialist",instructions:"",is_lead:false});renderWorkbenchTeam();});
  $$('[data-remove-team-member]').forEach(button=>button.addEventListener("click",()=>{captureTeamDraft();state.teamDraft.members.splice(Number(button.dataset.removeTeamMember),1);if(!state.teamDraft.members.some(member=>member.is_lead))state.teamDraft.members[0].is_lead=true;renderWorkbenchTeam();}));
  $("#teamRunForm")?.addEventListener("submit",startWorkbenchTeamRun);
  $("#addTeamTask")?.addEventListener("click",()=>{$("#teamTaskRows").insertAdjacentHTML("beforeend",teamTaskRowHTML(team,$$('[data-team-task]').length));bindTeamTaskRemovers();});
  bindTeamTaskRemovers();
  $$('[data-cancel-team-run]').forEach(button=>button.addEventListener("click",()=>cancelWorkbenchTeamRun(button.dataset.cancelTeamRun)));
  $$('[data-team-approval]').forEach(button=>button.addEventListener("click",()=>decideWorkbenchTeamApproval(button.dataset.runId,button.dataset.taskId,button.dataset.teamApproval)));
  $$('[data-team-session]').forEach(button=>button.addEventListener("click",async()=>{await selectSession(button.dataset.teamSession);switchTab("chat");}));
  if(state.teamRuns.some(run=>["queued","running"].includes(run.state)))scheduleWorkbenchPoll(pollTeamRuns,900);
}

function bindTeamTaskRemovers(){$$('[data-remove-team-task]').forEach(button=>button.onclick=()=>button.closest('[data-team-task]').remove());}

async function saveWorkbenchTeam(event){event.preventDefault();captureTeamDraft();const draft=state.teamDraft;try{const team=await api("/api/teams",{method:"POST",body:JSON.stringify({id:draft.id||"",expected_revision:draft.expected_revision||0,project_id:state.selectedProject||"",name:draft.name,instructions:draft.instructions,actor:"local-user",members:draft.members})});state.teams=await api("/api/teams");state.selectedTeam=team.id;state.teamDraft=newTeamDraft(team);toast("Reusable team saved with one explicit lead");renderWorkbenchTeam();}catch(error){toast(error.message,true);}}
async function startWorkbenchTeamRun(event){event.preventDefault();const form=new FormData(event.currentTarget);const tasks=$$('[data-team-task]',event.currentTarget).map(row=>({id:row.querySelector('[data-task-field="id"]').value.trim(),member_id:row.querySelector('[data-task-field="member_id"]').value,title:row.querySelector('[data-task-field="title"]').value.trim(),prompt:row.querySelector('[data-task-field="prompt"]').value.trim(),depends_on:row.querySelector('[data-task-field="depends"]').value.split(",").map(value=>value.trim()).filter(Boolean)}));try{const run=await api("/api/team-runs",{method:"POST",body:JSON.stringify({team_id:state.selectedTeam,project_id:state.selectedProject||"",objective:form.get("objective"),provider_id:form.get("provider_id"),context_profile:form.get("context_profile"),qualification_reason:form.get("qualification_reason"),max_parallel:Number(form.get("max_parallel")),actor:"local-user",tasks})});state.teamRuns.unshift(run);toast("Team run started; child sessions keep independent provenance");renderWorkbenchTeam();}catch(error){toast(error.message,true);}}
async function cancelWorkbenchTeamRun(id){const approved=await askAction({title:"Cancel this team run?",message:"Hermetrix will cancel every active child context and mark queued/running tasks cancelled. Completed child effects are not undone or retried.",confirmLabel:"Cancel team",danger:true});if(!approved)return;try{const run=await api(`/api/team-runs/${encodeURIComponent(id)}/cancel`,{method:"POST",body:JSON.stringify({actor:"local-user"})});state.teamRuns=state.teamRuns.map(item=>item.id===run.id?run:item);toast("Team and active child contexts cancelled");renderWorkbenchTeam();}catch(error){toast(error.message,true);}}
async function decideWorkbenchTeamApproval(runId,taskId,decision){const response=await askAction({title:decision==="approve"?"Approve this child effect once?":"Deny this child effect?",message:"The decision is bound to the exact child approval and arguments hash. The child resumes its existing turn; Hermetrix does not replay its prompt or earlier effects.",confirmLabel:decision==="approve"?"Approve exact effect":"Deny effect",reasonLabel:decision==="deny"?"Reason":"",danger:decision==="deny"});if(!response)return;try{const run=await api(`/api/team-runs/${encodeURIComponent(runId)}/tasks/${encodeURIComponent(taskId)}/approval`,{method:"POST",body:JSON.stringify({actor:"local-user",decision,reason:decision==="deny"?response:"approved after team preview"})});state.teamRuns=state.teamRuns.map(item=>item.id===run.id?run:item);toast(decision==="approve"?"Child effect approved; DAG resumes from its receipt":"Child effect denied; DAG resumes without mutation");renderWorkbenchTeam();}catch(error){toast(error.message,true);}}
async function pollTeamRuns(){if(state.workbenchTab!=="team")return;try{state.teamRuns=await api("/api/team-runs");if(document.activeElement?.closest("#teamCreateForm,#teamRunForm")){scheduleWorkbenchPoll(pollTeamRuns,900);return;}renderWorkbenchTeam();}catch(error){toast(error.message,true);}}

// CONFIG_SECTIONS is the settings room's navigation. Configuration used to sit
// in the workspace as a fourteen-entry tab strip beside a five-entry sidebar,
// which meant the screen asked "which of nineteen places?" before it asked
// anything about the work. Settings is now one room with its own navigation,
// its own search, and nothing from the session competing with it.
//
// `terms` is what the search box matches beyond the label: someone typing
// "api key" is looking for Models, and a search that only matches page titles
// is a search box that lies about what it can find.
const CONFIG_SECTIONS = [
  { group: "Models", items: [
    { id:"providers", icon:"⌁", label:"Models", blurb:"Endpoints, API keys and qualification",
      terms:"provider endpoint api key token openai compatible qualification context window local runtime ollama" }
  ]},
  { group: "Tools", items: [
    { id:"mcp", icon:"⌘", label:"Tool Center", blurb:"MCP connections and the capability graph",
      terms:"mcp server bearer token streamable http discovery capability tool schema approval" }
  ]},
  { group: "Skills", items: [
    { id:"library", icon:"✦", label:"Skill Studio", blurb:"Active Skills and authority policy",
      terms:"skill library active authority policy promote fork scope pinned" },
    { id:"proposals", icon:"◔", label:"Proposals", blurb:"Candidates waiting for a decision",
      terms:"candidate proposal review promote reject quarantine", badge:"proposals" },
    { id:"learning", icon:"◵", label:"Learning", blurb:"Background reviews of real turns",
      terms:"learning review queue reviewer evidence", badge:"reviews" },
    { id:"insights", icon:"◇", label:"Insights", blurb:"Curator findings, report only",
      terms:"curator finding stale duplicate consolidation relation" },
    { id:"archive", icon:"▤", label:"Archive", blurb:"Restore archived Skills as candidates",
      terms:"archive restore deleted reversible" }
  ]},
  { group: "Context", items: [
    { id:"context", icon:"◫", label:"Context", blurb:"Compile a prompt and read the ledger",
      terms:"context profile budget fragment compile ledger spill token estimate 32k 64k 128k 256k 1m" },
    { id:"fidelity", icon:"◎", label:"Fidelity", blurb:"Evidence behind qualified capacity",
      terms:"fidelity recall evidence corpus case run positional" }
  ]},
  { group: "System", items: [
    { id:"projects", icon:"▦", label:"Projects", blurb:"Bounded workspaces and commands",
      terms:"project workspace root command allowlist file tree" },
    { id:"office", icon:"◷", label:"Background jobs", blurb:"Long-running work and its receipts",
      terms:"job background queue cancel receipt" },
    { id:"artifacts", icon:"◧", label:"Artifacts", blurb:"Content-addressed outputs",
      terms:"artifact cas checksum deliverable docx xlsx pptx pdf" },
    { id:"maintenance", icon:"⚙", label:"Maintenance", blurb:"Usage, memory, backup and recovery",
      terms:"usage memory backup import export schedule garbage collection quarantine restore setting" }
  ]}
];

const CONFIG_PAGE_IDS = CONFIG_SECTIONS.flatMap(section => section.items.map(item => item.id));
const SKILL_PAGES = ["library", "proposals", "learning", "insights", "archive"];

function configItem(id) {
  for (const section of CONFIG_SECTIONS) {
    const found = section.items.find(item => item.id === id);
    if (found) return found;
  }
  return null;
}

// renderConfigNav draws only the sections the search matches. An empty group is
// not drawn at all: a heading with nothing under it reads as a place you can go.
function renderConfigNav() {
  const nav = $("#configNav");
  if (!nav) return;
  const query = ($("#configSearch")?.value || "").trim().toLowerCase();
  const counts = { proposals: state.pendingProposals || 0, reviews: state.pendingReviews || 0 };
  const navItemHTML = item => {
    const badge = item.badge ? counts[item.badge] || 0 : 0;
    const active = item.id === state.activeTab ? "active" : "";
    return `<button type="button" class="config-nav-item ${active}" data-config-page="${escapeHTML(item.id)}"><span>${escapeHTML(item.icon)}</span><span><strong>${escapeHTML(item.label)}</strong><small>${escapeHTML(item.blurb)}</small></span>${badge ? `<b>${badge}</b>` : ""}</button>`;
  };
  const groups = CONFIG_SECTIONS
    .map(section => ({ group: section.group, items: section.items.filter(item =>
      !query || `${item.label} ${item.blurb} ${item.terms}`.toLowerCase().includes(query)) }))
    .filter(section => section.items.length);
  nav.innerHTML = groups.length
    ? groups.map(section => `<p class="config-nav-group">${escapeHTML(section.group)}</p>${section.items.map(navItemHTML).join("")}`).join("")
    : `<p class="command-empty">Nothing in settings matches “${escapeHTML(query)}”.</p>`;
  $$("[data-config-page]", nav).forEach(button =>
    button.addEventListener("click", () => switchTab(button.dataset.configPage)));
}

function openConfig(page = "") {
  const target = CONFIG_PAGE_IDS.includes(page) ? page
    : CONFIG_PAGE_IDS.includes(state.activeTab) ? state.activeTab
    : "providers";
  switchTab(target);
}

function closeConfig() { switchTab("chat"); }

// switchTab is the single place that decides whether you are looking at the
// work or at the settings that shape it.
function switchTab(tab) {
  const isConfig = CONFIG_PAGE_IDS.includes(tab);
  state.activeTab = isConfig ? tab : "chat";
  const overlay = $("#configOverlay");
  overlay.hidden = !isConfig;
  document.documentElement.classList.toggle("config-open", isConfig);
  // The settings room is a fixed overlay that covers the header completely;
  // a screen reader does not get that for free, so it is told directly that
  // the header underneath is not part of the page while settings is open.
  $("#appHeader").setAttribute("aria-hidden", String(isConfig));
  $$(".view").forEach(node => node.classList.toggle("active", node.id === `view-${state.activeTab}`));
  if (!isConfig) return;
  const item = configItem(tab);
  $("#configTitle").textContent = item.label;
  const onSkillPage = SKILL_PAGES.includes(tab);
  $("#stats").hidden = !onSkillPage;
  $("#libraryToolbar").hidden = tab !== "library";
  $("#configPane").scrollTop = 0;
  renderConfigNav();
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


/* ---------------------------------------------------------------------------
   Command palette, capability picker and density.

   index.html has carried the markup and style.css the styling for all three
   since the usability pass, but none of them had any behaviour: every entry
   point -- the topbar button, Cmd-K, the composer's "Skills & tools", the
   session capability chips and typing "@" -- called into nothing, and the
   capability entry points threw ReferenceError because openCapabilityPicker
   was never defined. This section is that behaviour.
--------------------------------------------------------------------------- */

// Every page reachable from the palette, in the order a person looks for them
// rather than the order the views happen to appear in the document.
// Terminal and browser are not here: switchWorkbench(room) cannot open them
// any more, since they no longer have a chat-side room to switch to. The
// Actions entries below reach them the one remaining way, through a pane.
const PALETTE_ROOMS = [
  ["review", "Review room", "Pending approvals and decisions"],
  ["files", "Files room", "Read and write inside the project root"],
  ["artifacts", "Office room", "Build DOCX, XLSX, PPTX and PDF"],
  ["team", "Team room", "Agent team roster and runs"]
];

// buildCommands is rebuilt on every open so session-scoped entries reflect the
// sessions that exist right now.
function buildCommands() {
  const commands = [];
  commands.push({ group: "Go to", icon: "◈", title: "Agent Workspace",
    subtitle: "Chat, tool calls and approvals", keywords: "chat session workspace home",
    run: closeConfig });
  for (const section of CONFIG_SECTIONS) {
    for (const item of section.items) {
      commands.push({ group: "Settings", icon: item.icon, title: item.label, subtitle: item.blurb,
        keywords: `${section.group} ${item.terms}`, run: () => switchTab(item.id) });
    }
  }
  for (const [room, title, subtitle] of PALETTE_ROOMS) {
    commands.push({ group: "Workbench", icon: "▣", title, subtitle, keywords: `workbench ${room}`,
      run: () => switchWorkbench(room) });
  }
  for (const session of state.sessions.slice(0, 6)) {
    commands.push({ group: "Sessions", icon: "◈", title: session.title,
      subtitle: `${session.model} · ${session.context_profile}`, keywords: `session ${session.model}`,
      run: () => { switchTab("chat"); selectSession(session.id); } });
  }
  commands.push(
    { group: "Actions", icon: "＋", title: "New agent session", subtitle: "Choose a provider and context envelope",
      keywords: "new session start chat", run: () => $("#railNewSession").click() },
    { group: "Actions", icon: "✦", title: "Propose a Skill", subtitle: "Creates a candidate; never an active Skill",
      keywords: "new skill proposal candidate", run: openCandidateDialog },
    { group: "Actions", icon: "⚙", title: "Open settings", subtitle: "Models, tools, skills, context and system",
      keywords: "settings configuration preferences config", run: () => openConfig() },
    { group: "Actions", icon: "@", title: "Mention a Skill or tool", subtitle: "Insert a capability into the composer",
      keywords: "skills tools mcp mention capability", run: () => openCapabilityPicker("all") },
    { group: "Actions", icon: "☰", title: "Toggle the list pane", subtitle: "Show or hide the rail zone",
      keywords: "rail sessions list toggle zone", run: () => $("#toggleRail").click() },
    { group: "Actions", icon: "▣", title: "Toggle the evidence pane", subtitle: "Show or hide the side zone",
      keywords: "workbench inspector toggle side zone", run: () => $("#toggleSide").click() },
    { group: "Actions", icon: "⇔", title: "Toggle density", subtitle: "Compact for a laptop, comfortable for a desktop",
      keywords: "density compact comfortable laptop desktop zoom", run: toggleDensity },
    { group: "Actions", icon: "⌨", title: "Open a terminal pane", subtitle: "Real PTY bound to the project, in Code",
      keywords: "terminal pty shell pane code", run: () => openContentPane("terminal") },
    { group: "Actions", icon: "◧", title: "Open a browser pane", subtitle: "Managed browser with untrusted evidence, in Code",
      keywords: "browser pane code managed", run: () => openContentPane("browser") },
    { group: "Actions", icon: "⟳", title: "Refresh everything", subtitle: "Reload every panel from the server",
      keywords: "refresh reload", run: load }
  );
  return commands;
}

function matchingCommands(query) {
  const needle = query.trim().toLowerCase();
  if (!needle) return state.commandItems;
  return state.commandItems.filter(command =>
    `${command.title} ${command.subtitle} ${command.keywords || ""} ${command.group}`.toLowerCase().includes(needle));
}

function renderCommandList(query) {
  const list = $("#commandList");
  if (!list) return;
  const matches = matchingCommands(query);
  state.commandMatches = matches;
  if (state.commandIndex >= matches.length) state.commandIndex = Math.max(0, matches.length - 1);
  if (!matches.length) {
    list.innerHTML = `<div class="command-empty">Nothing matches “${escapeHTML(query)}”.</div>`;
    return;
  }
  let markup = "";
  let currentGroup = "";
  matches.forEach((command, index) => {
    if (command.group !== currentGroup) {
      currentGroup = command.group;
      markup += `<p class="command-group-label">${escapeHTML(currentGroup)}</p>`;
    }
    markup += `<button type="button" class="command-item ${index === state.commandIndex ? "active" : ""}" data-command-index="${index}"><span>${escapeHTML(command.icon)}</span><span><strong>${escapeHTML(command.title)}</strong><small>${escapeHTML(command.subtitle)}</small></span>${command.keys ? `<kbd>${escapeHTML(command.keys)}</kbd>` : ""}</button>`;
  });
  list.innerHTML = markup;
  $$("[data-command-index]", list).forEach(button =>
    button.addEventListener("click", () => runCommand(Number(button.dataset.commandIndex))));
}

function moveCommandSelection(delta) {
  const matches = state.commandMatches || [];
  if (!matches.length) return;
  state.commandIndex = (state.commandIndex + delta + matches.length) % matches.length;
  const list = $("#commandList");
  $$("[data-command-index]", list).forEach(button => {
    const active = Number(button.dataset.commandIndex) === state.commandIndex;
    button.classList.toggle("active", active);
    if (active) button.scrollIntoView({ block: "nearest" });
  });
}

function runCommand(index) {
  const command = (state.commandMatches || [])[index];
  $("#commandDialog")?.close();
  if (!command) return;
  // Run after the dialog has closed so a command that focuses an input is not
  // fighting the modal for focus. A macrotask, not requestAnimationFrame: a
  // backgrounded tab stops painting, and the command would never run at all.
  setTimeout(() => {
    try { command.run(); } catch (error) { toast(error.message, true); }
  }, 0);
}

function openCommandPalette() {
  const dialog = $("#commandDialog");
  if (!dialog || dialog.open) return;
  state.commandItems = buildCommands();
  state.commandIndex = 0;
  const input = $("#commandInput");
  input.value = "";
  renderCommandList("");
  dialog.showModal();
  input.focus();
}

function bindCommandPalette() {
  const dialog = $("#commandDialog");
  const input = $("#commandInput");
  if (!dialog || !input) return;
  $("#commandButton")?.addEventListener("click", openCommandPalette);
  input.addEventListener("input", () => { state.commandIndex = 0; renderCommandList(input.value); });
  // The palette form is method="dialog", so an unhandled Enter would close the
  // dialog and run nothing at all.
  $("#commandForm")?.addEventListener("submit", event => { event.preventDefault(); runCommand(state.commandIndex); });
  dialog.addEventListener("keydown", event => {
    if (event.key === "ArrowDown") { event.preventDefault(); moveCommandSelection(1); }
    else if (event.key === "ArrowUp") { event.preventDefault(); moveCommandSelection(-1); }
    else if (event.key === "Enter") { event.preventDefault(); runCommand(state.commandIndex); }
  });
}

/* --- Capability picker ---------------------------------------------------- */

const CAPABILITY_FILTERS = [
  ["all", "Everything"], ["skills", "Skills"], ["tools", "Direct tools"], ["mcp", "MCP catalog"]
];

function capabilityPickHTML(kind, icon, name, title, subtitle, badge, id = "", version = "") {
  return `<button type="button" class="capability-pick" data-mention-kind="${escapeHTML(kind)}" data-mention-name="${escapeHTML(name)}" data-mention-id="${escapeHTML(id)}" data-mention-version="${escapeHTML(version)}"><span>${escapeHTML(icon)}</span><span><strong>${escapeHTML(title)}</strong><small>${escapeHTML(subtitle || "No description")}</small></span>${badge}</button>`;
}

function renderCapabilityPicker() {
  const body = $("#capabilityPickerBody");
  if (!body) return;
  const filter = state.capabilityPickerFilter;
  const query = ($("#capabilityPickerQuery")?.value || "").trim().toLowerCase();
  const session = state.sessionDetail?.session;
  const contract = session?.contract || {};
  const selected = new Set((contract.selected_skills || []).map(item => item.canonical_name));
  const matches = (haystack) => !query || haystack.toLowerCase().includes(query);
  const chips = `<div class="capability-filter">${CAPABILITY_FILTERS.map(([value, label]) =>
    `<button type="button" class="capability-chip ${value === filter ? "active" : ""}" data-picker-filter="${value}">${label}</button>`).join("")}</div>`;

  let markup = chips;
  if (filter === "all" || filter === "skills") {
    const skills = (contract.skill_catalog || []).filter(item => matches(`${item.canonical_name} ${item.summary}`));
    markup += `<section class="capability-picker-section"><header><span>Skills in this session</span><span>${skills.length}</span></header><div class="capability-picker-list">${skills.length
      ? skills.map(item => capabilityPickHTML("skill", "✦", item.canonical_name, item.canonical_name, item.summary,
          selected.has(item.canonical_name) ? pill("in context", "green") : (item.pinned ? pill("pinned", "blue") : ""), item.skill_id, item.version_id)).join("")
      : `<p class="command-empty">${!session ? "Start a session first — a Skill catalog is frozen when the session opens."
          : contract.skill_catalog?.length ? "No Skill matches that search."
          : "This session's Skill catalog is empty. Promote a Skill in Skill Studio first."}</p>`}</div></section>`;
  }
  if (filter === "all" || filter === "tools") {
    const tools = (contract.tool_bindings || []).filter(item => matches(`${item.name} ${item.description}`));
    markup += `<section class="capability-picker-section"><header><span>Direct tools</span><span>${tools.length}</span></header><div class="capability-picker-list">${tools.length
      ? tools.map(item => capabilityPickHTML("tool", "⌘", item.name, item.name, item.description,
          pill(item.requires_approval ? "approval" : item.effect || "read", item.requires_approval ? "amber" : "green"))).join("")
      : `<p class="command-empty">${!session ? "Start a session first — tool bindings are frozen into its Session Contract." : "No direct tool matches that search."}</p>`}</div></section>`;
  }
  if (filter === "all" || filter === "mcp") {
    const indexed = Number(state.capability_summary?.total || 0);
    const results = state.capabilityPickerResults;
    markup += `<section class="capability-picker-section"><header><span>MCP catalog</span><span>${indexed.toLocaleString()} indexed</span></header><div class="capability-picker-list">${
      !query ? `<p class="command-empty">Type to search ${indexed.toLocaleString()} deferred tools. Only the matches you open are ever loaded.</p>`
      : state.capabilityPickerSearching ? `<p class="command-empty">Searching…</p>`
      : results.length ? results.map(item => capabilityPickHTML("mcp", "◈", item.title || item.name, item.title || item.name, item.description,
          `${pill(item.effect, item.requires_approval ? "amber" : "green")}${pill(item.readiness, item.readiness === "ready" ? "green" : "red")}`, item.id)).join("")
      : `<p class="command-empty">No indexed tool matches “${escapeHTML(query)}”.</p>`}</div></section>`;
  }
  body.innerHTML = markup;
  $$("[data-picker-filter]", body).forEach(button => button.addEventListener("click", () => {
    state.capabilityPickerFilter = button.dataset.pickerFilter;
    renderCapabilityPicker();
  }));
  $$("[data-mention-kind]", body).forEach(button => button.addEventListener("click", () => {
    const item = { kind:button.dataset.mentionKind, name:button.dataset.mentionName,
      id:button.dataset.mentionId, version:button.dataset.mentionVersion };
    if (item.kind === "skill") mentionSkill(item);
    else mentionCapability(item);
  }));
}

function appendComposerInstruction(instruction) {
  $("#capabilityDialog")?.close();
  const input = $("#chatInput");
  if (!input) {
    toast("Start a session first, then mention a Skill or tool", true);
    return;
  }
  const existing = input.value;
  const separator = !existing || existing.endsWith(" ") || existing.endsWith("\n") ? "" : " ";
  input.value = `${existing}${separator}${instruction} `;
  state.draftMessage = input.value;
  state.composerFocused = true;
  state.composerCaret = input.value.length;
  if (!$("#configOverlay").hidden) closeConfig();
  input.focus();
  input.setSelectionRange(input.value.length, input.value.length);
}

// Mentioning a Skill never mutates the frozen Session Contract. If the Skill
// was promoted after the session opened, the honest operation is to ask for a
// new session rather than imply that the running model can retrieve it.
function mentionSkill(skill) {
  const catalog = state.sessionDetail?.session?.contract?.skill_catalog || [];
  const binding = catalog.find(item => item.skill_id === skill.id || item.canonical_name === skill.name || item.canonical_name === skill.canonical_name);
  if (!binding) {
    toast("That Skill is not in this session’s frozen catalog. Start a new session to use it.", true);
    return;
  }
  appendComposerInstruction(`Use the session-bound Skill "${binding.canonical_name}". Load its exact frozen version with skill_view using skill_id "${binding.skill_id}" and version_id "${binding.version_id}" before acting.`);
}

// Direct tools and MCP capabilities get different instructions. The latter
// must still pass through search/describe/call, revision checks and approval.
function mentionCapability(capability) {
  if (typeof capability === "string") capability = { kind:"tool", name:capability };
  if (capability.kind === "mcp") {
    appendComposerInstruction(`Use the deferred capability "${capability.name}" (catalog id "${capability.id}"). Verify it with tool_search, load its exact schema and revision with tool_describe, then use tool_call; do not bypass required approval.`);
    return;
  }
  appendComposerInstruction(`Use the session-bound direct tool "${capability.name}" when needed. Inspect its arguments carefully and preserve every approval requirement.`);
}

async function searchPickerCapabilities() {
  const query = ($("#capabilityPickerQuery")?.value || "").trim();
  if (!query) { state.capabilityPickerResults = []; renderCapabilityPicker(); return; }
  state.capabilityPickerSearching = true;
  renderCapabilityPicker();
  try {
    const result = await api(`/api/capabilities?query=${encodeURIComponent(query)}&limit=12`);
    // Discard a response that a newer keystroke has already superseded.
    if (($("#capabilityPickerQuery")?.value || "").trim() !== query) return;
    state.capabilityPickerResults = result.results || [];
  } catch (error) {
    state.capabilityPickerResults = [];
    toast(error.message, true);
  } finally {
    state.capabilityPickerSearching = false;
    renderCapabilityPicker();
  }
}

function openCapabilityPicker(filter = "all") {
  const dialog = $("#capabilityDialog");
  if (!dialog) return;
  state.capabilityPickerFilter = CAPABILITY_FILTERS.some(([value]) => value === filter) ? filter : "all";
  state.capabilityPickerResults = [];
  state.capabilityPickerSearching = false;
  const query = $("#capabilityPickerQuery");
  if (query) query.value = "";
  renderCapabilityPicker();
  if (!dialog.open) dialog.showModal();
  query?.focus();
}

function bindCapabilityPicker() {
  const query = $("#capabilityPickerQuery");
  $("#capabilityClose")?.addEventListener("click", () => $("#capabilityDialog").close());
  $("#openToolCenter")?.addEventListener("click", () => { $("#capabilityDialog").close(); switchTab("mcp"); });
  $("#openSkillStudio")?.addEventListener("click", () => { $("#capabilityDialog").close(); switchTab("library"); });
  query?.addEventListener("input", () => {
    renderCapabilityPicker();
    clearTimeout(capabilitySearchTimer);
    capabilitySearchTimer = setTimeout(searchPickerCapabilities, 220);
  });
}

/* --- Density -------------------------------------------------------------- */

// Density is a data attribute rather than a stylesheet swap so the whole
// cockpit re-flows from one token set, and so nothing here has to touch an
// inline style: the server sends style-src 'self' and every inline style
// assignment would be blocked.
const DENSITY_STORAGE_KEY = "hermetrix.density";

function storedDensity() {
  try {
    const stored = localStorage.getItem(DENSITY_STORAGE_KEY);
    return stored === "compact" || stored === "comfortable" ? stored : "";
  } catch { return ""; }
}

function viewportDensity() { return window.innerWidth < 1440 ? "compact" : "comfortable"; }

function applyDensity(density) {
  state.density = density;
  document.documentElement.dataset.density = density;
  const button = $("#densityToggle");
  if (!button) return;
  const compact = density === "compact";
  button.textContent = compact ? "Compact" : "Comfortable";
  button.setAttribute("aria-pressed", String(compact));
  button.title = compact
    ? "Compact spacing, sized for a laptop display. Click for comfortable."
    : "Comfortable spacing, sized for a desktop display. Click for compact.";
}

function toggleDensity() {
  const next = state.density === "compact" ? "comfortable" : "compact";
  try { localStorage.setItem(DENSITY_STORAGE_KEY, next); } catch {}
  applyDensity(next);
}

function bindDensity() {
  applyDensity(storedDensity() || viewportDensity());
  $("#densityToggle")?.addEventListener("click", toggleDensity);
  // Follow the window only while the user has not made a choice of their own.
  window.addEventListener("resize", () => {
    if (storedDensity()) return;
    const next = viewportDensity();
    if (next !== state.density) applyDensity(next);
  });
}

/* --- Resizable zones --------------------------------------------------------
   The rail, the conversation and the workbench used to be three widths a
   designer picked once. A file tree at 320px cannot show code and a terminal
   at 320px cannot show a test run, so the width has to come from whoever is
   looking at the screen, not from us. Each zone still has a floor and a
   ceiling: collapsing one to zero would swallow whatever is inside it, and
   letting it eat the whole window would do the same to its neighbours. */
const ZONE_LIMITS = { rail: [150, 420], side: [220, 640] };

// setZoneWidth writes a custom property on the root instead of an inline
// width on the zone itself. The server sends style-src 'self', so an inline
// style on a rendered element is silently dropped; a custom property is a
// variable the stylesheet reads, which style-src has no opinion about. The
// property name below is a literal at every call site, never a variable,
// because the CSP rule this file enforces on itself (see ui_contract_test.go)
// verifies that literal rather than trusting whatever a caller passes in.
function setZoneWidth(zone, px) {
  const [min, max] = ZONE_LIMITS[zone];
  const clamped = Math.min(max, Math.max(min, Math.round(px)));
  if (zone === "rail") document.documentElement.style.setProperty("--rail-width", `${clamped}px`);
  else document.documentElement.style.setProperty("--workbench-max", `${clamped}px`);
  state.zoneWidths[zone] = clamped;
  return clamped;
}

// Task 10 persists these widths across a reload. Until it exists the drag
// still has to end cleanly rather than throw when the pointer lifts.
function saveLayout() {}

function startZoneDrag(handle, event) {
  const zone = handle.dataset.handle;
  const zones = $("#zones").getBoundingClientRect();
  const move = pointer => {
    const px = zone === "rail" ? pointer.clientX - zones.left : zones.right - pointer.clientX;
    setZoneWidth(zone, px);
  };
  move(event);
  const stop = () => {
    document.removeEventListener("pointermove", move);
    document.removeEventListener("pointerup", stop);
    saveLayout();
  };
  document.addEventListener("pointermove", move);
  document.addEventListener("pointerup", stop);
}

// collapseZone hides a zone through the stylesheet -- .zones.rail-hidden and
// .zones.side-hidden collapse its grid column to nothing -- rather than
// removing it from the document, so re-opening it does not have to re-render
// its content. Focus has to move out first: an element that becomes
// display:none cannot keep the keyboard's focus, and a screen reader
// announces nothing at all for a focus target that just vanished.
function collapseZone(zone, collapsed) {
  const target = zone === "rail" ? $("#zoneRail") : $("#zoneSide");
  if (collapsed && target.contains(document.activeElement)) $("#zoneMain").focus();
  $("#zones").classList.toggle(`${zone}-hidden`, collapsed);
  (zone === "rail" ? $("#toggleRail") : $("#toggleSide")).setAttribute("aria-pressed", String(collapsed));
}

document.addEventListener("DOMContentLoaded", () => {
  // The rail button and the workbench tabs start wired here because chat is
  // already on screen at first paint; switchView repeats this same wiring
  // every time a trip through another view recreates those nodes.
  wireChatSkeleton();
  $$("#viewSwitch [data-view]").forEach(button => button.addEventListener("click", () => switchView(button.dataset.view)));
  $("#openConfig").addEventListener("click", () => openConfig());
  $("#closeConfig").addEventListener("click", closeConfig);
  $("#configSearch").addEventListener("input", renderConfigNav);
  $$(".zone-handle").forEach(handle => handle.addEventListener("pointerdown", event => startZoneDrag(handle, event)));
  $("#toggleRail").addEventListener("click", () => collapseZone("rail", !$("#zones").classList.contains("rail-hidden")));
  $("#toggleSide").addEventListener("click", () => {
    const collapsed = !$("#zones").classList.contains("side-hidden");
    collapseZone("side", collapsed);
    if (!collapsed) renderCurrentWorkbench();
  });
  $("#refreshButton").addEventListener("click", load);
  $("#createButton").addEventListener("click", openCandidateDialog);
  $("#candidateForm").addEventListener("submit", submitCandidate);
  $("#searchInput").addEventListener("input", renderLibrary);
  $("#stateFilter").addEventListener("change", renderLibrary);
  bindDensity();
  bindCommandPalette();
  bindCapabilityPicker();
  bindFolderPicker();
  // One global accelerator rather than one per view: the palette is the single
  // way into every page, room and action, which is what lets the tab strip and
  // the rail stay short.
  document.addEventListener("keydown", event => {
    if ((event.metaKey || event.ctrlKey) && !event.altKey && event.key.toLowerCase() === "k") {
      event.preventDefault();
      // Every command here (new session, refresh, mention a Skill…) reaches
      // into a workspace that only exists once a project is open. Before that,
      // the picker's own search box is the only search there is.
      if (!$("#appShell").hidden) openCommandPalette();
      return;
    }
    // Escape leaves settings the way it leaves a dialog. A <dialog> that is
    // open handles its own Escape, so this only fires for the overlay.
    if (event.key === "Escape" && !$("#configOverlay").hidden && !$("dialog[open]")) closeConfig();
  });
  $("#pickerSearch").addEventListener("input", renderPicker);
  $("#pickerCreate").addEventListener("click", createProjectFromPicker);
  $("#projectChip").addEventListener("click", showPicker);
  initPicker();
});
