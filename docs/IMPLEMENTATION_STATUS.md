# 实现状态

Windows WebView2 测试包（2026-10-30）：提交 70d00f4 已推送 develop，手动仅 Windows 构建 41030036111 全部成功。Windows 真实 WebView2 窗口的 302/JS/弹窗回调、取消、关闭、网络失败及重复登录合成测试通过；全模块测试、vet、UI、NSIS 打包、静默安装及中文快捷方式校验通过。产物 tinglan-windows-x64-unsigned，安装包 tinglan-0.0.2-x64-setup-unsigned.exe（约 4.7 MiB）已下载并核验 SHA256。未合并 main、未发布 Release；真实华为扫码与用户设备体验仍待验。覆盖下方“未推送/未运行”的实施时状态。

Windows WebView2 登录试验（develop，2026-10-30）：Windows 的 Authorize 入口改为专用 WebView2 窗口，保留原 hms 回调、state/PKCE/单次消费，不枚举浏览器或注册全局协议。窗口使用独立临时 profile，关闭/取消/完成后释放 controller 并重试清理；拦截导航与新窗口、拒绝设备权限和下载，缺 Runtime 时明确提示。macOS 系统授权会话不变。使用 WebView2 Go loader 与 COM 绑定（不是引入 Wails UI），已补许可证收集。macOS 全模块测试/vet、Windows amd64 登录模块与测试交叉编译/vet 通过；新增 Windows 专属真实 WebView2 合成回归（302、JS、弹窗、取消、关闭、网络失败、重复登录）已接入手动构建步骤，尚未运行，真实扫码仍待用户验收。当前仅本地改动，未推送、未触发 CI、未合并或发布。

回调地址实测（2026-10-30，develop）：使用当前 client_id、随机 state/PKCE、全新无登录 Chromium context，仅替换 redirect_uri 请求公开授权页。`hms://redirect_uri` 进入华为扫码登录页；`tinglan://oauth/callback` 与 `tinglan:/oauth2redirect` 均返回 error=1101/sub_error=20022（invalid redirect_uri）；`http://127.0.0.1:8765/callback` 与 `http://127.0.0.1:49152/callback` 均返回 error=1101/sub_error=20023（redirect_uri not registered）。实际执行了页面 JavaScript，不以首个 HTTP 200 判成功。复测脚本为 `native/scripts/probe-oauth-redirects.mjs`，需本地 playwright-core 与 Chromium（可通过 HWMUSIC_AUTH_BROWSER 指定），不进 CI、不读私人会话、不扫码、不兑换令牌。此结果仅覆盖当前 OAuth 身份与上述地址；保留现有自动回调流程，未注册系统协议、未合并或发布。

登录流程纠正（develop）：撤回不可用的“默认浏览器 + 手动粘贴”方案，移除手动回调 UI。两端统一为授权窗口自动回调；macOS 保持系统 AuthenticationServices，Windows 使用隔离的 Edge/Chrome 窗口。修正 Edge 私密模式参数、等待 CDP 初始化完成再导航，导航失败及时结束并提示；Windows-only Actions 41022042185 已通过真实浏览器窗口的本地合成重定向、JS、取消及加载失败测试，并完成测试/vet/34 项 UI/打包/安装校验。本机 macOS 全模块测试、vet、34 项 UI 和 Chromium 合成测试通过。实际 Windows 华为扫码仍待验；未合并 main、未发布 Release。

Windows 安装修复验证：手动 Windows-only Actions 41015868533 成功。测试/vet/UI、GUI subsystem 与 exe 内嵌资源检查、NSIS 打包、静默安装、中文“听澜.lnk”及目标文件哈希检查全部通过。先前安装检查失败源于 WScript 快捷方式读取返回空目标；改用 IShellLinkW/IPersistFile 后，目标与实际安装路径一致。产物 tinglan-windows-x64-unsigned 仅保留在 Actions；默认浏览器真实扫码、系统栏图标视觉与正常启动体验仍待用户验证，未合并/发布。

CI 触发纠正：此前 develop 虽无 push 触发，但打开的 PR 会间接启动三平台构建。现调整为草稿 PR 全部跳过，正式 PR 仅 macOS 单平台测试/vet，不打包；develop 安装包只能手动勾选平台生成，默认全部不选。main 合并后的三平台打包/发布保持不变。

Windows 用户反馈修复（2026-10-30，develop）：安装脚本明确 UTF-8 编码；exe 改 GUI subsystem 并嵌入应用 ICO，安装/卸载器使用同一图标。Windows 登录改默认浏览器，不再自动启动独立 Edge/Chrome；暂用手动粘贴 hms 回调，不注册全局协议，真实授权仍待用户验证。会话恢复与主动登录状态分离，首次安装不再显示“取消登录”，显式登录可取消旧恢复任务。新增本地回归通过；新增 CI PE/资源校验和隔离目录静默安装/中文快捷方式检查，CI 结果待核验。

当前优先级（用户最新决定）：下一步仅验证 Windows 可用性并修复相关缺陷，暂停新功能。代码留在 develop；PR #7 转为草稿，未启用自动合并。不合并 main、不发布新版本，待用户实机验证并明确确认。Windows 验收清单：安装/启动/卸载、登录/恢复/过期退出、库与搜索、MP3/FLAC/进度/切歌、歌词、托盘隐藏后续播、媒体控制。当前仅 CI 和安装包校验通过，上述实机项均 pending。

Windows CI 验证（2026-10-30）：PR #7 的 Actions 41006196146 三目标均通过，Windows x64 的全模块单元测试、vet、34 项 headless UI 检查、NSIS 打包均成功。安装包 tinglan-0.0.2-x64-setup-unsigned.exe 已下载核验 SHA256。修复 MSYS2 安装路径、字体枚举回调指针类型，以及窗口/字体弹窗同名“关闭”导致自动化误点的问题。未跳过失败检查；PR 构建没有发布 Release。Windows 真实安装、登录、音频输出和托盘操作仍未实机验收。

分支/CI 调整（2026-10-30）：按用户最新要求启用 develop 开发、main PR-only（管理员同样受限，禁止强推/删除，要求 Build gate）。main 合并自动构建 macOS arm64/x64 与 Windows x64 并发布唯一构建标签 Latest；develop 只手动选择目标并保存 artifacts。Windows CI 已通过（见下方证据）；安装与播放实机验证仍待完成，Linux 不打包。此前仅 macOS、版本标签触发的记录为历史配置。

更新时间：2026-10-30。最新凭据决策：用户选择应用数据目录存储，覆盖本文较早的钥匙串/固定签名记录。用户要求用 Viva 迁移，覆盖历史“不换 Go”规则。默认启动已切换 Viva；2026-10-30 已移除旧 Electron 源码与工具，原用户数据保留。没有访问私人会话、扫码、真实写操作、提交、推送或发布。

发布流程更新（2026-10-30）：两架构 Actions 构建均已成功。仓库改名为 talongao/tinglan，About 已更新。新增 v* 标签发布 macOS 预发布 Release，合并并校验两架构 SHA256；普通分支仍只上传 artifacts。版本升级为 0.0.2，应用 Info.plist 从 package.json 读取版本。

最新纯原生整理（2026-10-30）：

- verified（离线）：搜索兼容布尔/数字/字符串分页标记，使用原始页游标并去重，重复页停止请求；滚动接近底部加载，失败显式重试。搜索去掉数量与播放全部；点歌加入当前队列并播放，已有歌曲不重复添加。新增分页、队列与末行可达性回归通过。
- 按用户要求移除创建歌单入口，不提供添加歌曲到歌单；已有歌单浏览、简介修改与删除保留。README 明确边界并保留真实截图及 LINUX DO。
- 旧 Electron/React 源码、测试、构建配置、依赖与 out/main、out/preload、out/renderer 已移出工作树；本机隔离备份在 ~/.delete-quarantine/tinglan-native-20261030。Node 仅剩构建脚本及可选图标栅格化开发依赖。Lucide 矢量数据与 ISC 许可随原生源码保存。
- CI 仅配置 macos-15（arm64）与 macos-15-intel（x64），产出独立 DMG/SHA256SUMS artifacts，不发布 Release，Windows/Linux 暂停打包。已核对官方 runner 标签；Intel 运行仍 pending。
- verified（macOS arm64）：全模块 Go 测试、player/huawei/UI race、vet、34 项合成 UI 布局、包白名单/依赖许可/SHA256、DMG hdiutil 校验通过。产物 release/native/darwin-arm64/tinglan-0.0.1-arm64-unsigned.dmg。未重启用户当前实例，未做私人账号联网验证，未推送或发布。

最新视觉与缓存调整（2026-10-29，覆盖此前三块卡片方案）：

- 修正内联翻译遗漏：`英文^中文译文`、CJK 对照文本、`原文^音译^翻译` 均拆为副行，同时间戳重复译文去重。逐字标记仅留原句，译文按小字号下排；解析测试及 1180/960 宽度英文翻译布局检查通过，歌词/UI 测试与 vet 通过，Tinglan macOS ARM64 包已重新构建。

- 英文品牌改为 Tinglan：执行文件 `tinglan`、本地 `Tinglan.app`、安装包 `tinglan-*.dmg/exe/tar.gz`，启动与包校验同步。原 package.name、账号数据目录、bundle/注册表兼容标识保留。独立产物使用 `out/native/tinglan/<platform>-<arch>`，旧产物未删除；macOS ARM64 新命名包验证通过，Windows/Linux 未实机打包。
- 内存口径纠正：965 首合成元数据单独保留堆约 224800 bytes，JSON 225811 bytes；不是此前 184/141 MiB 的来源。封面磁盘按需加载已实施。文字缓存改为 LRU 逐项淘汰（6 MiB/256 项），避免满额整批重建；AVFoundation Range worker 加每块 autoreleasepool，避免临时 NSData 随整段请求保留。
- 同机新旧构建 965 首启动 3/6/9 秒 RSS：旧 137600/137872/137824 KiB，新 138992/138880/138784 KiB，无明显下降。新构建 vmmap footprint 109.6 MiB、peak 163.9 MiB，IOSurface resident 28.0 MiB、malloc zone resident 21.6 MiB；此数据不是长期播放结果，用户 280 MiB 尚未复现和归因。证据在忽略目录 `out/native/qa/memory-*`。全模块测试、相关 race、vet、34 布局、隐藏窗口合成 MP3→FLAC 续播通过。

- 歌词中的 CJK 原文 + `^` + 拉丁音译拆成上下两行，音译使用 13px 辅助文字；同时有译文时再单独一行。逐字时间戳不混入音译，跟随偏移/裁剪/点击区域按实际副行数计算。解析与 1180/960 宽度排布回归通过，保留普通文本中的字面 `^`。

- 大封面补 macOS 动画纹理上传修复：Viva 0.1.3 原生纹理 hash 包含目标尺寸，但 Go 上传判定不含；封面缩放按像素尺寸更新版本，静止小封面仍复用。新增回归验证放大/缩小每个尺寸变化均上传；撤掉修复可复现 52→70px 漏传。旧 headless 截图强制上传全部位图，不能单凭截图证明运行时纹理复用正确。
- 移除侧栏底部会话状态圆点/文字；浮动控制栏增加双层柔影和细描边，按钮悬停使用主题浅色反馈、播放键增加按下反馈。深浅主题 hover 回归、全模块 Go 测试、vet 与 34 项离线布局通过；macOS arm64 测试包重新构建。

- 音量改为最右侧单图标，点击上浮调节条与百分比；队列去掉数字，右侧统一单排。菜单互斥，支持拖动、键盘、Esc/点击外部关闭；控制条及弹窗使用主题底色/文字/强调色，歌词页仅淡出底板。新增交互回归通过，Go 全模块测试（含合成音频）、交互 race 与 vet 通过；34 项离线布局检查通过，已查看浅色/深色音量浮窗截图。macOS arm64 未签名 DMG 已重建并通过包内容、许可、SHA256 和镜像校验。

- 侧栏/内容区背景铺满窗口，只用竖线分隔；普通页使用悬浮底栏，歌词页底板随 320ms 动画淡出，控制组件坐标不变。大小封面独立绘制身份/层级，共用像素数据；大封面展开，小封面保留。新增实心播放控制、圆润侧栏图标。
- 播放顺序按钮只显示图标，点击上浮图标+文字选单；音质也用上浮选单，选中才改变设置。按钮文字依据可见字形居中。统一 12 套主题，移除自定义颜色入口。
- 歌单错误不再写入全局横幅，仅歌单刷新按钮变为重试；收藏刷新保留旧完整列表，后台分页去重结束再替换。没有已确认的收藏总数字段，首次登录仍须完整分页确认总数，不能将首屏 50 首标成总数。HTTP 200 仍可能是业务/响应格式失败；本轮未读取私人响应，服务端原因尚未定位。
- 封面缩略图磁盘缓存采用 128 个固定槽（最多约 32 MiB），只存哈希和小图，不保存 URL；按需加载，内存最多 16 张（约 4 MiB 像素）、并发解码 2。macOS 文字测量改为 CoreText 直接测宽。合成启动 RSS 144304 KiB（约 141 MiB），较上轮接近；GPU/窗口/字体运行时仍需常驻，未宣称降到几十 MiB。
- 新增回归：两尺寸封面同时可见、菜单选择/互斥、字形居中、完整刷新不闪回 50、歌单错误隔离、缓存跨实例读回/槽位上限。未进行私人账号联网验证。

最新播放器/UI 合并修复（2026-10-29）：

- verified（合成）：普通页/歌词页进度统一使用绘制时的几何与实际音频时钟；鼠标点击、拖动与系统媒体跳转测试通过。macOS 本地和 Range 流启用精确时长解析；播放响应包含曲目身份时校验一致性。列表、播放接口与音频时长分开保存，实际音频优先；差异超过 2 秒只记录数值，不记录歌曲 ID、URL 或凭据，不截断已播放文字掩盖差异。
- verified（macOS 合成）：隐藏窗口时主线程直接处理播放器/网络结果，不依赖 GPU 重绘；播放期间持有允许系统休眠的活动声明。`--demo 2 --fixture-dir <synthetic> --check-background` 实际隐藏窗口并完成 MP3 → FLAC 自动续播，未读取私人会话。普通最小化、睡眠/唤醒与用户歌曲仍待实测。
- verified（离线 UI）：底栏统一布局、“词”与音质同风格、队列/音量分开、单一互斥播放模式、暂停队列定位；封面展开与播放控制 320ms 位移动画；注视点字号与当前歌词高亮分离。晨光/午夜蓝/深林/石墨主题贯穿普通页和歌词页，原生窗口按钮融入侧栏。
- verified（同机合成内存）：965 首启动 3 秒 RSS 从 188400 KiB 降到 144736 KiB（约 184 → 141 MiB）；文字位图按内容宽度分配、缓存 12 MiB/384 项，封面 40 项，取消不再使用的模糊副本。不是活动监视器 footprint，也不代表真实封面库长期播放峰值。
- 证据：全模块 Go 测试、音频夹具、UI/存储/队列/资源 race、vet；21 项 headless 布局（含 4 个主题）。用户歌曲的 4:45 / 4:22 尚未取得实际响应与媒体对照，不能宣称已定位为元数据错误或错配音源。Windows 完整构建继续按用户要求暂缓。

| 原生里程碑 | 状态 | 证据 / 待验 |
|---|---|---|
| M0 工程与边界 | verified（macOS 开发构建） | Go 依赖锁定、凭据封装、TLS/重定向/资源白名单、全模块测试和 vet；原生包不带 Electron/研究数据。 |
| M1 登录与播放 | in-progress | PKCE/state/回调生命周期、macOS 系统授权桥、加密会话代码；本地与模拟 HTTPS Range 的 MP3/FLAC 通过。本人扫码、真实 CDN 全曲、本地凭据真实登录恢复 pending。 |
| M2 只读库 | in-progress | 收藏完整分页/去重/965 首/重试、搜索、歌单/专辑/艺人/历史；账号隔离缓存；真实完整音乐库 pending。 |
| M3 播放器 | in-progress | 队列/随机/循环/seek/音量/恢复不自动播放、歌词缓存/导入/翻译/逐字/偏移/手动跟随；模拟资源通过，真实权益/资源过期/睡眠恢复 pending。 |
| M4 写操作 | in-progress | 收藏、创建私密歌单、简介、删除 owned；写后读回、创建持久化未决意图、不盲重发；合成测试通过，原生真实写路径 pending。 |
| M5 UI 与系统 | in-progress | 详情/沉浸歌词/系统字体筛选/动态模糊已补，21 项 headless layout 通过。macOS MediaPlayer、Linux MPRIS、Windows SMTC 均有实现；后两仅交叉编译。用户已取消逐像素要求，真实桌面交互仍待验。 |
| M6 发行 | in-progress | 默认 build/dist 已切 Go/Viva，macOS 未签名 .app/DMG/独立程序与包内容检查通过；Windows NSIS / Linux tar.gz 脚本及三端手动工作流已补，目标系统未执行。未发布。 |

本轮日用调整：显示名改为「听澜」，技术包名/数据目录不变；侧栏图标和阴影随主题色；字体改为当前展示 + 可输入筛选的系统字体列表；隐藏透明度/圆角设置；移除播放成功后的常驻“正在播放”状态；空格仅在应用内切换播放/暂停，输入框保留空格。封面补 WebP/GIF 解码、备用字段/尺寸、专辑兜底、失败重试及按使用淘汰缓存。真实音乐库封面补全效果仍需用户验证。

交互补齐：搜索页与编辑/字体弹窗打开时自动聚焦；输入框回车提交，字体筛选支持上下键高亮与回车选择；弹窗限制 Tab 范围、Esc 关闭并恢复原焦点，删除类确认默认聚焦取消；中文输入法组合态不误提交/关闭。UI 驱动回归覆盖这些行为及滚轮不回跳。

本轮验证：Go 全模块测试、vet，UI/协议/资源 race，21 项 headless 布局（含字体弹窗）、176 项 Node 测试及 TypeScript 检查通过。合成 MP3/FLAC 播放/暂停通过；macOS arm64 未签名 DMG 已重建，包白名单、依赖许可（含 WebP 解码器）、SHA256 和磁盘镜像校验通过。未读取私人会话，未发布。

会话策略：不做自动续期，业务请求和每五分钟校验识别明确鉴权失效后退出并要求重登；未知数字码、网络/CDN/权益错误不误退出，不猜测 expireTime 时区或使用 AT 一小时计时。Windows 完整构建/实机验证按用户要求暂缓。

图标更新：新增可编辑 SVG 红色唱片/音符标识，重跑 `scripts/make-icon.mjs` 同步 PNG 与单色托盘；原生侧栏共用新图标，macOS 打包生成多分辨率 ICNS，保留 Lucide ISC 归属。

本轮修复：进度定位期间暂停被覆盖、详情返回滚动位置、收藏写后刷新代次、歌词偏移 JSON null、200 音频响应截断、歌单缓存退出清理；设置改为原表单/开关/下拉并可滚动，补歌词缓存统计、导入清除确认、艺人专辑翻页与歌词恢复跟随。

验证范围：macOS arm64 Go 单测、race、vet；AVFoundation 和 portableaudio（miniaudio + Go 解码器）使用合成 MP3/FLAC。原 176 项 Node 单测、typecheck/build 已通过；本机 Electron runtime 下载失败，未重跑 Electron host 测试。React/CSS 参考截图来自同机隔离 Chromium，不是 Electron host 验收。

视觉验收口径更新：用户明确“倒是不用这么抠细节”，停止逐像素优化，以整体观感、布局和交互一致收口。5 页差分报告仍保留于 `out/native/qa/report.json`，均非精确匹配，不换算“还原率”。补齐自动浏览器回调（HTTP/JS/取消合成实测）、三端媒体适配、详情标题/hero、双语歌词行距和背景模糊层级。详见 [VIVA_MIGRATION.md](VIVA_MIGRATION.md)。

以下为原 Electron 版本的历史实现证据，不适用于 Viva。

更新时间：2026-10-18。当前以 **Linux 真实 Electron 环境**完成共享 API、业务逻辑、页面和播放器验证；Windows/macOS 不阻塞开发，不使用 Wine 作为运行/验收依赖。不对外发布。

## M0–M6

| 里程碑 | 状态 | 当前证据与剩余验收 |
|---|---|---|
| M0 工程与安全边界 | verified | 锁定依赖；`npm ci`、typecheck、134 项离线单测、构建通过。Linux Electron 验证 renderer 无 Node/通用 IPC、精确来源与导航限制；app.asar 白名单检查通过。 |
| M1 登录与播放 | in-progress | 历史本人扫码已在正式 Electron 完成 CAS → OAuth → 音乐会话 → 首批 50 首收藏，无写操作。当前 Linux 本地合成 MP3/FLAC 实测播放、seek、ended、页面切换不中断通过。**真实华为 CDN 音源 MP3/FLAC 在 Electron 全曲播放、可用系统 safeStorage 下重启恢复仍待验收**。当前系统 backend=basic_text，应用正确 memory-only。 |
| M2 音乐库只读 | in-progress | 收藏、个人歌单/详情、搜索、专辑/歌手、历史已实现；歌单/专辑有分页。新增收藏总数后台索引 + 滚动分页 + 失败重试 + 切页/位置保留。965 首合成库在正式主进程与真实 renderer 分别通过；**不是本人 960+ 首的本轮实测**。收藏/歌单只在网络失败时回退 5MB 有标记缓存；400 不误判失效。 |
| M3 播放器日用能力 | in-progress | 单 Audio、顺序/随机/循环、音量/seek、音质选择、歌词高亮/手动滚动暂停跟随、队列恢复不自动播放。本轮补全歌词：解析词级 `<mm:ss.xx>` 逐字时间、同时间戳双语合并为翻译行、忽略 `[ti:][ar:]` 等元数据、主进程按曲缓存 LRC 结果；新增沉浸歌词页（点击行跳转、进度拖动、±0.5s 偏移按曲记忆、滚轮暂停跟随、Esc 退出）。设置同步、媒体键旧闭包与初始化队列竞态已修。965 首队列真实磁盘读回完整且无播放 URL。真实 CDN/听播、过期资源和睡眠恢复仍未验收。 |
| M4 受控写操作 | in-progress | 主进程串行写，收藏幂等读回；简介/删除先确认当前账号 owned，再读回核对。创建保留本次基线与未决意图，超时不重发，进程重启仍阻止盲目重复创建。生产业务集成测试使用合成响应通过。历史真实接口写验证及 added=0/removed=0 证据仍有效；**新桌面写路径没有当前授权下的真实写验收**。 |
| M5 视觉与系统集成 | in-progress | 视觉整体重做：浮起玻璃面板、窄侧栏、无边框内容、独立播放坞；配色与图标体系自定（暖白中性色 + 默认华为红）。CSS 变量化设计系统，960/1100 两档响应式。设置改为二级分类（播放/外观/关于），外观可调主题色、面板透明度、圆角、字号、字体，写入设置并实时生效。沉浸歌词改为左侧封面 + 右侧居中歌词。Linux Electron 实际截图覆盖 1180×760、960×640、歌词抽屉与沉浸歌词页、队列抽屉、965 首长列表。**新视觉未经用户最终确认，不宣称定稿**。托盘/关闭/媒体键有代码，物理媒体键、无托盘桌面及睡眠恢复仍待验。 |
| M6 发行工程 | in-progress | Linux x64 AppImage 已构建；从实际 AppImage 提取二进制并启动隔离 profile，核对打包文件与当前 out 一致、renderer 无 Node、设置可保存，全部通过。Windows 目标已按要求改为 **NSIS 安装包**，单文件 portable 构建入口、校验脚本与 CI 工作流已删除（`dist:win:portable`、`test:windows-portable`、`windows-portable.yml`）。安装包只能在原生 Windows 上构建，本机 Linux 不产出；CI（`windows-installer.yml`）已切到 `dist:win` + `test:windows-installer`，**尚未在 Windows runner 上实跑，安装/启动/播放均未验收**。macOS arm64/x64 DMG 入口保留。deb 等待真实主页/维护者身份，不编造。 |

## UI 重做与歌词能力（2026-10-18）

- 视觉：`src/renderer/styles.css` 全量重写为 token 化设计系统（暖白中性色 + 单一华为红强调、圆角/阴影/间距/动效变量）。布局从「侧栏 + 通栏播放条」改为「浮起玻璃面板 + 独立播放坞」；列表加入封面缩略图、hover 播放按钮、当前行强调；设置页开关与下拉重绘；新增 `prefers-reduced-motion` 与 960/1180 两档断点。
- 借鉴边界：参考 Q-music 的布局语言与质感，未复制其色板（其主色为绿）、图标或组件实现，全部为本项目自写。
- 歌词解析（`src/lyrics.mjs`）：支持词级 `<mm:ss.xx>` 逐字标记、同一时间戳第二行合并为翻译、丢弃 `[ti:][ar:][al:][by:]` 元数据、纯文本歌词也保留逐字段；新增 6 项解析回归（共 11 项歌词单测）。
- 歌词来源降级：`resolvePlayback` 没有可用 `lyricAddress`、或地址拉取/解析为空时，改读响应内嵌的 `lyrics`（含 `contentExInfo` 与再套一层 JSON 的形态）；仍只接受华为 CDN 白名单地址，内嵌文本有 512KB 上限，新增 5 项 `lyric-resource` 单测。
- 歌词缓存：`AuthManager.getLyrics` 复用进程内 `memory-cache`（键 `lyrics:<trackId>`，账号切换/退出即失效）；renderer 另有一层按曲缓存，避免反复开关面板触发取链。
- 沉浸歌词页：全屏深色舞台 + 封面模糊背景，点击行 seek、进度条拖动、±0.5s 偏移（按曲存 `localStorage`，全局值兜底）、滚轮暂停跟随、Esc 退出；侧栏歌词抽屉保留并互斥于队列抽屉。
- 详情页：歌单/专辑/歌手新增封面 + 标题 + 主操作区（hero），取代原来的小标题行；歌单简介、删除确认、分页加载行为不变。
- 回归：typecheck、150 项离线单测、`test:electron`、`test:electron-ui`、`test:electron-business`、`test:electron-favorites` 全部通过；UI 测试仍覆盖歌词抽屉/队列互斥、播放与收藏写入。截图更新为 `docs/assets/{library,settings,favorites-total}.png`。

## 外观设置 / 歌词布局 / Windows 安装包（2026-10-18 第二轮）

- 沉浸歌词（`LyricsStage`）改为两栏：左侧封面 + 曲名/艺人/专辑，右侧歌词区域横向居中、当前行放大并保留逐字/翻译；底部仍是播放控制、进度、跟随与 ±0.5s 校正。
- 设置页拆成二级面板：`播放`（关闭行为、音质、模式、历史同步）、`外观`、`关于`；版本号常驻面板底部，`退出程序` 移到「关于」。
- 外观设置写入 `AppSettings.appearance`（`accent` / `panelOpacity` / `cornerRadius` / `fontScale` / `fontFamily`），`sanitizeAppearance` 做枚举校验与范围钳制（透明度 0.55–1、圆角 0–18px、字号 85%–120%），部分 patch 不会重置其它字段。renderer 通过 `document.documentElement` 上的 CSS 变量实时应用（`--accent*`、`--panel-alpha`、`--radius-unit`、`--fs-scale`、`--font-ui`）；设置页做乐观本地状态，连续拖动不丢步。
- 播放坞音量图标改为静音切换（记录上次可听音量）。
- 封面兜底：真实曲目封面来自 `picture.bigImgURL/middleImgURL/smallImgURL`（主域名 `music-pics-drcn.dbankcdn.cn` 等，均在 CSP 与队列持久化白名单内）；酷客部分封面仍是 `http://image.kuke.com`，会被 CSP 拦截，因此列表、播放坞、详情 hero 与歌词页统一使用带 `onError` 兜底的 `Cover` 组件，加载失败显示图标而不是破图。
- Windows 目标改为 **NSIS 安装包**：删除 `dist:win:portable`、`test:windows-portable`、`test:portable-data`、`windows-portable.yml`；新增 `dist:win`（仅原生 Windows）+ `test:windows-installer`（NSIS 标记、内嵌 x64 应用、无私有目录、图标像素、与 `out/` 逐文件一致）+ `windows-installer.yml`。**安装包尚未在 Windows runner 上实跑，安装/启动/播放未验收。**
- 回归：typecheck、152 项离线单测（新增外观 sanitize 与部分 patch 用例）、`test:electron`、`test:electron-ui`（新增外观改色/圆角/字体写盘与恢复默认断言）、`test:electron-business`、`test:electron-favorites` 全部通过。

## 第三方歌词 / 自定义外观 / 系统字体（2026-10-18 第三轮）

- 歌词兜底：服务端返回空歌词时，主进程按「歌名 + 艺人 + 时长」匹配第三方歌词库（酷狗 → LRCLIB）。纯函数 `lyrics-provider.mjs` 负责归一化与打分：标题非精确匹配时必须时长一致，艺人必须匹配，`MIN_SCORE` 以下一律拒绝，避免「安静下来」冒充「安静」。第三方结果带 `source`/`matchedTitle`，抽屉与沉浸页会标注来源；可在「设置 → 播放 → 第三方歌词匹配」关闭。实测（本机网络）：酷狗对《假如》《安静》《朵》《终有一天》《Bohemian Rhapsody》均命中；LRCLIB 华语覆盖较弱但英文可用。
- 自定义主题色：色板末尾增加原生取色器，`#rrggbb` 经 sanitize 校验，`theme.mjs` 从单个颜色派生 light/deep/soft/line/shadow。
- 系统字体：`fonts:list` 白名单 IPC，Windows 走 PowerShell + `System.Drawing.InstalledFontCollection`，Linux 走 `fc-list`；渲染进程用带搜索的字体列表（每项以自身字体渲染）选择，选择结果写入 `appearance.customFont` 并成为 CSS font-family 栈首项。Chromium 的 `queryLocalFonts()` 在 `file://` 文档下不可用，因此改为主进程枚举，main 窗口权限保持全拒。
- 回归：typecheck、170 项离线单测（新增 10 项歌词匹配、5 项主题色、2 项字体解析）、`test:electron`、`test:electron-ui`（新增自定义色写盘与 `fonts:list` 断言）、`test:electron-business`、`test:electron-favorites` 全部通过；`scripts/capture-ui.mjs` 增加系统字体与自定义色截图。

## 歌词磁盘缓存与本地导入（2026-10-18 第四轮）

- 磁盘缓存：`userData/lyrics-cache` 按曲目写入 JSON（sha256 键名、单文件上限 512 KB、TTL 90 天、超过 3000 个文件按 mtime 清理）。读取顺序为「本地导入 → 内存 → 磁盘 → 华为 → 第三方」，空结果不落盘，保证后续仍能重试。缓存不含账号信息，切换/退出账号不删除。
- 本地导入：`lyrics:import` 由主进程打开文件对话框，支持 `.lrc`/`.txt`，按 BOM 识别 UTF-16，UTF-8 校验失败回退 GBK；解析为空则报错不覆盖。导入内容写入 `userData/lyrics-local` 并优先于网络结果，歌词面板与沉浸页都可「移除本地」。
- 设置：`设置 → 播放` 显示缓存占用、网络条数、本地导入条数，提供「清除网络缓存」「清除本地导入」，清除后立即刷新数字。
- 回归：typecheck、176 项离线单测（新增 6 项歌词存储：GBK/UTF-16/UTF-8 BOM 解码、缓存文档校验、键稳定性）、`test:electron`（IPC 边界新增 `lyrics`）、`test:electron-ui`（真实文件导入 + 来源标注 + 缓存清除）、`test:electron-business`、`test:electron-favorites` 全部通过。

## 开源准备（README 重写）

- README 按开源项目结构重写：现状表（区分已实测/未验证）、截图（合成数据）、快速开始、功能范围与非目标、构建与打包（Windows 便携版 / Linux AppImage / macOS DMG / 交叉构建说明）、CI、测试、架构与安全边界、数据与隐私、已知限制、参与贡献、第三方组件、许可、免责声明。
- 原“Linux 内部构建 / Windows 单文件便携版（内部测试、未签名）/ GitHub Actions 自动打包”三节改为面向外部读者的构建与 CI 说明；不再以“内部”为标题，但保留未签名、未真机验收等事实说明。
- 新增 `docs/assets/` 三张合成数据截图；`test:release-contents` 确认它们不进入发行包。
- 许可已定为 **AGPL-3.0-only**：加入 `LICENSE`（GNU 官方文本）、`NOTICE`（版权、SPDX、第三方归属）、`package.json` 的 `license` 字段；README 说明许可范围、衍生作品与网络服务的源码义务，以及“许可不授予华为接口/商标/内容权利”。
- README 进一步明确 OAuth 应用标识出处：`client_id=101444301` 来自第三方公开帖（吾爱破解下载工具 / `Tontonnow/hwyy`），帖中未说明获取方式；我们的抓取显示它同时是华为音乐在 AppGallery 的应用 ID（`C101444301`），官方回调 scheme 为 `com.huawei.apps.101444301:/oauth2redirect`，因此极可能是官方 App 自身身份。
- 作者选择方案 B：仓库开源（AGPL-3.0）并保留现状；后续按作者要求恢复 **GitHub Release 发布**（未签名学习构建，发布说明写明未签名、非官方、OAuth 身份不属于本项目）。OSS 上的历史二进制已删除并验证 404；不再使用第三方对象存储分发。
- GitHub 仓库转为公开；Gitee 仍私有。

## CI：GitHub Actions 原生 Windows 打包（已跑通）

- 仓库镜像到 GitHub 私有仓库（`talongao/huawei-music-desktop`），CI 工作流 `windows-installer.yml`（原 `windows-portable.yml`）：手动或 `v*` 标签触发，windows-latest 上执行 `npm ci` → 单元测试 → `dist:win` → 安装包静态校验 → SHA256SUMS，发布到 GitHub Release；不上传 OSS。改动后尚未在 Windows runner 上实跑。
- 产物发布：手动运行或 `v*` 标签会创建/更新对应标签的 GitHub Release（exe + SHA256SUMS），不自动传 OSS；标签与 `package.json` 版本不一致会失败。
- 可选 Windows 烟测需要 ffmpeg，runner 不自带，已在步骤内 `choco install ffmpeg`；该步骤失败不阻断任务（此前失败即为缺少 ffmpeg，非应用缺陷）。
- 首次运行验证：run 38936061865 全绿，产物 100,141,218 字节，`SHA256 6d8c829c1613f46da6c01abb04699e7fe8a0001bbfe2de46424d3373dc7b774e`，应用 exe 名称为“华为音乐第三方客户端（学习版）.exe”，图标像素校验通过。
- 为让校验适配 Windows，静态检查改为：解包定位（不解析 7-Zip 清单文本）、兼容展平布局、asar 解包后按文件比对（不再依赖 asar 条目分隔符）。这些修复保留了 Linux 侧原有的 payload 名与 PE 架构断言。
- 原生 Windows 构建与本地 Linux 交叉构建字节不同，两者均通过同一套静态校验；**Windows 启动/播放仍未验收**。

## 第四批完成（去掉默认菜单 / 改名学习版）

- 移除 Electron 默认菜单：Windows/Linux `Menu.setApplicationMenu(null)`，窗口 `autoHideMenuBar`；macOS 仅保留 appMenu/editMenu/windowMenu 最小角色，Cmd+Q 等仍可用。smoke 回归新增断言：应用菜单为 null 且窗口菜单栏不可见。
- 改名为“华为音乐第三方客户端（学习版）”：显示名集中在 `src/shared/app-name.mjs`（窗口标题、侧栏、设置页、托盘 tooltip），`build.productName` 同步改为该名称，exe 显示名与 Linux desktop `Name` 一致；`package.json` 的 `name` 与 `appId` 未改，因此 userData 目录和已存设置/登录数据不迁移、不丢失。
- 回归：140 项单测、smoke（含菜单断言）、UI、业务、965 首、cancel/callback/navigation 均通过；Windows 包静态校验通过（应用 exe 改为动态发现，图标像素校验仍通过）；重命名后 Linux 包含新名称与图标。

## 第三批完成（登录内嵌窗口 / 自绘图标）

- 登录宿主由独立 `BrowserWindow` 改为主窗口内的 `WebContentsView`：远程页依旧无 preload、无 Node、独立临时 partition，权限/下载全拒；顶部保留 48px 取消条（`LOGIN_VIEW_TOP_INSET`），新增 `auth:cancelLogin` 白名单 IPC；窗口 resize 会重排视图，窗口关闭/取消/清理失败仍 fail-closed。
- 回归更新：取消用例改为点应用内“取消登录”，并断言全程 `BrowserWindow.getAllWindows().length === 1`；回调用例同步；live 工具改从 `web-contents-created` 取脱敏导航轨迹、按 URL 解析内嵌页。
- 图标：`scripts/make-icon.mjs` 从 Lucide（ISC）Music 字形生成 `build/icon.png`(512) 与 `build/tray.png`(32)，托盘改用生成的 `src/main/icons.ts`；归属声明写入 `build/icon.LICENSE.txt`。
- 图标证据：Windows 包内 `.icon-ico/icon.ico` 被解析出 16/24/32/48/64/128/256 七档，最大档 PNG 解码后品牌底色覆盖 85.6%、字形 11%（Electron 默认图标不可能满足）；Linux AppImage 内的 `huawei-music-desktop.png` 与 `build/icon.png` SHA-256 逐字节一致，`.desktop` 的 `Icon=` 指向它。
- 回归：140 项单测、smoke、UI、业务、965 首、cancel/callback/navigation、Linux AppImage 启动与重复解压不丢数据均通过。

## 第二轮完成（歌单缓存 / 随机播放 / 历史同步）

- 歌单缓存：`src/main/memory-cache.mjs` + `AuthManager` 进程内 5 分钟缓存；重复读歌单/详情不再请求，显式 `refresh` 回源；写操作与切换账号失效。生产业务测试断言重复读不增加网络请求、刷新只增加一次。
- 随机播放：设置页新增“随机播放（打乱队列顺序）”模式，与工具栏随机按钮同源；随机模式下“上一首”按实际播放顺序回退（`pushPlayHistory`/`popPlayHistory`）。
- 播放历史上报：`recentlyplayed/create` 按公开 H5 形态实现（`src/music.mjs` + `huawei.createRecentPlay`），真实收听达阈值后只报一次，秒数为实际收听；`syncHistory` 可关；`retry:false`、失败静默。**未用真实账号发起过写请求**，服务端效果待验。
- 测试：140 项单测、Electron smoke/UI/业务/965 首回归全部通过；UI 回归断言真实播放只上报一次且不超过整曲时长；业务回归断言上报负载与失败静默。

## 上一轮完成

- `src/main/favorites-catalog.mjs`：50 首分页计数到末页、ID 去重、在途请求共享、刷新/账号变更失效、无进展保护。接口无可靠总数字段，不猜接口或硬编码 960/965。
- `src/renderer/favorites-page.tsx`：总数与已加载数独立显示，底部预取下一批，错误保留内容并暂停自动重试；保留切页数据/滚动位置；播放全部先补齐分页，可取消准备。
- `src/main/writes.mjs`：owned 归属核验、写后读回、幂等/互斥、创建未决意图持久化。JSON 写入按文件串行，账号退出清队列、缓存、历史、未决意图及损坏备份；保留通用设置。
- `test:electron-business`：**不替换业务 IPC**，仅模拟登录页和 HTTP transport，生产 AuthManager/API/IPC 流程覆盖读取、写约束、缓存、400、退出清理；主进程 965 首扫描正好 20 页，随后读末页不重发。
- `test:electron-favorites`：965 首合成库，50→100→失败→重试→150，返回页面位置不变；播放全部取得 965 首，实际 queue.json 也是 965 首，无音频地址。
- `test:electron-ui`：页面、合成音频、写 UI、设置即时同步、最小窗口几何、队列跨进程恢复。
- 登录取消回归修复测试时序：等实际 CAS 页面后再投递错误 state；生产登录逻辑未因该测试更换。

## 测试边界

已运行：`npm ci`、typecheck/build、134 项单测、`test:electron`、`test:electron-ui`、`test:electron-business`、`test:electron-favorites`、`test:login-callback`、`test:login-cancel`、`test:login-navigation`、发行内容检查。公开登录页回归没有扫码或令牌交换。

`test:safe-storage-restart` 在当前 Linux 明确失败于 basic_text/backend 检查。**不通过 mock 加密器、明文后端或其它平台声明来代替它。**

截图位于被忽略的 `release/qa/`，其中 `linux-favorites-965-synthetic.png` 与 Smoke MP3/FLAC 都是离线测试内容，绝不是用户真实账号数据。当前 AppImage 是内部测试，不代表 M1 或三端验收完成。

本轮未读取/导入 `.local/` 私人会话、未执行任何真实账号写操作，因此没有新增真实对象需要清理。测试 profile 与合成资源由脚本 finally 清除。源码后按用户授权提交至 Gitee 私有仓库；安装包/凭据/日志未上传，未改系统代理。

## 运行与剩余参与点

- 历史 Electron Linux 桌面现使用 `start:electron`；headless 自动化：`npm run test:electron-favorites` 等命令自动使用 Xvfb。
- Linux 构建：`npm run dist:linux`；产物 `release/linux-x64/`。`test:packaged` 可检查 unpacked 或从 AppImage 提取的实际二进制。
- 真实账号完整收藏：新增 `npm run test:login-live -- --login --qr --full-library`，同一次本人授权核验总数、完整分页和滚动加载，只输出统计；默认不会运行。真实 MP3/FLAC 播放仍另验，不以该脚本冒充。
- 最新 AppImage：`release/linux-x64/huawei-music-desktop-0.0.1-linux-x86_64-internal.AppImage`；`release/linux-x64/SHA256SUMS` 已生成。`npm run test:packaged -- <AppImage路径>` 已通过，未上传。
- ~~Windows portable~~（已废弃）：2026-10-18 起不再产出单文件免安装版，改为 NSIS 安装包；历史验证记录保留但不再适用当前构建目标。
- ~~`npm run test:portable-data`~~（已随 portable 目标移除）：该测试验证的是「解压到临时目录后用户数据不丢」，安装包场景改为覆盖安装保留 userData，待 Windows 真机验收。
- 视觉选择：`design-previews/2026-10-15-desktop/index.html` 的 A/B/C/D，未选前保持当前基础样式。
- Linux 密钥服务、真实听播、托盘/物理媒体键/睡眠仍需环境证据；Windows/macOS 原生测试延后，不阻塞共享逻辑。

历史流水及旧沙箱阻塞记录见 [IMPLEMENTATION_HISTORY.md](IMPLEMENTATION_HISTORY.md)，只作历史，不是当前环境结论。登录修复证据见 [LOGIN_CALLBACK_FIX.md](LOGIN_CALLBACK_FIX.md)。

2026-10-29 凭据与歌单调整：已核对 Apple TN2206，现有本地 ad-hoc 构建的 designated requirement 只有 cdhash，不能维持跨重编译身份。启动改用固定位置 .app，进程内复用成功读取的密钥，构建支持 HWMUSIC_MAC_SIGN_IDENTITY 指定已有稳定签名证书；未创建证书/放宽 ACL，真实钥匙串授权与跨升级免提示仍 pending。凭据仍为 AES-GCM 密文，密钥只在系统安全存储与进程内存。

歌单与详情/远程历史缓存优先显示，5 分钟内复用（包括空结果），过期后台刷新；手动刷新和写后刷新跳过缓存，写后仍由协议层完整读取服务端校验。混合歌单发现使用此前真实验证的 limit=999，保留分页与去重；合成 1001 条记录两次请求覆盖跨页去重。网络失败保留已有内容并提示，认证错误仍退出。播放成功不再设置常驻状态文字。

本次调整验证：Go 全模块测试、UI/存储/协议 race 与 vet 通过；新增合成密文读写测试不访问系统钥匙串；21 项 headless 布局通过。macOS arm64 DMG 已重建，包白名单/许可/SHA256/hdiutil 校验通过。真实账号缓存速度、稳定证书签名及跨版本钥匙串授权待验。

2026-10-29 macOS 固定签名：用户明确选择免费自签名，凭据继续使用系统钥匙串，没有改为目录凭据。本机已创建 Tinglan Local Development 证书及不可导出的私钥（仅 codesign 可用），没有修改系统/用户根信任。启动、独立程序和 .app 构建自动从用户配置读取固定指纹，其他平台不变。两个不同版本的合成探针签名 requirement 一致，跨重编译钥匙串读取成功并清理测试项；真实旧会话的首次迁移授权仍待验。它不等于 Developer ID 签名或公证，未发布。

最新凭据调整：用户明确选择应用数据目录存储。session.local.json 原子保存密文及同存随机密钥，依赖目录/文件权限 0700/0600，不能保护同用户权限的读取。启动/保存/退出不再调用钥匙串；不导入旧 session.enc，需重新登录，退出清除新旧会话文件。默认构建不再自动访问本机签名私钥，证书保留；仅显式 HWMUSIC_MAC_SIGN_IDENTITY 启用签名。未读取私人会话。

本地凭据验证：合成会话跨 Store 重建恢复、权限、原子保存失败保留旧值、旧格式忽略、损坏/超限/符号链接拒绝及退出清理通过；存储与 UI race、全模块 vet 通过。macOS 未签名 DMG 已重建，包白名单/许可/SHA256/镜像校验通过。没有真实账号读取或联网验证。

2026-10-29 歌单/歌词/标题栏调整：歌单卡片改方形封面、行高随卡片尺寸；混合发现 999 分页遇 HTTP 400 回退到 50，合成回归通过。读取错误展示所属页面及安全的 HTTP/数字业务码，不输出 URL/令牌；截图中的真实失败根因尚无响应证据，不能认定已定位为分页错误。

歌词入口改无边框“词”，直接进入沉浸页，移除歌词抽屉；右下角“···”菜单提供导入和 ±0.5 秒偏移，取消跟随开关。滚动/拖动后 5 秒无操作恢复跟随，持续操作延长等待，拖动不触发歌词点击 seek；保留空时间戳的播放语义，但不为空文本占用视觉行高。截图歌曲的实际歌词文件未读取，真实间距效果待验。

采用 Viva WindowCustomTitlebar，自绘背景与顶部拖动区，双击行为交框架；macOS 保留左侧系统红黄绿控件，Windows/Linux 使用 Viva 右侧最小化/最大化/还原/关闭控件。关闭仍遵守关闭到托盘设置。17 项 headless 布局覆盖歌单卡片、沉浸菜单与小窗；macOS 真实窗口操作及 Windows/Linux 桌面行为待验，合成图不等同实机验收。
