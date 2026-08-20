# Dev Status Board — Architecture, Product Contract, and Implementation Plan

> 日期：2026-08-20  
> 状态：**M0 CONTRACT FROZEN — M0.1 Closure Applied**  
> 权威合同：`Docs/M0_V1_State_Runtime_and_Navigation_Contract_2026-08-20.md`

## 1. 产品定义

DevBoard V1 是：

> **本地优先的开发状态聚合 + 安全导航系统。**

V1 同时包含：

1. **STATUS DISPLAY**
2. **SAFE NAVIGATION**

主要信息：AI coding-agent lifecycle、actionable alerts、host/system resources、tracked AI/dev process groups、Git/project/worktree、optional AI quota、safe navigation targets。

V1 允许 `focus_app`、`focus_agent`、`focus_project`、`open_project`。

V1 不允许 approve/deny/stop/retry、send_prompt/execute_shell，也不允许客户端提供 shell、AppleScript、可执行路径、任意 URL/application。generic execution endpoint 不存在；执行型 Control Layer 属于未来 V2。

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
│ Public Display Projection│
│ Navigation Projection   │
└─────────────────────────┘
    ↓                 ↓
Display            Navigation Router
    ↓                 ↓
Kindle/Phone       NavigationTarget Resolver
Browser                ↓
                   Host Adapter
                       ↓
                   macOS App
```

Kindle touch、Phone、Keyboard 与未来 MX Master 4 复用同一 `NavigationIntent`，不建立设备专属业务语义。

## 3. InternalRootState 与 PublicState

M0.1 明确区分：

```text
InternalRootState
    ↓ explicit allow-list projection
PublicState
    ↓
GET /api/state / Display
```

### InternalRootState

内部 State Authority / snapshot，可包含：

- `worktreeRoot`；
- opaque `focusLocator`；
- private NavigationTarget detail；
- internal snapshot metadata。

`Docs/contracts/root-state-v1.example.json` 是 **InternalRootState 示例**，包含 `stateKind=internal`。它不是 `/api/state` 的响应格式。

### PublicState

`GET /api/state` 只输出：

```text
Docs/contracts/public-state-v1.example.json
```

该示例 `stateKind=public`。

PublicState 禁止：

- worktreeRoot；
- focusLocator；
- absolute paths；
- API key / OAuth / credentials；
- raw prompt / response / transcript / hook payload；
- arbitrary env；
- navigation long-lived secret；
- NavigationTarget private detail。

可公开的导航元数据只有：

```text
targetId
kind
allowedActions
```

M1 必须显式构造 PublicState，不能直接 serialize InternalRootState 后做黑名单删除。

## 4. DisplayMeta

PublicState 根 `meta` 固定为 DisplayMeta，不是任意扩展对象，也不是 internal metadata 的复制。

V1 字段：

- `displayContractVersion`
- `kindleRefreshSeconds`
- `completeHighVisibilitySeconds`
- `completeRetentionSeconds`
- `safeNavigationEnabled`
- `wakeLockMode = best-effort`

这些是 presentation/capability hints，不是 Agent lifecycle Authority。Internal state 使用独立 `internalMeta`。

## 5. Agent 三维状态

### Activity

```text
idle
working
attention
error
```

### Outcome

```text
none
completed
failed
```

### Freshness

```text
fresh
stale
```

`COMPLETE` = `idle + recent completed` 的派生 DisplayStatus。

`STALE` 表示该 Agent 当前 lifecycle state 的可信度/年龄，不覆盖历史 activity。`DisplayStatus` 不持久化为 Authority。

## 6. Top-level Turn

V1 task = 一次 user prompt 发起的 top-level agent turn。

| Event | Reduction |
|---|---|
| `UserPromptSubmit` | begin turn → working + none |
| `PermissionRequest` | attention |
| `PostToolUse` | working |
| `Stop` | idle + completed |
| `StopFailure` | fatal 时 error + failed |
| `SessionEnd` | session-scoped idle；保留 recent outcome |

可恢复 tool failure 不等于 terminal ERROR。Subagent 不得完成 parent top-level turn。

## 7. AgentEvent nullability 与 scope

Normalized envelope：

- `schemaVersion`
- `eventId`
- `provider`
- `sessionId`
- `turnId`
- `eventType`
- `occurredAt`
- `cwd`
- sanitized `metadata`

冻结：

```text
turnId: string | null
cwd: string | null
```

不能因为合法 provider lifecycle event 没有 `turnId/cwd` 就拒绝整个事件。

### Turn-scoped

只有 identity/order 能确认属于 current turn 时才允许修改 current turn。

如果 provider 没有可靠 turnId：schema 仍合法；reducer 不猜 turn identity；只使用明确安全的 session-level fact；必要时降低该 Agent freshness/capability；不伪造 completion。

### Session-scoped

`SessionEnd` 是典型 session-scoped event：

```text
turnId = null
cwd = null
```

它通过 canonical session identity + ordering 处理，不建立新的 turn identity。

示例：

- `agent-event-v1.example.json`
- `agent-event-session-v1.example.json`

## 8. Ordering / idempotency

Reducer：

- duplicate eventId = no-op；
- 只有 begin-turn 可替换 current turn；
- B turn 开始后 A turn 迟到事件不能覆盖 B；
- delayed old begin 不能替换新 turn；
- older current-turn event 不能回滚状态；
- SessionEnd 不能制造/替换 turn identity；
- repeated notification 使用 stable alert identity 去重；
- subagent 不得完成 parent。

## 9. SourceHealth 与 Agent freshness

必须严格分开。

### AgentState.freshness

范围：一个 agent/session lifecycle state，表示当前记住的这个 Agent 状态是否仍可信。

例如 daemon restart 恢复出 working：

```text
activity=working
freshness=stale
```

### SourceHealth

范围：collector/provider/transport capability。

```text
status = available | degraded | unavailable
lastAttemptAt
lastSuccessAt
message
```

例如 hook transport 故障、provider 版本缺少必要 lifecycle capability、quota adapter 不可用。

**正常 completed/idle session 后没有新事件，不会自动把整个 Codex hook source 降为 degraded。**

SourceHealth 不是第二个 Agent freshness 字段。M0.1 的 AgentState 示例移除 per-agent SourceHealth；provider/collector health 统一放 root `sources`。

## 10. Generic NavigationTarget

所有 V1 navigation 共用一个 server-owned target registry：

```text
targetId
kind
hostId
allowedActions
detail     // private
```

kind：

```text
agent
project
app
```

动作矩阵：

| kind | actions |
|---|---|
| agent | focus_agent |
| project | focus_project, open_project |
| app | focus_app |

`detail` 只存在 InternalState/可信配置。

### agent detail

AgentTarget 现在明确是：

> `NavigationTarget(kind=agent)` 的 private detail。

可含 provider/session/turn/project/worktree/preferredApp/opaque focusLocator。

### project detail

可含 projectId/worktreeId/private worktreeRoot/preferredApp/opaque locator。

### app detail

可含 server-owned appRef / opaque locator。

Client 永远只提交：

```text
action + targetId
```

不会构造 detail。Public projection 只暴露 `targetId/kind/allowedActions`。

`navigation-target-v1.example.json` 同时给出 agent/project/app 三种 synthetic target，确保 M5 不需要重新发明 target contract。

## 11. NavigationIntent / Result

NavigationIntent：

- `schemaVersion`
- `requestId`
- `action`
- `targetId`
- `source`
- `requestedAt`

NavigationResult：

- `schemaVersion`
- `requestId`
- `status`
- `resolvedTarget`
- `message`
- `completedAt`

status：

```text
accepted
completed
unavailable
unsupported
failed
```

所有对外交互 envelope 都可版本化。Navigation failure 不修改 Agent lifecycle。

## 12. Navigation security

Target Resolver：

```text
Intent
↓
schema/action/source/size validation
↓
trusted targetId lookup
↓
allowedActions
↓
host/capability
↓
Host Adapter
```

未知 target、错误 kind/action、过期 target 均拒绝。禁止把 `targetId` 当 path/URL/command/app identifier/AppleScript 解释。

LAN 机制保持：

> per-install random long-lived navigation secret + same-origin browser flow

long-lived secret：

- 不进 PublicState；
- 不写日志；
- 不进 URL/query；
- 不成为 client target detail。

无 wildcard CORS，无 generic execution endpoint。

## 13. Kindle POST / Redirect / GET

旧 Kindle 使用 meta refresh，因此 Safe Navigation 必须遵守：

```text
POST NavigationIntent
        ↓
validate + deduplicate
        ↓
perform/reuse one result
        ↓
redirect
        ↓
GET /display/kindle
```

强制 invariant：

> **side effect at most once；refresh/reload 永远是 GET。**

POST 不能直接 render 带 meta refresh 的 dashboard。

### Replay

- `requestId` = idempotency key；
- server-issued one-time nonce 放 POST body；
- nonce 与预期 action/target 做足够绑定/校验；
- 第一次合法请求消费 replay identity，并最多执行一次；
- 同一 requestId 重放不再次执行，只复用结果并 redirect；
- 已消费 nonce 用于其他 request 必须拒绝。

one-time nonce 不是 long-lived navigation secret。

303 vs 302 的旧 Kindle 兼容性可后续实机确认，但 PRG invariant 不变。

## 14. Host identity

State 从 V1 有 `host.id`、`host.displayName`；NavigationTarget 有 `hostId`。

V1 runtime 可只有一个 local Mac，但 contract 不假设永久单主机；未来 Hub → Mac mini / MacBook 不需要改 target identity。

## 15. System metrics

本地 V1 不要求 Glances daemon。

优先：embedded mature Go metrics library（首选候选 gopsutil，M3 最终审计确认）。

System：CPU、memory、swap、disk、process groups。ProcessGroup 可聚合多个 PID。

冻结：memory = resident memory sum；CPU = process CPU sum，M3 固定 library/unit；unavailable metric = null，不伪装 0。

Glances 只保留未来 remote/NAS/VPS adapter。

## 16. Project / Worktree

来源：pinned config 或 Agent cwd auto-discovery。cwd 存在时解析 nearest Git worktree root。

内部 identity：projectId、displayName、worktreeId、private worktreeRoot、repositoryIdentity、branch、dirty、modifiedCount、untrackedCount、ahead、behind。

PublicState 不暴露 worktreeRoot。PR/CI optional；无 `gh` 时 local Git 继续正常。

## 17. Alert identity

类型：attention、error、complete、stale。

Agent-related alert 必须直接包含：

```text
agentId = <provider>:<sessionId>
```

`turnId: string | null`。

`alertId` 是 opaque stable identifier，消费者不能 parse alertId 来识别 provider。

去重 identity：

```text
type + agentId + turnId(where applicable)
```

Attention：同 turn 恢复/stop、新 turn、session end 时 resolve。Error 只针对 terminal/fatal failure。

Complete：0–10m high visibility，10–30m recent，之后 hidden。SessionEnd 不立即清除 recent COMPLETE。

## 18. Kindle Display

```text
GET /display/kindle
```

要求：SSR HTML、basic CSS、black/white high contrast、large touch targets、meta refresh、status 必须有文字、无 modern JS 必需依赖。

不依赖 Fetch/Promise/EventSource/WebSocket/CSS Grid/Canvas/SVG animation/React/Vue。

Side-effecting card 使用 server-rendered POST form，并遵守 PRG/replay。

支持 portrait/landscape：

```text
/display/kindle?layout=portrait
/display/kindle?layout=landscape
```

不假设 browser chrome 可隐藏。

## 19. Modern Display

`GET /display` 可用 responsive CSS、small vanilla JS、SSE、Screen Wake Lock API。Wake Lock 只是 best effort。

## 20. Persistence / Restart

V1：

```text
memory
+
atomic state snapshot
```

无数据库。

可恢复 recent outcome/alert、known project/worktree、source timestamp。

daemon restart 后，之前 active working/attention 恢复为 `freshness=stale`，直到新 lifecycle event 确认；这不会自动把 provider SourceHealth 降为 degraded。

elapsedSeconds 由 timestamp 派生。

## 21. 施工里程碑

### M0 / M0.1

仅 Docs/contracts。

### M1 — Core + State + Mock Display

- Go skeleton
- config
- state store
- mock state
- explicit PublicState projection
- `/health`
- `/api/state`
- `/display`
- `/display/kindle`

**M1 不实现 navigation runtime。**

### M2 — Agent Event Ingestion

CLI helper、Unix socket、reducers、Codex、Claude、alert engine。

### M3 — System Metrics

embedded local collector、process groups。

### M4 — Project / Worktree

git discovery、pinned project、cwd discovery、local status。

### M5 — Safe Navigation

NavigationIntent/Result、generic NavigationTarget resolver、replay-safe PRG navigation surface、macOS focus_app/focus_project/open_project/focus_agent adapters。

### M6 — Optional Quota

CodexBar adapter、independent SourceHealth。

### M7 — Production Runtime

launchd、atomic snapshot、log retention、startup checks、graceful shutdown。

Future V2：execution-changing Control Layer、MX Master 4 / Action Ring / Haptic、keyboard backlight、multi-host transport、approve / stop / retry。

## 22. M0.1 收口结论

M0.1 关闭：

- InternalRootState / PublicState 边界；
- DisplayMeta；
- agent/project/app generic NavigationTarget；
- AgentEvent turnId/cwd nullability；
- session-scoped SessionEnd；
- Kindle PRG / replay；
- direct alert agentId；
- SourceHealth / freshness 分离；
- NavigationResult schemaVersion。

本阶段没有 Go、HTTP runtime、collector、hook install、navigation runtime、窗口切换或 MX Master 4。

**M0 CONTRACT FROZEN**
