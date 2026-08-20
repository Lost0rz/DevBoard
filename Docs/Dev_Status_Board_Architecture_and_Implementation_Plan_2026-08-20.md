# Dev Status Board — Architecture, Product Contract, and Implementation Plan

> 日期：2026-08-20  
> 状态：V1 方案冻结草案  
> 目标：把旧 Kindle、手机、平板和普通浏览器变成常驻开发状态显示终端，由统一中间层聚合 AI Agent 状态、系统资源、Git/项目状态、AI 编程额度与提醒；交互控制层留到后续版本。

---

## 1. 项目定义

Dev Status Board 不是“额度看板”，也不是“Kindle 专用项目”。

它的核心定位是：

> **一个面向开发工作流的本地信息聚合中间层 + 多终端只读 Display。**

它需要把原本分散的信息统一起来：

- Codex / Claude Code 当前是否在工作；
- 某个 Agent 是否已经完成任务；
- 是否正在等待用户批准、回答问题或处理错误；
- 哪些项目/仓库当前有活动；
- Mac 当前 CPU / Memory / Swap / Disk 状态；
- Codex / Claude / Ghostty 等关键进程的资源占用；
- Git working tree / branch / PR / CI 状态；
- Codex / Claude 等 AI 编程工具的额度与 reset 时间；
- 后续可扩展其他本地或网络状态。

最终这些信息通过统一 State Contract 输出给 Display。

---

## 2. 设计原则

### 2.1 Display 与 Data Source 必须解耦

Kindle、iPhone、iPad、Mac 浏览器不应该直接知道 Codex、Claude、Glances 或 Git 的内部实现。

```text
Data Sources
    ↓
Collectors / Hooks
    ↓
State Core
    ↓
Display API / HTML Renderer
    ↓
Old Kindle / Phone / Tablet / Browser
```

这样未来换终端，不需要改 Collector；增加数据源，也不需要重写 Display。

### 2.2 旧 Kindle 是第一类终端，但不是唯一终端

V1 同时提供：

- `/display`：现代浏览器；
- `/display/kindle`：旧 Kindle 浏览器。

旧 Kindle 页面必须采用最保守的兼容策略：

- Server-side rendered HTML；
- 基础 CSS；
- `<meta http-equiv="refresh">`；
- 不依赖 React / Vue；
- 不依赖 WebSocket；
- 不依赖复杂 JavaScript；
- 不依赖现代 Canvas / 动画。

### 2.3 中间层是项目核心

真正的 Product Core 是统一状态模型，而不是 UI。

Collector 的职责只是把不同来源转换成统一模型。

### 2.4 第一版只读

V1 不做：

- Agent approve/reject；
- 远程执行 shell；
- MX Master 4 控制；
- 自动打开 Terminal；
- 自动切换窗口；
- 直接操控 Codex / Claude Session。

这些进入 V2 Control Layer。

V1 只负责：

> **可靠、及时、低资源占用地汇总和展示状态。**

---

## 3. 整体架构

```text
┌─────────────────────────────────────────────────────────────┐
│                        DATA SOURCES                         │
├─────────────────────────────────────────────────────────────┤
│ Codex Hooks │ Claude Hooks │ Glances │ Git/GH │ CodexBar   │
│ Agent State │ Agent State  │ System  │ Project│ Quota      │
└──────┬────────────┬────────────┬──────────┬──────────┬──────┘
       │            │            │          │          │
       ▼            ▼            ▼          ▼          ▼
┌─────────────────────────────────────────────────────────────┐
│                    COLLECTOR / ADAPTER                      │
│  normalize / cache / error handling / stale detection       │
└────────────────────────────┬────────────────────────────────┘
                             ▼
┌─────────────────────────────────────────────────────────────┐
│                       STATE CORE                            │
│                                                             │
│ HostState                                                   │
│ AgentState[]                                                │
│ AlertState[]                                                │
│ ProjectState[]                                              │
│ QuotaState[]                                                │
│ DisplayMeta                                                 │
└────────────────────────────┬────────────────────────────────┘
                             ▼
┌─────────────────────────────────────────────────────────────┐
│                     DISPLAY SERVER                          │
│                                                             │
│ GET /api/state                                              │
│ GET /display                                                │
│ GET /display/kindle                                         │
│ GET /health                                                 │
│ SSE /events   (modern browser only)                         │
└────────────────────────────┬────────────────────────────────┘
                             ▼
             ┌───────────────┼────────────────┐
             ▼               ▼                ▼
        Old Kindle        Phone/iPad       Desktop Browser
        15–30s pull       SSE/live         SSE/live
```

---

## 4. 数据源设计

### 4.1 Agent 状态：Codex / Claude Code Hooks

Agent 状态是整个产品第一优先级。

统一状态：

```text
IDLE
WORKING
ATTENTION
COMPLETE
ERROR
STALE
```

| 状态 | 含义 |
|---|---|
| IDLE | 当前没有活跃任务 |
| WORKING | Agent 正在执行 |
| ATTENTION | 等待用户批准、输入、选择或回答 |
| COMPLETE | 当前任务已经完成 |
| ERROR | 任务执行失败或异常退出 |
| STALE | 长时间没有新的 Hook / heartbeat，状态可信度下降 |

Claude Code 示例：

```text
UserPromptSubmit
→ WORKING

PermissionRequest / AskUserQuestion
→ ATTENTION

PostToolUseFailure / StopFailure
→ ERROR

Stop / SessionEnd
→ COMPLETE / IDLE
```

Codex 采用相同的最终状态模型。

#### AgentState 建议字段

```json
{
  "id": "codex-producttool-01",
  "provider": "codex",
  "project": "producttool",
  "sessionId": "optional",
  "status": "working",
  "summary": "Running tests",
  "startedAt": "2026-08-20T13:42:00+08:00",
  "updatedAt": "2026-08-20T13:48:21+08:00",
  "elapsedSeconds": 381,
  "source": "hook",
  "target": {
    "type": "agent-session",
    "app": "ghostty",
    "project": "producttool",
    "sessionId": "optional"
  }
}
```

`target` 在 V1 不执行任何动作，只为未来 MX Master 4 / Control Layer 预留。

---

### 4.2 系统状态：Glances

系统资源监控不自行实现，优先复用成熟项目 Glances。

V1 需要读取：

- CPU total；
- Memory total / used / available / percent；
- Swap；
- Disk；
- Network；
- Load；
- Process list。

默认重点跟踪进程：

```yaml
tracked_processes:
  - codex
  - claude
  - Ghostty
  - ChatGPT
```

Display 上优先显示：

```text
Codex   1.8 GB   22%
Claude  2.4 GB   17%
Ghostty 620 MB    3%
```

Glances 只作为 Data Source，不采用它的现代 Web UI 作为 Kindle UI。

---

### 4.3 Git / Project 状态

每个配置仓库读取：

```text
git branch --show-current
git status --porcelain
git rev-parse HEAD
```

可选 GitHub CLI：

```text
gh pr status
gh pr view
gh run list
```

统一 ProjectState：

```json
{
  "name": "producttool",
  "path": "/Users/.../producttool",
  "branch": "codex/m12",
  "dirty": true,
  "modified": 3,
  "untracked": 0,
  "ahead": 2,
  "behind": 0,
  "pr": {
    "number": 428,
    "state": "OPEN",
    "ci": "PASS"
  }
}
```

GitHub 网络信息不是 V1 必需；本地 Git 状态先可靠完成。

---

### 4.4 AI Quota：CodexBar

Quota 不是核心，只作为可插拔模块。

建议通过 CodexBar CLI 获取：

```bash
codexbar usage
codexbar usage --provider codex
codexbar usage --provider claude
```

统一成：

```json
{
  "provider": "codex",
  "windows": [
    {
      "name": "5h",
      "usedPercent": 68,
      "resetsAt": "..."
    }
  ]
}
```

可完全关闭：

```yaml
modules:
  quota: false
```

关闭后中间层与 Display 不应报错，也不影响 Agent/System/Git。

---

## 5. State Core

State Core 是唯一的 Display Authority。

任何 Display 都不能直接读取：

- `~/.codex`；
- `~/.claude`；
- Glances raw response；
- CodexBar raw JSON；
- Git 命令输出。

所有数据先归一化进入 State Core。

### RootState

```json
{
  "version": 1,
  "generatedAt": "2026-08-20T13:58:00+08:00",
  "host": {},
  "agents": [],
  "alerts": [],
  "projects": [],
  "quota": [],
  "meta": {}
}
```

### 更新规则

- Hook：事件驱动；
- System：定时轮询；
- Git：定时轮询；
- Quota：低频轮询；
- Display：只读。

V1 Store：

```text
Memory
+
state.json snapshot
```

不引入数据库。

---

## 6. Alert Engine

Alert Engine 负责把“状态”转换成“提醒优先级”。

```text
P0 ERROR
P1 ATTENTION
P2 COMPLETE
P3 WORKING
P4 INFO
```

### ATTENTION

一直保留，直到：

- Agent 恢复 WORKING；
- Session 结束；
- 该提醒被系统确认已过期。

### COMPLETE

建议生命周期：

```text
0–10 min     high visibility
10–30 min    recent
>30 min      hidden
```

### ERROR

保持高优先级，直到：

- 新任务开始；
- Session 明确恢复；
- 状态过期。

### Kindle 排序

```text
ERROR
ATTENTION
COMPLETE
WORKING
SYSTEM
PROJECT
QUOTA
```

真正需要人处理的东西始终位于屏幕最上方。

---

## 7. Display Layer

### 7.1 Modern Display

Endpoint：

```text
/display
```

支持：

- Desktop browser；
- iPhone；
- iPad；
- Android；
- 其他现代浏览器设备。

建议：

- responsive CSS；
- SSE；
- 少量 vanilla JavaScript；
- Screen Wake Lock API；
- 无 SPA Framework 必需依赖。

目标事件延迟：`< 1s`。

### 7.2 Old Kindle Display

Endpoint：

```text
/display/kindle
```

旧 Kindle 页面使用 Server Render：

```html
<meta http-equiv="refresh" content="20">
```

可配置：

```yaml
display:
  kindle_refresh_seconds: 20
```

禁止依赖：

- Fetch API；
- Promise；
- WebSocket；
- EventSource；
- modern CSS Grid；
- complex SVG；
- 动画。

E-Ink 视觉原则：

- 黑白高对比；
- 大字号；
- 减少细线；
- 不依赖颜色表达状态；
- 状态必须同时有文字和符号；
- 重要提醒使用粗边框；
- 减少页面大面积频繁变化；
- 不做秒级时钟。

---

## 8. 页面信息结构

```text
┌──────────────────────────────┐
│ DEV STATUS          13:58    │
├──────────────────────────────┤
│ ACTION REQUIRED              │
│ ! CLAUDE · Gift              │
│   Permission required  01:42 │
├──────────────────────────────┤
│ AGENTS                       │
│ ● CODEX · producttool  08:42 │
│   WORKING · tests            │
│                              │
│ ✓ CODEX · FloatTabs    12:31 │
│   COMPLETE                   │
├──────────────────────────────┤
│ SYSTEM                       │
│ CPU 24%      MEM 17.2 / 24G  │
│ SWAP 1.1G    DISK 71%        │
│ Codex 1.8G   Claude 2.4G     │
├──────────────────────────────┤
│ PROJECTS                     │
│ producttool  M3  PR #428 ✓   │
│ Gift         CLEAN           │
├──────────────────────────────┤
│ AI LIMITS                    │
│ Codex 5h 68%   Claude 43%    │
├──────────────────────────────┤
│ Updated 13:58                │
└──────────────────────────────┘
```

优先级：

| 模块 | V1 优先级 |
|---|---:|
| Agent Working/Attention/Error/Complete | P0 |
| Hook Alerts | P0 |
| Agent elapsed time | P0 |
| Mac CPU / Memory / Swap | P1 |
| Agent process CPU / RAM | P1 |
| Git status | P1 |
| PR / CI | P2 |
| Disk / Network | P2 |
| AI quota | P3 |

---

## 9. Refresh Strategy

不同数据源使用不同频率。

### Hook

事件驱动，收到后立即更新。

### System

```text
5s
```

必要时可调 10s。

### Git

```text
15–30s
```

只有已配置仓库参与轮询。

### Quota

```text
120–300s
```

严禁和 Kindle 页面刷新频率绑定。

### Modern Display

SSE 推送。

### Old Kindle

默认每 20 秒完整 GET 一次。

后续根据实机残影和浏览器稳定性可调 30 / 60 秒。

---

## 10. API Contract

```text
GET  /api/state
GET  /api/agents
GET  /api/system
GET  /api/projects
GET  /api/quota
POST /hooks/codex
POST /hooks/claude
GET  /events
GET  /display
GET  /display/kindle
GET  /health
```

`/events` 只服务现代浏览器 SSE。

---

## 11. Security Model

V1 默认为本地局域网工具。

默认：

```text
bind: 127.0.0.1
```

明确开启 LAN 后：

```text
bind: 0.0.0.0
```

必须遵守：

- 不在网页暴露 OAuth Token；
- 不把 `~/.claude/.credentials.json` 内容传给 Display；
- 不把 Codex auth/token 传给客户端；
- Hook endpoint 只接受 localhost，或要求 secret；
- `/api/state` 不包含敏感命令、Prompt 正文或凭据；
- Project path 可配置隐藏完整绝对路径；
- 日志禁止记录 secret。

外网访问与多用户 auth 不进入 V1。

---

## 12. Resource Budget

这是常驻工具，必须轻量。

目标：

- Core idle CPU 接近 0；
- Memory 尽量 `< 100 MB`；
- 没有 Electron；
- 没有嵌入 Chromium；
- 没有 Node 常驻前端构建链；
- 不存高频历史指标；
- 不做秒级图表。

---

## 13. 技术栈建议

### Core

Go。

理由：

- 单二进制；
- 内存可控；
- HTTP/SSE 原生；
- subprocess 简单；
- JSON 简单；
- macOS / Linux 易支持；
- launchd 部署简单；
- 适合长期后台常驻。

### Frontend

```text
Go html/template
+
CSS
+
少量 vanilla JS
```

V1 不使用 React / Vue 作为必要依赖。

### Storage

```text
memory
+
state.json
```

V2 若需要历史数据，再引入 SQLite。

---

## 14. 配置文件

建议 `config.yaml`：

```yaml
server:
  host: 0.0.0.0
  port: 8787

display:
  kindle_refresh_seconds: 20
  complete_highlight_minutes: 10
  complete_keep_minutes: 30

modules:
  agents: true
  system: true
  git: true
  quota: true

system:
  glances_url: http://127.0.0.1:61208
  poll_seconds: 5
  tracked_processes:
    - codex
    - claude
    - Ghostty
    - ChatGPT

projects:
  poll_seconds: 20
  items:
    - name: producttool
      path: /Users/me/Projects/producttool
    - name: Gift
      path: /Users/me/Projects/Gift

quota:
  provider: codexbar
  poll_seconds: 180

hooks:
  secret: optional-local-secret
```

---

## 15. 建议仓库结构

```text
dev-status-board/
│
├── cmd/
│   └── devboard/
│       └── main.go
│
├── internal/
│   ├── state/
│   │   ├── model.go
│   │   └── store.go
│   ├── collectors/
│   │   ├── system/
│   │   ├── git/
│   │   ├── quota/
│   │   └── agents/
│   ├── hooks/
│   │   ├── codex.go
│   │   └── claude.go
│   ├── alerts/
│   │   └── engine.go
│   └── web/
│       ├── server.go
│       └── render.go
│
├── web/
│   ├── templates/
│   │   ├── display.html
│   │   └── kindle.html
│   └── static/
│       └── app.css
│
├── Docs/
│   └── Dev_Status_Board_Architecture_and_Implementation_Plan_2026-08-20.md
│
├── launchd/
│   └── com.devboard.agent.plist.example
│
├── config.example.yaml
├── go.mod
├── LICENSE
└── README.md
```

---

## 16. V1 施工阶段

### M0 — Contract Freeze

目标：

- 冻结 RootState；
- 冻结 Agent status；
- 冻结 Alert lifecycle；
- 冻结 Display API；
- 冻结 Old Kindle compatibility boundary。

验收：

- 不写业务实现；
- schema 与文档完整；
- 后续 Collector 不允许绕过 State Core。

### M1 — Core + Static Display

实现：

- Go 项目；
- config；
- State Store；
- `/health`；
- `/api/state`；
- `/display`；
- `/display/kindle`；
- mock state。

验收：

- Mac / iPhone / Kindle 都能打开；
- Kindle 可以连续运行；
- 页面刷新稳定；
- 无现代 JS 必需依赖。

### M2 — Agent Hooks

实现：

- Codex hook endpoint；
- Claude hook endpoint；
- status normalization；
- Agent elapsed；
- stale detection；
- Alert Engine。

验收：

- WORKING；
- ATTENTION；
- COMPLETE；
- ERROR；
- 多 Agent 并存；
- Kindle 按优先级排序。

### M3 — System Metrics

实现：

- Glances adapter；
- CPU；
- memory；
- swap；
- disk；
- tracked process。

验收：

- 资源信息与 Activity Monitor 基本一致；
- Glances 挂掉时页面降级而非整体失败；
- stale 信息明确标识。

### M4 — Project / Git

实现：

- branch；
- dirty；
- modified/untracked；
- ahead/behind；
- optional PR / CI。

验收：

- 多仓库支持；
- 单仓库错误不影响其他仓库；
- 未安装 gh 时仍正常显示本地 Git。

### M5 — Quota Module

实现：

- CodexBar adapter；
- Codex / Claude quota；
- reset；
- disabled mode。

验收：

- quota 完全关闭时系统正常；
- quota fetch 失败只显示 unavailable；
- 不影响 Agent 和 System。

### M6 — Production Runtime

实现：

- launchd；
- health；
- log rotation；
- graceful shutdown；
- config validation；
- startup self-check。

验收：

- Mac 重启后自动运行；
- Core crash 后可恢复；
- Kindle 无需重新配置 URL；
- 日志有保存上限。

---

## 17. V2 — Control Layer

V2 才引入操作能力。

```text
MX Master 4
    ↓
Action Ring
    ↓
POST /actions/focus/:agent
    ↓
Control Core
    ↓
Ghostty / Codex Desktop / Claude
```

候选动作：

- Focus Agent；
- Open Project；
- Focus Ghostty Tab；
- Open Codex Desktop Thread；
- Jump to ATTENTION；
- Retry；
- Approve；
- Stop；
- Open PR；
- Open Diff。

### MX Master 4 协同

未来可以接：

- Logi Actions Ring；
- Haptic；
- Easy Switch；
- 键盘 backlight；
- Agent status → Haptic；
- Agent attention → Keyboard backlight。

V1 只保留 `target` contract，不实现动作。

---

## 18. Open-source Dependencies / References

### Glances

用途：CPU、Memory、Swap、Disk、Network、Process、REST API。

定位：**System Metric Authority**。

### CodexBar

用途：Codex / Claude quota、reset、usage、future providers。

定位：**Optional Quota Adapter**。

### OpenMicro / Agent Integration References

用途：Codex / Claude Hook installation patterns、Agent event normalization、attention / complete / error semantics。

定位：**Agent integration reference，不作为运行时硬依赖**。

---

## 19. 非目标

V1 明确不做：

- 自己重新实现完整系统监控；
- 自己重新实现 Codex quota protocol；
- 自己重新实现 Claude quota protocol；
- Electron；
- SPA；
- 数据云同步；
- 多用户；
- 外网 SaaS；
- 远程控制 Agent；
- 完整历史指标数据库；
- E-ink 专用图像渲染 pipeline；
- Kindle jailbreak / FBInk 依赖。

---

## 20. 成功标准

### 使用层

打开任何浏览器即可看到：

- 当前 Agent；
- 是否正在执行；
- 是否等待操作；
- 是否已经完成；
- 是否报错；
- Mac 当前状态；
- 关键 Agent 进程资源；
- 项目 Git 状态；
- 可选 AI quota。

### Kindle

旧 Kindle：

- 不越狱；
- 通过原生 browser 打开；
- 页面长期常驻；
- 15–60 秒刷新可配置；
- 页面结构稳定；
- 无复杂 JS；
- 高对比 E-Ink UI。

### 架构

- 新 Collector 可以插拔；
- Display 不知道 Collector 内部实现；
- Quota 可关闭；
- Glances 可失败降级；
- 一个 Agent 异常不影响其他 Agent；
- State Contract 可以直接支撑未来 Control Layer。

---

## 21. 最终架构原则

这个项目最重要的不是 Kindle。

Kindle 只是当前最合适的低功耗 Display。

真正需要长期保留的是：

```text
Collectors
    ↓
Unified State Core
    ↓
Display
    ↓
Future Control
```

只要这四层保持清晰，后续：

- 增加新的 AI Agent；
- 增加新的系统监控；
- 增加手机 Display；
- 增加桌面悬浮窗；
- 增加 MX Master 4；
- 增加 Haptic；
- 增加键盘背光；
- 增加远程控制；

都不需要推翻 V1。

因此 V1 的施工重点顺序必须是：

```text
State Contract
→ Agent Events
→ Display
→ System
→ Project
→ Quota
→ Runtime
```

而不是先做复杂 UI。
