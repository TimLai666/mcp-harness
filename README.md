# mcp-harness

`mcp-harness` 是一個讓外部遠端 agent 安全操作本機工作區的 MCP Server。

它的目標不是內建模型，也不是再做一個聊天機器人。它比較像沒有內建模型的 Codex runtime：ChatGPT、Claude 或其他支援 MCP 的外部 agent 負責思考與規劃，`mcp-harness` 負責把檔案、shell、git、skills、toolsets 和專案工作區包成可控的本機 harness。

## 動機

如果 ChatGPT 應用程式可以接 MCP，那本機就可以架一個 MCP Server，把「讀檔、改檔、跑測試、看 git diff、使用 skills、連其他 MCP」這些能力提供給 ChatGPT。

這樣外部 agent 可以像 Codex 一樣處理本機 repo，但模型用量與能力由外部平台提供，`mcp-harness` 只做本機執行層、權限邊界、工作流程提示與操作記錄。

## 目前狀態

這個 repo 目前已有 Go 版 MVP。

已完成：

- `cmd/mcp-harness`：MCP stdio server，暴露 `harness` tool
- `cmd/mcp-harness-web`：Web UI 控制台
- `internal/harness`：prompt 合成、專案解析、沙盒、tool call parser、`@檔案` references、skills loader、基礎 toolsets
- `prompts/main.md`：harness protocol prompt
- `prompts/rules.md`：通用工作規則
- `AGENTS.md`：本 repo 的開發與文件維護規則
- `Dockerfile`、`docker-compose.yml`：Web 控制台部署骨架
- 檔案式 approval queue：`default`、`auto`、`full_access` 三種 access mode
- 外接 MCP 設定與呼叫 MVP：支援 `stdio` 與 `streamable_http`
- tool call history：每一步執行前後 snapshot、diff、version restore
- skills 與 MCP 設定在同一 session 內會重新讀取，支援熱插拔

尚未完成：

- 專案資料庫與權限設定
- 外接 MCP tools 的動態 schema validation
- 大型 workspace 的高效 snapshot/diff store
- 完整 e2e 測試

目前已可本機啟動與測試，但仍是 MVP，不是完整安全產品。

## Quickstart

需求：

- Go 1.23 以上
- Git

跑測試：

```powershell
go test ./...
```

啟動 MCP stdio server：

```powershell
go run ./cmd/mcp-harness
```

啟動 Web UI：

```powershell
go run ./cmd/mcp-harness-web
```

然後打開：

```text
http://127.0.0.1:8765
```

Docker 啟動 Web UI：

```powershell
docker compose up --build
```

設定資料預設放在：

```text
~/.mcp-harness
```

可用環境變數改位置：

```powershell
$env:MCP_HARNESS_HOME="C:\path\to\data"
```

Web 監聽位址可用環境變數調整：

```powershell
$env:MCP_HARNESS_WEB_ADDR="127.0.0.1:8765"
```

## 核心概念

### Harness

Harness 是本機執行層。外部 agent 透過 MCP 呼叫 `harness()`，把自然語言指令與 harness tool call 傳進來。

Harness 會：

- 注入 system prompt、專案上下文、可用 toolsets、可用 skills
- 解析使用者訊息中的 `@檔案` references，並在可行時注入檔案內容
- 解析 harness tool call
- 在選定專案或預設沙盒中執行讀寫、搜尋、shell、git 等操作
- 回傳工具結果、錯誤、狀態與下一步提示
- 記錄每次操作，供 Web UI 檢視與審核
- 對每個 tool call 擷取執行前後 snapshot；即使檔案是被 `terminal.run` 改到，也會計算 diff

Harness 不會：

- 自己呼叫模型
- 自己決定高風險動作
- 在未授權的路徑任意操作
- 把 mock 結果當成真實驗證

### Access Mode

`harness()` 支援三種 access mode：

- `default`：預設模式。高風險操作會進 approval queue，不會直接執行。
- `auto`：類似 Codex auto mode。agent 可以在使用者已明確授權，且工具參數含 `user_authorized: true` 與 `approval_reason` 時自行執行高風險操作。
- `full_access`：完整存取權。高風險操作直接執行，適合使用者已明確把本次 session 交給 agent 的情境。

目前列為高風險的操作包含檔案修改、workspace version restore、MCP 設定變更、呼叫未信任外接 MCP、明顯破壞性的 shell command。approval queue 存在 `MCP_HARNESS_HOME/approvals`。

### MCP Server

本專案預計只提供一個 MCP Server。第一版至少暴露一個主要工具：

```json
{
  "name": "harness",
  "description": "Run a local harness turn for a selected project or sandbox. The external agent may include harness tool calls in the message.",
  "input": {
    "project": "optional project id, alias, or absolute path",
    "message": "natural language instructions and zero or more harness tool calls",
    "mode": "inspect | work",
    "access_mode": "default | auto | full_access",
    "session_id": "optional existing session id"
  }
}
```

設計重點是讓外部 agent 只需要知道一個 MCP 工具，但工具內部可以再透過 harness tool call 操作多個本機 toolsets。

### Prompt 合成

目前已實作單向合成，不在多份 prompt 重複同一件事：

1. `prompts/rules.md`：通用行為規則，例如確認、驗證、回報格式、語言風格。
2. `prompts/main.md`：harness 專屬協議，例如 context 注入、`@檔案`、tool call 格式、toolsets、skills。
3. skills：只注入 metadata，`skill.use` 才回完整 `SKILL.md`。
4. runtime context：project、sandbox、tool catalog、referenced files、observations。

合成順序建議：

```text
system:
  rules.md
  main.md
  harness runtime context

user:
  original user message
```

project instructions 尚未實作掃描。下一步應補 AGENTS/CLAUDE/README 類專案規範注入。

若兩份 prompt 需要講到同一主題，保留一個權威來源，另一份只用一句話指向來源。例如 `main.md` 只說「通用行為由 rules prompt 提供」，不要再重寫驗證、回報格式或工作模式。

### Projects

Web UI 會管理多個專案。每個專案包含：

- `id`：穩定識別碼
- `name`：使用者自訂名稱
- `path`：本機資料夾路徑
- `description`：可選，給 agent 看
- `default_mode`：預設工作模式，例如 inspect 或 work
- `allowed_toolsets`：這個專案允許使用的 toolsets

Agent 需要同時看得到 `name` 和 `path`。`name` 是人類可讀的任務脈絡，`path` 是實際工作邊界。

沒有指定專案時，harness 使用預設沙盒。沙盒可用來做臨時檔案、草稿、測試資料或不屬於任何 repo 的任務。

### Toolsets

Toolset 是工具集合。內建 toolsets 建議先做：

- `workspace`：列目錄、讀檔、搜尋、寫檔、套 patch
- `terminal`：執行命令，預設限制在專案根目錄或沙盒
- `git`：status、diff、log、show、branch 等非破壞性操作
- `project`：列出專案、切換專案、讀取專案資訊
- `skill`：列 skills、讀 SKILL.md、載入 skill
- `mcp`：列出、新增、移除、呼叫外接 MCP server
- `history`：列出 tool call history、查看單步 diff、還原 workspace version

外接 MCP 的概念參考 [`TimLai666/agent` 的 `tim` 分支](https://github.com/TimLai666/agent/tree/tim)：內建 MCP、新增的本機 MCP、遠端 MCP 都應該被 namespaced，避免工具名稱互撞。

外接 MCP 設定存在 `MCP_HARNESS_HOME/mcps.json`：

```json
{
  "servers": [
    {
      "id": "browser",
      "name": "Browser",
      "transport": "stdio",
      "command": "node",
      "args": ["server.js"],
      "trusted": false
    }
  ]
}
```

`mcp.list` 每次呼叫都會重新讀設定；`mcp.call` 每次呼叫都會建立新的 MCP client session，所以同一個對話中修改 `mcps.json` 或用 `mcp.add` 新增 server，下一步就會生效。

### Skills

Skills 採 Claude Code 相容的資料夾格式：

```text
skills/
  my-skill/
    SKILL.md
    scripts/
    references/
    assets/
```

`SKILL.md` 必須有 YAML frontmatter：

```markdown
---
name: my-skill
description: Use when ...
---

# My Skill

Instructions...
```

載入策略：

- 啟動時只載入 metadata，避免 prompt 太肥
- 命中 skill 後才讀完整 `SKILL.md`
- `scripts/`、`references/`、`assets/` 只在 skill 指示需要時讀取
- 同一輪最多自動啟用 3 個 skills
- 已啟用 skill 會記在 session state；每次 prompt 合成時重新讀 `SKILL.md`，所以同一個對話中修改 skill 會立即生效

這個方向同樣參考 [`TimLai666/agent` 的 skills loader 做法](https://github.com/TimLai666/agent/tree/tim)。

## Harness Tool Call 設計

建議不用括號式格式，改用專用 XML tag 包一個 JSON object：

建議格式：

```text
<harness_tool_call>
{"tool":"toolset.tool","args":{"key":"value"}}
</harness_tool_call>
```

無參數工具：

```text
<harness_tool_call>
{"tool":"git.status","args":{}}
</harness_tool_call>
```

這比括號式工具呼叫更適合 LLM，理由是：

- XML-like tag 是模型常見的輸出模式，起訖邊界清楚。
- JSON object 有欄位名稱，模型較不容易把參數順序搞錯。
- parser 可以先抓完整 `<harness_tool_call>` block，再做 JSON decode 與 schema validation。
- `tool` 和 `args` 固定欄位比 `toolset.tool(...)` 裡面混語法更容易報錯與修正。
- 要避免誤判時，只接受獨立起訖行，不接受 inline tag。

### Tool Call 解析規則

Parser 應只接受下列格式：

```text
<harness_tool_call>
{"tool":"workspace.read_file","args":{"path":"README.md"}}
</harness_tool_call>
```

規則：

- opening tag 必須是獨立一行 `<harness_tool_call>`
- closing tag 必須是獨立一行 `</harness_tool_call>`
- block body 必須是單一 JSON object
- JSON 必須包含 `tool` 與 `args`
- `tool` 必須符合 `toolset.tool`
- `toolset` 與 `tool` 只允許小寫英數與底線
- `args` 必須是 JSON object
- 不接受 inline tag、markdown code fence、positional args
- 不接受 unknown toolset 或 unknown tool
- 執行前先用 schema 驗證參數
- 執行結果要回傳結構化 observation，不只回傳純文字

### Tool Call 範例

讀檔：

```text
<harness_tool_call>
{"tool":"workspace.read_file","args":{"path":"README.md"}}
</harness_tool_call>
```

搜尋：

```text
<harness_tool_call>
{"tool":"workspace.search","args":{"pattern":"harness","glob":"**/*.md"}}
</harness_tool_call>
```

套 patch：

```text
<harness_tool_call>
{"tool":"workspace.apply_patch","args":{"patch":"*** Begin Patch\n*** Update File: README.md\n@@\n-# old\n+# new\n*** End Patch\n"}}
</harness_tool_call>
```

跑測試：

```text
<harness_tool_call>
{"tool":"terminal.run","args":{"command":"npm test","timeout_ms":120000}}
</harness_tool_call>
```

使用 skill：

```text
<harness_tool_call>
{"tool":"skill.use","args":{"name":"code-review","reason":"The user asked for a code review."}}
</harness_tool_call>
```

呼叫外接 MCP：

```text
<harness_tool_call>
{"tool":"mcp.call","args":{"server":"browser","tool":"screenshot","arguments":{"url":"http://localhost:3000"}}}
</harness_tool_call>
```

查詢 history：

```text
<harness_tool_call>
{"tool":"history.list","args":{"current_project":true,"limit":20,"include_diff":true}}
</harness_tool_call>
```

還原到某個 version：

```text
<harness_tool_call>
{"tool":"history.restore","args":{"version_id":"hist-..."}}
</harness_tool_call>
```

`history.restore` 會修改檔案，在 `default` 模式會先進 approval queue。

## History、Diff 與 Restore

每個 harness tool call 都會記錄一筆 `HistoryEvent`：

- `before_version`：工具執行前的 workspace snapshot
- `after_version`：工具執行後的 workspace snapshot
- `diff`：前後 snapshot 的文字 diff
- `tool`、`args`、`status`、`error`：工具呼叫資訊

這個機制包在 tool call 外層，所以 `workspace.write_file`、`workspace.apply_patch`、`terminal.run` 或 `history.restore` 改檔都會留下 diff。

目前 snapshot 是檔案式 MVP，存在 `MCP_HARNESS_HOME/history`。它會跳過 `.git`、`node_modules`、`vendor`、`dist`、`build` 等大型目錄，只保存文字檔內容；大型檔、二進位檔或超過上限的內容會標記為 omitted，因此不保證可完整還原這些檔案。

## `@檔案` references

使用者可以在訊息中用 `@` 指定檔案或資料夾：

```text
請讀 @README.md，把定位寫清楚。
比對 @prompts/main.md 和 @prompts/rules.md。
更新 @"docs/product brief.md" 裡的架構段落。
```

建議由 harness 在呼叫 agent 前先處理：

- 支援 `@path` 和 `@"path with spaces"`。
- 路徑預設相對於目前 project root 或 sandbox。
- 小型文字檔可以直接注入 prompt 的 `referenced_files`。
- 大檔、二進位檔或目錄只注入 metadata，讓 agent 再用工具做 bounded read 或列目錄。
- 若 reference 超出目前 project/sandbox，除非已明確授權，否則拒絕解析。

這能減少外部 agent 猜路徑或忘記讀檔的機率，也讓 UI 之後可以做類似 Codex App 的檔案提及體驗。

## Web UI 控制台

Web UI 的定位是控制台，不是聊天玩具。MVP 已提供：

- 專案列表
- 新增專案
- 選 project/sandbox
- 選 inspect/work mode
- 選 access mode
- 呼叫 harness
- 顯示 JSON 結果
- 顯示 approval queue 並可核准或拒絕
- 顯示 MCP servers
- 顯示 per-project tool history、每一步 diff，並可 restore before/after version

它還不是最終 UI，但已可用來測 harness 流程。

後續完整控制台要像 Codex App 一樣能管理多個工作區，但第一畫面應以操作效率為主。

第一版建議頁面：

- Projects：專案清單、名稱、路徑、狀態、最近 sessions
- Project Detail：專案設定、允許 toolsets、skills、沙盒路徑
- Sessions：每次外部 agent 呼叫的 transcript、tool calls、diff、驗證結果
- Approvals：待確認的高風險操作
- Toolsets：內建與外接 MCP 狀態
- Skills：已安裝 skills、metadata、測試命中
- Settings：Docker、資料目錄、預設沙盒、權限政策

UI 風格建議：

- 安靜、密集、可掃描，不做行銷式 landing page
- 左側專案列表，中間 session 與檔案/任務狀態，右側 detail panel
- 工具呼叫用 timeline 呈現，清楚標出 pending、running、done、failed
- diff、命令輸出、錯誤訊息要能展開查看
- 高風險操作用明確 approval queue，不混在聊天內容裡

## 安全邊界

這個專案本質上是「讓遠端 agent 操作本機」，安全邊界必須從第一版就設計進去。

最低要求：

- 每個專案有明確 root，檔案操作不得越界
- 預設 sandbox 與真實專案分開
- destructive 操作必須可攔截，例如刪除、覆寫大量檔案、reset、clean、force push
- 所有 tool call 都要有 audit log
- 所有 tool call 都要有 step-level diff；使用 shell 改檔也要被 snapshot 捕捉
- shell command 要有 timeout、cwd、輸出大小限制
- 外接 MCP 必須 namespaced
- secret 檔案與常見敏感路徑預設唯讀或拒絕

第一版可以先不做完整權限模型，但不能把「全機無限制操作」當成預設。

## 部署

目前已提供 Dockerfile 與 Compose：

```text
docker compose up --build
```

Web UI 會在 container 內看見 `/data`。如果要讓 container 操作本機 repo，需要明確 mount 專案目錄：

```yaml
volumes:
  - C:/Users/tingz/Documents/GitHub:/workspace/GitHub
```

MCP stdio server 通常由 MCP host 直接啟動 binary，比較不適合長駐在 Compose 裡。

## 建議里程碑

### M1：規格與 Prompt

- 補齊 `AGENTS.md`
- 補齊 `README.md`
- 完成 `prompts/main.md`
- 定義 harness tool call parser 規則與 toolset catalog

### M2：MCP Server 最小版

- 實作 `harness()` MCP tool
- 實作 project sandbox
- 實作 `workspace.read_file`、`workspace.search`、`workspace.apply_patch`
- 回傳結構化 observation

狀態：已完成 MVP。

### M3：Toolsets 與 Skills

- 實作 toolset registry
- 實作 skills loader
- 支援外接 MCP namespacing
- 補 tool schema validation

狀態：toolset registry、skills loader、內建工具輕量 schema validation、外接 MCP 設定與呼叫已完成 MVP；外接 MCP tools 的動態 schema validation 尚未完成。

### M4：Web UI 控制台

- 專案管理
- session log
- tool call timeline
- diff 檢視
- approvals queue

狀態：已完成可操作 Web UI 骨架、approval queue、MCP server 清單、tool history、diff 與 restore MVP；完整 session transcript 與更細緻的 UI 還未完成。

### M5：Docker 與安全政策

- Docker Compose
- 資料庫
- 權限設定
- audit log
- 基本 e2e 測試

狀態：Docker Compose、JSONL session log、檔案式 approval/history store 已完成；資料庫、完整權限政策、e2e 尚未完成。

## 開發原則

- 文件不能宣稱尚未實作的功能已可用
- prompt 與 parser 規格要同步
- 每個 toolset 都要有 schema、錯誤格式與測試
- 優先做最小可驗證版本，不先做大型抽象
- 對外部 agent 暴露的介面要穩定，內部實作可以迭代
