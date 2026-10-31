/* Read-only presentation of inclusion receipts, never raw retrieved bodies. */
(() => {
 "use strict";
 const escape=value=>String(value??"").replace(/[&<>"']/g,ch=>({"&":"&amp;","<":"&lt;",">":"&gt;",'"':"&quot;","'":"&#39;"}[ch]));
 function render(execution) {
  if (execution?.context_error) return `<p class="form-note" role="status">${escape(execution.context_error)}</p>`;
  const receipt=execution?.context_receipt;
  if (!receipt) return "";
  const skills=receipt.skills||[],knowledge=receipt.knowledge||[];
  return `<details class="task-context-receipt"><summary>บริบทที่ส่งให้โมเดล · ${Number(receipt.source_files)||0} ไฟล์ · ${skills.length} สกิล · ${knowledge.length} แหล่งความรู้</summary>
   <p>รายการนี้มาจากข้อมูลที่บันทึกพร้อมข้อเสนอ การมีรายการอยู่ยังไม่ยืนยันว่าช่วยให้ผลดีขึ้น</p>
   ${skills.length?`<ul>${skills.map(ref=>`<li>${escape(ref.name)}<details><summary>รุ่นที่ใช้</summary><small>${escape(ref.skill_id)} · ${escape(ref.version_id)} · ${escape(ref.content_hash)}</small></details></li>`).join("")}</ul>`:"<p>ไม่มีสกิลในข้อเสนอนี้</p>"}
   ${knowledge.length?`<ul>${knowledge.map(ref=>`<li>${escape(ref.citation)} · รุ่น ${escape(ref.version)}</li>`).join("")}</ul>`:"<p>ไม่มีข้อมูลจาก Project Brain ในข้อเสนอนี้</p>"}
   <small>หลักฐาน ${escape(receipt.input_artifact_id)} · ${escape(receipt.input_sha256)}</small></details>`;
 }
 const api=Object.freeze({render});
 if(typeof module!=="undefined"&&module.exports)module.exports=api;
 if(typeof window!=="undefined")window.HermetrixTaskContextView=api;
})();
