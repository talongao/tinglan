# 写一个听澜音源插件

> 目标：10 分钟做出一个能跑的音源插件，在听澜的搜索页里搜到它返回的歌。
> 不需要读别的文档；协议速查放在最后。

## 你会做出什么

一个目录，两个文件：

```text
my-source/
├── source.json   # 告诉听澜怎么启动你的插件
└── main.mjs      # 插件本体
```

导入后，听澜的搜索页会多一个你命名的页签；在里面搜索，显示你返回的歌曲。

---

## 第 1 步：确认环境

需要一个 Node（18 以上）。在终端里：

```bash
node -v
```

有版本号就继续。没有就先装 Node，或改用任何能读写标准输入输出的语言（Go、Python 都行，
协议在附录）。

## 第 2 步：建目录

```bash
mkdir my-source
cd my-source
```

## 第 3 步：写 `source.json`

创建 `source.json`，内容完全照抄：

```json
{
  "schemaVersion": 1,
  "entry": {
    "any": { "command": "node", "args": ["main.mjs"] }
  }
}
```

这里只做一件事：告诉听澜「用 `node main.mjs` 启动这个插件」。
`any` 表示所有系统都用这一条；如果某个平台要不同的命令，可以写
`"darwin-arm64"`、`"windows-x64"` 这样的键覆盖它。

> 不要在这里写 `id`。导入时交给听澜分配。

## 第 4 步：写 `main.mjs`

创建 `main.mjs`，内容完全照抄（约 90 行，不用改）：

```js
#!/usr/bin/env node
// 听澜音源插件最小实现。
let buffer = Buffer.alloc(0);

process.stdin.on("data", (chunk) => {
  buffer = Buffer.concat([buffer, chunk]);
  for (;;) {
    const head = buffer.indexOf("\r\n\r\n");
    if (head < 0) return;
    const match = /content-length:\s*(\d+)/i.exec(buffer.subarray(0, head).toString());
    if (!match) { buffer = buffer.subarray(head + 4); continue; }
    const start = head + 4;
    const length = Number(match[1]);
    if (buffer.length < start + length) return;
    const body = buffer.subarray(start, start + length).toString();
    buffer = buffer.subarray(start + length);
    try { handle(JSON.parse(body)); } catch (e) { log(`bad frame: ${e}`); }
  }
});

function send(message) {
  const body = JSON.stringify(message);
  process.stdout.write(`Content-Length: ${Buffer.byteLength(body)}\r\n\r\n${body}`);
}

function reply(id, result) { send({ jsonrpc: "2.0", id, result }); }
function fail(id, code, message) { send({ jsonrpc: "2.0", id, error: { code, message } }); }

// 插件自述。shortName 是列表里那个小徽标，1~2 个字。
const descriptor = {
  schemaVersion: 1,
  name: "我的音源",
  shortName: "我",
  accent: "#3C873A",
  capabilities: { search: true },
};

// 假装这是你从平台 API 拿到的数据。
const songs = [
  { id: "s1", title: "第一首歌", artists: [{ id: "a1", name: "某歌手" }] },
  { id: "s2", title: "第二首歌", artists: [{ id: "a1", name: "某歌手" }] },
];

function handle(msg) {
  if (msg.method === undefined) return; // 听澜发来的响应，插件这里用不到
  const id = msg.id;

  switch (msg.method) {
    // 握手：听澜问你叫什么、要哪些能力
    case "initialize": {
      const assignedId = msg.params?.assignedId;
      reply(id, {
        protocolVersion: 1,
        descriptor: { ...descriptor, id: assignedId }, // id 必须用听澜分配的
        requiredHostServices: [],
      });
      return;
    }

    case "initialized":
      return;

    // 登录状态。先不做登录，就报「未登录」
    case "source.auth.status":
    case "source.auth.verify":
      reply(id, { state: "signedOut" });
      return;

    // 搜索：听澜把关键词给你，你把歌单给它
    case "source.catalog.search": {
      const query = String(msg.params?.query ?? "");
      const items = songs
        .filter((s) => query === "" || s.title.includes(query))
        .map((s) => ({ ...s }));
      reply(id, { items, hasMore: false });
      return;
    }

    case "shutdown":
      reply(id, {});
      setTimeout(() => process.exit(0), 10);
      return;

    default:
      fail(id, -32601, `unsupported method: ${msg.method}`);
  }
}

function log(text) {
  process.stderr.write(`${text}\n`); // 日志只能走 stderr
}
```

### 这段代码在干什么

| 部分 | 作用 |
| --- | --- |
| 读帧循环 | 听澜按 `Content-Length` 头发消息，照这个格式拆包 |
| `initialize` | 握手。告诉听澜插件名、徽标、支持哪些能力 |
| `source.auth.status` | 上报登录状态；先写死 `signedOut` |
| `source.catalog.search` | 收到关键词，返回 `items` 数组 |
| `hasMore` | 还有下一页就 `true`，这是最后一页就 `false` |

三条规则必须遵守：

1. `stdout` 只能写协议帧，不能 `console.log`。
2. 日志写 `stderr`。
3. `descriptor.id` 用 `assignedId`，不要自己造。

## 第 5 步：导入并启用

1. 打开听澜 → 左下角 **设置** → **音源**
2. 点 **导入插件**，在弹出的系统窗口里选中 `my-source` 目录，点 **打开**
3. 听澜会弹确认框，显示插件名和路径，点 **启用**

**此时你应该看到**：音源列表里出现「我的音源」，行尾开关是开着的。

## 第 6 步：验证

1. 左侧点 **搜索**
2. 输入框下方出现两个页签，切到 **我的音源**
3. 输入「第一」，点 **搜索**

**此时你应该看到**：「第一首歌」，右边有蓝色的小徽标「我」。

播放它不会有声音——因为你还没实现「取播放地址」，这一步在下一节。

## 第 7 步：接入你自己的平台

现在把上面两处假数据换掉。

### 1. 换成真实搜索

```js
async function search(query) {
  const url = `https://你的平台/api/search?q=${encodeURIComponent(query)}`;
  const res = await fetch(url, {
    headers: { Authorization: `Bearer ${token}` }, // 需要登录时
  });
  if (!res.ok) throw new Error(`HTTP ${res.status}`);
  const data = await res.json();

  // 把平台的数据结构映射成听澜的 Track
  return data.songs.map((song) => ({
    id: String(song.id),
    title: song.name,
    artists: song.artists.map((a) => ({ id: String(a.id), name: a.name })),
    album: { id: String(song.album.id), title: song.album.name },
    coverUrl: song.album.pic,
    durationSeconds: song.durationMs / 1000,
  }));
}
```

`handle` 里改成异步：

```js
case "source.catalog.search": {
  const query = String(msg.params?.query ?? "");
  search(query)
    .then((items) => reply(id, { items, hasMore: false }))
    .catch((e) => fail(id, -32009, `搜索失败：${e.message}`)); // -32009 = 临时故障
  return;
}
```

几条映射规则：

- `id` 用平台的歌曲 ID（字符串），后面取播放地址还要用它。
- `source` 字段不用写，听澜会补成你的插件身份。
- 时长用秒（`durationSeconds`），不是毫秒。
- 拿不到封面就不写 `coverUrl`，界面显示占位图。

### 2. 加分页（可选）

结果多的时候，把游标还回去：

```js
reply(id, { items, nextCursor: String(page + 1), hasMore: page < lastPage });
```

听澜下次会用同一个 `nextCursor` 再来问。最后一页必须 `hasMore: false`。

### 3. 加播放地址

在 `capabilities` 里加 `resolve: true`，然后实现：

```js
case "source.library.resolve": {
  const track = msg.params?.track;
  fetchPlayURL(track.id) // 你的平台 API
    .then((url) => reply(id, { url, access: "full" }))
    .catch((e) => fail(id, -32009, `取播放地址失败：${e.message}`));
  return;
}
```

返回的 `url` 只放在内存里传递，听澜不会写盘。

### 4. 加登录（可选）

需要账号才能搜索/播放时，用听澜内嵌的登录窗口拿回调，再自己换 token：

```js
case "source.auth.login": {
  const writeToken = msg.params?.writeToken; // 存会话必须带它
  const result = await host.call("host.webview.open", {
    attemptId: msg.params?.attemptId,
    url: "https://你的平台/oauth/authorize?...",
    allowedDomains: ["你的平台域名"],
    callbackScheme: "myapp",
    callbackHost: "callback",
    callbackPath: "/auth",
  });
  if (result.canceled) return reply(id, { succeeded: false });
  const session = await exchangeCode(result.callbackUrl); // 你自己换 token
  await host.call("host.session.set", {
    key: "session",
    value: Buffer.from(JSON.stringify(session)).toString("base64"),
    ifRevision: 0,
    writeToken,
  });
  reply(id, { succeeded: true, persisted: true, status: { state: "signedIn" } });
  return;
}
```

这里的 `host.call` 不是现成的：你需要让插件能「发请求给听澜并等响应」，
也就是把上面的 `send` 和读帧循环配对成带 id 的等待表。完整实现见 Go SDK
（`native/internal/plugin/sdk/`）或协议文档 §7。字段和错误码见[附录](#宿主服务)。

## 出问题了怎么办

| 你看到的 | 原因 | 怎么办 |
| --- | --- | --- |
| `启动插件失败（node）：executable file not found` | 听澜的 PATH 里没有 node | `source.json` 里写 node 的绝对路径，如 `/opt/homebrew/bin/node` |
| `unsupported protocol 0` | `initialize` 没按格式回 | 检查是否返回了 `protocolVersion` 和 `descriptor` |
| 设置页有插件，搜索页没页签 | 插件没启用，或 `capabilities.search` 不是 `true` | 先启用；再检查能力声明 |
| 搜索结果徽标显示「?」 | `shortName` 为空 | 写 1~2 个字 |
| 搜索一直转圈 | 你忘了回 `reply`，或 `hasMore` 永远 true | 每个请求都必须有回应 |
| 导入时提示「已经导入过了」 | 同一个插件重复导入 | 直接用已有的那个，或先删旧目录再导入 |
| 点播放没反应 | 没实现 `resolve` | 见第 7 步第 3 小节 |

手动试插件（不打开听澜）：

```bash
body='{"jsonrpc":"2.0","id":"1","method":"source.catalog.search","params":{"query":"第一"}}'
printf 'Content-Length: %d\r\n\r\n%s' "${#body}" "$body" | node main.mjs
```

## 做完之后你应该看到

- 设置 → 音源：多一个「我的音源」，开关是开的，可以停用/再启用。
- 搜索页：输入框下面多一个「我的音源」页签，切过去搜「第一」出现「第一首歌」。
- 重启听澜：插件仍然在，状态保持启用。

## 你只需要记住这些

1. `stdout` 只写帧；日志走 `stderr`。
2. 握手时用听澜给的 `assignedId` 当 `id`。
3. 每个请求都要回一条消息，成功 `result`、失败 `error`。
4. 分页最后一页 `hasMore: false`。
5. 不要自己写 `source.json` 的 `id`，导入时听澜分配。
6. 插件以你的用户权限运行，没有沙箱，只装你信得过的插件。

## 附录：协议速查

### 帧格式

```text
Content-Length: 123\r\n
\r\n
{"jsonrpc":"2.0","id":"h:1","method":"initialize","params":{...}}
```

单帧最大 16 MiB；单次调用默认 60 秒超时。

### 方法表

```text
source.describe               → Descriptor           （必做）
source.auth.status            → AuthStatus           （必做）
source.catalog.search         → Page<Track>          （必做）
source.auth.verify            → AuthStatus
source.auth.login             {kind, attemptId, writeToken} → AuthResult
source.auth.logout            → {}
source.auth.persistSession    {operationId, writeToken} → {persisted}
source.library.import         {collection, runId, cursor, limit} → ImportPage
source.library.resolve        {track, quality} → Playback
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

### 分页

```json
{ "items": [ ... ], "nextCursor": "2", "hasMore": true }
```

游标内容由你定义，听澜只负责原样带回。

### Track

```json
{
  "id": "platform-track-id",
  "title": "歌名",
  "artists": [{ "id": "artist-id", "name": "歌手" }],
  "album": { "id": "album-id", "title": "专辑" },
  "coverUrl": "https://...",
  "durationSeconds": 240
}
```

### 错误码

| code | 含义 | 听澜的表现 |
| --- | --- | --- |
| -32001 | 需要登录 | 提示去登录 |
| -32002 | 无权限 | 说明原因，不重试 |
| -32003 | 不存在 | 返回上一级 |
| -32004 | 限流 | 显示重试时间 |
| -32005 | 未实现该能力 | 隐藏入口 |
| -32006 | 已取消 | 不提示 |
| -32007 | 写冲突 | 刷新后重试 |
| -32008 | 协议错误 | 记日志 |
| -32009 | 临时故障 | 允许重试 |
| -32010 | 不可用 | 提示稍后再试 |
| -32601 | 方法不存在 | 视为未实现 |

### 宿主服务

```text
host.session.get    {key} → {value(base64), revision, exists}
host.session.set    {key, value(base64), ifRevision, writeToken} → {revision}
host.session.delete {key, ifRevision, writeToken} → {}
host.session.list   → {keys[]}
host.webview.open   {attemptId, url, allowedDomains, callbackScheme, callbackHost, callbackPath} → {callbackUrl, cookies, canceled}
host.log            {level, message}
```

会话值用 base64；单值 ≤1 MiB、每插件 ≤8 MiB、最多 64 个 key。
`host.auth.present`（二维码展示）目前未实现，会返回 `-32004`。

### 参考

- 能直接导入的完整示例：[`native/internal/plugin/testdata/node-source-plugin/`](../native/internal/plugin/testdata/node-source-plugin/)
- 完整协议：[SOURCE_PLUGIN_PROTOCOL.md](SOURCE_PLUGIN_PROTOCOL.md)
- 契约语义：[SOURCE_CONTRACT.md](SOURCE_CONTRACT.md)
