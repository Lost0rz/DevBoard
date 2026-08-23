# DevBoard M6 Browser AI Watch — Reference Audit

> 日期：2026-08-23
> 审计基线：`36e688fe6c38b834efe06a596059c89aff0a2b86`
> 基线分支：`codex/pc1-integration-closure`
> 审计分支：`codex/m6-browser-ai-watch-reference-audit`
> 范围：Chrome/Chromium + ChatGPT Web 首个实现批次的参考审计与架构建议
> 状态：**M6_REFERENCE_AUDIT=READY_FOR_CORE_AUDIT**

本文件只冻结参考结论和设计建议，不实现 M6，不修改现有冻结合同，不修改 Go、JavaScript、Swift、模板或部署文件。

本审计完全绕开真实 NAS：没有 NAS SSH、部署、验收、远程接收端或真实 Chrome profile 操作；没有安装浏览器扩展；没有使用真实会话 transcript 作为测试数据；没有修改 GitHub Issue/PR；没有创建 PR。

## 1. 审计结论摘要

首个实现批次推荐：

```text
用户 action 明确选择当前 ChatGPT 会话
→ activeTab 临时授予当前 tab
→ scripting 动态注入一个 dormant-until-selected 的隔离世界 content script
→ ChatGPT adapter 只观察允许的 DOM/ARIA 状态信号
→ MutationObserver + 小型确定性状态机
→ content script 只发送净化后的状态信号给 MV3 service worker
→ service worker 通过受认证的 loopback POST 送入本机 DevBoard Node
→ Node 在本地 reducer 中形成 BrowserWatchState
→ 既有 PublicState projector 输出净化后的 Browser AI Watch
→ 既有 Node → Hub PublicState 拓扑继续工作
```

这是本轮比较的方案中“侵入性最低、权限最小、可维护性最高”的组合。它仍然承认 ChatGPT Web DOM 是不稳定的非官方观察面，因此 adapter 失配时必须降级为 `source health=degraded`，而不是改用网络拦截、屏幕 OCR 或猜测。

明确选择：

- Manifest V3 作为扩展运行时基础；
- `activeTab` + `scripting` + 用户 action 作为会话选择与一次性注入边界；
- isolated-world content script；
- ChatGPT-specific `MutationObserver` 与确定性状态机；
- MV3 service worker 只做消息校验、选择状态、净化、发送和有限的当前状态缓冲；
- 本机 Node 的固定 loopback HTTP POST；
- Node 是浏览器事件的本地权威入口，Hub 只看到既有 PublicState 投影。

明确不选择：

- `chrome.debugger` / Chrome DevTools Protocol；
- `webRequest`、代理或网络流量拦截；
- 历史 API、全局 tab 轮询或任意网页监控；
- 通用截图、桌面捕获、OCR；
- ChatGPT 网络接口、`fetch`/WebSocket 劫持；
- 使用 Chrome 系统通知或 OS accessibility API 作为状态来源；
- 完整 DOM、prompt、回复、transcript、cookie、Authorization header 或 session token 的采集。

## 2. 冻结合同约束映射

本审计已完整阅读：

- [`Docs/contracts/mvp-monitoring-v1.md`](contracts/mvp-monitoring-v1.md)
- [`Docs/contracts/mvp-feature-freeze-v1.md`](contracts/mvp-feature-freeze-v1.md)
- [`Docs/contracts/reference-first-integration-v1.md`](contracts/reference-first-integration-v1.md)
- [`Docs/contracts/m4-task-observability-v1.md`](contracts/m4-task-observability-v1.md)
- [`Docs/contracts/m5-multi-host-v1.md`](contracts/m5-multi-host-v1.md)

对 M6 的直接约束如下：

| 合同约束 | M6 设计回应 |
| --- | --- |
| 只监控用户明确选择的 AI 会话 | 只在当前 tab 的用户 action 后动态注入并建立 `watchSessionId`；没有选择时 content script 不启用 observer。 |
| 不监控任意网页或任意标签 | 不使用 `<all_urls>`、全局静态 content script、`tabs`/`history` 扫描；首次只支持明确的 ChatGPT host，并以当前 tab 选择为入口。 |
| 不采集完整 DOM | 只读取 selector adapter 所需的有限按钮/ARIA/当前状态节点；不读取或序列化 `document.body.innerText`、`innerHTML`、`outerHTML` 或完整节点树。 |
| 不保存 prompt、回复或 transcript | 状态机只保存枚举、时间、计数器和安全标签；首批不生成摘要。任何候选文本都不得进入 Node 请求。 |
| 不采集 cookie、Authorization header、session token | 不请求 `cookies`、`webRequest`、`debugger`、`proxy`；扩展不读取 ChatGPT 网络层。Node token 只属于 DevBoard 本地链路，不是 ChatGPT 凭证。 |
| 不绕过登录、CSP、浏览器权限或安全机制 | 只使用用户 action 授予的 `activeTab`；不注入 MAIN world，不修改 CSP，不执行页面操作。无法注入时明确降级。 |
| 不执行远程控制、自动回复或页面操作 | Node 接口只接受观测事件；没有 Node → extension 命令、点击、输入、导航、聚焦、重试或回复路由。 |
| 不实现 Safe Navigation | 不新增 navigation target，不启用任何控制动作。 |
| 不修改 Node → Hub 拓扑 | extension → 本机 Node；Node 继续复用已有 PublicState projector 和既有 Node → Hub sanitized snapshot。 |
| Hub 不存在时仍可验证 | `/api/state` 与本地 display 继续工作；Node endpoint 用 fake receiver/`httptest`、模拟事件和离线 Hub 测试验证。 |
| source health/freshness 诚实 | `browser-ai-watch` 以独立 SourceHealth 表示未选择、可用、失配、断连和过期；不把旧状态当作当前状态。 |

M4 的“source failure fail-open”原则同样适用：扩展、DOM adapter、loopback endpoint 的失败只能影响 Browser AI Watch source，不得停止或改变 Codex/Claude agent 执行。

## 3. 资料来源与检索日期

检索日期统一为 **2026-08-23（Asia/Shanghai）**。Chrome 文档、OpenAI 官方资料和 GitHub 固定提交均在该日期核查；开源代码在仓库外临时目录审阅，没有复制进 DevBoard。

### 3.1 Chrome/Chromium 官方资料

| 资料 | 本审计使用的事实 |
| --- | --- |
| [Manifest V3 overview](https://developer.chrome.com/docs/extensions/develop/migrate/what-is-mv3) | MV3 使用短生命周期 service worker；禁止远程托管代码；不能依赖长期存活的后台页面。 |
| [Manifest file format](https://developer.chrome.com/docs/extensions/mv3/manifest) | `background.service_worker`、`content_scripts`、`permissions`、`optional_host_permissions` 的边界。 |
| [The `activeTab` permission](https://developer.chrome.com/docs/extensions/develop/concepts/activeTab) | 用户 action 后只对当前 tab 临时授予 host access；跨 origin 导航或关闭 tab 后撤销；不产生安装警告。 |
| [Content scripts](https://developer.chrome.com/docs/extensions/develop/concepts/content-scripts) | content script 可读 DOM、通过消息与扩展通信，默认运行于 isolated world；不能直接使用所有 Chrome API。 |
| [`chrome.scripting`](https://developer.chrome.com/docs/extensions/reference/api/scripting) | 动态注入需要 `scripting` 加 `activeTab` 或 host permission；默认执行世界为 isolated。 |
| [`chrome.runtime` message passing](https://developer.chrome.com/docs/extensions/develop/concepts/messaging) | content script 与 service worker 使用 JSON-serializable one-time message；跨组件边界必须显式校验消息。 |
| [Extension service worker lifecycle](https://developer.chrome.com/docs/extensions/develop/concepts/service-workers/lifecycle) | service worker 会因空闲终止；状态不能依赖全局变量，必须使用受控存储和事件触发。 |
| [`chrome.storage`](https://developer.chrome.com/docs/extensions/reference/api/storage) | `storage.session` 适合 service worker 的短期内存状态，浏览器重启时清空；`storage.local` 可持久化但不应存 transcript 或同步敏感数据。 |
| [`chrome.tabs`](https://developer.chrome.com/docs/extensions/reference/api/tabs) | `tabs` 许可主要暴露 URL/title/favicon 等敏感 Tab 字段；当前方案只使用 action 回调和 sender tab identity，不申请 `tabs`。 |
| [Declare permissions](https://developer.chrome.com/docs/extensions/develop/concepts/declare-permissions) | 能力应按需声明，优先 optional permissions；host pattern 和 content-script match 也会触发用户警告。 |
| [Permission warning guidelines](https://developer.chrome.com/docs/extensions/develop/concepts/permission-warnings) | 单一目的、最小权限、optional permission 和 `activeTab` 是减少授权面的官方建议。 |
| [`chrome.notifications`](https://developer.chrome.com/docs/extensions/reference/api/notifications) | 该 API 是扩展向系统托盘创建通知的输出能力，不是 ChatGPT 会话状态读取 API；首批不申请。 |
| [`chrome.debugger`](https://developer.chrome.com/docs/extensions/reference/api/debugger) | debugger 许可可 attach tab 并访问多个 CDP domain；权限警告和可访问面远超本需求。 |
| [Chrome DevTools Protocol](https://chromedevtools.github.io/devtools-protocol/) | CDP tip-of-tree 变化频繁且不保证向后兼容；不适合作为首批页面状态契约。 |
| [`chrome.history`](https://developer.chrome.com/docs/extensions/reference/api/history) | 需要 `history` 许可，访问浏览器访问记录；与选择单一会话无关。 |
| [`chrome.webRequest`](https://developer.chrome.com/docs/extensions/reference/api/webRequest) | 需要 webRequest 和 host permissions；可见 URL/headers/request lifecycle，但不提供已建立 WebSocket 的单条消息，且会扩大敏感数据风险。 |

### 3.2 OpenAI/ChatGPT 官方资料

| 资料 | 可公开确认的行为与边界 |
| --- | --- |
| [Scheduled Tasks in ChatGPT](https://help.openai.com/en/articles/10291617-tasks-in-chatgpt) | ChatGPT Web 提供任务管理和用户选择的 push/email/desktop notification；官方材料描述的是任务通知和浏览器通知许可，不是一个供本地扩展读取当前普通会话生成状态的稳定 API。 |
| [Using the built-in browser in the ChatGPT desktop app](https://help.openai.com/en/articles/20001277-using-the-built-in-browser-in-the-chatgpt-desktop-app) | ChatGPT desktop built-in browser 有自己的浏览器状态和扩展/登录能力；不能将其状态假定为用户 Chrome profile 的状态。 |
| [OpenAI Conversations API reference](https://platform.openai.com/docs/api-reference/conversations) | 这是 OpenAI API 的 conversation resource，不是 ChatGPT Web 页面 DOM 或登录会话的本地监听接口；不把 API 凭证或 API conversation 接入 M6。 |

由以上资料得出的限定性推论是：OpenAI 官方公开资料可确认 ChatGPT Web 的用户通知和 Web 任务体验，但本审计没有发现可作为 M6 依赖的“普通 ChatGPT Web 会话 working/completed/attention”本地官方接口。因此 DOM adapter 只能是显式、可降级的参考实现，不得宣称 provider-authoritative。

### 3.3 维护中的开源浏览器 AI 状态监控参考

#### `dawidstruzik/chat-alert`

- 仓库：[github.com/dawidstruzik/chat-alert](https://github.com/dawidstruzik/chat-alert)
- 固定审阅提交：[665be3d48362c3d1a2b08f009f78a47b66d94ef9](https://github.com/dawidstruzik/chat-alert/tree/665be3d48362c3d1a2b08f009f78a47b66d94ef9)
- 最近提交（审阅时）：2026-01-05，增加通知音频。
- 许可证：MIT，见仓库 [LICENSE](https://github.com/dawidstruzik/chat-alert/blob/665be3d48362c3d1a2b08f009f78a47b66d94ef9/LICENSE)。
- 有用行为：ChatGPT host allow-list、Stop button 出现/消失、稳定窗口、每 tab 状态机、MV3 service worker 与本地通知。
- 不采用部分：`MAIN` world `fetch` interceptor、读取 `/backend-api/conversation/` 的 `update_time`、回复 preview、宽泛 `tabs`/`notifications`/`offscreen` 权限、自动监控新 ChatGPT tab。
- 决策：**USE AS BEHAVIORAL REFERENCE ONLY**。MIT 允许代码再利用，但该实现的网络拦截和正文 preview 与 M6 冻结隐私边界冲突；本轮不复制源码。

#### `kkonstantin08/chatgpt-done-notifier`

- 仓库：[github.com/kkonstantin08/chatgpt-done-notifier](https://github.com/kkonstantin08/chatgpt-done-notifier)
- 固定审阅提交：[54381bf1c3362570d0dd8186dc258fa0c35e95ad](https://github.com/kkonstantin08/chatgpt-done-notifier/tree/54381bf1c3362570d0dd8186dc258fa0c35e95ad)
- 最近提交（审阅时）：2026-05-18，修复 race condition、错误处理和数值校验。
- 许可证：审阅提交未发现 `LICENSE`/`COPYING` 文件，`package.json` 也未声明许可证；因此不复制代码。
- 有用行为：`MutationObserver`、ChatGPT selector adapter、Stop button + assistant activity + stabilization window、manual stop、error suppression、每 generation cycle dedupe，以及针对 DOM/state machine 的 Vitest 测试。
- 不采用部分：完整 assistant fingerprint（首尾文本片段）、宽泛 error body 扫描、debug log 内容设计和其“notification”产品面。
- 决策：**USE AS BEHAVIORAL REFERENCE ONLY**。只吸收状态机和测试维度，不吸收源码或其字段。

## 4. 候选方案比较

评分含义：5 为最有利，1 为最不利；“侵入性”分数越高越低侵入，“权限最小”越高越好，“维护性”越高越好。分数是针对 DevBoard M6 冻结边界的架构判断，不是对技术在其他产品中的通用评级。

| 方案 | 可得信号 | 权限/数据面 | 侵入性 | 权限最小 | 维护性 | 决策 |
| --- | --- | --- | ---: | ---: | ---: | --- |
| 1. Manifest V3 扩展 | 事件驱动生命周期、用户 action、content script、service worker、受控 storage | 可以做到只申请 `activeTab`、`scripting`、`storage`；loopback host access 作为可选权限 | 5 | 5 | 4 | **USE**，作为运行时基础，不单独等于状态检测。 |
| 2. opt-in content script + MutationObserver | 选定 tab 内的 Stop/ARIA/有限 DOM 变化；可建立 working→completed 的局部信号 | 不读网络层、不读 cookie；只在用户选定 tab 注入；DOM 结构依赖明显 | 5 | 5 | 4 | **RECOMMENDED**，首批唯一的 ChatGPT 状态检测机制。 |
| 3. 浏览器通知/可访问性状态 | Chrome notifications 能发送通知；页面 ARIA label 可作为 DOM selector；OS accessibility 不是普通 ChatGPT 会话 API | `notifications` 是额外输出权限；accessibilityFeatures 权限/平台面不提供所需语义；官方 ChatGPT 任务通知不能代表普通会话 | 3 | 2 | 2 | **仅保留页面 ARIA 作为 adapter 输入**；不读 OS notification，不申请通知权限，不以通知作为 source of truth。 |
| 4. Chrome DevTools Protocol | DOM/Runtime/Network/Accessibility 等广泛调试域；可直接观察和执行 | `debugger` 权限警告“Access page debugger backend / Read and change all data”；可触达网络、DOM、运行时和 storage | 1 | 1 | 2 | **REJECT**，权限面、控制能力、协议不稳定性均超过只读状态需要。 |
| 5. 本地浏览器历史或标签轮询 | tab URL/title、history visit、关闭/更新事件；不能可靠得到生成状态 | `history`/`tabs`/`webNavigation` 会扩大浏览活动面；轮询任意 tab 违反显式选择边界 | 2 | 1 | 2 | **REJECT** 作为检测器；可在不读取正文的情况下使用已选择 tab 的生命周期事件做辅助清理。 |
| 6. 通用屏幕截图/OCR | 像素级按钮、文字、视觉状态 | desktop/tab capture 权限、敏感屏幕数据、OCR 误识别、CPU 和测试成本 | 1 | 1 | 1 | **REJECT**，不符合隐私和维护性目标。 |
| 7. 代理或网络流量拦截 | request lifecycle、部分 headers、WebSocket handshake；不能稳定提供应用消息语义 | `webRequest`/host permission/代理扩大 URL/header 暴露；有 Authorization/Cookie 误收集风险；不获取已建立 WebSocket 单条消息 | 1 | 1 | 1 | **REJECT**，明确禁止读取或拦截 ChatGPT 网络流量。 |

### 4.1 选择依据

方案 2 的优点不是“DOM 永远稳定”，而是它把失配成本限制在一个 provider adapter 中，并且不扩大账户/网络/屏幕权限。方案 2 必须配合以下约束才能成立：

1. observer 只在用户 action 后被注入；
2. adapter 只返回有限 signal，不返回 DOM 或文本；
3. 状态机必须先观察到 working cycle 和 assistant-side activity，Stop 消失后再经过稳定窗口才可发出 completed/new reply；
4. attention 只能来自已审计的明确用户等待信号，不能将普通 idle 当作 attention；
5. adapter 找不到信号时停发业务结论并报告 degraded；
6. 每个 event 有 `watchSessionId + sequence`，Node 做去重、乱序拒绝和 freshness 计算；
7. 用户刷新、跨 origin 导航、关闭 tab、浏览器重启后不得隐式恢复选择。

## 5. 推荐架构

### 5.1 组件边界

```text
ChatGPT Web page (untrusted DOM)
        │ user action only
        ▼
MV3 action/popup
        │ activeTab + scripting
        ▼
isolated-world content script
  ├─ selection binding (private route fingerprint only)
  ├─ ChatGPT adapter (selectors + ARIA only)
  ├─ MutationObserver
  └─ deterministic BrowserWatch state machine
        │ chrome.runtime.sendMessage (normalized JSON only)
        ▼
MV3 service worker
  ├─ validates sender/selection/schema
  ├─ stores only current bounded watch state
  ├─ POSTs to fixed loopback Node endpoint
  └─ no page control / no network inspection
        │ authenticated POST, exact Origin + Host
        ▼
DevBoard Node (local Go process, local authority)
  ├─ BrowserWatch reducer + bounded current-state store
  ├─ source health/freshness
  ├─ PublicState projector (allow-list)
  └─ existing Node → Hub sanitized PublicState path
        │ existing M5/M5.4 topology
        ▼
Hub receives only sanitized PublicState
```

DevBoard Node 在这里指本机 DevBoard Node role，不是要求引入 Node.js 服务。浏览器扩展不直接请求 Hub，也不写 Hub store。

### 5.2 用户选择/取消监控语义

推荐第一批只提供明确的当前-tab action：

1. 用户打开一个 ChatGPT Web conversation；
2. 用户点击扩展 action，扩展校验当前 tab origin 是 `https://chatgpt.com` 或经过 Core Auditor 批准的 legacy `https://chat.openai.com`；
3. 首次点击建立 `watchSessionId`，显示“仅监控此会话的状态，不保存 prompt/回复”的确认与可选安全 label；
4. service worker 使用该 action 的 `activeTab` 临时权限，以 `scripting.executeScript` 注入 isolated-world observer；
5. observer 只把选定 conversation 的状态 signal 发回扩展；
6. 再次点击同一 tab 的 action，或扩展 UI 的“取消监控”，发送 `watch_stopped`，断开 observer，删除当前 selection；
7. 如果路由指向另一个 conversation、origin 变化、tab 被关闭、扩展被 reload 或浏览器重启，selection 不自动迁移到新会话；需要用户重新 action。

可选 label 只接受用户明确输入或扩展生成的常量，例如 `ChatGPT conversation`；首批不从页面标题、侧栏、prompt 或回复中自动抽取 label。用户输入 label 仍要做控制字符、长度和高熵/token-like 拒绝。

这使多个 tab 可以分别 opt-in，但不要求扩展扫描所有 tab。第一次施工批次不提供“列出所有未选择 ChatGPT tab”的能力；如果以后增加此 UI，必须单独复核 `tabs` 权限是否必要。

### 5.3 ChatGPT adapter 与 MutationObserver

首批 adapter 只允许以下输入：

- Stop/stop-generating/stop-streaming 等经 fixture 和真实授权 E2E 核验的 button signal；
- 当前 assistant turn 是否存在及其有限结构变化（不传 text）；
- 明确的 `aria-label`/`role`/`data-testid` state signal；
- 明确的错误 marker 的枚举化结果，不传错误文案；
- 页面 route 是否仍是用户选择的同一 conversation 的私有判断。

禁止的实现手段：

- `window.fetch`/XHR/WebSocket override；
- `window.postMessage` MAIN-world bridge；
- `document.body.innerText` 全文扫描；
- `innerHTML`/`outerHTML`/完整 DOM snapshot；
- 读取 prompt、assistant reply、code block、attachment、URL query 或 markdown；
- 通过 CSS class 之外的高熵内容猜测 conversation identity；
- 对页面执行 click、input、submit、navigate、focus、scroll 或任何控制。

MutationObserver 应只驱动 adapter 的重新检查；在没有 observer root 时允许有限、退避的 bootstrap 检查。active cycle 期间可以有小间隔的观察补偿，但必须合并重复 signal，不得每个 DOM mutation 都发网络请求。

### 5.4 source health 与 freshness

根级 source key 建议为 `browser-ai-watch`，不要为每个 conversation 创建一个 SourceHealth 条目。每个 watch 的业务状态单独保存在 bounded BrowserWatchState 中。

| 条件 | BrowserWatch state | SourceHealth | Freshness |
| --- | --- | --- | --- |
| 没有用户选择 | 无业务状态 | `unavailable`，通用 message `No conversation selected.` | 无 |
| 已选、adapter 已观察到合法 signal、Node 最近收到 heartbeat | `working`/`complete`/`attention` | `available` | `fresh` |
| DOM selector 失配、未知 provider UI 或无法确认 route | 保留 last-good 但不产生新业务结论 | `degraded` | `stale` |
| Node endpoint 未配置、认证失败、无法连接 | 保留 extension 侧单个 current state，不宣称 Node 已接收 | Node source `unavailable` | stale 后丢弃 |
| tab 关闭、浏览器重启、扩展 reload | 发 stop 事件为 best effort；否则等待 TTL | `unavailable` 或 `degraded`，按是否仍有 last-good | stale，过 TTL 删除 |

建议的首批阈值（Core Auditor 可调整）：Node 以 receive time 计算 freshness，30 秒内 fresh，30–120 秒 stale，超过 120 秒 unavailable；last-good BrowserWatch 内容保留不超过 5 分钟，之后只保留“已选择/未选择”所需的最小 source metadata，不保留旧业务事实。`observedAt` 仍保留为 source fact，但 UI age 以 Node receive time 为主，防止浏览器时间漂移误导。

## 6. 权限和威胁模型

### 6.1 最小 permissions 建议

首批 `manifest.json` 建议：

```json
{
  "manifest_version": 3,
  "permissions": ["activeTab", "scripting", "storage"],
  "optional_host_permissions": ["http://127.0.0.1/*"]
}
```

说明：

- `activeTab`：用户 action 后的当前 tab 临时 access；不申请 ChatGPT persistent host permission；
- `scripting`：仅用于把 observer 注入当前用户选择的 tab；
- `storage`：`storage.session` 存当前 selection、sequence 和最后一个 bounded state；`storage.local` 只存本地 pairing token/config，且设置 `TRUSTED_CONTEXTS` access level；
- `optional_host_permissions`：只有用户在扩展设置中明确配置并启用本机 DevBoard Node 时才请求 loopback fetch access；它不是 ChatGPT host access，也不允许任意网络；
- 不使用 `content_scripts.matches`，避免安装后自动在所有 ChatGPT tab 注入；不使用 `<all_urls>`。

首批不申请：`tabs`、`history`、`webNavigation`、`notifications`、`offscreen`、`debugger`、`webRequest`、`webRequestBlocking`、`declarativeNetRequest`、`cookies`、`proxy`、`desktopCapture`、`tabCapture`、`nativeMessaging`、`userScripts`、`alarms`、`identity`、`externally_connectable`。

如果 Core Auditor 要求浏览器重启后自动恢复 watch，必须重新审查 optional ChatGPT host permission 和静态 content script 的权限警告；本审计推荐不做自动恢复，以保持“每次浏览器生命周期明确选择”的最小授权语义。

### 6.2 威胁模型

| 资产/边界 | 威胁 | 控制 |
| --- | --- | --- |
| ChatGPT DOM 中的 prompt/reply | selector 或 debug log 意外泄露正文 | adapter 只返回枚举/计数；禁止全文读取、原文 log、原文 error；privacy tests 放 canary strings。 |
| ChatGPT 页面脚本 | 页面尝试篡改 observer 或消息 | isolated world；不使用 MAIN world；service worker 校验 `sender.tab`、watchSession、schema 和 host；不信任页面提供的 label/URL。 |
| 扩展 service worker | MV3 重启造成错序/丢状态 | 监听器在顶层注册；状态保存在 `storage.session`；event 使用 watchSession + sequence；只保留当前 bounded state，不建 transcript queue。 |
| 本机 loopback Node | 其他本地进程伪造 browser event 或网页 CSRF | Node 只 bind `127.0.0.1`；精确 `Host`；精确 extension `Origin`；Bearer token；POST/JSON only；无 cookie；无 wildcard CORS；body cap；不跟随 redirect。 |
| Node bearer token | token 泄露到 DOM、日志或 PublicState | token 只在 service worker trusted context 和 Node config；不发给 content script；不写 log/错误；不进入 PublicState/Hub。 |
| 浏览器重启/旧 tab 恢复 | 旧 selection 被误认为仍获用户授权 | `storage.session` 清空后不自动 resume；动态 script 不作为持久授权；Node TTL 让旧状态过期。 |
| DOM churn / event flood | CPU、loopback POST flood、状态抖动 | MutationObserver 触发合并；状态变更才发 event；heartbeat 固定间隔；按 watch bounded current state coalescing；Node body/rate/sequence bound。 |
| provider UI 升级 | 错报 working/completed/attention | adapter capability 版本化；selector fixtures；unknown→degraded；不 fallback 到 OCR/network。 |
| Node → Hub | 未净化浏览器字段进入多主机/Hub | Node 的 PublicState projector 只 allow-list public fields；现有 snapshot validator；Hub 不接受 extension event。 |
| 依赖/供应链 | 扩展加载远程脚本或过度依赖 | MV3 包内代码；无 CDN/eval/remote code；固定依赖和审计；不复制无许可证参考源码。 |

### 6.3 loopback HTTP、Origin、Host、CSRF 提案

以下是第一施工批次应冻结的 endpoint 安全契约，当前仅为设计：

- URL：`http://127.0.0.1:8787/api/node/v1/browser-ai/events`；端口如未来可配置，仍只允许当前 Node listener 的 `127.0.0.1:<port>`；
- bind：Node 只 bind IPv4 loopback，首批不 bind `0.0.0.0`、局域网、IPv6、Unix socket 或 NAS 地址；
- Host：拒绝 `localhost`、其他 IP、缺失 port、外部 host 和代理转发 Host；只接受精确 `127.0.0.1:<configured-port>`；
- Origin：生产 extension ID 固定后只接受精确 `chrome-extension://<approved-extension-id>`；拒绝空 Origin、`null`、其他 extension/page origin；开发测试使用明确配置的 fixture extension origin，不使用 `*`；
- CORS：只对 exact Origin 返回 `Access-Control-Allow-Origin`，带 `Vary: Origin`；`OPTIONS` 只允许 exact Origin、`POST`、`Authorization, Content-Type`；
- auth：`Authorization: Bearer <DevBoard local token>`；token 与 ChatGPT cookies/session 无关，Node 使用 constant-time compare；缺失/错误返回 401/403，响应不带内部原因；
- CSRF：不接受 cookie 认证；不接受 GET 改状态；POST 必须同时通过 exact Origin、exact Host、Bearer、JSON Content-Type；Origin 约束和非浏览器本地进程伪造防护不能互相替代；
- body：最大 16 KiB，超限在读取时拒绝；只接受 schemaVersion 1 的 allow-list JSON，拒绝 raw DOM、URL、prompt/reply 字段；
- transport：服务端不跟随任何 redirect；响应 `Cache-Control: no-store`；不记录 request body、Authorization、Origin、raw error；
- failure response：只返回 bounded `accepted/duplicate/stale/rejected` 分类，不返回 selector、DOM、URL 或密钥诊断。

本地 Node 的 endpoint 是浏览器事件入口，不改变现有 `/api/state` GET 语义，也不成为 Hub endpoint。Hub 仍只通过现有 M5/M5.4 机制获得 Node 的净化 PublicState。

## 7. 隐私字段分类和数据净化

### 7.1 字段分类

| 分类 | 允许存在的位置 | 例子 | 规则 |
| --- | --- | --- | --- |
| PRIVATE TRANSIENT | content script/adapter 的瞬时局部变量 | route path、DOM element reference、Stop signal、私有 fingerprint、用户刚输入的 label | 只为当前判断存在；不发 Node；不 log；尽快丢弃。 |
| PRIVATE NORMALIZED | service worker/Node 内存或 `storage.session` | `tabId`、`watchSessionId`、sequence、cycle counter、adapter capability、Node last receive time | 仅用于去重/路由/TTL；不进入 PublicState；不持久化 transcript。 |
| LOCAL SECRET | service worker trusted storage 与 Node config | loopback bearer token、approved extension ID | 不进 content script、DOM、日志、PublicState、Hub；不用 `storage.sync`。 |
| PUBLIC | Node projector 和现有 PublicState | provider/service、用户明确提供的 safe label、working/completed/attention、newReply、observedAt/age、source health/freshness | 只输出 allow-list，字段和长度固定。 |
| FORBIDDEN | 所有持久化、日志、网络 payload | prompt、assistant reply、transcript、完整 DOM、页面 title/sidebar label、conversation URL/ID、cookie、Authorization、session token、截图/OCR、raw network error | 结构上不进入 event schema；privacy test 必须阻断。 |

### 7.2 PublicState 最小字段

建议的首批 public browser watch：

```text
BrowserWatch {
  id                  // DevBoard opaque public watch id, not tab/conversation id
  provider            // "openai"
  service             // "chatgpt-web"
  conversationLabel   // user-entered or generated constant, <=64 UTF-8 bytes
  state               // working | completed | attention
  newReply            // true only on working→completed delivery event
  observedAt
  age                 // derived/display value; source freshness is separate
  freshness           // fresh | stale
  sourceStatus        // available | degraded | unavailable
  attentionKind?      // bounded enum only; no question text
}
```

首批不公开 `tabId`、`watchSessionId`、conversation URL/path/id、ChatGPT page title、model name、account identity、reply preview 或 summary。`host` 由既有 Node PublicState host boundary 提供，不由浏览器自己声明任意 host。

建议上限：

- `conversationLabel`：64 UTF-8 bytes；控制字符、绝对路径、URL、Bearer/token/password/secret-like、高熵 opaque string 拒绝；
- `provider`/`service`：各 32 ASCII bytes；
- `attentionKind`/`sourceStatus`/`reason`：allow-list enum；
- 单个 event JSON：16 KiB hard cap，实际预期远小于 2 KiB；
- public source message：不携带 raw error、selector、URL 或 response body，只使用固定 generic message；
- event/heartbeat 之外不保存历史列表；当前 watch 最多一条 last state；
- 可选 summary：首批**延后，不实现、不传送**。如果未来 Core Auditor 批准，最大 160 UTF-8 bytes、最多一行、只允许用户明确提供的安全摘要或 provider 明确 bounded metadata，仍不得从完整回复中摘取。

这样既满足“可选的有界净化摘要必须评估”的要求，也避免为了一个非必要 UI 字段把回复文本重新引入 M6 数据面。

## 8. 拟议状态机

### 8.1 状态

扩展内部和 Node PublicState 的状态命名可以不同，但语义必须一一对应：

```text
UNSELECTED          // 仅扩展内部；不发送业务事实
SELECTED_STARTING   // 已选择，adapter 尚未确认 ChatGPT 状态
WORKING             // 观察到本轮生成 cycle 和有效 activity
COMPLETED_NEW_REPLY // working→稳定完成；一次性 newReply=true
ATTENTION           // 观察到明确且已审计的 waiting-for-user signal
STOPPED             // 用户取消/路由改变/tab 结束；不再作为 active watch
STALE               // freshness 维度，不是新的 ChatGPT 业务结论
DEGRADED            // source health 维度，不是 ChatGPT lifecycle
```

`STALE`、`DEGRADED`、`UNAVAILABLE` 是 freshness/source-health 维度，不能伪装成 ChatGPT 的 `working` 或 `attention` 状态。

### 8.2 事件与转移

| 输入 | 转移 | public effect |
| --- | --- | --- |
| 用户 action | `UNSELECTED → SELECTED_STARTING` | 创建私有 watch session；可显示 source starting，但不声称 ChatGPT 正在工作。 |
| adapter 观察到 Stop signal + assistant activity | `SELECTED_STARTING → WORKING` | `state=working`，发送 bounded state event。 |
| working cycle 内的有效 DOM/ARIA activity | `WORKING → WORKING` | 只刷新 `observedAt`/heartbeat；不保存文本，不发送每次 mutation。 |
| Stop 消失，且本 cycle 先有 Stop、assistant activity，经过稳定窗口 | `WORKING → COMPLETED_NEW_REPLY` | 一次 `newReply=true`；之后重复 heartbeat 不再产生 new reply。 |
| 明确、已审计的 waiting-for-user signal | `WORKING/SELECTED_STARTING → ATTENTION` | 只发送 `attentionKind` enum；不发送问题内容或按钮文案。 |
| attention 后出现同一 watch 的明确继续生成 signal | `ATTENTION → WORKING` | 清除 attention；不执行任何用户操作。 |
| 用户点击取消 | `* → STOPPED` | 发送 `watch_stopped` best effort，Node 清除/标记当前 watch。 |
| route fingerprint 不再匹配、跨 origin 导航 | `* → STOPPED` | 停止 observer，要求再次用户 action；不把新会话继承为旧 watch。 |
| tab 关闭/页面 unload | `* → STOPPED`（best effort） | service worker `tabs.onRemoved` 或 TTL 负责最终清理；不依赖 unload 必达。 |
| DOM adapter unsupported/unknown | `* → SELECTED_STARTING` 或保留 last-good | `sourceStatus=degraded`、freshness=stale；不产生新 working/completed/attention 事实。 |
| Node 无法接收 | 本地保持 bounded current state | Node 端按 receive freshness 降级；不创建无限离线队列。 |
| 浏览器重启/扩展 reload | selection 丢失 | 不自动 resume；旧 Node 状态按 TTL stale/unavailable。 |

### 8.3 完成与 attention 的谨慎规则

- Stop button 消失本身不够，必须先观察到本轮 Stop、assistant-side activity 和稳定窗口；这借鉴了两个开源参考的共同防误报思路。
- 用户手动点击 Stop 不能发布 `newReply=true`；可记录为 private reason 或直接回到 stopped/unknown。
- 普通 idle、输入框可见、页面有通知图标、当前 tab 不聚焦，都不能单独推断 `attention`。
- 如果 ChatGPT 没有稳定、可审计的 waiting signal，M6 首批应公开 `attention` capability unavailable，而不是猜测。
- `completed/new_reply` 是一次 delivery event，不是永久“当前正在完成”的状态；source freshness 与高可见 retention 分开。

## 9. 拟议本机接口

### 9.1 Extension → Node request

Endpoint：

```http
POST http://127.0.0.1:8787/api/node/v1/browser-ai/events
Authorization: Bearer <local-devboard-token>
Origin: chrome-extension://<approved-extension-id>
Host: 127.0.0.1:8787
Content-Type: application/json
Cache-Control: no-store
```

建议的 JSON allow-list：

```json
{
  "schemaVersion": 1,
  "eventId": "opaque-random-id",
  "watchSessionId": "opaque-session-id",
  "sequence": 42,
  "eventType": "state",
  "provider": "openai",
  "service": "chatgpt-web",
  "conversationLabel": "Project review",
  "state": "completed",
  "newReply": true,
  "attentionKind": null,
  "observedAt": "2026-08-23T00:00:00Z",
  "sourceStatus": "available",
  "reason": "natural_completion"
}
```

以上 JSON 中没有 `tabId`、URL、conversation ID、DOM、prompt、reply、summary、cookie、ChatGPT Authorization、session token、raw error。`reason` 只能来自固定 enum，例如 `user_selected`、`natural_completion`、`manual_stop`、`route_changed`、`tab_closed`、`dom_unsupported`、`node_unavailable`。

`eventType` 建议只允许 `started`、`state`、`heartbeat`、`stopped`。Node 不把 heartbeat 当作新 reply。

### 9.2 Node response

```json
{
  "schemaVersion": 1,
  "accepted": true,
  "eventId": "opaque-random-id",
  "result": "applied"
}
```

`result` 只允许 `applied`、`duplicate`、`stale`、`stopped`、`rejected`。不得回显请求 body，不得返回 selector、DOM、URL 或认证细节。

### 9.3 重复、乱序、重启、关闭语义

Node reducer 以 `(watchSessionId, sequence)` 为最小排序键：

- 同一 key、同一内容重复：幂等返回 `duplicate`，不重复生成 new reply；
- 同一 key、内容不同：返回 `rejected`/conflict，不覆盖现有状态，source health 可降级；
- sequence 小于已接受值：返回 `stale`，不回滚 working/attention/completed；
- sequence 大于已接受值：按 allow-list 校验后原子替换该 watch 的 current state；
- 同一 watch 的 completed heartbeat 只刷新 freshness，不重新点亮 new reply；
- service worker 重启：从 `storage.session` 恢复当前 watch/sequence；如果 storage.session 已清空，则不会自动重建授权；
- 浏览器重启：旧 watch session 不恢复；Node 只等待 TTL，不能从旧 PublicState 推断仍有监控授权；
- tab 关闭：扩展通过 `tabs.onRemoved`/unload 发 stop 是 best effort；Node 没有 stop 也必须在 TTL 后删除旧状态；
- Hub 不存在：Node 本地 reducer、`/api/state`、display、fake receiver tests 仍可验证；不需要改变 Hub 或 NAS。

### 9.4 Node 内部到 PublicState

Node 内部可以保留 `BrowserWatchState` 和 `BrowserWatchStore`，但 PublicState 只经过显式 allow-list projector：

```text
private BrowserWatchState
  ├─ watchSessionId / sequence / lastReceiveAt  (private)
  ├─ adapter capability / route binding          (private)
  └─ public fields below                         (allow-list)
       provider, service, safe label, state,
       newReply, observedAt, freshness, source status,
       bounded attention enum
```

Hub 只接收既有 sanitized PublicState。建议 M6 增加一个 additive public field，不改变 `schemaVersion=1` 的既有兼容原则；具体字段名和是否放 root `browserWatches` 仍需 Core Auditor 决策。

## 10. Future provider adapter 边界

将 ChatGPT 选择为第一个 adapter，不应把 ChatGPT selector 变成通用 scraper：

```text
BrowserAIAdapter
  providerKey()
  matchesSelectedOrigin()
  observe(document) -> NormalizedObservation
  capabilities() -> {working, completed, attention}
  sanitizeLabel(input) -> SafeLabel
```

约束：

- common layer 只处理选择、消息 schema、状态机、去重、Node transport、source health 和 PublicState projection；
- provider adapter 只包含 host match、selector/ARIA 常量、有限 observation 和 fixture；
- adapter 不读取网络、不读 cookie、不执行页面动作、不创建 provider-specific mini-app；
- 每个新服务必须用户单独 action opt-in；不因新增服务把 `activeTab` 换成 `<all_urls>`；
- provider 不提供稳定 attention signal 时，capability 设为 unavailable，UI 显示 unknown/degraded，而不是通用猜测；
- adapter 版本和所验证的 Chrome/provider UI 假设必须写入 audit/测试 metadata；
- ChatGPT SPA route binding 是 adapter 的私有逻辑，不能成为 public conversation ID。

## 11. 第一施工批次的建议文件边界

以下只是后续获得施工授权后的建议，不是本次变更清单。本次只新增本审计文档。

### 新增扩展目录（建议无远程构建依赖）

```text
browser-extension/
  manifest.json
  src/background/service-worker.js
  src/content/observer.js
  src/content/state-machine.js
  src/content/adapters/chatgpt-web.js
  src/shared/message-schema.js
  src/shared/privacy.js
  test/fixtures/chatgpt/*.html
  test/*.test.js
```

首批建议使用已审计的本地打包 JavaScript，不引入网络 CDN、运行时下载代码或大型 browser automation framework。TypeScript/bundler 可以在确有维护收益时另行审计，不应成为 M6 参考审计的隐含前置依赖。

### Go Node 侧建议新增/最小变更边界

```text
internal/browserwatch/model.go
internal/browserwatch/adapter.go
internal/browserwatch/reducer.go
internal/browserwatch/store.go
internal/browserwatch/privacy.go
internal/browserwatch/transport.go
internal/browserwatch/*_test.go
internal/web/browserwatch_endpoint.go
internal/state/model.go          # 仅在技术合同批准后增加 private/public additive types
internal/state/public.go
internal/state/projector.go
internal/state/*_test.go
internal/uplink/*_test.go         # 仅验证既有 PublicState 投影包含净化字段
```

第一批不应修改：

- M0/M4/M5 冻结合同；
- Hub 接收端协议或路由；
- Node → Hub 拓扑、token/sequence/last-good 语义；
- `internal/web/templates/kindle.html`；
- Safe Navigation、navigation target 或任何控制 handler；
- agent hooks、Codex/Claude reducer；
- quota、history、screen capture、network proxy；
- 真实 NAS 配置、部署文件、验收脚本。

### 第一批实现出口

首批实现只有在下列条件都通过后才能进入独立实现审计：

1. synthetic fixture page 能覆盖 working/completed/manual-stop/attention-unsupported/route-change；
2. Node fake receiver 能验证 auth、Origin、Host、body cap、去重、乱序和 TTL；
3. `/api/state` 在无 Hub 情况下包含净化后的 BrowserWatchState；
4. existing Node → Hub snapshot test 证明没有 raw browser fields；
5. disposable Chrome profile E2E 不使用真实 profile、真实账号或 transcript；
6. `git diff --check`、unit/contract/privacy/race tests 和 scope audit 全部通过。

## 12. 测试策略与矩阵

### 12.1 单元测试

| 领域 | 必测项 |
| --- | --- |
| selector adapter | Stop signal 的 allow-list、ARIA/data-testid 版本、未知 UI→unsupported、无正文读取；ChatGPT/legacy host match。 |
| MutationObserver bridge | mutation 合并、observer root 缺失、page unload、route change、observer disconnect、不会把每个 mutation 当 event。 |
| state machine | selected starting、working、activity、稳定窗口 completion、manual stop、attention capability、unknown/degraded、newReply 一次性。 |
| privacy sanitizer | label 控制字符/URL/path/token/高熵拒绝；event schema 不存在 prompt/reply/DOM/cookie/header 字段；各字段 UTF-8 byte bound。 |
| extension message | sender tab、watchSession、source origin、message type 和 enum 校验；未知 message fail-closed。 |
| Node reducer | duplicate idempotence、same-key conflict、lower sequence、higher sequence、cross-session isolation、heartbeat 不重发 newReply、atomic replace。 |
| freshness | 30s/120s/5m 阈值、observedAt 漂移、receiveAt authority、last-good retention 与过期删除。 |

### 12.2 契约/HTTP 测试

| 场景 | 断言 |
| --- | --- |
| method | `GET`、`PUT`、form POST、未知 path 拒绝；只有明确 `POST /api/node/v1/browser-ai/events`。 |
| Host | `127.0.0.1:<port>` 接受；`localhost`、`::1`、局域网 IP、外部 Host、缺 port 拒绝。 |
| Origin/CSRF | approved extension origin 接受；`null`、无 Origin、其他 extension/page origin、wildcard 配置拒绝。 |
| auth | 缺失/错误/过期 token 拒绝；正确 token 不回显；日志和 PublicState 无 token。 |
| body | 16 KiB exactly/over-limit、未知字段、重复 key、bad JSON、wrong schema、raw field、invalid UTF-8 全部有界处理。 |
| response | no-store、无 redirect、无 raw error、bounded JSON response；CORS 只反射 exact approved Origin。 |
| local-only | 服务只监听 loopback；不发起 outbound Hub/NAS 请求；Hub 不存在不影响 Node `/api/state`。 |

### 12.3 Browser E2E

只能使用 disposable Chrome/Chromium profile 和本地 synthetic fixture page，不使用真实 Chrome profile、真实 ChatGPT 登录、真实 prompt 或 transcript：

1. 用户 action 选择 fixture conversation 后才注入 observer；未选择 tab 不产生 DOM event；
2. 两个 fixture tab 只有一个被选择时，另一个保持未监控；
3. 选中 tab 切换到后台仍能收到状态；
4. Stop signal 出现、assistant activity 变化、Stop 消失、稳定窗口后只产生一次 completed/new reply；
5. manual stop 不产生 new reply；
6. 明确 attention fixture 映射为 enum；普通 idle 不变成 attention；
7. DOM selector 升级/未知 fixture 变为 degraded/stale，不切到 OCR/network；
8. SPA route 变化、origin 变化、refresh、tab close 都不继承到新 conversation；
9. browser restart/extension reload 不自动恢复 selection；
10. service worker 被回收并由后续 message 唤醒时，sequence/idempotence 不错乱；
11. Node 不可用后恢复，只补当前 bounded state，不发送 transcript queue；
12. fake receiver 拒绝错误 Origin/Host/token，正常 extension origin 继续成功；
13. multi-watch 同时运行时，watchSession、label、newReply 不串线；
14. Hub 不启动时本地 `/api/state`、display 和 fake receiver 测试仍通过。

### 12.4 隐私与安全测试

使用合成 canary 值（例如 `PROMPT_CANARY_M6`、`REPLY_CANARY_M6`、`COOKIE_CANARY_M6`、`AUTH_CANARY_M6`），断言：

- Chrome message payload、Node request body、Node memory snapshot、logs、PublicState、Node → Hub snapshot 中都不存在 canary；
- no `document.body.innerText`/`innerHTML`/`outerHTML`/screenshot/network body path；
- 没有 cookie、Authorization、session token permission 或 API invocation；
- page script 无法读取 service-worker pairing token；
- untrusted label/DOM 不能逃逸 UTF-8、控制字符、URL/path/token 规则；
- PublicState 只含 allow-list，不含 tab/conversation IDs 和 raw source error；
- source degraded/stale 不会擦除 unrelated Codex/Claude/Host/Network state；
- browser extension disabled、Node down、Hub down 都 fail-open，不阻断 ChatGPT 或 DevBoard 其他 collector。

### 12.5 真实 provider 验证边界

真实 ChatGPT E2E 不在本次审计执行。未来若 Core Auditor 批准，必须使用用户明确授权的独立测试账号/临时 profile，并只验证状态枚举和隐私出口；禁止将真实 prompt/reply 作为 fixture、日志或提交附件。真实验证若因 ChatGPT UI 变更、登录要求或 provider 限制不可完成，应保留 source `degraded`，不得通过放宽权限或网络拦截“修好”。

## 13. 已知不稳定点和降级行为

| 不稳定点 | 影响 | 必须的降级 |
| --- | --- | --- |
| ChatGPT button/ARIA/data-testid/DOM hierarchy 改变 | Stop/assistant signal 失配，completion 可能漏报 | adapter capability 失败，source degraded/stale；不扩大 selector 到全文、不换 CDP/OCR。 |
| ChatGPT A/B test、模型 UI、legacy host redirect | 同一版本表现不同 | 固定 host/selector fixture matrix；unknown signal 进入 degraded。 |
| SPA route、虚拟化/延迟渲染、page reload | selection 绑定和 latest assistant node 不稳定 | route mismatch 停止；有限 bootstrap retry；不从新 route 继承旧 watch。 |
| MV3 service worker ephemeral | 全局 Map 丢失、timer 中断 | storage.session、事件驱动、序列化状态；不依赖长驻 worker。 |
| `activeTab` 权限 | 跨 origin/关闭 tab 后撤销；浏览器重启不保证旧授权 | 每个 browser lifecycle 重新选择；不静默恢复。 |
| background tab throttling | heartbeat/DOM observation 延迟 | Node 依据 receiveAt 计算 stale；只显示最后已确认状态，并明确 freshness。 |
| attention 语义不稳定 | 将 idle/通知/输入框误作等待用户 | 只支持明确 audited signal；否则 capability unavailable。 |
| ChatGPT 官方通知变化 | 普通会话状态无法从官方 notification 推断 | notifications 仅是未来可选输出，不作为 source。 |
| Chrome/Chromium 版本差异 | API availability、service worker 行为、权限 UX 变化 | 固定 minimum Chrome version；契约测试和版本记录；不使用 tip-of-tree CDP。 |
| 本机 loopback token/origin 配置错误 | extension→Node 连接失败 | source unavailable/degraded；不自动退回无认证、外网或 NAS。 |
| Node/Hub 网络故障 | Hub 看不到最新 BrowserWatch | Node 本地 `/api/state` 保持可验证；Hub 只看到最后合法 PublicState 并按既有 freshness 处理。 |

## 14. 明确非目标

M6 首个实现批次不包含：

- 任意网页、任意 tab、浏览器历史、书签、top sites 或全局浏览行为分析；
- 读取、保存、同步、上传完整 prompt、完整 reply、transcript、代码块、附件、页面 title/sidebar label；
- cookie、Authorization header、session token、ChatGPT API token、浏览器 profile 文件或 local storage 读取；
- 登录、绕过 CSP、绕过 host permission、绕过 provider 安全机制；
- 自动回复、点击、输入、提交、滚动、导航、聚焦、下载、复制或任何页面控制；
- Safe Navigation、remote approve/deny、stop/retry/continue、quota/account switching；
- Chrome DevTools Protocol、remote debugging port、debugger attach；
- webRequest、declarativeNetRequest、proxy、WebSocket/HTTP body interception；
- desktop/tab capture、截图、OCR、视觉模型；
- Chrome system notification/accessibility API 作为状态源；
- Hub 新 endpoint、Node→Hub 新拓扑、NAS SSH/deploy/acceptance；
- 真实 transcript fixture、真实 profile 变更、扩展安装；
- 其他 AI Web 服务的实现；只保留 adapter interface 边界。

## 15. Core Auditor 需要决策的问题

以下问题不应在实现时默默假设：

1. **选择生命周期**：是否批准推荐的 action-only `activeTab` 方案，即浏览器重启、扩展 reload、跨 origin 导航后必须重新选择；若要求自动恢复，需要重新打开 persistent host permission 的安全审计。
2. **ChatGPT host 范围**：首批是否同时批准 `chatgpt.com` 和 legacy `chat.openai.com`；后者若仅作 redirect，应否只记录 degraded 而不注入。
3. **公共 label 语义**：是否批准“常量 label 或用户明确输入 label”，而不是从 ChatGPT 页面标题/侧栏自动抽取；这是隐私最小方案。
4. **摘要是否延期**：本审计建议首批完全不传 bounded reply summary；若产品必须有摘要，应先定义来源、长度、敏感词拒绝和“不保存原文”的可测试契约。
5. **attention capability**：是否要求首批必须有真实、稳定的 waiting-for-user signal；若没有，是否接受首批只提供 working/completed/new reply，attention 标为 unavailable。
6. **Node pairing**：是否批准人工配置 loopback bearer token，还是要求另行设计 one-time pairing；两者都不得使用 ChatGPT credential，也不得把 token 放入 PublicState。
7. **Node endpoint 与状态字段**：是否批准 `/api/node/v1/browser-ai/events` 作为新增本机 loopback route，以及 public field 采用 root `browserWatches` 还是其他 additive 命名；不得修改既有 `/api/state` 语义。
8. **freshness/retention**：是否批准 30s fresh、120s unavailable、5m last-good retention；若不同，需同时修改 reducer、UI、E2E 和 M5 snapshot expectations。
9. **多 watch UI**：首批是否只允许多个 tab 各自 action 选择但不提供全局 tab list；若要全局列表，需重新评估 `tabs` permission 和公共 label泄露。
10. **真实 provider gate**：真实 ChatGPT E2E 是否在 Core Auditor 后另行授权；在授权前只使用 synthetic fixture，不以真实 transcript 作为测试输入。

## 16. 参考再利用登记

| Reference | Revision | License | Reuse decision | DevBoard-specific difference |
| --- | --- | --- | --- | --- |
| Chrome MV3 official APIs | Docs retrieved 2026-08-23 | Official docs/code samples respective licenses | **ADAPT API contract** | Only action-selected tab, no broad host access, no page control. |
| OpenAI ChatGPT Web task notifications | Official help article retrieved 2026-08-23 | Official documentation | **BEHAVIORAL REFERENCE ONLY** | Tasks/notifications do not become current conversation state API. |
| `dawidstruzik/chat-alert` | `665be3d48362c3d1a2b08f009f78a47b66d94ef9` | MIT | **BEHAVIORAL REFERENCE ONLY** | Reject MAIN-world fetch interception and reply preview; use no notifications in minimum batch. |
| `kkonstantin08/chatgpt-done-notifier` | `54381bf1c3362570d0dd8186dc258fa0c35e95ad` | No declared license found | **BEHAVIORAL REFERENCE ONLY** | Do not copy code; retain only state-machine/test lessons; remove text fingerprints and broad body scan. |
| Existing DevBoard M5 sanitized PublicState/uplink | Baseline SHA `36e688fe6c38b834efe06a596059c89aff0a2b86` | DevBoard repository | **USE DIRECTLY as authority** | Browser event enters local Node, then existing public projection and Node→Hub path. |

## 17. 审计状态

```text
M6_REFERENCE_AUDIT=READY_FOR_CORE_AUDIT
```

这表示 reference audit、方案比较、最小权限、威胁模型、数据边界、状态机、本机接口和测试策略已经形成，可交由 Core Auditor 决定；不表示 M6 已实现、扩展已安装、真实 ChatGPT 已验收或 NAS 已接触。
