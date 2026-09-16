# Realtime voice — Future extension design

วันที่: 2026-09-13 · สถานะ: architecture proposal รองรับ R28 ไม่ใช่ implemented/benchmarked feature และไม่มีการเปิดไมค์หรือเรียก speech provider ในรอบนี้

เชื่อมกับ [blueprint](2026-09-13-blueprint.md), [requirements Q32–Q34](2026-09-13-requirements.md) และ [Pi Knowledge Hub](2026-09-13-knowledge-hub.md)

## 1. เป้าหมายและขอบเขต

ผู้ใช้ต้องการให้ harness พูดและสนทนา realtime ได้ในอนาคต จึงต้องเผื่อ streaming, continuous conversation, interruption และ text/voice continuity ตั้งแต่ฐาน task/UI/provider architecture ไม่ใช่เพียงปุ่มอ่านคำตอบหลัง generate เสร็จ

เป้าหมาย UX ที่เสนอ: กดเริ่มคุยใน task ปัจจุบัน เห็น mic/listening/transcribing/thinking/speaking/tool-running states, subtitle และปุ่มหยุดที่เข้าถึงได้ พูดแทรกแก้โจทย์ได้ และกลับไปพิมพ์โดยไม่สร้างประวัติหรือ task อีกชุด ภาษาไทยสลับคำอังกฤษ/ชื่อ symbol เป็น test candidate ตามบริบทผู้ใช้ แต่ language coverage ต้องยืนยัน

ไม่รวม always-on ambient recording, wake-word background daemon, voice identity authentication, voice cloning หรือเปิดไมค์รับงานอัตโนมัติไว้ใน default scope

## 2. ขอบเขต modules และ provider contracts

| Module | หน้าที่ | Boundary |
|---|---|---|
| Desktop audio driver | device selection, capture/playback, permissions, sample/codec handling, echo control เท่าที่ platform รองรับ | ไม่ตัดสิน task authority; mic ไม่เริ่มจากการเปิด project อย่างเดียว |
| Voice session service | connection/turn IDs, endpointing, sequence/epoch, cancellation, bounded buffers และ playback position | task binding คงที่; ไม่ผูกกับ selected pane ที่เปลี่ยนได้ |
| Speech adapters | streaming ASR, streaming TTS หรือ native audio conversation | capability/limits/error/usage reporting แยกต่อ provider |
| Existing task engine | committed instruction, requirements/plan, tools, checks, receipts | เสียงไม่ข้าม policy/approval หรือทำ effects อีกทางหนึ่ง |
| Knowledge bridge | allowed finalized/redacted records และ evidence lineage | ไม่ขน audio frames และไม่อยู่ใน critical path ของการพูด |

สองเส้นทางที่ต้องเผื่อ:

- **Speech chain:** streaming ASR → committed user text → selected task LLM → speech-safe output chunks → streaming TTS รองรับ text model เดิมโดยเปลี่ยน ASR/TTS แยกได้
- **Native audio:** speech-to-speech provider ที่มี turn/interruption/events ตามที่ต้องใช้ แต่ tool requests ต้องผ่าน task engine/policy/receipt เดิม ถ้า provider บังคับใช้ tool เองนอก boundary นี้ ให้ disable tool execution หรือถือว่าไม่ compatible

Gateway/text model ที่เลือกไว้ยังไม่ยืนยัน audio support; ไม่สันนิษฐานจาก chat streaming Mac จับ/เล่นเสียงได้โดยไม่บังคับ inference ในเครื่อง และ Pi เก็บ metadata/knowledge ไม่ถูกกำหนดให้รัน speech model งาน local Windows ในอนาคตต้องวัด STT/TTS + coding model ที่ใช้ทรัพยากรร่วมกัน

Capability profile ต้องระบุ input/output audio, ASR partial/final/revisions, supported languages/voices/formats, endpointing, interruption/cancel acknowledgement, playback/context reconciliation, auth/egress, retention และ usage/budget; ไม่รับรอง feature ที่ provider ไม่เปิดเผย

Transport เลือกตาม adapter และ packaged webview ที่พิสูจน์แล้ว: WebRTC เป็นมาตรฐานสำหรับ realtime media/data ส่วน media capture มี API/constraints ของตัวเอง การมี web renderer ไม่รับประกันว่า OS permissions, echo cancellation หรือ device behavior เหมือนกัน ต้อง spike ทั้ง Windows/macOS ดู [W3C WebRTC](https://www.w3.org/TR/webrtc/) และ [Media Capture and Streams](https://www.w3.org/TR/mediacapture-streams/)

Provider ที่ใช้ WebSocket/native IPC ต้องมี framing, sample format, queue bounds, backpressure และ reconnect rules ชัดเหมือนกัน; ไม่บังคับใช้ MCP หรือ SQLite writes ต่อ audio frame และไม่เพิ่ม transport ทุกแบบจนกว่าจะมี provider/use case จริง

## 3. Turn, task และ interruption semantics

Event envelope ที่เสนอ: voiceSessionID, deviceID, projectID, taskID, turnID, instructionRevision, responseEpoch, sequence, capture/playback timestamps และ correlation IDs ไปยัง task attempts บันทึก durable semantic events แยกจาก transient audio/partial text

Partial transcript แสดง/แก้ไขได้และอาจใช้ read-only preparation ที่ยกเลิกได้ แต่ไม่สร้าง authority/dispatch effects Finalized utterance จึง commit เป็น instruction ผ่าน task engine หากชื่อไฟล์/env/เป้าหมายสำคัญไม่ชัด ให้ชี้ unknown/ขอแก้หรือยืนยัน ไม่ทำ exact destructive command จากการเดา ASR

Model-native audio อาจมี tool event ก่อน transcript จบ จึงต้องมี explicit utterance commit/authorization boundary เทียบเท่า หาก provider ทำ boundary นี้ไม่ได้ ปิด effectful tools ในโหมดนั้น Transcript ที่แก้ภายหลังสร้าง revision/replan ไม่ replay effect เดิม

| การกระทำ | Behavior ที่เสนอ |
|---|---|
| พูดแทรก (barge-in) | หยุด local playback, flush queued audio, เปลี่ยน response epoch, request cancel generation/TTS; discard late old chunks |
| ปิด mic | หยุด capture/ส่ง audio ใหม่จริง ไม่เท่ากับ cancel task หรือ stop playback |
| หยุดเสียง | หยุด output ของ response ปัจจุบัน; task ที่กำลังทำแสดงสถานะต่อ |
| หยุดงานนี้ | หยุด admission ของ steps ใหม่และขอ cancel owned in-flight jobs ผ่าน task engine; แสดง acknowledged/uncertain และผลที่ย้อนคืนไม่ได้ |
| จบ voice session | หยุด capture/playback/connection; แสดงว่า task ใดยังทำงานตาม policy ไม่เริ่ม mic ใหม่เอง |

เสียงพูดแทรกไม่ใช่คำอนุญาต rollback/cancel ทุก process หรือเปลี่ยน requirement จน utterance commit ส่วนคำว่า “หยุด” ที่กำกวมเสนอให้หยุดเสียงและพัก new effect dispatch ก่อน clarify ต้องมีปุ่มหยุดงานที่ไม่พึ่ง ASR/model/network และไม่รายงาน remote work หยุดแล้วหากยังไม่ ack

ใช้ playback progress แยก generated response กับส่วนที่ device เล่นแล้ว; ถูกพูดแทรกต้องไม่ใส่ประโยคที่ยังไม่เล่นกลับไปเป็นสิ่งที่ผู้ใช้ได้รับครบ Audio offset/transcript alignment ที่ไม่แม่นให้ annotate unknown หรือเริ่ม response context ใหม่ ไม่เดาว่าผู้ใช้ได้ยิน Playback acknowledgement ก็ไม่ใช่หลักฐานว่าผู้ใช้เข้าใจ/อนุมัติ

Voice approval ต้องผูก pending operation/target/env/revision และ confirmation policy เดิม; คำรับสั้น ๆ เช่น “อืม/ได้” จาก ambient audio, partial ASR หรือ tool/media output ไม่ให้สิทธิ์ใหม่ สำหรับ production/destructive actions เสนอ UI confirmation ของ operation ที่ชัดเจนก่อน แม้ voice chat ยังเปิด

## 4. Resource budget, latency และ long tasks

Voice I/O มี event loop/queue แยกจาก run/test/render และ background learning มี explicit resource class ให้ interactive audio/turn routing ไม่ติดคิว synthesis ของบทเรียน แต่การสนทนาไม่ override safety checks หรืออ้าง preempt local GPU ได้โดยไม่มี runtime support

ใช้ bounded jitter/output buffers, queue overflow policy ที่เปิดเผย และ speech chunking ที่เหมาะกับภาษา/ข้อความ ห้ามอ่าน shell snippets, credentials หรือ raw logs ยาวออกเสียงโดยปริยาย สรุปสถานะตาม actual receipt และแนบรายละเอียดใน UI; ระหว่าง long tool job ตอบได้ว่า “กำลังรัน” ไม่ใช่ “สำเร็จ”

Context 96k ยังเป็น task budget สำหรับเส้นทาง text chain; ASR partials ไม่ append ซ้ำและ raw audio ไม่เข้า text history ส่วน native audio provider ต้องมี conversation/audio budget และ compaction rules ของตัวเอง ไม่อนุมานว่า token/duration เท่ากับ text profile ข้อมูลเสียงที่เก็บมี retention แยกจาก context budget

วัด p50/p95 แยก: speech-end detection → ASR final → queue wait → first model output → first playable audio → playback start และ barge-in detection → silence รวมทั้ง false interruption/missed endpoint, technical-term errors, dropped frames, underruns, resource contention, provider cost และ task success Latency targets รอเครื่อง/provider จริงและผู้ใช้เลือก ไม่รับประกัน sub-second จากการมี streaming API

## 5. Privacy, reconnect และ handoff

Default ที่เสนอ: mic off จนเริ่ม session, indicator และ device selection ชัด, stop capture เมื่อ permission ถูกถอน/อุปกรณ์หาย/lock ตาม policy, bounded memory-only raw audio และไม่เก็บเสียงถาวรจน opt in การไม่บันทึกเสียงไม่ได้แปลว่าไม่ส่ง cloud ต้องแสดง separate processing destination/consent ก่อนเริ่ม

ผู้ใช้เลือก text gateway ไม่ได้อนุญาต audio egress/recording โดยปริยาย หาก native-audio route ส่ง raw voice ไป provider จะ redaction ก่อนส่งได้จำกัด ต้องแจ้งขอบเขตนี้ ส่วน sensitive transcript/เสียงที่ generate ห้ามเข้า logs/Hub/learning จาก default debug telemetry

เก็บ finalized transcripts, corrections, concise decisions และ lessons เฉพาะ scope/retention ที่เลือก Voice-derived lesson แยก ASR error ออกจาก task/model error ไม่เรียนรู้จาก partial/retracted hypothesis หรือความเงียบว่าเป็นผลสำเร็จ การลบครอบ derived summaries/embeddings/backup policy ตาม lineage

Reconnect ใช้ epoch/sequence dedup ไม่ replay mic backlog หรือ tool effects หลัง timeout Provider session resume ที่ไม่มี guarantees ให้เริ่ม audio session ใหม่จาก authorized durable text/task state โดยชี้ช่วงที่ขาด ไม่แต่งบทสนทนาชดเชย

Cross-device handoff ส่ง task/checkpoint กับประวัติที่อนุญาต ไม่ส่ง active mic permission/connection/OS device handle ปลายทางต้องเริ่ม voice session เอง ขณะ offline ownership ไม่ชัด เครื่องเก่าไม่ได้มีสิทธิ์ออก effects ต่อเพียงเพราะยังได้ยินเสียง; ใช้ task/resource ownership เดิม

## 6. ลำดับส่งมอบและ acceptance

- **Foundation/P1:** modality-neutral events, voice/task identity, interruption vs task cancellation contracts, capability registry, native permission/device spike และ retention schema; ยังไม่เปิด microphone feature ให้ทำงานอัตโนมัติ
- **V0:** push-to-talk streaming speech chain + transcript/text fallback ใน task เดิม ตรวจ capture/provider/output จริงครบวงจร เป็น stepping stone ไม่เรียกว่าบรรลุ continuous realtime ทั้งหมด
- **V1:** continuous conversation ภายใน session ที่ผู้ใช้เปิด, endpointing/echo handling, barge-in, concurrent task status, backpressure/reconnect และ latency gates บนทั้งสอง OS
- **V2 (optional):** native speech-to-speech หรือ local speech workers ตาม capability/cost/privacy/quality ที่วัดได้; ไม่ต้องทำครบทุก provider เพื่อส่งมอบ V1

Mandatory acceptance สำหรับ voice release:

1. พูดโจทย์จริง → streaming response ผ่านลำโพง/หูฟัง พร้อม transcript และ task binding ถูกต้องทั้ง Windows/Mac; ไม่ผ่านเพียง mock chunks
2. พูดแทรกหลายครั้ง → เสียงเก่าหยุด/ไม่กลับมา, late ASR/TTS events ไม่ทำ wrong turn; generated-but-unplayed text ไม่แอบกลายเป็น delivered history
3. Tool กำลังรัน + interrupt/stop/change request → ไม่เกิด duplicate effects, ไม่ข้าม approvals และ cancel status ตรง receipt
4. Partial ASR, quoted commands, “อืม”, echo จากลำโพงและเสียงวิดีโอไม่กลายเป็น approval; tested mic mute หยุด outbound audio จริง
5. เปลี่ยน project/pane/device, unplug Bluetooth/USB, permission revoke, sleep/network/Pi disconnect → state ชัด ไม่ย้ายคำสั่งผิด task และไม่ auto-resume capture
6. สลับไทย/อังกฤษและศัพท์โค้ดตาม scope พร้อมกรณีไม่แน่ใจ/แก้ transcript; วัดผิดพลาดด้านความหมาย ไม่ดู transcription score อย่างเดียว
7. เล่นเสียงขณะ coding/tests/learning ใช้ทรัพยากร → วัด p50/p95 latency, interruption และ starvation ตาม threshold ที่ตกลง
8. Raw audio/transcripts ไม่ค้างใน logs/DB/Hub เมื่อไม่อนุญาต; consent/revoke/delete/retention และ cloud/local fallback ทำตาม policy ไม่เปลี่ยนปลายทางเอง

Open decisions: Q32–Q34 เรื่องภาษา/mic mode, voice providers/cloud/local, retention, latency, background conversation และ authority ยังไม่ล็อก ไม่เริ่ม provider installation/download/recording ก่อนขอบเขตชัดเจน
