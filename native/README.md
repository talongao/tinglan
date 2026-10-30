# 听澜原生开发说明

默认启动已切到 `cmd/music`（Viva v0.1.3 / Go 1.26.10）。旧 Electron 客户端已移除；不是已经完成全部验收的发行版。macOS arm64 已用于用户实际听歌并经授权截图；自动化验收使用离线合成数据，不替代不同账号权益验证。Windows/Linux 尚未实机验收。按用户最新要求，不以逐像素一致作为收口条件。

## 启动与本地构建

在仓库根目录：

```sh
npm start
npm run build:native
npm run build:native:mac
npm run dist
```

脚本支持 `HWMUSIC_GO`。默认 Go 不在 PATH 时，启动器可使用已构建的本平台独立程序；源码修改后仍需重建。macOS 需要 Xcode Command Line Tools；Linux 需要 X11/XCB、Vulkan、Pango/Cairo 和音频设备；Windows 需要 cgo 编译器。原生依赖由 Go 获取，不需要 Electron。图标与共享名称已生成，图标矢量源与 ISC 许可保存在 `native/assets/lucide/`；只有栅格化图标才需要根目录 Playwright 开发依赖（`native/scripts/make-icon.mjs`）；`generate-assets.mjs` 仅使用 Node.js 与 Go。

`build` / `dist` 默认均为原生。开发统一在 `develop`，Actions 手动勾选 macOS arm64 / Intel x64 / Windows x64，只上传 artifacts。PR 到 `main` 执行三平台检查，必须通过 `Build gate`；合并后重新构建三平台并自动发布 Latest。标签为 `v<package.version>-build.<run_number>`，不覆盖旧版本。Windows 使用当前用户 NSIS 安装包，尚未实机验收；Linux 暂不打包。手动工作流必须选择 develop 且至少勾选一个平台。

本地 `.app` 位于 `out/native/Tinglan.app`，独立程序位于 `out/native/tinglan/<platform>-<arch>/`，带许可证与 SHA256。默认未签名；仅显式设置签名环境变量时使用证书。未公证、不发布，不注册全局 hms。

普通启动自动读取原生专用 profile：系统配置目录下 `huawei-music-desktop/native-v1`。通用设置、歌词缓存与按账号散列隔离的队列/库缓存分开；凭据按用户选择保存在本机应用数据目录，保存失败则仅保留内存会话。不读取或迁移旧 Electron 密文。

## 离线验证

在 `native/` 下：

```sh
 go run ./cmd/music --demo 965
 go run ./cmd/music --demo 2 --fixture-dir /absolute/path/to/synthetic-audio --profile /absolute/path/to/isolated-profile
 go test ./... -count=1
 go vet ./...
```

`--demo` 不读默认 profile 或系统凭据、不请求业务网络；`--fixture-dir` 只使用自行生成的 `tone.mp3` / `tone.flac`，默认静音。不得指向私人媒体或旧用户数据。普通全模块测试没有夹具时明确跳过音频集成检查。

根目录 `test:native:ui` 构建 headless 版本并验证 1180/1100/1020/960 断点、设置滚动、专辑/艺人/歌单详情、沉浸歌词与抽屉共 34 项布局检查、965 首完整队列落盘，自动清理合成 profile。截图与报告写在忽略目录 `out/native/qa/`。

macOS 音频脚本 `native/scripts/test-audio.sh` 使用 Python、系统 afconvert 和开发工具 lame 生成 4 秒合成音频，验证 AVFoundation 与 portableaudio 两个实现；开发工具不进入发行包。音频也可通过 `HWMUSIC_AUDIO_FIXTURES` 显式指定合成目录。

## 当前边界

- macOS 使用 AuthenticationServices 临时系统授权会话接收本次 hms 回调。Windows 使用默认浏览器并手动粘贴授权回调；Linux 优先独立临时 Chrome/Edge/Chromium 浏览器会话，通过本机 CDP 自动接收回调，不注册系统 hms；只有无兼容浏览器时回退默认浏览器手动粘贴。HTTP/JavaScript 回调与取消已用本地合成服务器测试，真实华为扫码待验。
- 业务 HTTP、歌词、封面、音频 Range 均在 Go 内部处理；音频不落盘、不搭本地服务器、不向 CDN 发送业务凭据。
- 收藏/搜索/歌单/专辑/艺人/历史、队列、受控写操作、网络与本地歌词已有实现与合成测试。
- macOS 托盘/单实例/媒体键与普通字体选择有适配；物理媒体键待验，Windows/Linux 字体选择已补适配并交叉编译，显示待实机验收；Linux 已接 MPRIS（曲目、状态、播放/定位、音量/随机/循环），Windows 已接 SMTC（曲目、状态、时间线与媒体事件）；后两者只有交叉编译证据，未做实机验收。
- 详情 hero、沉浸歌词、外观表单与可搜索系统字体选择、半透明边框及动态背景模糊已补。截图差分保留作参考，不声称精确匹配；用户已明确无需继续抠像素细节。详见 [迁移记录](../docs/VIVA_MIGRATION.md)。

### 听澜界面更新（2026-10-29）

- 显示名由 `native/app-name.mjs` 生成，数据目录和技术包名保持不变。
- 封面支持 JPEG/PNG/GIF/WebP，优先尝试安全的各尺寸/备用地址，缺失时查询专辑封面；失败退避重试，缓存按最近使用淘汰。
- 侧栏图标与阴影跟随主题色；透明度/圆角设置暂时隐藏，当前字体展示与系统字体输入筛选保留。
- 应用内空格切换播放/暂停；输入框、编辑弹窗和字体弹窗不截获空格，长按不反复切换。
- 不做自动续期。业务请求及每五分钟的会话校验发现鉴权 HTTP 401 或明确 token/session 过期响应后清理登录并提示重登；网络错误、CDN/权益 403、未经确认的数字业务码不作为过期依据。音乐 expireTime 时区未确认，不用账号 AT 一小时或该字段猜测到期。
- Windows 完整构建/实机验证暂缓；交叉编译部分包不等于完整原生程序可在 macOS 构建。

交互补齐：搜索页与编辑/字体弹窗打开时自动聚焦；输入框回车提交，字体筛选支持上下键高亮与回车选择；弹窗限制 Tab 范围、Esc 关闭并恢复原焦点，删除类确认默认聚焦取消；中文输入法组合态不误提交/关闭。UI 驱动回归覆盖这些行为及滚轮不回跳。


### 本地凭据与可选签名

用户选择将音乐会话保存在应用数据目录，不再依赖系统钥匙串。macOS 路径为 `~/Library/Application Support/huawei-music-desktop/native-v1/session.local.json`，不写入 `.app` 或安装目录。文件为 0600、目录为 0700（Windows 权限语义和真实 ACL 未验）。文件包含 AES-GCM 密文及其随机密钥，一次原子替换；密钥同存不构成对同用户程序的加密保护，实际保护依赖操作系统用户隔离和文件权限。

启动/保存/退出均不调用系统凭据接口。旧 `session.enc` 不自动解密，升级后重新登录一次；退出账号删除新旧会话文件，但不访问或删除旧钥匙串项。旧 Electron 数据不迁移。文件损坏或保存失败不输出凭据，保存失败时提示退出后重新登录。原生包白名单不包含用户数据。

构建默认不再自动读取本机签名配置。`HWMUSIC_MAC_SIGN_IDENTITY` 仍可显式启用 macOS 证书签名，Windows/Linux 不受影响。此前安装的本地证书保留，构建不访问其私钥；凭据保存与签名身份无关。自签名不等于 Developer ID 或公证。

歌单/详情/远程历史采用账号隔离的磁盘缓存：立即显示缓存，5 分钟内复用，过期后台更新；手动刷新和写操作后的刷新强制联网。混合歌单扫描每页 999 条（项目既有真实证据），继续完整分页、去重与防止无进展。真实账号的速度改善仍待体验。

### 歌词与窗口交互

底部“词”直接打开沉浸歌词；不再使用歌词抽屉。歌词默认跟随，滚动/拖动后 5 秒无操作自动恢复。右下角“···”包含导入歌词和时间微调，空时间戳不占行高，翻译仍保留额外空间。

标题区域与页面一体绘制，顶部可拖动。macOS 保留系统左侧红黄绿控件，Windows/Linux 使用 Viva 提供的右侧窗口控制，关闭到托盘设置仍生效；后两平台实际窗口行为未验收。

播放控制统一使用实际解码音频时长；列表/播放接口时长不再混入已就绪的音频时钟。时长差异超过 2 秒时只输出三组秒数用于诊断。macOS 精确时长解析与隐藏窗口主线程调度已启用，可用隔离合成音频和 `--demo 2 --fixture-dir <directory> --check-background` 验证自动下一首。

外观提供晨光、午夜蓝、深林、石墨；主题与强调色可分别调整。普通页与歌词页共用底部封面锚点，展开时封面和播放控制有 320ms 动画。播放顺序为单一模式，队列打开定位当前项（含暂停）。文字位图缓存 12 MiB，封面缓存 40 项；真实账号长期内存峰值仍需实测。

最新布局为贯穿窗口的双区域加悬浮底栏，歌词页控制组件不移位，底板淡出、封面展开。主题合并为 12 个完整预设，取消自定义色号。播放顺序与音质点击后向上展开选单。

封面采用磁盘小图缓存（128 固定槽，约 32 MiB 上限）与 16 张内存热缓存，并发解码 2；文本测宽在 macOS 直接使用 CoreText。收藏刷新保留已知完整列表，分页扫描完成后再替换；首次完整总数仍由去重分页确认。歌单失败不再显示跨页面横幅，仅提供页内重试入口。
