const { escapeHTML, asList, toolArgumentsPreview, toolReceiptOf, toolOutputPreview, groupTimeline } = HermetrixRuntime;
const state = { skills: [], candidates: [], archives: [], relations: [], reviews: [], curator_runs: [], profiles: [], providers: [], mcp_servers: [], capability_summary: { total:0, by_source:{}, by_readiness:{} }, capabilityResults: [], capabilityPickerResults: [], capabilityPickerFilter:"all", selectedCapability: null, mcpServerQuery:"", mcpServerFilter:"all", mcpCapabilityQuery:"", sessions: [], projects: [], projectFiles: [], jobs: [], artifacts: [], terminals: [], browserTabs: [], teams: [], teamRuns: [], durableTasks:[], selectedDurableTask:null, taskExecutions:{}, taskDecisionLabs:{}, settings: [], memories: [], backups: [], usage: {}, fidelityCases: [], fidelityRuns: [], qualifications: [], curatorFindings: [], schedules: [], gcRuns: [], skillAuthority:null, authorityActions:[], sharePreview:null, workspacePreview:null, recoveryReport:null, activeTab: "chat", view: "chat", workbenchTab:"review", selectedSkill: null, selectedSkillDetail:null, selectedSession: null, selectedProject: null, currentProject: null, selectedTerminal:null, selectedBrowserTab:null, selectedTeam:null, teamDraft:null, projectFile:null, projectFileDiff:"", sessionDetail: null, contextResult: null, modelProbe: null, sending: false, draftQualificationReason:"", sessionError:"", commandItems: [], commandMatches: [], commandIndex: 0, capabilityPickerSearching: false, density: "comfortable", sessionOptionsOpen: false, railProjectsOpen: true, railSetupOpen: false, railProjectOpen: {}, sessionReady: false, elicitations: [], folderListing: null, draftMessage: "", composerAttachments: [], composerFocused: false, composerCaret: 0, zoneWidths: {}, panes: [], maximisedPane: null, paneLayout: "bottom-wide", draggedPane: null, paneSplitX: 50, paneSplitY: 50, authPrincipal: "" };
const $ = (selector, root = document) => root.querySelector(selector);
const $$ = (selector, root = document) => [...root.querySelectorAll(selector)];
const UI_ICON_NAMES = new Set(["search","plus","close","sidebar","workbench","refresh","settings","file","files","terminal","browser","activity","model","tools","skill","review","learning","insights","archive","context","fidelity","project","jobs","artifact","chat","at","expand","contract","grip"]);
function uiIcon(name) {
  const icon = UI_ICON_NAMES.has(name) ? name : "activity";
  return `<svg class="ui-icon" aria-hidden="true"><use href="/assets/icons/hermetrix-ui.svg#${icon}"></use></svg>`;
}

let authenticationAttempt;
async function authenticateControlAPI() {
  if (authenticationAttempt) return authenticationAttempt;
  authenticationAttempt = (async () => {
    const token = window.prompt("Hermetrix authentication token");
    if (!token) throw new Error("Authentication is required");
    const response = await fetch("/api/auth/session", { method:"POST", headers:{"Content-Type":"application/json"}, body:JSON.stringify({token}) });
    const body = await response.json().catch(() => ({}));
    if (!response.ok) throw new Error(body.error || "Authentication failed");
    state.authPrincipal = body.principal || state.authPrincipal;
    return body;
  })();
  try { return await authenticationAttempt; }
  finally { authenticationAttempt = null; }
}

async function api(path, options = {}, authenticatedRetry = false) {
  const response = await fetch(path, { headers: { "Content-Type": "application/json", ...(options.headers || {}) }, ...options });
	state.authPrincipal = response.headers.get("X-Hermetrix-Principal") || state.authPrincipal;
  if (response.status === 401 && path !== "/api/auth/session" && !authenticatedRetry) {
    await authenticateControlAPI();
    return api(path, options, true);
  }
  const body = await response.json().catch(() => ({}));
  if (!response.ok) throw new Error(typeof body.error === "string" ? body.error : body.error?.message || body.message || `Request failed (${response.status})`);
  return body;
}

function currentActor() { return state.authPrincipal || "local-user"; }

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
  // Opening a room from elsewhere (a skill row, a candidate) has to reveal the
  // side zone even if the user had collapsed it earlier.
  collapseZone("side", false);
}

function ensureReviewSurface() {
  if (!state.panes.includes("review")) openContentPane("review");
  else if (!$(".pane-body[data-pane-kind='review']")) renderPanes();
  return $(".pane-body[data-pane-kind='review']");
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
  const pinned = matches.filter(item => item.pinned);
  const recent = matches.filter(item => !item.pinned);
  const pinnedRoot = $("#pickerPinned");
  const recentRoot = $("#pickerRecent");
  pinnedRoot.innerHTML = group("ปักหมุด", pinned);
  recentRoot.innerHTML = group("ล่าสุด", recent);
  pinnedRoot.hidden = pinned.length === 0;
  recentRoot.hidden = recent.length === 0;
  const exact = state.projects.some(item => item.name.toLowerCase() === query);
  const create = $("#pickerCreate");
  create.hidden = !query || exact;
  create.textContent = `สร้างโปรเจค “${query}”`;
  $$("[data-open-project]").forEach(button =>
    button.addEventListener("click", () => openProject(button.dataset.openProject)));
}

// Opening records that someone worked here, which is what "recent" is ordered
// by, and then hands the screen to the shell.
let projectOpenGeneration = 0;
let sessionSelectionGeneration = 0;
async function openProject(id, options = {}) {
  if (state.sending) { toast("รอให้งานในแชทนี้เสร็จก่อนเปลี่ยนโปรเจกต์", true); return; }
  if (state.sessionCreationPending && !options.createdSession) { toast("กำลังสร้างแชท กรุณารอสักครู่", true); return; }
  ++navigationGeneration;
  const generation = ++projectOpenGeneration;
  const selection = options.selectionGeneration ?? ++sessionSelectionGeneration;
  ++loadGeneration;
  invalidateSurfaces();
  try {
    captureWorkspaceDrafts();
    captureCodeDraft();
    const project = await api(`/api/projects/${encodeURIComponent(id)}/open`, { method:"POST", body:"{}" });
    if (generation !== projectOpenGeneration || selection !== sessionSelectionGeneration || state.sending) return;
    captureWorkspaceDrafts();
    state.currentProject = project;
    state.draftProjectID = state.currentProject.id;
    state.selectedSession = null;
    state.sessionDetail = null;
    state.elicitations = [];
    restoreWorkspaceDrafts(project.id);
    state.projectFile = null;
    state.projectFileDiff = "";
    state.projectPath = "";
    state.projectFiles = [];
    state.workspaceFiles = [];
    state.selectedTerminal = null;
    state.terminalOutput = "";
    const projects = await api("/api/projects");
    if (generation !== projectOpenGeneration || selection !== sessionSelectionGeneration) return;
    state.projects = asList(projects);
    state.selectedProject = id;
    showShell();
    // The layout key is project and view scoped, so opening a project has to
    // restore this project's memory of whichever view is already on screen --
    // the same restore switchView performs on every later transition. This
    // goes through the same applyLayoutForView() switchView uses rather than
    // a second copy of its guard: the picker is reachable from any view (the
    // project chip works from Code and Knowledge too, not only Chat), so
    // reopening a project while parked off Chat needs the identical rule
    // that a view with no side content stays collapsed, whatever a stale
    // saved value or a stale #zoneSide left over from before this project
    // was opened might otherwise imply.
    applyLayoutForView();
    if (state.view === "code") renderPanes();
    await load();
    if (generation !== projectOpenGeneration || selection !== sessionSelectionGeneration) return;
    if (options.selectSession === false) return project;
    // Land on this workspace's own conversation. Opening a project while a
    // different project's session is selected leaves chat showing someone
    // else's transcript — or nothing at all — with no hint why.
    if (state.sessionDetail?.session?.project_id !== id) {
      const own = state.sessions.filter(item => item.project_id === id)
        .sort((a, b) => String(b.updated_at || "").localeCompare(String(a.updated_at || "")));
      if (own.length) await selectSession(own[0].id);
      else { state.selectedSession = null; state.sessionDetail = null; renderChat(); }
    }
    return project;
  } catch (error) { if (generation === projectOpenGeneration) toast(error.message, true); }
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
  if (state.sessionCreationPending) { toast("กำลังเริ่มแชท กรุณารอสักครู่"); return; }
  restorePlanningWorkspace();
  ++navigationGeneration;
  captureWorkspaceDrafts();
  dismissMobileRail();
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

const SURFACE_DATA = {
  library: {skillAuthority:"/api/skill-authority", authorityActions:"/api/skill-authority/actions"},
  projects: {jobs:"/api/jobs", artifacts:"/api/artifacts", memories:"/api/memories", durableTasks:"/api/tasks?limit=100"},
  office: {jobs:"/api/jobs"}, artifacts: {artifacts:"/api/artifacts"}, insights: {curatorFindings:"/api/curator/findings"},
  fidelity: {fidelityCases:"/api/fidelity/cases", fidelityRuns:"/api/fidelity/runs"},
  maintenance: {settings:"/api/settings", memories:"/api/memories", backups:"/api/backups", usage:"/api/usage", curatorFindings:"/api/curator/findings", schedules:"/api/maintenance/schedules", gcRuns:"/api/maintenance/gc"},
  review: {jobs:"/api/jobs", skillAuthority:"/api/skill-authority"}, output: {jobs:"/api/jobs"},
  terminal: {terminals:"/api/terminals"}, browser: {browserTabs:"/api/browser/tabs"},
  team: {teams:"/api/teams", teamRuns:"/api/team-runs"}, tasks: {durableTasks:"/api/tasks?limit=100"}
};
const surfaceLoads = new Map();
const readySurfaces = new Set();
let surfaceGeneration = 0;

function invalidateSurfaces() {
  ++surfaceGeneration;
  surfaceLoads.clear();
  readySurfaces.clear();
}

function surfaceScope(surface) {
  const projectID = surface === "projects" ? state.selectedProject : state.currentProject?.id;
  const path = ["files", "projects"].includes(surface) ? state.projectPath || "" : "";
  return `${surfaceGeneration}:${state.currentProject?.id || ""}:${projectID || ""}:${path}`;
}

async function hydrateSurface(surface, force = false) {
  const scope = surfaceScope(surface);
  const existing = surfaceLoads.get(surface);
  if (!force && existing?.scope === scope) return existing.promise;
  const endpoints = {...SURFACE_DATA[surface]};
  const project = surface === "projects" ? state.projects.find(item => item.id === state.selectedProject) : state.currentProject;
  if (["files", "projects"].includes(surface) && project?.root_path) {
    endpoints.projectFiles = `/api/projects/${encodeURIComponent(project.id)}/files?path=${encodeURIComponent(state.projectPath || "")}`;
  }
  const request = {scope, promise:null};
  readySurfaces.delete(surface);
  request.promise = (async () => {
    try {
      const entries = await Promise.all(Object.entries(endpoints).map(async ([key, path]) => [key, await api(path)]));
      if (surfaceScope(surface) !== scope || surfaceLoads.get(surface) !== request) return false;
      for (const [key, value] of entries) state[key] = ["usage", "skillAuthority"].includes(key) ? value : asList(value);
      if (["files", "projects"].includes(surface)) {
        if (!project?.root_path) state.projectFiles = [];
        if (project?.id === state.currentProject?.id) state.workspaceFiles = state.projectFiles;
      }
      if (surface === "team" && !state.selectedTeam && state.teams.length) state.selectedTeam = state.teams[0].id;
      readySurfaces.add(surface);
      return true;
    } catch (error) {
      if (surfaceScope(surface) !== scope || surfaceLoads.get(surface) !== request) return false;
      surfaceLoads.delete(surface);
      throw error;
    }
  })();
  surfaceLoads.set(surface, request);
  return request.promise;
}

let loadGeneration = 0;
async function load() {
  const generation = ++loadGeneration;
  const projectID = state.currentProject?.id;
  invalidateSurfaces();
  try {
    const [bootstrap, projects, qualifications, capabilities] = await Promise.allSettled([
      api("/api/bootstrap"), api("/api/projects"), api("/api/qualifications"), api("/api/capabilities?limit=1")
    ]);
    if (generation !== loadGeneration || projectID !== state.currentProject?.id) return false;
    if (bootstrap.status === "rejected") throw bootstrap.reason;
    const data = bootstrap.value;
    if (!data || typeof data !== "object" || Array.isArray(data)) throw new Error("Workspace data could not be loaded. Refresh to try again.");
    Object.assign(state, data);
    for (const key of ["skills", "candidates", "archives", "relations", "reviews", "curator_runs", "profiles", "providers", "mcp_servers", "sessions", "direct_tools"]) {
      state[key] = asList(data[key]);
    }
    // Belt to the server's braces: one endpoint answering null instead of []
    // used to throw here and leave every panel in the cockpit unrendered.
    if (projects.status === "fulfilled") state.projects = asList(projects.value);
    if (qualifications.status === "fulfilled") state.qualifications = asList(qualifications.value);
    state.runtimeCapabilities = capabilities.status === "fulfilled" ? capabilities.value?.runtime || {} : {};
    state.sessionError = "";
    for (const result of [projects, qualifications, capabilities]) {
      if (result.status === "rejected") toast(result.reason?.message || "Some workspace details could not be loaded", true);
    }
    if (!state.selectedProject && state.projects.length) state.selectedProject = state.projects[0].id;
    if (!state.selectedTerminal && state.terminals.length) state.selectedTerminal = state.terminals.find(item => item.state === "running")?.id || state.terminals[0].id;
    if (!state.selectedBrowserTab && state.browserTabs.length) state.selectedBrowserTab = state.browserTabs.find(item => item.state === "ready")?.id || state.browserTabs[0].id;
    if (!state.selectedTeam && state.teams.length) state.selectedTeam = state.teams[0].id;
    renderAll();
    if (CONFIG_PAGE_IDS.includes(state.activeTab)) {
      const tab = state.activeTab;
      const hydrated = await hydrateSurface(tab);
      if (hydrated && generation === loadGeneration && state.activeTab === tab) renderConfigPage(tab);
    } else if (state.view === "code" || !$("#zones").classList.contains("side-hidden")) {
      renderPanes();
    }
    return true;
  } catch (error) {
    if (generation !== loadGeneration || projectID !== state.currentProject?.id) return false;
    state.sessionError = error.message;
    if (CONFIG_PAGE_IDS.includes(state.activeTab)) {
      const root = $(`#view-${state.activeTab}`);
      if (root) root.textContent = error.message;
    }
    renderChat();
    toast(error.message, true);
    return false;
  }
}

function renderAll() {
  // Isolate each panel: one renderer throwing on an unexpected payload must not
  // leave every panel after it in the list blank.
  for (const render of [renderChat]) {
    try { render(); } catch (error) { console.error(`${render.name} failed`, error); }
  }
  state.pendingProposals = state.candidates.filter(item => ["needs_review", "quarantined"].includes(item.state)).length;
  state.pendingReviews = state.reviews.filter(item => item.state === "queued" || item.state === "running").length;
  const waiting = state.pendingProposals + state.pendingReviews;
  $("#proposalBadge").hidden = waiting === 0;
  $("#proposalBadge").textContent = waiting;
  renderProjectChip();
  if (CONFIG_PAGE_IDS.includes(state.activeTab)) renderConfigPage(state.activeTab);
}

function renderConfigPage(tab) {
  if (SURFACE_DATA[tab] && !readySurfaces.has(tab)) {
    const root = $(`#view-${tab}`);
    if (root) root.innerHTML = `<div class="probe-empty" role="status">กำลังโหลด…</div>`;
    return;
  }
  const renderers = {providers:renderProviders, mcp:renderMCP, tools:renderDirectTools, library:renderLibrary, proposals:renderProposals,
    learning:renderLearning, insights:renderInsights, archive:renderArchive, context:renderContext,
    projects:renderProjects, office:renderOffice, artifacts:renderArtifacts, fidelity:renderFidelity, maintenance:renderMaintenance,
    discord:() => window.HermetrixDiscord?.render($("#view-discord"))};
  renderers[tab]?.();
  if (SKILL_PAGES.includes(tab)) renderStats();
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
  const collection = state.skillCollectionFilter || "all";
  const items = state.skills.filter(item => {
    const haystack = `${item.canonical_name} ${item.summary} ${item.origin} ${item.owner}`.toLowerCase();
    return (!query || haystack.includes(query)) && (!filter || item.state === filter) &&
      (collection === "all" || collection === "pinned" && item.pinned || collection === "agent" && (String(item.origin || "").startsWith("agent") || item.owner === "agent") || collection === "enabled" && item.enabled);
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
  const list = items.length ? `<div class="skill-list skill-gallery">${items.map(item => {
    const promotion = promotionBySkill.get(item.id);
    const name = item.canonical_name || "Skill";
    const monogram = name.replace(/^aetox[-_]/i, "").slice(0, 2).toLowerCase();
    return `<article class="skill-row skill-tile ${state.selectedSkill === item.id ? "selected" : ""}" data-skill-id="${escapeHTML(item.id)}" tabindex="0" aria-label="เปิดสกิล ${escapeHTML(name)}">
      <div><span class="skill-avatar" aria-hidden="true">${escapeHTML(monogram)}</span><div class="row-title"><h3>${escapeHTML(name)}</h3>${item.pinned ? pill("ปักหมุด", "blue") : ""}${promotion ? `<span title="promoted by agent">${pill("โดยเอเจนต์", "amber")}</span>` : ""}</div>
      <p>${escapeHTML(item.summary || "ยังไม่มีคำอธิบาย")}</p>
      <div class="skill-tile-status">${pill(item.state, item.state === "active" ? "green" : "amber")}<small>ใช้ ${Number(item.injected_count || 0).toLocaleString()} ครั้ง</small></div>
      ${promotion ? `<p class="skill-promotion">Hermetrix promoted this on ${escapeHTML(formatDate(promotion.completed_at || promotion.created_at))} under policy r${promotion.policy_revision}. Open it to edit, or undo it here.</p><div class="action-row"><button class="ghost" data-revert-promotion="${escapeHTML(promotion.id)}">Undo this promotion</button></div>` : ""}</div>
      <div class="skill-row-actions"><button class="ghost" type="button" data-mention-skill="${escapeHTML(item.id)}">ใช้ในแชท</button></div>
    </article>`;
  }).join("")}</div>` : `<div class="empty"><h3>${state.skills.length ? "ไม่พบสกิลที่ตรงกับตัวกรอง" : "ยังไม่มีสกิล"}</h3><p>สร้างข้อเสนอสกิลใหม่ หรือปรับคำค้นหาและตัวกรอง</p></div>`;
  // Skill Studio opens on what a person came to do -- read the library, add a
  // Skill -- with the authority policy folded away behind its own summary
  // rather than sitting above the list it governs.
  const pending = state.candidates.filter(item => ["needs_review", "quarantined"].includes(item.state)).length;
  const intro = `<section class="skill-studio-intro skill-directory-head"><div><h2>สกิล</h2><p>สกิลที่ใช้งานอยู่ใน Hermetrix เปิดดูรายละเอียด ปักหมุด หรือเรียกใช้ในแชทได้</p></div><div class="action-row"><button class="ghost" type="button" id="studioReviewButton">ข้อเสนอที่รอ ${pending}</button><button class="primary" type="button" id="studioCreateButton">+ สร้างข้อเสนอ</button></div></section>`;
  const collections = [["all", "ทั้งหมด", state.skills.length], ["enabled", "เปิดใช้", state.skills.filter(item => item.enabled).length], ["pinned", "ปักหมุด", state.skills.filter(item => item.pinned).length], ["agent", "จากเอเจนต์", state.skills.filter(item => String(item.origin || "").startsWith("agent") || item.owner === "agent").length]];
  $("#libraryIntro").innerHTML = intro;
  root.innerHTML = `<div class="skill-collection-tabs" role="group" aria-label="กรองสกิล">${collections.map(([id, label, count]) => `<button type="button" data-skill-collection="${id}" aria-pressed="${collection === id}">${label} <small>${count}</small></button>`).join("")}</div><div class="skill-directory-count">แสดง ${items.length} จาก ${state.skills.length} สกิล</div>${list}<div class="skill-policy-wrap">${policyPanel}</div>`;
  $$('[data-skill-collection]', root).forEach(button => button.addEventListener("click", () => { state.skillCollectionFilter = button.dataset.skillCollection; renderLibrary(); }));
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
    const reviewSurface = ensureReviewSurface();
    const attempts = skill.success_count + skill.failure_count;
    const observed = attempts ? `${skill.success_count}/${attempts} observed success` : "No explicit outcomes yet";
    reviewSurface.innerHTML = `
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
    const candidate = await api(`/api/skills/${encodeURIComponent(skill.id)}/improvements`, { method:"POST", body:JSON.stringify({ actor:currentActor(), reason }) });
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
    await api(`/api/skills/${encodeURIComponent(skill.id)}/archive`, { method: "POST", body: JSON.stringify({ actor: currentActor(), reason }) });
    state.selectedSkill = null;
    ensureReviewSurface().innerHTML = `<div class="empty-inspector"><span class="orb">✓</span><h2>Archived safely</h2><p>The exact version remains available in Archive and restore creates a new proposal.</p></div>`;
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
      actor:currentActor(), expected_version_id:skill.current_version_id, [field]:value
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
      canonical_name:canonicalName, actor:currentActor(), reason:`User-created fork of ${skill.canonical_name}`
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
      max_candidate_tokens:Number(form.get("max_candidate_tokens")), actor:currentActor(),
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
    const candidate = await api(`/api/skill-authority/actions/${encodeURIComponent(id)}/rollback`, { method:"POST", body:JSON.stringify({ actor:currentActor(), reason }) });
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
      <div class="action-row"><button class="ghost" data-review="${escapeHTML(item.id)}">Inspect content</button><button class="primary" data-promote="${escapeHTML(item.id)}" title="${errors.length ? `Blocked by ${errors[0].code} — see findings above` : !item.checks.passed ? "Checks must pass before promotion" : "Promote this candidate to an immutable active version"}" ${errors.length || !item.checks.passed ? "disabled" : ""}>Approve & promote</button><button class="danger" data-reject="${escapeHTML(item.id)}">Reject</button></div></article>`;
  }).join("")}</div>`;
  $$('[data-review]', root).forEach(button => button.addEventListener("click", () => inspectCandidate(button.dataset.review)));
  $$('[data-promote]', root).forEach(button => button.addEventListener("click", () => promoteCandidate(button.dataset.promote)));
  $$('[data-reject]', root).forEach(button => button.addEventListener("click", () => rejectCandidate(button.dataset.reject)));
}

function renderLearning() {
  const root = $("#view-learning");
  const queued = state.reviews.filter(item => item.state === "queued").length;
  root.innerHTML = `<div class="panel"><div class="proposal-head"><div><h3>Background review queue</h3><p>Jobs persist across restart, use structured digests, yield to foreground inference, and can create only checked candidates.</p></div><button class="primary" id="runReviewButton" title="${queued ? "Run the oldest queued background review now" : "No queued reviews — new milestones, corrections and explicit learn requests enqueue here"}" ${queued ? "" : "disabled"}>Run next review</button></div></div>
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
    ensureReviewSurface().innerHTML = `<div class="inspect-head"><p class="eyebrow">Untrusted candidate</p><h2>${escapeHTML(item.canonical_name)}</h2><div class="meta">${pill(item.state,item.checks.passed ? "green" : "red")}${pill(`revision ${item.revision}`)}</div></div>
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
    await api(`/api/candidates/${encodeURIComponent(item.id)}/capability-review`, { method:"POST", body:JSON.stringify({ actor:currentActor(), decision:"approve", expected_revision:item.revision }) });
    toast("Capability widening approved for this exact revision");
  } catch (error) { toast(error.message, true); }
}

async function saveCandidateEdit(item) {
  try {
    const updated = await api(`/api/candidates/${encodeURIComponent(item.id)}`, { method:"PATCH", body:JSON.stringify({ markdown:$("#candidateEditor").value, actor:currentActor(), expected_revision:item.revision }) });
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
    await api(`/api/candidates/${encodeURIComponent(id)}/promote`, { method: "POST", body: JSON.stringify({ actor: currentActor(), expected_revision: item.revision }) });
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
    await api(`/api/candidates/${encodeURIComponent(id)}/reject`, { method: "POST", body: JSON.stringify({ actor: currentActor(), reason, expected_revision: item.revision }) });
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
  try { await api(`/api/archives/${encodeURIComponent(id)}/restore`, { method: "POST", body: JSON.stringify({ actor: currentActor(), reason }) }); toast("Restore proposal created — active state is unchanged"); await load(); switchTab("proposals"); } catch (error) { toast(error.message, true); }
}

function profileLabel(profile) {
  const labels = { "compact-16k":"Compact 16k", "compact-32k":"Compact 32k", "certified-64k":"Certified 64k", "extended-96k":"Extended 96k", "extended-128k":"Extended 128k", "extended-256k":"Extended 256k", "ultra-1m":"Ultra 1M" };
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
  // Gate 2: the small declared envelopes need no qualification run; larger
  // envelopes need exact evidence or a reviewed override.
  if (profile.name === "compact-16k" || profile.name === "compact-32k") return { admitted:true, mode:"compatibility", budget };
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
  // A failed turn used to render as nothing at all: the user saw an empty
  // conversation and assumed the model replied nonsense. Say what happened
  // and what to do next instead.
  if (event.event_kind === "turn_failed") {
    return `<article class="chat-message assistant"><div class="message-role">Hermetrix</div><div class="message-body">${pill("turn failed", "red")} ${escapeHTML(event.content || "Turn failed")}<br><small>งานนี้ใหญ่เกินงบ 12 model steps — ลองถามแคบลงเป็นงานเดียว (เช่น สรุปไฟล์เดียว แทนทั้ง repo) แล้วดู tool receipts ใน Review ว่าเงินหมดตรงไหน</small></div></article>`;
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

// The left rail is global navigation, not a list that changes meaning between
// Chat and Code. Projects own their sessions, so the hierarchy is visible and
// clickable in the same place instead of flattening every conversation into
// one anonymous list.
function renderRailNavigation(selectedID) {
  const projectGroups = state.projects.map(project => {
    const sessions = state.sessions.filter(item => item.project_id === project.id);
    const projectOpen = Object.prototype.hasOwnProperty.call(state.railProjectOpen, project.id)
      ? state.railProjectOpen[project.id]
      : project.id === state.currentProject?.id;
    return `<section class="rail-project ${project.id === state.currentProject?.id ? "active" : ""}">
      <div class="rail-project-head">
        <button class="rail-project-button" data-rail-project="${escapeHTML(project.id)}" title="Open ${escapeHTML(project.root_path || project.name)}">
          ${uiIcon("project")}<strong>${escapeHTML(project.name)}</strong><small>${sessions.length}</small>
        </button>
        <button class="rail-project-toggle" data-rail-project-toggle="${escapeHTML(project.id)}" aria-expanded="${projectOpen}" aria-label="${projectOpen ? "Collapse" : "Expand"} ${escapeHTML(project.name)}"><span aria-hidden="true">›</span></button>
      </div>
      <div class="rail-project-sessions" data-rail-project-sessions="${escapeHTML(project.id)}" ${projectOpen ? "" : "hidden"}>${sessions.map(item => {
        const meta = `${item.model} · ${item.context_profile}`;
        return `<div class="session-row"><button class="session-item ${item.id === selectedID ? "active" : ""}" data-session-id="${escapeHTML(item.id)}" title="${escapeHTML(`${item.title} — ${meta}`)}"><strong>${escapeHTML(item.title)}</strong><span>${escapeHTML(meta)}</span></button><button class="session-delete" data-delete-session="${escapeHTML(item.id)}" title="Delete this session" aria-label="Delete ${escapeHTML(item.title)}">${uiIcon("close")}</button></div>`;
      }).join("") || `<p class="rail-project-empty">No sessions</p>`}</div>
    </section>`;
  }).join("");
  return `<nav class="rail-primary" aria-label="Hermetrix navigation">
      <button class="rail-nav-item ${state.view === "chat" ? "active" : ""}" data-rail-view="chat">${uiIcon("chat")}<span>Chat</span></button>
      <button class="rail-nav-item ${state.view === "code" ? "active" : ""}" data-rail-view="code">${uiIcon("files")}<span>Workspace</span></button>
      <button class="rail-nav-item" data-open-tasks>${uiIcon("activity")}<span>Plans · แผนงาน</span></button>
      <button class="rail-nav-item" data-rail-config="mcp">${uiIcon("tools")}<span>MCP server</span></button>
      <button class="rail-nav-item" data-rail-config="providers">${uiIcon("model")}<span>Models</span></button>
    </nav>
    <details class="rail-projects" id="railProjects" ${state.railProjectsOpen ? "open" : ""}>
      <summary class="nav-label rail-project-label"><span>Projects</span><small>${state.projects.length}</small></summary>
      <div class="rail-project-list">${projectGroups || `<p class="rail-project-empty">No projects</p>`}</div>
    </details>`;
}

function bindChatNavigation(dock) {
  $("#chatProviderSelect")?.addEventListener("change", event => { state.draftProviderID = event.target.value; state.draftProfileName = ""; state.draftQualificationReason=""; state.sessionError=""; renderChat(); $("#chatProviderSelect")?.focus(); });
  $("#chatProfileSelect")?.addEventListener("change", event => { state.draftProfileName = event.target.value; state.draftQualificationReason=""; state.sessionError=""; renderChat(); $("#chatProfileSelect")?.focus(); });
  // Update state without re-rendering: the textarea lives inside an open
  // <details>, and a re-render would collapse it and take the caret with it.
  $("#chatQualificationReason")?.addEventListener("input", event => { state.draftQualificationReason=event.target.value; });
  // Remember the sidebar's disclosure state across re-renders and app restarts.
  $("#railProjects")?.addEventListener("toggle", event => { state.railProjectsOpen = event.target.open; saveLayout(); });
  $("#sessionSetup")?.addEventListener("click", openSessionSetup);
  $("#sessionOptions")?.addEventListener("toggle", event => { state.sessionOptionsOpen = event.target.open; saveLayout(); });
  $("#openProvidersFromDock")?.addEventListener("click", () => { closeSessionSetup(); switchTab("providers"); });
  $$("[data-rail-view]", dock).forEach(button => button.addEventListener("click", () => switchView(button.dataset.railView)));
  $$("[data-rail-config]", dock).forEach(button => button.addEventListener("click", () => openConfig(button.dataset.railConfig)));
  $$("[data-rail-project]", dock).forEach(button => button.addEventListener("click", async () => { dismissMobileRail(); if (state.view !== "chat") switchView("chat"); await openProject(button.dataset.railProject); }));
  $$("[data-rail-project-toggle]", dock).forEach(button => button.addEventListener("click", () => {
    const projectID = button.dataset.railProjectToggle;
    const open = button.getAttribute("aria-expanded") !== "true";
    state.railProjectOpen[projectID] = open;
    button.setAttribute("aria-expanded", String(open));
    button.setAttribute("aria-label", `${open ? "Collapse" : "Expand"} ${state.projects.find(project => project.id === projectID)?.name || "project"}`);
    const sessions = dock.querySelector(`[data-rail-project-sessions="${CSS.escape(projectID)}"]`);
    if (sessions) sessions.hidden = !open;
    saveLayout();
  }));
  $$("[data-session-id]", dock).forEach(button => button.addEventListener("click", async () => { dismissMobileRail(); if (state.view !== "chat") switchView("chat"); await selectSession(button.dataset.sessionId); }));
  $$("[data-delete-session]", dock).forEach(button => button.addEventListener("click", event => {
    event.stopPropagation();
    deleteSession(button.dataset.deleteSession);
  }));
  $$("[data-open-tasks]", dock).forEach(button => button.addEventListener("click", openTasks));
}

function renderChat() {
  const root = $("#view-chat");
  const dock = $("#sessionDock");
  if (!dock) return;
  // Streaming re-renders this whole view on every delta. Take the composer's
  // draft, caret and focus before the markup is replaced so they can be put
  // back afterwards; without this, typing while a turn streams loses a
  // character every time a token arrives.
  captureComposer();
  const enabledProviders = state.providers.filter(provider => provider.enabled);
  if (!state.draftProviderID || !enabledProviders.some(provider => provider.id === state.draftProviderID)) {
    state.draftProviderID = HermetrixRuntime.preferredProvider(enabledProviders)?.id || null;
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
  const timeline = (state.sessionDetail?.events || []).filter(event => ["message", "tool_call", "tool_result", "approval_required", "approval_decision", "turn_failed"].includes(event.event_kind));
  // An MCP server can stop mid tool call to ask a question. It is waiting on
  // the answer right now, so it is rendered after the transcript rather than
  // inside it: it is not history yet.
  const questions = state.elicitations.filter(item => item.session_id === selectedID);
  const session = state.sessionDetail?.session;
  const contract = session?.contract || {};
  // Context meter: provider-reported input tokens of the latest sampled step
  // against this session's envelope. It is a measured high-water mark, not a
  // prediction — output reserve and estimator error are not folded in.
  const contextProfile = state.profiles.find(item => item.name === session?.context_profile);
  const contextUsages = (state.sessionDetail?.events || [])
    .map(item => Number(item.metadata?.usage?.prompt_tokens || 0))
    .filter(value => value > 0);
  const contextLast = contextUsages.length ? contextUsages[contextUsages.length - 1] : 0;
  const contextMax = contextUsages.length ? Math.max(...contextUsages) : 0;
  const contextTotal = Number(contextProfile?.total || 0);
  const contextBar = session && contextTotal ? `<div class="context-meter" title="Provider-reported input tokens, latest sampled step"><div class="context-meter-track"><div class="context-meter-fill" style="width:${Math.min(100, Math.round(contextLast / contextTotal * 100))}%"></div></div><small>context ${contextLast.toLocaleString()} / ${contextTotal.toLocaleString()}${contextMax > contextLast ? ` · max ${contextMax.toLocaleString()}` : ""}</small></div>` : "";
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
  dock.innerHTML = `${renderRailNavigation(selectedID)}<button type="button" class="rail-nav-item session-setup" id="sessionSetup" aria-haspopup="dialog">${uiIcon("model")}<span>โมเดลและตัวเลือก</span></button>`;
  $("#sessionSetupBody").innerHTML = `<div class="session-create">${enabledProviders.length ? `
        <p class="session-summary">โปรเจกต์: ${escapeHTML(draftProjectName)}<span>การตั้งค่านี้ใช้กับแชทใหม่ แชทเดิมใช้โมเดลที่เลือกไว้ตอนเริ่ม</span></p>
        <label>Model<select id="chatProviderSelect">${enabledProviders.map(provider => `<option value="${escapeHTML(provider.id)}" ${provider.id === state.draftProviderID ? "selected" : ""}>${escapeHTML(provider.name)} · ${escapeHTML(provider.model)}</option>`).join("")}</select></label>
        <button type="button" class="ghost" id="openProvidersFromDock">จัดการโมเดลและการเชื่อมต่อ</button>
        ${draftProvider && !draftProvider.credential_ready ? `<p class="session-error" role="alert">${escapeHTML(draftProvider.name)} has no API key. Open Models and paste one — it takes effect immediately.</p>` : ""}
        ${draftProfile && !admission.admitted && !needsOverride ? `<p class="session-error" role="alert">Only ${admission.budget.toLocaleString()} answer tokens left. Choose a larger envelope under Options.</p>` : ""}
        ${needsOverride ? `<p class="session-note">Opens under a reviewed 24-hour override. The reason is recorded with the session and editable under Options.</p>` : ""}
        ${state.sessionError ? `<p class="session-error" role="alert">${escapeHTML(state.sessionError)}</p>` : ""}
        <details class="session-options" id="sessionOptions" ${optionsOpen ? "open" : ""}><summary>ตัวเลือกเพิ่มเติม · ${escapeHTML(draftProfile ? profileLabel(draftProfile) : "Context")}</summary><div class="session-options-body">
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
      </div>`;
  const railStart = $("#railNewSession");
  railStart.disabled = state.sending || Boolean(state.startingSession);
  // One short label on one line. Which envelope it opens under is the summary
  // line's job, not the button's.
  railStart.innerHTML = `${uiIcon("plus")}<span>New task</span>`;
  bindChatNavigation(dock);
  // Navigation and model setup are shared by Chat, Workspace and Plans.
  // Only the mounted Chat view needs transcript and composer markup.
  if (!root) return;
  root.innerHTML = `<div class="chat-layout"><section class="chat-stage">
      ${session ? `<header class="chat-head"><div><p class="eyebrow">${escapeHTML(session.provider_name)} / ${escapeHTML(session.context_profile)}</p><h2>${escapeHTML(session.title)}</h2><small>contract ${escapeHTML(shortHash(session.contract_revision))} · cache epoch ${session.cache_epoch} · ${escapeHTML(session.contract?.qualification?.mode || "unbound")}</small><div class="session-capabilities"><button class="capability-chip" data-open-capabilities="skills">Skills <strong>${selectedSkills.length}/${skillCatalog.length}</strong></button><button class="capability-chip" data-open-capabilities="tools">Direct tools <strong>${directTools.length}</strong></button><button class="capability-chip" data-open-capabilities="mcp">MCP ready <strong>${readyMCPTools}</strong></button></div></div><div class="chat-state">${pill(session.state, session.state === "active" ? "green" : "amber")}${pill(session.model,"blue")}</div>${contextBar}</header>
        <div class="message-list" id="messageList">${timeline.length ? groupTimeline(timeline).map(renderTimelineItem).join("") : `<div class="chat-welcome"><img src="/assets/brand/hermetrix-mark-flat.svg" alt=""><h3>Hermetrix is ready</h3><p>อธิบายสิ่งที่ต้องการให้ช่วยได้เลย คุณจะเห็นคำตอบและผลการใช้เครื่องมือที่นี่</p></div>`}${questions.map(elicitationCardHTML).join("")}<article class="chat-message assistant streaming ${state.sending ? "" : "hidden"}" id="streamingAssistant"><div class="message-role">Hermetrix</div><div class="message-body"></div><div class="message-proof" id="streamStatus">waiting for provider…</div></article></div>
        <form class="composer" id="chatForm"><div class="composer-tools"><button type="button" class="composer-tool-button" id="composerCapabilityButton">${uiIcon("plus")}<span>Skills & tools</span></button><button type="button" class="composer-tool-button" id="composerFilesButton">${uiIcon("files")}<span>Files</span></button><button type="button" class="composer-tool-button" id="composerTerminalButton">${uiIcon("terminal")}<span>Terminal</span></button><button type="button" class="composer-tool-button" id="composerImageButton" ${activeSessionSupportsVision() ? "" : "disabled"} title="${activeSessionSupportsVision() ? "Attach a qualified image" : "This session has no qualified image runtime"}">${uiIcon("artifact")}<span>Image</span></button><input hidden id="composerImageInput" type="file" multiple accept="image/png,image/jpeg,image/webp"><span class="composer-context">${escapeHTML(projectName)} · ${escapeHTML(session.context_profile)}</span></div><div class="action-row" id="composerAttachments">${composerAttachmentHTML()}</div><textarea id="chatInput" rows="2" maxlength="1048576" placeholder="Ask Hermetrix to work…  Enter sends, drop or paste images to attach" ${state.sending ? "disabled" : ""}></textarea><button class="primary" ${state.sending ? "disabled" : ""}>${state.sending ? "Running…" : "Send"}</button></form>` : `<div class="task-start">
          <div class="task-start-heading"><img src="/assets/brand/hermetrix-mark-flat.svg" alt=""><p class="eyebrow">${escapeHTML(state.currentProject?.name || "Hermetrix")}</p><h1>วันนี้ให้ช่วยทำอะไร?</h1><p>คุยเพื่อหาคำตอบ หรือวางแผนงานเป็นขั้นตอนก่อนเริ่มแก้ไข</p></div>
          <form class="start-composer" id="startTaskForm">
            <label class="sr-only" for="startPrompt">เป้าหมายของงาน</label><textarea id="startPrompt" rows="4" maxlength="1048576" placeholder="อธิบายงาน ปัญหา หรือสิ่งที่อยากปรับปรุง…">${escapeHTML(state.startDraft || "")}</textarea>
            <div class="start-composer-footer"><button type="button" class="model-choice" id="startModelOptions">${uiIcon("model")}${escapeHTML(draftProvider?.name || "เลือกโมเดล")} <span>⌄</span></button><div class="action-row"><button type="button" class="ghost" id="startPlanButton">${uiIcon("activity")} วางแผนงาน</button><button class="primary" id="startChatButton" ${canStart && !state.startingSession ? "" : "disabled"}>${state.startingSession ? "กำลังเริ่ม…" : "เริ่มคุย →"}</button></div></div>
          </form>
          <div class="start-readiness" role="status">${canStart ? `${pill("พร้อมเริ่ม", "green")}<span>${escapeHTML(draftProvider.model)} · ${escapeHTML(profileLabel(draftProfile))}${needsOverride ? " · ใช้ข้อยกเว้น 24 ชั่วโมงตามเหตุผลในตัวเลือก" : ""}</span>` : `<span>ยังเริ่มคุยไม่ได้: ${draftProvider && !draftProvider.credential_ready ? "โมเดลนี้ยังไม่มี API key" : "กรุณาตั้งค่าโมเดลและขนาดบริบท"}</span><button class="ghost" type="button" id="openProvidersButton">ตั้งค่าโมเดล</button>`}</div>
          ${state.sessionError ? `<p class="session-error" role="alert">${escapeHTML(state.sessionError)}</p>` : ""}
          <div class="start-suggestions"><button data-start-suggestion="ช่วยอธิบายโครงสร้างโปรเจกต์นี้ และแนะนำจุดเริ่มต้น">${uiIcon("files")}<span>เข้าใจโปรเจกต์<small>สำรวจโครงสร้างและจุดเริ่มต้น</small></span></button><button data-start-suggestion="ช่วยหาสาเหตุของปัญหา และเสนอวิธีตรวจสอบก่อนแก้ไข">${uiIcon("search")}<span>ตรวจปัญหา<small>หาต้นเหตุพร้อมวิธีพิสูจน์</small></span></button><button data-start-plan="ปรับปรุงโปรเจกต์นี้ โดยแบ่งเป็นขั้นตอนเล็ก ๆ พร้อมเกณฑ์ตรวจรับ">${uiIcon("activity")}<span>วางแผนปรับปรุง<small>แบ่งงานและเกณฑ์ตรวจรับ</small></span></button><button data-start-suggestion="ช่วยตรวจโค้ดส่วนที่ควรปรับปรุง โดยเรียงตามผลกระทบและความเสี่ยง">${uiIcon("review")}<span>ตรวจโค้ด<small>ดูจุดเสี่ยงและแนวทางแก้</small></span></button></div>
          <p class="start-hint">แผนงานจะแสดงข้อเสนอและรายการตรวจให้ทบทวนก่อนลงมือ · ไม่จำเป็นต้องเพิ่ม Skill เพื่อเริ่มคุย</p>
        </div>`}
    </section></div>`;
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
  $("#startPrompt")?.addEventListener("input", event => { state.startDraft = event.target.value; });
  $("#startTaskForm")?.addEventListener("submit", startChatFromGoal);
  $("#startPlanButton")?.addEventListener("click", () => { state.taskDraftObjective = $("#startPrompt").value; openTasks(); });
  $("#startModelOptions")?.addEventListener("click", openSessionSetup);
  $$("[data-start-suggestion]", root).forEach(button => button.addEventListener("click", () => { state.startDraft = button.dataset.startSuggestion; $("#startPrompt").value = state.startDraft; $("#startPrompt").focus(); }));
  $$("[data-start-plan]", root).forEach(button => button.addEventListener("click", () => { state.taskDraftObjective = button.dataset.startPlan; openTasks(); }));
  bindComposer();
  $("#chatForm")?.addEventListener("submit", sendTurn);
  $("#openProvidersButton")?.addEventListener("click", () => switchTab("providers"));
  $("#checklistNewSession")?.addEventListener("click", () => $("#railNewSession")?.click());
  $("#checklistOpenSkills")?.addEventListener("click", () => switchTab("library"));
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
  return `<button class="new-chat" id="railNewSession">${uiIcon("plus")}<span>New session</span></button>
    <div class="session-dock" id="sessionDock" aria-label="Agent sessions"></div>
    <div class="rail-footer">
      <div class="runtime-card">
        <img class="runtime-mark" src="/assets/brand/hermetrix-mark-flat.svg" alt="">
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
  return `${paneToolbarHTML()}
    <div class="workspace-pane-host" id="workspacePaneHost" aria-label="Resizable workspace panes"></div>`;
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
    label: "Workspace",
    rail: () => renderChatRail(),
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
let sessionSetupReturnFocus = "#startModelOptions";
function openSessionSetup(event) {
  sessionSetupReturnFocus = event?.currentTarget?.id === "sessionSetup" ? "#sessionSetup" : "#startModelOptions";
  dismissMobileRail();
  const dialog = $("#sessionSetupDialog");
  if (!dialog.open) dialog.showModal();
  $("#chatProviderSelect")?.focus();
}

function closeSessionSetup() { $("#sessionSetupDialog")?.close(); }

function dismissMobileRail() {
  $("#zones")?.classList.remove("mobile-rail-open");
  $("#toggleRail")?.setAttribute("aria-expanded", "false");
}

function wireChatSkeleton() {
  const newTask = $("#railNewSession");
  if (!newTask) return;
  newTask.onclick = () => {
    if (state.sending || state.startingSession) return;
    switchTab("chat");
    if (state.view !== "chat") switchView("chat");
    if (!beginNewTaskDraft()) return;
    dismissMobileRail();
    collapseZone("side", true);
    renderChat();
    $("#startPrompt")?.focus();
  };
}

// applyLayoutForView restores this project's memory of the view already on
// screen (widths, panes, maximised pane, rail/side collapse) and then
// enforces the one rule memory can never override: a view with nothing to
// put in its side pane still has nothing to show, whatever was saved for
// it. Both entry paths into the shell go through this one function --
// switchView's later transitions, and openProject's first paint, which is
// reachable from the picker at any view because the project chip works
// everywhere, not only from Chat -- so neither path can drift from the
// other's rule the way openProject once did.
function applyLayoutForView() {
  applyLayout();
  if (!VIEWS[state.view].side()) collapseZone("side", true);
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
let navigationGeneration = 0;
function restorePlanningWorkspace() {
  const saved = state.workspaceBeforePlans;
  if (!saved) return false;
  state.workspaceBeforePlans = null;
  if (saved.projectID !== state.currentProject?.id || state.view !== "code") return false;
  state.panes = saved.panes;
  state.paneLayout = saved.paneLayout;
  state.maximisedPane = saved.maximisedPane;
  state.compactPane = saved.compactPane;
  saveLayout();
  return true;
}

function switchView(name) {
  ++navigationGeneration;
  captureWorkspaceDrafts();
  captureCodeDraft();
  dismissMobileRail();
  const restoredWorkspace = restorePlanningWorkspace();
  const view = VIEWS[name] ? name : "chat";
  if (view === "code" && state.compactPane === "tasks") state.compactPane = state.projectFile ? "editor" : "files";
  $("#plansViewButton")?.classList.remove("on");
  $$("[data-open-tasks]").forEach(button => button.classList.remove("active"));
  $$("[data-rail-view]").forEach(button => button.classList.toggle("active", button.dataset.railView === view));
  $$("#viewSwitch [data-view]").forEach(button => button.classList.toggle("on", button.dataset.view === view));
  if (view === state.view) {
    if (restoredWorkspace) { renderPanes(); return; }
    if (view === "code" && state.maximisedPane !== null && state.panes[state.maximisedPane] === "tasks") {
      state.maximisedPane = null;
      renderPanes();
    }
    return;
  }
  state.view = view;
  // A terminal or team run polls on its own timer independent of any render
  // call. Leaving chat tears down the workbench pane those polls write into,
  // so the timer is cancelled here rather than left to throw on a pane that
  // no longer exists.
  stopWorkbenchPolling();
  // Chat and Code share one global project/session rail. Changing the centre
  // view must not replace the user's navigation with a different tool list.
  if (!["chat", "code"].includes(view) || !$("#sessionDock")) {
    $("#zoneRail").innerHTML = VIEWS[view].rail();
  }
  $$("[data-rail-view]").forEach(button => button.classList.toggle("active", button.dataset.railView === view));
  $("#zoneMain").innerHTML = VIEWS[view].main();
  $("#zoneSide").innerHTML = VIEWS[view].side();
  // Widths, panes and side-collapse are this project's memory of the view
  // being entered, not a rule that forces the side pane open every time chat
  // comes back -- that rule used to live here and it meant a side someone had
  // deliberately collapsed reopened itself the moment they returned from
  // another view. applyLayoutForView() carries both halves of that fix: the
  // restore, and the guard that a view with nothing to put in its side pane
  // still has nothing to show regardless of what was saved -- the same
  // guard openProject needs for the identical reason, so it lives in one
  // place rather than two copies that could drift.
  applyLayoutForView();
  if (view === "chat") {
    wireChatSkeleton();
    renderChat();
    if (!$("#zones").classList.contains("side-hidden")) renderPanes();
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

function activeSessionSupportsVision() {
  const providerID = state.sessionDetail?.session?.provider_id;
  const provider = state.providers.find(item => item.id === providerID);
  if (!provider?.runtime_fingerprint_id) return false;
  return state.qualifications.some(run => run.provider_id === providerID && run.state === "completed" && run.eligible &&
    run.runtime_fingerprint_id === provider.runtime_fingerprint_id && (run.modalities || []).includes("image"));
}

function composerAttachmentHTML() {
  return state.composerAttachments.map(item => `<span class="pill ${item.state === "ready" ? "green" : item.state === "failed" ? "red" : "amber"}">
    ${escapeHTML(item.name)} · ${escapeHTML(item.state)}${item.state !== "uploading" ? ` <button type="button" data-remove-composer-image="${escapeHTML(item.local_id)}" aria-label="Remove image">×</button>` : ""}</span>`).join("");
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

  // Only a provider/runtime pair with completed image qualification exposes
  // this input. Upload and processing state stays separate from turn state.
  const attachComposerImages = async files => {
    if (!activeSessionSupportsVision()) { toast("This session has no qualified image runtime", true); return; }
    const sessionID = state.sessionDetail?.session?.id;
    const projectID = state.currentProject?.id;
    const stillSelected = () => state.sessionDetail?.session?.id === sessionID && state.currentProject?.id === projectID;
    const images = [...files].filter(file => ["image/png","image/jpeg","image/webp"].includes(file.type));
    if (!images.length) { if (files.length) toast("Only PNG, JPEG, or WebP images are accepted", true); return; }
    const remaining = Math.max(0, 5 - state.composerAttachments.length);
    for (const file of images.slice(0, remaining)) {
      if (!stillSelected()) break;
      if (file.size > 8 * 1024 * 1024) { toast(`${file.name || "รูป"}: เกิน 8 MiB`, true); continue; }
      const pending = {local_id:`upload-${Date.now()}-${Math.random()}`,name:file.name || "image",state:"uploading",artifact_id:""};
      state.composerAttachments.push(pending); renderChat();
      try {
        const form = new FormData(); form.append("file",file,file.name); form.append("project_id",projectID || ""); form.append("session_id",sessionID || "");
        const response = await fetch("/api/media/uploads",{method:"POST",body:form});
        const artifact = await response.json().catch(() => ({})); if(!response.ok) throw new Error(artifact.error || `Upload failed (${response.status})`);
        pending.state="ready"; pending.artifact_id=artifact.id; pending.name=artifact.name; if (stillSelected()) toast(`${pending.name} is ready as image evidence`);
      } catch (error) { pending.state="failed"; pending.error=error.message; if (stillSelected()) toast(error.message,true); }
      if (stillSelected()) renderChat();
    }
  };
  $("#composerImageButton")?.addEventListener("click",()=>$("#composerImageInput")?.click());
  $("#composerImageInput")?.addEventListener("change",event=>void attachComposerImages(event.target.files || []));
  $$('[data-remove-composer-image]').forEach(button=>button.addEventListener("click",()=>{state.composerAttachments=state.composerAttachments.filter(item=>item.local_id!==button.dataset.removeComposerImage);renderChat();}));
  input.addEventListener("paste", event => {
    if (event.clipboardData?.files?.length) {
      event.preventDefault();
      void attachComposerImages(event.clipboardData.files);
    }
  });
  const composerForm = $("#chatForm");
  composerForm?.addEventListener("dragover", event => {
    if ([...event.dataTransfer.types].includes("Files")) {
      event.preventDefault();
      composerForm.classList.add("drop-target");
    }
  });
  composerForm?.addEventListener("dragleave", () => composerForm.classList.remove("drop-target"));
  composerForm?.addEventListener("drop", event => {
    composerForm.classList.remove("drop-target");
    if (event.dataTransfer?.files?.length) {
      event.preventDefault();
      void attachComposerImages(event.dataTransfer.files);
    }
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
async function startChatFromGoal(event) {
  event.preventDefault();
  if (state.startingSession || state.sending || !state.sessionReady) return;
  const goal = $("#startPrompt")?.value.trim();
  if (!goal) { $("#startPrompt")?.focus(); return; }
  state.startDraft = goal;
  state.startingSession = true;
  renderChat();
  try {
    const session = await createAgentSession(goal);
    if (!session) return;
    state.startDraft = "";
    const ok = await submitChatText(goal);
    if (!ok) { setComposerDraft(goal); renderChat(); }
  } finally { state.startingSession = false; renderChat(); }
}

async function openTasks() {
  if (state.sessionCreationPending) { toast("กำลังเริ่มแชท กรุณารอสักครู่"); return; }
  const projectID = state.currentProject?.id;
  const generation = ++navigationGeneration;
  const trigger = $("#plansViewButton");
  trigger?.setAttribute("aria-busy", "true");
  try {
    const hydrated = await hydrateSurface("tasks", true);
    if (!hydrated || generation !== navigationGeneration || projectID !== state.currentProject?.id) return;
    switchTab("chat");
    switchView("code");
    // Plans uses the whole workspace temporarily; returning must restore
    // the user's arrangement rather than leaving an extra Tasks split.
    state.workspaceBeforePlans = {projectID, panes:state.panes.slice(), paneLayout:state.paneLayout,
      maximisedPane:state.maximisedPane, compactPane:state.compactPane};
    openContentPane("tasks", {persist:false});
    state.maximisedPane = state.panes.indexOf("tasks");
    renderPanes();
    $$("#viewSwitch [data-view]").forEach(button => button.classList.remove("on"));
    $("#plansViewButton")?.classList.add("on");
    $$(".rail-nav-item").forEach(button => button.classList.toggle("active", button.hasAttribute("data-open-tasks")));
    $("#durableTaskForm textarea[name='objective']")?.focus();
  } catch (error) { if (generation === navigationGeneration && projectID === state.currentProject?.id) toast(error.message, true); }
  finally { trigger?.removeAttribute("aria-busy"); }
}

function captureComposer() {
  const input = $("#chatInput");
  if (!input) return;
  state.composerFocused = document.activeElement === input;
  state.composerCaret = input.selectionStart;
  state.draftMessage = input.value;
}

const sessionDrafts = new Map();
const projectDrafts = new Map();

function captureWorkspaceDrafts() {
  captureComposer();
  const projectID = state.currentProject?.id;
  if (projectID) {
    projectDrafts.set(projectID, {startDraft:state.startDraft || "", taskDraftObjective:state.taskDraftObjective || "",
      taskDraftDetails:{...(state.taskDraftDetails || {})}});
  }
  const sessionID = state.sessionDetail?.session?.id;
  if (sessionID) {
    sessionDrafts.set(sessionID, {text:state.draftMessage || "", attachments:state.composerAttachments.slice(),
      caret:state.composerCaret || 0, paneText:state.paneChatDraft || ""});
  }
}

function restoreWorkspaceDrafts(projectID, sessionID = null) {
  const project = projectDrafts.get(projectID) || {};
  const session = sessionDrafts.get(sessionID) || {};
  state.startDraft = project.startDraft || "";
  state.taskDraftObjective = project.taskDraftObjective || "";
  state.taskDraftDetails = {...(project.taskDraftDetails || {})};
  state.draftMessage = session.text || "";
  state.composerAttachments = (session.attachments || []).slice();
  state.composerCaret = session.caret || 0;
  state.composerFocused = false;
  state.paneChatDraft = session.paneText || "";
  // renderChat captures the old mounted textarea before replacing it. Keep
  // that DOM value aligned with the newly selected draft before rendering.
  const input = $("#chatInput");
  if (input) input.value = state.draftMessage;
  const start = $("#startPrompt");
  if (start) start.value = state.startDraft;
}

function setComposerDraft(text, attachments = state.composerAttachments, sessionID = state.sessionDetail?.session?.id) {
  if (sessionID) sessionDrafts.set(sessionID, {...sessionDrafts.get(sessionID), text, attachments:attachments.slice()});
  if (sessionID !== state.sessionDetail?.session?.id) return;
  state.draftMessage = text;
  state.composerAttachments = attachments.slice();
  const input = $("#chatInput");
  if (input) input.value = text;
}

function beginNewTaskDraft() {
  if (state.sending || state.startingSession) return false;
  captureWorkspaceDrafts();
  ++sessionSelectionGeneration;
  state.sessionDetail = null;
  state.selectedSession = null;
  state.elicitations = [];
  restoreWorkspaceDrafts(state.currentProject?.id);
  state.startDraft = "";
  const start = $("#startPrompt");
  if (start) start.value = "";
  return true;
}

async function createAgentSession(title = "") {
  if (state.sessionCreationPending || !state.draftProviderID || !state.draftProfileName) return;
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
  state.sessionCreationPending = true;
  ++sessionSelectionGeneration;
  ++navigationGeneration;
  try {
    state.sessionError = "";
    const body = {
      provider_id: state.draftProviderID,
      project_id: projectID,
      context_profile: state.draftProfileName,
      // Name the session after what it is bound to, so the list is readable
      // once more than one session exists.
      title: title.slice(0, 100) || (project ? `${project.name} · ${profileLabel(profile)}` : `Chat · ${profileLabel(profile)}`)
    };
    if (admission.mode === "override_required") {
      const reason = (state.draftQualificationReason.trim() || suggestedOverrideReason(provider, profile));
      body.qualification_override = { actor:currentActor(), reason };
    }
    const session = await api("/api/sessions", { method:"POST", body:JSON.stringify(body) });
    state.draftQualificationReason = "";
    await load();
    if (!await selectSession(session.id, {createdSession:true})) {
      state.sessionError = "สร้างแชทแล้ว แต่โหลดรายละเอียดไม่สำเร็จ เลือกแชทจากแถบด้านข้างเพื่อดำเนินการต่อ ข้อความของคุณยังอยู่";
      renderChat();
      return;
    }
    switchTab("chat");
    $("#chatInput")?.focus();
    return session;
  } catch (error) {
    // Keep the failure on screen. A 2.6-second toast was the only report that
    // the server had refused the session, and it was routinely missed.
    state.sessionError = error.message;
    renderChat();
  } finally {
    state.sessionCreationPending = false;
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

async function selectSession(id, options = {}) {
  if (state.sessionCreationPending && !options.createdSession && !options.refresh) { toast("กำลังสร้างแชท กรุณารอสักครู่", true); return; }
  const activeTurn = state.turnSessionID || state.sessionDetail?.session?.id;
  if (state.sending && id !== activeTurn) { toast("รอให้แชทนี้ตอบเสร็จก่อนเปลี่ยนแชท", true); return; }
  if (!options.refresh) ++navigationGeneration;
  const generation = ++sessionSelectionGeneration;
  captureWorkspaceDrafts();
  try {
    const detail = await api(`/api/sessions/${encodeURIComponent(id)}`);
    if (generation !== sessionSelectionGeneration) return;
    if (state.sending && id !== (state.turnSessionID || state.sessionDetail?.session?.id)) return;
    if (!detail?.session || detail.session.id !== id) throw new Error("โหลดรายละเอียดแชทไม่สำเร็จ กรุณาเลือกแชทอีกครั้ง");
    const projectID = detail.session.project_id;
    if (projectID && projectID !== state.currentProject?.id) {
      const project = await openProject(projectID, {selectSession:false, selectionGeneration:generation, createdSession:options.createdSession});
      if (!project || generation !== sessionSelectionGeneration || state.currentProject?.id !== projectID) return;
    }
    captureWorkspaceDrafts();
    state.selectedSession = id;
    state.selectedSkillDetail = null;
    state.sessionDetail = detail;
    state.draftProjectID = projectID || state.currentProject?.id;
    state.sessionError = "";
    state.elicitations = [];
    restoreWorkspaceDrafts(state.currentProject?.id, id);
    renderChat();
    if (state.panes.includes("review") && !$("#zones").classList.contains("side-hidden")) renderPanes();
    return state.sessionDetail;
  } catch (error) {
    if (generation !== sessionSelectionGeneration) return;
    state.sessionError = error.message;
    renderChat();
    toast(error.message, true);
  }
}

async function sendTurn(event) {
  event.preventDefault();
  if (state.sending || !state.sessionDetail?.session) return;
  const input = $("#chatInput");
  const content = input.value.trim();
  const sessionID = state.sessionDetail.session.id;
  const attachments = state.composerAttachments.slice();
  if (!content && !attachments.some(item => item.state === "ready")) return;
  if (attachments.some(item => item.state !== "ready")) { toast("Remove failed images or wait for every upload to become ready", true); return; }
  // Clear the mounted box BEFORE renderChat: renderChat calls
  // captureComposer, which would otherwise read the old DOM value back into
  // state.draftMessage and resurrect the text in the fresh markup.
  input.value = "";
  state.draftMessage = "";
  state.composerCaret = 0;
  state.composerFocused = true;
  const ok = await submitChatText(content, attachments);
  // The turn never committed, so hand the text back instead of eating it.
  if (!ok) setComposerDraft(content, attachments, sessionID);
  else setComposerDraft("", [], sessionID);
  if (state.sessionDetail?.session?.id === sessionID) renderChat();
}

// submitChatText is the shared turn pipeline behind both composers: the full
// Chat view box and the Workspace chat pane. It touches no DOM input itself —
// callers own their draft — and every render it triggers is a safe no-op in a
// view whose zone is not mounted. Returns true when the turn committed.
async function submitChatText(content, attachments = []) {
  if (state.sending || !state.sessionDetail?.session) return false;
  const sessionID = state.sessionDetail.session.id;
  state.turnSessionID = sessionID;
  state.sending = true;
  renderChat();
  refreshPaneChat();
  setTimeout(pollElicitations, 600);
  try {
    const payload = attachments.length ? {parts:[...(content ? [{kind:"text",text:content}] : []),...attachments.map(item=>({kind:"image",artifact_id:item.artifact_id,metadata:{name:item.name}}))]} : {content};
    const response = await fetch(`/api/sessions/${encodeURIComponent(sessionID)}/turns`, { method:"POST", headers:{"Content-Type":"application/json"}, body:JSON.stringify(payload) });
    if (!response.ok) {
      const body = await response.json().catch(() => ({}));
      throw new Error(body.error || `Turn failed (${response.status})`);
    }
    await consumeAgentStream(response);
    await selectSession(sessionID, {refresh:true});
    return true;
  } catch (error) {
    toast(error.message, true);
    await selectSession(sessionID, {refresh:true}).catch(() => {});
    return false;
  } finally {
    state.sending = false;
    state.turnSessionID = null;
    state.elicitations = [];
    renderChat();
    refreshPaneChat();
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
      body:JSON.stringify({ actor:currentActor(), decision, reason:decision === "deny" ? response : "approved after preview" })
    });
    if (!stream.ok) {
      const body = await stream.json().catch(() => ({}));
      throw new Error(body.error || `Approval failed (${stream.status})`);
    }
    await consumeAgentStream(stream);
    await selectSession(sessionID, {refresh:true});
    toast(decision === "approve" ? "Exact write approved and receipt committed" : "Write denied; Hermetrix continued without mutation");
  } catch (error) {
    toast(error.message, true);
    await selectSession(sessionID, {refresh:true}).catch(() => {});
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
      window.HermetrixAssistant?.onStream(item);
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
  const hero = `<section class="capability-hero"><div class="capability-hero-head"><div><p class="eyebrow">Models</p><h3>${list.length ? `${list.length} model${list.length === 1 ? "" : "s"} connected` : "No model connected yet"}</h3><p>Connect OpenAI-compatible gateways or native Anthropic and Gemini endpoints. Credentials remain isolated per profile.</p></div><div class="capability-hero-metrics">${metrics.map(([value, label]) => `<div class="capability-metric"><strong>${escapeHTML(value)}</strong><span>${escapeHTML(label)}</span></div>`).join("")}</div></div></section>`;
  const cards = list.length ? list.map(provider => `<article class="provider-card"><div class="provider-head"><div><div class="row-title"><h3>${escapeHTML(provider.name)}</h3>${pill(provider.adapter_kind, "blue")}${pill(provider.enabled ? "enabled" : "disabled", provider.enabled ? "green" : "amber")}${pill(provider.context_evidence, provider.context_evidence === "qualified" ? "green" : "amber")}</div><p>${escapeHTML(provider.base_url)}</p></div>${pill(provider.credential_stored ? "key saved" : provider.api_key_env ? "key from environment" : "no key set", provider.credential_stored || provider.api_key_env ? "green" : "amber")}</div><div class="kv"><span>Model</span><strong>${escapeHTML(provider.model)}</strong><span>Context</span><strong>${provider.context_window.toLocaleString()}</strong><span>Output</span><span>${provider.max_output_tokens.toLocaleString()}</span><span>Key source</span><span>${provider.credential_stored ? "saved on this machine" : provider.api_key_env ? `environment · ${escapeHTML(provider.api_key_env)}` : "none required"}</span></div><div class="action-row"><button class="ghost" data-provider-key="${escapeHTML(provider.id)}">${provider.credential_stored ? "Replace API key" : "Set API key"}</button><button class="ghost" data-test-provider="${escapeHTML(provider.id)}" title="${provider.credential_ready ? "Send one cheap request to prove the endpoint, model and key work together" : "Set the API key first — a provider without credentials cannot be tested"}" ${provider.credential_ready ? "" : "disabled"}>Test connection</button><button class="primary" data-qualify-provider="${escapeHTML(provider.id)}" title="${provider.credential_ready ? "Run the full context-tier and capability-grade suite" : "Set the API key first — qualification needs a working credential"}" ${provider.credential_ready ? "" : "disabled"}>Full qualification</button></div></article>`).join("") : `<div class="empty"><h3>No model connected</h3><p>Open the Model registry panel and connect one. A hosted endpoint needs its API key; a local runtime usually needs none.</p></div>`;
  const flow = `<div class="panel"><p class="eyebrow">How connecting works</p><div class="tool-flow">${MODEL_FLOW.map(([title, detail], index) => `<article><b>${index + 1}</b><div><strong>${escapeHTML(title)}</strong><small>${escapeHTML(detail)}</small></div></article>`).join("")}</div></div>`;
  const setup = `<details class="panel connection-setup" ${list.length ? "" : "open"}><summary><div><p class="eyebrow">Model registry</p><h3>Connect a model</h3></div></summary><div class="connection-setup-body"><form id="providerForm">
      <label>Name<input name="name" required maxlength="80" placeholder="OpenAI, my local gateway…"></label>
      <label>Protocol<select name="adapter_kind"><option value="openai-compatible">OpenAI-compatible</option><option value="anthropic-native">Anthropic native</option><option value="gemini-native">Gemini native</option></select></label>
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
    await api("/api/providers", { method:"POST", body:JSON.stringify({ name:values.get("name"), adapter_kind:values.get("adapter_kind"), base_url:values.get("base_url"), model:values.get("model"), api_key:key, api_key_env:values.get("api_key_env"), context_window:Number(values.get("context_window")), context_evidence:"declared", max_output_tokens:Number(values.get("max_output_tokens")) }) });
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

function directToolGroup(name) {
  if (name.startsWith("workspace.") && name !== "workspace.run") return "ไฟล์";
  if (name === "workspace.run") return "รันคำสั่ง";
  if (name === "browser") return "เว็บ";
  if (name.startsWith("skill_") || name.startsWith("context_")) return "ความรู้และสกิล";
  if (name.startsWith("tool_")) return "ค้นหาเครื่องมือ";
  return "อื่น ๆ";
}

function renderDirectTools() {
  const root = $("#view-tools");
  if (!root) return;
  const tools = asList(state.direct_tools);
  const groups = ["ไฟล์", "รันคำสั่ง", "เว็บ", "ความรู้และสกิล", "ค้นหาเครื่องมือ", "อื่น ๆ"];
  root.innerHTML = `<div class="direct-tools-head"><h2>เครื่องมือในตัว <small>${tools.length}</small></h2><p>เครื่องมือที่ Hermetrix มีให้ใช้งาน รายการและเงื่อนไขการอนุมัติมาจาก registry ปัจจุบันของเซิร์ฟเวอร์</p></div>
    <label class="direct-tools-search">${uiIcon("search")}<span class="sr-only">ค้นหาเครื่องมือ</span><input id="directToolQuery" type="search" placeholder="ค้นหาเครื่องมือ…" value="${escapeHTML(state.directToolQuery || "")}"></label>
    <div id="directToolGroups">${groups.map(group => {
      const members = tools.filter(item => directToolGroup(item.name) === group);
      return members.length ? `<section class="direct-tool-group" data-tool-group><h3>${group} <small>${members.length} รายการ</small></h3><div class="direct-tool-grid">${members.map(tool => {
        const name = String(tool.name || "");
        const short = name.split(".").pop().slice(0, 2);
        return `<details class="direct-tool-card" data-tool-text="${escapeHTML(`${name} ${tool.description || ""}`.toLowerCase())}"><summary><span class="direct-tool-avatar" aria-hidden="true">${escapeHTML(short)}</span><span><strong>${escapeHTML(name)}</strong><small>${escapeHTML(tool.description || "ไม่มีคำอธิบาย")}</small></span></summary><div class="direct-tool-detail"><div class="meta">${pill(tool.effect || "read", tool.requires_approval ? "amber" : "green")}${tool.requires_approval ? pill("ต้องอนุมัติ", "amber") : ""}</div><pre>${escapeHTML(JSON.stringify(tool.parameters || {}, null, 2))}</pre><button class="ghost" type="button" data-use-direct-tool="${escapeHTML(name)}">ใช้ในแชท</button></div></details>`;
      }).join("")}</div></section>` : "";
    }).join("")}</div><p id="directToolEmpty" class="probe-empty" hidden>${tools.length ? "ไม่พบเครื่องมือที่ตรงกับคำค้น" : "ยังไม่มีเครื่องมือใน registry"}</p>`;
  const filter = () => {
    const query = (state.directToolQuery || "").trim().toLowerCase();
    let visible = 0;
    $$('[data-tool-group]', root).forEach(section => {
      let sectionVisible = 0;
      $$('.direct-tool-card', section).forEach(card => {
        card.hidden = Boolean(query && !card.dataset.toolText.includes(query));
        if (!card.hidden) sectionVisible++;
      });
      section.hidden = sectionVisible === 0;
      visible += sectionVisible;
    });
    $("#directToolEmpty").hidden = visible > 0;
  };
  $("#directToolQuery").addEventListener("input", event => { state.directToolQuery = event.target.value; filter(); });
  $$('[data-use-direct-tool]', root).forEach(button => button.addEventListener("click", () => mentionCapability({kind:"tool", name:button.dataset.useDirectTool})));
  filter();
}

function renderMCP() {
  const root = $("#view-mcp");
  if (!root) return;
  const summary = state.capability_summary || { total:0, by_source:{}, by_readiness:{} };
  const servers = state.mcp_servers;
  const untrusted = servers.filter(server => !server.trust_annotations).length;
  const metrics = [
    [Number(summary.total || 0).toLocaleString(), "indexed capabilities"],
    [Number(summary.by_readiness?.ready || 0).toLocaleString(), "ready"],
    [servers.length.toLocaleString(), "connections"],
    [untrusted.toLocaleString(), "approval by default"]
  ];
  const hero = `<section class="capability-hero mcp-hero"><div class="capability-hero-head"><div><p class="eyebrow">เครื่องมือและการเชื่อมต่อ</p><h3>MCP server</h3><p>เชื่อมเซิร์ฟเวอร์เครื่องมือ ค้นหาความสามารถที่พร้อมใช้ และตรวจสถานะจากข้อมูลจริงของเครื่องนี้</p></div><div class="capability-hero-metrics">${metrics.map(([value, label]) => `<div class="capability-metric"><strong>${escapeHTML(value)}</strong><span>${escapeHTML(label)}</span></div>`).join("")}</div></div></section>`;
  const selected = state.selectedCapability ? `<article class="provider-card capability-detail"><div class="provider-head"><div><div class="row-title"><h3>${escapeHTML(state.selectedCapability.title || state.selectedCapability.name)}</h3>${pill(state.selectedCapability.effect, state.selectedCapability.requires_approval ? "amber" : "green")}${pill(state.selectedCapability.readiness, state.selectedCapability.readiness === "ready" ? "green" : "red")}</div><p>${escapeHTML(state.selectedCapability.description || "No description supplied by MCP server")}</p></div>${pill(state.selectedCapability.source)}</div><div class="kv"><span>Capability ID</span><code>${escapeHTML(state.selectedCapability.id)}</code><span>Revision</span><code>${escapeHTML(state.selectedCapability.revision)}</code><span>Source ref</span><code>${escapeHTML(state.selectedCapability.source_ref)}</code><span>Approval</span><strong>${state.selectedCapability.requires_approval ? "required" : "not required"}</strong></div><section class="inspect-section"><h3>Exact input schema</h3><pre>${escapeHTML(JSON.stringify(state.selectedCapability.input_schema, null, 2))}</pre></section><p class="form-note neutral">This schema is loaded on demand. It is not part of the direct model prompt until tool_describe is called.</p><div class="action-row"><button class="primary" type="button" data-use-capability="${escapeHTML(state.selectedCapability.id)}">Use in chat</button></div></article>` : `<div class="probe-empty">Search the catalog, then open a result to read its exact revision and schema.</div>`;
  const search = `<div class="panel mcp-capability-panel"><div class="provider-head"><div><p class="eyebrow">รายการเครื่องมือ</p><h3>ค้นหาความสามารถที่เซิร์ฟเวอร์เผยแพร่</h3></div>${pill(`${summary.by_readiness?.ready || 0} ready`, "green")}</div>
      <form id="capabilitySearchForm" class="capability-search"><label class="sr-only" for="capabilityQuery">ค้นหาเครื่องมือ</label><input id="capabilityQuery" required value="${escapeHTML(state.mcpCapabilityQuery)}" placeholder="เช่น calendar, repository search, database…"><button class="ghost">ค้นหา</button></form>
      <div id="capabilityResults">${state.capabilityResults.length ? state.capabilityResults.map(item => `<button class="capability-result" data-capability-id="${escapeHTML(item.id)}"><span><strong>${escapeHTML(item.title || item.name)}</strong><small>${escapeHTML(item.description || "No description")}</small></span><span>${pill(capabilityKind(item), "blue")}${pill(item.effect, item.requires_approval ? "amber" : "green")}${pill(item.readiness, item.readiness === "ready" ? "green" : "red")}</span></button>`).join("") : `<div class="probe-empty">Search returns bounded metadata only—never the complete catalog schemas.</div>`}</div>
      ${selected}</div>`;
  const serverTools = `<div class="mcp-server-tools"><label class="mcp-server-search">${uiIcon("search")}<span class="sr-only">ค้นหา MCP server</span><input id="mcpServerQuery" type="search" value="${escapeHTML(state.mcpServerQuery)}" placeholder="ค้นหา MCP server…" autocomplete="off"></label><div class="mcp-filters" role="group" aria-label="กรองสถานะเซิร์ฟเวอร์"><button type="button" data-mcp-filter="all" aria-pressed="${state.mcpServerFilter === "all"}">ทั้งหมด</button><button type="button" data-mcp-filter="ready" aria-pressed="${state.mcpServerFilter === "ready"}">พร้อมใช้</button><button type="button" data-mcp-filter="attention" aria-pressed="${state.mcpServerFilter === "attention"}">ต้องตรวจสอบ</button></div><button type="button" class="ghost" id="mcpDiscoverAll" ${servers.some(server => server.enabled && server.credential_ready) ? "" : "disabled"}>${uiIcon("refresh")} ค้นพบทั้งหมด</button></div>`;
  const serverList = `<div class="mcp-connection-head"><h3>ติดตั้งแล้ว <small>${servers.length}</small></h3>${pill(`${servers.filter(server => server.status === "ready").length}/${servers.length} ready`, servers.length && servers.every(server => server.status === "ready") ? "green" : "amber")}</div>
    <div class="card-list mcp-server-list">${servers.length ? servers.map(server => `<article class="provider-card"><div class="provider-head"><div><div class="row-title"><h3>${escapeHTML(server.name)}</h3>${pill(server.status, server.status === "ready" ? "green" : server.status === "error" ? "red" : "amber")}${pill(server.last_protocol || server.protocol_mode, "blue")}</div><p><code>${escapeHTML(server.endpoint)}</code></p></div>${pill(server.credential_stored ? "token saved" : server.api_key_env ? "token from environment" : "no token set", server.credential_stored || server.api_key_env ? "green" : "amber")}</div><div class="kv"><span>Runs as</span><strong>${server.transport_kind === "stdio" ? "local program" : "remote URL"}</strong><span>Publishes</span><strong>${escapeHTML(describeCatalog(server.id))}</strong><span>Timeout</span><span>${server.request_timeout_ms.toLocaleString()} ms</span><span>Risk hints</span><strong>${server.trust_annotations ? "trusted by user" : "untrusted · approval default"}</strong><span>Token source</span><span>${server.credential_stored ? "saved on this machine" : server.api_key_env ? `environment · ${escapeHTML(server.api_key_env)}` : "none required"}</span><span>Discovered</span><span>${formatDate(server.last_discovered_at)}</span></div>${server.last_error ? `<ul class="findings"><li class="error">${escapeHTML(server.last_error)}</li></ul>` : ""}<div class="action-row"><button class="ghost" data-mcp-key="${escapeHTML(server.id)}">${server.credential_stored ? "Replace token" : "Set token"}</button><button class="primary" data-discover-mcp="${escapeHTML(server.id)}" ${server.enabled && server.credential_ready ? "" : "disabled"}>Discover catalog</button></div></article>`).join("") : `<div class="empty"><h3>No MCP connections yet</h3><p>Connect one in the registry panel: most published MCP servers are a program you launch, such as <code>npx -y @modelcontextprotocol/server-everything</code>. Nothing reaches the model until you run discovery.</p></div>`}</div>`;
  const flow = `<details class="panel mcp-flow"><summary>การเชื่อมต่อและการอนุมัติทำงานอย่างไร</summary><div class="tool-flow">${TOOL_FLOW.map(([title, detail], index) => `<article><b>${index + 1}</b><div><strong>${escapeHTML(title)}</strong><small>${escapeHTML(detail)}</small></div></article>`).join("")}</div></details>`;
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
  root.innerHTML = `${hero}${serverTools}${serverList}<p class="probe-empty" id="mcpServerEmpty" hidden>ไม่พบเซิร์ฟเวอร์ที่ตรงกับคำค้นหรือสถานะที่เลือก</p>${search}<div class="mcp-setup-row">${setup}${flow}</div>`;
  $$(".mcp-server-list > article", root).forEach(card => {
    const details = document.createElement("details");
    details.className = "mcp-server-detail";
    details.innerHTML = "<summary>รายละเอียดและการจัดการ</summary>";
    for (const child of [...card.children].slice(1)) details.append(child);
    card.append(details);
  });
  const filterServers = () => {
    const query = state.mcpServerQuery.trim().toLowerCase();
    let visible = 0;
    $$(".mcp-server-list > article", root).forEach((card, index) => {
      const server = servers[index];
      card.hidden = Boolean((query && !`${server.name} ${server.endpoint} ${server.status}`.toLowerCase().includes(query)) ||
        (state.mcpServerFilter === "ready" && server.status !== "ready") ||
        (state.mcpServerFilter === "attention" && server.status === "ready"));
      if (!card.hidden) visible++;
    });
    $("#mcpServerEmpty").hidden = !servers.length || visible > 0;
    $$("[data-mcp-filter]", root).forEach(button => button.setAttribute("aria-pressed", String(button.dataset.mcpFilter === state.mcpServerFilter)));
  };
  $("#mcpServerQuery").addEventListener("input", event => { state.mcpServerQuery = event.target.value; filterServers(); });
  $$("[data-mcp-filter]", root).forEach(button => button.addEventListener("click", () => { state.mcpServerFilter = button.dataset.mcpFilter; filterServers(); }));
  $("#mcpDiscoverAll").addEventListener("click", discoverAllMCPServers);
  filterServers();
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
  $("#capabilityQuery")?.addEventListener("input", event => { state.mcpCapabilityQuery = event.target.value; });
  $$('[data-discover-mcp]', root).forEach(button => button.addEventListener("click", () => discoverMCPServer(button.dataset.discoverMcp)));
  $$('[data-capability-id]', root).forEach(button => button.addEventListener("click", () => inspectCapability(button.dataset.capabilityId)));
  $$('[data-use-capability]', root).forEach(button => button.addEventListener("click", () => {
    const capability = state.selectedCapability;
    if (capability) mentionCapability({ kind:"mcp", name:capability.title || capability.name, id:capability.id });
  }));
  $$('[data-mcp-key]', root).forEach(button => button.addEventListener("click", () => setMCPCredential(button.dataset.mcpKey)));
}

async function discoverAllMCPServers() {
  const button = $("#mcpDiscoverAll");
  const servers = state.mcp_servers.filter(server => server.enabled && server.credential_ready);
  button.disabled = true;
  button.textContent = "กำลังค้นพบ…";
  const failures = [];
  try {
    for (const server of servers) {
      try {
        await api(`/api/mcp/servers/${encodeURIComponent(server.id)}/discover`, { method:"POST", body:"{}" });
      } catch (error) {
        failures.push(`${server.name}: ${error.message}`);
      }
    }
    await load();
    switchTab("mcp");
    toast(failures.length ? `ค้นพบสำเร็จ ${servers.length - failures.length}/${servers.length} · ${failures.join(" · ")}` : `ค้นพบเซิร์ฟเวอร์ ${servers.length} รายการแล้ว`, Boolean(failures.length));
  } catch (error) {
    button.disabled = false;
    button.textContent = "ค้นพบทั้งหมด";
    toast(error.message, true);
  }
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
  state.mcpCapabilityQuery = query;
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

function projectSharingPanel(project) {
  if (!project) return "";
  const eligible = project.visibility === "project_shared" && project.export_policy === "explicit_selection";
  const artifacts = state.artifacts.filter(item => item.project_id === project.id);
  const tasks = state.durableTasks.filter(item => item.project_id === project.id);
  const skills = state.skills.filter(item => item.scope_kind === "project" && item.scope_ref === project.id);
  const memories = state.memories.filter(item => item.scope_kind === "project" && item.scope_ref === project.id && item.state === "active");
  const preview = state.sharePreview?.project_id === project.id ? state.sharePreview : null;
  const previewRows = preview ? preview.entries.map(entry => `<li><strong>${escapeHTML(entry.kind)}</strong> ${escapeHTML(entry.path || entry.name || entry.artifact_id)} · ${Number(entry.bytes || 0).toLocaleString()} bytes · <code>${escapeHTML(shortHash(entry.sha256))}</code></li>`).join("") : "";
  return `<section class="inspect-section"><div class="provider-head"><div><p class="eyebrow">Explicit project sharing</p><h3>Preview exact files and artifacts</h3><p class="form-note neutral">Chat history, credentials, runtime state and private dependencies are excluded. A downloaded copy cannot be revoked later.</p></div>${pill(eligible ? "eligible" : "private", eligible ? "green" : "amber")}</div>
    ${eligible ? `<form id="sharePreviewForm"><label>Relative file paths, one per line<textarea name="paths" rows="4" placeholder="README.md&#10;docs/guide.md"></textarea></label><fieldset><legend>Artifacts</legend>${artifacts.map(item => `<label class="check-label"><input type="checkbox" name="artifact_ids" value="${escapeHTML(item.id)}" ${item.visibility === "project_shared" && item.export_policy === "explicit_selection" ? "" : "disabled"}> ${escapeHTML(item.name)} · ${escapeHTML(item.visibility)}</label>`).join("") || `<p class="form-note neutral">No project artifacts.</p>`}</fieldset><fieldset><legend>Task drafts</legend>${tasks.map(item => `<label class="check-label"><input type="checkbox" name="task_ids" value="${escapeHTML(item.id)}" ${item.visibility === "project_shared" && item.export_policy === "explicit_selection" ? "" : "disabled"}> ${escapeHTML(item.title)} · ${escapeHTML(item.visibility)}</label>`).join("") || `<p class="form-note neutral">No project tasks.</p>`}</fieldset><fieldset><legend>Skill candidates</legend>${skills.map(item => `<label class="check-label"><input type="checkbox" name="skill_ids" value="${escapeHTML(item.id)}" ${item.visibility === "project_shared" && item.export_policy === "explicit_selection" ? "" : "disabled"}> ${escapeHTML(item.canonical_name)} · ${escapeHTML(item.visibility)}</label>`).join("") || `<p class="form-note neutral">No project Skills.</p>`}</fieldset><fieldset><legend>Explicit memories</legend>${memories.map(item => `<label class="check-label"><input type="checkbox" name="memory_ids" value="${escapeHTML(item.id)}" ${item.visibility === "project_shared" && item.export_policy === "explicit_selection" ? "" : "disabled"}> ${escapeHTML(item.memory_kind)} · ${escapeHTML(item.visibility)}</label>`).join("") || `<p class="form-note neutral">No project memories.</p>`}</fieldset><button class="ghost">Build immutable preview</button></form>` : `<button class="primary" id="enableProjectSharing">Enable explicit project sharing</button>`}
    ${preview ? `<div class="panel"><div class="provider-head"><div><h3>Preview expires ${formatDate(preview.expires_at)}</h3><p>${preview.entries.length} included · ${preview.omitted.length} omitted</p></div>${pill(preview.state,"green")}</div><ul class="findings">${previewRows || "<li>No included objects</li>"}</ul>${preview.omitted.length ? `<p><strong>Excluded</strong></p><ul class="findings">${preview.omitted.map(item => `<li>${escapeHTML(item)}</li>`).join("")}</ul>` : ""}<p class="form-note neutral"><strong>Metadata that remains:</strong> ${escapeHTML(preview.remaining_metadata.join(", ") || "none")}</p><button class="primary" id="exportSharePreview">Export exactly this preview</button></div>` : ""}</section>`;
}

function renderProjects() {
  const root = $("#view-projects");
  if (!root) return;
  const project = state.projects.find(item => item.id === state.selectedProject);
  root.innerHTML = `<div class="workbench-grid"><div class="panel"><p class="eyebrow">Bounded workspace registry</p><h3>Add project</h3><form id="projectForm"><label>Name<input name="name" required maxlength="100" placeholder="My workspace"></label><label>Existing local root<span class="path-field"><input name="root_path" id="projectRoot" required placeholder="/absolute/path"><button type="button" class="ghost" id="browseRoot">Browse…</button></span></label><button class="primary">Register project</button></form><section class="inspect-section"><h3>Projects</h3><div class="project-list">${state.projects.map(item => `<button class="session-item ${item.id === state.selectedProject ? "active" : ""}" data-project-id="${escapeHTML(item.id)}"><strong>${escapeHTML(item.name)}</strong><span>${escapeHTML(item.root_path)}</span></button>`).join("")}</div></section></div>
    <div class="panel"><div class="provider-head"><div><p class="eyebrow">Project workbench</p><h3>${escapeHTML(project?.name || "Select a project")}</h3><p>${escapeHTML(project?.root_path || "")}</p></div>${project ? pill(project.state,"green") : ""}</div>${project ? `<div class="file-browser"><div class="file-path"><code>${escapeHTML(state.projectPath || ".")}</code>${state.projectPath ? `<button class="ghost" id="projectUpButton">Up</button>` : ""}</div>${state.projectFiles.map(item => `<button class="file-row" data-file-path="${escapeHTML(item.path)}" data-directory="${item.directory}"><span>${uiIcon(item.directory ? "files" : "file")}</span><strong>${escapeHTML(item.name)}</strong><small>${item.directory ? "directory" : `${Number(item.bytes).toLocaleString()} bytes`}</small></button>`).join("") || `<div class="probe-empty">Directory is empty.</div>`}</div>${projectSharingPanel(project)}<section class="inspect-section"><h3>Direct background command</h3><form id="commandForm"><div class="form-grid"><label>Executable<select name="executable"><option>go</option><option>git</option><option>node</option><option>npm</option><option>python3</option><option>rg</option><option>ls</option></select></label><label>Timeout seconds<input name="timeout" type="number" min="1" max="600" value="30"></label></div><label>Arguments as JSON array<textarea name="arguments" rows="3">["test", "./..."]</textarea></label><label>Working directory<input name="working_dir" value="${escapeHTML(state.projectPath || ".")}"></label><p class="form-note neutral">No shell is involved. Executable allowlist, root boundary, minimal environment, timeout, output limit and process-group cancellation are enforced server-side.</p><button class="primary">Start background job</button></form></section>` : `<div class="empty"><h3>No project selected</h3><p>Register an existing local directory to create a bounded workbench.</p></div>`}</div></div>`;
  $("#projectForm")?.addEventListener("submit", createProject);
  $("#browseRoot")?.addEventListener("click", () => openFolderPicker($("#projectRoot")?.value || ""));
  $$('[data-project-id]', root).forEach(button => button.addEventListener("click", () => selectProject(button.dataset.projectId, "")));
  $$('[data-file-path]', root).forEach(button => button.addEventListener("click", () => button.dataset.directory === "true" ? selectProject(state.selectedProject, button.dataset.filePath) : (activateWorkbenchChrome("files"), openWorkbenchFile(button.dataset.filePath))));
  $("#projectUpButton")?.addEventListener("click", () => selectProject(state.selectedProject, (state.projectPath || "").split("/").slice(0,-1).join("/")));
  $("#commandForm")?.addEventListener("submit", startCommand);
	$("#enableProjectSharing")?.addEventListener("click", enableProjectSharing);
	$("#sharePreviewForm")?.addEventListener("submit", createProjectSharePreview);
	$("#exportSharePreview")?.addEventListener("click", exportProjectSharePreview);
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

async function enableProjectSharing() {
  const project = state.projects.find(item => item.id === state.selectedProject);
  if (!project) return;
  const approved = await askAction({title:"Enable explicit project sharing?",message:"Nothing is exported yet. Every file and eligible object still requires an immutable preview before download.",confirmLabel:"Enable previews"});
  if (!approved) return;
  try {
    await api("/api/sharing/visibility", {method:"POST", body:JSON.stringify({object_kind:"project", object_id:project.id, visibility:"project_shared", export_policy:"explicit_selection", expected_revision:project.sharing_revision, actor:currentActor(), reason:"enabled from project share dialog"})});
    toast("Project is eligible for explicit previews");
    await load();
    state.selectedProject = project.id;
    renderProjects();
  } catch (error) { toast(error.message, true); }
}

async function createProjectSharePreview(event) {
  event.preventDefault();
  const project = state.projects.find(item => item.id === state.selectedProject);
  if (!project) return;
  const form = new FormData(event.currentTarget);
  const paths = String(form.get("paths") || "").split(/\r?\n/).map(item => item.trim()).filter(Boolean);
  const artifactIDs = form.getAll("artifact_ids").map(String);
  const taskIDs = form.getAll("task_ids").map(String);
  const skillIDs = form.getAll("skill_ids").map(String);
  const memoryIDs = form.getAll("memory_ids").map(String);
  if (!paths.length && !artifactIDs.length && !taskIDs.length && !skillIDs.length && !memoryIDs.length) { toast("Select at least one file or shared object", true); return; }
  try {
    state.sharePreview = await api(`/api/projects/${encodeURIComponent(project.id)}/share/previews`, {method:"POST", body:JSON.stringify({paths, artifact_ids:artifactIDs, task_ids:taskIDs, skill_ids:skillIDs, memory_ids:memoryIDs, actor:currentActor()})});
    renderProjects();
    toast("Immutable share preview is ready");
  } catch (error) { toast(error.message, true); }
}

async function exportProjectSharePreview() {
  const project = state.projects.find(item => item.id === state.selectedProject);
  const preview = state.sharePreview;
  if (!project || !preview || preview.project_id !== project.id) return;
  const approved = await askAction({title:"Export this exact preview?",message:"The server will recheck every hash and sharing revision. A downloaded copy cannot be revoked later.",confirmLabel:"Create package"});
  if (!approved) return;
  try {
    const item = await api(`/api/projects/${encodeURIComponent(project.id)}/share/exports`, {method:"POST", body:JSON.stringify({preview_id:preview.id, manifest_digest:preview.manifest_digest, idempotency_key:crypto.randomUUID()})});
    toast("Project share package created");
    window.location.href = `/api/share/exports/${encodeURIComponent(item.id)}/content`;
  } catch (error) { toast(error.message, true); }
}

async function startCommand(event) {
  event.preventDefault();
  const form = new FormData(event.currentTarget);
  let args;
  try { args = JSON.parse(form.get("arguments")); if (!Array.isArray(args) || !args.every(item => typeof item === "string")) throw new Error(); }
  catch { toast("Arguments must be a JSON array of strings", true); return; }
  try {
    await api(`/api/projects/${encodeURIComponent(state.selectedProject)}/commands`, { method:"POST", body:JSON.stringify({ actor:currentActor(), executable:form.get("executable"), arguments:args, working_dir:form.get("working_dir"), timeout_seconds:Number(form.get("timeout")) }) });
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
  root.innerHTML = `<div class="workbench-grid"><div class="panel"><p class="eyebrow">Content-addressed outputs</p><h3>Create artifact</h3><form id="artifactForm"><label>Project<select name="project_id"><option value="">Global</option>${state.projects.map(item => `<option value="${item.id}">${escapeHTML(item.name)}</option>`).join("")}</select></label><label>Name<input name="name" required placeholder="report.md"></label><div class="form-grid"><label>Kind<input name="kind" value="report" required></label><label>MIME type<input name="mime_type" value="text/markdown" required></label></div><label>Content<textarea name="content" rows="10" required></textarea></label><button class="primary">Save immutable artifact</button></form></div><div class="card-list">${state.artifacts.map(item => `<article class="provider-card"><div class="provider-head"><div><h3>${escapeHTML(item.name)}</h3><p>${escapeHTML(item.mime_type)} · ${Number(item.byte_size).toLocaleString()} bytes</p></div>${pill(item.kind,"blue")}${pill(item.visibility,item.visibility === "project_shared" ? "green" : "amber")}</div><div class="kv"><span>Checksum</span><code>${escapeHTML(item.checksum)}</code><span>Project</span><span>${escapeHTML(state.projects.find(project => project.id === item.project_id)?.name || "global")}</span><span>Created</span><span>${formatDate(item.created_at)}</span></div><div class="action-row"><a class="button-link" href="/api/artifacts/${encodeURIComponent(item.id)}/content" target="_blank" rel="noreferrer">Open verified content</a>${item.project_id && item.visibility !== "project_shared" ? `<button class="ghost" data-share-artifact="${escapeHTML(item.id)}">Allow explicit project sharing</button>` : ""}</div></article>`).join("") || `<div class="empty"><h3>No artifacts</h3><p>Command logs and generated outputs will appear here.</p></div>`}</div></div>`;
  $("#artifactForm")?.addEventListener("submit", createArtifact);
	$$('[data-share-artifact]', root).forEach(button => button.addEventListener("click", () => enableArtifactSharing(button.dataset.shareArtifact)));
}

async function createArtifact(event) {
  event.preventDefault(); const form = new FormData(event.currentTarget);
  try { await api("/api/artifacts", { method:"POST", body:JSON.stringify({ project_id:form.get("project_id"), name:form.get("name"), kind:form.get("kind"), mime_type:form.get("mime_type"), content:form.get("content"), metadata:{ created_by:"user" } }) }); toast("Artifact stored by checksum"); await load(); switchTab("artifacts"); }
  catch (error) { toast(error.message, true); }
}

async function enableArtifactSharing(id) {
  const item = state.artifacts.find(artifact => artifact.id === id);
  if (!item) return;
  try {
    await api(`/api/artifacts/${encodeURIComponent(id)}/sharing`, {method:"PATCH", body:JSON.stringify({visibility:"project_shared", export_policy:"explicit_selection", expected_revision:item.sharing_revision, actor:currentActor(), reason:"enabled from artifact share control"})});
    toast("Artifact may now be explicitly selected in its project preview");
    await load();
    switchTab("artifacts");
  } catch (error) { toast(error.message, true); }
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

function workspacePortabilityPanel() {
  const preview = state.workspacePreview;
  const sourceIDs = preview?.summary?.project_ids || [];
  return `<div class="panel"><p class="eyebrow">Encrypted workspace migration</p><h3>Move private canonical content</h3><p class="form-note neutral">The age-encrypted package excludes credentials, sessions, approvals, effects, browser profiles and runtime authority. Imported objects receive new IDs and private defaults.</p>
    <form id="workspaceExportForm"><fieldset><legend>Projects</legend>${state.projects.map(item => `<label class="check-label"><input type="checkbox" name="project_ids" value="${escapeHTML(item.id)}"> ${escapeHTML(item.name)}</label>`).join("") || `<p>No projects registered.</p>`}</fieldset><label>Migration passphrase<input name="passphrase" type="password" minlength="12" autocomplete="new-password" required></label><button class="primary">Create encrypted package</button></form>
    <hr><form id="workspacePreviewForm"><label>Encrypted workspace package<input name="file" type="file" accept=".age,application/vnd.hermetrix.workspace+age" required></label><label>Passphrase<input name="passphrase" type="password" minlength="12" autocomplete="current-password" required></label><button class="ghost">Decrypt and preview</button></form>
    ${preview ? `<div class="panel"><div class="provider-head"><div><h3>Verified migration preview</h3><p>${Number(preview.summary?.projects || 0)} projects · authority imported: ${preview.summary?.authority_imported ? "yes" : "no"}</p></div>${pill(preview.state,"green")}</div><form id="workspaceApplyForm"><label>Destination roots as JSON map<textarea name="root_mappings" rows="5" required>${escapeHTML(JSON.stringify(Object.fromEntries(sourceIDs.map(id => [id, ""])), null, 2))}</textarea></label><label>Passphrase again<input name="passphrase" type="password" minlength="12" autocomplete="current-password" required></label><button class="primary">Restore to new roots</button></form></div>` : ""}</div>`;
}

function recoveryPanel() {
  const runs = state.backups.filter(item => item.kind === "full_recovery");
  const report = state.recoveryReport;
  return `<div class="panel"><div class="provider-head"><div><p class="eyebrow">Full disaster recovery</p><h3>Consistent SQLite + CAS snapshot</h3><p class="form-note neutral">The credential vault is deliberately separate. Windows DPAPI capture is for the same account/machine; cross-machine recovery reenrolls credentials.</p></div><button class="primary" id="createFullRecovery">Create recovery package</button></div><label>Verify an existing recovery package<input id="verifyRecoveryFile" type="file" accept=".zip,application/vnd.hermetrix.full-recovery+zip"></label><button class="ghost" id="verifyFullRecovery">Verify without restoring</button>
    ${report ? `<div class="kv"><span>Integrity</span><strong>${escapeHTML(report.integrity_check)}</strong><span>Foreign-key errors</span><strong>${report.foreign_key_errors}</strong><span>Schema</span><strong>${report.manifest.schema_version}</strong><span>CAS blobs</span><strong>${report.verified_blob_count}</strong><span>Compatible</span><strong>${report.compatible ? "yes" : "no"}</strong></div>` : ""}
    <div class="card-list">${runs.map(item => `<article class="provider-card"><div class="provider-head"><div><h3>${escapeHTML(item.id)}</h3><p>${formatDate(item.created_at)}</p></div>${pill(item.state,item.state === "completed" ? "green" : "amber")}</div><div class="kv"><span>Checksum</span><code>${escapeHTML(item.checksum)}</code><span>Database bytes</span><strong>${Number(item.counts.database_bytes || 0).toLocaleString()}</strong><span>CAS blobs</span><strong>${item.counts.blobs || 0}</strong></div>${item.state === "completed" ? `<a class="button-link" href="/api/recovery/${encodeURIComponent(item.id)}/content">Download recovery package</a>` : ""}</article>`).join("") || `<p class="form-note neutral">No recovery package has been created.</p>`}</div></div>`;
}

function renderMaintenance() {
  const root = $("#view-maintenance");
  if (!root) return;
  const usage = state.usage || {};
  root.innerHTML = `<div class="maintenance-grid"><div class="panel-stack"><div class="panel"><p class="eyebrow">Usage & provenance</p><h3>Derived runtime totals</h3><div class="kv"><span>Sessions</span><strong>${usage.sessions || 0}</strong><span>Model steps</span><strong>${usage.model_steps || 0}</strong><span>Tool calls</span><strong>${usage.tool_calls || 0}</strong><span>Tool success</span><strong>${usage.tool_succeeded || 0}</strong><span>Total tokens</span><strong>${Number(usage.total_tokens || 0).toLocaleString()}</strong></div></div><div class="panel"><p class="eyebrow">Safe settings</p><h3>Non-secret JSON settings</h3><form id="settingForm"><label>Key<input name="key" required value="retention.skill_activation_days"></label><label>JSON value<input name="value" required value="365"></label><button class="ghost">Save setting</button></form><div class="meta">${state.settings.map(item => pill(`${item.key}=${JSON.stringify(item.value)}`)).join("")}</div></div><div class="panel"><p class="eyebrow">Explicit memory</p><h3>User-controlled memory</h3><form id="memoryForm"><div class="form-grid"><label>Scope<select name="scope_kind"><option value="user">User</option><option value="project">Project</option></select></label><label>Project<select name="scope_ref"><option value="">None</option>${state.projects.map(item => `<option value="${item.id}">${escapeHTML(item.name)}</option>`).join("")}</select></label></div><label>Kind<input name="memory_kind" value="preference" required></label><label>Content<textarea name="content" rows="3" required></textarea></label><button class="ghost">Save explicit memory</button></form><div class="card-list">${state.memories.map(item => `<article class="memory-row"><div><strong>${escapeHTML(item.memory_kind)}</strong><p>${escapeHTML(item.content)}</p></div>${pill(item.state,item.state === "active" ? "green" : "amber")}${item.state === "active" ? `<button class="danger" data-archive-memory="${item.id}">Archive</button>` : ""}</article>`).join("")}</div></div></div>
    <div class="panel-stack"><div class="panel"><div class="provider-head"><div><p class="eyebrow">Skill portability</p><h3>Export / preview / candidate-only import</h3><p class="form-note neutral">This package contains Skill lifecycle data only. Project sharing, encrypted workspace migration, and full disaster recovery use separate flows.</p></div><button class="primary" id="exportBackupButton">Export Skills</button></div><label>Import Skill package<input id="importBackupFile" type="file" accept="application/json,.json"></label><button class="ghost" id="previewImportButton">Verify & preview import</button><div class="card-list">${state.backups.filter(item => item.kind !== "full_recovery").map(item => `<article class="provider-card"><div class="provider-head"><div><h3>${escapeHTML(item.kind)}</h3><p>${formatDate(item.created_at)}</p></div>${pill(item.state,item.state === "completed" || item.state === "imported" ? "green" : "amber")}</div><div class="kv"><span>Checksum</span><code>${escapeHTML(item.checksum || "pending")}</code><span>Skills</span><strong>${item.counts.skills || 0}</strong><span>Conflicts</span><strong>${item.counts.skill_conflicts || 0}</strong></div><div class="action-row">${item.kind === "export" && item.state === "completed" ? `<a class="button-link" href="/api/backups/${item.id}/download">Download Skill package</a>` : ""}${item.kind === "import_preview" && item.state === "awaiting_apply" ? `<button class="primary" data-apply-import="${item.id}">Apply as candidates</button>` : ""}</div></article>`).join("")}</div></div>
      ${workspacePortabilityPanel()}${recoveryPanel()}
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
	$("#workspaceExportForm")?.addEventListener("submit", exportWorkspaceMigration);
	$("#workspacePreviewForm")?.addEventListener("submit", previewWorkspaceMigration);
	$("#workspaceApplyForm")?.addEventListener("submit", applyWorkspaceMigration);
	$("#createFullRecovery")?.addEventListener("click", createFullRecovery);
	$("#verifyFullRecovery")?.addEventListener("click", verifyFullRecovery);
}

async function saveSetting(event) { event.preventDefault(); const form = new FormData(event.currentTarget); let value; try { value=JSON.parse(form.get("value")); } catch { toast("Setting value must be valid JSON",true); return; } try { await api("/api/settings",{method:"PUT",body:JSON.stringify({key:form.get("key"),value})}); toast("Non-secret setting saved"); await load(); switchTab("maintenance"); } catch(error){toast(error.message,true);} }
async function saveMemory(event) { event.preventDefault(); const form=new FormData(event.currentTarget); const scope=form.get("scope_kind"); try { await api("/api/memories",{method:"POST",body:JSON.stringify({scope_kind:scope,scope_ref:scope === "project" ? form.get("scope_ref") : "",memory_kind:form.get("memory_kind"),content:form.get("content"),source:"user"})}); toast("Explicit user memory saved"); await load(); switchTab("maintenance"); } catch(error){toast(error.message,true);} }
async function archiveMemory(id) { try { await api(`/api/memories/${encodeURIComponent(id)}/archive`,{method:"POST",body:"{}"}); toast("Memory archived"); await load(); switchTab("maintenance"); } catch(error){toast(error.message,true);} }
async function exportBackup() { try { const run=await api("/api/backups",{method:"POST",body:JSON.stringify({actor:currentActor()})}); toast("Skill portability export completed"); await load(); window.location.href=`/api/backups/${encodeURIComponent(run.id)}/download`; } catch(error){toast(error.message,true);} }
async function previewImport() { const file=$("#importBackupFile").files[0]; if(!file){toast("Choose a Skill package",true);return;} try { const body=await api(`/api/imports/preview?actor=${encodeURIComponent(currentActor())}`,{method:"POST",headers:{"Content-Type":"application/vnd.hermetrix.backup+json"},body:file}); toast(`Verified Skill import preview · ${body.skill_conflicts} conflicts`); await load(); switchTab("maintenance"); } catch(error){toast(error.message,true);} }
async function applyImport(id) { const approved=await askAction({title:"Restore backup as candidates?",message:"Blobs are checksum-verified. Skills become reviewable candidates only; active skills are never overwritten.",confirmLabel:"Create candidates"}); if(!approved)return; try { const result=await api(`/api/imports/${encodeURIComponent(id)}/apply`,{method:"POST",body:JSON.stringify({actor:currentActor()})}); toast(`Created ${result.candidate_ids.length} candidates · ${result.conflicts} conflicts`); await load(); switchTab("proposals"); } catch(error){toast(error.message,true);} }
async function exportWorkspaceMigration(event) { event.preventDefault(); const form=new FormData(event.currentTarget); const projectIDs=form.getAll("project_ids").map(String); if(!projectIDs.length){toast("Select at least one project",true);return;} try { const item=await api("/api/workspace-migrations/exports",{method:"POST",body:JSON.stringify({project_ids:projectIDs,passphrase:form.get("passphrase"),actor:currentActor()})}); event.currentTarget.reset(); toast("Encrypted workspace package created"); window.location.href=`/api/workspace-migrations/${encodeURIComponent(item.id)}/content`; } catch(error){toast(error.message,true);} }
async function previewWorkspaceMigration(event) { event.preventDefault(); const form=new FormData(event.currentTarget); form.set("actor",currentActor()); try { const response=await fetch("/api/workspace-migrations/import-previews",{method:"POST",body:form}); const body=await response.json().catch(()=>({})); if(!response.ok)throw new Error(typeof body.error === "string" ? body.error : `Migration preview failed (${response.status})`); state.workspacePreview=body; event.currentTarget.reset(); toast("Encrypted workspace package verified"); renderMaintenance(); } catch(error){toast(error.message,true);} }
async function applyWorkspaceMigration(event) { event.preventDefault(); if(!state.workspacePreview)return; const form=new FormData(event.currentTarget); let mappings; try { mappings=JSON.parse(form.get("root_mappings")); if(!mappings || Array.isArray(mappings) || typeof mappings !== "object")throw new Error(); } catch { toast("Destination roots must be a JSON object",true); return; } try { await api("/api/workspace-migrations/imports",{method:"POST",body:JSON.stringify({preview_id:state.workspacePreview.id,passphrase:form.get("passphrase"),root_mappings:mappings,actor:currentActor()})}); state.workspacePreview=null; toast("Workspace restored with new private IDs"); await load(); switchTab("maintenance"); } catch(error){toast(error.message,true);} }
async function createFullRecovery() { const approved=await askAction({title:"Create full recovery package?",message:"This snapshots SQLite and CAS consistently. The credential vault stays separate and is not included in the download.",confirmLabel:"Create snapshot"}); if(!approved)return; try { const run=await api("/api/recovery/full",{method:"POST",body:JSON.stringify({actor:currentActor()})}); toast("Full recovery package verified and stored"); await load(); switchTab("maintenance"); window.location.href=`/api/recovery/${encodeURIComponent(run.id)}/content`; } catch(error){toast(error.message,true);} }
async function verifyFullRecovery() { const file=$("#verifyRecoveryFile")?.files[0]; if(!file){toast("Choose a recovery package",true);return;} try { const response=await fetch("/api/recovery/verify",{method:"POST",headers:{"Content-Type":"application/vnd.hermetrix.full-recovery+zip"},body:file}); const body=await response.json().catch(()=>({})); if(!response.ok)throw new Error(typeof body.error === "string" ? body.error : `Recovery verification failed (${response.status})`); state.recoveryReport=body; toast("Recovery package integrity verified"); renderMaintenance(); } catch(error){toast(error.message,true);} }
async function saveSchedule(event) { event.preventDefault(); const form=new FormData(event.currentTarget); try { await api("/api/maintenance/schedules",{method:"POST",body:JSON.stringify({name:form.get("name"),task_kind:form.get("task_kind"),interval_seconds:Number(form.get("interval_seconds")),enabled:form.get("enabled")==="on",require_idle:form.get("require_idle")==="on",require_ac_power:form.get("require_ac_power")==="on"})}); toast("Maintenance schedule saved"); await load(); switchTab("maintenance"); } catch(error){toast(error.message,true);} }
async function runDueMaintenance() { try { const detected=await api("/api/maintenance/system-state"); const runs=await api("/api/maintenance/run-due",{method:"POST",body:JSON.stringify(detected)}); toast(runs.length ? `Evaluated ${runs.length} due schedules` : "No schedules are due"); await load(); switchTab("maintenance"); } catch(error){toast(error.message,true);} }
async function dryRunGC() { try { const run=await api("/api/maintenance/gc/dry-run",{method:"POST",body:"{}"}); toast(`GC dry-run found ${run.unreachable_count} unreachable objects`); await load(); switchTab("maintenance"); } catch(error){toast(error.message,true);} }
async function applyGC(id) { const approved=await askAction({title:"Quarantine exact GC snapshot?",message:"The CAS set must still match the dry-run. Objects are moved to recoverable quarantine, never deleted.",confirmLabel:"Quarantine exact set",danger:true}); if(!approved)return; try { await api(`/api/maintenance/gc/${encodeURIComponent(id)}/apply`,{method:"POST",body:JSON.stringify({actor:currentActor()})}); toast("Exact snapshot moved to recoverable quarantine"); await load(); switchTab("maintenance"); } catch(error){toast(error.message,true);} }
async function restoreGC(id) { try { await api(`/api/maintenance/gc/${encodeURIComponent(id)}/restore`,{method:"POST",body:JSON.stringify({actor:currentActor()})}); toast("Quarantined CAS objects restored after integrity verification"); await load(); switchTab("maintenance"); } catch(error){toast(error.message,true);} }

const workbenchPollTimers = new Map();
let terminalPollInFlight = false;

function switchWorkbench(tab) {
  stopWorkbenchPolling();
  activateWorkbenchChrome(tab);
  openContentPane(tab);
}

function renderCurrentWorkbench() {
  renderPanes();
}

// Terminal has exactly one home now: a Code pane. Its poll loop and the
// output fetch it drives both have to ask whether that pane is actually open
// rather than checking a chat-side tab that no longer exists -- checking the
// old tab would silently stop a running terminal's output the moment
// anything else became the active workbench tab.
function workspacePaneVisible(kind) {
  if (document.hidden || $("#appShell")?.hidden || !$("#configOverlay")?.hidden) return false;
  if (!state.panes.includes(kind)) return false;
  if (state.view !== "code" && $("#zones")?.classList.contains("side-hidden")) return false;
  const body = $(`.pane-body[data-pane-kind='${kind}']`);
  return Boolean(body && !body.closest(".pane-hidden"));
}

function terminalPaneOpen() {
  return workspacePaneVisible("terminal");
}

function stopWorkbenchPolling() {
  for (const timer of workbenchPollTimers.values()) clearTimeout(timer);
  workbenchPollTimers.clear();
}

function scheduleWorkbenchPoll(callback, delay = 700) {
  clearTimeout(workbenchPollTimers.get(callback));
  workbenchPollTimers.delete(callback);
  const kind = callback === pollTerminal ? "terminal" : "team";
  if (!workspacePaneVisible(kind)) return;
  workbenchPollTimers.set(callback, setTimeout(() => {
    workbenchPollTimers.delete(callback);
    if (workspacePaneVisible(kind)) void callback();
  }, delay));
}

function syncWorkbenchPolling() {
  window.HermetrixWorkspace?.resume();
  stopWorkbenchPolling();
  if (terminalPaneOpen() && state.selectedTerminal) scheduleWorkbenchPoll(pollTerminal, 0);
  if (workspacePaneVisible("team") && state.teamRuns.some(run => ["queued", "running"].includes(run.state))) {
    scheduleWorkbenchPoll(pollTeamRuns, 0);
  }
}

document.addEventListener("visibilitychange", syncWorkbenchPolling);

function renderWorkbenchReview(target = $(".pane-body[data-pane-kind='review']") || $("#workbenchContent")) {
  if (!target) return;
  const queued = state.reviews.filter(item => ["queued","running"].includes(item.state));
  const session = state.sessionDetail?.session;
  const contract = session?.contract || {};
  const selectedSkills = contract.selected_skills || [];
  const pendingApprovals = (state.sessionDetail?.approvals || []).filter(item => item.state === "pending");
  const sessionPanel = session ? `<div class="panel session-contract-panel"><div class="provider-head"><div><p class="eyebrow">Current Session Contract</p><h3>${escapeHTML(session.title)}</h3></div>${pill(session.state, session.state === "active" ? "green" : "amber")}</div><div class="kv"><span>Model</span><strong>${escapeHTML(session.model)}</strong><span>Context</span><strong>${escapeHTML(session.context_profile)}</strong><span>Project</span><strong>${escapeHTML(state.projects.find(item => item.id === session.project_id)?.name || "chat only")}</strong><span>Skills in context</span><strong>${selectedSkills.length}</strong><span>Direct tools</span><strong>${(contract.tool_bindings || []).length}</strong><span>Pending approvals</span><strong>${pendingApprovals.length}</strong><span>Contract</span><code>${escapeHTML(shortHash(session.contract_revision))}</code><span>Capability revision</span><code>${escapeHTML(shortHash(contract.capability_revision))}</code></div>${selectedSkills.length ? `<div class="meta session-skill-list">${selectedSkills.map(item => pill(item.canonical_name,"blue")).join("")}</div>` : `<p class="form-note neutral">No Skill body is injected yet; the session can still retrieve a frozen Skill with skill_search and skill_view.</p>`}<div class="action-row"><button class="primary" id="reviewOpenCapabilities">Skills & tools</button><button class="ghost" id="reviewOpenTools">Tool Center</button></div></div>` : `<div class="panel"><p class="eyebrow">Session review</p><h3>Start or select a session</h3><p class="dialog-message">Its immutable model, context envelope, Skill catalog, direct tools and approval state will appear here beside the conversation.</p></div>`;
  target.innerHTML = `${sessionPanel}<div class="panel"><p class="eyebrow">Authority & background work</p><h3>Evidence before authority</h3><p class="dialog-message">Skill candidates, write approvals, background reviews and command receipts stay inspectable here. Agents cannot widen authority through this room.</p><div class="kv"><span>Proposals</span><strong>${state.candidates.filter(item => ["needs_review","quarantined"].includes(item.state)).length}</strong><span>Review jobs</span><strong>${queued.length}</strong><span>Policy</span><strong>${escapeHTML(state.skillAuthority?.mode || "manual")}</strong></div><div class="action-row"><button class="primary" id="reviewOpenSkills">Open Skill Studio</button><button class="ghost" id="reviewRunNext" title="${queued.length ? "Run the oldest queued background review now" : "No queued reviews — new milestones, corrections and explicit learn requests enqueue here"}" ${queued.length ? "" : "disabled"}>Run next review</button></div></div>
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
  const project = state.currentProject;
  return `<div class="workspace-files"><div class="provider-head"><div><strong>Explorer</strong><small>${escapeHTML(project?.name || "No project")}</small></div><button class="ghost" id="newWorkbenchFile" ${project?.root_path ? "" : "disabled"}>+ New file</button></div><label class="file-filter"><input type="search" id="workbenchFileFilter" aria-label="Filter this folder" placeholder="ค้นหาไฟล์…"></label>
    ${project?.root_path ? `<div class="file-path"><code>${escapeHTML(state.projectPath || ".")}</code>${state.projectPath ? `<button class="ghost" id="workbenchFileUp">Up</button>` : ""}</div><div class="file-browser">${(state.workspaceFiles || []).map(item => `<button class="file-row" data-workbench-file="${escapeHTML(item.path)}" data-directory="${item.directory}"><span>${uiIcon(item.directory ? "files" : "file")}</span><strong>${escapeHTML(item.name)}</strong><small>${item.directory ? "" : `${Number(item.bytes).toLocaleString()} B`}</small></button>`).join("") || `<div class="probe-empty">Directory is empty.</div>`}</div>` : `<div class="probe-empty">This project has no code folder. Add its folder in project settings to use Files, Code and Terminal.</div>`}
  </div>`;
}

const codeDrafts = new Map();
const codeTabs = new Map();
const CODE_DRAFT_STORAGE_PREFIX = "hermetrix.ide.draft.v1:";
const CODE_DRAFT_MAX_BYTES = 2 * 1024 * 1024;
const pendingCodeDraftWrites = new Map();
let activeCodeEditor = null;
let activeTerminalEmulator = null;
let terminalCursor = { id: "", value: 0 };
let terminalResizeTimer = null;
let terminalInputChain = Promise.resolve();
function codeDraftKey(projectID, path) { return JSON.stringify([projectID, path]); }
function persistedCodeDraft(projectID, path) {
  if (typeof sessionStorage === "undefined") return null;
  try {
    const raw = sessionStorage.getItem(CODE_DRAFT_STORAGE_PREFIX + codeDraftKey(projectID, path));
    if (!raw || raw.length > CODE_DRAFT_MAX_BYTES) return null;
    const draft = JSON.parse(raw);
    return draft?.projectID === projectID && draft?.path === path && typeof draft.content === "string" &&
      typeof draft.originalContent === "string" ? draft : null;
  } catch { return null; }
}
function persistCodeDraft(key, draft) {
  if (typeof sessionStorage === "undefined" || !draft) return;
  try {
    const storageKey = CODE_DRAFT_STORAGE_PREFIX + key;
    if (draft.content === draft.originalContent) { sessionStorage.removeItem(storageKey); return; }
    const encoded = JSON.stringify({projectID:draft.projectID, path:draft.path, content:draft.content,
      originalContent:draft.originalContent, sha256:draft.sha256 || "", mode:draft.mode || "", bytes:draft.bytes || 0, touched:true});
    if (encoded.length <= CODE_DRAFT_MAX_BYTES) sessionStorage.setItem(storageKey, encoded);
  } catch { /* Storage can be unavailable or full; the in-memory draft remains authoritative. */ }
}
function scheduleCodeDraftPersistence(key, draft) {
  clearTimeout(pendingCodeDraftWrites.get(key));
  pendingCodeDraftWrites.set(key, setTimeout(() => {
    pendingCodeDraftWrites.delete(key);
    persistCodeDraft(key, codeDrafts.get(key) || draft);
  }, 250));
}
window.addEventListener("pagehide", () => {
  for (const [key, draft] of codeDrafts) persistCodeDraft(key, draft);
});
function projectCodeTabs(projectID = state.currentProject?.id) {
  if (!projectID) return [];
  if (!codeTabs.has(projectID)) codeTabs.set(projectID, []);
  return codeTabs.get(projectID);
}
function rememberCodeTab(projectID, path) {
  const tabs = projectCodeTabs(projectID);
  if (!tabs.includes(path)) tabs.push(path);
}
function captureCodeDraft() {
  const document = state.projectFile;
  const key = codeDraftKey(document?.projectID, document?.path);
  const content = activeCodeEditor?.documentKey === key
    ? activeCodeEditor.getValue()
    : $("#workbenchFileContent")?.value;
  if (document && typeof content === "string") {
    // A mounted editor that never reported a change and reads back empty is
    // "not ready", not "the user deleted everything" (a real clear-all always
    // fires onChange first). Letting that empty read through would poison the
    // cached draft and blank the file on every later render.
    if (activeCodeEditor?.documentKey === key && content === "" &&
        activeCodeEditor?.changed !== true &&
        (codeDrafts.get(key)?.content || document.content)) return;
    const draft = { ...document, content };
    if (activeCodeEditor?.documentKey === key && activeCodeEditor?.changed === true) draft.touched = true;
    codeDrafts.set(key, draft);
    if (typeof scheduleCodeDraftPersistence === "function") scheduleCodeDraftPersistence(key, draft);
    state.projectFile = draft;
  }
}

function disposeWorkspaceWidgets() {
  activeCodeEditor?.dispose();
  activeTerminalEmulator?.dispose();
  activeCodeEditor = null;
  activeTerminalEmulator = null;
  clearTimeout(terminalResizeTimer);
}

function codeDiffText(document, content) {
  const before = (document.originalContent || "").split("\n");
  const after = content.split("\n");
  let prefix = 0, suffix = 0;
  while (prefix < before.length && prefix < after.length && before[prefix] === after[prefix]) prefix++;
  while (suffix < before.length - prefix && suffix < after.length - prefix && before[before.length - 1 - suffix] === after[after.length - 1 - suffix]) suffix++;
  return content === document.originalContent ? "No unsaved changes." :
    ["--- Saved version", "+++ Working copy", `@@ from line ${prefix + 1} @@`, ...before.slice(prefix, before.length - suffix).map(line => "- " + line), ...after.slice(prefix, after.length - suffix).map(line => "+ " + line)].join("\n");
}

function codeSymbols(path, content) {
  const extension = String(path || "").split(".").pop().toLowerCase();
  const patterns = extension === "go"
    ? [/^\s*func\s+(?:\([^)]*\)\s*)?([A-Za-z_]\w*)/, /^\s*type\s+([A-Za-z_]\w*)\s+/]
    : extension === "py"
      ? [/^\s*(?:async\s+)?def\s+([A-Za-z_]\w*)/, /^\s*class\s+([A-Za-z_]\w*)/]
      : ["yml", "yaml"].includes(extension)
        // Top-level mapping keys only: indented lines are values, not sections.
        ? [/^([A-Za-z_][\w.-]*)\s*:/]
        : extension === "mod"
          ? [/^module\s+(\S+)/, /^\s+([A-Za-z0-9_./-]+)\s+v/]
          : [ /^\s*(?:export\s+)?(?:async\s+)?function\s+([A-Za-z_$][\w$]*)/, /^\s*(?:export\s+)?class\s+([A-Za-z_$][\w$]*)/, /^\s*(?:export\s+)?(?:const|let|var)\s+([A-Za-z_$][\w$]*)\s*=\s*(?:async\s*)?\(/ ];
  return String(content || "").split("\n").flatMap((line, index) => {
    for (const pattern of patterns) {
      const match = line.match(pattern);
      if (match) return [{ name:match[1], line:index + 1 }];
    }
    return [];
  });
}

function shellQuote(value) { return `'${String(value).replaceAll("'", `'"'"'`)}'`; }

function editorCommandFor(action, document) {
  const extension = document.path.split(".").pop().toLowerCase();
  const file = shellQuote(document.path);
  const directory = document.path.includes("/") ? `./${document.path.split("/").slice(0, -1).join("/")}` : ".";
  const goTarget = shellQuote(directory);
  const goMain = /^\s*package\s+main\b/m.test(document.content || "");
  const commands = {
    go: {
      format:`gofmt -w ${file}`,
      run:goMain ? `go run ${goTarget}` : `go test ${goTarget}`,
      test:"go test ./...",
      debug:`command -v dlv >/dev/null && dlv ${goMain ? "debug" : "test"} ${goTarget} || printf '\\nDelve is not installed. Install dlv to debug Go.\\n'`
    },
    py: {
      format:`python3 -m black ${file}`,
      run:`python3 ${file}`,
      test:"python3 -m pytest",
      debug:`python3 -m pdb ${file}`
    },
    js: { run:`node ${file}`, test:"npm test", debug:`node --inspect-brk ${file}` },
    mod: {
      format:`go mod edit -fmt ${file}`,
      run:`go build ./...`,
      test:"go test ./..."
    },
    mjs: { run:`node ${file}`, test:"npm test", debug:`node --inspect-brk ${file}` },
    cjs: { run:`node ${file}`, test:"npm test", debug:`node --inspect-brk ${file}` },
    ts: { run:`npm exec --offline -- tsx ${file}`, test:"npm test", debug:`node --inspect-brk --import tsx ${file}` }
  };
  return commands[extension]?.[action] || "";
}

function renderCodeEditor(body) {
  const document = state.projectFile;
  if (!document || document.projectID !== state.currentProject?.id) {
    body.innerHTML = `<div class="code-empty"><h3>Open a file to start coding</h3><p>Select a file from Files to edit it here.</p><button class="ghost" id="codeOpenFiles">Browse files</button></div>`;
    $("#codeOpenFiles")?.addEventListener("click", () => state.paneLayout === "ide" ? window.HermetrixWorkspace?.panel("files") : openContentPane("files"));
    return;
  }
  rememberCodeTab(document.projectID, document.path);
  const tabs = projectCodeTabs(document.projectID);
  const symbols = codeSymbols(document.path, document.content);
  const editorActionHTML = action => {
    const supported = window.HermetrixWorkspace?.supports(action, document.path) ?? false;
    const title = supported ? `${action} this file · results stay in the workspace` : `${action} is not configured for this file type`;
    return `<button class="ghost" type="button" data-editor-action="${action}" title="${escapeHTML(title)}" ${supported ? "" : "disabled"}>${action[0].toUpperCase() + action.slice(1)}</button>`;
  };
  body.innerHTML = `<form class="code-editor" id="workbenchFileForm">
    <div class="code-tabs" role="tablist" aria-label="Open files">${tabs.map(path => {
      const draft = codeDrafts.get(codeDraftKey(document.projectID, path));
      const dirty = draft && draft.content !== draft.originalContent;
      return `<span class="code-tab ${path === document.path ? "active" : ""}" role="tab" aria-selected="${path === document.path}"><button type="button" data-code-tab="${escapeHTML(path)}" title="${escapeHTML(path)}">${escapeHTML(path.split("/").pop())}${dirty ? `<i aria-label="Unsaved">•</i>` : ""}</button><button type="button" class="code-tab-close" data-code-tab-close="${escapeHTML(path)}" aria-label="Close ${escapeHTML(path)}">×</button></span>`;
    }).join("")}</div>
    <div class="code-editor-toolbar"><code title="${escapeHTML(document.path)}">${escapeHTML(document.path)}</code><div class="code-actions">${["format", "run", "test", "debug"].map(editorActionHTML).join("")}<button class="ghost" type="button" id="codeOutlineToggle" aria-pressed="${Boolean(state.ideOutlineOpen)}">Outline</button><button class="ghost" type="button" id="codeReview">Changes</button><button class="ghost" type="button" id="codeAskAI">Ask AI</button><span id="codeSaveState">${document.content !== document.originalContent ? "Unsaved" : "Saved"}</span><button class="primary">Save</button></div></div>
    <div id="codeFeedback" class="code-feedback" role="status" hidden></div>
    <div class="code-workarea ${state.ideOutlineOpen ? "outline-open" : ""}"><aside class="code-outline"><strong>Outline</strong>${symbols.map(symbol => `<button type="button" data-code-symbol="${symbol.line}"><span>${escapeHTML(symbol.name)}</span><small>${symbol.line}</small></button>`).join("") || `<small>No symbols found</small>`}</aside><div id="workbenchFileContent" class="code-editor-host" aria-label="Code editor"></div></div>
    <footer class="code-status"><span>${escapeHTML(document.path.split(".").pop().toUpperCase())}</span><span id="codeCursor">Ln 1, Col 1</span><span>Ctrl+F Find · Ctrl+H Replace</span><span>UTF-8</span></footer>
    <details class="code-review" id="codeReviewPanel"><summary>Changes</summary><pre class="diff-view" id="codeDiff"></pre></details>
  </form>`;
  const host = $("#workbenchFileContent");
  if (!window.HermetrixIDE?.createEditor) {
    host.innerHTML = `<textarea aria-label="Code editor fallback" spellcheck="false">${escapeHTML(document.content)}</textarea>`;
    const area = host.querySelector("textarea");
    activeCodeEditor = { documentKey:codeDraftKey(document.projectID, document.path), changed:!document.content, getValue:() => area.value, focus:() => area.focus(), dispose:() => {} };
    area.addEventListener("input", () => { activeCodeEditor.changed = true; });
  } else {
    const editor = window.HermetrixIDE.createEditor(host, {
      doc: document.content,
      path: document.path,
      wrap: ideWordWrapEnabled(),
      onChange: content => {
        editor.changed = true;
        const draft = { ...state.projectFile, content, touched:true };
        codeDrafts.set(codeDraftKey(document.projectID, document.path), draft);
        if (typeof scheduleCodeDraftPersistence === "function") scheduleCodeDraftPersistence(codeDraftKey(document.projectID, document.path), draft);
        state.projectFile = draft;
        const savedState = $("#codeSaveState");
        if (savedState) savedState.textContent = content === state.projectFile.originalContent ? "Saved" : "Unsaved";
        window.HermetrixWorkspace?.dirty();
      },
      onCursor: (line, column) => { const status=$("#codeCursor"); if(status) status.textContent=`Ln ${line}, Col ${column}`; },
      onSave: () => $("#workbenchFileForm")?.requestSubmit(),
      onBreakpoint: (line, enabled) => window.HermetrixWorkspace?.breakpoint(document, line, enabled)
    });
    editor.documentKey = codeDraftKey(document.projectID, document.path);
    editor.changed = !document.content;
    activeCodeEditor = editor;
    // Self-heal a mount that came up empty on a non-empty document: fall back
    // to the plain textarea rather than showing a blank file.
    let mounted = "";
    try { mounted = editor.getValue?.() ?? ""; } catch {}
    if (document.content && !mounted) {
      try { editor.dispose?.(); } catch {}
      host.innerHTML = `<textarea aria-label="Code editor fallback" spellcheck="false">${escapeHTML(document.content)}</textarea>`;
      const area = host.querySelector("textarea");
      activeCodeEditor = { documentKey:editor.documentKey, changed:true, getValue:() => area.value, focus:() => area.focus(), dispose:() => {} };
      area.addEventListener("input", () => {});
      toast("Rich editor mounted empty — using the plain editor. Reload the file if this persists.", true);
    }
  }
  $("#workbenchFileForm").addEventListener("submit", saveWorkbenchFile);
  $("#codeAskAI")?.addEventListener("click", () => window.HermetrixAssistant?.action("ask"));
  $("#codeReview").addEventListener("click", () => {
    $("#codeDiff").textContent = codeDiffText(document, activeCodeEditor.getValue());
    $("#codeReviewPanel").open = true;
  });
  $("#codeOutlineToggle").addEventListener("click", event => {
    state.ideOutlineOpen = !state.ideOutlineOpen;
    $(".code-workarea")?.classList.toggle("outline-open", state.ideOutlineOpen);
    event.currentTarget.setAttribute("aria-pressed", String(state.ideOutlineOpen));
    activeCodeEditor?.resize?.();
  });
  $$('[data-code-tab]').forEach(button => button.addEventListener("click", () => openWorkbenchFile(button.dataset.codeTab)));
  $$('[data-code-tab-close]').forEach(button => button.addEventListener("click", () => closeCodeTab(document.projectID, button.dataset.codeTab)));
  $$('[data-code-symbol]').forEach(button => button.addEventListener("click", () => activeCodeEditor?.goToLine(button.dataset.codeSymbol)));
  $$('[data-editor-action]').forEach(button => button.addEventListener("click", () => runEditorAction(button.dataset.editorAction)));
  window.HermetrixWorkspace?.editorMounted(document);
}

async function runEditorAction(action) {
  return window.HermetrixWorkspace?.action(action);
}

async function reloadCodeDocument(projectID, path) {
  captureCodeDraft();
  const cached = codeDrafts.get(codeDraftKey(projectID, path));
  if (cached && cached.content !== cached.originalContent) { toast("This file has unsaved edits. Save or review them before reloading.", true); return; }
  try {
    const document = await api(`/api/projects/${encodeURIComponent(projectID)}/file?path=${encodeURIComponent(path)}`);
    if (state.currentProject?.id !== projectID || state.projectFile?.path !== path) return;
    const fresh = { ...document, projectID, originalContent:document.content };
    codeDrafts.set(codeDraftKey(projectID, path), fresh);
    if (typeof persistCodeDraft === "function") persistCodeDraft(codeDraftKey(projectID, path), fresh);
    state.projectFile = fresh;
    refreshWorkbenchSurface("editor");
  } catch (error) { toast(error.message, true); }
}

function closeCodeTab(projectID, path) {
  captureCodeDraft();
  const tabs = projectCodeTabs(projectID);
  const index = tabs.indexOf(path);
  if (index >= 0) tabs.splice(index, 1);
  if (state.projectFile?.projectID === projectID && state.projectFile?.path === path) {
    const next = tabs[Math.min(index, tabs.length - 1)];
    state.projectFile = null;
    if (next) { void openWorkbenchFile(next); return; }
  }
  refreshWorkbenchSurface("editor");
}

// The listeners below used to be attached at the bottom of the function that
// wrote the markup into the DOM. Now that the HTML can land in either of two
// hosts (#workbenchContent or a pane's .pane-body), binding has to happen
// after whichever host actually received it -- there is nothing on the page
// to attach to before that -- so it is a separate step both callers run
// right after they set innerHTML.
function bindWorkbenchFilesEvents() {
  $("#workbenchFileFilter")?.addEventListener("input", event => { const query=event.target.value.toLowerCase(); $$('[data-workbench-file]').forEach(button => { button.hidden=!button.dataset.workbenchFile.toLowerCase().includes(query); }); });
  $("#workbenchProject")?.addEventListener("change", event => selectProject(event.target.value, ""));
  $("#workbenchFileUp")?.addEventListener("click", () => browseWorkspace((state.projectPath || "").split("/").slice(0,-1).join("/")));
  $$('[data-workbench-file]').forEach(button => button.addEventListener("click", () => button.dataset.directory === "true" ? browseWorkspace(button.dataset.workbenchFile) : openWorkbenchFile(button.dataset.workbenchFile)));
  $("#newWorkbenchFile")?.addEventListener("click", newWorkbenchFile);
}

async function browseWorkspace(path) {
  const projectID = state.currentProject?.id;
  try {
    const files = await api(`/api/projects/${encodeURIComponent(projectID)}/files?path=${encodeURIComponent(path)}`);
    if (state.currentProject?.id !== projectID) return;
    state.projectPath = path;
    state.workspaceFiles = files;
    refreshWorkbenchSurface("files");
  } catch (error) { toast(error.message, true); }
}

// The chat-side workbench room's own entry point: still exactly what callers
// elsewhere in this file expect a "render the files room" call to do.
function renderWorkbenchFiles() {
  $("#workbenchContent").innerHTML = renderWorkbenchFilesHTML();
  bindWorkbenchFilesEvents();
}

let ideLoading;
let codeOpenGeneration = 0;
function ensureIDE() {
  if (window.HermetrixIDE) return Promise.resolve(window.HermetrixIDE);
  if (ideLoading) return ideLoading;
  ideLoading = new Promise((resolve, reject) => {
    if (!document.querySelector('link[href="/vendor/ide.css"]')) {
      const style = document.createElement("link");
      style.rel = "stylesheet";
      style.href = "/vendor/ide.css";
      document.head.appendChild(style);
    }
    const script = document.createElement("script");
    script.src = "/vendor/ide.js";
    script.onload = () => resolve(window.HermetrixIDE);
    script.onerror = () => { script.remove(); ideLoading = null; reject(new Error("โหลดตัวแก้ไขโค้ดไม่สำเร็จ กรุณาลองเปิดไฟล์อีกครั้ง")); };
    document.head.appendChild(script);
  });
  return ideLoading;
}

async function openWorkbenchFile(path) {
  const generation = ++codeOpenGeneration;
  captureCodeDraft();
  const projectID = state.currentProject?.id;
  if (!projectID) return;
  rememberCodeTab(projectID, path);
  try {
    await ensureIDE();
    // A cached draft that is empty over a non-empty original without ever
    // being touched is a stale poisoned read, not the user's work — refetch.
    const key = codeDraftKey(projectID, path);
    const cached = codeDrafts.get(key) || persistedCodeDraft(projectID, path);
    if (cached && !codeDrafts.has(key)) codeDrafts.set(key, cached);
    const document = (cached && !(cached.content === "" && cached.originalContent && !cached.touched))
      ? cached
      : await api(`/api/projects/${encodeURIComponent(projectID)}/file?path=${encodeURIComponent(path)}`);
    if (state.currentProject?.id !== projectID || generation !== codeOpenGeneration) return;
    state.projectFile = { ...document, projectID, originalContent: document.originalContent ?? document.content };
    state.projectFileDiff = "";
    openContentPane("editor");
    requestAnimationFrame(() => activeCodeEditor?.focus());
  } catch (error) { toast(error.message, true); }
}

async function newWorkbenchFile() {
  const projectID = state.currentProject?.id;
  const path = await askAction({title:"New file",message:"File name or path inside this project.",confirmLabel:"Create",reasonLabel:"File path"});
  if (!path || state.currentProject?.id !== projectID) return;
  // Read an existing file first so a new-file draft cannot mask its contents.
  try { await api(`/api/projects/${encodeURIComponent(projectID)}/file?path=${encodeURIComponent(path.trim())}`); await openWorkbenchFile(path.trim()); return; }
  catch (error) { if (!/not exist|not found|no such file/i.test(error.message)) { toast(error.message, true); return; } }
  captureCodeDraft();
  try { await ensureIDE(); } catch (error) { toast(error.message, true); return; }
  if (state.currentProject?.id !== projectID) return;
  state.projectFile = {projectID,path:path.trim(),content:"",originalContent:"",sha256:"",mode:"0644",bytes:0};
  state.projectFileDiff = "";
  openContentPane("editor");
}

async function saveWorkbenchFile(event) {
  event?.preventDefault();
  captureCodeDraft();
  let document = state.projectFile;
  if (!document || document.projectID !== state.currentProject?.id) return;
  try {
    if (ideFormatOnSaveEnabled() && window.HermetrixWorkspace?.supports("format", document.path)) {
      if (activeCodeEditor?.setValue && activeCodeEditor.documentKey === codeDraftKey(document.projectID, document.path)) {
        if (!await window.HermetrixWorkspace.format(document)) return null;
        captureCodeDraft();
        document = state.projectFile;
        if (!document || document.projectID !== state.currentProject?.id) return null;
      } else {
        toast("Rich editor unavailable — saving without formatting", true);
      }
    }
    const result = await api(`/api/projects/${encodeURIComponent(document.projectID)}/file`, {method:"PUT",body:JSON.stringify({path:document.path,content:document.content,expected_sha256:document.sha256 || "",actor:currentActor()})});
    const saved = {...result.document, projectID:document.projectID, originalContent:result.document.content};
    // A save may finish after more typing or a project switch.
    captureCodeDraft();
    const key = codeDraftKey(document.projectID, document.path);
    const latest = codeDrafts.get(key);
    const updated = {...saved, content:latest?.content ?? saved.content};
    codeDrafts.set(key, updated);
    if (typeof persistCodeDraft === "function") persistCodeDraft(key, updated);
    if (state.currentProject?.id === document.projectID && state.projectFile?.path === document.path) {
      state.projectFile = updated;
      state.projectFileDiff = result.diff;
      window.HermetrixWorkspace?.dirty();
    }
    toast("File saved");
    return updated;
  } catch (error) { toast(error.message, true); window.HermetrixWorkspace?.feedback(error.message, true); return null; }
}

function stripANSI(value="") {
  return String(value)
    .replace(/\x1B\][\s\S]*?(?:\x07|\x1B\\)/g, "")
    .replace(/\x1B\[[0-?]*[ -/]*[@-~]/g, "")
    .replace(/\x1B(?:[=>]|[ -/]*[@-~])/g, "")
    .replace(/[^\n]\x08/g, "")
    .replace(/[\x00-\x08\x0B\x0C\x0E-\x1F\x7F]/g, "");
}

const terminalStarts = new Map();
const terminalAutoAttempted = new Set();
function projectTerminals() { return state.terminals.filter(item => item.project_id === state.currentProject?.id && item.state === "running"); }
function renderWorkbenchTerminalHTML() {
  if (state.runtimeCapabilities?.interactive_terminal !== true) {
    const message = state.runtimeCapabilities?.interactive_terminal === false
      ? "Interactive terminal is unavailable in this build. Use your system terminal in the project folder."
      : "Terminal support could not be verified. Refresh to check the runtime again.";
    return `<div class="workspace-terminal"><div class="probe-empty" role="status"><h3>Terminal unavailable</h3><p>${message}</p>${state.currentProject?.root_path ? `<code>${escapeHTML(state.currentProject.root_path)}</code>` : ""}</div></div>`;
  }
  const terminals = projectTerminals();
  let terminal = terminals.find(item => item.id === state.selectedTerminal);
  if (!terminal) {
    terminal = terminals.find(item => item.state === "running");
    state.selectedTerminal = terminal?.id || null;
    state.terminalOutput = "";
  }
  return `<div class="workspace-terminal">
    <div class="terminal-tabs">${terminals.map((item, index) => `<button class="ghost ${item.id === terminal?.id ? "active" : ""}" data-terminal-id="${escapeHTML(item.id)}">${escapeHTML(item.shell)} ${index + 1}</button>`).join("")}<button class="ghost" id="terminalNew" ${!state.currentProject?.root_path || terminalStarts.has(state.currentProject?.id) ? "disabled" : ""}>+ Terminal</button></div>
    ${terminal ? `<div class="terminal-screen" id="terminalScreen" role="application" aria-label="Interactive terminal"></div><div class="terminal-actions"><span>Interactive PTY · type, paste, Tab, arrows and Ctrl-C work directly</span><button class="ghost" id="terminalClose">Close terminal</button></div>` : `<div class="probe-empty">${state.currentProject?.root_path ? (terminalAutoAttempted.has(state.currentProject.id) && !terminalStarts.has(state.currentProject.id) ? "Could not open a terminal. Use + Terminal to retry." : "Opening terminal in your project…") : "This project has no code folder."}</div>`}
  </div>`;
}

function bindWorkbenchTerminalEvents() {
  if (state.runtimeCapabilities?.interactive_terminal !== true) return;
  $("#terminalNew")?.addEventListener("click", () => startProjectTerminal(true));
  $$('[data-terminal-id]').forEach(button => button.addEventListener("click", () => { state.selectedTerminal=button.dataset.terminalId; terminalCursor={id:"",value:0}; refreshWorkbenchSurface("terminal"); }));
  $("#terminalClose")?.addEventListener("click", closeWorkbenchTerminal);
  const projectID = state.currentProject?.id;
  if (state.selectedTerminal) mountWorkbenchTerminal();
  if (state.currentProject?.root_path && !projectTerminals().some(item => item.state === "running") &&
      !terminalAutoAttempted.has(projectID)) {
    terminalAutoAttempted.add(projectID);
    void startProjectTerminal();
  }
}

async function mountWorkbenchTerminal() {
  const id = state.selectedTerminal;
  const screen = $("#terminalScreen");
  if (!id || !screen) return;
  try { await ensureIDE(); } catch (error) { toast(error.message, true); return; }
  if (!screen.isConnected || state.selectedTerminal !== id || !terminalPaneOpen()) return;
  activeTerminalEmulator?.dispose();
  activeTerminalEmulator = null;
  terminalCursor = { id, value: 0 };
  if (window.HermetrixIDE?.createTerminal) {
    activeTerminalEmulator = window.HermetrixIDE.createTerminal(screen, {
      onData: input => sendRawTerminalInput(input, id),
      onResize: (columns, rows) => resizeTerminalTo(id, columns, rows)
    });
  }
  void pollTerminal();
}

function renderWorkbenchTerminal() {
  $("#workbenchContent").innerHTML = renderWorkbenchTerminalHTML();
  bindWorkbenchTerminalEvents();
}

async function startProjectTerminal(force = false) {
  if (state.runtimeCapabilities?.interactive_terminal !== true) {
    toast("Interactive terminal is unavailable. Use your system terminal in the project folder.", true);
    return null;
  }
  const project = state.currentProject;
  if (!project?.root_path || terminalStarts.has(project.id)) return null;
  const existing = projectTerminals().find(item => item.state === "running");
  if (!force && existing) return existing;
  terminalAutoAttempted.add(project.id);
  terminalStarts.set(project.id, true);
  try {
    const terminal = await api("/api/terminals", {method:"POST",body:JSON.stringify({project_id:project.id,working_dir:".",actor:currentActor(),columns:100,rows:30})});
    state.terminals = [...state.terminals.filter(item => item.id !== terminal.id), terminal];
    if (state.currentProject?.id === project.id) {
      state.selectedTerminal = terminal.id;
      state.terminalOutput = "";
    }
    return terminal;
  } catch (error) { toast(error.message, true); }
  finally {
    terminalStarts.delete(project.id);
    if (state.currentProject?.id === project.id) refreshWorkbenchSurface("terminal");
  }
  return null;
}

async function pollTerminal() {
  const id=state.selectedTerminal;
  if (!id || !terminalPaneOpen()) return;
  if (terminalPollInFlight) { scheduleWorkbenchPoll(pollTerminal, 160); return; }
  const screen = $("#terminalScreen");
  if (!screen) return;
  terminalPollInFlight = true;
  try {
    const cursor = terminalCursor.id === id ? terminalCursor.value : 0;
    const output=await api(`/api/terminals/${encodeURIComponent(id)}/output?cursor=${cursor}`);
    if (state.selectedTerminal !== id || !terminalPaneOpen() || screen !== $("#terminalScreen")) return;
    const terminal=state.terminals.find(item => item.id===id);
    if (terminal) Object.assign(terminal,{state:output.state,exit_code:output.exit_code,error:output.error,cursor:output.cursor});
    if (output.truncated) activeTerminalEmulator?.reset();
    terminalCursor = { id, value: Number(output.cursor || cursor) };
    if (activeTerminalEmulator) activeTerminalEmulator.write(output.output || "");
    else {
      state.terminalOutput = cursor ? state.terminalOutput + (output.output || "") : (output.output || "");
      if(screen){screen.textContent=stripANSI(state.terminalOutput);screen.scrollTop=screen.scrollHeight;}
    }
    if (output.state === "running") scheduleWorkbenchPoll(pollTerminal, output.output ? 160 : 800);
  } catch(error){if (terminalPaneOpen() && screen === $("#terminalScreen")) toast(error.message,true);}
  finally { terminalPollInFlight = false; }
}

function sendRawTerminalInput(input, id = state.selectedTerminal) {
  if (!id || !input) return terminalInputChain;
  terminalInputChain = terminalInputChain
    .catch(() => {})
    .then(() => api(`/api/terminals/${encodeURIComponent(id)}/input`,{method:"POST",body:JSON.stringify({input})}))
    .then(() => { if (state.selectedTerminal === id) scheduleWorkbenchPoll(pollTerminal,40); })
    .catch(error => toast(error.message,true));
  return terminalInputChain;
}
function resizeTerminalTo(id, columns, rows) {
  clearTimeout(terminalResizeTimer);
  // ResizeObserver fires once while a pane is still being laid out. xterm can
  // report 2x1 in that frame; it is not a usable terminal size and the server
  // intentionally rejects it. Wait for the next stable observation instead
  // of showing a false error toast.
  if (columns < 20 || columns > 500 || rows < 5 || rows > 200) return;
  terminalResizeTimer = setTimeout(() => {
    void api(`/api/terminals/${encodeURIComponent(id)}/resize`, {method:"POST",body:JSON.stringify({columns,rows})})
      .catch(error => toast(error.message,true));
  }, 80);
}
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

async function openWorkbenchBrowser(event){event.preventDefault();const form=new FormData(event.currentTarget);try{const tab=await api("/api/browser/tabs",{method:"POST",body:JSON.stringify({project_id:form.get("project_id"),url:form.get("url"),allow_private:form.get("allow_private")==="on",actor:currentActor()})});state.browserTabs.unshift(tab);state.selectedBrowserTab=tab.id;refreshWorkbenchSurface("browser");}catch(error){toast(error.message,true);}}
async function browserWorkbenchAction(action,ref=0,text=""){const tab=state.browserTabs.find(item=>item.id===state.selectedBrowserTab);if(!tab)return;const url=action==="navigate"?new FormData($("#browserOpenForm")).get("url"):"";try{const updated=await api(`/api/browser/tabs/${encodeURIComponent(tab.id)}/actions`,{method:"POST",body:JSON.stringify({action,url,ref,text,actor:currentActor()})});state.browserTabs=state.browserTabs.map(item=>item.id===updated.id?updated:item);state.selectedBrowserTab=updated.id;refreshWorkbenchSurface("browser");}catch(error){toast(error.message,true);}}
async function typeBrowserElement(ref){const text=await askAction({title:`Type into browser element ${ref}`,message:"The value is sent only to this exact element reference on the active managed tab.",confirmLabel:"Type value",reasonLabel:"Text"});if(text===null)return;await browserWorkbenchAction("type",ref,text);}

// Files, terminal and browser can each be showing in one of two places (a
// chat-side workbench room, or a Code pane) or neither. A callback bound
// inside their markup has no way to know which one raised it -- it is a
// listener on a DOM node, not a closure over the render call that made that
// node -- so it asks state instead of assuming a fixed target, and does
// nothing if the content in question is not actually on screen anywhere.
function refreshWorkbenchSurface(id) {
  if (state.panes.includes(id) && $("#workspacePaneHost")) renderPanes();
}

// Terminal and browser now live only as pane content in Code. Anything that
// used to jump straight to their old side-strip room -- the composer's quick
// button, the command palette -- opens or reveals a pane instead, so there
// is exactly one door into either room rather than two.
function openContentPane(id, {persist = true} = {}) {
  if (state.view === "code" && state.paneLayout === "ide" && ["files", "chat", "git", "environment"].includes(id)) {
    window.HermetrixWorkspace?.panel(id);
    return;
  }
  // The chat-side workbench is hidden on narrow screens; reveal content in
  // the main workspace there so Files/Terminal never open an invisible pane.
  if (state.view === "chat" && window.innerWidth <= 920) switchView("code");
  state.compactPane = id;
  if (!state.panes.length) state.panes = ["review"];
  if (!state.panes.includes(id)) {
    if (state.panes.length < MAX_PANES) state.panes.push(id);
    else state.panes[state.panes.length - 1] = id;
  }
  state.maximisedPane = null;
  if (state.view === "chat") collapseZone("side", false);
  renderPanes();
  if (persist) saveLayout();
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

const taskActions = new Map();
const taskErrors = new Map();
const taskLeaseTimers = new Map();
const taskLeaseRenewals = new Map();
const taskProposalBodies = new Map();

function taskDecisionLab(taskID) {
  state.taskDecisionLabs ||= {};
  return state.taskDecisionLabs[taskID] ||= {loading:false, loaded:false, shadows:[], metrics:null, benchmarks:[], admission:{enabled:false}, progress:null, knowledge:[], recommendation:null};
}

function taskDecisionLabHTML(task) {
  const lab = taskDecisionLab(task.id);
  const locals = taskEligibleProviders(task).filter(taskProviderIsLocal);
  const latest = lab.benchmarks?.[0];
  const policy = lab.admission?.policy;
  const shadow = lab.shadows?.[0];
  const recommendation = lab.recommendation?.decision;
  const progress = lab.progress;
  const knowledge = lab.knowledge || [];
  if (lab.loading && !lab.loaded) return `<details class="inspect-section"><summary>Local decision evaluation</summary><p role="status">Loading decision evidence…</p></details>`;
  return `<details class="inspect-section task-decision-lab"><summary>Local decision evaluation ${policy?.enabled ? pill("read-only enabled", "green") : pill("shadow only", "blue")}</summary>
    <p>Compare the local model with the deterministic rule. Enabling admission only lets it recommend policy-free read actions; it cannot execute tools or edit files.</p>
    ${lab.error ? `<p class="session-error" role="alert">${escapeHTML(lab.error)}</p>` : ""}
    <label>Local decision model<select id="taskDecisionProvider">${taskProviderOptions(locals, policy?.provider_id || latest?.provider_id)}</select></label>
    ${progress ? `<div class="kv"><span>Attempts</span><strong>${progress.attempts}/20</strong><span>Attempts left</span><strong>${progress.remaining_attempts}</strong><span>Planner escalations left</span><strong>${progress.remaining_planner_escalations}</strong><span>Progress state</span><strong>${progress.budget_exhausted ? "budget exhausted" : progress.no_progress ? "repeated failure" : "within budget"}</strong></div>${(progress.blocked_reasons || []).length ? `<p class="session-error">${escapeHTML(progress.blocked_reasons.join(" · "))}</p>` : ""}` : ""}
    <details><summary>Retrieved project knowledge · ${knowledge.length}</summary>${knowledge.length ? knowledge.map(item => `<article class="artifact-mini"><strong>${escapeHTML(item.memory_kind)}</strong><p>${escapeHTML(item.snippet)}</p><small>score ${item.score} · ${escapeHTML((item.matched_terms || []).join(", "))}</small></article>`).join("") : `<p class="form-note neutral">No relevant active user memory matched this task.</p>`}</details>
    <div class="action-row"><button class="ghost" id="taskDecisionShadow" ${locals.length ? "" : "disabled"}>Compare this task</button><button class="ghost" id="taskDecisionBenchmark" ${locals.length ? "" : "disabled"}>Run 6-case benchmark</button></div>
    ${shadow ? `<div class="kv"><span>Latest comparison</span><strong>${shadow.agreement ? "agreed" : shadow.valid ? "disagreed" : "invalid"}</strong><span>Rule</span><code>${escapeHTML(shadow.baseline?.action_id || "—")}</code><span>Model</span><code>${escapeHTML(shadow.model?.action_id || shadow.model_error || "—")}</code><span>Latency</span><span>${Number(shadow.latency_ms || 0).toLocaleString()} ms</span></div>` : `<p class="form-note neutral">No comparison receipt for this task yet.</p>`}
    ${lab.metrics ? `<p class="form-note neutral">${lab.metrics.total_runs} task comparisons · ${Math.round((lab.metrics.agreement_rate || 0) * 100)}% agreement · ${Math.round(lab.metrics.average_latency_ms || 0)} ms average</p>` : ""}
    ${latest ? `<div class="kv"><span>Latest benchmark</span><strong>${latest.correct_cases}/${latest.total_cases} correct</strong><span>Invalid</span><strong>${Math.round((latest.invalid_rate || 0) * 100)}%</strong><span>Average latency</span><strong>${Math.round(latest.average_latency_ms || 0)} ms</strong><span>Gate</span><strong>${latest.passed ? "passed" : "failed"}</strong></div>` : `<p class="form-note neutral">Run the fixed corpus before admission can be enabled.</p>`}
    <div class="action-row">${policy?.enabled ? `<button class="danger" id="taskDecisionAdmission" data-enabled="false">Disable read-only selector</button><button class="primary" id="taskDecisionRecommend">Ask for read-only recommendation</button>` : `<button class="primary" id="taskDecisionAdmission" data-enabled="true" ${latest?.passed ? "" : "disabled"}>Enable read-only selector</button>`}</div>
    ${recommendation ? `<p class="task-readiness"><strong>Recommendation:</strong> ${escapeHTML(recommendation.action_id)} · ${escapeHTML(recommendation.reason || "")} ${recommendation.fallback_used ? "(deterministic fallback)" : ""}</p>` : ""}
  </details>`;
}

function projectDurableTasks() {
  return state.durableTasks.filter(item => !state.currentProject?.id || item.project_id === state.currentProject.id);
}

function taskProviderIsLocal(provider) {
  try {
    const host = new URL(provider.base_url).hostname.toLowerCase();
    return host === "localhost" || host === "[::1]" || /^127\.\d+\.\d+\.\d+$/.test(host) || /^\[::ffff:7f[0-9a-f]{2}:[0-9a-f]+\]$/.test(host);
  } catch { return false; }
}

function taskEligibleProviders(task, implementerID = "") {
  const implementer = state.providers.find(item => item.id === implementerID);
  return state.providers.filter(item => item.enabled && item.credential_ready &&
    (task?.egress_policy === "remote_allowed" || taskProviderIsLocal(item)) &&
    (!implementerID || (item.id !== implementerID && implementer &&
      (String(item.base_url).trim().toLowerCase() !== String(implementer.base_url).trim().toLowerCase() ||
       String(item.model).trim().toLowerCase() !== String(implementer.model).trim().toLowerCase()))));
}

function taskProviderOptions(providers, preferredID = "") {
  return providers.map(item => `<option value="${escapeHTML(item.id)}" ${item.id === preferredID ? "selected" : ""}>${escapeHTML(item.name)} · ${escapeHTML(item.model)}</option>`).join("");
}

function taskRunIsLive(run) {
  return run?.state === "running" && !!run.lease_token && Date.parse(run.lease_expires_at) > Date.now();
}

function taskCanStartStep(task, execution) {
  if ((execution.effects || []).some(effect => ["planned", "dispatched", "uncertain"].includes(effect.state))) return false;
  const currentRun = !execution.run || execution.run.plan_revision === task.active_plan_revision;
  if (currentRun && execution.proposal && execution.proposal.state !== "verified") return false;
  return task.state === "ready" || task.state === "paused" ||
    (task.state === "running" && taskRunIsLive(execution.run) &&
      (!execution.attempt || ["running", "completed"].includes(execution.attempt.state)));
}

function taskStage(task, execution) {
  if (task.state === "completed") return {number:5, label:"Complete", detail:"All planned steps, checks and independent reviews passed."};
  if (!task.active_plan_revision) return {number:2, label:"Create a plan", detail:"Generate proposed steps, then review their scope and checks before starting."};
  const proposal = execution?.run?.plan_revision === task.active_plan_revision ? execution.proposal : null;
  if (proposal?.state === "awaiting_post_review") return {number:5, label:"Independent review", detail:"Checks passed. A different model or endpoint must review the evidence."};
  if (proposal?.state === "applied") return {number:4, label:"Run checks", detail:"Changes are applied. Run the commands recorded in the plan to collect evidence."};
  if (proposal && ["pending_review", "approved"].includes(proposal.state)) return {number:3, label:"Review changes", detail:"Inspect the proposed file contents before approving and applying them."};
  return {number:3, label:"Work through the plan", detail:"Generate a change for one step at a time. No files change until you approve and apply it."};
}

async function renewTaskAuthority(taskID) {
  if (taskLeaseRenewals.has(taskID)) return taskLeaseRenewals.get(taskID);
  const run = state.taskExecutions[taskID]?.run;
  if (!taskRunIsLive(run)) throw new Error("This run's lease has expired. Its saved work is retained; recover the run before applying further changes.");
  const renewal = (async () => {
    const updated = await api(`/api/task-runs/${encodeURIComponent(run.id)}/lease`, {method:"POST", body:JSON.stringify({lease_token:run.lease_token, lease_seconds:900})});
    const execution = state.taskExecutions[taskID];
    if (execution?.run?.id === updated.id) { execution.run = updated; delete execution.leaseError; }
    return {run_id:updated.id, lease_token:updated.lease_token};
  })();
  taskLeaseRenewals.set(taskID, renewal);
  try { return await renewal; } finally { taskLeaseRenewals.delete(taskID); }
}

function maintainTaskLease(task, execution, body) {
  const active = task?.state === "running" && execution?.run?.plan_revision === task.active_plan_revision && taskRunIsLive(execution?.run);
  for (const [id, entry] of taskLeaseTimers) {
    if (id !== task?.id || entry.body !== body || !body.isConnected || !active) {
      clearInterval(entry.timer); taskLeaseTimers.delete(id);
    }
  }
  if (!active || taskLeaseTimers.has(task.id)) return;
  const timer = setInterval(async () => {
    if (!body.isConnected || state.selectedDurableTask !== task.id || state.taskExecutions[task.id]?.task?.state === "completed") {
      clearInterval(timer); taskLeaseTimers.delete(task.id); return;
    }
    try { await renewTaskAuthority(task.id); }
    catch (error) {
      clearInterval(timer); taskLeaseTimers.delete(task.id);
      if (state.taskExecutions[task.id]) state.taskExecutions[task.id].leaseError = error.message;
      if (body.isConnected && !taskActions.has(task.id)) renderTaskCockpit(body);
    }
  }, 60000);
  taskLeaseTimers.set(task.id, {timer, body});
}

function taskProposalHTML(proposal) {
  if (!proposal) return "";
  const content = taskProposalBodies.get(proposal.artifact_id);
  const link = `<a class="button-link" href="/api/artifacts/${encodeURIComponent(proposal.artifact_id)}/content" target="_blank" rel="noreferrer">Open full proposal</a>`;
  if (!content) return `<p role="status">Loading proposed changes…</p>${link}`;
  if (content.error) return `<p class="session-error">${escapeHTML(content.error)}</p><button class="ghost" id="taskReload">Retry loading changes</button>${link}`;
  return `<section class="task-proposal-preview"><h4>${escapeHTML(content.summary || "Proposed changes")}</h4>
    ${(content.risks || []).length ? `<p class="form-note">Risks: ${escapeHTML(content.risks.join(" · "))}</p>` : ""}
    ${(content.assumptions || []).length ? `<p>Assumptions: ${escapeHTML(content.assumptions.join(" · "))}</p>` : ""}
    ${(content.changes || []).map(change => `<details class="tool-receipt"><summary><strong>${escapeHTML(change.path)}</strong><span>Proposed file</span></summary><pre>${escapeHTML(change.content)}</pre></details>`).join("")}${link}</section>`;
}

function taskExecutionHTML(task, execution) {
  if (!execution) return `<div class="probe-empty" role="status">Loading task progress…</div>`;
  if (execution.error) return `<div class="probe-empty">${escapeHTML(execution.error)} <button class="ghost" id="taskReload">Retry</button></div>`;
  const proposal = execution.proposal;
  const providers = taskEligibleProviders(task);
  const uncertainEffects = (execution.effects || []).filter(effect => effect.state === "uncertain");
  const busy = taskActions.get(task.id);
  const expired = task.state === "running" && execution.run?.plan_revision === task.active_plan_revision && !taskRunIsLive(execution.run);
  let action = "";
  if (uncertainEffects.length) {
    action = `<p>${uncertainEffects.length} action(s) need recovery after a restart. Reconciliation only inspects durable local evidence; it never replays the action.</p><button class="primary" id="taskReconcileEffects">Check saved results</button>`;
  } else if (expired || execution.leaseError) {
    action = `<p class="session-error" role="alert">${escapeHTML(execution.leaseError || "This run's lease expired. Saved proposals and evidence are retained, but execution cannot continue until the run is recovered.")}</p><button class="primary" id="taskRecoverExpiredRun">Recover saved work</button>`;
  } else if (taskCanStartStep(task, execution)) {
    const nextStep = (task.plan?.steps || []).find(step => step.state !== "completed" && step.state !== "skipped");
    action = `<form id="taskProposalForm"><h4>${proposal?.state === "verified" ? "Continue to the next step" : "Ready to start"}${nextStep ? ` · ${escapeHTML(nextStep.title)}` : ""}</h4>
      <label>Model<select name="provider_id" required>${taskProviderOptions(providers, $("#chatProvider")?.value)}</select></label>
      <details><summary>Choose files manually</summary><label>One project-relative path per line<textarea name="files" rows="3" placeholder="Leave empty for bounded automatic selection&#10;internal/service.go"></textarea></label></details>
      <p>Review the checks above. Generated commands have not been verified against this project yet.</p>
      <button class="primary" ${providers.length ? "" : "disabled"}>${proposal?.state === "verified" ? "Start next step" : "Generate proposed changes"}</button></form>`;
  } else if (proposal?.state === "pending_review") {
    const ready = taskProposalBodies.get(proposal.artifact_id)?.changes?.length;
    action = `${taskProposalHTML(proposal)}<div class="action-row"><button class="primary" data-task-proposal-decision="approved" ${ready ? "" : "disabled"}>Approve proposal</button><button class="danger" data-task-proposal-decision="rejected">Reject proposal</button></div>`;
  } else if (proposal?.state === "approved") {
    action = `${taskProposalHTML(proposal)}<button class="primary" id="taskApplyProposal">Apply approved changes</button>`;
  } else if (proposal?.state === "applied") {
    action = `<p>Run the exact checks listed in this plan. Results will be saved; a failed check triggers rollback.</p><button class="primary" id="taskVerifyProposal">Run checks</button>`;
  } else if (proposal?.state === "awaiting_post_review") {
    const reviewers = taskEligibleProviders(task, proposal.provider_id);
    action = `${reviewers.length ? "" : `<p class="session-error">Add an independent ${task.egress_policy === "remote_allowed" ? "" : "local "}model in Models. The reviewer needs a different model or endpoint and working credentials.</p><button class="ghost" data-task-models>Open Models</button>`}<label>Reviewer<select id="taskReviewerProvider">${taskProviderOptions(reviewers)}</select></label><button class="primary" id="taskPostReview" ${reviewers.length ? "" : "disabled"}>Review results</button>`;
  } else if (task.state === "completed") {
    action = `<p>All steps completed with passing checks and independent review.</p>`;
  } else if (proposal && ["rejected", "verification_failed", "post_review_rejected", "apply_failed", "recovery_required"].includes(proposal.state)) {
    action = `<p class="session-error">${escapeHTML(proposal.state.replaceAll("_", " "))}. Inspect the saved evidence and revise the plan before another attempt.</p>${taskProposalHTML(proposal)}`;
  }
  if (!action && !busy && !execution.run) return "";
  return `<section class="inspect-section task-next-action"><fieldset ${busy ? "disabled" : ""}><legend>Next action</legend>${busy ? `<p role="status">${escapeHTML(busy)}…</p>` : ""}${action}</fieldset>
    <details><summary>Execution details and evidence</summary><div class="kv"><span>Run</span><strong>${escapeHTML(execution.run?.state || "not started")}</strong><span>Attempt</span><strong>${escapeHTML(execution.attempt?.state || "not started")}</strong><span>Proposal</span><strong>${escapeHTML(proposal?.state || "none")}</strong><span>Actions recorded</span><strong>${(execution.effects || []).length}</strong></div>
    ${(execution.effects || []).map(effect => `<p>${escapeHTML(effect.action)} · ${escapeHTML(effect.state)}${effect.error ? ` · ${escapeHTML(effect.error)}` : ""}</p>`).join("")}
    ${proposal?.verification_artifact_id ? `<a class="button-link" href="/api/artifacts/${encodeURIComponent(proposal.verification_artifact_id)}/content" target="_blank" rel="noreferrer">Open check evidence</a>` : ""}</details></section>`;
}

function renderTaskCockpit(body) {
  const tasks = projectDurableTasks();
  const selected = tasks.find(item => item.id === state.selectedDurableTask) || tasks[0];
  if (selected) state.selectedDurableTask = selected.id;
  const steps = selected?.plan?.steps || [];
  const execution = selected ? state.taskExecutions[selected.id] : undefined;
  const providers = taskEligibleProviders(selected);
  const stage = selected ? taskStage(selected, execution) : null;
  const busy = taskActions.has(selected?.id) || taskActions.has("create");
  const draft = state.taskDraftDetails || {};
  const draftObjective = state.taskDraftObjective || "";
  const canReplan = selected && selected.state !== "completed" && !busy &&
    !(execution?.effects || []).some(effect => ["planned", "dispatched", "uncertain"].includes(effect.state)) &&
    (!execution?.proposal || ["rejected", "verification_failed", "post_review_rejected", "apply_failed"].includes(execution.proposal.state)) &&
    (selected.state !== "running" || !taskRunIsLive(execution?.run) || execution?.proposal?.state === "rejected");
  body.innerHTML = `<div class="panel task-cockpit"><div class="provider-head"><div><p class="eyebrow">Tasks</p><h3>Turn a goal into checked work</h3><p>Describe the outcome, review a plan, then follow each step.</p></div>${selected ? pill(selected.state.replaceAll("_", " "), selected.state === "completed" ? "green" : "blue") : ""}</div>
    ${taskErrors.get(selected?.id) || taskErrors.get("create") ? `<div class="task-feedback" role="alert">${escapeHTML(taskErrors.get(selected?.id) || taskErrors.get("create"))}</div>` : ""}
    ${tasks.length ? `<label>Task<select id="durableTaskSelect">${tasks.map(task => `<option value="${escapeHTML(task.id)}" ${task.id === selected.id ? "selected" : ""}>${escapeHTML(task.title)} · ${escapeHTML(task.state.replaceAll("_", " "))}</option>`).join("")}</select></label>` : ""}
    <details class="task-create" ${!selected || draftObjective ? "open" : ""}><summary>${selected ? "+ New task" : "1 · Describe the task"}</summary><form id="durableTaskForm"><fieldset ${taskActions.has("create") ? "disabled" : ""}>
      <label>What should be achieved?<textarea name="objective" required rows="3" placeholder="Describe what you want to build or fix">${escapeHTML(draftObjective)}</textarea></label>
      <label>How will you know it works?<textarea name="criteria" required rows="3" placeholder="One observable result per line">${escapeHTML(draft.criteria || "")}</textarea></label>
      <details><summary>More details</summary><label>Short title · optional<input name="title" maxlength="160" value="${escapeHTML(draft.title || "")}"></label><label>Constraints · one per line<textarea name="constraints" rows="2">${escapeHTML(draft.constraints || "")}</textarea></label><label>Open questions · one per line<textarea name="unknowns" rows="2">${escapeHTML(draft.unknowns || "")}</textarea></label></details>
      <p>Project: ${escapeHTML(state.currentProject?.name || "Choose a project first")}. Task data stays with local models.</p><button class="primary" ${state.currentProject?.id ? "" : "disabled"}>${taskActions.has("create") ? "Creating…" : "Create task"}</button></fieldset></form></details>
    ${selected ? `<section class="inspect-section"><div class="provider-head"><div><h3>${escapeHTML(selected.title)}</h3><p>${escapeHTML(selected.objective)}</p></div><span>${steps.filter(step => step.state === "completed").length}/${steps.length} steps</span></div>
      <p class="eyebrow">${stage.number} · ${escapeHTML(stage.label)}</p><p>${escapeHTML(stage.detail)}</p>
      ${selected.pause_reason ? `<p class="session-error">${escapeHTML(selected.pause_reason)}</p>` : ""}
      <details><summary>Success criteria and constraints</summary><ul>${(selected.requirement?.criteria || []).map(item => `<li>${escapeHTML(item.description)}</li>`).join("")}</ul>${(selected.requirement?.constraints || []).map(item => `<p>${escapeHTML(item)}</p>`).join("")}${(selected.requirement?.unknowns || []).map(item => `<p>Open question: ${escapeHTML(item)}</p>`).join("")}</details>
      ${!providers.length ? `<p class="session-error">No ready ${selected.egress_policy === "remote_allowed" ? "" : "local "}model is configured for this task. Connect one or fix its credentials in Models.</p><button class="ghost" data-task-models>Open Models</button>` : ""}
      ${providers.length && !providers.some(provider => taskEligibleProviders(selected, provider.id).length) ? `<p class="task-readiness">วางแผนและเสนอการแก้ไขได้ แต่การตรวจงานขั้นสุดท้ายต้องเพิ่มโมเดลหรือ endpoint อิสระอีกหนึ่งตัวใน Models</p>` : ""}
      ${!steps.length ? `<label>Planning model<select id="taskPlannerProvider">${taskProviderOptions(providers)}</select></label><button class="primary" id="taskAutoPlan" ${busy || !providers.length ? "disabled" : ""}>${taskActions.get(selected.id) || "Create plan"}</button>` : `<ol class="task-plan-steps">${steps.map(step => `<li><details ${step.state === "completed" ? "" : "open"}><summary><strong>${escapeHTML(step.title)}</strong> ${pill(step.state, step.state === "completed" ? "green" : "blue")}</summary><p>${escapeHTML(step.instructions)}</p><p><strong>Checks to run</strong></p><ul>${(step.checks || []).map(check => `<li><code>${escapeHTML(check)}</code></li>`).join("")}</ul><small>Success criteria: ${escapeHTML((step.requirement_ids || []).join(", "))}</small></details></li>`).join("")}</ol><p class="form-note neutral">The plan proposes a path; feasibility is established by actual checks and review. Commands run directly without a shell.</p>`}
      ${steps.length && canReplan ? `<details><summary>Revise this plan</summary><p>Generate a new plan from the same goal and success criteria. Review it before starting again.</p><label>Planning model<select id="taskPlannerProvider">${taskProviderOptions(providers)}</select></label><button class="ghost" id="taskAutoPlan" ${providers.length ? "" : "disabled"}>Generate revised plan</button></details>` : ""}
    </section>${taskExecutionHTML(selected, execution)}${taskDecisionLabHTML(selected)}` : ""}</div>`;
  $("#durableTaskForm")?.addEventListener("submit", createDurableTask);
  $("#durableTaskForm")?.addEventListener("input", event => {
    const form = new FormData(event.currentTarget);
    state.taskDraftObjective = String(form.get("objective") || "");
    state.taskDraftDetails = Object.fromEntries(["title", "criteria", "constraints", "unknowns"].map(name => [name, String(form.get(name) || "")]));
  });
  if (draftObjective && !selected) $("#durableTaskForm textarea[name='objective']")?.focus();
  $("#durableTaskSelect")?.addEventListener("change", async event => {
    state.selectedDurableTask = event.target.value;
    await loadTaskExecution(state.selectedDurableTask);
    if (body.isConnected) renderTaskCockpit(body);
  });
  $$('[data-task-models]', body).forEach(button => button.addEventListener("click", () => openConfig("providers")));
  $("#taskAutoPlan")?.addEventListener("click", () => autoPlanDurableTask(body));
  $("#taskProposalForm")?.addEventListener("submit", event => startTaskProposal(event, body));
  $$('[data-task-proposal-decision]', body).forEach(button => button.addEventListener("click", () => decideTaskProposal(button.dataset.taskProposalDecision, body)));
  $("#taskApplyProposal")?.addEventListener("click", () => applyTaskProposal(body));
  $("#taskVerifyProposal")?.addEventListener("click", () => verifyTaskProposal(body));
  $("#taskPostReview")?.addEventListener("click", () => postReviewTaskProposal(body));
  $("#taskReconcileEffects")?.addEventListener("click", () => reconcileTaskEffects(body));
  $("#taskRecoverExpiredRun")?.addEventListener("click", () => recoverExpiredTaskRun(body));
  $("#taskDecisionShadow")?.addEventListener("click", () => compareTaskDecision(body));
  $("#taskDecisionBenchmark")?.addEventListener("click", () => benchmarkTaskDecision(body));
  $("#taskDecisionAdmission")?.addEventListener("click", event => changeTaskDecisionAdmission(event, body));
  $("#taskDecisionRecommend")?.addEventListener("click", () => recommendTaskDecision(body));
  $("#taskReload")?.addEventListener("click", () => refreshDurableTask(selected.id, body));
  maintainTaskLease(selected, execution, body);
  if (selected && !Object.prototype.hasOwnProperty.call(state.taskExecutions, selected.id)) {
    state.taskExecutions[selected.id] = null;
    loadTaskExecution(selected.id).then(() => { if (body.isConnected && state.selectedDurableTask === selected.id) renderTaskCockpit(body); });
  }
  if (selected && !taskDecisionLab(selected.id).loaded && !taskDecisionLab(selected.id).loading) {
    loadTaskDecisionLab(selected.id).then(() => { if (body.isConnected && state.selectedDurableTask === selected.id) renderTaskCockpit(body); });
  }
}

async function loadTaskDecisionLab(taskID) {
  if (!taskID) return;
  const lab = taskDecisionLab(taskID);
  lab.loading = true;
  delete lab.error;
  try {
    const task = state.durableTasks.find(item => item.id === taskID);
    const [shadows, metrics, benchmarks, admission, progress, knowledge] = await Promise.all([
      api(`/api/tasks/${encodeURIComponent(taskID)}/decision-shadows?limit=10`),
      api(`/api/tasks/${encodeURIComponent(taskID)}/decision-shadow-metrics`),
      api("/api/decision/benchmarks?limit=20"),
      api("/api/decision/admission"),
      api(`/api/tasks/${encodeURIComponent(taskID)}/progress?revision=${encodeURIComponent(task?.revision || 0)}`),
      api(`/api/tasks/${encodeURIComponent(taskID)}/knowledge?revision=${encodeURIComponent(task?.revision || 0)}`),
    ]);
    Object.assign(lab, {shadows:asList(shadows), metrics, benchmarks:asList(benchmarks), admission, progress, knowledge:asList(knowledge?.matches), loaded:true});
  } catch (error) { lab.error = error.message; }
  finally { lab.loading = false; }
}

function selectedTaskDecisionProvider() {
  return $("#taskDecisionProvider")?.value || "";
}

async function compareTaskDecision(body) {
  const task = state.durableTasks.find(item => item.id === state.selectedDurableTask);
  const providerID = selectedTaskDecisionProvider();
  if (!task || !providerID) return;
  await performTaskAction(task.id, body, "Comparing local decisions", async () => {
    await api(`/api/tasks/${encodeURIComponent(task.id)}/decision-shadow`, {method:"POST", body:JSON.stringify({expected_task_revision:task.revision, provider_id:providerID})});
    taskDecisionLab(task.id).loaded = false;
    await loadTaskDecisionLab(task.id);
    toast("Decision comparison receipt saved");
  });
}

async function benchmarkTaskDecision(body) {
  const task = state.durableTasks.find(item => item.id === state.selectedDurableTask);
  const providerID = selectedTaskDecisionProvider();
  if (!task || !providerID) return;
  await performTaskAction(task.id, body, "Running decision benchmark", async () => {
    await api("/api/decision/benchmarks", {method:"POST", body:JSON.stringify({provider_id:providerID})});
    taskDecisionLab(task.id).loaded = false;
    await loadTaskDecisionLab(task.id);
    toast("Six-case decision benchmark recorded");
  });
}

async function changeTaskDecisionAdmission(event, body) {
  const task = state.durableTasks.find(item => item.id === state.selectedDurableTask);
  const lab = task && taskDecisionLab(task.id);
  const enabled = event.currentTarget.dataset.enabled === "true";
  const providerID = enabled ? selectedTaskDecisionProvider() : lab?.admission?.policy?.provider_id;
  const benchmarkRunID = enabled ? lab?.benchmarks?.find(item => item.provider_id === providerID && item.passed)?.id : lab?.admission?.policy?.benchmark_run_id;
  if (!task || !providerID || !benchmarkRunID) return;
  const reason = await askAction({title:enabled ? "Enable read-only model selection?" : "Disable read-only model selection?", message:enabled ? "This admits the exact benchmarked provider revision for recommendations limited to policy-free read actions. It never runs the action." : "New recommendations will use the deterministic rule path only.", confirmLabel:enabled ? "Enable read-only" : "Disable", reasonLabel:"Decision reason", danger:!enabled});
  if (!reason) return;
  await performTaskAction(task.id, body, enabled ? "Enabling read-only selector" : "Disabling read-only selector", async () => {
    await api("/api/decision/admission", {method:"PUT", body:JSON.stringify({provider_id:providerID, benchmark_run_id:benchmarkRunID, actor:currentActor(), reason, enabled})});
    taskDecisionLab(task.id).loaded = false;
    await loadTaskDecisionLab(task.id);
    toast(enabled ? "Read-only selector enabled" : "Read-only selector disabled");
  });
}

async function recommendTaskDecision(body) {
  const task = state.durableTasks.find(item => item.id === state.selectedDurableTask);
  if (!task) return;
  await performTaskAction(task.id, body, "Asking read-only selector", async () => {
    taskDecisionLab(task.id).recommendation = await api(`/api/tasks/${encodeURIComponent(task.id)}/decision-read-only`, {method:"POST", body:JSON.stringify({expected_task_revision:task.revision})});
    toast("Read-only recommendation ready; no action was executed");
  });
}

async function loadTaskExecution(taskID) {
  if (!taskID) return;
  try {
    const execution = await api(`/api/tasks/${encodeURIComponent(taskID)}/execution`);
    state.taskExecutions[taskID] = execution;
    const artifactID = execution.proposal?.artifact_id;
    if (artifactID && (!taskProposalBodies.has(artifactID) || taskProposalBodies.get(artifactID).error)) {
      try { taskProposalBodies.set(artifactID, await api(`/api/artifacts/${encodeURIComponent(artifactID)}/content`)); }
      catch (error) { taskProposalBodies.set(artifactID, {error:error.message}); }
    }
  } catch (error) { state.taskExecutions[taskID] = {error:error.message, effects:[]}; }
}

async function refreshDurableTask(taskID, body) {
  await loadTaskExecution(taskID);
  const task = state.taskExecutions[taskID]?.task;
  if (task) state.durableTasks = state.durableTasks.map(item => item.id === taskID ? task : item);
  if (body.isConnected && state.selectedDurableTask === taskID) renderTaskCockpit(body);
}

async function performTaskAction(taskID, body, label, action) {
  if (taskActions.has(taskID)) return;
  taskErrors.delete(taskID);
  taskActions.set(taskID, label);
  if (body.isConnected) renderTaskCockpit(body);
  try { await action(); }
  catch (error) { taskErrors.set(taskID, error.message); toast(error.message, true); }
  finally { taskActions.delete(taskID); await refreshDurableTask(taskID, body); }
}

async function reconcileTaskEffects(body) {
  const taskID = state.selectedDurableTask;
  const effects = (state.taskExecutions[taskID]?.effects || []).filter(effect => effect.state === "uncertain");
  if (!taskID || !effects.length) return;
  await performTaskAction(taskID, body, "Checking saved results", async () => {
    for (const effect of effects) await api(`/api/task-effects/${encodeURIComponent(effect.operation_id)}/reconcile`, {method:"POST", body:"{}"});
    toast("Saved results checked; no action was replayed");
  });
}

async function recoverExpiredTaskRun(body) {
  const taskID = state.selectedDurableTask;
  const task = state.durableTasks.find(item => item.id === taskID);
  if (!task || taskActions.has(taskID)) return;
  await performTaskAction(taskID, body, "Recovering saved work", async () => {
    const recovered = await api(`/api/tasks/${encodeURIComponent(taskID)}/recover-expired-run`, {method:"POST", body:JSON.stringify({expected_task_revision:task.revision, actor:currentActor()})});
    state.durableTasks = state.durableTasks.map(item => item.id === recovered.id ? recovered : item);
    toast("Saved work recovered without replaying any action");
  });
}

async function startTaskProposal(event, body) {
  event.preventDefault();
  const task = state.durableTasks.find(item => item.id === state.selectedDurableTask);
  const form = new FormData(event.currentTarget);
  const files = String(form.get("files") || "").split("\n").map(value => value.trim()).filter(Boolean);
  const providerID = String(form.get("provider_id") || "");
  if (!task || !taskEligibleProviders(task).some(item => item.id === providerID)) return;
  await performTaskAction(task.id, body, "Generating proposed changes", async () => {
    let execution = state.taskExecutions[task.id] || {effects:[]};
    let run = task.state === "running" && execution.run?.state === "running" && execution.run.plan_revision === task.active_plan_revision ? execution.run : null;
    let attempt = execution.attempt?.state === "running" && execution.attempt.run_id === run?.id ? execution.attempt : null;
    let packet = attempt?.packet || null;
    if (!run) {
      const started = await api(`/api/tasks/${encodeURIComponent(task.id)}/runs`, {method:"POST", body:JSON.stringify({expected_task_revision:task.revision, owner:currentActor(), lease_seconds:900})});
      run = started.run;
      state.durableTasks = state.durableTasks.map(item => item.id === task.id ? started.task : item);
      state.taskExecutions[task.id] = {...execution, run, task:started.task};
    }
    const authority = await renewTaskAuthority(task.id);
    maintainTaskLease(state.taskExecutions[task.id].task || task, state.taskExecutions[task.id], body);
    if (!attempt) {
      packet = await api(`/api/tasks/${encodeURIComponent(task.id)}/next-packet`);
      attempt = await api(`/api/task-runs/${encodeURIComponent(run.id)}/attempts`, {method:"POST", body:JSON.stringify({
        lease_token:authority.lease_token, step_key:packet.step.key, expected_task_revision:packet.task_revision,
        expected_step_revision:packet.step.revision, input_hash:packet.canonical_packet_hash, packet
      })});
    }
    if (!attempt.packet) throw new Error("This attempt has no saved step packet. Recover it before continuing.");
    if (!files.length) {
      const selection = await api(`/api/task-attempts/${encodeURIComponent(attempt.id)}/select-files`, {method:"POST", body:JSON.stringify({provider_id:providerID, authority})});
      files.push(...(selection.result?.files || []));
      if (!files.length) throw new Error("No eligible files were selected. Choose the files for this step manually.");
    }
    await api(`/api/task-attempts/${encodeURIComponent(attempt.id)}/proposals`, {method:"POST", body:JSON.stringify({provider_id:providerID, files, authority:await renewTaskAuthority(task.id)})});
    toast("Proposed changes are ready to inspect");
  });
}

async function decideTaskProposal(verdict, body) {
  const taskID = state.selectedDurableTask;
  const proposal = state.taskExecutions[taskID]?.proposal;
  if (!proposal || taskActions.has(taskID)) return;
  if (verdict === "approved" && !taskProposalBodies.get(proposal.artifact_id)?.changes?.length) return;
  const rationale = await askAction({title:verdict === "approved" ? "Approve these changes?" : "Reject these changes?", message:"This records your review of the proposed files. Applying the changes is the next step.", confirmLabel:verdict === "approved" ? "Approve" : "Reject", reasonLabel:"Review note", danger:verdict === "rejected"});
  if (!rationale) return;
  await performTaskAction(taskID, body, "Saving review", async () => {
    await api(`/api/task-code-proposals/${encodeURIComponent(proposal.id)}/decision`, {method:"POST", body:JSON.stringify({actor:currentActor(), verdict, rationale, findings:[]})});
  });
}

async function applyTaskProposal(body) {
  const taskID = state.selectedDurableTask;
  const proposal = state.taskExecutions[taskID]?.proposal;
  if (!proposal || taskActions.has(taskID)) return;
  const confirmed = await askAction({title:"Apply approved changes?", message:"The original files will be checked against the proposal and rollback evidence saved before writing.", confirmLabel:"Apply changes"});
  if (!confirmed) return;
  await performTaskAction(taskID, body, "Applying changes", async () => {
    await api(`/api/task-code-proposals/${encodeURIComponent(proposal.id)}/apply`, {method:"POST", body:JSON.stringify({actor:currentActor(), authority:await renewTaskAuthority(taskID)})});
    toast("Changes applied. Run checks next.");
  });
}

async function verifyTaskProposal(body) {
  const taskID = state.selectedDurableTask;
  const proposal = state.taskExecutions[taskID]?.proposal;
  if (!proposal || taskActions.has(taskID)) return;
  await performTaskAction(taskID, body, "Running planned checks", async () => {
    await api(`/api/task-code-proposals/${encodeURIComponent(proposal.id)}/verify-frozen`, {method:"POST", body:JSON.stringify({actor:currentActor(), authority:await renewTaskAuthority(taskID)})});
    toast("Check results saved");
  });
}

async function postReviewTaskProposal(body) {
  const taskID = state.selectedDurableTask;
  const task = state.durableTasks.find(item => item.id === taskID);
  const proposal = state.taskExecutions[taskID]?.proposal;
  const providerID = $("#taskReviewerProvider")?.value;
  if (!proposal || !taskEligibleProviders(task, proposal.provider_id).some(item => item.id === providerID)) return;
  await performTaskAction(taskID, body, "Reviewing check results", async () => {
    const output = await api(`/api/task-code-proposals/${encodeURIComponent(proposal.id)}/post-review`, {method:"POST", body:JSON.stringify({provider_id:providerID, authority:await renewTaskAuthority(taskID)})});
    toast(output.review?.verdict === "reject" ? "Review rejected the changes. Inspect the evidence." : "Independent review passed");
  });
}

async function createDurableTask(event) {
  event.preventDefault();
  if (taskActions.has("create")) return;
  const body = event.currentTarget.closest?.(".pane-body");
  const form = new FormData(event.currentTarget);
  const lines = name => String(form.get(name) || "").split("\n").map(value => value.trim()).filter(Boolean);
  const criteria = lines("criteria").map((description, index) => ({id:`AC-${index + 1}`, description}));
  const objective = String(form.get("objective") || "").trim();
  if (!criteria.length || !objective || !state.currentProject?.id) return;
  taskActions.set("create", "Creating task");
  taskErrors.delete("create");
  const submit = event.currentTarget.querySelector('button:not([type]), button[type="submit"]');
  if (submit) submit.disabled = true;
  try {
    const task = await api("/api/tasks", {method:"POST", body:JSON.stringify({project_id:state.currentProject.id,
      title:String(form.get("title") || "").trim() || objective.split("\n")[0].slice(0,80), objective, original_request:objective,
      criteria, constraints:lines("constraints"), unknowns:lines("unknowns"), egress_policy:"local_only", actor:currentActor()})});
    state.durableTasks.unshift(task);
    state.selectedDurableTask = task.id;
    state.taskDraftObjective = "";
    state.taskDraftDetails = {};
    taskActions.delete("create");
    renderPanes();
    toast("Task created. Create a plan next.");
  } catch (error) { taskErrors.set("create", error.message); toast(error.message, true); }
  finally { taskActions.delete("create"); if (submit?.isConnected) submit.disabled = false; if (taskErrors.has("create") && body?.isConnected) renderTaskCockpit(body); }
}

async function autoPlanDurableTask(body) {
  const task = state.durableTasks.find(item => item.id === state.selectedDurableTask);
  const providerID = $("#taskPlannerProvider")?.value;
  if (!task || !taskEligibleProviders(task).some(item => item.id === providerID)) return;
  await performTaskAction(task.id, body, "Creating a plan", async () => {
    const output = await api(`/api/tasks/${encodeURIComponent(task.id)}/auto-plan`, {method:"POST", body:JSON.stringify({expected_task_revision:task.revision, provider_id:providerID, actor:currentActor()})});
    state.durableTasks = state.durableTasks.map(item => item.id === task.id ? output.task : item);
    toast("Plan created. Review the steps and checks before starting.");
  });
}


// Four is the ceiling because a fifth pane on one screen is smaller than the
// thing inside it, and because a bounded number is a number that can be
// tested. This is a split, not a tiling manager.
const MAX_PANES = 4;

// Every real workspace surface is available in every slot. A content type is
// mounted once at a time because its forms have stable IDs; selecting a type
// already open in another slot swaps the two instead of creating duplicate
// controls with ambiguous event targets.
const PANE_CONTENT = [
  { id: "chat", icon: "chat", label: "Chat" },
  { id: "tasks", icon: "activity", label: "Tasks" },
  { id: "editor", icon: "file", label: "Code" },
  { id: "review", icon: "review", label: "Review" },
  { id: "files", icon: "files", label: "Files" },
  { id: "git", icon: "project", label: "Git" },
  { id: "environment", icon: "settings", label: "Tooling" },
  { id: "terminal", icon: "terminal", label: "Terminal" },
  { id: "browser", icon: "browser", label: "Browser" },
  { id: "artifacts", icon: "artifact", label: "Office" },
  { id: "team", icon: "project", label: "Team" },
  { id: "output", icon: "activity", label: "Output" }
  ,{ id: "debug", icon: "activity", label: "Debug" }
];

function paneContent(id) {
  return PANE_CONTENT.find(item => item.id === id) || PANE_CONTENT[0];
}

const PANE_LAYOUTS = {
  1: [{ id: "single", label: "Single pane" }],
  2: [{ id: "ide", label: "Editor + sidebar" }, { id: "columns", label: "Side by side" }, { id: "rows", label: "Stacked" }],
  3: [
    { id: "ide", label: "AI workspace" },
    { id: "bottom-wide", label: "2 top · 1 bottom" },
    { id: "top-wide", label: "1 top · 2 bottom" },
    { id: "left-wide", label: "1 left · 2 right" },
    { id: "right-wide", label: "2 left · 1 right" }
  ],
  4: [{ id: "ide", label: "AI workspace + console" }, { id: "quad", label: "2 × 2 grid" }]
};

function normalisePaneLayout(count, requested = state.paneLayout) {
  const options = PANE_LAYOUTS[count] || PANE_LAYOUTS[1];
  return options.some(item => item.id === requested) ? requested : options[0].id;
}

function paneToolbarHTML() {
  const count = state.panes.length || 1;
  const layout = normalisePaneLayout(count);
  const options = PANE_LAYOUTS[count] || PANE_LAYOUTS[1];
  const planning = state.maximisedPane !== null && state.panes[state.maximisedPane] === "tasks";
  return `<header class="workspace-toolbar ${planning ? "planning-toolbar" : layout === "ide" ? "ide-toolbar" : ""}"><div><strong>${planning ? "Plans · แผนงาน" : "Workspace"}</strong><small>${planning ? "เป้าหมาย → แผน → ทบทวนการแก้ไข → ตรวจผล" : layout === "ide" ? "ไฟล์ → เขียนโค้ด → รันและตรวจผล · AI บนเครื่อง" : "ลากหัวช่องเพื่อย้าย · ลากเส้นเพื่อปรับขนาด"}</small></div>
    ${layout === "ide" && !planning ? `<div class="ide-mode-switch" role="group" aria-label="Workspace mode"><button type="button" id="ideAgentMode">Agent</button><button type="button" class="active" aria-current="page">Editor</button></div>` : ""}
    <div class="workspace-actions">
      ${planning ? "" : `<button class="ghost compact" id="workspaceIDE">AI workspace</button><button class="ghost compact" data-ide-panel="terminal">Terminal</button><button class="ghost compact" data-ide-panel="output">Output</button><button class="ghost compact" data-ide-panel="debug">Debug</button>`}
      ${options.length > 1 ? `<label class="pane-layout-control"><span>Layout</span><select id="paneLayoutSelect" aria-label="Workspace layout">${options.map(option => `<option value="${option.id}" ${option.id === layout ? "selected" : ""}>${option.label}</option>`).join("")}</select></label>` : ""}
      <span id="paneCountLabel">${count}/4 panes</span>
      <button class="ghost compact" id="paneAdd">${uiIcon("plus")}<span>Split</span></button>
    </div>${planning ? "" : `<nav class="compact-pane-tabs" aria-label="Workspace panels">${state.panes.map(id => `<button type="button" data-compact-pane="${id}" aria-pressed="${id === compactPaneID()}">${escapeHTML(paneContent(id).label)}</button>`).join("")}</nav>`}</header>`;
}

function ideActivityHTML() {
  const side = state.panes[1];
  return `<nav class="ide-activity" aria-label="Editor sidebar">${[
    ["files", "files", "Explorer"], ["chat", "chat", "Local AI"],
    ["git", "project", "Git"], ["environment", "settings", "Tooling"]
  ].map(([id, icon, label]) => `<button type="button" data-ide-side="${id}" aria-label="${label}" title="${label}" aria-pressed="${side === id}">${uiIcon(icon)}</button>`).join("")}</nav>`;
}

function compactPaneID() { return state.panes.includes(state.compactPane) ? state.compactPane : state.panes[0]; }
function compactWorkspace() { return window.innerWidth <= (state.paneLayout === "ide" ? 900 : 700); }

function paneDividerHTML(count, layout) {
  if (layout === "ide") return "";
  if (state.maximisedPane !== null || count < 2) return "";
  const vertical = `<div class="pane-divider vertical" data-pane-divider="vertical" role="separator" aria-orientation="vertical" aria-label="Resize workspace columns" tabindex="0"></div>`;
  const horizontal = `<div class="pane-divider horizontal" data-pane-divider="horizontal" role="separator" aria-orientation="horizontal" aria-label="Resize workspace rows" tabindex="0"></div>`;
  if (count === 2) return layout === "rows" ? horizontal : vertical;
  return `${vertical}${horizontal}`;
}

function paneDropGuidesHTML(count) {
  if (state.maximisedPane !== null || count < 2 || count > 3) return "";
  return `<div class="pane-drop-guides" aria-hidden="true">
    ${["top", "right", "bottom", "left"].map(edge => `<div class="pane-drop-edge ${edge}" data-pane-drop-edge="${edge}">${edge}</div>`).join("")}
  </div>`;
}

// A chat pane brings the conversation into the Workspace view: the same
// turn pipeline as the Chat view composer, with a compact read-only tail of
// the selected session. Full receipts stay in Chat; this pane is for asking
// next to the code.
function paneChatHTML() {
  const session = state.sessionDetail?.session;
  if (!session) return `<div class="pane-chat-empty"><h3>No session selected</h3><p>Pick one in the rail — new turns always land on the selected session.</p></div>`;
  const messages = (state.sessionDetail?.events || [])
    .filter(item => item.event_kind === "message" && (item.role === "user" || item.role === "assistant"))
    .slice(-8);
  return `<div class="pane-chat-head"><strong>${escapeHTML(session.title)}</strong><small>${escapeHTML(session.provider_name)} · ${escapeHTML(session.context_profile)}</small></div>
  <div class="pane-chat-log">${messages.map(item => `<article class="pane-chat-msg ${item.role}"><span>${item.role === "user" ? "You" : "Hermetrix"}</span><p>${escapeHTML((item.content || "").slice(0, 600))}</p></article>`).join("") || `<p class="form-note neutral">No messages yet — ask below.</p>`}</div>
  <form class="pane-chat-form"><textarea class="pane-chat-input" rows="2" maxlength="1048576" placeholder="Ask Hermetrix… Enter sends" ${state.sending ? "disabled" : ""}>${escapeHTML(state.paneChatDraft || "")}</textarea><button class="primary" ${state.sending ? "disabled" : ""}>${state.sending ? "Running…" : "Send"}</button></form>`;
}
function renderPaneChat(body) {
  if (window.HermetrixAssistant) { window.HermetrixAssistant.render(body); return; }
  body.innerHTML = paneChatHTML();
  bindPaneChat(body);
}
function bindPaneChat(body) {
  const input = body.querySelector(".pane-chat-input");
  const form = body.querySelector(".pane-chat-form");
  if (!input || !form) return;
  input.addEventListener("input", () => { state.paneChatDraft = input.value; });
  input.addEventListener("keydown", event => {
    if (event.key === "Enter" && !event.shiftKey && !event.altKey && !event.isComposing) {
      event.preventDefault();
      form.requestSubmit();
    }
  });
  form.addEventListener("submit", async event => {
    event.preventDefault();
    const content = input.value.trim();
    if (!content || state.sending) return;
    input.value = "";
    state.paneChatDraft = "";
    const ok = await submitChatText(content);
    if (!ok) {
      state.paneChatDraft = content;
      const box = body.querySelector(".pane-chat-input");
      if (box) box.value = content;
    }
    refreshPaneChat();
  });
  const log = body.querySelector(".pane-chat-log");
  if (log) log.scrollTop = log.scrollHeight;
}
// refreshPaneChat redraws mounted chat panes without a full renderPanes — a
// full rebuild would destroy the CodeMirror instance next door on every turn.
// A pane holding keyboard focus is left alone so typing is never clobbered.
function refreshPaneChat() {
  if (window.HermetrixAssistant) { window.HermetrixAssistant.refresh(); return; }
  for (const body of document.querySelectorAll('.pane-body[data-pane-kind="chat"]')) {
    if (body.contains(document.activeElement)) continue;
    renderPaneChat(body);
  }
}

const ideGitSnapshots = new Map();
const IDE_WORD_WRAP_KEY = "hermetrix.ide.wordWrap";
const IDE_FORMAT_ON_SAVE_KEY = "hermetrix.ide.formatOnSave";

function ideFormatOnSaveEnabled() {
  if (typeof state.ideFormatOnSave === "boolean") return state.ideFormatOnSave;
  try { state.ideFormatOnSave = localStorage.getItem(IDE_FORMAT_ON_SAVE_KEY) === "true"; }
  catch { state.ideFormatOnSave = false; }
  return state.ideFormatOnSave;
}

function ideWordWrapEnabled() {
  if (typeof state.ideWordWrap === "boolean") return state.ideWordWrap;
  try { state.ideWordWrap = localStorage.getItem(IDE_WORD_WRAP_KEY) !== "false"; }
  catch { state.ideWordWrap = true; }
  return state.ideWordWrap;
}

function renderPaneEnvironment(body) {
  const projectID = state.currentProject?.id;
  body.innerHTML = `<div class="ide-side-panel"><header><h3>ตั้งค่า Editor</h3></header><label class="ide-setting-toggle"><span><strong>จัดรูปแบบตอนบันทึก</strong><small>จัดรูปแบบไฟล์ที่รองรับก่อนเขียนลงดิสก์</small></span><input type="checkbox" data-editor-format-save ${ideFormatOnSaveEnabled() ? "checked" : ""}></label><label class="ide-setting-toggle"><span><strong>ตัดบรรทัดยาว</strong><small>แสดงโค้ดภายในความกว้างของหน้าต่าง โดยไม่แก้เนื้อหาไฟล์</small></span><input type="checkbox" data-editor-wrap ${ideWordWrapEnabled() ? "checked" : ""}></label><p class="ide-side-note">Format ด้วย Alt+Shift+F และบันทึกด้วย Ctrl+S</p><header><h3>เครื่องมือในเครื่อง</h3><button class="ghost compact" data-env-refresh>Refresh</button></header><p>เครื่องมือที่ตรวจพบสำหรับโปรเจกต์นี้</p><div data-env-result role="status">กำลังตรวจสอบ…</div><p class="ide-side-note">การเติมคำปัจจุบันอ้างอิงไฟล์ที่เปิดอยู่ ยังไม่มีระบบจัดการ Language server ในแอป</p></div>`;
  body.querySelector("[data-editor-format-save]").addEventListener("change", event => {
    state.ideFormatOnSave = event.target.checked;
    try { localStorage.setItem(IDE_FORMAT_ON_SAVE_KEY, String(state.ideFormatOnSave)); } catch {}
  });
  body.querySelector("[data-editor-wrap]").addEventListener("change", event => {
    state.ideWordWrap = event.target.checked;
    try { localStorage.setItem(IDE_WORD_WRAP_KEY, String(state.ideWordWrap)); } catch {}
    activeCodeEditor?.setWrap?.(state.ideWordWrap);
    const fallback = $("#workbenchFileContent textarea");
    if (fallback) fallback.wrap = state.ideWordWrap ? "soft" : "off";
  });
  const load = async () => {
    const result = body.querySelector("[data-env-result]");
    if (!result) return;
    result.textContent = "กำลังตรวจสอบ…";
    try {
      const capabilities = await api(`/api/projects/${encodeURIComponent(projectID)}/ide`);
      if (!body.isConnected || state.currentProject?.id !== projectID) return;
      const tools = Object.entries(capabilities.tools || {}).sort(([a], [b]) => a.localeCompare(b));
      result.innerHTML = `<div class="ide-tool-list">${tools.map(([name, ready]) => `<div><strong>${escapeHTML(name)}</strong><span class="${ready ? "ready" : ""}">${ready ? "พร้อมใช้" : "ไม่พบ"}</span></div>`).join("") || "ยังไม่มีข้อมูลเครื่องมือ"}</div>`;
    } catch (error) { if (body.isConnected) result.textContent = error.message; }
  };
  body.querySelector("[data-env-refresh]").addEventListener("click", load);
  void load();
}

function renderPaneGit(body) {
  const projectID = state.currentProject?.id;
  const snapshot = ideGitSnapshots.get(projectID);
  const statusLines = (snapshot?.status || "").split("\n").filter(Boolean);
  const branch = statusLines[0]?.startsWith("## ") ? statusLines.shift().slice(3).replace(/^No commits yet on /, "") : "";
  const commits = (snapshot?.log || "").split("\n").filter(Boolean);
  const content = snapshot?.error ? `<p class="session-error">${escapeHTML(snapshot.error)}</p>` : snapshot?.loading || !snapshot ? "กำลังอ่าน Git…" :
    `<div class="ide-git-branch">${uiIcon("review")}<strong>${escapeHTML(branch || "Git")}</strong><small>${statusLines.length} ไฟล์เปลี่ยน</small></div>
    <section class="ide-git-section"><h4>ไฟล์ที่เปลี่ยน <small>${statusLines.length}</small></h4>${statusLines.length ? `<div class="ide-git-files">${statusLines.slice(0, 100).map(line => `<div><span class="ide-git-code">${escapeHTML(line.slice(0, 2).trim() || "?")}</span><span title="${escapeHTML(line.slice(3))}">${escapeHTML(line.slice(3))}</span></div>`).join("")}</div>${statusLines.length > 100 ? `<small>แสดง 100 ไฟล์แรก</small>` : ""}` : `<p class="ide-side-note">ไม่มีไฟล์ที่เปลี่ยน</p>`}</section>
    <section class="ide-git-section"><h4>ไทม์ไลน์ <small>${commits.length}</small></h4>${commits.length ? `<ol class="ide-git-timeline">${commits.map(line => { const space = line.indexOf(" "); return `<li><code>${escapeHTML(space < 0 ? line : line.slice(0, space))}</code><span>${escapeHTML(space < 0 ? "" : line.slice(space + 1))}</span></li>`; }).join("")}</ol>` : `<p class="ide-side-note">ยังไม่มี commit</p>`}</section>`;
  body.innerHTML = `<div class="ide-side-panel ide-git-panel"><header><h3>Git</h3><button class="ghost compact" data-git-refresh ${snapshot?.loading ? "disabled" : ""}>Refresh</button></header><div data-git-result role="status">${content}</div></div>`;
  body.querySelector("[data-git-refresh]").addEventListener("click", () => void loadPaneGit(projectID, body));
  if (!snapshot) void loadPaneGit(projectID, body);
}

async function loadPaneGit(projectID, body) {
  if (!projectID || ideGitSnapshots.get(projectID)?.loading) return;
  ideGitSnapshots.set(projectID, {loading:true});
  if (body.isConnected) renderPaneGit(body);
  const command = async argumentsList => {
    let job = await api(`/api/projects/${encodeURIComponent(projectID)}/commands`, {method:"POST", body:JSON.stringify({project_id:projectID, actor:currentActor(), executable:"git", arguments:argumentsList, working_dir:".", timeout_seconds:15})});
    for (let attempt = 0; attempt < 60 && ["queued", "running"].includes(job.state); attempt++) {
      await new Promise(resolve => setTimeout(resolve, 250));
      job = await api(`/api/projects/${encodeURIComponent(projectID)}/ide/jobs/${encodeURIComponent(job.id)}`);
    }
    if (job.state !== "completed") throw new Error(job.error || "Git ไม่ตอบกลับภายในเวลาที่กำหนด");
    if (Number(job.result?.exit_code || 0) !== 0) throw new Error(String(job.result?.output || "Git command failed").trim());
    return String(job.result?.output || "").trim();
  };
  try {
    const capabilities = await api(`/api/projects/${encodeURIComponent(projectID)}/ide`);
    if (!capabilities.tools?.git) throw new Error("ไม่พบ Git ใน PATH ของเซิร์ฟเวอร์");
    const status = await command(["status", "--short", "--branch"]);
    let log = "";
    try { log = await command(["log", "-8", "--oneline"]); } catch { /* A new repository has no commits yet. */ }
    ideGitSnapshots.set(projectID, {status, log});
  } catch (error) { ideGitSnapshots.set(projectID, {error:error.message}); }
  if (body.isConnected && state.currentProject?.id === projectID && body.dataset.paneKind === "git") renderPaneGit(body);
}

function mountPaneContent(body, id) {
  if (!body) return;
  if ((SURFACE_DATA[id] || id === "files") && !readySurfaces.has(id)) {
    body.innerHTML = `<div class="probe-empty" role="status">กำลังโหลด…</div>`;
    hydrateSurface(id).then(hydrated => { if (hydrated && body.isConnected) mountPaneContent(body, id); }).catch(error => {
      if (body.isConnected) { body.textContent = error.message; }
    });
    return;
  }
  if (id === "chat") { renderPaneChat(body); return; }
  if (id === "tasks") { renderTaskCockpit(body); return; }
  if (id === "editor") { renderCodeEditor(body); return; }
  if (id === "git") { renderPaneGit(body); return; }
  if (id === "environment") { renderPaneEnvironment(body); return; }
  if (id === "debug") { window.HermetrixWorkspace?.renderDebug(body); return; }
  if (id === "output" && window.HermetrixWorkspace) { window.HermetrixWorkspace.renderOutput(body); return; }
  if (id === "review") { renderWorkbenchReview(body); return; }
  if (id === "files") { body.innerHTML = renderWorkbenchFilesHTML(); bindWorkbenchFilesEvents(); return; }
  if (id === "terminal") { body.innerHTML = renderWorkbenchTerminalHTML(); bindWorkbenchTerminalEvents(); return; }
  if (id === "browser") { body.innerHTML = renderWorkbenchBrowserHTML(); bindWorkbenchBrowserEvents(); return; }
  if (id === "artifacts") { renderWorkbenchArtifacts(body); return; }
  if (id === "team") { renderWorkbenchTeam(body); return; }
  body.innerHTML = renderPaneOutputHTML();
}

function renderPanes() {
  captureCodeDraft();
  stopWorkbenchPolling();
  disposeWorkspaceWidgets();
  if (!state.panes.length) state.panes = ["review"];
  let host = $("#workspacePaneHost");
  if (state.view === "code") {
    $("#zoneMain").innerHTML = `${paneToolbarHTML()}<div class="workspace-pane-host" id="workspacePaneHost" aria-label="Resizable workspace panes"></div>`;
    host = $("#workspacePaneHost");
  } else if (host?.previousElementSibling?.classList.contains("workspace-toolbar")) {
    host.previousElementSibling.outerHTML = paneToolbarHTML();
    host = $("#workspacePaneHost");
  }
  if (!host) return;
  document.documentElement.style.setProperty("--pane-split-x", `${state.paneSplitX}%`);
  document.documentElement.style.setProperty("--pane-split-y", `${state.paneSplitY}%`);
  const count = state.panes.length;
  state.paneLayout = normalisePaneLayout(count);
  host.classList.toggle("ide-host", state.paneLayout === "ide" && state.view === "code");
  host.innerHTML = `<div class="pane-grid pane-layout-${count} pane-arrangement-${state.paneLayout} ${state.maximisedPane === null ? "" : "one-up"}">${
    state.panes.map((id, index) => {
      const hidden = state.maximisedPane !== null ? state.maximisedPane !== index : compactWorkspace() && id !== compactPaneID();
      const item = paneContent(id);
      return `<section class="pane pane-index-${index} ${hidden ? "pane-hidden" : ""}" data-pane="${index}" data-kind="${item.id}">
        <header class="pane-head">
          <button type="button" class="pane-drag-handle" draggable="true" data-pane-drag="${index}" aria-label="Drag ${escapeHTML(item.label)} pane to move it" title="Drag to move · arrow keys also reorder">${uiIcon("grip")}</button>
          ${uiIcon(item.icon)}
          <select data-pane-content="${index}" aria-label="Pane content">${
            PANE_CONTENT.map(option =>
              `<option value="${option.id}" ${option.id === id ? "selected" : ""}>${escapeHTML(option.label)}</option>`).join("")
          }</select>
          <button class="ghost compact" data-pane-max="${index}" aria-label="Maximise this pane" title="Maximise this pane">${
            uiIcon(state.maximisedPane === index ? "contract" : "expand")}</button>
          ${state.panes.length > 1
            ? `<button class="ghost compact" data-pane-close="${index}" aria-label="Close this pane" title="Close this pane">${uiIcon("close")}</button>` : ""}
        </header>
        <div class="pane-body" data-pane-kind="${item.id}"></div>
      </section>`;
    }).join("")
  }${paneDividerHTML(count, state.paneLayout)}${paneDropGuidesHTML(count)}</div>${host.classList.contains("ide-host") ? ideActivityHTML() : ""}`;
  $("#paneCountLabel").textContent = `${count}/4 panes`;
  $("#paneAdd").disabled = count >= MAX_PANES;
  bindPaneControls();
  $$('[data-compact-pane]').forEach(button => button.addEventListener("click", () => { state.compactPane = button.dataset.compactPane; state.maximisedPane = null; renderPanes(); }));
  state.panes.forEach((id, index) => {
    if (state.maximisedPane !== null && state.maximisedPane !== index) return;
    if (state.maximisedPane === null && compactWorkspace() && id !== compactPaneID()) return;
    mountPaneContent($(`.pane[data-pane="${index}"] .pane-body`, host), id);
  });
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
  const next = paneContent(id).id;
  const other = state.panes.indexOf(next);
  if (other >= 0 && other !== index) state.panes[other] = state.panes[index];
  state.panes[index] = next;
  renderPanes();
  saveLayout();
}

function setPaneLayout(layout) {
  state.paneLayout = normalisePaneLayout(state.panes.length, layout);
  state.maximisedPane = null;
  renderPanes();
  saveLayout();
}

function movePane(from, to) {
  if (!Number.isInteger(from) || !Number.isInteger(to) || from === to ||
      from < 0 || to < 0 || from >= state.panes.length || to >= state.panes.length) return;
  const [moved] = state.panes.splice(from, 1);
  state.panes.splice(to, 0, moved);
  state.maximisedPane = null;
  renderPanes();
  saveLayout();
}

function placePaneAtEdge(from, edge) {
  const count = state.panes.length;
  if (count === 2) {
    state.paneLayout = edge === "top" || edge === "bottom" ? "rows" : "columns";
    movePane(from, edge === "right" || edge === "bottom" ? 1 : 0);
    if (from === (edge === "right" || edge === "bottom" ? 1 : 0)) { renderPanes(); saveLayout(); }
    return;
  }
  if (count === 3) {
    const placement = {
      left: ["left-wide", 0], right: ["right-wide", 2],
      top: ["top-wide", 0], bottom: ["bottom-wide", 2]
    }[edge];
    if (!placement) return;
    state.paneLayout = placement[0];
    if (from === placement[1]) { renderPanes(); saveLayout(); }
    else movePane(from, placement[1]);
  }
}

function clearPaneDragState(grid = $(".pane-grid")) {
  state.draggedPane = null;
  grid?.classList.remove("is-reordering");
  $$(".pane-drop-target, .pane-drop-edge.active", grid || document).forEach(node => node.classList.remove("pane-drop-target", "active"));
}

function beginPaneReorder(handle, event) {
  state.draggedPane = Number(handle.dataset.paneDrag);
  const grid = handle.closest(".pane-grid");
  grid?.classList.add("is-reordering");
  event.dataTransfer.effectAllowed = "move";
  event.dataTransfer.setData("text/plain", String(state.draggedPane));
}

function dropPaneOnPane(pane, event) {
  event.preventDefault();
  const from = state.draggedPane;
  const to = Number(pane.dataset.pane);
  clearPaneDragState(pane.closest(".pane-grid"));
  movePane(from, to);
}

function dropPaneAtEdge(edge, event) {
  event.preventDefault();
  const from = state.draggedPane;
  clearPaneDragState(edge.closest(".pane-grid"));
  placePaneAtEdge(from, edge.dataset.paneDropEdge);
}

function maximisePane(index) {
  state.maximisedPane = state.maximisedPane === index ? null : index;
  renderPanes();
  saveLayout();
}

function bindPaneControls() {
  $("#workspaceIDE")?.addEventListener("click", () => window.HermetrixWorkspace?.open());
  $("#ideAgentMode")?.addEventListener("click", () => switchView("chat"));
  $$('[data-ide-side]').forEach(button => button.addEventListener("click", () => window.HermetrixWorkspace?.panel(button.dataset.ideSide)));
  $$('[data-ide-panel]').forEach(button => button.addEventListener("click", () => window.HermetrixWorkspace?.panel(button.dataset.idePanel)));
  $("#paneAdd")?.addEventListener("click", splitPane);
  $("#paneLayoutSelect")?.addEventListener("change", event => setPaneLayout(event.target.value));
  $$("[data-pane-content]").forEach(select =>
    select.addEventListener("change", event => setPaneContent(Number(select.dataset.paneContent), event.target.value)));
  $$("[data-pane-max]").forEach(button =>
    button.addEventListener("click", () => maximisePane(Number(button.dataset.paneMax))));
  $$("[data-pane-close]").forEach(button =>
    button.addEventListener("click", () => closePane(Number(button.dataset.paneClose))));
  $$("[data-pane-divider]").forEach(divider => divider.addEventListener("pointerdown", event => startPaneDrag(divider, event)));
  $$("[data-pane-drag]").forEach(handle => {
    handle.addEventListener("dragstart", event => beginPaneReorder(handle, event));
    handle.addEventListener("dragend", () => clearPaneDragState(handle.closest(".pane-grid")));
    handle.addEventListener("keydown", event => {
      const index = Number(handle.dataset.paneDrag);
      const delta = event.key === "ArrowLeft" || event.key === "ArrowUp" ? -1
        : event.key === "ArrowRight" || event.key === "ArrowDown" ? 1 : 0;
      if (!delta) return;
      event.preventDefault();
      movePane(index, Math.min(state.panes.length - 1, Math.max(0, index + delta)));
    });
  });
  $$(".pane[data-pane]").forEach(pane => {
    pane.addEventListener("dragover", event => { event.preventDefault(); event.dataTransfer.dropEffect = "move"; pane.classList.add("pane-drop-target"); });
    pane.addEventListener("dragleave", event => { if (!pane.contains(event.relatedTarget)) pane.classList.remove("pane-drop-target"); });
    pane.addEventListener("drop", event => dropPaneOnPane(pane, event));
  });
  $$("[data-pane-drop-edge]").forEach(edge => {
    edge.addEventListener("dragover", event => { event.preventDefault(); event.stopPropagation(); edge.classList.add("active"); });
    edge.addEventListener("dragleave", () => edge.classList.remove("active"));
    edge.addEventListener("drop", event => { event.stopPropagation(); dropPaneAtEdge(edge, event); });
  });
}

function startPaneDrag(divider, event) {
  const grid = divider.closest(".pane-grid");
  if (!grid) return;
  divider.setPointerCapture?.(event.pointerId);
  const move = pointer => {
    const rect = grid.getBoundingClientRect();
    if (divider.dataset.paneDivider === "vertical") {
      state.paneSplitX = Math.min(78, Math.max(22, (pointer.clientX - rect.left) / rect.width * 100));
      document.documentElement.style.setProperty("--pane-split-x", `${state.paneSplitX}%`);
    } else {
      state.paneSplitY = Math.min(78, Math.max(22, (pointer.clientY - rect.top) / rect.height * 100));
      document.documentElement.style.setProperty("--pane-split-y", `${state.paneSplitY}%`);
    }
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

function renderWorkbenchArtifacts(target = $(".pane-body[data-pane-kind='artifacts']") || $("#workbenchContent")){
  if(!target)return;
  target.innerHTML=`<div class="panel"><p class="eyebrow">Office deliverables</p><h3>Create a real editable file</h3><form id="deliverableForm"><label>Project<select name="project_id"><option value="">Global</option>${state.projects.map(item=>`<option value="${escapeHTML(item.id)}" ${item.id===state.selectedProject?"selected":""}>${escapeHTML(item.name)}</option>`).join("")}</select></label><div class="form-grid"><label>Format<select name="format"><option>docx</option><option>xlsx</option><option>pptx</option><option>pdf</option></select></label><label>Title<input name="title" required value="Hermetrix report"></label></div><label>Content<textarea name="content" rows="8" required placeholder="Paragraphs; use tab-separated rows for XLSX or --- between PPTX slides"></textarea></label><p class="form-note neutral">DOCX/XLSX/PPTX support Unicode. Native PDF currently fails closed for non-Basic-Latin text instead of generating missing glyphs.</p><button class="primary">Build immutable deliverable</button></form></div><section class="office-preview" id="deliverablePreview"></section><div class="card-list spaced">${state.artifacts.slice(0,30).map(item=>`<article class="artifact-mini"><div class="provider-head"><div><strong>${escapeHTML(item.name)}</strong><p>${escapeHTML(item.mime_type)} · ${Number(item.byte_size).toLocaleString()} B</p></div>${pill(item.kind,"blue")}</div><div class="action-row"><a class="button-link" href="/api/artifacts/${encodeURIComponent(item.id)}/content" target="_blank" rel="noreferrer">Open / download</a></div></article>`).join("")||`<div class="probe-empty">No artifacts yet.</div>`}</div>`;
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

async function createDeliverable(event){event.preventDefault();const form=new FormData(event.currentTarget);const format=form.get("format");const content=String(form.get("content")||"");const body={project_id:form.get("project_id"),format,title:form.get("title"),actor:currentActor(),paragraphs:content.split(/\n+/).filter(Boolean)};if(format==="xlsx")body.rows=content.split("\n").map(row=>row.split("\t"));if(format==="pptx")body.slides=content.split(/\n---\n/).map(block=>{const lines=block.split("\n").filter(Boolean);return{title:lines.shift()||form.get("title"),bullets:lines};});try{const artifact=await api("/api/deliverables",{method:"POST",body:JSON.stringify(body)});state.artifacts=await api("/api/artifacts");toast(`Created ${artifact.name} · ${shortHash(artifact.checksum)}`);renderWorkbenchArtifacts();}catch(error){toast(error.message,true);}}

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

function renderWorkbenchTeam(target = $(".pane-body[data-pane-kind='team']") || $("#workbenchContent")){
  if(!target)return;
  const team=state.teams.find(item=>item.id===state.selectedTeam);
  if(!state.teamDraft || state.teamDraft.sourceID!==(team?.id||"new"))state.teamDraft=newTeamDraft(team||null);
  const draft=state.teamDraft;
  const provider=state.providers.find(item=>item.enabled);
  const profiles=availableProfiles(provider);
  const profile=bestProfileFor(provider,profiles);
  target.innerHTML=`<div class="panel"><div class="provider-head"><div><p class="eyebrow">Reusable roster · explicit authority</p><h3>${draft.id?"Edit":"Create"} Agent Team</h3></div>${draft.id?pill(`revision ${draft.expected_revision}`,"blue"):pill("new roster","green")}</div><label>Roster<select id="teamSelect"><option value="">＋ New team</option>${state.teams.map(item=>`<option value="${escapeHTML(item.id)}" ${item.id===state.selectedTeam?"selected":""}>${escapeHTML(item.name)}</option>`).join("")}</select></label><form id="teamCreateForm"><label>Team name<input name="name" required value="${escapeHTML(draft.name)}"></label><label>Unit rules<textarea name="instructions" rows="3" required>${escapeHTML(draft.instructions)}</textarea></label><section class="team-editor-list">${renderTeamMemberRows(draft)}</section><div class="action-row"><button type="button" class="ghost" id="addTeamMember" ${draft.members.length>=12?"disabled":""}>＋ Add member</button><button class="primary">${draft.id?"Save exact revision":"Save reusable team"}</button></div></form></div>
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

async function saveWorkbenchTeam(event){event.preventDefault();captureTeamDraft();const draft=state.teamDraft;try{const team=await api("/api/teams",{method:"POST",body:JSON.stringify({id:draft.id||"",expected_revision:draft.expected_revision||0,project_id:state.selectedProject||"",name:draft.name,instructions:draft.instructions,actor:currentActor(),members:draft.members})});state.teams=await api("/api/teams");state.selectedTeam=team.id;state.teamDraft=newTeamDraft(team);toast("Reusable team saved with one explicit lead");renderWorkbenchTeam();}catch(error){toast(error.message,true);}}
async function startWorkbenchTeamRun(event){event.preventDefault();const form=new FormData(event.currentTarget);const tasks=$$('[data-team-task]',event.currentTarget).map(row=>({id:row.querySelector('[data-task-field="id"]').value.trim(),member_id:row.querySelector('[data-task-field="member_id"]').value,title:row.querySelector('[data-task-field="title"]').value.trim(),prompt:row.querySelector('[data-task-field="prompt"]').value.trim(),depends_on:row.querySelector('[data-task-field="depends"]').value.split(",").map(value=>value.trim()).filter(Boolean)}));try{const run=await api("/api/team-runs",{method:"POST",body:JSON.stringify({team_id:state.selectedTeam,project_id:state.selectedProject||"",objective:form.get("objective"),provider_id:form.get("provider_id"),context_profile:form.get("context_profile"),qualification_reason:form.get("qualification_reason"),max_parallel:Number(form.get("max_parallel")),actor:currentActor(),tasks})});state.teamRuns.unshift(run);toast("Team run started; child sessions keep independent provenance");renderWorkbenchTeam();}catch(error){toast(error.message,true);}}
async function cancelWorkbenchTeamRun(id){const approved=await askAction({title:"Cancel this team run?",message:"Hermetrix will cancel every active child context and mark queued/running tasks cancelled. Completed child effects are not undone or retried.",confirmLabel:"Cancel team",danger:true});if(!approved)return;try{const run=await api(`/api/team-runs/${encodeURIComponent(id)}/cancel`,{method:"POST",body:JSON.stringify({actor:currentActor()})});state.teamRuns=state.teamRuns.map(item=>item.id===run.id?run:item);toast("Team and active child contexts cancelled");renderWorkbenchTeam();}catch(error){toast(error.message,true);}}
async function decideWorkbenchTeamApproval(runId,taskId,decision){const response=await askAction({title:decision==="approve"?"Approve this child effect once?":"Deny this child effect?",message:"The decision is bound to the exact child approval and arguments hash. The child resumes its existing turn; Hermetrix does not replay its prompt or earlier effects.",confirmLabel:decision==="approve"?"Approve exact effect":"Deny effect",reasonLabel:decision==="deny"?"Reason":"",danger:decision==="deny"});if(!response)return;try{const run=await api(`/api/team-runs/${encodeURIComponent(runId)}/tasks/${encodeURIComponent(taskId)}/approval`,{method:"POST",body:JSON.stringify({actor:currentActor(),decision,reason:decision==="deny"?response:"approved after team preview"})});state.teamRuns=state.teamRuns.map(item=>item.id===run.id?run:item);toast(decision==="approve"?"Child effect approved; DAG resumes from its receipt":"Child effect denied; DAG resumes without mutation");renderWorkbenchTeam();}catch(error){toast(error.message,true);}}
async function pollTeamRuns(){if(!workspacePaneVisible("team"))return;try{const runs=await api("/api/team-runs");if(!workspacePaneVisible("team"))return;state.teamRuns=runs;if(document.activeElement?.closest("#teamCreateForm,#teamRunForm")){scheduleWorkbenchPoll(pollTeamRuns,900);return;}renderWorkbenchTeam();}catch(error){if(workspacePaneVisible("team"))toast(error.message,true);}}

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
    { id:"providers", icon:"model", label:"Models", blurb:"Endpoints, API keys and qualification",
      terms:"provider endpoint api key token openai compatible qualification context window local runtime ollama" }
  ]},
  { group: "Tools", items: [
    { id:"mcp", icon:"tools", label:"MCP server", blurb:"MCP connections and the capability graph",
      terms:"mcp server bearer token streamable http discovery capability tool schema approval" },
    { id:"tools", icon:"tools", label:"เครื่องมือในตัว", blurb:"เครื่องมือที่แชทเรียกใช้ได้",
      terms:"direct built in tools files commands browser skill context discovery เครื่องมือ" }
  ]},
  { group: "Skills", items: [
    { id:"library", icon:"skill", label:"Skill Studio", blurb:"Active Skills and authority policy",
      terms:"skill library active authority policy promote fork scope pinned" },
    { id:"proposals", icon:"review", label:"Proposals", blurb:"Candidates waiting for a decision",
      terms:"candidate proposal review promote reject quarantine", badge:"proposals" },
    { id:"learning", icon:"learning", label:"Learning", blurb:"Background reviews of real turns",
      terms:"learning review queue reviewer evidence", badge:"reviews" },
    { id:"insights", icon:"insights", label:"Insights", blurb:"Curator findings, report only",
      terms:"curator finding stale duplicate consolidation relation" },
    { id:"archive", icon:"archive", label:"Archive", blurb:"Restore archived Skills as candidates",
      terms:"archive restore deleted reversible" }
  ]},
  { group: "Context", items: [
    { id:"context", icon:"context", label:"Context", blurb:"Compile a prompt and read the ledger",
      terms:"context profile budget fragment compile ledger spill token estimate 32k 64k 128k 256k 1m" },
    { id:"fidelity", icon:"fidelity", label:"Fidelity", blurb:"Evidence behind qualified capacity",
      terms:"fidelity recall evidence corpus case run positional" }
  ]},
  { group: "System", items: [
    { id:"discord", icon:"chat", label:"Discord remote", blurb:"สั่งงานผ่าน Discord ของคุณ",
      terms:"discord remote bot gateway รีโมท ดิสคอร์ด" },
    { id:"projects", icon:"project", label:"Projects", blurb:"Bounded workspaces and commands",
      terms:"project workspace root command allowlist file tree" },
    { id:"office", icon:"jobs", label:"Background jobs", blurb:"Long-running work and its receipts",
      terms:"job background queue cancel receipt" },
    { id:"artifacts", icon:"artifact", label:"Artifacts", blurb:"Content-addressed outputs",
      terms:"artifact cas checksum deliverable docx xlsx pptx pdf" },
    { id:"maintenance", icon:"settings", label:"Maintenance", blurb:"Usage, memory, backup and recovery",
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
    return `<button type="button" class="config-nav-item ${active}" data-config-page="${escapeHTML(item.id)}"><span>${uiIcon(item.icon)}</span><span><strong>${escapeHTML(item.label)}</strong><small>${escapeHTML(item.blurb)}</small></span>${badge ? `<b>${badge}</b>` : ""}</button>`;
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
  if (isConfig) { ++navigationGeneration; dismissMobileRail(); }
  state.activeTab = isConfig ? tab : "chat";
  const overlay = $("#configOverlay");
  overlay.hidden = !isConfig;
  document.documentElement.classList.toggle("config-open", isConfig);
  // The settings room is a fixed overlay that covers the header completely;
  // a screen reader does not get that for free, so it is told directly that
  // the header underneath is not part of the page while settings is open.
  $("#appHeader").setAttribute("aria-hidden", String(isConfig));
  $$(".view").forEach(node => node.classList.toggle("active", node.id === `view-${state.activeTab}`));
  syncWorkbenchPolling();
  if (!isConfig) return;
  renderConfigPage(tab);
  hydrateSurface(tab).then(hydrated => { if (hydrated && state.activeTab === tab) renderConfigPage(tab); }).catch(error => {
    if (state.activeTab === tab) { const root = $(`#view-${tab}`); if (root) root.textContent = error.message; }
    toast(error.message, true);
  });
  const item = configItem(tab);
  $("#configTitle").textContent = item.label;
  const onSkillPage = SKILL_PAGES.includes(tab);
  $("#stats").hidden = !onSkillPage || tab === "library";
  $("#libraryIntro").hidden = tab !== "library";
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
  commands.push({ group: "Go to", icon: "chat", title: "Agent Workspace",
    subtitle: "Chat, tool calls and approvals", keywords: "chat session workspace home",
    run: closeConfig });
  for (const section of CONFIG_SECTIONS) {
    for (const item of section.items) {
      commands.push({ group: "Settings", icon: item.icon, title: item.label, subtitle: item.blurb,
        keywords: `${section.group} ${item.terms}`, run: () => switchTab(item.id) });
    }
  }
  for (const [room, title, subtitle] of PALETTE_ROOMS) {
    commands.push({ group: "Workbench", icon: room === "files" ? "files" : room === "artifacts" ? "artifact" : room === "team" ? "tools" : "review", title, subtitle, keywords: `workbench ${room}`,
      run: () => switchWorkbench(room) });
  }
  for (const session of state.sessions.slice(0, 6)) {
    commands.push({ group: "Sessions", icon: "chat", title: session.title,
      subtitle: `${session.model} · ${session.context_profile}`, keywords: `session ${session.model}`,
      run: () => { switchTab("chat"); selectSession(session.id); } });
  }
  commands.push(
    { group: "Actions", icon: "plus", title: "New agent session", subtitle: "Choose a provider and context envelope",
      keywords: "new session start chat", run: () => $("#railNewSession").click() },
    { group: "Actions", icon: "skill", title: "Propose a Skill", subtitle: "Creates a candidate; never an active Skill",
      keywords: "new skill proposal candidate", run: openCandidateDialog },
    { group: "Actions", icon: "settings", title: "Open settings", subtitle: "Models, tools, skills, context and system",
      keywords: "settings configuration preferences config", run: () => openConfig() },
    { group: "Actions", icon: "at", title: "Mention a Skill or tool", subtitle: "Insert a capability into the composer",
      keywords: "skills tools mcp mention capability", run: () => openCapabilityPicker("all") },
    { group: "Actions", icon: "sidebar", title: "Toggle the list pane", subtitle: "Show or hide the rail zone",
      keywords: "rail sessions list toggle zone", run: () => $("#toggleRail").click() },
    { group: "Actions", icon: "workbench", title: "Toggle the evidence pane", subtitle: "Show or hide the side zone",
      keywords: "workbench inspector toggle side zone", run: () => $("#toggleSide").click() },
    { group: "Actions", icon: "context", title: "Toggle density", subtitle: "Compact for a laptop, comfortable for a desktop",
      keywords: "density compact comfortable laptop desktop zoom", run: toggleDensity },
    { group: "Actions", icon: "terminal", title: "Open a terminal pane", subtitle: "Real PTY bound to the project, in Code",
      keywords: "terminal pty shell pane code", run: () => openContentPane("terminal") },
    { group: "Actions", icon: "browser", title: "Open a browser pane", subtitle: "Managed browser with untrusted evidence, in Code",
      keywords: "browser pane code managed", run: () => openContentPane("browser") },
    { group: "Actions", icon: "refresh", title: "Refresh everything", subtitle: "Reload every panel from the server",
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
    markup += `<button type="button" class="command-item ${index === state.commandIndex ? "active" : ""}" data-command-index="${index}"><span>${uiIcon(command.icon)}</span><span><strong>${escapeHTML(command.title)}</strong><small>${escapeHTML(command.subtitle)}</small></span>${command.keys ? `<kbd>${escapeHTML(command.keys)}</kbd>` : ""}</button>`;
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
  return `<button type="button" class="capability-pick" data-mention-kind="${escapeHTML(kind)}" data-mention-name="${escapeHTML(name)}" data-mention-id="${escapeHTML(id)}" data-mention-version="${escapeHTML(version)}"><span>${uiIcon(icon)}</span><span><strong>${escapeHTML(title)}</strong><small>${escapeHTML(subtitle || "No description")}</small></span>${badge}</button>`;
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
      ? skills.map(item => capabilityPickHTML("skill", "skill", item.canonical_name, item.canonical_name, item.summary,
          selected.has(item.canonical_name) ? pill("in context", "green") : (item.pinned ? pill("pinned", "blue") : ""), item.skill_id, item.version_id)).join("")
      : `<p class="command-empty">${!session ? "Start a session first — a Skill catalog is frozen when the session opens."
          : contract.skill_catalog?.length ? "No Skill matches that search."
          : "This session's Skill catalog is empty. Promote a Skill in Skill Studio first."}</p>`}</div></section>`;
  }
  if (filter === "all" || filter === "tools") {
    const tools = (contract.tool_bindings || []).filter(item => matches(`${item.name} ${item.description}`));
    markup += `<section class="capability-picker-section"><header><span>Direct tools</span><span>${tools.length}</span></header><div class="capability-picker-list">${tools.length
      ? tools.map(item => capabilityPickHTML("tool", "tools", item.name, item.name, item.description,
          pill(item.requires_approval ? "approval" : item.effect || "read", item.requires_approval ? "amber" : "green"))).join("")
      : `<p class="command-empty">${!session ? "Start a session first — tool bindings are frozen into its Session Contract." : "No direct tool matches that search."}</p>`}</div></section>`;
  }
  if (filter === "all" || filter === "mcp") {
    const indexed = Number(state.capability_summary?.total || 0);
    const results = state.capabilityPickerResults;
    markup += `<section class="capability-picker-section"><header><span>MCP catalog</span><span>${indexed.toLocaleString()} indexed</span></header><div class="capability-picker-list">${
      !query ? `<p class="command-empty">Type to search ${indexed.toLocaleString()} deferred tools. Only the matches you open are ever loaded.</p>`
      : state.capabilityPickerSearching ? `<p class="command-empty">Searching…</p>`
      : results.length ? results.map(item => capabilityPickHTML("mcp", "tools", item.title || item.name, item.title || item.name, item.description,
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
  button.textContent = "Aa";
  button.setAttribute("aria-pressed", String(compact));
  button.setAttribute("aria-label", compact ? "Use comfortable spacing" : "Use compact spacing");
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
const ZONE_LIMITS = { rail: [190, 380], side: [360, 1120] };

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

// Layout is a preference of this screen, not data about the project: two
// people opening the same project want their own arrangement, and a backup
// export should not carry anyone's pane sizes. That is why this lives in
// localStorage, keyed per project and per view, rather than in SQLite.
function layoutKey() {
  return `hermetrix.layout.v3.${state.currentProject?.id || "none"}.${state.view}`;
}

function saveLayout() {
  try {
    const layout = state.view === "code" && state.workspaceBeforePlans?.projectID === state.currentProject?.id
      ? state.workspaceBeforePlans : state;
    localStorage.setItem(layoutKey(), JSON.stringify({
      zones: state.zoneWidths,
      panes: layout.panes,
      maximised: layout.maximisedPane,
      paneLayout: layout.paneLayout,
      paneSplitX: state.paneSplitX,
      paneSplitY: state.paneSplitY,
      railProjectsOpen: state.railProjectsOpen,
      railSetupOpen: state.railSetupOpen,
      railProjectOpen: state.railProjectOpen,
      sessionOptionsOpen: state.sessionOptionsOpen,
      railHidden: $("#zones")?.classList.contains("rail-hidden") || false,
      sideHidden: $("#zones")?.classList.contains("side-hidden") || false
    }));
  } catch { /* private window, cleared storage: the app still works without it */ }
}

// applyLayout restores what it can and silently falls back for the rest. A
// stored value that no longer parses, or storage a private window refuses to
// read at all, must not be the thing that blanks the screen -- it just means
// this view opens at its defaults, the same as the very first time anyone
// opened it.
function applyLayout() {
  let saved = null;
  try { saved = JSON.parse(localStorage.getItem(layoutKey()) || "null"); } catch { saved = null; }
  const zones = saved?.zones || {};
  setZoneWidth("rail", zones.rail || 248);
  setZoneWidth("side", zones.side || Math.min(760, Math.max(460, Math.round(window.innerWidth * .42))));
  let restoredPanes = Array.isArray(saved?.panes) && saved.panes.length ? saved.panes : state.view === "code" ? ["editor", "files"] : ["files", "editor"];
  if (saved?.paneLayout === "ide" && restoredPanes[0] === "files" && restoredPanes[1] === "editor") {
    restoredPanes = ["editor", restoredPanes.includes("chat") ? "chat" : "files", ...restoredPanes.filter(id => ["terminal", "output", "debug"].includes(id)).slice(0, 1)];
  }
  state.panes = [...new Set(restoredPanes.map(id => paneContent(id).id))].slice(0, MAX_PANES);
  state.paneLayout = normalisePaneLayout(state.panes.length, saved?.paneLayout || (state.view === "code" ? "ide" : "columns"));
  state.paneSplitX = Number.isFinite(saved?.paneSplitX) ? Math.min(78, Math.max(22, saved.paneSplitX)) : 50;
  state.paneSplitY = Number.isFinite(saved?.paneSplitY) ? Math.min(78, Math.max(22, saved.paneSplitY)) : 50;
  state.railProjectsOpen = saved?.railProjectsOpen !== false;
  state.railSetupOpen = Boolean(saved?.railSetupOpen);
  state.railProjectOpen = saved?.railProjectOpen && typeof saved.railProjectOpen === "object" ? saved.railProjectOpen : {};
  state.sessionOptionsOpen = Boolean(saved?.sessionOptionsOpen);
  document.documentElement.style.setProperty("--pane-split-x", `${state.paneSplitX}%`);
  document.documentElement.style.setProperty("--pane-split-y", `${state.paneSplitY}%`);
  state.maximisedPane = Number.isInteger(saved?.maximised) && saved.maximised < state.panes.length
    ? saved.maximised : null;
  collapseZone("rail", saved?.railHidden ?? state.view === "code");
  collapseZone("side", saved?.sideHidden ?? state.view === "chat");
}

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
  syncWorkbenchPolling();
}

document.addEventListener("DOMContentLoaded", () => {
  // The rail button and the workbench tabs start wired here because chat is
  // already on screen at first paint; switchView repeats this same wiring
  // every time a trip through another view recreates those nodes.
  wireChatSkeleton();
  $("#sessionSetupClose").addEventListener("click", closeSessionSetup);
  $("#sessionSetupDone").addEventListener("click", closeSessionSetup);
  $("#sessionSetupDialog").addEventListener("close", () => $(sessionSetupReturnFocus)?.focus());
  $$("#viewSwitch [data-view]").forEach(button => button.addEventListener("click", () => switchView(button.dataset.view)));
  $("#openConfig").addEventListener("click", () => openConfig());
  $("#plansViewButton").addEventListener("click", openTasks);
  $("#closeConfig").addEventListener("click", closeConfig);
  $("#configSearch").addEventListener("input", renderConfigNav);
  $$(".zone-handle").forEach(handle => handle.addEventListener("pointerdown", event => startZoneDrag(handle, event)));
  $("#toggleRail").addEventListener("click", () => {
    if (window.innerWidth <= 700) {
      const open = $("#zones").classList.toggle("mobile-rail-open");
      $("#toggleRail").setAttribute("aria-expanded", String(open));
      return;
    }
    collapseZone("rail", !$("#zones").classList.contains("rail-hidden"));
    saveLayout();
  });
  $("#toggleSide").addEventListener("click", () => {
    if (window.innerWidth <= 920) { switchView("code"); return; }
    const collapsed = !$("#zones").classList.contains("side-hidden");
    collapseZone("side", collapsed);
    if (!collapsed) renderCurrentWorkbench();
    saveLayout();
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
    if (event.key === "Escape" && !$("dialog[open]")) dismissMobileRail();
  });
  $("#zoneMain").addEventListener("pointerdown", () => { if (window.innerWidth <= 700) dismissMobileRail(); });
  window.matchMedia("(max-width: 700px)").addEventListener("change", () => {
    dismissMobileRail();
    if (!$("#appShell").hidden && $("#workspacePaneHost")) renderPanes();
  });
  window.matchMedia("(max-width: 900px)").addEventListener("change", () => {
    if (state.paneLayout === "ide" && !$("#appShell").hidden && $("#workspacePaneHost")) renderPanes();
  });
  $("#pickerSearch").addEventListener("input", renderPicker);
  $("#pickerCreate").addEventListener("click", createProjectFromPicker);
  $("#projectChip").addEventListener("click", showPicker);
  initPicker();
});
