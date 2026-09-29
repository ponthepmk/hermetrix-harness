(function exposeHermetrixRuntime(root, factory) {
  const runtime = factory();
  if (typeof module !== "undefined" && module.exports) module.exports = runtime;
  root.HermetrixRuntime = runtime;
})(typeof globalThis !== "undefined" ? globalThis : this, function buildHermetrixRuntime() {
  "use strict";

  function escapeHTML(value = "") {
    return String(value).replace(/[&<>'"]/g, char => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", "'": "&#39;", '"': "&quot;" })[char]);
  }

  // Chat content is untrusted. Format a small Markdown subset while escaping
  // every text fragment; no authored HTML or clickable URLs enter the DOM.
  function renderChatMarkdown(value = "") {
    const inline = value => String(value).split(/(`[^`\n]+`)/g).map(part => {
      if (part.startsWith("`") && part.endsWith("`")) return `<code>${escapeHTML(part.slice(1, -1))}</code>`;
      return escapeHTML(part).replace(/\*\*([^*\n]+)\*\*/g, "<strong>$1</strong>");
    }).join("");
    const lines = String(value).replace(/\r\n?/g, "\n").split("\n");
    const out = [];
    let list = "";
    let paragraph = [];
    let code = [];
    let fenced = false;
    const flushList = () => { if (list) { out.push(`</${list}>`); list = ""; } };
    const flushParagraph = () => { if (paragraph.length) { out.push(`<p>${paragraph.map(inline).join("<br>")}</p>`); paragraph = []; } };
    for (const line of lines) {
      if (/^\s*```/.test(line)) {
        flushParagraph(); flushList();
        if (fenced) { out.push(`<pre><code>${escapeHTML(code.join("\n"))}</code></pre>`); code = []; }
        fenced = !fenced;
        continue;
      }
      if (fenced) { code.push(line); continue; }
      if (!line.trim()) { flushParagraph(); flushList(); continue; }
      const heading = line.match(/^\s{0,3}(#{1,3})\s+(.+)$/);
      const bullet = line.match(/^\s{0,3}([-*]|\d+\.)\s+(.+)$/);
      if (heading) {
        flushParagraph(); flushList();
        const level = heading[1].length + 2;
        out.push(`<h${level}>${inline(heading[2])}</h${level}>`);
      } else if (/^\s{0,3}(?:-{3,}|\*{3,})\s*$/.test(line)) {
        flushParagraph(); flushList(); out.push("<hr>");
      } else if (bullet) {
        flushParagraph();
        const kind = bullet[1] === "-" || bullet[1] === "*" ? "ul" : "ol";
        if (list !== kind) { flushList(); out.push(`<${kind}>`); list = kind; }
        out.push(`<li>${inline(bullet[2])}</li>`);
      } else {
        flushList(); paragraph.push(line);
      }
    }
    if (fenced) out.push(`<pre><code>${escapeHTML(code.join("\n"))}</code></pre>`);
    flushParagraph(); flushList();
    return out.join("");
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

  function preferredProvider(providers, selectedID) {
    const enabled = asList(providers).filter(provider => provider.enabled);
    return enabled.find(provider => provider.id === selectedID)
      || enabled.find(provider => provider.credential_ready && provider.context_evidence === "runtime_probe" && provider.context_window >= 65536 && provider.max_output_tokens >= 8192)
      || enabled.find(provider => provider.credential_ready)
      || enabled[0] || null;
  }

  return Object.freeze({ escapeHTML, renderChatMarkdown, asList, toolArgumentsPreview, toolReceiptOf, toolOutputPreview, groupTimeline, preferredProvider });
});
