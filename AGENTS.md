# 项目工作规则

- 当前 roadmap：只做 Windows 可用性验证与相关缺陷修复，验收前不增加新功能。所有改动留在 develop；PR #7 保持草稿，不合并 main、不发布新版，直到用户明确确认。CI 成功不代替 Windows 实机验收。

- 听澜（Tinglan）是 Go + Viva 原生华为音乐第三方桌面客户端。旧 Electron/React 客户端与工具已移除，不恢复旧技术栈。
- 开工先读 README.md、docs/IMPLEMENTATION_STATUS.md、docs/API_CONTRACT.md、docs/authorized-feature-validation.md。历史方案/验证记录保留作参考，与当前状态冲突时以当前实现和最新用户要求为准。
- 原生代码在 native/；启动 npm start，构建 npm run build，测试 npm test，静态检查 npm run vet，界面回归 npm run test:native:ui。Go 可执行文件可通过 HWMUSIC_GO 传给构建/启动脚本。
- macOS Apple Silicon 是当前主要实机环境。GitHub Actions 构建 macOS arm64 / Intel x64 DMG 和 Windows x64 NSIS 安装包；Windows 尚未实机验收，Linux 暂不打包，编译成功不能宣称验收。
- 不使用其他音乐平台音源，不依赖手机播放器，不绕过会员/地域/DRM，不实现音频下载或解密。
- .local/、research/ 含私人凭据与研究材料，禁止打印、提交、上传、打包。不得主动使用保存的私人会话或执行真实账号写操作；需要当前用户授权。
- 登录与业务请求由 Go 内部模块处理；不关闭 TLS 校验，不抢占全局 hms://，不改系统代理。凭据按用户决定存应用数据目录，不使用钥匙串；音乐会话确认失效后重新登录，不自动续期。
- 保留 package.json 的 name、bundle 标识和用户数据目录，避免升级丢数据。显示名统一来自 native/app-name.mjs。
- 收藏总数由完整分页后 ID 去重确认，不能用首屏数量冒充总数。搜索滚动分页，不显示已加载计数/播放全部；搜索点歌加入当前队列，不替换已有队列。
- 不提供创建歌单/添加歌曲到歌单入口；已有歌单可浏览，已验证的简介修改/删除逻辑保留。playlist/detail/all 必须使用真实 playListID，删除用 playlist/delete，不能用 favorite/delete 代替。
- 顶层 playback.fileURL 是音频，fileInfos 可能是 JSON。auditionTime 不能单独判为试听；完整播放按实际资源、权限与解码时长验证。
- 图标由 native/scripts/make-icon.mjs 与 generate-assets.mjs 生成；矢量源在 native/assets/lucide/，保留 ISC 与 build/icon.LICENSE.txt 归属。
- 日常开发统一在 develop。main（含管理员）必须通过 PR 合并，Build gate 必须通过，禁止强推和删除。PR 自动验证三个目标；develop push 不构建，仅 workflow_dispatch 勾选目标后构建并保留 artifacts，不发布。
- 二进制公开分发仅通过 GitHub Release；main 合并触发三平台构建，全成功后以 v<version>-build.<run_number> 标签发布 Latest，不覆盖历史版本。手动运行仅允许 develop。发布说明必须保留未签名、非官方、OAuth 身份不属于本项目以及未验平台边界。
- 不擅自提交、推送、发布、公开仓库或删除研究现场。合成测试/截图不能当作私人账号证据，真实截图需用户授权。
- 维护简短、可核对的 docs/IMPLEMENTATION_STATUS.md，明确区分实现、离线测试与实机验收。不要引入复杂框架、后端服务或云端凭据代理。
