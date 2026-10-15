import MarkdownIt from "markdown-it";

const markdown = new MarkdownIt({html:false, linkify:true, breaks:false});
const escape = markdown.utils.escapeHtml;
markdown.renderer.rules.image = (tokens, index) => escape(tokens[index].content || "รูปภาพ");
const linkOpen = markdown.renderer.rules.link_open;
markdown.renderer.rules.link_open = (tokens, index, options, env, renderer) => {
  tokens[index].attrSet("target", "_blank");
  tokens[index].attrSet("rel", "noopener noreferrer");
  return linkOpen ? linkOpen(tokens,index,options,env,renderer) : renderer.renderToken(tokens,index,options);
};
const fence = markdown.renderer.rules.fence;
markdown.renderer.rules.fence = (tokens, index, options, env, renderer) => {
  const token = tokens[index], language = token.info.trim().split(/\s+/)[0];
  if (language === "mermaid") return `<figure class="chat-diagram" data-diagram-source="${escape(token.content)}"><figcaption><span>แผนภาพ</span><button type="button" class="ghost compact" data-diagram-expand aria-label="ขยายแผนภาพ" disabled>⛶</button></figcaption><div class="diagram-frame" role="status">กำลังเตรียมแผนภาพ…</div><details><summary>ดูโค้ดแผนภาพ</summary><pre><code>${escape(token.content)}</code></pre></details></figure>`;
  return `<div class="chat-code"><div class="chat-code-language">${escape(language || "โค้ด")}</div>${fence(tokens,index,options,env,renderer)}</div>`;
};
markdown.renderer.rules.table_open = () => '<div class="chat-table-scroll" tabindex="0" role="region" aria-label="ตาราง เลื่อนเพื่อดูทุกคอลัมน์"><table>';
markdown.renderer.rules.table_close = () => '</table></div>';
for(const name of ["th_open","td_open"]) markdown.renderer.rules[name]=(tokens,index,options,env,renderer)=>{
  const token=tokens[index],alignment=token.attrGet("style")?.match(/^text-align:(left|right|center)$/)?.[1];
  if(token.attrs) token.attrs=token.attrs.filter(([name])=>name!=="style");
  if(alignment) token.attrJoin("class",`chat-align-${alignment}`);
  return renderer.renderToken(tokens,index,options);
};
export function render(value) { return markdown.render(String(value || "")); }
