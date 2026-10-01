(() => {
  const key = 'hermetrix.conversation.approvalMode';
  const characterKey = 'hermetrix.conversation.character';
  function read(name, fallback) { try { return localStorage.getItem(name) || fallback; } catch { return fallback; } }
  let mode = read(key, 'ask_all') === 'auto' ? 'auto' : 'ask_all';
  let character = read(characterKey, 'on') !== 'off';
  const commands = [
    ['help', 'ดูคำสั่งทั้งหมด'], ['new', 'เริ่มแชตใหม่'], ['files', 'เปิดไฟล์'],
    ['terminal', 'เปิดเทอร์มินัล'], ['attach', 'แนบไฟล์ข้อความหรือโค้ด'], ['models', 'ตั้งค่าโมเดล'], ['image', 'แนบรูปภาพ'],
    ['approve auto', 'อนุมัติเครื่องมือในแชตอัตโนมัติ'], ['approve ask', 'ถามทุกคำขออนุมัติ'],
    ['character', 'แสดงหรือซ่อนตัวละคร'],
  ];
  function matching(value) {
    if (!value.startsWith('/') || value.includes('\n')) return [];
    const query = value.slice(1).toLowerCase();
    return commands.filter(([name]) => name.startsWith(query));
  }
  function setMode(value) {
    mode = value === 'auto' ? 'auto' : 'ask_all';
    try { localStorage.setItem(key, mode); } catch {}
    for (const select of document.querySelectorAll('[data-approval-mode]')) select.value = mode;
    document.dispatchEvent(new CustomEvent('hermetrix:approval-mode'));
  }
  function bind(input, form, {run, draft}) {
    if (!input || !form || input.dataset.conversationControls) return;
    input.dataset.conversationControls = 'true';
    const tools = document.createElement('div');
    tools.className = 'conversation-controls';
    tools.innerHTML = '<label>Approve <select data-approval-mode aria-label="โหมดอนุมัติเครื่องมือในแชต"><option value="ask_all">Ask all</option><option value="auto">Auto approve</option></select></label><button type="button" data-character hidden aria-label="แสดงหรือซ่อนตัวละคร">ตัวละคร</button><button type="button" data-scroll-bottom aria-label="เลื่อนไปข้อความล่าสุด">↓</button><small>Enter ส่ง · Shift+Enter ขึ้นบรรทัดใหม่</small>';
    const menu = document.createElement('div');
    menu.className = 'slash-menu'; menu.hidden = true; menu.setAttribute('role', 'listbox'); menu.setAttribute('aria-label', 'คำสั่งแชต');
    form.insertBefore(tools, input); form.insertBefore(menu, input);
    tools.querySelector('select').value = mode;
    tools.querySelector('select').addEventListener('change', event => { setMode(event.target.value); input.focus(); });
    tools.querySelector('[data-scroll-bottom]').addEventListener('click', () => { const log = form.closest('.chat-stage, .ide-assistant')?.querySelector('.message-list, .ide-assistant-log'); if (log) log.scrollTo({top: log.scrollHeight, behavior: 'smooth'}); input.focus(); });
    tools.querySelector('[data-character]').addEventListener('click', () => {
      character = !character; try { localStorage.setItem(characterKey, character ? 'on' : 'off'); } catch {}
      for (const node of document.querySelectorAll('.conversation-character')) node.hidden = !character;
      input.focus();
    });
    let selected = 0, items = [];
    function execute(name) {
      input.value = ''; draft(''); menu.hidden = true; input.focus();
      if (name === 'approve auto') setMode('auto');
      else if (name === 'approve ask') setMode('ask_all');
      else if (name === 'character') tools.querySelector('[data-character]').click();
      else if (name === 'help') { input.value = '/'; draft('/'); update(); }
      else run(name);
    }
    function update() {
      items = matching(input.value); selected = Math.min(selected, Math.max(0, items.length - 1));
      menu.replaceChildren(); menu.hidden = !items.length;
      items.forEach(([name, label], index) => {
        const button = document.createElement('button'); button.type = 'button'; button.setAttribute('role', 'option');
        button.setAttribute('aria-selected', String(index === selected)); button.textContent = `/${name} — ${label}`;
        button.addEventListener('mousedown', event => event.preventDefault()); button.addEventListener('click', () => execute(name)); menu.append(button);
      });
    }
    input.addEventListener('input', update);
    input.addEventListener('keydown', event => {
      if (event.isComposing || event.keyCode === 229) return;
      if (!input.value.startsWith('/') || event.shiftKey || event.altKey) return;
      if (event.key === 'Escape') { event.preventDefault(); event.stopImmediatePropagation(); menu.hidden = true; return; }
      if (items.length && !menu.hidden && ['ArrowDown', 'ArrowUp'].includes(event.key)) {
        event.preventDefault(); event.stopImmediatePropagation(); selected = (selected + (event.key === 'ArrowDown' ? 1 : items.length - 1)) % items.length; update();
      } else if (event.key === 'Enter' || event.key === 'Tab' && !menu.hidden) {
        event.preventDefault(); event.stopImmediatePropagation();
        const exact = commands.find(([name]) => '/' + name === input.value.trim());
        if (exact || items.length) execute((exact || items[selected])[0]);
        else { menu.hidden = false; menu.textContent = 'ไม่พบคำสั่ง · พิมพ์ /help เพื่อดูคำสั่ง'; }
      }
    }, true);
    update();
  }
  function updateCharacter(parent, text, busy) {
    if (!parent) return;
    let node = parent.querySelector('.conversation-character');
    if (!node) {
      node = document.createElement('aside'); node.className = 'conversation-character'; node.setAttribute('aria-label', 'Hermetrix ตัวละครสนทนา');
      node.innerHTML = '<button type="button" class="character-robot" aria-label="คุยด้วยเสียงกับตัวละคร"><span class="character-antenna"></span><div class="character-face"><i></i><i></i><b></b></div></button><div class="character-bubble"></div>';
      node.querySelector('.character-robot').addEventListener('click', () => window.HermetrixVoice?.open());
      const host = parent.querySelector('.conversation-controls') || parent;
      host.append(node);
    }
    node.hidden = !character; node.classList.toggle('thinking', Boolean(busy));
    node.querySelector('.character-bubble').textContent = busy ? 'กำลังคิด…' : String(text || 'สวัสดีครับ พิมพ์คุยได้เลย หรือใช้ / เพื่อเลือกคำสั่ง').replace(/[`*#]/g, '').slice(0, 240);
  }
  window.HermetrixConversation = {bind, matching, setMode, mode: () => mode, updateCharacter};
})();
