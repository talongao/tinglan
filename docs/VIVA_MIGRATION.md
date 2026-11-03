# Viva 迁移记录

2026-10-29：用户明确要求用 Go + Viva 替代 Electron，并保持原 UI。该要求覆盖旧文档“不换 Go”；隐私、音源、凭据与发布约束继续有效。用户不在电脑旁，先以合成数据推进，不以等待扫码作为开发停止门。

## 入口与运行结构

`npm start` / `dev` 已切换 `native/cmd/music`，独立 Go 模块固定 Viva v0.1.3。应用由 Viva 原生绘制，没有内嵌 Electron/Chromium/Node。原 Electron 入口为 `start:electron`，保留原 `package.json.name` 和旧 userData；`build`、`dist:*` 已切原生；旧入口改为 `build:electron`、`dist:*:electron`。旧 Windows 自动发布改为仅手动构建，未发布或推送。

Go 内部包持有会话与业务 HTTP；macOS AVFoundation 通过 AVAssetResourceLoader 请求受限 Range，Windows/Linux 使用 beep MP3/FLAC 解码加 miniaudio 输出。所有远程音频请求经过 Go 白名单和逐跳重定向校验，分块内存有上限，不落盘、不搭后端或本地代理。

## 已迁移并有离线证据

- 授权协议：PKCE、随机 state、精确回调匹配、单次消费、超时、取消、候选会话只读验证；错误脱敏，业务请求拒绝重定向，不关闭 TLS。
- 本地凭据：按用户选择，AES-GCM 密文和随机密钥同存应用数据目录，依靠文件权限保护，不访问系统钥匙串；保存失败则内存会话。账号分隔队列、历史、收藏及歌单缓存；保存/退出代次保护，不自动导入旧密文。
- 音乐库：收藏 50 首分页、ID 去重、无进展保护、失败重试、后台完整总数、965 首完整队列；搜索分页、owned/collected 歌单、历史、专辑分页、艺人歌曲和专辑列表翻页。chart 详情不假装支持。
- 播放：本地和模拟 HTTPS Range 的 MP3/FLAC、暂停、seek、ended、音量、关闭；队列顺序/列表循环/单曲循环/随机、按真实播放历史返回上一首、移除与清空、恢复不自动播放。portableaudio 的定位期间暂停竞态已修并有阻塞解码器回归。
- 受控写入：收藏增删、自建私密歌单、简介、删除 owned；写后读回、账号代次、互斥；创建以队列去重 1–200 首并校验名称，持久化未决意图，超时不重复创建。未执行真实账号写操作。
- 资源与歌词：受限封面获取、内存上限、缩放/圆角、按使用淘汰；华为歌词、本地导入优先、歌词缓存、酷狗/LRCLIB 严格匹配兜底（只取歌词）、UTF-8/UTF-16/GBK、逐行/翻译/逐字、点击定位、偏移保存、手动滚动及恢复跟随。
- UI：侧栏、收藏行、搜索、歌单、历史、设置表单、下拉和开关、播放坞、队列与歌词面板；设置保存、主题色、自定义色、可搜索系统字体、字号（圆角/透明度控件暂时隐藏）；macOS CoreText、Linux Pango/Cairo、Windows GDI 比例字体适配（后两项只有交叉编译证据）。
- 系统：Viva 托盘/关闭行为/单实例；macOS MediaPlayer、Linux MPRIS、Windows SMTC 媒体控制与曲目信息桥。Linux 音量/循环/随机双向同步，Windows 时间线与定位。默认测试不注册媒体键；浏览器集成测试显式启用且仅访问本地合成页面。

## 真实证据与剩余工作

| 项目 | 当前结论 |
|---|---|
| 本机环境 | macOS arm64 / Apple M4 / Go 1.27.3；不代表 Windows/Linux 验收。 |
| Go 检查 | 全模块单测、UI 驱动、业务/存储/资源 race、vet 通过；Windows/Linux 字体、授权和媒体包交叉编译通过，媒体集成测试也已编译。 |
| 合成音频 | 本地及 TLS 测试服务器 Range MP3/FLAC，AVFoundation 与 portableaudio 路径通过；不是华为真实权益音源。 |
| 原 Electron 基线 | 176 项 Node 单测、typecheck、build 通过；runtime 下载超时，本机未重跑 Electron host 集成。 |
| UI 几何 | 1180×760、1100/1020/960×640、设置滚动与 965 首队列含专辑/艺人/歌单、沉浸页和抽屉共 14 个 headless layout 检查为 0 problems，磁盘队列 965 首。 |
| 视觉检查 | 用户最新明确无需抠像素细节，以整体观感、布局与交互一致为准。5 页差分保留参考，均 ExactMatch=false，不作为完成阻塞；详情 hero、沉浸页、系统字体筛选、玻璃面板已补。 |
| 本地打包 | macOS .app、独立程序、DMG 已本地构建；白名单包内容、许可证、SHA256、hdiutil verify 通过。未签名、不发布。 |
| 本人登录/会话恢复 | pending：macOS AuthenticationServices 对华为跨域扫码回调、应用数据目录会话重启；按用户要求不做自动续期；明确会话失效后退出并要求重登。 |
| 真实听播/写入 | pending：真实 MP3/FLAC 全曲、过期资源、睡眠恢复、受控临时对象写后读回清理。 |
| 视觉范围 | 已停止逐像素优化；字体栅格、阴影和少量控件细节可能与 Chromium 不同。双语歌词行距、字体测量、弹层绘制层级均已修复。 |
| Windows/Linux | 音频实现经 macOS portableaudio 路径测试。自动浏览器回调、字体及系统媒体适配均有实现与交叉编译证据；真实登录、音频输出、系统媒体/托盘、显示和安装卸载仍待目标系统验证。 |
| 三端发行 | 原生 DMG / Windows NSIS / Linux tar.gz 构建脚本与包白名单检查已接入默认 dist；只有 macOS 执行通过。手动 native-checks 工作流包含目标系统构建与 Linux 隔离 D-Bus 测试，未触发。 |

## 验证与产物位置

- `native/scripts/check-ui.mjs`：无账号的 headless 检查、合成临时 profile 自动清理、965 首持久化断言。
- `native/scripts/capture-reference.mjs`：原 React/CSS 合成参考，隔离 browser context，仅本地网络。
- `native/tools/pixeldiff`：同尺寸 PNG 全画面 RGB 绝对差分，输出误差指标和 PNG；不使用模糊、遮罩或自动降低阈值来制造通过。
- `out/native/qa/report.json`、`*-diff.png`、各断点截图：忽略目录，不进入应用包，不代表本人数据。
- `out/native/Tinglan.app`、`out/native/tinglan/darwin-arm64/`：本地构建；保留 Lucide 许可，不含 `.local/`、`research/` 或 QA 夹具。

本人回来后验证扫码、真实权限音源、保存会话重启与实际交互；Windows/Linux 仍需目标系统验收，不能归为“只剩扫码”。用户明确无需继续逐像素对齐，视觉差分不作为停止开发的借口或继续抠细节的门槛。未发布、未推送，也没有新的真实写对象需要清理。

参考：[Viva v0.1.3](https://github.com/vivaui/viva/tree/v0.1.3)。Viva 本身没有 WebView/音频和 CSS backdrop blur 等能力，本项目通过平台授权、音频、字体适配补足其中部分；不把上游实验性三端支持当成本项目验收。

macOS 本地签名更新：本机已启用免费固定自签名证书，自动复用于构建/启动。合成程序跨重编译保持签名身份并读取钥匙串通过；旧会话迁移仍待验。其他机器无证书配置仍为未签名，Windows/Linux 不受影响。仅内部本地签名，非 Developer ID/公证，未发布。

最新决策覆盖此前签名方案：凭据改存 session.local.json，目录/文件权限 0700/0600，密钥与密文同存，不宣称系统安全存储级保护；旧钥匙串会话不导入，需重新登录。默认构建不再自动使用本机证书，显式环境变量仍可启用签名。

2026-10-29 播放器合并回归：隐藏窗口不再等待重绘才处理续播；普通/沉浸进度与系统媒体统一 seek，使用 AVFoundation 实际时长，保留列表/接口数值用于脱敏差异诊断。合成后台 MP3→FLAC、双进度拖动、系统越界 seek 已通过；用户实际曲目时长不一致的根因仍 pending。新增全局深色主题、封面/控制位移动画、互斥播放模式及队列定位。内存同机 965 首启动 RSS 184→141 MiB；完整证据见 IMPLEMENTATION_STATUS。旧 Electron 差分只是历史对照，不用于评价新设计是否通过。

最新视觉决策覆盖此前卡片/控制位移方案：侧栏与主内容铺满，仅竖线分隔；悬浮底栏进入歌词后淡出底板，控件坐标固定、封面动画展开。12 个统一主题替代独立色号，顺序/音质采用上浮选择。封面磁盘缓存加 16 张内存热缓存；实际私人封面与业务 HTTP 200 失败原因仍待用户数据验证。详细证据见 IMPLEMENTATION_STATUS。
