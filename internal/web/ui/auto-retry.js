// Durable continuations run on the server. This poller displays their results
// without stealing focus or replacing a draft typed while the model works.
function autoRetryNoticeHTML(detail) {
  let retry = detail?.auto_retry;
  if (retry && detail?.session?.state === 'running' && detail.events?.some(event => event.turn_id === detail.session.active_turn_id && event.metadata?.auto_retry)) retry = {...retry,state:'running'};
  const client = state.failedRequest;
  const clientPending = Boolean(client && detail?.session?.id) && client.sessionID === detail.session.id && client.retryAt && !client.paused && state.chatTurnError;
  if (!retry || !['pending','running','paused'].includes(retry.state)) {
    if (!clientPending) return '';
    return `<div class="auto-retry-notice" role="status"><span>ลองส่งอัตโนมัติทุก 5 นาที · ครั้งถัดไป ${escapeHTML(new Date(client.retryAt).toLocaleTimeString([], {hour:'2-digit',minute:'2-digit'}))}</span><button type="button" class="ghost compact" data-pause-auto-retry>หยุดทำต่ออัตโนมัติ</button></div>`;
  }
  if (retry.state === 'paused') return '<div class="auto-retry-notice"><span>หยุดทำต่ออัตโนมัติแล้ว · กดทำต่อเองได้</span></div>';
  const when = new Date(retry.next_attempt_at).toLocaleTimeString([], {hour:'2-digit',minute:'2-digit'});
  return `<div class="auto-retry-notice" role="status"><span>${retry.state === 'running' ? 'กำลังทำต่อจากงานที่บันทึกไว้' : `ทำต่ออัตโนมัติทุก 5 นาที · ครั้งถัดไป ${escapeHTML(when)}`}</span><button type="button" class="ghost compact" data-pause-auto-retry>หยุดทำต่ออัตโนมัติ</button></div>`;
}

function bindAutoRetryPause(root) {
  for (const button of root?.querySelectorAll('[data-pause-auto-retry]') || []) {
    if (button.dataset.bound) continue;
    button.dataset.bound = 'true';
    button.addEventListener('click', async () => {
      const id = state.sessionDetail?.session?.id;
      if (!id) return;
      button.disabled = true;
      try {
        await api(`/api/sessions/${encodeURIComponent(id)}/auto-retry/pause`, {method:'POST',body:'{}'});
        if (state.failedRequest?.sessionID === id) state.failedRequest.paused = true;
        await selectSession(id, {refresh:true});
        refreshPaneChat();
      } catch (error) { toast(error.message,true); button.disabled = false; }
    });
  }
}

let autoRetryPolling = false;
async function refreshAutomaticTurns() {
  const id = state.sessionDetail?.session?.id;
  if (!id || autoRetryPolling || state.sending || state.sessionCreationPending || state.startingSession) return;
  const retry = state.sessionDetail.auto_retry;
  const failed = state.failedRequest;
  const clientPending = failed?.sessionID === id && failed.retryAt && !failed.paused && state.chatTurnError;
  if (!clientPending && !['pending','running'].includes(retry?.state) && state.sessionDetail.session.state !== 'running') return;
  autoRetryPolling = true;
  try {
    const detail = await api(`/api/sessions/${encodeURIComponent(id)}`);
    if (state.sessionDetail?.session?.id !== id || state.sending) return;
    captureComposer();
    const previous = state.sessionDetail;
    state.sessionDetail = detail;
    if (previous.events?.at(-1)?.id !== detail.events?.at(-1)?.id || previous.session.state !== detail.session.state || previous.auto_retry?.state !== detail.auto_retry?.state) {
      renderChat(); refreshPaneChat(); scheduleAutomaticApprovals();
      if (workspacePaneVisible('review')) renderWorkbenchReview();
    }
    if (!clientPending || Date.now() < failed.retryAt || detail.session.state !== 'active' || detail.session.active_turn_id) return;
    const latest = [...(detail.events || [])].reverse().find(event => event.event_kind === 'message' || event.event_kind === 'turn_failed');
    // A committed failure/partial belongs to the durable server schedule. A
    // completed answer received after a dropped stream must never be replayed.
    if (latest?.id !== failed.beforeEventID) { failed.retryAt = null; return; }
    if (detail.auto_retry?.state === 'paused') { failed.paused = true; return; }
    failed.retryAt = Date.now() + 300000;
    const ok = await submitChatText(failed.content, failed.attachments || [], {draftText:failed.draftText});
    if (ok) {
      if (state.draftMessage === failed.draftText) setComposerDraft('',[],id);
      if (state.paneChatDraft === failed.draftText) state.paneChatDraft = '';
      renderChat(); refreshPaneChat();
    }
  } catch { /* Keep the original deadline/request; retry connectivity next poll. */ }
  finally { autoRetryPolling = false; }
}
setInterval(refreshAutomaticTurns, 5000);

const syncedApprovalModes = new Map();
async function syncConversationApprovalMode(id) {
 const mode = window.HermetrixConversation?.mode?.() || 'ask_all';
 if (syncedApprovalModes.get(id) === mode) return;
 await api(`/api/sessions/${encodeURIComponent(id)}/approval-mode`, {method:'POST', body:JSON.stringify({mode})});
 syncedApprovalModes.set(id, mode);
}
function applySessionApprovalMode(detail) {
 if (!detail.approval_mode || !detail.session?.id) return;
 syncedApprovalModes.set(detail.session.id,detail.approval_mode);
 window.HermetrixConversation?.setMode(detail.approval_mode);
}
window.HermetrixAutoRetry = {syncApprovalMode:syncConversationApprovalMode,applySessionMode:applySessionApprovalMode};
document.addEventListener('hermetrix:approval-mode', () => {
 const id = state.sessionDetail?.session?.id;
 if (id) void syncConversationApprovalMode(id).catch(error => toast(error.message,true));
});
