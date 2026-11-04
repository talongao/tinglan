# 音源插件接口规范

本文定义「宿主（听澜）」与「外置音源插件」之间的调用约定：宿主在什么时候、用什么参数调用插件；插件必须实现哪些方法、返回什么；每个字段是什么含义。

它是一份**接口规范**，不绑定语言。任何能读写标准输入输出、能解析 JSON 的程序都可以做插件——Node、Python、Go、Rust、C++ 都行。

- 写插件：读第 4~7 节，按第 10 节的清单逐项实现。
- 核对宿主行为：第 2、3、8 节描述的是当前实现。
- 自测：第 9 节有一个与语言无关的检查脚本。

可运行的示例：[`native/internal/plugin/testdata/node-source-plugin/`](../native/internal/plugin/testdata/node-source-plugin/)（Node，约 120 行）与 `native/cmd/huawei-source/`（Go，内置华为源）。

---

## 1. 总体结构

```text
         ┌──────────────────────────── 听澜宿主进程 ────────────────────────────┐
         │  UI（搜索 / 歌单 / 详情 / 播放 / 设置）                                │
         │     │ 调用 source.Source                                        │
         │     ▼                                                             │
         │  plugin.Manager ──启动/停止──▶ 子进程 A（插件）                       │
         │       │                        stdin  ◀── JSON-RPC ──▶  stdout      │
         │       └──────────────────────▶ 子进程 B（插件）                       │
         │  宿主服务：会话存储（加密落盘）、WebView 登录窗口、日志                   │
         └───────────────────────────────────────────────────────────────────┘
```

三个要点：

1. **一个插件 = 一个子进程。** 宿主启动它，之后只通过 stdin/stdout 上的 JSON-RPC 2.0 通信。
2. **能力由插件自述。** 握手时返回 descriptor，告诉宿主自己支持搜索、导入、播放解析等；宿主据此显示入口，不支持的入口不出现。
3. **调用是双向的。** 宿主调用 `source.*`，插件可以反向调用 `host.*`（弹登录窗口、读写会话、写日志）。

宿主内部用 Go 接口 `source.Source` 表达这些能力；插件协议就是它的 JSON 投影。

---

## 2. 进程与生命周期

### 2.1 清单与启动

宿主读取 `<插件目录>/source.json`：

```json
{
  "schemaVersion": 1,
  "entry": {
    "any": { "command": "node", "args": ["main.mjs"] }
  }
}
```

`entry` 的每一项支持两种形式：

| 形式 | 含义 |
| --- | --- |
| `"bin/plugin"` | 插件目录内的可执行文件，必须有执行权限 |
| `{ "command", "args", "env" }` | `command` 走 PATH 或绝对路径；`args` 中的相对路径若在插件目录内会转成绝对路径 |

平台键优先、`any` 兜底：`darwin-arm64`、`darwin-x64`、`windows-x64`、`linux-x64`。宿主**只选一个**，不会在失败后替换。

启动参数：工作目录 = 插件目录；环境变量继承宿主环境再叠加 `env`；不经 shell，不做变量展开；stdin/stdout 专用于协议；stderr 由宿主保留末尾若干 KB 用于诊断。

### 2.2 生命周期

```text
宿主                                     插件
 │  spawn(command, args)                  │
 │ ────────────────────────────────────▶ │
 │  initialize（请求）                     │
 │ ────────────────────────────────────▶ │  解析参数、返回 descriptor
 │ ◀──────────────────────────────────── │
 │  initialized（通知）                    │
 │ ────────────────────────────────────▶ │  进入服务状态
 │  source.*  /  host.*  双向调用          │
 │ ◀───────────────────────────────────▶ │
 │  shutdown（通知）                       │
 │ ────────────────────────────────────▶ │  退出
 │  关 stdin → 等 2 秒 → 必要时 kill        │
```

- 收到 `shutdown` 后应尽快退出；通知不需要响应。
- 时间预算：单次调用 **60 秒**；导入探测 30 秒、启用 25 秒、启动恢复 20 秒、读取登录态 10 秒。超时的调用被取消。
- 插件崩溃不会自动重启；宿主标记为故障，由用户停用或重新启用。
- **没有沙箱**：插件以当前用户权限运行。

### 2.3 一次典型调用

```text
宿主 ─ initialize ──────────────────────────▶ 插件
宿主 ◀ descriptor / protocolVersion ────────── 插件
宿主 ─ initialized（通知）───────────────────▶ 插件
宿主 ─ source.catalog.search ────────────────▶ 插件
宿主 ◀ Page<Track> ────────────────────────── 插件
宿主 ─ source.library.resolve ───────────────▶ 插件
宿主 ◀ Playback{url, access} ──────────────── 插件
```

---

## 3. 传输格式

### 3.1 帧

每条消息一帧：`Content-Length` 头 + 空行 + UTF-8 JSON 正文。

```text
Content-Length: 137\r\n
\r\n
{"jsonrpc":"2.0","id":"h:1","method":"initialize","params":{}}
```

- 长度是**正文字节数**，不是字符数。
- 可能一次读到多帧，也可能只读到半帧；必须缓冲并按长度切分。
- 单帧上限 **16 MiB**；超过视为协议错误。
- 不支持 batch。
- 头名大小写不敏感。

### 3.2 JSON-RPC 2.0

请求：

```json
{"jsonrpc":"2.0","id":"h:12","method":"source.catalog.search","params":{"query":"晚风","limit":20}}
```

成功：

```json
{"jsonrpc":"2.0","id":"h:12","result":{"items":[],"nextCursor":"","hasMore":false}}
```

失败：

```json
{"jsonrpc":"2.0","id":"h:12","error":{"code":-32001,"message":"需要登录"}}
```

通知（无 `id`，不回复）：

```json
{"jsonrpc":"2.0","method":"initialized","params":{"generation":1}}
```

约定：

- 宿主请求 id 形如 `h:1`；插件反向调用建议用 `p:1`，避免撞号。
- 回复可以不按请求顺序。
- 未知 `method` 返回 `-32601`。
- 无法解析的帧视为连接损坏，插件会被停用。

### 3.3 并发、取消、超时

- 宿主可能并行发多个请求；插件必须并发处理或至少保证每个请求最终有响应。
- 插件处理中可反向调用宿主；宿主读循环独立，不会因此卡住。
- 取消：宿主发 `$/cancelRequest`，`params` 为 `{"id":"h:12"}`。插件应尽快放弃，可不回复。
- 超时后宿主按失败处理，插件可跑完但结果不会被采用。

---

## 4. 握手（initialize）

### 4.1 请求

```json
{
  "jsonrpc": "2.0",
  "id": "h:1",
  "method": "initialize",
  "params": {
    "assignedId": "huawei",
    "protocolVersion": { "min": 1, "max": 1 },
    "hostServices": ["session", "webview", "log"],
    "limits": {
      "sessionValueBytes": 1048576,
      "sessionTotalBytes": 8388608,
      "sessionKeys": 64
    },
    "hostVersion": "1.0"
  }
}
```

| 字段 | 含义 |
| --- | --- |
| `assignedId` | 宿主确认的插件身份。导入探测时为空串；正式启用时是最终 ID |
| `protocolVersion` | 宿主支持的主版本范围 |
| `hostServices` | 宿主提供的服务组 |
| `limits` | 会话存储限制（单值 / 总量 / 键数） |
| `hostVersion` | 仅用于日志，不参与协商 |

### 4.2 响应

```json
{
  "jsonrpc": "2.0",
  "id": "h:1",
  "result": {
    "protocolVersion": 1,
    "descriptor": {
      "schemaVersion": 1,
      "id": "huawei",
      "name": "华为音乐",
      "shortName": "华",
      "accent": "#C7000B",
      "capabilities": {
        "search": true,
        "playlist": true,
        "import": true,
        "resolve": true,
        "lyrics": true,
        "collections": [{ "kind": "favorites" }, { "kind": "playlist" }],
        "maxPageSize": 50,
        "loginKinds": ["webview"]
      },
      "resourceDomains": [".dbankcdn.cn", ".tingmall.com"]
    },
    "requiredHostServices": ["session", "webview"]
  }
}
```

| 字段 | 必须 | 含义 |
| --- | --- | --- |
| `protocolVersion` | 是 | 必须为 `1` |
| `descriptor.id` | 是 | 正式启用时必须等于 `assignedId`；探测时返回稳定的非空 id |
| `descriptor.name` | 是 | 展示名 |
| `descriptor.shortName` | 否 | 徽标 1~2 字，空则取 `name` 前两字 |
| `descriptor.accent` | 否 | `#RRGGBB` |
| `descriptor.capabilities` | 否 | 见下表 |
| `descriptor.resourceDomains` | 否 | 允许加载封面/媒体的域名后缀 |
| `requiredHostServices` | 否 | 必须是 `hostServices` 的子集，否则拒绝启用 |

`capabilities`：

| 字段 | 类型 | 含义 |
| --- | --- | --- |
| `search` | bool | 支持搜索 |
| `playlist` | bool | 支持歌单列表/详情 |
| `import` | bool | 支持导入收藏/歌单 |
| `resolve` | bool | 支持解析播放地址 |
| `lyrics` | bool | 支持歌词 |
| `collections` | array | 可导入集合，如 `[{kind:"favorites"},{kind:"playlist"}]` |
| `maxPageSize` | int | 宿主分页请求上限提示 |
| `loginKinds` | array | 支持 `webview`；空表示不可登录 |
| `loginRequired` | array | 需要登录的能力名 |

### 4.3 initialized 通知

```json
{"jsonrpc":"2.0","method":"initialized","params":{"generation":1}}
```

收到后才应认为可用。**握手期间不要联网、不要弹窗**，否则导入/启用会超时。

### 4.4 拒绝启用

- `protocolVersion` ≠ 1；
- `descriptor.id` 为空，或正式启用时与 `assignedId` 不一致；
- `descriptor.name` 为空；
- `requiredHostServices` 超出宿主能力。

### 4.5 身份从哪来

宿主不信任 `source.json` 的展示信息：

1. 导入时以空 `assignedId` 启动一次做**探测**，读 descriptor，按名称生成 ID（如 `node-4656`），把目录复制到 `sources/<id>/` 并缓存 `descriptor.json`。
2. 正式启动时把最终 ID 作为 `assignedId` 传入，插件必须接受。

插件不能把 ID 写死，只能把它当作本次运行的身份。

---

## 5. 宿主会调用哪些方法

宿主按能力调用；没实现的方法返回 `-32005`（未实现）即可。

| 能力 | 方法 | 触发场景 |
| --- | --- | --- |
| 必须 | `source.auth.status` | 设置页、判断能否搜索/播放 |
| `search` | `source.catalog.search` | 搜索、滚动加载 |
| `playlist` | `source.catalog.playlists`、`source.catalog.playlistDetail` | 歌单页、导入、详情 |
| 通用 | `source.catalog.albumDetail`、`source.catalog.artistDetail` | 点专辑/艺人 |
| 通用 | `source.catalog.history` | 历史接口（当前 UI 只显示本机历史） |
| `import` | `source.library.import` | 「从音源导入收藏/歌单」、更新只读歌单 |
| `resolve` | `source.library.resolve` | 播放来自该源的歌 |
| `lyrics` | `source.lyrics.get` | 歌词面板 |
| `loginKinds` | `source.auth.login`、`source.auth.logout`、`source.auth.verify`、`source.auth.persistSession` | 登录、启动校验、重试保存 |
| 兼容 | `source.manage.*` | 旧接口，当前可不实现 |

descriptor 只在 `initialize` 返回一次；没有单独的 describe 方法。

---

## 6. 方法参考

参数与返回都是 JSON 对象；`ref` 是宿主传回的来源引用 `{source, kind, id, playlistKind?}`。

### 6.1 身份与会话

#### `source.auth.status` — 本地快照，不联网

请求 `{}`，返回：

```json
{
  "state": "signedIn",
  "accountKey": "稳定且非凭据的账号标识",
  "displayName": "账号名",
  "lastVerifiedAt": "2026-11-04T10:00:00+08:00",
  "expiresAt": "2026-12-01T00:00:00+08:00",
  "ephemeral": false
}
```

`state`：`signedOut` / `signedIn` / `expired` / `unknown`。

- `accountKey` 必须稳定（换账号才变），且不能是 token；
- 拿不准返回 `unknown`，不要猜 `expired`；
- 只在内存有、尚未保存成功时 `ephemeral: true`。

#### `source.auth.verify` — 联网校验

请求 `{}`，返回同上。401/403 → `expired`；网络错误 → `unknown` 并保留凭据。

#### `source.auth.login`

```json
{ "kind": "webview", "attemptId": "attempt-1730000000", "writeToken": "宿主签发" }
```

返回：

```json
{ "succeeded": true, "persisted": true, "status": { "state": "signedIn", "accountKey": "..." } }
```

推荐流程：`host.webview.open` 拿回调 → 校验 state/回调 → 换 token → 用本次 `writeToken` 调 `host.session.set` → 返回。

`persisted:false` 表示只在内存成功（保存失败），宿主提示「仅本次运行有效」。`writeToken` 与本次 attempt 绑定，不可复用。

#### `source.auth.persistSession`

保存失败后的重试：`{ "operationId": "persist-1", "writeToken": "..." }` → `{ "persisted": true }`。

#### `source.auth.logout`

`{}` → `{}`。清除会话；宿主也会清空该插件的会话命名空间。

### 6.2 搜索

#### `source.catalog.search`

```json
{ "query": "晚风", "cursor": "", "limit": 20 }
```

```json
{
  "items": [
    {
      "id": "platform-track-id",
      "title": "晚风",
      "artists": [{ "name": "歌手" }],
      "album": { "title": "专辑" },
      "coverUrl": "https://...",
      "durationSeconds": 240
    }
  ],
  "nextCursor": "2",
  "hasMore": true
}
```

- `cursor` 对宿主不透明，原样回传；空 = 第一页。
- 最后一页必须 `hasMore:false`。
- 不要自己填 `source`，宿主会覆盖为插件身份。

### 6.3 歌单

#### `source.catalog.playlists`

```json
{ "kind": "owned", "cursor": "", "limit": 20 }
```

`kind`：`owned` / `favorite` / `chart`，空表示平台默认。

```json
{
  "items": [{ "id": "pl-1", "kind": "owned", "name": "歌单", "coverUrl": "https://...", "trackCount": 12 }],
  "nextCursor": "",
  "hasMore": false
}
```

#### `source.catalog.playlistDetail`

```json
{ "ref": { "source": "插件id", "kind": "playlist", "id": "pl-1", "playlistKind": "owned" }, "cursor": "", "limit": 50 }
```

```json
{
  "playlist": { "id": "pl-1", "name": "歌单", "trackCount": 12 },
  "tracks": [],
  "nextCursor": "",
  "hasMore": false,
  "truncated": false,
  "access": "anonymous"
}
```

`truncated:true` 表示平台只给有限列表、无法翻页；`access` ∈ `anonymous` / `login` / `member`，仅用于提示。

### 6.4 专辑与艺人

`source.catalog.albumDetail`：

```json
{ "ref": { "source": "插件id", "kind": "album", "id": "album-id" }, "cursor": "", "limit": 50 }
```

```json
{ "album": { "id": "album-id", "title": "专辑", "coverUrl": "https://..." }, "tracks": [], "nextCursor": "", "hasMore": false, "access": "anonymous" }
```

`source.catalog.artistDetail` 额外有 `section`（`tracks` / `albums`）与两套游标：

```json
{
  "artist": { "id": "artist-id", "name": "艺人", "coverUrl": "https://..." },
  "tracks": [],
  "albums": [],
  "nextTracksCursor": "",
  "nextAlbumsCursor": "",
  "hasMoreTracks": false,
  "hasMoreAlbums": false,
  "truncated": false
}
```

### 6.5 历史

`source.catalog.history`：`{ "cursor": "", "limit": 20 }` → `Page<Track>`。

### 6.6 导入

`source.library.import`：

```json
{ "collection": { "kind": "favorites" }, "runId": "run-123", "cursor": "", "limit": 50 }
```

`collection.kind` 为 `favorites` 或 `playlist`（后者带 `id`、`playlistKind`）。

```json
{ "tracks": [], "nextCursor": "2", "partial": false, "remoteDeleted": false }
```

语义（重要）：

- 宿主拉**完整快照**，只有正常跑到末页才提交；中途失败/取消不删本地数据。
- `partial:true` 表示这页可用但平台还有数据没给到，宿主会重试。
- `remoteDeleted:true` 只允许在正常结束的末页返回，且必须确认平台侧真的删除了该集合；网络错误或空列表不能这样标记。
- 远端删除 → 宿主删除本地只读副本；本地没有绑定 → 收藏仍保留（收藏是本机数据）。

### 6.7 播放解析

`source.library.resolve`：

```json
{ "track": { "id": "platform-track-id", "...": "完整 Track" }, "quality": "standard" }
```

```json
{ "url": "https://.../audio.flac", "quality": "lossless", "access": "full", "durationSeconds": 240, "lyricUrl": "https://..." }
```

- `url` 只存在内存，不落盘；
- `access`：`full` / `preview` / `unknown`，按实际可播放权限判断，不要猜；
- 返回的 `quality` 是实际音质，不是请求音质。

### 6.8 歌词

`source.lyrics.get`：

```json
{ "ref": { "source": "插件id", "id": "platform-track-id" } }
```

```json
[
  { "time": 0, "text": "第一行" },
  { "time": 3.5, "text": "第二行", "translation": "译文" }
]
```

`time` 单位秒（浮点）；`words` 可选，用于逐字时间；没有歌词返回空数组。

### 6.9 manage（兼容）

当前壳不主动向平台写数据，以下方法可不实现：

- `source.manage.reportHistory {track, listenedSeconds}`
- `source.manage.updatePlaylistDescription {ref, description}`
- `source.manage.deletePlaylist {ref}`

---

## 7. 数据结构

### Track

| 字段 | 类型 | 说明 |
| --- | --- | --- |
| `id` | string | 平台歌曲 ID，必须非空 |
| `title` | string | 歌名 |
| `artists` | array | `{id?, name}` |
| `album` | object | `{id?, title, coverUrl?}` |
| `coverUrl` | string | https 封面 |
| `durationSeconds` | number | 秒；拿不到可省略 |
| `source` / `onlySource` / `collected` | — | 插件不要填 |

### Playlist

`{ "id", "kind": "owned|favorite|chart", "name", "description"?, "coverUrl"?, "trackCount"? }`

### Page

`{ "items": [...], "nextCursor": "2", "hasMore": true }`

### Access

`anonymous` / `login` / `member`，只用于提示宿主 UI。

---

## 8. 宿主服务（插件调用宿主）

插件发请求，id 建议 `p:1`、`p:2`…；宿主按表处理。

### 8.1 会话存储

| 方法 | 参数 | 返回 |
| --- | --- | --- |
| `host.session.get` | `{key}` | `{exists, revision, value}` |
| `host.session.set` | `{key, value, ifRevision, writeToken}` | `{revision}` |
| `host.session.delete` | `{key, ifRevision, writeToken}` | `{}` |
| `host.session.list` | — | `{keys: []}` |

- `value` 必须是 **base64**，单值 ≤ 1 MiB；加密落盘，但同权限程序仍可读，不是保险箱。
- `ifRevision: 0` = 仅创建，已存在返回 `-32008`；覆盖传上次 revision。
- `writeToken` 由宿主在登录/重试保存时随请求下发，绑定当次 attempt。
- 命名空间按插件隔离。

### 8.2 登录窗口

`host.webview.open`：

```json
{
  "attemptId": "attempt-123",
  "url": "https://平台登录页",
  "allowedDomains": ["accounts.example.org", "*.example.org"],
  "callback": { "scheme": "my-music", "host": "oauth", "path": "/callback" }
}
```

```json
{ "callbackUrl": "my-music://oauth/callback?code=...", "cookies": [], "sessionSnapshot": {}, "canceled": false }
```

- `url` 必须 https；
- 只有命中 `callback` 的导航会被拦截；
- 用户关窗 → `canceled:true`；
- 不导出浏览器存储；`requireSessionExport:true` 会失败（`-32004`）。

### 8.3 日志

`host.log`：`{ "level": "info", "message": "..." }`；也可直接写 stderr。

### 8.4 未实现

- `host.auth.present`（宿主二维码窗口）：返回 `-32004`；纯 HTTP 扫码目前只能走 `host.webview.open`。
- `host.webview.close`：只回空结果。
- 升级/热重载：重新导入生成新实例，旧实例不自动更新。

---

## 9. 错误码

```json
{ "jsonrpc":"2.0","id":"h:3","error":{"code":-32001,"message":"需要登录"} }
```

| code | 含义 | 宿主表现 |
| --- | --- | --- |
| -32001 | 需要登录 / 写令牌无效 | 提示去登录 |
| -32002 | 无权限 | 提示原因，不重试 |
| -32003 | 不存在 | 返回上一级或空态 |
| -32004 | 限流 / 不支持 | 重试或隐藏入口 |
| -32005 | 未实现该能力 | 视为不支持 |
| -32006 | 已取消 | 静默 |
| -32007 | 写冲突 | 提示刷新后重试 |
| -32008 | 协议 / 版本错误 | 记日志、停用 |
| -32009 | 临时网络故障 | 允许重试 |
| -32010 | 不可用 | 提示稍后再试 |
| -32601 | 方法不存在 | 按未实现处理 |
| -32602 | 参数错误 | 记日志 |

插件返回未知 code 时，宿主按未知错误处理并显示 `message`。

---

## 10. 任意语言的实现要求与自测

最小实现只需要：读 stdin、写 stdout、解析/生成 JSON、按字节处理帧。

必须满足：

1. **stdout 只输出协议帧**；调试输出一律走 stderr。
2. **按 Content-Length 精确读字节**，不能按行读；处理半帧与多帧。
3. **每写一帧立即刷新缓冲**。
4. **UTF-8**，长度按字节计算（中文/emoji 不能用字符数）。
5. **并发**：保证每个请求最终有响应，或返回取消。
6. **超时**：单次调用 60 秒，握手阶段更短。
7. 收到 `shutdown` 后退出，清理子进程。
8. 不要依赖交互式终端（stdin 是管道）。

语言无关自测（Python，放到插件目录旁）：

```python
import json, subprocess, sys, os

def frame(obj):
    body = json.dumps(obj, ensure_ascii=False).encode("utf-8")
    return b"Content-Length: %d\r\n\r\n%s" % (len(body), body)

plugin_dir = os.path.abspath(sys.argv[1])
spec = json.load(open(os.path.join(plugin_dir, "source.json")))["entry"]
entry = spec.get("any") or next(iter(spec.values()))
if isinstance(entry, str):
    argv = [os.path.join(plugin_dir, entry)]
else:
    argv = [entry["command"]] + list(entry.get("args") or [])

proc = subprocess.Popen(argv, cwd=plugin_dir, stdin=subprocess.PIPE, stdout=subprocess.PIPE)

def read_frame():
    header = b""
    while b"\r\n\r\n" not in header:
        header += proc.stdout.read(1)
    length = int(header.decode().split(":")[1].split("\r\n")[0])
    return json.loads(proc.stdout.read(length))

def call(msg):
    proc.stdin.write(frame(msg)); proc.stdin.flush()
    return read_frame()

init = call({"jsonrpc": "2.0", "id": "h:1", "method": "initialize",
             "params": {"assignedId": "test-id", "protocolVersion": {"min": 1, "max": 1},
                        "hostServices": ["session", "webview", "log"], "hostVersion": "1.0"}})
print("descriptor:", init["result"]["descriptor"])
proc.stdin.write(frame({"jsonrpc": "2.0", "method": "initialized", "params": {"generation": 1}}))
proc.stdin.flush()
print("status:", call({"jsonrpc": "2.0", "id": "h:2", "method": "source.auth.status", "params": {}}))
print("search:", call({"jsonrpc": "2.0", "id": "h:3", "method": "source.catalog.search",
                       "params": {"query": "晚风", "cursor": "", "limit": 20}}))
```

用法：`python3 check.py /path/to/plugin-dir`（脚本按这个目录里的 `source.json` 启动插件）。

---

## 11. 实现清单

按要支持的能力勾选：

- [ ] 按帧读写 JSON，stdout 干净
- [ ] `initialize` 返回 descriptor（id/name/至少一个能力）
- [ ] `source.auth.status`（未登录返回 `signedOut`）
- [ ] `source.catalog.search` 分页正确，末页 `hasMore:false`
- [ ] `source.library.resolve` 返回可播放 https 地址与实际 `access`
- [ ] `source.catalog.playlists` / `playlistDetail`
- [ ] `source.catalog.albumDetail` / `artistDetail`
- [ ] `source.library.import` 完整快照语义、`remoteDeleted` 只在确认删除时返回
- [ ] `source.lyrics.get`
- [ ] 登录：`source.auth.login` + `host.webview.open` + `host.session.set`
- [ ] 用第 10 节脚本自测通过

---

## 12. 版本与兼容

- 协议主版本 1；双方声明范围，无交集拒绝启用。
- descriptor `schemaVersion` 当前为 1；新增字段保持向后兼容。
- 宿主忽略未知字段，但拒绝未知的必需能力。
- 语义变更会同步更新本文档与 `docs/SOURCE_PLUGIN_PROTOCOL.md`。

## 13. 参考

- 协议细节与宿主实现备注：[SOURCE_PLUGIN_PROTOCOL.md](SOURCE_PLUGIN_PROTOCOL.md)
- 业务语义契约（Go 接口层）：[SOURCE_CONTRACT.md](SOURCE_CONTRACT.md)
- Node 示例：[native/internal/plugin/testdata/node-source-plugin/](../native/internal/plugin/testdata/node-source-plugin/)
- 内置华为插件（Go）：`native/cmd/huawei-source/`
