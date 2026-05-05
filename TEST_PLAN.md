# ctx-saver — Test Plan (Phase 6 validation)

Version: 1.0
Date: 2026-04-25
Tester: Self (single-user benchmark)
Target: Verify Phase 6 effectiveness before considering team rollout

---

## Test goals (priority order)

1. **Token saving** — context efficiency (ลด token consumption ได้จริงไหม)
2. **Tool adherence** — Copilot ใช้ ctx-saver แทน native tools มากขึ้นไหม (Phase 6 หลัก)
3. **Workflow performance** — งานจริงเร็วขึ้น/ช้าลง? คุณภาพดีขึ้น/แย่ลง?

## Hypothesis

> Phase 6 (aggressive descriptions + copilot-instructions.md + ctx_session_init + adherence metrics) ทำให้:
> - Copilot Enterprise adherence ≥ 80% (vs baseline ~30-50%)
> - Token consumption ต่อ task ลดลง ≥ 60%
> - Task completion time ไม่ช้าลงเกิน 10% (overhead acceptable)
> - Quality (correctness) ของ output ไม่ลด

---

## Test design

### Single-blind controlled comparison

ใช้ design นี้เพราะพี่เป็นคนทดสอบเอง — ต้องระวัง bias:

```
Group A (Control):     Phase 1-5 only (no Phase 6)
Group B (Treatment):   Phase 1-6 (full)

Each scenario run TWICE:
  1. Group A first (cold) — ตอนยังไม่รู้คำตอบ
  2. Group B second (cold-ish) — รู้แล้วว่าเฉลย แต่ workflow ใหม่

NOTE: B ทีหลัง = B ได้เปรียบเรื่อง familiarity → bias ที่จะหักออก
```

**Bias mitigation:**
- ใช้ **dummy scenarios** ที่พี่ไม่เคยทำ (ไม่มี muscle memory)
- รัน A กับ B **ใน session แยก completely** (clean DB)
- บันทึก metric **ทันที** หลัง session (ไม่ recall ทีหลัง)
- ถ้าผลใกล้เคียงกัน → assume Phase 6 ไม่มี impact (ไม่ optimistic bias)

### Two-platform matrix

ทำซ้ำกัน 2 platform เพราะ Phase 6 design มาเพื่อ Copilot โดยเฉพาะ:

| | Copilot Enterprise | Claude Code |
|---|---|---|
| Group A (no Phase 6) | A-Co | A-Cl |
| Group B (with Phase 6) | B-Co | B-Cl |

**Expected:**
- A-Co → low adherence (control = bad)
- B-Co → high adherence (Phase 6 fixes Copilot)
- A-Cl → already high (Claude doesn't need Phase 6)
- B-Cl → still high, no regression (no harm to Claude)

**Watch out:**
- ถ้า B-Cl < A-Cl → Phase 6 ทำให้ Claude แย่ลง = ต้อง revert
- ถ้า B-Co < A-Co → ทำกลับด้าน = bug

---

## Test environment

### Pre-flight checklist

ก่อนเริ่ม test ทุก session:

- [ ] Note ลง spreadsheet: date, time, scenario name, group (A/B), platform
- [ ] `ctx-saver --version` → record (ต้องเหมือนกันทุก run)
- [ ] `git rev-parse HEAD` ใน ctx-saver repo → record commit hash
- [ ] Clean DB: `rm ~/.local/share/ctx-saver/<project_hash>.db`
- [ ] Restart MCP host (Copilot/Claude Code)
- [ ] เปิด terminal แยกไว้รัน `ctx-saver stats` ระหว่าง test

### Group A setup (no Phase 6)

ต้อง simulate "ก่อน Phase 6":
- Checkout commit ก่อน Phase 6 OR
- ใช้ branch `pre-phase6` ที่ tag ไว้ OR
- Disable feature flags ใน config.yaml:
  ```yaml
  # config.yaml — Group A
  features:
    aggressive_descriptions: false
    session_init_enabled: false
    adherence_tracking: false
  copilot_instructions_md: false  # ลบ .github/copilot-instructions.md ชั่วคราว
  ```

**คำแนะนำ:** สร้าง git branch ที่ checkout ได้ง่าย:
```bash
git tag pre-phase6 <commit-before-phase6>
git tag post-phase6 main
# Switch:
git checkout pre-phase6 && make build && make install   # for Group A
git checkout post-phase6 && make build && make install  # for Group B
```

### Group B setup (Phase 6 active)

- Latest main (post-phase6 tag)
- `.github/copilot-instructions.md` present
- `ctx_session_init` registered
- All Phase 6 features default-on

---

## Dummy fixtures

### ทำไมต้อง dummy

- Reproducible — รันเหมือนกันได้หลายรอบ
- ไม่กระทบงานจริงแบงก์ (privacy)
- ตัด confounding variable (ขนาด spec ไม่เปลี่ยน, ความซับซ้อนคงที่)
- Safe to share (ถ้าวันหนึ่งอยากให้ทีมลอง)

### Fixture set ที่ต้องสร้าง

#### F1: `mock-payment-spec.md` (ขนาดประมาณจริง)

Confluence-style API spec ขนาด **~12 KB / ~250 บรรทัด**:

```markdown
# Payment API v2.3 Spec

## Overview
... (paragraph 1: business context, ~150 words)

## Authentication
... (~100 words explaining JWT + refresh)

## Endpoints

### POST /api/v2/payments/initiate
- **Description:** ...
- **Request body:**
  ```json
  { ... 30+ fields ... }
  ```
- **Validation rules:**
  - amount: min 1, max 999999.99
  - currency: ISO 4217, must be one of [THB, USD, EUR, JPY]
  - reference_id: regex `^PAY-\d{10}$`
  - ... (8-10 rules per endpoint)
- **Responses:**
  - 200: ...
  - 400: ... (with 8 sub-codes)
  - 401: ...
  - 403: ...
  - 422: validation error (15 sub-codes)
  - 500: ...

### POST /api/v2/payments/confirm
... (similar structure)

### GET /api/v2/payments/{id}
... (similar)

### POST /api/v2/payments/refund
... (similar)

## Error Code Reference
... (table with 30+ error codes, descriptions, HTTP codes)

## Sequence Diagrams
### Successful Payment
... (mermaid or ASCII diagram)
### Failed Validation
... (mermaid or ASCII diagram)
### Refund Flow
... (mermaid or ASCII diagram)

## Business Rules
... (10-15 rules: e.g., "Amount > 50,000 requires 2FA", etc.)

## Compliance Notes
... (PCI-DSS related, ~200 words)
```

**ทำไมต้องเป็นแบบนี้:**
- ขนาดใกล้ Confluence จริงในแบงก์
- มี sections ชัด → test `ctx_get_section`
- มี validation rules + error codes → test `ctx_search` ด้วย special chars (`#`, `-`, `:`)
- มี business rules ที่ต้อง cross-reference → test ว่า AI จำได้ตลอด session ไหม

#### F2: `mock-flutter-project/`

Minimal Flutter app ที่:
- มี existing model ผิด spec บางจุด (intentional bugs)
- มี failing tests 3-4 ตัว
- Dependencies ครบใน pubspec.yaml
- Build ได้จริง (รัน `flutter test` ได้)

```
mock-flutter-project/
├── pubspec.yaml
├── lib/
│   ├── models/
│   │   ├── payment_request.dart      # มี validation ผิด
│   │   └── payment_response.dart     # missing field ตาม spec
│   ├── services/
│   │   └── payment_service.dart      # มี bug ใน error handling
│   └── main.dart
└── test/
    ├── payment_request_test.dart     # มี test ที่ fail
    ├── payment_response_test.dart
    └── payment_service_test.dart
```

#### F3: `mock-go-project/`

Minimal Go service ที่:
- Generate model + repository จาก spec
- ใช้ pgx + repository pattern (ตาม profile พี่)
- มี integration test 2-3 ตัว
- มี existing code ที่ไม่ match spec

```
mock-go-project/
├── go.mod
├── cmd/server/main.go
├── internal/
│   ├── domain/
│   │   └── payment.go            # incomplete struct
│   ├── repository/
│   │   └── payment_repo.go       # missing methods
│   └── handler/
│       └── payment_handler.go    # มี validation gap
└── test/
    └── payment_test.go            # มี failing tests
```

---

## Test scenarios

### Scenario 1 — "Spec to code" (Token saving + adherence)

**Goal:** วัด basic workflow — อ่าน spec → generate code → test

**Prompt (ใช้เหมือนกันทุก run):**
```
อ่าน docs/mock-payment-spec.md ทั้งหมด แล้ว generate Dart model 
สำหรับ POST /api/v2/payments/initiate request body ที่ตรงกับ spec 100%
รวม validation annotations ครบทุก field และ test cases ครอบคลุม validation rules ทั้งหมด

ทำงานใน mock-flutter-project/
```

**สิ่งที่บันทึก:**
- Total turns ใช้
- Tools used (count by name)
- Final code correctness (manual review)
- `ctx_stats` snapshot ที่จบ session

**Pass criteria:**
- B-Co adherence ≥ 80%
- B-Co tokens ≤ 50% ของ A-Co
- Final code correctness B ≥ A (ไม่แย่ลง)

### Scenario 2 — "Debug failing tests" (Workflow performance)

**Goal:** Iterative loop ที่ build/test หลายรอบ

**Setup:**
- เริ่มจาก `mock-flutter-project` ที่มี test fail อยู่ 3-4 ตัว

**Prompt:**
```
รัน flutter test ใน mock-flutter-project/ — มี test fail อยู่หลายตัว
แก้ทีละตัวจนผ่านหมด แต่ละ fix ต้องอ้างอิง spec ที่ docs/mock-payment-spec.md
และ explain ก่อน code
```

**สิ่งที่บันทึก:**
- Total turns
- จำนวน flutter test invocations
- Unique vs duplicate command (test deduplication)
- ใช้ cached spec (ctx_get_section) หรือ re-read?

**Pass criteria:**
- B-Co reuse cache ≥ 50% ของ command repeat
- B-Co duplicate test invocations ≤ A-Co
- Quality: ทุก test pass สุดท้าย (binary)

### Scenario 3 — "Multi-session continuity" (Cross-session)

**Goal:** Test ว่า window ใหม่เห็น cache ของ window เก่าไหม + Copilot รู้จัก reuse

**Setup (Day 1):**
- Run scenario 1 จนเสร็จ — มี output cached ใน DB

**Setup (Day 2 / new window):**
- เปิด new VS Code window
- Prompt: "ทำงานต่อจากเมื่อวาน — ตรวจสอบว่า payment_response.dart ตรง spec หรือยัง"

**สิ่งที่บันทึก:**
- Copilot เรียก `ctx_session_init` ทันทีไหม?
- Copilot ใช้ cached spec หรือ re-fetch?
- กี่ turns ก่อน Copilot "เข้าใจ" context

**Pass criteria:**
- B-Co ใช้ cached output ≥ 1 ครั้งภายใน first 3 turns
- B-Co ไม่ re-run spec fetch ที่มี cache อยู่แล้ว

### Scenario 4 — "Special character search" (FTS robustness)

**Goal:** Test Phase 5 FTS escape + Phase 6 doesn't break it

**Prompt:**
```
ใน mock-payment-spec.md, ค้นหา error code "PAY-VLD-001" และอธิบายว่าใช้ตอนไหน
จากนั้นค้น "amount | currency" และดูว่า field ทั้งสองมี validation อะไรบ้าง
```

**สิ่งที่บันทึก:**
- ctx_search call ใช้ query แบบไหน (raw หรือ escaped)
- Search result correctness
- ใช้ `ctx_outline` หรือ `ctx_get_section` ก่อน search ไหม

**Pass criteria:**
- ทั้ง A และ B ทำได้ (Phase 5 baseline)
- B ใช้ `ctx_get_section` หรือ `ctx_outline` แสดงว่าอ่าน description ถูก

### Scenario 5 — "Stress / context exhaustion" (limit testing)

**Goal:** Test ว่า ctx-saver ป้องกัน context exhaustion ได้แค่ไหน

**Prompt (จัดให้ยาว):**
```
1. อ่าน mock-payment-spec.md
2. Generate Dart models ทั้ง 4 endpoints
3. Generate Go domain structs ทั้ง 4 endpoints
4. Generate Go repository พร้อม pgx queries
5. Generate Flutter test cases ครบทุก validation rule
6. Run flutter test, fix bugs
7. Run go test, fix bugs
8. Generate sequence diagram เป็น mermaid format
9. สรุป business rules ทั้งหมดเป็น checklist
```

**บันทึก:**
- จุดที่ context "ตึง" (≥80% used)
- จุดที่ Copilot "ลืม" earlier instructions
- ผลสุดท้ายเสร็จไหม

**Pass criteria:**
- B-Co ทำงานเสร็จ; A-Co อาจไม่เสร็จ (ลืม) — แสดง value
- B-Co context usage peak ≤ 60% เมื่อจบ
- A-Co context usage peak ≥ 80% (ถ้าไม่ใช่แสดงว่า scenario ไม่ stress พอ)

---

## Metrics & data collection

### Automated metrics (จาก `ctx_stats`)

ทุก scenario จบ → รัน:
```
ctx_stats(scope="session")
```

แล้วบันทึก:
```json
{
  "scenario": "S1",
  "group": "B-Co",
  "platform": "Copilot",
  "phase6": true,
  "ctx_stats_output": {
    "outputs_stored": ...,
    "raw_bytes": ...,
    "summary_bytes_estimate": ...,
    "saving_percent": ...,
    "adherence_score": ...,
    "native_shell_count": ...,
    "native_read_count": ...,
    "ctx_execute_count": ...,
    "ctx_read_file_count": ...,
    "top_commands": [...]
  }
}
```

### Manual observations (สำคัญที่ automated จับไม่ได้)

ระหว่าง session บันทึก:
- **First tool called:** อะไร? (ดู Copilot adherence แท้จริง)
- **Confusion moments:** Copilot ถามซ้ำ / ลืม / contradictory ไหม? กี่ครั้ง?
- **Duplicate commands:** spotted ครั้งแรกตอน turn ไหน? Copilot รับ hint ไหม?
- **Quality issues:** Code ที่ generate มา bug ชัด ๆ กี่ตัว?
- **Subjective speed:** "feel like" เร็วขึ้น/ช้าลงไหม?

ใช้ template:
```markdown
## Session log: <scenario>-<group>

Time start: __:__
Time end: __:__
Total turns: __

### Tool calls (in order)
1. <tool_name>: <brief desc>
2. ...

### Confusion moments
- Turn N: <description>

### Duplicates spotted
- Turn N: <command repeated>

### Code quality issues
- <issue 1>
- <issue 2>

### Overall feel
<1-2 sentences>
```

### Result aggregation table

หลัง 5 scenarios × 2 groups × 2 platforms = 20 sessions เสร็จ:

| Scenario | A-Co adherence | B-Co adherence | Δ | A-Co tokens | B-Co tokens | Δ | A-Cl adh | B-Cl adh | Δ |
|---|---|---|---|---|---|---|---|---|---|
| S1 | | | | | | | | | |
| S2 | | | | | | | | | |
| S3 | | | | | | | | | |
| S4 | | | | | | | | | |
| S5 | | | | | | | | | |

**Tokens** = `raw_bytes + tool definitions overhead` ใช้ approximate ratio bytes/token = 4 (rough)

---

## Test execution timeline

### Day 1: Fixture creation (2-3 ชั่วโมง)

- [ ] สร้าง `mock-payment-spec.md` (ทำให้ realistic ขนาดเหมือน Confluence จริง)
- [ ] สร้าง `mock-flutter-project/` (รัน `flutter test` ได้, มี fail intentional)
- [ ] สร้าง `mock-go-project/` (รัน `go test` ได้, มี fail intentional)
- [ ] Commit + tag fixtures version
- [ ] Tag git: `git tag pre-phase6 <hash>`, `git tag post-phase6 main`

### Day 2: Group A runs (ครึ่งวัน)

- [ ] Switch to `pre-phase6` build
- [ ] รัน Scenario 1-5 บน Copilot — record
- [ ] รัน Scenario 1-5 บน Claude Code — record

### Day 3: Group B runs (ครึ่งวัน)

- [ ] Switch to `post-phase6` build
- [ ] รัน Scenario 1-5 บน Copilot — record
- [ ] รัน Scenario 1-5 บน Claude Code — record

### Day 4: Analysis + report (2-3 ชั่วโมง)

- [ ] Aggregate results into table
- [ ] Compute deltas
- [ ] Verify hypothesis criteria
- [ ] Write findings → `docs/phase6-test-results.md`
- [ ] Decide: ship / iterate / revert

### Total time: 2-3 working days

---

## Decision criteria (ผลแบบไหน "ผ่าน")

### ✅ Phase 6 PASSES (ship to team)

ต้องตรงทุกข้อ:
- B-Co adherence ≥ 80% เฉลี่ย 5 scenarios
- B-Co token saving ≥ 60% เทียบ A-Co เฉลี่ย
- B-Cl ไม่ regress (≥ A-Cl อย่างน้อย 95%)
- Code quality B ≥ A (counts of bugs ≤)
- Subjective: ใช้แล้วรู้สึกดีกว่าจริง ไม่ใช่แค่ตัวเลขดี

### ⚠️ Phase 6 NEEDS ITERATION

ถ้าตรงข้อใดข้อหนึ่ง:
- B-Co adherence 60-80% — descriptions ยังไม่แรงพอ → revisit Task 6.1
- Token saving 40-60% — session_init payload ใหญ่ไป → trim
- B-Cl regress 5-10% — Claude Code ถูกกระทบ → ปรับ description ให้สั้นลง
- Bug count B > A — ค่อยเช็คว่า adherence push ทำให้คุณภาพแย่ลงหรือเปล่า

### 🔴 Phase 6 FAILS (revert)

- B-Co adherence < 60% — Phase 6 ไม่ work effective
- B-Cl regress >10% — ทำลาย Claude Code experience
- Code quality drop ชัดเจน — adherence ดีแต่ผิด

---

## Privacy & compliance (สำคัญสำหรับงานแบงก์)

⚠️ **ห้าม** ใช้ใน test:
- Real customer data
- Real account numbers / IDs
- Real internal API endpoints
- Production credentials
- Code จาก private repo ของแบงก์

✅ **ใช้** ได้:
- Synthetic data (mock customers, fake amounts)
- Public-style endpoints (`/api/v2/payments/...`)
- Generic credentials (`mock-jwt-token`, `MOCK_API_KEY`)
- Code generated from scratch สำหรับ test เท่านั้น

หลังจบ test:
- [ ] ไม่ commit fixtures + results เข้า work repo (ใช้ separate sandbox repo)
- [ ] Clean test DB: `rm ~/.local/share/ctx-saver/mock-*.db`

---

## Test artifacts (ที่จะมีหลังจบ)

```
ctx-saver-test/
├── TEST_PLAN.md              # this file
├── fixtures/
│   ├── mock-payment-spec.md
│   ├── mock-flutter-project/
│   └── mock-go-project/
├── runs/
│   ├── 2026-04-XX-A-Co-S1.md   # individual session log
│   ├── 2026-04-XX-A-Co-S2.md
│   ├── ...
│   └── 2026-04-XX-B-Cl-S5.md
├── stats/
│   ├── A-Co-S1.json            # ctx_stats output
│   ├── ...
│   └── B-Cl-S5.json
├── analysis/
│   ├── results-table.md         # aggregate
│   ├── findings.md              # narrative analysis
│   └── recommendations.md       # next steps
└── docs/
    └── phase6-test-results.md   # public summary
```

---

## Bias awareness (ที่ต้องระวังเมื่อทดสอบเอง)

ตัวพี่เป็นทั้ง designer + tester → bias เยอะมาก ระวัง:

1. **Confirmation bias** — อยากให้ B ดีกว่า A → unconsciously ทำให้ B ดีกว่า  
   **Mitigation:** ตอน A ทำเต็มที่เหมือน B (ไม่ "give up" ใน A เร็ว)

2. **Order effects** — ทำ B ทีหลัง = ได้ practice scenario แล้ว  
   **Mitigation:** alternate order (S1: A→B, S2: B→A, S3: A→B, ...)

3. **Tool selection bias** — รู้ว่า ctx-saver ดี → push ใช้ใน B  
   **Mitigation:** ห้าม manual override — Copilot/Claude เลือกเอง

4. **Recall bias** — บันทึกทีหลัง → จำผิด  
   **Mitigation:** บันทึก **ระหว่าง** session ทันที, screenshot key moments

5. **Sample size** — n=2 ต่อ scenario น้อย noise สูง  
   **Mitigation:** ถ้าผลใกล้เคียง → run อีก 2 ครั้งต่อ scenario (n=4)

---

## Suggested next session prompt for Copilot

ก็อปข้างล่างนี้ส่ง Copilot ใน new VS Code window (มี Phase 6 active):

```
อ่าน TEST_PLAN.md ในโฟลเดอร์นี้ — เริ่มจากการสร้าง fixtures ใน Day 1

Task 1: สร้าง fixtures/mock-payment-spec.md ตาม section "F1" ใน plan
- ขนาด ~12KB / 250 บรรทัด
- 4 endpoints (initiate, confirm, get, refund)
- 30+ error codes (ใช้ pattern PAY-XXX-NNN)
- Validation rules with regex/min/max ครบทุก field
- 3 sequence diagrams (mermaid format)
- 10-15 business rules
- Compliance notes section

ทำใน /tmp/ctx-saver-test/ ก่อน อย่าเขียนใน work repo

ทำเสร็จ commit ลง separate test repo
```

---

## Open questions (ที่ผมคิดออก แต่ตัดสินใจไม่ได้)

1. **Run multiple times per scenario?** ถ้า n=2 น้อยไป — แต่ x4 = ใช้เวลา 2 เท่า
2. **Standardize prompts หรือ free-form?** Standardize = reproducible แต่ไม่เหมือน real workflow
3. **Test Claude Code Sonnet vs Opus?** ผลอาจต่าง — แต่ scope เพิ่มเป็น 2 เท่า
4. **Long-term test (1 สัปดาห์)?** ตอนนี้ design = 2-3 วัน, ถ้าอยาก measure decay/improvement ต้อง longer

แนะนำ: **start ด้วย design นี้ก่อน** ถ้าผลก้ำกึ่ง ค่อยขยาย scope

---

## Final reminder

**Test ตัวเอง = บอกตัวเองตรง ๆ**

ถ้า B-Co adherence แค่ 65% แล้วใจอยากให้ผ่าน → **NO**, ต้องไป iterate Phase 6 ก่อน  
ถ้า B-Cl ดู regress → **NO**, ต้องเช็คว่า description ยาวไปกระทบ Claude หรือเปล่า

Test value มาจาก **ความตรงไปตรงมา** ไม่ใช่ผลที่อยากเห็น
