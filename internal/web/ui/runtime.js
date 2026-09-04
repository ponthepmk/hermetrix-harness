(function exposeHermetrixRuntime(root, factory) {
  const runtime = factory();
  if (typeof module !== "undefined" && module.exports) module.exports = runtime;
  root.HermetrixRuntime = runtime;
})(typeof globalThis !== "undefined" ? globalThis : this, function buildHermetrixRuntime() {
  "use strict";

  function escapeHTML(value = "") {
    return String(value).replace(/[&<>'"]/g, char => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", "'": "&#39;", '"': "&quot;" })[char]);
  }

  function asList(value) {
    return Array.isArray(value) ? value : [];
  }

  function toolArgumentsPreview(event) {
    let preview = String(event.content || "");
    if (event.metadata?.tool_name === "workspace.write_file") {
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

  return Object.freeze({ escapeHTML, asList, toolArgumentsPreview, toolReceiptOf, toolOutputPreview, groupTimeline });
});
