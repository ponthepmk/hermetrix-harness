/* Local voice uses the same committed text turn as the ordinary composer.
   Microphone frames and unconfirmed ASR hypotheses never enter task history. */
(() => {
  const panel = document.querySelector("#voicePanel");
  if (!panel) return;
  const el = selector => panel.querySelector(selector);
  const voice = {
    projectID: null, sessionID: null, stream: null, context: null, analyser: null,
    monitor: null, recorder: null, chunks: [], recordingStarted: 0, lastLoud: 0,
    liveSocket: null, liveNode: null, liveSink: null, liveSeq: 0,
    loudFrames: 0, utterance: 0, asrAbort: null, turnAbort: null, autoTimer: null,
    outputEpoch: 0, outputAbort: null, sound: null, soundURL: null, queue: [],
    speaking: false, spokenBuffer: "", sentUtterance: -1, sentTurns: new Set(), pendingFinals: [], voiceSessionID: "", playbackResolve: null, capabilities: null,
    nativeAbort: null, nativeActive: false, nativeRequested: false, nativeDevices: [], micRequestEpoch: 0, nativeFrames: 0,
    nativeIgnoreUtterance: false, nativeSuppressUntil: 0, draftFinal: null, focusReturn: null
  };

  function status(message, tone = "") {
    const target = el("#voiceStatus");
    target.textContent = message;
    target.dataset.tone = tone;
    panel.dataset.tone = tone;
    panel.dataset.voiceState = tone === "error" ? "error" : voice.sound || message.startsWith("กำลังพูดตอบ") ? "speaking" :
      tone === "listening" || message.startsWith("ฟังอยู่") ? "listening" : voice.stream || voice.nativeActive ? "processing" : "idle";
  }

  function setVoiceLevel(level) {
    const strength = Math.min(1, Math.max(0, Number(level) * 20));
    el("#voiceLevel").value = Math.round(strength * 100);
    panel.style.setProperty("--voice-ring-one", (1 + strength * .2).toFixed(3));
    panel.style.setProperty("--voice-ring-two", (1 + strength * .35).toFixed(3));
    panel.style.setProperty("--voice-ring-opacity", (.38 + strength * .55).toFixed(3));
    panel.style.setProperty("--voice-core-scale", (1 + strength * .07).toFixed(3));
  }

  function updateControls() {
    el("#voiceMic").textContent = voice.stream || voice.nativeActive ? "ปิดไมค์" : "เปิดไมค์";
    el("#voiceMic").disabled = !voice.capabilities?.asr_ready;
    el("#voiceNativeMic").textContent = voice.nativeActive ? "ปิดไมค์ Windows" : "ใช้ไมค์ Windows";
    el("#voiceSend").disabled = !el("#voiceTranscript").value.trim() || state.sending || !turns.ready();
    el("#voiceAutoSend").disabled = !state.sessionDetail?.session;
    const orb = el("#voiceOrb");
    orb.disabled = !voice.capabilities?.asr_ready;
    orb.setAttribute("aria-pressed", String(Boolean(voice.stream || voice.nativeActive)));
    orb.setAttribute("aria-label", voice.stream || voice.nativeActive ? "ปิดไมโครโฟน" : "เปิดไมโครโฟน");
    el("#voiceInspectStop").hidden = !turns.snapshot().blocked;
    el("#voiceInspectStop").disabled = turns.snapshot().pending;
    el("#voiceQueueCount").textContent = voice.pendingFinals.length ? `รอส่ง ${voice.pendingFinals.length} ข้อความ` : "";
  }

  const turns = window.HermetrixVoiceTurnControl.create({
    stop: (sessionID, turnID) => api(`/api/sessions/${encodeURIComponent(sessionID)}/stop`, {method:"POST",body:JSON.stringify({turn_id:turnID})}),
    read: sessionID => api(`/api/sessions/${encodeURIComponent(sessionID)}`),
    reconcile: (sessionID, turnID) => api(`/api/sessions/${encodeURIComponent(sessionID)}/stop/reconcile`, {method:"POST",body:JSON.stringify({turn_id:turnID})}),
    onState(phase) {
      if (panel.hidden) return;
      if (["stopping","waiting_for_commit"].includes(phase)) status("กำลังหยุดคำตอบเดิม · พูดต่อได้");
      else if (phase === "needs_review") status("ยังยืนยันการหยุดไม่ได้ · ตรวจแชตก่อนทำต่อ", "error");
      else if (phase === "idle" && voice.pendingFinals.length) scheduleAutoSend();
      updateControls();
    },
  });

  function interruptAnswer() {
    stopOutput();
    void turns.interrupt().then(() => {
      if (turns.ready() && voice.pendingFinals.length) scheduleAutoSend();
      updateControls();
    });
  }

  function clearAutoSend() {
    if (voice.autoTimer) clearTimeout(voice.autoTimer);
    voice.autoTimer = null;
  }

  function scheduleAutoSend(delay = 0) {
    clearAutoSend();
    voice.autoTimer = setTimeout(async () => {
      voice.autoTimer = null;
      if (panel.hidden || !el("#voiceAutoSend").checked || !voice.pendingFinals.length) return;
      if (turns.snapshot().blocked) { updateControls(); return; }
      if (state.sending || !turns.ready()) {
        status("ได้ยินแล้ว · รอยืนยันคำตอบเดิมก่อนส่งข้อความถัดไป");
        scheduleAutoSend(400);
        return;
      }
      const session = state.sessionDetail?.session;
      if (session?.state !== "active" || session.active_turn_id) {
        status(session?.state === "awaiting_approval" ? "มีคำสั่งรอให้ยืนยันในแชต · เก็บคำพูดถัดไปไว้แล้ว" : "รอตรวจสถานะแชต · เก็บคำพูดถัดไปไว้แล้ว");
        scheduleAutoSend(1000);
        return;
      }
      const next = voice.pendingFinals.shift();
      await sendTranscript(next);
      if (voice.pendingFinals.length) scheduleAutoSend();
    }, delay);
  }

  function stopOutput() {
    voice.outputEpoch++;
    voice.outputAbort?.abort();
    voice.outputAbort = null;
    voice.playbackResolve?.();
    voice.playbackResolve = null;
    if (voice.sound) {
      voice.sound.pause();
      voice.sound.currentTime = 0;
      voice.sound = null;
    }
    if (voice.soundURL) URL.revokeObjectURL(voice.soundURL);
    voice.soundURL = null;
    voice.queue = [];
    voice.spokenBuffer = "";
    voice.speaking = false;
  }

  function stopMic(finalize = false) {
    if (voice.liveNode) { voice.liveNode.port.onmessage = null; voice.liveNode.disconnect(); voice.liveNode = null; }
    voice.liveSink?.disconnect();
    voice.liveSink = null;
    voice.liveSocket?.close();
    voice.liveSocket = null;
    if (voice.monitor) clearInterval(voice.monitor);
    voice.monitor = null;
    if (voice.recorder && voice.recorder.state !== "inactive") {
      if (finalize) finishRecording();
      else {
        voice.recorder.onstop = null;
        voice.recorder.stop();
        voice.recorder = null;
        voice.chunks = [];
      }
    }
    voice.stream?.getTracks().forEach(track => track.stop());
    voice.stream = null;
    void voice.context?.close();
    voice.context = null;
    voice.analyser = null;
    setVoiceLevel(0);
    updateControls();
    if (!finalize) status("ไมค์ปิดแล้ว งานที่ส่งไปยังคงแสดงสถานะในแชต");
  }

  function end() {
    clearAutoSend();
    const unsent = voice.pendingFinals.filter(item => !voice.sentTurns.has(item.voiceTurnID)).map(item => item.text);
    const draft = el("#voiceTranscript").value.trim();
    const draftKey = voice.draftFinal?.voiceTurnID || `${voice.voiceSessionID}:${voice.utterance}`;
    if (draft && !voice.sentTurns.has(draftKey) && !unsent.includes(draft)) unsent.push(draft);
    if (!panel.hidden && voice.sessionID && unsent.length) {
      const selected = voice.sessionID === state.sessionDetail?.session?.id;
      if (selected) captureComposer();
      const previous = selected ? {text:state.draftMessage,attachments:state.composerAttachments} : sessionDrafts.get(voice.sessionID) || {text:readSessionDraft(voice.sessionID)};
      setComposerDraft([previous.text,...unsent].filter(Boolean).join("\n\n"),previous.attachments || [],voice.sessionID);
      toast("เก็บคำพูดที่ยังไม่ส่งไว้ในร่างแชตแล้ว");
    }
    voice.pendingFinals = [];
    voice.utterance++;
    voice.micRequestEpoch++;
    voice.nativeRequested = false;
    voice.nativeAbort?.abort();
    voice.nativeAbort = null;
    voice.nativeActive = false;
    voice.nativeIgnoreUtterance = false;
    voice.asrAbort?.abort();
    turns.reset();
    stopMic(false);
    stopOutput();
    panel.hidden = true;
    document.body.append(panel);
    document.body.classList.remove("voice-active");
    voice.projectID = null;
    voice.sessionID = null;
    setVoiceLevel(0);
    const opener = voice.focusReturn?.isConnected && voice.focusReturn !== document.body && voice.focusReturn.getClientRects().length ? voice.focusReturn :
      [...document.querySelectorAll('[data-voice-open]')].find(button => button.getClientRects().length);
    (opener || document.querySelector('#chatInput'))?.focus();
  }

  function recordingType() {
    if (!window.MediaRecorder) return "";
    return ["audio/webm;codecs=opus", "audio/webm", "audio/mp4"]
      .find(type => MediaRecorder.isTypeSupported(type)) || "";
  }

  function startRecording() {
    if (!voice.stream || voice.recorder) return;
    const type = recordingType();
    if (!type) { status("เบราว์เซอร์นี้บันทึกเสียงจากไมค์ไม่ได้", "error"); stopMic(false); return; }
    if (!voice.pendingFinals.length) clearAutoSend();
    voice.asrAbort?.abort();
    interruptAnswer();
    voice.utterance++;
    const thisUtterance = voice.utterance;
    voice.chunks = [];
    voice.recordingStarted = Date.now();
    voice.lastLoud = Date.now();
    const recorder = new MediaRecorder(voice.stream, { mimeType: type });
    voice.recorder = recorder;
    recorder.ondataavailable = event => { if (event.data.size) voice.chunks.push(event.data); };
    recorder.onerror = () => { voice.recorder = null; status("ไมค์บันทึกเสียงไม่สำเร็จ ลองเปิดใหม่", "error"); };
    recorder.onstop = () => {
      voice.recorder = null;
      const blob = new Blob(voice.chunks, { type: type.split(";")[0] });
      voice.chunks = [];
      if (blob.size >= 100 && thisUtterance === voice.utterance) void transcribe(blob, thisUtterance);
    };
    recorder.start();
    status("กำลังฟัง… หยุดพูดสักครู่เพื่อถอดเสียง", "listening");
  }

  function finishRecording() {
    if (!voice.recorder || voice.recorder.state === "inactive") return;
    voice.recorder.stop();
    status("กำลังถอดคำพูดด้วย Whisper ในเครื่อง…");
  }

  function monitorMic() {
    if (!voice.analyser) return;
    const samples = new Float32Array(voice.analyser.fftSize);
    voice.analyser.getFloatTimeDomainData(samples);
    let power = 0;
    for (const value of samples) power += value * value;
    const level = Math.sqrt(power / samples.length);
    setVoiceLevel(level);
    const loud = level > (voice.speaking ? .035 : .012);
    if (loud) {
      voice.loudFrames++;
      voice.lastLoud = Date.now();
      if (!voice.recorder && voice.loudFrames >= 2) startRecording();
    } else voice.loudFrames = 0;
    if (voice.recorder && (Date.now() - voice.lastLoud > 950 && Date.now() - voice.recordingStarted > 500 || Date.now() - voice.recordingStarted > 18000)) finishRecording();
  }

  async function startMic() {
    if (voice.nativeActive) { voice.nativeAbort?.abort(); voice.nativeActive = false; updateControls(); status("ไมค์ Windows ปิดแล้ว"); return; }
    if (voice.stream) { stopMic(true); return; }
    voice.nativeRequested = false;
    const requestEpoch = ++voice.micRequestEpoch;
    if (!voice.capabilities?.asr_ready) { status(voice.capabilities?.reason || "Whisper ยังไม่พร้อม", "error"); return; }
    if (!navigator.mediaDevices?.getUserMedia || !window.MediaRecorder) { status("เบราว์เซอร์นี้ใช้ไมค์ไม่ได้ กรุณาเปิด Preview ในเบราว์เซอร์ที่รองรับ", "error"); return; }
    status("กำลังรอเบราว์เซอร์อนุญาตไมค์ให้ Hermetrix…");
    const hint = setTimeout(() => status(voice.nativeDevices.length ?
      "ยังรอสิทธิ์เบราว์เซอร์อยู่ เลือก ‘ใช้ไมค์ Windows’ ด้านล่างเพื่อพูดใน Preview ได้เลย" :
      "ยังรอสิทธิ์ไมค์อยู่ หากไม่เห็นคำขออนุญาต ให้เปิด Preview ใน Chrome แล้วลองอีกครั้ง", "error"), 4000);
    try {
      const stream = await navigator.mediaDevices.getUserMedia({ audio: { echoCancellation: true, noiseSuppression: true, autoGainControl: true } });
      if (panel.hidden || voice.nativeRequested || voice.nativeActive || requestEpoch !== voice.micRequestEpoch) { stream.getTracks().forEach(track => track.stop()); return; }
      voice.stream = stream;
      localStorage.setItem("hermetrix.voice.input", "browser");
      stream.getAudioTracks().forEach(track => { track.onended = () => { if (voice.stream) stopMic(false); }; });
      const AudioContextType = window.AudioContext || window.webkitAudioContext;
      voice.context = new AudioContextType();
      const source = voice.context.createMediaStreamSource(stream);
      voice.analyser = voice.context.createAnalyser();
      voice.analyser.fftSize = 2048;
      source.connect(voice.analyser);
      try {
        await startLiveCapture(source);
        voice.monitor = setInterval(() => {
          if (!voice.analyser) return;
          const samples = new Float32Array(voice.analyser.fftSize);
          voice.analyser.getFloatTimeDomainData(samples);
          let power = 0;
          for (const value of samples) power += value * value;
          setVoiceLevel(Math.sqrt(power / samples.length));
        }, 80);
        status("ไมค์เปิดแล้ว พูดได้เลย · แสดงคำพูดระหว่างพูด · เสียงอยู่บนเครื่องนี้", "listening");
      } catch {
        voice.monitor = setInterval(monitorMic, 80);
        status("ไมค์เปิดแล้ว · โหมดถอดเสียงหลังจบประโยค (เสียงสดยังไม่พร้อม)", "listening");
      }
      updateControls();
    } catch (error) {
      stopMic(false);
      status(error.name === "NotAllowedError" ? "ยังไม่ได้อนุญาตไมค์ให้หน้านี้" : `เปิดไมค์ไม่ได้: ${error.message}`, "error");
    } finally { clearTimeout(hint); }
  }

  async function startLiveCapture(source) {
    if (!voice.context.audioWorklet || !window.WebSocket) throw new Error("live audio unavailable");
    const scheme = location.protocol === "https:" ? "wss:" : "ws:";
    const socket = new WebSocket(`${scheme}//${location.host}/api/voice/live`);
    await new Promise((resolve, reject) => {
      const timeout = setTimeout(() => reject(new Error("voice socket timeout")), 3000);
      socket.onopen = () => { clearTimeout(timeout); resolve(); };
      socket.onerror = () => { clearTimeout(timeout); reject(new Error("voice socket failed")); };
    }).catch(error => { socket.close(); throw error; });
    try {
      await voice.context.audioWorklet.addModule("/voice-worklet.js");
      const node = new AudioWorkletNode(voice.context, "hermetrix-voice-capture");
      const sink = voice.context.createGain();
      sink.gain.value = 0;
      voice.liveSeq = 0;
      node.port.onmessage = event => {
        if (socket.readyState !== WebSocket.OPEN || socket.bufferedAmount > 128000) return;
        const pcm = event.data;
        if (!(pcm instanceof ArrayBuffer) || pcm.byteLength !== 1280) return;
        const packet = new ArrayBuffer(1288);
        const view = new DataView(packet);
        view.setUint32(0, voice.liveSeq, true);
        view.setUint32(4, voice.liveSeq * 40 + 40, true);
        new Uint8Array(packet, 8).set(new Uint8Array(pcm));
        voice.liveSeq++;
        socket.send(packet);
      };
      socket.onmessage = event => {
        try { onNativeEvent(JSON.parse(event.data)); }
        catch { status("รับผลถอดเสียงสดไม่ได้", "error"); }
      };
      socket.onclose = () => {
        if (voice.liveSocket === socket && voice.stream) {
          stopMic(false);
          status("การเชื่อมต่อเสียงสดหยุดลง เปิดไมค์ใหม่หรือพิมพ์ข้อความได้", "error");
        }
      };
      source.connect(node);
      node.connect(sink);
      sink.connect(voice.context.destination);
      voice.liveSocket = socket;
      voice.liveNode = node;
      voice.liveSink = sink;
    } catch (error) { socket.close(); throw error; }
  }

  async function transcribe(blob, utterance) {
    const controller = new AbortController();
    voice.asrAbort = controller;
    try {
      const response = await fetch("/api/voice/transcribe", { method: "POST", headers: { "Content-Type": blob.type }, body: blob, signal: controller.signal });
      const result = await response.json();
      if (!response.ok) throw new Error(result.error || "ถอดเสียงไม่ได้");
      if (utterance !== voice.utterance || panel.hidden) return;
      acceptTranscript((result.text || "").trim());
    } catch (error) {
      if (error.name !== "AbortError" && utterance === voice.utterance) status(error.message, "error");
    } finally {
      if (voice.asrAbort === controller) voice.asrAbort = null;
    }
  }

  function acceptTranscript(transcript, voiceTurnID = "") {
    if (!transcript || panel.hidden) { status("ไม่พบคำพูด ลองพูดอีกครั้ง", "error"); return; }
    if (/^\s*[\[(](?:[^\])]*(?:music|applause|laughter|noise|silence|ดนตรี)[^\])]*)[\])]\s*$/i.test(transcript)) {
      status("ได้ยินเสียงพื้นหลัง ไม่มีข้อความที่จะส่ง");
      return;
    }
    const key = voiceTurnID || `${voice.voiceSessionID}:${voice.utterance}`;
    if (voice.sentTurns.has(key) || voice.pendingFinals.some(item => item.voiceTurnID === key)) return;
    el("#voiceTranscript").value = transcript;
    const final = {text:transcript, utterance:voice.utterance, sessionID:voice.sessionID, voiceTurnID:key};
    voice.draftFinal = final;
    updateControls();
    if (!state.sessionDetail?.session) {
      state.startDraft = transcript;
      const input = document.querySelector("#startPrompt");
      if (input) input.value = transcript;
      status("ถอดเสียงแล้ว · ตั้งค่าโมเดลที่ตอบได้ก่อนเริ่มแชต", "error");
    } else if (el("#voiceAutoSend").checked) {
      status("ถอดเสียงแล้ว · กำลังส่งข้อความ");
      if (voice.pendingFinals.length >= 16) {
        stopMic(false);
        status("คิวเสียงเต็ม · ข้อความล่าสุดอยู่ในช่องด้านล่าง กดส่งเมื่อคิวว่าง", "error");
        updateControls();
        return;
      }
      voice.pendingFinals.push(final);
      updateControls();
      scheduleAutoSend();
    } else status("ถอดเสียงแล้ว ตรวจข้อความและกดส่งเมื่อพร้อม");
  }

  async function loadNativeDevices() {
    try {
      const response = await fetch("/api/voice/devices", { cache: "no-store" });
      if (!response.ok) return;
      const data = await response.json();
      voice.nativeDevices = Array.isArray(data.devices) ? data.devices.filter(name => typeof name === "string") : [];
      const select = el("#voiceDevice");
      select.replaceChildren(...voice.nativeDevices.map(name => new Option(name, name)));
      el("#voiceNativeRow").hidden = !voice.nativeDevices.length;
      el("#voiceInputSettings").hidden = !voice.nativeDevices.length;
    } catch { /* Browser microphone remains available when device discovery fails. */ }
  }

  function onNativeEvent(item) {
    if (item.type === "capture_started") {
      if (item.voice_session_id) voice.voiceSessionID = item.voice_session_id;
      if (voice.nativeDevices.includes(item.device)) localStorage.setItem("hermetrix.voice.device", item.device);
      status("ไมค์ Windows เปิดแล้ว กำลังรอสัญญาณเสียง…", "listening");
    } else if (item.type === "level") {
      setVoiceLevel(item.rms || 0);
      if (++voice.nativeFrames === 1) status("ไมค์ Windows รับสัญญาณแล้ว พูดได้เลย", "listening");
    }
    else if (item.type === "speech_start") {
      // Windows capture has no browser echo cancellation. Do not turn the
      // speaker's own reply into a new user message while it is playing.
      if (voice.nativeActive && (voice.sound || Date.now() < voice.nativeSuppressUntil)) {
        voice.nativeIgnoreUtterance = true;
        status("กำลังพูดตอบ · กดหยุดเสียงตอบหากต้องการพูดแทรก");
        return;
      }
      voice.nativeIgnoreUtterance = false;
      if (!voice.pendingFinals.length) clearAutoSend();
      interruptAnswer();
      voice.utterance++;
      voice.sentUtterance = -1;
      status("กำลังฟังคำพูด…", "listening");
    } else if (item.type === "partial") {
      if (!voice.nativeIgnoreUtterance && !panel.hidden && item.text) {
        el("#voiceTranscript").value = item.text;
        status("คำพูดระหว่างพูด · อาจเปลี่ยนเมื่อจบประโยค", "listening");
        updateControls();
      }
    } else if (item.type === "transcribing") { if (!voice.nativeIgnoreUtterance) status("กำลังถอดคำพูดด้วย Whisper ในเครื่อง…"); }
    else if (item.type === "final") {
      if (voice.nativeIgnoreUtterance) voice.nativeIgnoreUtterance = false;
      else acceptTranscript((item.text || "").trim(), item.voice_turn_id || "");
    }
    else if (item.type === "no_speech") {
      if (voice.nativeIgnoreUtterance) voice.nativeIgnoreUtterance = false;
      else status("ไม่พบคำพูด ลองพูดอีกครั้ง", "error");
    }
    else if (item.type === "transcribe_error") status(item.message || "ถอดคำพูดไม่สำเร็จ", "error");
    else if (item.type === "capture_error") throw new Error(item.message || "ไมค์ Windows หยุดทำงาน");
  }

  async function startNativeMic() {
    if (voice.nativeActive) {
      voice.nativeAbort?.abort();
      voice.nativeActive = false;
      updateControls();
      status("ไมค์ Windows ปิดแล้ว งานที่ส่งไปยังคงแสดงสถานะในแชต");
      return;
    }
    const device = el("#voiceDevice").value;
    if (!voice.nativeDevices.includes(device)) { status("เลือกไมค์ Windows ก่อน", "error"); return; }
    voice.nativeRequested = true;
    localStorage.setItem("hermetrix.voice.input", "windows");
    localStorage.setItem("hermetrix.voice.device", device);
    voice.micRequestEpoch++;
    voice.asrAbort?.abort();
    if (voice.stream) stopMic(false);
    const controller = new AbortController();
    voice.nativeAbort = controller;
    voice.nativeActive = true;
    voice.nativeFrames = 0;
    updateControls();
    status("กำลังเปิดไมค์ Windows…");
    try {
      const response = await fetch("/api/voice/native/stream", { method: "POST", headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ device }), signal: controller.signal });
      if (!response.ok) throw new Error((await response.json().catch(() => ({}))).error || "เปิดไมค์ Windows ไม่ได้");
      if (!response.body) throw new Error("เบราว์เซอร์รับสถานะไมค์สดไม่ได้");
      const reader = response.body.getReader();
      const decoder = new TextDecoder();
      let buffer = "";
      while (true) {
        const { value, done } = await reader.read();
        buffer += decoder.decode(value || new Uint8Array(), { stream: !done });
        const lines = buffer.split("\n");
        buffer = lines.pop() || "";
        for (const line of lines) if (line.trim()) onNativeEvent(JSON.parse(line));
        if (done) break;
      }
      if (!controller.signal.aborted) status("ไมค์ Windows หยุดส่งเสียงแล้ว", "error");
    } catch (error) {
      if (error.name !== "AbortError" && !panel.hidden) status(error.message, "error");
    } finally {
      if (voice.nativeAbort === controller) voice.nativeAbort = null;
      voice.nativeActive = false;
      setVoiceLevel(0);
      updateControls();
    }
  }

  function cleanSpeech(value) {
    return value.replace(/```[\s\S]*?(?:```|$)/g, "").replace(/`[^`]*(?:`|$)/g, "")
      .replace(/https?:\/\/\S+/g, "").replace(/\[[^\]]+\]\([^)]*\)/g, "")
      .replace(/[#*_>|]/g, " ").replace(/\s+/g, " ");
  }

  function queueSpeech(text) {
    const safe = cleanSpeech(text).trim();
    if (!safe || !voice.capabilities?.tts_ready || !el("#voiceReply").checked) return;
    voice.queue.push(safe.slice(0, 500));
    void playSpeech();
  }

  async function playSpeech() {
    if (voice.speaking || !voice.queue.length || panel.hidden) return;
    voice.speaking = true;
    const epoch = voice.outputEpoch;
    try {
      while (voice.queue.length && epoch === voice.outputEpoch && !panel.hidden) {
        const text = voice.queue.shift();
        const controller = new AbortController();
        voice.outputAbort = controller;
        const response = await fetch("/api/voice/speak", { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify({ text }), signal: controller.signal });
        if (!response.ok) throw new Error((await response.json().catch(() => ({}))).error || "พูดตอบไม่ได้");
        const blob = await response.blob();
        if (epoch !== voice.outputEpoch) break;
        voice.soundURL = URL.createObjectURL(blob);
        const sound = new Audio(voice.soundURL);
        voice.sound = sound;
        status(voice.nativeActive ? "กำลังพูดตอบ · กดหยุดเสียงตอบหากต้องการพูดแทรก" : "กำลังพูดตอบ · พูดแทรกหรือกดหยุดเสียงได้");
        await new Promise((resolve, reject) => {
          voice.playbackResolve = resolve;
          sound.onended = resolve;
          sound.onerror = () => reject(new Error("เล่นเสียงตอบไม่ได้"));
          sound.play().catch(reject);
        });
        voice.playbackResolve = null;
        if (voice.nativeActive) voice.nativeSuppressUntil = Date.now() + 500;
        if (voice.soundURL) URL.revokeObjectURL(voice.soundURL);
        voice.soundURL = null;
        voice.sound = null;
      }
      if (epoch === voice.outputEpoch) status(voice.stream || voice.nativeActive ? "ฟังอยู่ · พูดต่อได้" : "เล่นเสียงตอบเสร็จแล้ว", voice.stream || voice.nativeActive ? "listening" : "");
    } catch (error) {
      if (error.name !== "AbortError" && epoch === voice.outputEpoch) status(error.message, "error");
    } finally {
      if (epoch === voice.outputEpoch) { voice.speaking = false; voice.outputAbort = null; }
    }
  }

  function onTurnEvent(event) {
    if (event.type === "delta" && event.delta?.content) {
      voice.spokenBuffer += event.delta.content;
      const boundary = voice.spokenBuffer.search(/[.!?。！？\n]/);
      if (boundary >= 12 || voice.spokenBuffer.length > 150) {
        const cut = boundary >= 12 ? boundary + 1 : voice.spokenBuffer.lastIndexOf(" ", 150);
        const count = cut > 20 ? cut : Math.min(150, voice.spokenBuffer.length);
        queueSpeech(voice.spokenBuffer.slice(0, count));
        voice.spokenBuffer = voice.spokenBuffer.slice(count);
      }
    } else if (event.type === "approval_required") {
      queueSpeech("กรุณาตรวจและยืนยันการกระทำบนหน้าจอ");
      status("รอคุณตรวจและยืนยันการกระทำในแชต");
    } else if (event.type === "tool_call") status("กำลังทำงานตามคำขอ · ดูสถานะในแชต");
    else if (event.type === "failed") status(event.error || "งานตอบกลับไม่สำเร็จ", "error");
  }

  async function sendTranscript(final = null) {
    clearAutoSend();
    const text = (final?.text || el("#voiceTranscript").value).trim();
    if (!text || state.sending || panel.hidden || !turns.ready()) return;
    if (!state.sessionDetail?.session) {
      state.startDraft = text;
      const input = document.querySelector("#startPrompt");
      if (input) input.value = text;
      status("เลือกโมเดลและกดเริ่มคุยเพื่อส่งข้อความ", "error");
      return;
    }
    final = final || (voice.draftFinal?.text === text ? voice.draftFinal : null);
    const utterance = final?.utterance ?? voice.utterance;
    if (final?.sessionID && final.sessionID !== state.sessionDetail.session.id) return;
    const key = final?.voiceTurnID || `${voice.voiceSessionID}:${utterance}`;
    if (voice.sentTurns.has(key)) { status("ข้อความนี้ส่งแล้ว · ตรวจผลในแชตได้"); return; }
    const sessionID = state.sessionDetail.session.id;
    const ticket = turns.begin(sessionID);
    if (!ticket) return;
    voice.pendingFinals = voice.pendingFinals.filter(item => item.voiceTurnID !== key);
    voice.sentTurns.add(key);
    voice.sentUtterance = utterance;
    voice.spokenBuffer = "";
    const controller = new AbortController();
    const outputEpoch = voice.outputEpoch;
    voice.turnAbort = controller;
    status("ส่งข้อความแล้ว กำลังรับคำตอบสด…");
    updateControls();
    let ok = false;
    try {
      ok = await submitChatText(text, [], { signal: controller.signal, idempotencyKey:key, automaticRetry:false,
        onEvent: item => {
          if (voice.turnAbort !== controller || voice.sessionID !== sessionID || panel.hidden) return;
          if (item.type === "user_committed") ticket.commit(item.turn_id);
          ticket.terminal(item.type, item.turn_id);
          if (outputEpoch === voice.outputEpoch && !controller.signal.aborted) onTurnEvent(item);
        } });
    } finally { ticket.finish({failed:!ok}); }
    if (voice.turnAbort === controller) voice.turnAbort = null;
    if (outputEpoch === voice.outputEpoch && !panel.hidden) {
      if (ok && voice.spokenBuffer.trim()) queueSpeech(voice.spokenBuffer);
      voice.spokenBuffer = "";
      if (!ok && !controller.signal.aborted && !turns.snapshot().interrupted) status("ยังยืนยันผลข้อความนี้ไม่ได้ · ตรวจแชตก่อนส่งใหม่", "error");
      else if (ok && !voice.queue.length && !voice.speaking) status(voice.stream || voice.nativeActive ? "ฟังอยู่ · พูดต่อได้" : "คำตอบอยู่ในแชต", voice.stream || voice.nativeActive ? "listening" : "");
    }
    updateControls();
  }

  async function open() {
    if (!panel.hidden) { panel.focus(); return; }
    voice.focusReturn = document.activeElement;
    if (!state.sessionDetail?.session && state.sessionReady) {
      const created = await createAgentSession("คุยด้วยเสียง");
      if (!created) return;
    }
    voice.projectID = state.currentProject?.id || null;
    voice.sessionID = state.sessionDetail?.session?.id || null;
    voice.sentUtterance = -1;
    voice.sentTurns.clear();
    voice.pendingFinals = [];
    voice.draftFinal = null;
    turns.reset();
    voice.voiceSessionID = crypto.randomUUID();
    panel.hidden = false;
    document.querySelector("#voiceHost")?.append(panel);
    document.body.classList.add("voice-active");
    panel.tabIndex = -1;
    panel.focus();
    el("#voiceAutoSend").checked = Boolean(voice.sessionID);
    status("กำลังตรวจ Whisper และเสียงพูดในเครื่อง…");
    try {
      const response = await fetch("/api/voice/capabilities", { cache: "no-store" });
      voice.capabilities = await response.json();
      const session = state.sessionDetail?.session;
      const modelLabel = session?.model ? session.model.split(/[\\/]/).pop().replace(/\.gguf$/i, "") : "";
      el("#voiceModelLabel").textContent = `ฟัง: ${voice.capabilities.asr || "Whisper ในเครื่อง"} · คิด: ${session ? `${session.provider_name} / ${modelLabel}` : "เลือกโมเดลก่อน"} · พูด: ${voice.capabilities.tts || "เสียงในเครื่อง"}`;
      el("#voiceModelLabel").title = session?.model || "";
      if (!response.ok || !voice.capabilities.asr_ready) throw new Error(voice.capabilities.reason || "เสียงในเครื่องยังไม่พร้อม");
      updateControls();
      await loadNativeDevices();
      const lastDevice = localStorage.getItem("hermetrix.voice.device");
      if (localStorage.getItem("hermetrix.voice.input") === "windows" && voice.nativeDevices.includes(lastDevice)) {
        el("#voiceDevice").value = lastDevice;
        void startNativeMic();
      } else await startMic();
      if (!voice.capabilities.tts_ready && (voice.stream || voice.nativeActive)) status("รับเสียงได้ แต่เสียงพูดตอบยังไม่พร้อมในเครื่องนี้", "error");
    } catch (error) { status(error.message, "error"); updateControls(); }
  }

  function onRender() {
    document.querySelectorAll("[data-voice-open]").forEach(button => { button.onclick = open; });
    if (!panel.hidden && (voice.projectID !== (state.currentProject?.id || null) ||
      voice.sessionID !== (state.sessionDetail?.session?.id || null))) end();
    else if (!panel.hidden) {
      document.querySelector("#voiceHost")?.append(panel);
      updateControls();
    }
  }

  function beforeRender() {
    if (!panel.hidden && panel.parentElement !== document.body) document.body.append(panel);
  }

  el("#voiceEnd").addEventListener("click", end);
  el("#voiceMic").addEventListener("click", () => void startMic());
  el("#voiceOrb").addEventListener("click", () => void startMic());
  el("#voiceNativeMic").addEventListener("click", () => void startNativeMic());
  el("#voiceSend").addEventListener("click", () => void sendTranscript());
  el("#voiceTestSound").addEventListener("click", () => {
    if (!voice.capabilities?.tts_ready) { status("เสียงพูดตอบในเครื่องนี้ยังไม่พร้อม", "error"); return; }
    queueSpeech("ทดสอบเสียงตอบจาก Hermetrix ในเครื่อง");
  });
  el("#voiceStopOutput").addEventListener("click", () => {
    interruptAnswer();
    if (turns.ready()) status(voice.stream || voice.nativeActive ? "หยุดคำตอบแล้ว · ไมค์ยังเปิดอยู่" : "หยุดคำตอบแล้ว");
  });
  el("#voiceInspectStop").addEventListener("click", async () => {
    if (await turns.inspect()) {
      status("ตรวจยืนยันแล้ว · พูดต่อหรือส่งข้อความได้", voice.stream ? "listening" : "");
      if (voice.pendingFinals.length) scheduleAutoSend();
    } else {
      await selectSession(voice.sessionID, {refresh:true}).catch(() => {});
      status("ตรวจสถานะในแชตและผลเครื่องมือก่อนทำต่อ", "error");
    }
    updateControls();
  });
  el("#voiceReply").addEventListener("change", () => { if (!el("#voiceReply").checked) stopOutput(); });
  el("#voiceAutoSend").addEventListener("change", () => { if (el("#voiceAutoSend").checked && voice.pendingFinals.length) scheduleAutoSend(); else clearAutoSend(); });
  el("#voiceTranscript").addEventListener("input", () => {
    clearAutoSend();
    if (voice.draftFinal) voice.pendingFinals = voice.pendingFinals.filter(item => item !== voice.draftFinal);
    voice.utterance++;
    voice.draftFinal = null;
    voice.sentUtterance = -1;
    updateControls(); status("แก้ข้อความแล้ว · กดส่งเมื่อพร้อม");
  });
  panel.addEventListener("keydown", event => { if (event.key === "Escape") { event.preventDefault(); end(); } });
  document.addEventListener("visibilitychange", () => {
    if (document.hidden && voice.stream) stopMic(false);
    if (document.hidden && voice.nativeActive) { voice.nativeAbort?.abort(); voice.nativeActive = false; }
  });
  window.addEventListener("beforeunload", end);
  window.HermetrixVoice = { onRender, beforeRender, end, open, onCaptureEvent:onNativeEvent };
  onRender();
})();
