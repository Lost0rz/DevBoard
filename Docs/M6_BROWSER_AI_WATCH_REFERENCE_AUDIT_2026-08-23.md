# DevBoard M6 Browser AI Watch — Reference Audit

> 日期：2026-08-23（Asia/Shanghai）
> 精确审计基线：`36e688fe6c38b834efe06a596059c89aff0a2b86`
> 本轮修订起点：`4d0a33811af49b2705561222c19cbf024d87b212`
> 基线分支：`codex/pc1-integration-closure`
> 审计分支：`codex/m6-browser-ai-watch-reference-audit`
> 首期研究范围：Chrome/Chromium + ChatGPT Web
> 状态：**M6_REFERENCE_AUDIT=READY_FOR_CORE_REAUDIT**

本文件只做 M6 Browser AI Watch 的 reference audit 和架构建议，不实现 M6，不修改冻结合同，不修改 Go、JavaScript、Swift、模板或部署文件。本审计完全绕开真实 NAS：不尝试 NAS SSH、部署、验收或远程接收；不安装浏览器扩展，不修改真实 Chrome profile，不使用真实会话 transcript，不修改 GitHub Issue/PR，不创建 PR。

## 1. 审计结论摘要

首个实现批次的推荐链路是：

```text
用户 action 明确选择当前 https://chatgpt.com 会话
→ activeTab 临时授权 + scripting 动态注入 isolated-world content script
→ ChatGPT adapter 只观察有限 DOM/ARIA 状态信号
→ MutationObserver + 确定性状态机
→ MV3 service worker 校验、净化、通过 runtime.connectNative() 发送
→ DevBoard-owned native host（stdio）
→ 独立 0600 browser-watch Unix socket
→ 本机 Node BrowserWatch reducer
→ 现有 PublicState projector
→ 不改变 Node → Hub 拓扑，Hub 只接收净化后的 PublicState
```

正式推荐的本机传输为：

```text
Chrome Native Messaging
→ DevBoard-owned native host
→ 独立 browser-watch Unix socket
→ Node BrowserWatch reducer
```

Native Messaging 在首期比 loopback HTTP 更适合的原因是：扩展不持有 Node Bearer token；浏览器到 helper 没有新 HTTP listener、CORS 或 CSRF 路由；Chrome 官方的 `allowed_origins` 可精确绑定稳定 extension ID；helper 到 Node 的边界可以用独立、0600 的 Unix socket；BrowserWatch 仍使用独立 schema，不塞进现有 `AgentEvent`。它不是对同用户恶意进程的绝对隔离：稳定 extension ID 是 allow-list 身份而非秘密，dogfood 仍依赖 trusted unpacked extension、用户级 DevBoard helper 和本机用户信任边界。

明确采用：

- Manifest V3、action-only `activeTab`、`scripting`、isolated-world content script；
- 首期只支持 `https://chatgpt.com`；`https://chat.openai.com` 不注入，只报告 unsupported/redirect；
- opt-in content script + provider-specific `MutationObserver`；
- Native Messaging `nativeMessaging` permission，service worker → native host；
- DevBoard-owned stable helper → 独立 0600 browser-watch Unix socket → Node reducer；
- 同时最多一个 active watch；选择新 tab 时停止并替换旧 watch；
- label 固定为 `ChatGPT conversation`；首期不读取页面标题，不开放用户文本 label；
- `working/generating`、`completed/new_reply` 和 source health/freshness；attention 首期为 capability unavailable；summary 延期；
- 浏览器重启、扩展 reload、跨 origin 后必须重新选择；
- synthetic fixture 全部通过后，真实 ChatGPT E2E 另行授权；不接触 NAS。

明确拒绝：CDP/debugger、历史或全局 tab 轮询、通用截图/OCR、代理或网络流量拦截，以及把 BrowserWatch event 塞进现有 AgentEvent schema。

## 2. 冻结合同与已决定的第一批边界

已完整阅读：

- [`Docs/contracts/mvp-monitoring-v1.md`](contracts/mvp-monitoring-v1.md)
- [`Docs/contracts/mvp-feature-freeze-v1.md`](contracts/mvp-feature-freeze-v1.md)
- [`Docs/contracts/reference-first-integration-v1.md`](contracts/reference-first-integration-v1.md)
- [`Docs/contracts/m4-task-observability-v1.md`](contracts/m4-task-observability-v1.md)
- [`Docs/contracts/m5-multi-host-v1.md`](contracts/m5-multi-host-v1.md)

以下是 Core Auditor 已决定的范围，本文件不再把它们列为开放问题：

| 决定 | 第一批合同化建议 |
| --- | --- |
| 选择生命周期 | action-only `activeTab`；浏览器重启、扩展 reload、跨 origin 后必须重新选择。 |
| host | 只支持 `https://chatgpt.com`；legacy `chat.openai.com` 不注入，只报告 `unsupported/redirect`。 |
| label | 固定 `ChatGPT conversation`；不读取页面标题，不开放用户文本 label。 |
| summary | 延期，不实现，不传送。 |
| attention | 首期 `capability unavailable`，不推测 `waiting_for_user`。 |
| watch 数量 | 同时最多一个 active watch；选择新 tab 必须停止并替换旧 watch。 |
| freshness | 30 秒内 `fresh`；30–120 秒 `stale`；超过 120 秒 `unavailable`；last-good 最长保留 5 分钟。 |
| 真实验证 | synthetic fixture 全部通过后，真实 ChatGPT E2E 另行授权。 |
| NAS | 不接触 NAS。 |

不改变既有 Node → Hub 拓扑。事件先进入本机 Node，Hub 只接收净化后的 PublicState。Node、Hub 或 native host 故障只影响 Browser AI Watch source，不得让现有 agent event、Codex/Claude collector 或其他状态 fail-closed。

## 3. 资料来源及检索日期

资料均于 **2026-08-23** 检索。外部研究优先采用官方资料；开源项目只作为行为和测试参考，不复制未确认许可证的代码。

### 3.1 Chrome/Chromium 官方资料

| 资料 | 本审计使用的事实 |
| --- | --- |
| [Native messaging](https://developer.chrome.com/docs/extensions/develop/concepts/native-messaging) | host manifest 的 `name`、绝对 `path`、`type=stdio`、无 wildcard 的 `allowed_origins`；macOS 按 Google Chrome、Chrome for Testing、Chromium 分开查找；stdio framing 和官方长度上限；caller origin 参数；`nativeMessaging` permission；Native Messaging 只可从 extension pages/service worker 使用。 |
| [Manifest file format](https://developer.chrome.com/docs/extensions/reference/manifest) | MV3、`background.service_worker`、`permissions`、`optional_host_permissions`、`key` 等 manifest 边界。 |
| [Manifest `key`](https://developer.chrome.com/docs/extensions/reference/manifest/key) | `key` 可在开发/非 Web Store 场景保持稳定 extension ID；稳定 ID 可用于限制服务器/host 接受的 extension origin。 |
| [The `activeTab` permission](https://developer.chrome.com/docs/extensions/develop/concepts/activeTab) | 用户 action 后只向当前 tab 临时授予访问；跨 origin 导航或 tab 结束后失效；不需要 broad host permission。 |
| [Content scripts](https://developer.chrome.com/docs/extensions/develop/concepts/content-scripts) | content script 可读 DOM、默认 isolated world，并通过消息与 service worker 通信。 |
| [`chrome.scripting`](https://developer.chrome.com/docs/extensions/reference/api/scripting) | 动态注入需要 `scripting` 以及 `activeTab` 或 host permission；首期只在选择后注入。 |
| [Message passing](https://developer.chrome.com/docs/extensions/develop/concepts/messaging) | content script → service worker 的 JSON-serializable 消息必须显式校验；Native Messaging 不可直接从 content script 调用。 |
| [Extension service worker lifecycle](https://developer.chrome.com/docs/extensions/develop/concepts/service-workers/lifecycle) | service worker 会因空闲终止；选择和 sequence 不能依赖长驻全局变量。 |
| [`chrome.storage`](https://developer.chrome.com/docs/extensions/reference/api/storage) | 只保留 bounded session/selection 元数据；不存 prompt、reply、transcript 或 token。 |
| [Declare permissions](https://developer.chrome.com/docs/extensions/develop/concepts/declare-permissions) 与 [permission warnings](https://developer.chrome.com/docs/extensions/develop/concepts/permission-warnings) | 只声明必需 API，避免 `tabs`、`history`、`debugger`、`webRequest`、`cookies`、`proxy` 等扩大权限面。 |
| [`chrome.notifications`](https://developer.chrome.com/docs/extensions/reference/api/notifications) | 这是输出通知 API，不是普通 ChatGPT 会话状态 source；首期不申请。 |
| [`chrome.debugger`](https://developer.chrome.com/docs/extensions/reference/api/debugger) 与 [Chrome DevTools Protocol](https://chromedevtools.github.io/devtools-protocol/) | debugger/CDP 可触达广泛 DOM、Runtime、Network、Storage；权限和维护面超过只读状态需要。 |
| [`chrome.history`](https://developer.chrome.com/docs/extensions/reference/api/history)、[`chrome.tabs`](https://developer.chrome.com/docs/extensions/reference/api/tabs)、[`chrome.webRequest`](https://developer.chrome.com/docs/extensions/reference/api/webRequest) | history/tabs 会扩大浏览活动面；webRequest 不应成为 ChatGPT 应用消息 source，并会带来 URL/header/凭据暴露风险。 |

Native Messaging 官方页面给出的 macOS 用户级目录是：Google Chrome `~/Library/Application Support/Google/Chrome/NativeMessagingHosts/`，Chrome for Testing `~/Library/Application Support/Google/ChromeForTesting/NativeMessagingHosts/`，Chromium `~/Library/Application Support/Chromium/NativeMessagingHosts/`。Chrome 官方还注明 Chrome 146 之前 Chrome for Testing 使用 Google Chrome 的旧路径；实现必须显式选择浏览器渠道和版本，不把路径猜成共享目录。

官方协议是双向 UTF-8 JSON，前置 32-bit native-byte-order message length；Chrome 官方上限为 host→extension 1 MiB、extension→host 64 MiB。DevBoard 首期主动收紧为 service worker 和 native host 都拒绝超过 16 KiB 的业务消息，stdout 响应不超过 4 KiB。

### 3.2 OpenAI/ChatGPT 官方资料

| 资料 | 结论 |
| --- | --- |
| [Scheduled Tasks in ChatGPT](https://help.openai.com/en/articles/10291617-tasks-in-chatgpt) | 可确认 ChatGPT 的任务、浏览器/桌面通知体验；没有确认普通 ChatGPT Web 会话 working/completed/attention 的本地官方接口。 |
| [Using the built-in browser in the ChatGPT desktop app](https://help.openai.com/en/articles/20001277-using-the-built-in-browser-in-the-chatgpt-desktop-app) | ChatGPT desktop built-in browser 是独立浏览器状态，不能当作用户 Chrome profile。 |
| [OpenAI Conversations API reference](https://platform.openai.com/docs/api-reference/conversations) | 是 OpenAI API conversation resource，不是 ChatGPT Web 登录页面监听接口；不把 API credential 或 API conversation 接入 M6。 |

因此 ChatGPT DOM adapter 只能是显式、可降级的观察面，不能宣称 provider-authoritative。

### 3.3 维护中的开源行为参考

- [`dawidstruzik/chat-alert` fixed commit](https://github.com/dawidstruzik/chat-alert/tree/665be3d48362c3d1a2b08f009f78a47b66d94ef9)：MIT；参考 host allow-list、Stop signal、稳定窗口和每-tab 状态机；不采用其 MAIN-world fetch interceptor、backend API update time、reply preview 或宽权限。
- [`kkonstantin08/chatgpt-done-notifier` fixed commit](https://github.com/kkonstantin08/chatgpt-done-notifier/tree/54381bf1c3362570d0dd8186dc258fa0c35e95ad)：审阅提交未发现声明许可证；只参考 MutationObserver、selector adapter、stabilization window、dedupe 和测试维度，不复制代码、文本 fingerprint 或 broad body scan。

## 4. 候选技术路线比较

评分是针对 M6 冻结边界的架构判断；“侵入性、权限最小、维护性”分数越高越好。

| 路线 | 可得信号 | 权限/数据面 | 侵入性 | 权限最小 | 维护性 | 决策 |
| --- | --- | --- | ---: | ---: | ---: | --- |
| 1. Manifest V3 浏览器扩展 | action、activeTab、content script、service worker、storage | 可只声明 `activeTab`、`scripting`、`storage`、`nativeMessaging` | 5 | 5 | 4 | **USE**，作为运行时基础。 |
| 2. opt-in content script + MutationObserver | 选定 tab 的有限 Stop/ARIA/结构变化 | 不读网络、cookie 或完整 DOM；只在用户选择后注入 | 5 | 5 | 4 | **RECOMMENDED**，首期唯一状态检测器。 |
| 3. 浏览器通知/可访问性状态 | 通知输出或页面 ARIA；不是普通会话权威 API | `notifications`/平台 accessibility 会扩大权限，且语义不稳定 | 3 | 2 | 2 | **REJECT AS SOURCE**；只允许页面 ARIA 作为 adapter 输入，attention 仍 unavailable。 |
| 4. Chrome DevTools Protocol | DOM、Runtime、Network、Accessibility 等广泛调试域 | `debugger` 可读取/改变页面并触达网络/存储 | 1 | 1 | 2 | **REJECT**，过度授权且 CDP tip-of-tree 不保证兼容。 |
| 5. 本地浏览器历史或标签轮询 | URL/title、history、tab 生命周期 | 需要 history/tabs/webNavigation 等，天然扩大浏览活动面 | 2 | 1 | 2 | **REJECT AS DETECTOR**；只保留已选择 tab 的关闭清理语义。 |
| 6. 通用屏幕截图/OCR | 像素级按钮、视觉文字 | 屏幕敏感数据、capture 权限、误识别、CPU/测试成本 | 1 | 1 | 1 | **REJECT**。 |
| 7. 代理或网络流量拦截 | request lifecycle、headers、部分握手 | webRequest/host/proxy 可见 URL、Cookie、Authorization；不提供稳定应用消息语义 | 1 | 1 | 1 | **REJECT**，不读取/拦截 ChatGPT 网络流量。 |

选择路线 2 是因为失配成本可以限制在 ChatGPT adapter 内，同时不扩大账户、网络和屏幕权限。DOM 失配必须变成 `sourceStatus=degraded`，不能通过 CDP、OCR、网络拦截或全文扫描“修好”。

## 5. 本机传输正式比较：loopback HTTP 与 Native Messaging

### 5.1 A — loopback HTTP

候选形态：

```text
service worker
→ POST http://127.0.0.1:<configured-port>/api/node/v1/browser-ai/events
→ Node BrowserWatch HTTP route
→ BrowserWatch reducer
```

必须同时满足以下条件才可作为 fallback：

- manifest 只把 `http://127.0.0.1/*` 放入 optional host permission，并在用户明确启用本机桥接时申请；不使用 `<all_urls>`、`http://localhost/*`、`http://*/*` 或 LAN host permission。
- Node 首次初始化生成至少 32 random bytes 的 token；token 只保存于 Node-owned 0600 配置/系统钥匙串，并通过一次性人工配对交给 extension trusted page；content script 永远拿不到 token。不能把 token 放进 URL、PublicState、日志、页面 DOM、`storage.sync` 或 transcript。
- 配对必须显式显示浏览器 extension ID 和一次性 code；重新配对生成新 token。人工 reset/re-pair 轮换 token；撤销清除 Node token、使旧 token 立即失效，并清理 extension `storage.local` 中的桥接配置。扩展卸载不会可靠替 Node 清理 secret，故卸载/取消集成必须有独立的显式 revoke/repair；不能把卸载 URL 当作本地清理保证。
- Node 精确接受 `Host: 127.0.0.1:<configured-port>`；拒绝 `localhost`、`::1`、其他 IP、缺端口、外部 Host 和转发 Host。精确接受 `Origin: chrome-extension://<stable-extension-id>`，拒绝无 Origin、`null`、其他 extension/page origin；CORS 只返回同一个 exact Origin 和 `Vary: Origin`，不返回 `*`。
- 只接受明确的 `POST` 路径、`Authorization: Bearer <token>`、`Content-Type: application/json`、无 redirect；body hard cap 16 KiB；不接受 cookie 认证、GET 改状态或 form body。Origin/CORS 是浏览器 CSRF 边界，不是本地进程认证。
- 本机任意进程可以尝试连接 127.0.0.1，可以伪造 Host/Origin header；如果 token 泄露，其他进程可伪造事件。token 轮换和 exact checks 只降低风险，不能把 loopback 变成强身份通道。Node 还新增 HTTP route、路由日志、CORS、body parser、auth 和测试面。
- Node 运行时必须 fail-open：HTTP route、token 错误或 Node 关闭只能将 BrowserWatch 标成 unavailable/degraded，不得改变现有 agent event schema、Node → Hub 拓扑或其他 source。

结论：loopback HTTP 可被严格做成安全 fallback，但它把浏览器桥接 token 放进 extension 受信边界并新增一个本地 HTTP attack surface；因此不是首期首选。

### 5.2 B — Chrome Native Messaging（首选）

推荐形态：

```text
MV3 service worker
→ chrome.runtime.connectNative("com.devboard.browser_watch")
→ Chrome stdio Native Messaging host
→ DevBoard-owned stable helper
→ ~/Library/Application Support/DevBoard/run/browser-watch.sock (0600)
→ Node BrowserWatch reducer
```

逐项安全和维护约束：

- manifest 声明 `nativeMessaging`；Native Messaging API 只能由 extension page/service worker 使用，content script 先把已净化消息交给 service worker。首批 service worker 使用 `connectNative` 长连接；`onDisconnect` 只产生 source unavailable，不触发 Node fail-closed。
- Native host manifest 使用 `type: "stdio"`、绝对 `path` 和 exact `allowed_origins: ["chrome-extension://<stable-extension-id>/"]`；`allowed_origins` 禁止 wildcard。扩展不持有 Node token，也不通过 HTTP/CORS 访问 Node。
- helper 是 DevBoard-owned stable binary/launcher，建议路径为 `~/Library/Application Support/DevBoard/bin/devboard-browser-watch-host`；manifest 的 `path` 必须在安装时展开成绝对路径。helper 只转发新的 `BrowserWatchMessageV1`，不把数据变成既有 `AgentEvent`，不访问 Hub/NAS，不保存 transcript。
- helper 连接独立 `~/Library/Application Support/DevBoard/run/browser-watch.sock`；父目录建议 0700，socket 0600，并按当前用户/peer credential 检查。它不复用现有 agent event socket、HTTP route 或 AgentEvent schema。若同一 OS 用户运行恶意进程，0600 不是绝对隔离；这属于 trusted dogfood 的残余威胁，helper/Node 仍必须做 schema、长度、sequence 和来源约束。
- Chrome 官方 stdin/stdout framing 是 UTF-8 JSON 前置 32-bit native-byte-order length。Chrome 官方 host→extension 上限为 1 MiB，extension→host 上限为 64 MiB；DevBoard 在 service worker、helper 和 Unix socket 入口都使用 16 KiB hard cap，响应 <=4 KiB，拒绝超限后关闭该连接。
- stdout 只能输出 framing 正确的 ACK/结果 JSON，绝不打印业务内容、页面文本、prompt、reply、URL、cookie、Authorization、session token 或凭据。诊断不得走 stdout；stderr 只允许 bounded generic diagnostics，例如 `protocol_error`、`socket_unavailable`，不含请求 body、label、selector、token、路径中的敏感业务片段。Chrome 对 host stderr/协议失败会写错误日志，因此隐私测试必须覆盖 stderr。
- helper 输入 schema 必须是独立的 `BrowserWatchMessageV1`，字段为固定 enum、opaque event/session id、sequence、固定 label、状态、时间和 source status；unknown field、duplicate JSON key、wrong type、raw text/URL/DOM/prompt/reply 字段均拒绝。Node 返回 bounded `applied|duplicate|stale|rejected`，不回显 body。
- Native Messaging 的 auth 是精确 extension origin allow-list，不是秘密 token。`manifest.key` 的公钥材料可稳定 ID，但不应被误当作 secret；trusted unpacked dogfood 仍需要扩展目录/公钥、helper 和 user-level manifest 的完整性。任何 ID/key 改变都应让 source unavailable 并要求重新安装/修复，而不是放宽 allowed origins。
- 首批没有 Node 新 HTTP route；本机测试用 Unix-socket fake receiver 或内存 transport seam 验证 Node reducer。Node 仍是 BrowserWatch 权威入口，Hub 仍只收到 PublicState。

### 5.3 正式取舍

| 维度 | loopback HTTP | Native Messaging |
| --- | --- | --- |
| 浏览器权限 | optional `127.0.0.1` host permission | `nativeMessaging`；无 loopback host permission |
| extension secret | 必须保存/轮换/revoke Bearer token | 不保存 Node token；仅使用 exact stable extension origin |
| CSRF/CORS | 需精确 Origin、Host、CORS、无 redirect | 无 HTTP CORS/CSRF route |
| 本地伪造 | 其他进程可碰 TCP，可伪造 headers；token 泄露后可发事件 | helper stdio 不暴露 TCP；同用户仍可运行/替换本地 helper，需 trusted dogfood 和 0600 socket |
| Node 变更 | 新增 HTTP route、auth、parser、CORS 和 route tests | 独立 BrowserWatch socket seam，不新增 AgentEvent route |
| 生命周期 | route 常驻；HTTP 连接与 token 独立管理 | service worker/helper `onDisconnect` 变 source health；可按用户 action 重连 |
| 安装边界 | 无 native manifest，但 token 配对/卸载清理复杂 | 需精确用户级 manifest、稳定 helper、install/repair/uninstall 所有权 |
| 维护性 | 通用、易模拟，但边界代码较多 | Chrome 官方机制和浏览器渠道路径需维护，链路边界更清晰 |
| 首期结论 | **REJECT AS PRIMARY; FALLBACK ONLY** | **RECOMMENDED** |

没有发现 Chrome 官方资料支持的 Native Messaging 明确阻塞。故首期不以“loopback 更简单”为理由反选 Native Messaging；若未来选择 loopback，必须另行证明其收益足以抵偿 token/CORS/HTTP attack surface，并完成上表的 token 全生命周期。

## 6. 扩展 ID、分发和权限审计

### 6.1 第一批分发政策

- 仅 macOS dogfood；仅 unpacked trusted extension；不自动安装扩展。
- `manifest.json` 使用 `key` 保持稳定 extension ID。该 ID 是 Native Messaging `allowed_origins` 的唯一 extension origin，也是 loopback fallback 的 exact HTTP `Origin`。
- `key` 只包含可公开的公钥材料；私钥不得提交仓库、日志、artifact、native host manifest 或测试附件。不能把 `key` 或 extension ID 当作秘密。
- 第一批不发布 Chrome Web Store。正式 Chrome Web Store 分发属于后续产品化门槛，必须重新审查更新渠道、签名/发布、权限文案、native host 安装和 ID 连续性。
- 不允许 `allowed_origins`、CORS allow-list 或 HTTP Origin 使用 wildcard；没有稳定 ID 时不能启动 BrowserWatch transport。
- Chrome、Chromium、Chrome for Testing 是三个显式渠道，不猜测共享 profile 或共享 NativeMessagingHosts 路径；安装命令必须要求渠道参数，未知渠道拒绝。

### 6.2 最小 Chrome permissions

推荐的第一批 `manifest.json` 权限集合：

```json
{
  "permissions": [
    "activeTab",
    "scripting",
    "storage",
    "nativeMessaging"
  ]
}
```

第一批不申请 `host_permissions` 或 `optional_host_permissions`；推荐 Native Messaging 不需要 `http://127.0.0.1/*`。也不申请 `tabs`、`history`、`webNavigation`、`notifications`、`debugger`、`webRequest`、`declarativeNetRequest`、`proxy`、`cookies`、`offscreen`、capture 或 accessibility 权限。`storage` 只存当前 bounded selection/sequence/capability，不存 transcript、token 或同步秘密。

### 6.3 Native host 用户级安装边界（macOS）

固定 host name：`com.devboard.browser_watch`。

| 浏览器渠道 | macOS 用户级 manifest 目录 | 首期策略 |
| --- | --- | --- |
| Google Chrome（Chrome 146 及以后路径） | `~/Library/Application Support/Google/Chrome/NativeMessagingHosts/` | 允许 dogfood；只写 DevBoard 自有 manifest。 |
| Google Chrome for Testing（Chrome 146 及以后路径） | `~/Library/Application Support/Google/ChromeForTesting/NativeMessagingHosts/` | 作为独立渠道处理；不复用 Chrome 路径。 |
| Chromium | `~/Library/Application Support/Chromium/NativeMessagingHosts/` | 作为独立渠道处理；不复用 Chrome 路径。 |

manifest 文件名固定为 `com.devboard.browser_watch.json`，内容形态为：

```json
{
  "name": "com.devboard.browser_watch",
  "description": "DevBoard Browser Watch native host",
  "path": "/absolute/path/to/DevBoard/bin/devboard-browser-watch-host",
  "type": "stdio",
  "allowed_origins": [
    "chrome-extension://<stable-extension-id>/"
  ]
}
```

路径中的 `<stable-extension-id>` 必须是 dogfood manifest key 对应的精确 ID；不允许通配符。Chrome 官方注明 Chrome 146 之前 Chrome for Testing 使用 Google Chrome 的旧路径；实现必须按显式 browser channel + detected version 选择目录，不把旧路径和新路径同时写成共享 fallback。

安装、repair 和 uninstall 的所有权规则：

1. 这是后续 DevBoard 用户级安装动作，不是扩展自动安装动作；本轮不执行安装。
2. install/repair 只创建 DevBoard 自己的 `~/Library/Application Support/DevBoard/bin/`、`run/`、manifest 目标目录和自己的 manifest。建议目录 0700、manifest 0600、socket 0600、helper 0700。
3. manifest `path` 指向 DevBoard-owned stable binary/launcher。若已有同名文件内容或 owner marker 不是 DevBoard 自有内容，repair 必须拒绝覆盖并报告冲突；不能覆盖其他软件的 host。
4. install/repair 幂等：同一 channel、同一 stable ID、同一 helper digest 时不重复改写；helper、manifest、allowed origin 或路径发生漂移时只更新 DevBoard 自有文件，并再次校验绝对路径、JSON schema 和 allow-list。
5. uninstall 只移除 DevBoard 自有 helper、自己的 socket/run 内容和具有 DevBoard owner marker/精确内容的 manifest；不修改其他 extension、Chrome profile、Preferences、Extensions registry 或共享目录中的其他文件。发现 manifest 已被外部修改时保留并报告，不删除。
6. Chrome、Chromium、Chrome for Testing 各自 repair/uninstall；不能因为一个渠道卸载而删除其他渠道 manifest。helper 不向 stdout/stderr 输出业务内容或凭据。
7. Node/native host 不可用时只将 BrowserWatch source 标为 unavailable/degraded；Node 不运行时 fail-open，不停止现有 agent event，不改变 Hub 或 NAS 行为。

## 7. 推荐架构与用户语义

```text
ChatGPT Web (untrusted DOM; selected tab only)
        │ user action
        ▼
MV3 action/popup
        │ activeTab + scripting
        ▼
isolated-world content script
  ├─ fixed origin/route binding (private)
  ├─ ChatGPT adapter: finite selector/ARIA signals only
  ├─ MutationObserver with debounce
  └─ deterministic BrowserWatch state machine
        │ chrome.runtime.sendMessage (normalized JSON only)
        ▼
MV3 service worker
  ├─ validates sender, selected tab, schema and current session
  ├─ bounded storage.session state
  ├─ runtime.connectNative("com.devboard.browser_watch")
  └─ no page control / no network inspection
        │ stdio Native Messaging
        ▼
DevBoard-owned native host
        │ separate 0600 browser-watch.sock
        ▼
DevBoard Node BrowserWatch reducer
  ├─ duplicate/order/freshness/last-good semantics
  ├─ private BrowserWatchState
  └─ explicit PublicState allow-list projector
        │ existing Node → Hub path
        ▼
Hub receives sanitized PublicState only
```

### 7.1 用户选择与取消

1. 用户打开 `https://chatgpt.com` conversation，点击扩展 action。
2. 扩展只接受当前 tab 的 exact origin；任何 `chat.openai.com` 页面不注入，报告 `unsupported/redirect`。
3. action 建立一个 opaque `watchSessionId`；label 永远是 `ChatGPT conversation`，不读取 title/sidebar/prompt/reply。
4. 先停止现有 active watch，再选择当前 tab；首期同一时间最多一个 active watch。
5. 以本次 action 的 `activeTab` 授权动态注入 isolated-world observer；未选择 tab 没有 observer。
6. 再次 action 或明确取消时发送 `watch_stopped`，断开 observer 并清除 selection；stop 是 best effort，Node 仍按 TTL 清理。
7. 浏览器重启、扩展 reload、跨 origin、tab close、路由失配后不自动恢复；需要用户再次 action。service worker 因空闲被回收时，可以从 `storage.session` 恢复当前 bounded sequence 并重连 native host，但不得跨 browser restart/reload 重新取得授权。

### 7.2 ChatGPT adapter

只允许读取：

- 经 synthetic fixture 核验的 Stop/stop-generating/stop-streaming button signal；
- 当前 assistant turn 是否存在及其有限结构变化，不传 text；
- `aria-label`、`role`、`data-testid` 等枚举化状态 signal；
- 固定 error marker 的 enum，不传错误文案；
- route 是否仍绑定当前 selected page 的私有 fingerprint，不进入 public state。

禁止 `fetch`/XHR/WebSocket override、MAIN-world bridge、`document.body.innerText`、`innerHTML`、`outerHTML`、截图、页面操作、网络请求、cookie、local/session storage 读取。DOM adapter 找不到信号时停发 working/completed 事实，改为 `sourceStatus=degraded` 和 freshness 降级。

## 8. 隐私字段分类与净化

| 分类 | 允许位置 | 示例 | 规则 |
| --- | --- | --- | --- |
| PRIVATE TRANSIENT | content script/adapter 瞬时局部变量 | route path、DOM element reference、Stop signal、私有 fingerprint | 不发 Node、不 log、尽快丢弃。 |
| PRIVATE NORMALIZED | service worker/Node 内存或 `storage.session` | tab identity、watchSessionId、sequence、cycle counter、capability、last receive time | 只用于路由/去重/TTL，不进 PublicState，不持久化 transcript。 |
| LOCAL HOST SECRET | Node/helper 私有配置 | 可选 helper↔Node challenge key、stable helper digest | 不进 extension、DOM、日志、PublicState、Hub；extension 不持有 Node token。 |
| PUBLIC | Node projector/既有 PublicState | provider/service、固定 safe label、working/completed、newReply、observedAt/age、freshness、source health、attention capability | 严格 allow-list、固定长度。 |
| FORBIDDEN | 所有持久化/日志/网络 payload | prompt、reply、transcript、完整 DOM、title/sidebar label、conversation URL/ID、cookie、Authorization、session token、screenshot/OCR、raw network error | 结构上不存在于 message schema；privacy tests 必须阻断。 |

首期最小公共状态建议：

```text
BrowserWatchPublicState {
  provider: "openai"
  service: "chatgpt-web"
  conversationLabel: "ChatGPT conversation"
  state: working/generating | completed
  newReply: boolean
  attention: null
  attentionCapability: unavailable
  observedAt: RFC3339 timestamp
  age: derived duration
  freshness: fresh | stale | unavailable
  sourceHealth: available | degraded | unavailable
}
```

`working/generating` 是同一个 canonical working 状态的两种产品文案；`completed/newReply` 是一次 delivery event，不是永久完成态；`attention` 首期必须为 null/unavailable，不把 idle、输入框、通知图标、tab focus 或 Stop 消失猜成 waiting-for-user。summary **延期、不实现、不传送**。后续即使考虑有界净化摘要，也必须另行定义来源、敏感数据拒绝、最大长度和不保存原文的契约。

硬上限：固定 label 只使用 ASCII 常量；provider/service 各 <=32 ASCII bytes；enum 固定 allow-list；单个 Native Messaging/Unix socket business message <=16 KiB，预期远小于 2 KiB；不保存 event history，只保存当前一个 watch 的 bounded last-good。

## 9. 拟议状态机

```text
UNSELECTED
  → SELECTED_STARTING
  → WORKING
  → COMPLETED_NEW_REPLY
  → WORKING (下一明确生成 cycle)

任意 active state → STOPPED（取消、route/origin 改变、tab close、browser lifecycle）
任意 active state → DEGRADED/STALE（adapter、native host、Node source failure）
```

| 输入 | 转移 | 公共效果 |
| --- | --- | --- |
| 用户 action | unselected → selected_starting | 只建立私有 watch；不声称正在生成。 |
| Stop + assistant-side activity | selected_starting → working | `state=working`。 |
| 同一 generation 的有限 activity | working → working | 刷新 observedAt/heartbeat，不保存文本。 |
| Stop 消失且之前确有本 cycle Stop、activity 和稳定窗口 | working → completed_new_reply | 一次 `newReply=true`；后续 heartbeat 不重复点亮。 |
| 普通 idle/输入框/通知 | 不转 attention | `attentionCapability=unavailable`。 |
| 用户取消 | active → stopped | stop best effort；Node 按 session/TTL 清理。 |
| 路由/跨 origin 不匹配 | active → stopped | 停止 observer，必须重新选择。 |
| DOM unsupported/unknown | active → degraded/stale | 不产生新 working/completed 事实。 |
| native host/socket/Node 无法接收 | current bounded state → unavailable | 不创建无限离线队列，不影响其他 agent/source。 |
| 浏览器重启/扩展 reload | selection lost | 不自动 resume；旧状态按 TTL 过期。 |

Node reducer 规则：同一 `(watchSessionId, sequence)` 同内容是 `duplicate`；同 key 不同内容是 `rejected`；较小 sequence 是 `stale`；更大 sequence 在 allow-list 通过后原子替换。completed heartbeat 只刷新 freshness，不重复 newReply。freshness 以 Node receive time 为主：0–30 秒 fresh，>30–120 秒 stale，>120 秒 unavailable；last-good 最多 5 分钟，之后清除。

## 10. 拟议本机接口

### 10.1 Native Messaging → browser-watch socket

首期不新增 Node HTTP route。接口链路和 schema 为：

```text
runtime.connectNative("com.devboard.browser_watch")
→ Chrome stdio framing
→ native host validates origin + message schema
→ 0600 ~/Library/Application Support/DevBoard/run/browser-watch.sock
→ Node BrowserWatch reducer
```

扩展发给 native host 的业务 JSON 是新的 `BrowserWatchMessageV1`，示例：

```json
{
  "schemaVersion": 1,
  "messageType": "browser_watch_observation",
  "eventId": "opaque-random-id",
  "watchSessionId": "opaque-session-id",
  "sequence": 42,
  "provider": "openai",
  "service": "chatgpt-web",
  "conversationLabel": "ChatGPT conversation",
  "state": "completed",
  "newReply": true,
  "attention": null,
  "attentionCapability": "unavailable",
  "observedAt": "2026-08-23T00:00:00Z",
  "sourceHealth": "available",
  "reason": "natural_completion"
}
```

`messageType`、字段 allow-list 和枚举独立于现有 `AgentEvent`；不复用、不弱化、不把 BrowserWatch event 塞入现有 AgentEvent schema。Node 应有独立 `BrowserWatchMessageV1`/`BrowserWatchState`/reducer seam。Unix socket fake receiver 可直接接收该 schema，测试无需 Chrome、真实 profile、Hub 或 NAS。

native host ACK 示例：

```json
{
  "schemaVersion": 1,
  "accepted": true,
  "eventId": "opaque-random-id",
  "result": "applied"
}
```

`result` 仅允许 `applied`、`duplicate`、`stale`、`rejected`；不得回显 body、selector、URL、DOM、error text 或 credentials。Node 接收失败时 helper 返回 bounded `unavailable`/断开，extension 只更新 source health。

### 10.2 Loopback fallback contract（非首期）

若 Core Auditor 后续要求 HTTP fallback，新增 route 只能是：

```http
POST http://127.0.0.1:<configured-port>/api/node/v1/browser-ai/events
Host: 127.0.0.1:<configured-port>
Origin: chrome-extension://<stable-extension-id>
Authorization: Bearer <rotatable-local-token>
Content-Type: application/json
```

仅 exact Host/Origin、Bearer、method、schema、16 KiB body cap 通过；无 redirect、`Cache-Control: no-store`、exact CORS、无 cookie auth。这个 fallback 必须单独进行 token 生成、人工配对、轮换、撤销、卸载后的 revoke、日志清理和其他本地进程伪造测试，且仍使用独立 BrowserWatch schema，不使用 AgentEvent。

### 10.3 PublicState projector

Node 内部保留私有：`watchSessionId`、sequence、tab binding、adapter capability、lastReceiveAt、native transport status。PublicState 只经过 allow-list projector，输出 provider/service、固定 label、working/completed、newReply、attention unavailable、observedAt/age、freshness/source health。Hub 仍只通过既有 M5/M5.4 sanitized snapshot 接收，不看到 extension ID、tab ID、conversation path、Native host error 或任何原文。

## 11. Future provider adapter 边界

```text
BrowserAIAdapter
  matchesSelectedOrigin()
  observe(document) -> NormalizedObservation
  capabilities() -> {working, completed, attention}
```

common layer 负责 action selection、schema、state machine、dedupe、Native/Unix transport、freshness 和 PublicState projector；provider adapter 只负责 host match、有限 selector/ARIA 常量、fixture 和 capability。新增 AI Web 服务必须单独 action opt-in，不能因为增加 provider 将 `activeTab` 换成 `<all_urls>`。没有稳定 attention signal 的 provider 必须显式 `capability unavailable`，不得通用推测。

## 12. 第一施工批次的建议文件边界

以下不是本次变更清单，只是获得施工授权后的建议边界。

### Extension

```text
browser-extension/
  manifest.json                         # public key + stable ID policy
  src/background/service-worker.js      # action, selection, Native Messaging
  src/content/observer.js               # opt-in MutationObserver only
  src/content/state-machine.js
  src/content/adapters/chatgpt-web.js
  src/shared/browser-watch-schema.js    # not AgentEvent
  src/shared/privacy.js
  test/fixtures/chatgpt/*.html
  test/*.test.js
```

### DevBoard Node/native host

```text
cmd/devboard-browser-watch-host/main.go       # stable stdio helper entry
internal/browserwatchhost/native_protocol.go  # Chrome framing + 16 KiB cap
internal/browserwatchhost/socket.go           # separate Unix socket, 0600
internal/browserwatchhost/install.go          # explicit channel-aware ownership
internal/browserwatch/model.go
internal/browserwatch/reducer.go
internal/browserwatch/store.go
internal/browserwatch/privacy.go
internal/browserwatch/transport.go
internal/browserwatch/*_test.go
internal/state/*_test.go                      # additive PublicState projector tests
internal/uplink/*_test.go                     # no topology change
```

第一批不应修改 M0/M4/M5 冻结合同、现有 AgentEvent schema、Hub 路由、Node → Hub 拓扑、模板、Safe Navigation、agent hooks、quota/history、capture/network proxy、部署文件或 NAS 配置。Native host manifest 是未来显式安装动作的用户级生成文件，不应把当前工作区变成真实 Chrome profile 修改。

## 13. 测试策略与矩阵

### 13.1 单元测试

| 领域 | 必测项 |
| --- | --- |
| adapter | exact `chatgpt.com` match；legacy unsupported/redirect；Stop/ARIA/data-testid allow-list；unknown UI → degraded；无正文读取。 |
| observer | mutation 合并/debounce、observer root 缺失、route change、disconnect、unload、不会每 mutation 发 event。 |
| state machine | selected/working/stabilized completion/manual stop/attention unavailable/unknown/newReply exactly once。 |
| privacy | fixed label、UTF-8 bytes、control/URL/token/high-entropy rejection；schema 不存在 prompt/reply/DOM/cookie/header。 |
| reducer | duplicate、same-key conflict、lower sequence、cross-session isolation、heartbeat、atomic replace、TTL。 |
| freshness | 30s、120s、5m 边界；observedAt 漂移；receiveAt 权威；last-good 删除。 |

### 13.2 Native host/本机契约测试

| 场景 | 断言 |
| --- | --- |
| manifest | 三个 macOS browser channel 的精确用户级目录；绝对 `path`；`type=stdio`；exact stable origin；wildcard/错误 ID 拒绝。 |
| install/repair | 同一输入幂等；binary/manifest/socket ownership；冲突文件不覆盖；repair 不写其他 profile/extension 配置。 |
| uninstall | 只移除 DevBoard 自有文件；外部修改 manifest 保留；一个渠道不删除另一个渠道。 |
| framing | native-endian 32-bit length；malformed length、invalid UTF-8、duplicate key、16 KiB over-limit、stdout 非 JSON 全拒绝；stdout 无日志。 |
| stderr | synthetic prompt/reply/cookie/auth canary 不出现在 stdout、stderr、helper log 或 Node log。 |
| socket | parent directory 0700、socket 0600、wrong uid/peer rejected where supported；fake receiver 可独立模拟 Node。 |
| fail-open | helper/Node/socket down 只产生 BrowserWatch unavailable/degraded；不阻断 agent event、Hub 或其他 source。 |

### 13.3 Browser E2E

只用 disposable Chrome/Chromium profile、本地 synthetic fixture 和 fake native host；不使用真实 profile、真实账号、真实 ChatGPT transcript：

1. 未 action 的 tab 不注入、不产生 DOM event；action 后只监控当前 tab。
2. 选择新 tab 先停止旧 watch，始终最多一个 active watch。
3. Stop + assistant activity + 稳定窗口后只产生一次 completed/new reply；manual stop 不产生 new reply。
4. 普通 idle、输入框、通知图标不变成 attention；首期显示 capability unavailable。
5. selector/DOM fixture 升级变 degraded/stale，不切换 OCR/CDP/network。
6. route change、cross-origin、refresh、tab close、browser restart、extension reload 均要求重新 action。
7. service worker 回收后 bounded sequence/reconnect 不乱序；native host 断连只影响 source health。
8. 重复、乱序、超限、错误 origin/ID 的消息不污染 Node state。
9. Node 不存在、Hub 不启动时，本地 reducer、fake socket receiver 和 `/api/state` 仍可验证；本轮不新增 HTTP route。
10. synthetic fixture 全部通过后，真实 ChatGPT E2E 才可由 Core Auditor 另行授权。

### 13.4 隐私测试

使用合成 `PROMPT_CANARY_M6`、`REPLY_CANARY_M6`、`COOKIE_CANARY_M6`、`AUTH_CANARY_M6`，断言 Chrome message、native stdout/stderr、Unix socket body、Node memory/log/PublicState、Node → Hub snapshot 均不存在 canary；不存在 `document.body.innerText`/`innerHTML`/`outerHTML`/screenshot/network body path；没有 cookie、Authorization、session token API/permission；public output 只有 allow-list；source failure 不擦除 unrelated state。

## 14. 已知不稳定点与降级行为

| 不稳定点 | 影响 | 降级 |
| --- | --- | --- |
| ChatGPT button/ARIA/data-testid/DOM hierarchy、A/B test、虚拟化渲染 | signal 失配、completion 漏报 | adapter degraded/stale；不扩大 selector、不换 OCR/CDP/network。 |
| SPA route、origin、page reload | watch binding 失效 | 停止 observer，要求重新 action；不继承新会话。 |
| MV3 service worker ephemeral/background throttling | timer/全局 Map/heartbeat 延迟 | `storage.session` + event-driven；Node 按 receiveAt stale；不依赖长驻 worker。 |
| browser restart/extension reload/activeTab revoke | 旧授权消失 | selection 丢失，必须重新选择。 |
| Native host manifest channel/version 路径 | host not found | 显式 channel/version matrix；未知路径不猜测、不启动。 |
| manifest key/stable ID 改变 | allowed origin mismatch | source unavailable；要求 trusted re-install/repair，不放宽 allow-list。 |
| Native host stdout/stderr/protocol、socket 权限或 Node down | bridge 断连 | source unavailable/degraded；Node/agent/Hub fail-open。 |
| 同用户恶意进程/同 ID unpacked extension | 可尝试伪造 helper/socket 或获得同 ID | 0600/0700、exact allow-list、schema/sequence、trusted dogfood；不把 ID 当秘密；更强 OS sandbox 不属于首期。 |
| OpenAI 无普通会话官方状态 API | 无 provider-authoritative source | 只报告已观察 enum；不读取网络或凭据；失配即 degraded。 |

## 15. 明确非目标

M6 首个实现批次不包含：

- 任意网页、任意 tab、历史、书签、top sites 或浏览行为分析；
- 完整 prompt/reply/transcript、页面 title/sidebar label、conversation URL/ID、模型/账号身份；
- cookie、Authorization header、session token、API token、浏览器 profile 文件/local storage；
- 登录、绕过 CSP/权限/provider 安全机制；
- 远程控制、自动回复、点击、输入、提交、滚动、导航、聚焦、下载、复制；
- Safe Navigation、remote approve/deny、stop/retry/continue、quota/account switching；
- CDP/debugger、remote debugging port、webRequest、proxy、WebSocket/HTTP body interception；
- desktop/tab capture、截图、OCR、视觉模型、系统通知/accessibility API 作为 source；
- Node 新 HTTP route（Native Messaging 首期）、Hub 新 endpoint、Node → Hub 拓扑改变、NAS SSH/deploy/acceptance；
- Chrome Web Store 发布、自动安装扩展、自动安装/修改其他软件的 native host；
- 真实 profile、真实 transcript fixture、未经另行授权的真实 ChatGPT E2E；
- 其他 AI Web 服务实现；只保留 adapter 边界。

## 16. 仍需 Core Auditor 决定的问题

已决的生命周期、host、label、summary、attention、watch 数量、freshness、E2E gate、Native Messaging 首选、分发政策和 NAS 禁止项不再列在这里。真正仍需决策的只有：

1. **公共字段命名**：`BrowserWatchPublicState` 是否采用 root `browserWatches`，以及 `attentionCapability`/`sourceHealth`/`freshness` 的最终字段名；不得因此引入 raw browser fields 或修改既有 AgentEvent schema。
2. **实现文件边界**：首批 extension、Node reducer、stable native host、channel-aware install/repair helper 的最终目录/构建打包边界；不得扩大到冻结合同、Hub、模板、部署或 NAS。

## 17. 参考再利用登记

| Reference | Revision/date | License/decision | DevBoard-specific difference |
| --- | --- | --- | --- |
| Chrome MV3/Native Messaging official docs | Retrieved 2026-08-23 | **ADAPT API contract** | Exact stable ID, one selected tab, bounded schema, no HTTP route in first batch. |
| OpenAI official help/API docs | Retrieved 2026-08-23 | **BEHAVIORAL REFERENCE ONLY** | No Web session API/credential integration. |
| `dawidstruzik/chat-alert` | `665be3d48362c3d1a2b08f009f78a47b66d94ef9`, MIT | **BEHAVIORAL REFERENCE ONLY** | Reject MAIN-world network interception and reply preview. |
| `kkonstantin08/chatgpt-done-notifier` | `54381bf1c3362570d0dd8186dc258fa0c35e95ad`, no declared license found | **BEHAVIORAL REFERENCE ONLY** | Do not copy code or text fingerprint/body scan. |
| Existing DevBoard M5 PublicState/uplink | `36e688fe6c38b834efe06a596059c89aff0a2b86` | **USE AS AUTHORITY** | BrowserWatch enters local Node then existing sanitized Node → Hub path. |

## 18. 审计状态

```text
M6_REFERENCE_AUDIT=READY_FOR_CORE_REAUDIT
```

这表示 reference audit、Native Messaging/loopback 传输比较、扩展 ID/分发审计、权限和威胁模型、隐私边界、状态机、本机接口、安装卸载边界和测试策略已完成，等待 Core Auditor 复审；不表示 M6 已实现、扩展已安装、真实 ChatGPT 已验收或 NAS 已接触。
