# Dev Status Board — Architecture, Product Contract, and Implementation Plan

> 日期：2026-08-20  
> 状态：**M0 CONTRACT FROZEN**  
> 权威合同：`Docs/M0_V1_State_Runtime_and_Navigation_Contract_2026-08-20.md`

## 1. 产品定义

DevBoard V1 是：

> **本地优先的开发状态聚合 + 安全导航系统。**

它把多个开发信号归一化成统一状态，并输出给旧 Kindle、手机、平板和桌面浏览器等常驻 Display。

V1 的两项能力：

1. **STATUS DISPLAY**
2. **SAFE NAVIGATION**

主要信息类别：

- AI coding-agent lifecycle；
- actionable agent alerts；
- host/system resources；
- tracked AI/dev process groups；
- Git/project/worktree；
- optional AI quota；
- safe navigation targets。

DevBoard 不是 quota-only dashboard、Kindle-only app、AI orchestration platform、remote shell、Electron app、Codex/Claude 替代品或完整 observability platform。

V1 只允许导航动作：

- `focus_app`
- `focus_agent`
- `focus_project`
- `open_project`

V1 不允许 `approve/deny/stop/retry/send_prompt/execute_shell`，也不接受客户端提供的任意命令、AppleScript、URL 或 executable path。执行型 Control Layer 属于未来 V2。

## 2. 核心架构

```text
Data Sources
    ↓
Collectors / Hook Adapters
    ↓
Normalized Events
    ↓
Unified State Core
    ↓
┌─────────────────────────┐
│ Display Projection      │
│ Navigation Projection   │
└─────────────────────────┘
    ↓                 ↓
Display            Navigation Router
    ↓                 ↓
Kindle/Phone       Target Resolver
Browser                ↓
                   Host Adapter
                       ↓
                   macOS App
```

Kindle touch、Phone、Keyboard 与未来 MX Master 4 不建立各自业务语义；它们复用同一个 `NavigationIntent`。

State Core 的内部权威与公开输出严格分开：

```text
InternalState
    ↓ explicit sanitization / derivation
PublicState
```

Display 不直接读取 hook raw payload、系统 collector raw response、Git 命令输出或 quota raw response。

## 3. Agent 状态模型

旧的单一枚举：

```text
IDLE / WORKING / ATTENTION / COMPLETE / ERROR / STALE
```

不再作为权威状态模型。

### 3.1 Activity

```text
idle
working
attention
error
```

### 3.2 Outcome

```text
none
completed
failed
```

### 3.3 Freshness

```text
fresh
stale
```

`COMPLETE` 是派生显示：`activity=idle` + recent `outcome=completed`。

`STALE` 是可信度，不覆盖历史 activity。比如：

```text
activity=working
freshness=stale
→ STALE · was WORKING
```

`DisplayStatus` 只派生，不持久化为业务 Authority。

## 4. 顶层 Turn 语义

V1 的 “task” 精确定义为：

> 一次由用户 prompt 发起的 top-level agent turn。

不推断整个项目、milestone、PR 或业务任务完成。

核心 reducer：

| Event | 结果 |
|---|---|
| `UserPromptSubmit` | 新 turn；`working + none`；设置 `startedAt` |
| `PermissionRequest` | `attention` |
| `PostToolUse` | `working` |
| `Stop` | `idle + completed`；设置 `completedAt` |
| `StopFailure` | `error + failed` |
| `SessionEnd` | `idle`；保留 recent outcome |

Claude Code：

- `AskUserQuestion` → attention
- `Elicitation` → attention
- notification `permission_prompt` → attention
- notification `elicitation_dialog` → attention
- notification `idle_prompt` → bounded idle fallback
- `PostToolUseFailure` → working
- `PermissionDenied` → working
- `ElicitationResult` → working
- `StopFailure` → error + failed

可恢复工具失败不是 terminal ERROR。

Codex adapter 只使用运行时实际可取得的 lifecycle facts；若无法可靠确认 completion，则 SourceHealth 降级/stale，不能伪造 COMPLETE。

Subagent 不进入 V1 顶层完成语义。

## 5. AgentEvent 与顺序

Normalized `AgentEvent` 至少包含：

- `schemaVersion`
- `eventId`
- `provider`
- `sessionId`
- `turnId`
- `eventType`
- `occurredAt`
- `cwd`
- sanitized allow-listed `metadata`

provider 初始值：

- `codex`
- `claude-code`

canonical session identity：

```text
<provider>:<sessionId>
```

Reducer 必须满足：

- duplicate `eventId` 幂等；
- 只有 begin-turn (`UserPromptSubmit`) 能替换 current turn；
- B turn 开始后，A turn 的迟到事件不得覆盖 B；
- 旧 begin-turn 迟到/重复不得重新成为 current；
- current-turn 旧时间事件不得把状态倒退；
- repeated notification 使用 stable alert identity 去重；
- subagent 事件不得完成 parent turn。

M2 的本地接入方向是 CLI/helper + Unix-domain ingestion；M0 不安装 hook、不实现 socket。

## 6. AgentTarget 与 Safe Navigation

`AgentTarget` 是可信导航元数据，不是执行 Authority。

核心字段：

- `targetId`
- `hostId`
- `provider`
- `sessionId`
- optional `turnId`
- optional `projectId`
- optional `worktreeId`
- optional `preferredApp`
- optional server-owned opaque `focusLocator`

客户端只发送：

```text
action + targetId
```

绝不发送 shell、AppleScript、任意路径、任意 URL 或 executable。

`NavigationIntent`：

- `schemaVersion`
- `requestId`
- `action`
- `targetId`
- `source`
- `requestedAt`

action：

- `focus_app`
- `focus_agent`
- `focus_project`
- `open_project`

source 可包括：

- `kindle`
- `web`
- `phone`
- `keyboard`
- `mx-master-4`

`mx-master-4` V1 仅冻结 contract，不实现。

`NavigationResult`：

- `requestId`
- `status`
- `resolvedTarget`
- `message`
- `completedAt`

status：

- `accepted`
- `completed`
- `unavailable`
- `unsupported`
- `failed`

Navigation failure 与 Agent lifecycle 完全隔离。

## 7. Host identity

Root state 从 V1 就包含：

- `host.id`
- `host.displayName`

`AgentTarget` 包含 `hostId`。

V1 runtime 可以只实现单本机，但数据合同不假设永远只有一台主机，为未来：

```text
Kindle → DevBoard Hub → Mac mini node / MacBook node
```

保留扩展空间。V1 不实现 multi-host transport。

## 8. System Metrics

本地 V1 不要求 Glances daemon。

优先实现方向：

> embedded mature Go metrics library（首选候选 `gopsutil`，M3 审计后最终选型）。

本地状态：

- CPU
- memory
- swap
- disk
- tracked process groups

ProcessGroup 可以匹配多个 PID：

```text
Codex
Claude
Ghostty
ChatGPT
```

冻结聚合：

- memory = matched PIDs resident memory 之和；
- CPU = matched process CPU 值之和，并在 M3 固定 library/unit convention；
- 缺失 metric = `null`，绝不伪装为 `0`。

Glances 保留为未来 remote/NAS/VPS/external adapter，不是本地 V1 Authority。

## 9. SourceHealth

所有 optional/external collector 都有：

```text
status = available | degraded | unavailable
lastAttemptAt
lastSuccessAt
message
```

`message` 必须 sanitized。

Collector 隔离：

- quota unavailable 不降低 Agent 状态；
- 单 project 失败不影响其他 project；
- `gh` 缺失不影响 local Git；
- Codex lifecycle capability 不完整只降低 Codex source confidence。

## 10. Project / Worktree

项目来源：

1. pinned config；
2. Agent `cwd` auto-discovery。

有 `cwd` 时解析最近 Git worktree root。

身份必须 worktree-aware：

- `projectId`
- `displayName`
- `worktreeId`
- internal `worktreeRoot`
- `repositoryIdentity`
- `branch`
- `dirty`
- `modifiedCount`
- `untrackedCount`
- `ahead`
- `behind`
- `sourceHealth`

不能只用 friendly repo name 当 identity。

绝对路径默认私有；PublicState 输出 sanitized project/worktree identity。

PR/CI optional。本地 Git 无 `gh` 也必须工作。

## 11. Alert Engine

alert type：

- `attention`
- `error`
- `complete`
- `stale`

推荐显示优先级：

```text
ATTENTION
ERROR
STALE ACTIVE
COMPLETE
WORKING
INFO
```

Attention 直到同 turn 恢复 working、stop、同 session 新 turn、或 session end。

Error 只用于 terminal/fatal failure。

Complete：

```text
0–10 min   high visibility
10–30 min  recent
>30 min    hidden
```

`SessionEnd` 不立即清除 recent COMPLETE。

重复 hook 使用 stable alert identity 去重。

## 12. Kindle Display

Endpoint：

```text
GET /display/kindle
```

旧 Kindle 是第一类 V1 target。

必须：

- server-rendered HTML；
- basic CSS；
- high contrast black/white；
- large touch targets；
- meta refresh；
- status 有文字；
- 无 modern JS 依赖。

不要求：

- Fetch
- Promise
- EventSource
- WebSocket
- CSS Grid
- Canvas
- SVG animation
- React/Vue

### Touch

Agent/Project card 可以触发 V1 navigation。

优先使用：

- side-effect-free 页面跳转的 `<a href="...">`；
- 或 server-rendered conventional `<form method="POST">` 执行导航。

不依赖 JS click handler；整张卡应是大触控目标。

### E-Ink

- WORKING：白底 + 标准边框；
- COMPLETE：支持时黑底白字；
- ATTENTION：极粗边框 + `ACTION REQUIRED`；
- ERROR：重边框 + 明确 `ERROR`；
- STALE：明确 stale 文字。

不能只靠颜色。

### Orientation

支持 portrait / landscape，不硬编码单一 600×800。

fallback：

```text
/display/kindle?layout=portrait
/display/kindle?layout=landscape
```

可谨慎使用 orientation media query。

### Browser chrome

不假定 Kindle toolbar/menu 可以隐藏。页面在 browser chrome 存在时仍必须可用。

Kindle jailbreak/fullscreen、`~ds` 或设备 anti-sleep 配置不属于 DevBoard runtime。

## 13. Modern Display

Endpoint：

```text
GET /display
```

允许 responsive CSS、少量 vanilla JS、SSE、Screen Wake Lock API。

Wake Lock 只能 best effort；页面应能显示 acquired/unavailable，不能承诺一定不休眠。

## 14. Web surface 与安全边界

V1 Web 分两类：

### READ

- `/health`
- `/api/state`
- `/display`
- `/display/kindle`
- 后续只读细分 endpoint

### NAVIGATION

M5 才实现的 allow-listed navigation endpoint。

必须校验：

- allowed action；
- known/trusted target；
- request size；
- HTTP method；
- host/capability；
- origin/auth policy。

禁止 wildcard CORS 与 generic execution endpoint。

V1 LAN 计划机制冻结为：

> **per-install random navigation token + same-origin browser navigation flow**

token：

- 不进入 PublicState JSON；
- 不写日志；
- 不接受 cross-origin 任意调用；
- cookie/form/nonce 的精确兼容实现，M1 在任何导航 runtime 开始前完成并实测 Kindle。

## 15. Persistence / Restart

V1：

```text
memory
+
atomic state snapshot
```

不引入数据库。

恢复：

- recent outcomes；
- recent alerts；
- known projects/worktrees；
- source timestamps。

daemon restart 后，历史 working/attention 不能继续被当作 fresh；先恢复 `freshness=stale`，直到新 lifecycle event 重新确认。

TTL 基于 timestamp。`elapsedSeconds` 由时间戳派生，不作为持久化 Authority。

## 16. Public State Sanitization

Display/Public API 禁止暴露：

- API keys / OAuth tokens；
- Claude/Codex credentials；
- raw prompt / assistant response / transcript；
- raw hook payload；
- arbitrary env；
- absolute filesystem path（默认）；
- private focusLocator；
- navigation token。

必须显式做：

```text
InternalState
→ PublicState Projection
```

不能直接把 raw RootState 自动 JSON 化成 public API。

## 17. API/Runtime Direction

M0 只冻结方向，不实现 runtime。

M1 read surface：

```text
GET /health
GET /api/state
GET /display
GET /display/kindle
```

现代 display 可在 M1+加入 SSE。

M2 agent event ingestion 优先 host-local CLI/helper + Unix domain socket。

M5 才加入 allow-listed navigation endpoint 与 macOS navigation host adapter。

没有 generic execution API。

## 18. Resource Budget

常驻工具目标：

- core idle CPU 接近 0；
- memory 尽量 `<100 MB`；
- 无 Electron；
- 无内嵌 Chromium；
- 无 Node 常驻前端链；
- 不保留高频历史指标；
- 不做秒级图表。

Core 技术方向：Go。

Frontend：

```text
Go html/template
+
CSS
+
small vanilla JS (modern display only)
```

Storage：

```text
memory
+
atomic state snapshot
```

## 19. 仓库结构方向

M0 实际只存在 Docs。后续方向：

```text
DevBoard/
├── cmd/devboard/
├── internal/
│   ├── state/
│   ├── collectors/
│   ├── ingest/
│   ├── alerts/
│   ├── navigation/
│   └── web/
├── web/
├── Docs/
│   ├── M0_V1_State_Runtime_and_Navigation_Contract_2026-08-20.md
│   └── contracts/
├── launchd/
├── config.example.yaml
└── go.mod
```

M0 不创建这些 runtime 路径。

## 20. 施工里程碑

### M0 — Contract Freeze

仅：

- architecture reconciliation；
- state/runtime/navigation contract；
- synthetic JSON examples；
- contract validation。

### M1 — Core + State + Mock Display

- Go skeleton
- config
- state store
- mock states
- public projection
- `/health`
- `/api/state`
- `/display`
- `/display/kindle`

### M2 — Agent Event Ingestion

- CLI helper
- Unix socket
- reducers
- Codex
- Claude
- alert engine

### M3 — System Metrics

- embedded local collector
- process groups

### M4 — Project / Worktree

- git discovery
- pinned project
- cwd discovery
- local status

### M5 — Safe Navigation

- NavigationIntent
- target resolver
- macOS `focus_app`
- `focus_project`
- `focus_agent` capability adapters

### M6 — Optional Quota

- CodexBar adapter
- independent SourceHealth

### M7 — Production Runtime

- launchd
- atomic snapshot
- log retention
- startup checks
- graceful shutdown

Future V2：

- execution-changing Control Layer
- MX Master 4 / Action Ring / Haptic
- keyboard backlight
- multi-host node transport
- approve / stop / retry

## 21. M0 冻结结论

M0 的 state、lifecycle、freshness、worktree identity、source health、public sanitization、Kindle compatibility、Safe Navigation allow-list 与安全边界，均以 `M0_V1_State_Runtime_and_Navigation_Contract_2026-08-20.md` 为权威。

本阶段没有 Go runtime、collector、hook install、窗口切换、MX Master 4 或 Agent execution control。

**M0 CONTRACT FROZEN**
