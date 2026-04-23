# ctx-saver

[English](README.md) | **ภาษาไทย**

MCP server แบบ self-hosted (Go) ที่ช่วยลดการใช้ context window ของ AI โดยการแยกเก็บ output ขนาดใหญ่และคืนค่าเฉพาะสรุปย่อแทน

## ทำไมต้องใช้

คำสั่งอย่าง `jira issue list`, `kubectl get pods`, และ `git log` สร้าง output หลายกิโลไบต์ที่กิน context window หมด ctx-saver จะดักจับ output เหล่านั้น เก็บไว้ใน SQLite database ที่มี FTS5 full-text indexing และคืนค่าแค่สรุปย่อ (head + tail) หากต้องการข้อมูลเพิ่มเติมให้ใช้ `ctx_search` หรือ `ctx_get_full`

**ไม่มีคลาวด์ ไม่มีการติดตาม ไม่ต้องสมัครบัญชี โค้ดตรวจสอบได้ 100%**

## เริ่มต้นใช้งาน (5 นาที)

### ตัวเลือก A — go install (ต้องการ Go 1.25+)

```bash
# ติดตั้ง release ล่าสุด
go install github.com/ChonlakanSutthimatmongkhol/ctx-saver/cmd/ctx-saver@latest

# หรือระบุ version ที่ต้องการ
go install github.com/ChonlakanSutthimatmongkhol/ctx-saver/cmd/ctx-saver@v0.1.0
```

ไฟล์ binary จะอยู่ที่ `$(go env GOPATH)/bin/ctx-saver`

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

ตรวจสอบ: Command Palette → **MCP: List Servers** — ควรแสดง `ctx-saver` พร้อม 7 tools

### ติดตั้ง hooks (ไม่บังคับแต่แนะนำ)

Hooks ช่วยให้ระบบ routing คำสั่งที่มี output ขนาดใหญ่และการกู้คืน session history ทำงานอัตโนมัติ

```bash
# Claude Code
./scripts/install-hooks.sh claude

# VS Code Copilot (รันจาก project root)
./scripts/install-hooks.sh copilot
```

Script จะตรวจหา binary path อัตโนมัติ สำรองค่า config เดิม และ merge hooks JSON โดยไม่เขียนทับ settings ที่ไม่เกี่ยวข้อง ต้องการ `jq` (`brew install jq` / `apt-get install jq`)

ลบ hooks:
```bash
./scripts/uninstall-hooks.sh claude   # หรือ copilot
```

ดูรายละเอียดเพิ่มเติมที่ [พฤติกรรมของ Hook](#hooks) ด้านล่าง

## Tools

| Tool | วัตถุประสงค์ |
|------|-------------|
| `ctx_execute` | รันคำสั่ง shell/python/go/node; output ขนาดใหญ่จะถูกเก็บและสรุปย่อ |
| `ctx_read_file` | อ่านไฟล์ โดยเลือกส่งผ่าน processing script ได้ |
| `ctx_outline` | ดึง headings / สารบัญจาก stored output เพื่อเลือกคำค้นหาก่อน search |
| `ctx_search` | FTS5 full-text search ในทุก output ที่เก็บไว้ (รองรับ `context_lines`) |
| `ctx_list_outputs` | แสดง output ทั้งหมดที่เก็บไว้ใน project นี้ |
| `ctx_get_full` | ดึง output ฉบับเต็มหรือระบุช่วงบรรทัด |
| `ctx_stats` | รายงานสถิติการเก็บข้อมูลและ hook activity (scope: `session\|today\|7d\|all`) |

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
  auto_index_threshold_bytes: 5120   # 5 KB

logging:
  level: info
  file: ~/.local/share/ctx-saver/server.log

hooks:
  session_history_limit: 10   # จำนวน event สูงสุดที่ inject เข้า SessionStart context

deny_commands:
  - "rm -rf /"
  - "sudo *"
  - "dd if=*"
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

## Build

```bash
# ต้องการ Go 1.25+
make build          # → bin/ctx-saver
make test           # unit tests + coverage
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
internal/summary/              head+tail+stats summariser
internal/handlers/             one file per MCP tool
internal/hooks/                PreToolUse / PostToolUse / SessionStart hooks
internal/server/               MCP server wiring
tests/                         integration tests + testdata
scripts/                       install.sh, install-hooks.sh, uninstall-hooks.sh, benchmark.sh
configs/                       setup guides and hook config templates per platform
```

## Roadmap

- **Phase 1:** subprocess sandbox, SQLite FTS5, 5 MCP tools
- **Phase 2:** Anthropic `srt` OS-level sandbox (toggle via `sandbox.use_srt: true`)
- **Phase 3 (ปัจจุบัน):** Lifecycle hooks — routing enforcement, session capture, context restoration
