# AGENTS.md

本 repo 目前已有 Go 版 MVP。工作時先把它當成「MCP harness runtime + Web 控制台」專案。SQLite primary store、approval queue、外接 MCP client、內建工具 schema validation、外接 MCP dynamic input schema validation、tool history/diff/restore、基本 MCP/Web e2e 都已有 MVP；不要把多使用者/RBAC、blob GC 或完整瀏覽器 e2e 說成已完成。

## 專案定位

`mcp-harness` 要做的是一個本機 MCP Server，讓 ChatGPT、Claude 或其他外部遠端 agent 操作本機專案。每個能力都是獨立、有結構化參數的 direct MCP tool（`workspace_*`、`terminal_run`、`git_*`、`project_*`、`use_skill`、`mcp_*`、`history_*`）。`harness` 本身只回傳協議 prompt 與環境概況，不執行任何本機工作。

不要再使用 DSL：不要把能力包成一個吃自然語言的 `harness(message)`，也不要在訊息裡塞 `<harness_tool_call>` JSON block。OpenAI 等平台的安全層會把這種「萬能本機執行器」形狀擋掉。能力要拆成名稱明確、參數結構化的窄工具。

核心邊界：

- 外部 agent 負責推理與規劃。
- `mcp-harness` 負責本機執行、檔案操作、工具註冊、skills 載入、專案邊界、audit log。
- 每個會改檔的 tool call 都要記錄 history 與 step-level diff；即使檔案是被 `terminal_run` 改到，也要由前後 snapshot 算出 diff。
- Harness 本身不內建模型。
- Web UI 是控制台，用來管理 projects、sessions、toolsets、skills、approvals、access policy，不是行銷頁，也不是聊天或任務輸入介面。
- 權限控制在 Web UI / 伺服器端：operator 設定 access policy（`default` 或 `full_access`），agent 不能用參數自行提權，也沒有 access-mode 參數。高風險操作預設進 approval queue。

## 目前檔案

- `README.md`：產品定位、架構、harness tool call、里程碑。
- `prompts/main.md`：外部 agent 透過 harness 工作時的 runtime prompt。
- `prompts/rules.md`：使用者通用工作規則。
- `AGENTS.md`：本 repo 的協作與維護規則。
- `cmd/mcp-harness`：MCP stdio server。
- `cmd/mcp-harness-web`：Web UI 控制台與遠端 MCP endpoint。
- `internal/harness`：核心 runtime、單一工具執行、toolsets、skills、prompt、event broker。
- `internal/harness/events.go`：in-process event broker，串流 `terminal_run` 輸出與 tool/approval/history/project 事件給 Web UI；project registry 變更(建立/clone/新增/改名/重定位/刪除)都會推 `project` 事件讓控制台即時更新。
- `internal/mcpserver`：direct MCP tool 註冊與名稱轉換(`exec()`)。
- `internal/web`：Web API、SSE(`/api/events`)、HTML 控制台。
- `MCP_HARNESS_HOME/harness.db`：SQLite primary store。
- `MCP_HARNESS_HOME/history/blobs`：workspace version snapshot blob store。
- `MCP_HARNESS_HOME/mcps.json`：legacy 外接 MCP server 設定匯入來源；DB 建好後以 SQLite 為主。

如果新增實作，請同步更新 README 的「目前狀態」與里程碑，不要讓文件宣稱未實作功能已可用。

## 語言

預設使用台灣繁體中文。避免中國用語與翻譯腔。

程式碼、工具名稱、schema 欄位、MCP 名稱、harness tool call keyword 保持英文。

## 技術棧

- Go 是主要實作語言。
- MCP server 使用官方 `github.com/modelcontextprotocol/go-sdk`。
- Web UI 目前使用 Go standard library `net/http`，不要未經確認改成前端框架。
- 設定、session、approval、history MVP 以 `~/.mcp-harness/harness.db` 為主；舊 JSON/JSONL 檔只作首次 DB 建立時的 legacy import 來源。

## 文件原則

- 實作前先把介面與安全邊界寫清楚。
- 文件要區分「已完成」、「預計」、「尚未實作」。
- 不要用 mock 或概念描述包裝成已驗證功能。
- 新增或修改 direct MCP tool 時，要同步更新 README 與 `prompts/main.md`。
- 修改 access policy、history、MCP、skills hot-reload 行為時，要同步更新 README 與 `prompts/main.md`。

## Direct MCP Tool 設計規則

每個能力是一個 direct MCP tool，由 `internal/mcpserver/server.go` 註冊，參數用 Go struct 定義、由 SDK 反射成 input schema。

維護時請遵守：

- tool 名稱用小寫英數與底線，動詞或 `namespace_動作` 形式，要能一眼看出它做什麼，例如 `workspace_read_file`、`project_clone`、`use_skill`。
- 不要做吃自然語言任務的萬能工具；唯一的 shell 入口是 `terminal_run`，只接受單一 `command`，不要再開 `script`、`args`、`terminal_input` 這類任意執行介面。
- 不要暴露 `access_mode`、`full_access`、`user_authorized` 這類讓 agent 自行提權的參數。權限由伺服器端 access policy 與 approval queue 控制。
- 公開的 MCP tool 名稱（底線式）對應內部 toolset 名稱（`toolset.tool` 點式）；`exec()` 負責轉換，內部 registry、schema、history 仍用點式名稱。
- 高風險工具要支援 `approval_id`：未核准時回傳 `approval_required`，operator 在 Web UI 核准後，agent 用相同參數加上 `approval_id` 重打。
- 每個工具的參數仍要走 `ValidateToolArgs` 的 schema 驗證。
- `harness` tool 只回傳 prompt 與概況，永遠不執行本機工作。

不要把能力重新包回 DSL、`message` 自然語言或 `<harness_tool_call>` block。

## 參考來源

使用者指定可參考 `https://github.com/TimLai666/agent/tree/tim`。

目前只借用方向：

- skills 使用 `skills/*/SKILL.md` 與 YAML frontmatter。
- skills 採 progressive disclosure，先載 metadata，命中後再讀完整內容。
- toolsets/MCP 需要 namespace，避免外接 MCP 工具名稱互撞。

不要照搬該 repo 的模型 provider、GUI、記憶系統或 CLI 實作，除非任務明確要求。

## 開發原則

- 先讀現有檔案，再修改。
- 小步改動，保留清楚 diff。
- 不新增依賴、不選框架、不改資料儲存方式，除非使用者確認或 README 已列為下一階段。
- 不做 destructive git 或檔案操作。
- 若要新增 MCP Server、Web UI、資料庫或 Docker 實作，先提出短計畫與取捨。

## 驗證

文件修改至少要做：

- 讀回修改後檔案，確認內容完整。
- 檢查是否仍有明顯 placeholder。
- 用 `git diff --check` 檢查空白問題。

程式實作後才加入對應測試、lint、build 或 MCP client smoke test。

目前至少要跑：

```powershell
gofmt -w cmd internal
go test ./...
go build ./cmd/mcp-harness
go build ./cmd/mcp-harness-web
```
