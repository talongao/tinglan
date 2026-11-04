# 项目工作规则

- 当前 roadmap：Windows 可用性验证与相关缺陷修复仍是发布门槛；用户已于 2026-10-31 将“多源收藏（华为音乐作为其中一源，首期接入网易云）”纳入 roadmap。第一阶段（华为源 + 多源通用能力）与数据库 v3 实施（SQLite 23 表、收录音乐、歌单、导入 dialog、搜索 tabs、TTML、去 Node）代码已完成并有本地里程碑提交（未推送），macOS 已实机验证登录、播放与插件，Windows 尚未实机验收；网易云适配器暂不实现。不额外扩张范围。日常改动留在 develop；用户已于 2026-10-30 明确授权将 PR #7 合并 main 并发布本次测试版，后续合并和发布仍需用户授权。CI 成功不代替 Windows 实机验收。

- 听澜（Tinglan）是 Go + Viva 原生华为音乐第三方桌面客户端。旧 Electron/React 客户端与工具已移除，不恢复旧技术栈。
- 开工先读 README.md、docs/IMPLEMENTATION_STATUS.md、docs/API_CONTRACT.md、docs/authorized-feature-validation.md。历史方案/验证记录保留作参考，与当前状态冲突时以当前实现和最新用户要求为准。
- 原生代码在 native/（Go 模块根目录，`go.mod` 在这里）；以下命令都在 native/ 下执行：启动 `go run ./cmd/tinglan-tool start`，构建 `go run ./cmd/tinglan-tool build`，测试 `go test ./...`，静态检查 `go vet ./...`，界面回归 `go run ./cmd/tinglan-tool ui-check`。仓库不再依赖 Node.js/npm。
- macOS Apple Silicon 是当前主要实机环境。GitHub Actions 构建 macOS arm64 / Intel x64 DMG 和 Windows x64 NSIS 安装包；Windows 尚未实机验收，Linux 暂不打包，编译成功不能宣称验收。
- 音源以华为音乐为其中一源，按 roadmap 首期接入网易云音乐，其他平台后续评估；客户端只做在线播放，遵守各平台的服务条款与会员/地域限制，不提供下载、离线保存或媒体文件处理功能。
- .local/、research/ 含私人凭据与研究材料，禁止打印、提交、上传、打包。不得主动使用保存的私人会话或执行真实账号写操作；需要当前用户授权。
- 登录与业务请求由 Go 内部模块处理；不关闭 TLS 校验，不抢占全局 hms://，不改系统代理。凭据按用户决定存应用数据目录，不使用钥匙串；音乐会话确认失效后重新登录，不自动续期。
- 技术包名、bundle 标识 `org.tinglan.unofficial.internal.viva` 和用户数据目录 `tinglan/native-v1` 统一来自 `native/internal/appmeta`。旧目录 `huawei-music-desktop/native-v1` 由 `appmeta.MigrateDataDir` 在首次启动时整体迁移，不要手工删除。显示名统一来自 appmeta。
- 收藏总数由完整分页后 ID 去重确认，不能用首屏数量冒充总数。搜索滚动分页，不显示已加载计数/播放全部；搜索点歌加入当前队列，不替换已有队列。
- 歌单：可创建本地歌单、向本地歌单加歌；导入歌单只读、按当前账号快照镜像，切换账号后更新需用户确认覆盖。所有写操作只落在本地库，不写回平台。playlist/detail/all 必须使用真实 playListID，删除用 playlist/delete，不能用 favorite/delete 代替。
- 顶层 playback.fileURL 是音频，fileInfos 可能是 JSON。auditionTime 不能单独判为试听；完整播放按实际资源、权限与解码时长验证。
- 图标由 `go run ./cmd/tinglan-tool icons` 与 `assets` 生成（原 Node 脚本已移除）；矢量源在 native/assets/lucide/（SVG），保留 ISC 与 build/icon.LICENSE.txt 归属。
- 日常开发统一在 develop。main（含管理员）必须通过 PR 合并，Build gate 必须通过，禁止强推和删除。草稿 PR 不运行构建，正式 PR 仅执行单平台代码检查、不打包；develop push 不构建，仅 workflow_dispatch 勾选目标后构建并保留 artifacts，不发布。
- 二进制公开分发仅通过 GitHub Release；main 合并触发三平台构建，全成功后以 v<version>-build.<run_number> 标签发布 Latest，不覆盖历史版本。手动运行仅允许 develop。发布说明必须保留未签名、非官方、OAuth 身份不属于本项目以及未验平台边界。
- 不擅自提交、推送、发布、公开仓库或删除研究现场。合成测试/截图不能当作私人账号证据，真实截图需用户授权。
- 维护简短、可核对的 docs/IMPLEMENTATION_STATUS.md，明确区分实现、离线测试与实机验收。不要引入复杂框架、后端服务或云端凭据代理。
- 外置音源由宿主管理在应用数据目录 `sources/<id>/`；交付清单只写启动配置，能力由握手 descriptor 提供。修改插件接口时同步 `docs/SOURCE_PLUGIN_PROTOCOL.md` 与 `docs/PLUGIN_GUIDE.md`，从指南提取示例做离线握手/搜索验证；脚本插件的可选运行时不属于宿主构建依赖。
