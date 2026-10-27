(() => {
  const frames = new Map();
  const cache = new Map();
  let viewer, viewerTrigger;
  document.addEventListener("click", event => {
    const button = event.target.closest('[data-diagram-expand]');
    const graphic = button?.closest('.chat-diagram')?.querySelector('.diagram-frame img');
    if(!graphic) return;
    if(!viewer) {
      viewer=document.createElement('dialog');
      viewer.className='diagram-viewer';
      viewer.setAttribute('aria-label','แผนภาพขยาย');
      viewer.innerHTML='<header><strong>แผนภาพ</strong><button type="button" class="ghost" aria-label="ปิดแผนภาพ">×</button></header><div class="diagram-zoom"></div>';
      viewer.querySelector('button').addEventListener('click',()=>viewer.close());
      viewer.addEventListener('close',()=>viewerTrigger?.isConnected&&viewerTrigger.focus());
      document.body.append(viewer);
    }
    viewerTrigger=button;
    viewer.querySelector('.diagram-zoom').replaceChildren(graphic.cloneNode());
    viewer.showModal();
  });
  function showImage(figure, svg) {
    const image = document.createElement("img");
    image.alt = "แผนภาพประกอบคำตอบ เปิดโค้ดแผนภาพด้านล่างเพื่ออ่านโครงสร้าง";
    image.src = "data:image/svg+xml;charset=utf-8," + encodeURIComponent(svg);
    image.addEventListener("load", () => { figure.dataset.rendered = "true"; figure.querySelector('[data-diagram-expand]').disabled=false; },{once:true});
    figure.querySelector('.diagram-frame').replaceChildren(image);
  }
  function hydrate() {
    document.querySelectorAll('.chat-message:not(.streaming) .chat-diagram:not([data-mounted])').forEach(figure => {
      figure.dataset.mounted = "true";
      if(cache.has(figure.dataset.diagramSource)) { showImage(figure,cache.get(figure.dataset.diagramSource)); return; }
      const frame = document.createElement("iframe");
      frame.title = "แผนภาพประกอบคำตอบ";
      frame.setAttribute("sandbox", "allow-scripts");
      frame.setAttribute("referrerpolicy", "no-referrer");
      frame.src = "/diagram.html";
      frame.addEventListener("load", () => {
        frames.set(frame.contentWindow, {frame,figure});
        frame.contentWindow.postMessage({kind:"hermetrix-diagram",source:figure.dataset.diagramSource}, "*");
      },{once:true});
      setTimeout(()=>{
        if(figure.isConnected&&!figure.dataset.rendered) {
          figure.dataset.rendered="error";
          figure.querySelector('.diagram-frame').textContent="ยังแสดงแผนภาพไม่ได้ เปิดโค้ดแผนภาพเพื่ออ่านโครงสร้าง";
          frames.delete(frame.contentWindow);
        }
      },20000);
      figure.querySelector('.diagram-frame').replaceChildren(frame);
    });
    for(const [windowRef,{frame}] of frames) if(!frame.isConnected) frames.delete(windowRef);
  }
  window.addEventListener("message", event => {
    const mounted = frames.get(event.source);
    if (!mounted || event.origin !== "null") return;
    if (event.data?.kind === "hermetrix-diagram-ready") {
      const svg = event.data.svg;
      if(typeof svg !== "string" || svg.length>500000 || !/^<svg\s/.test(svg)) return;
      cache.set(mounted.figure.dataset.diagramSource,svg);
      if(cache.size>40) cache.delete(cache.keys().next().value);
      showImage(mounted.figure,svg);
      frames.delete(event.source);
    } else if(event.data?.kind === "hermetrix-diagram-error") mounted.figure.dataset.rendered = "error";
  });
  new MutationObserver(hydrate).observe(document.querySelector('.app-shell'), {childList:true,subtree:true});
  hydrate();
})();
