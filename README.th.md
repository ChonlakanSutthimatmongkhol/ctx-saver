# ctx-saver

[English](README.md) | **ภาษาไทย**

MCP server แบบ self-hosted (Go) ที่ช่วยลดการใช้ context window ของ AI โดยการแยกเก็บ output ขนาดใหญ่และคืนค่าเฉพาะสรุปย่อแทน

## ทำไมต้องใช้

คำสั่งที่คืนค่า output ขนาดใหญ่กินเนื้อที่ context window เร็วมาก:

- **Infrastructure**: `kubectl get pods -A`, `docker ps -a --no-trunc`, `aws s3 ls --recursive`
- **Logs & monitoring**: `journalctl`, `docker logs`, `npm install` (build logs), `git log --all --oneline`
- **Search**: `find / -name "*.ts"`, `grep -r pattern`, `curl https://api.example.com/users`
- **Package mgmt**: `pip list`, `go mod graph`, `npm ls --all`
- **ข้อมูล**: `cat large_file.json`, `jira issue list`, `ls -la /var/log/`

ctx-saver จะดักจับ output เหล่านั้น เก็บไว้ใน SQLite database ที่มี FTS5 indexing และคืนค่าแค่สรุปย่อ (head + tail) หากต้องการข้อมูลเพิ่มเติมให้ใช้ `ctx_search` หรือ `ctx_get_full`

**ไม่มีคลาวด์ ไม่มีการติดตาม ไม่ต้องสมัครบัญชี โค้ดตรวจสอบได้ 100%**

## เริ่มต้นใช้งาน (5 นาที)

### ตัวเลือก A — go install (ต้องการ Go 1.25+)

```bash
go install github.com/ChonlakanSutthimatmongkhol/ctx-saver/cmd/ctx-saver@latest
```

ไฟล์ binary จะอยู่ที่ `$(go env GOPATH)/bin/` ต้องให้ path นั้นอยู่ใน `PATH` ด้วย (เพิ่มลง `~/.zshrc` หรือ `~/.bashrc` ครั้งเดียว):

```bash
export PATH="$PATH:$(go env GOPATH)/bin"
```

### ตัวเลือก B — clone และ build เอง

```bash
git clone https://github.com/ChonlakanSutthimatmongkhol/ctx-saver.git
cd ctx-saver
make install        # build + คัดลอกไปที่ /usr/local/bin/ctx-saver
```

### ตั้งค่า AI client

**Claude Code**
```bash
claude mcp add ctx-saver -- $(go env GOPATH)/bin/ctx-saver
```

**VS Code Copilot** — สร้างไฟล์ `.vscode/mcp.json` ใน project root:
```json
{
  "servers": {
    "ctx-saver": {
      "command": "/usr/local/bin/ctx-saver"
    }
  }
}
```

> **ผู้ใช้ตัวเลือก A** (`go install`): เปลี่ยน path ด้านบนเป็น `$(go env GOPATH)/bin/ctx-saver`
> หรือจะรัน `ctx-saver init copilot` ก็ได้ — ตรวจหา binary path อัตโนมัติ (ไม่ต้องใช้ `jq`)

หรือตั้งค่าแบบ global ใน VS Code `settings.json`:
```json
{
  "mcp.servers": {
    "ctx-saver": {
      "command": "/Users/<you>/go/bin/ctx-saver"
    }
  }
}
```

ตรวจสอบ: Command Palette → **MCP: List Servers** — ควรแสดง `ctx-saver` พร้อม 11 tools

### ติดตั้ง hooks สำหรับ Claude และลงทะเบียน server สำหรับ Copilot

hooks ของ Claude ช่วยให้ระบบ routing คำสั่งที่มี output ขนาดใหญ่และการกู้คืน session history ทำงานอัตโนมัติ
สำหรับ VS Code Copilot ขั้นตอนนี้เป็นการลงทะเบียน `ctx-saver` MCP server เท่านั้น

```bash
# Claude Code
ctx-saver init claude

# VS Code Copilot (รันจาก project root; ลงทะเบียนเฉพาะ MCP server)
ctx-saver init copilot
```

คำสั่งทั้งสองตรวจหา binary path อัตโนมัติ สำรองค่า config เดิม และ merge JSON อย่างปลอดภัยโดยไม่เขียนทับ settings ที่ไม่เกี่ยวข้อง ไม่ต้องใช้ `jq`

> **ผู้ที่ clone repo:** `./scripts/install-hooks.sh claude` และ `./scripts/install-hooks.sh copilot` ยังใช้ได้เช่นเดิม (ต้องการ `jq`)

สำหรับ VS Code Copilot ตอนนี้ `.vscode/mcp.json` รองรับเฉพาะ `servers` และจะไม่ยอมรับ key ระดับบนชื่อ `hooks`

สรุปคือ:
- Copilot ใช้ MCP tools ของ `ctx-saver` ได้ตามปกติ
- Copilot ยังไม่รัน lifecycle hooks อัตโนมัติ (`pretooluse`, `posttooluse`, `sessionstart`)
- hooks แบบอัตโนมัติรองรับผ่าน Claude Code เท่านั้นในตอนนี้ (`~/.claude/settings.json`)

หมายเหตุ: คำสั่ง `ctx-saver hook <event>` ยังเรียกแบบ manual ได้ หากต้องการทดสอบเป็นรายครั้ง

ลบ hooks ของ Claude:
```bash
./scripts/uninstall-hooks.sh claude
```

ถ้าต้องการลบ server ของ VS Code Copilot ให้ลบ `servers.ctx-saver` ออกจาก `.vscode/mcp.json`

ดูรายละเอียดเพิ่มเติมที่ [พฤติกรรมของ Hook](#hooks) ด้านล่าง

## นโยบาย Cache Freshness (v0.5.0)

ทุก response จากการดึงข้อมูลจะมี field `freshness` แนบมาด้วย:

```json
{
  "freshness": {
    "source_kind": "shell:kubectl",
    "cached_at": "2026-04-26T03:00:00Z",
    "age_seconds": 7200,
    "age_human": "2h ago",
    "stale_level": "aging",
    "refresh_hint": ""
  }
}
```

| `stale_level` | อายุ | พฤติกรรม AI |
|---|---|---|
| `fresh` | < 1 ชั่วโมง | ใช้ข้อมูลได้เลย |
| `aging` | 1–24 ชั่วโมง | ใช้ได้ แจ้งอายุถ้าเกี่ยวข้อง |
| `stale` | 1–7 วัน | แจ้งผู้ใช้; เสนอรัน `ctx_execute` ใหม่ |
| `critical` | > 7 วัน | **ห้ามใช้ตัดสินใจ** ระบบจะตั้ง `user_confirmation_required: true` — AI ต้องแสดง prompt ให้ผู้ใช้ยืนยันก่อนดำเนินการต่อ |

**Auto-refresh**: source ที่มี `auto_refresh: true` (เช่น `shell:kubectl`, `shell:acli`) จะถูกรันใหม่อัตโนมัติเมื่อดึงข้อมูลแล้ว output เก่าเกิน TTL โดยคง `output_id` เดิมไว้

**ข้ามการยืนยัน**: ส่ง `accept_stale: true` ใน input ของ tool ใดก็ได้ เพื่อข้าม confirmation gate

**ปิดทั้งหมด**: ตั้งค่า `freshness.enabled: false` ใน config

ดูรายละเอียดที่ [docs/migration-v0.5.md](docs/migration-v0.5.md) และตัวอย่าง config ที่ [configs/freshness-examples/](configs/freshness-examples/)

## Smart Summarizer

`ctx_execute` ตรวจจับ format ของ output อัตโนมัติและสรุปผลแบบ structured:

| Format | ตรวจจับเมื่อ | สรุปรวม |
|--------|-------------|---------|
| `flutter_test` | คำสั่งมี `flutter test` หรือ output มี `All tests passed!` / `Some tests failed.` | จำนวน pass/fail/skip, ชื่อ test ที่ fail, ระยะเวลา |
| `go_test` | คำสั่งมี `go test` หรือ output มี `=== RUN` + `--- PASS/FAIL` | จำนวน package ที่ pass/fail, รายละเอียด test ที่ fail, coverage % |
| `json` | output เป็น JSON ที่ขึ้นต้นด้วย `{` หรือ `[` | top-level keys + types, ความยาว array, ตัวอย่างค่า |
| `git_log` | คำสั่งมี `git log` หรือ output ขึ้นต้นด้วย `commit <hash>` | จำนวน commit, commit ล่าสุด/เก่าสุด, top authors |
| `generic` | fallback สำหรับอื่นๆ | head + tail lines พร้อมจำนวนบรรทัดที่ตัดออก |

ตั้งค่า `summary.smart_format: false` ใน config เพื่อใช้ generic summariser เสมอ

### Search features

**Auto-escape** — อักขระพิเศษ (`#`, `-`, `|`, `:`, `*`, `(`, `)`) ใน `ctx_search` จะถูก escape อัตโนมัติเป็น FTS5 phrase literal ไม่ต้อง escape เอง

**LIKE fallback** — ถ้า FTS5 ยังล้มเหลว ระบบจะ retry ด้วย LIKE scan อัตโนมัติ response จะมี `search_mode: "fts5"` หรือ `search_mode: "like_fallback"`

**Synonym expansion** — query ถูก expand อัตโนมัติด้วย dictionary ในตัว เช่น `api_path` → `[api_path, endpoint, route, url, path]`, `authentication` → `[auth, login, jwt, oauth, bearer, token]` response มี `expanded_queries` แสดงว่า search อะไรจริงๆ

เพิ่ม synonym เฉพาะโปรเจกต์ด้วยการสร้าง `.ctx-saver-synonyms.yaml` ใน project root:

```yaml
payment_flow: [checkout, billing, invoice, transaction]
user_model: [account, profile, member]
```

Project override จะแทนที่ (ไม่ merge) entry ใน built-in ที่มี key เดียวกัน

## Tools

| Tool | วัตถุประสงค์ |
|------|-------------|
| `ctx_session_init` ⭐ | **เรียกก่อนในทุก session ใหม่** คืน project rules, event ล่าสุด, inventory ของ output ที่เก็บไว้, และ config Copilot Enterprise ต้องเรียกเองตรงๆ; Claude Code ใช้ SessionStart hook อัตโนมัติ |
| `ctx_execute` | รันคำสั่ง shell/python/go/node; output ขนาดใหญ่จะถูกเก็บและสรุปย่อ แสดง `duplicate_hint` ถ้ารันคำสั่งเดิมภายใน 30 นาทีที่ผ่านมา |
| `ctx_read_file` | อ่านไฟล์ โดยเลือกส่งผ่าน processing script ได้ ใช้ `fields="signatures"` เพื่อดึงเฉพาะ function/type/const declaration พร้อมหมายเลขบรรทัดต้นฉบับ (รองรับ Go, Python, Dart) |
| `ctx_outline` | ดึง headings / สารบัญจาก stored output รวม `freshness` field |
| `ctx_get_section` | ดึง section เฉพาะด้วย heading text (ใช้หลัง `ctx_outline`) รวม `freshness` + `user_confirmation_required` |
| `ctx_search` | FTS5 full-text search ในทุก output ที่เก็บไว้ รวม `freshness` ต่อผลลัพธ์ อักขระพิเศษ escape อัตโนมัติ; fallback ไป LIKE ขยาย query ด้วย synonym อัตโนมัติ |
| `ctx_get_full` | ดึง output ฉบับเต็มหรือระบุช่วงบรรทัด รวม `freshness` + `user_confirmation_required`; ใช้ `accept_stale: true` เพื่อข้ามการยืนยัน |
| `ctx_stats` | รายงานสถิติการเก็บข้อมูลและ hook activity (scope: `session\|today\|7d\|all`); ใช้ `view="outputs"` เพื่อแสดง output ทั้งหมด (แทน `ctx_list_outputs`) |
| `ctx_note` | บันทึกหรือแสดง architectural decision ที่รอดจาก `/compact`; ใช้ `action="list"` เพื่อดู decision ที่บันทึกไว้ (แทน `ctx_list_notes`) |
| `ctx_purge` | **[DESTRUCTIVE]** ลบ cached output และ session event ทั้งหมดของ project ต้องส่ง `confirm="yes"` Decision notes จะถูกเก็บไว้ตามค่าเริ่มต้น ส่ง `all=true` เพื่อลบด้วย |

## Cache Purge (v0.6.0)

ใช้ `ctx_purge` เพื่อล้าง cache เมื่อสลับ feature context, cache เก่าหรือรกรุงรัง, หรือก่อนส่งมอบงาน demo

```
ctx_purge(confirm="yes")           # ลบ output + event; เก็บ notes ไว้
ctx_purge(confirm="yes", all=true)  # ลบ ctx_note entries ด้วย
```

| เป้าหมาย | ค่าเริ่มต้น | `all=true` |
|----------|------------|------------|
| Cached outputs | ✅ ลบ | ✅ ลบ |
| Session events | ✅ ลบ | ✅ ลบ |
| Decision notes | ❌ เก็บไว้ | ✅ ลบ |

> ⚠️ ย้อนกลับไม่ได้ output ที่ลบแล้วกู้คืนไม่ได้

## Decision Log (v0.5.1)

ใช้ `ctx_note` เพื่อบันทึก design choice ที่ไม่ชัดเจน, constraint ที่ค้นพบ, หรือ tradeoff ที่ตกลงกันไว้ เพื่อให้ session ถัดไปและหลัง `/compact` สามารถดึงเหตุผลกลับมาได้

```
ctx_note(
  text="ใช้ WithFreshness builder pattern เพราะ positional arg จะทำให้ 15 test sites พัง",
  tags=["arch", "phase7"],
  importance="high"
)

ctx_note(action="list", scope="session")
ctx_note(action="list", tags=["arch"], min_importance="high")
```

Decision จะถูก inject อัตโนมัติใน `ctx_session_init` (สูงสุด 10 รายการล่าสุดที่ importance normal+high จาก 7 วันที่ผ่านมา)

## วิธีการทำงาน

```
Claude Code / VS Code Copilot
        │
        ▼  MCP (stdio)
  ctx-saver server (Go binary)
        │
        ├── ctx_execute: subprocess → จับ output
        │       ├── เล็ก (≤5KB) → คืนค่าตรง
        │       └── ใหญ่ (>5KB) → เก็บใน SQLite + คืนค่าสรุป
        │
        └── SQLite (~/.local/share/ctx-saver/<hash>.db)
                ├── outputs table  (ข้อความเต็ม + metadata)
                └── outputs_fts    (FTS5 + BM25 ranking)
```

## ตัวเลขใช้งานจริง: ลด token ได้เท่าไร

วัดจากคำสั่งจริงที่รันผ่าน `ctx_execute` ใน repository นี้

Benchmark snapshot (`2026-05-06`). สมมติฐานสำหรับประมาณ token: `1 token ≈ 4 bytes`

| คำสั่ง | Format | Raw (bytes) | Summary (bytes) | ประหยัด | Token ที่ประหยัดได้ |
|--------|--------|-------------|-----------------|---------|---------------------|
| `go test -race -v ./...` (790 lines) | `go_test` ✦ | 39,313 | 115 | **99.7%** | ~9,800 |
| `git log --oneline -500` | `git_log` ✦ | 8,701 | 155 | **98.2%** | ~2,135 |
| jira 200 issues (JSON) | `json` ✦ | 87,938 | 320 | **99.6%** | ~21,905 |
| `kubectl get pods` (100 pods) | `generic` | 6,153 | 1,850 | **69.9%** | ~1,076 |
| `app.log` (2,000 บรรทัด) | `generic` | 176,998 | 1,500 | **99.2%** | ~43,875 |
| `git diff HEAD~5` (398 บรรทัด) | `generic` | 21,795 | 750 | **96.6%** | ~5,261 |
| `grep -rn 'func' ./...` (500 matches) | `generic` | 50,344 | 1,450 | **97.1%** | ~12,224 |
| **รวม** | | **391,242** | **6,140** | **98.4%** | **~96,276** |

✦ Smart-format summariser: ดึง metadata แบบมีโครงสร้างแทนการตัด head/tail

ผลรวมรอบนี้ลดได้ **98.4%** (จาก 391,242 bytes เหลือ 6,140 bytes)

สำหรับการเปรียบเทียบแบบ dry-run (จำลองด้วย head-20 + tail-5) ให้รัน `scripts/benchmark.sh`

## การตั้งค่า

ค่าตั้งต้นอยู่ที่ `~/.config/ctx-saver/config.yaml` สามารถ override ต่อ project ได้ที่ไฟล์ `.ctx-saver.yaml` ใน project root

```yaml
sandbox:
  timeout_seconds: 60

storage:
  data_dir: ~/.local/share/ctx-saver
  retention_days: 14
  max_output_size_mb: 50

summary:
  head_lines: 20
  tail_lines: 5
  auto_index_threshold_bytes: 32768  # 32 KB (v0.6.0+)
  smart_format: true                  # format-aware summariser (flutter_test | go_test | json | git_log | generic)
  enabled_formatters: []              # ว่าง = เปิดทั้งหมด; ระบุชื่อเพื่อจำกัด

dedup:
  enabled: true
  window_minutes: 30   # แสดง duplicate_hint ถ้ารันคำสั่งเดิมภายใน window นี้

logging:
  level: info
  file: ~/.local/share/ctx-saver/server.log

hooks:
  session_history_limit: 10   # จำนวน event สูงสุดที่ inject เข้า SessionStart context

deny_commands:
  - "rm -rf /"
  - "sudo *"
  - "dd if=*"

freshness:
  enabled: true
  default_max_age_seconds: 3600           # 1 ชั่วโมง สำหรับ source ที่ไม่รู้จัก
  user_confirm_threshold_seconds: 604800  # 7 วัน → ถาม user ก่อนใช้
  sources:
    shell:kubectl: { max_age_seconds: 60,  auto_refresh: true }
    shell:acli:    { max_age_seconds: 300, auto_refresh: true }
    shell:git:     { max_age_seconds: 120, auto_refresh: false }
    # ดู configs/freshness-examples/ สำหรับ preset เพิ่มเติม
```

## การปรับแต่งประสิทธิภาพ Token (Token efficiency tuning)

`auto_index_threshold_bytes` กำหนดว่า output ขนาดเท่าไหร่ถึงจะ return inline (ตรงๆ)
แทนที่จะเก็บด้วย `output_id`:

| ค่า | ผล |
|-----|-----|
| `32768` (ค่าเริ่มต้น) | ไฟล์ Go/Python ทั่วไป (300–500 บรรทัด) return inline ไม่ต้องรอ round-trip |
| `65536` | เก็บ build output ขนาดกลาง inline ได้; ใช้ token ต่อ turn มากขึ้น แต่เรียก tool น้อยลง |
| `5120` | พฤติกรรมแบบ v0.5.x — บังคับใช้ `output_id` กับไฟล์ส่วนใหญ่ |

ตั้งค่าใน `~/.config/ctx-saver/config.yaml`:

```yaml
summary:
  auto_index_threshold_bytes: 32768  # ปรับตามต้องการ
```

## Hooks

Hooks ทำงานเป็น subprocess เบาๆ คู่กับ AI agent ใช้ binary เดียวกัน (`ctx-saver hook <event>`) จึงไม่ต้องติดตั้งเพิ่มหลังจาก `make install`

| Hook | Event | การทำงาน |
|------|-------|---------|
| PreToolUse | ก่อนการเรียกใช้ shell/bash tool | บล็อกคำสั่งอันตราย (`rm -rf`, pipe-to-shell, `eval`, `sudo -s`); redirect คำสั่งที่มี output ขนาดใหญ่ (`curl`, `wget`, `cat *.log`, `find`, `journalctl`) ให้ใช้ `ctx_execute` แทน |
| PostToolUse | หลังทุก tool call | บันทึกสรุปของ tool call ลง SQLite ต่อ project สำหรับการกู้คืน session |
| SessionStart | เมื่อเริ่ม session ใหม่ | inject routing rules และ session history ล่าสุด (สูงสุด `hooks.session_history_limit` event ที่ deduplicate แล้ว) เข้า context ของ model |

### curl แบบปลอดภัย

`curl --version`, `curl -I`, `curl --head`, และ `curl -o /dev/null` จะ **ไม่ถูก redirect** — เฉพาะ request ที่น่าจะคืนค่า body ขนาดใหญ่เท่านั้นที่ส่งผ่าน `ctx_execute`

### รูปแบบคำสั่งอันตรายที่ PreToolUse บล็อก

- `rm -rf` / `rm -fr` / ทุกรูปแบบ `rm -[rRfF]+`
- `find / … -delete`
- redirect ไปยัง raw disk (`> /dev/sda`, `> /dev/nvme0`)
- pipe ไปยัง shell interpreter (`curl … | bash`, `wget … | sh`, `| zsh`, …)
- ทุกรูปแบบของ `eval`
- `sudo -s`, `sudo rm`, `sudo dd`
- อ่านไฟล์ credential (`.env`, `id_rsa`, `.pem`, `.key`)

## สำหรับผู้ใช้ Copilot Enterprise

ถ้าคุณใช้ GitHub Copilot ในบริษัท (เช่น ธนาคารหรือ fintech) ดู [Copilot Enterprise Setup Guide](docs/copilot-enterprise-setup.md) สำหรับ:
- Policy ที่ admin ต้องเปิด (MCP server allowlist)
- ขั้นตอนการติดตั้งสำหรับ VS Code Copilot Agent mode
- การ verify ว่า ctx-saver ทำงานถูกต้อง (เรียก `ctx_session_init`)
- แก้ปัญหา tool-adherence (Copilot ไม่ใช้ ctx_execute แทน runInTerminal)
- วิธีพูดคุยกับ IT/Security เพื่อขอ approve MCP

**Quick start:**
```bash
go install github.com/ChonlakanSutthimatmongkhol/ctx-saver/cmd/ctx-saver@latest
ctx-saver init copilot                # ลง MCP server ใน .vscode/mcp.json
ctx-saver init copilot-instructions   # ลง Copilot rules ใน .github/
```

## Project Knowledge (ความรู้เกี่ยวกับ Project)

ยิ่งใช้ ctx-saver กับ project นานขึ้น → AI รู้จัก project ดีขึ้น
หลังจาก 3+ sessions, ctx-saver สร้างไฟล์ `.ctx-saver/project-knowledge.md` ที่มี:

- **ไฟล์ที่อ่านบ่อย** — พร้อม cache stability (hash เปลี่ยนบ่อยหรือไม่)
- **คำสั่งที่รันบ่อย** — พร้อม average output size
- **ลำดับคำสั่งที่พบบ่อย** — คู่คำสั่งที่มักรันต่อกัน
- **การตัดสินใจสำคัญ** — notes ที่ tag `importance=high`
- **รูปแบบการทำงาน** — session และ output counts

ไฟล์นี้ถูก reference จาก `CLAUDE.md` / `copilot-instructions.md` ทำให้ทุก session
ได้ context โดยไม่เพิ่ม token cost

### การสร้าง knowledge

```bash
ctx-saver knowledge refresh          # สร้าง/อัปเดต project-knowledge.md
ctx-saver knowledge show             # แสดงผลที่ stdout (ไม่เขียนไฟล์)
ctx-saver knowledge reset            # ลบ project-knowledge.md
ctx-saver knowledge refresh --quiet  # ไม่แสดง output (สำหรับ cron)
```

### ตั้ง Cron (แนะนำ)

```bash
# macOS/Linux — refresh ทุกคืนวันทำงาน 23:00
crontab -e
# เพิ่ม:
0 23 * * 1-5 cd /path/to/project && ctx-saver knowledge refresh --quiet
```

**Idle detection** ทำงานอัตโนมัติขณะ MCP server เปิดอยู่: หลังจาก idle
`knowledge.idle_minutes` (default 30 นาที) จะ refresh ใน background โดยอัตโนมัติ

### การตั้งค่า

```yaml
knowledge:
  min_sessions: 3        # จำนวน sessions ขั้นต่ำก่อนสร้างไฟล์
  idle_minutes: 30       # 0 = ปิด idle detection
  top_files_limit: 10
  top_commands_limit: 10
  decisions_limit: 10
```

## Build

```bash
# ต้องการ Go 1.25+
make build          # → bin/ctx-saver
make test           # unit tests
make lint           # golangci-lint
make install        # → /usr/local/bin/ctx-saver
```

## ความปลอดภัย

- SQLite database permissions: `0600` (เจ้าของอ่าน/เขียนเท่านั้น)
- ตรวจสอบ command deny list ก่อน execute ทุกครั้ง
- ปฏิเสธ binary output (null bytes)
- ป้องกัน path traversal ด้วย `filepath.Abs` + `filepath.Clean`
- Log file ตัด command string ให้เหลือ 120 ตัวอักษรเพื่อหลีกเลี่ยงการ log secret
- ไม่มีการเชื่อมต่อเครือข่าย — ทำงานบนเครื่องท้องถิ่นเท่านั้น

## โครงสร้าง Repository

```
cmd/ctx-saver/main.go          entry point
internal/config/               YAML config loader
internal/sandbox/              execution interface (subprocess + srt stub)
internal/store/                SQLite + FTS5 storage layer
internal/summary/              smart summariser: format-aware (flutter_test, go_test, json, git_log) + generic fallback
  internal/summary/formats/      one file per formatter + tests
internal/search/               synonym expansion (builtin YAML + project override)
internal/handlers/             one file per MCP tool
internal/hooks/                PreToolUse / PostToolUse / SessionStart hooks
internal/server/               MCP server wiring
tests/                         integration tests + testdata
scripts/                       install.sh, install-hooks.sh, uninstall-hooks.sh, benchmark.sh
configs/                       setup guides and hook config templates per platform
```

## การเลือกใช้เทคโนโลยี

### Subprocess sandbox (ไม่ใช่ containers หรือ VMs)

**ทำไม**: ความเรียบง่ายและการเริ่มต้นทันที Container ใช้ 50–200ms ต่อการ execute; subprocess สปอนเร็ว ~1ms เท่านั้น สำหรับเครื่องมือที่ทำงานในเซสชั่นของคุณแล้ว (เช่น AI chat) การแยกเก็บ output ผ่าน `exec.Command` พอเพียง เราสนใจการแยกเก็บ **output** ไม่ใช่ **process** — ภัยคุกคาม model คือการ pollution ของ context ไม่ใช่ malware

**ในอนาคต**: จะเพิ่ม Anthropic `srt` (Secure Runtime) สำหรับการแยกเก็บระดับ OS เมื่อต้องการ (toggle via `sandbox.use_srt: true`)

### FTS5 แทนการ index แบบเดิมๆ

**ทำไม**: BM25 ranking ใน FTS5 สร้างมาแล้วและ tune สำหรับการค้นหาภาษาธรรมชาติ การค้นหา "pod status" ผ่าน 50MB ของ `kubectl` logs จะคืนบรรทัดที่เกี่ยวข้องก่อน ไม่ใช่แค่ substring matches ไม่มี query complexity เพิ่มเติม — แค่ `SELECT … FROM outputs_fts WHERE outputs_fts MATCH 'pod status'`

### SQLite แทน Redis/Postgres

**ทำไม**: Self-hosted ไม่ต้องจัดการเซอร์วิสภายนอก `sqlite` อยู่ในไฟล์เดียว `~/.local/share/ctx-saver/<hash>.db` — permissions คือ `0600` backup ง่าย และคุณเป็นเจ้าของข้อมูล สำหรับเครื่องมือที่ทำงานท้องถิ่น configuration ที่เป็นศูนย์ ดีกว่า "setup database server"

### ฐานข้อมูลต่อ project คำนวณจาก hash

**ทำไม**: การแยกเก็บ `~/projects/backend` database แยกจาก `~/projects/frontend` Tools และ configs เป็นอิสระต่อ project แต่สามารถ query stats ได้ทั่วทุก project (scope: `all`)

### สรุปแบบ head+tail แทนการสกัด full-text

**ทำไม**: Context ที่มีขอบเขต ใช้ 20 บรรทัดแรก + 5 บรรทัดสุดท้ายของ JSON response 1000 บรรทัด รับประกัน ~100 tokens แทน ~3000 เห็นโครงสร้างและข้อมูลสำคัญทันที สามารถ search รายละเอียดถ้าต้องการ และใช้ context ในสิ่งที่สำคัญ
