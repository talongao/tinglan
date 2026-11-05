# 音源适配器能否做成插件：可行性研究

状态：研究结论，未实现。结论先行：**可以，但不建议现在做，也不要用 Go 的 `plugin` 包**。

## 1. 现状与真正的缺口

`native/internal/source/source.go` 已经定义了跨源契约：

```go
type Adapter interface {
	ID() music.SourceID
	Capabilities() Capabilities
	Login(ctx) (LoginState, error)
	Import(ctx, ImportRequest) (ImportPage, error)
	Resolve(ctx, ResolveRequest) (music.Playback, error)
}
```

问题不在接口，而在**接口没有覆盖 UI 真正用到的能力**。`cmd/music` 仍在直接调用华为客户端：

| UI 功能 | 现在直接调用 |
| --- | --- |
| 搜索 | `huawei.Client.Search` |
| 歌单列表 / 详情 | `Client.Playlists`、`Client.PlaylistDetail` |
| 播放历史 | `Client.History` |
| 专辑 / 歌手页 | `Client.AlbumDetail`、`Client.ArtistDetail` |
| 歌词 | 华为歌词 + 第三方匹配 |

只要这些还写死在 `cmd/music`，换一个源就不是“装个插件”，而是“改一遍 UI”。所以第一步永远是把契约补全成 `Source`（Import / Resolve / Search / Playlists / History / Detail / Lyrics + Capabilities），UI 只认 registry。这一步对多源本身就有价值，与是否做插件无关。

## 2. 三条技术路线

### A. 进程内静态注册（现状）

新的源 = 新 Go 包 + 在 `main` 里 `registry.Register(...)`，随二进制一起编译。

- 优点：零协议成本、零序列化、能直接用 `net/http` 与 WKWebView 登录态、单二进制分发不变。
- 缺点：第三方无法独立加源；加源必须重新发版；一个源的 panic 可能拖垮整个进程。
- 适合：源数量少（2–5 个）、都由我们自己维护。**当前阶段选这条。**

### B. 外置进程适配器（JSON-RPC over stdio）

宿主启动 `sources/<id>/adapter` 子进程，用长度前缀 JSON 或 JSON-RPC 2.0 通信；协议即 `Source` 接口的线格式。

- 优点：跨平台（macOS/Windows 都能用）、可独立升级、崩溃隔离、可用任意语言写。
- 代价：
  - 登录：`WKWebView`/WebView2 在宿主进程里，插件进程拿不到登录态。只能由宿主完成登录后把**短期令牌**交给插件，或插件返回授权 URL、宿主开窗、回调再转交。这会把当前“凭据只在应用数据目录、不落插件”的边界变复杂。
  - 安全：插件能接触音频流地址和账号令牌，等于把信任边界从“我们的代码”扩大到“用户装的二进制”。需要签名/白名单/首次授权提示。
  - 工程：协议版本协商、心跳、超时、子进程回收、Windows 控制台窗口、公证/签名、安装包体积。
- 适合：真的出现第三方源生态时。

### C. WASM（wazero）

纯 Go、无 cgo、跨平台沙箱，看起来很美。

- 但源适配器的核心工作是**带登录态的 HTTPS 请求**和**解析私有 JSON**；WASM 里做 TLS、cookie、重定向、代理都很别扭，性能也差。
- 登录仍然绕不开宿主 webview。
- 适合纯解析型（给一段 JSON 出结构化结果），不适合完整音源。

## 3. Go `plugin` 包：直接排除

- Windows 不支持；
- 要求主程序与插件用**完全相同的 Go 版本、依赖版本、构建参数**，任何一次发版都会让旧插件失效；
- macOS 上还要处理签名与 `-buildmode=plugin` 的加载限制。

对桌面分发是灾难，不考虑。

## 4. 已定方向（2026-11-03 用户确认，取代本文早先的“暂不做外置插件”结论）

1. **先完善契约，优先级最高**：`source.Adapter` 扩成完整 `Source`（Auth/Library/Catalog/Lyrics），
   中立 `music.*` 类型贯穿 UI、队列、详情与歌词；`cmd/music` 不再直接依赖 `internal/huawei`。
   契约见 `docs/SOURCE_CONTRACT.md`。
2. **外置插件走 JSON-RPC 2.0 over stdio**：双向调用，插件负责登录业务，宿主提供受控 WebView、
   二维码展示与不透明会话存储（按插件 ID 隔离）。协议见 `docs/SOURCE_PLUGIN_PROTOCOL.md`。
3. **内置与外置同契约、同 registry**：华为作为内置源提供自己的 Descriptor；插件不能覆盖内置 ID。
4. **网易云暂不实现**：先用合成源验证二维码通道与协议，真实登录留作后续验收项，不记作已通过。

路线 A（进程内注册）仍是内置源与第二阶段网易云的实现方式；路线 B（外置进程）用于用户自行安装的源。

## 5. 与当前约束的关系

- “不引入复杂框架、后端服务或云端凭据代理”依然成立：插件是本地进程，不经过云端的凭据代理；
  会话存储由宿主本地加密管理，插件只拿不透明字节。
- 插件化不改变“只在线播放、不下载/不规避平台限制”的边界；它扩大的是登录态暴露面，
  已写进协议文档的安全边界说明与首次启用确认流程。
- Agent 侧评审记录见 `docs/SOURCE_CONTRACT.md` 与 `docs/SOURCE_PLUGIN_PROTOCOL.md`；
  本轮未实现代码，契约冻结后再按两份文档的顺序改。
