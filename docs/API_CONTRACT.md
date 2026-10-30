# 华为音乐接口契约：第一版实现依据

核验日期：2026-10-14。这是本人账号与公开样本的兼容性实验记录，不是华为公开支持承诺。未验证能力不得从接口名字推断成功。

## 1. 共用规则

业务域：`https://api-drcn.music.dbankcloud.cn`。

- POST 用 JSON，GET 用 URLSearchParams 编码；不要拼接未经编码的用户输入。
- Content-Type: application/json。
- 业务成功通常要求 HTTP 成功且 `result.resultCode === "000000"`。
- 只读调用 timeout=25 秒，写入不自动重试。
- 部分 HTTP 400 没有 body，通过可选 `x-error-code` 补充诊断，不当作认证失效。
- 响应中的 `contentExInfo` 经常是 JSON 字符串，必须 try/catch 二次解析。空字符串、非 JSON 和缺字段不能让整页崩溃。
- 数量、时长、码率等多为字符串，转换失败视为未知，不默认为有效 0。
- `contentID` 为不透明字符串，不假定全是整数。
- 初期调用使用最少已验证头信息；不要凭空捏造 deviceDigest、硬件标识或把 Android UA 当作必须条件。

### 音乐会话请求头

已验证的形式：

```text
Authorization: Digest username=app-music-baseline, nonce=<本次随机值>, created=<UTC ISO8601，无毫秒>, token=<music token>
```

无音乐会话调用不加 token 部分；使用 `src/auth.mjs` 的构造逻辑。`created` 每请求更新，nonce 随机。不要从日志或抓包中硬编码旧头信息。

这一串的 token 是 loginex 返回的音乐 token，不是 OAuth Access Token。

## 2. 登录

### 2.1 授权入口

`https://oauth-login.platform.hicloud.com/oauth2/v3/authorize`

已验证实验参数：

```text
client_id=101444301
redirect_uri=hms://redirect_uri
response_type=code
access_type=offline
scope=openid
countryCode=cn
ui_locales=zh-cn
state=<随机，单次>
code_challenge=<S256(verifier)>
code_challenge_method=S256
```

本人扫码后，这组参数成功取得账号令牌与音乐会话。client_id 不是为本客户端注册的应用：它来自第三方公开帖，且据观察到同时是华为音乐在 AppGallery 的应用 ID，官方回调 scheme 也为 `com.huawei.apps.101444301:/oauth2redirect`，因此极可能是官方 App 自身的 OAuth 身份。它随时可能失效，随客户端分发会涉及服务条款与身份冒用问题。不能把“技术可登录”当作“已获分发许可”。

`http://127.0.0.1:8765/callback` 本次被拒绝为未注册地址。不要擅自改回调并假定能用。不得注册系统全局 hms:// 处理器覆盖其他应用；使用受限登录窗口内的导航拦截。

2026-10-30 无登录复测：保持当前 client_id 与其他授权参数，只替换回调地址。`tinglan://oauth/callback`、`tinglan:/oauth2redirect` 均返回 1101/20022（invalid redirect_uri）；`http://127.0.0.1:8765/callback`、`http://127.0.0.1:49152/callback` 均返回 1101/20023（redirect_uri not registered）。对照 `hms://redirect_uri` 可进入扫码页。授权入口首个响应均可能为 HTTP 200，必须执行页面脚本后判断。此验证未登录或兑换令牌，不能据此声称完成账号授权。

### 2.2 授权码兑换

`POST https://oauth-login.cloud.huawei.com/oauth2/v3/token`

Content-Type: application/x-www-form-urlencoded。

```text
grant_type=authorization_code
client_id=<同次授权的 client_id>
redirect_uri=<同次授权的 redirect_uri>
code=<本次 code>
code_verifier=<本次 verifier>
```

本人真实实验中不传 client_secret 成功，返回 access_token、refresh_token、expires_in=3600。不要把这外推到所有客户端配置。

### 2.3 音乐会话兑换

`POST /music-odpapp-service/v5/service/userinfo/loginex`

```json
{"accessToken":"<账号 AT>"}
```

请求带不含 token 的 Digest 头。响应关注：

- `result.resultCode`
- `token`：音乐会话凭据
- `snsUserID`：本人音乐用户标识
- `expireTime`：已见 14 位 `yyyyMMddHHmmss` 字符串，时区尚未完全确认

只将需要的音乐会话字段进入系统安全存储，不把全部用户信息保存成通用 Session blob。

### 2.4 Refresh Token：首版禁止依赖它

同 token endpoint，grant_type=refresh_token + client_id + 本人真实 refresh_token、不传 client_secret，本次返回 HTTP 400 / error=1101 / sub_error=20171。不能宣称自动续期可用。

首版使用音乐会话，真正失效才重新扫码。不要设“每 55 分钟刷新”的无效定时器；不要把 RT 获取成功当作 refresh 能用。

## 3. 收藏、歌单与历史

### 3.1 收藏列表与我的歌单发现

`POST /music-userbehavior-service/v3/service/favorite/simple/all`

```json
{
  "targetSnsUserID":"<本人音乐会话的 snsUserID>",
  "start":0,
  "limit":50,
  "contentTypes":[1]
}
```

响应：`favoriteInfos[]`，另有 updateTime、sortInfo。不要把它们当未经验证的增量同步游标。

- 歌曲：contentTypes=[1]。
- 歌单发现：已测 contentTypes=[1,4,5,7]，limit=999，返回歌曲及本人新建 type=5 歌单。
- 官方 H5 将 1 作为歌曲、4 作为收藏歌单、5 作为自建歌单、7 作为榜单。
- 单独 [4] 曾返回空；这不是错误，也不能证明完整歌单列表协议只用 [4]。
- 第一版可以分分页请求 [1,4,5,7]，保留 type=4/5/7 作为歌单列表；type=1 进入收藏缓存。请求返回不足 limit 则结束；返回满页继续读取，去重并检查无进展，不能无限循环。
- 已验证 start=0/5、limit=5 两页不重复，以及 limit=999 成功；limit=1000 返回 400。建议常规 limit=50，不能把 999 当保证永远不变的官方上限。
- 完整列表、分页和不同账号空态需要加入真实验收；不以第一次返回的空数组直接判定接口失效。
- **总数没有已验证的直接字段。** 正式客户端以相同 `start/limit=50` 分页到短页，再按真实 contentID 去重统计；不猜测 `totalCount`/`totalNum`。总数未确认时只显示“已加载 N 首／正在统计”，不能把首批数量叫总数。
- 客户端 `favoritesSummary()` 与 `favorites()` 共享主进程在途请求和内存页索引；刷新收藏、收藏变更或切换账号使该索引失效。界面靠近列表底部自动追加下一页，失败停止自动请求、保留列表并显示重试；“播放全部”先补齐所有分页。
- 965 首合成库已在 Linux Electron 验证总数、滚动分页、失败重试、切页恢复和完整队列；不代表本轮重新读取了本人真实音乐库。

### 3.2 本人自建歌单详情

`GET /music-userbehavior-service/v1/service/playlist/detail/all`

参数：`playListID=<自建歌单真实ID>&start=0&limit=50`。

**playListID 必填。这不是所有歌单列表。**

响应：

```text
playListInfoEx.playListInfo          歌单元数据
playListInfoEx.playListSongInfoList  歌曲数组
```

歌单字段实测包含 contentID、contentName、contentType、picture、totalCount、shareFlag、snsUserID。更新简介后从详情实际出现的描述字段读取（可参考本地测试结构，不把私人响应直接做夹具）；不要臆造固定 `description` 一定存在。

### 3.3 公开/收藏歌单详情

`POST /music-operation-service/v2/service/musiclist/detail/bymusiclistid`

```json
{"musicListID":"<ID>","copyrightSwitch":"1","start":0,"limit":"50"}
```

响应：`musicListInfoEx.musicListDetail` + `musicListInfoEx.songSimpleInfos[]`。

不要把本人的 type=5 自建歌单 ID 无条件发到这个接口。type=7 榜单详情未单独验证，第一版可以标“暂不支持该类型”，不误用同路由声称成功。

### 3.4 收藏状态与增删

查询：`GET /music-userbehavior-service/v1/service/favorite/simple/exist`

参数：`contentType=1&contentID=<歌曲ID>`。成功时 `exist` 为 "0" 或 "1"。

新增：`POST /music-userbehavior-service/v1/service/favorite/create`

删除：`POST /music-userbehavior-service/v1/service/favorite/delete`

```json
{"contentType":"1","contentID":"<歌曲ID>"}
```

均已实测：0 → create → 1 → delete → 0。前端按目标状态而不是盲切布尔值；写超时先查 exist，不重复 create。

**favorite/delete 对自建 type=5 曾返回 000000 但没有删除歌单。禁止复用此接口删除自建歌单。**

### 3.5 创建自建歌单

`POST /music-userbehavior-service/v1/service/playlist/create`

```json
{
  "createTime":"<UTC yyyyMMddHHmmss>",
  "playListName":"<用户输入>",
  "songCodes":["<歌曲ID>"],
  "shareFlag":"0"
}
```

已测带一首歌创建，响应返回 `playListID`。空数组是否允许未验证；首版创建入口来自歌曲选择，至少包含一首。

shareFlag=0 用于本次私密测试，默认保持。不要自动公开歌单。

写超时可能已创建成功：保留请求意图，在本人歌单中按返回/创建时间和临时识别信息核对，不立即重发制造重复歌单；自动清理只允许已确认本次创建的 ID。

### 3.6 修改简介

`POST /music-userbehavior-service/v1/service/playlist/update`

```json
{"actionType":"1","listDesc":"<简介>","playListID":"<本人自建歌单ID>"}
```

已测成功且读回确认。labelList 可选但未验证必要语义，首版不发送。

不把 actionType=1 推断为通用更改名称/删除/改封面操作；其他动作未测。

### 3.7 删除自建歌单

`POST /music-userbehavior-service/v1/service/playlist/delete`

```json
{"playListID":"<本人自建歌单ID>"}
```

已测成功，重新拉列表确认临时歌单消失，音乐库 ID 集合恢复基线。

删除前再次检查属于 owned 且由用户确认；测试只能删除本轮返回的新 ID，不能按名称模糊匹配批量删。

### 3.8 华为播放历史（只读）

`GET /music-userbehavior-service/v1/service/recentlyplayed/query?contentType=1`

响应 `contentSimpleInfos[]`，当前样本返回 200 条。未验证任意分页参数，不向 UI 承诺无限历史。

客户端自己的最近播放单独命名为“本机记录”，与华为历史分开显示。

### 3.9 播放历史上报（写入；参数取自公开 H5，尚未真实账号验证）

`POST /music-userbehavior-service/v1/service/recentlyplayed/create`

公开 H5 打包代码里的 `uploadRecentPlay` 构造（唯一证据来源）：

```json
{"createRecentPlayInfos":[{
  "songCode":"<歌曲ID>","contentCode":"<同一歌曲ID>","contentType":"1",
  "songName":"<曲名>","listenType":"1",
  "duration":<本次实际收听秒数>,"createTime":"<UTC yyyyMMddHHmmss>",
  "playCount":1,"songDuration":<整曲秒数>
}]}
```

- 路径在公开 H5 资源列表中真实存在，字段名与顺序来自其压缩代码；**没有用真实账号发起过写请求**，业务码、去重规则和计数口径均未知。
- 客户端只在真实播放达到阈值（默认 30 秒与整曲 50% 中较小者）后上报一次；`duration` 是累计实际收听秒数，不用整曲时长冒充，也不给未真正播放的曲目上报。
- 写入 `retry:false`，不自动重试；失败静默，不影响播放，也不把会话判为失效。设置项 `syncHistory` 可关闭，默认开启。
- 真实账号验收（播放后华为历史是否出现该曲目、是否产生重复记录）仍为 pending；不得以“代码已实现”或“接口 200”代替。

## 4. 搜索、专辑、歌手

### 搜索歌曲

`POST /music-search-service/v10/service/fuzzysearch`

```json
{"contentType":"1","correct":"1","limit":"20","queryWord":"<关键词>","start":"0"}
```

响应 `songSimpleInfos[]`、hasNextPage、totalNum。普通 Windows UA/无设备环境已成功；不强依赖 Android UA。

### 专辑详情

`GET /music-metacontent-service/v1/service/album/detail/byalbumcode`

参数 albumCode、start、limit（copyrightSwitch 可选，首版按已测最少字段）。

响应 `albumInfoEx.albumInfo`、`albumInfoEx.songSimpleInfos[]`。

### 歌手详情

`POST /music-metacontent-service/v2/service/artist/detail/byartistcode`

```json
{"artistCode":"<ID>","songNum":20,"albumNum":20}
```

响应 `artistInfoEx.artistInfo`、songSimpleInfos、albumSimpleInfos。start 分页未验证，首版给有限列表，不造无限滚动接口。

## 5. 曲目归一化

歌曲常见字段：

- contentID → id
- contentName → title
- artistID[] 与 artistName / artistNamePairs → artists
- albumID、albumName / albumNamePair → album
- picture.smallImgURL / middleImgURL / bigImgURL → cover
- contentExInfo（字符串 JSON）中的 completeFileInfos、fileInfos、lyrics 等

不要按逗号随意切 artistName 丢失艺名信息。优先具备 name/id 的数组，无数组时完整字符串作为一个显示名。

completeFileInfos 可以为 key→对象结构，也可能为数组，原研究代码有兼容两种的线索，解析器应处理。quality "1" 作为 standard、"3" 作为 lossless；其它品质保存未知，不暴露未实现按钮。

duration 是秒的实测字段，返回播放响应里的 auditionBegin/End/Time 是毫秒字段，不能混用单位。

`status`、vip、minPrivilege 等不应凭未知数值直接硬判不可播；由实际播放请求错误及验证过的语义决定。

## 6. 播放资源与歌词

### 6.1 取得音频

`POST /music-play-service/v3/service/file/bycontentcode`

```json
{"contentCode":"<歌曲ID>","contentType":"1","qualityType":"1"}
```

无损 qualityType="3"。本人账号已验证不传 publicKey 的样本正常返回可直接解码的资源。

关注：result、fileURL、playQuality、songInfo、fileInfos、auditionTime/Begin/End、type。资源 URL 不写日志或持久化缓存。

**主音频用顶层 fileURL，不盲用 fileInfos[0].fileURL。** 当前样本 fileInfos 有 mediaFileType=JSON、fileType=18，可能是辅助描述，不是音频。

### 6.2 当前证据

- 普通公开样本：MP3 全曲 203.885714 秒，完整 FFmpeg 解码成功。
- 本人会员样本：MP3 258.429388 秒、FLAC 258.378685 秒，大小分别与元数据完全一致，全曲与末尾 seek 解码成功。
- 同一会员样本响应仍有 auditionTime=30000，所以该字段不能单独作为 30 秒截断的依据。
- 这不代表所有会员曲目、单购专辑、区域限制、特殊音质都可播；正式实现需更多样本与失败状态验证。
- 不支持的加密/容器如出现，应明确报不支持，不复用下载工具的解密代码假装通用播放。

### 6.3 歌词与封面

来源优先使用曲目响应中：lyricAddress，或 contentExInfo.lyricAddres（注意历史拼写）、lyrics 中的 lineByLineLyric 等实际字段。字段是 JSON 字符串时先解析。

主进程按 trackId 找已知资源，再下载歌词、解析行级时间戳。不给 renderer 暴露任意远程 fetch。

封面允许直接交给 renderer，需 CDN allowlist 和 CSP。某 kuke HTTP 封面返回 403，同路径 HTTPS 返回正常 JPEG；只对确认可用的路径升级，不全局忽略 TLS 错误。

## 7. 暂不接入的端点

每日推荐 H5 线索：POST /music-recommend-service/v1/service/recommend/querydaily，参数 rcmScenario/scenario/termType/recommendStrategy。本次多个已知变体均 HTTP 400，用户明确允许暂缓。不要把它列为第一版验收，不做假每日推荐替代。

## 8. 证据与回归测试来源

- 当前独立实现：src/auth.mjs、music.mjs，以及 test/。
- 可直接迁移的测试：state/PKCE、单次回调、脱敏错误、本人 ID 约束、写入文件权限；正式版需要安全存储而非沿用明文实验文件。
- 已授权写操作研究脚本：research/test-writes.mjs（不要自动运行）。
- 写操作清理证据：.local/authorized-tests/cleanup-summary.json（只含摘要）。
- 音频解码证据：.local/authorized-tests/member-decode-report.json（只含摘要）。
- 真实原始响应均为私有材料，禁止复制进单元测试；用保持字段结构的人工匿名样本替代。

响应存在不等于权限已确认；接口 000000 不等于最终状态改变；样本解码成功不等于 Electron/所有 OS 已经能正常播放。每层分别验收。
