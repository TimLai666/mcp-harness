# mcp-harness

`mcp-harness` 是一個讓外部遠端 agent 安全操作本機工作區的 MCP Server。

它的目標不是內建模型，也不是再做一個聊天機器人。它比較像沒有內建模型的 Codex runtime：ChatGPT、Claude 或其他支援 MCP 的外部 agent 負責思考與規劃，`mcp-harness` 負責把檔案、shell、git、skills、toolsets 和專案工作區包成可控的本機 harness。

## 動機

如果 ChatGPT 應用程式可以接 MCP，那本機就可以架一個 MCP Server，把「讀檔、改檔、跑測試、看 git diff、使用 skills、連其他 MCP」這些能力提供給 ChatGPT。

這樣外部 agent 可以像 Codex 一樣處理本機 repo，但模型用量與能力由外部平台提供，`mcp-harness` 只做本機執行層、權限邊界、工作流程提示與操作記錄。

## 目前狀態

這個 repo 目前已有 Go 版 MVP。

已完成：

- `cmd/mcp-harness`：MCP stdio server，保留給本機 MCP host 直接啟動 binary 的情境
- `cmd/mcp-harness-web`：Web UI 控制台與遠端 MCP Streamable HTTP endpoint
- `internal/harness`：prompt 合成、專案解析、沙盒、tool call parser、`@檔案` references、skills loader、基礎 toolsets
- `prompts/main.md`：harness protocol prompt
- `prompts/rules.md`：通用工作規則
- `AGENTS.md`：本 repo 的開發與文件維護規則
- `Dockerfile`、`docker-compose.yml`：Web 控制台部署骨架
- approval queue：`default`、`auto`、`full_access` 三種 access mode，狀態存在 SQLite
- 外接 MCP 設定與呼叫 MVP：支援 `stdio` 與 `streamable_http`
- 遠端 MCP endpoint：`cmd/mcp-harness-web` 會在同一個 HTTP server 掛 `/mcp`，給遠端 MCP client 使用 Streamable HTTP 連線
- 外接 MCP dynamic input schema validation：`mcp.call` 會先列工具並驗證目標 tool 的 `inputSchema`
- tool call history：每一步執行前後 snapshot、diff、version restore
- workspace version snapshot blob store：SQLite 存 metadata，壓縮 snapshot JSON 存在 `MCP_HARNESS_HOME/history/blobs`
- harness-managed workspaces：agent 可透過 `project.create` 建立空工作區，或透過 `project.clone` clone git repo 並註冊成 project；路徑固定在 `MCP_HARNESS_HOME/workspaces`
- skills 與 MCP 設定在同一 session 內會重新讀取，支援熱插拔
- SQLite primary store：`MCP_HARNESS_HOME/harness.db`，啟動時自動 migration，首次建立 DB 時匯入 legacy JSON/JSONL
- project instruction injection：root-level `AGENTS.md`、`CLAUDE.md` 等規範檔會注入 harness context
- 基本 e2e 測試：direct MCP server、外接 stdio MCP schema validation、Web API smoke

尚未完成：

- 多使用者/RBAC 等更完整權限模型
- 大型 snapshot blob GC
- 更細緻的 Codex App 級 Web UI

目前已可本機啟動與測試，但仍是 MVP，不是完整安全產品。

## Quickstart

需求：

- Go 1.23 以上
- Git

跑測試：

```powershell
go test ./...
```

啟動 MCP stdio server，本機 MCP host 直接啟動 binary 時才需要：

```powershell
go run ./cmd/mcp-harness
```

啟動 Web UI 與遠端 MCP endpoint：

```powershell
go run ./cmd/mcp-harness-web
```

然後打開：

```text
http://127.0.0.1:8765
```

遠端 MCP client 連：

```text
http://<host>:8765/mcp
```

Docker 啟動 Web UI 與遠端 MCP endpoint：

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

Web 與遠端 MCP endpoint 的監聽位址可用環境變數調整，預設是 `:8765`：

```powershell
$env:MCP_HARNESS_WEB_ADDR=":8765"
```

若只想開本機連線，可改成：

```powershell
$env:MCP_HARNESS_WEB_ADDR="127.0.0.1:8765"
```

對外開放 `/mcp` 時建議設定 bearer token：

```powershell
$env:MCP_HARNESS_MCP_BEARER_TOKEN="change-this-token"
```

主要資料庫：

```text
~/.mcp-harness/harness.db
```

Harness-managed workspaces 預設放在：

```text
~/.mcp-harness/workspaces
```

若 `harness.db` 不存在，啟動時會從舊的 `projects.json`、`mcps.json`、`approvals/`、`sessions/`、`history/` 匯入一次。舊檔會保留，不會自動刪除。

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

目前列為高風險的操作包含檔案修改、workspace version restore、project registry 變更、建立或 clone harness-managed workspace、MCP 設定變更、呼叫未信任外接 MCP、明顯破壞性的 shell command。approval queue 存在 `MCP_HARNESS_HOME/harness.db`。

### MCP Server

本專案提供同一組 MCP tools 的兩種 transport：

- 遠端主要入口：`cmd/mcp-harness-web` 的 Streamable HTTP endpoint，路徑是 `/mcp`。
- 本機相容入口：`cmd/mcp-harness` 的 stdio server，給只能直接啟動本機 binary 的 MCP host 使用。

遠端 MCP endpoint：

```text
http://<host>:8765/mcp
```

Web 控制台與 REST API 跟 MCP endpoint 共用同一個 HTTP server；控制台是 `/`，REST API 是 `/api/...`，MCP client 只應連 `/mcp`。

這個 server 會暴露少量 ChatGPT 可直接呼叫的 MCP tools；真正會操作本機工作區的入口仍是 `harness`。

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

目前 direct MCP tools：

- `harness`：唯一會執行本機工作流的入口。支援 project、mode、access mode、session、內部 harness tool calls。
- `project_list`：列出已設定專案。
- `skill_list`：列出可用 skills metadata。
- `mcp_list`：列出已設定外接 MCP servers，不 probe、不呼叫外部 server。
- `approval_list`：列出 approval records，不能 approve/reject。
- `history_list`：列出 tool-call history events。
- `history_show`：查看單一 history event 與 diff。
- `history_restore_preview`：預覽 restore 某個 workspace version 會套用的 diff，不修改檔案。

分層原則：查詢型、控制台型能力可以提升成 direct MCP tools；會改檔、跑 shell、真正 restore version、修改 MCP 設定、或呼叫外部 server 的能力，先留在 `harness` 內部 toolsets，讓它們走 mode、approval、history/diff 的完整流程。

安全邊界：`/mcp` 是遠端可連的本機執行入口。預設 access mode 仍會把高風險操作送進 approval queue，但遠端部署時仍應設定 `MCP_HARNESS_MCP_BEARER_TOKEN`，並在公網前面放 TLS reverse proxy。

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

project instructions 已支援 root-level 掃描。Harness 會在每次 turn 讀取目前 workspace 的 `AGENTS.md`、`CLAUDE.md`、`GEMINI.md`、`.github/copilot-instructions.md`，並注入 `project_instructions`。

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

Agent 可在 `harness()` 內透過 project toolset 管理 workspace：

- `project.add`：註冊已存在、且 harness process 看得到的資料夾。
- `project.create`：在 `MCP_HARNESS_HOME/workspaces` 建立空的持久化 workspace，並註冊成 project。
- `project.clone`：用 `git clone` 把 repo clone 到 `MCP_HARNESS_HOME/workspaces`，並註冊成 project。

這三個 tool 都會修改 project registry 或磁碟內容，因此走 access mode 與 approval queue。建立或 clone 完成後，下一輪 `harness()` 可用回傳的 project id 或 path 切到新 workspace。

### Toolsets

Toolset 是工具集合。內建 toolsets 建議先做：

- `workspace`：列目錄、讀檔、搜尋、寫檔、套 patch
- `terminal`：執行命令，預設限制在專案根目錄或沙盒
- `git`：status、diff、log、show、branch 等非破壞性操作
- `project`：列出目前專案、註冊既有路徑、建立空 workspace、clone repo 並註冊 project
- `skill`：列 skills、讀 SKILL.md、載入 skill
- `mcp`：列出、新增、移除、呼叫外接 MCP server
- `history`：列出 tool call history、查看單步 diff、還原 workspace version

外接 MCP 的概念參考 [`TimLai666/agent` 的 `tim` 分支](https://github.com/TimLai666/agent/tree/tim)：內建 MCP、新增的本機 MCP、遠端 MCP 都應該被 namespaced，避免工具名稱互撞。

外接 MCP 設定存在 `MCP_HARNESS_HOME/harness.db`。舊版 `MCP_HARNESS_HOME/mcps.json` 仍可在首次建立 DB 時匯入：

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

`mcp.list` 每次呼叫都會重新讀 SQLite store；`mcp.call` 每次呼叫都會建立新的 MCP client session，並先 `ListTools` 驗證目標 tool 是否存在與 `inputSchema` 是否符合。因此同一個對話中用 `mcp.add`、`mcp.remove` 或 Web API 修改 MCP server，下一步就會生效。舊的 `mcps.json` 只作為 `harness.db` 首次建立時的 legacy import 來源。

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

- 掃描順序是 repo-local `skills/`、`MCP_HARNESS_HOME/skills`、user home `.agents/skills`、user home `.claude/skills`
- 啟動時只載入 metadata，避免 prompt 太肥
- 命中 skill 後才讀完整 `SKILL.md`
- `scripts/`、`references/`、`assets/` 只在 skill 指示需要時讀取
- Agent 應只啟用任務需要的 skills，避免 prompt 過大
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

目前 snapshot metadata 存在 SQLite，壓縮 snapshot JSON 存在 `MCP_HARNESS_HOME/history/blobs`。它會跳過 `.git`、`node_modules`、`vendor`、`dist`、`build` 等大型目錄，只保存文字檔內容；大型檔、二進位檔或超過上限的內容會標記為 omitted，因此不保證可完整還原這些檔案。尚未做 blob GC。

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

Web UI 的定位是控制台，不是聊天或任務輸入介面。MVP 已提供：

- 專案列表
- 新增專案
- 查看 project/sandbox 狀態
- 查看 sessions、turns、tool calls
- 顯示 approval queue 並可核准或拒絕
- 顯示 MCP servers
- 顯示 skills metadata
- 顯示 per-project tool history、每一步 diff，並可 restore before/after version
- restore 前先 preview diff

它不提供聊天框，也不直接啟動 agent workflow。遠端 agent 仍透過 MCP `harness()` 執行任務；Web UI 只做控制台與審核。

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
  - mcp-harness-data:/data
  - mcp-harness-agents-skills:/root/.agents/skills
  - C:/Users/tingz/Documents/GitHub:/workspace/GitHub
```

Compose 內的 `MCP_HARNESS_HOME` 是 `/data`，所以 SQLite DB、MCP 設定、approval/history/session、snapshot blobs、sandbox，以及 `project.create` / `project.clone` 建立的 `/data/workspaces` 都會由 `mcp-harness-data` volume 持久化。容器 user home 的 `.agents/skills` 會由 `mcp-harness-agents-skills` volume 持久化。

Compose 對外開的是 Web 控制台和遠端 MCP endpoint。預設 endpoint 是：

```text
http://<docker-host>:8765/mcp
```

若要從不可信網路連進來，請設定 `MCP_HARNESS_MCP_BEARER_TOKEN`，並用 reverse proxy 補 TLS。MCP stdio server 仍保留給本機 MCP host 直接啟動 binary 的情境，不是遠端部署主路徑。

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

狀態：toolset registry、skills loader、內建工具輕量 schema validation、外接 MCP 設定與呼叫、外接 MCP tools 動態 input schema validation 已完成 MVP。

### M4：Web UI 控制台

- 專案管理
- session log
- tool call timeline
- diff 檢視
- approvals queue

狀態：已完成控制台型 Web UI、遠端 MCP Streamable HTTP endpoint、approval queue、MCP server 清單、skills metadata、session/tool call timeline、tool history、diff 與 restore preview MVP；更細緻的 Codex App 級 UI 還未完成。

### M5：Docker 與安全政策

- Docker Compose
- 資料庫
- 權限設定
- audit log
- 基本 e2e 測試

狀態：Docker Compose、SQLite primary store、legacy JSON/JSONL import、approval/history/session store、project `allowed_toolsets` enforcement、harness-managed workspace create/clone、基本 MCP/Web e2e 測試已完成；多使用者/RBAC、snapshot blob GC、完整瀏覽器 e2e 尚未完成。

## 開發原則

- 文件不能宣稱尚未實作的功能已可用
- prompt 與 parser 規格要同步
- 每個 toolset 都要有 schema、錯誤格式與測試
- 優先做最小可驗證版本，不先做大型抽象
- 對外部 agent 暴露的介面要穩定，內部實作可以迭代
