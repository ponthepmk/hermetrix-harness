const { escapeHTML, renderChatMarkdown, asList, toolArgumentsPreview, toolReceiptOf, toolOutputPreview, groupTimeline } = HermetrixRuntime;
const state = { skills: [], candidates: [], archives: [], relations: [], reviews: [], curator_runs: [], profiles: [], providers: [], mcp_servers: [], capability_summary: { total:0, by_source:{}, by_readiness:{} }, capabilityResults: [], capabilityPickerResults: [], capabilityPickerFilter:"all", selectedCapability: null, mcpServerQuery:"", mcpServerFilter:"all", mcpCapabilityQuery:"", sessions: [], projects: [], projectFiles: [], jobs: [], artifacts: [], terminals: [], browserTabs: [], teams: [], teamRuns: [], durableTasks:[], selectedDurableTask:null, taskExecutions:{}, taskDecisionLabs:{}, settings: [], memories: [], backups: [], usage: {}, fidelityCases: [], fidelityRuns: [], qualifications: [], curatorFindings: [], schedules: [], gcRuns: [], skillAuthority:null, authorityActions:[], sharePreview:null, workspacePreview:null, recoveryReport:null, activeTab: "chat", view: "chat", workbenchTab:"review", selectedSkill: null, selectedSkillDetail:null, selectedSession: null, selectedProject: null, currentProject: null, selectedTerminal:null, selectedBrowserTab:null, selectedTeam:null, draftQualificationReason:"", sessionError:"", chatTurnError:"", selectedTeam:null, teamDraft:null, projectFile:null, projectFileDiff:"", sessionDetail: null, contextResult: null, modelProbe: null, sending: false, commandItems: [], commandMatches: [], commandIndex: 0, capabilityPickerSearching: false, density: "comfortable", sessionOptionsOpen: false, railProjectsOpen: true, railSetupOpen: false, railProjectOpen: {}, sessionReady: false, elicitations: [], folderListing: null, draftMessage: "", composerAttachments: [], composerFocused: false, composerCaret: 0, zoneWidths: {}, panes: [], paneWidths: {}, maximisedPane: null, paneLayout: "adaptive", draggedPane: null, paneSplitX: 55, paneSplitY: 55, paneSplitLeftY: 55, paneSplitRightY: 55, paneIdeSplitX: 70, paneIdeSplitY: 72, paneIdeRightY: 72, authPrincipal: "" };
const DEFAULT_PROVIDER_KEY = "hermetrix.default-provider.v1";
const LAST_PROJECT_KEY = "hermetrix.last-project.v1";
const LAST_SURFACE_KEY = "hermetrix.last-surface.v1";
const LAST_SESSION_KEY = "hermetrix.last-session.v1";
const SESSION_DRAFT_PREFIX = "hermetrix.session-draft.v1:";
const PROJECT_DRAFT_PREFIX = "hermetrix.project-draft.v1:";
const DEFAULT_PROVIDER_SETTING = "ui.default_provider_id";
function rememberSurface(view) {
  try { sessionStorage.setItem(LAST_SURFACE_KEY, JSON.stringify({view, projectID:state.currentProject?.id || "", taskID:state.selectedDurableTask || ""})); } catch {}
}
function rememberedSurface() {
  try { return JSON.parse(sessionStorage.getItem(LAST_SURFACE_KEY) || "null"); } catch { return null; }
}
try {
  state.defaultProviderID = localStorage.getItem(DEFAULT_PROVIDER_KEY) || "";
  state.draftProviderID = state.defaultProviderID;
} catch { state.defaultProviderID = ""; state.draftProviderID = ""; }
let providerDefaultLoaded = false;
function selectDraftProvider(id) {
  state.draftProviderID = id;
  state.draftProfileName = "";
  state.draftQualificationReason = "";
}
async function saveDefaultProvider(id) {
  const provider = state.providers.find(item => item.id === id && item.enabled && item.credential_ready);
  if (!provider) return;
  try {
    await api("/api/settings", {method:"PUT", body:JSON.stringify({key:DEFAULT_PROVIDER_SETTING, value:id})});
    state.defaultProviderID = id;
    try { localStorage.setItem(DEFAULT_PROVIDER_KEY, id); } catch { /* Server setting remains authoritative. */ }
    selectDraftProvider(id);
    toast(`${provider.name} เป็นโมเดลเริ่มต้นสำหรับงานใหม่แล้ว`);
    renderProviders();
    renderChat();
    refreshPaneChat();
  } catch (error) { toast(`บันทึกโมเดลเริ่มต้นไม่สำเร็จ: ${error.message}`, true); }
}
const videoEvidenceJobs = new Map();
const videoTranscriptBodies = new Map();
const $ = (selector, root = document) => root.querySelector(selector);
const $$ = (selector, root = document) => [...root.querySelectorAll(selector)];
const UI_ICON_NAMES = new Set(["search","plus","close","sidebar","workbench","refresh","settings","file","files","terminal","browser","activity","model","tools","skill","review","learning","insights","archive","context","fidelity","project","jobs","artifact","chat","at","expand","contract","grip"]);
state.socialConnections = [];
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
  const headers = options.body instanceof FormData ? {...(options.headers || {})} : { "Content-Type": "application/json", ...(options.headers || {}) };
  const response = await fetch(path, { ...options, headers });
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
const STATUS_LABELS = Object.freeze({
  active:"ใช้งาน", ready:"พร้อมใช้", queued:"อยู่ในคิว", running:"กำลังทำงาน",
  awaiting_approval:"รออนุมัติ", pending:"รอดำเนินการ", blocked:"ติดขัด",
  completed:"เสร็จแล้ว", succeeded:"สำเร็จ", failed:"ไม่สำเร็จ",
  cancelled:"ยกเลิกแล้ว", interrupted:"งานหยุดกลางคัน", approved:"อนุมัติแล้ว",
  rejected:"ปฏิเสธแล้ว", disabled:"ปิดใช้งาน", draft:"ฉบับร่าง",
});
function pill(value, tone = "") {
  const raw = String(value ?? "");
  const label = STATUS_LABELS[raw] || raw;
  return `<span class="pill ${tone}"${label !== raw ? ` title="สถานะระบบ: ${escapeHTML(raw)}"` : ""}>${escapeHTML(label)}</span>`;
}

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
  $("#projectName").textContent = project ? project.name : "เลือก Workspace";
  $("#projectChip").title = project ? (project.root_path || "เลือก Workspace อื่น") : "เลือก Workspace สำหรับงาน";
}

// Keep Workspace choices current even while the user is in personal Chat.
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
  create.textContent = `สร้าง Workspace เปล่า “${query}”`;
  $$("[data-open-project]").forEach(button =>
    button.addEventListener("click", () => openProject(button.dataset.openProject, { targetView: pickerTargetView })));
}

// Opening records that someone worked here, which is what "recent" is ordered
// by, and then hands the screen to the shell.
let projectOpenGeneration = 0;
let sessionSelectionGeneration = 0;
let pickerTargetView = "chat";
async function openProject(id, options = {}) {
  if (state.sending) { toast("รอให้งานในแชทนี้เสร็จก่อนเปลี่ยนโปรเจกต์", true); return; }
  if (state.sessionCreationPending && !options.createdSession) { toast("กำลังสร้างแชท กรุณารอสักครู่", true); return; }
  ++navigationGeneration;
  const generation = ++projectOpenGeneration;
  state.projectOpening = true;
  const shell = $("#appShell");
  shell?.setAttribute("aria-busy", "true");
  if (shell) shell.inert = true;
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
    try { localStorage.setItem(LAST_PROJECT_KEY, project.id); } catch { /* The current page still keeps its selection. */ }
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
    if (options.targetView && options.targetView !== state.view) {
      switchView(options.targetView);
    } else {
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
    }
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
    if (state.pendingWorkspaceGoal) {
      const goal = state.pendingWorkspaceGoal;
      state.pendingWorkspaceGoal = "";
      state.taskDraftObjective = goal;
      state.taskDraftDetails = {criteria:goal, require_user_acceptance:true};
      state.taskDraftAutoPlan = true;
      await openTasks();
    }
    return project;
  } catch (error) { if (generation === projectOpenGeneration) toast(error.message, true); }
  finally {
    if (generation === projectOpenGeneration) {
      state.projectOpening = false;
      shell?.removeAttribute("aria-busy");
      if (shell) shell.inert = false;
    }
  }
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
      await openProject(created.id, { targetView: pickerTargetView });
    }
  });
}

// showShell and showPicker are the only two things that decide which of the two
// top-level screens is on.
function showShell() {
  $("#projectPicker").hidden = true;
  $("#appShell").hidden = false;
  $("#projectName").textContent = state.currentProject?.name || "เลือก Workspace";
}

function showPicker(targetView = state.view) {
  if (state.sessionCreationPending) { toast("กำลังเริ่มแชท กรุณารอสักครู่"); return; }
  pickerTargetView = typeof targetView === "string" && VIEWS[targetView] ? targetView : state.view;
  restorePlanningWorkspace();
  ++navigationGeneration;
  captureWorkspaceDrafts();
  dismissMobileRail();
  $("#appShell").hidden = true;
  $("#projectPicker").hidden = false;
  renderPicker();
  $("#pickerSearch")?.focus();
}

// Personal Chat opens immediately. Workspace choices remain ready in the
// picker for a deliberate transition into work with project effects.
async function initPicker() {
  let savedSessionID = "";
  try { savedSessionID = sessionStorage.getItem(LAST_SESSION_KEY) || ""; } catch {}
  try {
    state.projects = await api("/api/projects");
  } catch (error) { toast(error.message, true); }
  renderPicker();
  showShell();
  await load();
  const savedSurface = rememberedSurface();
  let savedProjectID = "";
  try { savedProjectID = localStorage.getItem(LAST_PROJECT_KEY) || ""; } catch { /* Continue in personal Chat. */ }
  if (savedProjectID && state.projects.some(item => item.id === savedProjectID)) {
    const restored = await openProject(savedProjectID, {targetView:"chat"});
    if (restored) {
      const savedSession = state.sessions.find(item => item.id === savedSessionID && (!item.project_id || item.project_id === restored.id));
      if (savedSession) await selectSession(savedSession.id);
      if (savedSurface?.projectID === restored.id) {
        if (savedSurface.view === "plans") {
          await openTasks();
          if (savedSurface.taskID === "__new__" || (savedSurface.taskID && state.durableTasks.some(item => item.id === savedSurface.taskID && item.project_id === restored.id))) {
            state.selectedDurableTask = savedSurface.taskID;
            state.plansOverview = false;
            renderPanes();
          }
          rememberSurface("plans");
        } else if (savedSurface.view === "code") switchView("code");
      }
      return;
    }
  }
  const personalSessions = state.sessions.filter(item => !item.project_id)
    .sort((a, b) => String(b.updated_at || "").localeCompare(String(a.updated_at || "")));
  const personal = personalSessions.find(item => item.id === savedSessionID) || personalSessions[0];
  if (personal) await selectSession(personal.id);
}

function openWorkspace() {
  if (!state.currentProject?.id) { showPicker("code"); return; }
  switchView("code");
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
  capabilities: {mcp_servers:"/api/mcp/servers"},
  mcp: {socialConnections:"/api/social/connections"},
  library: {skillAuthority:"/api/skill-authority", authorityActions:"/api/skill-authority/actions"},
  projects: {jobs:"/api/jobs", artifacts:"/api/artifacts", memories:"/api/memories", durableTasks:"/api/tasks?limit=100"},
  office: {jobs:"/api/jobs"}, artifacts: {artifacts:"/api/artifacts"}, insights: {curatorFindings:"/api/curator/findings"},
  fidelity: {fidelityCases:"/api/fidelity/cases", fidelityRuns:"/api/fidelity/runs"},
  maintenance: {settings:"/api/settings", memories:"/api/memories", backups:"/api/backups", usage:"/api/usage", curatorFindings:"/api/curator/findings", schedules:"/api/maintenance/schedules", gcRuns:"/api/maintenance/gc"},
  review: {jobs:"/api/jobs", skillAuthority:"/api/skill-authority"}, output: {jobs:"/api/jobs"},
  terminal: {terminals:"/api/terminals"}, browser: {browserTabs:"/api/browser/tabs"},
  team: {teams:"/api/teams", teamRuns:"/api/team-runs"}, tasks: {durableTasks:"/api/tasks?limit=100", socialConnections:"/api/social/connections"}
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
    const [bootstrap, projects, qualifications, capabilities, archivedProjects, settings] = await Promise.allSettled([
      api("/api/bootstrap"), api("/api/projects"), api("/api/qualifications"), api("/api/capabilities?limit=1"), api("/api/projects?archived=true"), api("/api/settings")
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
    if (archivedProjects.status === "fulfilled") state.archivedProjects = asList(archivedProjects.value);
    if (qualifications.status === "fulfilled") state.qualifications = asList(qualifications.value);
    if (settings.status === "fulfilled") {
      state.settings = asList(settings.value);
      const savedID = state.settings.find(item => item.key === DEFAULT_PROVIDER_SETTING)?.value;
      const validID = id => state.providers.some(item => item.id === id && item.enabled && item.credential_ready);
      const preferredID = HermetrixRuntime.preferredProvider(state.providers.filter(item => item.enabled && item.credential_ready))?.id || "";
      state.defaultProviderID = validID(savedID) ? savedID : validID(state.defaultProviderID) ? state.defaultProviderID : preferredID;
      if (!providerDefaultLoaded) {
        selectDraftProvider(state.defaultProviderID);
        providerDefaultLoaded = true;
      }
    }
    state.runtimeCapabilities = capabilities.status === "fulfilled" ? capabilities.value?.runtime || {} : {};
    state.sessionError = "";
    for (const result of [projects, qualifications, capabilities, settings]) {
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
  const renderers = {providers:renderProviders, capabilities:renderCapabilityCenter, mcp:renderMCP, tools:renderDirectTools, appearance:renderAppearance, library:renderLibrary, proposals:renderProposals,
    learning:renderLearning, insights:renderInsights, archive:renderArchive, context:renderContext,
    projects:renderProjects, office:renderOffice, artifacts:renderArtifacts, fidelity:renderFidelity, maintenance:renderMaintenance,
    discord:() => window.HermetrixDiscord?.render($("#view-discord"))};
  renderers[tab]?.();
  const introduction = CONFIG_PAGE_INTROS[tab];
  const root = $(`#view-${tab}`);
  if (introduction && root && !root.querySelector(".config-page-intro")) {
    root.insertAdjacentHTML("afterbegin", `<header class="config-page-intro"><p class="eyebrow">${escapeHTML(introduction[0])}</p><div class="config-page-title"><h2>${escapeHTML(introduction[1])}</h2><details class="context-help"><summary aria-label="เกี่ยวกับ${escapeHTML(introduction[1])}" title="เกี่ยวกับหน้านี้">i</summary><p>${escapeHTML(introduction[2])}</p></details></div></header>`);
  }
}

function contextSelectionHTML(snapshot, diagnostic = false) {
  if (!snapshot?.report) return "";
  const report = snapshot.report;
  const brainSelected = (report.selected_ids || []).filter(id => id.startsWith("project-brain:"));
  const brainOmitted = (report.dropped_ids || []).filter(id => id.startsWith("project-brain:"));
  const list = (ids, empty) => ids?.length
    ? `<ul>${ids.slice(0, 30).map(id => `<li><code>${escapeHTML(id)}</code></li>`).join("")}${ids.length > 30 ? `<li>และอีก ${ids.length - 30} รายการ</li>` : ""}</ul>`
    : `<p>${empty}</p>`;
  return `<details class="context-selection"><summary>${diagnostic ? "ผลจำลองการจัดบริบท" : "ข้อมูลที่ใช้ในคำขอล่าสุด"} · เลือก ${(report.selected_ids || []).length} · ไม่เลือก ${(report.dropped_ids || []).length}</summary>
    <p>${diagnostic ? "รายการนี้มาจากตัวอย่างที่กรอกด้านซ้าย" : "ผลนี้เป็นภาพของคำขอครั้งก่อน การส่งครั้งถัดไปอาจเลือกข้อมูลต่างกัน"}</p>
    ${brainSelected.length || brainOmitted.length ? `<section class="context-brain-status"><h4>Project Brain ในคำขอนี้</h4><p>อยู่ในบริบทที่ส่ง ${brainSelected.length} · เสนอแต่ไม่ส่ง ${brainOmitted.length} รายการ ข้อมูลจากแหล่งภายนอกใช้เป็นแหล่งอ้างอิงและต้องตรวจความเหมาะสมกับงาน</p></section>` : ""}
    <div class="context-selection-groups"><section><h4>ส่งเข้าโมเดล</h4>${list(report.selected_ids, "ไม่มีรายการ")}</section><section><h4>ไม่ส่งเข้าโมเดล</h4>${list(report.dropped_ids, "ไม่มีรายการที่ถูกตัด")}</section></div>
    ${(report.spilled || []).length ? `<p>ผลเครื่องมือขนาดใหญ่ถูกเก็บเป็นหลักฐาน ${report.spilled.length} รายการ และส่งเพียงตัวอย่างพร้อมรหัสอ้างอิง</p>` : ""}
    ${(report.warnings || []).length ? `<ul>${report.warnings.map(item => `<li>${escapeHTML(item)}</li>`).join("")}</ul>` : ""}</details>`;
}

function sessionContextMeterHTML(session, profile, events, snapshot) {
  if (!session) return "";
  if (!profile?.total) return `<div class="context-meter"><small>Context · ความจุและการใช้จริงยังไม่ทราบ</small></div>`;
  const usages = (events || []).map(item => Number(item.metadata?.usage?.prompt_tokens || 0)).filter(value => value > 0);
  const total = Number(profile.total);
  const outputReserve = Number(profile.output_reserve || 0);
  const uncertaintyReserve = Number(profile.uncertainty_reserve || 0);
  const inputBudget = Math.max(0, total - outputReserve - uncertaintyReserve);
  const reserveLabel = `สำรองคำตอบ ${outputReserve.toLocaleString()} · สำรองความคลาดเคลื่อน ${uncertaintyReserve.toLocaleString()}`;
  const latest = usages.at(-1);
  const estimated = Number(snapshot?.report?.predicted_prompt || 0);
  const displayed = latest || estimated;
  const meter = displayed ? `<progress max="${Math.max(1, inputBudget)}" value="${Math.min(inputBudget, displayed)}" aria-label="การใช้บริบทล่าสุด"></progress>` : "";
  const actualLabel = latest ? `ใช้จริงล่าสุด ${latest.toLocaleString()}` : "การใช้จริงยังไม่ทราบ";
  const estimateLabel = estimated ? `ประมาณก่อนส่งครั้งล่าสุด ${estimated.toLocaleString()}` : "ประมาณก่อนส่งยังไม่ทราบ";
  return `<div class="context-meter" title="ค่าประมาณก่อนส่งและค่าที่โมเดลรายงานเป็นของคำขอล่าสุดที่มีข้อมูล ไม่ใช่ยอดสะสม">${meter}<small>Context · ${actualLabel} / งบ input ${inputBudget.toLocaleString()} · ${estimateLabel} · ${reserveLabel} · รอบถัดไปยังไม่ทราบ</small>${contextSelectionHTML(snapshot)}</div>`;
}

let tokenUsageDetailsOpen = false;

function sessionUsageHTML(session, events, snapshot) {
  if (!session) return "";
  const all = events || [];
  const turns = new Map();
  for (const item of all) {
    if (!item.turn_id) continue;
    if (!turns.has(item.turn_id)) turns.set(item.turn_id, []);
    turns.get(item.turn_id).push(item);
  }
  const usageFor = turn => {
    const outcome = [...turn].reverse().find(item =>
      (item.event_kind === "turn_failed" || (item.event_kind === "message" && item.role === "assistant")) &&
      Number(item.metadata?.usage?.total_tokens) > 0);
    const observed = turn.filter(item => item.event_kind === "tool_call" && Number(item.metadata?.usage?.total_tokens) > 0);
    const samples = outcome ? [outcome.metadata.usage] : observed.map(item => item.metadata.usage);
    return {
      tokens: samples.reduce((sum, usage) => sum + Number(usage.total_tokens || 0), 0),
      prompt: samples.reduce((sum, usage) => sum + Number(usage.prompt_tokens || 0), 0),
      completion: samples.reduce((sum, usage) => sum + Number(usage.completion_tokens || 0), 0),
      exact: Boolean(outcome)
    };
  };
  const turn = [...turns.values()].at(-1) || [];
  const latest = usageFor(turn);
  const usages = [...turns.values()].map(usageFor);
  const totalTokens = usages.reduce((sum, usage) => sum + usage.tokens, 0);
  const exact = usages.every(usage => usage.exact);
  const budget = Number(session.contract?.task_budget?.max_cumulative_tokens || 0);
  const steps = turn.filter(item => item.event_kind === "model_step_bound").length;
  const maxSteps = Number(session.contract?.task_budget?.max_model_steps || 0);
  const profile = state.profiles.find(item => item.name === session.context_profile);
  const inputBudget = profile ? Math.max(0, Number(profile.total) - Number(profile.output_reserve) - Number(profile.uncertainty_reserve)) : 0;
  const estimatedInput = Number(snapshot?.report?.predicted_prompt || 0);
  const hasStepFailure = turn.some(item => item.event_kind === "turn_failed" && /model-step budget/i.test(item.content || ""));
  const latestStatus = latest.tokens ? (latest.exact ? "วัดได้จริง" : "ยอดขั้นต่ำจากข้อมูลที่บันทึก") : "ยังไม่มีข้อมูลการใช้จากโมเดล";
  const chatStatus = totalTokens ? `ใช้รวมในแชต${exact ? "" : "อย่างน้อย"} ${totalTokens.toLocaleString()} token` : "ยังไม่มีการใช้ token ในแชตนี้";
  const stepStatus = maxSteps ? `${steps} / ${maxSteps} ขั้น` : "ไม่ทราบ";
  const contextStatus = estimatedInput && inputBudget ? `ประมาณ ${estimatedInput.toLocaleString()} / ${inputBudget.toLocaleString()} token` : "ยังไม่มีค่าประมาณรอบล่าสุด";
  const lastLabel = `${latest.tokens.toLocaleString()}${budget ? ` / ${budget.toLocaleString()}` : ""} token`;
  const progress = budget ? `<progress max="${budget}" value="${Math.min(budget, latest.tokens)}" aria-label="การใช้ token ของคำสั่งล่าสุด ${lastLabel}"></progress>` : "";
  const tone = budget && latest.tokens >= budget ? "is-full" : budget && latest.tokens >= budget * .8 ? "is-near" : "";
  return `<details class="session-usage token-meter ${tone}" ${tokenUsageDetailsOpen ? "open" : ""}>
    <summary><span class="token-meter-top"><strong>Token · คำสั่งล่าสุด</strong><b>${lastLabel}</b></span>${progress}
      <span class="token-meter-foot"><span>${chatStatus}</span><span class="token-meter-expand">รายละเอียด <i aria-hidden="true">⌄</i></span></span></summary>
    <div class="token-meter-detail"><dl>
      <div><dt>คำสั่งล่าสุด</dt><dd>${lastLabel} · ${latestStatus}</dd></div>
      <div><dt>รับเข้า / ตอบออก</dt><dd>${latest.prompt.toLocaleString()} / ${latest.completion.toLocaleString()} token${latest.exact ? "" : " · ยอดขั้นต่ำ"}</dd></div>
      <div><dt>รวมทั้งแชต</dt><dd>${totalTokens.toLocaleString()} token${exact ? "" : " · อย่างน้อย"}</dd></div>
      <div><dt>ขั้นโมเดลของคำสั่งล่าสุด</dt><dd>${stepStatus}</dd></div>
      <div><dt>บริบทที่ส่งรอบล่าสุด</dt><dd>${contextStatus}</dd></div>
    </dl><p>หลอดเทียบกับเพดาน token ต่อคำสั่ง${hasStepFailure ? ` · คำสั่งก่อนหยุดเพราะครบ ${maxSteps} ขั้นโมเดล` : ""} ยอดรวมทั้งแชตสะสมข้ามคำสั่ง ส่วนบริบทคือขนาดข้อความที่ส่งเข้าโมเดลในรอบล่าสุด</p></div>
  </details>`;
}

const CONFIG_PAGE_INTROS = {
  proposals:["สกิล", "ข้อเสนอสกิล", "ตรวจเนื้อหาและหลักฐานก่อนอนุมัติให้ใช้งาน"],
  learning:["สกิล", "การเรียนรู้", "ติดตามรีวิวที่ทำเบื้องหลังและผลที่ได้จากแต่ละงาน"],
  insights:["สกิล", "ข้อค้นพบ", "ตรวจข้อเสนอแนะจากการวิเคราะห์สกิล โดยยังไม่มีการเปลี่ยนแปลงอัตโนมัติ"],
  archive:["สกิล", "สกิลที่เก็บไว้", "ค้นหาและนำสกิลเก่ากลับมาเป็นข้อเสนอเพื่อพิจารณา"],
  context:["โมเดล", "บริบทโมเดล", "ตรวจขนาดบริบทของโมเดลและดูว่าข้อมูลใดถูกส่งเข้าไป"],
  fidelity:["โมเดล", "หลักฐานคุณภาพ", "ดูผลทดสอบตัวจัดบริบทด้วยข้อมูลตัวอย่างในเครื่อง"],
  discord:["โปรเจกต์และสิทธิ์", "สั่งงานผ่าน Discord", "ตั้งค่าการสั่งงานระยะไกลและตรวจสถานะการเชื่อมต่อ"],
  projects:["โปรเจกต์และสิทธิ์", "โปรเจกต์", "เลือกโฟลเดอร์งาน จัดการไฟล์ และกำหนดขอบเขตการแชร์"],
  artifacts:["ขั้นสูง", "ไฟล์ผลลัพธ์", "เปิดไฟล์ที่สร้างจากงานหรือเก็บไฟล์ใหม่ไว้ในโปรเจกต์"],
  maintenance:["ขั้นสูง", "การดูแลระบบ", "ดูการใช้งาน ความจำ สำรองข้อมูล และงานบำรุงรักษา"]
};

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
      <div class="skill-row-actions"><button class="ghost" type="button" data-mention-skill="${escapeHTML(item.id)}">ใช้ในแชท</button><button class="ghost" type="button" data-archive-skill="${escapeHTML(item.id)}" title="นำออกจากสกิลที่ใช้ · กู้คืนได้ใน Archive">นำออก</button></div>
    </article>`;
  }).join("")}</div>` : `<div class="empty"><h3>${state.skills.length ? "ไม่พบสกิลที่ตรงกับตัวกรอง" : "ยังไม่มีสกิล"}</h3><p>สร้างข้อเสนอสกิลใหม่ หรือปรับคำค้นหาและตัวกรอง</p></div>`;
  // Skill Studio opens on what a person came to do -- read the library, add a
  // Skill -- with the authority policy folded away behind its own summary
  // rather than sitting above the list it governs.
  const pending = state.candidates.filter(item => ["needs_review", "quarantined"].includes(item.state)).length;
  const intro = `<section class="skill-studio-intro skill-directory-head"><div><h2>สกิล</h2><p>นำเข้าไฟล์ .skill หรือขอให้แชตสร้างสกิล แล้วตรวจข้อเสนอก่อนเปิดใช้</p></div><div class="action-row"><button class="ghost" type="button" id="studioChatSkillButton">สั่งแชตสร้างสกิล</button><button class="ghost" type="button" id="studioImportButton">นำเข้า .skill</button><input type="file" id="studioImportFile" accept=".skill" hidden><button class="ghost" type="button" id="studioReviewButton">ข้อเสนอที่รอ ${pending}</button><button class="primary" type="button" id="studioCreateButton">เขียนสกิลใหม่</button></div></section>`;
  const collections = [["all", "ทั้งหมด", state.skills.length], ["enabled", "เปิดใช้", state.skills.filter(item => item.enabled).length], ["pinned", "ปักหมุด", state.skills.filter(item => item.pinned).length], ["agent", "จากเอเจนต์", state.skills.filter(item => String(item.origin || "").startsWith("agent") || item.owner === "agent").length]];
  $("#libraryIntro").innerHTML = intro;
  root.innerHTML = `<div class="skill-collection-tabs" role="group" aria-label="กรองสกิล">${collections.map(([id, label, count]) => `<button type="button" data-skill-collection="${id}" aria-pressed="${collection === id}">${label} <small>${count}</small></button>`).join("")}</div><div class="skill-directory-count">แสดง ${items.length} จาก ${state.skills.length} สกิล</div>${list}<div class="skill-policy-wrap">${policyPanel}</div>`;
  $$('[data-skill-collection]', root).forEach(button => button.addEventListener("click", () => { state.skillCollectionFilter = button.dataset.skillCollection; renderLibrary(); }));
  $("#studioReviewButton")?.addEventListener("click", () => switchTab("proposals"));
  $("#studioCreateButton")?.addEventListener("click", openCandidateDialog);
  $("#studioChatSkillButton")?.addEventListener("click", () => {
    switchTab("chat");
    if (!state.currentProject) { showPicker(); toast("เลือกโปรเจกต์เพื่อเริ่มแชตสร้างสกิล"); return; }
    if (state.view !== "chat") switchView("chat");
    const instruction = "ช่วยสร้างสกิลจากวิธีทำงานที่ฉันอธิบาย โดยใช้ skill_manage สร้างข้อเสนอและบอกฉันว่าต้องตรวจทานที่ Skill Studio > Proposals";
    if ($("#chatInput")) appendComposerInstruction(instruction);
    else if ($("#startPrompt")) { $("#startPrompt").value = instruction + " "; $("#startPrompt").focus(); }
  });
  $("#studioImportFile")?.addEventListener("change", importSkillFile);
  $("#studioImportButton")?.addEventListener("click", () => $("#studioImportFile")?.click());
  $("#authorityForm")?.addEventListener("submit", saveAuthorityPolicy);
  $("#runAuthorityButton")?.addEventListener("click", runAuthorityPolicy);
  $$('[data-authority-rollback]', root).forEach(button => button.addEventListener("click", () => rollbackAuthorityAction(button.dataset.authorityRollback)));
  $$('[data-mention-skill]', root).forEach(button => button.addEventListener("click", event => {
    event.stopPropagation();
    const skill = state.skills.find(item => item.id === button.dataset.mentionSkill);
    if (skill) mentionSkill(skill);
  }));
  $$('[data-archive-skill]', root).forEach(button => button.addEventListener("click", event => {
    event.stopPropagation();
    const skill = state.skills.find(item => item.id === button.dataset.archiveSkill);
    if (skill) archiveSkill(skill);
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
  const recent = [...state.reviews].sort((a,b) => String(b.created_at || "").localeCompare(String(a.created_at || ""))).slice(0, 5);
  root.innerHTML = `<div class="learning-overview panel"><div><strong>${queued}</strong><span>รอตรวจ</span></div><div><strong>${state.reviews.filter(item => item.state === "running").length}</strong><span>กำลังตรวจ</span></div><div><strong>${state.reviews.filter(item => item.candidate_id).length}</strong><span>สร้างข้อเสนอแล้ว</span></div></div>
    <div class="section-heading"><div><h3>รีวิวล่าสุด</h3><p>งานที่ค้างจะกลับเข้าคิวหลังเปิดแอป และระบบตรวจอัตโนมัติประมาณทุก 1 นาที</p></div><div class="action-row"><button class="ghost" id="learningOpenOffice">ดูงานทั้งหมด</button><button class="primary" id="runReviewButton" ${queued ? "" : "disabled"}>ตรวจงานถัดไป</button></div></div>
    <div class="office-list learning-recent">${recent.length ? recent.map(item => officeEntryHTML({kind:"review",key:`review:${item.id}`,item,created:item.created_at})).join("") : `<div class="empty"><h3>ยังไม่มีรีวิว</h3><p>เมื่อมีงานที่เข้าเกณฑ์ ระบบจะแสดงสถานะและผลไว้ที่นี่</p></div>`}</div>`;
  $("#runReviewButton").addEventListener("click", runNextReview);
  $("#learningOpenOffice").addEventListener("click", () => { officeKind = "review"; officeFilter = "all"; switchTab("office"); });
  $$('[data-office-proposal]', root).forEach(button => button.addEventListener("click", () => { switchTab("proposals"); inspectCandidate(button.dataset.officeProposal); }));
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
  root.innerHTML = `<div class="panel"><div class="proposal-head"><div><h3>วิเคราะห์สกิล · ${archiveEnabled ? "เก็บอัตโนมัติตามเกณฑ์" : "รายงานเท่านั้น"}</h3><p>ระบบค้นข้อมูลด้วยกฎที่ตรวจซ้ำได้ก่อน ข้อเสนอเรื่องสกิลซ้ำหรือการรวมสกิลยังต้องตรวจทบทวน ${archiveEnabled ? "ระบบเก็บได้เฉพาะสกิลเก่าที่ agent สร้างและมีหลักฐานมั่นใจสูงตามนโยบายฉบับปัจจุบัน โดยกู้คืนได้ทุกครั้ง" : "นโยบายปัจจุบันไม่อนุญาตให้การวิเคราะห์แก้สกิลเอง"}</p><div class="meta">${lastRun ? `${pill(`ล่าสุด ${formatDate(lastRun.completed_at || lastRun.started_at)}`)}${pill(`${lastRun.findings_count} ข้อค้นพบ`)}${pill(lastRun.analyzer_revision)}` : pill("ยังไม่เคยวิเคราะห์")}</div></div><button class="primary" id="analyzeButton">วิเคราะห์ตอนนี้</button></div></div>
    <div class="card-list spaced">${state.curatorFindings.length ? state.curatorFindings.map(item => `<article class="insight-card"><div class="insight-head"><div><h3>${escapeHTML(item.finding_kind)} · ${Math.round(item.score*100)}%</h3><p>${escapeHTML((item.evidence?.reasons || []).join("; ") || item.evidence?.note || "ต้องให้ผู้ใช้ทบทวนตามสกิลฉบับนี้")}</p></div>${pill(item.severity,item.severity === "warning" ? "amber" : "blue")}</div><div class="kv"><span>สกิลแรก</span><code>${escapeHTML(item.left_skill_id || "—")}</code><span>สกิลที่สอง</span><code>${escapeHTML(item.right_skill_id || "—")}</code><span>ข้อเสนอ</span><strong>${escapeHTML(item.proposal?.action || "รายงานเท่านั้น")}</strong><span>แก้อัตโนมัติ</span><strong>${item.proposal?.automatic_mutation === false ? "ห้าม" : "ไม่มี"}</strong></div>${item.proposal?.review_steps ? `<ol class="review-steps">${item.proposal.review_steps.map(step => `<li>${escapeHTML(step)}</li>`).join("")}</ol>` : ""}</article>`).join("") : `<div class="empty"><h3>ยังไม่มีข้อค้นพบ</h3><p>วิเคราะห์สกิลซ้ำ สกิลที่ซ้อนกัน และสกิลที่เก่า โดยไม่เปลี่ยนสกิลที่ใช้งานอยู่</p></div>`}</div>`;
  $("#analyzeButton").addEventListener("click", analyzeRelations);
}

async function analyzeRelations() {
  try { const result = await api("/api/curator/run", { method: "POST", body: "{}" }); const count = result.run?.findings_count || 0; const automated = result.authority_actions?.length || 0; toast(`วิเคราะห์เสร็จแล้ว · ${count} ข้อค้นพบ${automated ? ` · ${automated} การกระทำตามนโยบาย` : ""}`); await load(); } catch (error) { toast(error.message, true); }
}

function renderArchive() {
  const root = $("#view-archive");
  if (!state.archives.length) { root.innerHTML = `<div class="empty"><h3>ยังไม่มีสกิลที่เก็บไว้</h3><p>สกิลที่เก็บไว้สามารถนำกลับมาเป็นข้อเสนอได้ เมื่อมีรายการจะแสดงที่นี่</p></div>`; return; }
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
  if (Number(provider?.reasoning_sample || 0) >= 10 && budget < MINIMUM_ANSWER_BUDGET) {
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

// A main provider with an exact 64k qualification opens at 64k by default.
// Smaller or unverified runtimes keep the smallest admitted envelope.
function bestProfileFor(provider, profiles) {
  const ordered = [...profiles].sort((a, b) => a.total - b.total);
  const qualified64k = ordered.find(profile => profile.total === 65536 && profileAdmission(provider, profile).mode === "qualified");
  if (qualified64k) return qualified64k;
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

function turnFailureGuidance(detail) {
  const message = String(detail || "").toLowerCase();
  if (message.includes("provider/model revision changed after session creation"))
    return "แชตนี้ผูกกับโมเดลรุ่นก่อนการตั้งค่าล่าสุด เริ่มแชตใหม่ในโปรเจกต์เดิมเพื่อส่งข้อความต่อ";
  if (/connectex|connection refused|actively refused/.test(message))
    return "เซิร์ฟเวอร์โมเดลในเครื่องยังไม่เปิดหรือเชื่อมต่อไม่ได้ เปิด Models เพื่อตรวจการเชื่อมต่อ แล้วส่งข้อความเดิมอีกครั้ง";
  if (message.includes("inference resource is quarantined") || message.includes("cancelled_runtime_idle_unconfirmed"))
    return "โมเดลในเครื่องถูกพักหลังคำตอบถูกขัดจังหวะ รอให้โมเดลหยุด แล้วเปิด Hermetrix ใหม่ก่อนลองส่งอีกครั้ง";
  if (message.includes("context canceled") || message.includes("context cancelled"))
    return "คำตอบถูกขัดจังหวะก่อนจบ ตรวจข้อความที่ส่งแล้วค่อยลองใหม่";
  if (message.includes("model steps") || message.includes("step budget"))
    return "โมเดลใช้ครบจำนวนขั้นของงานนี้ขณะยังขอเครื่องมือเพิ่ม หลักฐานที่ตรวจแล้วไม่หาย พิมพ์ ‘ทำต่อจากที่ตรวจแล้วและสรุปผล’ เพื่อทำต่อ";
  return "คำตอบยังไม่สำเร็จ ตรวจสถานะโมเดลและรายละเอียดใน Review แล้วลองใหม่";
}

function renderConversationTimeline(events) {
  const timeline = [];
  let completedTools = [];
  const flushTools = () => {
    if (!completedTools.length) return;
    if (completedTools.length < 4) timeline.push(...completedTools.map(renderTimelineItem));
    else {
      const firstID = completedTools[0].call?.id || completedTools[0].call?.metadata?.tool_call_id || "tools";
      timeline.push(`<details class="ide-tool-batch" data-batch-key="${escapeHTML(firstID)}"><summary>ดูการใช้เครื่องมือ ${completedTools.length} รายการ</summary><div class="ide-tool-batch-items">${completedTools.map(renderTimelineItem).join("")}</div></details>`);
    }
    completedTools = [];
  };
  for (const item of groupTimeline(events)) {
    if (item.kind === "tool_step") completedTools.push(item);
    else { flushTools(); timeline.push(renderTimelineItem(item)); }
  }
  flushTools();
  return timeline.join("");
}

function renderTimelineEvent(event) {
  if (event.event_kind === "message" && ["user", "assistant"].includes(event.role)) {
    const content = String(event.content || "");
    const body = event.role === "user" && content.length > 4000
      ? `<details class="message-long"><summary>${escapeHTML(content.slice(0, 220).replace(/\s+/g, " "))}… <span>ดูข้อความทั้งหมด · ${content.length.toLocaleString()} อักขระ</span></summary><div class="message-body">${escapeHTML(content)}</div></details>`
      : `<div class="message-body">${event.role === "assistant" ? renderChatMarkdown(content) : escapeHTML(content)}</div>`;
    return `<article class="chat-message ${event.role}"><div class="message-role">${event.role === "user" ? "คุณ" : "Hermetrix"}</div>${body}${event.role === "assistant" && event.metadata?.step_binding_id ? `<div class="message-proof">ผูกกับคำขอ ${escapeHTML(shortHash(event.metadata.step_binding_id))} · ${event.metadata.usage?.total_tokens || 0} โทเค็น</div>` : ""}</article>`;
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
    const status = {pending:"รออนุมัติ", executed:"ทำแล้ว", denied:"ปฏิเสธแล้ว", cancelled:"ยกเลิกแล้ว"}[approval.state] || approval.state;
    const effect = approval.effect === "unknown" ? "ยังไม่ทราบ" : approval.effect;
    return `<article class="approval-card"><div class="approval-head"><div>${pill("คำสั่งที่ต้องอนุมัติ", tone)}<strong>${escapeHTML(approval.tool_name)}</strong></div>${pill(status, tone)}</div><p>${escapeHTML(approval.summary)}</p><pre>${escapeHTML(approval.preview || "ไม่มีตัวอย่างข้อมูล")}</pre>${approval.metadata?.screenshot_artifact_id ? `<a href="/api/artifacts/${encodeURIComponent(approval.metadata.screenshot_artifact_id)}/content" target="_blank" rel="noopener noreferrer">ดูภาพหน้าจอก่อนยืนยัน</a>` : ""}<small>ผลกระทบ ${escapeHTML(effect)} · คำสั่ง ${escapeHTML(shortHash(approval.arguments_hash))}</small>${approval.state === "pending" ? `<div class="action-row"><button class="primary" data-approve-tool="${escapeHTML(approval.id)}">อนุมัติครั้งเดียว</button><button class="danger" data-deny-tool="${escapeHTML(approval.id)}">ปฏิเสธ</button></div>` : ""}</article>`;
  }
  if (event.event_kind === "approval_decision") {
    const approved = event.metadata?.decision === "approve";
    return `<article class="approval-decision">${pill(approved ? "อนุมัติครั้งเดียว" : "ปฏิเสธแล้ว", approved ? "green" : "red")}<span>${escapeHTML(event.metadata?.reason || "ไม่ได้ระบุเหตุผล")}</span><small>${escapeHTML(event.metadata?.actor || "ผู้ใช้")}</small></article>`;
  }
  // A failed turn used to render as nothing at all: the user saw an empty
  // conversation and assumed the model replied nonsense. Say what happened
  // and what to do next instead.
  if (event.event_kind === "turn_failed") {
    return `<article class="chat-message assistant"><div class="message-role">Hermetrix</div><div class="message-body">${pill("turn failed", "red")} ${escapeHTML(event.content || "Turn failed")}<br><small>${escapeHTML(turnFailureGuidance(event.content))}</small></div></article>`;
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
  const personalSessions = state.sessions.filter(item => !item.project_id);
  const personalGroup = `<section class="rail-project"><div class="nav-label rail-project-label">แชทส่วนตัว <small>${personalSessions.length}</small></div>
    <div class="rail-project-sessions">${personalSessions.map(item => `<div class="session-row"><button class="session-item ${item.id === selectedID ? "active" : ""}" data-session-id="${escapeHTML(item.id)}" title="${escapeHTML(item.title)}"><strong>${escapeHTML(item.title)}</strong><span>${escapeHTML(providerModelLabel(item))}</span></button><button class="session-delete" data-delete-session="${escapeHTML(item.id)}" aria-label="ลบ ${escapeHTML(item.title)}">${uiIcon("close")}</button></div>`).join("") || `<p class="rail-project-empty">เริ่มแชทใหม่ได้เลย</p>`}</div></section>`;
  const projectGroups = state.projects.map(project => {
    const sessions = state.sessions.filter(item => item.project_id === project.id);
    const projectOpen = Object.prototype.hasOwnProperty.call(state.railProjectOpen, project.id)
      ? state.railProjectOpen[project.id]
      : project.id === state.currentProject?.id;
    return `<section class="rail-project ${project.id === state.currentProject?.id ? "active" : ""}">
      <div class="rail-project-head">
         <button class="rail-project-button" data-rail-project="${escapeHTML(project.id)}" title="เปิด ${escapeHTML(project.root_path || project.name)}">
          ${uiIcon("project")}<strong>${escapeHTML(project.name)}</strong><small>${sessions.length}</small>
        </button>
         <button class="rail-project-toggle" data-rail-project-toggle="${escapeHTML(project.id)}" aria-expanded="${projectOpen}" aria-label="${projectOpen ? "ย่อ" : "ขยาย"} ${escapeHTML(project.name)}"><span aria-hidden="true">›</span></button>
      </div>
      <div class="rail-project-sessions" data-rail-project-sessions="${escapeHTML(project.id)}" ${projectOpen ? "" : "hidden"}>${sessions.map(item => {
        const meta = `${providerModelLabel(item)} · ${item.context_profile}`;
         return `<div class="session-row"><button class="session-item ${item.id === selectedID ? "active" : ""}" data-session-id="${escapeHTML(item.id)}" title="${escapeHTML(`${item.title} — ${meta}`)}"><strong>${escapeHTML(item.title)}</strong><span>${escapeHTML(meta)}</span></button><button class="session-delete" data-delete-session="${escapeHTML(item.id)}" title="ลบแชตนี้" aria-label="ลบ ${escapeHTML(item.title)}">${uiIcon("close")}</button></div>`;
       }).join("") || `<p class="rail-project-empty">ยังไม่มีแชต</p>`}</div>
    </section>`;
  }).join("");
   return `<nav class="rail-primary" aria-label="เมนู Hermetrix">
       <button class="rail-nav-item ${state.view === "chat" ? "active" : ""}" data-rail-view="chat">${uiIcon("chat")}<span>แชท</span></button>
       <button class="rail-nav-item ${state.view === "code" ? "active" : ""}" data-rail-view="code">${uiIcon("files")}<span>พื้นที่ทำงาน</span></button>
       <button class="rail-nav-item" data-open-tasks>${uiIcon("activity")}<span>แผนงาน</span></button>
       <button class="rail-nav-item" data-rail-config="capabilities" title="Tools, Skills และ MCP ในที่เดียว">${uiIcon("tools")}<span>ความสามารถ</span></button>
       <button class="rail-nav-item" data-rail-config="providers">${uiIcon("model")}<span>โมเดล</span></button>
    </nav>
    ${personalGroup}<details class="rail-projects" id="railProjects" ${state.railProjectsOpen ? "open" : ""}>
       <summary class="nav-label rail-project-label"><span>โปรเจกต์</span><small>${state.projects.length}</small></summary>
       <div class="rail-project-list">${projectGroups || `<p class="rail-project-empty">ยังไม่มีโปรเจกต์</p>`}</div>
    </details>`;
}

function bindChatNavigation(dock) {
  $("#chatProviderSelect")?.addEventListener("change", event => { selectDraftProvider(event.target.value); state.sessionError=""; renderChat(); $("#chatProviderSelect")?.focus(); });
  $("#chatProfileSelect")?.addEventListener("change", event => { state.draftProfileName = event.target.value; state.draftQualificationReason=""; state.sessionError=""; renderChat(); $("#chatProfileSelect")?.focus(); });
  // Update state without re-rendering: the textarea lives inside an open
  // <details>, and a re-render would collapse it and take the caret with it.
  $("#chatQualificationReason")?.addEventListener("input", event => { state.draftQualificationReason=event.target.value; });
  // Remember the sidebar's disclosure state across re-renders and app restarts.
  $("#railProjects")?.addEventListener("toggle", event => { state.railProjectsOpen = event.target.open; saveLayout(); });
  $("#sessionSetup")?.addEventListener("click", openSessionSetup);
  $("#sessionOptions")?.addEventListener("toggle", event => { state.sessionOptionsOpen = event.target.open; saveLayout(); });
  $("#openProvidersFromDock")?.addEventListener("click", () => { closeSessionSetup(); switchTab("providers"); });
  $$("[data-rail-view]", dock).forEach(button => button.addEventListener("click", () => button.dataset.railView === "code" ? openWorkspace() : switchView(button.dataset.railView)));
  $$("[data-rail-config]", dock).forEach(button => button.addEventListener("click", () => openConfig(button.dataset.railConfig)));
  $$("[data-rail-project]", dock).forEach(button => button.addEventListener("click", async () => { dismissMobileRail(); if (state.view !== "chat") switchView("chat"); await openProject(button.dataset.railProject); }));
  $$("[data-rail-project-toggle]", dock).forEach(button => button.addEventListener("click", () => {
    const projectID = button.dataset.railProjectToggle;
    const open = button.getAttribute("aria-expanded") !== "true";
    state.railProjectOpen[projectID] = open;
    button.setAttribute("aria-expanded", String(open));
     button.setAttribute("aria-label", `${open ? "ย่อ" : "ขยาย"} ${state.projects.find(project => project.id === projectID)?.name || "โปรเจกต์"}`);
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
    selectDraftProvider(state.defaultProviderID && enabledProviders.some(provider => provider.id === state.defaultProviderID) ? state.defaultProviderID : HermetrixRuntime.preferredProvider(enabledProviders)?.id || "");
  }
  const draftProvider = enabledProviders.find(provider => provider.id === state.draftProviderID);
  const compatibleProfiles = availableProfiles(draftProvider);
  if (!compatibleProfiles.some(profile => profile.name === state.draftProfileName)) {
    state.draftProfileName = bestProfileFor(draftProvider, compatibleProfiles)?.name || "";
  }
  // Bind to the open project by default, not merely the first one in the
  // list: the picker is what put you here, and a session started from this
  // rail belongs where you actually are.
  if (state.draftProjectID === undefined) state.draftProjectID = "";
  const draftProfile = compatibleProfiles.find(profile => profile.name === state.draftProfileName);
  const admission = profileAdmission(draftProvider, draftProfile);
  const needsOverride = admission.mode === "override_required";
  const overrideReason = state.draftQualificationReason.trim() || suggestedOverrideReason(draftProvider, draftProfile);
  const canStart = Boolean(draftProvider && draftProfile && draftProvider.credential_ready &&
    (admission.admitted || (needsOverride && overrideReason.length >= 8)));
  const selectedID = state.sessionDetail?.session?.id || state.selectedSession;
  const previousMessageList = root ? $("#messageList", root) : null;
  const sameTranscript = previousMessageList?.dataset.sessionId === String(selectedID || "");
  const scrollWasNearBottom = !sameTranscript || previousMessageList.scrollHeight - previousMessageList.scrollTop - previousMessageList.clientHeight < 80;
  const previousScrollTop = sameTranscript ? previousMessageList.scrollTop : 0;
  const timeline = (state.sessionDetail?.events || []).filter(event => ["message", "tool_call", "tool_result", "approval_required", "approval_decision", "turn_failed"].includes(event.event_kind));
  const incompatibleSession = state.chatTurnError.includes("provider/model revision changed after session creation") || timeline.some(event => event.event_kind === "turn_failed" && String(event.content || "").includes("provider/model revision changed after session creation"));
  const latestOutcome = [...timeline].reverse().find(event => event.event_kind === "turn_failed" || (event.event_kind === "message" && event.role === "assistant"));
  const offlineProvider = !incompatibleSession && (/connectex|connection refused|actively refused/i.test(state.chatTurnError) || (latestOutcome?.event_kind === "turn_failed" && /connectex|connection refused|actively refused/i.test(latestOutcome.content || "")));
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
  const projectName = state.projects.find(item => item.id === session?.project_id)?.name || "แชทส่วนตัว";
  // Starting a session is one button. Provider, project and context are a
  // remembered default shown as a single line, and the three selects that used
  // to greet every new session live behind Options. Choosing a model is
  // configuration, not something to redo before each conversation.
  state.sessionReady = canStart;
  const draftProjectName = "แชทส่วนตัว";
  // Options stays shut unless the user opened it. It used to spring open
  // whenever an override was needed, which is most remote endpoints, and the
  // three selects plus a full explanation then filled the rail from the brand
  // to the settings row. The summary line carries the fact; the explanation
  // lives behind Options where someone can go and read it.
  const optionsOpen = state.sessionOptionsOpen;
  dock.innerHTML = `${renderRailNavigation(selectedID)}<button type="button" class="rail-nav-item session-setup" id="sessionSetup" aria-haspopup="dialog">${uiIcon("model")}<span>โมเดลและตัวเลือก</span></button>`;
  $("#sessionSetupBody").innerHTML = `<div class="session-create">${enabledProviders.length ? `
        <p class="session-summary">${escapeHTML(draftProjectName)} · ${draftProvider && !taskProviderIsLocal(draftProvider) ? "โมเดลภายนอก · ขออนุญาตก่อนเริ่มแชท" : "โมเดลในเครื่อง"}<span>การตั้งค่านี้ใช้กับแชทใหม่ แชทเดิมใช้โมเดลที่เลือกไว้ตอนเริ่ม</span></p>
         <label>โมเดล<select id="chatProviderSelect">${enabledProviders.map(provider => `<option value="${escapeHTML(provider.id)}" ${provider.id === state.draftProviderID ? "selected" : ""} ${provider.credential_ready ? "" : "disabled"}>${escapeHTML(providerOptionLabel(provider))}</option>`).join("")}</select></label>
        <button type="button" class="ghost" id="openProvidersFromDock">จัดการโมเดลและการเชื่อมต่อ</button>
         ${draftProvider && !draftProvider.credential_ready ? `<p class="session-error" role="alert">${escapeHTML(draftProvider.name)} ยังไม่มี API key เปิดหน้า Models เพื่อเพิ่มคีย์ แล้วใช้งานได้ทันที</p>` : ""}
         ${draftProfile && !admission.admitted && !needsOverride ? `<p class="session-error" role="alert">เหลืองบตอบเพียง ${admission.budget.toLocaleString()} โทเค็น เลือกขนาดบริบทที่ใหญ่ขึ้นในตัวเลือกเพิ่มเติม</p>` : ""}
         ${needsOverride ? `<p class="session-note">เซสชันนี้ใช้ข้อยกเว้นที่ตรวจแล้วเป็นเวลา 24 ชั่วโมง เหตุผลจะถูกบันทึกและแก้ไขได้ในตัวเลือกเพิ่มเติม</p>` : ""}
        ${state.sessionError ? `<p class="session-error" role="alert">${escapeHTML(state.sessionError)}</p>` : ""}
         <details class="session-options" id="sessionOptions" ${optionsOpen ? "open" : ""}><summary>ตัวเลือกเพิ่มเติม · ${escapeHTML(draftProfile ? profileLabel(draftProfile) : "บริบท")}</summary><div class="session-options-body">
           <label>ขนาดบริบท<select id="chatProfileSelect" ${compatibleProfiles.length ? "" : "disabled"}>${compatibleProfiles.map(profile => {
            const status = profileAdmission(draftProvider, profile);
             const note = status.blocking ? "เล็กเกินไปสำหรับโมเดลนี้"
               : status.mode === "qualified" ? "ผ่านการทดสอบ"
               : status.mode === "compatibility" ? "พร้อมใช้"
               : "ใช้ข้อยกเว้น";
             return `<option value="${profile.name}" ${profile.name === state.draftProfileName ? "selected" : ""} ${status.blocking ? "disabled" : ""}>${profileLabel(profile)} · งบตอบ ${status.budget.toLocaleString()} โทเค็น · ${note}</option>`;
          }).join("")}</select></label>
           ${draftProfile && admission.admitted ? `<p class="readiness-line">${admission.mode === "qualified" ? `ผ่านการทดสอบ ${escapeHTML(shortHash(admission.qualification.id))}` : `งบตอบ ${admission.budget.toLocaleString()} โทเค็น`}</p>` : ""}
           ${needsOverride ? `<div class="session-readiness review"><p class="readiness-line">ปลายทางโมเดลภายนอกทดสอบด้วยระบบในเครื่องไม่ได้ จึงใช้ข้อยกเว้นที่ตรวจแล้วเป็นเวลา 24 ชั่วโมง</p><textarea id="chatQualificationReason" rows="3" minlength="8">${escapeHTML(overrideReason)}</textarea></div>` : ""}
        </div></details>` : `
         <div class="session-needs-model"><p>ยังไม่มีโมเดลที่เชื่อมต่อ เพิ่มโมเดลแล้วเริ่มแชตใหม่ได้ทันที</p><button class="primary" id="openProvidersFromDock">เพิ่มโมเดล</button></div>`}
      </div>`;
  const railStart = $("#railNewSession");
  railStart.disabled = state.sending || Boolean(state.startingSession);
  // One short label on one line. Which envelope it opens under is the summary
  // line's job, not the button's.
  railStart.innerHTML = `${uiIcon("plus")}<span>แชทใหม่</span>`;
  bindChatNavigation(dock);
  // Navigation and model setup are shared by Chat, Workspace and Plans.
  // Only the mounted Chat view needs transcript and composer markup.
  if (!root) return;
  window.HermetrixVoice?.beforeRender();
  root.innerHTML = `<div class="chat-layout"><section class="chat-stage">
      ${session ? `<header class="chat-head"><div><p class="eyebrow">${escapeHTML(session.provider_name)} / ${escapeHTML(session.context_profile)}</p><h2>${escapeHTML(session.title)}</h2>${sessionUsageHTML(session, state.sessionDetail?.events, state.sessionDetail?.latest_context_snapshot)}<small>สัญญา ${escapeHTML(shortHash(session.contract_revision))} · รุ่นแคช ${session.cache_epoch} · ${escapeHTML(session.contract?.qualification?.mode || "ยังไม่ผูก")}</small><div class="session-capabilities"><button class="capability-chip" data-open-capabilities="skills">สกิล <strong>${selectedSkills.length}/${skillCatalog.length}</strong></button><button class="capability-chip" data-open-capabilities="tools">เครื่องมือ <strong>${directTools.length}</strong></button><button class="capability-chip" data-open-capabilities="mcp">MCP พร้อมใช้ <strong>${readyMCPTools}</strong></button></div></div><div class="chat-state">${pill(session.state, session.state === "active" ? "green" : "amber")}${pill(session.model,"blue")}</div></header>
        ${incompatibleSession ? `<div class="session-recovery" role="alert"><span>โมเดลของแชตนี้เปลี่ยนไปแล้ว จึงส่งข้อความต่อไม่ได้</span><button type="button" class="primary" id="recoverNewSession">เริ่มแชตใหม่</button></div>` : ""}
        ${offlineProvider ? `<div class="session-recovery" role="alert"><span>เซิร์ฟเวอร์โมเดลในเครื่องยังไม่เปิด ข้อความล่าสุดอยู่ในประวัติแชต</span><button type="button" class="primary" id="checkChatProvider">เปิด Models และทดสอบ</button></div>` : ""}
         <div class="message-list" id="messageList" data-session-id="${escapeHTML(selectedID || "")}">${timeline.length ? renderConversationTimeline(timeline) : `<div class="chat-welcome"><button type="button" class="voice-avatar" data-voice-open aria-label="เริ่มคุยด้วยเสียงกับ Hermetrix"><img src="/assets/brand/hermetrix-logo.png" alt=""></button><h3>Hermetrix พร้อมใช้งาน</h3><p>อธิบายสิ่งที่ต้องการให้ช่วยได้เลย คุณจะเห็นคำตอบและผลการใช้เครื่องมือที่นี่</p></div>`}${questions.map(elicitationCardHTML).join("")}<article class="chat-message assistant streaming ${state.sending ? "" : "hidden"}" id="streamingAssistant"><div class="message-role">Hermetrix</div><div class="message-body">${renderChatMarkdown(state.streamingText || "")}</div><div class="message-proof" id="streamStatus">กำลังรอโมเดล…</div></article></div>
        <div class="voice-host" id="voiceHost"></div>
        <form class="composer" id="chatForm"><div class="composer-tools"><button type="button" class="composer-tool-button" id="composerCapabilityButton">${uiIcon("plus")}<span>สกิลและเครื่องมือ</span></button><button type="button" class="composer-tool-button" id="composerFilesButton">${uiIcon("files")}<span>ไฟล์</span></button><button type="button" class="composer-tool-button" id="composerTerminalButton">${uiIcon("terminal")}<span>เทอร์มินัล</span></button><button type="button" class="composer-tool-button" id="composerImageButton" ${activeSessionSupportsVision() ? "" : "disabled"} title="${activeSessionSupportsVision() ? "แนบรูปภาพที่โมเดลรองรับ" : "เซสชันนี้ยังไม่มีโมเดลที่รองรับรูปภาพ"}">${uiIcon("artifact")}<span>รูปภาพ</span></button><button type="button" class="composer-tool-button voice-entry" data-voice-open aria-label="เริ่มคุยด้วยเสียงในเครื่อง">🎙 <span>พูด</span></button><input hidden id="composerImageInput" type="file" multiple accept="image/png,image/jpeg,image/webp"><span class="composer-context">${escapeHTML(projectName)} · ${escapeHTML(session.context_profile)}</span></div><div class="action-row" id="composerAttachments">${composerAttachmentHTML()}</div><textarea id="chatInput" rows="2" maxlength="1048576" placeholder="พิมพ์ข้อความถึง Hermetrix…" ${state.sending ? "disabled" : ""}></textarea><button class="primary" ${state.sending ? "disabled" : ""}>${state.sending ? "กำลังทำงาน…" : "ส่ง"}</button></form>` : `<div class="task-start">
          <div class="task-start-heading"><button type="button" class="voice-avatar" data-voice-open aria-label="เริ่มคุยด้วยเสียงกับ Hermetrix"><img src="/assets/brand/hermetrix-logo.png" alt=""></button><p class="eyebrow">แชทส่วนตัว</p><h1>อยากคุยเรื่องอะไร?</h1><p>ถามหรือคุยได้ทันที หากต้องลงมือทำงานให้นำเป้าหมายไป Workspace</p></div>
          <div class="voice-host" id="voiceHost"></div>
          <form class="start-composer" id="startTaskForm">
            <label class="sr-only" for="startPrompt">ข้อความที่อยากคุย</label><textarea id="startPrompt" rows="4" maxlength="1048576" placeholder="พิมพ์คำถามหรือเรื่องที่อยากคุย…">${escapeHTML(state.startDraft || "")}</textarea>
            <div class="start-composer-footer"><button type="button" class="model-choice" id="startModelOptions">${uiIcon("model")}${escapeHTML(draftProvider?.name || "เลือกโมเดล")} <span>⌄</span></button><div class="action-row"><button type="button" class="ghost voice-entry" data-voice-open aria-label="พูดด้วยเสียงในเครื่อง">🎙 พูด</button><button type="button" class="primary" id="startChatButton" ${canStart && !state.startingSession ? "" : "disabled"}>${state.startingSession ? "กำลังเริ่ม…" : "เริ่มคุย"}</button><button class="ghost" id="startPlanButton" ${state.startingSession ? "disabled" : ""}>${uiIcon("activity")} ไปทำใน Workspace</button></div></div>
          </form>
          <div class="start-readiness" role="status">${canStart ? `${pill("เลือกโมเดลแล้ว", "blue")}<span>${escapeHTML(providerModelLabel(draftProvider))} · ${escapeHTML(profileLabel(draftProfile))} · ${state.providerTestFeedback?.[draftProvider.id]?.error === false ? "ทดสอบการเชื่อมต่อแล้วในหน้านี้" : "จะตรวจการเชื่อมต่อเมื่อส่ง"}${needsOverride ? " · ใช้ข้อยกเว้น 24 ชั่วโมงตามเหตุผลในตัวเลือก" : ""}</span>` : `<span>ยังเริ่มคุยไม่ได้: ${draftProvider && !draftProvider.credential_ready ? "โมเดลนี้ยังไม่มี API key" : admission.mode === "answer_budget" ? "โมเดลนี้ใช้โทเค็นคิดจนไม่เหลือคำตอบ เลือกโมเดลอื่นหรือปรับรุ่นโมเดล" : "กรุณาตั้งค่าโมเดลและขนาดบริบท"}</span><button class="ghost" type="button" id="openProvidersButton">ตั้งค่าโมเดล</button>`}</div>
          ${state.sessionError ? `<p class="session-error" role="alert">${escapeHTML(state.sessionError)}</p>` : ""}
          <div class="start-suggestions"><button data-start-suggestion="ช่วยอธิบายเรื่องนี้ให้เข้าใจง่าย">${uiIcon("search")}<span>ขอคำอธิบาย<small>คุยเพื่อทำความเข้าใจ</small></span></button><button data-start-suggestion="ช่วยเปรียบเทียบทางเลือกและข้อดีข้อเสีย">${uiIcon("review")}<span>เปรียบเทียบทางเลือก<small>ก่อนตัดสินใจ</small></span></button><button data-start-plan="อยากเริ่มงานใหม่ ช่วยสรุปเป้าหมายและเกณฑ์ตรวจรับ">${uiIcon("activity")}<span>เริ่มงานใน Workspace<small>ตรวจเป้าหมายก่อนลงมือ</small></span></button><button type="button" data-start-config="capabilities">${uiIcon("tools")}<span>ดูความสามารถ<small>ตรวจ Tools, Skills และ MCP ที่พร้อมใช้</small></span></button></div>
          <p class="start-hint">แชทนี้เป็นส่วนตัวและไม่รันเครื่องมือ งานที่ต้องเปลี่ยนข้อมูลจะเริ่มจาก Workspace หลังคุณตรวจแผน</p>
        </div>`}
    </section></div>`;
  $$("[data-elicit-accept]", root).forEach(form => form.addEventListener("submit", answerElicitation));
  $$("[data-elicit-decline]", root).forEach(button => button.addEventListener("click", () => declineElicitation(button.dataset.elicitDecline)));
  $$("[data-approve-tool]", root).forEach(button => button.addEventListener("click", () => decideToolApproval(button.dataset.approveTool, "approve")));
  $$("[data-deny-tool]", root).forEach(button => button.addEventListener("click", () => decideToolApproval(button.dataset.denyTool, "deny")));
  $$("[data-open-capabilities]", root).forEach(button => button.addEventListener("click", () => openCapabilityPicker(button.dataset.openCapabilities)));
  $(".chat-head .session-usage", root)?.addEventListener("toggle", event => { tokenUsageDetailsOpen = event.currentTarget.open; });
  $("#composerCapabilityButton")?.addEventListener("click", () => openCapabilityPicker("all"));
  $("#composerFilesButton")?.addEventListener("click", () => switchWorkbench("files"));
  // Terminal no longer has a side-strip room to switch to; this opens (or
  // reveals) a terminal pane in Code instead, so the quick button still
  // works without reviving the second door the redesign closed.
  $("#composerTerminalButton")?.addEventListener("click", () => openContentPane("terminal"));
  $("#startPrompt")?.addEventListener("input", event => { state.startDraft = event.target.value; rememberProjectDraft(); });
  $("#startTaskForm")?.addEventListener("submit", startPlanFromGoal);
  $("#startChatButton")?.addEventListener("click", startChatFromGoal);
  $("#startModelOptions")?.addEventListener("click", openSessionSetup);
  $$("[data-start-suggestion]", root).forEach(button => button.addEventListener("click", () => { state.startDraft = button.dataset.startSuggestion; $("#startPrompt").value = state.startDraft; $("#startPrompt").focus(); }));
  $$("[data-start-plan]", root).forEach(button => button.addEventListener("click", () => { state.startDraft = button.dataset.startPlan; startPlanFromGoal(); }));
  $$("[data-start-config]", root).forEach(button => button.addEventListener("click", () => openConfig(button.dataset.startConfig)));
  bindComposer();
  window.HermetrixVoice?.onRender();
  $("#chatForm")?.addEventListener("submit", sendTurn);
  $("#recoverNewSession")?.addEventListener("click", () => {
    const pendingText = $("#chatInput")?.value?.trim() || state.draftMessage || "";
    if (beginNewTaskDraft()) {
      state.startDraft = pendingText;
      renderChat();
      $("#startPrompt")?.focus();
    }
  });
  $("#checkChatProvider")?.addEventListener("click", () => openConfig("providers"));
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
    else list.scrollTop = scrollWasNearBottom ? list.scrollHeight : previousScrollTop;
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
        <img class="runtime-mark" src="/assets/brand/hermetrix-logo.png" alt="">
        <div><strong>Hermetrix Engine</strong><small><span class="status-dot"></span> ทำงานในเครื่อง · ควบคุมสิทธิ์</small></div>
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
    <div class="workspace-pane-host" id="workspacePaneHost" aria-label="ช่องทำงานที่ปรับขนาดได้"></div>`;
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
  if (view !== state.view) window.HermetrixVoice?.end();
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
  rememberSurface(view);
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
      saveSessionDraft(state.sessionDetail?.session?.id, state.draftMessage);
      openCapabilityPicker("all");
      return;
    }
    state.draftMessage = event.target.value;
    saveSessionDraft(state.sessionDetail?.session?.id, state.draftMessage);
  });

  // Only a provider/runtime pair with completed image qualification exposes
  // this input. Upload and processing state stays separate from turn state.
  const attachComposerImages = async files => {
    if (!activeSessionSupportsVision()) { toast("เซสชันนี้ยังไม่มีโมเดลที่รองรับรูปภาพ", true); return; }
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
async function startPlanFromGoal(event) {
  event?.preventDefault();
  const goal = (event ? $("#startPrompt")?.value : state.startDraft)?.trim();
  if (!goal) { $("#startPrompt")?.focus(); return; }
  const destination = state.currentProject?.name;
  const approved = await askAction({title:"นำเป้าหมายไป Workspace", eyebrow:"ตรวจข้อมูลก่อนส่งต่อ",
    message:`ข้อความที่จะนำไปเป็นร่างงาน: ${goal.slice(0, 500)}${destination ? ` · Workspace ปลายทาง: ${destination}` : " · ขั้นต่อไปเลือก Workspace ปลายทาง"}`,
    confirmLabel:destination ? "เปิดร่างงาน" : "เลือก Workspace"});
  if (!approved) return;
  if (!state.currentProject?.id) {
    state.pendingWorkspaceGoal = goal;
    showPicker();
    return;
  }
  state.startDraft = goal;
  state.taskDraftObjective = goal;
  state.taskDraftDetails = {criteria:goal, require_user_acceptance:true};
  state.taskDraftAutoPlan = true;
  openTasks();
}

async function startChatFromGoal(event) {
  event?.preventDefault();
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
  state.plansOverview = !state.taskDraftObjective;
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
    if (!state.panes.includes("tasks")) state.panes.push("tasks");
    state.compactPane = "tasks";
    state.maximisedPane = state.panes.indexOf("tasks");
    renderPanes();
    $$("#viewSwitch [data-view]").forEach(button => button.classList.remove("on"));
    $("#plansViewButton")?.classList.add("on");
    $$(".rail-nav-item").forEach(button => button.classList.toggle("active", button.hasAttribute("data-open-tasks")));
    $("#durableTaskForm textarea[name='objective']")?.focus();
    rememberSurface("plans");
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
function rememberProjectDraft(projectID = state.currentProject?.id) {
  if (!projectID) return;
  const draft = {startDraft:state.startDraft || "", taskDraftObjective:state.taskDraftObjective || "", taskDraftAutoPlan:Boolean(state.taskDraftAutoPlan), taskDraftDetails:{...(state.taskDraftDetails || {})}};
  projectDrafts.set(projectID, draft);
  if (typeof sessionStorage === "undefined") return;
  try { sessionStorage.setItem(PROJECT_DRAFT_PREFIX + projectID, JSON.stringify(draft)); } catch {}
}

function renderAppearance() {
  const root = $("#view-appearance");
  if (!root) return;
  root.innerHTML = `<div class="panel"><div class="provider-head"><div><p class="eyebrow">APPEARANCE & LANGUAGE</p><h2>หน้าตาและการอ่าน</h2><p>ปรับระยะห่างให้เหมาะกับหน้าจอ การเลือกนี้บันทึกไว้ในเบราว์เซอร์เครื่องนี้</p></div></div><fieldset><legend>ระยะห่างของหน้าจอ</legend><label class="check-label"><input type="radio" name="appearanceDensity" value="comfortable" ${state.density === "comfortable" ? "checked" : ""}> อ่านสบาย · ตัวอักษรและช่องไฟมากขึ้น</label><label class="check-label"><input type="radio" name="appearanceDensity" value="compact" ${state.density === "compact" ? "checked" : ""}> กระชับ · เห็นข้อมูลมากขึ้นบนจอเล็ก</label></fieldset><p class="form-note neutral">ภาษาแสดงผลหลัก: ไทย ชื่อโมเดล เครื่องมือ และคำสั่งเทคนิคคงตามชื่อจริง</p></div>`;
  $$("[name='appearanceDensity']", root).forEach(input => input.addEventListener("change", () => {
    if (!input.checked) return;
    try { localStorage.setItem(DENSITY_STORAGE_KEY, input.value); } catch {}
    applyDensity(input.value);
  }));
}
function readProjectDraft(projectID) {
  if (!projectID || typeof sessionStorage === "undefined") return {};
  try { return JSON.parse(sessionStorage.getItem(PROJECT_DRAFT_PREFIX + projectID) || "null") || {}; } catch { return {}; }
}
function saveSessionDraft(sessionID, text) {
  if (!sessionID || typeof sessionStorage === "undefined") return;
  try {
    if (text && text.length <= 262144) sessionStorage.setItem(SESSION_DRAFT_PREFIX + sessionID, text);
    else if (!text) sessionStorage.removeItem(SESSION_DRAFT_PREFIX + sessionID);
  } catch {}
}
function readSessionDraft(sessionID) {
  if (!sessionID || typeof sessionStorage === "undefined") return "";
  try { return sessionStorage.getItem(SESSION_DRAFT_PREFIX + sessionID) || ""; } catch { return ""; }
}

function captureWorkspaceDrafts() {
  captureComposer();
  const projectID = state.currentProject?.id;
  if (projectID && (projectDrafts.has(projectID) || state.startDraft || state.taskDraftObjective)) rememberProjectDraft(projectID);
  const sessionID = state.sessionDetail?.session?.id;
  if (sessionID) {
    if (!sessionDrafts.has(sessionID) && !state.draftMessage) state.draftMessage = readSessionDraft(sessionID);
    sessionDrafts.set(sessionID, {text:state.draftMessage || "", attachments:state.composerAttachments.slice(),
      caret:state.composerCaret || 0, paneText:state.paneChatDraft || ""});
    saveSessionDraft(sessionID, state.draftMessage || "");
  }
}

function restoreWorkspaceDrafts(projectID, sessionID = null) {
  const project = projectDrafts.get(projectID) || readProjectDraft(projectID);
  const session = sessionDrafts.get(sessionID) || {text:readSessionDraft(sessionID)};
  state.startDraft = project.startDraft || "";
  state.taskDraftObjective = project.taskDraftObjective || "";
  state.taskDraftAutoPlan = Boolean(project.taskDraftAutoPlan);
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
  saveSessionDraft(sessionID, text);
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
  state.chatTurnError = "";
  state.selectedSession = null;
  state.elicitations = [];
  restoreWorkspaceDrafts(state.currentProject?.id);
  state.startDraft = "";
  const start = $("#startPrompt");
  if (start) start.value = "";
  return true;
}

async function createAgentSession(title = "", options = {}) {
  if (state.sessionCreationPending || !state.draftProviderID || !state.draftProfileName) return;
  // Chat stays personal. Workspace tools request a project binding explicitly.
  const projectID = options.projectID || "";
  if (projectID && state.currentProject?.id !== projectID) throw new Error("Workspace changed before session creation");
  const provider = state.providers.find(item => item.id === state.draftProviderID);
  const profile = state.profiles.find(item => item.name === state.draftProfileName);
  const admission = profileAdmission(provider, profile);
  const project = state.projects.find(item => item.id === projectID);
  const remote = provider && !taskProviderIsLocal(provider);
  let remoteEgressApproval = null;
  if (remote) {
    const reason = await askAction({title:"อนุญาตให้ใช้โมเดลภายนอก?", eyebrow:"ตรวจการส่งข้อมูล", message:`แชทนี้จะส่งข้อความและข้อมูล Workspace ที่คุณเลือกไปยัง ${provider.name} · ${provider.model} ที่ ${provider.base_url}`, confirmLabel:"อนุญาตและเริ่มแชท", reasonLabel:"เหตุผลการอนุญาต"});
    if (!reason) return;
    remoteEgressApproval = {actor:currentActor(),reason};
  }
  state.sessionCreationPending = true;
  ++sessionSelectionGeneration;
  ++navigationGeneration;
  try {
    state.sessionError = "";
    const body = {
      provider_id: state.draftProviderID,
      project_id: projectID,
      context_profile: state.draftProfileName,
      egress_policy: remote ? "remote_allowed" : "local_only",
      remote_egress_approval: remoteEgressApproval,
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
    if (!options.stayInWorkspace) {
      switchTab("chat");
      $("#chatInput")?.focus();
    }
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
    if (state.selectedSession !== id) state.chatTurnError = "";
    state.selectedSession = id;
    try { sessionStorage.setItem(LAST_SESSION_KEY, id); } catch {}
    state.selectedSkillDetail = null;
    state.sessionDetail = detail;
    state.draftProjectID = projectID || state.currentProject?.id;
    state.sessionError = "";
    state.elicitations = [];
    restoreWorkspaceDrafts(state.currentProject?.id, id);
    renderChat();
    const reviewPane = $(".pane-body[data-pane-kind='review']");
    if (state.panes.includes("review") && reviewPane) renderWorkbenchReview(reviewPane);
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
async function submitChatText(content, attachments = [], options = {}) {
  if (state.sending || !state.sessionDetail?.session) return false;
  const sessionID = state.sessionDetail.session.id;
  state.turnSessionID = sessionID;
  state.sending = true;
  state.chatTurnError = "";
  state.streamingText = "";
  renderChat();
  refreshPaneChat();
  setTimeout(pollElicitations, 600);
  try {
    const payload = attachments.length ? {parts:[...(content ? [{kind:"text",text:content}] : []),...attachments.map(item=>({kind:"image",artifact_id:item.artifact_id,metadata:{name:item.name}}))]} : {content};
    if (options.idempotencyKey) payload.idempotency_key = options.idempotencyKey;
    const response = await fetch(`/api/sessions/${encodeURIComponent(sessionID)}/turns`, { method:"POST", headers:{"Content-Type":"application/json"}, body:JSON.stringify(payload), signal:options.signal });
    if (!response.ok) {
      const body = await response.json().catch(() => ({}));
      throw new Error(body.error || `Turn failed (${response.status})`);
    }
    await consumeAgentStream(response, options.onEvent);
    await selectSession(sessionID, {refresh:true});
    return true;
  } catch (error) {
    if (error.name !== "AbortError") toast(error.message, true);
    state.chatTurnError = error.message || "";
    await selectSession(sessionID, {refresh:true}).catch(() => {});
    return false;
  } finally {
    state.sending = false;
    state.streamingText = "";
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
    title: decision === "approve" ? "อนุมัติคำสั่งนี้ครั้งเดียว?" : "ปฏิเสธคำสั่งนี้?",
    message: `${approval.summary}. การอนุมัติผูกกับคำสั่งและข้อมูลที่แสดงนี้เท่านั้น ไม่ครอบคลุมคำสั่งครั้งถัดไป`,
    confirmLabel: decision === "approve" ? "อนุมัติคำสั่งนี้" : "ปฏิเสธ",
    reasonLabel: decision === "deny" ? "เหตุผล" : "",
    danger: decision === "deny"
  });
  if (!response) return;
  const sessionID = approval.session_id;
  state.sending = true;
  state.streamingText = "";
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
    toast(decision === "approve" ? "อนุมัติคำสั่งนี้แล้วและบันทึกหลักฐาน" : "ปฏิเสธคำสั่งนี้แล้ว ระบบทำงานต่อโดยไม่เรียกใช้คำสั่ง");
  } catch (error) {
    toast(error.message, true);
    await selectSession(sessionID, {refresh:true}).catch(() => {});
  } finally {
    state.sending = false;
    state.streamingText = "";
    renderChat();
  }
}

async function consumeAgentStream(response, onEvent) {
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
      onEvent?.(item);
      window.HermetrixAssistant?.onStream(item);
      if (item.type === "user_committed" && item.event) {
        state.sessionDetail.events.push(item.event);
        renderChat();
      } else if (item.type === "step_bound") {
        const status = $("#streamStatus");
        if (status) status.textContent = `${item.context_report?.profile || "context"} · immutable step ${shortHash(item.binding?.id)}`;
      } else if (item.type === "project_brain_unavailable") {
        toast(item.error || "Project Brain evidence was unavailable; continuing without it", true);
      } else if (item.type === "delta" && item.delta?.content) {
        const list = $("#messageList");
        const followTurn = list && list.scrollHeight - list.scrollTop - list.clientHeight < 80;
        state.streamingText = (state.streamingText || "") + item.delta.content;
        const target = $("#streamingAssistant .message-body");
        if (target) target.innerHTML = renderChatMarkdown(state.streamingText);
        if (list && followTurn) list.scrollTop = list.scrollHeight;
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
  const list = state.providers;
  const qualified = list.filter(item => state.qualifications.some(run => run.provider_id === item.id && run.model === item.model && run.provider_revision === item.revision)).length;
  const hero = `<header class="models-header"><div><p class="eyebrow">การตั้งค่าโมเดล</p><h2>โมเดล</h2><p>จัดการผู้ให้บริการและเลือกโมเดลเริ่มต้นสำหรับงานใหม่ที่นี่ งานที่เริ่มแล้วจะใช้โมเดลเดิม</p></div><div class="models-header-actions"><span class="models-count">${list.length} โมเดล · ${qualified} ตรวจแล้ว</span><button type="button" class="primary" id="modelAdd">+ เพิ่มโมเดล</button></div></header>`;
  const cards = list.length ? list.map(provider => {
    const feedback = state.providerTestFeedback?.[provider.id];
    const latest = state.qualifications.find(run => run.provider_id === provider.id && run.model === provider.model && run.provider_revision === provider.revision);
    const keyState = providerCredentialLabel(provider);
    const connectionState = feedback ? feedback.error ? "ทดสอบไม่ผ่าน" : "เชื่อมต่อสำเร็จในหน้านี้" : "ยังไม่ทดสอบการเชื่อมต่อ";
    return `<article class="model-card" data-model-card="${escapeHTML(provider.id)}"><div class="model-card-main"><div class="model-card-title"><div><h3>${escapeHTML(provider.name)}</h3><p>${escapeHTML(providerModelLabel(provider))}</p></div>${pill(provider.id === state.defaultProviderID ? "ค่าเริ่มต้น" : provider.enabled ? "เปิดใช้งาน" : "ปิดอยู่", provider.id === state.defaultProviderID || provider.enabled ? "green" : "amber")}</div><p class="model-card-endpoint">${escapeHTML(provider.base_url)}</p><div class="model-card-meta"><span>${taskProviderIsLocal(provider) ? "ในเครื่อง" : "ภายนอก"} · ${escapeHTML(provider.adapter_kind)}</span><span>${escapeHTML(keyState)}</span><span data-provider-connection="${escapeHTML(provider.id)}">${escapeHTML(connectionState)}</span><span>${latest ? `บริบท: ${escapeHTML(latest.context_tier)} · grade ${escapeHTML(latest.capability_grade)}` : "ขนาดบริบทยังไม่ผ่านการตรวจ"}</span></div><div class="model-card-actions"><button type="button" class="ghost" data-default-provider="${escapeHTML(provider.id)}" ${provider.id === state.defaultProviderID || !provider.enabled || !provider.credential_ready ? "disabled" : ""}>${provider.id === state.defaultProviderID ? "ใช้เป็นค่าเริ่มต้นแล้ว" : "ตั้งเป็นค่าเริ่มต้น"}</button><button type="button" class="ghost" data-test-provider="${escapeHTML(provider.id)}" ${provider.enabled && provider.credential_ready ? "" : "disabled"}>ทดสอบการเชื่อมต่อ</button><button type="button" class="ghost" data-edit-provider="${escapeHTML(provider.id)}">แก้ไข</button><button type="button" class="ghost" data-provider-key="${escapeHTML(provider.id)}">จัดการ API key</button><button type="button" class="ghost" data-open-qualification="${escapeHTML(provider.id)}" ${provider.enabled && provider.credential_ready ? "" : "disabled"}>ตรวจความพร้อม</button></div><p class="model-test-result ${feedback?.error ? "error" : ""}" data-provider-test-result="${escapeHTML(provider.id)}" role="status">${feedback ? escapeHTML(feedback.text) : ""}</p></div><details class="model-card-details" data-model-details="${escapeHTML(provider.id)}"><summary>รายละเอียดและตัวเลือกการตรวจ</summary><div class="model-card-details-body"><div class="model-detail-grid"><span>Context</span><strong>${provider.context_window.toLocaleString()}</strong><span>Output</span><strong>${provider.max_output_tokens.toLocaleString()}</strong><span>หลักฐานบริบท</span><strong>${provider.context_evidence === "qualified" ? "ตรวจแล้ว" : "ค่าที่ระบุไว้"}</strong></div><form class="model-qualify-form" data-qualify-form="${escapeHTML(provider.id)}"><h4>ตรวจความพร้อมของ ${escapeHTML(provider.name)}</h4><label>ขนาดบริบทที่ต้องการ<select name="requested_profile">${state.profiles.map(profile => `<option value="${escapeHTML(profile.name)}" ${profile.name === "certified-64k" ? "selected" : ""}>${profileLabel(profile)}</option>`).join("")}</select></label><label>Runtime ในเครื่อง (ถ้ามี)<select name="runtime"><option value="">ไม่ระบุ</option><option value="ollama">Ollama</option><option value="lmstudio">LM Studio</option><option value="vllm">vLLM</option><option value="llamacpp">llama.cpp</option></select></label><label class="model-runtime-endpoint" hidden>URL ของ runtime<input name="endpoint" type="url" value="http://127.0.0.1:11434"></label><button class="primary" type="submit">เริ่มตรวจความพร้อม</button></form></div></details></article>`;
  }).join("") : `<div class="empty model-empty"><h3>ยังไม่มีโมเดล</h3><p>เพิ่มโมเดลในเครื่องหรือผู้ให้บริการที่ใช้ แล้วทดสอบก่อนเริ่มแชต</p></div>`;
  const setup = `<details class="model-connect panel" id="modelConnect" ${list.length ? "" : "open"}><summary>เพิ่มหรือแก้ไขโมเดล</summary><div class="connection-setup-body"><form id="providerForm"><input type="hidden" name="id" value="">
      <div class="model-form-grid"><label>ชื่อที่เรียก<input name="name" required maxlength="80" placeholder="เช่น Bonsai Local"></label><label>โปรโตคอล<select name="adapter_kind"><option value="openai-compatible">OpenAI-compatible</option><option value="anthropic-native">Anthropic native</option><option value="gemini-native">Gemini native</option></select></label></div>
      <div class="model-form-grid"><label>URL เซิร์ฟเวอร์<input name="base_url" required type="url" placeholder="http://127.0.0.1:8088/v1"></label><label>Model ID<input name="model" required maxlength="240" placeholder="ชื่อโมเดลที่เซิร์ฟเวอร์ใช้"></label></div>
      <label>API key <small>ปล่อยว่างสำหรับโมเดลในเครื่อง หรือเมื่อต้องการคงคีย์เดิม</small><input name="api_key" type="password" autocomplete="off" spellcheck="false" placeholder="ใส่คีย์เมื่อผู้ให้บริการกำหนด"></label>
      <details class="field-help"><summary>คีย์เก็บไว้ที่ไหน?</summary><p>คีย์จะอยู่ในไฟล์ลับบนเครื่องนี้ และไม่อยู่ในฐานข้อมูลหรือไฟล์สำรอง</p></details>
      <details class="advanced-fields"><summary>Advanced</summary><div class="advanced-fields-body">
        <div class="form-grid"><label>Context window<input name="context_window" type="number" min="4096" max="2097152" value="131072" required></label><label>Max output<input name="max_output_tokens" type="number" min="128" value="8192" required></label></div><label>ตัวแปร environment สำหรับคีย์<input name="api_key_env" pattern="[A-Z][A-Z0-9_]{1,126}" placeholder="HERMETRIX_PROVIDER_API_KEY"></label><label class="check-label"><input name="enabled" type="checkbox" checked> เปิดใช้งานโมเดลนี้</label>
      </div></details>
      <div class="model-form-actions"><button class="primary" id="providerSubmit">บันทึกโมเดล</button><button type="button" class="ghost" id="providerCancel">ยกเลิก</button></div>
    </form></div></details>`;
  const runs = state.qualifications.length ? `<details class="model-runs" id="modelRuns"><summary>ผลตรวจที่ผ่านมา <span>${state.qualifications.length}</span></summary><div class="model-run-list">${state.qualifications.map(run => `<article class="model-run"><div><strong>${escapeHTML(run.provider_name)} · ${escapeHTML(run.model)}</strong><small>${formatDate(run.completed_at || run.started_at)} · ${escapeHTML(run.context_tier)} · ${run.eligible ? "พร้อมใช้" : "ต้องตรวจเพิ่ม"}</small></div>${pill(`grade ${run.capability_grade}`, run.capability_grade === "A" ? "green" : run.capability_grade === "B" ? "amber" : "red")}</article>`).join("")}</div></details>` : "";
  root.innerHTML = `${hero}<p class="form-note neutral model-context-hint">ขนาดบริบทเป็นพื้นที่รวมของข้อมูลเข้าและคำตอบ ระบบกันพื้นที่ให้คำตอบและความไม่แน่นอน จึงส่งข้อความเข้าได้จริงน้อยกว่าตัวเลข 64k หรือ 128k ที่แสดง ดูจำนวนที่ใช้จริงหลังส่งงานในหน้า “บริบทโมเดล”</p><div class="models-surface">${setup}<div class="model-list">${cards}</div>${runs}</div>`;
  $("#modelAdd")?.addEventListener("click", () => openProviderEditor());
  $$("[data-default-provider]", root).forEach(button => button.addEventListener("click", () => saveDefaultProvider(button.dataset.defaultProvider)));
  $("#providerCancel")?.addEventListener("click", () => { $("#modelConnect").open = false; $("#providerForm").reset(); $("#providerForm").elements.namedItem("id").value = ""; });
  $("#providerForm")?.addEventListener("submit", saveProvider);
  $$("[data-test-provider]", root).forEach(button => button.addEventListener("click", () => testProvider(button.dataset.testProvider)));
  $$("[data-edit-provider]", root).forEach(button => button.addEventListener("click", () => openProviderEditor(button.dataset.editProvider)));
  $$("[data-open-qualification]", root).forEach(button => button.addEventListener("click", () => { const details = $(`[data-model-details="${CSS.escape(button.dataset.openQualification)}"]`, root); details.open = true; details.querySelector('[name="requested_profile"]')?.focus(); }));
  $$("[data-qualify-form]", root).forEach(form => {
    const provider = list.find(item => item.id === form.dataset.qualifyForm);
    const profiles = new Set(availableProfiles(provider).map(profile => profile.name));
    const select = form.elements.namedItem("requested_profile");
    [...select.options].forEach(option => { if (!profiles.has(option.value)) option.remove(); });
    if (!profiles.has(select.value)) select.selectedIndex = 0;
    form.querySelector('button[type="submit"]').disabled = !select.options.length;
    form.addEventListener("submit", event => { event.preventDefault(); qualifyProvider(form.dataset.qualifyForm, form); });
    form.elements.namedItem("runtime").addEventListener("change", event => { form.querySelector(".model-runtime-endpoint").hidden = !event.target.value; });
  });
  $$("[data-provider-key]", root).forEach(button => button.addEventListener("click", () => setProviderCredential(button.dataset.providerKey)));
}

function openProviderEditor(id = "") {
  const form = $("#providerForm");
  const panel = $("#modelConnect");
  if (!form || !panel) return;
  form.reset();
  const provider = state.providers.find(item => item.id === id);
  if (provider) {
    for (const name of ["id", "name", "adapter_kind", "base_url", "model", "api_key_env", "context_window", "max_output_tokens"]) {
      form.elements.namedItem(name).value = provider[name] ?? "";
    }
    form.elements.namedItem("enabled").checked = provider.enabled;
    form.querySelector(".advanced-fields").open = true;
  }
  $("#providerSubmit").textContent = provider ? "บันทึกการแก้ไข" : "บันทึกโมเดล";
  panel.open = true;
  panel.scrollIntoView({block:"start", behavior:"smooth"});
  form.elements.namedItem("name").focus({preventScroll:true});
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
    if (state.providerTestFeedback) delete state.providerTestFeedback[id];
    toast(token.trim() ? "API key saved on this machine" : "Saved API key removed");
    await load();
    switchTab("providers");
  } catch (error) { toast(error.message, true); }
}

async function qualifyProvider(id, form) {
  const provider = state.providers.find(item => item.id === id);
  if (!provider || !form) return;
  const values = new FormData(form);
  const runtime = String(values.get("runtime") || "");
  const input = { provider_id:id, requested_profile:String(values.get("requested_profile") || "") };
  if (runtime) input.runtime_probe = { runtime, endpoint:String(values.get("endpoint") || "").trim(), model:provider.model };
  const button = form.querySelector('button[type="submit"]');
  button.disabled = true;
  button.textContent = "กำลังตรวจ…";
  try {
    const run = await api("/api/qualifications", { method:"POST", body:JSON.stringify(input) });
    const message = run.eligible ? `ตรวจแล้ว: ${run.context_tier} · grade ${run.capability_grade}` : "ตรวจเสร็จ แต่ยังต้องตัดสินใจเรื่องขนาดบริบท";
    state.providerTestFeedback = {...state.providerTestFeedback, [id]: {text:message, error:false}};
    await load(); switchTab("providers");
    $("#modelRuns")?.setAttribute("open", "");
  } catch (error) {
    state.providerTestFeedback = {...state.providerTestFeedback, [id]: {text:error.message, error:true}};
    const status = $(`[data-provider-test-result="${CSS.escape(id)}"]`);
    if (status) { status.textContent = error.message; status.classList.add("error"); }
    button.disabled = false;
    button.textContent = "เริ่มตรวจความพร้อม";
  }
}

async function saveProvider(event) {
  event.preventDefault();
  const form = event.currentTarget;
  const values = new FormData(form);
  try {
    const key = String(values.get("api_key") || "");
    const editedID = String(values.get("id") || "");
    await api("/api/providers", { method:"POST", body:JSON.stringify({ id:values.get("id") || "", name:values.get("name"), adapter_kind:values.get("adapter_kind"), base_url:values.get("base_url"), model:values.get("model"), api_key:key, api_key_env:values.get("api_key_env"), context_window:Number(values.get("context_window")), context_evidence:"declared", max_output_tokens:Number(values.get("max_output_tokens")), enabled:values.get("enabled") === "on" }) });
    if (editedID && state.providerTestFeedback) delete state.providerTestFeedback[editedID];
    form.reset();
    toast(key.trim() ? "Model connected and API key saved on this machine" : "Model connected");
    await load();
    switchTab("providers");
  } catch (error) { toast(error.message, true); }
}

async function testProvider(id) {
  const button = $(`[data-test-provider="${CSS.escape(id)}"]`);
  const status = $(`[data-provider-test-result="${CSS.escape(id)}"]`);
  const connection = $(`[data-provider-connection="${CSS.escape(id)}"]`);
  if (button) { button.disabled = true; button.textContent = "กำลังทดสอบ…"; }
  if (status) { status.textContent = "กำลังส่งคำขอทดสอบ…"; status.classList.remove("error"); }
  if (connection) connection.textContent = "กำลังทดสอบการเชื่อมต่อ";
  try {
    const result = await api(`/api/providers/${encodeURIComponent(id)}/test`, { method:"POST", body:"{}" });
    const message = `เชื่อมต่อสำเร็จ · ตอบกลับใน ${result.latency_ms} ms`;
    state.providerTestFeedback = {...state.providerTestFeedback, [id]: {text:message, error:false}};
    if (status) status.textContent = message;
    if (connection) connection.textContent = "เชื่อมต่อสำเร็จในหน้านี้";
  } catch (error) {
    state.providerTestFeedback = {...state.providerTestFeedback, [id]: {text:error.message, error:true}};
    if (status) { status.textContent = error.message; status.classList.add("error"); }
    if (connection) connection.textContent = "ทดสอบไม่ผ่าน";
  } finally { if (button) { button.disabled = false; button.textContent = "ทดสอบการเชื่อมต่อ"; } }
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

function capabilitySourceLabel(item) {
  const server = state.mcp_servers.find(candidate => candidate.id === item?.source_ref);
  return server?.name || item?.source_ref || item?.source || "MCP server";
}

function capabilityDisplayTitle(item) {
  return `${capabilitySourceLabel(item)} · ${item.title || item.name}`;
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

let capabilityCenterSearchSeq = 0;

function capabilityCenterServerStatus(server) {
  if (!server.enabled) return ["ปิดใช้งาน", "amber"];
  if (!server.credential_ready) return ["ต้องตั้งค่าการเข้าถึง", "amber"];
  if (server.status === "ready") return ["ค้นพบเครื่องมือแล้ว", "green"];
  if (server.status === "error") return ["เชื่อมต่อไม่สำเร็จ", "red"];
  return ["รอค้นพบเครื่องมือ", "amber"];
}

function capabilityUseReadiness(kind, name) {
  const session = state.sessionDetail?.session;
  if (!session?.project_id) return "เปิดแชทที่ผูกโปรเจกต์จาก Workspace ก่อนใช้เครื่องมือ";
  const bindings = session.contract?.tool_bindings || [];
  const required = kind === "mcp" ? ["tool_search", "tool_describe", "tool_call"] : [name];
  if (required.some(tool => !bindings.some(binding => binding.name === tool))) return "แชทนี้ไม่มีเครื่องมือนี้ในสัญญาที่ตรึงไว้ เริ่มแชทใหม่ใน Workspace";
  return "";
}

function refreshCapabilityCenterResults(mcpOnly = false) {
  const root = $("#view-capabilities");
  if (!root) return;
  const query = String(state.capabilityCenterQuery || "").trim().toLowerCase();
  const matches = value => !query || String(value || "").toLowerCase().includes(query);
  const direct = asList(state.direct_tools).filter(item => matches(`${item.name} ${item.description}`));
  const skills = asList(state.skills).filter(item => matches(`${item.canonical_name} ${item.summary} ${item.scope_kind}`));
  const servers = asList(state.mcp_servers).filter(item => matches(`${item.name} ${item.endpoint}`));
  const result = (state.capabilityCenterResults || []).filter(item => item.source === "mcp");
  if (!mcpOnly) {
  const toolRow = item => `<details class="capability-center-item"><summary><strong>${escapeHTML(item.name)}</strong>${pill(item.requires_approval ? "ต้องอนุมัติ" : "มีในระบบ", item.requires_approval ? "amber" : "blue")}</summary><p>${escapeHTML(item.description || "ไม่มีคำอธิบาย")}</p><small>เครื่องมือในตัว · ${escapeHTML(item.effect || "อ่านข้อมูล")} · revision ${escapeHTML(item.revision || "ไม่ระบุ")}</small><button type="button" class="ghost" data-center-tool="${escapeHTML(item.name)}">ดู schema และวิธีใช้</button></details>`;
  $("#capabilityCenterTools").innerHTML = direct.length ? direct.map(toolRow).join("") : `<p class="probe-empty">ไม่พบเครื่องมือในตัวที่ตรงกับคำค้น</p>`;
  $("#capabilityCenterSkills").innerHTML = skills.length ? skills.map(item => `<details class="capability-center-item"><summary><strong>${escapeHTML(item.canonical_name)}</strong>${pill(item.enabled && item.state === "active" ? "พร้อมใช้ในแชทใหม่" : "ยังไม่พร้อม", item.enabled && item.state === "active" ? "green" : "amber")}</summary><p>${escapeHTML(item.summary || "ไม่มีคำอธิบาย")}</p><small>Skill · ${escapeHTML(item.scope_kind)} · version ${escapeHTML(shortHash(item.current_version_id || ""))}</small><button type="button" class="ghost" data-center-skill="${escapeHTML(item.canonical_name)}">ดูใน Skill Studio</button></details>`).join("") : `<p class="probe-empty">${query ? "ไม่พบ Skill ที่ตรงกับคำค้น" : "ยังไม่มี Skill ที่เปิดใช้ เพิ่มและตรวจได้ใน Skill Studio"}</p>`;
  $("#capabilityCenterServers").innerHTML = servers.length ? servers.map(item => { const [label, tone] = capabilityCenterServerStatus(item); return `<div class="capability-center-item"><div class="capability-center-item-head"><strong>${escapeHTML(item.name)}</strong>${pill(label, tone)}</div><small>MCP server · ${escapeHTML(item.transport_kind)} · ${Number(item.tool_count || 0)} เครื่องมือที่ค้นพบ</small>${item.last_error ? `<p class="capability-center-error">${escapeHTML(item.last_error)}</p>` : ""}<button type="button" class="ghost" data-center-server="${escapeHTML(item.name)}">ตรวจสถานะและจัดการ</button></div>`; }).join("") : `<p class="probe-empty">${query ? "ไม่พบ MCP server ที่ตรงกับคำค้น" : "ยังไม่มี MCP server เชื่อมต่อ"}</p>`;
  }
  $("#capabilityCenterMCP").innerHTML = !query ? `<p class="probe-empty">พิมพ์ชื่อเครื่องมือเพื่อค้นหา MCP ที่เชื่อมต่ออยู่</p>` : state.capabilityCenterSearching ? `<p class="probe-empty" role="status">กำลังค้นหา MCP…</p>` : state.capabilityCenterError ? `<p class="probe-empty" role="alert">ค้นหา MCP ไม่สำเร็จ: ${escapeHTML(state.capabilityCenterError)}</p>` : result.length ? result.map(item => `<details class="capability-center-item"><summary><strong>${escapeHTML(capabilityDisplayTitle(item))}</strong>${pill(item.readiness === "ready" ? "พร้อมใช้" : "ยังไม่พร้อม", item.readiness === "ready" ? "green" : "amber")}</summary><p>${escapeHTML(item.description || "ไม่มีคำอธิบาย")}</p><small>${escapeHTML(item.effect || "unknown")} · ${item.requires_approval ? "ต้องอนุมัติก่อนเรียก" : "ไม่ต้องอนุมัติ"}</small><button type="button" class="ghost" data-center-inspect="${escapeHTML(item.id)}">ตรวจ schema และสิทธิ์</button></details>`).join("") : `<p class="probe-empty">ไม่พบเครื่องมือ MCP ที่ตรงกับคำค้น</p>`;
  $("#capabilityCenterMCPCount").textContent = String(result.length);
  if (!mcpOnly) {
  for (const [id, count] of [["capabilityCenterToolCount", direct.length], ["capabilityCenterSkillCount", skills.length]]) $("#"+id).textContent = String(count);
  $$("[data-center-filter]", root).forEach(button => button.setAttribute("aria-pressed", String(button.dataset.centerFilter === (state.capabilityCenterFilter || "all"))));
  $$("[data-center-section]", root).forEach(section => { section.hidden = (state.capabilityCenterFilter || "all") !== "all" && state.capabilityCenterFilter !== section.dataset.centerSection; });
  $$("[data-center-tool]", root).forEach(button => button.addEventListener("click", () => { state.directToolQuery = button.dataset.centerTool; switchTab("tools"); }));
  $$("[data-center-skill]", root).forEach(button => button.addEventListener("click", () => { state.skillCollectionFilter = "all"; $("#stateFilter").value = ""; $("#searchInput").value = button.dataset.centerSkill; switchTab("library"); }));
  $$("[data-center-server]", root).forEach(button => button.addEventListener("click", () => { state.mcpServerFilter = "all"; state.mcpServerQuery = button.dataset.centerServer; switchTab("mcp"); }));
  }
  $$("[data-center-inspect]", root).forEach(button => button.addEventListener("click", () => { state.mcpCapabilityQuery = state.capabilityCenterQuery || ""; state.capabilityResults = state.capabilityCenterResults || []; switchTab("mcp"); inspectCapability(button.dataset.centerInspect); }));
}

function renderCapabilityCenter() {
  const root = $("#view-capabilities");
  if (!root) return;
  root.innerHTML = `<div class="capability-center"><header class="provider-head"><div><p class="eyebrow">ความสามารถ</p><h2>Tools · Skills · MCP</h2><p>ค้นหา ตรวจสถานะ แล้วเปิดรายละเอียดจากแหล่งข้อมูลจริง เครื่องมือจะใช้ได้ตามสัญญาของแชทและสิทธิ์ที่ตั้งไว้</p></div></header><label class="capability-center-search">ค้นหาความสามารถ<input id="capabilityCenterQuery" type="search" placeholder="ชื่อเครื่องมือ Skill หรือ MCP server…" value="${escapeHTML(state.capabilityCenterQuery || "")}"></label><div class="capability-center-filters" role="group" aria-label="กรองชนิดความสามารถ"><button type="button" data-center-filter="all">ทั้งหมด</button><button type="button" data-center-filter="tools">เครื่องมือในตัว</button><button type="button" data-center-filter="skills">Skills</button><button type="button" data-center-filter="mcp">MCP</button></div><div class="capability-center-grid"><section class="panel" data-center-section="tools"><div class="provider-head"><h3>เครื่องมือในตัว <small id="capabilityCenterToolCount"></small></h3><button type="button" class="ghost compact" data-center-page="tools">ดูทั้งหมด</button></div><div id="capabilityCenterTools" class="capability-center-list"></div></section><section class="panel" data-center-section="skills"><div class="provider-head"><h3>Skills <small id="capabilityCenterSkillCount"></small></h3><button type="button" class="ghost compact" data-center-page="library">Skill Studio</button></div><div id="capabilityCenterSkills" class="capability-center-list"></div></section><section class="panel" data-center-section="mcp"><div class="provider-head"><h3>MCP server</h3><button type="button" class="ghost compact" data-center-page="mcp">จัดการการเชื่อมต่อ</button></div><div id="capabilityCenterServers" class="capability-center-list"></div><h4>เครื่องมือ MCP <small id="capabilityCenterMCPCount"></small></h4><div id="capabilityCenterMCP" class="capability-center-list"></div></section></div></div>`;
  refreshCapabilityCenterResults();
  $("#capabilityCenterQuery").addEventListener("input", event => {
    const query = event.target.value;
    state.capabilityCenterQuery = query;
    state.capabilityCenterResults = [];
    state.capabilityCenterError = "";
    state.capabilityCenterSearching = Boolean(query.trim());
    refreshCapabilityCenterResults();
    const seq = ++capabilityCenterSearchSeq;
    if (!query.trim()) return;
    setTimeout(async () => {
      if (seq !== capabilityCenterSearchSeq || state.activeTab !== "capabilities") return;
      try {
        const data = await api(`/api/capabilities?query=${encodeURIComponent(query.trim())}&limit=50`);
        if (seq !== capabilityCenterSearchSeq || state.activeTab !== "capabilities") return;
        state.capabilityCenterResults = data.results || [];
      } catch (error) { if (seq === capabilityCenterSearchSeq) state.capabilityCenterError = error.message; }
      finally { if (seq === capabilityCenterSearchSeq && state.activeTab === "capabilities") { state.capabilityCenterSearching = false; refreshCapabilityCenterResults(true); } }
    }, 180);
  });
  $$('[data-center-filter]', root).forEach(button => button.addEventListener("click", () => { state.capabilityCenterFilter = button.dataset.centerFilter; refreshCapabilityCenterResults(); }));
  $$('[data-center-page]', root).forEach(button => button.addEventListener("click", () => switchTab(button.dataset.centerPage)));
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
        const useError = capabilityUseReadiness("tool", name);
        return `<details class="direct-tool-card" data-tool-text="${escapeHTML(`${name} ${tool.description || ""}`.toLowerCase())}"><summary><span class="direct-tool-avatar" aria-hidden="true">${escapeHTML(short)}</span><span><strong>${escapeHTML(name)}</strong><small>${escapeHTML(tool.description || "ไม่มีคำอธิบาย")}</small></span></summary><div class="direct-tool-detail"><div class="meta">${pill(tool.effect || "read", tool.requires_approval ? "amber" : "green")}${tool.requires_approval ? pill("ต้องอนุมัติ", "amber") : ""}</div><pre>${escapeHTML(JSON.stringify(tool.parameters || {}, null, 2))}</pre>${useError ? `<p class="form-note neutral">${escapeHTML(useError)}</p>` : ""}<button class="ghost" type="button" data-use-direct-tool="${escapeHTML(name)}" ${useError ? "disabled" : ""}>ใช้ในแชท</button></div></details>`;
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
  const comfyServer = servers.find(server => server.name === "Comfy Local" && server.transport_kind === "stdio");
  const untrusted = servers.filter(server => !server.trust_annotations).length;
  const metrics = [
    [Number(summary.total || 0).toLocaleString(), "indexed capabilities"],
    [Number(summary.by_readiness?.ready || 0).toLocaleString(), "ready"],
    [servers.length.toLocaleString(), "connections"],
    [untrusted.toLocaleString(), "approval by default"]
  ];
  const hero = `<section class="capability-hero mcp-hero"><div class="capability-hero-head"><div><p class="eyebrow">เครื่องมือและการเชื่อมต่อ</p><h3>MCP server</h3><p>เชื่อมเซิร์ฟเวอร์เครื่องมือ ค้นหาความสามารถที่พร้อมใช้ และตรวจสถานะจากข้อมูลจริงของเครื่องนี้</p></div><div class="capability-hero-metrics">${metrics.map(([value, label]) => `<div class="capability-metric"><strong>${escapeHTML(value)}</strong><span>${escapeHTML(label)}</span></div>`).join("")}</div></div></section>`;
  const comfyCard = `<section class="panel comfy-connect"><div class="provider-head"><div><p class="eyebrow">สร้างวิดีโอ</p><h3>Comfy ในเครื่อง</h3><p>เชื่อมตัว Comfy MCP ที่ติดตั้งในเครื่อง แล้วตรวจว่า ComfyUI เปิดอยู่และมีเครื่องมือพร้อม</p></div>${pill(comfyServer?.status === "ready" ? "เชื่อม MCP แล้ว" : "ยังไม่เชื่อม", comfyServer?.status === "ready" ? "green" : "amber")}</div><form id="comfyConnectForm"><label>ตำแหน่งไฟล์ comfy-mcp.exe<input name="executable" required value="${escapeHTML(state.comfyMCPPath || comfyServer?.executable || "")}" placeholder="เช่น D:\\ComfyUI\\.venv\\Scripts\\comfy-mcp.exe"></label><button class="ghost" type="submit">เชื่อม Comfy MCP</button></form><div class="action-row"><button class="ghost" id="comfyCheckReadiness" ${comfyServer?.status === "ready" ? "" : "disabled"}>ตรวจ ComfyUI</button></div>${state.comfyReadiness ? `<p role="status">${state.comfyReadiness.server_running ? "ComfyUI เปิดอยู่" : escapeHTML(state.comfyReadiness.reason || "ComfyUI ยังไม่พร้อม")} · ${Number(state.comfyReadiness.tool_count || 0)} เครื่องมือ</p>` : ""}${state.comfyError ? `<p class="task-feedback" role="alert">${escapeHTML(state.comfyError)}</p>` : ""}<p class="form-note neutral">การเชื่อม MCP ยังไม่เริ่มสร้างวิดีโอ ต้องตรวจ workflow และโมเดลของงานจริงก่อน</p></section>`;
  const social = state.socialConnections || [];
  const socialCard = `<section class="panel social-connect"><div class="provider-head"><div><p class="eyebrow">ปลายทางคอนเทนต์</p><h3>Facebook Page และ TikTok</h3><p>ตรวจตัวตนบัญชีก่อนใช้เป็นปลายทางของงาน</p></div>${pill(`${social.filter(item => item.status === "ready" && item.credential_stored).length} บัญชีที่ตรวจแล้ว`, social.length ? "blue" : "amber")}</div>
    <div class="card-list">${social.map(item => `<article class="provider-card"><div class="provider-head"><div><strong>${escapeHTML(item.platform === "facebook" ? "Facebook Page" : "TikTok")} · ${escapeHTML(item.display_name)}</strong><p>${escapeHTML(item.account_id)} · ${item.platform === "facebook" ? "ยืนยันตัวตน Page; สิทธิ์โพสต์ตรวจเมื่อส่ง" : item.capability === "video.upload_identity" ? "ยืนยันตัวตนสำหรับส่งร่าง; สิทธิ์ video.upload ตรวจเมื่อส่ง" : "ตรวจ creator info ของ video.publish"}</p></div>${pill(item.status === "ready" && item.credential_stored ? "ตรวจบัญชีแล้ว" : "ต้องเชื่อมใหม่", item.status === "ready" && item.credential_stored ? "green" : "amber")}</div><div class="action-row"><button class="ghost" type="button" data-social-check="${escapeHTML(item.id)}">ตรวจอีกครั้ง</button><button class="danger" type="button" data-social-disconnect="${escapeHTML(item.id)}">ตัดการเชื่อมต่อ</button></div><small>ตรวจล่าสุด ${formatDate(item.checked_at)}</small></article>`).join("") || `<p class="form-note neutral">ยังไม่มีบัญชีที่ตรวจผ่าน API</p>`}</div>
    <form id="socialConnectForm"><label>แพลตฟอร์ม<select name="platform"><option value="facebook">Facebook Page</option><option value="tiktok">TikTok</option></select></label><label id="socialModeLabel" hidden>เส้นทาง TikTok<select name="mode"><option value="direct">โพสต์ตรง · video.publish</option><option value="draft">ส่งร่างให้จบใน TikTok · video.upload</option></select></label><label>Access token จากแอปนักพัฒนา<input name="token" type="password" required autocomplete="off" spellcheck="false" maxlength="16384" placeholder="วางโทเคนที่มีสิทธิ์ของบัญชีนี้"></label><button class="ghost" type="submit">ตรวจและเชื่อมบัญชี</button></form><p class="form-note neutral">การเข้าสู่ระบบใน browser อย่างเดียวยังไม่ใช่สิทธิ์ API สำหรับเผยแพร่ การเชื่อมบัญชีนี้ยังไม่อัปโหลดหรือโพสต์</p></section>`;
  const selected = state.selectedCapability ? `<article class="provider-card capability-detail"><div class="provider-head"><div><div class="row-title"><h3>${escapeHTML(capabilityDisplayTitle(state.selectedCapability))}</h3>${pill(state.selectedCapability.effect, state.selectedCapability.requires_approval ? "amber" : "green")}${pill(state.selectedCapability.readiness, state.selectedCapability.readiness === "ready" ? "green" : "red")}</div><p>${escapeHTML(state.selectedCapability.description || "No description supplied by MCP server")}</p></div>${pill(state.selectedCapability.source)}</div><div class="kv"><span>Capability ID</span><code>${escapeHTML(state.selectedCapability.id)}</code><span>Revision</span><code>${escapeHTML(state.selectedCapability.revision)}</code><span>Source ref</span><code>${escapeHTML(state.selectedCapability.source_ref)}</code><span>Approval</span><strong>${state.selectedCapability.requires_approval ? "required" : "not required"}</strong></div><section class="inspect-section"><h3>Exact input schema</h3><pre>${escapeHTML(JSON.stringify(state.selectedCapability.input_schema, null, 2))}</pre></section><p class="form-note neutral">This schema is loaded on demand. It is not part of the direct model prompt until tool_describe is called.</p><div class="action-row"><button class="primary" type="button" data-use-capability="${escapeHTML(state.selectedCapability.id)}">Use in chat</button></div></article>` : `<div class="probe-empty">Search the catalog, then open a result to read its exact revision and schema.</div>`;
  const search = `<div class="panel mcp-capability-panel"><div class="provider-head"><div><p class="eyebrow">รายการเครื่องมือ</p><h3>ค้นหาความสามารถที่เซิร์ฟเวอร์เผยแพร่</h3></div>${pill(`${summary.by_readiness?.ready || 0} ready`, "green")}</div>
      <form id="capabilitySearchForm" class="capability-search"><label class="sr-only" for="capabilityQuery">ค้นหาเครื่องมือ</label><input id="capabilityQuery" required value="${escapeHTML(state.mcpCapabilityQuery)}" placeholder="เช่น calendar, repository search, database…"><button class="ghost">ค้นหา</button></form>
      <div id="capabilityResults">${state.capabilityResults.length ? state.capabilityResults.map(item => `<button class="capability-result" data-capability-id="${escapeHTML(item.id)}"><span><strong>${escapeHTML(capabilityDisplayTitle(item))}</strong><small>${escapeHTML(item.description || "No description")}</small></span><span>${pill(capabilityKind(item), "blue")}${pill(item.effect, item.requires_approval ? "amber" : "green")}${pill(item.readiness, item.readiness === "ready" ? "green" : "red")}</span></button>`).join("") : `<div class="probe-empty">Search returns bounded metadata only—never the complete catalog schemas.</div>`}</div>
      ${selected}</div>`;
  const serverTools = `<div class="mcp-server-tools"><label class="mcp-server-search">${uiIcon("search")}<span class="sr-only">ค้นหา MCP server</span><input id="mcpServerQuery" type="search" value="${escapeHTML(state.mcpServerQuery)}" placeholder="ค้นหา MCP server…" autocomplete="off"></label><div class="mcp-filters" role="group" aria-label="กรองสถานะเซิร์ฟเวอร์"><button type="button" data-mcp-filter="all" aria-pressed="${state.mcpServerFilter === "all"}">ทั้งหมด</button><button type="button" data-mcp-filter="ready" aria-pressed="${state.mcpServerFilter === "ready"}">พร้อมใช้</button><button type="button" data-mcp-filter="attention" aria-pressed="${state.mcpServerFilter === "attention"}">ต้องตรวจสอบ</button></div><button type="button" class="ghost" id="mcpDiscoverAll" ${servers.some(server => server.enabled && server.credential_ready) ? "" : "disabled"}>${uiIcon("refresh")} ค้นพบทั้งหมด</button></div>`;
  const serverList = `<div class="mcp-connection-head"><h3>ติดตั้งแล้ว <small>${servers.length}</small></h3>${pill(`${servers.filter(server => server.status === "ready").length}/${servers.length} ready`, servers.length && servers.every(server => server.status === "ready") ? "green" : "amber")}</div>
    <div class="card-list mcp-server-list">${servers.length ? servers.map(server => `<article class="provider-card"><div class="provider-head"><div><div class="row-title"><h3>${escapeHTML(server.name)}</h3>${pill(server.status, server.status === "ready" ? "green" : server.status === "error" ? "red" : "amber")}${pill(server.last_protocol || server.protocol_mode, "blue")}</div><p><code>${escapeHTML(server.endpoint)}</code></p></div>${pill(server.credential_stored ? "token saved" : server.api_key_env ? "token from environment" : "no token set", server.credential_stored || server.api_key_env ? "green" : "amber")}</div><div class="kv"><span>Runs as</span><strong>${server.transport_kind === "stdio" ? "local program" : "remote URL"}</strong><span>Publishes</span><strong>${escapeHTML(describeCatalog(server.id))}</strong><span>Timeout</span><span>${server.request_timeout_ms.toLocaleString()} ms</span><span>Risk hints</span><strong>${server.trust_annotations ? "trusted by user" : "untrusted · approval default"}</strong><span>Token source</span><span>${server.credential_stored ? "saved on this machine" : server.api_key_env ? `environment · ${escapeHTML(server.api_key_env)}` : "none required"}</span><span>Discovered</span><span>${formatDate(server.last_discovered_at)}</span></div>${server.last_error ? `<ul class="findings"><li class="error">${escapeHTML(server.last_error)}</li></ul>` : ""}<div class="action-row"><button class="ghost" data-mcp-key="${escapeHTML(server.id)}">${server.credential_stored ? "Replace token" : "Set token"}</button><button class="primary" data-discover-mcp="${escapeHTML(server.id)}" ${server.enabled && server.credential_ready ? "" : "disabled"}>Discover catalog</button></div></article>`).join("") : `<div class="empty"><h3>No MCP connections yet</h3><p>Connect one in the registry panel: most published MCP servers are a program you launch, such as <code>npx -y @modelcontextprotocol/server-everything</code>. Nothing reaches the model until you run discovery.</p></div>`}</div>`;
  const flow = `<details class="panel mcp-flow"><summary>การเชื่อมต่อและการอนุมัติทำงานอย่างไร</summary><div class="tool-flow">${TOOL_FLOW.map(([title, detail], index) => `<article><b>${index + 1}</b><div><strong>${escapeHTML(title)}</strong><small>${escapeHTML(detail)}</small></div></article>`).join("")}</div></details>`;
  const setup = `<details class="panel connection-setup" ${servers.length ? "" : "open"}><summary><div><p class="eyebrow">MCP connection registry</p><h3>Connect a tool server</h3></div></summary><div class="connection-setup-body"><form id="mcpForm">
      <label>Name<input name="name" required maxlength="80" placeholder="Local knowledge tools"></label>
      <label>How does this server run?<select name="transport_kind" id="mcpTransport"><option value="stdio">A program on this machine · stdio</option><option value="streamable-http">A URL · Streamable HTTP</option></select></label>
      <label id="mcpEndpointLabel">Command that starts it<input name="endpoint" id="mcpEndpoint" required placeholder="npx -y @modelcontextprotocol/server-everything"></label>
      <details class="field-help"><summary>How does this command run?</summary><p id="mcpEndpointNote">The program runs on this machine with only PATH, HOME and its own token in its environment, and it is started directly rather than through a shell. Allowed launchers: npx, node, bun, deno, uv, uvx, python, python3, docker, go.</p></details>
      <label>Bearer token<input name="api_key" type="password" autocomplete="off" spellcheck="false" placeholder="Paste the token — leave empty if the server needs none"></label>
      <details class="field-help"><summary>Where is the token stored?</summary><p>The token is written to <code>secrets.json</code> in your data directory with owner-only permissions. It never enters the database, a backup export, a log line or any API response.</p></details>
      <label class="check-label"><input name="trust_annotations" type="checkbox"> Trust this server's risk annotations</label>
      <details class="field-help"><summary>What does trusting annotations change?</summary><p>By default, annotations are untrusted and every remote call requires your approval.</p></details>
      <details class="advanced-fields"><summary>Advanced</summary><div class="advanced-fields-body">
        <div class="form-grid"><label>Protocol<select name="protocol_mode"><option value="auto">Auto · current then legacy</option><option value="2026-07-28">2026-07-28 · stateless</option><option value="2025-11-25">2025-11-25 · session</option></select></label><label>Timeout ms<input name="request_timeout_ms" type="number" min="1000" max="120000" value="15000" required></label></div>
        <label>Read the token from an environment variable instead<input name="api_key_env" pattern="[A-Z][A-Z0-9_]{1,126}" placeholder="HERMETRIX_MCP_API_KEY"></label>
      </div></details>
      <button class="primary">Connect server</button>
    </form></div></details>`;
  root.innerHTML = `${hero}${comfyCard}${socialCard}${serverTools}${serverList}<p class="probe-empty" id="mcpServerEmpty" hidden>ไม่พบเซิร์ฟเวอร์ที่ตรงกับคำค้นหรือสถานะที่เลือก</p>${search}<div class="mcp-setup-row">${setup}${flow}</div>`;
  const selectedUseButton = root.querySelector("[data-use-capability]");
  const selectedUseError = state.selectedCapability?.readiness !== "ready"
    ? "เครื่องมือ MCP นี้ยังไม่พร้อม ตรวจการเชื่อมต่อและค้นพบอีกครั้ง"
    : capabilityUseReadiness("mcp");
  if (selectedUseButton && selectedUseError) {
    selectedUseButton.disabled = true;
    const note = document.createElement("p");
    note.className = "form-note neutral";
    note.textContent = selectedUseError;
    selectedUseButton.closest(".action-row").before(note);
  }
  if (!servers.length) root.querySelector(".mcp-hero")?.after(root.querySelector(".mcp-setup-row"));
  $$(".mcp-server-list > article", root).forEach((card, index) => {
    const server = servers[index];
    if (server.transport_kind === "streamable-http" && server.endpoint.startsWith("https://")) {
      const button = document.createElement("button");
      button.type = "button";
      button.className = "ghost";
      button.dataset.mcpAccess = server.id;
      button.textContent = server.access_stored ? "Replace Cloudflare Access token" : "Set Cloudflare Access token";
      card.querySelector(".action-row").append(button);
    }
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
  $("#comfyConnectForm")?.addEventListener("input", event => { state.comfyMCPPath = event.currentTarget.elements.executable.value; });
  $("#comfyConnectForm")?.addEventListener("submit", connectComfyMCP);
  $("#comfyCheckReadiness")?.addEventListener("click", checkComfyReadiness);
  $("#socialConnectForm")?.addEventListener("submit", connectSocialAccount);
  $("#socialConnectForm [name=platform]")?.addEventListener("change", event => { $("#socialModeLabel").hidden = event.target.value !== "tiktok"; });
  $$('[data-social-check]', root).forEach(button => button.addEventListener("click", () => checkSocialAccount(button.dataset.socialCheck)));
  $$('[data-social-disconnect]', root).forEach(button => button.addEventListener("click", () => disconnectSocialAccount(button.dataset.socialDisconnect)));
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
  $$('[data-mcp-access]', root).forEach(button => button.addEventListener("click", () => setMCPAccessCredential(button.dataset.mcpAccess)));
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

function setMCPAccessCredential(id) {
  const server = state.mcp_servers.find(item => item.id === id);
  if (!server) return;
  const dialog = $("#mcpAccessDialog");
  const form = $("#mcpAccessForm");
  form.reset();
  $("#mcpAccessTitle").textContent = `Cloudflare Access for ${server.name}`;
  $("#mcpAccessCancel").onclick = () => dialog.close();
  $("#mcpAccessClose").onclick = () => dialog.close();
  form.onsubmit = async event => {
    event.preventDefault();
    const clientID = $("#mcpAccessClientID").value.trim();
    const clientSecret = $("#mcpAccessClientSecret").value.trim();
    if (Boolean(clientID) !== Boolean(clientSecret)) {
      toast("Enter both Cloudflare Access fields, or leave both blank to remove them", true);
      return;
    }
    const submit = form.querySelector('button[type="submit"]');
    submit.disabled = true;
    try {
      await api(`/api/mcp/servers/${encodeURIComponent(id)}/access-credential`, { method:"PUT", body:JSON.stringify({client_id:clientID, client_secret:clientSecret}) });
      form.reset();
      dialog.close();
      toast(clientID ? "Cloudflare Access token saved on this machine" : "Cloudflare Access token removed");
      await load();
      switchTab("mcp");
    } catch (error) { toast(error.message, true); }
    finally { submit.disabled = false; }
  };
  dialog.showModal();
  $("#mcpAccessClientID").focus();
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

async function connectComfyMCP(event) {
  event.preventDefault();
  const executable = String(new FormData(event.currentTarget).get("executable") || "").trim();
  state.comfyMCPPath = executable;
  const approved = await askAction({title:"เชื่อม Comfy MCP ในเครื่อง?", message:`Hermetrix จะเปิดโปรแกรมนี้เพื่อค้นหาเครื่องมือ: ${executable}`, confirmLabel:"เชื่อมและค้นหา"});
  if (!approved) return;
  const button = event.currentTarget.querySelector('button[type="submit"]');
  if (button) button.disabled = true;
  state.comfyError = "";
  try {
    const result = await api("/api/comfy/connect", {method:"POST", body:JSON.stringify({executable})});
    toast(`เชื่อม Comfy MCP แล้ว · ${result.discovery.tools} เครื่องมือ`);
    await load();
    switchTab("mcp");
    await checkComfyReadiness();
  } catch (error) { state.comfyError = error.message; toast(error.message, true); renderMCP(); }
  finally { if (button?.isConnected) button.disabled = false; }
}

async function checkComfyReadiness() {
  state.comfyError = "";
  try { state.comfyReadiness = await api("/api/comfy/readiness"); }
  catch (error) { state.comfyError = error.message; }
  renderMCP();
}

async function refreshSocialAccounts() {
  state.socialConnections = await api("/api/social/connections");
  renderMCP();
}

async function connectSocialAccount(event) {
  event.preventDefault();
  const form = event.currentTarget;
  const values = new FormData(form);
  const platform = String(values.get("platform") || "");
  const mode = platform === "tiktok" ? String(values.get("mode") || "direct") : "";
  const token = String(values.get("token") || "").trim();
  if (!token) return;
  const confirmed = await askAction({title:"ตรวจและเชื่อมบัญชี?", message:`Hermetrix จะส่งโทเคนไปตรวจตัวตนกับ ${platform === "facebook" ? "Facebook" : "TikTok"} และเก็บไว้ในที่เก็บข้อมูลลับของเครื่อง`, confirmLabel:"ตรวจบัญชี"});
  if (!confirmed) return;
  const button = form.querySelector('button[type="submit"]');
  if (button) button.disabled = true;
  try {
    const account = await api("/api/social/connections", {method:"POST", body:JSON.stringify({platform,mode,token})});
    form.reset();
    toast(`เชื่อม ${account.display_name} แล้ว`);
    await refreshSocialAccounts();
  } catch (error) { toast(error.message,true); }
  finally { form.elements.token.value = ""; if (button?.isConnected) button.disabled = false; }
}

async function checkSocialAccount(id) {
  try {
    const account = await api(`/api/social/connections/${encodeURIComponent(id)}/check`, {method:"POST",body:"{}"});
    toast(`ตรวจ ${account.display_name} แล้ว`);
  } catch (error) { toast(error.message,true); }
  finally { try { await refreshSocialAccounts(); } catch (error) { toast(error.message,true); } }
}

async function disconnectSocialAccount(id) {
  const account = (state.socialConnections || []).find(item => item.id === id);
  if (!account) return;
  const confirmed = await askAction({title:"ตัดการเชื่อมต่อบัญชี?", message:`ลบโทเคนที่เก็บไว้ของ ${account.display_name} จากเครื่องนี้`, confirmLabel:"ตัดการเชื่อมต่อ"});
  if (!confirmed) return;
  try {
    await api(`/api/social/connections/${encodeURIComponent(id)}`, {method:"DELETE"});
    toast(`ตัดการเชื่อมต่อ ${account.display_name} แล้ว`);
    await refreshSocialAccounts();
  } catch (error) { toast(error.message,true); }
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
  // A probe or compilation redraws this panel. Keep the user's inputs and the
  // selected profile paired with the result that was just calculated.
  const priorInputs = Object.fromEntries(["runtimeSelect", "modelInput", "endpointInput", "profileSelect", "contextSample", "burstInput"]
    .map(id => [id, root.querySelector?.(`#${id}`)?.value]));
  const profiles = state.profiles.map(profile => {
    const inputCeiling = profile.total - profile.output_reserve - profile.uncertainty_reserve;
    return `<option value="${profile.name}" ${profile.name === "compact-32k" ? "selected" : ""}>${profileLabel(profile)} · รับข้อมูลได้ไม่เกิน ${inputCeiling.toLocaleString()} โทเคนก่อนหักคำสั่งและเครื่องมือ</option>`;
  }).join("");
  root.innerHTML = `<div class="context-grid"><div class="panel-stack"><div class="panel"><p class="eyebrow">ตรวจความจุจริง</p><h3>ตรวจบริบทของโมเดลในเครื่อง</h3><div class="form-grid model-grid"><label>ระบบที่รันโมเดล<select id="runtimeSelect"><option value="ollama">Ollama</option><option value="lmstudio">LM Studio</option><option value="vllm">vLLM</option><option value="llamacpp">llama.cpp</option></select></label><label>ชื่อโมเดล<input id="modelInput" placeholder="qwen3-coder:30b" required></label></div><label>ที่อยู่โมเดลในเครื่อง<input id="endpointInput" value="http://127.0.0.1:11434" spellcheck="false"></label><button class="ghost" id="probeButton">ตรวจความจุที่จัดสรร</button><div id="probeResult">${renderProbeResult(state.modelProbe)}</div><p class="form-note neutral">ผลนี้ยืนยันเฉพาะขนาดบริบทที่ระบบจัดสรร ยังไม่ยืนยันการเรียกเครื่องมือ รูปแบบคำตอบ หรือคุณภาพของงาน</p></div><div class="panel"><p class="eyebrow">ทดลองจัดบริบท</p><h3>ดูว่าระบบเลือกข้อมูลใดส่งให้โมเดล</h3><label>ขนาดบริบท<select id="profileSelect">${profiles}</select></label><label>ตัวอย่างบทสนทนา<textarea id="contextSample" rows="12">ผู้ใช้ต้องการสร้างระบบ Skill lifecycle ที่ทุกการเปลี่ยนแปลงตรวจสอบและย้อนกลับได้\n\nWe inspected the repository, ran tests, and recorded tool receipts. Preserve the exact goal, acceptance criteria, current decisions, and unresolved failures while compressing older narrative.</textarea></label><label>พื้นที่เผื่อการเรียกเครื่องมือครั้งถัดไป<input id="burstInput" type="number" min="0" max="8192" value="1024"></label><button class="primary" id="compileButton">ทดลองและดูผล</button></div></div><div class="panel" id="contextResult">${state.contextResult ? renderContextResult(state.contextResult) : `<div class="empty"><h3>ยังไม่มีผลทดลอง</h3><p>เลือกขนาดบริบทแล้วกดทดลอง ระบบจะแจ้งข้อผิดพลาดหากข้อมูลสำคัญหรือเครื่องมือเกินพื้นที่ที่กันไว้</p></div>`}</div></div>`;
  for (const [id, value] of Object.entries(priorInputs)) {
    if (value !== undefined) root.querySelector(`#${id}`).value = value;
  }
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
  if (!result) return `<div class="probe-empty">ยังไม่ได้ตรวจความจุของโมเดลในเครื่อง</div>`;
  const tone = result.mode === "certified-context" ? "green" : result.mode === "compact-context" ? "amber" : "red";
  const warnings = (result.warnings || []).map(item => `<li>${escapeHTML(item)}</li>`).join("");
  return `<div class="probe-result"><div class="probe-title">${pill(result.mode, tone)}<strong>${result.verified ? "ตรวจจากระบบที่รันจริงแล้ว" : "มีเพียงข้อมูลจากการตั้งค่า"}</strong></div><div class="kv"><span>จัดสรรจริง</span><strong>${result.allocated_context ? result.allocated_context.toLocaleString() : "ยังไม่ยืนยัน"}</strong><span>ตั้งค่าไว้</span><strong>${result.configured_context ? result.configured_context.toLocaleString() : "ไม่มีข้อมูล"}</strong><span>โมเดลรองรับสูงสุด</span><strong>${result.training_context ? result.training_context.toLocaleString() : "ไม่มีข้อมูล"}</strong><span>ที่มา</span><span class="hash">${escapeHTML(result.context_source)}</span></div>${warnings ? `<ul class="findings">${warnings}</ul>` : ""}</div>`;
}

async function probeLocalModel() {
  const model = $("#modelInput").value.trim();
  if (!model) { toast("ใส่ชื่อโมเดลที่กำลังรันให้ตรงกับระบบ", true); return; }
  try {
    state.modelProbe = await api("/api/local-model/probe", { method:"POST", body:JSON.stringify({ runtime:$("#runtimeSelect").value, endpoint:$("#endpointInput").value.trim(), model }) });
    renderContext();
    toast(state.modelProbe.mode === "certified-context" ? "ตรวจพบพื้นที่บริบทที่จัดสรรอย่างน้อย 64k" : "ตรวจระบบแล้ว อาจต้องเลือกบริบทขนาดเล็กลง");
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
  return `<h3>${escapeHTML(report.profile)}</h3><div class="kv"><span>ข้อมูลเข้าที่คาด</span><strong>${report.predicted_input.toLocaleString()}</strong><span>ข้อมูลเดิม</span><strong>${report.original_tokens.toLocaleString()}</strong><span>ที่เลือก</span><strong>${report.selected_tokens.toLocaleString()}</strong><span>ที่ตัดออก</span><strong>${report.dropped_tokens.toLocaleString()}</strong><span>ที่สรุปไว้</span><strong>${report.compacted_tokens.toLocaleString()}</strong><span>พื้นที่คำตอบ</span><strong>${report.output_reserve.toLocaleString()}</strong><span>พื้นที่คงเหลือ</span><strong>${report.free.toLocaleString()}</strong><span>สัดส่วนข้อมูลที่เก็บ</span><strong>${(report.compression_ratio*100).toFixed(1)}%</strong><span>ข้อมูลสำคัญที่เก็บ</span><strong>${(report.integrity.essential_retention*100).toFixed(0)}%</strong><span>คู่เหตุการณ์</span><strong>${report.integrity.causal_pairs_selected} ใช้ตรง · ${report.integrity.causal_pairs_compacted} สรุป · ${report.integrity.causal_pairs_omitted} ไม่ใช้</strong><span>ข้อมูลที่แยกเก็บ</span><strong>${report.spilled.length}</strong></div>${contextSelectionHTML({report}, true)}<section class="inspect-section"><h3>พื้นที่แต่ละส่วน</h3>${bars}</section><section class="inspect-section"><h3>ตัวอย่างข้อมูลที่เลือก</h3><pre>${escapeHTML(preview)}</pre></section>`;
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
    toast("จัดบริบทได้ภายในพื้นที่ที่กันไว้");
  } catch (error) { toast(error.message, true); }
}

/* --- Folder picker ---------------------------------------------------------
   A browser will not tell a web page where a folder is: a file input reports
   names, never paths. So the walk happens on the server and this renders what
   it answers. Typing a path by hand still works; this is the way that does not
   require knowing it. */

let folderPickerMode = "project-form";
let folderNameEdited = false;
function folderProjectName(path) {
  const part = String(path).split(/[\\/]/).filter(Boolean).pop() || "Workspace";
  return /^[A-Za-z]:$/.test(part) ? `Drive ${part[0].toUpperCase()}` : part;
}

async function openFolderPicker(startPath = "", mode = "project-form") {
  const dialog = $("#folderDialog");
  if (!dialog) return;
  folderPickerMode = mode;
  dialog.dataset.mode = mode;
  folderNameEdited = false;
  $("#folderProjectName").value = "";
  $("#folderProjectNameLabel").hidden = mode !== "picker";
  $("#folderTitle").textContent = mode === "picker" ? "เพิ่ม Workspace จากโฟลเดอร์" : "เลือกโฟลเดอร์";
  $("#folderChoose").textContent = mode === "picker" ? "เพิ่มและเปิด Workspace" : "เลือกโฟลเดอร์นี้";
  $("#folderError").hidden = true;
  if (!dialog.open) dialog.showModal();
  await loadFolder(startPath);
}

async function loadFolder(path) {
  const body = $("#folderBody");
  if (!body) return;
  state.folderListing = null;
  $("#folderChoose").disabled = true;
  $("#folderFilter").value = "";
  $("#folderListCount").textContent = "";
  $("#folderAddress").value = path || "";
  body.setAttribute("aria-busy", "true");
  body.innerHTML = `<p class="command-empty">กำลังอ่านโฟลเดอร์…</p>`;
  try {
    state.folderListing = await api(`/api/filesystem/directories?path=${encodeURIComponent(path || "")}`);
  } catch (error) {
    body.innerHTML = `<p class="command-empty">${escapeHTML(error.message)}</p>`;
    body.removeAttribute("aria-busy");
    return;
  }
  renderFolderPicker();
}

function filterFolderRows() {
  const listing = state.folderListing;
  if (!listing) return;
  const query = $("#folderFilter").value.trim().toLocaleLowerCase();
  let shown = 0;
  $$('[data-folder-entry]', $("#folderBody")).forEach(button => {
    const matches = button.lastElementChild.textContent.toLocaleLowerCase().includes(query);
    button.hidden = !matches;
    if (matches) shown++;
  });
  $("#folderFilterEmpty").hidden = shown !== 0 || listing.entries.length === 0;
  $("#folderListCount").textContent = `${shown}${query ? ` / ${listing.entries.length}` : ""} โฟลเดอร์`;
}

function renderFolderPicker() {
  const listing = state.folderListing;
  const body = $("#folderBody");
  if (!listing || !body) return;
  $("#folderPath").textContent = listing.path;
  $("#folderPath").title = listing.path;
  $("#folderAddress").value = listing.path;
  $("#folderChoose").disabled = listing.unreadable;
  if (folderPickerMode === "picker" && !folderNameEdited) $("#folderProjectName").value = folderProjectName(listing.path);
  const roots = $("#folderRoots");
  roots.innerHTML = (listing.roots || []).map(root =>
    `<button type="button" class="ghost ${root.path === listing.path ? "active" : ""}" data-folder-root="${escapeHTML(root.path)}">${escapeHTML(root.name)}</button>`).join("");
  $$("[data-folder-root]", roots).forEach(button => button.addEventListener("click", () => loadFolder(button.dataset.folderRoot)));
  const rows = listing.entries.map(entry =>
    `<button type="button" class="folder-row" data-folder-entry data-folder="${escapeHTML(entry.path)}"><span>${uiIcon("files")}</span><span>${escapeHTML(entry.name)}</span></button>`).join("");
  body.innerHTML = `${listing.parent ? `<button type="button" class="folder-row up" data-folder="${escapeHTML(listing.parent)}"><span>↑</span><span>ขึ้นหนึ่งระดับ</span></button>` : ""}
    ${listing.unreadable ? `<p class="command-empty">อ่านโฟลเดอร์นี้ไม่ได้ ลองขึ้นหนึ่งระดับหรือเลือกตำแหน่งอื่น</p>`
      : rows || `<p class="command-empty">ไม่มีโฟลเดอร์ย่อย สามารถเลือกโฟลเดอร์นี้ได้</p>`}
    <p class="command-empty" id="folderFilterEmpty" hidden>ไม่พบโฟลเดอร์ที่ตรงกับคำค้น</p>
    ${listing.truncated ? `<p class="command-empty">แสดง 500 โฟลเดอร์แรก หากไม่พบให้พิมพ์ path ด้านบน</p>` : ""}`;
  body.removeAttribute("aria-busy");
  $$("[data-folder]", body).forEach(button => button.addEventListener("click", async () => { await loadFolder(button.dataset.folder); body.focus(); }));
  filterFolderRows();
}

function bindFolderPicker() {
  $("#folderClose")?.addEventListener("click", () => $("#folderDialog").close());
  $("#folderHome")?.addEventListener("click", () => loadFolder(state.folderListing?.home || ""));
  $("#folderAddressForm")?.addEventListener("submit", event => { event.preventDefault(); loadFolder($("#folderAddress").value.trim()); });
  $("#folderFilter")?.addEventListener("input", filterFolderRows);
  $("#folderProjectName")?.addEventListener("input", () => { folderNameEdited = true; });
  $("#folderChoose")?.addEventListener("click", async () => {
    const chosen = state.folderListing?.path;
    if (!chosen) return;
    if (folderPickerMode === "picker") {
      const name = $("#folderProjectName").value.trim();
      const error = $("#folderError");
      if (!name) { error.textContent = "กรุณาตั้งชื่อ Workspace"; error.hidden = false; return; }
      const existing = state.projects.find(project => project.root_path?.replaceAll("/", "\\").toLowerCase() === chosen.replaceAll("/", "\\").toLowerCase());
      const button = $("#folderChoose");
      button.disabled = true;
      button.textContent = existing ? "กำลังเปิด Workspace…" : "กำลังเพิ่ม Workspace…";
      error.hidden = true;
      try {
        const project = existing || await api("/api/projects", { method:"POST", body:JSON.stringify({ name, root_path:chosen }) });
        $("#folderDialog").close();
        await openProject(project.id, { targetView: pickerTargetView });
      } catch (failure) {
        error.textContent = failure.message;
        error.hidden = false;
      } finally {
        button.textContent = "เพิ่มและเปิด Workspace";
        button.disabled = !state.folderListing || state.folderListing.unreadable;
      }
      return;
    }
    $("#folderDialog").close();
    const field = $("#projectRoot");
    if (!field) return;
    field.value = chosen;
    // Name the project after the folder unless the user already named it.
    const name = $('#projectForm input[name="name"]');
    if (name && !name.value.trim()) name.value = chosen.split(/[\\/]/).filter(Boolean).pop() || "";
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
  const registry = root.querySelector(".workbench-grid > .panel");
  const addHeading = registry?.querySelector("h3");
  const addForm = $("#projectForm", root);
  if (addHeading && addForm) {
    const addDetails = document.createElement("details");
    addDetails.className = "project-add-details";
    addDetails.innerHTML = '<summary>+ เพิ่มโปรเจกต์จากโฟลเดอร์</summary>';
    addHeading.replaceWith(addDetails);
    addDetails.append(addForm);
  }
  if (registry && state.archivedProjects?.length) {
    registry.insertAdjacentHTML("beforeend", `<details class="inspect-section"><summary>โปรเจกต์ที่นำออก (${state.archivedProjects.length})</summary><div class="project-list">${state.archivedProjects.map(item => `<div class="project-archived-row"><span>${escapeHTML(item.name)}</span><button class="ghost" data-restore-project="${escapeHTML(item.id)}">กู้คืน</button></div>`).join("")}</div></details>`);
  }
  const workbench = root.querySelector(".workbench-grid > .panel:nth-child(2)");
  if (project && workbench) {
    const header = workbench.querySelector(".provider-head");
    header.insertAdjacentHTML("afterend", `<div class="action-row project-manage-actions"><button class="primary" id="projectOpenWorkspace">เปิด Workspace</button><button class="ghost" id="projectRename">เปลี่ยนชื่อ</button><button class="ghost" id="projectArchive">นำออกจากรายการ</button></div>`);
    const command = $("#commandForm", root)?.closest("section.inspect-section");
    if (command) {
      const advanced = document.createElement("details");
      advanced.className = "inspect-section project-advanced";
      advanced.innerHTML = '<summary>ขั้นสูง · รันคำสั่งเบื้องหลัง</summary>';
      command.replaceWith(advanced);
      advanced.append(command);
    }
  }
  $("#projectForm")?.addEventListener("submit", createProject);
  $("#browseRoot")?.addEventListener("click", () => openFolderPicker($("#projectRoot")?.value || ""));
  $$('[data-project-id]', root).forEach(button => button.addEventListener("click", () => selectProject(button.dataset.projectId, "")));
  $$('[data-file-path]', root).forEach(button => button.addEventListener("click", () => button.dataset.directory === "true" ? selectProject(state.selectedProject, button.dataset.filePath) : (activateWorkbenchChrome("files"), openWorkbenchFile(button.dataset.filePath))));
  $("#projectUpButton")?.addEventListener("click", () => selectProject(state.selectedProject, (state.projectPath || "").split("/").slice(0,-1).join("/")));
  $("#commandForm")?.addEventListener("submit", startCommand);
  $("#projectOpenWorkspace")?.addEventListener("click", async () => { if (await openProject(project.id)) { switchTab("chat"); switchView("code"); } });
  $("#projectRename")?.addEventListener("click", () => renameProject(project));
  $("#projectArchive")?.addEventListener("click", () => archiveProjectFromUI(project));
  $$('[data-restore-project]', root).forEach(button => button.addEventListener("click", () => restoreProjectFromUI(button.dataset.restoreProject)));
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

let officeFilter = "overview";
let officeKind = "all";
let officeQuery = "";
let officeHistoryLimit = 20;
let officePollTimer = null;
let officePollGeneration = 0;
let officePollFailed = false;
const officeExpanded = new Set();

function officeItems() {
  const commands = state.jobs.map(job => ({kind:"command", key:`command:${job.id}`, item:job, created:job.created_at}));
  const reviews = state.reviews.map(job => ({kind:"review", key:`review:${job.id}`, item:job, created:job.created_at}));
  return [...commands, ...reviews].sort((a, b) => String(b.created || "").localeCompare(String(a.created || "")));
}

function officeCategory(entry) {
  if (["queued", "running"].includes(entry.item.state)) return "active";
  if (["failed", "interrupted"].includes(entry.item.state)) return "attention";
  return "history";
}

function officeState(entry) {
  const state = entry.item.state;
  const command = entry.kind === "command";
  const labels = {queued:"รอเริ่ม", running:"กำลังทำ", completed:"เสร็จแล้ว", failed:"ล้มเหลว", interrupted:"หยุดกลางทาง", canceled:"ยกเลิก"};
  const next = command
    ? {queued:"กำลังรอเริ่มในแอปนี้", running:"กำลังรันบนเครื่องนี้ · จะบันทึกผลเมื่อจบ", completed:"เสร็จแล้ว · เปิดผลลัพธ์ด้านล่าง", failed:"ตรวจข้อผิดพลาดและผลลัพธ์ก่อนเริ่มงานใหม่", interrupted:"แอปหยุดก่อนจบ · จะไม่รันคำสั่งนี้ซ้ำเอง", canceled:"หยุดตามคำขอแล้ว"}
    : {queued:"อยู่ในคิว · แอปจะตรวจอัตโนมัติประมาณทุก 1 นาที", running:"กำลังตรวจ · หากแอปหยุดจะกลับเข้าคิว", completed:"ตรวจเสร็จแล้ว · ดูผลหรือข้อเสนอที่สร้าง", failed:"ตรวจไม่สำเร็จ · ดูข้อผิดพลาด งานนี้ไม่เริ่มซ้ำเอง"};
  return {label:labels[state] || state, next:next[state] || "ตรวจรายละเอียดงาน", tone:state === "completed" ? "green" : ["failed", "interrupted"].includes(state) ? "red" : "amber"};
}

function officeEntryHTML(entry) {
  const item = entry.item;
  const command = entry.kind === "command";
  const status = officeState(entry);
  const args = Array.isArray(item.payload?.arguments) ? item.payload.arguments : [];
  const name = command ? [item.payload?.executable || item.kind, ...args].join(" ") : item.digest?.goal_and_constraints || item.trigger_kind || "งานตรวจทาน";
  const project = state.projects.find(project => project.id === item.payload?.project_id);
  const context = command ? `${project?.name || item.payload?.working_dir || "โปรเจกต์"} · ${item.payload?.actor || "ผู้ใช้"}` : `รีวิวจาก ${item.trigger_kind || "เหตุการณ์"}`;
  const output = String(item.result?.output || "");
  const artifact = item.result?.artifact_id;
  return `<details class="office-entry office-entry-${officeCategory(entry)}" data-office-key="${escapeHTML(entry.key)}" ${officeExpanded.has(entry.key) ? "open" : ""}>
    <summary><span class="office-entry-icon" aria-hidden="true">${uiIcon(command ? "terminal" : "learning")}</span><span class="office-entry-copy"><strong>${escapeHTML(name)}</strong><small>${escapeHTML(context)}</small><span class="office-entry-next">${escapeHTML(status.next)}</span></span><span class="office-entry-meta">${pill(status.label,status.tone)}<small>${formatDate(item.started_at || item.created_at)}</small></span></summary>
    <div class="office-entry-detail"><div class="office-timeline"><div><span>สร้าง</span><strong>${formatDate(item.created_at)}</strong></div><div><span>เริ่ม</span><strong>${formatDate(item.started_at)}</strong></div><div><span>จบ</span><strong>${formatDate(item.completed_at)}</strong></div></div>
      <div class="office-facts"><div><span>${command ? "โปรเจกต์" : "เซสชัน"}</span><strong>${escapeHTML(command ? project?.name || item.payload?.project_id || "—" : item.session_id || "—")}</strong></div><div><span>${command ? "ผู้สั่ง" : "ผู้ตรวจ"}</span><strong>${escapeHTML(command ? item.payload?.actor || "—" : item.reviewer_revision || "—")}</strong></div><div><span>${command ? "ระยะเวลา" : "จำนวนครั้ง"}</span><strong>${command ? `${Number(item.result?.duration_ms || 0).toLocaleString()} ms` : `${Number(item.attempts || 0)} ครั้ง`}</strong></div>${command ? `<div><span>ผลคำสั่ง</span><strong>${item.result?.exit_code ?? "ยังไม่มี"}</strong></div>` : ""}</div>
      ${item.error ? `<p class="office-error" role="alert">${escapeHTML(item.error)}</p>` : ""}
      ${command && output ? `<details class="office-output"><summary>ดูผลลัพธ์คำสั่ง${item.result?.truncated ? " · ถูกตัดให้สั้น" : ""}</summary><pre>${escapeHTML(output.slice(0, 4000))}</pre></details>` : ""}
      <div class="action-row">${command && artifact ? `<a class="button-link" href="/api/artifacts/${encodeURIComponent(artifact)}/content" target="_blank" rel="noreferrer">เปิดผลลัพธ์เต็ม</a>` : ""}${!command && item.candidate_id ? `<button class="ghost" data-office-proposal="${escapeHTML(item.candidate_id)}">ดูข้อเสนอสกิล</button>` : ""}${command && ["queued","running"].includes(item.state) ? `<button class="danger" data-cancel-job="${escapeHTML(item.id)}">หยุดงาน</button>` : ""}</div>
      <details class="office-technical"><summary>ข้อมูลทางเทคนิค</summary><div class="kv"><span>Job ID</span><code>${escapeHTML(item.id)}</code>${command ? `<span>Arguments</span><code>${escapeHTML(JSON.stringify(args))}</code><span>Working dir</span><code>${escapeHTML(item.payload?.working_dir || ".")}</code><span>Timeout</span><span>${Number(item.payload?.timeout_seconds || 0)} s</span>` : `<span>Reviewer</span><code>${escapeHTML(item.reviewer_revision || "—")}</code><span>Decision</span><span>${escapeHTML(item.decision?.kind || "—")}</span>`}</div></details>
    </div></details>`;
}

function renderOfficeList() {
  const root = $("#view-office");
  const list = $("#officeList", root);
  if (!list) return;
  const query = officeQuery.trim().toLowerCase();
  let entries = officeItems().filter(entry => {
    if (officeKind !== "all" && entry.kind !== officeKind) return false;
    if (!query) return true;
    const item = entry.item;
    return `${item.payload?.executable || ""} ${Array.isArray(item.payload?.arguments) ? item.payload.arguments.join(" ") : ""} ${item.digest?.goal_and_constraints || ""} ${item.trigger_kind || ""} ${state.projects.find(project => project.id === item.payload?.project_id)?.name || ""}`.toLowerCase().includes(query);
  });
  if (officeFilter === "overview") entries = entries.filter(entry => officeCategory(entry) !== "history").concat(entries.filter(entry => officeCategory(entry) === "history").slice(0, 5));
  else if (officeFilter !== "all") entries = entries.filter(entry => officeCategory(entry) === officeFilter);
  const total = entries.length;
  entries = entries.slice(0, officeFilter === "overview" ? 30 : officeHistoryLimit);
  list.innerHTML = entries.length ? entries.map(officeEntryHTML).join("") : `<div class="empty office-empty"><h3>${officeFilter === "active" || officeFilter === "overview" ? "ไม่มีงานที่กำลังทำ" : "ไม่พบงาน"}</h3><p>${officeFilter === "overview" ? "งานที่จบแล้วอยู่ในแท็บประวัติ" : "ลองเปลี่ยนตัวกรองหรือคำค้นหา"}</p></div>`;
  $("#officeCount", root).textContent = `แสดง ${entries.length} จาก ${total} งาน`;
  $("#officeMore", root).hidden = entries.length >= total;
  $$('[data-office-key]', list).forEach(details => details.addEventListener("toggle", () => {
    if (details.open) officeExpanded.add(details.dataset.officeKey); else officeExpanded.delete(details.dataset.officeKey);
  }));
  $$('[data-cancel-job]', list).forEach(button => button.addEventListener("click", () => cancelBackgroundJob(button.dataset.cancelJob)));
  $$('[data-office-proposal]', list).forEach(button => button.addEventListener("click", () => { switchTab("proposals"); inspectCandidate(button.dataset.officeProposal); }));
}

function stopOfficePoll() {
  ++officePollGeneration;
  clearTimeout(officePollTimer);
  officePollTimer = null;
}

function scheduleOfficePoll() {
  stopOfficePoll();
  if (state.activeTab !== "office" || $("#configOverlay")?.hidden) return;
  const generation = officePollGeneration;
  officePollTimer = setTimeout(async () => {
    if (generation !== officePollGeneration || state.activeTab !== "office") return;
    try {
      const [jobs, reviews] = await Promise.all([api("/api/jobs"), api("/api/reviews")]);
      if (generation !== officePollGeneration || state.activeTab !== "office") return;
      const changed = JSON.stringify([state.jobs,state.reviews]) !== JSON.stringify([jobs,reviews]);
      state.jobs = asList(jobs); state.reviews = asList(reviews);
      officePollFailed = false;
      if (changed) renderOffice();
    } catch (error) {
      if (generation !== officePollGeneration || state.activeTab !== "office") return;
      if (!officePollFailed) toast(`อัปเดตสถานะงานไม่ได้: ${error.message}`, true);
      officePollFailed = true;
    }
    scheduleOfficePoll();
  }, 5000);
}

function renderOffice() {
  const root = $("#view-office");
  if (!root) return;
  const searchFocused = document.activeElement?.id === "officeSearch";
  const searchPosition = searchFocused ? document.activeElement.selectionStart : null;
  const items = officeItems();
  const active = items.filter(entry => officeCategory(entry) === "active").length;
  const attention = items.filter(entry => officeCategory(entry) === "attention").length;
  const history = items.filter(entry => officeCategory(entry) === "history").length;
  const queuedReviews = state.reviews.filter(item => item.state === "queued").length;
  root.innerHTML = `<section class="office-hero"><div><p class="eyebrow">BACKGROUND JOBS</p><h2>งานเบื้องหลัง</h2><p>ดูว่างานใดกำลังทำ จบเมื่อไร และต้องทำอะไรต่อ · อัปเดตอัตโนมัติทุก 5 วินาที</p></div><div class="action-row"><button class="ghost" id="officeRefresh">อัปเดตสถานะ</button><button class="primary" id="officeRunReview" ${queuedReviews ? "" : "disabled"}>ตรวจคิวรีวิว (${queuedReviews})</button></div></section>
    <div class="office-summary"><button data-office-filter="active" class="${officeFilter === "active" ? "selected" : ""}"><strong>${active}</strong><span>กำลังทำ / รอเริ่ม</span></button><button data-office-filter="attention" class="${officeFilter === "attention" ? "selected" : ""}"><strong>${attention}</strong><span>ต้องตรวจสอบ</span></button><button data-office-filter="history" class="${officeFilter === "history" ? "selected" : ""}"><strong>${history}</strong><span>ประวัติ</span></button></div>
    <div class="office-policy"><p><strong>คำสั่งในโปรเจกต์</strong> หากแอปปิดระหว่างรัน งานจะขึ้น “หยุดกลางทาง” และไม่เริ่มซ้ำเอง</p><p><strong>รีวิวการเรียนรู้</strong> งานที่ค้างจะกลับเข้าคิวหลังเปิดแอป และตรวจอัตโนมัติประมาณทุก 1 นาที</p></div>
    <div class="office-controls"><div class="office-filters" role="group" aria-label="กรองสถานะ"><button data-office-filter="overview" class="${officeFilter === "overview" ? "selected" : ""}">ภาพรวม</button><button data-office-filter="active" class="${officeFilter === "active" ? "selected" : ""}">กำลังทำ</button><button data-office-filter="attention" class="${officeFilter === "attention" ? "selected" : ""}">ต้องตรวจ</button><button data-office-filter="history" class="${officeFilter === "history" ? "selected" : ""}">ประวัติ</button></div><label class="sr-only" for="officeSearch">ค้นหางาน</label><input id="officeSearch" type="search" placeholder="ค้นหางาน โปรเจกต์ หรือคำสั่ง" value="${escapeHTML(officeQuery)}"><select id="officeKind" aria-label="ประเภทงาน"><option value="all" ${officeKind === "all" ? "selected" : ""}>ทุกประเภท</option><option value="command" ${officeKind === "command" ? "selected" : ""}>คำสั่ง</option><option value="review" ${officeKind === "review" ? "selected" : ""}>รีวิว</option></select></div>
    <div class="office-list-head"><h3>รายการงาน</h3><span id="officeCount"></span></div><div class="office-list" id="officeList"></div><button class="ghost office-more" id="officeMore">แสดงเพิ่มเติม</button>`;
  renderOfficeList();
  if (searchFocused) {
    const search = $("#officeSearch", root);
    search.focus();
    search.setSelectionRange(searchPosition, searchPosition);
  }
  $$('[data-office-filter]', root).forEach(button => button.addEventListener("click", () => { officeFilter = button.dataset.officeFilter; renderOffice(); }));
  $("#officeSearch", root).addEventListener("input", event => { officeQuery = event.target.value; renderOfficeList(); });
  $("#officeKind", root).addEventListener("change", event => { officeKind = event.target.value; renderOfficeList(); });
  $("#officeMore", root).addEventListener("click", () => { officeHistoryLimit += 20; renderOfficeList(); });
  $("#officeRefresh", root).addEventListener("click", async () => { try { const [jobs,reviews] = await Promise.all([api("/api/jobs"),api("/api/reviews")]); state.jobs = asList(jobs); state.reviews = asList(reviews); renderOffice(); } catch (error) { toast(error.message,true); } });
  $("#officeRunReview", root).addEventListener("click", async () => { await runNextReview(); if (state.activeTab === "office") renderOffice(); });
  scheduleOfficePoll();
}

async function cancelBackgroundJob(id) {
  try { await api(`/api/jobs/${encodeURIComponent(id)}/cancel`, { method:"POST", body:"{}" }); toast("Cancellation requested; process group will be terminated"); await load(); switchTab("office"); }
  catch (error) { toast(error.message, true); }
}

function videoFrameEvidenceHTML(item, jobs) {
  if (!jobs) return "";
  const cards = jobs.map(job => {
    const transcript = job.processor_kind === "audio_transcribe";
    const frames = !transcript && job.state === "completed" ? (job.result_artifact_ids || []).map(id => {
      const frame = state.artifacts.find(artifact => artifact.id === id);
      return frame ? `<figure><img src="/api/artifacts/${encodeURIComponent(id)}/content" alt="เฟรมตัวอย่างที่ ${Number(frame.metadata?.frame_time_seconds || 0).toFixed(2)} วินาที"><figcaption>${Number(frame.metadata?.frame_time_seconds || 0).toFixed(2)} วินาที · ${escapeHTML(frame.name)}</figcaption></figure>` : "";
    }).join("") : "";
    const resultID = transcript ? job.result_artifact_ids?.[0] : "";
    const body = resultID ? videoTranscriptBodies.get(resultID) : null;
    const segments = Array.isArray(body?.segments) ? body.segments : [];
    const transcriptHTML = transcript && job.state === "completed" ? (body?.error ? `<p role="alert">อ่านผลถอดเสียงไม่ได้: ${escapeHTML(body.error)}</p>` : !body ? `<p role="status">กำลังโหลดข้อความถอดเสียง…</p>` : segments.length ? `<p>ภาษาที่ตรวจพบ: ${escapeHTML(body.language || "ไม่ทราบ")}</p><ol class="video-transcript-segments">${segments.map(segment => `<li><button class="ghost" type="button" data-video-seek="${escapeHTML(item.id)}" data-second="${Math.max(0,Number(segment.start_ms || 0)/1000)}">${(Number(segment.start_ms || 0)/1000).toFixed(1)} วินาที</button> ${escapeHTML(segment.text)}</li>`).join("")}</ol>` : `<p>ไม่พบคำพูดที่ถอดได้ในวิดีโอนี้</p>`) : "";
    const status = transcript ? ({completed:"ถอดเสียงเสร็จ",running:"กำลังถอดเสียง",queued:"รอถอดเสียง",interrupted:"งานถอดเสียงสะดุด",failed:"ถอดเสียงไม่สำเร็จ",cancelled:"ยกเลิกการถอดเสียง"}[job.state] || job.state) : ({completed:"ตรวจเฟรมเสร็จ",running:"กำลังตรวจเฟรม",queued:"รอคิว",interrupted:"งานสะดุด",failed:"ตรวจเฟรมไม่สำเร็จ",cancelled:"ยกเลิกแล้ว"}[job.state] || job.state);
    return `<div><strong>${escapeHTML(status)}</strong>${job.error ? `<p role="alert">${escapeHTML(job.error)}</p>` : ""}${frames}${transcriptHTML}${["queued","running"].includes(job.state) ? `<button class="ghost" data-video-job-refresh="${escapeHTML(item.id)}">อัปเดตสถานะ</button>${transcript ? `<button class="ghost" data-video-job-cancel="${escapeHTML(job.id)}" data-video-source="${escapeHTML(item.id)}">หยุดถอดเสียง</button>` : ""}` : ""}</div>`;
  }).join("");
  const frameJobs = jobs.filter(job => job.processor_kind === "video_extract");
  const audioJobs = jobs.filter(job => job.processor_kind === "audio_transcribe");
  const retryFrames = frameJobs.length > 0 && frameJobs.every(job => ["failed","cancelled","interrupted"].includes(job.state));
  const retryAudio = audioJobs.length > 0 && audioJobs.every(job => ["failed","cancelled","interrupted"].includes(job.state));
  return `<div class="video-frame-results"><p class="form-note neutral">เฟรมเป็นตัวอย่างจากสามเวลา เสียงใช้การถอดคำพูดอัตโนมัติและอาจคลาดเคลื่อน ยังไม่ได้วิเคราะห์ภาพทุกช่วงหรือเสียงที่ไม่ใช่คำพูด</p>${cards}${!frameJobs.length ? `<button class="ghost" data-video-frame-start="${escapeHTML(item.id)}">สร้างเฟรมตัวอย่าง</button>` : ""}${retryFrames ? `<button class="ghost" data-video-frame-retry="${escapeHTML(item.id)}">ลองถอดเฟรมอีกครั้ง</button>` : ""}${!audioJobs.length || retryAudio ? `<button class="ghost" data-video-transcribe="${escapeHTML(item.id)}" data-retry="${retryAudio ? "true" : "false"}">${retryAudio ? "ลองถอดเสียงอีกครั้ง" : "ถอดเสียงพร้อมเวลา"}</button>` : ""}</div>`;
}

function renderArtifacts() {
  const root = $("#view-artifacts");
  if (!root) return;
  root.innerHTML = `<div class="workbench-grid"><div class="panel"><p class="eyebrow">Content-addressed outputs</p><h3>Create artifact</h3><form id="artifactForm"><label>Project<select name="project_id"><option value="">Global</option>${state.projects.map(item => `<option value="${item.id}">${escapeHTML(item.name)}</option>`).join("")}</select></label><label>Name<input name="name" required placeholder="report.md"></label><div class="form-grid"><label>Kind<input name="kind" value="report" required></label><label>MIME type<input name="mime_type" value="text/markdown" required></label></div><label>Content<textarea name="content" rows="10" required></textarea></label><button class="primary">Save immutable artifact</button></form></div><div class="card-list">${state.artifacts.map(item => `<article class="provider-card"><div class="provider-head"><div><h3>${escapeHTML(item.name)}</h3><p>${escapeHTML(item.mime_type)} · ${Number(item.byte_size).toLocaleString()} bytes</p></div>${pill(item.kind,"blue")}${pill(item.visibility,item.visibility === "project_shared" ? "green" : "amber")}</div><div class="kv"><span>Checksum</span><code>${escapeHTML(item.checksum)}</code><span>Project</span><span>${escapeHTML(state.projects.find(project => project.id === item.project_id)?.name || "global")}</span><span>Created</span><span>${formatDate(item.created_at)}</span></div><div class="action-row"><a class="button-link" href="/api/artifacts/${encodeURIComponent(item.id)}/content" target="_blank" rel="noreferrer">Open verified content</a>${item.project_id && item.visibility !== "project_shared" ? `<button class="ghost" data-share-artifact="${escapeHTML(item.id)}">Allow explicit project sharing</button>` : ""}</div></article>`).join("") || `<div class="empty"><h3>No artifacts</h3><p>Command logs and generated outputs will appear here.</p></div>`}</div></div>`;
  const grid = root.querySelector(".workbench-grid");
  const creator = grid?.querySelector(":scope > .panel");
  if (creator) {
    const details = document.createElement("details");
    details.className = "artifact-create";
    details.innerHTML = `<summary>${uiIcon("plus")} เพิ่มไฟล์ผลลัพธ์</summary>`;
    creator.replaceWith(details);
    details.append(creator);
    grid.prepend(details);
  }
  grid?.insertAdjacentHTML("afterbegin", `<section class="panel artifact-video-upload"><h3>นำเข้าวิดีโอ</h3><p>MP4 หรือ WebM ขนาดไม่เกิน 256 MiB ความยาวไม่เกิน 10 นาที เมื่อบันทึกแล้วเล่นและเลื่อนไปยังช่วงที่ต้องการได้</p><form id="videoUploadForm"><label>โปรเจกต์<select name="project_id"><option value="">ส่วนตัว</option>${state.projects.map(item => `<option value="${escapeHTML(item.id)}">${escapeHTML(item.name)}</option>`).join("")}</select></label><label>ไฟล์วิดีโอ<input type="file" name="file" accept="video/mp4,video/webm,.mp4,.webm" required></label><button class="primary" type="submit">นำเข้าวิดีโอ</button></form></section>`);
  $$(".card-list > .provider-card", root).forEach((card, index) => {
    const item = state.artifacts[index];
    if (item?.kind !== "video") return;
    const stream = `/api/artifacts/${encodeURIComponent(item.id)}/stream`;
    card.querySelector(".action-row a")?.setAttribute("href", stream);
    card.querySelector(".action-row a")?.replaceChildren(document.createTextNode("เปิดวิดีโอ"));
    const jobs = videoEvidenceJobs.get(item.id);
    const frameEvidence = videoFrameEvidenceHTML(item,jobs);
    card.querySelector(".action-row")?.insertAdjacentHTML("beforebegin", `<video class="artifact-video" data-video-player="${escapeHTML(item.id)}" controls playsinline preload="metadata" src="${stream}" aria-label="วิดีโอ ${escapeHTML(item.name)}"></video><p class="form-note neutral">${item.metadata?.duration_seconds ? `${Number(item.metadata.duration_seconds).toFixed(1)} วินาที · ${Number(item.metadata.width)} × ${Number(item.metadata.height)} · ${escapeHTML(item.metadata.video_codec || "")}` : "รายละเอียดวิดีโอยังไม่พร้อม"} · ข้อมูลเทคนิคไม่บอกความหมายของภาพหรือเสียง</p><section class="video-frame-evidence"><button class="ghost" type="button" data-video-evidence="${escapeHTML(item.id)}">ดูหลักฐานภาพและเสียง</button>${frameEvidence}</section>`);
  });
  $("#videoUploadForm")?.addEventListener("submit", uploadVideoArtifact);
  $$('[data-video-evidence]',root).forEach(button => button.addEventListener("click", () => loadVideoEvidence(button.dataset.videoEvidence)));
  $$('[data-video-frame-start]',root).forEach(button => button.addEventListener("click", () => startVideoFrameEvidence(button.dataset.videoFrameStart)));
  $$('[data-video-frame-retry]',root).forEach(button => button.addEventListener("click", () => startVideoFrameEvidence(button.dataset.videoFrameRetry,true)));
  $$('[data-video-transcribe]',root).forEach(button => button.addEventListener("click", () => startVideoTranscription(button.dataset.videoTranscribe,button.dataset.retry === "true")));
  $$('[data-video-job-cancel]',root).forEach(button => button.addEventListener("click", () => cancelVideoTranscription(button.dataset.videoJobCancel,button.dataset.videoSource)));
  $$('[data-video-seek]',root).forEach(button => button.addEventListener("click", () => { const player = root.querySelector(`[data-video-player="${CSS.escape(button.dataset.videoSeek)}"]`); if (player) { player.currentTime = Number(button.dataset.second || 0); player.scrollIntoView({block:"center"}); player.focus(); } }));
  $$('[data-video-job-refresh]',root).forEach(button => button.addEventListener("click", () => loadVideoEvidence(button.dataset.videoJobRefresh)));
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
  state.fidelityProfile ||= "compact-32k";
  root.innerHTML = `<div class="panel"><div class="provider-head"><div><p class="eyebrow">หลักฐานก่อนและหลังย่อบริบท</p><h3>ทดสอบความครบถ้วนของบริบทภาษาไทยและอังกฤษ</h3><p>วัดการคงข้อมูลสำคัญ การตัดสินใจ งานค้าง สถานะไฟล์ เหตุและผล การเปลี่ยนงานหรือแพตช์ และข้อความที่แต่งขึ้น พร้อมบอกเมื่อใช้ข้อมูลสำรอง</p></div>${pill(`${state.fidelityCases.length} กรณี`,"blue")}</div><label>โปรไฟล์ที่ใช้ทดสอบ<select id="fidelityProfile">${state.profiles.map(profile => `<option value="${profile.name}" ${profile.name === state.fidelityProfile ? "selected" : ""}>${profileLabel(profile)}</option>`).join("")}</select></label></div><div class="fidelity-grid"><div class="card-list">${state.fidelityCases.map(item => `<article class="provider-card"><div class="provider-head"><div><h3>${escapeHTML(item.name)}</h3><p>${escapeHTML(item.benchmark_class)} · ${escapeHTML(item.language)}</p></div>${pill(`${item.fragments.length} ส่วน`)}</div><div class="action-row"><button class="primary" data-run-fidelity="${escapeHTML(item.id)}">ทดสอบการย่อบริบท</button></div></article>`).join("")}</div><div class="card-list">${state.fidelityRuns.map(run => `<article class="provider-card"><div class="provider-head"><div><h3>${escapeHTML(run.case_name)}</h3><p>${escapeHTML(run.profile_name)} · ${formatDate(run.completed_at || run.started_at)}</p></div>${pill(run.metrics.passed ? "ผ่าน" : "ไม่ผ่าน",run.metrics.passed ? "green" : "red")}</div><div class="kv"><span>ข้อมูลสำคัญ</span><strong>${Math.round(run.metrics.essential_exact_retention*100)}%</strong><span>การตัดสินใจ</span><strong>${Math.round(run.metrics.decision_recall*100)}%</strong><span>งานค้าง</span><strong>${Math.round(run.metrics.open_task_recall*100)}%</strong><span>เหตุและผลที่แยกจากกัน</span><strong>${run.metrics.causal_pair_splits}</strong><span>โทเค็นที่ประหยัด</span><strong>${Number(run.metrics.tokens_saved).toLocaleString()}</strong><span>ข้อมูลที่แต่งขึ้น</span><strong>${run.metrics.hallucination_count}</strong><span>ข้อมูลสำรอง</span><strong>${run.metrics.fallback_used ? "ใช้ข้อมูลสำรองที่ตรวจแล้ว" : "ไม่ได้ใช้"}</strong></div></article>`).join("") || `<div class="empty"><h3>ยังไม่มีผลทดสอบ</h3><p>เลือกกรณีภาษาไทยหรืออังกฤษเพื่อทดสอบการย่อบริบท</p></div>`}</div></div>`;
  root.querySelector(".provider-head > div")?.insertAdjacentHTML("beforeend", '<p class="form-note neutral">ผลนี้ทดสอบตัวจัดบริบทด้วยข้อมูลตัวอย่างในเครื่อง ไม่ได้เรียกโมเดล และไม่ใช่การรับรองขนาดบริบทของ provider</p>');
  $("#fidelityProfile")?.addEventListener("change", event => { state.fidelityProfile = event.target.value; });
  $$('[data-run-fidelity]', root).forEach(button => button.addEventListener("click", () => runFidelity(button.dataset.runFidelity)));
}

async function runFidelity(id) {
  try { state.fidelityProfile = $("#fidelityProfile").value; const run = await api(`/api/fidelity/cases/${encodeURIComponent(id)}/run`, { method:"POST", body:JSON.stringify({ profile_name:state.fidelityProfile }) }); toast(run.metrics.passed ? "ผลทดสอบความครบถ้วนผ่าน" : "ผลทดสอบความครบถ้วนไม่ผ่าน", !run.metrics.passed); await load(); switchTab("fidelity"); }
  catch (error) { toast(error.message, true); }
}

function workspacePortabilityPanel() {
  const preview = state.workspacePreview;
  const sourceIDs = preview?.summary?.project_ids || [];
  return `<div class="panel"><p class="eyebrow">ย้าย Workspace แบบเข้ารหัส</p><h3>ย้ายข้อมูลหลักส่วนตัว</h3><p class="form-note neutral">แพ็กเกจที่เข้ารหัสด้วย age ไม่รวมคีย์ เซสชัน การอนุมัติ ผลการกระทำ โปรไฟล์ Browser หรือสิทธิ์ของ runtime ข้อมูลที่นำเข้าจะได้ ID ใหม่และเริ่มเป็นส่วนตัว</p>
    <form id="workspaceExportForm"><fieldset><legend>โปรเจกต์</legend>${state.projects.map(item => `<label class="check-label"><input type="checkbox" name="project_ids" value="${escapeHTML(item.id)}"> ${escapeHTML(item.name)}</label>`).join("") || `<p>ยังไม่มีโปรเจกต์</p>`}</fieldset><label>รหัสผ่านสำหรับย้ายข้อมูล<input name="passphrase" type="password" minlength="12" autocomplete="new-password" required></label><button class="primary">สร้างแพ็กเกจเข้ารหัส</button></form>
    <hr><form id="workspacePreviewForm"><label>แพ็กเกจ Workspace ที่เข้ารหัส<input name="file" type="file" accept=".age,application/vnd.hermetrix.workspace+age" required></label><label>รหัสผ่าน<input name="passphrase" type="password" minlength="12" autocomplete="current-password" required></label><button class="ghost">ถอดรหัสและดูตัวอย่าง</button></form>
    ${preview ? `<div class="panel"><div class="provider-head"><div><h3>ตัวอย่างข้อมูลที่ตรวจแล้ว</h3><p>${Number(preview.summary?.projects || 0)} โปรเจกต์ · นำเข้าสิทธิ์: ${preview.summary?.authority_imported ? "ใช่" : "ไม่"}</p></div>${pill(preview.state,"green")}</div><form id="workspaceApplyForm"><label>ตำแหน่งปลายทางในรูป JSON<textarea name="root_mappings" rows="5" required>${escapeHTML(JSON.stringify(Object.fromEntries(sourceIDs.map(id => [id, ""])), null, 2))}</textarea></label><label>ยืนยันรหัสผ่าน<input name="passphrase" type="password" minlength="12" autocomplete="current-password" required></label><button class="primary">นำเข้าไปยังตำแหน่งใหม่</button></form></div>` : ""}</div>`;
}

function recoveryPanel() {
  const runs = state.backups.filter(item => item.kind === "full_recovery");
  const report = state.recoveryReport;
  return `<div class="panel"><div class="provider-head"><div><p class="eyebrow">กู้ระบบทั้งชุด</p><h3>สำรองฐานข้อมูล SQLite และไฟล์ CAS พร้อมกัน</h3><p class="form-note neutral">คลังคีย์แยกจากแพ็กเกจสำรอง การบันทึกด้วย Windows DPAPI ใช้กับบัญชีและเครื่องเดิม หากย้ายเครื่องต้องลงทะเบียนคีย์ใหม่</p></div><button class="primary" id="createFullRecovery">สร้างแพ็กเกจกู้ระบบ</button></div><label>ตรวจแพ็กเกจกู้ระบบที่มีอยู่<input id="verifyRecoveryFile" type="file" accept=".zip,application/vnd.hermetrix.full-recovery+zip"></label><button class="ghost" id="verifyFullRecovery">ตรวจโดยไม่กู้ข้อมูล</button>
    ${report ? `<div class="kv"><span>ความสมบูรณ์</span><strong>${escapeHTML(report.integrity_check)}</strong><span>ข้อผิดพลาดความเชื่อมโยงข้อมูล</span><strong>${report.foreign_key_errors}</strong><span>โครงสร้างข้อมูล</span><strong>${report.manifest.schema_version}</strong><span>ไฟล์ CAS</span><strong>${report.verified_blob_count}</strong><span>ใช้กับรุ่นนี้ได้</span><strong>${report.compatible ? "ได้" : "ไม่ได้"}</strong></div>` : ""}
    <div class="card-list">${runs.map(item => `<article class="provider-card"><div class="provider-head"><div><h3>${escapeHTML(item.id)}</h3><p>${formatDate(item.created_at)}</p></div>${pill(item.state,item.state === "completed" ? "green" : "amber")}</div><div class="kv"><span>ผลตรวจไฟล์</span><code>${escapeHTML(item.checksum)}</code><span>ขนาดฐานข้อมูล (ไบต์)</span><strong>${Number(item.counts.database_bytes || 0).toLocaleString()}</strong><span>ไฟล์ CAS</span><strong>${item.counts.blobs || 0}</strong></div>${item.state === "completed" ? `<a class="button-link" href="/api/recovery/${encodeURIComponent(item.id)}/content">ดาวน์โหลดแพ็กเกจกู้ระบบ</a>` : ""}</article>`).join("") || `<p class="form-note neutral">ยังไม่มีแพ็กเกจกู้ระบบ</p>`}</div></div>`;
}

const maintenanceExpanded = new Set();
function renderMaintenance() {
  const root = $("#view-maintenance");
  if (!root) return;
  const usage = state.usage || {};
  root.innerHTML = `<div class="maintenance-grid"><div class="panel-stack"><div class="panel"><p class="eyebrow">ยอดการใช้งานและที่มา</p><h3>ยอดการใช้งานจากระบบ</h3><div class="kv"><span>เซสชัน</span><strong>${usage.sessions || 0}</strong><span>ขั้นตอนโมเดล</span><strong>${usage.model_steps || 0}</strong><span>การเรียกเครื่องมือ</span><strong>${usage.tool_calls || 0}</strong><span>เครื่องมือสำเร็จ</span><strong>${usage.tool_succeeded || 0}</strong><span>โทเค็นรวม</span><strong>${Number(usage.total_tokens || 0).toLocaleString()}</strong></div></div><div class="panel"><p class="eyebrow">การตั้งค่าทั่วไป</p><h3>ค่า JSON ที่ไม่ใช่ความลับ</h3><form id="settingForm"><label>ชื่อค่า<input name="key" required value="retention.skill_activation_days"></label><label>ค่า JSON<input name="value" required value="365"></label><button class="ghost">บันทึกค่า</button></form><div class="meta">${state.settings.map(item => pill(`${item.key}=${JSON.stringify(item.value)}`)).join("")}</div></div><div class="panel"><p class="eyebrow">ข้อมูลที่ผู้ใช้บันทึก</p><h3>ความจำที่ผู้ใช้ควบคุม</h3><form id="memoryForm"><div class="form-grid"><label>ขอบเขต<select name="scope_kind"><option value="user">ผู้ใช้</option><option value="project">โปรเจกต์</option></select></label><label>โปรเจกต์<select name="scope_ref"><option value="">ไม่เลือก</option>${state.projects.map(item => `<option value="${item.id}">${escapeHTML(item.name)}</option>`).join("")}</select></label></div><label>ประเภท<input name="memory_kind" value="preference" required></label><label>เนื้อหา<textarea name="content" rows="3" required></textarea></label><button class="ghost">บันทึกความจำ</button></form><div class="card-list">${state.memories.map(item => `<article class="memory-row"><div><strong>${escapeHTML(item.memory_kind)}</strong><p>${escapeHTML(item.content)}</p></div>${pill(item.state,item.state === "active" ? "green" : "amber")}${item.state === "active" ? `<button class="danger" data-archive-memory="${item.id}">เก็บความจำ</button>` : ""}</article>`).join("")}</div></div></div>
    <div class="panel-stack"><div class="panel"><div class="provider-head"><div><p class="eyebrow">ย้ายข้อมูลสกิล</p><h3>ส่งออก · ดูก่อนนำเข้า · รับเป็นข้อเสนอ</h3><p class="form-note neutral">แพ็กเกจนี้มีเฉพาะข้อมูลวงจรสกิล การแชร์โปรเจกต์ การย้าย Workspace แบบเข้ารหัส และการกู้ระบบใช้ขั้นตอนแยกกัน</p></div><button class="primary" id="exportBackupButton">ส่งออกสกิล</button></div><label>นำเข้าแพ็กเกจสกิล<input id="importBackupFile" type="file" accept="application/json,.json"></label><button class="ghost" id="previewImportButton">ตรวจและดูตัวอย่างก่อนนำเข้า</button><div class="card-list">${state.backups.filter(item => item.kind !== "full_recovery").map(item => `<article class="provider-card"><div class="provider-head"><div><h3>${escapeHTML(item.kind)}</h3><p>${formatDate(item.created_at)}</p></div>${pill(item.state,item.state === "completed" || item.state === "imported" ? "green" : "amber")}</div><div class="kv"><span>ผลตรวจไฟล์</span><code>${escapeHTML(item.checksum || "pending")}</code><span>สกิล</span><strong>${item.counts.skills || 0}</strong><span>รายการขัดแย้ง</span><strong>${item.counts.skill_conflicts || 0}</strong></div><div class="action-row">${item.kind === "export" && item.state === "completed" ? `<a class="button-link" href="/api/backups/${item.id}/download">ดาวน์โหลดแพ็กเกจสกิล</a>` : ""}${item.kind === "import_preview" && item.state === "awaiting_apply" ? `<button class="primary" data-apply-import="${item.id}">รับเป็นข้อเสนอ</button>` : ""}</div></article>`).join("")}</div></div>
      ${workspacePortabilityPanel()}${recoveryPanel()}
      <div class="panel"><div class="provider-head"><div><p class="eyebrow">นโยบายงานเบื้องหลัง</p><h3>กำหนดงานดูแลระบบ</h3></div><button class="ghost" id="runDueButton">รันงานที่ถึงกำหนดตามนโยบาย</button></div><form id="scheduleForm"><label>ชื่อ<input name="name" value="Weekly curator" required></label><div class="form-grid"><label>งาน<select name="task_kind"><option value="curator">รายงานวิเคราะห์สกิล</option><option value="gc_dry_run">ตรวจไฟล์ที่อาจลบได้</option></select></label><label>ช่วงเวลา (วินาที)<input name="interval_seconds" type="number" min="300" value="604800"></label></div><label class="check-label"><input name="enabled" type="checkbox"> เปิดใช้งาน</label><label class="check-label"><input name="require_idle" type="checkbox" checked> รอให้เครื่องว่าง 5 นาที</label><label class="check-label"><input name="require_ac_power" type="checkbox" checked> ต้องเสียบไฟ</label><button class="ghost">บันทึกกำหนดการ</button></form><div class="meta">${state.schedules.map(item => pill(`${item.name} · ${item.enabled ? "เปิดใช้" : "ปิดใช้"} · รอบถัดไป ${formatDate(item.next_run_at)}`)).join("")}</div></div>
      <div class="panel"><div class="provider-head"><div><p class="eyebrow">เก็บกวาดไฟล์ที่กู้คืนได้</p><h3>ตรวจตัวอย่าง → เก็บภาพก่อนแก้ → แยกไฟล์</h3></div><button class="primary" id="dryRunGCButton">ตรวจตัวอย่างรอบใหม่</button></div><div class="card-list">${state.gcRuns.map(run => `<article class="provider-card"><div class="provider-head"><div><h3>${run.unreachable_count} ไฟล์ที่ไม่มีการอ้างอิง · ${Number(run.reclaimable_bytes).toLocaleString()} bytes</h3><p>${formatDate(run.created_at)} · ${shortHash(run.snapshot_revision)}</p></div>${pill(run.state,run.state === "restored" ? "green" : "amber")}</div><div class="action-row">${run.state === "planned" ? `<button class="danger" data-apply-gc="${run.id}">แยกไฟล์ชุดนี้</button>` : ""}${["quarantined","partial_quarantine"].includes(run.state) ? `<button class="ghost" data-restore-gc="${run.id}">คืนไฟล์ที่แยกไว้</button>` : ""}</div></article>`).join("")}</div></div></div></div>`;
  $$(".maintenance-grid > .panel-stack > .panel", root).forEach((panel, index) => {
    if (index === 0) return;
    const title = panel.querySelector("h3")?.textContent || "ตัวเลือกเพิ่มเติม";
    const details = document.createElement("details");
    details.className = "maintenance-section";
    details.dataset.maintenanceIndex = String(index);
    details.open = maintenanceExpanded.has(index);
    details.innerHTML = `<summary>${escapeHTML(title)}</summary>`;
    panel.replaceWith(details);
    details.append(panel);
    details.addEventListener("toggle", () => { if (details.open) maintenanceExpanded.add(index); else maintenanceExpanded.delete(index); });
  });
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
  const contextProfile = state.profiles.find(item => item.name === session?.context_profile);
  const contextBar = sessionContextMeterHTML(session, contextProfile, state.sessionDetail?.events, state.sessionDetail?.latest_context_snapshot) +
    (state.sessionDetail?.context_snapshot_error ? `<p role="alert">${escapeHTML(state.sessionDetail.context_snapshot_error)}</p>` : "");
  const usagePanel = `<section class="panel review-usage-panel" aria-label="Token และ Context"><div class="review-usage-heading"><p class="eyebrow">การใช้งานแชท</p><h3>Token และ Context</h3></div>${session ? `${sessionUsageHTML(session, state.sessionDetail?.events, state.sessionDetail?.latest_context_snapshot)}${contextBar}` : `<p class="form-note neutral">เริ่มหรือเลือกแชทเพื่อดูจำนวน token ที่ใช้ งบต่อคำสั่ง และบริบทล่าสุด</p>`}</section>`;
  const contract = session?.contract || {};
  const selectedSkills = contract.selected_skills || [];
  const lastContextSkills = new Set((state.sessionDetail?.latest_context_snapshot?.report?.selected_ids || []).filter(id => id.startsWith("skill:")));
  const pendingApprovals = (state.sessionDetail?.approvals || []).filter(item => item.state === "pending");
  const skillAccessNote = session?.project_id && (contract.tool_bindings || []).some(item => item.name === "skill_view")
    ? "ยังไม่ได้แนบเนื้อหา Skill ในคำขอนี้ แต่โมเดลค้นหาและอ่านเวอร์ชันที่ผูกกับ session ได้ด้วย skill_search และ skill_view"
    : "แชตส่วนตัวนี้ไม่มีเครื่องมือเรียก skill_view หากต้องการให้โมเดลค้นหาและอ่าน Skill ให้เริ่มแชตในโปรเจกต์";
  const sessionPanel = session ? `<div class="panel session-contract-panel"><div class="provider-head"><div><p class="eyebrow">ข้อมูลของแชตนี้</p><h3>${escapeHTML(session.title)}</h3></div>${pill(session.state, session.state === "active" ? "green" : "amber")}</div><div class="kv"><span>โมเดล</span><strong>${escapeHTML(session.model)}</strong><span>บริบท</span><strong>${escapeHTML(session.context_profile)}</strong><span>โปรเจกต์</span><strong>${escapeHTML(state.projects.find(item => item.id === session.project_id)?.name || "แชตส่วนตัว")}</strong><span>สกิลที่เลือก</span><strong>${selectedSkills.length}</strong><span>สกิลในคำขอล่าสุด</span><strong>${lastContextSkills.size}</strong><span>เครื่องมือ</span><strong>${(contract.tool_bindings || []).length}</strong><span>รออนุมัติ</span><strong>${pendingApprovals.length}</strong><span>สัญญา</span><code>${escapeHTML(shortHash(session.contract_revision))}</code><span>รุ่นความสามารถ</span><code>${escapeHTML(shortHash(contract.capability_revision))}</code></div>${selectedSkills.length ? `<div class="meta session-skill-list">${selectedSkills.map(item => pill(item.canonical_name,lastContextSkills.has(`skill:${item.skill_id}:${item.version_id}`) ? "green" : "blue")).join("")}</div>` : `<p class="form-note neutral">${skillAccessNote}</p>`}<div class="action-row"><button class="primary" id="reviewOpenCapabilities">สกิลและเครื่องมือ</button><button class="ghost" id="reviewOpenTools">ศูนย์เครื่องมือ</button></div></div>` : `<div class="panel"><p class="eyebrow">ตรวจแชต</p><h3>เริ่มหรือเลือกแชต</h3><p class="dialog-message">ข้อมูลโมเดล บริบท สกิล เครื่องมือ และการอนุมัติของแชตจะแสดงที่นี่</p></div>`;
  target.innerHTML = `${usagePanel}${sessionPanel}<div class="panel"><p class="eyebrow">สิทธิ์และงานเบื้องหลัง</p><h3>ตรวจหลักฐานก่อนให้สิทธิ์</h3><p class="dialog-message">ตรวจข้อเสนอสกิล คำขอเขียนไฟล์ งานตรวจทาน และผลคำสั่งได้ที่นี่ ตัวแทนทำงานเพิ่มสิทธิ์ของตัวเองจากหน้านี้ไม่ได้</p><div class="kv"><span>ข้อเสนอ</span><strong>${state.candidates.filter(item => ["needs_review","quarantined"].includes(item.state)).length}</strong><span>งานตรวจทาน</span><strong>${queued.length}</strong><span>นโยบาย</span><strong>${escapeHTML(state.skillAuthority?.mode || "manual")}</strong></div><div class="action-row"><button class="primary" id="reviewOpenSkills">เปิดสตูดิโอสกิล</button><button class="ghost" id="reviewRunNext" title="${queued.length ? "เริ่มตรวจงานที่รออยู่นานที่สุด" : "ไม่มีงานตรวจทานค้างอยู่ งานใหม่จะเข้าคิวเมื่อมีเหตุการณ์หรือคำขอเรียนรู้"}" ${queued.length ? "" : "disabled"}>ตรวจงานถัดไป</button></div></div>
  <div class="card-list spaced">${state.jobs.slice(0,5).map(job => `<article class="artifact-mini"><div class="provider-head"><strong>${escapeHTML(job.payload?.executable || job.kind)}</strong>${pill(job.state,job.state === "completed" ? "green" : job.state === "failed" ? "red" : "amber")}</div><small>${formatDate(job.created_at)} · ${escapeHTML(job.result?.artifact_id || "รอผลการทำงาน")}</small></article>`).join("") || `<div class="probe-empty">ยังไม่มีผลการทำงานล่าสุด</div>`}</div>`;
  target.querySelector(".review-usage-panel .session-usage")?.addEventListener("toggle", event => { tokenUsageDetailsOpen = event.currentTarget.open; });
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
  const entries = state.workspaceFiles || [];
  const ordered = items => items.sort((a, b) => a.name.localeCompare(b.name, undefined, {numeric:true, sensitivity:"base"}));
  const sections = [
    ["โฟลเดอร์", ordered(entries.filter(item => item.directory && !item.name.startsWith(".")))],
    ["ไฟล์", ordered(entries.filter(item => !item.directory && !item.name.startsWith(".")))],
    ["รายการซ่อน", ordered(entries.filter(item => item.name.startsWith(".")))],
  ];
  const fileRows = sections.filter(([, items]) => items.length).map(([label, items]) => {
    const rows = items.map(item => `<button class="file-row" data-workbench-file="${escapeHTML(item.path)}" data-directory="${item.directory}"><span>${uiIcon(item.directory ? "files" : "file")}</span><strong>${escapeHTML(item.name)}</strong><small>${item.directory ? "" : `${Number(item.bytes).toLocaleString()} B`}</small></button>`).join("");
    return label === "รายการซ่อน"
      ? `<details class="file-group file-group-hidden"><summary>${label} <span>${items.length}</span></summary>${rows}</details>`
      : `<section class="file-group"><h4>${label}</h4>${rows}</section>`;
  }).join("");
  return `<div class="workspace-files"><div class="provider-head"><div><strong>ไฟล์โปรเจกต์</strong><small>${escapeHTML(project?.name || "ยังไม่เลือกโปรเจกต์")}</small></div><button class="ghost" id="newWorkbenchFile" ${project?.root_path ? "" : "disabled"}>+ ไฟล์ใหม่</button></div><label class="file-filter"><input type="search" id="workbenchFileFilter" aria-label="ค้นหาไฟล์ในโฟลเดอร์นี้" placeholder="ค้นหาไฟล์ในโฟลเดอร์นี้…"></label>
    ${project?.root_path ? `<div class="file-path"><button type="button" class="file-root-button" id="workbenchFileRoot" title="กลับโฟลเดอร์หลักของโปรเจกต์">${uiIcon("files")}<span>${escapeHTML(project.name)}</span></button>${state.projectPath ? `<span class="file-path-separator" aria-hidden="true">›</span><code title="${escapeHTML(state.projectPath)}">${escapeHTML(state.projectPath)}</code><button class="ghost" id="workbenchFileUp" aria-label="ขึ้นหนึ่งระดับ">ขึ้นหนึ่งระดับ</button>` : ""}</div><div class="file-browser">${fileRows || `<div class="probe-empty">โฟลเดอร์นี้ยังไม่มีไฟล์</div>`}<div class="probe-empty file-filter-empty" hidden>ไม่พบไฟล์ที่ตรงกับคำค้น</div></div>` : `<div class="probe-empty">โปรเจกต์นี้ยังไม่มีโฟลเดอร์โค้ด เพิ่ม path ในการตั้งค่าโปรเจกต์ก่อนใช้ Files, Code และ Terminal</div>`}
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
    body.innerHTML = `<div class="code-empty"><h3>เริ่มแก้ไฟล์</h3><p>เลือกไฟล์จากช่อง Files แล้วระบบจะเปิดไฟล์ในช่อง Code นี้</p><button class="ghost" id="codeOpenFiles">เปิดรายการไฟล์</button></div>`;
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
    host.innerHTML = `<textarea aria-label="Code editor fallback" spellcheck="false" wrap="${ideWordWrapEnabled() ? "soft" : "off"}">${escapeHTML(document.content)}</textarea>`;
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
      host.innerHTML = `<textarea aria-label="Code editor fallback" spellcheck="false" wrap="${ideWordWrapEnabled() ? "soft" : "off"}">${escapeHTML(document.content)}</textarea>`;
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
  $$('[data-code-tab-close]').forEach(button => button.addEventListener("click", () => closeCodeTab(document.projectID, button.dataset.codeTabClose)));
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
  if (!path) return;
  captureCodeDraft();
  const tabs = projectCodeTabs(projectID);
  const index = tabs.indexOf(path);
  if (index >= 0) tabs.splice(index, 1);
  if (state.projectFile?.projectID === projectID && state.projectFile?.path === path) {
    const next = tabs[Math.min(index, tabs.length - 1)];
    state.projectFile = null;
    if (next) { void openWorkbenchFile(next); return; }
    const editorIndex = state.panes.indexOf("editor");
    if (editorIndex >= 0) {
      if (state.panes.includes("review")) state.panes.splice(editorIndex, 1);
      else state.panes[editorIndex] = "review";
      state.maximisedPane = null;
      state.compactPane = state.panes.includes("review") ? "review" : "files";
      renderPanes();
      saveLayout();
      return;
    }
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
  $("#workbenchFileFilter")?.addEventListener("input", event => {
    const query=event.target.value.trim().toLowerCase();
    let visible = 0;
    $$('[data-workbench-file]').forEach(button => {
      button.hidden = !button.dataset.workbenchFile.toLowerCase().includes(query);
      if (!button.hidden) visible++;
    });
    $$('.file-group').forEach(group => {
      group.hidden = !group.querySelector('[data-workbench-file]:not([hidden])');
      if (group.matches('details')) group.open = Boolean(query) && !group.hidden;
    });
    const empty = $('.file-filter-empty');
    if (empty) empty.hidden = !query || visible > 0;
  });
  $("#workbenchProject")?.addEventListener("change", event => selectProject(event.target.value, ""));
  $("#workbenchFileRoot")?.addEventListener("click", () => browseWorkspace(""));
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
    rememberCodeTab(projectID, path);
    state.projectFile = { ...document, projectID, originalContent: document.originalContent ?? document.content };
    state.projectFileDiff = "";
    openContentPane("editor");
    requestAnimationFrame(() => {
      if (!compactWorkspace()) $(".pane[data-kind='editor']")?.scrollIntoView({block:"nearest", inline:"nearest"});
      activeCodeEditor?.focus();
    });
  } catch (error) {
    toast(/workbench editor supports files up to 2 MiB/i.test(error.message)
      ? "ไฟล์นี้ใหญ่เกิน 2 MiB สำหรับตัวแก้ไข กรุณาเลือกไฟล์ข้อความที่เล็กกว่านี้"
      : error.message, true);
  }
}

async function newWorkbenchFile() {
  const projectID = state.currentProject?.id;
  const path = await askAction({title:"New file",message:"File name or path inside this project.",confirmLabel:"Create",reasonLabel:"File path"});
  if (!path || state.currentProject?.id !== projectID) return;
  // Read an existing file first so a new-file draft cannot mask its contents.
  try { await api(`/api/projects/${encodeURIComponent(projectID)}/file?path=${encodeURIComponent(path.trim())}`); await openWorkbenchFile(path.trim()); return; }
  catch (error) { if (!/not exist|not found|no such file|cannot find (?:the )?(?:file|path)/i.test(error.message)) { toast(error.message, true); return; } }
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
    ${terminal ? `<div class="terminal-screen" id="terminalScreen" role="application" aria-label="Interactive terminal"></div><div class="terminal-actions"><span>Interactive PTY · type, paste, Tab, arrows and Ctrl-C work directly</span><button class="ghost" id="terminalClose">Close terminal</button></div><details class="terminal-output-details"><summary>อ่านข้อความ Terminal</summary><pre id="terminalAccessibleOutput" aria-label="ผลลัพธ์ Terminal ล่าสุด"></pre></details>` : `<div class="probe-empty">${state.currentProject?.root_path ? (terminalAutoAttempted.has(state.currentProject.id) && !terminalStarts.has(state.currentProject.id) ? "Could not open a terminal. Use + Terminal to retry." : "Opening terminal in your project…") : "This project has no code folder."}</div>`}
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
  state.terminalOutput = "";
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
    if (output.truncated) { activeTerminalEmulator?.reset(); state.terminalOutput = ""; }
    terminalCursor = { id, value: Number(output.cursor || cursor) };
    state.terminalOutput = `${cursor && !output.truncated ? state.terminalOutput || "" : ""}${output.output || ""}`.slice(-16384);
    const transcript = $("#terminalAccessibleOutput");
    if (transcript) transcript.textContent = stripANSI(state.terminalOutput).slice(-8192);
    if (activeTerminalEmulator) activeTerminalEmulator.write(output.output || "");
    else {
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

function renderBrowserScreenshot(tab) {
  if (!tab?.screenshot_artifact_id) return "";
  const hotspots = tab.state === "ready" ? (tab.elements || []).filter(element => !element.is_password && element.width_pct > 0 && element.height_pct > 0 && ["a", "button", "input", "textarea"].includes(element.tag)) : [];
  const controls = hotspots.map(element => {
    const typing = ["input", "textarea"].includes(element.tag);
    const action = typing ? "data-browser-type" : "data-browser-click";
    const label = `${typing ? "ตรวจและพิมพ์ใน" : "ตรวจและคลิก"} ${element.text || element.placeholder || element.tag}`;
    return `<button type="button" class="browser-shot-hotspot" ${action}="${element.ref}" data-x-pct="${element.x_pct}" data-y-pct="${element.y_pct}" data-width-pct="${element.width_pct}" data-height-pct="${element.height_pct}" aria-label="${escapeHTML(label)}" title="${escapeHTML(label)}"></button>`;
  }).join("");
  return `<div class="browser-shot-stage"><img class="browser-shot" src="/api/artifacts/${encodeURIComponent(tab.screenshot_artifact_id)}/content" alt="ภาพหน้าเว็บ ${escapeHTML(tab.title || tab.url)}"><div class="browser-shot-hotspots">${controls}</div></div>${controls ? `<p class="form-note neutral">กดตำแหน่งบนภาพเพื่อเลือกคำสั่ง ระบบจะให้ตรวจยืนยันก่อนคลิกหรือพิมพ์</p>` : ""}`;
}

function renderWorkbenchBrowserHTML() {
  const tab=state.browserTabs.find(item => item.id===state.selectedBrowserTab);
  const ready=tab?.state==="ready";
  const reopen=tab?.state==="interrupted";
  const browserProject=tab ? tab.project_id : state.selectedProject;
  return `<div class="panel"><div class="provider-head"><div><p class="eyebrow">เบราว์เซอร์สำหรับงานนี้</p><h3>${escapeHTML(tab?.title || "เปิดเว็บไซต์")}</h3></div>${tab ? pill(tab.state,tab.state==="ready"?"green":"amber") : ""}</div>
    ${reopen ? `<p class="form-note neutral" role="status">แท็บนี้หยุดทำงานหลังแอปรีสตาร์ต ภาพและข้อความด้านล่างเป็นข้อมูลก่อนหยุด เปิดที่อยู่เดิมในแท็บใหม่แล้วตรวจหน้าอีกครั้งก่อนทำต่อ</p>` : ""}
    ${tab?.state==="blocked" ? `<p class="session-error" role="alert">แท็บนี้ถูกหยุดเพราะนโยบายการเข้าถึง: ${escapeHTML(tab.error || "ตรวจ URL และสิทธิ์ของโปรเจกต์ก่อนเปิดแท็บใหม่")}</p>` : ""}
    <form id="browserOpenForm"><label>ที่อยู่เว็บไซต์<input name="url" type="url" value="${escapeHTML(tab?.url || "https://")}" required></label><label>โปรเจกต์<select name="project_id"><option value="" ${!browserProject?"selected":""}>ไม่ผูกโปรเจกต์</option>${state.projects.map(item=>`<option value="${escapeHTML(item.id)}" ${item.id===browserProject?"selected":""}>${escapeHTML(item.name)}</option>`).join("")}</select></label><label class="check-label"><input name="allow_private" type="checkbox" ${tab?.allow_private?"checked":""}> อนุญาตที่อยู่ในเครื่องหรือเครือข่ายส่วนตัวสำหรับแท็บนี้</label><div class="action-row"><button class="primary">${reopen ? "เปิดที่อยู่เดิมในแท็บใหม่" : "เปิดแท็บใหม่"}</button>${ready?`<button class="ghost" type="button" data-browser-action="navigate">ไปที่ที่อยู่นี้</button><button class="ghost" type="button" data-browser-action="back">ย้อนกลับ</button><button class="ghost" type="button" data-browser-action="scroll_up">เลื่อนหน้าเว็บขึ้น</button><button class="ghost" type="button" data-browser-action="scroll_down">เลื่อนหน้าเว็บลง</button><button class="ghost" type="button" data-browser-action="inspect">อ่านหน้าใหม่</button><button class="ghost" type="button" data-browser-action="capture">จับภาพ</button><button class="danger" type="button" data-browser-action="close">ปิดแท็บ</button>`:""}</div></form>
    ${ready ? `<div class="action-row"><button class="ghost" type="button" data-browser-action="forward">ถัดไป</button><button class="ghost" type="button" data-browser-action="reload">โหลดหน้าใหม่</button></div>` : ""}
    <div class="meta">${state.browserTabs.map(item=>`<button class="ghost" data-browser-id="${escapeHTML(item.id)}">${escapeHTML(item.title||item.url)} · ${escapeHTML(item.state)}</button>`).join("")}</div>
    ${tab?`${renderBrowserScreenshot(tab)}${renderBrowserInspection(tab.inspection)}<section class="inspect-section"><h3>ข้อความในหน้าเว็บ${ready?"":"ก่อนหยุด"}</h3><pre>${escapeHTML((tab.text_snapshot||"").slice(0,8000))}</pre></section>${ready?`<div class="browser-elements">${(tab.elements||[]).map(element=>`<article class="browser-element"><strong>${element.ref}</strong><span><strong>${escapeHTML(element.is_password ? "ช่องรหัสผ่าน · ใช้ในเบราว์เซอร์เอง" : element.text||element.placeholder||element.tag)}</strong><small>${escapeHTML(element.tag)} ${escapeHTML(element.role||"")}</small></span><span>${element.is_password ? "" : `<button class="ghost" data-browser-click="${element.ref}">ตรวจและคลิก</button>${["input","textarea"].includes(element.tag)?`<button class="ghost" data-browser-type="${element.ref}">ตรวจและพิมพ์</button>`:""}`}</span></article>`).join("")}</div>`:""}`:`<div class="probe-empty">เปิดเว็บไซต์เพื่อดูภาพและรายการที่คลิกหรือพิมพ์ได้ การกระทำที่อาจเปลี่ยนข้อมูลจะแสดงให้ตรวจยืนยันก่อน</div>`}
    ${ready && (tab.elements || []).some(element => !element.is_password) ? `<form id="browserKeyForm" class="inspect-section"><h3>กดปุ่มบนหน้าเว็บ</h3><p>เลือกรายการและปุ่มที่จะกด ระบบจะจับภาพและให้ตรวจยืนยันก่อนส่งปุ่ม</p><label>รายการ<select name="ref">${tab.elements.filter(element => !element.is_password).map(element => `<option value="${element.ref}">${element.ref}. ${escapeHTML(element.text || element.placeholder || element.tag)}</option>`).join("")}</select></label><label>ปุ่ม<select name="key"><option>Tab</option><option>Enter</option><option>Escape</option><option>ArrowDown</option><option>ArrowUp</option></select></label><button class="ghost" type="submit">ตรวจและกดปุ่ม</button></form>` : ""}
  </div>${renderDesktopObservationHTML()}`;
}

function renderDesktopObservationHTML() {
  const windows = state.desktopWindows || [];
  const observed = state.desktopObservation;
  const selected = state.desktopSelectedWindow || "";
  return `<section class="panel desktop-observation"><div class="provider-head"><div><p class="eyebrow">Windows · UI Automation</p><h3>ดูหน้าต่างบนเครื่อง</h3><p>เลือกหน้าต่างเพื่อดูภาพและรายการที่เข้าถึงได้ ข้อมูลจะจับจากหน้าต่างที่เลือกในขณะนั้น</p></div></div>
    <div class="action-row"><button class="ghost" id="desktopListWindows" type="button">${state.desktopWindowsLoaded ? "โหลดรายการใหม่" : "แสดงหน้าต่าง"}</button></div>
    ${state.desktopActionPending ? `<div class="action-row"><p role="status">กำลังส่งคำสั่ง Windows · ${escapeHTML(state.desktopActionPending.id)}</p><button class="danger" id="desktopStopAction" type="button">หยุดคำสั่ง</button></div>` : ""}
    ${state.desktopError ? `<p class="session-error" role="alert">${escapeHTML(state.desktopError)}</p>` : ""}
    ${windows.length ? `<label>หน้าต่าง<select id="desktopWindowSelect">${windows.map(item => { const value = `${item.process_id}:${item.handle}`; return `<option value="${escapeHTML(value)}" ${selected === value ? "selected" : ""}>${escapeHTML(item.title)} · PID ${item.process_id}</option>`; }).join("")}</select></label><button class="primary" id="desktopObserveWindow" type="button">ดูหน้าต่างที่เลือก</button>` : state.desktopWindowsLoaded ? `<p>ไม่พบหน้าต่างที่เข้าถึงได้ในเซสชันนี้</p>` : ""}
    ${observed ? `<p>ข้อมูลเวลา ${formatDate(observed.observed_at)} · ${escapeHTML(observed.window.title)} · PID ${observed.window.process_id}</p>${observed.screenshot_artifact_id ? `<img class="browser-shot" src="/api/artifacts/${encodeURIComponent(observed.screenshot_artifact_id)}/content" alt="ภาพหน้าต่าง ${escapeHTML(observed.window.title)}">` : `<p class="session-error">ภาพหน้าต่างไม่พร้อม: ${escapeHTML(observed.screenshot_error || "ระบบไม่สามารถจับภาพหน้าต่างนี้")}</p>`}<details><summary>รายการในหน้าต่าง · ${(observed.elements || []).length}</summary><ul>${(observed.elements || []).map(item => `<li>${item.ref}. ${escapeHTML(item.name || item.automation_id || item.control_type)} <small>${escapeHTML(item.control_type)} ${escapeHTML(item.native_class || "")}</small>${item.enabled && observed.screenshot_artifact_id && /(\.Button$|BUTTON)/i.test(`${item.control_type} ${item.native_class}`) ? `<button class="ghost" data-desktop-ref="${item.ref}" data-desktop-action="invoke">ตรวจและกด</button>` : ""}${item.enabled && !item.is_password && observed.screenshot_artifact_id && /(\.Edit$|EDIT)/i.test(`${item.control_type} ${item.native_class}`) ? `<label>ข้อความ<input data-desktop-value-ref="${item.ref}" aria-label="ข้อความสำหรับ ${escapeHTML(item.name || item.automation_id || `รายการ ${item.ref}`)}"><button class="ghost" data-desktop-ref="${item.ref}" data-desktop-action="set_value">ตรวจและพิมพ์</button></label>` : ""}</li>`).join("")}</ul></details>` : ""}
  </section>`;
}

async function uploadVideoArtifact(event) {
  event.preventDefault();
  const form = event.currentTarget;
  const file = form.querySelector('input[name="file"]')?.files?.[0];
  if (!file) return;
  if (file.size > 256 * 1024 * 1024) { toast("ไฟล์วิดีโอเกิน 256 MiB", true); return; }
  const button = form.querySelector('button[type="submit"]');
  button.disabled = true;
  button.textContent = "กำลังตรวจและนำเข้า…";
  try {
    const upload = new FormData(form);
    await api("/api/media/videos", {method:"POST", body:upload});
    toast("นำเข้าวิดีโอแล้ว");
    await load();
    switchTab("artifacts");
  } catch (error) {
    const message = /up to 2 MiB/i.test(error.message)
      ? "ไฟล์นี้ใหญ่เกิน 2 MiB สำหรับตัวแก้ไข เลือกไฟล์ข้อความที่เล็กกว่านี้"
      : error.message;
    toast(message, true);
  }
  finally { if (button.isConnected) { button.disabled = false; button.textContent = "นำเข้าวิดีโอ"; } }
}

async function loadVideoEvidence(artifactID) {
  try {
    const jobs = asList(await api(`/api/media/jobs?source_artifact_id=${encodeURIComponent(artifactID)}`));
    videoEvidenceJobs.set(artifactID,jobs);
    if (jobs.some(job => job.state === "completed")) state.artifacts = asList(await api("/api/artifacts"));
    await Promise.all(jobs.filter(job => job.processor_kind === "audio_transcribe" && job.state === "completed").flatMap(job => job.result_artifact_ids || []).filter(id => !videoTranscriptBodies.has(id)).map(async id => {
      try { videoTranscriptBodies.set(id, await api(`/api/artifacts/${encodeURIComponent(id)}/content`)); }
      catch (error) { videoTranscriptBodies.set(id, {error:error.message}); }
    }));
    renderArtifacts();
  } catch (error) { toast(error.message,true); }
}

async function startVideoTranscription(artifactID, retry=false) {
  const source = state.artifacts.find(item => item.id === artifactID && item.kind === "video");
  if (!source) return;
  const approved = await askAction({title:"ถอดคำพูดจากวิดีโอนี้?",message:`Hermetrix จะถอดเสียงจาก ${source.name} ด้วยโมเดลในเครื่อง และบันทึกข้อความพร้อมเวลา คำที่ได้อาจคลาดเคลื่อน โปรดตรวจเทียบวิดีโอก่อนนำไปใช้`,confirmLabel:"เริ่มถอดเสียง"});
  if (!approved) return;
  try {
    await api("/api/media/jobs",{method:"POST",body:JSON.stringify({source_artifact_id:artifactID,processor_kind:"audio_transcribe",operation_id:`audio_transcribe:${artifactID}`,idempotency_key:retry ? `builtin-whisper-cpp-v1-${crypto.randomUUID()}` : "builtin-whisper-cpp-v1"})});
    toast("กำลังถอดเสียง ตรวจสถานะได้ในหน้านี้");
    await loadVideoEvidence(artifactID);
  } catch (error) { toast(error.message,true); }
}

async function cancelVideoTranscription(jobID, artifactID) {
  try { await api(`/api/media/jobs/${encodeURIComponent(jobID)}/cancel`,{method:"POST",body:"{}"}); toast("ส่งคำขอหยุดถอดเสียงแล้ว"); await loadVideoEvidence(artifactID); }
  catch (error) { toast(error.message,true); }
}

async function startVideoFrameEvidence(artifactID, retry=false) {
  const source = state.artifacts.find(item => item.id === artifactID && item.kind === "video");
  if (!source) return;
  const approved = await askAction({title:"สร้างเฟรมตัวอย่างจากวิดีโอนี้?",message:`Hermetrix จะถอดภาพสามช่วงเวลาจาก ${source.name} และบันทึกเป็นหลักฐานในเครื่อง การตรวจนี้ไม่วิเคราะห์เสียงหรือทุกเฟรม`,confirmLabel:"สร้างเฟรมตัวอย่าง"});
  if (!approved) return;
  try {
    await api("/api/media/jobs",{method:"POST",body:JSON.stringify({source_artifact_id:artifactID,processor_kind:"video_extract",operation_id:`video_extract:${artifactID}`,idempotency_key:retry ? `builtin-video-frames-v1-${crypto.randomUUID()}` : "builtin-video-frames-v1"})});
    toast("กำลังสร้างเฟรมตัวอย่าง ตรวจสถานะได้ในหน้านี้");
    await loadVideoEvidence(artifactID);
  } catch (error) { toast(error.message,true); }
}

function renderBrowserInspection(inspection) {
  if (!inspection) return "";
  const resources=inspection.resources||[];
  const api=resources.filter(item=>["fetch","xmlhttprequest"].includes(item.initiator_type));
  const lines=items=>items.length?items.join("\n"):"None observed yet. Interact with the page and inspect again.";
  return `<section class="inspect-section"><h3>Passive page inspection</h3><p class="form-note neutral">Shows the current DOM and resources available in this page's timing buffer. A route or failed request is a lead to verify, not a confirmed bug.</p>
    <details open><summary>Observed fetch/XHR (${api.length})</summary><pre>${escapeHTML(lines(api.map(item=>`${item.status?`HTTP ${item.status}`:"Status unavailable"} · ${item.url}`)))}</pre></details>
    <details><summary>Console errors seen in tab (${(inspection.page_errors||[]).length})</summary><pre>${escapeHTML(lines(inspection.page_errors||[]))}</pre></details>
    <details><summary>Other loaded resources (${resources.length-api.length})</summary><pre>${escapeHTML(lines(resources.filter(item=>!["fetch","xmlhttprequest"].includes(item.initiator_type)).map(item=>`${item.initiator_type} · ${item.status?`HTTP ${item.status}`:"Status unavailable"} · ${item.url}`)))}</pre></details>
    <details><summary>Routes and forms</summary><pre>${escapeHTML(lines([...(inspection.routes||[]),...(inspection.forms||[]).map(item=>`${item.method} form → ${item.action}`)]))}</pre></details>
    <details><summary>Client scripts (${(inspection.scripts||[]).length})</summary><pre>${escapeHTML(lines((inspection.scripts||[]).map(item=>item.url||`Inline script: ${item.inline_excerpt||"(empty)"}`)))}</pre></details>
    <details><summary>DOM excerpt</summary><pre>${escapeHTML(inspection.dom_excerpt||"No DOM captured.")}</pre></details>
  </section>`;
}

function bindWorkbenchBrowserEvents() {
  // The app CSP blocks HTML style attributes. Position the fresh-observation
  // hotspots through CSSOM so the visible screenshot and its controls align.
  $$(".browser-shot-hotspot").forEach(button => {
    for (const [property, dataName] of [["left","xPct"],["top","yPct"],["width","widthPct"],["height","heightPct"]]) {
      const value = Number(button.dataset[dataName]);
      if (Number.isFinite(value)) button.style[property] = `${Math.max(0, Math.min(100, value))}%`;
    }
  });
  $("#browserOpenForm")?.addEventListener("submit",openWorkbenchBrowser);
  $("#browserKeyForm")?.addEventListener("submit",event=>{event.preventDefault();const form=new FormData(event.currentTarget);browserWorkbenchAction("key",Number(form.get("ref")),String(form.get("key")));});
  $$('[data-browser-id]').forEach(button=>button.addEventListener("click",()=>{state.selectedBrowserTab=button.dataset.browserId;refreshWorkbenchSurface("browser");}));
  $$('[data-browser-action]').forEach(button=>button.addEventListener("click",()=>browserWorkbenchAction(button.dataset.browserAction)));
  $$('[data-browser-click]').forEach(button=>button.addEventListener("click",()=>browserWorkbenchAction("click",Number(button.dataset.browserClick))));
  $$('[data-browser-type]').forEach(button=>button.addEventListener("click",()=>typeBrowserElement(Number(button.dataset.browserType))));
  $("#desktopListWindows")?.addEventListener("click", listDesktopWindows);
  $("#desktopWindowSelect")?.addEventListener("change", event => { state.desktopSelectedWindow = event.target.value; state.desktopObservation = null; refreshWorkbenchSurface("browser"); });
  $("#desktopObserveWindow")?.addEventListener("click", observeDesktopWindow);
  $$('[data-desktop-action]').forEach(button => button.addEventListener("click", () => performDesktopAction(button.dataset.desktopAction, Number(button.dataset.desktopRef))));
  $("#desktopStopAction")?.addEventListener("click", stopDesktopAction);
}

let desktopActionAbortController = null;

function stopDesktopAction() {
  if (!desktopActionAbortController || !state.desktopActionPending) return;
  desktopActionAbortController.abort();
  state.desktopError = "หยุดการรอผลแล้ว คำสั่งอาจเกิดขึ้นไปแล้ว โปรดตรวจหน้าต่างและผลในงานก่อนทำต่อ";
  refreshWorkbenchSurface("browser");
}

async function performDesktopAction(action, ref) {
  const observed = state.desktopObservation;
  const target = observed?.elements?.find(item => item.ref === ref);
  if (!target || !observed.observation_hash) return;
  state.desktopError = "";
  const textValue = action === "set_value" ? String($(`[data-desktop-value-ref="${ref}"]`)?.value || "") : "";
  const task = state.durableTasks.find(item => item.id === state.selectedDurableTask && item.project_id === state.currentProject?.id);
  try {
    const prepared = await api("/api/desktop/actions", {method:"POST", body:JSON.stringify({
      task_id:task?.id || "", handle:observed.window.handle, process_id:observed.window.process_id, action,
      element_ref:ref, text:textValue, observation_hash:observed.observation_hash})});
    const confirmed = await askAction({title:action === "invoke" ? "ยืนยันการกดใน Windows" : "ยืนยันการพิมพ์ใน Windows",
      message:`หน้าต่าง: ${observed.window.title} · PID ${observed.window.process_id}\nรายการ: ${target.name || target.automation_id || target.control_type}${action === "set_value" ? `\nข้อความ: ${textValue}` : ""}\nการยืนยันใช้กับรายการนี้และภาพหน้าต่างฉบับที่ตรวจเท่านั้น`,
      confirmLabel:action === "invoke" ? "กดรายการนี้" : "พิมพ์ข้อความนี้"});
    if (!confirmed) {
      await api(`/api/desktop/actions/${encodeURIComponent(prepared.id)}/cancel`, {method:"POST", body:JSON.stringify({subject_hash:prepared.subject_hash})});
      if (task) await loadTaskExecution(task.id);
      return;
    }
    const controller = new AbortController();
    desktopActionAbortController = controller;
    state.desktopActionPending = {id:prepared.id, task_id:task?.id || ""};
    refreshWorkbenchSurface("browser");
    let receipt;
    try {
      receipt = await api(`/api/desktop/actions/${encodeURIComponent(prepared.id)}/confirm`, {method:"POST", signal:controller.signal,
        body:JSON.stringify({subject_hash:prepared.subject_hash, text:textValue, actor:currentActor()})});
    } catch (error) {
      if (!controller.signal.aborted) throw error;
      try {
        const current = await api(`/api/desktop/actions/${encodeURIComponent(prepared.id)}`);
        if (current.state === "prepared") await api(`/api/desktop/actions/${encodeURIComponent(prepared.id)}/cancel`, {method:"POST", body:JSON.stringify({subject_hash:prepared.subject_hash})});
      } catch (_) { /* Keep the uncertain receipt for manual inspection. */ }
      if (task) await loadTaskExecution(task.id);
      return;
    } finally {
      if (desktopActionAbortController === controller) desktopActionAbortController = null;
      state.desktopActionPending = null;
      refreshWorkbenchSurface("browser");
    }
    state.desktopObservation = receipt.observation || null;
    if (receipt.state !== "completed") state.desktopError = `ผลคำสั่งไม่แน่นอน: ${receipt.error || "โปรดตรวจหน้าต่างก่อนทำต่อ"}`;
    else toast("ตรวจผลหลังคำสั่งแล้ว");
    if (task) await loadTaskExecution(task.id);
    refreshWorkbenchSurface("browser");
  } catch (error) { state.desktopError = error.message; refreshWorkbenchSurface("browser"); }
}

async function listDesktopWindows() {
  state.desktopError = "";
  try {
    state.desktopWindows = await api("/api/desktop/windows");
    state.desktopWindowsLoaded = true;
    if (!state.desktopWindows.some(item => `${item.process_id}:${item.handle}` === state.desktopSelectedWindow)) {
      state.desktopSelectedWindow = state.desktopWindows.length ? `${state.desktopWindows[0].process_id}:${state.desktopWindows[0].handle}` : "";
      state.desktopObservation = null;
    }
  } catch (error) { state.desktopError = error.message; state.desktopWindowsLoaded = true; }
  refreshWorkbenchSurface("browser");
}

async function observeDesktopWindow() {
  const [processID, handle] = String(state.desktopSelectedWindow || $("#desktopWindowSelect")?.value || "").split(":").map(Number);
  if (!processID || !handle) return;
  state.desktopError = "";
  state.desktopObservation = null;
  refreshWorkbenchSurface("browser");
  try {
    const observed = await api("/api/desktop/observe", {method:"POST", body:JSON.stringify({process_id:processID, handle})});
    if (state.desktopSelectedWindow === `${processID}:${handle}`) state.desktopObservation = observed;
  } catch (error) { state.desktopError = error.message; }
  refreshWorkbenchSurface("browser");
}

function renderWorkbenchBrowser() {
  $("#workbenchContent").innerHTML = renderWorkbenchBrowserHTML();
  bindWorkbenchBrowserEvents();
}

async function openWorkbenchBrowser(event){event.preventDefault();const form=new FormData(event.currentTarget);try{const tab=await api("/api/browser/tabs",{method:"POST",body:JSON.stringify({project_id:form.get("project_id"),url:form.get("url"),allow_private:form.get("allow_private")==="on",actor:currentActor()})});state.browserTabs.unshift(tab);state.selectedBrowserTab=tab.id;refreshWorkbenchSurface("browser");}catch(error){toast(error.message,true);}}
async function browserWorkbenchAction(action,ref=0,text="") {
  const tab=state.browserTabs.find(item=>item.id===state.selectedBrowserTab);
  if(!tab)return;
  const url=action==="navigate"?new FormData($("#browserOpenForm")).get("url"):"";
  const changesPage = action === "click" || action === "type" || action === "key";
  let prepared = null;
  const task = state.durableTasks.find(item => item.id === state.selectedDurableTask && item.project_id === tab.project_id);
  if(changesPage) {
    const element=tab.elements?.find(item=>item.ref===ref);
    if(!element) { toast("ตำแหน่งบนหน้าเปลี่ยนแล้ว โปรดอ่านหน้าใหม่",true); return; }
    try {
      prepared=await api(`/api/browser/tabs/${encodeURIComponent(tab.id)}/actions/prepare`,{method:"POST",body:JSON.stringify({action,ref,text,observation_hash:tab.observation_hash,task_id:task?.id || "",actor:currentActor()})});
      const confirmed=await askAction({title:action === "click" ? "ยืนยันการคลิกในเบราว์เซอร์" : action === "key" ? "ยืนยันการกดปุ่มในเบราว์เซอร์" : "ยืนยันการพิมพ์ในเบราว์เซอร์",message:`หน้า ${tab.url}\nรายการ ${prepared.target?.text||prepared.target?.placeholder||prepared.target?.tag||ref}\n${action === "type" ? `ข้อความที่จะพิมพ์: ${text}\n` : action === "key" ? `ปุ่มที่จะกด: ${text}\n` : ""}ภาพก่อนทำ: ${prepared.screenshot_before_id || "ไม่พร้อม"}\nคำสั่งนี้อาจเปลี่ยนข้อมูลบนเว็บ ตรวจหน้าและคำสั่งก่อนยืนยัน`,confirmLabel:action === "click" ? "คลิก" : action === "key" ? "กดปุ่ม" : "พิมพ์"});
      if(!confirmed) { await api(`/api/browser/actions/${encodeURIComponent(prepared.id)}/cancel`,{method:"POST",body:JSON.stringify({subject_hash:prepared.subject_hash})}); return; }
    } catch(error) { if(prepared) await api(`/api/browser/actions/${encodeURIComponent(prepared.id)}/cancel`,{method:"POST",body:JSON.stringify({subject_hash:prepared.subject_hash})}).catch(()=>{}); toast(error.message,true); return; }
  }
  if(state.selectedBrowserTab!==tab.id || state.browserTabs.find(item=>item.id===tab.id)?.updated_at!==tab.updated_at) {
    if(prepared) await api(`/api/browser/actions/${encodeURIComponent(prepared.id)}/cancel`,{method:"POST",body:JSON.stringify({subject_hash:prepared.subject_hash})}).catch(()=>{});
    toast("หน้าเปลี่ยนระหว่างรอยืนยัน โปรดตรวจอีกครั้ง",true); return;
  }
  try {
    const updated=await api(`/api/browser/tabs/${encodeURIComponent(tab.id)}/actions`,{method:"POST",body:JSON.stringify({action,url,ref,text,observation_hash:tab.observation_hash,task_id:task?.id || "",receipt_id:prepared?.id || "",subject_hash:prepared?.subject_hash || "",actor:currentActor()})});
    state.browserTabs=state.browserTabs.map(item=>item.id===updated.id?updated:item);
    state.selectedBrowserTab=updated.id;
    refreshWorkbenchSurface("browser");
    if(task) await loadTaskExecution(task.id);
  } catch(error){toast(error.message,true); if(task) await loadTaskExecution(task.id).catch(()=>{});}
}
async function typeBrowserElement(ref){const text=await askAction({title:`ข้อความสำหรับรายการ ${ref}`,message:"พิมพ์ข้อความที่จะใช้ แล้วระบบจะแสดงคำสั่งให้ตรวจและยืนยันอีกครั้งก่อนเปลี่ยนข้อมูลบนเว็บ",confirmLabel:"ตรวจคำสั่ง",reasonLabel:"ข้อความ"});if(text===null)return;await browserWorkbenchAction("type",ref,text);}

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
  if (id === "editor" && !state.projectFile) { openContentPane("files", {persist}); return; }
  if (id === "editor" && state.view === "code" && state.paneLayout === "ide" && !state.panes.includes("editor")) {
    const placeholder = state.panes.indexOf("review");
    if (placeholder >= 0) state.panes[placeholder] = "editor";
  }
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
    const beforeFiles = id === "editor" ? state.panes.indexOf("files") : -1;
    if (beforeFiles >= 0) state.panes.splice(beforeFiles, 0, id);
    else state.panes.push(id);
  }
  state.maximisedPane = null;
  if (state.view === "chat") collapseZone("side", false);
  renderPanes();
  revealWorkspacePane(id);
  if (persist) saveLayout();
}

function revealWorkspacePane(id) {
  if (compactWorkspace()) return;
  const index = state.panes.indexOf(id);
  const pane = index >= 0 ? $(`#workspacePaneHost .pane[data-pane="${index}"]`) : null;
  pane?.scrollIntoView({block:"nearest", inline:"start", behavior:"smooth"});
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
const taskBrowserVerificationTabs = new Map();
const taskSocialCreatorInfo = new Map();
const taskSocialExportDrafts = new Map();
const taskErrors = new Map();
const taskRevisionDrafts = new Map();
const taskClarificationDrafts = new Map();
const TASK_CLARIFICATION_DRAFT_PREFIX = "hermetrix.task.clarification.v1:";
function taskClarificationDraft(task) {
  if (!task) return {question:"", answer:""};
  const fallback = {question:task.requirement?.unknowns?.[0] || "", answer:""};
  const cached = taskClarificationDrafts.get(task.id);
  if (cached && cached.requirementRevision === task.active_requirement_revision &&
      task.requirement?.unknowns?.includes(cached.question)) return cached;
  if (typeof sessionStorage === "undefined") return fallback;
  try {
    const raw = sessionStorage.getItem(TASK_CLARIFICATION_DRAFT_PREFIX + task.id);
    if (!raw || raw.length > 4096) return fallback;
    const draft = JSON.parse(raw);
    if (draft.requirementRevision !== task.active_requirement_revision ||
        !task.requirement?.unknowns?.includes(draft.question) ||
        typeof draft.answer !== "string" || draft.answer.length > 2000) return fallback;
    taskClarificationDrafts.set(task.id, {requirementRevision:draft.requirementRevision,
      question:draft.question, answer:draft.answer});
    return taskClarificationDrafts.get(task.id);
  } catch { return fallback; }
}
function persistTaskClarificationDraft(task, draft) {
  taskClarificationDrafts.set(task.id, {...draft, requirementRevision:task.active_requirement_revision});
  if (typeof sessionStorage === "undefined") return;
  try {
    sessionStorage.setItem(TASK_CLARIFICATION_DRAFT_PREFIX + task.id, JSON.stringify({
      requirementRevision:task.active_requirement_revision, question:draft.question, answer:draft.answer}));
  } catch { /* The draft remains in memory if this browser blocks session storage. */ }
}
function clearTaskClarificationDraft(taskID) {
  taskClarificationDrafts.delete(taskID);
  if (typeof sessionStorage === "undefined") return;
  try { sessionStorage.removeItem(TASK_CLARIFICATION_DRAFT_PREFIX + taskID); } catch {}
}
const taskPlanReviewFeedbacks = new Map();
const taskResearchDrafts = new Map();
const taskContentBriefDrafts = new Map();
const taskContentClaimDrafts = new Map();
const taskOptionalToolsOpen = new Set();
const taskContentDraftEdits = new Map();
const taskComfyPaths = new Map();
const taskComfyPreflights = new Map();
const taskExecutionLoadSerial = new Map();
let pendingTaskJump = null;
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
  if (lab.loading && !lab.loaded) return `<details class="inspect-section"><summary>ประเมินการตัดสินใจของโมเดลในเครื่อง</summary><p role="status">กำลังอ่านหลักฐานการตัดสินใจ…</p></details>`;
  return `<details class="inspect-section task-decision-lab"><summary>ประเมินการตัดสินใจของโมเดลในเครื่อง ${policy?.enabled ? pill("แนะนำงานอ่านได้", "green") : pill("เปรียบเทียบอย่างเดียว", "blue")}</summary>
    <p>เทียบผลของโมเดลในเครื่องกับกฎของระบบ เมื่อเปิดใช้ โมเดลแนะนำได้เฉพาะการอ่านที่ไม่ติดนโยบาย และไม่สามารถเรียกเครื่องมือหรือแก้ไฟล์</p>
    ${lab.error ? `<p class="session-error" role="alert">${escapeHTML(lab.error)}</p>` : ""}
    <label>โมเดลในเครื่อง<select id="taskDecisionProvider">${taskProviderOptions(locals, policy?.provider_id || latest?.provider_id)}</select></label>
    ${progress ? `<div class="kv"><span>จำนวนครั้งที่ลอง</span><strong>${progress.attempts}/20</strong><span>เหลือลองได้</span><strong>${progress.remaining_attempts}</strong><span>เหลือส่งให้ผู้วางแผน</span><strong>${progress.remaining_planner_escalations}</strong><span>ความคืบหน้า</span><strong>${progress.budget_exhausted ? "ใช้จำนวนครั้งครบแล้ว" : progress.no_progress ? "ล้มเหลวซ้ำ" : "ยังอยู่ในขอบเขต"}</strong></div>${(progress.blocked_reasons || []).length ? `<p class="session-error">${escapeHTML(progress.blocked_reasons.join(" · "))}</p>` : ""}` : ""}
    <details><summary>ความรู้ของโปรเจกต์ที่พบ · ${knowledge.length}</summary>${knowledge.length ? knowledge.map(item => `<article class="artifact-mini"><strong>${escapeHTML(item.memory_kind)}</strong><p>${escapeHTML(item.snippet)}</p><small>คะแนน ${item.score} · ${escapeHTML((item.matched_terms || []).join(", "))}</small></article>`).join("") : `<p class="form-note neutral">ไม่พบความรู้ที่เกี่ยวข้องกับงานนี้</p>`}</details>
    <div class="action-row"><button class="ghost" id="taskDecisionShadow" ${locals.length ? "" : "disabled"}>เปรียบเทียบงานนี้</button><button class="ghost" id="taskDecisionBenchmark" ${locals.length ? "" : "disabled"}>ทดสอบเกณฑ์ 6 กรณี</button></div>
    ${shadow ? `<div class="kv"><span>ผลเปรียบเทียบล่าสุด</span><strong>${shadow.agreement ? "ตรงกัน" : shadow.valid ? "ไม่ตรงกัน" : "ผลใช้ไม่ได้"}</strong><span>กฎ</span><code>${escapeHTML(shadow.baseline?.action_id || "—")}</code><span>โมเดล</span><code>${escapeHTML(shadow.model?.action_id || shadow.model_error || "—")}</code><span>เวลา</span><span>${Number(shadow.latency_ms || 0).toLocaleString()} ms</span></div>` : `<p class="form-note neutral">ยังไม่มีผลเปรียบเทียบของงานนี้</p>`}
    ${lab.metrics ? `<p class="form-note neutral">เปรียบเทียบ ${lab.metrics.total_runs} งาน · ตรงกัน ${Math.round((lab.metrics.agreement_rate || 0) * 100)}% · เฉลี่ย ${Math.round(lab.metrics.average_latency_ms || 0)} ms</p>` : ""}
    ${latest ? `<div class="kv"><span>เกณฑ์ล่าสุด</span><strong>ถูก ${latest.correct_cases}/${latest.total_cases} กรณี</strong><span>ผลใช้ไม่ได้</span><strong>${Math.round((latest.invalid_rate || 0) * 100)}%</strong><span>เวลาเฉลี่ย</span><strong>${Math.round(latest.average_latency_ms || 0)} ms</strong><span>ผล</span><strong>${latest.passed ? "ผ่าน" : "ไม่ผ่าน"}</strong></div>` : `<p class="form-note neutral">ต้องทดสอบเกณฑ์คงที่ก่อนเปิดการแนะนำงานอ่าน</p>`}
    <div class="action-row">${policy?.enabled ? `<button class="danger" id="taskDecisionAdmission" data-enabled="false">ปิดการแนะนำงานอ่าน</button><button class="primary" id="taskDecisionRecommend">ขอคำแนะนำงานอ่าน</button>` : `<button class="primary" id="taskDecisionAdmission" data-enabled="true" ${latest?.passed ? "" : "disabled"}>เปิดการแนะนำงานอ่าน</button>`}</div>
    ${recommendation ? `<p class="task-readiness"><strong>คำแนะนำ:</strong> ${escapeHTML(recommendation.action_id)} · ${escapeHTML(recommendation.reason || "")} ${recommendation.fallback_used ? "(ใช้กฎของระบบแทน)" : ""}</p>` : ""}
  </details>`;
}

function projectDurableTasks() {
  return state.durableTasks.filter(item => !state.currentProject?.id || item.project_id === state.currentProject.id);
}

function taskBoardLane(task) {
  if (["completed", "cancelled"].includes(task.state)) return "done";
  if (["waiting_for_input", "paused", "failed"].includes(task.state)) return "blocked";
  if (task.state === "verifying" || ["pending_review", "awaiting_post_review"].includes(state.taskExecutions[task.id]?.proposal?.state)) return "review";
  if (task.state === "running") return "doing";
  return "planned";
}

function taskKanbanHTML(tasks, selectedID) {
  const lanes = [["planned", "วางแผน"], ["doing", "กำลังทำ"], ["review", "รอตรวจ"], ["blocked", "ติดขัด"], ["done", "เสร็จแล้ว"]];
  return `<div class="task-kanban" role="region" aria-label="บอร์ดงานของโปรเจกต์">${lanes.map(([id, label]) => {
    const cards = tasks.filter(task => taskBoardLane(task) === id);
    return `<section class="task-kanban-lane" aria-label="${label}"><header><h4>${label}</h4><span>${cards.length}</span></header><div class="task-kanban-cards">${cards.map(task => {
      const steps = task.plan?.steps || [];
      const done = steps.filter(step => step.state === "completed").length;
      return `<button type="button" class="task-kanban-card ${task.id === selectedID ? "selected" : ""}" data-board-task="${escapeHTML(task.id)}" aria-pressed="${task.id === selectedID}"><strong>${escapeHTML(task.title)}</strong><small>${escapeHTML(task.objective)}</small><span>${steps.length ? `${done}/${steps.length} ขั้น` : "ยังไม่มีแผน"}</span></button>`;
    }).join("") || `<p class="task-kanban-empty">ยังไม่มีงาน</p>`}</div></section>`;
  }).join("")}</div>`;
}

function taskProviderIsLocal(provider) {
  try {
    const host = new URL(provider.base_url).hostname.toLowerCase();
    return host === "localhost" || host === "[::1]" || /^127\.\d+\.\d+\.\d+$/.test(host) || /^\[::ffff:7f[0-9a-f]{2}:[0-9a-f]+\]$/.test(host);
  } catch { return false; }
}

function taskEligibleProviders(task, implementerID = "") {
  return state.providers.filter(item => item.enabled && item.credential_ready &&
    (task?.egress_policy === "remote_allowed" || taskProviderIsLocal(item)) &&
    (!implementerID || item.id !== implementerID));
}

function providerModelLabel(provider) {
  const model = String(provider?.model || "");
  return model.includes("\\") || model.includes("/") ? model.split(/[\\/]/).pop() || model : model;
}

function providerCredentialLabel(provider) {
  if (provider.credential_stored) return "คีย์บันทึกในเครื่อง";
  if (provider.api_key_env) return provider.credential_ready ? "คีย์จาก environment" : "ยังไม่มีคีย์จาก environment";
  return taskProviderIsLocal(provider) ? "ตั้งค่าแบบไม่ใช้คีย์" : "ไม่ได้ระบุคีย์ · ต้องทดสอบปลายทาง";
}

function providerOptionLabel(provider) {
  return `${provider.name} · ${providerModelLabel(provider)} · ${taskProviderIsLocal(provider) ? "ในเครื่อง" : "ภายนอก"}${provider.credential_ready ? "" : " · ยังไม่มีคีย์"}`;
}

function taskProviderOptions(providers, preferredID = state.defaultProviderID || "") {
  return providers.map(item => `<option value="${escapeHTML(item.id)}" ${item.id === preferredID ? "selected" : ""}>${escapeHTML(providerOptionLabel(item))}</option>`).join("");
}

function taskReviewIdentityText(implementerID, reviewerID) {
  const implementer = state.providers.find(item => item.id === implementerID);
  const reviewer = state.providers.find(item => item.id === reviewerID);
  if (!implementer || !reviewer) return "เลือกโปรไฟล์ผู้ตรวจเพื่อดูว่าใช้โมเดลและปลายทางใด";
  const sameModel = implementer.model === reviewer.model;
  const sameEndpoint = String(implementer.base_url || "").replace(/\/+$/, "") === String(reviewer.base_url || "").replace(/\/+$/, "");
  if (sameModel && sameEndpoint) return "ตรวจรอบใหม่ด้วยโปรไฟล์แยก แต่ใช้โมเดลและปลายทางเดียวกับผู้แก้ไข ผลนี้ไม่ใช่การตรวจด้วยโมเดลต่างตัว";
  if (sameModel) return "ตรวจรอบใหม่ด้วยโมเดลชื่อเดียวกันบนปลายทางต่างกัน ตรวจหลักฐานและข้อเสนอด้วยตนเองก่อนรับผล";
  return "ตรวจรอบใหม่ด้วยโมเดลต่างตัว ตรวจหลักฐานและข้อเสนอด้วยตนเองก่อนรับผล";
}

function taskRunIsLive(run) {
  return run?.state === "running" && !!run.lease_token && Date.parse(run.lease_expires_at) > Date.now();
}

function taskPlanAwaitsReview(task) {
  return Boolean(task?.requirement?.contract?.require_plan_review && task.plan?.review_status === "pending" && task.state !== "completed");
}

function taskCanStartStep(task, execution) {
  if ((execution.effects || []).some(effect => ["planned", "dispatched", "uncertain"].includes(effect.state))) return false;
  if (execution.attempt?.state === "running" && (execution.effects || []).some(effect => effect.action === "provider.propose" && effect.state === "observed" && effect.receipt?.status === "failed")) return false;
  const currentRun = !execution.run || execution.run.plan_revision === task.active_plan_revision;
  const rejectedRetryReady = execution.proposal?.state === "rejected" && execution.run?.state === "failed" && execution.attempt?.state === "failed";
  if (currentRun && execution.proposal && !["verified", "post_review_rejected"].includes(execution.proposal.state) && !rejectedRetryReady) return false;
  return task.state === "ready" || task.state === "paused" ||
    (task.state === "running" && taskRunIsLive(execution.run) &&
      (!execution.attempt || ["running", "completed"].includes(execution.attempt.state)));
}

function taskStage(task, execution) {
  if (task.state === "completed") return {number:5, label:"เสร็จแล้ว", detail:"หลักฐานของเป้าหมายฉบับนี้ผ่านการตรวจแล้ว"};
  if (!task.active_plan_revision) return {number:2, label:"วางแผน", detail:"สร้างขั้นตอนและตรวจขอบเขตกับวิธีพิสูจน์ก่อนเริ่มงาน"};
  if (taskPlanAwaitsReview(task)) return {number:2, label:"ทบทวนแผน", detail:"ตรวจขั้นตอน เกณฑ์ และวิธีพิสูจน์ก่อนอนุมัติให้เริ่มงาน"};
  if (task.plan?.review_status === "rejected") return {number:2, label:"แก้แผน", detail:"แผนฉบับนี้ถูกส่งกลับ แก้ Goal หรือสร้างแผนฉบับใหม่"};
  if (task.state === "verifying") return {number:5, label:"ตรวจผล", detail:"ตรวจหลักฐานปัจจุบันและแก้รายการที่ยังไม่ผ่านก่อนปิดงาน"};
  const proposal = execution?.run?.plan_revision === task.active_plan_revision ? execution.proposal : null;
  if (proposal?.state === "awaiting_post_review") return {number:5, label:"รอตรวจทาน", detail:"ผลตรวจผ่านแล้ว ต้องตรวจหลักฐานอีกรอบก่อนปิดงาน"};
  if (proposal?.state === "applied") return {number:4, label:"รันการตรวจ", detail:"บันทึกการเปลี่ยนแปลงแล้ว ให้รัน checks ในแผนเพื่อเก็บหลักฐาน"};
  if (proposal && ["pending_review", "approved"].includes(proposal.state)) return {number:3, label:"ตรวจการแก้ไข", detail:"อ่านไฟล์ที่เสนอให้ครบก่อนอนุมัติและนำไปใช้"};
  const failedStep = task.plan?.steps?.find(step => step.state === "failed");
  if (failedStep) return {number:3, label:"ขั้นตอนติดขัด", detail:`ตรวจสาเหตุของ “${failedStep.title}” แล้วเริ่มรอบแก้ไขจากหลักฐานที่บันทึกไว้`};
  return {number:3, label:"ทำตามแผน", detail:"ทำทีละขั้นและตรวจผลก่อนเดินหน้าต่อ"};
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
  const link = `<a class="button-link" href="/api/artifacts/${encodeURIComponent(proposal.artifact_id)}/content" target="_blank" rel="noreferrer">เปิดข้อเสนอฉบับเต็ม</a>`;
  if (!content) return `<p role="status">กำลังโหลดข้อเสนอแก้ไฟล์…</p>${link}`;
  if (content.error) return `<p class="session-error" role="alert">ยังโหลดข้อเสนอไม่ได้: ${escapeHTML(content.error)}</p><button class="ghost" id="taskReload">ลองโหลดอีกครั้ง</button>${link}`;
  return `<section class="task-proposal-preview"><h4>${escapeHTML(content.summary || "ข้อเสนอแก้ไฟล์")}</h4>
    ${(content.risks || []).length ? `<p class="form-note">ความเสี่ยง: ${escapeHTML(content.risks.join(" · "))}</p>` : ""}
    ${(content.assumptions || []).length ? `<p>ข้อสมมติ: ${escapeHTML(content.assumptions.join(" · "))}</p>` : ""}
    ${(content.changes || []).map(change => `<details class="tool-receipt"><summary><strong>${escapeHTML(change.path)}</strong><span>ไฟล์ที่เสนอแก้</span></summary><pre>${escapeHTML(change.content)}</pre></details>`).join("")}${link}</section>`;
}

function taskPlanReferenceNote(task) {
  if (taskPlanAwaitsReview(task)) return "แผนนี้รอการยืนยันก่อนเริ่มงาน";
  if (task.state === "completed") return "งานตามแผนนี้เสร็จแล้ว ตรวจผลและหลักฐานได้ด้านล่าง";
  if ((task.plan?.steps || []).some(step => step.state === "failed")) return "แผนนี้ใช้แล้ว แต่มีขั้นตอนตรวจไม่ผ่าน ตรวจสาเหตุและหลักฐานก่อนเริ่มรอบใหม่";
  return "แผนนี้ใช้อ้างอิงระหว่างทำงาน ผลสำเร็จจะยืนยันจากหลักฐานการตรวจ";
}

function taskFriendlyError(error) {
  const message = String(error || "");
  if (message.includes("invalid proposal JSON")) return "โมเดลส่งข้อเสนอในรูปแบบที่ระบบอ่านไม่ได้ ระบบยังไม่เขียนไฟล์ เลือกไฟล์หรือโมเดลแล้วเริ่มรอบใหม่ได้";
  if (message.includes("provider proposal failed with observed receipt")) return "ข้อเสนอรอบก่อนล้มเหลวและบันทึกผลไว้แล้ว ตรวจรายละเอียดก่อนเริ่มรอบใหม่";
  if (message.includes("completion blocked by User acceptance")) return "ผลตรวจครบแล้ว รอคุณตรวจและรับรองก่อนปิดงาน";
  if (message.startsWith("mandatory check ") && message.endsWith(" failed")) return `วิธีตรวจ ${message.slice(16, -7)} ไม่ผ่าน`;
  return message;
}

function taskExecutionHTML(task, execution) {
  if (!execution) return `<div class="probe-empty" role="status">กำลังโหลดความคืบหน้าของงาน…</div>`;
  if (execution.error) return `<div class="probe-empty">${escapeHTML(taskFriendlyError(execution.error))} <button class="ghost" id="taskReload">ลองโหลดอีกครั้ง</button></div>`;
  const proposal = execution.proposal;
  const providers = taskEligibleProviders(task);
  const uncertainEffects = (execution.effects || []).filter(effect => effect.state === "uncertain");
  const failedProposal = (execution.effects || []).some(effect => effect.action === "provider.propose" && effect.state === "observed" && effect.receipt?.status === "failed") && proposal?.attempt_id !== execution.attempt?.id && ["running","interrupted"].includes(execution.attempt?.state);
  const busy = taskActions.get(task.id);
  const expired = task.state === "running" && execution.run?.plan_revision === task.active_plan_revision && !taskRunIsLive(execution.run);
  let action = "";
  if (uncertainEffects.length) {
    action = `<p>มี ${uncertainEffects.length} การกระทำที่ต้องตรวจหลังรีสตาร์ต ระบบจะอ่านหลักฐานที่บันทึกไว้โดยไม่เรียกการกระทำนั้นซ้ำ</p><button class="primary" id="taskReconcileEffects">ตรวจผลที่บันทึกไว้</button>`;
  } else if (task.state === "paused" && execution.run?.state === "paused" && execution.attempt?.state === "interrupted" && proposal?.state === "awaiting_post_review") {
    action = `<p>การตรวจโมเดลถูกขัดจังหวะ ผลทดสอบและไฟล์ยังอยู่ครบ ตรวจหลักฐานที่บันทึกไว้ แล้วต่อการรีวิวด้วยสิทธิ์รอบใหม่</p><button class="primary" id="taskResumeObservedReview">ต่อการรีวิวที่ค้าง</button>`;
  } else if (task.state === "paused" && execution.run?.state === "paused" && execution.attempt?.state === "completed" && proposal?.state === "awaiting_post_review") {
    action = `<p>โมเดลตรวจผ่านและบันทึกผลแล้ว แต่การปิดขั้นถูกขัดจังหวะ ตรวจหลักฐานที่บันทึกไว้แล้วดำเนินการต่อโดยไม่เรียกโมเดลซ้ำ</p><button class="primary" id="taskResumeApprovedReview">ต่อการปิดขั้นที่ตรวจผ่าน</button>`;
  } else if (failedProposal) {
    action = `<p>โมเดลสร้างข้อเสนอไม่สำเร็จและมีผลล้มเหลวที่บันทึกไว้แล้ว ปิดรอบนี้เพื่อเลือกโมเดลหรือไฟล์ใหม่โดยไม่เรียกคำขอเดิมซ้ำ</p><button class="primary" id="taskRetryFailedProposal">เริ่มรอบใหม่หลังข้อเสนอล้มเหลว</button>`;
  } else if (proposal?.state === "rejected" && ["running", "interrupted"].includes(execution.attempt?.state)) {
    action = `<p>ข้อเสนอถูกปฏิเสธแล้ว ระบบจะเก็บเหตุผลไว้ให้โมเดลใช้แก้ในรอบใหม่ โดยยังอ้างอิงแผนเดิม</p><button class="primary" id="taskRetryRejectedProposal">แก้ข้อเสนอแล้วลองใหม่</button>${taskProposalHTML(proposal)}`;
  } else if (expired || execution.leaseError) {
    action = `<p class="session-error" role="alert">${escapeHTML(execution.leaseError || "สิทธิ์ของรอบงานหมดอายุ ข้อเสนอและหลักฐานยังอยู่ ต้องกู้รอบงานก่อนทำต่อ")}</p><button class="primary" id="taskRecoverExpiredRun">กู้งานที่บันทึกไว้</button>`;
  } else if (taskCanStartStep(task, execution)) {
    const nextStep = (task.plan?.steps || []).find(step => step.state !== "completed" && step.state !== "skipped");
    const priorFailure = nextStep?.state === "failed" ? `<p class="session-error" role="alert">${escapeHTML(taskFriendlyError(nextStep.last_error || "ขั้นตอนก่อนหน้าตรวจไม่ผ่าน"))}. ${proposal?.state === "verification_failed" ? "ระบบพยายามคืนไฟล์จากหลักฐานก่อนแก้แล้ว ตรวจไฟล์และผลคำสั่งก่อนเริ่มรอบใหม่" : "ตรวจหลักฐานก่อนเริ่มรอบใหม่"}</p>` : "";
    action = `${priorFailure}<form id="taskProposalForm"><h4>${proposal?.state === "verified" ? "เริ่มขั้นถัดไป" : proposal?.state === "post_review_rejected" ? "แก้แล้วลองขั้นนี้ใหม่" : nextStep?.state === "failed" ? "เริ่มรอบแก้ไขขั้นนี้" : "เริ่มขั้นนี้"}${nextStep ? ` · ${escapeHTML(nextStep.title)}` : ""}</h4>
      <label>โมเดล<select name="provider_id" required>${taskProviderOptions(providers, $("#chatProvider")?.value)}</select></label>
      <details><summary>เลือกไฟล์เอง</summary><label>ระบุ path ภายในโปรเจกต์ บรรทัดละไฟล์<textarea name="files" rows="3" placeholder="เว้นว่างเพื่อให้ระบบเลือกไฟล์จากโปรเจกต์&#10;internal/service.go"></textarea></label></details>
      <p>ตรวจขอบเขตและวิธีพิสูจน์ในแผนก่อนเริ่ม ข้อเสนอที่สร้างจะเปิดให้ทบทวนก่อนนำไปใช้</p>
      <button class="primary" ${providers.length ? "" : "disabled"}>${proposal?.state === "verified" ? "เริ่มขั้นถัดไป" : "สร้างข้อเสนอให้ตรวจ"}</button></form>`;
  } else if (proposal?.state === "pending_review") {
    const ready = taskProposalBodies.get(proposal.artifact_id)?.changes?.length;
    action = `${taskProposalHTML(proposal)}<div class="action-row"><button class="primary" data-task-proposal-decision="approved" ${ready ? "" : "disabled"}>อนุมัติข้อเสนอ</button><button class="danger" data-task-proposal-decision="rejected">ส่งข้อเสนอกลับ</button></div>`;
  } else if (proposal?.state === "approved") {
    action = `${taskProposalHTML(proposal)}<button class="primary" id="taskApplyProposal">เขียนไฟล์ตามข้อเสนอ</button>`;
  } else if (proposal?.state === "applied") {
    action = `<p>รันวิธีตรวจตามแผนและบันทึกผล หากตรวจไม่ผ่าน ระบบจะพยายามคืนไฟล์จากหลักฐานก่อนแก้</p><button class="primary" id="taskVerifyProposal">รันวิธีตรวจ</button>`;
  } else if (proposal?.state === "awaiting_post_review") {
    const reviewers = taskEligibleProviders(task, proposal.provider_id);
    action = `${reviewers.length ? "" : `<p class="session-error">เพิ่มโปรไฟล์ ${task.egress_policy === "remote_allowed" ? "" : "ในเครื่อง"}อีกอันใน Models เพื่อใช้ตรวจรอบแยก โปรไฟล์ใหม่นั้นอาจใช้โมเดลเดิมได้</p><button class="ghost" data-task-models>เปิด Models</button>`}<label>โมเดลผู้ตรวจ<select id="taskReviewerProvider">${taskProviderOptions(reviewers)}</select></label><p class="form-note" id="taskReviewIdentity">${escapeHTML(taskReviewIdentityText(proposal.provider_id, reviewers[0]?.id))}</p><button class="primary" id="taskPostReview" ${reviewers.length ? "" : "disabled"}>ตรวจผล</button>`;
  } else if (task.state === "completed") {
    action = `<p>งานเสร็จแล้วตามเป้าหมายฉบับนี้ ผลตรวจและหลักฐานอยู่ด้านล่าง</p>`;
  } else if (proposal && ["rejected", "verification_failed", "post_review_rejected", "apply_failed", "recovery_required"].includes(proposal.state)) {
    const failedStep = (task.plan?.steps || []).find(step => step.state === "failed");
    const reason = taskFriendlyError(failedStep?.last_error || proposal.state.replaceAll("_", " "));
    const recovery = proposal.state === "verification_failed" ? "ระบบพยายามคืนไฟล์จากหลักฐานก่อนแก้แล้ว ตรวจไฟล์และผลคำสั่งก่อนเริ่มรอบใหม่" : "ตรวจหลักฐานและปรับแผนก่อนเริ่มรอบใหม่";
    action = `<p class="session-error" role="alert">${escapeHTML(reason)}. ${escapeHTML(recovery)}</p>${taskProposalHTML(proposal)}`;
  }
  if (!action && !busy && !execution.run) return "";
  return `<section class="inspect-section task-next-action"><fieldset ${busy ? "disabled" : ""}><legend>ทำต่อ</legend>${busy ? `<p role="status">${escapeHTML(busy)}…</p>` : ""}${action}</fieldset>
    <details><summary>รายละเอียดการทำงานและหลักฐาน</summary><div class="kv"><span>รอบงาน</span><strong>${escapeHTML(execution.run?.state || "ยังไม่เริ่ม")}</strong><span>รอบลอง</span><strong>${escapeHTML(execution.attempt?.state || "ยังไม่เริ่ม")}</strong><span>ข้อเสนอ</span><strong>${escapeHTML(proposal?.state || "ไม่มี")}</strong><span>การกระทำที่บันทึก</span><strong>${(execution.effects || []).length}</strong></div>
    ${(execution.effects || []).map(effect => `<p>${escapeHTML(effect.action)} · ${escapeHTML(effect.state)}${effect.error ? ` · ${escapeHTML(effect.error)}` : ""}${(effect.receipt?.evidence_refs || []).length ? ` · ${effect.receipt.evidence_refs.map(taskEvidenceRefHTML).join(" · ")}` : ""}</p>`).join("")}
    ${proposal?.verification_artifact_id ? `<a class="button-link" href="/api/artifacts/${encodeURIComponent(proposal.verification_artifact_id)}/content" target="_blank" rel="noreferrer">เปิดผลตรวจ</a>` : ""}</details></section>`;
}

function taskEvidenceRefHTML(ref) {
  const value = String(ref || "");
  if (value.startsWith("rollback_artifact:") && value.length > 18) {
    return `<a href="/api/artifacts/${encodeURIComponent(value.slice(18))}/content" target="_blank" rel="noopener noreferrer">ผลคืนไฟล์</a>`;
  }
  if (value.startsWith("artifact:") && value.length > 9) {
    return `<a href="/api/artifacts/${encodeURIComponent(value.slice(9))}/content" target="_blank" rel="noopener noreferrer">${escapeHTML(value)}</a>`;
  }
  return escapeHTML(value);
}

function taskActivityHTML(execution) {
  if (!execution || execution.error) return "";
  const items = execution.activity || [];
  const labels = {goal_revision:"เป้าหมาย",validation:"ผลตรวจ",failure:"ข้อผิดพลาดที่ยืนยันแล้ว",checkpoint:"จุดบันทึกงาน",plan_review:"การทบทวนแผน",user_acceptance:"รับรองผล",effect:"ผลเครื่องมือ",browser_effect:"Browser",desktop_effect:"Windows",comfy_effect:"Comfy",social_effect:"ส่งออกโซเชียล"};
  return `<details class="inspect-section task-activity"><summary>ประวัติหลักฐานและคำตัดสิน · ${items.length} รายการ</summary>
    ${execution.activityError ? `<p role="alert">${escapeHTML(execution.activityError)}</p>` : ""}
    ${items.length ? `<ol class="task-activity-list">${items.map(item => `<li><strong>${escapeHTML(labels[item.kind] || item.kind)} · ${escapeHTML(item.kind === "failure" && item.label === "test" ? "วิธีตรวจ" : item.label || "")}</strong><span>${escapeHTML(item.kind === "failure" && item.status === "confirmed" ? "ยืนยันแล้ว" : item.status)} · ${item.current ? "ฉบับปัจจุบัน" : "ฉบับก่อน"}</span><small>${formatDate(item.created_at)} · ${escapeHTML(item.id)}</small>${item.evidence_refs?.length ? `<p>หลักฐาน: ${item.evidence_refs.map(taskEvidenceRefHTML).join(", ")}</p>` : ""}</li>`).join("")}</ol>` : `<p>ยังไม่มีบันทึกหลักฐานของงานนี้</p>`}</details>`;
}

function taskResearchHTML(task, execution) {
  const draft = taskResearchDrafts.get(task.id) || {query:execution?.content?.brief?.topic || "", days:execution?.content?.brief?.freshness_days || 30};
  const items = execution?.research || [];
  const error = execution?.researchError;
  return `<section class="inspect-section task-research" aria-labelledby="taskResearchHeading"><div class="provider-head"><div><h3 id="taskResearchHeading">ค้นบทความใหม่</h3><p>ดูข้อมูลบทความจาก Crossref แล้วเลือกแหล่งที่จะอ่านต่อ ข้อมูลชื่อและวันที่ยังไม่ยืนยันข้อเท็จจริงในบทความ</p></div>${pill(`${items.filter(item => item.selected).length} แหล่งที่เลือก`, "blue")}</div>
    ${task.egress_policy !== "remote_allowed" ? `<p class="form-note neutral">งานนี้เก็บข้อมูลไว้ในเครื่อง หากต้องค้นภายนอก ให้สร้างงานใหม่และเลือก “อนุญาตค้นข้อมูลภายนอก” ตอนสร้างงาน</p>` : `<form id="taskResearchForm"><label>หัวข้อที่ต้องการค้น<input name="query" required minlength="2" maxlength="240" value="${escapeHTML(draft.query)}" placeholder="เช่น AI กับธุรกิจขนาดเล็ก"></label><label>เผยแพร่ภายใน<select name="days"><option value="7" ${draft.days === 7 ? "selected" : ""}>7 วัน</option><option value="30" ${draft.days === 30 ? "selected" : ""}>30 วัน</option><option value="90" ${draft.days === 90 ? "selected" : ""}>90 วัน</option><option value="365" ${draft.days === 365 ? "selected" : ""}>1 ปี</option></select></label><button class="ghost" type="submit" ${taskActions.has(task.id) ? "disabled" : ""}>ค้นบทความ</button></form>`}
    ${error ? `<p class="task-feedback" role="alert">${escapeHTML(error)}</p>` : ""}
    ${execution && !error && !items.length ? `<p class="form-note neutral">ยังไม่มีบทความที่บันทึกไว้ ค้นด้วยหัวข้อเพื่อเริ่มตรวจแหล่งข้อมูล</p>` : ""}
    ${items.length ? `<ul class="task-research-results">${items.map(item => `<li><div><strong>${escapeHTML(item.title)}</strong><p>${escapeHTML(item.publisher || "ไม่ระบุผู้เผยแพร่")} · ${escapeHTML(item.published_date || "ไม่ระบุวันที่")} ${item.published_precision && item.published_precision !== "day" ? `(${escapeHTML(item.published_precision === "year" ? "ทราบเฉพาะปี" : "ทราบถึงเดือน")})` : ""}</p><small>DOI: ${escapeHTML(item.doi)} · ดึงข้อมูล ${escapeHTML(new Date(item.retrieved_at).toLocaleString("th-TH"))}</small><a href="${escapeHTML(item.source_url)}" target="_blank" rel="noopener noreferrer">เปิดแหล่งต้นทาง</a></div><button class="ghost" type="button" data-research-select="${escapeHTML(item.id)}" data-selected="${item.selected ? "true" : "false"}" ${taskActions.has(task.id) ? "disabled" : ""}>${item.selected ? "นำออก" : "เลือกแหล่งนี้"}</button></li>`).join("")}</ul>` : ""}
  </section>`;
}

function taskTikTokExportFieldsHTML(draft, account, creator) {
  if (draft.platform !== "tiktok") return `<p class="form-note neutral">Facebook ใช้การมองเห็นของ Page</p>`;
  if (account.capability === "video.upload_identity") return `<p class="form-note neutral">การมองเห็นและคำบรรยายจะเลือกใน TikTok หลังรับร่าง</p>`;
  const options = creator?.privacy_level_options || [];
  const labels = {SELF_ONLY:"เห็นเฉพาะฉัน", MUTUAL_FOLLOW_FRIENDS:"เพื่อนที่ติดตามกัน", FOLLOWER_OF_CREATOR:"ผู้ติดตาม", PUBLIC_TO_EVERYONE:"สาธารณะ"};
  return `<p>ตรวจตัวเลือกของ ${escapeHTML(account.display_name)} จาก TikTok ก่อนส่งออก</p><button class="ghost" type="button" data-creator-check="${escapeHTML(account.id)}">ตรวจบัญชีและตัวเลือก</button>
    ${creator && !creator.error && options.length ? `<label>การมองเห็น<select name="privacy_level" required><option value="">เลือกการมองเห็น</option>${options.map(option => `<option value="${escapeHTML(option)}">${escapeHTML(labels[option] || option)}</option>`).join("")}</select></label><p class="form-note neutral">บัญชีนี้โพสต์วิดีโอได้สูงสุด ${Number(creator.max_video_post_duration_sec || 0)} วินาที</p>
      <fieldset><legend>อนุญาตการโต้ตอบ</legend><label class="check-label"><input type="checkbox" name="allow_comment" ${creator.comment_disabled ? "disabled" : ""}> แสดงความคิดเห็น${creator.comment_disabled ? " · บัญชีปิดไว้" : ""}</label><label class="check-label"><input type="checkbox" name="allow_duet" ${creator.duet_disabled ? "disabled" : ""}> Duet${creator.duet_disabled ? " · บัญชีปิดไว้" : ""}</label><label class="check-label"><input type="checkbox" name="allow_stitch" ${creator.stitch_disabled ? "disabled" : ""}> Stitch${creator.stitch_disabled ? " · บัญชีปิดไว้" : ""}</label></fieldset>` : `<p class="form-note neutral" ${creator?.error ? 'role="alert"' : ""}>${escapeHTML(creator?.error || "ยังไม่ตรวจตัวเลือกบัญชี")}</p>`}
    <label class="check-label"><input type="checkbox" name="commercial_disclosure"> วิดีโอนี้ส่งเสริมธุรกิจ แบรนด์ หรือสินค้า</label><div data-commercial-options hidden><label class="check-label"><input type="checkbox" name="brand_organic"> โปรโมตสินค้า/บริการของตนเอง</label><label class="check-label"><input type="checkbox" name="brand_content"> โปรโมตแบรนด์อื่นหรือมีผู้สนับสนุน</label><p class="form-note neutral" data-commercial-note>เลือกประเภทที่ตรงกับวิดีโอนี้</p></div>
    <label class="check-label"><input type="checkbox" name="music_consent" required> ฉันยอมรับ <a href="https://www.tiktok.com/legal/page/global/music-usage-confirmation/en" target="_blank" rel="noopener noreferrer">ข้อกำหนดการใช้เพลงของ TikTok</a> และหากเป็นเนื้อหาแบรนด์ ยอมรับ <a href="https://www.tiktok.com/legal/page/global/bc-policy/en" target="_blank" rel="noopener noreferrer">นโยบายเนื้อหาแบรนด์</a></label>`;
}

function taskClaimFormHTML(selectedSources, claimDraft, taskID) {
  const consequential = claimDraft.consequential !== false;
  return `<form id="taskContentClaimForm"><label>แหล่งข้อมูลหลัก<select name="source_id">${selectedSources.map(item => `<option value="${escapeHTML(item.id)}" ${claimDraft.source_id === item.id ? "selected" : ""}>${escapeHTML(item.title)}</option>`).join("")}</select></label><label>ประเด็นที่จะใช้ในโพสต์<input name="statement" required maxlength="1000" value="${escapeHTML(claimDraft.statement || "")}"></label><label>ข้อความหลักฐานจากบทความหลัก<textarea name="evidence_excerpt" rows="3" required maxlength="3000" placeholder="คัดเฉพาะส่วนที่ใช้ตรวจประเด็นนี้">${escapeHTML(claimDraft.evidence_excerpt || "")}</textarea></label>
    <label class="check-label"><input type="checkbox" name="consequential" ${consequential ? "checked" : ""}> เป็นประเด็นสำคัญ ต้องเทียบกับบทความอีกแหล่ง</label><div data-claim-second ${consequential ? "" : "hidden"}><label>บทความที่สอง<select name="corroborating_source_id" ${consequential ? "required" : ""}><option value="">เลือกคนละบทความ</option>${selectedSources.map(item => `<option value="${escapeHTML(item.id)}" ${claimDraft.corroborating_source_id === item.id ? "selected" : ""}>${escapeHTML(item.title)}</option>`).join("")}</select></label><label>ข้อความหลักฐานจากบทความที่สอง<textarea name="corroborating_excerpt" rows="3" maxlength="3000" ${consequential ? "required" : ""} placeholder="คัดข้อความที่ยืนยันหรือขัดกับประเด็นนี้">${escapeHTML(claimDraft.corroborating_excerpt || "")}</textarea></label><p class="form-note neutral">เปิดต้นทางของทั้งสองบทความและตรวจว่าเป็นงานคนละชิ้นก่อนกดว่ามีหลักฐานรองรับ</p></div>
    <button class="ghost" type="submit" ${taskActions.has(taskID) ? "disabled" : ""}>เพิ่มประเด็นให้ตรวจ</button></form>`;
}

function taskContentHTML(task, execution) {
  const content = execution?.content || {claims:[], drafts:[]};
  const brief = content.brief;
  const briefDraft = taskContentBriefDrafts.get(task.id) || brief || {topic:"",audience:"",language:"ไทย",facebook_page:"",tiktok_account:"",freshness_days:30,revision:0};
  const selectedSources = (execution?.research || []).filter(item => item.selected);
  const claimDraft = taskContentClaimDrafts.get(task.id) || {source_id:selectedSources[0]?.id || "",statement:"",evidence_excerpt:"",consequential:true,corroborating_source_id:"",corroborating_excerpt:""};
  const videos = (state.artifacts || []).filter(item => item.kind === "video" && item.project_id === task.project_id);
  const platforms = [{key:"facebook",name:"Facebook",destination:brief?.facebook_page},{key:"tiktok",name:"TikTok",destination:brief?.tiktok_account}].filter(item => item.destination);
  const connected = platform => (state.socialConnections || []).filter(item => item.platform === platform && item.status === "ready" && item.credential_stored);
  return `<section class="inspect-section task-content" aria-labelledby="taskContentHeading"><div class="provider-head"><div><h3 id="taskContentHeading">ทำคอนเทนต์จากแหล่งข้อมูล</h3><p>บันทึกหัวข้อ ตรวจข้อกล่าวอ้าง แล้วเตรียมร่างให้ตรวจแยกแต่ละบัญชี</p></div>${pill(`${content.drafts?.length || 0} ร่าง`, "blue")}</div>
    ${execution?.contentError ? `<p class="task-feedback" role="alert">${escapeHTML(execution.contentError)}</p>` : ""}
    <div class="task-content-step"><h4>1 · บอกเรื่องที่จะทำ</h4><p class="form-note neutral">กรอกหัวข้อ กลุ่มคนอ่าน และภาษา แล้วเลือกบัญชีที่เชื่อมไว้หรือพิมพ์ชื่อบัญชีที่ต้องการ เช่น “AI สำหรับร้านเล็ก” · “เจ้าของร้าน” · “ไทย” ระบบจะเตรียมร่างให้ตรวจ ยังไม่เผยแพร่เอง</p><form id="taskContentBriefForm"><input type="hidden" name="revision" value="${Number(brief?.revision || 0)}"><label>หัวข้อ<input name="topic" required maxlength="240" value="${escapeHTML(briefDraft.topic || "")}" placeholder="เช่น AI ช่วยธุรกิจขนาดเล็ก"></label><label>กลุ่มผู้อ่าน<input name="audience" required maxlength="240" value="${escapeHTML(briefDraft.audience || "")}" placeholder="เช่น เจ้าของร้านค้า"></label><label>ภาษา<input name="language" required maxlength="80" value="${escapeHTML(briefDraft.language || "")}"></label><label>Facebook Page<input name="facebook_page" list="connectedFacebookPages" maxlength="160" value="${escapeHTML(briefDraft.facebook_page || "")}" placeholder="เลือก Page ที่เชื่อมไว้ หรือเว้นว่าง"></label><datalist id="connectedFacebookPages">${connected("facebook").map(item => `<option value="${escapeHTML(item.display_name)}"></option>`).join("")}</datalist><label>TikTok account<input name="tiktok_account" list="connectedTikTokAccounts" maxlength="160" value="${escapeHTML(briefDraft.tiktok_account || "")}" placeholder="เลือกบัญชีที่เชื่อมไว้ หรือเว้นว่าง"></label><datalist id="connectedTikTokAccounts">${connected("tiktok").map(item => `<option value="${escapeHTML(item.display_name)}"></option>`).join("")}</datalist><label>ช่วงเวลาบทความ<select name="freshness_days">${[7,30,90,365].map(days => `<option value="${days}" ${Number(briefDraft.freshness_days) === days ? "selected" : ""}>${days === 365 ? "1 ปี" : `${days} วัน`}</option>`).join("")}</select></label><button class="ghost" type="submit" ${taskActions.has(task.id) ? "disabled" : ""}>บันทึกเรื่องที่จะทำ</button></form><p class="form-note neutral">เปลี่ยนข้อมูลนี้เมื่อไร ระบบจะให้ตรวจและอนุมัติร่างใหม่อีกครั้ง</p></div>
    <div class="task-content-step"><h4>2 · ตรวจข้อกล่าวอ้าง</h4>${selectedSources.length ? `${taskClaimFormHTML(selectedSources, claimDraft, task.id)}` : `<p class="form-note neutral">เลือกบทความจากขั้นค้นหาก่อน แล้วเปิดต้นทางเพื่ออ่านเนื้อหาจริง</p>`}
      ${(content.claims || []).length ? `<ul class="task-content-claims">${content.claims.map(claim => `<li><strong>${escapeHTML(claim.statement)}</strong><p>แหล่ง: <a href="${escapeHTML(claim.source_url)}" target="_blank" rel="noopener noreferrer">${escapeHTML(claim.source_title)}</a></p><blockquote>${escapeHTML(claim.evidence_excerpt)}</blockquote>${claim.consequential ? `<p>บทความยืนยันที่สอง: <a href="${escapeHTML(claim.corroborating_source_url)}" target="_blank" rel="noopener noreferrer">${escapeHTML(claim.corroborating_source_title || "ไม่พบแหล่ง")}</a></p><blockquote>${escapeHTML(claim.corroborating_excerpt)}</blockquote>` : `<p class="form-note neutral">ประเด็นทั่วไป · ใช้บทความเดียว</p>`}<p>${claim.current ? escapeHTML({pending:"รอตรวจ",supported:"ตรวจว่ามีหลักฐาน",unsupported:"ไม่พบหลักฐาน",conflict:"ข้อมูลขัดกัน"}[claim.review_status] || claim.review_status) : "แหล่งเปลี่ยนหรือถูกนำออก · ต้องตรวจใหม่"}${claim.reviewer ? ` · ผู้ตรวจ ${escapeHTML(claim.reviewer)}` : ""}</p><div class="action-row"><button class="ghost" type="button" data-claim-review="${escapeHTML(claim.id)}" data-verdict="supported" ${!claim.current || taskActions.has(task.id) ? "disabled" : ""}>มีหลักฐานรองรับ</button><button class="ghost" type="button" data-claim-review="${escapeHTML(claim.id)}" data-verdict="conflict" ${!claim.current || taskActions.has(task.id) ? "disabled" : ""}>ข้อมูลขัดกัน</button><button class="ghost" type="button" data-claim-review="${escapeHTML(claim.id)}" data-verdict="unsupported" ${!claim.current || taskActions.has(task.id) ? "disabled" : ""}>ไม่มีหลักฐาน</button><button class="ghost" type="button" data-claim-remove="${escapeHTML(claim.id)}" ${taskActions.has(task.id) ? "disabled" : ""}>นำประเด็นออก</button></div></li>`).join("")}</ul>` : ""}
    </div>
    <div class="task-content-step"><h4>3 · สร้างสื่อด้วย Comfy</h4><p class="form-note neutral">เลือกไฟล์ workflow แบบ API JSON ที่อยู่ในโฟลเดอร์โปรเจกต์ ตรวจ node, โมเดล และค่าเครดิตก่อนเริ่ม</p><form id="taskComfyPreflightForm"><label>ตำแหน่งไฟล์ workflow<input name="workflow_path" type="text" required value="${escapeHTML(taskComfyPaths.get(task.id) || "")}" placeholder="เช่น D:\\Project\\video-workflow.json"></label><button class="ghost" type="submit" ${taskActions.has(task.id) ? "disabled" : ""}>ตรวจ workflow</button></form>${taskComfyPreflights.has(task.id) ? (() => { const check = taskComfyPreflights.get(task.id); return check.error ? `<p role="alert" class="task-feedback">${escapeHTML(check.error)}</p>` : `<div><p role="status">${check.ready ? "พร้อมรันใน ComfyUI เครื่องนี้" : `ยังรันไม่ได้ · ${escapeHTML(check.reason || "โปรดตรวจผลใน Comfy")}`} · ${Number(check.node_count || 0)} nodes · ${check.spends_credits ? "มีค่าเครดิต" : "ไม่ใช้เครดิต partner"}</p>${check.ready ? `<button class="primary" type="button" id="taskComfyStart" ${taskActions.has(task.id) ? "disabled" : ""}>เริ่มสร้างสื่อจาก workflow นี้</button>` : ""}</div>`; })() : ""}${execution?.comfyJobsError ? `<p role="alert" class="task-feedback">${escapeHTML(execution.comfyJobsError)}</p>` : ""}${(execution?.comfyJobs || []).map(job => `<article class="task-comfy-job"><strong>${escapeHTML(job.state === "completed" ? "สร้างสื่อเสร็จ" : job.state === "uncertain" ? "ผลยังไม่แน่นอน" : job.state === "failed" ? "สร้างสื่อไม่สำเร็จ" : job.state === "cancelled" ? "ยกเลิกแล้ว" : "กำลังสร้างสื่อ")}</strong><p>${escapeHTML(job.workflow_path)} · ${escapeHTML(job.prompt_id || "ยังไม่มีรหัสงานจาก Comfy")}</p>${job.error ? `<p role="alert">${escapeHTML(job.error)}</p>` : ""}${(job.artifact_ids || []).map(id => { const artifact = (state.artifacts || []).find(item => item.id === id); return `<div><a href="/api/artifacts/${encodeURIComponent(id)}/content" target="_blank" rel="noopener noreferrer">เปิดผลลัพธ์ ${escapeHTML(artifact?.name || id)}</a>${artifact?.kind === "video" ? `<video controls playsinline preload="metadata" src="/api/artifacts/${encodeURIComponent(id)}/stream" aria-label="วิดีโอจาก Comfy ${escapeHTML(artifact.name)}"></video>` : ""}</div>`; }).join("")}${["queued","running","uncertain"].includes(job.state) && job.prompt_id ? `<div class="action-row"><button class="ghost" data-comfy-refresh="${escapeHTML(job.id)}">ตรวจสถานะ</button>${job.state !== "uncertain" ? `<button class="ghost" data-comfy-cancel="${escapeHTML(job.id)}">ยกเลิกงาน</button>` : ""}</div>` : ""}</article>`).join("")}<p class="form-note neutral">ผล Comfy ต้องดูและตรวจคุณภาพก่อนนำไปใช้ในร่างโพสต์ วิดีโอใช้ได้เมื่อไฟล์ผ่านการตรวจชนิด MP4 หรือ WebM</p></div>
    <div class="task-content-step"><h4>4 · เขียนและตรวจร่าง</h4><p class="form-note neutral">สร้างโครงร่างจากประเด็นที่ตรวจแล้วได้ โดยต้องอ่าน ปรับ และบันทึกร่างแต่ละปลายทางเอง</p>${brief && platforms.length ? `<button class="ghost" type="button" id="taskContentSuggest" ${taskActions.has(task.id) ? "disabled" : ""}>ร่างข้อความจากหลักฐานที่ตรวจแล้ว</button>` : ""}${!brief ? `<p class="form-note neutral">บันทึก brief ก่อนเริ่มร่าง</p>` : platforms.map(platform => {
      const draft = (content.drafts || []).find(item => item.platform === platform.key && item.destination === platform.destination);
      const key = `${task.id}:${platform.key}`;
      const edit = taskContentDraftEdits.get(key) || draft || {body:"",script:"",storyboard:"",media_artifact_id:"",media_rights_note:""};
      return `<article class="task-content-draft"><div class="provider-head"><div><h5>${platform.name} · ${escapeHTML(platform.destination)}</h5><p>${draft ? `ฉบับที่ ${draft.revision} · ${draft.approved ? "อนุมัติฉบับนี้แล้ว" : "รอตรวจ"}` : "ยังไม่มีร่าง"}</p></div>${draft ? pill(draft.approved ? "อนุมัติแล้ว" : "ร่าง", draft.approved ? "green" : "blue") : ""}</div>
        <form data-content-draft="${platform.key}"><input type="hidden" name="id" value="${escapeHTML(draft?.id || "")}"><input type="hidden" name="expected_revision" value="${Number(draft?.revision || 0)}"><label>${platform.key === "facebook" ? "ข้อความโพสต์" : "คำบรรยาย TikTok"}<textarea name="body" required rows="5" maxlength="5000">${escapeHTML(edit.body || "")}</textarea></label>${platform.key === "tiktok" ? `<label>สคริปต์<textarea name="script" rows="4" maxlength="10000">${escapeHTML(edit.script || "")}</textarea></label><label>ลำดับภาพ<textarea name="storyboard" rows="4" maxlength="10000">${escapeHTML(edit.storyboard || "")}</textarea></label>` : ""}<label>วิดีโอ${platform.key === "tiktok" ? " · ต้องมีก่อนอนุมัติ" : " · ไม่บังคับ"}<select name="media_artifact_id"><option value="">ยังไม่เลือก</option>${videos.map(video => `<option value="${escapeHTML(video.id)}" ${edit.media_artifact_id === video.id ? "selected" : ""}>${escapeHTML(video.name)}</option>`).join("")}</select></label><label>สิทธิ์และที่มาของวิดีโอ<textarea name="media_rights_note" rows="2" maxlength="2000" placeholder="เช่น สร้างเองด้วย workflow ในเครื่องและตรวจสิทธิ์ขององค์ประกอบแล้ว">${escapeHTML(edit.media_rights_note || "")}</textarea></label><button class="ghost" type="submit" ${taskActions.has(task.id) ? "disabled" : ""}>${draft ? "บันทึกร่างฉบับใหม่" : "บันทึกร่าง"}</button></form>
        ${draft ? `<div class="task-content-preview"><h6>ตัวอย่างฉบับที่บันทึก</h6><p>${escapeHTML(draft.body)}</p>${draft.script ? `<p><strong>สคริปต์:</strong> ${escapeHTML(draft.script)}</p>` : ""}${draft.storyboard ? `<p><strong>ลำดับภาพ:</strong> ${escapeHTML(draft.storyboard)}</p>` : ""}${draft.media_artifact_id ? `<video controls playsinline preload="metadata" src="/api/artifacts/${encodeURIComponent(draft.media_artifact_id)}/stream" aria-label="วิดีโอร่าง ${platform.name}"></video><p><strong>สิทธิ์และที่มา:</strong> ${escapeHTML(draft.media_rights_note || "ยังไม่ระบุ")}</p>` : ""}<p>${draft.approval_ready ? "พร้อมให้ตรวจและอนุมัติ" : escapeHTML(draft.readiness_reason || "ยังไม่พร้อม")}</p><button class="primary" type="button" data-content-approve="${escapeHTML(draft.id)}" ${draft.approval_ready && !draft.approved && !taskActions.has(task.id) ? "" : "disabled"}>${draft.approved ? "อนุมัติฉบับนี้แล้ว" : "ตรวจและอนุมัติฉบับนี้"}</button><p class="form-note neutral">การอนุมัตินี้ยังไม่อัปโหลดหรือเผยแพร่</p></div>` : ""}
      </article>`;
    }).join("")}</div>
    <div class="task-content-step"><h4>5 · ตรวจบัญชีปลายทาง</h4><p class="form-note neutral">ตรวจว่าบัญชีที่ตั้งไว้ใน brief ตรงกับบัญชีที่เชื่อมผ่าน API และมีสิทธิ์พร้อมใช้งาน</p>${platforms.map(platform => { const match = (state.socialConnections || []).find(item => item.platform === platform.key && (item.display_name === platform.destination || item.account_id === platform.destination || `@${item.account_id}` === platform.destination)); return `<p>${escapeHTML(platform.name)} · ${escapeHTML(platform.destination)}: ${match && match.status === "ready" && match.credential_stored ? "ตรวจตัวตนบัญชีแล้ว" : "ยังไม่มีการเชื่อมที่ตรงกัน"}</p>`; }).join("")}<button class="ghost" type="button" id="taskSocialSettings">เปิดการเชื่อมบัญชี</button><p class="form-note neutral">การอนุมัติร่างด้านบนยังไม่ใช่คำสั่งเผยแพร่ ต้องตรวจตัวอย่างปลายทางและอนุมัติการส่งออกอีกครั้งก่อนโพสต์</p></div>
    <div class="task-content-step"><h4>6 · ตรวจและส่งออกแต่ละชิ้น</h4>${execution?.socialPostsError ? `<p role="alert">${escapeHTML(execution.socialPostsError)}</p>` : ""}${(content.drafts || []).filter(draft => draft.approved).map(draft => { const account = (state.socialConnections || []).find(item => item.platform === draft.platform && item.status === "ready" && item.credential_stored && (item.display_name === draft.destination || item.account_id === draft.destination || `@${item.account_id}` === draft.destination)); const posts = (execution?.socialPosts || []).filter(item => item.draft_id === draft.id); const creator = taskSocialCreatorInfo.get(account?.id); return `<article class="task-content-draft"><h5>${escapeHTML(draft.platform === "facebook" ? "Facebook Page" : "TikTok")} · ${escapeHTML(draft.destination)}</h5><p>ร่างที่อนุมัติ: ${escapeHTML(draft.body)}</p>${draft.media_artifact_id ? `<video controls playsinline preload="metadata" src="/api/artifacts/${encodeURIComponent(draft.media_artifact_id)}/stream" aria-label="วิดีโอที่จะส่งออก"></video>` : ""}<p>${account ? `บัญชีที่ตรวจแล้ว: ${escapeHTML(account.display_name)}` : "ยังไม่มีบัญชีที่ตรงและพร้อมใช้"}</p>${account ? `<form data-social-intent="${escapeHTML(draft.id)}" data-connection-id="${escapeHTML(account.id)}">${taskTikTokExportFieldsHTML(draft, account, creator)}<label class="check-label"><input type="checkbox" name="is_aigc" ${draft.media_artifact_id && (state.artifacts || []).find(item => item.id === draft.media_artifact_id)?.metadata?.source === "comfy-mcp" ? "checked" : ""}> สื่อนี้สร้างด้วย AI</label><button class="ghost" type="submit" ${draft.platform === "tiktok" && account.capability !== "video.upload_identity" && (!creator || creator.error || !(creator.privacy_level_options || []).length || !Number(creator.max_video_post_duration_sec)) ? "disabled" : ""}>เตรียมตัวอย่างก่อนส่งออก</button></form>` : ""}${posts.map(post => `<div class="task-content-preview"><h6>คำสั่งส่งออก · ${escapeHTML({review:"รอตรวจ",approved:"อนุมัติแล้ว",initializing:"กำลังเริ่ม",uploading:"กำลังอัปโหลด",finishing:"กำลังเผยแพร่",processing:"แพลตฟอร์มกำลังตรวจ",published:"เผยแพร่แล้ว",draft_uploaded:"ส่งเป็นร่าง",uncertain:"ผลยังไม่แน่นอน",failed:"ไม่สำเร็จ"}[post.state] || post.state)}</h6><p>ข้อความ: ${escapeHTML(draft.body)}</p><p>บัญชี: ${escapeHTML(draft.destination)} · ${escapeHTML(post.route)} · ${escapeHTML(post.privacy_level || "ตาม Page")} · ${post.is_aigc ? "ป้ายกำกับ AI" : "ไม่ได้ตั้งป้ายกำกับ AI"}${post.route === "tiktok_direct" ? ` · ${post.brand_content ? "มีเนื้อหาแบรนด์" : "ไม่มีเนื้อหาแบรนด์"} · ${post.brand_organic ? "โปรโมตสินค้า/บริการของตนเอง" : "ไม่โปรโมตสินค้า/บริการของตนเอง"}` : ""}</p>${post.subject_hash !== draft.subject_hash ? `<p role="alert">ร่างเปลี่ยนแล้ว ต้องเตรียมคำสั่งใหม่</p>` : ""}${post.error ? `<p role="alert">${escapeHTML(post.error)}</p>` : ""}${post.remote_url && /^https:\/\/(www\.)?(facebook\.com|tiktok\.com)\//.test(post.remote_url) ? `<a href="${escapeHTML(post.remote_url)}" target="_blank" rel="noopener noreferrer">เปิดโพสต์บนแพลตฟอร์ม</a>` : ""}<div class="action-row">${post.state === "review" && post.subject_hash === draft.subject_hash ? `<button class="primary" type="button" data-social-approve="${escapeHTML(post.id)}">อนุมัติส่งออกชิ้นนี้</button>` : ""}${post.state === "approved" && post.subject_hash === draft.subject_hash ? `<button class="primary" type="button" data-social-dispatch="${escapeHTML(post.id)}">อัปโหลดและเผยแพร่</button>` : ""}${["initializing","uploading","finishing","processing","draft_uploaded","uncertain"].includes(post.state) ? `<button class="ghost" type="button" data-social-refresh="${escapeHTML(post.id)}">ตรวจสถานะจริง</button>` : ""}</div>${post.remote_id ? `<small>รหัสจากแพลตฟอร์ม: ${escapeHTML(post.remote_id)} · ${escapeHTML(post.remote_status || "กำลังตรวจ")}</small>` : ""}</div>`).join("")}</article>`; }).join("") || `<p class="form-note neutral">อนุมัติร่างจากขั้นที่ 4 ก่อน</p>`}<p class="form-note neutral">แต่ละปลายทางต้องตรวจตัวอย่างและอนุมัติคำสั่งส่งออกของตัวเอง หากผลจากแพลตฟอร์มไม่แน่นอน ระบบจะไม่ส่งซ้ำอัตโนมัติ</p></div>
  </section>`;
}

function taskDesktopActionsHTML(task, execution) {
  const actions = execution?.desktopActions || [];
  if (!actions.length && !execution?.desktopActionsError) return "";
  const labels = {prepared:"รอยืนยัน", dispatched:"กำลังดำเนินการ", completed:"ตรวจผลแล้ว", cancelled:"ยกเลิกแล้ว", uncertain:"ผลยังไม่แน่นอน"};
  const itemHTML = item => {
    const observed = state.desktopObservation?.window?.handle === item.handle && state.desktopObservation?.window?.process_id === item.process_id ? state.desktopObservation : null;
    const reconciled = item.reconciliation;
    return `<li><div><strong>${escapeHTML(item.target?.name || item.target?.automation_id || item.target?.control_type || "รายการในหน้าต่าง")}</strong><p>${escapeHTML(item.action === "invoke" ? "กดรายการ" : "ใส่ข้อความ")} · ${escapeHTML(reconciled ? reconciled.resolution === "observed_applied" ? "ตรวจใหม่แล้วว่าเกิดผล" : "ตรวจใหม่แล้วว่าไม่เกิดผล" : labels[item.state] || item.state)}</p><small>${escapeHTML(new Date(item.created_at).toLocaleString("th-TH"))} · ${escapeHTML(item.id)}</small>${item.error ? `<p role="alert">${escapeHTML(item.error)}</p>` : ""}
      ${reconciled ? `<p>บันทึกการตรวจ: ${escapeHTML(reconciled.note)} · ${escapeHTML(reconciled.actor)}</p><a href="/api/artifacts/${encodeURIComponent(reconciled.screenshot_artifact_id)}/content" target="_blank" rel="noopener noreferrer">ภาพหลักฐานหลังตรวจ</a>` : ""}
      ${item.state === "uncertain" && !reconciled ? `<p class="form-note neutral">ตรวจหน้าต่างจริงอีกครั้งก่อนระบุผล ห้ามส่งคำสั่งเดิมซ้ำก่อนตรวจสอบ</p>${observed?.screenshot_artifact_id ? `<a href="/api/artifacts/${encodeURIComponent(observed.screenshot_artifact_id)}/content" target="_blank" rel="noopener noreferrer">ดูภาพหน้าต่างที่ตรวจล่าสุด</a><div class="action-row"><button class="ghost" type="button" data-desktop-reconcile="${escapeHTML(item.id)}" data-resolution="observed_applied" ${taskActions.has(task.id) ? "disabled" : ""}>ตรวจว่าเกิดผลแล้ว</button><button class="ghost" type="button" data-desktop-reconcile="${escapeHTML(item.id)}" data-resolution="observed_not_applied" ${taskActions.has(task.id) ? "disabled" : ""}>ตรวจว่าไม่เกิดผล</button></div>` : ""}` : ""}</div>
      ${item.state === "prepared" ? `<button class="ghost" type="button" data-desktop-cancel="${escapeHTML(item.id)}" data-subject-hash="${escapeHTML(item.subject_hash)}" ${taskActions.has(task.id) ? "disabled" : ""}>ยกเลิกคำสั่งค้าง</button>` : ""}
      ${item.state === "uncertain" && !reconciled ? `<button class="ghost" type="button" data-desktop-observe="${escapeHTML(item.id)}" ${taskActions.has(task.id) ? "disabled" : ""}>ตรวจหน้าต่างอีกครั้ง</button>` : ""}</li>`;
  };
  return `<section class="inspect-section task-desktop-actions" aria-labelledby="taskDesktopActionsHeading"><div class="provider-head"><div><h3 id="taskDesktopActionsHeading">การควบคุมแอป Windows ของงานนี้</h3><p>คำสั่งแต่ละครั้งผูกกับรายการและภาพหน้าต่างที่ตรวจไว้</p></div>${pill(`${actions.length} ครั้ง`, "blue")}</div>
    ${execution?.desktopActionsError ? `<p class="task-feedback" role="alert">${escapeHTML(execution.desktopActionsError)}</p>` : ""}
    ${actions.length ? `<ul class="task-research-results">${actions.map(itemHTML).join("")}</ul>` : ""}
  </section>`;
}

function taskBrowserActionsHTML(task, execution) {
  const actions = execution?.browserActions || [];
  if (!actions.length && !execution?.browserActionsError) return "";
  const labels = {prepared:"รอยืนยัน",dispatched:"ผลยังไม่แน่นอน",completed:"บันทึกผลแล้ว",cancelled:"ยกเลิกแล้ว",uncertain:"ผลยังไม่แน่นอน"};
  return `<section class="inspect-section task-browser-actions" aria-labelledby="taskBrowserActionsHeading"><div class="provider-head"><div><h3 id="taskBrowserActionsHeading">การควบคุม browser ของงานนี้</h3><p>คลิกและพิมพ์แต่ละครั้งมีภาพก่อนทำและรายการอนุมัติใช้ได้ครั้งเดียว</p></div>${pill(`${actions.length} ครั้ง`,"blue")}</div>
    ${execution?.browserActionsError ? `<p class="task-feedback" role="alert">${escapeHTML(execution.browserActionsError)}</p>` : ""}
    ${actions.length ? `<ul class="task-research-results">${actions.map(item => {
      const originalTab = state.browserTabs.find(tab => tab.id === item.tab_id);
      const candidates = state.browserTabs.filter(tab => {
        if (tab.state !== "ready" || tab.project_id !== task.project_id) return false;
        try { return !originalTab || new URL(tab.url).origin === new URL(originalTab.url).origin; }
        catch { return false; }
      });
      const chosen = taskBrowserVerificationTabs.get(item.id) || (candidates.some(tab => tab.id === item.tab_id) ? item.tab_id : candidates[0]?.id || "");
      if (chosen && !taskBrowserVerificationTabs.has(item.id)) taskBrowserVerificationTabs.set(item.id, chosen);
      const observed = state.browserTabs.find(tab => tab.id === chosen && tab.observation_hash && tab.screenshot_artifact_id);
      const reconciled = item.reconciliation;
      return `<li><div><strong>${escapeHTML(item.target?.text || item.target?.placeholder || item.target?.tag || "รายการในหน้า")}</strong><p>${item.action === "click" ? "คลิก" : "พิมพ์"} · ${escapeHTML(reconciled ? reconciled.resolution === "observed_applied" ? "ตรวจใหม่แล้วว่าเกิดผล" : "ตรวจใหม่แล้วว่าไม่เกิดผล" : labels[item.state] || item.state)}</p><small>${escapeHTML(item.id)} · ${escapeHTML(new Date(item.created_at).toLocaleString("th-TH"))}</small>${item.error ? `<p role="alert">${escapeHTML(item.error)}</p>` : ""}${item.screenshot_before_id ? `<a href="/api/artifacts/${encodeURIComponent(item.screenshot_before_id)}/content" target="_blank" rel="noopener noreferrer">ภาพก่อนทำ</a>` : ""}${item.screenshot_after_id ? `<a href="/api/artifacts/${encodeURIComponent(item.screenshot_after_id)}/content" target="_blank" rel="noopener noreferrer">ภาพหลังทำ</a>` : ""}${reconciled ? `<p>${escapeHTML(reconciled.note)} · ${escapeHTML(reconciled.actor)}</p><a href="/api/artifacts/${encodeURIComponent(reconciled.screenshot_artifact_id)}/content" target="_blank" rel="noopener noreferrer">ภาพที่ใช้ตรวจใหม่</a>` : ""}${item.state === "uncertain" && !reconciled ? `<p class="form-note neutral">ตรวจผลบนเว็บจริงก่อนบันทึก ห้ามคลิกหรือพิมพ์ซ้ำ หากแอปเปิดใหม่แล้วให้เปิดแท็บที่เว็บไซต์เดิมในโปรเจกต์นี้</p>${candidates.length ? `<label>แท็บที่ใช้ตรวจผล<select data-browser-verify-tab="${escapeHTML(item.id)}">${candidates.map(tab => `<option value="${escapeHTML(tab.id)}" ${tab.id === chosen ? "selected" : ""}>${escapeHTML(tab.title || tab.url)}</option>`).join("")}</select></label>` : `<p>ยังไม่มีแท็บที่พร้อมตรวจ เปิดเว็บไซต์เดิมใน browser ของโปรเจกต์นี้ก่อน</p>`}${observed ? `<a href="/api/artifacts/${encodeURIComponent(observed.screenshot_artifact_id)}/content" target="_blank" rel="noopener noreferrer">ภาพที่ตรวจล่าสุด</a><div class="action-row"><button class="ghost" data-browser-reconcile="${escapeHTML(item.id)}" data-resolution="observed_applied">ตรวจว่าเกิดผลแล้ว</button><button class="ghost" data-browser-reconcile="${escapeHTML(item.id)}" data-resolution="observed_not_applied">ตรวจว่าไม่เกิดผล</button></div>` : ""}` : ""}</div><div class="action-row">${item.state === "prepared" ? `<button class="ghost" data-browser-cancel="${escapeHTML(item.id)}" data-subject-hash="${escapeHTML(item.subject_hash)}">ยกเลิกคำสั่งค้าง</button>` : ""}${item.state === "uncertain" && !reconciled && candidates.length ? `<button class="ghost" data-browser-observe="${escapeHTML(item.id)}">ตรวจแท็บอีกครั้ง</button>` : ""}</div></li>`;
    }).join("")}</ul>` : ""}</section>`;
}

function taskCompletionHTML(task, execution) {
  if (!execution) return `<section class="inspect-section task-gate-card"><h3>ตรวจความพร้อมปิดงาน</h3><p role="status">กำลังตรวจหลักฐานปัจจุบัน…</p></section>`;
  if (execution.error) return "";
  const report = execution.completion;
  if (!report) return `<section class="inspect-section task-gate-card"><h3>ตรวจความพร้อมปิดงาน</h3><p role="status">กำลังตรวจหลักฐานปัจจุบัน…</p></section>`;
  if (report.error) return `<section class="inspect-section task-gate-card"><h3>ตรวจความพร้อมปิดงาน</h3><p role="alert">${escapeHTML(report.error)}</p><button class="ghost" id="taskReload">ลองใหม่</button></section>`;
  const gates = report.gates || [];
  const passed = gates.filter(gate => gate.status === "pass").length;
  const statusLabels = {pass:"ผ่าน", fail:"ไม่ผ่าน", blocked:"ติดขัด", unknown:"ยังไม่ทราบ", not_run:"ยังไม่ตรวจ", stale:"หลักฐานเก่า"};
  const evidenceGroups = [{prefix:"criterion:",label:"เกณฑ์สำเร็จ"},{prefix:"check:",label:"วิธีตรวจ"},{prefix:"step:",label:"ขั้นตอน"}].map(group => {
    const items = gates.filter(gate => gate.id.startsWith(group.prefix));
    return {...group,total:items.length,passed:items.filter(gate => gate.status === "pass").length};
  }).filter(group => group.total > 0);
  const selectedAction = execution.nextAction?.decision?.action_id;
  const actionLabels = {reconcile_effects:"ตรวจผลการกระทำที่ค้าง", finish:"ปิดงาน", review_plan:"ทบทวนแผน", request_user_acceptance:"ตรวจและรับรองผล", ask_planner:"ทบทวนแผน", inspect_file:"ตรวจไฟล์ของขั้นตอน", search_repository:"ค้นส่วนที่เกี่ยวข้อง", retrieve_memory:"ตรวจความรู้เดิม", run_test:"รันการตรวจ", view_evidence:"ดูหลักฐานของงาน"};
  const gateLabels = {state:"สถานะงาน", plan:"แผนฉบับปัจจุบัน", plan_review:"การทบทวนแผน", effects:"การกระทำที่ยังไม่ทราบผล", desktop_effects:"การควบคุมแอป Windows", browser_effects:"การควบคุม browser", review:"ข้อเสนอแก้ไขที่รอตรวจ", user_acceptance:"การรับรองผลจากผู้ใช้", content_delivery:"ผลเผยแพร่คอนเทนต์"};
  const nextReason = selectedAction === "finish" ? "หลักฐานและเกณฑ์ปัจจุบันครบแล้ว" : execution.nextAction?.decision?.reason || "";
  const reasonLabels = {
    "Task must be verifying before completion":"งานต้องถึงขั้นตรวจผลก่อนจึงจะปิดได้",
    "A current plan with steps is required":"ต้องมีแผนฉบับปัจจุบันและขั้นตอนก่อน",
    "No validation for the current subject revision":"ยังไม่มีผลตรวจของงานฉบับปัจจุบัน",
    "Passing validation has no evidence reference":"ผลตรวจที่ผ่านยังไม่มีหลักฐานอ้างอิง",
    "the task has no safe runnable step":"ยังไม่มีขั้นตอนที่เริ่มได้อย่างปลอดภัย",
    "all active criteria and steps have passing evidence":"ทุกเกณฑ์และขั้นตอนมีหลักฐานว่าผ่านแล้ว",
    "Finish current checks before accepting the result":"ตรวจงานฉบับปัจจุบันให้ครบก่อนรับรองผล",
    "User acceptance is required for the current evidence":"ต้องรับรองผลตามหลักฐานฉบับปัจจุบันก่อนปิดงาน",
    "current evidence is ready for user acceptance":"หลักฐานปัจจุบันพร้อมให้ตรวจและรับรองผล",
    "Requested content has no verified platform publication receipts":"ยังไม่มีหลักฐานจากแพลตฟอร์มว่าคอนเทนต์เผยแพร่แล้ว"
    ,"The current plan is awaiting review":"ต้องทบทวนแผนฉบับปัจจุบันก่อนเริ่มงาน"
    ,"the current plan is awaiting review":"แผนฉบับปัจจุบันรอการทบทวน"
    ,"Only a previous revision has validation; check the current goal and plan again":"มีผลตรวจจากฉบับเก่า ต้องตรวจ Goal และแผนฉบับปัจจุบันอีกครั้ง"
    ,"inspect the current step before choosing an effect":"ตรวจไฟล์และขอบเขตของขั้นตอนก่อนเลือกการกระทำ"
  };
  const explain = reason => reasonLabels[reason] || reason;
  return `<section class="inspect-section task-gate-card" aria-labelledby="taskGateHeading"><div class="task-gate-head"><div><h3 id="taskGateHeading" tabindex="-1">ตรวจความพร้อมปิดงาน</h3><p>ผ่าน ${passed}/${gates.length} รายการ · งานฉบับที่ ${report.task_revision}</p></div>${report.completed ? pill(passed === gates.length ? "เสร็จแล้ว" : "ต้องตรวจอีกครั้ง", passed === gates.length ? "green" : "blue") : report.can_complete ? pill("พร้อมปิดงาน", "green") : pill("ยังไม่ครบ", "blue")}</div>
    ${evidenceGroups.length ? `<div class="task-evidence-ledger" aria-label="ความคืบหน้าตามหลักฐานฉบับปัจจุบัน">${evidenceGroups.map(group => `<div><strong>${group.label} ${group.passed}/${group.total}</strong><progress value="${group.passed}" max="${group.total}" aria-label="${group.label} ผ่าน ${group.passed} จาก ${group.total}"></progress></div>`).join("")}</div>` : ""}
    ${report.completed ? `<p class="task-gate-next">งานเสร็จแล้ว · เปิดผลตรวจทั้งหมดด้านล่างเพื่อดูหลักฐาน</p>` : execution.nextAction?.error ? `<p class="form-note neutral">ยังอ่านขั้นตอนถัดไปไม่ได้: ${escapeHTML(execution.nextAction.error)}</p>` : selectedAction ? `<p class="task-gate-next"><strong>ขั้นตอนที่แนะนำ:</strong> ${escapeHTML(actionLabels[selectedAction] || selectedAction)}${nextReason ? ` · ${escapeHTML(explain(nextReason))}` : ""}</p>` : ""}
    <details class="task-gate-details"><summary>ดูผลตรวจทั้งหมด · ${gates.length} รายการ</summary><ul class="task-gate-list">${gates.map(gate => `<li class="task-gate-row"><span class="task-gate-status" data-status="${escapeHTML(gate.status)}">${escapeHTML(statusLabels[gate.status] || gate.status)}</span><div><strong>${escapeHTML(gateLabels[gate.id] || gate.label)}</strong>${gate.reason ? `<p>${escapeHTML(explain(gate.reason))}</p>` : ""}${gate.evidence_refs?.length ? `<small>หลักฐาน: ${gate.evidence_refs.map(taskEvidenceRefHTML).join(", ")}</small>` : ""}</div></li>`).join("")}</ul></details>
    ${task.state === "verifying" ? `<div class="task-gate-footer">${report.can_complete ? `<button class="primary" id="taskComplete" ${taskActions.has(task.id) ? "disabled" : ""}>ปิดงาน</button>` : report.awaiting_user_acceptance ? `<button class="primary" id="taskAcceptResult" ${taskActions.has(task.id) ? "disabled" : ""}>ตรวจและรับรองผล</button>` : `<p>แก้รายการที่ยังไม่ผ่าน แล้วโหลดงานใหม่ เมื่อเซิร์ฟเวอร์ยืนยันหลักฐานครบจึงจะปิดงานได้</p>`}</div>` : ""}
  </section>`;
}

function renderCurrentTaskCockpit(taskID) {
  if (state.selectedDurableTask !== taskID) return;
  const task = state.durableTasks.find(item => item.id === taskID);
  if (!task || (state.currentProject?.id && task.project_id !== state.currentProject.id)) return;
  const liveBody = document.querySelector('.pane-body[data-pane-kind="tasks"]');
  if (liveBody?.isConnected) renderTaskCockpit(liveBody);
}

function taskJumpAction(task, execution) {
  if (!task) return null;
  if (task.state === "completed") return {target:"taskGateHeading", label:"ดูหลักฐาน"};
  if (taskPlanAwaitsReview(task)) return {target:"taskPlanReview", label:"ตรวจแผน"};
  if (task.plan?.review_status === "rejected") return {target:"taskAutoPlan", label:"สร้างแผนใหม่"};
  if ((execution?.effects || []).some(effect => effect.state === "uncertain")) return {target:"taskReconcileEffects", label:"ตรวจผลที่ค้าง"};
  if (task.state === "paused" && execution?.run?.state === "paused" && execution?.attempt?.state === "interrupted" && execution?.proposal?.state === "awaiting_post_review") return {target:"taskResumeObservedReview", label:"ต่อการรีวิวที่ค้าง"};
  if (task.state === "paused" && execution?.run?.state === "paused" && execution?.attempt?.state === "completed" && execution?.proposal?.state === "awaiting_post_review") return {target:"taskResumeApprovedReview", label:"ต่อการปิดขั้นที่ตรวจผ่าน"};
  if (["running","interrupted"].includes(execution?.attempt?.state) && execution?.proposal?.attempt_id !== execution?.attempt?.id && (execution?.effects || []).some(effect => effect.action === "provider.propose" && effect.state === "observed" && effect.receipt?.status === "failed")) return {target:"taskRetryFailedProposal", label:"เริ่มรอบใหม่"};
  if ((execution?.desktopActions || []).some(item => ["prepared","dispatched","uncertain"].includes(item.state) && !item.reconciliation)) return {target:"taskDesktopActionsHeading", label:"ตรวจผล Windows"};
  if ((execution?.browserActions || []).some(item => ["prepared","dispatched","uncertain"].includes(item.state) && !item.reconciliation)) return {target:"taskBrowserActionsHeading", label:"ตรวจผล browser"};
  if ((execution?.comfyJobs || []).some(item => item.state === "uncertain")) return {target:"taskContentHeading", label:"ตรวจคิว Comfy"};
  if ((execution?.socialPosts || []).some(item => ["uploading","processing","uncertain"].includes(item.state))) return {target:"taskContentHeading", label:"ตรวจผลโพสต์"};
  if (execution?.completion?.can_complete) return {target:"taskComplete", label:"ตรวจและปิดงาน"};
  if (execution?.completion?.awaiting_user_acceptance) return {target:"taskAcceptResult", label:"รับรองผล"};
  if (execution && taskCanStartStep(task, execution)) return {target:"taskProposalForm", label:"เริ่มตามแผน"};
  if (!task.active_plan_revision) return {target:"taskAutoPlan", label:"ไปสร้างแผน"};
  return {target:"taskGateHeading", label:"ดูสิ่งที่ค้าง"};
}

function taskFlowHTML(stage, jump) {
  const phases = ["กำหนดเป้าหมาย", "ตรวจแผน", "ลงมือทำ", "ทบทวน", "ตรวจผล"];
  const current = Math.max(1, Math.min(5, stage.number));
  return `<section class="task-flow" aria-label="ลำดับการทำงาน"><div class="task-flow-current"><div><span class="task-flow-kicker">สถานะตอนนี้ · ${escapeHTML(stage.label)}</span><h4>${escapeHTML(stage.detail)}</h4></div></div><ol class="task-flow-steps">${phases.map((label, index) => `<li class="${index + 1 === current ? "active" : index + 1 < current ? "passed" : ""}"><span>${index + 1}</span>${label}</li>`).join("")}</ol></section>`;
}

function taskPlanDiffHTML(task, previousPlan) {
  if (!task?.plan || task.plan.revision < 2) return "";
  if (!previousPlan) return `<p class="task-feedback">กำลังโหลดแผนฉบับก่อนเพื่อเปรียบเทียบ</p>`;
  if (previousPlan.error) return `<p class="task-feedback" role="status">ยังเปรียบเทียบแผนไม่ได้: ${escapeHTML(previousPlan.error)}</p>`;
  const previous = new Map((previousPlan.steps || []).map(step => [step.key, step]));
  const changes = [];
  for (const step of task.plan.steps || []) {
    const before = previous.get(step.key);
    if (!before) changes.push({kind:"เพิ่ม", title:step.title});
    else if (["title", "instructions", "requirement_ids", "dependencies", "checks", "effect_scope"].some(key => JSON.stringify(before[key]) !== JSON.stringify(step[key])))
      changes.push({kind:"แก้", title:step.title});
    previous.delete(step.key);
  }
  for (const step of previous.values()) changes.push({kind:"นำออก", title:step.title});
  return `<details class="task-plan-diff" ${task.plan.review_status === "pending" ? "open" : ""}><summary>แผนฉบับ ${task.plan.revision} เทียบกับฉบับ ${previousPlan.revision} · ${changes.length} จุดเปลี่ยน</summary><p>ตรวจการเปลี่ยนขั้นตอนก่อนอนุมัติแผนฉบับนี้</p>${changes.length ? `<ul>${changes.map(change => `<li><strong>${escapeHTML(change.kind)}</strong> ${escapeHTML(change.title)}</li>`).join("")}</ul>` : `<p>ขั้นตอนเหมือนฉบับก่อน</p>`}</details>`;
}

function renderTaskCockpit(body) {
  if (state.plansOverview) {
    renderPlansOverview(body);
    return;
  }
  const tasks = projectDurableTasks();
  const selected = state.selectedDurableTask === "__new__" ? null : tasks.find(item => item.id === state.selectedDurableTask) || tasks[0];
  if (selected) state.selectedDurableTask = selected.id;
  const steps = selected?.plan?.steps || [];
  const execution = selected ? state.taskExecutions[selected.id] : undefined;
  const providers = taskEligibleProviders(selected);
  const stage = selected ? taskStage(selected, execution) : null;
  const jump = taskJumpAction(selected, execution);
  const busy = taskActions.has(selected?.id) || taskActions.has("create");
  const draft = state.taskDraftDetails || {};
  const draftObjective = state.taskDraftObjective || "";
  const canReplan = selected && selected.state !== "completed" && !busy &&
    !(execution?.effects || []).some(effect => ["planned", "dispatched", "uncertain"].includes(effect.state)) &&
    !(execution?.browserActions || []).some(item => ["prepared", "dispatched", "uncertain"].includes(item.state) && !item.reconciliation) &&
    (!execution?.proposal || ["rejected", "verification_failed", "post_review_rejected", "apply_failed"].includes(execution.proposal.state)) &&
    (selected.state !== "running" || !taskRunIsLive(execution?.run) || execution?.proposal?.state === "rejected");
  const canReviseGoal = selected && !["completed","cancelled"].includes(selected.state) && !busy &&
    !taskRunIsLive(execution?.run) && !(execution?.effects || []).some(effect => ["planned","dispatched","uncertain"].includes(effect.state)) &&
    !(execution?.browserActions || []).some(item => ["prepared", "dispatched", "uncertain"].includes(item.state) && !item.reconciliation) &&
    (!execution?.proposal || ["verified","rejected","post_review_rejected","apply_failed","verification_failed"].includes(execution.proposal.state));
  const revisionDraft = selected && (taskRevisionDrafts.get(selected.id) || {
    outcome:selected.objective, criteria:(selected.requirement?.criteria || []).map(item => item.description).join("\n"),
    scope:selected.requirement?.contract?.scope || "", experience_preview:selected.requirement?.contract?.experience_preview || "", assumptions:(selected.requirement?.contract?.assumptions || []).join("\n"),
    affected_journeys:(selected.requirement?.contract?.affected_journeys || []).join("\n"),
    verification:selected.requirement?.contract?.verification || "", constraints:(selected.requirement?.constraints || []).join("\n"),
    unknowns:(selected.requirement?.unknowns || []).join("\n"),
    require_user_acceptance:Boolean(selected.requirement?.contract?.require_user_acceptance)
  });
  const clarificationDraft = taskClarificationDraft(selected);
  const optionalToolsWereOpen = Boolean(body.querySelector("#taskOptionalTools")?.open);
  if (selected && optionalToolsWereOpen) taskOptionalToolsOpen.add(selected.id);
  body.innerHTML = `<div class="panel task-cockpit"><div class="provider-head"><div><p class="eyebrow">${escapeHTML(state.currentProject?.name || "โปรเจกต์")}</p><h3>${selected ? escapeHTML(selected.title) : "สร้างงานใหม่"}</h3>${selected ? "" : "<p>กำหนดเป้าหมายและเกณฑ์สำเร็จ แล้วสร้างแผนให้ตรวจ</p>"}</div></div>
    ${taskErrors.get(selected?.id) || taskErrors.get("create") ? `<div class="task-feedback" role="alert">${escapeHTML(taskFriendlyError(taskErrors.get(selected?.id) || taskErrors.get("create")))}</div>` : ""}
    ${selected ? `<div class="task-detail-toolbar"><button class="ghost" type="button" id="taskBackToOverview">← งานทั้งหมด</button><span>${tasks.length} งานในโปรเจกต์นี้</span></div>` : ""}
    ${selected ? `<div id="taskCurrentAction" class="task-current-action">${taskPlanAwaitsReview(selected) ? `<p class="task-current-caption">ตรวจแผนก่อนเริ่มงาน</p><button class="primary" type="button" data-task-jump="taskPlanReview">ไปตรวจและยืนยันแผน</button>` : selected.plan?.review_status === "rejected" ? `<p class="task-current-caption">แผนถูกส่งกลับแก้</p><button class="primary" type="button" data-task-jump="taskAutoPlan">สร้างแผนใหม่</button>` : !selected.active_plan_revision ? `<p class="task-current-caption">ยังไม่มีแผนสำหรับเป้าหมายนี้</p><button class="primary" type="button" data-task-jump="taskAutoPlan">ไปสร้างแผน</button>` : taskExecutionHTML(selected, execution)}</div>` : ""}
    ${selected ? taskFlowHTML(stage, jump) : ""}
    ${selected ? `<nav class="task-section-nav" aria-label="ส่วนของงาน"><a href="#taskCurrentAction">ทำต่อ</a><a href="#taskGoal">เป้าหมาย</a><a href="#taskPlan">แผน</a><a href="#taskResults">ผลและหลักฐาน</a></nav>` : ""}
    <details class="task-create" ${!selected || draftObjective ? "open" : ""}><summary>${selected ? "+ เพิ่มงานใหม่" : "+ สร้างงานใหม่"}</summary><form id="durableTaskForm"><fieldset ${taskActions.has("create") ? "disabled" : ""}>
      <label>ต้องการให้งานนี้ได้ผลอะไร?<textarea name="objective" required rows="3" placeholder="อธิบายสิ่งที่ต้องการสร้างหรือแก้ไข">${escapeHTML(draftObjective)}</textarea></label>
      <label>ผลที่คาดว่าจะเห็นหรือใช้งานได้<textarea name="experience_preview" rows="3" placeholder="เช่น หน้าเริ่มงานแสดงสรุปเป้าหมาย แผน และปุ่มส่งกลับแก้ก่อนเริ่มทำ">${escapeHTML(draft.experience_preview || "")}</textarea></label>
      <label>จะรู้ได้อย่างไรว่าสำเร็จ?<textarea name="criteria" required rows="3" placeholder="หนึ่งผลที่ตรวจได้ต่อหนึ่งบรรทัด">${escapeHTML(draft.criteria || "")}</textarea></label>
      <p class="task-draft-note">นี่คือแบบร่าง แก้เป้าหมาย ผลที่คาดว่าจะเห็น และเกณฑ์ตรวจได้ก่อนสร้างแผน การสร้างแผนยังไม่เริ่มแก้โปรเจกต์</p>
      <details><summary>รายละเอียดเพิ่มเติม</summary><label>ชื่อสั้น ๆ · ไม่บังคับ<input name="title" maxlength="160" value="${escapeHTML(draft.title || "")}"></label><label>ขอบเขตงาน<textarea name="scope" rows="2">${escapeHTML(draft.scope || "")}</textarea></label><label>สมมติฐาน · หนึ่งข้อต่อบรรทัด<textarea name="assumptions" rows="2">${escapeHTML(draft.assumptions || "")}</textarea></label><label>เส้นทางที่ได้รับผล · หนึ่งรายการต่อบรรทัด<textarea name="affected_journeys" rows="2">${escapeHTML(draft.affected_journeys || "")}</textarea></label><label>วิธียืนยันผลที่ต้องการ<textarea name="verification" rows="2">${escapeHTML(draft.verification || "")}</textarea></label><label>ข้อจำกัด · หนึ่งข้อต่อบรรทัด<textarea name="constraints" rows="2">${escapeHTML(draft.constraints || "")}</textarea></label><label>เรื่องที่ยังต้องรู้ · หนึ่งข้อต่อบรรทัด<textarea name="unknowns" rows="2">${escapeHTML(draft.unknowns || "")}</textarea></label><label class="task-acceptance-choice"><input type="checkbox" name="require_user_acceptance" ${draft.require_user_acceptance ? "checked" : ""}>ให้ฉันตรวจและรับรองผลก่อนปิดงาน</label><label class="task-acceptance-choice"><input type="checkbox" name="allow_remote_research" ${draft.allow_remote_research ? "checked" : ""}>อนุญาตบริการภายนอกสำหรับงานนี้ (คำค้นไป Crossref และอาจใช้โมเดลภายนอก)</label></details>
      <p>โปรเจกต์: ${escapeHTML(state.currentProject?.name || "เลือกโปรเจกต์ก่อน")}</p><button class="primary" ${state.currentProject?.id ? "" : "disabled"}>${taskActions.has("create") ? "กำลังสร้าง…" : state.taskDraftAutoPlan ? "สร้างร่างแผนให้ตรวจ" : "สร้างงาน"}</button></fieldset></form></details>
    ${selected ? `<section class="inspect-section" id="taskGoal"><div class="provider-head"><div><h3>เป้าหมายของงาน</h3></div><span>${steps.filter(step => step.state === "completed").length}/${steps.length} ขั้น</span></div>
      ${selected.pause_reason ? `<details class="task-pause-detail"><summary>รายละเอียดทางเทคนิคของสถานะ</summary><p class="session-error">${escapeHTML(selected.pause_reason)}</p></details>` : ""}
      ${steps.length ? `<section class="task-plan-brief" aria-label="สรุปก่อนทำงาน"><div><span class="task-brief-label">เป้าหมาย</span><p>${escapeHTML(selected.objective)}</p></div><div><span class="task-brief-label">ภาพที่คาดว่าจะได้</span><p>${escapeHTML(selected.requirement?.contract?.experience_preview || selected.plan?.reason || "ยังไม่ได้ระบุภาพที่คาดว่าจะได้ · เพิ่มได้ในแก้เป้าหมายหรือเกณฑ์")}</p></div><div><span class="task-brief-label">วิธีรู้ว่าสำเร็จ</span><ul>${(selected.requirement?.criteria || []).map(item => `<li>${escapeHTML(item.description)}</li>`).join("")}</ul></div></section>` : ""}
      <details><summary>เกณฑ์สำเร็จและข้อจำกัด</summary>${selected.requirement?.contract?.scope ? `<p><strong>ขอบเขต:</strong> ${escapeHTML(selected.requirement.contract.scope)}</p>` : ""}<ul>${(selected.requirement?.criteria || []).map(item => `<li>${escapeHTML(item.description)}</li>`).join("")}</ul>${(selected.requirement?.contract?.assumptions || []).map(item => `<p>สมมติฐาน: ${escapeHTML(item)}</p>`).join("")}${(selected.requirement?.contract?.affected_journeys || []).map(item => `<p>เส้นทางที่ได้รับผล: ${escapeHTML(item)}</p>`).join("")}${selected.requirement?.contract?.verification ? `<p>วิธียืนยันผล: ${escapeHTML(selected.requirement.contract.verification)}</p>` : ""}${(selected.requirement?.constraints || []).map(item => `<p>${escapeHTML(item)}</p>`).join("")}${(selected.requirement?.unknowns || []).map(item => `<p>เรื่องที่ยังต้องรู้: ${escapeHTML(item)}</p>`).join("")}${selected.requirement?.contract?.reviewer_policy === "independent_code_changes" ? `<p>การแก้โค้ดต้องผ่านการตรวจอีกครั้งด้วยโปรไฟล์แยก พร้อมหลักฐานการทดสอบ</p>` : ""}${selected.requirement?.contract?.require_user_acceptance ? `<p>ต้องให้ผู้ใช้รับรองผลก่อนปิดงาน</p>` : ""}</details>
      ${selected.state !== "completed" && selected.state !== "cancelled" ? `<details class="task-revise" ${taskRevisionDrafts.has(selected.id) ? "open" : ""}><summary>แก้เป้าหมาย ภาพที่คาดว่าจะได้ หรือเกณฑ์</summary><p>การบันทึกจะสร้าง Goal ฉบับใหม่และต้องสร้างแผนใหม่ หลักฐานเดิมยังดูได้แต่ใช้ปิดงานฉบับใหม่นี้ไม่ได้</p><form id="taskReviseForm"><fieldset ${!canReviseGoal ? "disabled" : ""}><label>ผลลัพธ์ที่ต้องการ<textarea name="outcome" required rows="2">${escapeHTML(revisionDraft.outcome)}</textarea></label><label>ภาพหน้าจอหรือ flow ที่คาดว่าจะได้<textarea name="experience_preview" rows="3">${escapeHTML(revisionDraft.experience_preview)}</textarea></label><label>เกณฑ์ที่ตรวจได้ · หนึ่งข้อต่อบรรทัด<textarea name="criteria" required rows="3">${escapeHTML(revisionDraft.criteria)}</textarea></label><label>ขอบเขต<textarea name="scope" rows="2">${escapeHTML(revisionDraft.scope)}</textarea></label><label>สมมติฐาน · หนึ่งข้อต่อบรรทัด<textarea name="assumptions" rows="2">${escapeHTML(revisionDraft.assumptions)}</textarea></label><label>เส้นทางที่ได้รับผล · หนึ่งรายการต่อบรรทัด<textarea name="affected_journeys" rows="2">${escapeHTML(revisionDraft.affected_journeys)}</textarea></label><label>วิธียืนยันผล<textarea name="verification" rows="2">${escapeHTML(revisionDraft.verification)}</textarea></label><label>ข้อจำกัด · หนึ่งข้อต่อบรรทัด<textarea name="constraints" rows="2">${escapeHTML(revisionDraft.constraints)}</textarea></label><label>เรื่องที่ยังต้องรู้ · หนึ่งข้อต่อบรรทัด<textarea name="unknowns" rows="2">${escapeHTML(revisionDraft.unknowns)}</textarea></label><label class="task-acceptance-choice"><input type="checkbox" name="require_user_acceptance" ${revisionDraft.require_user_acceptance ? "checked" : ""}>ให้ฉันตรวจและรับรองผลก่อนปิดงาน</label><button class="ghost" type="submit">บันทึก Goal ฉบับใหม่</button></fieldset></form>${!canReviseGoal ? `<p>มีงานหรือผลการกระทำที่ยังไม่สิ้นสุด ตรวจและจัดการสิ่งที่ค้างก่อนแก้เป้าหมาย</p>` : ""}</details>` : ""}
      ${(selected.requirement?.contract?.clarifications || []).length ? `<details class="task-clarification-history"><summary>คำตอบที่บันทึกใน Goal · ${selected.requirement.contract.clarifications.length}</summary><ol>${selected.requirement.contract.clarifications.map(item => `<li><strong>${escapeHTML(item.question)}</strong><p>${escapeHTML(item.answer)}</p></li>`).join("")}</ol></details>` : ""}
      ${(selected.requirement?.unknowns || []).length && !["completed","cancelled"].includes(selected.state) ? `<section class="task-clarification" aria-labelledby="taskClarificationHeading"><h4 id="taskClarificationHeading">ตอบเรื่องที่ยังต้องรู้</h4><p>คำตอบจะสร้าง Goal ฉบับใหม่ และต้องตรวจแผนกับหลักฐานอีกครั้ง</p><form id="taskClarificationForm"><fieldset ${!canReviseGoal ? "disabled" : ""}><label>คำถาม<select name="question">${selected.requirement.unknowns.map(question => `<option value="${escapeHTML(question)}" ${question === clarificationDraft.question ? "selected" : ""}>${escapeHTML(question)}</option>`).join("")}</select></label><label>คำตอบ<textarea name="answer" rows="3" maxlength="2000" required>${escapeHTML(clarificationDraft.answer)}</textarea></label><button class="ghost" type="submit">บันทึกคำตอบและทบทวนแผน</button></fieldset></form>${!canReviseGoal ? `<p>จัดการงานหรือผลการกระทำที่ค้างก่อนตอบคำถามนี้</p>` : ""}</section>` : ""}
      <div id="taskPlan">${!steps.length ? `<label>โมเดลวางแผน<select id="taskPlannerProvider">${taskProviderOptions(providers)}</select></label><button class="primary" id="taskAutoPlan" ${busy || !providers.length ? "disabled" : ""}>${taskActions.get(selected.id) || "สร้างแผน"}</button>` : `<section class="task-plan-reference" aria-label="แผนที่ใช้อ้างอิง"><div class="task-plan-reference-head"><h4>แผนฉบับ ${selected.plan.revision}</h4>${pill(selected.plan.review_status === "approved" ? "ยืนยันแล้ว" : selected.plan.review_status === "rejected" ? "ส่งกลับแก้" : taskPlanAwaitsReview(selected) ? "รอตรวจ" : "ใช้งานแล้ว", selected.plan.review_status === "approved" || !taskPlanAwaitsReview(selected) ? "green" : "amber")}</div><p>${escapeHTML(selected.plan.reason || "ขั้นตอนที่จะทำตามเป้าหมายนี้")}</p><details class="task-plan-all-steps" ${taskPlanAwaitsReview(selected) ? "open" : ""}><summary>ดูขั้นตอนและวิธีตรวจ · ${steps.length} ขั้น</summary><ol class="task-plan-steps">${steps.map((step, index) => `<li><details ${index === 0 && taskPlanAwaitsReview(selected) ? "open" : ""}><summary><strong>${escapeHTML(step.title)}</strong> ${pill(step.state, step.state === "completed" ? "green" : step.state === "failed" ? "red" : "blue")}</summary><p>${escapeHTML(step.instructions)}</p><p><strong>วิธีตรวจ</strong></p><ul>${(step.checks || []).map(check => `<li><code>${escapeHTML(check)}</code></li>`).join("")}</ul><small>เกณฑ์ที่ขั้นนี้ต้องพิสูจน์: ${escapeHTML((step.requirement_ids || []).map(id => selected.requirement?.criteria?.find(item => item.id === id)?.description || id).join(" · "))}</small></details></li>`).join("")}</ol></details></section><p class="form-note neutral">${taskPlanReferenceNote(selected)}</p>`}</div>
      ${taskPlanDiffHTML(selected, execution?.previousPlan)}
      ${taskPlanAwaitsReview(selected) ? `<section class="task-plan-review" id="taskPlanReview" aria-labelledby="taskPlanReviewHeading"><h4 id="taskPlanReviewHeading" tabindex="-1">ตรวจแบบและแผนก่อนเริ่ม</h4><p>ตรวจเป้าหมาย ภาพที่คาดว่าจะได้ ขั้นตอน และวิธีพิสูจน์ด้านบน คุณแก้ Goal หรือสร้างแผนใหม่ได้จนกว่าจะตรงใจ การยืนยันผูกกับแผนฉบับนี้เท่านั้น</p><label>ข้อเสนอแนะถ้าส่งกลับแก้<textarea id="taskPlanReviewReason" rows="2" maxlength="2000" placeholder="บอกส่วนที่ต้องแก้">${escapeHTML(taskPlanReviewFeedbacks.get(selected.id) || "")}</textarea></label><div class="action-row"><button class="primary" id="taskApprovePlan" ${busy ? "disabled" : ""}>ยืนยันแผนเพื่อเริ่มงาน</button><button class="ghost" id="taskRejectPlan" ${busy ? "disabled" : ""}>ส่งกลับแก้</button></div></section>` : selected.plan?.review_status === "rejected" ? `<section class="task-feedback" role="status"><strong>แผนฉบับนี้ถูกส่งกลับ</strong><p>${escapeHTML(selected.plan.review_reason || "โปรดแก้ Goal หรือสร้างแผนใหม่ก่อนเริ่มงาน")}</p><p>เมื่อสร้างแผนใหม่ ระบบจะนำข้อเสนอแนะนี้ไปประกอบการวางแผน และให้คุณตรวจอีกครั้ง</p></section>` : ""}
      ${steps.length && canReplan ? `<details><summary>ปรับแผนนี้</summary><p>สร้างแผนฉบับใหม่จากเป้าหมายและเกณฑ์สำเร็จเดิม แล้วตรวจแผนก่อนเริ่มอีกครั้ง</p><label>โมเดลวางแผน<select id="taskPlannerProvider">${taskProviderOptions(providers)}</select></label><button class="ghost" id="taskAutoPlan" ${providers.length ? "" : "disabled"}>สร้างแผนฉบับใหม่</button></details>` : ""}
      ${!providers.length ? `<p class="session-error">No ready ${selected.egress_policy === "remote_allowed" ? "" : "local "}model is configured for this task. Connect one or fix its credentials in Models.</p><button class="ghost" data-task-models>Open Models</button>` : ""}
      ${providers.length && !providers.some(provider => taskEligibleProviders(selected, provider.id).length) ? `<p class="task-readiness">วางแผนและเสนอการแก้ไขได้ แต่การตรวจงานขั้นสุดท้ายต้องมีโปรไฟล์อีกตัวสำหรับการตรวจรอบแยกใน Models</p>` : ""}
      <div id="taskResults">${taskCompletionHTML(selected, execution)}</div>
    </section>${taskActivityHTML(execution)}<details id="taskOptionalTools" class="task-optional-tools" ${taskOptionalToolsOpen.has(selected.id) || (execution?.research || []).length || execution?.content?.brief || (execution?.socialPosts || []).length ? "open" : ""}><summary>เครื่องมือคอนเทนต์ · ค้นแหล่งข้อมูลและเตรียมโพสต์</summary>${taskResearchHTML(selected, execution)}${taskContentHTML(selected, execution)}</details>${taskDesktopActionsHTML(selected, execution)}${taskBrowserActionsHTML(selected, execution)}${taskDecisionLabHTML(selected)}` : ""}</div>`;
  $("#taskOptionalTools")?.addEventListener("toggle", event => {
    if (event.currentTarget.open) taskOptionalToolsOpen.add(selected.id);
    else taskOptionalToolsOpen.delete(selected.id);
  });
  $("#durableTaskForm")?.addEventListener("submit", createDurableTask);
  $("#taskReviseForm")?.addEventListener("submit", event => reviseDurableTask(event, body));
  $("#taskClarificationForm")?.addEventListener("submit", event => clarifyDurableTask(event, body));
  $("#taskClarificationForm")?.addEventListener("input", event => {
    const form = new FormData(event.currentTarget);
    persistTaskClarificationDraft(selected, {question:String(form.get("question") || ""), answer:String(form.get("answer") || "")});
  });
  $("#taskReviseForm")?.addEventListener("input", event => {
    const form = new FormData(event.currentTarget);
    const values = Object.fromEntries(["outcome","experience_preview","criteria","scope","assumptions","affected_journeys","verification","constraints","unknowns"].map(name => [name, String(form.get(name) || "")]));
    values.require_user_acceptance = form.has("require_user_acceptance");
    taskRevisionDrafts.set(selected.id, values);
  });
  $("#durableTaskForm")?.addEventListener("input", event => {
    const form = new FormData(event.currentTarget);
    state.taskDraftObjective = String(form.get("objective") || "");
    state.taskDraftDetails = Object.fromEntries(["title", "experience_preview", "criteria", "scope", "assumptions", "affected_journeys", "verification", "constraints", "unknowns"].map(name => [name, String(form.get(name) || "")]));
    state.taskDraftDetails.require_user_acceptance = form.has("require_user_acceptance");
    state.taskDraftDetails.allow_remote_research = form.has("allow_remote_research");
    rememberProjectDraft();
  });
  if (draftObjective && !selected) $("#durableTaskForm textarea[name='objective']")?.focus();
  $("#durableTaskSelect")?.addEventListener("change", async event => {
    state.selectedDurableTask = event.target.value;
    rememberSurface(state.workspaceBeforePlans ? "plans" : "code");
    await loadTaskExecution(state.selectedDurableTask);
    if (body.isConnected) renderTaskCockpit(body);
  });
  $("#taskBackToOverview")?.addEventListener("click", () => {
    state.plansOverview = true;
    renderPanes();
  });
  $$('[data-board-task]', body).forEach(button => button.addEventListener("click", async () => {
    if (state.selectedDurableTask === button.dataset.boardTask) return;
    state.selectedDurableTask = button.dataset.boardTask;
    rememberSurface(state.workspaceBeforePlans ? "plans" : "code");
    await loadTaskExecution(state.selectedDurableTask);
    if (body.isConnected) renderTaskCockpit(body);
  }));
  $$('[data-task-models]', body).forEach(button => button.addEventListener("click", () => openConfig("providers")));
  $("#taskAutoPlan")?.addEventListener("click", () => autoPlanDurableTask(body));
  $("#taskProposalForm")?.addEventListener("submit", event => startTaskProposal(event, body));
  $$('[data-task-proposal-decision]', body).forEach(button => button.addEventListener("click", () => decideTaskProposal(button.dataset.taskProposalDecision, body)));
  $("#taskApplyProposal")?.addEventListener("click", () => applyTaskProposal(body));
  $("#taskVerifyProposal")?.addEventListener("click", () => verifyTaskProposal(body));
  $("#taskPostReview")?.addEventListener("click", () => postReviewTaskProposal(body));
  $("#taskReviewerProvider")?.addEventListener("change", event => { const note = $("#taskReviewIdentity"); if (note) note.textContent = taskReviewIdentityText(state.taskExecutions[selected.id]?.proposal?.provider_id, event.target.value); });
  $("#taskReconcileEffects")?.addEventListener("click", () => reconcileTaskEffects(body));
  $("#taskRecoverExpiredRun")?.addEventListener("click", () => recoverExpiredTaskRun(body));
  $("#taskResumeObservedReview")?.addEventListener("click", () => resumeObservedTaskReview(body));
  $("#taskResumeApprovedReview")?.addEventListener("click", () => resumeApprovedTaskReview(body));
  $("#taskRetryFailedProposal")?.addEventListener("click", () => retryFailedTaskProposal(body));
  $("#taskRetryRejectedProposal")?.addEventListener("click", () => retryRejectedTaskProposal(body));
  $("#taskDecisionShadow")?.addEventListener("click", () => compareTaskDecision(body));
  $("#taskDecisionBenchmark")?.addEventListener("click", () => benchmarkTaskDecision(body));
  $("#taskDecisionAdmission")?.addEventListener("click", event => changeTaskDecisionAdmission(event, body));
  $("#taskDecisionRecommend")?.addEventListener("click", () => recommendTaskDecision(body));
  $("#taskReload")?.addEventListener("click", () => refreshDurableTask(selected.id));
  $("#taskComplete")?.addEventListener("click", () => completeDurableTaskFromCockpit(body));
  $("#taskAcceptResult")?.addEventListener("click", () => acceptDurableTaskResult(body));
  $("#taskApprovePlan")?.addEventListener("click", () => reviewDurablePlan("approved", body));
  $("#taskRejectPlan")?.addEventListener("click", () => reviewDurablePlan("rejected", body));
  $$('[data-desktop-cancel]', body).forEach(button => button.addEventListener("click", () => cancelTaskDesktopAction(selected.id, button.dataset.desktopCancel, button.dataset.subjectHash, body)));
  $$('[data-desktop-observe]', body).forEach(button => button.addEventListener("click", () => observeTaskDesktopAction(selected.id, button.dataset.desktopObserve, body)));
  $$('[data-desktop-reconcile]', body).forEach(button => button.addEventListener("click", () => reconcileTaskDesktopAction(selected.id, button.dataset.desktopReconcile, button.dataset.resolution, body)));
  $$('[data-browser-cancel]', body).forEach(button => button.addEventListener("click", () => cancelTaskBrowserAction(selected.id, button.dataset.browserCancel, button.dataset.subjectHash, body)));
  $$('[data-browser-verify-tab]', body).forEach(select => select.addEventListener("change", event => { taskBrowserVerificationTabs.set(select.dataset.browserVerifyTab,event.target.value); renderTaskCockpit(body); }));
  $$('[data-browser-observe]', body).forEach(button => button.addEventListener("click", () => observeTaskBrowserAction(selected.id, button.dataset.browserObserve, body)));
  $$('[data-browser-reconcile]', body).forEach(button => button.addEventListener("click", () => reconcileTaskBrowserAction(selected.id, button.dataset.browserReconcile, button.dataset.resolution, body)));
  $("#taskPlanReviewReason")?.addEventListener("input", event => taskPlanReviewFeedbacks.set(selected.id, event.target.value));
  $("#taskResearchForm")?.addEventListener("input", event => {
    const form = new FormData(event.currentTarget);
    taskResearchDrafts.set(selected.id, {query:String(form.get("query") || ""), days:Number(form.get("days") || 30)});
  });
  $("#taskResearchForm")?.addEventListener("submit", event => searchTaskResearch(event, body));
  body.querySelectorAll("[data-research-select]").forEach(button => button.addEventListener("click", () => selectTaskResearch(selected.id, button.dataset.researchSelect, button.dataset.selected === "true", body)));
  $("#taskContentBriefForm")?.addEventListener("input", event => { const form = new FormData(event.currentTarget); taskContentBriefDrafts.set(selected.id, Object.fromEntries(["topic","audience","language","facebook_page","tiktok_account","freshness_days"].map(name => [name, String(form.get(name) || "")]))); });
  $("#taskContentBriefForm")?.addEventListener("submit", event => saveTaskContentBrief(event, body));
  $("#taskContentClaimForm")?.addEventListener("input", event => {
    const element = event.currentTarget;
    const form = new FormData(element);
    const draft = Object.fromEntries(["source_id","statement","evidence_excerpt","corroborating_source_id","corroborating_excerpt"].map(name => [name, String(form.get(name) || "")]));
    draft.consequential = form.has("consequential");
    taskContentClaimDrafts.set(selected.id, draft);
    const second = element.querySelector("[data-claim-second]");
    if (second) second.hidden = !draft.consequential;
    second?.querySelectorAll("select, textarea").forEach(input => { input.required = draft.consequential; });
  });
  $("#taskContentClaimForm")?.addEventListener("submit", event => createTaskContentClaim(event, body));
  body.querySelectorAll("[data-claim-review]").forEach(button => button.addEventListener("click", () => reviewTaskContentClaim(selected.id, button.dataset.claimReview, button.dataset.verdict, body)));
  body.querySelectorAll("[data-claim-remove]").forEach(button => button.addEventListener("click", () => removeTaskContentClaim(selected.id, button.dataset.claimRemove, body)));
  body.querySelectorAll("[data-content-draft]").forEach(form => {
    form.addEventListener("input", () => { const values = new FormData(form); taskContentDraftEdits.set(`${selected.id}:${form.dataset.contentDraft}`, Object.fromEntries(["body","script","storyboard","media_artifact_id","media_rights_note"].map(name => [name, String(values.get(name) || "")]))); });
    form.addEventListener("submit", event => saveTaskContentDraft(event, selected.id, body));
  });
  body.querySelectorAll("[data-content-approve]").forEach(button => button.addEventListener("click", () => approveTaskContentDraft(selected.id, button.dataset.contentApprove, body)));
  $("#taskContentSuggest")?.addEventListener("click", () => suggestTaskContent(selected.id, body));
  $("#taskSocialSettings")?.addEventListener("click", () => openConfig("mcp"));
  body.querySelectorAll("[data-creator-check]").forEach(button => {
    const connectionID = button.dataset.creatorCheck;
    button.addEventListener("click", () => checkTaskTikTokCreatorInfo(selected.id, connectionID));
    const cachedCreator = taskSocialCreatorInfo.get(connectionID);
    const checkedAt = Date.parse(cachedCreator?.checked_at || "");
    if (!cachedCreator || !cachedCreator.error && Number.isFinite(checkedAt) && Date.now() - checkedAt > 5 * 60 * 1000) {
      taskSocialCreatorInfo.set(connectionID, {error:"กำลังตรวจบัญชี…"});
      Promise.resolve().then(() => checkTaskTikTokCreatorInfo(selected.id, connectionID));
    }
  });
  body.querySelectorAll("[data-social-intent]").forEach(form => {
    const draftKey = `${selected.id}:${form.dataset.socialIntent}:${form.dataset.connectionId}`;
    const saved = taskSocialExportDrafts.get(draftKey);
    if (saved) form.querySelectorAll("input[name], select[name]").forEach(input => {
      if (input.type === "checkbox") input.checked = !input.disabled && Boolean(saved[input.name]);
      else if (saved[input.name] != null && (input.tagName !== "SELECT" || [...input.options].some(option => option.value === saved[input.name]))) input.value = saved[input.name];
    });
    const saveExportDraft = () => { const values = new FormData(form); taskSocialExportDrafts.set(draftKey, Object.fromEntries(values.entries())); };
    const disclosure = form.querySelector('[name="commercial_disclosure"]');
    const commercialOptions = form.querySelector('[data-commercial-options]');
    const updateCommercial = () => {
      if (!disclosure || !commercialOptions) return;
      commercialOptions.hidden = !disclosure.checked;
      if (!disclosure.checked) commercialOptions.querySelectorAll('input[type="checkbox"]').forEach(input => { input.checked = false; });
      const branded = Boolean(disclosure.checked && form.querySelector('[name="brand_content"]')?.checked);
      for (const value of ["SELF_ONLY", "FOLLOWER_OF_CREATOR"]) {
        const option = form.querySelector(`[name="privacy_level"] option[value="${value}"]`);
        if (option) option.disabled = branded;
      }
      const privacy = form.querySelector('[name="privacy_level"]');
      if (branded && ["SELF_ONLY", "FOLLOWER_OF_CREATOR"].includes(privacy?.value)) privacy.value = "";
      const note = form.querySelector('[data-commercial-note]');
      if (note) note.textContent = branded ? "TikTok จะแสดงป้ายกำกับผู้สนับสนุน และต้องเลือกสาธารณะหรือเพื่อน" : form.querySelector('[name="brand_organic"]')?.checked ? "TikTok จะแสดงป้ายกำกับเนื้อหาโปรโมต" : "เลือกประเภทที่ตรงกับวิดีโอนี้";
    };
    disclosure?.addEventListener("change", updateCommercial);
    commercialOptions?.addEventListener("change", updateCommercial);
    updateCommercial();
    form.addEventListener("input", saveExportDraft);
    form.addEventListener("change", saveExportDraft);
    form.addEventListener("submit", event => prepareTaskSocialPost(event, selected.id, body));
  });
  body.querySelectorAll("[data-social-approve]").forEach(button => button.addEventListener("click", () => approveTaskSocialPost(selected.id, button.dataset.socialApprove, body)));
  body.querySelectorAll("[data-social-dispatch]").forEach(button => {
    const post = state.taskExecutions[selected.id]?.socialPosts?.find(item => item.id === button.dataset.socialDispatch);
    if (post?.route === "tiktok_draft") button.textContent = "อัปโหลดเป็นร่าง";
    button.addEventListener("click", () => dispatchTaskSocialPost(selected.id, button.dataset.socialDispatch, body));
  });
  body.querySelectorAll("[data-social-refresh]").forEach(button => button.addEventListener("click", () => refreshTaskSocialPost(selected.id, button.dataset.socialRefresh, body)));
  const exportStep = [...body.querySelectorAll(".task-content-step")].find(section => section.querySelector("h4")?.textContent.startsWith("6 ·"));
  const approvedDrafts = (state.taskExecutions[selected.id]?.content?.drafts || []).filter(draft => draft.approved);
  exportStep?.querySelectorAll(".task-content-draft").forEach((article, index) => {
    const draft = approvedDrafts[index];
    const posts = (state.taskExecutions[selected.id]?.socialPosts || []).filter(post => post.draft_id === draft?.id);
    article.querySelectorAll(".task-content-preview").forEach((preview, postIndex) => {
      const post = posts[postIndex];
      if (!post) return;
      const summary = preview.querySelectorAll("p")[1];
      const route = {facebook_feed:"โพสต์ข้อความ",facebook_reel:"Facebook Reel",tiktok_direct:"TikTok Direct Post",tiktok_draft:"ส่งวิดีโอเป็นร่าง"}[post.route] || post.route;
      const visibility = post.route === "tiktok_draft" ? "ตั้งการมองเห็นและป้าย AI ใน TikTok" : post.privacy_level || "ตาม Page";
      if (summary) summary.textContent = `บัญชี: ${draft.destination} · ${route} · ${visibility} · ${post.is_aigc ? "สื่อนี้สร้างด้วย AI" : "ไม่ได้ระบุว่าสื่อสร้างด้วย AI"}${post.route === "tiktok_direct" ? ` · ${post.brand_content ? "มีเนื้อหาแบรนด์" : "ไม่มีเนื้อหาแบรนด์"} · ${post.brand_organic ? "โปรโมตสินค้า/บริการของตนเอง" : "ไม่โปรโมตสินค้า/บริการของตนเอง"} · ความคิดเห็น ${post.allow_comment ? "เปิด" : "ปิด"} · Duet ${post.allow_duet ? "เปิด" : "ปิด"} · Stitch ${post.allow_stitch ? "เปิด" : "ปิด"}` : ""}`;
      if (post.state === "failed" && (!post.remote_id || post.remote_status === "FAILED")) {
        const button = document.createElement("button");
        button.type = "button";
        button.className = "ghost";
        button.textContent = "เตรียมส่งใหม่หลังแก้ปัญหา";
        button.addEventListener("click", () => retryTaskSocialPost(selected.id, post.id, body));
        preview.querySelector(".action-row")?.append(button);
      }
    });
  });
  $("#taskComfyPreflightForm")?.addEventListener("input", event => { taskComfyPaths.set(selected.id, event.currentTarget.elements.workflow_path.value); taskComfyPreflights.delete(selected.id); });
  $("#taskComfyPreflightForm")?.addEventListener("submit", event => preflightTaskComfy(event, selected.id, body));
  $("#taskComfyStart")?.addEventListener("click", () => startTaskComfy(selected.id, body));
  body.querySelectorAll("[data-comfy-refresh]").forEach(button => button.addEventListener("click", () => refreshTaskComfy(selected.id, button.dataset.comfyRefresh, body)));
  body.querySelectorAll("[data-comfy-cancel]").forEach(button => button.addEventListener("click", () => cancelTaskComfy(selected.id, button.dataset.comfyCancel, body)));
  const jumpToTaskControl = targetID => {
    const target = body.querySelector(`#${targetID}`) || body.querySelector('[data-task-models]');
    if (!target) return;
    for (let disclosure = target.closest("details"); disclosure; disclosure = disclosure.parentElement?.closest("details")) disclosure.open = true;
    target.scrollIntoView({block:"center", behavior:"instant"});
    target.focus({preventScroll:true});
  };
  $$('[data-task-jump]', body).forEach(button => button.addEventListener("click", event => {
    pendingTaskJump = {taskID:selected.id, targetID:event.currentTarget.dataset.taskJump, expires:Date.now()+15000};
    jumpToTaskControl(pendingTaskJump.targetID);
  }));
  maintainTaskLease(selected, execution, body);
  if (selected && !Object.prototype.hasOwnProperty.call(state.taskExecutions, selected.id)) {
    state.taskExecutions[selected.id] = null;
    loadTaskExecution(selected.id).then(() => renderCurrentTaskCockpit(selected.id));
  }
  if (selected && !taskDecisionLab(selected.id).loaded && !taskDecisionLab(selected.id).loading) {
    loadTaskDecisionLab(selected.id).then(() => renderCurrentTaskCockpit(selected.id));
  }
  if (pendingTaskJump && selected && pendingTaskJump.taskID === selected.id) {
    requestAnimationFrame(() => {
      if (pendingTaskJump?.taskID !== selected.id || Date.now() > pendingTaskJump.expires) { pendingTaskJump = null; return; }
      jumpToTaskControl(pendingTaskJump.targetID);
    });
  }
}

function renderPlansOverview(body) {
  const tasks = state.durableTasks.slice();
  const active = tasks.filter(task => !["completed", "cancelled"].includes(task.state));
  const blocked = active.filter(task => taskBoardLane(task) === "blocked");
  const next = active.filter(task => !blocked.includes(task));
  const card = task => {
    const project = state.projects.find(item => item.id === task.project_id);
    const steps = task.plan?.steps || [];
    const nextStep = steps.find(step => !["completed", "skipped"].includes(step.state));
    const done = steps.filter(step => ["completed", "skipped"].includes(step.state)).length;
    const stage = taskStage(task, state.taskExecutions[task.id]);
    const nextAction = nextStep ? `ขั้นถัดไป: ${nextStep.title}` : stage.detail;
    return `<article class="plans-overview-card"><div><small>${escapeHTML(project?.name || "Workspace")} · ${escapeHTML(stage.label)} · ${steps.length ? `${done}/${steps.length} ขั้น` : "ยังไม่มีแผน"}</small><h4>${escapeHTML(task.title)}</h4><p>${escapeHTML(nextAction)}</p></div><button class="ghost" type="button" data-plans-task="${escapeHTML(task.id)}">เปิดงาน</button></article>`;
  };
  body.innerHTML = `<section class="plans-overview" aria-labelledby="plansOverviewTitle"><header class="plans-overview-head"><div><p class="eyebrow">Plans</p><h2 id="plansOverviewTitle">ภาพรวมแผนงาน</h2><p>เลือกงานเพื่อดูเป้าหมาย แผน หลักฐาน และสิ่งที่ต้องทำต่อ</p></div><span>${tasks.length} งานล่าสุด</span></header>
    <div class="plans-overview-summary"><div><strong>${blocked.length}</strong><span>ต้องจัดการก่อน</span></div><div><strong>${next.length}</strong><span>กำลังเดินต่อ</span></div><div><strong>${tasks.filter(task => task.state === "completed").length}</strong><span>เสร็จแล้ว</span></div></div>
    <section><h3>ต้องจัดการก่อน</h3>${blocked.length ? blocked.map(card).join("") : `<p class="task-kanban-empty">ไม่มีงานที่ติดขัด</p>`}</section>
    <section><h3>งานที่กำลังเดินต่อ</h3>${next.length ? next.map(card).join("") : `<p class="task-kanban-empty">ยังไม่มีงานที่กำลังทำ</p>`}</section>
    <details><summary>งานที่เสร็จแล้ว · ${tasks.length - active.length}</summary>${tasks.filter(task => !active.includes(task)).map(card).join("") || `<p class="task-kanban-empty">ยังไม่มีงานที่เสร็จแล้ว</p>`}</details>
    ${state.currentProject?.id ? `<div class="action-row"><button class="primary" type="button" id="plansNewTask">เริ่มงานใหม่ใน ${escapeHTML(state.currentProject.name)}</button><button class="ghost" type="button" id="plansChooseWorkspace">เลือก Workspace อื่น</button></div>` : `<button class="primary" type="button" id="plansChooseWorkspace">เลือก Workspace เพื่อเริ่มงานใหม่</button>`}</section>`;
  body.querySelectorAll("[data-plans-task]").forEach(button => button.addEventListener("click", async () => {
    const task = tasks.find(item => item.id === button.dataset.plansTask);
    if (!task) return;
    if (task.project_id !== state.currentProject?.id) {
      const opened = await openProject(task.project_id, {selectSession:false});
      if (!opened) return;
      await openTasks();
    }
    state.selectedDurableTask = task.id;
    state.plansOverview = false;
    rememberSurface("plans");
    renderPanes();
  }));
  body.querySelector("#plansChooseWorkspace")?.addEventListener("click", showPicker);
  body.querySelector("#plansNewTask")?.addEventListener("click", () => { state.plansOverview = false; state.selectedDurableTask = "__new__"; rememberSurface("plans"); renderPanes(); });
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
  const serial = (taskExecutionLoadSerial.get(taskID) || 0) + 1;
  taskExecutionLoadSerial.set(taskID, serial);
  try {
    const execution = await api(`/api/tasks/${encodeURIComponent(taskID)}/execution`);
    if (taskExecutionLoadSerial.get(taskID) !== serial) return;
    const revision = execution.task?.revision;
    if (revision) {
      const [completion, nextAction, previousPlan, research, content, comfyJobs, socialPosts, desktopActions, browserActions, activity] = await Promise.allSettled([
        api(`/api/tasks/${encodeURIComponent(taskID)}/completion?revision=${encodeURIComponent(revision)}`),
        api(`/api/tasks/${encodeURIComponent(taskID)}/next-action?revision=${encodeURIComponent(revision)}`),
        execution.task?.plan?.revision > 1 ? api(`/api/tasks/${encodeURIComponent(taskID)}/plans/${execution.task.plan.revision - 1}`) : Promise.resolve(null),
        api(`/api/tasks/${encodeURIComponent(taskID)}/research`),
        api(`/api/tasks/${encodeURIComponent(taskID)}/content`),
        api(`/api/comfy/jobs?task_id=${encodeURIComponent(taskID)}`),
        api(`/api/tasks/${encodeURIComponent(taskID)}/social/posts`),
        api(`/api/tasks/${encodeURIComponent(taskID)}/desktop/actions`),
        api(`/api/tasks/${encodeURIComponent(taskID)}/browser/actions`),
        api(`/api/tasks/${encodeURIComponent(taskID)}/activity?limit=100`),
      ]);
      if (taskExecutionLoadSerial.get(taskID) !== serial) return;
      execution.completion = completion.status === "fulfilled" ? completion.value : {error:completion.reason.message};
      execution.nextAction = nextAction.status === "fulfilled" ? nextAction.value : {error:nextAction.reason.message};
      execution.previousPlan = previousPlan.status === "fulfilled" ? previousPlan.value : {error:previousPlan.reason.message};
      execution.research = research.status === "fulfilled" ? asList(research.value) : [];
      if (research.status === "rejected") execution.researchError = research.reason.message;
      execution.content = content.status === "fulfilled" ? content.value : {claims:[], drafts:[]};
      execution.comfyJobs = comfyJobs.status === "fulfilled" ? asList(comfyJobs.value) : [];
      execution.socialPosts = socialPosts.status === "fulfilled" ? asList(socialPosts.value) : [];
      execution.desktopActions = desktopActions.status === "fulfilled" ? asList(desktopActions.value) : [];
      if (desktopActions.status === "rejected") execution.desktopActionsError = desktopActions.reason.message;
      execution.browserActions = browserActions.status === "fulfilled" ? asList(browserActions.value) : [];
      if (browserActions.status === "rejected") execution.browserActionsError = browserActions.reason.message;
      execution.activity = activity.status === "fulfilled" ? asList(activity.value) : [];
      if (activity.status === "rejected") execution.activityError = activity.reason.message;
      if (socialPosts.status === "rejected") execution.socialPostsError = socialPosts.reason.message;
      if (comfyJobs.status === "rejected") execution.comfyJobsError = comfyJobs.reason.message;
      if (content.status === "rejected") execution.contentError = content.reason.message;
      state.durableTasks = state.durableTasks.map(item => item.id === taskID && item.revision <= revision ? execution.task : item);
    }
    state.taskExecutions[taskID] = execution;
    const artifactID = execution.proposal?.artifact_id;
    if (artifactID && (!taskProposalBodies.has(artifactID) || taskProposalBodies.get(artifactID).error)) {
      try { taskProposalBodies.set(artifactID, await api(`/api/artifacts/${encodeURIComponent(artifactID)}/content`)); }
      catch (error) { taskProposalBodies.set(artifactID, {error:error.message}); }
    }
  } catch (error) { if (taskExecutionLoadSerial.get(taskID) === serial) state.taskExecutions[taskID] = {error:error.message, effects:[]}; }
}

async function cancelTaskDesktopAction(taskID, actionID, subjectHash, body) {
  if (!taskID || !actionID || !subjectHash) return;
  await performTaskAction(taskID, body, "กำลังยกเลิกคำสั่ง", async () => {
    await api(`/api/desktop/actions/${encodeURIComponent(actionID)}/cancel`, {method:"POST", body:JSON.stringify({subject_hash:subjectHash})});
    toast("ยกเลิกคำสั่งที่ยังไม่เริ่มแล้ว");
  });
}

async function observeTaskDesktopAction(taskID, actionID, body) {
  const action = (state.taskExecutions[taskID]?.desktopActions || []).find(item => item.id === actionID);
  if (!action || action.state !== "uncertain" || action.reconciliation) return;
  await performTaskAction(taskID, body, "กำลังตรวจหน้าต่าง", async () => {
    state.desktopObservation = await api("/api/desktop/observe", {method:"POST", body:JSON.stringify({handle:action.handle, process_id:action.process_id})});
    if (!state.desktopObservation.screenshot_artifact_id) throw new Error("ยังไม่มีภาพหน้าต่างสำหรับตรวจผล");
  });
}

async function reconcileTaskDesktopAction(taskID, actionID, resolution, body) {
  const action = (state.taskExecutions[taskID]?.desktopActions || []).find(item => item.id === actionID);
  const observed = state.desktopObservation;
  if (!action || action.state !== "uncertain" || action.reconciliation || !observed?.screenshot_artifact_id || observed.window?.handle !== action.handle || observed.window?.process_id !== action.process_id) return;
  const note = await askAction({title:resolution === "observed_applied" ? "ยืนยันว่าเกิดผลแล้ว?" : "ยืนยันว่าไม่เกิดผล?",
    message:`ตรวจภาพและหน้าต่างจริงของ ${action.target?.name || action.target?.control_type || "รายการนี้"} ก่อนบันทึกผล ระบบจะตรวจหน้าต่างอีกครั้งและเก็บภาพเป็นหลักฐาน`,
    confirmLabel:"บันทึกผลที่ตรวจ", reasonLabel:"อธิบายสิ่งที่เห็นในหน้าต่าง"});
  if (!note) return;
  await performTaskAction(taskID, body, "กำลังบันทึกผลที่ตรวจ", async () => {
    await api(`/api/desktop/actions/${encodeURIComponent(actionID)}/reconcile`, {method:"POST", body:JSON.stringify({resolution, observation_hash:observed.observation_hash, actor:currentActor(), note})});
    state.desktopObservation = null;
    toast("บันทึกผลพร้อมภาพหลักฐานแล้ว");
  });
}

async function cancelTaskBrowserAction(taskID, actionID, subjectHash, body) {
  if (!taskID || !actionID || !subjectHash) return;
  await performTaskAction(taskID, body, "กำลังยกเลิกคำสั่ง browser", async () => {
    await api(`/api/browser/actions/${encodeURIComponent(actionID)}/cancel`, {method:"POST",body:JSON.stringify({subject_hash:subjectHash})});
    toast("ยกเลิกคำสั่งที่ยังไม่เริ่มแล้ว");
  });
}

async function observeTaskBrowserAction(taskID, actionID, body) {
  const action = (state.taskExecutions[taskID]?.browserActions || []).find(item => item.id === actionID);
  if (!action || action.state !== "uncertain" || action.reconciliation) return;
  await performTaskAction(taskID, body, "กำลังตรวจแท็บ browser", async () => {
    const verificationTabID = taskBrowserVerificationTabs.get(action.id) || action.tab_id;
    const tab = await api(`/api/browser/tabs/${encodeURIComponent(verificationTabID)}/actions`,{method:"POST",body:JSON.stringify({action:"read",actor:currentActor()})});
    state.browserTabs = state.browserTabs.map(item => item.id === tab.id ? tab : item);
    if (!state.browserTabs.some(item => item.id === tab.id)) state.browserTabs.unshift(tab);
    if (!tab.screenshot_artifact_id) throw new Error("ยังไม่มีภาพ browser สำหรับตรวจผล");
  });
}

async function reconcileTaskBrowserAction(taskID, actionID, resolution, body) {
  const action = (state.taskExecutions[taskID]?.browserActions || []).find(item => item.id === actionID);
  const verificationTabID = taskBrowserVerificationTabs.get(actionID) || action?.tab_id;
  const observed = state.browserTabs.find(item => item.id === verificationTabID && item.observation_hash && item.screenshot_artifact_id);
  if (!action || action.state !== "uncertain" || action.reconciliation || !observed) return;
  const note = await askAction({title:resolution === "observed_applied" ? "ยืนยันว่าเกิดผลบนเว็บแล้ว?" : "ยืนยันว่าไม่เกิดผลบนเว็บ?",
    message:`ตรวจภาพและหน้าเว็บจริงของ ${action.target?.text || action.target?.placeholder || "รายการนี้"} ก่อนบันทึก ระบบจะตรวจแท็บอีกครั้งและเก็บภาพ`,
    confirmLabel:"บันทึกผลที่ตรวจ",reasonLabel:"อธิบายสิ่งที่เห็นบนหน้าเว็บ"});
  if (!note) return;
  await performTaskAction(taskID, body, "กำลังบันทึกผล browser", async () => {
    await api(`/api/browser/actions/${encodeURIComponent(actionID)}/reconcile`,{method:"POST",body:JSON.stringify({verification_tab_id:verificationTabID,resolution,observation_hash:observed.observation_hash,actor:currentActor(),note})});
    toast("บันทึกผล browser พร้อมภาพหลักฐานแล้ว");
  });
}

async function completeDurableTaskFromCockpit(body) {
  const task = state.durableTasks.find(item => item.id === state.selectedDurableTask);
  const report = task && state.taskExecutions[task.id]?.completion;
  if (!task || task.state !== "verifying" || !report?.can_complete || report.task_revision !== task.revision) return;
  const confirmed = await askAction({title:"ปิดงานนี้?", message:"Hermetrix จะตรวจเกณฑ์และหลักฐานฉบับปัจจุบันอีกครั้งก่อนบันทึกว่างานเสร็จ", confirmLabel:"ปิดงาน"});
  if (!confirmed) return;
  await performTaskAction(task.id, body, "กำลังปิดงาน", async () => {
    const completed = await api(`/api/tasks/${encodeURIComponent(task.id)}/complete`, {method:"POST", body:JSON.stringify({expected_task_revision:task.revision})});
    state.durableTasks = state.durableTasks.map(item => item.id === task.id ? completed : item);
    toast("ปิดงานพร้อมหลักฐานปัจจุบันแล้ว");
  });
}

async function acceptDurableTaskResult(body) {
  const task = state.durableTasks.find(item => item.id === state.selectedDurableTask);
  const report = task && state.taskExecutions[task.id]?.completion;
  if (!task || !report?.awaiting_user_acceptance || report.task_revision !== task.revision) return;
  const confirmed = await askAction({title:"รับรองผลของงานนี้?", message:"โปรดตรวจผลและหลักฐานที่แสดงอยู่ การรับรองจะผูกกับแผนและหลักฐานฉบับนี้ หากผลตรวจเปลี่ยน ระบบจะขอให้รับรองใหม่", confirmLabel:"รับรองผล"});
  if (!confirmed) return;
  await performTaskAction(task.id, body, "กำลังบันทึกการรับรอง", async () => {
    await api(`/api/tasks/${encodeURIComponent(task.id)}/user-acceptance`, {method:"POST", body:JSON.stringify({expected_task_revision:task.revision, subject_hash:report.subject_hash, actor:currentActor()})});
    toast("รับรองผลแล้ว ตอนนี้ปิดงานได้");
  });
}

async function reviewDurablePlan(verdict, body) {
  const task = state.durableTasks.find(item => item.id === state.selectedDurableTask);
  if (!task || task.plan?.review_status !== "pending" || taskActions.has(task.id)) return;
  const feedback = String($("#taskPlanReviewReason")?.value || "").trim();
  if (verdict === "rejected" && !feedback) {
    taskErrors.set(task.id, "บอกส่วนที่ต้องแก้ก่อนส่งแผนกลับ");
    renderCurrentTaskCockpit(task.id);
    return;
  }
  const confirmed = await askAction({title:verdict === "approved" ? "อนุมัติแผนนี้?" : "ส่งแผนนี้กลับแก้?",
    message:verdict === "approved" ? "โปรดตรวจทุกขั้น วิธีตรวจ และเกณฑ์ที่ต้องพิสูจน์ การอนุมัติใช้ได้เฉพาะแผนฉบับนี้" : "แผนฉบับนี้จะเริ่มงานไม่ได้จนกว่าจะสร้างแผนใหม่",
    confirmLabel:verdict === "approved" ? "อนุมัติแผน" : "ส่งกลับแก้"});
  if (!confirmed) return;
  await performTaskAction(task.id, body, "กำลังบันทึกผลตรวจแผน", async () => {
    const updated = await api(`/api/tasks/${encodeURIComponent(task.id)}/plan-review`, {method:"POST", body:JSON.stringify({
      expected_task_revision:task.revision, subject_hash:task.plan.subject_hash, actor:currentActor(), verdict,
      reason:feedback || "ตรวจขั้นตอน วิธีตรวจ และเกณฑ์ที่ต้องพิสูจน์แล้ว"})});
    state.durableTasks = state.durableTasks.map(item => item.id === task.id ? updated : item);
    taskPlanReviewFeedbacks.delete(task.id);
    toast(verdict === "approved" ? "อนุมัติแผนแล้ว เริ่มทำงานได้" : "ส่งแผนกลับแก้แล้ว");
  });
}

async function saveTaskContentBrief(event, body) {
  event.preventDefault();
  const taskID = state.selectedDurableTask;
  const form = new FormData(event.currentTarget);
  const input = Object.fromEntries(["topic","audience","language","facebook_page","tiktok_account"].map(name => [name, String(form.get(name) || "").trim()]));
  input.freshness_days = Number(form.get("freshness_days") || 30);
  input.revision = Number(form.get("revision") || 0);
  const approved = await askAction({title:"บันทึก brief นี้?", message:"หัวข้อ กลุ่มผู้อ่าน ภาษา และบัญชีปลายทางจะผูกกับงานนี้ การแก้ภายหลังทำให้การอนุมัติร่างเดิมหมดอายุ", confirmLabel:"บันทึก brief"});
  if (!approved) return;
  await performTaskAction(taskID, body, "กำลังบันทึก brief", async () => {
    await api(`/api/tasks/${encodeURIComponent(taskID)}/content/brief`, {method:"PUT", body:JSON.stringify(input)});
    taskContentBriefDrafts.delete(taskID);
    toast("บันทึก brief แล้ว");
  });
}

async function createTaskContentClaim(event, body) {
  event.preventDefault();
  const taskID = state.selectedDurableTask;
  const form = new FormData(event.currentTarget);
  const consequential = form.has("consequential");
  const input = {source_id:String(form.get("source_id") || ""), statement:String(form.get("statement") || "").trim(), evidence_excerpt:String(form.get("evidence_excerpt") || "").trim(),
    consequential, corroborating_source_id:consequential ? String(form.get("corroborating_source_id") || "") : "", corroborating_excerpt:consequential ? String(form.get("corroborating_excerpt") || "").trim() : ""};
  if (consequential && input.corroborating_source_id === input.source_id) { toast("เลือกบทความคนละชิ้นเป็นแหล่งยืนยันที่สอง", true); return; }
  const approved = await askAction({title:"เพิ่มประเด็นนี้ให้ตรวจ?", message:"ข้อความหลักฐานจะถูกเก็บกับแหล่งอ้างอิงที่เลือก และต้องมีผู้ตรวจผลก่อนอนุมัติร่าง", confirmLabel:"เพิ่มประเด็น"});
  if (!approved) return;
  await performTaskAction(taskID, body, "กำลังเพิ่มประเด็น", async () => {
    await api(`/api/tasks/${encodeURIComponent(taskID)}/content/claims`, {method:"POST", body:JSON.stringify(input)});
    taskContentClaimDrafts.delete(taskID);
    toast("เพิ่มประเด็นแล้ว โปรดตรวจหลักฐาน");
  });
}

async function reviewTaskContentClaim(taskID, claimID, verdict, body) {
  const label = {supported:"มีหลักฐานรองรับ", conflict:"ข้อมูลขัดกัน", unsupported:"ไม่มีหลักฐาน"}[verdict];
  const approved = await askAction({title:`บันทึกผลตรวจ: ${label}?`, message:"โปรดเปิดบทความต้นทางและเปรียบเทียบข้อความหลักฐานก่อนบันทึก ผลตรวจนี้จะเปลี่ยนความพร้อมของร่าง", confirmLabel:"บันทึกผลตรวจ"});
  if (!approved) return;
  await performTaskAction(taskID, body, "กำลังบันทึกผลตรวจ", async () => {
    await api(`/api/tasks/${encodeURIComponent(taskID)}/content/claims/${encodeURIComponent(claimID)}/review`, {method:"POST", body:JSON.stringify({verdict,actor:currentActor()})});
    toast("บันทึกผลตรวจแล้ว");
  });
}

async function removeTaskContentClaim(taskID, claimID, body) {
  const approved = await askAction({title:"นำประเด็นนี้ออก?", message:"ประเด็นจะถูกนำออกจากการตรวจคอนเทนต์ การอนุมัติร่างที่เคยผูกกับประเด็นนี้จะหมดอายุ", confirmLabel:"นำออก"});
  if (!approved) return;
  await performTaskAction(taskID, body, "กำลังนำประเด็นออก", async () => {
    await api(`/api/tasks/${encodeURIComponent(taskID)}/content/claims/${encodeURIComponent(claimID)}`, {method:"DELETE"});
    toast("นำประเด็นออกแล้ว");
  });
}

async function saveTaskContentDraft(event, taskID, body) {
  event.preventDefault();
  const form = new FormData(event.currentTarget);
  const platform = event.currentTarget.dataset.contentDraft;
  const brief = state.taskExecutions[taskID]?.content?.brief;
  const destination = platform === "facebook" ? brief?.facebook_page : brief?.tiktok_account;
  if (!destination) return;
  const input = {id:String(form.get("id") || ""), expected_revision:Number(form.get("expected_revision") || 0), platform, destination,
    body:String(form.get("body") || "").trim(), script:String(form.get("script") || "").trim(), storyboard:String(form.get("storyboard") || "").trim(), media_artifact_id:String(form.get("media_artifact_id") || ""),media_rights_note:String(form.get("media_rights_note") || "").trim()};
  const approved = await askAction({title:`บันทึกร่าง ${platform === "facebook" ? "Facebook" : "TikTok"}?`, message:`ปลายทาง: ${destination} · การแก้ร่างจะทำให้การอนุมัติฉบับเดิมหมดอายุ`, confirmLabel:"บันทึกร่าง"});
  if (!approved) return;
  await performTaskAction(taskID, body, "กำลังบันทึกร่าง", async () => {
    await api(`/api/tasks/${encodeURIComponent(taskID)}/content/drafts`, {method:"POST", body:JSON.stringify(input)});
    taskContentDraftEdits.delete(`${taskID}:${platform}`);
    toast("บันทึกร่างแล้ว โปรดตรวจตัวอย่าง");
  });
}

async function suggestTaskContent(taskID, body) {
  const approved = await askAction({title:"เติมโครงร่างจากหลักฐานที่ตรวจแล้ว?",message:"ระบบจะเติมข้อความ Facebook และสคริปต์ TikTok ในช่องแก้ไขที่ยังไม่บันทึก เนื้อหาที่พิมพ์ค้างในช่องเหล่านี้จะถูกแทนที่ โปรดอ่านและแก้ก่อนบันทึก",confirmLabel:"เติมโครงร่าง"});
  if (!approved) return;
  await performTaskAction(taskID,body,"กำลังจัดโครงร่าง",async () => {
    const suggestions = asList(await api(`/api/tasks/${encodeURIComponent(taskID)}/content/suggestions`));
    for (const item of suggestions) taskContentDraftEdits.set(`${taskID}:${item.platform}`,{body:item.body,script:item.script || "",storyboard:item.storyboard || "",media_artifact_id:"",media_rights_note:""});
    toast("เติมโครงร่างจากประเด็นที่ตรวจแล้ว โปรดอ่านและบันทึกแต่ละปลายทาง");
  });
}

async function preflightTaskComfy(event, taskID, body) {
  event.preventDefault();
  const workflowPath = String(new FormData(event.currentTarget).get("workflow_path") || "").trim();
  taskComfyPaths.set(taskID, workflowPath);
  await performTaskAction(taskID, body, "กำลังตรวจ Comfy workflow", async () => {
    try {
      const result = await api("/api/comfy/preflight", {method:"POST", body:JSON.stringify({task_id:taskID, workflow_path:workflowPath})});
      taskComfyPreflights.set(taskID, result);
      toast(result.ready ? "workflow พร้อมรันในเครื่อง" : "workflow ยังไม่พร้อม โปรดดูเหตุผลในหน้างาน", !result.ready);
    } catch (error) {
      taskComfyPreflights.set(taskID, {error:error.message});
      throw error;
    }
  });
}

async function startTaskComfy(taskID, body) {
  const check = taskComfyPreflights.get(taskID);
  if (!check?.ready || check.workflow_path !== String(taskComfyPaths.get(taskID) || "").trim()) return;
  const approved = await askAction({title:"เริ่มสร้างสื่อใน ComfyUI?", message:`workflow: ${check.workflow_path}\n${check.node_count} nodes · ผลตรวจระบุว่าใช้ ComfyUI ในเครื่องและไม่ใช้เครดิต partner\n\nHermetrix จะส่ง workflow ฉบับที่ตรวจแล้วไปสร้างสื่อหนึ่งครั้ง`, confirmLabel:"เริ่มสร้างสื่อ"});
  if (!approved) return;
  await performTaskAction(taskID, body, "กำลังส่งงานให้ Comfy", async () => {
    const job = await api("/api/comfy/jobs", {method:"POST", body:JSON.stringify({task_id:taskID, workflow_path:check.workflow_path, workflow_hash:check.workflow_hash, operation_id:crypto.randomUUID()})});
    toast(job.state === "uncertain" ? "ส่งงานแล้วแต่ผลยังไม่แน่นอน โปรดตรวจคิว Comfy ก่อนเริ่มงานใหม่" : "ส่งงานสร้างสื่อแล้ว ตรวจสถานะในหน้างานได้", job.state === "uncertain");
  });
}

async function refreshTaskComfy(taskID, jobID, body) {
  const approved = await askAction({title:"ตรวจผลจาก Comfy?", message:"Hermetrix จะอ่านสถานะงานจาก Comfy และนำไฟล์ผลลัพธ์ที่เสร็จแล้วเข้าคลังของโปรเจกต์", confirmLabel:"ตรวจสถานะ"});
  if (!approved) return;
  await performTaskAction(taskID, body, "กำลังตรวจผล Comfy", async () => {
    const job = await api(`/api/comfy/jobs/${encodeURIComponent(jobID)}/refresh`, {method:"POST", body:"{}"});
    if (job.state === "completed") { state.artifacts = asList(await api("/api/artifacts")); toast("บันทึกผล Comfy ในคลังแล้ว โปรดเปิดตรวจคุณภาพ"); }
    else toast(job.state === "uncertain" ? "ผลยังไม่แน่นอน โปรดตรวจคิว Comfy" : `สถานะงาน: ${job.state}`, job.state === "uncertain");
  });
}

async function cancelTaskComfy(taskID, jobID, body) {
  const approved = await askAction({title:"ยกเลิกงาน Comfy นี้?", message:"Hermetrix จะส่งคำขอยกเลิกเฉพาะงานนี้ และบันทึกผลที่ Comfy ตอบกลับ", confirmLabel:"ยกเลิกงาน"});
  if (!approved) return;
  await performTaskAction(taskID, body, "กำลังยกเลิกงาน Comfy", async () => {
    const job = await api(`/api/comfy/jobs/${encodeURIComponent(jobID)}/cancel`, {method:"POST", body:"{}"});
    toast(job.state === "uncertain" ? "ผลยกเลิกยังไม่แน่นอน โปรดตรวจสถานะ" : "ยกเลิกงานแล้ว", job.state === "uncertain");
  });
}

async function approveTaskContentDraft(taskID, draftID, body) {
  const draft = state.taskExecutions[taskID]?.content?.drafts?.find(item => item.id === draftID);
  if (!draft || !draft.approval_ready || draft.approved) return;
  const label = draft.platform === "facebook" ? "Facebook" : "TikTok";
  const approved = await askAction({title:`อนุมัติร่าง ${label} ฉบับนี้?`, message:`บัญชี: ${draft.destination}\n\nข้อความฉบับที่บันทึก:\n${draft.body}\n\nโปรดตรวจสคริปต์ ลำดับภาพ และวิดีโอในตัวอย่างด้านหลังด้วย การอนุมัติยังไม่เผยแพร่`, confirmLabel:"อนุมัติร่าง"});
  if (!approved) return;
  await performTaskAction(taskID, body, "กำลังบันทึกการอนุมัติ", async () => {
    await api(`/api/tasks/${encodeURIComponent(taskID)}/content/drafts/${encodeURIComponent(draftID)}/approval`, {method:"POST", body:JSON.stringify({subject_hash:draft.subject_hash, actor:currentActor()})});
    toast("อนุมัติร่างฉบับนี้แล้ว ยังไม่ได้เผยแพร่");
  });
}

async function checkTaskTikTokCreatorInfo(taskID, connectionID) {
  taskSocialCreatorInfo.set(connectionID, {error:"กำลังตรวจบัญชี…"});
  renderCurrentTaskCockpit(taskID);
  try {
    const info = await api(`/api/social/connections/${encodeURIComponent(connectionID)}/creator-info`);
    taskSocialCreatorInfo.set(connectionID, info);
  } catch (error) { taskSocialCreatorInfo.set(connectionID, {error:error.message}); }
  renderCurrentTaskCockpit(taskID);
}

function socialInteractionSummary(post) {
  return post.route === "tiktok_direct" ? `\nความคิดเห็น: ${post.allow_comment ? "อนุญาต" : "ปิด"}\nDuet: ${post.allow_duet ? "อนุญาต" : "ปิด"}\nStitch: ${post.allow_stitch ? "อนุญาต" : "ปิด"}` : "";
}

async function prepareTaskSocialPost(event, taskID, body) {
  event.preventDefault();
  const form = event.currentTarget;
  const draftID = form.dataset.socialIntent;
  const connectionID = form.dataset.connectionId;
  const draft = state.taskExecutions[taskID]?.content?.drafts?.find(item => item.id === draftID);
  const account = (state.socialConnections || []).find(item => item.id === connectionID);
  if (!draft?.approved || !account) return;
  const values = new FormData(form);
  const privacy = account.capability === "video.upload_identity" ? "" : String(values.get("privacy_level") || "");
  const isAIGC = values.get("is_aigc") === "on";
  const direct = draft.platform === "tiktok" && account.capability !== "video.upload_identity";
  const disclosure = direct && values.get("commercial_disclosure") === "on";
  const brandContent = disclosure && values.get("brand_content") === "on";
  const brandOrganic = disclosure && values.get("brand_organic") === "on";
  const allowComment = direct && values.get("allow_comment") === "on";
  const allowDuet = direct && values.get("allow_duet") === "on";
  const allowStitch = direct && values.get("allow_stitch") === "on";
  const musicConsent = direct && values.get("music_consent") === "on";
  const creator = taskSocialCreatorInfo.get(account.id);
  if (direct && (!creator || creator.error || !(creator.privacy_level_options || []).includes(privacy) || !Number(creator.max_video_post_duration_sec))) { toast("ตรวจตัวเลือกบัญชี TikTok และเลือกการมองเห็นก่อน", true); return; }
  if (disclosure && !brandContent && !brandOrganic) { toast("เลือกประเภทคอนเทนต์เชิงพาณิชย์ก่อน", true); return; }
  if (brandContent && !["PUBLIC_TO_EVERYONE", "MUTUAL_FOLLOW_FRIENDS"].includes(privacy)) { toast("เนื้อหาแบรนด์ต้องตั้งเป็นสาธารณะหรือเพื่อน", true); return; }
  if (direct && !musicConsent) { toast("ยืนยันข้อกำหนดการใช้เพลงก่อน", true); return; }
  const visibility = account.capability === "video.upload_identity" ? "เลือกใน TikTok หลังรับร่าง" : privacy || "ตาม Page";
  const commercial = draft.platform === "tiktok" && account.capability !== "video.upload_identity" ? `\nมีเนื้อหาแบรนด์: ${brandContent ? "ใช่" : "ไม่"}\nโปรโมตสินค้า/บริการของตนเอง: ${brandOrganic ? "ใช่" : "ไม่"}` : "";
  const interactions = direct ? `\nอนุญาตความคิดเห็น: ${allowComment ? "ใช่" : "ไม่"}\nอนุญาต Duet: ${allowDuet ? "ใช่" : "ไม่"}\nอนุญาต Stitch: ${allowStitch ? "ใช่" : "ไม่"}` : "";
  const confirmed = await askAction({title:"เตรียมคำสั่งส่งออก?", message:`ปลายทาง: ${account.display_name}\nข้อความ: ${draft.body}\nวิดีโอ: ${draft.media_artifact_id || "ไม่มี"}\nการมองเห็น: ${visibility}\nสื่อนี้สร้างด้วย AI: ${isAIGC ? "ใช่" : "ไม่"}${commercial}${interactions}\n\nขั้นนี้บันทึกตัวอย่างเพื่อทบทวน ยังไม่อัปโหลด`, confirmLabel:"เตรียมตัวอย่าง"});
  if (!confirmed) return;
  await performTaskAction(taskID, body, "กำลังเตรียมตัวอย่างส่งออก", async () => {
    await api(`/api/tasks/${encodeURIComponent(taskID)}/social/posts`, {method:"POST",body:JSON.stringify({draft_id:draftID,connection_id:connectionID,privacy_level:privacy,is_aigc:isAIGC,brand_content:brandContent,brand_organic:brandOrganic,allow_comment:allowComment,allow_duet:allowDuet,allow_stitch:allowStitch,music_consent:musicConsent})});
    toast("เตรียมตัวอย่างแล้ว ตรวจอีกครั้งก่อนอนุมัติ");
  });
}

async function approveTaskSocialPost(taskID, postID, body) {
  const post = state.taskExecutions[taskID]?.socialPosts?.find(item => item.id === postID);
  const draft = state.taskExecutions[taskID]?.content?.drafts?.find(item => item.id === post?.draft_id);
  if (!post || !draft || post.state !== "review") return;
  const visibility = post.route === "tiktok_draft" ? "เลือกใน TikTok หลังรับร่าง" : post.privacy_level || "ตาม Page";
  const commercial = post.route === "tiktok_direct" ? `\nมีเนื้อหาแบรนด์: ${post.brand_content ? "ใช่" : "ไม่"}\nโปรโมตสินค้า/บริการของตนเอง: ${post.brand_organic ? "ใช่" : "ไม่"}` : "";
  const confirmed = await askAction({title:"อนุมัติส่งออกชิ้นนี้?", message:`บัญชี: ${draft.destination}\nข้อความ: ${draft.body}\nวิดีโอ: ${draft.media_artifact_id || "ไม่มี"}\nการมองเห็น: ${visibility}\nสื่อนี้สร้างด้วย AI: ${post.is_aigc ? "ใช่" : "ไม่"}${commercial}${socialInteractionSummary(post)}\n\nการอนุมัตินี้ผูกกับร่าง สื่อ บัญชี และตัวเลือกฉบับนี้เท่านั้น`, confirmLabel:"อนุมัติชิ้นนี้"});
  if (!confirmed) return;
  await performTaskAction(taskID,body,"กำลังบันทึกการอนุมัติส่งออก",async () => {
    await api(`/api/social/posts/${encodeURIComponent(postID)}/approve`,{method:"POST",body:JSON.stringify({intent_hash:post.intent_hash})});
    toast("อนุมัติคำสั่งส่งออกแล้ว ยังไม่เผยแพร่");
  });
}

async function dispatchTaskSocialPost(taskID, postID, body) {
  const post = state.taskExecutions[taskID]?.socialPosts?.find(item => item.id === postID);
  const draft = state.taskExecutions[taskID]?.content?.drafts?.find(item => item.id === post?.draft_id);
  if (!post || !draft || post.state !== "approved") return;
  const draftOnly = post.route === "tiktok_draft";
  const commercial = post.route === "tiktok_direct" ? `\nมีเนื้อหาแบรนด์: ${post.brand_content ? "ใช่" : "ไม่"}\nโปรโมตสินค้า/บริการของตนเอง: ${post.brand_organic ? "ใช่" : "ไม่"}` : "";
  const confirmed = await askAction({title:draftOnly ? "อัปโหลดวิดีโอเป็นร่างใน TikTok?" : "อัปโหลดและเผยแพร่ตอนนี้?", message:`ปลายทาง: ${draft.destination}\nข้อความ: ${draft.body}\nวิดีโอ: ${draft.media_artifact_id || "ไม่มี"}\nการมองเห็น: ${draftOnly ? "เลือกใน TikTok หลังรับร่าง" : post.privacy_level || "ตาม Page"}${commercial}${socialInteractionSummary(post)}\n\n${draftOnly ? "คุณต้องเปิดการแจ้งเตือนใน TikTok เพื่อแก้คำบรรยาย ตั้งการมองเห็นและป้ายกำกับ AI แล้วเผยแพร่ด้วยตัวเอง" : "ระบบจะส่งชิ้นนี้ไปยังแพลตฟอร์มเพียงครั้งเดียว และตรวจผลจริงหลังส่ง"}`, confirmLabel:draftOnly ? "อัปโหลดเป็นร่าง" : "ส่งออกและเผยแพร่"});
  if (!confirmed) return;
  await performTaskAction(taskID,body,"กำลังส่งออกและตรวจสถานะ",async () => {
    const result = await api(`/api/social/posts/${encodeURIComponent(postID)}/dispatch`,{method:"POST",body:"{}"});
    toast(result.state === "uncertain" ? "ผลยังไม่แน่นอน โปรดตรวจสถานะก่อนทำต่อ" : result.state === "published" ? "แพลตฟอร์มยืนยันว่าเผยแพร่แล้ว" : result.state === "draft_uploaded" ? "ร่างถึง TikTok แล้ว โปรดเผยแพร่ต่อในแอป" : "ส่งออกแล้ว กำลังรอผลจากแพลตฟอร์ม",result.state === "uncertain");
  });
}

async function refreshTaskSocialPost(taskID, postID, body) {
  await performTaskAction(taskID,body,"กำลังตรวจสถานะบนแพลตฟอร์ม",async () => {
    const result = await api(`/api/social/posts/${encodeURIComponent(postID)}/refresh`,{method:"POST",body:"{}"});
    toast(result.state === "published" ? "แพลตฟอร์มยืนยันว่าเผยแพร่แล้ว" : result.state === "draft_uploaded" ? "อัปโหลดเป็นร่างแล้ว โปรดเผยแพร่ต่อใน TikTok" : "ตรวจสถานะแล้ว: "+result.remote_status);
  });
}

async function retryTaskSocialPost(taskID, postID, body) {
  const post = state.taskExecutions[taskID]?.socialPosts?.find(item => item.id === postID);
  if (!post || post.state !== "failed") return;
  const confirmed = await askAction({title:"เตรียมการส่งใหม่?", message:"ผลครั้งก่อนยืนยันว่าไม่สำเร็จแล้ว ระบบจะเก็บประวัติเดิมและสร้างคำสั่งใหม่ให้ตรวจและอนุมัติอีกครั้ง ยังไม่อัปโหลด", confirmLabel:"เตรียมคำสั่งใหม่"});
  if (!confirmed) return;
  await performTaskAction(taskID,body,"กำลังเตรียมการส่งใหม่",async () => {
    await api(`/api/social/posts/${encodeURIComponent(postID)}/retry`,{method:"POST",body:"{}"});
    toast("เตรียมคำสั่งใหม่แล้ว โปรดตรวจและอนุมัติอีกครั้ง");
  });
}

async function searchTaskResearch(event, body) {
  event.preventDefault();
  const task = state.durableTasks.find(item => item.id === state.selectedDurableTask);
  if (!task || task.egress_policy !== "remote_allowed") return;
  const form = new FormData(event.currentTarget);
  const query = String(form.get("query") || "").trim();
  const days = Number(form.get("days") || 30);
  taskResearchDrafts.set(task.id, {query, days});
  if (query.length < 2 || query.length > 240) return;
  const approved = await askAction({title:"ค้นบทความจาก Crossref?", message:`Hermetrix จะส่งคำค้น “${query}” ไปยัง Crossref และบันทึกข้อมูลบทความที่พบไว้ในงานนี้`, confirmLabel:"ส่งคำค้น"});
  if (!approved) return;
  await performTaskAction(task.id, body, "กำลังค้นบทความ", async () => {
    await api(`/api/tasks/${encodeURIComponent(task.id)}/research/search`, {method:"POST", body:JSON.stringify({query, days})});
    toast("บันทึกผลค้นบทความแล้ว เลือกแหล่งที่จะอ่านต่อได้");
  });
}

async function selectTaskResearch(taskID, candidateID, wasSelected, body) {
  const approved = await askAction({title:wasSelected ? "นำแหล่งนี้ออก?" : "เลือกแหล่งนี้?", message:wasSelected ? "รายการนี้จะยังอยู่ในผลค้น แต่ไม่ถูกเลือกสำหรับเนื้อหาชิ้นนี้" : "การเลือกจะบันทึกแหล่งไว้ในงาน ยังไม่ยืนยันว่าข้อความจากแหล่งนี้ถูกต้อง", confirmLabel:wasSelected ? "นำออก" : "เลือกแหล่ง"});
  if (!approved) return;
  await performTaskAction(taskID, body, "กำลังบันทึกแหล่งข้อมูล", async () => {
    await api(`/api/tasks/${encodeURIComponent(taskID)}/research/${encodeURIComponent(candidateID)}/selection`, {method:"POST", body:JSON.stringify({selected:!wasSelected})});
    toast(wasSelected ? "นำแหล่งออกแล้ว" : "เลือกแหล่งแล้ว");
  });
}

async function refreshDurableTask(taskID) {
  await loadTaskExecution(taskID);
  const task = state.taskExecutions[taskID]?.task;
  if (task) state.durableTasks = state.durableTasks.map(item => item.id === taskID ? task : item);
  if (state.selectedDurableTask === taskID) renderCurrentTaskCockpit(taskID);
}

async function performTaskAction(taskID, body, label, action) {
  if (taskActions.has(taskID)) return;
  taskErrors.delete(taskID);
  taskActions.set(taskID, label);
  if (body.isConnected) renderTaskCockpit(body);
  try { await action(); }
  catch (error) { taskErrors.set(taskID, error.message); toast(taskFriendlyError(error.message), true); }
  finally { taskActions.delete(taskID); await refreshDurableTask(taskID); }
}

async function reconcileTaskEffects(body) {
  const taskID = state.selectedDurableTask;
  const effects = (state.taskExecutions[taskID]?.effects || []).filter(effect => effect.state === "uncertain");
  if (!taskID || !effects.length) return;
  await performTaskAction(taskID, body, "กำลังตรวจผลที่บันทึกไว้", async () => {
    for (const effect of effects) await api(`/api/task-effects/${encodeURIComponent(effect.operation_id)}/reconcile`, {method:"POST", body:"{}"});
    toast("ตรวจผลที่บันทึกไว้แล้ว โดยไม่ทำคำสั่งเดิมซ้ำ");
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
  await performTaskAction(task.id, body, "กำลังสร้างข้อเสนอแก้ไฟล์", async () => {
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
    if (!attempt.packet) throw new Error("ไม่พบข้อมูลขั้นตอนที่บันทึกไว้ ตรวจและกู้รอบงานก่อนทำต่อ");
    if (!files.length) {
      const selection = await api(`/api/task-attempts/${encodeURIComponent(attempt.id)}/select-files`, {method:"POST", body:JSON.stringify({provider_id:providerID, authority})});
      files.push(...(selection.result?.files || []));
      if (!files.length) throw new Error("ยังไม่ได้เลือกไฟล์ที่ใช้แก้ เลือกไฟล์ของขั้นตอนนี้ด้วยตนเอง");
    }
    await api(`/api/task-attempts/${encodeURIComponent(attempt.id)}/proposals`, {method:"POST", body:JSON.stringify({provider_id:providerID, files, authority:await renewTaskAuthority(task.id)})});
    toast("ข้อเสนอแก้ไฟล์พร้อมให้ตรวจแล้ว");
  });
}

async function decideTaskProposal(verdict, body) {
  const taskID = state.selectedDurableTask;
  const proposal = state.taskExecutions[taskID]?.proposal;
  if (!proposal || taskActions.has(taskID)) return;
  if (verdict === "approved" && !taskProposalBodies.get(proposal.artifact_id)?.changes?.length) return;
  const rationale = await askAction({title:verdict === "approved" ? "อนุมัติข้อเสนอแก้ไฟล์นี้?" : "ส่งข้อเสนอแก้ไฟล์กลับ?", message:"การตัดสินใจนี้จะบันทึกกับข้อเสนอฉบับที่เห็น หลังอนุมัติจึงค่อยเขียนไฟล์ในขั้นถัดไป", confirmLabel:verdict === "approved" ? "อนุมัติข้อเสนอ" : "ส่งกลับแก้", reasonLabel:"เหตุผลที่ตรวจ", danger:verdict === "rejected"});
  if (!rationale) return;
  await performTaskAction(taskID, body, "กำลังบันทึกผลตรวจข้อเสนอ", async () => {
    await api(`/api/task-code-proposals/${encodeURIComponent(proposal.id)}/decision`, {method:"POST", body:JSON.stringify({actor:currentActor(), verdict, rationale, findings:[]})});
  });
}

async function applyTaskProposal(body) {
  const taskID = state.selectedDurableTask;
  const proposal = state.taskExecutions[taskID]?.proposal;
  if (!proposal || taskActions.has(taskID)) return;
  const confirmed = await askAction({title:"เขียนไฟล์ตามข้อเสนอที่อนุมัติ?", message:"ระบบจะตรวจไฟล์เดิมกับข้อเสนอและบันทึกหลักฐานสำหรับคืนไฟล์ก่อนเขียน", confirmLabel:"เขียนไฟล์"});
  if (!confirmed) return;
  await performTaskAction(taskID, body, "กำลังเขียนไฟล์ตามข้อเสนอ", async () => {
    await api(`/api/task-code-proposals/${encodeURIComponent(proposal.id)}/apply`, {method:"POST", body:JSON.stringify({actor:currentActor(), authority:await renewTaskAuthority(taskID)})});
    toast("เขียนไฟล์แล้ว ขั้นต่อไปให้รันวิธีตรวจตามแผน");
  });
}

async function verifyTaskProposal(body) {
  const taskID = state.selectedDurableTask;
  const proposal = state.taskExecutions[taskID]?.proposal;
  if (!proposal || taskActions.has(taskID)) return;
  await performTaskAction(taskID, body, "กำลังรันวิธีตรวจตามแผน", async () => {
    await api(`/api/task-code-proposals/${encodeURIComponent(proposal.id)}/verify-frozen`, {method:"POST", body:JSON.stringify({actor:currentActor(), authority:await renewTaskAuthority(taskID)})});
    toast("บันทึกผลตรวจแล้ว");
  });
}

async function postReviewTaskProposal(body) {
  const taskID = state.selectedDurableTask;
  const task = state.durableTasks.find(item => item.id === taskID);
  const proposal = state.taskExecutions[taskID]?.proposal;
  const providerID = $("#taskReviewerProvider")?.value;
  if (!proposal || !taskEligibleProviders(task, proposal.provider_id).some(item => item.id === providerID)) return;
  await performTaskAction(taskID, body, "กำลังตรวจผลโดยโมเดลแยก", async () => {
    const output = await api(`/api/task-code-proposals/${encodeURIComponent(proposal.id)}/post-review`, {method:"POST", body:JSON.stringify({provider_id:providerID, authority:await renewTaskAuthority(taskID)})});
    toast(output.review?.verdict === "reject" ? "ผู้ตรวจส่งข้อเสนอกลับ เปิดหลักฐานและผลคืนไฟล์ก่อนเริ่มใหม่" : "ผู้ตรวจรอบแยกยืนยันผลแล้ว");
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
  taskActions.set("create", "กำลังสร้างงาน");
  taskErrors.delete("create");
  const submit = event.currentTarget.querySelector('button:not([type]), button[type="submit"]');
  if (submit) submit.disabled = true;
  try {
    const task = await api("/api/tasks", {method:"POST", body:JSON.stringify({project_id:state.currentProject.id,
      title:String(form.get("title") || "").trim() || objective.split("\n")[0].slice(0,80), objective, original_request:objective,
      criteria, constraints:lines("constraints"), unknowns:lines("unknowns"), egress_policy:form.has("allow_remote_research") ? "remote_allowed" : "local_only",
      remote_egress_approval:form.has("allow_remote_research") ? {actor:currentActor(), reason:"ผู้ใช้เลือกให้ค้นบทความจาก Crossref สำหรับงานนี้"} : undefined, actor:currentActor(),
      contract:{scope:String(form.get("scope") || "").trim(), experience_preview:String(form.get("experience_preview") || "").trim(), assumptions:lines("assumptions"), affected_journeys:lines("affected_journeys"),
        verification:String(form.get("verification") || "").trim(), require_plan_review:true,
        require_user_acceptance:form.has("require_user_acceptance")}})});
    state.durableTasks.unshift(task);
    state.selectedDurableTask = task.id;
    const createPlanNow = Boolean(state.taskDraftAutoPlan);
    state.taskDraftObjective = "";
    state.taskDraftDetails = {};
    state.taskDraftAutoPlan = false;
    rememberProjectDraft();
    rememberSurface(state.workspaceBeforePlans ? "plans" : "code");
    taskActions.delete("create");
    renderPanes();
    if (createPlanNow && $("#taskPlannerProvider")?.value) {
      await autoPlanDurableTask(document.querySelector('.pane-body[data-pane-kind="tasks"]'));
    } else toast("สร้างงานแล้ว ขั้นต่อไปคือสร้างแผน");
  } catch (error) { taskErrors.set("create", error.message); toast(error.message, true); }
  finally { taskActions.delete("create"); if (submit?.isConnected) submit.disabled = false; if (taskErrors.has("create") && body?.isConnected) renderTaskCockpit(body); }
}

async function resumeObservedTaskReview(body) {
  const taskID = state.selectedDurableTask;
  const task = state.durableTasks.find(item => item.id === taskID);
  if (!task || taskActions.has(taskID)) return;
  await performTaskAction(taskID, body, "Recovering saved review", async () => {
    const resumed = await api(`/api/tasks/${encodeURIComponent(taskID)}/resume-observed-review`, {method:"POST", body:JSON.stringify({expected_task_revision:task.revision, actor:currentActor()})});
    state.durableTasks = state.durableTasks.map(item => item.id === resumed.id ? resumed : item);
    toast("Saved review is ready to continue; no action was replayed");
  });
}

async function resumeApprovedTaskReview(body) {
  const taskID = state.selectedDurableTask;
  const task = state.durableTasks.find(item => item.id === taskID);
  if (!task || taskActions.has(taskID)) return;
  await performTaskAction(taskID, body, "Restoring approved review", async () => {
    const resumed = await api(`/api/tasks/${encodeURIComponent(taskID)}/resume-approved-review`, {method:"POST", body:JSON.stringify({expected_task_revision:task.revision, actor:currentActor()})});
    state.durableTasks = state.durableTasks.map(item => item.id === resumed.id ? resumed : item);
    toast("บันทึกผลรีวิวเดิมครบแล้ว พร้อมปิดขั้นโดยไม่เรียกโมเดลซ้ำ");
  });
}

async function retryFailedTaskProposal(body) {
  const taskID = state.selectedDurableTask;
  const task = state.durableTasks.find(item => item.id === taskID);
  if (!task || taskActions.has(taskID)) return;
  await performTaskAction(taskID, body, "Closing failed proposal attempt", async () => {
    const updated = await api(`/api/tasks/${encodeURIComponent(taskID)}/retry-failed-proposal`, {method:"POST", body:JSON.stringify({expected_task_revision:task.revision, actor:currentActor()})});
    state.durableTasks = state.durableTasks.map(item => item.id === updated.id ? updated : item);
    toast("เลือกโมเดลและไฟล์เพื่อเริ่มรอบใหม่ได้แล้ว");
  });
}

async function retryRejectedTaskProposal(body) {
  const taskID = state.selectedDurableTask;
  const task = state.durableTasks.find(item => item.id === taskID);
  if (!task || taskActions.has(taskID)) return;
  await performTaskAction(taskID, body, "Closing rejected proposal attempt", async () => {
    const updated = await api(`/api/tasks/${encodeURIComponent(taskID)}/retry-rejected-proposal`, {method:"POST", body:JSON.stringify({expected_task_revision:task.revision, actor:currentActor()})});
    state.durableTasks = state.durableTasks.map(item => item.id === updated.id ? updated : item);
    toast("ข้อเสนอเดิมถูกเก็บไว้ พร้อมส่งเหตุผลตรวจกลับในรอบใหม่");
  });
}

function revisedTaskCriteria(task, descriptions) {
  const previous = task.requirement?.criteria || [];
  const reserved = new Set(previous.map(item => item.id));
  const reused = new Set();
  let next = 1;
  return [...new Set(descriptions)].map(description => {
    const match = previous.find(item => item.description === description && !reused.has(item.id));
    if (match) { reused.add(match.id); return {id:match.id, description}; }
    while (reserved.has(`AC-${next}`)) next++;
    const id = `AC-${next++}`;
    reserved.add(id);
    return {id, description};
  });
}

async function reviseDurableTask(event, body) {
  event.preventDefault();
  const task = state.durableTasks.find(item => item.id === state.selectedDurableTask);
  if (!task || taskActions.has(task.id)) return;
  const form = new FormData(event.currentTarget);
  const lines = name => String(form.get(name) || "").split("\n").map(value => value.trim()).filter(Boolean);
  const outcome = String(form.get("outcome") || "").trim();
  const criteria = revisedTaskCriteria(task, lines("criteria"));
  if (!outcome || !criteria.length) return;
  const contract = {outcome, scope:String(form.get("scope") || "").trim(), experience_preview:String(form.get("experience_preview") || "").trim(), assumptions:lines("assumptions"),
    affected_journeys:lines("affected_journeys"), verification:String(form.get("verification") || "").trim(),
    clarifications:task.requirement?.contract?.clarifications || [],
    require_plan_review:Boolean(task.requirement?.contract?.require_plan_review),
    require_user_acceptance:form.has("require_user_acceptance")};
  const confirmed = await askAction({title:"บันทึก Goal ฉบับใหม่?", message:"แผนฉบับปัจจุบันจะหยุดใช้ ต้องสร้างแผนและตรวจผลใหม่ หลักฐานเดิมยังคงอยู่ให้เปิดดู", confirmLabel:"บันทึกฉบับใหม่"});
  if (!confirmed) return;
  await performTaskAction(task.id, body, "กำลังแก้ Goal", async () => {
    const updated = await api(`/api/tasks/${encodeURIComponent(task.id)}/requirements`, {method:"POST", body:JSON.stringify({
      expected_task_revision:task.revision, actor:currentActor(), criteria, contract,
      constraints:lines("constraints"), unknowns:lines("unknowns")})});
    taskRevisionDrafts.delete(task.id);
    state.durableTasks = state.durableTasks.map(item => item.id === task.id ? updated : item);
    toast("บันทึก Goal ฉบับใหม่แล้ว ขั้นต่อไปคือสร้างแผนใหม่");
  });
}

async function clarifyDurableTask(event, body) {
  event.preventDefault();
  const task = state.durableTasks.find(item => item.id === state.selectedDurableTask);
  if (!task || taskActions.has(task.id)) return;
  const form = new FormData(event.currentTarget);
  const question = String(form.get("question") || "").trim();
  const answer = String(form.get("answer") || "").trim();
  if (!task.requirement?.unknowns?.includes(question) || !answer || answer.length > 2000) return;
  const remaining = task.requirement.unknowns.filter(item => item !== question);
  const contract = {...task.requirement.contract,
    clarifications:[...(task.requirement.contract?.clarifications || []), {question, answer}]};
  const confirmed = await askAction({title:"บันทึกคำตอบใน Goal ฉบับใหม่?",
    message:`${question}\n\nคำตอบ: ${answer}\n\nแผนและหลักฐานเดิมต้องตรวจใหม่`, confirmLabel:"บันทึกคำตอบ"});
  if (!confirmed) return;
  await performTaskAction(task.id, body, "กำลังบันทึกคำตอบ", async () => {
    const updated = await api(`/api/tasks/${encodeURIComponent(task.id)}/requirements`, {method:"POST", body:JSON.stringify({
      expected_task_revision:task.revision, actor:currentActor(), criteria:task.requirement.criteria,
      contract, constraints:task.requirement.constraints || [], unknowns:remaining})});
    clearTaskClarificationDraft(task.id);
    const revisionDraft = taskRevisionDrafts.get(task.id);
    if (revisionDraft) {
      revisionDraft.unknowns = revisionDraft.unknowns.split("\n").filter(item => item.trim() !== question).join("\n");
      taskRevisionDrafts.set(task.id, revisionDraft);
    }
    state.durableTasks = state.durableTasks.map(item => item.id === task.id ? updated : item);
    toast("บันทึกคำตอบแล้ว ขั้นต่อไปคือตรวจและสร้างแผนใหม่");
  });
}

async function autoPlanDurableTask(body) {
  const task = state.durableTasks.find(item => item.id === state.selectedDurableTask);
  const providerID = $("#taskPlannerProvider")?.value;
  if (!task || !taskEligibleProviders(task).some(item => item.id === providerID)) return;
  await performTaskAction(task.id, body, "กำลังสร้างแผน", async () => {
    const output = await api(`/api/tasks/${encodeURIComponent(task.id)}/auto-plan`, {method:"POST", body:JSON.stringify({expected_task_revision:task.revision, provider_id:providerID, actor:currentActor()})});
    state.durableTasks = state.durableTasks.map(item => item.id === task.id ? output.task : item);
    toast("สร้างแผนแล้ว ตรวจขั้นตอนและวิธีตรวจ ก่อนเริ่มงาน");
  });
}


// Tool surfaces have stable form IDs and mount once. Independent notes panes
// provide any number of extra working areas without duplicating those IDs.
const PANE_CONTENT = [
  { id: "chat", icon: "chat", label: "แชท" },
  { id: "tasks", icon: "activity", label: "งาน" },
  { id: "editor", icon: "file", label: "โค้ด" },
  { id: "review", icon: "review", label: "ตรวจงาน" },
  { id: "files", icon: "files", label: "ไฟล์" },
  { id: "git", icon: "project", label: "Git" },
  { id: "environment", icon: "settings", label: "เครื่องมือ" },
  { id: "terminal", icon: "terminal", label: "เทอร์มินัล" },
  { id: "browser", icon: "browser", label: "เบราว์เซอร์" },
  { id: "artifacts", icon: "artifact", label: "เอกสาร" },
  { id: "team", icon: "project", label: "ทีม" },
  { id: "output", icon: "activity", label: "ผลลัพธ์" }
  ,{ id: "debug", icon: "activity", label: "ดีบัก" }
  ,{ id: "notes", icon: "file", label: "บันทึก" }
];

function paneContent(id) {
  return PANE_CONTENT.find(item => item.id === id || item.id === "notes" && id?.startsWith("notes:")) || PANE_CONTENT[0];
}

const PANE_LAYOUTS = {
  1: [{ id: "single", label: "ช่องเดียว" }],
  2: [{ id: "adaptive", label: "ยืดหยุ่น" }, { id: "ide", label: "พื้นที่ทำงาน AI" }],
  3: [{ id: "adaptive", label: "ยืดหยุ่น" }, { id: "ide", label: "พื้นที่ทำงาน AI" }],
  4: [{ id: "adaptive", label: "ยืดหยุ่น" }, { id: "ide", label: "พื้นที่ทำงาน AI" }]
};

function normalisePaneLayout(count, requested = state.paneLayout) {
  const options = PANE_LAYOUTS[Math.min(count, 4)] || PANE_LAYOUTS[1];
  return options.some(item => item.id === requested) ? requested : options[0].id;
}

let workspaceToolbarExpanded = false;
function paneToolbarHTML() {
  const count = state.panes.length || 1;
  const layout = normalisePaneLayout(count);
  const planning = state.maximisedPane !== null && state.panes[state.maximisedPane] === "tasks";
  return `<header class="workspace-toolbar ${planning ? "planning-toolbar" : layout === "ide" ? "ide-toolbar" : ""} ${!planning && !workspaceToolbarExpanded ? "is-collapsed" : ""}"><div class="workspace-toolbar-heading"><strong>${planning ? "แผนงาน" : `พื้นที่ทำงาน <span class="workspace-active-label">· ${escapeHTML(paneContent(compactPaneID()).label)}</span>`}</strong><small>${planning ? "เป้าหมาย → แผน → ทบทวนการแก้ไข → ตรวจผล" : state.view === "chat" ? "เลือกเครื่องมือที่ต้องการจากปุ่มเพิ่ม" : "เพิ่มช่องได้ต่อเนื่อง · เลื่อนแนวนอนและลากขอบช่องเพื่อปรับขนาด"}</small>${planning ? "" : `<button type="button" class="ghost compact workspace-toolbar-toggle" id="workspaceToolbarToggle" aria-controls="workspaceActions workspacePaneTabs" aria-expanded="${workspaceToolbarExpanded}" aria-label="${workspaceToolbarExpanded ? "พับเมนูพื้นที่ทำงาน" : "แสดงเมนูพื้นที่ทำงาน"}"><span>เมนู</span><span aria-hidden="true">⌄</span></button>`}</div>
    ${layout === "ide" && !planning ? `<div class="ide-mode-switch" role="group" aria-label="Workspace mode"><button type="button" id="ideAgentMode">Agent</button><button type="button" class="active" aria-current="page">Editor</button></div>` : ""}
    <div class="workspace-actions" id="workspaceActions">
      ${planning ? "" : `<button class="ghost compact" id="workspaceIDE" aria-label="${layout === "ide" ? "มุมมองยืดหยุ่น" : "โหมดแก้ไฟล์"}" title="${layout === "ide" ? "มุมมองยืดหยุ่น" : "โหมดแก้ไฟล์"}">${uiIcon("workbench")}<span>${layout === "ide" ? "Flexible view" : "Editor mode"}</span></button><button class="ghost compact" data-ide-panel="terminal" aria-label="เปิด Terminal" title="เปิด Terminal">${uiIcon("terminal")}<span>Terminal</span></button>`}
      <span id="paneCountLabel">${count} ช่อง</span>
      <button type="button" class="ghost compact" id="paneAdd" ${state.view === "chat" ? 'aria-expanded="false" aria-controls="workspacePaneMenu" aria-label="เลือกเครื่องมือในแถบข้าง"' : 'aria-label="สร้างช่องทำงาน"'} title="${state.view === "chat" ? "เลือกเครื่องมือในแถบข้าง" : "สร้างช่องทำงานและเปิดทันที"}">${uiIcon("plus")}<span>${state.view === "chat" ? "เครื่องมือ" : "สร้างช่อง"}</span></button>
    </div>${state.view === "chat" && !planning ? `<div class="workspace-pane-menu" id="workspacePaneMenu" hidden><p>เปิดในแถบข้าง</p>${["terminal", "browser", "files", "review", "artifacts", "team"].map(id => { const item = paneContent(id); return `<button type="button" data-open-side-pane="${id}">${uiIcon(item.icon)}<span>${escapeHTML(item.label)}</span></button>`; }).join("")}</div>` : ""}${planning ? "" : `<nav class="compact-pane-tabs" id="workspacePaneTabs" aria-label="Workspace panels">${state.panes.map(id => `<button type="button" data-compact-pane="${id}" aria-pressed="${id === compactPaneID()}">${escapeHTML(paneContent(id).label)}</button>`).join("")}</nav>`}</header>`;
}

function ideActivityHTML() {
  const side = state.panes[1];
  return `<nav class="ide-activity" aria-label="Editor sidebar">${[
    ["files", "files", "Explorer"], ["chat", "chat", "Local AI"],
    ["git", "project", "Git"], ["environment", "settings", "Tooling"]
  ].map(([id, icon, label]) => `<button type="button" data-ide-side="${id}" aria-label="${label}" title="${label}" aria-pressed="${side === id}">${uiIcon(icon)}</button>`).join("")}</nav>`;
}

function compactPaneID() { return state.panes.includes(state.compactPane) ? state.compactPane : state.panes[0]; }
// Chat's workbench is a narrow side panel even on a wide desktop. Use one
// visible pane there; viewport width alone left other panes off screen.
function compactWorkspace() { return state.view === "chat" || window.innerWidth <= (state.paneLayout === "ide" ? 900 : 700); }

function paneDividerHTML(count, layout) {
  return "";
}

function paneDropGuidesHTML(count) {
  return "";
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
  <div class="pane-chat-log">${messages.map(item => `<article class="pane-chat-msg ${item.role}"><span>${item.role === "user" ? "คุณ" : "Hermetrix"}</span><p>${escapeHTML((item.content || "").slice(0, 600))}</p></article>`).join("") || `<p class="form-note neutral">ยังไม่มีข้อความ พิมพ์ถามด้านล่างได้เลย</p>`}</div>
  <form class="pane-chat-form"><textarea class="pane-chat-input" rows="2" maxlength="1048576" placeholder="ถาม Hermetrix… กด Enter เพื่อส่ง" ${state.sending ? "disabled" : ""}>${escapeHTML(state.paneChatDraft || "")}</textarea><button class="primary" ${state.sending ? "disabled" : ""}>${state.sending ? "กำลังทำงาน…" : "ส่ง"}</button></form>`;
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
// The previous default was wrap-on, including a persisted "true" on devices
// where the user never chose it. Start the new editor preference unwrapped so
// long source lines stay on one line and can be scrolled horizontally.
const IDE_WORD_WRAP_KEY = "hermetrix.ide.wordWrap.v2";
const IDE_FORMAT_ON_SAVE_KEY = "hermetrix.ide.formatOnSave";

function ideFormatOnSaveEnabled() {
  if (typeof state.ideFormatOnSave === "boolean") return state.ideFormatOnSave;
  try { state.ideFormatOnSave = localStorage.getItem(IDE_FORMAT_ON_SAVE_KEY) === "true"; }
  catch { state.ideFormatOnSave = false; }
  return state.ideFormatOnSave;
}

function ideWordWrapEnabled() {
  if (typeof state.ideWordWrap === "boolean") return state.ideWordWrap;
  try { state.ideWordWrap = localStorage.getItem(IDE_WORD_WRAP_KEY) === "true"; }
  catch { state.ideWordWrap = false; }
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
  if (id.startsWith("notes:")) {
    const key = `hermetrix.pane.notes.${id}`;
    let saved = "";
    try { saved = localStorage.getItem(key) || ""; } catch {}
    body.innerHTML = `<div class="pane-notes"><label for="paneNotes-${escapeHTML(id.slice(6))}">บันทึกระหว่างทำงาน</label><textarea id="paneNotes-${escapeHTML(id.slice(6))}" placeholder="จดสิ่งที่ต้องตรวจหรือทำต่อ…">${escapeHTML(saved)}</textarea><small>เก็บในเบราว์เซอร์เครื่องนี้</small></div>`;
    body.querySelector("textarea").addEventListener("input", event => { try { localStorage.setItem(key, event.target.value); } catch {} });
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
    $("#zoneMain").innerHTML = `${paneToolbarHTML()}<div class="workspace-pane-host" id="workspacePaneHost" aria-label="ช่องทำงานที่ปรับขนาดได้"></div>`;
    host = $("#workspacePaneHost");
  } else if (host?.previousElementSibling?.classList.contains("workspace-toolbar")) {
    host.previousElementSibling.outerHTML = paneToolbarHTML();
    host = $("#workspacePaneHost");
  }
  if (!host) return;
  document.documentElement.style.setProperty("--pane-split-x", `${state.paneSplitX}%`);
  document.documentElement.style.setProperty("--pane-split-y", `${state.paneSplitY}%`);
  document.documentElement.style.setProperty("--pane-left-y", `${state.paneSplitLeftY}%`);
  document.documentElement.style.setProperty("--pane-right-y", `${state.paneSplitRightY}%`);
  document.documentElement.style.setProperty("--pane-ide-x", `${state.paneIdeSplitX}%`);
  document.documentElement.style.setProperty("--pane-ide-y", `${state.paneIdeSplitY}%`);
  document.documentElement.style.setProperty("--pane-ide-right-y", `${state.paneIdeRightY}%`);
  const count = state.panes.length;
  state.paneLayout = normalisePaneLayout(count);
  host.classList.toggle("ide-host", state.paneLayout === "ide" && state.view === "code");
  host.innerHTML = `<div class="pane-grid pane-layout-${count} pane-arrangement-${state.paneLayout} ${state.maximisedPane === null ? "" : "one-up"}">${
    state.panes.map((id, index) => {
      const hidden = state.maximisedPane !== null ? state.maximisedPane !== index : compactWorkspace() && id !== compactPaneID();
      const item = paneContent(id);
      return `<section class="pane pane-index-${index} ${hidden ? "pane-hidden" : ""}" data-pane="${index}" data-kind="${item.id}">
        <header class="pane-head">
          <button type="button" class="pane-drag-handle" draggable="true" data-pane-drag="${index}" aria-label="ย้ายช่อง ${escapeHTML(item.label)}" title="ลากเพื่อย้าย หรือใช้ปุ่มลูกศรเรียงลำดับ">${uiIcon("grip")}</button>
          ${uiIcon(item.icon)}
          <select data-pane-content="${index}" aria-label="เนื้อหาในช่อง">${
            PANE_CONTENT.map(option =>
              `<option value="${option.id}" ${option.id === item.id ? "selected" : ""} ${option.id === "editor" && !state.projectFile ? "disabled" : ""}>${escapeHTML(option.label)}</option>`).join("")
          }</select>
          <button class="ghost compact pane-window-action" data-pane-max="${index}" aria-label="${state.maximisedPane === index ? "คืนขนาด" : "ขยายเต็มพื้นที่"}ช่อง ${escapeHTML(item.label)}" title="${state.maximisedPane === index ? "คืนขนาดช่อง" : "ขยายช่องเต็มพื้นที่"}">${state.maximisedPane === index ? "↙" : "⛶"}</button>
          ${state.panes.length > 1
            ? `<button class="ghost compact pane-window-action" data-pane-close="${index}" aria-label="ปิดช่อง ${escapeHTML(item.label)}" title="ปิดช่องนี้">×</button>` : ""}
        </header>
        <div class="pane-body" data-pane-kind="${item.id}"></div><div class="pane-size-handle" data-pane-size="${index}" role="separator" aria-orientation="vertical" aria-label="ปรับความกว้างช่อง ${escapeHTML(item.label)}" tabindex="0"></div>
      </section>`;
    }).join("")
  }${paneDividerHTML(count, state.paneLayout)}${paneDropGuidesHTML(count)}</div>${host.classList.contains("ide-host") ? ideActivityHTML() : ""}`;
  state.panes.forEach((id, index) => {
    const width = Math.max(320, Math.min(1200, Number(state.paneWidths[id]) || (id === "tasks" ? 680 : id === "editor" ? 600 : 460)));
    $(`.pane[data-pane="${index}"]`, host)?.style.setProperty("--pane-width", `${width}px`);
  });
  $("#paneCountLabel").textContent = `${count} ช่อง`;
  bindPaneControls();
  $$('[data-compact-pane]').forEach(button => button.addEventListener("click", () => { state.compactPane = button.dataset.compactPane; state.maximisedPane = null; workspaceToolbarExpanded = false; renderPanes(); revealWorkspacePane(state.compactPane); }));
  state.panes.forEach((id, index) => {
    if (state.maximisedPane !== null && state.maximisedPane !== index) return;
    if (state.maximisedPane === null && compactWorkspace() && id !== compactPaneID()) return;
    mountPaneContent($(`.pane[data-pane="${index}"] .pane-body`, host), id);
  });
}

function splitPane() {
  const unused = PANE_CONTENT.find(item => item.id !== "notes" && (item.id !== "editor" || state.projectFile) && !state.panes.includes(item.id));
  const id = unused?.id || `notes:${crypto.randomUUID()}`;
  state.panes.push(id);
  state.compactPane = id;
  state.maximisedPane = null;
  renderPanes();
  saveLayout();
  $("#workspacePaneTabs")?.lastElementChild?.scrollIntoView({ block: "nearest", inline: "nearest" });
  const host = $("#workspacePaneHost");
  if (host && !compactWorkspace()) host.scrollLeft = host.scrollWidth;
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
  if (next === "editor" && !state.projectFile) return;
  if (next === "notes") {
    state.panes[index] = `notes:${crypto.randomUUID()}`;
    renderPanes();
    saveLayout();
    return;
  }
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
  $("#workspaceToolbarToggle")?.addEventListener("click", () => {
    workspaceToolbarExpanded = !workspaceToolbarExpanded;
    const toolbar = $(".workspace-toolbar");
    toolbar?.classList.toggle("is-collapsed", !workspaceToolbarExpanded);
    const button = $("#workspaceToolbarToggle");
    button?.setAttribute("aria-expanded", String(workspaceToolbarExpanded));
    button?.setAttribute("aria-label", workspaceToolbarExpanded ? "พับเมนู Workspace" : "แสดงเมนู Workspace");
  });
  $("#workspaceIDE")?.addEventListener("click", () => {
    if (state.paneLayout === "ide") setPaneLayout("adaptive");
    else window.HermetrixWorkspace?.open();
  });
  $("#ideAgentMode")?.addEventListener("click", () => switchView("chat"));
  $$('[data-ide-side]').forEach(button => button.addEventListener("click", () => window.HermetrixWorkspace?.panel(button.dataset.ideSide)));
  $$('[data-ide-panel]').forEach(button => button.addEventListener("click", () => {
    if (state.paneLayout === "ide") window.HermetrixWorkspace?.panel(button.dataset.idePanel);
    else openContentPane(button.dataset.idePanel);
  }));
  $("#paneAdd")?.addEventListener("click", () => {
    const menu = $("#workspacePaneMenu");
    if (!menu) { splitPane(); return; }
    menu.hidden = !menu.hidden;
    $("#paneAdd").setAttribute("aria-expanded", String(!menu.hidden));
    if (!menu.hidden) menu.querySelector("button")?.focus();
  });
  $$("[data-open-side-pane]").forEach(button => button.addEventListener("click", () => openContentPane(button.dataset.openSidePane)));
  $("#workspacePaneMenu")?.addEventListener("keydown", event => {
    if (event.key !== "Escape") return;
    event.preventDefault();
    event.currentTarget.hidden = true;
    $("#paneAdd")?.setAttribute("aria-expanded", "false");
    $("#paneAdd")?.focus();
  });
  $$("[data-pane-content]").forEach(select =>
    select.addEventListener("change", event => setPaneContent(Number(select.dataset.paneContent), event.target.value)));
  $$("[data-pane-max]").forEach(button =>
    button.addEventListener("click", () => maximisePane(Number(button.dataset.paneMax))));
  $$("[data-pane-close]").forEach(button =>
    button.addEventListener("click", () => closePane(Number(button.dataset.paneClose))));
  $$("[data-pane-size]").forEach(handle => {
    const index = Number(handle.dataset.paneSize);
    const id = state.panes[index];
    const pane = handle.closest(".pane");
    const resize = width => {
      const next = Math.max(320, Math.min(1200, Math.round(width)));
      state.paneWidths[id] = next;
      pane.style.setProperty("--pane-width", `${next}px`);
      handle.setAttribute("aria-valuenow", String(next));
    };
    handle.setAttribute("aria-valuemin", "320");
    handle.setAttribute("aria-valuemax", "1200");
    handle.setAttribute("aria-valuenow", String(Math.round(pane.getBoundingClientRect().width)));
    handle.addEventListener("pointerdown", event => {
      if (event.button !== 0) return;
      event.preventDefault();
      const startX = event.clientX;
      const startWidth = pane.getBoundingClientRect().width;
      const move = update => { update.preventDefault(); resize(startWidth + update.clientX - startX); };
      const end = () => {
        document.removeEventListener("pointermove", move);
        document.removeEventListener("mousemove", move);
        document.removeEventListener("pointerup", end);
        document.removeEventListener("mouseup", end);
        document.removeEventListener("pointercancel", end);
        saveLayout();
      };
      document.addEventListener("pointermove", move);
      document.addEventListener("mousemove", move);
      document.addEventListener("pointerup", end);
      document.addEventListener("mouseup", end);
      document.addEventListener("pointercancel", end);
    });
    handle.addEventListener("keydown", event => {
      if (event.key !== "ArrowLeft" && event.key !== "ArrowRight") return;
      event.preventDefault();
      resize(pane.getBoundingClientRect().width + (event.key === "ArrowRight" ? 32 : -32));
      saveLayout();
    });
  });
  $$("[data-pane-divider]").forEach(divider => {
    divider.addEventListener("pointerdown", event => startPaneDrag(divider, event));
    divider.addEventListener("keydown", event => {
      const axis = divider.dataset.paneDivider;
      const delta = axis.endsWith("x") ? event.key === "ArrowLeft" ? -3 : event.key === "ArrowRight" ? 3 : 0
        : event.key === "ArrowUp" ? -3 : event.key === "ArrowDown" ? 3 : 0;
      if (!delta) return;
      event.preventDefault();
      const field = paneSplitField(axis);
      setPaneSplit(axis, state[field] + delta, divider.closest(".pane-grid"));
      saveLayout();
    });
    divider.addEventListener("dblclick", () => { setPaneSplit(divider.dataset.paneDivider, divider.dataset.paneDivider.endsWith("x") ? divider.dataset.paneDivider.startsWith("ide") ? 70 : 55 : 60, divider.closest(".pane-grid")); saveLayout(); });
  });
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

const PANE_SPLIT_FIELDS = {
  "adaptive-x": "paneSplitX",
  "adaptive-left-y": "paneSplitLeftY",
  "adaptive-right-y": "paneSplitRightY",
  "ide-x": "paneIdeSplitX",
  "ide-left-y": "paneIdeSplitY",
  "ide-right-y": "paneIdeRightY"
};
function paneSplitField(axis) { return PANE_SPLIT_FIELDS[axis]; }
function setPaneSplit(axis, percent, grid) {
  const field = paneSplitField(axis);
  if (!field || !grid || !Number.isFinite(percent)) return;
  const horizontal = axis.endsWith("x");
  const size = horizontal ? grid.clientWidth : grid.clientHeight;
  const firstMinimum = horizontal ? axis.startsWith("ide") ? 340 : 240 : 145;
  const lastMinimum = horizontal ? axis.startsWith("ide") ? 250 : 240 : 145;
  const minimum = size > firstMinimum + lastMinimum ? firstMinimum / size * 100 : 25;
  const maximum = size > firstMinimum + lastMinimum ? 100 - lastMinimum / size * 100 : 75;
  const value = Math.round(Math.min(maximum, Math.max(minimum, percent)) * 10) / 10;
  state[field] = value;
  switch (axis) {
    case "adaptive-x": document.documentElement.style.setProperty("--pane-split-x", `${value}%`); break;
    case "adaptive-left-y": document.documentElement.style.setProperty("--pane-left-y", `${value}%`); break;
    case "adaptive-right-y": document.documentElement.style.setProperty("--pane-right-y", `${value}%`); break;
    case "ide-x": document.documentElement.style.setProperty("--pane-ide-x", `${value}%`); break;
    case "ide-left-y": document.documentElement.style.setProperty("--pane-ide-y", `${value}%`); break;
    case "ide-right-y": document.documentElement.style.setProperty("--pane-ide-right-y", `${value}%`); break;
  }
  grid.querySelector(`[data-pane-divider="${axis}"]`)?.setAttribute("aria-valuenow", String(Math.round(value)));
}
function startPaneDrag(divider, event) {
  if (event.button !== 0) return;
  const grid = divider.closest(".pane-grid");
  if (!grid) return;
  event.preventDefault();
  const axis = divider.dataset.paneDivider;
  const move = pointer => {
    const rect = grid.getBoundingClientRect();
    const value = axis.endsWith("x") ? (pointer.clientX - rect.left) / rect.width * 100 : (pointer.clientY - rect.top) / rect.height * 100;
    setPaneSplit(axis, value, grid);
  };
  move(event);
  const stop = () => {
    document.removeEventListener("pointermove", move);
    document.removeEventListener("pointerup", stop);
    document.removeEventListener("pointercancel", stop);
    saveLayout();
  };
  document.addEventListener("pointermove", move);
  document.addEventListener("pointerup", stop);
  document.addEventListener("pointercancel", stop);
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
  return draft.members.map((member,index)=>`<article class="team-member-editor" data-team-member data-member-id="${escapeHTML(member.id||"")}"><div class="form-grid"><label>ชื่อ<input name="member_name_${index}" required value="${escapeHTML(member.name||"")}"></label><label>หน้าที่<input name="member_role_${index}" required value="${escapeHTML(member.role||"")}"></label></div><label>คำสั่งสำหรับสมาชิก<textarea name="member_instructions_${index}" rows="2" required>${escapeHTML(member.instructions||"")}</textarea></label><div class="action-row"><label class="check-label"><input type="radio" name="lead_index" value="${index}" ${member.is_lead?"checked":""} required> หัวหน้าทีม</label><button type="button" class="danger" data-remove-team-member="${index}" ${draft.members.length===1?"disabled":""}>นำออก</button></div></article>`).join("");
}

function teamTaskRowHTML(team,index){
  const id=`task-${Date.now().toString(36)}-${index+1}`;
  return `<article class="team-task-editor" data-team-task><div class="form-grid"><label>รหัสงาน<input data-task-field="id" required value="${id}"></label><label>ผู้รับงาน<select data-task-field="member_id" required>${team.members.map(member=>`<option value="${escapeHTML(member.id)}">${escapeHTML(member.name)} · ${escapeHTML(member.role)}</option>`).join("")}</select></label></div><label>โมเดลของงานนี้<select data-task-field="provider_id"><option value="">ใช้โมเดลของทีม</option>${state.providers.filter(item=>item.enabled&&item.credential_ready).map(item=>`<option value="${escapeHTML(item.id)}">${escapeHTML(providerOptionLabel(item))}</option>`).join("")}</select></label><label>ชื่องาน<input data-task-field="title" required placeholder="ตรวจผลแยก"></label><label>รหัสงานที่ต้องเสร็จก่อน<input data-task-field="depends" placeholder="task-a, task-b"></label><label>คำสั่งของงาน<textarea data-task-field="prompt" rows="2" required></textarea></label><button type="button" class="danger" data-remove-team-task>นำงานออก</button></article>`;
}

function renderWorkbenchTeam(target = $(".pane-body[data-pane-kind='team']") || $("#workbenchContent")){
  if(!target)return;
  const team=state.teams.find(item=>item.id===state.selectedTeam);
  if(!state.teamDraft || state.teamDraft.sourceID!==(team?.id||"new"))state.teamDraft=newTeamDraft(team||null);
  const draft=state.teamDraft;
  const provider=state.providers.find(item=>item.id===state.defaultProviderID&&item.enabled&&item.credential_ready)
    ||state.providers.find(item=>item.enabled&&item.credential_ready)||state.providers.find(item=>item.enabled);
  const profiles=availableProfiles(provider);
  const profile=bestProfileFor(provider,profiles);
  target.innerHTML=`<div class="panel"><div class="provider-head"><div><p class="eyebrow">ทีมที่บันทึกไว้ · กำหนดสิทธิ์ชัดเจน</p><h3>${draft.id?"แก้ไข":"สร้าง"}ทีมตัวแทนทำงาน</h3></div>${draft.id?pill(`รุ่น ${draft.expected_revision}`,"blue"):pill("ทีมใหม่","green")}</div><label>ทีม<select id="teamSelect"><option value="">＋ สร้างทีมใหม่</option>${state.teams.map(item=>`<option value="${escapeHTML(item.id)}" ${item.id===state.selectedTeam?"selected":""}>${escapeHTML(item.name)}</option>`).join("")}</select></label><form id="teamCreateForm"><label>ชื่อทีม<input name="name" required value="${escapeHTML(draft.name)}"></label><label>กติกาของทีม<textarea name="instructions" rows="3" required>${escapeHTML(draft.instructions)}</textarea></label><section class="team-editor-list">${renderTeamMemberRows(draft)}</section><div class="action-row"><button type="button" class="ghost" id="addTeamMember" ${draft.members.length>=12?"disabled":""}>＋ เพิ่มสมาชิก</button><button class="primary">${draft.id?"บันทึกรุ่นนี้":"บันทึกทีมไว้ใช้ซ้ำ"}</button></div></form></div>
  <div class="panel spaced">${team?`<div class="meta">${team.members.map(member=>pill(`${member.is_lead?"หัวหน้า · ":""}${member.name} / ${member.role}`,member.is_lead?"green":"blue")).join("")}</div><form id="teamRunForm"><label>เป้าหมาย<textarea name="objective" rows="4" required placeholder="ให้ทีมช่วยทำงานอะไร?"></textarea></label><div class="form-grid"><label>โมเดลหลัก<select name="provider_id" required>${state.providers.filter(item=>item.enabled&&item.credential_ready).map(item=>`<option value="${escapeHTML(item.id)}" ${item.id===provider?.id?"selected":""}>${escapeHTML(providerOptionLabel(item))}</option>`).join("")}</select></label><label>ขนาดบริบท<select name="context_profile" required>${profiles.map(item=>`<option value="${escapeHTML(item.name)}" ${item.name===profile?.name?"selected":""}>${escapeHTML(profileLabel(item))}</option>`).join("")}</select></label></div><label>เหตุผลที่เลือกขนาดบริบทนี้ (หากยังไม่ผ่านการตรวจ)<input name="qualification_reason" placeholder="เหตุผลที่ตรวจทานและยอมรับขนาดบริบทนี้"></label><section id="teamRemoteApproval" class="panel" hidden><p>ทีมจะส่งข้อมูลที่จำเป็นจากพื้นที่ทำงานไปยัง <strong id="teamRemoteTarget"></strong></p><label><input type="checkbox" name="allow_remote"> อนุญาตให้งานที่เลือกใช้ผู้ให้บริการภายนอกนี้</label><label>เหตุผลการอนุญาต<input name="remote_egress_reason" placeholder="เช่น ทดสอบงานในโปรเจกต์นี้กับโมเดลที่เลือก"></label></section><label>จำนวนงานที่ทำพร้อมกัน<input name="max_parallel" type="number" min="1" max="4" value="3"></label><details class="team-graph"><summary>กำหนดลำดับงานเอง · ไม่บังคับ</summary><p class="form-note neutral">ปล่อยว่างเพื่อให้ระบบแบ่งงานและสรุปผลโดยหัวหน้าทีมอัตโนมัติ หากกำหนดงานเอง ให้ระบุรหัสงานที่ต้องเสร็จก่อนอย่างตรงตัว</p><div id="teamTaskRows"></div><button class="ghost" type="button" id="addTeamTask">＋ เพิ่มงาน</button></details><button class="primary">เริ่มงานทีม</button></form>`:`<div class="probe-empty">บันทึกหรือเลือกทีมก่อนเริ่มงาน</div>`}</div>
  <div class="card-list spaced">${state.teamRuns.slice(0,20).map(run=>`<article class="provider-card"><div class="provider-head"><div><h3>${escapeHTML(state.teams.find(item=>item.id===run.team_id)?.name||run.team_name||"งานทีม")}</h3><p>${escapeHTML(run.objective)} · ${formatDate(run.created_at)}</p></div>${pill(run.state,run.state==="completed"?"green":["failed","cancelled"].includes(run.state)?"red":"amber")}</div><div class="kv"><span>โมเดลหลัก</span><strong>${escapeHTML(state.providers.find(item=>item.id===run.provider_id)?.name||run.provider_id)} · ${escapeHTML(run.context_profile)}</strong><span>งานพร้อมกัน</span><strong>${run.max_parallel}</strong><span>โทเค็น</span><strong>${Number((run.prompt_tokens||0)+(run.completion_tokens||0)).toLocaleString()}</strong></div>${["queued","running","awaiting_approval"].includes(run.state)?`<div class="action-row"><button class="danger" data-cancel-team-run="${escapeHTML(run.id)}">หยุดทีมและงานย่อย</button></div>`:""}${(run.tasks||[]).map(task=>`<section class="team-task"><div class="provider-head"><div><strong>${escapeHTML(task.title)}</strong><small>${escapeHTML(task.member_name||"")} · ${escapeHTML(task.member_role||"")} · ${escapeHTML(state.providers.find(item=>item.id===(task.provider_id||run.provider_id))?.name||task.provider_id||run.provider_id)} · ${escapeHTML(task.context_profile||run.context_profile)}</small></div>${pill(task.state,task.state==="completed"?"green":["failed","cancelled"].includes(task.state)?"red":"amber")}</div>${task.state==="awaiting_approval"?`<article class="team-approval"><strong>${escapeHTML(task.approval_summary||"งานย่อยขออนุมัติการกระทำ")}</strong><p>${escapeHTML(task.approval_effect||"การกระทำ")}</p><pre>${escapeHTML(task.approval_preview||"ไม่มีตัวอย่างให้ดู")}</pre><div class="action-row"><button class="primary" data-team-approval="approve" data-run-id="${escapeHTML(run.id)}" data-task-id="${escapeHTML(task.id)}">อนุมัติการกระทำนี้</button><button class="danger" data-team-approval="deny" data-run-id="${escapeHTML(run.id)}" data-task-id="${escapeHTML(task.id)}">ปฏิเสธ</button></div></article>`:""}${task.result?`<p>${escapeHTML(task.result.slice(0,900))}</p>`:""}${task.error?`<p class="form-note">${escapeHTML(task.error)}</p>`:""}${task.session_id?`<button class="ghost" data-team-session="${escapeHTML(task.session_id)}">เปิดแชตงานย่อย</button>`:""}</section>`).join("")}</article>`).join("")||`<div class="probe-empty">ยังไม่มีงานทีม เมื่อเริ่มงาน ระบบจะแบ่งงานให้สมาชิกทำพร้อมกันและให้หัวหน้าทีมสรุปผล</div>`}</div>`;
  $("#teamCreateForm")?.addEventListener("submit",saveWorkbenchTeam);
  $("#teamSelect")?.addEventListener("change",event=>{state.selectedTeam=event.target.value||null;state.teamDraft=null;renderWorkbenchTeam();});
  $("#addTeamMember")?.addEventListener("click",()=>{captureTeamDraft();state.teamDraft.members.push({name:"",role:"specialist",instructions:"",is_lead:false});renderWorkbenchTeam();});
  $$('[data-remove-team-member]').forEach(button=>button.addEventListener("click",()=>{captureTeamDraft();state.teamDraft.members.splice(Number(button.dataset.removeTeamMember),1);if(!state.teamDraft.members.some(member=>member.is_lead))state.teamDraft.members[0].is_lead=true;renderWorkbenchTeam();}));
  const teamRunForm=$("#teamRunForm");
  teamRunForm?.addEventListener("submit",startWorkbenchTeamRun);
  teamRunForm?.querySelector('[name="provider_id"]')?.addEventListener("change",()=>updateTeamRunProvider(teamRunForm));
	teamRunForm?.addEventListener("change",event=>{if(event.target.matches('[data-task-field="provider_id"]'))updateTeamRunProvider(teamRunForm);});
  if(teamRunForm)updateTeamRunProvider(teamRunForm);
  $("#addTeamTask")?.addEventListener("click",()=>{$("#teamTaskRows").insertAdjacentHTML("beforeend",teamTaskRowHTML(team,$$('[data-team-task]').length));bindTeamTaskRemovers();updateTeamRunProvider(teamRunForm);});
  bindTeamTaskRemovers();
  $$('[data-cancel-team-run]').forEach(button=>button.addEventListener("click",()=>cancelWorkbenchTeamRun(button.dataset.cancelTeamRun)));
  $$('[data-team-approval]').forEach(button=>button.addEventListener("click",()=>decideWorkbenchTeamApproval(button.dataset.runId,button.dataset.taskId,button.dataset.teamApproval)));
  $$('[data-team-session]').forEach(button=>button.addEventListener("click",async()=>{if(await selectSession(button.dataset.teamSession)){switchTab("chat");switchView("chat");}}));
  if(state.teamRuns.some(run=>["queued","running"].includes(run.state)))scheduleWorkbenchPoll(pollTeamRuns,900);
}

function bindTeamTaskRemovers(){$$('[data-remove-team-task]').forEach(button=>button.onclick=()=>{button.closest('[data-team-task]').remove();const form=$("#teamRunForm");if(form)updateTeamRunProvider(form);});}

function updateTeamRunProvider(form){
  const provider=state.providers.find(item=>item.id===form.querySelector('[name="provider_id"]')?.value);
  const context=form.querySelector('[name="context_profile"]');
  const previous=context.value;
  const profiles=availableProfiles(provider);
  const preferred=bestProfileFor(provider,profiles)?.name;
  context.innerHTML=profiles.map(item=>`<option value="${escapeHTML(item.name)}">${escapeHTML(profileLabel(item))}</option>`).join("");
  context.value=profiles.some(item=>item.name===previous)?previous:preferred||"";
  const routes=[provider,...$$("[data-team-task] [data-task-field=provider_id]",form).map(select=>state.providers.find(item=>item.id===select.value))].filter(Boolean);
  const remotes=routes.filter(item=>!taskProviderIsLocal(item));
  const remote=remotes.length>0;
  const approval=form.querySelector("#teamRemoteApproval");
  approval.hidden=!remote;
  approval.querySelector("#teamRemoteTarget").textContent=remote?remotes.map(item=>`${item.name} · ${providerModelLabel(item)} · ${item.base_url}`).join(" / "):"";
  approval.querySelector('[name="allow_remote"]').required=Boolean(remote);
  approval.querySelector('[name="remote_egress_reason"]').required=Boolean(remote);
  form.querySelector('button[type="submit"],button.primary:last-of-type').disabled=!provider||!profiles.length;
}

async function saveWorkbenchTeam(event){event.preventDefault();captureTeamDraft();const draft=state.teamDraft;try{const team=await api("/api/teams",{method:"POST",body:JSON.stringify({id:draft.id||"",expected_revision:draft.expected_revision||0,project_id:state.selectedProject||"",name:draft.name,instructions:draft.instructions,actor:currentActor(),members:draft.members})});state.teams=await api("/api/teams");state.selectedTeam=team.id;state.teamDraft=newTeamDraft(team);toast("Reusable team saved with one explicit lead");renderWorkbenchTeam();}catch(error){toast(error.message,true);}}
async function startWorkbenchTeamRun(event){
  event.preventDefault();
  const form=new FormData(event.currentTarget);
  const provider=state.providers.find(item=>item.id===form.get("provider_id"));
  const chosenTaskProviders=$$("[data-team-task] [data-task-field=provider_id]",event.currentTarget).map(select=>state.providers.find(item=>item.id===select.value)).filter(Boolean);
  const remote=[provider,...chosenTaskProviders].some(item=>item&&!taskProviderIsLocal(item));
  const remoteReason=String(form.get("remote_egress_reason")||"").trim();
  if(remote&&(!form.has("allow_remote")||!remoteReason)){
    toast("ตรวจและอนุญาตการส่งข้อมูลไปยังผู้ให้บริการภายนอกก่อนเริ่ม Team",true);
    return;
  }
  const tasks=$$('[data-team-task]',event.currentTarget).map(row=>({id:row.querySelector('[data-task-field="id"]').value.trim(),member_id:row.querySelector('[data-task-field="member_id"]').value,provider_id:row.querySelector('[data-task-field="provider_id"]').value,title:row.querySelector('[data-task-field="title"]').value.trim(),prompt:row.querySelector('[data-task-field="prompt"]').value.trim(),depends_on:row.querySelector('[data-task-field="depends"]').value.split(",").map(value=>value.trim()).filter(Boolean)}));
  const actor=currentActor();
  try{
    const run=await api("/api/team-runs",{method:"POST",body:JSON.stringify({team_id:state.selectedTeam,project_id:state.selectedProject||"",objective:form.get("objective"),provider_id:provider?.id,context_profile:form.get("context_profile"),qualification_reason:form.get("qualification_reason"),egress_policy:remote?"remote_allowed":"local_only",remote_egress_approval:remote?{actor,reason:remoteReason}:undefined,max_parallel:Number(form.get("max_parallel")),actor,tasks})});
    state.teamRuns.unshift(run);
    toast("เริ่ม Team แล้ว ติดตามผล Worker และ Main ด้านล่าง");
    renderWorkbenchTeam();
  }catch(error){toast(error.message,true);}
}
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
  { group: "โมเดล", items: [
    { id:"providers", icon:"model", label:"โมเดล", blurb:"ผู้ให้บริการ คีย์ และผลตรวจความพร้อม",
      terms:"provider endpoint api key token openai compatible qualification context window local runtime ollama" },
    { id:"context", icon:"context", label:"บริบทโมเดล", blurb:"ขนาดบริบทและข้อมูลที่ส่งเข้าโมเดล",
      terms:"context profile budget fragment compile ledger spill token estimate 32k 64k 128k 256k 1m" },
    { id:"fidelity", icon:"fidelity", label:"ความแม่นยำของบริบท", blurb:"หลักฐานรองรับความสามารถของโมเดล",
      terms:"fidelity recall evidence corpus case run positional" }
  ]},
  { group: "เครื่องมือ · สกิล · MCP", items: [
    { id:"capabilities", icon:"tools", label:"เครื่องมือ · สกิล · MCP", blurb:"ค้นหาและดูความพร้อมในที่เดียว",
      terms:"capability tool skill mcp server catalog ความสามารถ เครื่องมือ สกิล" },
    { id:"mcp", icon:"tools", label:"เซิร์ฟเวอร์ MCP", blurb:"การเชื่อมต่อและเครื่องมือของแต่ละเซิร์ฟเวอร์",
      terms:"mcp server bearer token streamable http discovery capability tool schema approval" },
    { id:"tools", icon:"tools", label:"เครื่องมือในตัว", blurb:"เครื่องมือที่แชทเรียกใช้ได้",
      terms:"direct built in tools files commands browser skill context discovery เครื่องมือ" },
    { id:"library", icon:"skill", label:"จัดการสกิล", blurb:"สกิลที่เปิดใช้และขอบเขตสิทธิ์",
      terms:"skill library active authority policy promote fork scope pinned" },
    { id:"proposals", icon:"review", label:"ข้อเสนอสกิล", blurb:"รายการที่รอคำตัดสิน",
      terms:"candidate proposal review promote reject quarantine", badge:"proposals" },
    { id:"learning", icon:"learning", label:"การเรียนรู้", blurb:"ผลทบทวนงานจริงเบื้องหลัง",
      terms:"learning review queue reviewer evidence", badge:"reviews" },
    { id:"insights", icon:"insights", label:"ข้อค้นพบ", blurb:"ผลวิเคราะห์ที่ยังไม่เปลี่ยนระบบ",
      terms:"curator finding stale duplicate consolidation relation" },
    { id:"archive", icon:"archive", label:"สกิลที่เก็บไว้", blurb:"นำสกิลกลับมาเป็นข้อเสนอได้",
      terms:"archive restore deleted reversible" }
  ]},
  { group: "โปรเจกต์และสิทธิ์", items: [
    { id:"discord", icon:"chat", label:"สั่งงานผ่าน Discord", blurb:"สั่งงานผ่าน Discord ของคุณ",
      terms:"discord remote bot gateway รีโมท ดิสคอร์ด" },
    { id:"projects", icon:"project", label:"โปรเจกต์", blurb:"โฟลเดอร์งานและคำสั่งที่อนุญาต",
      terms:"project workspace root command allowlist file tree permission" }
  ]},
  { group: "หน้าตาและภาษา", items: [
    { id:"appearance", icon:"settings", label:"หน้าตาและการอ่าน", blurb:"ระยะห่างและภาษาแสดงผล",
      terms:"appearance language density compact comfortable theme font ภาษา การอ่าน หน้าตา" }
  ]},
  { group: "ขั้นสูง", items: [
    { id:"office", icon:"jobs", label:"งานเบื้องหลัง", blurb:"งานที่ใช้เวลานานและหลักฐานผลลัพธ์",
      terms:"job background queue cancel receipt" },
    { id:"artifacts", icon:"artifact", label:"ไฟล์ผลลัพธ์", blurb:"ไฟล์และหลักฐานที่ระบบสร้าง",
      terms:"artifact cas checksum deliverable docx xlsx pptx pdf" },
    { id:"maintenance", icon:"settings", label:"ดูแลระบบ", blurb:"การใช้งาน ความจำ สำรองและกู้คืน",
      terms:"usage memory backup import export schedule garbage collection quarantine restore setting" }
  ]}
];

const CONFIG_PAGE_IDS = CONFIG_SECTIONS.flatMap(section => section.items.map(item => item.id));

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
    : `<p class="command-empty">ไม่พบการตั้งค่าที่ตรงกับ “${escapeHTML(query)}” ลองใช้คำค้นอื่น</p>`;
  $$("[data-config-page]", nav).forEach(button =>
    button.addEventListener("click", () => switchTab(button.dataset.configPage)));
  const jump = $("#configMobileJump");
  if (jump) {
    const visibleIDs = groups.flatMap(section => section.items.map(item => item.id));
    jump.innerHTML = `${visibleIDs.includes(state.activeTab) ? "" : `<option value="">${groups.length ? "เลือกจากผลการค้นหา" : "ไม่พบการตั้งค่า"}</option>`}${groups.map(section => `<optgroup label="${escapeHTML(section.group)}">${section.items.map(item => `<option value="${escapeHTML(item.id)}">${escapeHTML(item.label)}</option>`).join("")}</optgroup>`).join("")}`;
    jump.value = visibleIDs.includes(state.activeTab) ? state.activeTab : "";
    jump.onchange = () => { if (CONFIG_PAGE_IDS.includes(jump.value)) switchTab(jump.value); };
  }
}

function openConfig(page = "") {
  const target = CONFIG_PAGE_IDS.includes(page) ? page
    : CONFIG_PAGE_IDS.includes(state.activeTab) ? state.activeTab
    : "providers";
  switchTab(target);
}

let configReturnFocus = null;
function closeConfig() {
  switchTab("chat");
  const opener = configReturnFocus;
  configReturnFocus = null;
  const target = opener && opener !== document.body && opener.isConnected && !opener.closest?.("#configOverlay") ? opener : $("#openConfig");
  target?.focus?.({preventScroll:true});
}

// switchTab is the single place that decides whether you are looking at the
// work or at the settings that shape it.
function switchTab(tab) {
  const isConfig = CONFIG_PAGE_IDS.includes(tab);
  const overlay = $("#configOverlay");
  if (isConfig && overlay.hidden) configReturnFocus = document.activeElement;
  if (isConfig) { ++navigationGeneration; dismissMobileRail(); }
  state.activeTab = isConfig ? tab : "chat";
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
  $("#stats").hidden = true;
  $("#libraryIntro").hidden = tab !== "library";
  $("#libraryToolbar").hidden = tab !== "library";
  $("#configPane").scrollTop = 0;
  renderConfigNav();
}

let candidateDialogInvoker = null;
function openCandidateDialog() {
  candidateDialogInvoker = document.activeElement;
  $("#candidateDialog").showModal();
}

function closeCandidateDialog() { $("#candidateDialog").close(); }

async function importSkillFile(event) {
  const file = event.currentTarget.files?.[0];
  if (!file) return;
  const data = new FormData();
  data.set("file", file);
  event.currentTarget.disabled = true;
  try {
    const candidate = await api("/api/skills/import", {method:"POST", body:data});
    toast(candidate.state === "quarantined" ? "ไฟล์นำเข้าแล้ว แต่ต้องแก้ผลตรวจสอบก่อนเปิดใช้" : "นำเข้าแล้ว · ตรวจข้อเสนอเพื่อเปิดใช้");
    await load();
    switchTab("proposals");
    await inspectCandidate(candidate.id);
  } catch (error) { toast(error.message, true); }
  finally { event.currentTarget.disabled = false; event.currentTarget.value = ""; }
}

async function renameProject(project) {
  const name = await askAction({title:`เปลี่ยนชื่อ ${project.name}`, message:"ระบุชื่อใหม่ของโปรเจกต์", confirmLabel:"บันทึกชื่อ", reasonLabel:"ชื่อใหม่"});
  if (!name?.trim() || name.trim() === project.name) return;
  try {
    await api("/api/projects", {method:"POST", body:JSON.stringify({id:project.id, name:name.trim(), root_path:project.root_path})});
    await load();
    toast("เปลี่ยนชื่อโปรเจกต์แล้ว");
  } catch (error) { toast(error.message, true); }
}

async function archiveProjectFromUI(project) {
  const approved = await askAction({title:`นำ ${project.name} ออกจากรายการ?`, message:"ไฟล์ในโฟลเดอร์และประวัติงานจะยังอยู่ กู้คืนได้จากหน้า Projects", confirmLabel:"นำออกจากรายการ", danger:true});
  if (!approved) return;
  try {
    await api(`/api/projects/${encodeURIComponent(project.id)}/archive`, {method:"POST", body:"{}"});
    if (state.currentProject?.id === project.id) { state.currentProject = null; showPicker(); }
    state.selectedProject = null;
    state.projectFiles = [];
    await load();
    toast("นำออกจากรายการแล้ว · กู้คืนได้");
  } catch (error) { toast(error.message, true); }
}

async function restoreProjectFromUI(id) {
  try {
    const project = await api(`/api/projects/${encodeURIComponent(id)}/restore`, {method:"POST", body:"{}"});
    state.selectedProject = project.id;
    await load();
    toast("กู้คืนโปรเจกต์แล้ว");
  } catch (error) { toast(error.message, true); }
}

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

function skillPresenceBadge(item, suggested, lastContext) {
  if (lastContext.has(`skill:${item.skill_id}:${item.version_id}`)) return pill("in last request", "green");
  if (suggested.has(item.canonical_name)) return pill("suggested", "blue");
  return item.pinned ? pill("pinned", "blue") : "";
}

function skillSelectionSubtitle(item, suggestedBindings) {
  const reason = suggestedBindings.find(binding => binding.skill_id === item.skill_id && binding.version_id === item.version_id)?.selection_reason;
  return reason ? `${item.summary || ""} · Suggested because: ${reason}` : item.summary;
}

function renderCapabilityPicker() {
  const body = $("#capabilityPickerBody");
  if (!body) return;
  const filter = state.capabilityPickerFilter;
  const query = ($("#capabilityPickerQuery")?.value || "").trim().toLowerCase();
  const session = state.sessionDetail?.session;
  const contract = session?.contract || {};
  const suggestedBindings = contract.selected_skills || [];
  const selected = new Set(suggestedBindings.map(item => item.canonical_name));
  const lastContext = new Set(state.sessionDetail?.latest_context_snapshot?.report?.selected_ids || []);
  const matches = (haystack) => !query || haystack.toLowerCase().includes(query);
  const chips = `<div class="capability-filter">${CAPABILITY_FILTERS.map(([value, label]) =>
    `<button type="button" class="capability-chip ${value === filter ? "active" : ""}" data-picker-filter="${value}">${label}</button>`).join("")}</div>`;

  let markup = chips;
  if (filter === "all" || filter === "skills") {
    const skills = (contract.skill_catalog || []).filter(item => matches(`${item.canonical_name} ${item.summary}`));
    markup += `<section class="capability-picker-section"><header><span>Skills in this session</span><span>${skills.length}</span></header><div class="capability-picker-list">${skills.length
      ? skills.map(item => capabilityPickHTML("skill", "skill", item.canonical_name, item.canonical_name, skillSelectionSubtitle(item, suggestedBindings),
          skillPresenceBadge(item, selected, lastContext), item.skill_id, item.version_id)).join("")
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
      : results.length ? results.map(item => capabilityPickHTML("mcp", "tools", item.title || item.name, capabilityDisplayTitle(item), item.description,
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

// A newly promoted Skill needs a new Session Contract. Keep the existing
// conversation and its draft intact, then resolve the exact frozen version.
async function mentionSkill(skill) {
  const bindingForCurrentSession = () => (state.sessionDetail?.session?.contract?.skill_catalog || [])
    .find(item => item.skill_id === skill.id || item.canonical_name === skill.canonical_name);
  let binding = bindingForCurrentSession();
  if (!binding) {
    if (skill.state !== "active" || !skill.enabled) { toast("สกิลนี้ยังไม่พร้อมใช้ในแชทใหม่", true); return; }
    const confirmed = await askAction({
      title:`เริ่มแชทใหม่เพื่อใช้ ${skill.canonical_name}?`,
      message:"แชทเดิมใช้รายการ Skill ที่ตรึงไว้ตอนเริ่มงาน ข้อความร่างและประวัติของแชทเดิมจะยังอยู่",
      confirmLabel:"เริ่มแชทใหม่"
    });
    if (!confirmed || !beginNewTaskDraft()) return;
    const session = await createAgentSession(`ใช้สกิล ${skill.canonical_name}`);
    if (!session) return;
    binding = bindingForCurrentSession();
    if (!binding) { toast("แชทใหม่ไม่มี Skill นี้ในรายการที่ตรึงไว้ ตรวจการเปิดใช้ใน Skill Studio", true); return; }
  }
  appendComposerInstruction(`Follow the reviewed Skill "${binding.canonical_name}" (frozen version ${binding.version_id}) included in this session's context.`);
}

// Direct tools and MCP capabilities get different instructions. The latter
// must still pass through search/describe/call, revision checks and approval.
function mentionCapability(capability) {
  if (typeof capability === "string") capability = { kind:"tool", name:capability };
  const useError = capabilityUseReadiness(capability.kind === "mcp" ? "mcp" : "tool", capability.name);
  if (useError) { toast(useError, true); return; }
  if (capability.kind === "mcp") {
    const item = state.capabilityPickerResults.find(candidate => candidate.id === capability.id) || state.capabilityResults.find(candidate => candidate.id === capability.id);
    if (item?.readiness !== "ready") { toast("เครื่องมือ MCP นี้ยังไม่พร้อม ตรวจการเชื่อมต่อและค้นพบอีกครั้ง", true); return; }
    const source = item ? ` from MCP server "${capabilitySourceLabel(item)}"` : "";
    appendComposerInstruction(`Use the deferred capability "${capability.name}"${source} (catalog id "${capability.id}"). Verify it with tool_search, load its exact schema and revision with tool_describe, then use tool_call; do not bypass required approval.`);
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
  // Another screen can add/discover MCP servers while this chat stays open.
  // Refresh the server names and indexed count before presenting search results.
  void api("/api/bootstrap").then(data => {
    if (!dialog.open) return;
    state.mcp_servers = asList(data.mcp_servers);
    state.capability_summary = data.capability_summary || state.capability_summary;
    renderCapabilityPicker();
  }).catch(error => { if (dialog.open) toast(`อ่านรายการ MCP ล่าสุดไม่ได้: ${error.message}`, true); });
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
      paneWidths: layout.paneWidths,
      maximised: layout.maximisedPane,
      paneLayout: layout.paneLayout,
      paneSplitX: state.paneSplitX,
      paneSplitY: state.paneSplitY,
      paneSplitLeftY: state.paneSplitLeftY,
      paneSplitRightY: state.paneSplitRightY,
      paneIdeSplitX: state.paneIdeSplitX,
      paneIdeSplitY: state.paneIdeSplitY,
      paneIdeRightY: state.paneIdeRightY,
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
  let restoredPanes = Array.isArray(saved?.panes) && saved.panes.length ? saved.panes : ["chat", "review", "files"];
  if (saved?.paneLayout === "ide" && restoredPanes[0] === "files" && restoredPanes[1] === "editor") {
    restoredPanes = ["editor", restoredPanes.includes("chat") ? "chat" : "files", ...restoredPanes.filter(id => ["terminal", "output", "debug"].includes(id)).slice(0, 1)];
  }
  state.panes = [...new Set(restoredPanes.map(id => paneContent(id).id === "notes" && id.startsWith("notes:") ? id : paneContent(id).id))];
  // A file selection is transient. Restore the user's other panes, but never
  // resurrect an empty Code window after reopening a project or the page.
  if (!state.projectFile && state.panes.includes("editor")) {
    const editorIndex = state.panes.indexOf("editor");
    if (state.panes.includes("review")) state.panes.splice(editorIndex, 1);
    else state.panes[editorIndex] = "review";
  }
  if (!state.panes.includes(state.compactPane)) state.compactPane = state.panes[0];
  state.paneWidths = saved?.paneWidths && typeof saved.paneWidths === "object" ? saved.paneWidths : {};
  state.paneLayout = normalisePaneLayout(state.panes.length, saved?.paneLayout || "adaptive");
  const restoredSplit = (value, fallback) => Number.isFinite(value) ? Math.min(80, Math.max(20, value)) : fallback;
  state.paneSplitX = restoredSplit(saved?.paneSplitX, 55);
  state.paneSplitY = restoredSplit(saved?.paneSplitY, 55);
  state.paneSplitLeftY = restoredSplit(saved?.paneSplitLeftY ?? saved?.paneSplitY, 55);
  state.paneSplitRightY = restoredSplit(saved?.paneSplitRightY ?? saved?.paneSplitY, 55);
  state.paneIdeSplitX = restoredSplit(saved?.paneIdeSplitX, 70);
  state.paneIdeSplitY = restoredSplit(saved?.paneIdeSplitY, 72);
  state.paneIdeRightY = restoredSplit(saved?.paneIdeRightY, 72);
  state.railProjectsOpen = saved?.railProjectsOpen !== false;
  state.railSetupOpen = Boolean(saved?.railSetupOpen);
  state.railProjectOpen = saved?.railProjectOpen && typeof saved.railProjectOpen === "object" ? saved.railProjectOpen : {};
  state.sessionOptionsOpen = Boolean(saved?.sessionOptionsOpen);
  document.documentElement.style.setProperty("--pane-split-x", `${state.paneSplitX}%`);
  document.documentElement.style.setProperty("--pane-split-y", `${state.paneSplitY}%`);
  document.documentElement.style.setProperty("--pane-left-y", `${state.paneSplitLeftY}%`);
  document.documentElement.style.setProperty("--pane-right-y", `${state.paneSplitRightY}%`);
  document.documentElement.style.setProperty("--pane-ide-x", `${state.paneIdeSplitX}%`);
  document.documentElement.style.setProperty("--pane-ide-y", `${state.paneIdeSplitY}%`);
  document.documentElement.style.setProperty("--pane-ide-right-y", `${state.paneIdeRightY}%`);
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
  $$("#viewSwitch [data-view]").forEach(button => button.addEventListener("click", () => button.dataset.view === "code" ? openWorkspace() : switchView(button.dataset.view)));
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
  document.addEventListener("pointerdown", event => {
    const menu = $("#workspacePaneMenu");
    if (!menu || menu.hidden || menu.contains(event.target) || $("#paneAdd")?.contains(event.target)) return;
    menu.hidden = true;
    $("#paneAdd")?.setAttribute("aria-expanded", "false");
  });
  $("#refreshButton").addEventListener("click", load);
  $("#createButton").addEventListener("click", openCandidateDialog);
  $("#candidateForm").addEventListener("submit", submitCandidate);
  $$('[data-close-candidate]').forEach(button => button.addEventListener("click", closeCandidateDialog));
  $("#candidateDialog").addEventListener("close", () => {
    if (candidateDialogInvoker?.isConnected) candidateDialogInvoker.focus();
    candidateDialogInvoker = null;
  });
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
  $("#pickerBrowseExisting").addEventListener("click", () => openFolderPicker("", "picker"));
  $("#pickerBackToChat").addEventListener("click", () => { state.pendingWorkspaceGoal = ""; showShell(); switchView("chat"); });
  $("#projectChip").addEventListener("click", () => showPicker());
  initPicker();
});
