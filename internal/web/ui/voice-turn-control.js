/* Owns voice generation interruption independently of capture and playback.
   A stop POST is intent; the persisted exact-turn receipt confirms termination. */
(() => {
  "use strict";

  function create({stop, read, reconcile, delay = ms => new Promise(resolve => setTimeout(resolve, ms)), onState = () => {}, polls = 24}) {
    let current = null;
    const ready = () => !current || current.settled && !current.pending && !current.blocked && (!current.interrupted || current.confirmed);
    const notify = (request, phase) => {
      if (current === request) onState(phase, snapshot());
    };
    const snapshot = () => current ? {
      sessionID: current.sessionID, turnID: current.turnID, phase: current.phase,
      pending: current.pending, blocked: current.blocked, interrupted: current.interrupted,
    } : {phase:"idle", pending:false, blocked:false, interrupted:false};
    function phase(request, value) {
      if (current !== request) return;
      request.phase = value;
      notify(request, value);
    }

    async function confirm(request) {
      for (let attempt = 0; attempt < polls; attempt++) {
        if (current !== request) return false;
        const detail = await read(request.sessionID);
        if (current !== request) return false;
        const control = detail?.turn_control;
        const released = detail?.session && detail.session.active_turn_id !== request.turnID;
        if (released && (control?.turn_id === request.turnID && control.state === "stopped" || request.terminal === "completed")) {
          request.confirmed = true;
          request.blocked = false;
          return true;
        }
        if (control?.turn_id === request.turnID && control.state === "needs_review") break;
        if (attempt + 1 < polls) await delay(250);
      }
      return false;
    }

    function operate(request, inspect = false) {
      if (current !== request) return Promise.resolve(false);
      if (request.pending || !inspect && request.operation) return request.operation;
      if (!request.turnID) {
        if (inspect) { request.blocked = true; phase(request, "needs_review"); }
        return Promise.resolve(false);
      }
      request.pending = true;
      phase(request, "stopping");
      request.operation = Promise.resolve().then(async () => {
        if (current !== request) return false;
        try {
          await (inspect ? reconcile : stop)(request.sessionID, request.turnID);
        } catch {
          // A stop racing natural completion may have a valid terminal receipt.
          // Read it; never replay the mutation because its response was lost.
        }
        if (current !== request) return false;
        return confirm(request);
      }).catch(() => false).then(confirmed => {
        if (current === request) {
          request.pending = false;
          request.blocked = !confirmed;
          phase(request, confirmed ? ready() ? "idle" : "stopped" : "needs_review");
        }
        return confirmed;
      });
      return request.operation;
    }

    function interrupt(request = current) {
      if (!request || current !== request) return Promise.resolve(false);
      if (request.operation) return request.operation;
      if (request.settled && !request.blocked) return Promise.resolve(false);
      request.interrupted = true;
      phase(request, request.turnID ? "stopping" : "waiting_for_commit");
      return operate(request);
    }

    function begin(sessionID) {
      if (!sessionID || !ready()) return null;
      const request = {sessionID, turnID:"", settled:false, pending:false, blocked:false,
        interrupted:false, confirmed:false, operation:null, terminal:"", phase:"generating"};
      current = request;
      phase(request, "generating");
      return Object.freeze({
        commit(turnID) {
          if (current !== request || request.settled || !turnID || request.turnID && request.turnID !== turnID) return false;
          request.turnID = turnID;
          if (request.interrupted) void operate(request);
          return true;
        },
        terminal(kind, turnID) {
          if (current === request && request.turnID && turnID === request.turnID && ["completed", "stopped", "failed"].includes(kind)) request.terminal = kind;
        },
        finish({failed = false} = {}) {
          if (current !== request) return;
          request.settled = true;
          if (failed && !request.terminal || request.interrupted && !request.turnID) request.blocked = true;
          if (request.blocked) phase(request, "needs_review");
          else if (ready()) phase(request, "idle");
        },
        interrupt: () => interrupt(request),
      });
    }

    return Object.freeze({begin, ready, snapshot, interrupt: () => interrupt(),
      inspect: () => current ? operate(current, true) : Promise.resolve(true),
      reset() { current = null; onState("idle", snapshot()); },
    });
  }

  const api = Object.freeze({create});
  if (typeof module !== "undefined" && module.exports) module.exports = api;
  if (typeof window !== "undefined") window.HermetrixVoiceTurnControl = api;
})();
