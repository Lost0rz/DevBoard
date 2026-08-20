# DevBoard M2 — Manual Agent Hook Setup

> Date: 2026-08-20
> Milestone: M2 — Agent Event Ingestion + Lifecycle Runtime
> Installation policy: **manual only**

DevBoard M2 does not edit Codex or Claude Code configuration. Install hooks only after building DevBoard and deciding which provider settings file you want to change.

Use the **absolute filesystem path** to the built executable. Every example below uses:

```text
/ABSOLUTE/PATH/TO/devboard
```

Replace that string with the real path before installing anything.

## 1. Start DevBoard first

```bash
/ABSOLUTE/PATH/TO/devboard serve
```

Default live ingestion uses:

```text
<user-cache-dir>/devboard/activity.sock
```

The runtime directory is mode `0700`; the socket is mode `0600`. `DEVBOARD_RUNTIME_DIR` is available only as an explicit test/development override.

## 2. Codex

Current Codex hook documentation supports `hooks.json` next to active config layers, including:

```text
~/.codex/hooks.json
<repo>/.codex/hooks.json
```

Codex also supports inline `[hooks]` in `config.toml`. For DevBoard M2, prefer one representation at a given layer and use `hooks.json` for the clearest manual review.

Reference: https://developers.openai.com/codex/hooks

### Required M2 Codex events

DevBoard consumes only these currently verified lifecycle events:

- `UserPromptSubmit`
- `PreToolUse`
- `PermissionRequest`
- `PostToolUse`
- `Stop`
- `SessionEnd`

Do not add Claude-only lifecycle names such as `StopFailure`, `PostToolUseFailure`, or `PermissionDenied` to the DevBoard Codex configuration merely because Claude exposes them.

### Example `~/.codex/hooks.json`

```json
{
  "hooks": {
    "UserPromptSubmit": [
      {
        "hooks": [
          {
            "type": "command",
            "command": "/ABSOLUTE/PATH/TO/devboard agent-hook codex"
          }
        ]
      }
    ],
    "PreToolUse": [
      {
        "hooks": [
          {
            "type": "command",
            "command": "/ABSOLUTE/PATH/TO/devboard agent-hook codex"
          }
        ]
      }
    ],
    "PermissionRequest": [
      {
        "hooks": [
          {
            "type": "command",
            "command": "/ABSOLUTE/PATH/TO/devboard agent-hook codex"
          }
        ]
      }
    ],
    "PostToolUse": [
      {
        "hooks": [
          {
            "type": "command",
            "command": "/ABSOLUTE/PATH/TO/devboard agent-hook codex"
          }
        ]
      }
    ],
    "Stop": [
      {
        "hooks": [
          {
            "type": "command",
            "command": "/ABSOLUTE/PATH/TO/devboard agent-hook codex"
          }
        ]
      }
    ],
    "SessionEnd": [
      {
        "hooks": [
          {
            "type": "command",
            "command": "/ABSOLUTE/PATH/TO/devboard agent-hook codex"
          }
        ]
      }
    ]
  }
}
```

The top-level `hooks` object is required by the current `hooks.json` shape; do not place event names at the JSON root.

### Codex trust/enablement

Current Codex requires non-managed command hooks to be reviewed and trusted before they run. Use:

```text
/hooks
```

inside Codex to inspect sources, review changed definitions, trust them, and confirm they are enabled. A configuration file existing on disk is **not** proof that the DevBoard hook has fired.

After installation, run a real prompt/tool lifecycle and confirm `codex-hooks` changes from:

```text
degraded — No validated lifecycle event observed yet.
```

to `available` in `/api/state`.

## 3. Claude Code

Claude Code hooks can be configured in locations including:

```text
~/.claude/settings.json
.claude/settings.json
.claude/settings.local.json
```

Reference: https://code.claude.com/docs/en/hooks

M2 never edits these files automatically.

### Required M2 Claude events

- `UserPromptSubmit`
- `PreToolUse`
- `PermissionRequest`
- `PostToolUse`
- `PostToolUseFailure`
- `PermissionDenied`
- `Notification`
- `Stop`
- `StopFailure`
- `SessionEnd`
- `Elicitation`
- `ElicitationResult`

`AskUserQuestion` is **not** configured as a hook event. DevBoard detects it as:

```text
PreToolUse + tool_name == "AskUserQuestion"
```

and maps it to ATTENTION.

### Example Claude settings fragment

Current Claude Code supports exec-form command hooks. Supplying `args` runs the executable directly without shell parsing, so M2 prefers this form:

```json
{
  "hooks": {
    "UserPromptSubmit": [
      {"hooks": [{"type": "command", "command": "/ABSOLUTE/PATH/TO/devboard", "args": ["agent-hook", "claude-code"]}]}
    ],
    "PreToolUse": [
      {"hooks": [{"type": "command", "command": "/ABSOLUTE/PATH/TO/devboard", "args": ["agent-hook", "claude-code"]}]}
    ],
    "PermissionRequest": [
      {"hooks": [{"type": "command", "command": "/ABSOLUTE/PATH/TO/devboard", "args": ["agent-hook", "claude-code"]}]}
    ],
    "PostToolUse": [
      {"hooks": [{"type": "command", "command": "/ABSOLUTE/PATH/TO/devboard", "args": ["agent-hook", "claude-code"]}]}
    ],
    "PostToolUseFailure": [
      {"hooks": [{"type": "command", "command": "/ABSOLUTE/PATH/TO/devboard", "args": ["agent-hook", "claude-code"]}]}
    ],
    "PermissionDenied": [
      {"hooks": [{"type": "command", "command": "/ABSOLUTE/PATH/TO/devboard", "args": ["agent-hook", "claude-code"]}]}
    ],
    "Notification": [
      {"hooks": [{"type": "command", "command": "/ABSOLUTE/PATH/TO/devboard", "args": ["agent-hook", "claude-code"]}]}
    ],
    "Stop": [
      {"hooks": [{"type": "command", "command": "/ABSOLUTE/PATH/TO/devboard", "args": ["agent-hook", "claude-code"]}]}
    ],
    "StopFailure": [
      {"hooks": [{"type": "command", "command": "/ABSOLUTE/PATH/TO/devboard", "args": ["agent-hook", "claude-code"]}]}
    ],
    "SessionEnd": [
      {"hooks": [{"type": "command", "command": "/ABSOLUTE/PATH/TO/devboard", "args": ["agent-hook", "claude-code"]}]}
    ],
    "Elicitation": [
      {"hooks": [{"type": "command", "command": "/ABSOLUTE/PATH/TO/devboard", "args": ["agent-hook", "claude-code"]}]}
    ],
    "ElicitationResult": [
      {"hooks": [{"type": "command", "command": "/ABSOLUTE/PATH/TO/devboard", "args": ["agent-hook", "claude-code"]}]}
    ]
  }
}
```

Use Claude Code's read-only `/hooks` browser to confirm which settings file supplied each hook and inspect the final command/args.

Claude Code v2.1.196+ supplies `prompt_id`, which DevBoard uses as the normalized turn identity. For an older Claude version, `UserPromptSubmit` can begin a turn with a synthetic identity; later events without a reliable `prompt_id` are not guessed onto that turn and may make lifecycle confidence degraded/stale.

For `Stop`, DevBoard reads only the **counts** of `background_tasks` and `session_crons`. If either count is nonzero, M2 does not declare the turn complete. It never stores the task descriptions, commands, or cron prompts.

For `StopFailure`, DevBoard retains only the safe `error` type and never stores `error_details` or `last_assistant_message`.

### Why stdout must stay empty

`devboard agent-hook` intentionally writes nothing to stdout. Claude Code treats plain stdout from `UserPromptSubmit` as model-visible context, so monitoring output must never be injected into the conversation.

Operational failures are fail-open: malformed input, missing daemon/socket, timeout, unsupported event, or internal monitoring failure must not stop the coding agent.

## 4. Synthetic setup test without changing provider config

Start live DevBoard, then send a synthetic Codex event:

```bash
printf '%s\n' '{"session_id":"synthetic-codex","turn_id":"turn-1","cwd":"/tmp","hook_event_name":"UserPromptSubmit"}' \
  | /ABSOLUTE/PATH/TO/devboard agent-hook codex
```

The helper intentionally prints nothing. Inspect:

```text
http://127.0.0.1:8787/api/state
http://127.0.0.1:8787/display
http://127.0.0.1:8787/display/kindle
```

`/api/state` should contain a working agent with canonical ID:

```text
codex:synthetic-codex
```

For a synthetic Claude event:

```bash
printf '%s\n' '{"session_id":"synthetic-claude","prompt_id":"prompt-1","cwd":"/tmp","hook_event_name":"UserPromptSubmit"}' \
  | /ABSOLUTE/PATH/TO/devboard agent-hook claude-code
```

Then check `claude-hooks` SourceHealth in `/api/state`.

## 5. Privacy boundary

Raw provider JSON is parsed through allow-listed adapter structs. M2 does not normalize/store/copy:

- prompt text
- `last_assistant_message`
- `tool_input`
- `tool_response`
- shell commands
- `transcript_path`
- notification title/message text
- `error_details`
- background task description/command
- cron prompt
- raw provider JSON

`cwd` may exist only in the private normalized ingestion event for future milestones; PublicState and rendered pages do not expose it.

## 6. M2 boundaries

Still not implemented:

- system/process collector
- Git/GitHub collector
- quota collection
- Safe Navigation runtime
- navigation POST/actions
- macOS focus / AppleScript
- launchd
- persistence/database
- approve/deny/stop/retry execution

`safeNavigationEnabled` remains `false`.
