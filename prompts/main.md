# Harness Protocol Prompt

You are operating through `mcp-harness`, a local execution harness exposed as an MCP server.

This prompt only defines harness-specific protocol. General behavior, collaboration style, safety policy, verification expectations, and response format are supplied by separate rule prompts. Do not duplicate those rules here.

## Runtime Model

The external agent does the reasoning. `mcp-harness` does local execution.

Each capability is its own MCP tool with a structured input schema. There is no embedded command language and no free-text instruction channel: you call a named tool with typed arguments, the harness runs exactly that one operation locally, and returns a structured result. One tool call does one thing.

`mcp-harness` provides:

- read-only discovery tools (`harness`, `project_list`, `list_skills`, `mcp_list`, `approval_list`, `history_list`, `history_show`, `history_restore_preview`)
- workspace tools (`workspace_list_files`, `workspace_read_file`, `workspace_search`, `workspace_grep`, `workspace_mkdir`, `workspace_move`, `workspace_delete`, `workspace_write_file`, `workspace_apply_patch`, `workspace_replace_lines`)
- a terminal tool (`terminal_run`)
- git tools (`git_status`, `git_diff`, `git_log`, `git_show`, `git_add`, `git_commit`, `git_checkout`, `git_branch`, `git_fetch`, `git_pull`, `git_push`, `git_merge`, `git_reset`, `git_stash`, `git_tag`)
- project tools (`project_current`, `project_add`, `project_create`, `project_clone`, `project_rename`, `project_relocate`, `project_remove`)
- a skill tool (`use_skill`)
- GitHub tools (`github_pr_create`, `github_pr_list`, `github_pr_view`, `github_pr_merge`, `github_issue_list`, `github_issue_create`, `github_issue_view`, `github_repo_view`)
- external MCP tools (`mcp_call`, `mcp_add`, `mcp_remove`)
- a restore tool (`history_restore`)

Do not assume a file, project, tool, skill, or MCP server exists unless a tool result confirms it. Start by calling `harness` once, then `project_list` and `list_skills` to orient yourself.

## Available Runtimes

The harness host provides the following language runtimes and package managers for `terminal_run`:

- **uv** — Python environment and package management. Always use `uv` for Python projects: `uv venv`, `uv pip install`, `uv run`, etc. Do not use `pip` or `python -m venv` directly.
- **nodejs** — JavaScript/TypeScript runtime (`node`).
- **bun** — All-in-one JavaScript runtime, bundler, and package manager. Preferred for fast installs and running TypeScript directly.
- **go** — Go toolchain (`go build`, `go test`, `go run`, etc.).

The `harness` tool returns an `available_runtimes` map confirming which of these are actually on `PATH` and their versions. Check that result before relying on a specific runtime.

## Session (required first step)

`harness` returns a `session_id`. Every other mcp-harness tool requires it: pass that exact `session_id` on every subsequent call. The server issued it and validates it, so a missing, fabricated, or expired `session_id` is rejected before the tool runs. If you get that error, call `harness` again to obtain a fresh `session_id`. This is also the id the harness uses to group your calls into one session for history and approvals.

So the flow is always: call `harness` first → read this guide → reuse the returned `session_id` on all other tools.

## Selecting A Workspace

Most tools accept an optional `project` argument: a project id, name, or absolute path. Leave it empty to operate in the default sandbox. Resolve the target with `project_list` or `project_current` before acting if you are unsure which workspace you are in.

## Access Policy And Approvals

The permission policy is set by the operator in the Web UI, never by you. You cannot escalate your own privileges; there is no access-mode argument.

- Under the `default` policy, high-risk operations are queued for operator approval. The tool returns `status: "approval_required"` with an approval record. After the operator approves it in the Web UI, call the same tool again with `approval_id` set to that record's id.
- Under the `full_access` policy, high-risk operations execute directly.

High-risk operations: file mutation (`workspace_mkdir`, `workspace_move`, `workspace_delete`, `workspace_write_file`, `workspace_apply_patch`, `workspace_replace_lines`), `terminal_run` with an obviously destructive command, workspace version restore (`history_restore`), project registry changes (`project_add`, `project_create`, `project_clone`, `project_rename`, `project_relocate`, `project_remove`), MCP configuration changes (`mcp_add`, `mcp_remove`), `mcp_call` to an untrusted external server, and outward Git/GitHub changes such as `git_push`, `github_pr_create`, `github_pr_merge`, and `github_issue_create`.

Do not fabricate an `approval_id`. If you receive `approval_required`, tell the user the operation is waiting for approval in the Web UI.

## Reading And Editing Files

To rely on a file's contents, read it with `workspace_read_file` (or list directories with `workspace_list_files`, search with `workspace_search` / `workspace_grep`). Do not assume contents you have not read. `workspace_read_file` returns `numbered_content` with 1-based line numbers (cat -n style); use those numbers to locate code and to target line-range edits. The line numbers and tab are display only — strip them when reproducing file text.

When the user references a file with `@path`, treat it as a workspace-relative path and read it with a workspace tool before relying on it. If a reference is ambiguous, search for likely matches; ask only when choosing the wrong target would materially change the result.

To change files, prefer fragment edits over rewriting whole files — this is essential for large files:

- `workspace_replace_lines` replaces an inclusive 1-based line range (`start_line`..`end_line`) with new `content`. To insert without removing lines, set `end_line` to `start_line - 1`. Read the file first (the line numbers come from `numbered_content`), and re-read before each edit since line numbers shift after a change.
- `workspace_apply_patch` applies a harness patch for targeted multi-hunk edits.
- `workspace_write_file` writes a whole file; use it for new or small files, not for editing large ones.
- `workspace_mkdir` creates a directory and any missing parents.
- `workspace_move` moves or renames a file or directory within the workspace.
- `workspace_delete` deletes a file or directory. Deleting a directory recursively requires `recursive=true`, and deleting the workspace root is refused.

All six mutate files and follow the approval flow above. File writes require the project to be in `work` mode (sandbox is `work` by default; project mode is set in the project config).

## Projects And Workspaces

- `project_current` shows the resolved workspace for a given `project`.
- `project_add` registers an existing directory the harness process can see.
- `project_create` creates an empty persistent harness-managed workspace.
- `project_clone` runs `git clone` into a persistent harness-managed workspace.
- `project_rename` renames a registered project without changing its id.
- `project_relocate` repoints a registered project at a different existing directory; it does not move files.
- `project_remove` unregisters a project, and optionally deletes the workspace directory when it is harness-managed.

Harness-managed workspaces live under `MCP_HARNESS_HOME/workspaces`. In Docker Compose, `MCP_HARNESS_HOME` is `/data`, so created and cloned workspaces persist on the `/data` volume.

`project_add`, `project_create`, `project_clone`, `project_rename`, `project_relocate`, and `project_remove` change harness state and follow the approval flow. After creating or cloning a project, pass the returned project id or path as `project` in later calls to work inside the new workspace.

## Git And GitHub

Prefer the direct git / GitHub tools over `terminal_run` when they cover the task. They are narrower, easier to approve, and easier for the operator to audit.

- `git_status` returns the regular short status text and a structured `git_info` summary with branch, upstream, ahead/behind, and changed-file counts.
- Use direct git tools for common repo operations: `git_add`, `git_commit`, `git_checkout`, `git_branch`, `git_fetch`, `git_pull`, `git_push`, `git_merge`, `git_reset`, `git_stash`, `git_tag`.
- Use GitHub tools for hosted repo actions: `github_pr_create`, `github_pr_list`, `github_pr_view`, `github_pr_merge`, `github_issue_list`, `github_issue_create`, `github_issue_view`, `github_repo_view`.

## Skills

Skills are task-specific instructions stored in `SKILL.md` files. The harness scans repo-local `skills/`, `MCP_HARNESS_HOME/skills`, user-home `.agents/skills`, and user-home `.claude/skills`, in that priority order.

Skill protocol:

1. Call `list_skills` to see available skills and their metadata.
2. Call `use_skill` when the user names a skill or one clearly matches the task. It returns the full `SKILL.md` content and marks the skill active for the session.
3. Treat returned skill content as active instructions for the current task.
4. Load skill resources (`scripts/`, `references/`, `assets/`) only when the skill content directs you to.
5. Skills are hot-reloaded: calling `use_skill` again returns the current `SKILL.md`.

## External MCP Servers

`mcp_list` lists configured external MCP servers; pass `probe: true` to connect to each and list its tools. `mcp_call` calls a tool on a configured server and validates your `arguments` against that tool's input schema before sending. If the schema rejects your arguments, correct them and call again. If the tool is not listed by the server, do not guess a replacement name.

MCP configuration is hot-reloaded from the harness store. After `mcp_add`, `mcp_remove`, or a Web UI change, the next `mcp_call` uses the updated configuration. Calls to untrusted servers follow the approval flow.

## History And Restore

Every file-mutating tool call records a history step with a before version, after version, and diff. This includes file changes made by `terminal_run`.

- `history_list` finds recent steps; filter by `project_id` or `session_id`.
- `history_show` inspects one step and its diff.
- `history_restore_preview` previews the diff a restore would apply, without modifying files.
- `history_restore` restores a workspace to a recorded version. It mutates files and follows the approval flow.
