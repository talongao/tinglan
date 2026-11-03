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

## 4. 建议路线

1. **现在**：把 `source.Adapter` 扩成完整 `Source` 契约，`cmd/music` 不再 import `internal/huawei`（除登录与迁移）。这一步是纯重构，风险低、收益确定。
2. **接第二个源（网易云）时**：仍用进程内注册，验证契约是否真的够用；此时会暴露 Search/Playlist/Lyrics 的差异，比空想更可靠。
3. **只有出现“用户自己装源”的真实需求时**，再上路线 B：
   - 协议 v1：`hello`（协议版本、能力）→ `import/resolve/search/...` → `shutdown`；
   - 插件目录 `~/Library/Application Support/tinglan/native-v1/sources/<id>/`；
   - 登录由宿主代理，插件只拿短期令牌；
   - 首次启用必须显式确认，展示插件路径与签名摘要；
   - 插件崩溃不影响主进程播放。

## 5. 与当前约束的关系

- AGENTS.md 要求“不引入复杂框架、后端服务或云端凭据代理”。路线 B 的宿主代理登录正好踩在这条线上，落地前需要用户明确同意。
- 插件化不改变“只用平台提供的流、不下载/不解密”的边界，但会扩大凭据暴露面，必须写进安全说明。
