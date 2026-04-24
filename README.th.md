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

> **ผู้ใช้ตัวเลือก A** (`go install`): เปลี่ยน path ด้านบนเป็น `$(go env GOPATH)/bin/ctx-saver`
> หรือจะรัน `./scripts/install-hooks.sh copilot` ก็ได้ — script จะตรวจหา path ที่ถูกต้องอัตโนมัติ

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

### ติดตั้ง hooks สำหรับ Claude และลงทะเบียน server สำหรับ Copilot

hooks ของ Claude ช่วยให้ระบบ routing คำสั่งที่มี output ขนาดใหญ่และการกู้คืน session history ทำงานอัตโนมัติ
สำหรับ VS Code Copilot ขั้นตอนนี้เป็นการลงทะเบียน `ctx-saver` MCP server เท่านั้น

```bash
# Claude Code
./scripts/install-hooks.sh claude

# VS Code Copilot (รันจาก project root; ลงทะเบียนเฉพาะ MCP server)
./scripts/install-hooks.sh copilot
```

ถ้าติดตั้งผ่าน `go install` และไม่ได้ clone repository นี้ ให้ใช้ shallow clone ชั่วคราวเพื่อเรียกสคริปต์:

```bash
tmp="$(mktemp -d)"
git clone --depth 1 https://github.com/ChonlakanSutthimatmongkhol/ctx-saver.git "$tmp"

# ติดตั้ง hooks สำหรับ Claude Code
"$tmp/scripts/install-hooks.sh" claude

# ติดตั้ง server entry สำหรับ VS Code Copilot (ให้รันจาก project root ของคุณ)
cd /path/to/your/project
"$tmp/scripts/install-hooks.sh" copilot

rm -rf "$tmp"
```

Script จะตรวจหา binary path อัตโนมัติ สำรองค่า config เดิม และ merge JSON อย่างปลอดภัยโดยไม่เขียนทับ settings ที่ไม่เกี่ยวข้อง ต้องการ `jq` (`brew install jq` / `apt-get install jq`)

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

## Tools

| Tool | วัตถุประสงค์ |
|------|-------------|
| `ctx_execute` | รันคำสั่ง shell/python/go/node; output ขนาดใหญ่จะถูกเก็บและสรุปย่อ แสดง `duplicate_hint` ถ้ารันคำสั่งเดิมภายใน 30 นาทีที่ผ่านมา |
| `ctx_read_file` | อ่านไฟล์ โดยเลือกส่งผ่าน processing script ได้ |
| `ctx_outline` | ดึง headings / สารบัญจาก stored output เพื่อเลือกคำค้นหาก่อน search |
| `ctx_get_section` | ดึง section เฉพาะด้วย heading text (ใช้หลัง `ctx_outline` สำหรับเอกสารยาว) |
| `ctx_search` | FTS5 full-text search ในทุก output ที่เก็บไว้ (รองรับ `context_lines`). อักขระพิเศษ escape อัตโนมัติ; fallback ไป LIKE. ขยาย query ด้วย synonym อัตโนมัติ (เช่น `api_path` → endpoint, route…). กำหนด synonym ของโปรเจกต์ผ่าน `.ctx-saver-synonyms.yaml`. |
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

## ตัวเลขใช้งานจริง: ลด token ได้เท่าไร

วัดจากคำสั่งจริงที่รันผ่าน `ctx_execute` ใน repository นี้

Benchmark snapshot (`2026-04-23`): `go test -race -v ./internal/summary/...` และ `cat README.md`

สมมติฐานสำหรับประมาณ token: `1 token ≈ 4 bytes`

| คำสั่ง | Raw output (bytes) | Summary ที่ส่งกลับ (bytes) | Bytes ที่ประหยัดได้ | Token ที่ประหยัดได้ (ประมาณ) |
|--------|---------------------|-----------------------------|----------------------|-------------------------------|
| `go test -race -v ./internal/summary/...` | 5,640 | 110 | 5,530 | ~1,383 |
| `cat README.md` | 10,135 | 173 | 9,962 | ~2,490 |
| **รวม** | **15,775** | **283** | **15,492** | **~3,873** |

ผลรวมรอบนี้ลดได้ **98.21%** (จาก 15,775 bytes เหลือ 283 bytes)

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
internal/summary/              head+tail+stats summariser
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
