# 音源插件协议 v1（草案）

状态：与 Astra 评审合并后的候选稿，待确认后冻结。契约语义见 `docs/SOURCE_CONTRACT.md`；
本文只定义外置音源插件与宿主之间的进程协议、宿主服务、安装与故障处理。

## 1. 范围与非目标

- 协议让外置进程实现与内置源相同的 `Source` 语义，宿主不需要为某个插件写特例。
- v1 只支持「一个插件 = 一个 Source」。多源打包、多实例、热重载不在 v1 内。
- 不做沙箱承诺。插件以用户权限运行，可自行联网、读自己的文件；宿主只保证经宿主 API 的
  会话读写按插件隔离，不保证隔离同用户下的其他程序。
- 不提供通用 HTTP 代理、任意 JavaScript 执行或任意界面定义。

## 2. 进程模型

- 宿主为每个启用的插件启动一个子进程：`exec.Command(entry)`，直接执行，不经 shell。
- 工作目录为插件目录；stdin/stdout 用于协议，stderr 持续排空并按行限速记录，避免管道堵塞。
- 环境变量由宿主注入固定集合（插件 ID、协议版本、数据目录），不继承宿主的敏感变量。
- 插件不得写宿主的标准输出；stdout 出现非协议字节即判定协议错误。
- 关闭顺序：停止接单 → 取消在途任务 → 关闭关联登录窗口 → 发 `shutdown` → 最多等 2 秒 →
  终止进程并 `Wait` 回收。macOS 管理进程组，Windows 使用 Job Object 并关闭控制台窗口。
- 崩溃后 v1 不自动重启、不重放登录与导入；实例状态标为「故障」，由用户显式重试。

## 3. 帧格式

- UTF-8 编码的 JSON-RPC 2.0，单条消息一帧，`Content-Length: N\r\n\r\n<body>`。
- 长度按字节计算；头名大小写不敏感；`Content-Type` 可选，出现时必须是 `application/json`。
- v1 不支持 batch。单帧上限 8 MiB，单条消息超限直接终止实例并报 `protocol` 错误。
- 残缺帧、负长度、非 UTF-8、请求 ID 类型非法都终止实例，不做猜测性恢复。

## 4. 生命周期

```text
宿主                              插件
 |-- initialize (request) ---------->|  params: {protocolVersion, hostServices[], limits, hostVersion}
 |<-- result {protocolVersion, descriptor, requiredHostServices[], capabilities?} --|
 |-- initialized (notification) ---->|  params: {generation}（不在这里发写令牌）
 |-- source.* / host.* 双向调用 ... |
 |-- shutdown (request) ------------->|
 |<-- result -------------------------|
```

- `initialize` 是请求，**请求参数携带宿主愿意提供的服务子集与限制**（`hostServices`、大小/超时上限、
  资源域校验模式、平台标识）；插件在响应里返回它**实际需要的** `requiredHostServices` 子集与运行时 `Descriptor`。
- 插件只能使用自己声明且宿主同意提供的服务；请求未列出的服务一律不可调用，宿主返回 `unsupported`。
- 宿主必须核对 `requiredHostServices` 是 `hostServices` 的子集，否则拒绝启用并显示差集。
- 随后宿主发 `initialized` 通知，只携带账号代次；写令牌不在此下发，而是随
  `source.auth.login` / `source.auth.persistSession` 的调用参数下传。这之前插件不得发起业务调用。
- `Descriptor.ID` 必须等于 `manifest.id`，否则拒绝启用。
- 版本协商：双方各自给出支持的 [min,max] 范围，取交集；无交集拒绝启用并给出清晰提示。
  同一主版本只允许文档中约定的可选字段扩展；未知必需能力不得被忽略。
- `initialize` 期间宿主已运行授权检查（摘要、能力、路径）；未获用户批准的插件不得启动。

桌面框架差异只影响宿主实现，不改变协议：macOS 用 WKWebView，Windows 用 WebView2，两者都走 `host.webview`。

## 5. 业务方法

业务方法一一对应 Go 契约，使用 `source.*` 前缀，参数与返回值都是显式的 wire DTO：

```text
source.describe               → Descriptor
source.auth.status            → AuthStatus
source.auth.verify            → AuthStatus
source.auth.login             {kind, attemptId, writeToken} → AuthResult
source.auth.logout            → {}
source.auth.persistSession    {operationId, writeToken} → {persisted}
source.library.import         {collection, runId, cursor, limit} → ImportPage
source.library.resolve        {track, quality} → Playback
source.catalog.search         {query, cursor, limit} → Page<Track>
source.catalog.playlists      {kind, cursor, limit} → Page<Playlist>
source.catalog.playlistDetail {ref, section?, cursor, limit} → PlaylistDetail
source.catalog.albumDetail    {ref, section?, cursor, limit} → AlbumDetail
source.catalog.artistDetail   {ref, section, cursor, limit} → ArtistDetail
source.catalog.history        {cursor, limit} → Page<Track>
source.lyrics.get             {ref} → LyricsLine[]
```

- v1 的 `catalog.search` 只返回歌曲，没有 `kinds`，不分实体类型；多实体搜索是后续版本。
- `collection.kind` 取 `favorites` 或 `playlist`；后者必须带 `collection.id` 与
  `collection.playlistKind`（`owned|favorite|chart`）。枚举只此一套，不得另造 `collected`。
- `import` 的 `runId` 是宿主生成的关联 ID，插件只回显，不决定提交。
- 方法名与契约分组一致，不再另起一套命名。
- 请求 ID 使用 JSON-RPC envelope 的 `id`；不得再引入同名的业务 `requestId`。
  `runId`/`attemptId` 只用于跨调用关联（导入 run、登录 attempt）。
- 未实现的方法返回 `-32601`；实现了但当前不可用返回结构化 `unsupported` 业务错误。
- 宿主与插件各自维护请求表；建议宿主用 `h:*`、插件用 `p:*` 字符串 ID，便于日志排查。

## 6. 宿主服务

插件通过 `host.*` 回调宿主。所有宿主服务都绑定当前插件身份，插件不得传其他插件的 ID。

### 6.1 会话存储

```text
host.session.get    {key} → {value(base64), revision, exists}
host.session.set    {key, value(base64), ifRevision, writeToken} → {revision}
host.session.delete {key, ifRevision, writeToken} → {}
host.session.list   → {keys[]}
```

- namespace 由宿主从进程身份推导，插件不可指定。
- 值为不透明字节，JSON 以 base64 传输；宿主不解释内容，格式迁移由插件负责。
- 上限：单 value 解码后 1 MiB，每插件总量 8 MiB，最多 64 个 key；超限明确失败，不截断。
- 读写分离：`get/list` 不需要令牌；`set/delete` 必须带宿主签发的 `writeToken`。
- **令牌只由宿主签发，并随调用参数下传；插件没有申请入口。**
  - 登录：宿主在调 `source.auth.login` 时把 `writeToken` 放进参数，绑定这一次 attempt；
    attempt 取消、超时、失败或结束后立即失效，旧登录任务无法写回。
  - 重试保存：宿主在用户点击重试时生成 `operationId` 与一次性令牌，调
    `source.auth.persistSession {operationId, writeToken}`，插件只能在该次调用内使用。
  - 账号代次变化（登录成功、退出、切号）使所有未使用令牌立即失效；每次 `set/delete`
    的令牌必须未过期、未使用、代次一致，否则返回 `conflict`。
- `set/delete` 原子写并支持 `ifRevision` 乐观并发；空值与不存在必须可区分。
- 退出登录时宿主直接清空该源的会话命名空间（宿主撤销授权），不要求插件持删除令牌。
- 宿主用 AES-GCM 加密落盘，关联数据绑定插件 ID、key 与宿主封装版本。
- 失败后的会话只在内存中，UI 持续显示「仅本次运行有效」；用户点重试走上面的
  `persistSession`，成功后才转为已持久化，全程不重新认证。

### 6.2 登录窗口

```text
host.webview.open  {attemptId, url, allowedDomains[], callback{rules}, cookies?} → {callbackUrl, sessionSnapshot}
host.webview.close {attemptId} → {}
```

- 宿主打开带标题的临时窗口（macOS 复用 `hm_web_auth_start` 的 NSWindow + WKWebView；
  Windows 用 WebView2，非持久化数据存储），不注册全局 scheme，不改系统代理。
- 回调匹配至少校验 scheme + host + path；只匹配 scheme 视为无效。
- 回调发生或用户取消后立即关闭窗口；返回 `callbackUrl` 与受限会话快照。
- 可选 `cookies` 只允许写入本次临时窗口、声明域内的 Cookie；v1 不提供 `evaluateJavaScript`，
  不承诺 localStorage/sessionStorage。
- 宿主不解析平台令牌；插件自行验证 state/PKCE/一次性消费并兑换业务会话。
- 系统授权 sheet 若无法导出会话数据，不得在请求要求该能力时静默替代 WebView。

### 6.3 二维码展示

```text
host.auth.present {attemptId, kind:"qr", payload, expiresAt} → {}
```

- 宿主只渲染规定的数据类型（二维码内容 + 过期提示），不接收 HTML、脚本或任意界面定义。
- 扫码轮询、二维码刷新、成功判断全部由插件完成；阶段变化通过 `$/progress` 通知。
- 只有宿主当前主动发起的登录操作对应的 `attemptId` 才被接受；`initialize` 期间不得弹窗。

### 6.4 其它

```text
host.log       {level, message, fields?}        结构化日志，不含凭据/URL
$/progress     {requestId, phase, current, total, message}  进度通知
$/cancelRequest {id}                            取消在途请求
```

- 取消统一用 `$/cancelRequest`，进度统一用 `$/progress`；不另设 `host.cancel`/`host.progress`。
- v1 不提供 `host.http`：插件自己联网。宿主为播放器/封面加载资源属于特定业务通道。
- 通知只描述过程，不决定结果；页面响应与宿主提交才决定终态。

## 7. 双向调用与并发

最容易漏掉的一点：**宿主正在等待 `source.auth.login` 时，必须仍能处理插件发来的
`host.webview.open`。** 读循环不能同步执行耗时 handler。

- 读循环只负责解帧与分派；响应匹配、请求执行、写队列分别管理。
- 每个方向的调用表独立；等待对端响应时不得持 registry、会话或数据库锁。
- 写操作串行化到一个队列，避免并发写导致帧交错。
- 插件同一时刻最多 N 个在途业务调用（默认 4）；超过由宿主排队，不并发轰炸。

## 8. 线格式与持久化格式

- 线格式使用专用 wire DTO：固定字段名、单位、枚举、可空语义；不复用 Go 领域结构体。
- 播放地址只在线传输；持久化 DTO 永远没有 URL 字段。当前 `Playback.URL` 的 `json:"-"` 只是
  领域模型层的保护，不能当作线格式定义。
- 未知可选字段可忽略；未知枚举不得回落成 `full`、已登录或完整快照等「有利」值。
- 时间统一 RFC 3339 UTC；时长统一毫秒；金额/大小用整数。Go 契约里的 `Duration *float64` 是秒，
  wire 必须换成 `durationMs int64`，适配层负责换算，不得让协议沿用秒。
- 外置插件不能依赖 Go `internal` 包，协议文档、样例与一致性测试就是它的规范。

最小 wire DTO（字段名不得随意改动）：

```json
Track {
  "source": "huawei", "id": "…", "title": "…",
  "artists": [{"id":"…","name":"…","coverUrl":"…"}],
  "album": {"id":"…","title":"…","coverUrl":"…","artists":[…]},
  "coverUrl": "…", "durationMs": 0,
  "version": {"edition":"…","remaster":false}
}

Playback {
  "url": "https://…", "expiresAt": "2026-11-03T08:00:00Z",
  "quality": "lossless", "access": "full"
}
```

- `access` 只有 `full|preview|unknown` 三个值；缺失按 `unknown`，不得按 `full`。
- `expiresAt` 缺失表示「当次有效」，宿主不得跨播放复用该 URL。
- `Page<T>` 固定为 `{items, nextCursor, hasMore}`；`ImportPage` 额外有 `complete` 与
  `remoteDeleted`（5.1），不能只靠 `nextCursor` 为空推断整次完成。

## 9. 错误与超时

- 业务错误返回 JSON-RPC error：`code` 固定，`message` 为安全短句，`data.kind` 为契约中的错误类别，
  可选 `retryAfterMs`、`diagnostic`。

| kind | JSON-RPC code | 含义 | UI 建议 |
| --- | --- | --- | --- |
| `authRequired` | -32001 | 需要登录或会话失效 | 去登录 |
| `permission` | -32002 | 无权限（会员/地域/隐私） | 说明原因，不重试 |
| `notFound` | -32003 | 实体不存在 | 返回上一级 |
| `rateLimited` | -32004 | 触发限流 | 显示 `retryAfterMs` |
| `unsupported` | -32005 | 源未实现该能力 | 隐藏入口 |
| `canceled` | -32006 | 调用方取消 | 不提示错误 |
| `conflict` | -32007 | 写令牌/版本冲突 | 刷新后重试 |
| `protocol` | -32008 | 协议/数据格式错误 | 记日志并提示重试 |
| `transient` | -32009 | 网络或临时故障 | 允许重试 |
| `unavailable` | -32010 | 插件实例不可用 | 提示重启源 |

- 禁止把平台原始响应、凭据、URL、堆栈直接放进 `message`。
- 默认超时：普通业务请求与单页导入 30 秒；交互登录独立设置较长超时（例如 5 分钟）。
  进度通知不能无限延长硬超时。
- 取消不是「远端一定停止」：取消先于发布事务生效则不发布；事务已提交后收到取消返回已完成，
  不得显示「已取消」却已修改本地库。
- 长调用取消后仍不响应的插件按挂起处理：终止实例，其它在途调用返回 `unavailable`。
  普通网络超时不得立即当成进程崩溃。

## 10. 清单（source.json）

```json
{
  "schemaVersion": 1,
  "id": "netease",
  "name": "网易云音乐",
  "shortName": "网易",
  "accent": "#C20C0C",
  "icon": "assets/icon.svg",
  "entry": { "darwin-arm64": "bin/netease-darwin-arm64", "windows-x64": "bin/netease.exe" },
  "protocol": { "min": 1, "max": 1 },
  "capabilities": {
    "search": true, "playlists": true, "playlistDetail": true,
    "albumDetail": true, "artistDetail": true, "history": true,
    "lyrics": true, "import": true, "resolve": true,
    "loginKinds": ["qr"], "loginRequired": ["import", "resolve", "history"],
    "collections": [{ "kind": "favorites", "name": "我喜欢的音乐" }],
    "maxPageSize": 100, "qualities": ["standard", "lossless"]
  },
  "hostServices": ["session", "webview", "auth.present", "log"],
  "resourceDomains": ["*.music.163.com", "*.126.net"],
  "digest": { "algorithm": "sha256", "files": { "bin/netease-darwin-arm64": "…" } }
}
```

- `manifest` 提供未启动时的展示信息；`initialize` 返回的运行时 `Descriptor` 按下列规则与之一致：

  | 字段 | 规则 |
  | --- | --- |
  | `id` | 必须相等，否则拒绝启用 |
  | `name` / `shortName` / `icon` / `accent` | 允许本地化与更精确的运行时值，但不得指向插件目录外的资源 |
  | `capabilities.*` | 运行时只能是 manifest 的子集，不能新增能力 |
  | `hostServices` / `requiredHostServices` | 运行时必须是 initialize 请求中所提供服务的子集 |
  | `resourceDomains` | 运行时不得超出 manifest 声明 |
  | `protocol` | 由版本协商决定，不能单方面放宽 |
- 图标是插件目录内的受限本地资源：SVG 需去除 `<script>`、`<foreignObject>`、事件属性与外部 href，
  上限 64 KiB；PNG ≤128 KiB。缺图回退 shortName 文本 + 中性色。
- `resourceDomains` 是宿主媒体请求的域名策略（逐次校验重定向），不约束插件自身联网。
- `digest` 是校验输入，不是信任证明；宿主重算摘要，覆盖可执行文件、随包 DLL/脚本与关键资源。

## 11. 安装、授权与升级

- 插件安装在应用数据目录（`appmeta` 派生）下，不修改签名 app bundle，不依赖 Program Files 写权限，
  不需要管理员权限。
- 启用前展示：插件名称、实际路径、重算后的摘要、声明的能力与宿主服务，用户显式同意后记录授权。
- 授权记录绑定路径、插件 ID、包摘要、声明能力；升级改变摘要后重新确认，不能先启动新版本再询问。
- 运行时能力少于声明允许；申请额外宿主服务或资源域必须拒绝，不得静默扩大授权。
- 卸载删除插件目录与宿主会话命名空间；本地库中的歌曲与收藏不删除，只把该源绑定标记为不可用。

## 12. 安全边界说明

- 会话加密解决的是「同一台机器上其它程序不能直接读到明文」，不解决「同用户恶意程序」问题：
  密钥与密文同处本地文件，未沙箱化进程仍可读文件、联网。
- RPC 的 namespace 只防止插件经宿主 API 串读其它插件的会话。
- 摘要证明「安装后未被替换」，不证明作者可信。
- 生命周期管理能回收正常退出的插件进程，不应声称能约束主动逃逸的恶意进程。
- 这些限制必须原样写进用户可读的插件说明，不夸大隔离能力。

## 13. 版本与兼容

- `schemaVersion` 描述 manifest 结构；`protocol.min/max` 描述线协议。
- 主版本不兼容拒绝启用；次版本只允许可选字段扩展与新增宿主服务的协商启用。
- 不得用「猜一个旧版本」的方式降级；取交集失败就明确拒绝并给出版本信息。

## 14. 验证矩阵

| 场景 | 进程内源 | 外置插件 |
| --- | --- | --- |
| 分页不变量（cursor 推进、末页空、Complete） | 必测 | 必测 |
| 双向重入（等登录时回调 webview） | 必测 | 必测 |
| 取消/超时/挂起 | 必测 | 必测 |
| 崩溃与进程树回收 | — | 必测 |
| 账号切号与代次失效 | 必测 | 必测 |
| 异常分页、超限帧、中文路径 | 必测 | 必测 |
| 升级重新授权 | — | 必测 |

- 合成源用于协议与二维码通道验证；**不能把合成源结果记为真实网易云登录通过**。
- 真实华为 WebView 登录与真实扫码分别留证；Windows 实机验收独立记录。

## 15. 示例（节选）

```text
→ {"jsonrpc":"2.0","id":"h:1","method":"initialize","params":{"protocolVersion":{"min":1,"max":1},"hostServices":["session","webview","auth.present","log"],"limits":{"sessionValueBytes":1048576,"sessionTotalBytes":8388608,"sessionKeys":64},"hostVersion":"1.0"}}
← {"jsonrpc":"2.0","id":"h:1","result":{"protocolVersion":1,"descriptor":{...},"requiredHostServices":["session","webview","auth.present","log"]}}
→ {"jsonrpc":"2.0","method":"initialized","params":{"generation":1}}

→ {"jsonrpc":"2.0","id":"h:2","method":"source.auth.login","params":{"kind":"webview","attemptId":"login-1","writeToken":"wt-login-…"}}
  ← {"jsonrpc":"2.0","id":"p:1","method":"host.webview.open","params":{"attemptId":"login-1","url":"https://…","allowedDomains":["…"],"callback":{"scheme":"…","host":"…","path":"…"}}}
  → {"jsonrpc":"2.0","id":"p:1","result":{"callbackUrl":"…","sessionSnapshot":{...}}}
  ← {"jsonrpc":"2.0","id":"p:2","method":"host.session.set","params":{"key":"account","value":"…","ifRevision":0,"writeToken":"wt-login-…"}}
  → {"jsonrpc":"2.0","id":"p:2","result":{"revision":1}}
← {"jsonrpc":"2.0","id":"h:2","result":{"succeeded":true,"persisted":true,"status":{"state":"signedIn","accountKey":"…"}}}
```

注意方向：宿主先调 `source.auth.login`，插件在**这次调用内部**再嵌套调 `host.webview.open`；
不是宿主先开窗口再等插件返回。

## 16. 实现现状（2026-11-03）

已落地于 `native/internal/plugin/`：

- `manifest.go`：`source.json` 读取与校验（schemaVersion、id、protocol.min、entry 不得逃逸插件目录），
  `PlatformKey()` 把 `darwin-arm64`/`windows-x64` 映射到当前平台。
- `transport.go`：Content-Length 帧、请求 ID 关联、宿主方法在独立 goroutine 中处理以支持重入、
  `$/cancelRequest`、stderr 环形缓冲、单帧 16 MiB 上限、默认 60 秒调用超时。
- `source.go`：插件侧 `source.Source` 适配器，`mergeDescriptor` 校验运行时描述不得扩大 manifest 能力，
  错误码映射到契约的 `source.Kind*`，写令牌由适配器在 `Login` 前向宿主申请并随 `source.auth.login` 下传。
- `host.go`：`host.session.*`（加密命名空间、revision、写令牌）、`host.webview.open`、`host.log`。
- `manager.go`：扫描 `<数据目录>/sources/<id>/source.json`，启用/停用与进程生命周期，
  `LoadEnabled` 在启动时恢复上次启用集合，启用状态存在 `storage` 的 `plugin.enabled`。
- Shell：设置 → 音源 → 「外置音源」列出插件；首次启用弹出授权对话框（名称、目录、权限说明），
  启用后进入同一 `Registry`，搜索页签、导入对话框与设置列表都自动出现该源。

### 16.1 华为源作为预置外置插件（同日晚）

- `native/cmd/huawei-source` 是随应用分发的插件进程：`native/internal/plugin/sdk`（插件侧协议）
  与 `native/internal/plugin/wire`（帧与 JSON-RPC 基础）供它与第三方插件共用；
  `native/plugins/huawei/source.json` 声明 `builtin: true`、入口按平台、能力与资源域。
- `tinglan-tool mac-app` 把插件构建到 `Tinglan.app/Contents/Resources/sources/huawei/`；
  宿主启动时把该目录加入 Manager 根，`Builtin` 插件在用户未显式禁用时默认启用。
- 登录完全走协议：插件 `source.auth.login` 内部嵌套 `host.webview.open` 打开系统登录窗口，
  回调 URL 返回后在插件进程内做 code exchange，再把会话写进 `host.session`；宿主不再持有 provider 会话。
- 旧版本地登录在首次启动时迁移：宿主读取旧会话，按插件 `Auth().Status()` 的账号指纹比对，
  不一致先清空命名空间，再以 `host.session` 写入；迁移成功后删除旧会话文件。
- 会话状态、过期/未知、账号切换都由插件 `source.auth.status/verify` 回报；`cmd/music` 只镜像 `source.Session`。
- 播放地址不进入持久化 JSON：`music.Playback` 的 `URL`/`LyricURL` 标记为 `json:"-"`，
  插件边界用显式的 `playbackWire` 传递（`sdk.wirePlayback` 与 `plugin.playbackWire`）。
- 集成测试：`internal/plugin/huawei_plugin_test.go`（临时目录构建插件并握手）、
  `internal/plugin/live_huawei_test.go`（`TINGLAN_LIVE_PROFILE` 指向真实数据目录，验证搜索/歌单详情/历史/播放解析/歌词）；
  `internal/plugin/host_test.go` 校验 WebView 登录窗口的回调规则与域名白名单，并拒绝 `requireSessionExport` 与 http 地址。
- 实机验证（2026-11-03 晚）：应用启动后插件进程运行，音源页显示「华为音乐 · 已启用」与已登录按钮，
  收藏 942 首；搜索「周杰伦」经插件返回结果并带来源徽标；收藏导入分页经插件读取成功（前两页 100 首，只读）；`TestPlaybackStartLatency` 经
  `TINGLAN_PLUGIN_ROOT` 指向应用包插件目录，真实账号下 `AUDIO_STARTED after 1.76s`（位置推进、时长 270s），
  即插件解析 + 远程解码 + 播放全链路可用；live 测试另验证歌单详情、历史 200 条、歌词地址。
  登录窗口（2026-11-03 晚）：设置→音源→重新登录 经插件 `Auth.Login` → `host.webview.open` 弹出
  「听澜 · 华为账号登录」窗口（AXWebArea：华为账号-登录），关闭窗口即取消，原会话保持不变
  （`source_accounts` 仍为 huawei|91c7…|logged_in）。
  真实扫码登录（2026-11-03 21:43）：用户在插件弹出的登录窗口完成扫码，插件收到回调后
  `Accept → Exchange → ValidateSession → host.session.set`，宿主命名空间写入会话，
  shell 镜像账号后 `source_accounts` 更新为 huawei|91c7…|logged_in；随后搜索「周杰伦」
  经插件返回结果，证明新会话可用。未验证：Windows 实机（仅 CGO_ENABLED=0 交叉编译）。

尚未实现：

- `host.auth.present`（纯 HTTP 扫码需要在宿主窗口渲染二维码）当前返回 `-32004`；
  依赖它的源只能走 `host.webview.open`。
- 升级重新授权、签名校验、插件市场/下载、自动更新、多实例与热重载。
- `host.webview.open` 目前复用 macOS 内嵌 WKWebView 与 Windows WebView2 的登录窗口路径，
  `RequireSessionExport` 明确拒绝；Windows 侧未实机验证。

验证：`go test ./internal/plugin/` 覆盖发现、启用/停用、恢复、写令牌、命名空间隔离、路径逃逸；
`native/internal/plugin/testdata/sample-plugin/` 提供可重复安装的最小外置源，用于本机端到端验证。
