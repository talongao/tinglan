# 多源收藏 / 歌单 / 收录音乐：数据库设计 v3（待最终确认）

> 状态：单账号口径已定；账号变化更新改为“确认后覆盖”，待你最终确认后实施。未写实现代码，未提交。
> 范围：华为音乐作为首个源，网易云适配器暂不实现；收藏、歌单、收录音乐、旧数据迁移、TTML、去 Node 工具链。
> 依据：用户 2026-10-31 确认稿、Astra 的 3 blocker + 11 major 审查、字段级 A–R 审查。
> 完整 DDL、字段字典、逐表写入边界、审查追溯见 [`MULTISOURCE_DB_DESIGN_ASTRA.md`](MULTISOURCE_DB_DESIGN_ASTRA.md)；本文是决策与实施摘要，两者冲突时以 Astra 文档的 DDL/字段边界为准，并回改本文。

## 0. 结论

- 本地 SQLite，23 张表，不建 `FOREIGN KEY`；逻辑关系由唯一索引、`CHECK`、单写入口和事务内存在性检查保证。
- `tracks`（收录音乐）是唯一事实源；收藏和歌单只存 `track_id`，不复制歌曲元数据。
- 收藏是本地数据：远端更新只对账来源绑定，不删收藏；无可用绑定仍展示，但不可播放。
- 歌单是收藏夹：本地歌单可增删歌曲、可多源播放；导入歌单只读、按完整远端快照镜像，确认远端删除时硬删本地副本。
- 导入歌单按单账号假设：每个远端歌单只保留一份本地副本，不记录拥有者、不做跨账号保护；切换账号后点击“更新”会先确认，确认后按当前账号的完整快照覆盖成员。收藏不因账号变化删除。多账号镜像等实际遇到再处理。
- 合并可撤销；旧 track ID 沿 `merged_into` 永久可解析；合并审计只作审计，不能当缓存。
- 队列/历史仍是文件，不存播放 URL；搜索临时歌曲可以没有 `trackId`。
- 旧收藏、根/账号队列、自己歌单的缓存自动迁移；迁移用输入摘要和阶段日志，不以“DB 为空”判断完成。
- Jev 只用于灰区；不设固定调用上限，默认关闭，按 token 计费。
- TTML 首期支持逐字时间轴、翻译/音译角色；`timeContainer="seq"` 明确拒绝，不做错位歌词。
- 去 Node：用 Go `native/cmd/tinglan-tool` 替代 npm 构建/打包/图标/UI 检查；保留 bundle id、数据目录、显示名。

设计层没有新的产品二选一。用户已确认“先不管多账号”，单账号假设的多账号边界见 §9。剩余是实施、迁移故障演练和真实账号/Windows 实机验收。

## 1. 需求（确认版）

1. **收藏导入落位**：`我的收藏` 标题右侧“从…导入”，打开 dialog 列出支持的平台。
   - 没导入过 → `导入`；未登录 → `去登录`；导入过 → `更新`；已删除/从未导入 → `导入`，不是 `更新`。
   - 更新时若当前账号与上次成功快照账号不同，先确认“将按当前账号快照覆盖该歌单成员，但不会删除收藏/收录音乐”；取消不发起任务。
   - 导入/更新时登录过期先尝试源提供的刷新；不能刷新（如华为音乐）→ 退出该源登录态，toast 提示重新登录。
2. **搜索平台 tabs**：按 `settings.Sources` 中 enabled 且已注册的源生成 tab，先选平台再搜索，默认第一个；未登录 tab 显示登录引导。
3. **歌单**：
   - 可导入某平台歌单；导入歌单的播放只使用该平台音源。
   - 可创建本地歌单，可加歌/移歌，可按多源顺序播放。
4. **收录音乐**：收藏和歌单里的每一首歌都有本地稳定 ID；`tracks` 一行一首，多个平台来源绑定到同一行。
5. **收藏/歌单只存 ID**：所有“收藏 / 加入歌单”先 upsert 到收录音乐（精确绑定优先，代码相似度其次，Jev 只增强灰区），再写收藏或歌单关联。
6. **数据库**：本地 SQLite；只做逻辑外键，不建 `FOREIGN KEY` 约束。
7. **去 Node**：删除 package.json、npm scripts、Node 构建/打包/图标脚本，全部改 Go；保留 bundle id、用户数据目录、显示名。
8. **TTML 逐字歌词**：在现有 LRC/逐字 LRC 之外支持 TTML（逐字时间轴、翻译/音译）。
9. **不自动写回平台**：不创建远端歌单、不向平台写收藏，不绕过会员/地域/DRM，不下载/解密音频。

## 2. 核心不变量

- **收录音乐唯一事实源**：`tracks` 一行一首；`source_identities` 一行一个“平台 + 平台内 ID”的永久身份；`source_bindings` 是身份在某个账号下的播放绑定。
- **收藏/歌单只记录 `track_id`**，不复制标题/艺人/专辑。
- **更新只做 upsert**：正式数据更新不物理删除永久身份、绑定、收藏或收录记录；导入歌单镜像删除成员是唯一例外。
- **收藏永不因远端消失而少歌**：远端更新只停用/删除来源绑定，收藏成员保留；无绑定仍显示，播放时报“无可用音源”。
- **歌单删除就是删除**：本地/导入歌单都硬删 `playlists` + 成员；不动 `tracks`、不动 `favorites`。
- **导入歌单单副本**：`(profile, source, source_playlist_id)` 唯一；不记录拥有者。切换账号后仍可更新，但账号变化且有快照时，更新前必须确认“将按当前账号快照覆盖成员”，不静默覆盖。
- **合并可撤销**：保留原始 home track、成员原始归属、before/after、依赖和 revision；撤销按逆序恢复，不覆盖用户后续编辑。
- **不持久化播放地址**：音源 URL 会过期；队列/历史只存 source + sourceTrackId，播放时实时 Resolve。
- **搜索临时歌曲可以没有收录 ID**：队列条目允许内联元数据；只有收藏/入歌单才进入 `tracks`。
- **敏感信息仍走文件 vault**：DB 不存 API Key、OAuth token、Cookie、Jev Key。

## 3. 表结构

### 3.1 表分组

完整 DDL 和字段字典见 Astra 文档 §4/§4.2；这里只列职责和关键约束。

| 分组 | 表 | 职责 |
| --- | --- | --- |
| 元信息 | `schema_migrations`, `profiles`, `library_clock` | schema 版本；profile；全局 revision 与 import_seq 高水位 |
| 曲目 | `tracks`, `track_artists` | 收录音乐；标题/艺人/专辑/版本/时长/封面；艺人倒排索引 |
| 来源身份 | `source_accounts`, `source_identities`, `source_bindings`, `binding_origins` | 账号与代次；永久平台身份；账号下播放绑定；绑定的集合来源 |
| 收藏 | `favorites` | 只存 `track_id` + 顺序 |
| 歌单 | `playlists`, `playlist_tracks`, `member_refs` | 本地/导入歌单；成员投影；合并撤销的原始归属依据 |
| 导入状态 | `imports`, `import_runs`, `import_stage` | 每个集合的入口状态；完整快照任务；分页暂存与匹配计划 |
| 合并审计 | `merge_decisions`, `merge_before`, `merge_dependencies`, `merge_blocks` | 决策审计；before/after；撤销依赖；阻止立即重合并 |
| Jev | `jev_judgements` | 完整上下文键 + 分数 + TTL；只缓存成功响应 |
| 迁移 | `data_migrations`, `migration_attempts` | 输入摘要计划；DB/文件发布阶段；每次重试留史 |
| 不建的表 | `queue_items`, `history`, `lyrics`, `covers`, `settings` | 队列/历史/歌词/封面/设置继续用文件；外键不建 |

### 3.2 关键唯一约束与检查

- `tracks.id` 主键；`merged_into` 只指向不同根，不能成环；旧 ID 永久保留为别名。
- `source_accounts(source, account_key)` 唯一；每个源最多一个 `is_current=1`。
- `source_identities(source, source_track_id)` 唯一；`home_track_id` 永久；`first_import_seq` 不重置。
- `source_bindings(identity_id, account_id)` 唯一；`access` 为 `full|preview|unknown`；不再保留可漂移的 `active` 列，有效性由 `binding_origins.active` 推导。
- `binding_origins` 主键 `(binding_id, origin_kind, origin_key)`；`origin_kind` 为 `favorites|playlist|local|legacy`；`origin_key` 由固定构造器生成，账号/歌单不能混用。
- `favorites(profile_id, track_id)` 主键；`playlist_tracks(playlist_id, track_id)` 主键。
- `playlists`：`kind='local'` 时 `source/source_playlist_id` 全 NULL；`kind='imported'` 时两列非空；部分唯一索引 `(profile_id, source, source_playlist_id) WHERE kind='imported'`。
- `member_refs` 主键 `(owner_kind, owner_id, ref_key)`；`owner_kind` 为 `favorites|playlist`；合并撤销靠它恢复原始成员，不靠当前投影反推。
- `imports(profile_id, kind, remote_collection_id)` 唯一；`remote_collection_id` 是 `[source, 远端ID]` 的 JSON 数组（带源命名空间）；`account_id` 记录最近一次导入/更新使用的账号，不作拥有者；`status` 为 `never|imported|updated|failed|expired|deleted`；`deleted` 只用于导入歌单。
- `import_runs` 的 `account_generation`/`target_generation` 捕获账号与目标代次；终态不能回到 `fetching`。
- `import_stage(run_id, source_track_id)` 唯一；`UNIQUE(run_id, ordinal)`；重复页只刷新原行，不重排顺序。
- `merge_dependencies` 对 `depends_on` 建反向索引；`merge_before`/`merge_blocks` 带 `format_version` 和字节上限；未知版本拒绝撤销。
- `data_migrations` 主键为随机 `mig_` ID，`(migration_key, input_digest)` 唯一；`migration_attempts` 只有一个 `running`。
- JSON 字段有字节/数组数量 `CHECK`；Go DTO 再做字段形状、数值有限性、单位/空值映射；不靠 SQLite 类型亲和自动纠错。

### 3.3 明确不建的表

- `queue_items`/`playback_state`/`history`：队列和历史是文件，见 §4.4。
- `lyrics`/`covers`：继续文件缓存，按 `source + NUL + sourceTrackID + NUL + formatVersion` 的 SHA256 命名。
- `settings`：继续 `settings.json`。
- 外键约束：全部不建；删除/合并/导入由应用层事务保证。

## 4. 关键流程

### 4.1 收录与匹配

```
(platform, sourceTrackID, metadata)
  → 查 source_identities 精确身份
  → 命中：取 home_track_id，刷新账号绑定
  → 未命中：用代码相似度在 tracks 找候选
      → exact/代码分 ≥ AutoMerge：合并或新建
      → gray 且标题≥0.85、艺人有交集、版本兼容、时长差≤8s：可选 Jev
      → 其余：新建
  → upsert favorites / playlist_tracks
```

- 精确身份命中不调用 Jev；代码分低于 CandidateFloor 不调用；缓存命中不调用。
- 合并写 `merge_before`、`merge_dependencies`、`member_refs` 原始归属；撤销按依赖逆序，恢复原始成员/绑定/位置，保留用户后续编辑。
- `ResolveTrackID` 沿 `merged_into` 逐边查找，带环检测；根不存在或引用断裂返回结构错误，不把旧 ID 当平台 ID。
- 合并后的旧 ID 在队列文件、收藏、歌单中永久可解析；不需要跨文件原子改队列。

### 4.2 收藏导入与更新

- 入口状态由 `imports` 决定：`never|imported|updated|failed|expired|deleted`；收藏没有 `deleted`。
- 首次导入：创建 `import_runs`（fetching），分页写 `import_stage`；只有正常完成、账号代次/目标代次/计划 revision 全部有效，才在一个短 `BEGIN IMMEDIATE` 内发布。
- 发布内容：upsert 身份/绑定/来源、追加/刷新收藏、更新 `imports.status/last_success_at/counts`。失败/取消只标记 run，不删任何收藏或成员。
- 更新：远端已移出收藏的歌只停用/删除该源的绑定和 origin；`favorites`、`playlist_tracks`、`tracks` 保留。无任何绑定的收藏仍显示，标注“无可用音源”。
- `RemoteDeleted` 只由已确认的远端删除语义产生；超时、登录失败、403、空列表、解码失败都不能当删除。
- 收藏总数 = 完整分页后按规范 ID 去重后的数量；首屏数量不冒充总数。

### 4.3 歌单

- 本地歌单：`kind='local'`；可创建、改名、删除、加歌、移歌；只有本地歌单接收“加入歌单”；可混用多源。
- 导入歌单：`kind='imported'`；首次导入成功才创建本地副本；之后更新按当前登录账号的完整快照镜像成员（新增、刷新、移除远端已没有的成员）。
- 单副本：`(profile, source, source_playlist_id)` 唯一，不记录拥有者、不做跨账号保护；任意已登录账号都可更新，更新会用当前账号快照覆盖成员/计数/imports/origins。
- 账号变化确认：点击“更新”时，若当前账号 ≠ 当前副本快照来源账号（最新 committed run 的 `account_id`；迁移基线取 manifest 选中账号），先弹 dialog：“该歌单上次由其他账号导入。继续会按当前账号的完整快照覆盖本地成员（可能移除旧账号的歌曲），但不会删除收录音乐或收藏。” 取消则不创建 run、不改任何状态；确认后才正常创建 run。相同账号直接更新，不弹。从未导入/已删除/无快照不弹。
- 远端歌单确认删除：只删当前副本及其成员/refs，`imports.local_playlist_id` 清空、状态 `deleted`；入口回到“导入”。切换账号不新建第二份副本，更新前按上一条确认；多账号镜像等实际遇到再处理。
- 显式删除：本地歌单/导入副本都硬删；导入副本同时清对应 imports 引用并置 `deleted`；不删 `tracks`/`favorites`，不发远端删除。
- 播放：导入歌单队列条目带 `onlySource=<source>`；解析只在候选层过滤，不回退其他平台；源不可用/未登录正常报错。
- UI 展示本地歌单 + 已有导入副本；入口只按登录态显示“更新/去登录/需登录”，不显示账号归属、不因账号不同禁用；账号不同且已有快照时，点击“更新”先确认覆盖。

### 4.4 队列 / 历史文件

`queue.local.json`（原子写、0600、独立于账号）示例：

```json
{
  "schemaVersion": 2,
  "index": 0,
  "positionSeconds": 12.3,
  "items": [
    {
      "trackId": "trk_...",
      "title": "歌曲名",
      "artists": [{"id": "", "name": "歌手"}],
      "album": {"id": "", "title": "专辑"},
      "durationMs": 231000,
      "coverUrl": "https://...",
      "version": {"live": false, "remix": false},
      "sources": [{"source": "huawei", "trackId": "123456", "access": "unknown"}],
      "onlySource": "huawei"
    }
  ]
}
```

- `trackId` 可空；临时搜索歌用内联 `sources`；不存播放 URL、token、Cookie。
- 有 `trackId` 时先 `ResolveTrackID`，再收集整个别名子树的身份与当前可用绑定，按 `(import_seq, identity.id)` 排序；无绑定不可播放。
- 无 `trackId` 时用内联源，仍检查当前登录态；`onlySource` 只过滤候选，不改变 Resolve 接口。
- `history.local.json` 同结构，最多 200 条。
- 旧 `accounts/<hash>/queue.json` 与根 `queue.json` 迁移：按实际启动顺序（根→当前账号覆盖）选有效文件，不按 mtime 猜；新格式一旦发布，旧 attach 不得覆盖。

### 4.5 旧数据迁移

- 迁移日志以 `migration_key + input_digest` 为计划唯一键；阶段 `prepared|db_committed|file_ready|published|done|failed`，每次重试新增 attempt，不抹旧错误。
- `favorites.local.json`：逐条保留旧 `id`、`importSeq`、`access`、`quality`、来源元数据；迁移在事务内写 tracks/identities/bindings/favorites 和完成标记；文件归档只在 DB 提交后做。
- 歌单缓存：按账号目录分别读 `SHA256("playlists:")` 列表缓存和 `SHA256("detail:"+id)` 详情缓存；详情只取 `Tracks`，不读 `Playlists[0]`；只迁移用户自己的歌单；列表外的详情不迁移。
- 每个账号缓存映射到唯一 `source_accounts.id`；导入歌单按 `(profile, source, remote_id)` 建唯一副本，只取当前（或最后实际 attach）账号的缓存，`account_id` 写入选中账号（仅作记录）；其他账号同名缓存记录为已忽略输入，不合并、不建第二份副本。
- 迁移完成的判断是日志 + DB 标记，不是“DB 为空”；迁移失败不得静默进入可写空库；旧文件保留为证据，不删除。
- 路径只允许 profile 根下相对路径；拒绝绝对路径、`..`、NUL、Windows ADS/保留设备名、符号链接/重解析点；发布用目录句柄和不覆盖/原子替换，防 TOCTOU。

### 4.6 Jev 门控与缓存

- 默认关闭；只有用户启用并配置 key/model 才调用；模型按 token 计费。
- 精确绑定、代码分 ≥ AutoMerge、代码分 < CandidateFloor、已缓存/已判定：都不调用。
- 只有 gray 且同时满足：标题相似度 ≥0.85、艺人集合有交集、版本兼容、双方有时长时差 ≤8 秒，才进入 Jev。
- 不设固定调用次数上限；靠前置算法收窄灰区，而不是按候选数量截断。
- 缓存键包含两侧元数据摘要、规则版本、模板版本、模型 ID/别名、规范化 endpoint、credential generation；换模型/端点不复用。
- 只有业务成功、answer 存在且为有限 [0,1] 分数才写缓存；认证/限流/网络/协议失败分类提示，不缓存为永久不匹配；`merge_decisions` 永不作为免检缓存。
- 撤销合并时把对应缓存标 `invalidated` 并写 `merge_blocks`，新模型不能绕过显式撤销。

### 4.7 TTML

- `native/internal/lyrics/ttml.go`，标准库 `encoding/xml` token 流；`ParseAny` 按内容识别 TTML/LRC。
- `tt/body/div/p/span` 为行/词；`br` 断行；保持 time-container 栈，子时间相对父容器转绝对毫秒，`dur`/父结束取最小有效值并裁剪。
- 首期拒绝 `timeContainer="seq"`，返回 unsupported timing；缺 `end/dur` 的词只在同一父容器内用后一个有效兄弟 `begin` 推断，不跨 p/div。
- 翻译/音译按显式 `ttm:role` 或已识别并行结构映射；只语言不同不能断言翻译；歧义保留独立文本。
- 512 KiB、嵌套 32 层；不解析外部实体，不访问 XML 中的外部地址；坏时间可跳过并诊断，结构错误/超限不得发布部分数据。
- 接入源内联/URL、本地 `.lrc/.ttml/.xml` 和现有回退内容；缓存记录格式和解析器版本；不新增 Apple Music 抓取。

## 5. UI

- **通用 dialog**：基于现有 `m.dialog` 自绘（scrim + 卡片 + 标题/正文/按钮），Viva 无 modal 组件；用于导入、加歌单、确认框。
- **toast**：右下角轻量栈，3–4 秒消失；用于登录过期、导入结果、播放失败。
- **我的收藏**：标题右侧“从…导入”；列表查 `favorites`，join `tracks`，来源图标聚合 `source_bindings`；无可用绑定的曲目仍显示，标注“无可用音源”，点击 toast；收录音乐对用户无感，不单独展示。
- **搜索**：平台 tabs + 搜索框；结果行动作“收藏 / 加入歌单”；“加入歌单”只列本地歌单；点歌追加当前队列，不替换、不自动收藏。
- **我的歌单**：顶部“新建歌单”“导入歌单”；导入歌单显示“来源：华为音乐 · 只读”和“更新”，不能加歌/移歌；账号与上次快照不同时，点“更新”先弹确认，说明会覆盖成员但不会删除收藏；本地歌单可加歌/移歌/改名/删除。
- **设置→音源**：账号登录/退出、导入入口、决策模型配置（“决策模型配置（可选）”，一个“保存配置”+“测试连接”）；明文 HTTP 警告；不设固定调用上限。

## 6. 去 Node 工具链

- 删除 `package.json`、`package-lock.json`、`native/scripts/*.mjs`、`native/app-name.mjs`、`native/assets/lucide/*.mjs`、devDependency `playwright-core`、CI 的 Node 步骤。
- 新增 `native/cmd/tinglan-tool`：`build|start|mac-app|package|ui-check|assets|icons|licenses|ci-targets|version`；`native/internal/appmeta` 保存版本/显示名/bundle id。
- 保留 bundle id `org.tinglan.unofficial.internal.viva`、数据目录 `tinglan/native-v1`（旧目录自动迁移）、显示名“听澜”、Lucide ISC 与 icon 许可。
- 图标以已提交 PNG 母版生成 PNG/ICO/ICNS；SVG 仅设计源，不用不支持滤镜的渲染器悄悄改图标。
- `check-ui.mjs` 移植到 Go：构建 `-tags=vivahead`，临时 profile 驱动 layout/shot/click，保留 33/1 项检查；不把 `net/http 200` 冒充 OAuth 等价验证。
- 顺序：先建立 Go 工具并验证等价产物/CI，再移除 Node 入口；本设计不执行删除。

## 7. 实施顺序（确认后）

1. SQLite/schema/迁移：唯一约束、身份/refs/投影、`ResolveTrackID`、迁移日志与原子文件发布；用合成 profile 做崩溃点测试。
2. 导入与并发：华为完整快照契约、staging/代次/单事务发布；收藏对账、歌单镜像/硬删除；暂不实现网易。
3. 合并与 Jev：候选计划、before-state、依赖撤销、`merge_blocks`、缓存；网络/Jev 不持 DB 锁。
4. UI：收藏导入 dialog、搜索 tabs、歌单入口/状态、toast；进度按 run/账号代次过滤；源未注册不提供假入口。
5. TTML：时间基准/拒绝行为、本地导入、缓存、舞台逐字高亮；保持 LRC 可用。
6. 去 Node：Go 工具链、appmeta、PNG 图标、headless UI 检查、CI；先验等价再删旧入口。
7. 构建、`go vet`、单测、UI 回归。
8. 经用户授权后再做真实华为账号/权益验证与 Windows 实机验收；没证据不宣称 full 权益/Windows 验收完成。
9. 不自动提交、推送、发布。

## 8. 审查闭环

- **3 blockers**：完整快照的提交边界；合并撤销的 before-state/旧 ID 解析；旧歌单 list/detail 缓存读取。均已落到 §4.2/§4.3/§4.5 与 Astra 文档 §6/§9/§10.4。
- **11 majors**：共享绑定不误删；永久身份/import_seq；账号代次隔离（当前按单账号简化，不保留拥有者保护）；根/账号队列迁移；迁移日志与跨文件恢复；歌单幂等唯一约束；imports 状态/deleted；并发查插与事务；源快照无损字段；审计不作缓存；TTML 父级时间。均已闭合。
- **字段级 A–R**：迁移计划唯一键/attempt、派生 active 移除、track_id 反查索引、依赖反向索引、run 状态索引、艺人倒排、派生字段维护、账号过期字段、stage ordinal、审计格式版本、路径边界、revision 粒度、归一化映射、account_id/account_key、seq 高水位、JSON 限额、队列启动顺序、origin_key 编码。逐条见 Astra 文档 §15。
- **本轮决策**：
  - 导入歌单单副本：`(profile, source, source_playlist_id)` 唯一；不记录拥有者、不做跨账号保护；任意登录账号可更新，按当前账号快照覆盖；不建多账号副本，等实际 issue 再迁移。
  - `binding_origins.origin_kind='local'` 是“曾显式收录”的持久来源依据，不是当前收藏/歌单成员关系；取消收藏、移出本地歌单、删除本地歌单都不撤销该绑定。
- **验证**：Astra 文档的完整 DDL 已在内存 SQLite 重新执行并通过 **25/25 项**检查，覆盖单副本唯一性、同一集合重复 imports 拒绝、按 committed_revision 读取最新成功快照、异账号有快照需确认、同账号/无快照/从未导入/已删除/收藏更新不弹、取消零写入、确认后正常建 run、确认后切号旧 run 被拒、dialog 期间账号/目标代次变化使旧准备失效、14 条 finding 与 A–R 18 条追溯完整；详见 Astra 文档 §15.1。
- **账号变化交互**：不禁止、不静默覆盖；仅在“已有成功快照或迁移基线 + 当前账号不同”时，点击“更新”弹“取消 / 仍然更新”。快照来源以最新 committed run 的 `account_id` 为准，迁移基线取 manifest 的 selected 账号，不读可变的 `imports.account_id`。确认只授权本次任务创建，不成为永久授权。

## 9. 待确认与边界

- 设计已按用户决定简化为单账号口径：导入歌单只保留一份副本，不记录拥有者、不做跨账号保护；账号变化时更新前确认覆盖，不静默覆盖；不实现多账号并行镜像。若以后实际遇到多账号 issue，再单独设计 per-account 副本迁移。
- `binding_origins.origin_kind='local'` 是持久“曾显式收录”标记，不随取消收藏/移出本地歌单/删除本地歌单撤销；它不代表可播放。
- 不能从缺失的旧账号缓存还原其完整快照；无对应缓存时保留账号元数据并标待更新，不用另一账号成员补全。
- 无外键设计仍需实现层严格校验账号引用和任务代次；不能把“无 FK”当跳过完整性检查。
- `local` 来源存在不代表可播放：仍需当前账号登录且实时 `Resolve` 成功。
- 真实华为快照/权益、迁移故障恢复、Windows 实机验收必须在实施阶段验证；网易云适配器暂不实现。
- 设计待你最终确认；旧实现（`native/internal/canonical` 等）仍是未提交的第一阶段代码，后续按本设计迁移。
