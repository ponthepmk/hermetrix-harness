import mermaid from "mermaid";

mermaid.initialize({startOnLoad:false, securityLevel:"strict", theme:"base", look:"classic", htmlLabels:false,
  themeVariables:{darkMode:true,fontFamily:"system-ui, sans-serif",fontSize:"14px",primaryColor:"#202b30",primaryTextColor:"#e0e7e5",primaryBorderColor:"#619b91",lineColor:"#83a59e",secondaryColor:"#273239",tertiaryColor:"#202b30"},
  flowchart:{htmlLabels:false,useMaxWidth:false,nodeSpacing:20,rankSpacing:25,padding:12}, maxTextSize:20000, maxEdges:150, suppressErrorRendering:true,
  secure:["secure","securityLevel","startOnLoad","maxTextSize","maxEdges","htmlLabels","flowchart","themeCSS"]});
let rendered = false;
window.addEventListener("message", async event => {
  if (event.source !== parent || event.data?.kind !== "hermetrix-diagram" || rendered) return;
  rendered = true;
  const source = String(event.data.source || "");
  try {
    // Diagram configuration is owned by this isolated viewer, never by model text.
    if (source.length > 20000 || /^\s*---|%%\s*\{|\b(?:click|linkStyle|classDef|style)\s/m.test(source)) throw new Error("unsupported diagram configuration");
    const {svg} = await mermaid.render("diagramGraphic", source);
    document.querySelector("#diagramRoot").innerHTML = svg;
    const graphic = document.querySelector("svg");
    graphic?.setAttribute("role","img");
    graphic?.setAttribute("aria-label","แผนภาพประกอบคำตอบ");
    parent.postMessage({kind:"hermetrix-diagram-ready",svg},"*");
  } catch {
    document.querySelector("#diagramRoot").textContent = "แสดงแผนภาพไม่ได้ เปิดโค้ดแผนภาพเพื่อดูรายละเอียด";
    parent.postMessage({kind:"hermetrix-diagram-error"},"*");
  }
});
