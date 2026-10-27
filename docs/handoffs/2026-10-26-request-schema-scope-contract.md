# Request schema ต้องตรงกับ frozen writable scope

วันที่ 26 ตุลาคม 2026 · dependency ที่พบระหว่าง H1A · ยังไม่ apply

Bonsai Local ตอบ import repair เป็น2changesที่ใช้pathเดียวกันต่อเนื่อง2ครั้ง แม้มี writable file1ไฟล์ Validatorปฏิเสธถูกต้องและไม่มีการ salvage คำตอบที่ปฏิเสธ การบอกย้ำรูปแบบใน brief ไม่แก้ผลลัพธ์ จึงหยุดวิธีเดิมและตรวจ earliest divergence

proposalSchema ใน internal/worker/proposal.go ให้ changes minItems1แต่ไม่มีmaxItems และ path เป็นstringทั่วไป ขณะที่ parserบังคับจำนวนไม่เกินfiles,pathต้องอยู่ในfrozen scopeและไม่ซ้ำ จึงมีรูปแบบที่ schemaยอมแต่validatorไม่ยอม

การแก้ที่ต้องให้ Hermetrixเขียน: สร้าง schema จากfresh base ต่อrequest, changes.maxItemsเท่าจำนวนactual allowed paths, path.enumเฉพาะallowedpathsเรียงคงที่ ใช้ writablePathAllowedเดิม ไม่เปลี่ยนvalidator,wiretask,systemprompt,สิทธิ์หรือbudget ไม่ normalize/ยอมรับduplicatepathหลังรับคำตอบ หลายไฟล์ยังต้องถูกvalidatorตรวจduplicateตามเดิม เพราะenum+maxItemsไม่รับประกันunique pathทุกกรณี

เกณฑ์: frozen DATA matrixครอบคลุมsingle,subset/readonly,multiple,legacy,absent,Unicode,max32; exact whole requestยังตรงoriginal goldensยกเว้นschema2fieldsที่ตั้งใจเพิ่มและestimatedtokensที่เปลี่ยนตามrequestจริง ห้ามเขียนทับgoldenfixtureเดิม; เปรียบเทียบbaseconstraintsเดิมและactualsharedestimator; fresh schemaไม่แชร์mutation; duplicate/outside-scope outputsยังถูกปฏิเสธ

ทดลองmodelจริงด้วยtask import-repairต้นฉบับที่เคยfailหลังเปลี่ยนschema โดยscope/brief/model/budgetsเดิม ไม่มีsourceapplyจากคำตอบที่ถูกปฏิเสธ ก่อนเรียกต้องผ่านtestsและใช้outputใหม่ ถ้าตอบตามschemaได้เป็นหลักฐานเฉพาะกรณี ไม่ใช่proofว่าลดretryหรือtokenในงานทั่วไป ต้องเก็บusage/errorและไม่blindresample

H1A prepared-manifest models/schemaยังเป็นcandidate ไม่มีH1A Go sourceนำเข้า และcompletion/provenance/promotion/actualrunner/holdoutยังไม่ครบ รักษาR1-R10/H0-H8/M0-M10เต็ม

## Amendment — ขอบเขตที่ตรวจและนำเข้าจริง

Native review อนุมัติและ native batch apply นำเข้า 7 Go files แล้ว (7 receipts, 0 rollback) วันที่ 26 ตุลาคม 2026 ข้อความ “ยังไม่ apply” ด้านบนเป็นสถานะขณะตั้ง contract เก็บไว้เป็นประวัติ ส่วนผลตรวจหลังนำเข้าให้ดู results/receipt ล่าสุด

การเพิ่ม schema ทำให้ frozen curation case เดิมใช้ estimate 1,307 tokens เทียบ baseline 1,299 เกิน budget เดิมและทำให้ optional reference หลุด จึงเพิ่มการเปลี่ยน tool description อย่างชัดเจนเป็น `Bounded edits for review; no apply.` ผลสุดท้าย 1,297 tokens ยังคง reference เดิมและ source view 1,163 bytes ครบ โดยไม่เพิ่ม budget หรือเปลี่ยน DATA oracle

Golden expected request จึงอนุญาตความต่างเฉพาะ changes.maxItems, path.enum และ description ข้างต้น พร้อม estimate ที่คำนวณจาก request จริง Original transport fixture 3 cases และ curation DATA 8 cases ไม่แก้ไข Validator เดิมยังตรวจ duplicate/outside scope และ schema แต่ละครั้งไม่แชร์ mutable map

Live import-repair trial ยอมรับผล 1 change ตรง scope แต่โค้ดผิดไวยากรณ์ ไม่ได้ apply และไม่นับเป็น successful repair หลักฐานนี้พิสูจน์เพียง adherence ของ output shape ในกรณีที่สังเกต ยังไม่พิสูจน์การลดต้นทุนหรือคุณภาพทั่วไป
