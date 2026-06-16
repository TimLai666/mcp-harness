# AGENTS.md

本 repo 目前已有 Go 版 MVP。工作時先把它當成「MCP harness runtime + Web 控制台」專案，不要把尚未完成的 approval queue、外接 MCP client 或完整資料庫說成已完成。

## 專案定位

`mcp-harness` 要做的是一個本機 MCP Server，讓 ChatGPT、Claude 或其他外部遠端 agent 透過單一 `harness()` 工具操作本機專案。

核心邊界：

- 外部 agent 負責推理與規劃。
- `mcp-harness` 負責本機執行、檔案操作、工具註冊、skills 載入、專案邊界、audit log。
- Harness 本身不內建模型。
- Web UI 是控制台，用來管理 projects、sessions、toolsets、skills、approvals，不是行銷頁。

## 目前檔案

- `README.md`：產品定位、架構、harness tool call、里程碑。
- `prompts/main.md`：外部 agent 透過 harness 工作時的 runtime prompt。
- `prompts/rules.md`：使用者通用工作規則。
- `AGENTS.md`：本 repo 的協作與維護規則。
- `cmd/mcp-harness`：MCP stdio server。
- `cmd/mcp-harness-web`：Web UI 控制台。
- `internal/harness`：核心 runtime、toolsets、skills、prompt 合成。
- `internal/web`：Web API 與 HTML 控制台。

如果新增實作，請同步更新 README 的「目前狀態」與里程碑，不要讓文件宣稱未實作功能已可用。

## 語言

預設使用台灣繁體中文。避免中國用語與翻譯腔。

程式碼、工具名稱、schema 欄位、MCP 名稱、harness tool call keyword 保持英文。

## 技術棧

- Go 是主要實作語言。
- MCP server 使用官方 `github.com/modelcontextprotocol/go-sdk`。
- Web UI 目前使用 Go standard library `net/http`，不要未經確認改成前端框架。
- 設定與 session MVP 使用 `~/.mcp-harness` 下的 JSON/JSONL 檔。

## 文件原則

- 實作前先把介面與安全邊界寫清楚。
- 文件要區分「已完成」、「預計」、「尚未實作」。
- 不要用 mock 或概念描述包裝成已驗證功能。
- 修改 prompt 時，要同時考慮 parser 是否能穩定實作。
- 修改 harness tool call 格式時，要同步更新 README 與 `prompts/main.md`。

## Harness Tool Call 設計規則

目前 canonical tool call 是：

```text
<harness_tool_call>
{"tool":"toolset.tool","args":{"key":"value"}}
</harness_tool_call>
```

維護時請遵守：

- opening tag 與 closing tag 必須各自獨立一行。
- block body 必須是單一 JSON object。
- JSON 固定使用 `tool` 與 `args`。
- `tool` 採 `toolset.tool`，兩段都只用小寫英數與底線。
- `args` 只接受 JSON object，不接受 positional args。
- tool call block 不能混入說明文字或 markdown code fence。
- parser 必須做 JSON decode 與 schema validation。

不要把 tool call 改成 markdown code block、自然語言格式或括號式格式，除非已經同步設計 parser 與誤判防護。

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
