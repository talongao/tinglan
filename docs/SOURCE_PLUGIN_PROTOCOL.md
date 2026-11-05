# 音源插件协议 v1（当前实现参考）

核对日期：2026-11-04。本文记录当前代码可互操作的格式；此前冻结设计与实现有差异，
差异以各节「当前限制」明确标注，不把设计目标当作已提供能力。语义目标见
[SOURCE_CONTRACT.md](SOURCE_CONTRACT.md)，历史验证见 §16。

> 面向插件作者的上手教程（含最小 Node 示例、调试与常见问题）见
> [PLUGIN_GUIDE.md](PLUGIN_GUIDE.md)。

## 1. 范围与非目标

- 协议让外置进程实现与内置源相同的 `Source` 语义，宿主不需要为某个插件写特例。
- v1 只支持「一个插件 = 一个 Source」。多源打包、多实例、热重载不在 v1 内。
- 不做沙箱承诺。插件以用户权限运行，可自行联网、读自己的文件；宿主只保证经宿主 API 的
  会话读写按插件隔离，不保证隔离同用户下的其他程序。
- 不提供通用 HTTP 代理、任意 JavaScript 执行或任意界面定义。

## 2. 进程模型

- 一个已启用插件对应一个子进程，直接 `exec.Command(command,args...)`，不经 shell；工作目录为插件目录。
- stdin/stdout 专用于协议，日志写 stderr。宿主持续排空 stderr，保留最近 8 KiB，握手失败时可能显示其首行。
- 当前继承宿主 `os.Environ()`，再追加入口 `env`，没有敏感环境变量过滤或插件身份环境变量注入。
  插件身份来自插件清单 `source.json` 的 `id`；宿主在 `initialize.assignedId` 中回传同一个值。
- 当前关闭过程为发 `shutdown` **通知** → 关闭 stdin → 等待最多 2 秒 → kill 直接子进程并等待回收。
  不承诺进程组/Job Object 或整个后代进程树回收；插件应自行结束创建的任务。
- 崩溃不自动重启或重放任务；由用户停用后重新启用。当前 Manager 的 Running 是注册状态，
  不能当作实时进程健康证明。

## 3. 帧格式

- UTF-8 JSON-RPC 2.0，一条消息一帧：`Content-Length: N\r\n\r\n<body>`，长度按正文**字节**计算。
- 单帧上限 **16 MiB**（16,777,216 字节），不支持 batch；头名大小写不敏感。
- 必须缓冲半帧，并连续拆出同一次读取中的多帧；不能用「一行 JSON」代替。
- 只需发送 Content-Length；当前读取器忽略其它头，并不校验 Content-Type。
- 请求携带 `id`、`method`、`params`，响应回显 `id` 并提供 `result` 或 `error`；通知无 id、不回复。
  当前宿主发字符串 ID `h:1`，插件回调建议使用 `p:1`，避免两方向混淆。
- 畸形头、超限或残缺正文、JSON 解析失败导致连接失败。宿主与 SDK 的版本、UTF-8、ID 严格校验
  尚未完全一致；作者必须发送规范消息，不能依赖宽松解析行为。

## 4. 生命周期

```text
宿主                              插件
 |-- initialize (request) ---------->| {assignedId,protocolVersion,hostServices,limits,hostVersion}
 |<-- result -----------------------| {protocolVersion:1,descriptor,requiredHostServices}
 |-- initialized (notification) ---->| {generation:1}
 |-- source.* / host.* 双向调用 ... |
 |-- shutdown (notification) ------->| 随后关闭 stdin
```

- 当前宿主声明 `protocolVersion:{min:1,max:1}`、`hostServices:["session","webview","log"]`、
  `limits:{sessionValueBytes:1048576,sessionTotalBytes:8388608,sessionKeys:64}`、`hostVersion:"1.0"`。
  limits 尚不包含帧大小、平台或资源校验模式；这里的 hostVersion 是实现中的固定值。
- 返回的 `protocolVersion` 必须为数字 1；`requiredHostServices` 必须是宿主提供服务的子集。
  不需要宿主服务就返回 `[]`，不要声明尚不可用的 `auth.present`。
- descriptor 的 `name` 非空，`id` 必须与 `assignedId`（即 `source.json` 的 `id`）一致；不一致宿主拒绝加载。
  **id 由插件作者维护**，探测与正式启用传入同一个值；宿主的安装目录、注册表键、登录会话命名空间都以它为准。
- 展示信息、能力和资源域均来自运行时 descriptor，不从交付清单读取。
- `initialized` 不携带写令牌；令牌随登录或重试保存参数传入。握手应立即响应，不启动交互登录。
- 当前导入流程先执行插件探测，再弹启用确认；不能声称插件在确认前绝不会运行。
- 当前只检查所需服务是否属于提供集合，尚未逐次按插件声明限制 host 调用；作者仍应只用已协商服务。

## 5. 业务方法

业务方法对应 Go 契约，使用 `source.*` 前缀；参数与返回值的当前 JSON 形状见 §8：

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
source.manage.reportHistory   {track,listenedSeconds} → {}
source.manage.updatePlaylistDescription {ref,description} → {}
source.manage.deletePlaylist  {ref} → {}
```

- v1 的 `catalog.search` 只返回歌曲，没有 `kinds`，不分实体类型；多实体搜索是后续版本。
- `collection.kind` 取 `favorites` 或 `playlist`；后者必须带 `collection.id` 与
  `collection.playlistKind`（`owned|favorite|chart`）。枚举只此一套，不得另造 `collected`。
- `import` 的 `runId` 是宿主生成的关联 ID，插件不决定提交；当前 ImportPage 没有回显 runId 字段。
- `manage.*` 为现有兼容接口，不用于本地收藏或歌单写入。当前 shell 不再调用历史上报；
  新插件不得据此自动向平台写入，仍须遵守项目本地写入边界与用户授权。
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
host.session.list   {} → {keys[]}
```

- 命名空间由当前插件身份决定，不能在参数中选择其它源。值是插件管理的不透明字节，用 base64 表示。
- get/list 不需要令牌；set/delete 使用宿主传下的 `writeToken`，插件没有申请入口。
- set：首次写 `ifRevision:0`，覆盖用 get 返回的 revision；成功递增 revision。
  delete：非零 ifRevision 才检查版本，省略或 0 不检查；不存在时成功。
- 值与 revision 保存到宿主加密命名空间，底层原子落盘；不承诺并发 set 的读改写事务隔离，插件应串行保存。
- set 对单 value 解码后强制上限 1 MiB。握手还宣告总量 8 MiB、64 key，当前 Host 没有逐项落实这两项配额检查；作者应主动遵守。
- 无效/过期令牌返回 `-32001`；revision 冲突返回 `-32008`，当前没有独立 conflict 码。
- 退出时由宿主清空会话命名空间，插件清除内存，不需要自行申请删除令牌。

**令牌当前限制：** Host 在 Login 前生成令牌并记 10 分钟有效期，尚未实现一次消费、attempt 结束撤销、
账号代次绑定。设计要求仍是仅用于本次调用，插件不得保留或复用；取消后必须停止写回。
PersistSession 适配器只转发调用方传入的令牌，并不自行签发，不能假定宿主的重试保存 UI 已完整落地。
会话加密的密钥同存本机，不保护同用户权限程序读取（见 §12）。

### 6.2 登录窗口

```text
host.webview.open {attemptId,url,allowedDomains[],callback:{scheme,host,path},requireSessionExport?}
  → {callbackUrl,cookies:[],sessionSnapshot:{},canceled:false}
host.webview.close {attemptId} → {}
```

- 登录逻辑由插件实现，macOS 的宿主窗口走 WKWebView，Windows 走 WebView2；不注册全局 scheme，不改系统代理。
- url 必须 HTTPS；导航域支持精确域名和 `*.example.org`。插件应列出认证跳转所需域，不随意扩大范围。
- callback 当前只检查提供的字段：scheme/host 忽略大小写，path 前缀匹配，至少一个字段非空。
  作者应尽量提供完整规则，并在收到 URL 后再次精确验证 state、PKCE、回调和一次消费。
- 回调后插件自行兑换平台凭据，宿主不解析令牌。用户取消等 Authorize 错误当前转为 `-32009`。
- **当前限制：** 不预置 Cookie，也不导出浏览器存储；cookies/sessionSnapshot 固定为空，
  `requireSessionExport:true` 返回 `-32004`。close 只回空对象，不实际关闭窗口；Host 未按 attemptId 追踪窗口授权。
  不要依赖宿主尚未实现的校验代替插件自身的登录状态机。

### 6.3 二维码展示（未实现）

设计方法为 `host.auth.present {attemptId,kind:"qr",payload,expiresAt}`，当前固定返回
`-32004`（unsupported），且不出现在 initialize.hostServices 中。不能把它当作可用二维码接口。
平台网页内的扫码可通过 host.webview.open，纯 HTTP 扫码所需的宿主二维码窗口尚未提供。

### 6.4 其它

```text
host.log       {level, message, fields?}        结构化日志，不含凭据/URL
$/progress     {requestId, phase, current, total, message}  进度通知
$/cancelRequest {id}                            取消在途请求
```

- 取消统一用 `$/cancelRequest`，进度统一用 `$/progress`；不另设 `host.cancel`/`host.progress`。
- v1 不提供 `host.http`：插件自己联网。宿主为播放器/封面加载资源属于特定业务通道。
- 通知只描述过程，不决定结果；页面响应与宿主提交才决定终态。
- 当前宿主接收 `$/progress` 后忽略，未展示进度；宿主收到插件方向的取消通知也尚未传播到 Host handler。
  宿主超时/取消会向插件发 `$/cancelRequest`，Go SDK 可取消对应业务 context。
- host.log 当前只使用 level/message，fields 尚未处理；不要发送凭据或临时 URL。

## 7. 双向调用与并发

最容易漏掉的一点：**宿主正在等待 `source.auth.login` 时，必须仍能处理插件发来的
`host.webview.open`。** 读循环不能同步执行耗时 handler。

- 读循环只负责解帧与分派；响应匹配、请求执行、写队列分别管理。
- 每个方向的调用表独立；等待对端响应时不得持 registry、会话或数据库锁。
- 写操作串行化到一个队列，避免并发写导致帧交错。
- 当前传输与 SDK 没有默认 4 个请求的限流队列；插件应自行控制并发和平台限流。

## 8. 线格式与持久化格式

当前搜索/详情复用 `music` 与 `source` 的 JSON tags，播放有专用 wire 结构。
尚未全面实施独立 DTO 或统一毫秒格式；第三方插件应按本节实际字段发送。
播放 URL 只能在线传给宿主，不写入本地库、队列或日志。

歌曲示例：

```json
{
  "source":"由 assignedId 决定", "id":"track-1", "title":"示例歌",
  "artists":[{"id":"artist-1","name":"示例艺人","coverUrl":"https://img.example.org/a.jpg"}],
  "album":{"id":"album-1","title":"示例专辑"},
  "coverUrl":"https://img.example.org/cover.jpg", "durationSeconds":183.5,
  "version":{"live":true,"edition":"现场版"}
}
```

- 时长字段为 **durationSeconds（秒，可小数）**，未知省略；没有 durationMs。
- version 支持 live/remix/acoustic/instrumental/cover/demo 布尔标记与 edition 字符串，没有 remaster 布尔字段。
- 歌曲 ID 为稳定字符串，source 为分配 ID；不要设置宿主队列字段 collected/onlySource。
- 搜索、平台历史与详情中的曲目 source 会被宿主重写为当前插件身份；导入只补空 source，插件仍应始终正确填值。
- descriptor.capabilities 当前字段为 `import/resolve/search/lyrics/playlist`（playlist 是单数）、
  `collections/maxPageSize/loginKinds/loginRequired`。尚没有独立的 albumDetail、artistDetail、history 或 qualities 能力位。
- Page 为 `{items,nextCursor,hasMore}`；有后续页必须给不同的非空 cursor，末页 cursor 为空或省略。
- Playlist 为 `{source,id,kind,name,description?,coverUrl?,trackCount?}`，不是 `{ref,...}`。
- EntityRef 为 `{source,kind,id,playlistKind?}`；playlistKind 是 owned/favorite/chart。
  详情返回 playlist/album/artist 元数据与 tracks，另含分页和可选 access、truncated，类型见 source/contract.go。

播放结果：

```json
{"url":"https://audio.example.org/current.mp3","quality":"standard","access":"unknown","durationSeconds":183.5}
```

可选 evidence、lyricUrl、lyricText。access 只识别 full/preview，其它或缺失均为 unknown。
当前不使用 expiresAt，不跨播放缓存 URL。

**导入实际格式：** `source.ImportPage` 尚无 JSON tags，SDK 直接序列化以下字段：

```json
{"Tracks":[],"Next":"","Partial":false,"RemoteDeleted":false}
```

Next 非空表示下一页，空表示遍历结束；Partial 表示不完整；RemoteDeleted 是布尔值，
只能在确认远端集合删除时置 true。当前不是设计稿的 `{tracks,nextCursor,hasMore,complete,remoteDeleted:string}`，
不能用该设计稿结构互操作。账号代次、暂存与发布仍由宿主管理。
当前 canonical 导入循环尚未据 Partial/RemoteDeleted 实现完整保护与删除处理；
插件遇到不完整分页必须返回错误，不能只置 Partial 后期待宿主阻止发布。

**歌词格式：** 返回行数组 `[{time,text,translation?,romanization?,words?}]`，`time` 为行开始秒数（浮点，相对音频开头）；
`words` 为可选的逐字数组 `[{time,text}]`，`time` 同样是绝对秒数而非行内偏移。这是宿主与所有音源共享的唯一种逐字格式，
插件自行解析平台文档（华为 `.hmrc`、TTML、增强 LRC 等）后必须转换到该结构，宿主不解析任何平台私有歌词格式。
没有歌词返回 `[]`，不要用错误表达"这首没词"。平台若是 TTML（Apple 风格）或其他私有格式，
由插件自行解析成上述结构；`docs/PLUGIN_GUIDE.md` §6.8.1 给出了 TTML 的元素对应、时间容器规则与示例，
宿主不解析任何平台格式。SDK 的 `source.lyrics.get` 已修正为从 `{ref:{...}}` 拆包（并兼容裸 TrackRef）；
`lyricText`/`lyricUrl` 是播放结果里的旧通道，保留兼容，但逐字只有走本方法才能被渲染。

## 9. 错误与超时

当前宿主按 error.code 分类，message 是安全短句；data.kind、retryAfterMs、diagnostic 即使传入也不参与分类/UI。
下表是 `plugin/source.go:classify` 的实际映射：

| code | kind | 含义 |
| --- | --- | --- |
| -32001 | login | 登录需要/失效；Host 也用于无效写令牌 |
| -32002 | notfound | 资源不存在 |
| -32003 | ratelimit | 限流 |
| -32004 | unsupported | 不支持 |
| -32005 | network | 网络错误 |
| -32006 | permission | 会员/地域/隐私权限 |
| -32007 | transient | 临时故障 |
| -32008 | protocol | 协议错误；Host 也用于 revision 冲突 |
| -32009 | canceled | 已取消 |
| -32010 | unavailable | 插件不可用 |

未知方法应回 -32601，参数错误回 -32602，内部 RPC 错误可用 -32603。
当前 Go SDK 对普通 Go error 默认回 -32010，不自动按 source.Error.Kind 映射；要保留编号需返回 wire.Error。
SDK 版本不兼容分支目前回 -32002（历史实现），不应据此修改业务映射表。

- 默认单次调用 **60 秒**，调用方更短截止时间优先；登录也没有自动延长到 5 分钟。
  UI 导入探测为 30 秒、启用为 25 秒。进度不能续期。
- 超时/取消发送 `$/cancelRequest {id}` 并移除等待项，当前不会立即杀挂起进程。
- 取消不保证平台请求已停止；插件应取消网络任务、丢弃旧登录结果并停止写回。
- 禁止把平台原始响应、凭据、URL 或堆栈放到 message 或日志。

## 10. 清单（source.json）与能力发现

第三方交付清单只描述「怎么启动」，能力与展示信息由插件在 `initialize` 时自己上报，
宿主缓存到插件目录内的 `descriptor.json`（宿主写入，插件不得覆盖）：

```json
{
  "schemaVersion": 1,
  "entry": {
    "darwin-arm64": "bin/netease",
    "windows-x64": "bin/netease.exe",
    "any": { "command": "node", "args": ["main.mjs"], "env": { "NODE_ENV": "production" } }
  }
}
```

- `entry` 同时接受两种写法：
  - 字符串：插件目录内的可执行文件（相对路径，不得逃逸目录；必须有执行权限）。
  - 对象：`{command,args,env}`。这是启动参数写法，不要求实现 MCP 协议。**当前 args 非空才走解释器模式**：
    command 为 PATH 名称或绝对路径；带本机路径分隔符的相对 command 则在插件目录内解析。
    相对 args 若在插件目录内存在则解析为绝对路径；绝对 args 与任何含 `..` 的 args 被拒绝。
    args 为空的对象仍按目录内二进制处理，与字符串入口一样校验路径和执行权限。
- 平台键优先取宿主平台（如 `darwin-arm64`），缺失时回退 `any`。
- 仓库自带一个最小 Node 示例：`native/internal/plugin/testdata/node-source-plugin/`（`main.mjs` + `source.json`），
  演示纯脚本插件如何完成 `initialize` 握手与 `source.catalog.search`；导入后搜索「脚本」即可验证。
- 运行时由插件作者和用户负责：宿主不内置 Node/Python，也不做 shebang 探测。
  启用路径的启动错误包含 command，握手失败可能附带 stderr 首行；导入探测错误目前不保证附带 stderr。

- 官方插件在清单里固定身份：`"id": "huawei"`、`"builtin": true`，以 Release 资产（zip）分发，
  宿主不再把它打进安装包或应用包——新装机器默认没有任何音源，由用户自己添加。
  当前持久化只保存启用 ID 集合，官方源的显式停用尚不能可靠跨重启保留。
- **身份由插件作者维护**：清单必须带稳定的 `id`（`[a-z0-9_-]{1,40}`），一经发布不要更改。
  `系统设置 → 音源 → 导入插件` 打开系统目录选择器（不手填路径），宿主按清单 `id` 启动一次做探测，
  把目录复制到 `<数据目录>/sources/<id>/` 并写入 `descriptor.json`。
- **同一 id 再次导入就是更新**：停掉正在运行的进程、替换文件、保留启用状态并自动重启，不会出现第二份副本，
  也不需要重新授权；可选 `version` 字段只用于显示与更新提示，不参与身份判断。
- **探测与正式启用使用同一个 id**：`initialize.assignedId` 等于清单 `id`。SDK 用它覆盖
  握手响应里的 `Descriptor.id`；作者应保证后续数据（Track/Playlist 的 `source`）也使用该身份，
  当前宿主会覆盖目录查询结果里的 source（见 §8）。
- 缺失或非法 `id` 的清单会被拒绝导入；`descriptor.id` 与清单 `id` 不一致也会拒绝。
- 运行时 `Descriptor` 是唯一的展示与能力来源：`name` / `shortName` / `icon` / `accent` /
  `capabilities` / `resourceDomains` 全部以插件上报为准，宿主只要求 `id` 与分配值一致、
  `name` 非空；`hostServices` 需求必须是 initialize 所提供服务的子集。
- `descriptor.json` 是宿主缓存：插件停用时设置页仍能显示名称、图标与开关；
  作者不应读取或修改它；这是一项文件归属约定，当前同用户进程并没有文件访问隔离。
  导入时生成缓存，不承诺每次启用都刷新缓存。

- descriptor.icon 当前结构为 `{path}`；第三方图标加载与安全净化尚未完成验收，当前 UI 主要使用 shortName 角标，
  不承诺 SVG/PNG 图标显示。先提供简短 shortName。
- `resourceDomains` 是宿主媒体请求的域名策略（逐次校验重定向），不约束插件自身联网。
- 媒体域当前按子域后缀匹配：`.media.example.org` 允许 `audio.media.example.org`，不允许裸域；不用 `*.` 语法。
  URL 必须 HTTPS、无用户信息、默认端口或 443。
- `id` 是作者声明的身份，不是签名或信任证明；宿主不去重校验内容，也不承诺安装后防篡改。
- 安装复制最多 256 MiB 普通文件，忽略符号链接等非普通文件，以及 descriptor.json、.DS_Store；
  新包必须把所需脚本和资源放在目录中，不能依赖目录外链接。

## 11. 安装、授权与升级

- 用户目录为应用数据目录下 `sources/<id>/`；不修改签名 app bundle，不依赖 Program Files 写权限。
- UI「设置 → 音源 → 添加音源」接受 `.zip` 包（自动解压、拒绝绝对路径/`..`/符号链接，上限 4096 个文件与
  256 MiB）；「从文件夹添加…」保留给本地开发。两种方式都先探测（启动一次读 descriptor）再复制。
- 首次启用弹出一次授权：显示插件名、安装路径与「以你的用户权限运行」提示，确认后记为已授权；
  之后启动、开关或重新启用不再重复提示。新添加的插件仍会弹一次。
- 再次添加同一个 `id` 即原地更新：停旧进程、替换文件、保留启用状态与一次性授权并自动重启；已有会话与本地曲库绑定保留。
- 停用结束进程、注销 registry，保留本地曲库；修改原始交付目录不影响已安装副本。
- **未实现：** 升级重新授权、签名校验、市场、下载、自动更新、热重载、多实例配置与完整卸载接口。
  不承诺卸载时自动清理会话；清理需要独立设计和验收。

## 12. 安全边界说明

- 会话加密避免文件直接保存明文，不解决「同用户恶意程序」问题：
  密钥与密文同处本地文件，未沙箱化进程仍可读文件、联网。
- RPC 的 namespace 只防止插件经宿主 API 串读其它插件的会话。
- 插件 `id` 由作者声明，不证明作者可信，也不提供安装后完整性校验；同 `id` 的不同内容按更新处理。
- 官方插件（`builtin: true`）随应用分发，由 CI 按平台编译；第三方插件以源码/脚本打包发布，运行时由用户提供。
- 生命周期管理能回收正常退出的插件进程，不应声称能约束主动逃逸的恶意进程。
- 这些限制必须原样写进用户可读的插件说明，不夸大隔离能力。

## 13. 版本与兼容

- `schemaVersion` 描述 manifest 结构；initialize 的 `protocolVersion.min/max` 描述线协议，清单没有 protocol 字段。
- 主版本不兼容拒绝启用；次版本只允许可选字段扩展与新增宿主服务的协商启用。
- 不得用「猜一个旧版本」的方式降级；取交集失败就明确拒绝并给出版本信息。

## 14. 验证矩阵

以下是验收目标，包含尚未实现的升级授权与进程树管理，不代表这些项目已经通过。

| 场景 | 进程内源 | 外置插件 |
| --- | --- | --- |
| 分页不变量（cursor 推进、末页空、不完整时失败） | 必测 | 必测 |
| 双向重入（等登录时回调 webview） | 必测 | 必测 |
| 取消/超时/挂起 | 必测 | 必测 |
| 崩溃与进程树回收 | — | 必测 |
| 账号切号与代次失效 | 必测 | 必测 |
| 异常分页、超限帧、中文路径 | 必测 | 必测 |
| 升级重新授权 | — | 必测 |

- 合成源用于已实现的协议通道验证；二维码展示仍不可用；**不能把合成源结果记为真实网易云登录通过**。
- 真实华为 WebView 登录与真实扫码分别留证；Windows 实机验收独立记录。

## 15. 示例（节选）

```text
→ {"jsonrpc":"2.0","id":"h:1","method":"initialize","params":{"protocolVersion":{"min":1,"max":1},"assignedId":"example-1234","hostServices":["session","webview","log"],"limits":{"sessionValueBytes":1048576,"sessionTotalBytes":8388608,"sessionKeys":64},"hostVersion":"1.0"}}
← {"jsonrpc":"2.0","id":"h:1","result":{"protocolVersion":1,"descriptor":{...},"requiredHostServices":["session","webview","log"]}}
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

## 16. 实现与历史验证

下列验证为历史记录，本次文档核对没有重新执行真实账号操作；当前行为与限制以 §2–§13 为准。

已落地于 `native/internal/plugin/`：

- `manifest.go`：`source.json` 读取与校验（schemaVersion、可选 id、entry 路径与参数），
  `PlatformKey()` 把 `darwin-arm64`/`windows-x64` 映射到当前平台。
- `transport.go`：Content-Length 帧、请求 ID 关联、宿主方法在独立 goroutine 中处理以支持重入、
  `$/cancelRequest`、stderr 环形缓冲、单帧 16 MiB 上限、默认 60 秒调用超时。
- `source.go`：插件侧 `source.Source` 适配器，`acceptDescriptor` 校验分配 ID 与非空名称，能力来自握手，
  错误码映射到契约的 `source.Kind*`，写令牌由适配器在 `Login` 前向宿主申请并随 `source.auth.login` 下传。
- `host.go`：`host.session.*`（加密命名空间、revision、写令牌）、`host.webview.open`、`host.log`。
- `manager.go`：扫描 `<数据目录>/sources/<id>/source.json`，启用/停用与进程生命周期，
  `LoadEnabled` 在启动时恢复上次启用集合，启用状态存在 `storage` 的 `plugin.enabled`。
- Shell：设置 → 音源 → 「外置音源」列出插件；首次启用弹出授权对话框（名称、目录、权限说明），
  启用后进入同一 `Registry`，搜索页签、导入对话框与设置列表都自动出现该源。

### 16.1 华为源作为预置外置插件（2026-11-03 晚）

- `native/cmd/huawei-source` 是随应用分发的插件进程：`native/internal/plugin/sdk`（插件侧协议）
  与 `native/internal/plugin/wire`（帧与 JSON-RPC 基础）用于仓库内插件；外部模块受 Go internal 规则限制，尚无公开 SDK；
  `plugins/huawei/source.json` 声明 `builtin: true`、按平台的入口；能力与资源域来自握手，预置 descriptor.json 用于停用展示。
- `tinglan-tool plugins` 把每个官方插件编译成对应平台的 zip（`release/plugins/<id>-<version>-<platform>.zip`），
  随 Release 一起发布；`tinglan-tool mac-app` 只构建宿主，不复制任何插件，
  用户通过「添加音源」安装到 `<数据目录>/sources/<id>/`；跨重启停用状态的限制见 §10。
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
- 多源登录（2026-11-04）：宿主按音源分别缓存 `Auth().Status`，登录入口只出现在
  已启用且 descriptor 声明 `loginKinds` 的音源下；主账号会话仍只保存一个，其它音源的
  会话由插件自己的 `host.session` 命名空间保存。示例插件演示了这一路径。
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
