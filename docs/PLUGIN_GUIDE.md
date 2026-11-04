# 插件开发指南：10 分钟做出可搜索的音源

本文面向第一次接触听澜的开发者。你只需要会保存文本文件和运行 Node.js，不需要读 Go 代码，也不需要预先了解 JSON-RPC、LSP 或 MCP。

完成前五步，你会在听澜搜索页看到自己的「Hello Music」音源，搜索「晚风」得到示例歌曲。示例完全离线，不登录、不访问平台、不提供音频；搜索成功不代表已经支持播放。后半部分说明如何替换成真实平台接口。

本文按 2026-11-04 的实现编写。完整通信格式见[音源插件协议](SOURCE_PLUGIN_PROTOCOL.md)，业务语义见[音源契约](SOURCE_CONTRACT.md)。协议中的「当前限制」同样适用于本指南。

## 1. 准备环境（约 1 分钟）

准备：

- macOS 或 Windows 上带「设置 → 音源 → 导入插件」入口的听澜构建。旧安装包若没有按钮，需要换用包含插件功能的构建。
- Node.js 22 或更新版本，这是本教程的推荐环境。示例不需要 npm、第三方依赖或 Go。
- 能保存 UTF-8 纯文本的编辑器。Windows 请显示文件扩展名，避免文件实际叫 `main.mjs.txt`。

在终端检查 Node：

```sh
node --version
```

**你会看到：** `v22...`、`v24...` 或更新版本号。若提示找不到命令，先安装 Node，并重新打开终端和听澜。听澜自己的构建与界面由 Go + Viva 实现，不内置 Node；只有选择脚本插件的用户需要安装对应解释器。

插件是听澜启动的另一个程序。听澜往它的标准输入（stdin）发请求，它往标准输出（stdout）回结果；普通调试文字必须写到标准错误（stderr）。无需开端口或运行服务器。

## 2. 建目录和启动清单（约 1 分钟）

用文件管理器新建 `hello-source` 文件夹。下面两个文件直接放在里面：

```text
hello-source/
├── source.json
└── main.mjs
```

将下面完整内容保存为 `source.json`：

```json
{
  "schemaVersion": 1,
  "entry": {
    "any": { "command": "node", "args": ["main.mjs"] }
  }
}
```

清单只回答「怎样启动」：`schemaVersion: 1` 是清单格式版本；`any` 表示任意平台使用这个入口；`command` 是程序名，`args` 是逐项传给它的参数，相当于运行 `node main.mjs`。宿主直接启动进程，不经过命令行 shell，不能把 `node main.mjs` 整串写进 `command`。

不要在交付清单里填 `id`、能力、名称或资源域名，也不需要制作 `descriptor.json`。插件在启动握手时报告名称和能力；宿主在导入时分配正式 ID，并写自己的描述缓存。

**你会看到：** 文件夹里有 `source.json`；完成下一步后共两个文件。清单不包含注释或尾逗号。

## 3. 放入完整插件代码（约 3 分钟）

将下面全部内容保存为 `main.mjs`，无需额外文件：

```js
import { TextDecoder } from "node:util";

const MAX_FRAME = 16 * 1024 * 1024;
const decoder = new TextDecoder("utf-8", { fatal: true });
let buffer = Buffer.alloc(0);
let ready = false;
let initialized = false;
let closing = false;

const descriptor = {
  schemaVersion: 1,
  id: "hello-probe",
  name: "Hello Music",
  shortName: "H",
  accent: "#28786C",
  capabilities: {
    search: true,
    import: false,
    resolve: false,
    playlist: false,
    lyrics: false,
    loginKinds: [],
    maxPageSize: 20,
  },
  resourceDomains: [],
};

const tracks = [
  {
    id: "demo-1",
    title: "晚风练习曲",
    artists: [{ id: "artist-1", name: "示例乐队" }],
    album: { id: "album-1", title: "离线小样" },
    durationSeconds: 180,
  },
  {
    id: "demo-2",
    title: "清晨练习曲",
    artists: [{ id: "artist-1", name: "示例乐队" }],
    album: { id: "album-1", title: "离线小样" },
    durationSeconds: 210,
  },
];

function send(message, done) {
  const body = Buffer.from(JSON.stringify({ jsonrpc: "2.0", ...message }));
  if (body.length > MAX_FRAME) throw new Error("response too large");
  const header = Buffer.from(`Content-Length: ${body.length}\r\n\r\n`);
  process.stdout.write(Buffer.concat([header, body]), done);
}
function reply(id, result) { send({ id, result }); }
function fail(id, code, message) { send({ id, error: { code, message } }); }
function fatal() {
  process.stderr.write("Invalid protocol frame\n");
  process.exit(1);
}

function handle(message) {
  if (message.jsonrpc !== "2.0" || Array.isArray(message)) {
    throw new Error("invalid message");
  }
  // Notifications have no id and must not receive a response.
  if (message.method === "shutdown") {
    closing = true;
    if (message.id !== undefined) {
      send({ id: message.id, result: {} }, () => process.exit(0));
    } else {
      process.stdout.end(() => process.exit(0));
    }
    return;
  }
  if (message.method === "initialized") {
    ready = initialized;
    return;
  }
  if (message.method === "$/cancelRequest") return; // Demo work is immediate.
  if (message.id === undefined || message.method === undefined) return;
  const { id, method } = message;
  if (typeof id !== "string" && !Number.isSafeInteger(id)) {
    throw new Error("invalid id");
  }
  const params = message.params ?? {};
  if (method === "initialize") {
    const version = params.protocolVersion;
    if (!version || !(version.min <= 1 && version.max >= 1)) {
      return fail(id, -32008, "Unsupported protocol version");
    }
    // Import probe has an empty assignedId; normal startup supplies the real id.
    descriptor.id = params.assignedId || "hello-probe";
    initialized = true;
    return reply(id, {
      protocolVersion: 1,
      descriptor,
      requiredHostServices: [],
    });
  }
  if (!ready) return fail(id, -32008, "Not initialized");
  switch (method) {
    case "source.describe":
      return reply(id, descriptor);
    case "source.auth.status":
    case "source.auth.verify":
      return reply(id, { state: "signedOut" });
    case "source.catalog.search": {
      if (typeof params.query !== "string") {
        return fail(id, -32602, "query must be a string");
      }
      const cursor = params.cursor ?? "";
      const offset = cursor === "" ? 0 : Number(cursor);
      const limit = params.limit ?? 20;
      if (typeof cursor !== "string" ||
          (cursor !== "" && !/^(0|[1-9][0-9]*)$/.test(cursor)) ||
          !Number.isSafeInteger(offset) || offset < 0 ||
          !Number.isInteger(limit) || limit < 1) {
        return fail(id, -32602, "Invalid cursor or limit");
      }
      const query = params.query.trim().toLowerCase();
      const matches = tracks.filter((track) =>
        [track.title, ...track.artists.map((artist) => artist.name)]
          .some((text) => text.toLowerCase().includes(query)));
      const end = offset + Math.min(limit, 20);
      const items = matches.slice(offset, end)
        .map((track) => ({ ...track, source: descriptor.id }));
      const hasMore = end < matches.length;
      return reply(id, { items, nextCursor: hasMore ? String(end) : "", hasMore });
    }
    case "source.library.resolve":
      return fail(id, -32004, "Demo provides search only");
    default:
      return fail(id, -32601, "Method not implemented");
  }
}

process.stdin.on("data", (chunk) => {
  if (closing) return;
  buffer = Buffer.concat([buffer, chunk]);
  try {
    while (!closing) {
      const split = buffer.indexOf("\r\n\r\n");
      if (split < 0) {
        if (buffer.length > 8192) throw new Error("header too large");
        return;
      }
      if (split > 8192) throw new Error("header too large");
      const header = buffer.subarray(0, split).toString("ascii");
      const lengths = header.split("\r\n")
        .filter((line) => /^Content-Length:/i.test(line));
      if (lengths.length !== 1) throw new Error("missing or duplicate length");
      const match = /^Content-Length:\s*(\d+)\s*$/i.exec(lengths[0]);
      const length = match ? Number(match[1]) : NaN;
      if (!Number.isSafeInteger(length) || length > MAX_FRAME) {
        throw new Error("invalid length");
      }
      const start = split + 4;
      if (buffer.length < start + length) return;
      const body = buffer.subarray(start, start + length);
      buffer = buffer.subarray(start + length);
      handle(JSON.parse(decoder.decode(body)));
    }
  } catch {
    fatal();
  }
});
process.stdin.on("end", () => {
  if (buffer.length && !closing) fatal();
});
process.stdin.on("error", fatal);
process.stdout.on("error", () => process.exit(1));
```

代码里的两个概念：

- **JSON-RPC 2.0**：请求是带 `method`（要做什么）、`params`（参数）、`id`（此次调用编号）的 JSON。响应带同一个 `id`，并返回 `result` 或 `error`。没有 `id` 的消息叫通知，不回复。
- **Content-Length 帧**：输入流没有天然消息边界，所以每段 JSON 前面先写 UTF-8 字节数，再写一个空行。中文通常占多个字节，必须用 `Buffer.byteLength` 或 `Buffer.length`，不能用字符串的 `.length`。一次输入可能只有半条消息，也可能有多条；示例处理了两种情况。

`initialize` 是宿主的能力询问。插件返回 `descriptor`，其中 `search: true` 让搜索页显示这个源。随后宿主发送 `initialized`，开始正常调用。导入探测时 `assignedId` 为空，因此示例有临时 ID；正式启用时用宿主分配值覆盖它，歌曲的 `source` 也使用这个值。

在 `hello-source` 目录内检查语法：

```sh
node --check main.mjs
```

**你会看到：** 正常情况下没有输出，命令成功结束。直接运行 `node main.mjs` 后没有输出，是在等宿主发请求；手敲一行 JSON 不符合帧格式。调试请用第 6 节的检查脚本；插件日志用 `console.error()`，不要用 `console.log()`。

## 4. 导入和启用（约 2 分钟）

1. 打开听澜的「设置 → 音源」，点击「导入插件」。
2. 在系统目录选择器中选择整个 `hello-source` 文件夹，不是某个文件或上一级目录。
3. 听澜启动一次插件读取描述信息，再复制到数据目录。你会看到「已导入 Hello Music（ID hello-music-xxxx）」提示，后缀随机生成。
4. 出现「启用外置音源」对话框后，核对名称和安装路径，点击确认。你会看到「已启用 Hello Music」，音源开关处于开启状态。

**你会看到：** 音源列表出现 Hello Music；没有登录按钮，因为 `loginKinds` 是空数组。公开搜索不需要先登录。若取消启用，导入记录仍在，之后可打开该源开关。

导入会执行插件以探测信息，发生在启用确认之前。只选择自己编写或信任的目录。插件以当前用户权限运行，没有操作系统沙箱。

安装后的目录类似：

```text
<应用数据目录>/sources/hello-music-xxxx/
├── source.json       # 宿主副本，增加 id、digest
├── descriptor.json   # 宿主写的名称、能力缓存
└── main.mjs
```

用「设置 → 音源 → 打开插件文件夹」定位。默认应用数据目录：macOS 为 `~/Library/Application Support/tinglan/native-v1`；Windows 为 `%APPDATA%\tinglan\native-v1`。独立 profile 以实际目录为准。

不要修改宿主分配的 ID 或描述缓存。导入是复制，修改原始文件夹不会改变已安装版本；同内容再次导入提示「已经导入过了」，不同内容重新导入会生成另一份安装和新 ID，目前没有原地升级或热重载流程。

## 5. 在听澜里验收（约 1 分钟）

打开搜索页，选择「Hello Music」音源页签，输入 `晚风` 并提交。

| 操作 | 应看到的结果 |
| --- | --- |
| 搜索 `晚风` | `晚风练习曲`，艺人 `示例乐队`，专辑 `离线小样`，来源角标 `H` |
| 搜索 `练习曲` | 两首曲目：晚风练习曲、清晨练习曲 |
| 搜索 `不存在的歌曲` | 正常空结果，不应报网络或登录错误 |
| 尝试播放示例歌曲 | 不能播放；示例没有声明解析能力，也没有音频地址 |
| 在设置停用 Hello Music | 对应搜索源不再可用；重新启用后可以再次搜索 |

到这里，你已经做出一个可搜索的音源。搜索随滚动分页，不显示「已加载 N 首」或「播放全部」；真实可播放的搜索歌曲会加入当前队列，不替换已有队列。

## 6. 不开听澜也能验证通信

下面是独立的测试客户端，临时扮演听澜，实际启动 `main.mjs` 并发送握手、通知和搜索帧。保存为 `check.mjs`，放在 `hello-source` 的**上一级目录**，不作为插件交付内容：

```text
工作目录/
├── check.mjs
└── hello-source/
    ├── source.json
    └── main.mjs
```

```js
import assert from "node:assert/strict";
import { spawn } from "node:child_process";
import { fileURLToPath } from "node:url";

const child = spawn(process.execPath, ["main.mjs"], {
  cwd: fileURLToPath(new URL("./hello-source/", import.meta.url)),
  stdio: ["pipe", "pipe", "inherit"],
});
let buffer = Buffer.alloc(0);
let serial = 0;
const pending = new Map();
const exited = new Promise((resolve) => child.on("exit", resolve));
const deadline = setTimeout(() => {
  console.error("FAIL: no complete response within 5 seconds");
  child.kill();
  process.exitCode = 1;
}, 5000);

function frame(message) {
  const body = Buffer.from(JSON.stringify({ jsonrpc: "2.0", ...message }));
  return Buffer.concat([
    Buffer.from(`Content-Length: ${body.length}\r\n\r\n`), body,
  ]);
}
function call(method, params) {
  const id = `h:${++serial}`;
  return new Promise((resolve, reject) => {
    pending.set(id, { resolve, reject });
    child.stdin.write(frame({ id, method, params }));
  });
}
child.stdout.on("data", (chunk) => {
  buffer = Buffer.concat([buffer, chunk]);
  for (;;) {
    const split = buffer.indexOf("\r\n\r\n");
    if (split < 0) return;
    const header = buffer.subarray(0, split).toString();
    const match = /^Content-Length: (\d+)$/im.exec(header);
    assert.ok(match, "stdout must contain only protocol frames");
    const end = split + 4 + Number(match[1]);
    if (buffer.length < end) return;
    const message = JSON.parse(buffer.subarray(split + 4, end).toString());
    buffer = buffer.subarray(end);
    assert.equal(message.jsonrpc, "2.0");
    const waiter = pending.get(message.id);
    assert.ok(waiter, "response id must match a request");
    pending.delete(message.id);
    if (message.error) waiter.reject(new Error(JSON.stringify(message.error)));
    else waiter.resolve(message.result);
  }
});
child.on("error", (error) => {
  for (const waiter of pending.values()) waiter.reject(error);
  pending.clear();
});
child.on("exit", () => {
  for (const waiter of pending.values()) waiter.reject(new Error("plugin exited"));
  pending.clear();
});

try {
  const init = await call("initialize", {
    assignedId: "hello-test-1234",
    protocolVersion: { min: 1, max: 1 },
    hostServices: ["session", "webview", "log"],
    limits: { sessionValueBytes: 1048576, sessionTotalBytes: 8388608, sessionKeys: 64 },
    hostVersion: "1.0",
  });
  assert.equal(init.protocolVersion, 1);
  assert.equal(init.descriptor.id, "hello-test-1234");
  assert.equal(init.descriptor.capabilities.search, true);
  assert.deepEqual(init.requiredHostServices, []);
  child.stdin.write(frame({ method: "initialized", params: { generation: 1 } }));
  const found = await call("source.catalog.search", { query: "晚风", cursor: "", limit: 20 });
  assert.equal(found.items.length, 1);
  assert.equal(found.items[0].title, "晚风练习曲");
  assert.equal(found.items[0].source, "hello-test-1234");
  assert.equal(found.items[0].durationSeconds, 180);
  assert.equal(found.hasMore, false);
  const first = await call("source.catalog.search", { query: "练习曲", cursor: "", limit: 1 });
  assert.equal(first.hasMore, true);
  assert.equal(first.nextCursor, "1");
  const last = await call("source.catalog.search", { query: "练习曲", cursor: first.nextCursor, limit: 1 });
  assert.equal(last.items[0].id, "demo-2");
  assert.equal(last.hasMore, false);
  assert.equal(last.nextCursor, "");
  const empty = await call("source.catalog.search", { query: "不存在", cursor: "", limit: 20 });
  assert.deepEqual(empty.items, []);
  const status = await call("source.auth.status", {});
  assert.equal(status.state, "signedOut");
  await call("shutdown", {});
  assert.equal(await exited, 0);
  console.log("PASS: initialize, assignedId, Chinese search, pagination, empty search, status, shutdown");
} catch (error) {
  console.error(`FAIL: ${error.message}`);
  process.exitCode = 1;
  child.kill();
} finally {
  clearTimeout(deadline);
  child.stdin.end();
}
```

在 `check.mjs` 所在目录执行：

```sh
node check.mjs
```

**你会看到：** 一行 `PASS: initialize, assignedId, Chinese search, pagination, empty search, status, shutdown`，进程正常结束。测试脚本的 stdout 是给人看的，可以使用 `console.log`；插件的 stdout 仍只允许协议帧。这验证实际子进程通信，不替代听澜界面、真实平台或 Windows 实机验收。

## 7. 接入真实平台

保留帧读取、发送和握手，把示例数据处理换成自己的平台适配。插件自己发网络请求，宿主没有 `host.http` 通用代理。只接入有权使用的服务，遵守账号权益、地域与服务条款；不做下载、离线音频或解密。

### 7.1 把搜索响应转换成听澜歌曲

`source.catalog.search` 收到 `{query, cursor, limit}`。将 `query` 作为平台搜索词，用标准 URL/JSON 编码；读取响应后转换成以下结构，不把平台原始对象直接返回：

```json
{
  "items": [{
    "source": "hello-music-1234",
    "id": "track-42",
    "title": "示例歌曲",
    "artists": [{"id": "artist-8", "name": "示例艺人"}],
    "album": {"id": "album-3", "title": "示例专辑"},
    "coverUrl": "https://img.media.example.org/cover.jpg",
    "durationSeconds": 183.5,
    "version": {"live": true}
  }],
  "nextCursor": "page-2",
  "hasMore": true
}
```

这是字段示意；`example.org` 不是音乐服务，`source` 必须换成本次握手 ID。

| 平台信息 | 返回字段 | 处理方式 |
| --- | --- | --- |
| 稳定歌曲标识 | `id` | 字符串，保留原始标识，不用结果序号；同一源内稳定且非空，最长 200 字符 |
| 歌名 | `title` | 非空字符串 |
| 艺人 | `artists` | `{id?, name}` 数组，不按逗号随意拆艺名 |
| 专辑 | `album` | `{id?, title, coverUrl?}`；未知省略 |
| 时长 | `durationSeconds` | 数字，单位秒；平台毫秒除以 1000，未知省略 |
| 录音版本 | `version` | `live/remix/acoustic/instrumental/cover/demo` 布尔值，以及 `edition` 字符串 |
| 来源 | `source` | 当前 descriptor 的 ID；不要填写平台显示名 |

当前实现没有歌曲 `durationMs` 字段，发它会丢失时长。不要填写宿主队列状态 `collected`、`onlySource`，也不要附带 Cookie、令牌或播放 URL。

**验收：** 用有权测试的平台关键词搜索，歌名、艺人、专辑与平台结果对应；无结果返回 `items: []`，接口失败返回错误，不能伪装空结果。

### 7.2 分页要真正前进

首次 `cursor` 为空。平台若用 offset，把下一次 offset 编成字符串；若用 token，保留不透明 token。宿主只把 `nextCursor` 原样带回，不替你递增。

- 有下一页：`hasMore: true`，`nextCursor` 非空且不等于当前 cursor。
- 末页：`hasMore: false`，`nextCursor: ""`（也可省略）。
- 尊重 `limit` 与平台上限。去重不能改变下一页的位置；不能用过滤后条数计算平台 offset。
- 重复页、坏游标、分页失败要显式失败，不能无限循环或把半份列表说成完整结果。

**验收：** 像检查脚本一样将 `limit` 设为 1，连续调用两页，看到不同 ID 和终页标志；真实结果较多时滚动到底部，下一页应追加。

收藏/歌单导入是另一个方法 `source.library.import`，不能直接复用搜索页 JSON。当前实际返回字段是 `Tracks`、`Next`、`Partial`、`RemoteDeleted`（大小写和类型见协议 §8），尚不是设计稿的 `complete/hasMore` 格式。只做搜索时保持 `import: false`。

### 7.3 点击播放时解析音频地址

完成 `source.library.resolve` 后才声明 `resolve: true`。请求是 `{track, quality}`，`track` 是完整歌曲对象，用 `track.source + track.id` 定位平台歌曲；`quality` 常见 `standard` 或 `lossless`。

成功结果示意：

```json
{
  "url": "https://audio.media.example.org/current-stream.mp3",
  "quality": "standard",
  "access": "unknown",
  "durationSeconds": 183.5
}
```

播放地址必须在本次调用中取得，不放进搜索结果、日志、本地库或跨播放缓存。`access` 只允许 `full`、`preview`、`unknown`：有证据才声明完整或试听；`quality` 返回实际音质。当前宿主不使用 `expiresAt`，不要依赖它缓存地址。

在 descriptor 声明媒体域，例如 `resourceDomains: [".media.example.org"]`。当前媒体校验按**子域后缀**匹配：接受 `audio.media.example.org`，不接受裸域 `media.example.org`；不是 `*.media.example.org` 语法。只允许 HTTPS、无 URL 用户信息、默认端口或 443，重定向也要通过校验。这不限制插件自己访问业务 API。

播放器不接受额外转交的 Cookie、自定义鉴权头或 DRM 解密任务。平台只提供这些资源时返回 `-32004`（不支持）；会员或地域无权限返回 `-32006`。华为适配要使用顶层 `fileURL`，`fileInfos` 可能是 JSON 辅助文件，不能仅凭 `auditionTime` 判定试听。

**验收：** 经授权的真实资源能开始播放、进度推进并定位到后段；权益失败有明确提示。搜索成功、拿到 URL 或 HTTP 200 都不能单独证明完整播放。不要为验收保存音频文件。

### 7.4 登录由插件完成，宿主提供窗口和存储

无需登录的音源保留 `loginKinds: []`。需要网页登录时，descriptor 声明 `loginKinds: ["webview"]`，握手返回 `requiredHostServices: ["session", "webview"]`。宿主当前提供 `session`、`webview`、`log`，这些是服务组名，不是完整方法名。

登录顺序：

1. 用户点击该源登录按钮，宿主调用 `source.auth.login`，带 `kind`、`attemptId`、`writeToken`。
2. 插件生成本次 state、PKCE 等平台参数，向宿主发新请求 `host.webview.open`。插件请求可用 `p:1` 这样的 ID，不复用宿主正在等待的 ID。
3. 宿主打开内嵌窗口并拦截回调，返回 `callbackUrl`。插件自行验证 state、完整回调与单次消费，再向平台兑换和校验音乐会话。
4. 插件通过 `host.session.get` 获取 revision，用本次 `writeToken` 保存新会话，最后回复原来的 `source.auth.login`。

窗口请求的 `params` 示例（替换为平台批准的地址）：

```json
{
  "attemptId": "原样使用本次登录参数",
  "url": "https://accounts.example.org/authorize",
  "allowedDomains": ["accounts.example.org"],
  "callback": {"scheme": "my-music", "host": "oauth", "path": "/callback"}
}
```

`allowedDomains` 支持精确域名和 `*.example.org`，与媒体域语法不同。宿主目前对 callback path 做前缀匹配，插件仍须精确校验。不要注册系统全局回调协议。

| 方法 | 参数 | 返回 |
| --- | --- | --- |
| `host.session.get` | `{key:"session"}` | `{exists, value, revision}` |
| `host.session.set` | `{key, value, ifRevision, writeToken}` | `{revision}` |
| `host.session.delete` | `{key, ifRevision, writeToken}` | `{}` |
| `host.session.list` | `{}` | `{keys:[]}` |

`value` 用 `Buffer.from(JSON.stringify(session)).toString("base64")` 编码；base64 是字节表示，不是加密。读取后反向解码。首次写 `ifRevision: 0`，覆盖时传 get 返回的 revision，成功后记住新 revision。不要保存全部平台响应。宿主按插件命名空间加密落盘，但密钥同存本机，不能抵御同用户权限的恶意程序。

登录返回例如 `{succeeded:true, persisted:true, status:{state:"signedIn", accountKey:"稳定且非凭据的账号标识", displayName:"账号名称"}}`。`source.auth.status` 返回本地快照，`source.auth.verify` 才联网验证；不确定时返回 `unknown`，明确失效才返回 `expired`。仅内存会话使用 `persisted:false` 和 `status.ephemeral:true`；重试保存方法为 `source.auth.persistSession {operationId,writeToken}`，不要为保存失败重新认证，也不要自动续期。

**代码还需增加：** 读循环收到没有 `method` 的响应时，按 `id` 完成插件发起的 host 请求；业务 handler 使用异步任务，不能阻塞读循环。快速示例没有 host 请求表，不能只加一个 `await` 就完成登录。取消时用 `AbortController` 停止网络任务，阻止旧任务写回；完整 stdout 帧串行写出。

**当前限制：** `host.auth.present` 未实现，返回 `-32004`，握手不提供 `auth.present`；网页内扫码可用 WebView，宿主独立二维码展示不可用。Cookie 预置与存储导出未接通，`requireSessionExport: true` 会失败；`host.webview.close` 目前只回空结果。默认单次调用预算 60 秒（含登录），上层可缩短，不能依赖五分钟登录或进度续期。令牌实现限制见协议 §6.1。

**验收：** 登录按钮只在已启用源下出现；登录成功后状态为已登录，重启能恢复该源会话；关闭窗口应取消，不产生新会话。真实账号操作需单独授权，本教程离线检查不覆盖这些项目。

## 8. 排错与交付

| 现象 | 检查与处理 |
| --- | --- |
| 找不到「导入插件」 | 当前构建尚不含该入口，使用包含插件功能的构建 |
| 终端能用 Node，听澜找不到 | GUI 的 PATH 可能不同；将 command 改为 Node 可执行文件绝对路径，保留 `args: ["main.mjs"]` |
| Windows 路径导致 JSON 报错 | 使用 `C:/Program Files/nodejs/node.exe` 或转义反斜线；不要额外包 shell 引号 |
| 提示不支持当前平台 | 提供当前平台键或 `any`；Intel macOS 用 `darwin-x64`，不是 `darwin-amd64` |
| 入口没有执行权限 | 脚本用解释器对象入口；二进制字符串入口需有执行权限 |
| 启用时报 id 不匹配 | 正式握手采用 `assignedId`；只在探测时用临时 ID |
| 坏帧、中文结果卡住 | stdout 不能有启动横幅、调试日志；检查字节长度、CRLF 空行和半帧缓冲 |
| 导入或启用超时 | 握手应立即返回，不联网、不等待登录；UI 导入预算 30 秒、启用预算 25 秒 |
| 搜索页没有新源 | 确认已启用，且握手 descriptor 的 `capabilities.search` 为 true |
| 没有登录按钮 | 只有已启用且 descriptor `loginKinds` 非空的源显示按钮 |
| 改代码后无变化 | 导入的是副本；改动后的目录再次导入会成为新实例，不会热更新旧实例 |
| 空结果或重复翻页 | 检查 `items`、`nextCursor`、`hasMore`，排除重复 cursor、错误被吞成空列表 |
| 能搜不能播放/封面不显示 | 检查 resolve 实现、资源域后缀、HTTPS、重定向和账号权限；本教程示例本就不提供播放 |
| 会话保存失败 | base64 解码后不超过 1 MiB；使用本次令牌和最新 revision；不打印会话排错 |

交付时只装入插件所需文件。解释器入口的用户需自备 Node/Python，宿主不自动安装；不要把运行时存在当作当然。原生二进制可按平台提供：

```json
{
  "schemaVersion": 1,
  "entry": {
    "darwin-arm64": "bin/source-arm64",
    "darwin-x64": "bin/source-x64",
    "windows-x64": "bin/source.exe",
    "any": {"command": "node", "args": ["main.mjs"], "env": {"NODE_ENV": "production"}}
  }
}
```

宿主优先选匹配的平台键，缺失才选 `any`，不是平台入口启动失败后再试 `any`。字符串入口在插件目录内；对象入口目前以 **args 非空** 判断解释器模式，空 args 对象仍按目录内二进制处理。args 不允许绝对路径或任何含 `..` 的字符串，存在于插件目录内的相对文件参数会解析为绝对路径。不要依赖 shell 展开、管道或 shebang 自动识别。

**交付验收：** 在目标系统的干净目录复制包，检查运行时、导入、启用、搜索和停用；不要只用开发机源码目录测试。当前没有插件市场、签名验证、自动更新或可靠的升级重新授权流程；目录摘要用于重复导入识别，不证明作者可信，也不构成持续防篡改保证。卸载涉及安装副本和会话，本指南不提供未经验证的“一键卸载”承诺；日常调试可先停用。

## 9. 协议速查

| 项目 | 当前实现 |
| --- | --- |
| 帧 | `Content-Length: N\r\n\r\n<UTF-8 JSON>`，N 为正文**字节数**，上限 16 MiB，无 batch |
| 请求 | `{"jsonrpc":"2.0","id":"h:1","method":"...","params":{...}}` |
| 成功 | `{"jsonrpc":"2.0","id":"h:1","result":{...}}` |
| 失败 | `{"jsonrpc":"2.0","id":"h:1","error":{"code":-32004,"message":"不支持此能力"}}` |
| 生命周期 | initialize 请求 → initialized 通知 → source/host 双向调用 → shutdown；当前宿主 shutdown 为通知，并关闭 stdin |
| 超时 | 默认每次 60 秒；调用者更短截止时间优先，进度不延长预算 |
| 取消 | `$/cancelRequest {id}` 通知；SDK 可取消任务，插件自己停止网络与写回 |
| 进度 | `$/progress {requestId,phase,current,total,message}` 通知；宿主当前接收后忽略，不承诺 UI 展示 |

| 方法组 | 方法与关键参数 |
| --- | --- |
| 描述 | `source.describe {}` → descriptor |
| 登录 | `source.auth.status/verify {}`、`login {kind,attemptId,writeToken}`、`logout {}`、`persistSession {operationId,writeToken}` |
| 搜索 | `source.catalog.search {query,cursor,limit}` → `{items,nextCursor,hasMore}` |
| 列表/详情 | `source.catalog.playlists {kind,cursor,limit}`；`playlistDetail/albumDetail/artistDetail {ref,section,cursor,limit}` |
| 平台历史 | `source.catalog.history {cursor,limit}`；协议保留，当前界面只展示本机历史 |
| 导入/播放 | `source.library.import {collection,runId,cursor,limit}`；`resolve {track,quality}` |
| 歌词 | `source.lyrics.get {ref}` → 歌词行数组；Go SDK 当前存在参数拆包差异，见协议 §8 |
| 宿主服务 | `host.session.*`、`host.webview.open/close`、`host.log {level,message}` |

业务错误码按当前 `source.go` 的 `classify` 映射，不能套用其它 JSON-RPC 服务的编号：

| code | kind | 含义 |
| --- | --- | --- |
| -32001 | login | 需要登录、会话失效；宿主也用于无效写令牌 |
| -32002 | notfound | 资源不存在 |
| -32003 | ratelimit | 限流 |
| -32004 | unsupported | 不支持，例如尚未实现的二维码展示 |
| -32005 | network | 网络错误 |
| -32006 | permission | 会员、地域或其它权限限制 |
| -32007 | transient | 临时故障 |
| -32008 | protocol | 协议/数据错误；宿主也用于 revision 冲突 |
| -32009 | canceled | 已取消 |
| -32010 | unavailable | 实例不可用 |

未知方法返回 `-32601`，参数错误返回 `-32602`。`error.message` 只放安全短句，不含平台原始响应、令牌、URL 或堆栈。当前宿主按 `code` 分类，不依赖 `data.kind` 或 `retryAfterMs`。

## 10. 继续阅读

- [协议参考](SOURCE_PLUGIN_PROTOCOL.md)：字段、生命周期、宿主服务与实现限制。
- [语义契约](SOURCE_CONTRACT.md)：账号状态、导入发布、三态访问和播放绑定。
- [仓库 Node 示例](../native/internal/plugin/testdata/node-source-plugin/README.md)：另一份可直接导入的脚本样例，搜「脚本」。
- [华为插件实现](../native/cmd/huawei-source/main.go)：真实网页登录的调用结构，不是第三方平台的授权凭据模板。
- [当前实现状态](IMPLEMENTATION_STATUS.md)：区分实现、离线验证与实机验收。

Go 作者注意：仓库的 `internal/plugin/sdk` 和 `internal/plugin/wire` 受 Go `internal` 规则限制，不能被任意外部模块直接 import；目前没有独立公开 SDK。外部项目可按协议自行实现，使用任何能读写标准输入输出的语言。
