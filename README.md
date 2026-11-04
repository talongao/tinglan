<p align="center">
  <img src="build/icon.png" width="88" alt="听澜图标">
</p>
<h1 align="center">听澜 · Tinglan</h1>
<p align="center"><strong>把喜欢的歌，留在桌面上。</strong></p>
<p align="center">基于音源插件的第三方音乐客户端，原生、可扩展、数据留在本机。</p>
<p align="center">
  <a href="#音源插件">音源插件</a> ·
  <a href="#听歌也看歌词">听歌体验</a> ·
  <a href="#曲库">曲库</a> ·
  <a href="#决策模型可选">决策模型</a> ·
  <a href="#开始使用">开始使用</a>
</p>

![听澜沉浸歌词：晚风心里吹，原句下方显示粤语音译](docs/assets/tinglan/lyrics-cantonese.png)

听澜是一个用 **Go + Viva** 写的原生桌面音乐客户端。它通过音源插件接入平台：登录自己的账号，就能在电脑上浏览、搜索、播放，不需要手机充当播放器。界面、播放内核和构建工具链全部由 Go 实现，不依赖 Electron 或 Node.js。

> 非官方、实验性项目，与各音乐平台无隶属或合作关系。只播放账号已有权益允许的音频流，遵守各平台的服务条款与会员、地域限制，不提供下载、离线保存或媒体文件处理。完整说明见[免责声明与使用边界](docs/DISCLAIMER.md)。

## 音源插件

音源不是写死在程序里的。在「设置 → 音源 → 添加音源」选一个 `.zip` 音源包，首次启用确认一次，之后它就是这个平台上的一扇窗：搜索、歌单、专辑、艺人、播放、歌词都由它提供。同一个音源包再次添加即为升级，保留启用状态与登录会话。

- **独立进程，语言无关。** 每个音源以独立进程运行，通过 stdio 上的 JSON-RPC 2.0 与宿主通信。Node、Python、Go、Rust 都行，只要运行时能从命令行启动；脚本类插件需自备解释器。
- **能力自报。** 握手时插件说明自己的显示名、图标和它能做什么（搜索、歌单、导入、播放解析、歌词、写操作等），界面按能力显示入口。
- **可以自己写。** 音源放在仓库 `plugins/<id>/`，`source.json` 里由作者维护稳定的 `id` 与 `version`。欢迎提 PR 扩充音源。

十来分钟能写出一个可搜索、可播放的音源：[插件开发指南](docs/PLUGIN_GUIDE.md) · [插件协议](docs/SOURCE_PLUGIN_PROTOCOL.md) · [音源契约](docs/SOURCE_CONTRACT.md)。

## 听歌，也看歌词

**有些歌想跟着唱，有些歌想读懂。**

当前唱到的句子保持高亮；服务端提供逐字时间轴（TTML）时，歌词会逐字点亮。歌词源自带的音译或译文显示在原句下方——听澜只负责排版，不生成翻译。

- **顺着歌走，也能自己翻。** 默认自动跟随；手动滚动后停留 5 秒，无操作便回到当前歌词。
- **保留你的歌词版本。** 支持导入本地 LRC / TXT，也能按歌曲调整时间偏移。
- **听歌不用反复切窗口。** 应用内空格暂停或继续，托盘与系统媒体控制随手可用。
- **常用操作，随点随开。** 音质、播放顺序、音量用上浮菜单；播放队列打开就定位到当前歌曲。
- **放不下去时不硬撑。** 解析或解码失败自动跳到下一首，播放器上留一个重试入口。

<details>
<summary><strong>看看双语歌词 · 原文与翻译一起读</strong></summary>

![听澜双语歌词：俄文原句与中文翻译分行显示](docs/assets/tinglan/lyrics-dark.png)

</details>

## 曲库

**一首歌一条记录，多个来源挂在它下面。**

同一首录音在听澜里只占一行，来自不同音源的版本作为「来源」挂在这一行上。收藏、歌单、队列都只引用这一行的本地 ID——换音源、换账号、某首歌在平台上消失，都不会把收藏弄丢。

- **收藏是本地数据。** 从音源导入收藏只是把来源挂上来；平台侧取消收藏不会删掉本机收藏，要移除得自己在听澜里点掉。
- **歌单分两种。** 本地歌单是文件夹，随手新建、加歌、删歌，里面的歌可以来自不同音源；导入歌单是平台歌单的只读镜像，只能整份更新，以平台当前内容为准。
- **搜索按音源分页签。** 启用两个以上音源时，搜索页顶部可以选平台；只启用一个时不显示，免得多余点击。结果随滚动继续加载，点歌加入当前队列，不替换已有队列。
- **播放历史只记在本机。** 不上传、不同步。

![我的收藏：一首歌一行，右侧是来源与收藏状态](docs/assets/tinglan/favorites.png)

| 搜索喜欢的歌 | 找回刚刚听过的 |
| --- | --- |
| ![搜索周杰伦的歌曲，可见搜索范围与来源](docs/assets/tinglan/search.png) | ![听澜本机播放历史](docs/assets/tinglan/history-dark.png) |

> 截图取自本机运行界面；曲目与账号内容为示例，功能以当前版本为准。

## 决策模型（可选）

导入收藏或更新歌单时，平台数据常常缺时长、缺版本信息，同一首录音在不同专辑里长得几乎一样。听澜先用本地规则判断（标题规范化、艺人、专辑、版本标记、时长差），只有落在灰区、规则定不下来的成对曲目，才考虑交给决策模型。

在「设置 → 高级 → 决策模型」填接口地址、API Key 与模型，可以先「测试连接」再保存。

- **默认关闭，关闭时不产生任何费用。**
- **只发判断所需的曲目信息**：标题、艺人、版本、专辑、时长。不发音频、账号凭据、账号标识或带签名的播放地址。
- **只解决灰区。** 明显相同或不同的曲目不会调用模型；判定结果带缓存与有效期，撤销的合并不会被旧结果重新套用。
- Key 与登录会话一样存在本机应用数据目录的加密文件里，不使用系统钥匙串。

## 换一种心情

从明亮到深沉，**12 款预设主题**让音乐库、悬浮播放器和歌词页保持同一种气氛。字体可以从本机已安装字体中搜索选择，字号也能按习惯调整。

![听澜外观设置：主题、字体与字号](docs/assets/tinglan/appearance.png)

## 开始使用

目前以 **macOS Apple Silicon** 为主要开发与实际使用环境，本页截图也来自这一版本。Windows 已有适配代码与构建产物，**尚未完成实机验收**；Linux 暂不打包。

1. 从 [Latest Release](https://github.com/talongao/tinglan/releases/latest) 下载系统安装包，以及你要用的音源包（例如 `huawei-<版本>-darwin-arm64.zip`、`huawei-<版本>-windows-x64.zip`）。
2. 打开听澜 → 设置 → 音源 → 添加音源，选择音源包并确认启用。
3. 在该音源下点「登录」，用自己的账号登录。

测试包没有开发者证书签名或公证，也不是正式发行版。登录通过宿主内嵌窗口完成：macOS 用 WKWebView，Windows 用 WebView2（需要 Microsoft Edge WebView2 Runtime），不用手动复制回调链接。

<details>
<summary><strong>从源码启动</strong></summary>

准备 Go 1.27.3+ 和本平台 C 编译工具；macOS 需要 Xcode Command Line Tools。以下命令都在 `native/` 下执行：

```sh
go run ./cmd/tinglan-tool start     # 启动
go run ./cmd/tinglan-tool build     # 构建本机程序
go run ./cmd/tinglan-tool mac-app   # 构建 macOS 应用
go run ./cmd/tinglan-tool plugins   # 打包当前平台的音源 zip
go run ./cmd/tinglan-tool ui-check  # 离线界面回归（无头 Viva）
```

日常开发在 `develop`：到 Actions 手动选择 macOS Apple Silicon、macOS Intel 或 Windows x64 构建，试用包与音源包留在 Artifacts，不发布 Release。`main` 只接受通过检查的 PR；合并后自动构建三个平台，全部成功才发布到 [Latest Release](https://github.com/talongao/tinglan/releases/latest)，音源包与安装包一起发布，标签带独立构建编号。平台依赖、打包与测试方式见[原生开发说明](native/README.md)。

</details>

## 关于账号与隐私

登录状态与 API Key 按你的选择保存在本机应用数据目录的加密文件里，不访问系统钥匙串。凭据保护依赖操作系统用户隔离与文件权限；音乐会话不自动续期，确认失效后重新登录。

退出账号会停用该账号在本机曲库里的来源绑定，但不会删除收藏；重新登录后收藏仍在。

在线歌词优先使用音源返回的内容。可选的第三方歌词匹配只查歌词、不提供音源，匹配时发送歌名、艺人等曲目信息，不发送账号凭据，可在设置中关闭。

具体验证范围与待办见[实现状态](docs/IMPLEMENTATION_STATUS.md)，不以截图或编译成功代替完整验收。

## 接下来

**Windows 可用性验证**仍是发布门槛。音源插件、多源曲库、本地歌单与导入歌单、搜索页签、逐字歌词、决策模型灰区判断都已落地。

还没做的：网易云音乐适配器（roadmap 中，走纯 HTTP 扫码登录）、Windows 实机验收、插件签名与市场分发。

重点验证安装与启动、添加音源、登录与会话恢复、曲库与搜索、MP3/FLAC 播放及拖动进度、歌词、托盘续播与系统媒体控制。CI 构建成功只是起点，不代表这些体验已经验收。

## 一起把听澜做好

欢迎带着使用感受来提 [Issue](https://github.com/talongao/tinglan/issues)，也欢迎修问题、改排版与可访问性、补跨平台验证的 PR。给第三方音源提 PR 时，请放在 `plugins/<id>/`，在 `source.json` 写清稳定的 `id` 与 `version`，并在 README 说明所需运行时与遵守的平台条款。

反馈请写清系统版本、复现步骤和预期表现；截图避开账号信息。不要提交 Token、Cookie、授权码或私人数据。项目不接受绕过付费限制、音频解密或未经授权的资源替换。

开发资料：[原生开发说明](native/README.md) · [音源插件开发指南](docs/PLUGIN_GUIDE.md) · [插件协议](docs/SOURCE_PLUGIN_PROTOCOL.md) · [音源契约](docs/SOURCE_CONTRACT.md) · [实现状态](docs/IMPLEMENTATION_STATUS.md) · [接口契约](docs/API_CONTRACT.md) · [验证记录](docs/authorized-feature-validation.md)

## 社区

感谢 [LINUX DO](https://linux.do/) 社区。

愿这里也能遇到喜欢折腾桌面、认真听歌的朋友。

## 开源与致谢

听澜使用 [Viva](https://github.com/vivaui/viva) 构建原生界面，界面图标基于 [Lucide](https://lucide.dev/)（ISC）。应用图标的许可与归属保留在 [icon.LICENSE.txt](build/icon.LICENSE.txt)。

代码采用 **AGPL-3.0-only**，详见 [LICENSE](LICENSE) 与 [NOTICE](NOTICE)。许可证只覆盖代码，不授予音乐内容、商标或服务接口的使用权。登录所用 OAuth 应用身份不属于本项目；公开分发或商业使用前，请阅读[完整免责声明](docs/DISCLAIMER.md)并自行核查必要授权。
