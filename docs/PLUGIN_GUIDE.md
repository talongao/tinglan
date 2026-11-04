# 听澜音源插件开发指南

这份文档写给想给听澜写音源插件的人：从最小可运行的插件开始，逐项说明清单、
协议、宿主服务、错误处理和调试方式。协议细节以
[SOURCE_PLUGIN_PROTOCOL.md](SOURCE_PLUGIN_PROTOCOL.md) 为准，本文负责「怎么上手」。

## 0. 五分钟跑通

需要的环境：听澜桌面版、Node（或 Go / Python / 任意能读写 stdio 的运行时）。

```text
my-source/
├── source.json     # 清单：告诉宿主怎么启动插件
└── main.mjs        # 插件实现
```

`source.json`：

```json
{
  "schemaVersion": 1,
  "entry": {
    "any": { "command": "node", "args": ["main.mjs"] }
  }
}
```

启动听澜 → 设置 → 音源 → 导入插件 → 选择 `my-source` 目录 → 授权启用。
启用成功后会出现在设置页的音源列表里，搜索页出现对应页签。

仓库里有一个可以直接用的完整示例：
[`native/internal/plugin/testdata/node-source-plugin/`](../native/internal/plugin/testdata/node-source-plugin/)。

## 1. 插件是什么

听澜把「音源」抽象成 `Source` 契约（见 [SOURCE_CONTRACT.md](SOURCE_CONTRACT.md)）：
搜索、歌单、专辑、歌手、历史、导入收藏、播放解析、歌词、写操作。

内置的华为音乐也是一个插件，只是它随应用分发（`builtin: true`）。第三方音源
完全走同一套协议，进程内不加载你的代码，只通过 stdio 上的 JSON-RPC 通信。

几条硬性边界：

- 宿主不做音源切换的「绕过」：插件返回什么地址就播放什么地址，会员/地域限制由
  各平台自己判定。
- 插件以你的用户权限运行，没有沙箱。启用前的确认框会显示插件名、路径和说明。
- 宿主不内置 Node / Python，也不做 shebang 探测；运行时环境由插件作者和用户负责。
  MCP 生态里 server 写着 `node xxx` / `uv run xxx`，这里也是同样的约定。

## 2. 清单 `source.json`

```json
{
  "schemaVersion": 1,
  "entry": {
    "darwin-arm64": "bin/my-source",
    "windows-x64": { "command": "node", "args": ["main.mjs"], "env": { "NODE_ENV": "production" } },
    "any": "bin/my-source"
  }
}
```

| 字段 | 必填 | 说明 |
| --- | --- | --- |
| `schemaVersion` | 是 | 当前只接受 `1` |
| `entry` | 是 | 平台键 → 启动方式；平台键缺失时回退 `any` |
| `id` | 否 | 仅内置插件写死；导入的插件由宿主分配，不要自己写 |
| `builtin` | 否 | 仅随应用分发的插件为 `true` |
| `digest` | 否 | 宿主安装后写入，用于重复导入检测，插件不要手写 |

平台键：`darwin-arm64`、`darwin-x64`、`windows-x64`、`linux-x64`（`amd64` 归一为 `x64`）。

启动方式两种：

- **字符串**：插件目录内的可执行文件，必须是相对路径、不能逃逸目录、必须有执行位。
- **对象（MCP 式）**：`command` 按 PATH 或绝对路径解析，`args` / `env` 原样传入。
  相对 `args` 如果在插件目录内存在会转成绝对路径；绝对 `args` 和含 `..` 的 `args` 会被拒绝。

导入时宿主会启动一次插件读取它的自述（describe），成功后把清单里的 `id` 改写成
分配的身份（例如 `node-3df1`），同时缓存一份 `descriptor.json` 供停用状态下展示。

## 3. 通信：stdio + Content-Length + JSON-RPC 2.0

宿主和插件之间只有 stdin/stdout 两条管道。每帧格式与 LSP 相同：

```text
Content-Length: 123\r\n
\r\n
{"jsonrpc":"2.0","id":"h:1","method":"initialize","params":{...}}
```

- 单帧上限 16 MiB，超限直接断连。
- 单次调用默认 60 秒超时；超时宿主会发 `$/cancelRequest`，插件应尽快结束。
- stdout 只能写协议帧；日志写 stderr，宿主会保留最后一段用于报错。
- 请求 ID 建议宿主用 `h:*`、插件用 `p:*`，方便读日志。

Node 侧最小读帧实现：

```js
let buffer = Buffer.alloc(0);
process.stdin.on("data", (chunk) => {
  buffer = Buffer.concat([buffer, chunk]);
  for (;;) {
    const head = buffer.indexOf("\r\n\r\n");
    if (head < 0) return;
    const match = /content-length:\s*(\d+)/i.exec(buffer.subarray(0, head).toString());
    const start = head + 4;
    if (buffer.length < start + Number(match[1])) return;
    const body = buffer.subarray(start, start + Number(match[1])).toString();
    buffer = buffer.subarray(start + Number(match[1]));
    handle(JSON.parse(body));
  }
});
function send(payload) {
  const body = JSON.stringify(payload);
  process.stdout.write(`Content-Length: ${Buffer.byteLength(body)}\r\n\r\n${body}`);
}
```

## 4. 生命周期

```text
宿主 spawn 插件
   → initialize     宿主说明协议版本、宿主服务、分配给你的 id
   ← 插件回 descriptor + 必需宿主服务
   → initialized    通知（无参数）
   → source.* 调用
   → shutdown       插件回 {} 后退出
```

`initialize` 的请求：

```json
{
  "protocolVersion": { "min": 1, "max": 1 },
  "assignedId": "node-3df1",
  "hostServices": ["session", "webview", "log"],
  "limits": { "sessionValueBytes": 1048576, "sessionTotalBytes": 8388608, "sessionKeys": 64 },
  "hostVersion": "1.0"
}
```

`initialize` 的响应：

```json
{
  "protocolVersion": 1,
  "descriptor": {
    "schemaVersion": 1,
    "id": "node-3df1",
    "name": "我的音源",
    "shortName": "我",
    "accent": "#3C873A",
    "capabilities": { "search": true, "playlist": true, "import": true, "resolve": true, "lyrics": false }
  },
  "requiredHostServices": []
}
```

- `id` 必须是宿主给的那个（`assignedId`）；自报不一致会被拒绝启用。
- `name` 必填；`shortName` 取 1–2 个字符，用于列表徽标；`accent` 是十六进制颜色。
- `requiredHostServices` 只能要求宿主在 `hostServices` 里声明过的服务。

## 5. 方法

必须实现三个：`source.describe`、`source.auth.status`、`source.catalog.search`。
其余按 `capabilities` 里声明为 `true` 的实现；没声明的可以返回 `-32601`。

```text
source.describe               → Descriptor
source.auth.status            → AuthStatus
source.auth.verify            → AuthStatus            （联网校验，可返回 unknown）
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
source.manage.reportHistory   {track, listenedSeconds} → {}
source.manage.updatePlaylistDescription {ref, description} → {}
source.manage.deletePlaylist  {ref} → {}
```

分页统一用不透明游标：

```json
{ "items": [ ... ], "nextCursor": "20", "hasMore": true }
```

游标由插件自己定义（`"20"`、base64、时间戳都行），宿主只负责原样回传。
最后一页必须 `hasMore: false`，导入流程据此判断「跑完了」，否则不会提交。

### 5.1 最小实现（Node）

```js
function handle(msg) {
  if (msg.method === undefined) return; // 宿主的响应，忽略
  const id = msg.id;
  switch (msg.method) {
    case "initialize": {
      const assigned = msg.params?.assignedId;
      reply(id, {
        protocolVersion: 1,
        descriptor: { schemaVersion: 1, id: assigned, name: "我的音源", shortName: "我",
          capabilities: { search: true } },
        requiredHostServices: [],
      });
      return;
    }
    case "initialized":
      return;
    case "source.auth.status":
      reply(id, { state: "signedOut" });
      return;
    case "source.catalog.search": {
      const q = String(msg.params?.query ?? "");
      reply(id, {
        items: [{ id: "1", title: `你搜了：${q}`, artists: [{ id: "a", name: "歌手" }] }],
        hasMore: false,
      });
      return;
    }
    case "shutdown":
      reply(id, {});
      setTimeout(() => process.exit(0), 10);
      return;
    default:
      send({ jsonrpc: "2.0", id, error: { code: -32601, message: "unsupported" } });
  }
}
```

## 6. 数据格式

### Track

```json
{
  "source": "node-3df1",
  "id": "platform-track-id",
  "title": "歌名",
  "artists": [{ "id": "artist-id", "name": "歌手" }],
  "album": { "id": "album-id", "title": "专辑" },
  "coverUrl": "https://...",
  "durationSeconds": 240
}
```

- `source` 由宿主覆盖为你被分配的 id，插件写了也会被替换。
- `rawSource`（适配器原始负载）不会跨进程传输，不要依赖它。
- 不要设置 `onlySource` / `collected`，那是宿主队列状态。

### Playlist / Album / Artist

```json
{ "source": "node-3df1", "id": "playlist-id", "kind": "owned", "name": "我的歌单", "trackCount": 20 }
```

`kind` 取 `owned` / `favorite` / `chart`（歌单）或 `local` / `imported`（本机视角，插件不要用）。

### Playback

```json
{
  "url": "https://...",
  "quality": "lossless",
  "access": "full",
  "durationMs": 240000,
  "expiresAt": "2026-11-04T10:00:00Z"
}
```

`access` 取 `full` / `preview` / `unknown`。播放地址只在内存里传递，不落盘。

## 7. 宿主服务

插件可以回调宿主，宿主按当前插件身份做权限隔离：

```text
host.session.get    {key} → {value(base64), revision, exists}
host.session.set    {key, value(base64), ifRevision, writeToken} → {revision}
host.session.delete {key, ifRevision, writeToken} → {}
host.session.list   → {keys[]}
host.webview.open   {attemptId, url, allowedDomains, callbackScheme, callbackHost, callbackPath, requireSessionExport} → {callbackUrl, cookies, canceled}
host.auth.present   {attemptId, kind, payload, expiresAt} → {}   // 当前返回 -32004
host.log            {level, message}
```

会话存储要点：

- 值是任意字节，JSON 里用 base64 传；单值 ≤1 MiB、每插件 ≤8 MiB、最多 64 个 key。
- `get/list` 不需要令牌；`set/delete` 必须带宿主随登录调用下发的 `writeToken`，
  插件没有申请令牌的入口。
- 退出登录由宿主清空整个命名空间，插件不需要自己删。
- 数据随插件身份隔离，卸载插件会删除。

登录窗口：插件调 `host.webview.open` 后，宿主用内嵌 WKWebView/WebView2 打开授权页，
拦截指定的回调 scheme，把回调 URL 和 Cookie 返回给插件；插件自己完成 code→token 交换。
插件不应把登录逻辑交给宿主。

二维码：纯 HTTP 扫码登录的源需要宿主渲染二维码，对应 `host.auth.present`，
这一项尚未实现（返回 `-32004`），所以目前脚本插件请用 WebView 登录。

## 8. 错误码

插件用 JSON-RPC `error` 返回业务错误，宿主据此决定 UI：

| 场景 | code | 宿主表现 |
| --- | --- | --- |
| 需要登录 | -32001 | 提示去登录 |
| 无权限 | -32002 | 说明原因，不重试 |
| 不存在 | -32003 | 返回上一级 |
| 限流 | -32004 | 显示重试时间 |
| 不支持 | -32005 | 隐藏入口 |
| 已取消 | -32006 | 不提示 |
| 写冲突 | -32007 | 刷新后重试 |
| 协议错误 | -32008 | 记日志 |
| 临时故障 | -32009 | 允许重试 |
| 不可用 | -32010 | 提示稍后再试 |
| 方法不存在 | -32601 | 视为未实现 |

错误信息会展示给用户，写清楚原因，不要只写 `failed`。

## 9. 安装、升级与卸载

- **安装**：设置 → 音源 → 导入插件 → 系统目录选择器。
- **重复导入**：宿主对插件目录做摘要比对，同一份插件再次导入会提示「已经导入过了」并复用原条目。
- **启用**：需要用户在确认框里授权；内置插件默认启用，用户可手动停用。
- **升级**：先用「停用」，把新版本文件覆盖进 `<用户数据目录>/sources/<id>/`，再启用。
  覆盖后 `source.json` 里的 `id`/`digest` 由宿主维护，保持原值即可。
- **卸载**：停用后删除 `<用户数据目录>/sources/<id>/` 目录，同时会清掉该插件的会话数据。

macOS 用户数据目录：`~/Library/Application Support/tinglan/native-v1/`。
Windows：`%APPDATA%\tinglan\native-v1\`。

## 10. 调试

- 插件日志写 stderr；启动失败时宿主会把 `command` 和 stderr 末尾片段带进错误信息。
- 手动跑协议，不必打开应用：

  ```bash
  body='{"jsonrpc":"2.0","id":"1","method":"source.auth.status","params":{}}'
  printf 'Content-Length: %d\r\n\r\n%s' "${#body}" "$body" | node main.mjs
  ```

- 用 `TINGLAN_PLUGIN_ROOT=<你的插件父目录>` 启动应用或 `go test`，可以额外挂一个插件根目录，
  不污染用户数据目录。
- 仓库自带 Go 契约测试可参考：`native/internal/plugin/huawei_plugin_test.go`、
  `native/internal/plugin/real_sample_test.go`（需 `TINGLAN_PLUGIN_ROOT`）。

## 11. 安全与信任

- 插件是**第三方程序**，以你的用户权限运行，能读写你的网络与文件。宿主不做沙箱隔离，
  只在启用前显示插件名、路径和授权说明。
- 宿主对插件自报的 `id` 不信任：安装时分配身份，握手时校验一致，返回的曲目也会被
  强制改写成宿主身份。
- 插件不能访问其它插件的会话命名空间，也不能读取宿主里的本地曲库。
- 不要记录用户凭据到 stderr 或磁盘；凭据放 `host.session`，由宿主加密保存。
- 发布插件前请自测：登录、退出、重启后恢复、限流、断网、空结果、超长标题。

## 12. 常见问题

**`启动插件失败（node）：exec: "node": executable file not found in $PATH`**
宿主进程的 PATH 里没有 node。macOS 上从 Finder 启动的应用 PATH 可能与终端不同，
写绝对路径（`/opt/homebrew/bin/node`）或在 `args` 里显式给解释器路径。

**`unsupported protocol 0`**
`initialize` 响应不是结构化格式，检查是否返回了 `protocolVersion` 和 `descriptor`。

**插件出现在设置页，但搜索页没有页签**
只有 `sourceEnabled` 为真的源才会出现在搜索页签；先确认已启用，再看
`capabilities.search` 是否为 `true`。

**搜索结果没有来源徽标 / 徽标显示问号**
`descriptor.shortName` 为空。给它 1–2 个字符。

**导入收藏卡在「正在读取」**
分页最后一页没有返回 `hasMore: false`，宿主会一直认为还有下一页。

**Windows 上脚本插件起不来**
直接用 `.mjs`/`.py` 不行，Windows 没有 shebang：写
`{"command":"node","args":["main.mjs"]}` 或 `{"command":"cmd","args":["/c","main.bat"]}`。

## 13. 参考实现

- Go SDK：`native/internal/plugin/sdk/`，内置华为插件 `native/cmd/huawei-source/`
  用它实现了完整能力（登录、搜索、歌单、导入、解析、写操作）。
- Node 最小实现：`native/internal/plugin/testdata/node-source-plugin/`。
- 完整协议：[SOURCE_PLUGIN_PROTOCOL.md](SOURCE_PLUGIN_PROTOCOL.md)。
- 契约定义：[SOURCE_CONTRACT.md](SOURCE_CONTRACT.md)。
