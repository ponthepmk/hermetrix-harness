(() => {
  const root = document.getElementById("hardwareMetrics");
  const updated = document.getElementById("hardwareUpdated");
  const headline = document.getElementById("hardwareHeadline");
  if (!root || !updated || !headline) return;

  const tiles = new Map();
  let timer = null;
  let loading = false;
  const formatGiB = bytes => `${(bytes / 1073741824).toFixed(1)} GB`;

  function tile(key, kind, label) {
    if (tiles.has(key)) return tiles.get(key);
    const item = document.createElement("div");
    item.className = "hardware-tile";
    item.dataset.resource = key;
    item.dataset.kind = kind;
    item.dataset.ready = "false";
    item.setAttribute("role", "meter");
    item.setAttribute("aria-valuemin", "0");
    item.setAttribute("aria-valuemax", "100");
    item.innerHTML = `<span class="hardware-ring-wrap" aria-hidden="true"><svg class="hardware-ring" viewBox="0 0 40 40"><circle class="track" cx="20" cy="20" r="16" pathLength="100"></circle><circle class="value" cx="20" cy="20" r="16" pathLength="100" stroke-dashoffset="100"></circle></svg></span><span class="hardware-copy"><strong></strong><b>—</b><small>กำลังอ่านค่า</small></span>`;
    item.querySelector("strong").textContent = label;
    item.setAttribute("aria-label", `${label}: กำลังอ่านค่า`);
    root.append(item);
    tiles.set(key, item);
    return item;
  }

  function setTile(item, label, percent, detail, description = "") {
    item.querySelector("strong").textContent = label;
    const available = Number.isFinite(percent);
    item.dataset.ready = String(available);
    item.querySelector("b").textContent = available ? `${Math.round(percent)}%` : "—";
    item.querySelector("small").textContent = detail;
    item.title = description || `${label}: ${available ? `${Math.round(percent)}%` : detail}${available && detail ? ` · ${detail}` : ""}`;
    item.setAttribute("aria-label", item.title);
    if (available) item.setAttribute("aria-valuenow", String(Math.round(percent)));
    else item.removeAttribute("aria-valuenow");
    item.querySelector(".value").setAttribute("stroke-dashoffset", available ? String(100 - Math.min(100, Math.max(0, percent))) : "100");
  }

  function render(data) {
    document.getElementById("hardwareMonitor").dataset.status = "ready";
    setTile(tile("cpu", "cpu", "CPU"), "CPU", data.cpu_percent, data.cpu_percent === null ? "ไม่พร้อม" : "ใช้งานรวม");
    const ram = data.ram_total_bytes > 0 ? data.ram_used_bytes / data.ram_total_bytes * 100 : null;
    setTile(tile("ram", "ram", "RAM"), "RAM", ram, ram === null ? "ไม่พร้อม" : `${formatGiB(data.ram_used_bytes)} / ${formatGiB(data.ram_total_bytes)}`);
    headline.textContent = `CPU ${Number.isFinite(data.cpu_percent) ? `${Math.round(data.cpu_percent)}%` : "—"} · RAM ${Number.isFinite(ram) ? `${Math.round(ram)}%` : "—"}`;
    const gpus = Array.isArray(data.gpus) ? data.gpus : [];
    const present = new Set();
    for (const gpu of gpus) {
      const index = Number(gpu.index);
      if (!Number.isInteger(index) || index < 0) continue;
      present.add(index);
      const gpuLabel = gpus.length > 1 ? `GPU ${index}` : "GPU";
      const vramLabel = gpus.length > 1 ? `VRAM ${index}` : "VRAM";
      setTile(tile(`gpu-${index}`, "gpu", gpuLabel), gpuLabel, gpu.utilization_percent, gpu.name || "การ์ดจอ", `${gpuLabel}: ${gpu.utilization_percent === null ? "ไม่พร้อม" : `${Math.round(gpu.utilization_percent)}%`} · ${gpu.name || "การ์ดจอ"}`);
      const vram = gpu.vram_total_bytes > 0 ? gpu.vram_used_bytes / gpu.vram_total_bytes * 100 : null;
      setTile(tile(`vram-${index}`, "vram", vramLabel), vramLabel, vram, vram === null ? "ไม่พร้อม" : `${formatGiB(gpu.vram_used_bytes)} / ${formatGiB(gpu.vram_total_bytes)}`, `${vramLabel}: ${vram === null ? "ไม่พร้อม" : `${Math.round(vram)}%`} · ${formatGiB(gpu.vram_used_bytes)} / ${formatGiB(gpu.vram_total_bytes)} · ${gpu.name || "การ์ดจอ"}`);
    }
    if (!gpus.length) {
      setTile(tile("gpu-0", "gpu", "GPU"), "GPU", null, "ไม่พบข้อมูล GPU");
      setTile(tile("vram-0", "vram", "VRAM"), "VRAM", null, "ไม่พบข้อมูล VRAM");
    }
    for (const [key, item] of tiles) {
      const match = /^(gpu|vram)-(\d+)$/.exec(key);
      if (match && gpus.length && !present.has(Number(match[2]))) {
        item.remove();
        tiles.delete(key);
      }
    }
    updated.textContent = `อัปเดต ${new Date(data.captured_at).toLocaleTimeString("th-TH")}`;
  }

  async function refresh() {
    clearTimeout(timer);
    if (loading || document.hidden || document.getElementById("appShell")?.hidden) {
      timer = setTimeout(refresh, 3000);
      return;
    }
    loading = true;
    try {
      const response = await fetch("/api/system/metrics", { cache: "no-store" });
      if (!response.ok) throw new Error(`Metrics ${response.status}`);
      render(await response.json());
    } catch {
      document.getElementById("hardwareMonitor").dataset.status = "unavailable";
      for (const [key, item] of tiles) setTile(item, item.querySelector("strong").textContent, null, "ข้อมูลไม่พร้อม");
      if (!tiles.size) {
        for (const [key, kind, label] of [["cpu", "cpu", "CPU"], ["ram", "ram", "RAM"], ["gpu-0", "gpu", "GPU"], ["vram-0", "vram", "VRAM"]]) {
          setTile(tile(key, kind, label), label, null, "ข้อมูลไม่พร้อม");
        }
      }
      updated.textContent = "ข้อมูลเครื่องไม่พร้อม";
      headline.textContent = "ข้อมูลเครื่องไม่พร้อม";
    } finally {
      loading = false;
      timer = setTimeout(refresh, 3000);
    }
  }

  for (const [key, kind, label] of [["cpu", "cpu", "CPU"], ["ram", "ram", "RAM"], ["gpu-0", "gpu", "GPU"], ["vram-0", "vram", "VRAM"]]) tile(key, kind, label);
  document.addEventListener("visibilitychange", () => { if (!document.hidden) refresh(); });
  refresh();
})();
