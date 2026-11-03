# 音源契约 v1（草案）

状态：与 Astra 评审合并后的候选稿，待用户确认后冻结。冻结前实现仍以 `native/internal/source` 的现状为准。

本文定义「一个音源是什么、能做什么、怎样失败」，不定义插件进程协议；外置插件的线格式见
`docs/SOURCE_PLUGIN_PROTOCOL.md`。两份文档共享同一套语义，进程内音源直接实现 Go 接口，
外置插件通过 JSON-RPC 实现同一套方法。

## 1. 范围与边界

- 内置音源（华为音乐）与外置插件使用同一套契约、同一个 registry、同一套 UI 路径。
- 客户端只做在线播放。本地收藏、歌单、队列、播放历史由宿主管理，音源不写本地库，也不写回平台。
- 音源负责：登录、导入集合、解析播放资源、搜索、列表/详情、歌词；不负责页面、弹窗、存储策略。
- 音源不是沙箱：插件运行在用户权限下，能接触自己的登录态和音频地址。宿主只保证经宿主 API
  的会话读写按音源隔离，不承诺隔离同用户下的其他进程。
- 不支持的能力必须返回 `unsupported`，不得静默降级、伪造数据或把「不知道」当成「已确认」。

## 2. 中立模型

所有跨边界的数据使用 `native/internal/music` 的中立类型。任何 `huawei.*` 类型都不得出现在
契约、队列、缓存或插件线格式中；临时转换只能存在于迁移适配层。

### 2.1 标识

```go
type SourceID string // 小写 ASCII 字母、数字、连字符，5–40 字符

// TrackRef 是源内的稳定引用。宿主不得把本地规范 ID 当作 TrackRef。
type TrackRef struct {
    Source SourceID `json:"source"`
    ID     string   `json:"id"`     // 源侧 ID，长度 ≤200
}

// EntityRef 覆盖搜索结果里出现的所有实体。
type EntityRef struct {
    Kind   EntityKind `json:"kind"` // track | album | artist | playlist
    Source SourceID   `json:"source"`
    ID     string     `json:"id"`
}
```

约束：

- `SourceID` 只允许 `[a-z0-9-]`，拒绝 `..`、路径分隔符和 Windows 保留名；内置 ID 为 `huawei`。
- 音源 ID 一旦进入本地库、会话命名空间和授权记录就不可替换。同平台的另一个实现必须使用新 ID，
  需要迁移时显式迁移，不能按名称接管既有数据。
- `TrackRef` 的 `ID` 对同一音源必须稳定；账号切换不改变它。

### 2.2 元数据

沿用现有 `music.Track`（`Source`、`ID`、`Title`、`Artists`、`Album`、`CoverURL`、`Duration`、
`Version`），补充：

```go
type Playlist struct {
    Ref         EntityRef `json:"ref"`
    Name        string    `json:"name"`
    Description string    `json:"description,omitempty"`
    CoverURL    string    `json:"coverUrl,omitempty"`
    TrackCount  int       `json:"trackCount,omitempty"`
    Kind        string    `json:"kind,omitempty"` // owned | collected | chart | other
    UpdatedAt   string    `json:"updatedAt,omitempty"`
}

type Lyrics struct {
    Format string `json:"format"` // lrc | ttml | txt
    Text   string `json:"text"`
    Role   string `json:"role,omitempty"` // original | translation | romanization
}
```

- `Duration` 允许为空。宿主不得把「接口没给时长」显示成 `0:00`；列表不展示时长列。
- `Access` 保持三态：`unknown` / `full` / `preview`。没有证据时必须 `unknown`。
- `Version` 参与同录音判断，必须由音源按标题/专辑/版本标记填写，不能留空后由宿主猜。

### 2.3 分页

```go
type PageRequest struct {
    Limit  int    `json:"limit"`
    Cursor string `json:"cursor,omitempty"`
}

type Page[T any] struct {
    Items      []T    `json:"items"`
    NextCursor string `json:"nextCursor,omitempty"`
    HasMore    bool   `json:"hasMore"`
    // Complete 只对导入快照有意义：true 表示本次遍历完整结束、可用于覆盖。
    Complete bool `json:"complete,omitempty"`
}
```

不变量：

- `Cursor` 对宿主不透明，宿主不得自行累加 offset 或推断页大小。
- `HasMore=true` 时 `NextCursor` 必须非空且与上一页不同；`HasMore=false` 时为空。
- 分页中断、解析丢项、翻页异常必须返回错误或 `Complete=false`，不得发布覆盖。
- 详情中的曲目列表也是分页结果，`limit` 由音源在 `Capabilities.MaxPageSize` 内决定。

### 2.4 枚举归属

| 枚举 | 取值 | 出现在 |
| --- | --- | --- |
| `Collection.Kind` | `favorites` \| `playlist` | `ImportRequest.Collection` |
| `Playlist.Kind` / `PlaylistKind` | `owned` \| `favorite` \| `chart` | `music.Playlist`、`EntityRef.PlaylistKind`、`Collection.PlaylistKind` |
| `PlaylistRequest.Kind` | 同上，空表示源默认 | `Catalog.Playlists` |
| `AccessRequirement` | 空 \| `anonymous` \| `login` \| `member` | 详情结果 |
| `AuthState` | `signedOut` \| `signedIn` \| `expired` \| `unknown` | `AuthStatus` |
| `Access` | `full` \| `preview` \| `unknown` | 播放结果 |

- 同一概念只有一套枚举。`favorite`（我收藏的歌单）与 `favorites`（收藏歌曲集合）不得混用。
- 歌单必须在「详情」与「导入」两处都携带 `Kind` + 远端 ID；只带 ID 时按源默认处理，不得猜。

## 3. Source 接口

```go
type Source interface {
    ID() SourceID
    Describe() Descriptor
    Auth() AuthSource
    Library() LibrarySource
    Catalog() CatalogSource
    Lyrics() LyricsSource
    Manage() ManageSource
}
```

- 所有访问器必须非 nil；不支持的方法返回 `unsupported`。
- 实现必须并发安全，不得从后台调用直接操作 UI。
- `Descriptor` 返回不可变快照，宿主与 UI 只读。

```go
type Descriptor struct {
    SchemaVersion int          `json:"schemaVersion"`
    ID            SourceID     `json:"id"`
    Name          string       `json:"name"`
    ShortName     string       `json:"shortName"` // ≤4 字，用于小角标
    Icon          IconRef      `json:"icon,omitempty"`
    Accent        string       `json:"accent,omitempty"`
    Capabilities  Capabilities `json:"capabilities"`
}
```

### 3.1 写操作（ManageSource）

收藏、队列与歌单属于本地数据，宿主自己写本机库；只有“源侧副作用”进入契约：

```go
type ManageSource interface {
    ReportHistory(ctx context.Context, track music.Track, listenedSeconds int) error
    UpdatePlaylistDescription(ctx context.Context, ref EntityRef, description string) error
    DeletePlaylist(ctx context.Context, ref EntityRef) error
}
```

- 三个方法只改平台侧数据；本地收藏/歌单/队列的增删改永不经过 `ManageSource`。
- 未登录或会话缺失必须返回 `login`，不得静默成功；源不支持时返回 `unsupported`。
- 写操作与导入共用同一份账号代次校验：调用时携带的会话若已过期，宿主先重新 `Verify`，不能用旧会话写回。

### 3.2 Capabilities

```go
type Capabilities struct {
    Search      bool `json:"search"`
    Playlists   bool `json:"playlists"`
    PlaylistDetail bool `json:"playlistDetail"`
    AlbumDetail bool `json:"albumDetail"`
    ArtistDetail bool `json:"artistDetail"`
    History     bool `json:"history"`
    Lyrics      bool `json:"lyrics"`
    Import      bool `json:"import"`
    Resolve     bool `json:"resolve"`

    Collections  []Collection `json:"collections,omitempty"`
    MaxPageSize  int          `json:"maxPageSize,omitempty"`
    LoginKinds   []LoginKind  `json:"loginKinds,omitempty"`   // webview | qr | none
    LoginRequired []string    `json:"loginRequired,omitempty"`// search | import | resolve | history
    Qualities    []string     `json:"qualities,omitempty"`    // standard | lossless
}
```

- 能力是静态的，登录态不写进能力；「未登录」不能用来关闭公开搜索。
- `LoginRequired` 只是 UI 入口提示，不是调用前的硬门禁：同一操作（例如 `PlaylistDetail`）可能对公开歌单匿名可读、对私人歌单需要登录，
  因此实体级访问要求必须由结果携带（见 6.1 `AccessRequirement`），未知时允许发起请求并按 `ErrAuthRequired` 处理。
- 运行时能力可以少于 manifest 声明；申请额外宿主服务或资源域必须拒绝，不能静默扩大授权。

## 4. 登录与账号（AuthSource）

```go
type AuthSource interface {
    Status(ctx context.Context) (AuthStatus, error)  // 本地快照，不联网
    Verify(ctx context.Context) (AuthStatus, error)  // 显式联网校验
    Login(ctx context.Context, request LoginRequest) (AuthResult, error)
    PersistSession(ctx context.Context, request PersistRequest) (bool, error)
    Logout(ctx context.Context) error
}

type AuthState string // signedOut | signedIn | expired | unknown

type AuthStatus struct {
    State          AuthState `json:"state"`
    AccountKey     string    `json:"accountKey,omitempty"` // 稳定、非凭据
    DisplayName    string    `json:"displayName,omitempty"`
    LastVerifiedAt string    `json:"lastVerifiedAt,omitempty"`
    ExpiresAt      string    `json:"expiresAt,omitempty"`
    // Ephemeral 表示会话只在本次运行内存中，未持久化。
    Ephemeral bool `json:"ephemeral,omitempty"`
}

type LoginRequest struct {
    Kind       LoginKind `json:"kind"` // webview | qr
    AttemptID  string    `json:"attemptId"`
    WriteToken string    `json:"writeToken,omitempty"` // 宿主签发，绑定本次 attempt
}

type PersistRequest struct {
    OperationID string `json:"operationId"`
    WriteToken  string `json:"writeToken"` // 宿主签发的一次性令牌
}

type AuthResult struct {
    Status    AuthStatus `json:"status"`
    Succeeded bool       `json:"succeeded"` // 登录业务成功且会话可用（允许仅内存）
    Persisted bool       `json:"persisted"` // 会话已写入宿主存储
}
```

语义：

- `Status` 不联网，只返回本地缓存快照；`Verify` 才联网校验。
- 第一阶段把现有的「启动恢复校验 + 每五分钟校验」改成调用 `Auth.Verify`（调度与时机不变，
  只替换直连 `huawei.Client.ValidateSession` 的调用点）；因此 `cmd/music` 去直连与保留周期校验可以同时完成。
- 新增周期性策略（退避、失败降级等）放到第二步，不改变这一阶段的行为。
- 状态判定：只有音源明确判定失效才是 `expired`；仅本地存有凭据、尚未验证是 `unknown`，
  必须保留凭据并允许业务请求，**不得把 `unknown` 映射成 `expired` 或强制跳登录**。
- `Succeeded=true` 表示登录业务成功且会话可用；`Persisted=false` 表示会话只在内存中，
  宿主必须把状态标为「仅本次运行有效」并持续显示，正常退出时说明不会保留。
  持久化失败不得触发重新认证；重试保存成功后转回已持久化。
- `AccountKey` 在同一账号重新登录、插件升级后保持稳定；不同账号必须不同。它参与导入的代次保护，
  不能只用「已登录」布尔值。
- 每源只保留一个当前账号。账号代次由宿主管理：登录、退出、切号都递增代次，旧请求结果作废。
- `Login` 是长调用，过程中通过宿主服务展示 WebView 或二维码；只有返回 `Succeeded=true` 才算登录成功。
  二维码展示成功不等于登录成功。
- 会话写入必须持写令牌，且令牌只能由宿主签发并随调用下传（`Login` 参数或 `PersistSession` 参数），
  插件没有申请入口。登录令牌绑定单次 attempt，取消/超时/结束即失效；重试保存由宿主发起一次性
  `PersistSession` 调用，不重新登录；账号代次变化使所有未使用令牌失效。
- `Logout` 只清除本地登录状态，不隐含远端账号操作；退出后该源所有绑定标记为不可用，但本地曲库与收藏保留。
  退出时由宿主直接清空该源的会话命名空间（宿主撤销授权），不需要插件持删除令牌。
- 登录逻辑（state/PKCE/回调校验/换取业务会话）由音源负责；宿主不解析平台令牌。

### 4.1 进程内注入

内置源不通过 RPC，直接以 Go 接口获得宿主服务，构造关系如下（禁止适配器反向依赖 `cmd/music` 或 UI 包）：

```go
type Host interface {
    Session() SessionStore
    LoginUI() LoginUI
    Progress() ProgressSink
    Log() Logger
}

type SessionStore interface {
    Get(ctx context.Context, key string) (value []byte, revision int64, exists bool, err error)
    Set(ctx context.Context, key string, value []byte, ifRevision int64, writeToken string) (int64, error)
    Delete(ctx context.Context, key string, ifRevision int64, writeToken string) error
    List(ctx context.Context) ([]string, error)
}

type LoginUI interface {
    WebView(ctx context.Context, request WebViewRequest) (WebViewResult, error)
    PresentQRCode(ctx context.Context, request QRRequest) error
}
```

- 进程内源与插件使用同一套登录请求结构；`webauth` 只依赖 `Flow` 接口（`AuthorizeURL/Accept/Cancel/IsCallback/AllowedURL`），
  不再引用任何平台的 `Attempt` 类型。
- `Host` 由 `cmd/music` 装配后注入适配器构造函数，适配器只依赖 `source` 包定义的接口。
- 内置源的交互式登录用 `builtin.Interactive` 扩展（`SignIn(ctx) (*Session, error)` 与
  `CheckSession(ctx, *Session) (*Session, error)`）向上暴露；它不是 `Source` 的通用方法，
  插件侧的等价能力是 `source.auth.login` + 宿主 `host.webview`，两者行为必须一致：
  浏览器流程、回调校验、换取会话都由源自己完成，宿主只提供窗口、存储与取消。
- 会话在磁盘上统一为 `source.Session{Source, AccountKey, Token, UserID, ExpiresAt}`；适配器内部可以继续用平台结构，
  但必须以 `NeutralSession/ProviderSession` 之类的转换函数作为唯一边界，宿主与 UI 不得接触平台结构。

## 5. 导入与解析（LibrarySource）

```go
type LibrarySource interface {
    Import(ctx context.Context, request ImportRequest) (ImportPage, error)
    Resolve(ctx context.Context, request ResolveRequest) (Playback, error)
}

type ImportRequest struct {
    Collection Collection `json:"collection"`
    RunID      string     `json:"runId"` // 宿主生成，用于关联本次导入
    Cursor     string     `json:"cursor,omitempty"`
    Limit      int        `json:"limit,omitempty"`
}

type ImportPage struct {
    Tracks   []music.Track `json:"tracks"`
    Next     string        `json:"nextCursor,omitempty"`
    HasMore  bool          `json:"hasMore"`
    Complete bool          `json:"complete"`
    // RemoteDeleted 仅在音源确认远端集合已被删除时非空，值为安全证据说明
    // （例如平台返回的明确“不存在”业务码）。403、空列表、切号不可见不得填写。
    RemoteDeleted string `json:"remoteDeleted,omitempty"`
}

type ResolveRequest struct {
    Track   music.Track `json:"track"`
    Quality string      `json:"quality,omitempty"`
}
```

- 每次 `Import` 只返回一页。宿主拥有 run、暂存、取消与最终提交；音源不能决定写本地库。
- `Complete` 只允许在整次遍历正常结束时为 true。宿主只有在账号代次、目标代次和 run 状态都匹配时，
  才用一个短事务发布；失败/取消不删除任何本地数据。
- 收藏导入与导入歌单更新共用同一套暂存—发布流程；歌单更新不得边翻页边替换成员。
- 远端没有快照版本时只能承诺「本次完整遍历」，不能承诺原子快照。
- `ResolveRequest.Track` 里的元数据只作校验依据，定位资源必须用 `TrackRef`（即 `Track.Source`+`Track.ID`）。
  `Resolve` 返回的 `Playback.URL` 只在线交给播放器：不落盘、不写日志、不入库、不跨播放复用。
- 需要 Cookie、自定义鉴权头、DRM 或特殊流格式的资源，v1 明确 `unsupported`，不得偷偷加通用代理。
- 播放地址过期允许在当前播放流程内重新 `Resolve` 一次；CDN 拒绝不能直接解释为账号失效。

### 5.1 远端删除

- `ImportPage.RemoteDeleted` 是唯一允许触发「删除本地只读副本」的信号，且必须伴随音源确认证据。
- `ErrNotFound` 不得直接触发删除；403、切换账号后不可见、空列表、普通 404 都不能升级为「已删除」。
- 宿主删除时同时清理 `playlist_tracks` 与本地歌单行，并把 `imports.status` 标记为 `deleted`；收藏行保留。

## 6. 目录与详情（CatalogSource）

```go
type CatalogSource interface {
    Search(ctx context.Context, request SearchRequest) (Page[music.Track], error) // v1 只返回歌曲
    Playlists(ctx context.Context, request PlaylistRequest) (Page[music.Playlist], error)
    PlaylistDetail(ctx context.Context, request EntityRequest) (PlaylistDetail, error)
    AlbumDetail(ctx context.Context, request EntityRequest) (AlbumDetail, error)
    ArtistDetail(ctx context.Context, request EntityRequest) (ArtistDetail, error)
    History(ctx context.Context, request PageRequest) (Page[music.Track], error)
}

type SearchRequest struct {
    Query  string `json:"query"`
    Cursor string `json:"cursor,omitempty"`
    Limit  int    `json:"limit,omitempty"`
}

type PlaylistRequest struct {
    Kind   string `json:"kind,omitempty"` // owned | favorite | chart；空表示该源默认
    Cursor string `json:"cursor,omitempty"`
    Limit  int    `json:"limit,omitempty"`
}

type EntityRequest struct {
    Ref    EntityRef `json:"ref"`
    Cursor string    `json:"cursor,omitempty"`
    Limit  int       `json:"limit,omitempty"`
}

type AccessRequirement string // "" | anonymous | login | member

type PlaylistDetail struct {
    Playlist music.Playlist  `json:"playlist"`
    Tracks   []music.Track   `json:"tracks"`
    Next     string          `json:"nextCursor,omitempty"`
    HasMore  bool            `json:"hasMore"`
    Access   AccessRequirement `json:"access,omitempty"`
}

type AlbumDetail struct {
    Album   music.Album    `json:"album"`
    Tracks  []music.Track  `json:"tracks"`
    Next    string         `json:"nextCursor,omitempty"`
    HasMore bool           `json:"hasMore"`
    Access  AccessRequirement `json:"access,omitempty"`
}

type ArtistDetail struct {
    Artist      music.Artist  `json:"artist"`
    Tracks      []music.Track `json:"tracks"`
    Albums      []music.Album `json:"albums"`
    NextTracks  string        `json:"nextTracksCursor,omitempty"`
    NextAlbums  string        `json:"nextAlbumsCursor,omitempty"`
    HasMoreTracks bool        `json:"hasMoreTracks"`
    HasMoreAlbums bool        `json:"hasMoreAlbums"`
    Access      AccessRequirement `json:"access,omitempty"`
}
```

- 所有列表操作都接受请求对象（引用、cursor、limit），不再有无参数签名。
- **v1 的 `Search` 只返回歌曲**；多实体搜索是后续版本，`SearchResult` 与 `EntityRef` 先只服务详情与歌单引用。
- 搜索结果必须保留源身份（`TrackRef`），宿主不得把它转回某个具体平台类型。
- 歌单详情与导入引用必须同时带 `Playlist.Kind` 与远端 ID，否则无法区分「我创建的 / 我收藏的 / 榜单」。
- `ArtistDetail` 有两个分页序列：请求必须用 `EntityRequest.Section`（`tracks`|`albums`）指明本次翻哪一组，
  cursor 只对同一 section 有效；不能用一个 cursor 同时翻两组。
- 源侧预计算、无法继续分页的有限列表必须显式返回 `Truncated=true`，不能假装 `HasMore=false` 就是完整。
- 详情中的曲目与专辑分别分页；一次性返回的有限列表必须明确声明。
- `AccessRequirement` 由结果携带；UI 用它在详情页给出「登录后可查看完整内容」，不能用能力位猜。
- `History` 只读平台历史，不提供上报接口。宿主自己的播放历史由本地管理，旧华为历史上报路径在迁移时退出。

## 7. 歌词（LyricsSource）

```go
type LyricsSource interface {
    Lyrics(ctx context.Context, ref TrackRef) ([]Lyrics, error)
}
```

- 返回格式标识（lrc/ttml/txt）与角色（原文/翻译/音译），宿主负责解析与渲染。
- 首版让音源自己获取歌词，宿主不拿音源会话去抓歌词。
- 没有歌词返回 `NotFound`，不要返回空内容伪装成有歌词。

## 8. 错误

Go 侧保留 `source.Error{Source, Op, Kind, Err}` 与 `errors.Is/As`。`Kind` 至少包含：

```text
login            登录失效或需要登录
permission       账号权限/会员/地域限制（与登录失效分开）
notfound         资源不存在（不自动等于远端删除）
ratelimit        限流，可带 retryAfterMs
transient        网络/临时故障
unsupported      该源不支持此能力
canceled         调用方主动取消
timeout          超时
protocol         响应不符合契约/协议错误
unavailable      音源进程不可用或已崩溃
conflict         状态冲突（代次过期、账号不匹配）
```

- 线格式使用固定错误码 + `data.kind`，可带 `retryAfterMs` 与安全的诊断码；不传 Go 错误链，
  不把平台原始响应当 message，不包含凭据、URL 或个人信息。
- `Retryable` 只对 `transient`/`ratelimit` 为真；`protocol`/`unavailable` 走实例恢复流程，不自动重放登录与导入。

## 9. Registry 与内置源

- 宿主维护唯一 `Registry`，内置源与插件源进入同一张表；重复 `ID` 直接拒绝注册。
- `Registry` 顺序只用于：UI 展示顺序、默认源、导入时的源选择。**它不参与播放绑定查找**（见 §10.1）。
- 内置源也实现完整 `Source` 并暴露 `Descriptor`；UI 不再硬编码 `sourceLabel()` 与「华」角标。
- 资源策略：`Descriptor` 增加 `ResourceDomains []string`，内置源与插件 manifest 使用同一套域名策略，
  播放与封面 URL 必须属于该源的资源域；跨源 URL 一律拒绝。

## 10. 宿主职责

- 装配 `Registry`，向适配器注入 `Host`（见 4.1），持有账号代次、导入 run 与本地库。
- 解析播放：只接受 `TrackRef` 定位；播放地址不落盘、不写日志。
- 网络职责按请求归属划分：**源自己发起的平台业务请求由源负责**（含代理、TLS、重试）；
  宿主负责自己发起的媒体流、封面加载与登录窗口请求。适配器不得关闭 TLS 校验或改系统代理。
- 任何「远端删除」必须由适配器确认（5.1），宿主不得自行推断。

### 10.1 播放解析顺序

1. **有本地收录身份**（canonical track 存在且有有效绑定）：按绑定 `import_seq` 升序尝试，遵守 `onlySource`；
   访问级别优先完整，全部为试听时才降级为试听。
2. **无本地收录身份、但条目带 `TrackRef`**（搜索结果、临时队列项）：只交给该引用指定的源解析，失败即报错，不跨源探测。
3. **有本地收录身份但没有任何有效绑定**（例如全部绑定被禁用）：返回「无可用音源」，
   即使条目上残留旧 `TrackRef` 也不得绕过绑定状态直接使用；只能提示重新登录或重新导入。
4. **既无本地身份也无 `TrackRef`**：返回「无法定位音源」，不做 registry 猜测。

访问级别排序：完整 > 未知 > 试听。`unknown` 优先于 `preview`，不能因为源没给证据就降级成试听。

「未入库」和「没有源身份」是两件事：前者可有绑定，后者只能靠 `TrackRef`。

## 11. UI 契约

- 音源状态三态展示：未登录 / 已登录 / 可能已失效（`unknown` 不显示为失效，也不强制跳登录）。
- 会话仅存内存时（`Ephemeral`）持续显示「仅本次运行有效」，退出流程提示不会保留。
- 收藏/歌单/搜索行显示来源角标；角标来自 `Descriptor`（图标或 `ShortName`），不硬编码平台名。
- 「加入歌单」「收藏」只写本地；来源更新只改绑定，不移除本地收藏。
- 导入对话框区分：从未导入 / 未登录 / 可更新 / 已删除（`deleted` 显示「导入」而不是「更新」）。
- 失败提示必须带可执行动作（重试、去登录、稍后再试），不能只显示错误码。

## 12. 实施顺序与验收

1. **契约类型 + Host 接口 + 适配器骨架**：只加类型与编译期断言，不改行为。
2. **四处先定清**：账号状态（Status/Verify/Ephemeral）、详情 DTO、Host 注入、播放顺序（10.1）。
3. **迁移调用链**：`cmd/music` 改为只经 `Registry`/`Source`，删除对 `huawei.Client` 的直接调用；
   每迁一处跑 `go test ./...`，行为不变（用户可见差异为零）。
4. **资源层**：`resource.New()` 去掉华为硬编码，改用 `Descriptor.ResourceDomains`。
5. **外置 RPC**：协议文档冻结后再实现插件宿主。

验收：`cmd/music` 中 `huawei.` 引用仅存在于 `internal/huawei` 与装配处（登录恢复与周期校验经 `Auth.Verify`，不再是例外）；搜索/收藏/歌单/播放全链路可跑；
`go build ./...`、`go vet ./...`、`go test ./...`、`tinglan-tool ui-check` 全绿；Windows 未实机验证前不得宣称验收。

## 12.1 迁移状态（2026-11-03）

- 第一步已完成：`cmd/music` 不再 import `internal/huawei`，搜索/详情/歌单/历史/封面/写操作/错误分类全部经 `Source`/`Registry`；
  登录经 `builtin.Interactive.SignIn/CheckSession`；会话在磁盘上的格式不变（仍由 `huawei.SealSession/OpenSession` 加解密）。
- 导入与播放解析已迁到契约：`canonical.Library.Import(ctx, src source.Source, collection source.Collection, ...)`
  逐页调用 `src.Library().Import`，`ImportRequest.RunID` 贯穿整次快照；`playback.Resolver` 的 `Lookup` 改为
  `Source(music.SourceID) (source.Source, bool)`，绑定查找只走 `src.Library().Resolve`。
- 旧 `source.Adapter`、`Capabilities` 旧接口、`LoginState`、`source.Search`/`SearchPage` 与 `Registry` 的 `Adapter` 桥已删除；
  `Registry` 只维护 `Source` 表（`RegisterSource/Source/SourceList/Order/Reorder`），UI 与导入都经 `Source`。
- `ImportPage.RemoteDeleted` 已加入契约，供导入在正常跑完且源确认删除时标记远端删除；Huawei 适配器暂不产生该标记。
- 华为源同时存在两条装配路径：默认发行版经外置插件协议（`native/cmd/huawei-source`，见协议文档 §16.1）；
  `go run`/单测可用 `cmd/music` 的 `devSource`（`builtin.Huawei`）进程内回退，二者走同一个 `Source` 接口。

## 13. 已定与后续

本轮已定（C1–C10）：会话值为不透明字节（单值 1 MiB / 总量 8 MiB / 64 key），宿主加密落盘；
WebView 只做受限 Cookie 预置，不提供 `evaluateJavaScript`；插件安装在应用数据目录；
角标优先图标、不足 16px 用 `ShortName`；协议版本取交集、无交集拒绝启用；插件崩溃不自动重启；
内置源与插件同一 Registry 与 Descriptor；v1 不提供 `host.http`；宿主内部使用中立 `music.*` 类型。

后续待定：

- 网易云接入时间与登录方式（纯 HTTP 扫码 + 宿主二维码展示）——作为第二个源先做进程内实现。
- 外置插件的分发形式（应用内安装目录、签名与摘要校验、升级重新确认）在协议文档中定稿。
- 华为是否真的需要预置 Cookie：现有代码只证明需要回调 URL，接口支持但以验证结果为准。
- 周期性联网校验（现为每 5 分钟）挪到第二步，第一步保持行为不变。
