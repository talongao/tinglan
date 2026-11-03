# 多源收藏与歌单数据库修订设计（ASTRA）

日期：2026-11-01。状态：可供实施的设计修订，尚未实现、尚未实机验收。
依据：`MULTISOURCE_DB_DESIGN.md`、本轮确认范围，以及上一轮 3 blocker + 11 major。
本稿独立保存，不覆盖原稿。只实现华为适配器与通用能力，网易云适配器暂不实现。

## 1. 范围与不可变约定

1. 收藏是本地数据。远端更新只增加/刷新收藏和对账来源，不删除收藏成员；账号变化不会覆盖收藏。覆盖风险仅指导入歌单的成员镜像，不删除收录音乐或收藏；没有有效绑定仍展示，不可播放。
2. 歌单是收藏夹。只有本地歌单接受加入/移除歌曲；导入歌单只读，其成员按完整远端快照镜像。显式删除歌单或确认远端歌单删除时，硬删除本地副本和成员。
3. “更新只做 upsert”指正式数据更新：来源消失采用 `active=0` 状态更新，不物理删除永久身份、绑定、收藏或收录记录。歌单镜像删除成员是更新流程中的唯一成员删除例外。用户显式取消收藏、删除歌单，以及合并时对同一成员的内部去重，不属于远端更新删收藏。
4. `tracks` 是内部规范记录；有效根一首录音一行，旧 ID/合并前行作为别名保留。用户不见收录表。`favorites`、`playlist_tracks` 只引用曲目 ID，不存歌曲元数据。
5. 队列、历史、歌词、封面、设置、凭据继续用文件；DB 不存播放 URL、token、Cookie、Jev Key。搜索临时播放不创建 DB 曲目。
6. 导入歌单的播放限制只写队列 `onlySource`，同一个 Resolve 接口筛选候选，不建立另一套播放器。
7. SQLite 不建 FOREIGN KEY。逻辑约束通过唯一索引、CHECK、单写入口和事务内存在性检查保证。Jev 不设固定调用次数上限。
8. 本稿的本地歌单、TTML、Go 工具链范围按本轮用户要求执行；不增加远端创建歌单、添加歌曲或收藏写回能力。

### 1.1 保留的用户交互契约

- 我的收藏标题旁提供“从…导入”，使用 dialog（不是 modal API）列出支持平台。未导入显示“导入”、未登录显示“去登录”、已导入显示“更新”、失败显示“重试”（可细分重试导入/更新）。dialog 显示页进度、去重数量、失败原因并允许取消；未完成分页时数量只称“已处理”，不冒充总数。
- 导入/更新确认登录过期时，先检查并尝试源提供的 RefreshLogin；华为不支持刷新则直接退出该源登录态并 toast 提示重新登录。支持刷新的源本轮仅重试一次刷新，成功后用新代次重新开始完整快照，失败退出；不建立定时自动续期或重试循环。
- 搜索页按 settings.Sources 中 enabled 且已注册的平台生成 tabs，默认第一个，先选平台再搜索；未登录 tab 显示登录引导。搜索点歌追加当前队列，不替换队列，不自动收录。
- 本地歌单支持创建、改名、删除、加歌和移歌，多源播放。“加入歌单”dialog 仅列本地歌单。导入歌单只读、镜像远端成员，用 onlySource 限定播放；确认远端删除时硬删副本并将入口恢复为“导入”。
- 按2026-11-01决定（m03618），已有成功快照或迁移基线的导入歌单，当前源账号与快照来源账号不同时，点击“更新”先弹确认dialog：“账号不同，会按当前账号的完整快照覆盖该导入歌单的成员，但不会删除收录音乐或收藏。”按钮为“取消 / 仍然更新”。取消不建run、不改任何状态；确认后才创建run。同账号、从未导入、已删除或无快照不弹；收藏导入不适用此提示。
- 收藏和加入歌单使用同一收录入口：精确源身份优先，其次代码相似度，Jev 只增强灰区，最后才写成员 ID。没有有效绑定仍显示但不可播放。
- 旧收藏、根/账号队列和自己歌单缓存自动迁移，不要求用户重新导入来恢复本地数据；缺失账号归属不能靠重新导入作为迁移成功条件。网络更新只是后续验证来源的独立动作。
- Jev 默认关闭，按 token 计费，能不用则不用；无固定调用次数上限。TTML、Go 工具链、去掉 Node/package.json/npm 的范围保持；代码在设计确认后才改，本轮只改本文件。

## 2. 存储与类型约定

- DB：现有 `os.UserConfigDir()/tinglan/native-v1/library.sqlite3`（首次启动从旧目录 `huawei-music-desktop/native-v1` 整体迁移）。通过 `filepath.Join` 构造，不依赖进程 cwd。
- 时间统一 UTC Unix 毫秒 INTEGER；时长为非负毫秒，可空表示未知。JSON 字段使用 Go 明确 DTO 编码，不保存平台原始响应。
- 新 track/playlist/run/merge ID 用随机 128 bit、小写 hex，加 `trk_`/`pl_`/`run_`/`merge_` 前缀。旧规范 ID原样保留，不根据标题重算、不补前缀。
- `account_key` 为本地稳定摘要，不是显示名、token 或登录会话 ID。华为沿用旧账号目录使用的摘要算法，确保旧目录可关联；无法确认账号的历史数据用独立 `legacy:unassigned`，不擅自绑定当前账号。
- 平台曲目 ID 不透明且区分大小写；禁止用裸 ID 作为文件路径。缓存文件名用 `SHA256(source + NUL + sourceTrackID + NUL + formatVersion)`；避免 Windows 冒号、路径穿越与大小写文件名碰撞。
- SQLite 运行库要求支持 JSON 函数（至少 SQLite 3.38）；采用可跨目标平台构建的 Go 驱动，不新增 Node 或服务进程。部署锁定版本，不依赖操作系统 SQLite 功能碰运气。
- 每个连接设置 `busy_timeout=5000`；DB 初始化使用 WAL、`synchronous=FULL`。应用级单写队列，写事务统一 `BEGIN IMMEDIATE`；忙时在事务外有限退避，不在事务中调用网络/Jev。
- DB、WAL、SHM、备份和文件均在私有目录；POSIX 目录 0700、文件 0600，Windows 使用当前用户目录 ACL 并验收，不把 chmod 当作 ACL 证明。拒绝预置符号链接/重解析点将数据导向目录外。
- schema 升级在事务中执行 DDL 和版本登记；发现未来版本拒写。备份使用 SQLite backup API/一致性快照，不复制正在写的单个 DB 文件遗漏 WAL。

## 3. 表清单与逻辑关系

| 表 | 用途、关键约束 |
|---|---|
| schema_migrations | schema 版本、DDL 校验摘要；仅完成后登记 |
| profiles | 当前仅 `local`；不引入多用户功能 |
| library_clock | 单行全局 `revision` 和永久 `import_seq` 高水位 |
| tracks | 原始规范记录、标题索引和 `merged_into` 别名边；不物理删除 |
| track_artists | 原始曲目规范艺人倒排，`(track_id,artist_norm)` 主键、反向索引 |
| source_accounts | `(source,account_key)` 唯一；每源最多一个 current；会话代次 |
| source_identities | 永久 `(source,source_track_id)` 映射、原始归属和首次导入序号 |
| source_bindings | 身份与账号的绑定；不存 active，以 origins 的 EXISTS 推导；权限是带代次的观察 |
| binding_origins | 一个绑定来自哪个集合/显式本地收录操作；集合对账只更新自己的关系 |
| favorites | 当前收藏投影，主键 `(profile_id,track_id)`；无歌曲元数据 |
| playlists | 本地/导入歌单实体；导入歌单 `(profile,source,remote ID)` 部分唯一；不存账号列 |
| playlist_tracks | 当前歌单成员投影；主键 `(playlist_id,track_id)` |
| member_refs | 合并前的成员原始归属依据，无歌曲元数据；合并/撤销重算投影，保留后续编辑 |
| imports | 按profile/种类/带源命名空间集合键唯一；account_id仅记录最近一次导入/更新使用的账号，本地副本ID可空 |
| import_runs | 每次网络任务的账号/目标代次、快照状态、提交结果 |
| import_stage | run 内去重的完整快照曲目、原始出现顺序和拟定匹配计划 |
| merge_decisions | 只记合并审计，不作判断缓存 |
| merge_before | 合并的 before/after、实际修改字段、关联成员证明及原始归属 |
| merge_dependencies | 后一次合并依赖哪次未撤销合并，支持逆序撤销 |
| merge_blocks | 已撤销合并的两组身份隔离约束，防自动立即重合并 |
| jev_judgements | 完整上下文键、分数和 TTL；仅成功响应可缓存 |
| data_migrations | `(migration_key,input_digest)` 唯一计划，独立 ID；阶段和文件发布恢复信息 |
| migration_attempts | 每个迁移计划的尝试历史；重试新增 attempt，不抹掉旧错误 |

逻辑关系：身份指向原始 tracks；`ResolveTrackID` 得到当前根；账号绑定指向身份，来源关系指向绑定。
成员依据 `member_refs` 指向原始 tracks，投影 favorites/playlist_tracks 指向当前根。
导入状态指向账号和远端集合；任务指向导入状态；stage 仅属于任务。
歌单硬删除时删除其 member_refs 和 playlist_tracks；imports 保留远端键，但清空本地副本引用。
所有行引用在写事务中检查；不能用“无外键”作为跳过完整性校验的理由。

## 4. 完整 DDL

下列是一份新库 schema。正式实现按 schema_migrations 顺序执行，不用 `IF NOT EXISTS` 掩盖已存在但结构不兼容的表。JSON CHECK 检查容器、字节大小和数组数量；字段形状、数值有限性由 §4.2 的同一仓储 DTO 校验。所有 INTEGER 输入在 Go 端检查类型/溢出，不依赖 SQLite 类型亲和自动纠正字符串。

```sql
CREATE TABLE schema_migrations (
  version INTEGER PRIMARY KEY,
  ddl_digest TEXT NOT NULL,
  applied_at INTEGER NOT NULL
);
CREATE TABLE profiles (
  id TEXT PRIMARY KEY NOT NULL,
  name TEXT NOT NULL,
  created_at INTEGER NOT NULL
);
CREATE TABLE library_clock (
  singleton INTEGER PRIMARY KEY CHECK(singleton=1),
  revision INTEGER NOT NULL DEFAULT 0 CHECK(revision>=0),
  import_seq INTEGER NOT NULL DEFAULT 0 CHECK(import_seq>=0)
);
INSERT INTO library_clock(singleton) VALUES(1);

CREATE TABLE tracks (
  id TEXT PRIMARY KEY NOT NULL,
  title TEXT NOT NULL,
  artists_json TEXT NOT NULL CHECK(length(CAST(artists_json AS BLOB))<=65536 AND json_valid(artists_json) AND json_array_length(artists_json)<=64 AND json_type(artists_json)='array'),
  album_json TEXT NOT NULL CHECK(length(CAST(album_json AS BLOB))<=8192 AND json_valid(album_json) AND json_type(album_json) IN ('object','null')),
  version_json TEXT NOT NULL CHECK(length(CAST(version_json AS BLOB))<=2048 AND json_valid(version_json) AND json_type(version_json)='object'),
  version_key TEXT NOT NULL,
  cover_url TEXT NOT NULL DEFAULT '',
  duration_ms INTEGER CHECK(duration_ms>=0),
  norm_title TEXT NOT NULL,
  norm_artists TEXT NOT NULL,
  merged_into TEXT CHECK(merged_into IS NULL OR merged_into<>id),
  revision INTEGER NOT NULL DEFAULT 1 CHECK(revision>0),
  created_at INTEGER NOT NULL,
  updated_at INTEGER NOT NULL
);
CREATE INDEX idx_tracks_candidates ON tracks(norm_title,version_key)
  WHERE merged_into IS NULL;
CREATE INDEX idx_tracks_parent ON tracks(merged_into) WHERE merged_into IS NOT NULL;
CREATE TABLE track_artists (
  track_id TEXT NOT NULL,
  artist_norm TEXT NOT NULL CHECK(length(CAST(artist_norm AS BLOB)) BETWEEN 1 AND 1024),
  PRIMARY KEY(track_id,artist_norm)
);
CREATE INDEX idx_artist_tracks ON track_artists(artist_norm,track_id);

CREATE TABLE source_accounts (
  id TEXT PRIMARY KEY NOT NULL,
  source TEXT NOT NULL,
  account_key TEXT NOT NULL,
  account_label TEXT NOT NULL DEFAULT '',
  state TEXT NOT NULL CHECK(state IN ('logged_in','expired','logged_out')),
  is_current INTEGER NOT NULL DEFAULT 0 CHECK(is_current IN (0,1)),
  session_generation INTEGER NOT NULL DEFAULT 0 CHECK(session_generation>=0),
  credential_ref TEXT,
  expires_at INTEGER,
  last_verified_at INTEGER,
  updated_at INTEGER NOT NULL,
  UNIQUE(source,account_key)
);
CREATE UNIQUE INDEX ux_account_current ON source_accounts(source) WHERE is_current=1;

CREATE TABLE source_identities (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  source TEXT NOT NULL,
  source_track_id TEXT NOT NULL,
  home_track_id TEXT NOT NULL,
  import_seq INTEGER NOT NULL CHECK(import_seq>=0),
  first_imported_at INTEGER NOT NULL,
  title TEXT NOT NULL,
  artists_json TEXT NOT NULL CHECK(length(CAST(artists_json AS BLOB))<=65536 AND json_valid(artists_json) AND json_array_length(artists_json)<=64 AND json_type(artists_json)='array'),
  album_json TEXT NOT NULL CHECK(length(CAST(album_json AS BLOB))<=8192 AND json_valid(album_json) AND json_type(album_json) IN ('object','null')),
  version_json TEXT NOT NULL CHECK(length(CAST(version_json AS BLOB))<=2048 AND json_valid(version_json) AND json_type(version_json)='object'),
  version_key TEXT NOT NULL,
  cover_url TEXT NOT NULL DEFAULT '',
  duration_ms INTEGER CHECK(duration_ms>=0),
  metadata_digest TEXT NOT NULL,
  revision INTEGER NOT NULL DEFAULT 1 CHECK(revision>0),
  updated_at INTEGER NOT NULL,
  UNIQUE(source,source_track_id)
);
CREATE INDEX idx_identity_home ON source_identities(home_track_id,import_seq,id);

CREATE TABLE source_bindings (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  identity_id INTEGER NOT NULL,
  account_id TEXT NOT NULL,
  access TEXT NOT NULL DEFAULT 'unknown' CHECK(access IN ('unknown','full','preview')),
  quality TEXT NOT NULL DEFAULT '',
  evidence_json TEXT NOT NULL DEFAULT '{}' CHECK(length(CAST(evidence_json AS BLOB))<=8192 AND json_valid(evidence_json) AND json_type(evidence_json)='object'),
  checked_generation INTEGER,
  checked_at INTEGER,
  expires_at INTEGER,
  last_seen_at INTEGER NOT NULL,
  UNIQUE(identity_id,account_id)
);
CREATE INDEX idx_binding_account ON source_bindings(account_id,identity_id);
CREATE TABLE binding_origins (
  binding_id INTEGER NOT NULL,
  origin_kind TEXT NOT NULL CHECK(origin_kind IN ('favorites','playlist','local','legacy')),
  origin_key TEXT NOT NULL,
  active INTEGER NOT NULL CHECK(active IN (0,1)),
  last_seen_run TEXT,
  updated_at INTEGER NOT NULL,
  PRIMARY KEY(binding_id,origin_kind,origin_key)
);
CREATE INDEX idx_origin_collection ON binding_origins(origin_kind,origin_key,active);
CREATE INDEX idx_origin_active_binding ON binding_origins(binding_id) WHERE active=1;

CREATE TABLE favorites (
  profile_id TEXT NOT NULL,
  track_id TEXT NOT NULL,
  position INTEGER NOT NULL CHECK(position>=0),
  added_at INTEGER NOT NULL,
  PRIMARY KEY(profile_id,track_id)
);
CREATE INDEX idx_favorites_order ON favorites(profile_id,position,track_id);
CREATE INDEX idx_favorites_track ON favorites(track_id);
CREATE TABLE playlists (
  id TEXT PRIMARY KEY NOT NULL,
  profile_id TEXT NOT NULL,
  kind TEXT NOT NULL CHECK(kind IN ('local','imported')),
  source TEXT,
  source_playlist_id TEXT,
  name TEXT NOT NULL,
  description TEXT NOT NULL DEFAULT '',
  cover_url TEXT NOT NULL DEFAULT '',
  track_count INTEGER NOT NULL DEFAULT 0 CHECK(track_count>=0),
  revision INTEGER NOT NULL DEFAULT 1 CHECK(revision>0),
  created_at INTEGER NOT NULL,
  updated_at INTEGER NOT NULL,
  last_synced_at INTEGER,
  CHECK((kind='local' AND source IS NULL AND source_playlist_id IS NULL)
     OR (kind='imported' AND source IS NOT NULL AND length(source)>0
         AND source_playlist_id IS NOT NULL AND length(source_playlist_id)>0))
);
CREATE UNIQUE INDEX ux_playlists_remote ON playlists(profile_id,source,source_playlist_id)
  WHERE kind='imported';
CREATE INDEX idx_playlists_profile ON playlists(profile_id,updated_at,id);
CREATE TABLE playlist_tracks (
  playlist_id TEXT NOT NULL,
  track_id TEXT NOT NULL,
  position INTEGER NOT NULL CHECK(position>=0),
  added_at INTEGER NOT NULL,
  PRIMARY KEY(playlist_id,track_id)
);
CREATE INDEX idx_playlist_order ON playlist_tracks(playlist_id,position,track_id);
CREATE INDEX idx_playlist_track ON playlist_tracks(track_id);

CREATE TABLE member_refs (
  owner_kind TEXT NOT NULL CHECK(owner_kind IN ('favorites','playlist')),
  owner_id TEXT NOT NULL,
  ref_key TEXT NOT NULL,
  anchor_track_id TEXT NOT NULL,
  position INTEGER NOT NULL CHECK(position>=0),
  added_at INTEGER NOT NULL,
  revision INTEGER NOT NULL DEFAULT 1 CHECK(revision>0),
  PRIMARY KEY(owner_kind,owner_id,ref_key)
);
CREATE INDEX idx_member_anchor ON member_refs(anchor_track_id);

CREATE TABLE imports (
  id TEXT PRIMARY KEY NOT NULL,
  profile_id TEXT NOT NULL,
  account_id TEXT NOT NULL,
  source TEXT NOT NULL,
  kind TEXT NOT NULL CHECK(kind IN ('favorites','playlist')),
  remote_collection_id TEXT NOT NULL,
  local_playlist_id TEXT,
  status TEXT NOT NULL DEFAULT 'never'
    CHECK(status IN ('never','imported','updated','failed','expired','deleted')),
  target_generation INTEGER NOT NULL DEFAULT 0 CHECK(target_generation>=0),
  last_attempt_at INTEGER,
  last_success_at INTEGER,
  added_count INTEGER NOT NULL DEFAULT 0 CHECK(added_count>=0),
  updated_count INTEGER NOT NULL DEFAULT 0 CHECK(updated_count>=0),
  error_code TEXT NOT NULL DEFAULT '',
  updated_at INTEGER NOT NULL,
  CHECK(json_valid(remote_collection_id) AND json_type(remote_collection_id)='array'
    AND json_array_length(remote_collection_id)=2
    AND json_type(remote_collection_id,'$[0]')='text'
    AND json_extract(remote_collection_id,'$[0]')=source
    AND json_type(remote_collection_id,'$[1]')='text'),
  CHECK((kind='favorites' AND json_extract(remote_collection_id,'$[1]')=''
         AND local_playlist_id IS NULL AND status<>'deleted')
     OR (kind='playlist' AND length(json_extract(remote_collection_id,'$[1]'))>0)),
  UNIQUE(profile_id,kind,remote_collection_id)
);
CREATE INDEX idx_import_playlist ON imports(local_playlist_id) WHERE local_playlist_id IS NOT NULL;
CREATE TABLE import_runs (
  id TEXT PRIMARY KEY NOT NULL,
  import_id TEXT NOT NULL,
  account_id TEXT NOT NULL,
  account_generation INTEGER NOT NULL CHECK(account_generation>=0),
  target_generation INTEGER NOT NULL,
  state TEXT NOT NULL CHECK(state IN ('fetching','ready','committed','failed','cancelled','superseded')),
  outcome TEXT CHECK(outcome IN ('snapshot','remote_deleted')),
  snapshot_token TEXT,
  cursor TEXT NOT NULL DEFAULT '',
  unique_count INTEGER NOT NULL DEFAULT 0 CHECK(unique_count>=0),
  plan_revision INTEGER,
  committed_revision INTEGER,
  added_count INTEGER CHECK(added_count>=0),
  updated_count INTEGER CHECK(updated_count>=0),
  playlist_meta_json TEXT CHECK(playlist_meta_json IS NULL OR
    (length(CAST(playlist_meta_json AS BLOB))<=32768 AND json_valid(playlist_meta_json) AND json_type(playlist_meta_json)='object')),
  error_code TEXT NOT NULL DEFAULT '',
  started_at INTEGER NOT NULL,
  finished_at INTEGER
);
CREATE UNIQUE INDEX ux_import_live_run ON import_runs(import_id)
  WHERE state IN ('fetching','ready');
CREATE INDEX idx_runs_import_state_time ON import_runs(import_id,state,started_at DESC,id);
CREATE INDEX idx_runs_import_time ON import_runs(import_id,started_at DESC,id);
CREATE TABLE import_stage (
  run_id TEXT NOT NULL,
  source_track_id TEXT NOT NULL,
  ordinal INTEGER NOT NULL CHECK(ordinal>=0),
  metadata_json TEXT NOT NULL CHECK(length(CAST(metadata_json AS BLOB))<=131072 AND json_valid(metadata_json) AND json_type(metadata_json)='object'),
  metadata_digest TEXT NOT NULL,
  plan_json TEXT CHECK(plan_json IS NULL OR (length(CAST(plan_json AS BLOB))<=262144 AND json_valid(plan_json) AND json_type(plan_json)='object')),
  PRIMARY KEY(run_id,source_track_id),
  UNIQUE(run_id,ordinal)
);
CREATE INDEX idx_stage_order ON import_stage(run_id,ordinal,source_track_id);

CREATE TABLE merge_decisions (
  id TEXT PRIMARY KEY NOT NULL,
  left_track_id TEXT NOT NULL,
  right_track_id TEXT NOT NULL,
  winner_track_id TEXT NOT NULL,
  loser_track_id TEXT NOT NULL,
  method TEXT NOT NULL CHECK(method IN ('code','jev')),
  score REAL NOT NULL CHECK(score>=0 AND score<=1),
  rules_version TEXT NOT NULL,
  template_version TEXT NOT NULL,
  decision_digest TEXT NOT NULL,
  jev_cache_key TEXT,
  applied_revision INTEGER NOT NULL,
  created_at INTEGER NOT NULL,
  revoked_at INTEGER,
  CHECK(winner_track_id<>loser_track_id)
);
CREATE INDEX idx_merge_pair ON merge_decisions(left_track_id,right_track_id);
CREATE TABLE merge_before (
  merge_id TEXT NOT NULL,
  format_version INTEGER NOT NULL DEFAULT 1 CHECK(format_version=1),
  seq INTEGER NOT NULL,
  entity_kind TEXT NOT NULL CHECK(entity_kind IN ('track','identity','member','projection')),
  entity_key TEXT NOT NULL,
  changed_fields_json TEXT NOT NULL CHECK(length(CAST(changed_fields_json AS BLOB))<=4096 AND json_valid(changed_fields_json) AND json_type(changed_fields_json)='array'),
  before_json TEXT NOT NULL CHECK(length(CAST(before_json AS BLOB))<=262144 AND json_valid(before_json)),
  after_json TEXT NOT NULL CHECK(length(CAST(after_json AS BLOB))<=262144 AND json_valid(after_json)),
  PRIMARY KEY(merge_id,seq)
);
CREATE TABLE merge_dependencies (
  merge_id TEXT NOT NULL,
  depends_on TEXT NOT NULL,
  PRIMARY KEY(merge_id,depends_on),
  CHECK(merge_id<>depends_on)
);
CREATE INDEX idx_merge_dependants ON merge_dependencies(depends_on,merge_id);
CREATE TABLE merge_blocks (
  merge_id TEXT NOT NULL,
  chunk_no INTEGER NOT NULL CHECK(chunk_no>=0),
  format_version INTEGER NOT NULL DEFAULT 1 CHECK(format_version=1),
  left_anchors_json TEXT NOT NULL CHECK(length(CAST(left_anchors_json AS BLOB))<=65536 AND json_valid(left_anchors_json) AND json_type(left_anchors_json)='array' AND json_array_length(left_anchors_json)<=512),
  right_anchors_json TEXT NOT NULL CHECK(length(CAST(right_anchors_json AS BLOB))<=65536 AND json_valid(right_anchors_json) AND json_type(right_anchors_json)='array' AND json_array_length(right_anchors_json)<=512),
  created_at INTEGER NOT NULL,
  PRIMARY KEY(merge_id,chunk_no)
);
CREATE TABLE jev_judgements (
  cache_key TEXT PRIMARY KEY NOT NULL,
  pair_digest TEXT NOT NULL,
  score REAL NOT NULL CHECK(score>=0 AND score<=1),
  model_id TEXT NOT NULL,
  resolved_model_version TEXT,
  endpoint_identity TEXT NOT NULL,
  credential_generation INTEGER NOT NULL,
  rules_version TEXT NOT NULL,
  template_version TEXT NOT NULL,
  created_at INTEGER NOT NULL,
  expires_at INTEGER NOT NULL,
  invalidated_at INTEGER,
  CHECK(expires_at>created_at)
);
CREATE INDEX idx_jev_pair ON jev_judgements(pair_digest);
CREATE INDEX idx_jev_expiry ON jev_judgements(expires_at);

CREATE TABLE data_migrations (
  id TEXT PRIMARY KEY NOT NULL,
  migration_key TEXT NOT NULL,
  input_digest TEXT NOT NULL,
  inputs_json TEXT NOT NULL CHECK(length(CAST(inputs_json AS BLOB))<=1048576 AND json_valid(inputs_json) AND json_type(inputs_json)='array'),
  state TEXT NOT NULL CHECK(state IN ('prepared','db_committed','file_ready','published','done','failed')),
  completed_phase TEXT NOT NULL DEFAULT 'none'
    CHECK(completed_phase IN ('none','db_committed','file_ready','published','done')),
  destination_relpath TEXT,
  temp_relpath TEXT,
  output_digest TEXT,
  error_code TEXT NOT NULL DEFAULT '',
  updated_at INTEGER NOT NULL,
  UNIQUE(migration_key,input_digest)
);
CREATE TABLE migration_attempts (
  migration_id TEXT NOT NULL,
  attempt_no INTEGER NOT NULL CHECK(attempt_no>0),
  state TEXT NOT NULL CHECK(state IN ('running','succeeded','failed','interrupted')),
  start_phase TEXT NOT NULL CHECK(start_phase IN ('none','db_committed','file_ready','published','done')),
  end_phase TEXT CHECK(end_phase IN ('none','db_committed','file_ready','published','done')),
  error_code TEXT NOT NULL DEFAULT '',
  started_at INTEGER NOT NULL,
  finished_at INTEGER,
  PRIMARY KEY(migration_id,attempt_no)
);
CREATE UNIQUE INDEX ux_migration_running ON migration_attempts(migration_id) WHERE state='running';
```

### 4.1 字段语义补充

- `source_identities.home_track_id` 是原始归属，不因集合消失或合并而丢失。普通刷新不改它；合并通过 tracks 别名改变有效归属。`import_seq` 永不重置，历史重复序号保留，以 identity.id 打破平局。
- `source_bindings` 不保存 active；可用来源通过关联 `EXISTS(SELECT 1 FROM binding_origins o WHERE o.binding_id=b.id AND o.active=1)` 推导，登录态不改来源事实。实际可播放还须账号 current、logged_in，且 Resolve 成功。
- `origin_key` 的编码字典见 §4.2：远端集合引用 imports.id；local/legacy 使用定长结构的确定性 JSON 数组，不使用有歧义的字符串拼接。local 来源不是 UI 收藏类型，它只说明音源仍有独立收录依据，不受远端收藏差集删除。
- `member_refs` 是可逆合并必需的成员归属记录，不是第二个用户收藏列表。favorites 的 owner_id=profile ID；playlist 的 owner_id=playlist ID。ref_key 使用 `identity:<id>` 或 `local:<anchorID>`，稳定且不含账号凭据。
- 有效根相同的 refs 投影为一行，position 取最小值、added_at 取最早值；相同 position 按 track_id 排序。列表总数统计投影行数，不能统计 refs 或平台分页行数。
- `tracks` 和身份快照的 JSON：artists 为 `[{"id":"...","name":"..."}]`，album 为 null 或 `{"id":"...","title":"..."}`；version 保存全部 live/remix/acoustic/instrumental/cover/demo 布尔标记。艺人 ID 只在源内有意义。`norm_artists` 是规范化名称集合的确定性 JSON，不用含糊分隔字符串。
- `metadata_digest` 包含原始匹配字段和明确单位；不包含图片临时查询参数、播放地址或凭据。封面仅接受源资源策略允许的地址，日志不输出完整带签名 URL。
- `merge_before` 的 JSON 是内部行快照，不是平台原始响应，绝不包含 vault 或队列文件。审计只允许固定 entity_kind 和固定字段集合，恢复时不得动态执行 JSON 中的 SQL/表名。

### 4.2 字段字典、限额与迁移映射

所有 JSON 都是 UTF-8，SQL 上限按 `length(CAST(value AS BLOB))` 计算字节；仓储写入前先限制原始输入再解码，禁止靠截断满足限额。外部页出现超限项使该轮不能执行差集；旧文件超限保持原文件并报告具体字段，不能悄悄少迁数据。

| 字段类别 | 规范与映射、DTO 边界 |
|---|---|
| 时间字段 `_at`、expires_at | UTC Unix 毫秒 int64；未知用 SQL NULL（仅可空字段），不写空字符串；旧 RFC3339/time.Time 解析时区后转换。NULL 的 last_verified_at 表示从未确认；expires_at 仅存适配器语义明确的截止时间，不猜时区或固定时长 |
| duration_ms | NULL=未知，0=真实零时长；旧 durationSeconds 必须有限且非负，乘1000后四舍五入，转换前检查 int64 边界；不把负值钳成0 |
| access/quality | 旧 Access 0/1/2 → unknown/full/preview，其余拒绝；quality 缺失为空字符串。access 是带 checked_generation 的历史观察，过期/换号后按 unknown，不当当前授权 |
| Disabled/账号归属 | 旧 Disabled=true 的迁移 origin.active=0，false=1；绑定账号归属无法证明时为 legacy:unassigned 且 logged_out。即使 origin=1 也不能自动播放或认领当前账号；迁移保留全部本地成员 |
| artists_json | 数组最多64项、序列化<=64KiB；每项仅 id/name 字符串，id<=1024字节、name<=512字节，缺 id 用空串；原顺序及显示文字保留，不用展示连接符反拆艺人；空数组表示未知艺人，不写虚构“未知艺人”参与匹配 |
| album_json | JSON null 或仅含 id/title 的对象，<=8KiB；id<=1024字节、title<=4096字节，缺 album 用 JSON `null`（非SQL NULL/空串） |
| version_json/version_key | 对象<=2KiB，固定六个布尔字段 live/remix/acoustic/instrumental/cover/demo；缺字段=false，未知字段/非布尔拒绝。version_key 唯一由固定顺序的 true 字段生成，全部false为 studio，不接受调用方单独赋值 |
| 原始标题/名称/ID/URL | track title<=4096字节，歌单名<=4096，description<=16384；技术本地ID<=128、平台曲目/歌单ID<=1024字节；cover_url<=8192字节且须资源策略校验。ID不做Unicode归一化或大小写折叠；不得将空ID写入 |
| norm_title/norm_artists/artist_norm | 原文不改。匹配文本采用固定版本的 NFKC → Unicode默认完整case-fold → NFKC → 空白折叠/trim；不去音调、不把文字转拼音。版本标签解析在通用清洗前执行。归一化艺人去空/去重，按UTF-8字节排序得到norm_artists JSON；单token<=1024字节、总JSON<=128KiB。算法/Unicode版本属于rules_version，升级事务内重建索引并使旧匹配计划/缓存失效 |
| SQL NULL vs 空串 | 可选引用、未知时间/时长、未解析模型版本用NULL；说明文字、缺quality/cover/error用空串。imports.remote_collection_id是确定性JSON数组[source,真实集合ID]，收藏的第二项固定空串，歌单第二项非空；这样不含source列的唯一键仍隔离平台。不得把字符串“null”当缺失引用 |
| evidence/stage/plan | evidence_json<=8KiB且只含允许的判定字段；单stage metadata<=128KiB且复用上述DTO；plan_json<=256KiB，超限计划在内存按项组织后分批准备但整体提交，不能截断候选冒充完整。playlist_meta<=32KiB；inputs manifest<=1MiB，超限拆成有独立migration_key的输入批次 |

`origin_key` 的唯一编码规范（UTF-8紧凑JSON使用固定数组顺序，无多余空白，不接受任意调用方字符串）：

| origin_kind | origin_key格式 | 仓储内验证 |
|---|---|---|
| favorites | 原样 imports.id | kind=favorites、source与binding相同、集合键解码的真实ID为空；写入账号必须等于run.account_id，不与可变的imports.account_id比较 |
| playlist | 原样 imports.id | kind=playlist、集合键解码的真实远端ID非空、source与binding相同；写入账号必须等于run.account_id；历史账号关系可保留 |
| local | `["local",profile_id,"identity",identity_id的十进制字符串]` | profile/identity存在，identity等于binding.identity_id |
| legacy | `["legacy",data_migrations.id]` | 对应迁移计划存在且包含该输入；不能只用可重复的migration_key |

上述 origin 唯一性都限定在 `(binding_id,origin_kind,origin_key)`。member_refs.ref_key 同样由固定构造器产生：identity ID为十进制数字，本地anchor为合法技术ID，legacy引用旧规范ID；禁止从任意拼接字符串解析身份。account_id 一律引用 `source_accounts.id`（随机 `acct_` ID）；account_key 只用于稳定识别/唯一查找，绝不是 account_id。

表中run账号检查适用于网络导入；离线迁移没有run时，以已冻结manifest中选定的输入账号核对binding，last_seen_run为NULL，不伪造网络任务。member_refs.owner_kind/owner_id仅表示成员所属的收藏夹，不表示账号权限。

### 4.3 派生字段清单与唯一维护入口

仓储仅开放 `CommitLibraryMutation`（包含迁移/合并/镜像）及 `RecordRunOutcome` 两类写入口；UI/适配器不能直接写派生列。校验/重建作为未来 Go 工具子命令 `verify` / `rebuild_projection`，本轮不实现。

| 字段/表 | 唯一来源及写入时机 | verify / rebuild_projection |
|---|---|---|
| tracks/source_identities.version_key | 各自行的version_json经同一个DTO函数导出，同事务写 | 重算比较/修复，不改原JSON |
| tracks.norm_title、norm_artists、track_artists | 原track元数据及固定rules_version；元数据变更事务内更新 | 重建标题/艺人派生字段；倒排保存所有home track艺人，查询后Resolve去重，不能仅保存winner艺人漏掉来源别名 |
| favorites、playlist_tracks、playlists.track_count | 当前member_refs + Resolve；每次成员/合并变更在同事务刷新受影响owner；count=COUNT(playlist_tracks) | 重算投影/计数。不能从远端数量或审计before恢复用户已删成员 |
| source_bindings.active | 已删除该列，唯一事实是origin.active及账号状态 | 检查origin引用；有效性仅EXISTS，无重建赋值 |
| import_runs.unique_count | stage COUNT(*)，每页同事务更新 | 活跃run可重算；清理stage前冻结终态计数 |
| 当前副本快照来源账号（查询值，不新增列） | 最新committed run的account_id；无成功run的迁移基线取已提交manifest选中账号，详见§5.3 | 不从imports.account_id重建；校验副本存在性、成功结果及manifest副本ID，失败尝试不改变快照来源 |
| imports.added_count/updated_count | 最新成功run的已冻结结果；提交时写run及imports，失败不清零 | 与最新committed_revision的run比对/修复；不是从当前收藏反推历史 |
| library_clock.import_seq | 持久高水位，与新身份同事务递增 | 必须>=MAX(identity.import_seq)，修复只升不降；绝不以当前最大值覆盖更高旧高水位 |
| library_clock.revision、行revision | 每次相关提交递增，是失效时钟而非可重算缓存 | 不重建为零；导出重建后递增全局revision |

run的 added_count 定义为本轮新增的最终成员根数；updated_count 定义为本轮出现且提交前已存在的最终成员根数（包括相同元数据刷新），按最后根去重且两者不交叉。它们是一次操作的历史结果，不随后来合并重算。committed run 与最新成功计数保留；未完成run两列为NULL。重建只修复可推导字段，坏引用/别名环/未知审计版本先报错退出，不凭空创建身份。重建获取profile写锁、暂停导入、BEGIN IMMEDIATE原子替换投影；可先verify只读核对差异。

### 4.4 逐表字段写入边界

| 表 | 额外字段审查结果与边界 |
|---|---|
| schema_migrations | version单调、ddl_digest SHA256；已登记版本摘要不符拒写，不能把IF NOT EXISTS当升级 |
| profiles | 当前仅local；name只是显示，不能成为存储路径 |
| library_clock | revision每事务一次而非每行；import_seq每新身份一次；溢出拒绝提交 |
| tracks / track_artists | merged_into必须指向存在的不同根且不形成环；row revision只在内容/结构变化时增加；artist_norm是token不是JSON，反向索引用于交集 |
| source_accounts | state/current/session_generation由账号入口写；expires_at未知可NULL，last_verified_at只在真实成功鉴权后更新，缓存命中不刷新；过期时间本身不冒充音乐接口失效证据 |
| source_identities | home_track_id、import_seq、first_imported_at普通刷新不可改；metadata_digest由DTO计算，不接收外部提供摘要 |
| source_bindings | identity/account的source须一致；checked_generation必须来自该账号请求；evidence不含URL/token；网络未知不得持久化full |
| binding_origins | kind/key按§4.2验证；last_seen_run仅远端轮次填写，local/legacy为NULL；remote对账不得触及其他key |
| favorites / playlist_tracks | 只由member_refs投影写；position允许不同根同值，最终用ID稳定排序；反查track索引支撑合并 |
| playlists | kind一经创建不切换；imported的source/remote ID非空；无账号字段；local的两个来源字段全NULL；revision用于防删除后旧更新；last_synced_at仅真实完整快照成功写 |
| member_refs | owner_kind决定owner_id的逻辑表，事务内检查存在；revision记录成员编辑，ref删除是显式操作或歌单镜像 |
| imports | account_id引用账号的source必须一致，仅记录最近一次启动导入/更新所用账号，不是权限门槛；成功快照账号查最新committed run；status与副本存在性按§8.2联查；deleted只能playlist，错误仅记录脱敏枚举code |
| import_runs | account_id捕获启动账号且不可变，引用账号的source与imports.source一致；account_generation/target_generation非负，started/finished可校验先后；plan_revision是提示非硬冲突；state转换仓储限定，terminal不可回fetching |
| import_stage | 每run的source ID与ordinal都唯一；重复页upsert必须保持原ordinal；metadata/plan经过DTO验证 |
| merge_decisions | winner/loser必须为事务内验证的根；score在Go端检查finite，时间/版本不可后改；只审计不缓存 |
| merge_before / merge_blocks | 版本、分块、大小、恢复白名单见§9.3；禁止把未知格式当空审计 |
| merge_dependencies | 依赖必须存在且更早应用，不形成环；反向索引服务撤销依赖遍历 |
| jev_judgements | score有限且[0,1]，TTL必填；key含配置/规则/模板；超时不落负缓存，过期可清理 |
| data_migrations / migration_attempts | 同摘要复用计划但重试新增attempt；输入变更新计划，已提交阶段不倒退；路径见§10.5，错误只留code |

## 5. 写入协议、身份与账号隔离

### 5.1 单写入口及乐观匹配

所有成员、身份、合并和账号代次变更走同一仓储入口。全局 library_clock.revision 只做粗粒度失效通知，不再“任意写入就整批重做Jev”。计划记录读取的track/identity行revision、根/子树身份摘要、规则版本、候选集合摘要；事务外做代码/Jev。`BEGIN IMMEDIATE` 后复查身份唯一键、目标存在性、账号/目标代次与相关行revision。

如果全局revision变了，在事务内用艺人倒排及门控重新枚举本批相关候选ID/行revision，比较候选摘要，以检测新插入候选（仅校验旧行会漏掉phantom）。若候选、相关行及子树未变，允许沿用计划；变化则回滚，在事务外只重算受影响项。现有完全相同上下文的Jev分数可复用，网络不进入事务。新增merge_blocks/规则升级也必须参与摘要/阻止关系复查。

单写提交仍是SQLite串行，接受一次大快照提交期间的写等待；stage分页、run进度、Jev缓存写入不递增library revision。library revision只在正式库/匹配事实变化时每事务+1，减少热写失效；不拆分一次镜像的原子发布。

提交中一次完成新曲目、身份、绑定、来源、成员投影和计数，最后 revision+1。不能先插 tracks 提交，再尝试插绑定；唯一冲突必须回滚整项计划重新读取，不能留下无成员的竞争曲目。禁止 `INSERT OR REPLACE` 重建稳定 ID。

首次身份分配序号，在同一写事务执行：

```sql
UPDATE library_clock SET import_seq=import_seq+1 WHERE singleton=1;
SELECT import_seq FROM library_clock WHERE singleton=1;
```

串行提交的事务顺序定义不同任务的导入先后；同一快照按 ordinal 分配，Jev 返回速度不得改变顺序。同一身份重导、恢复或换账号绑定均保留其首次序号，不复制旧账号 access。

### 5.2 原始身份与可撤销匹配

精确 source identity 命中时读取 home_track_id，再 Resolve 根，更新身份快照即可。新源身份先拥有独立稳定 home track；若匹配某根则在同一事务应用一次有审计的合并，把新 home 作为别名。这样每次跨源匹配都有可拆回的原始归属，不必猜测误合并前源属于谁。

这是内部别名记录，不增加 UI 行。默认按最早首次导入的根为 winner，平局按 ID，保证重跑计划确定；没有身份的旧根用 created_at 和 ID 排序。匹配只搜索有效根及它们的源证据，不将别名当新候选。

`binding_origins.origin_kind=local` 有意表示“该profile曾在此账号绑定上显式收录此源身份”的持久来源依据，不是当前收藏/本地歌单成员关系。它按binding_id隔离账号、按profile/identity编码键去重，在同一profile的各本地收藏夹间共享。取消收藏、移除本地歌单成员或删除本地歌单不会停用它，也不删除binding；现存成员只由member_refs/投影表达。local origin不授权跨账号播放、不证明会员完整权益，当前账号登录及实时Resolve仍必须通过。纯远端导入不创建local origin。

### 5.3 账号与 vault

同平台切 A→B：事务内递增 A.session_generation、清 current，设置 B 为 current 并递增 B 代次；A 的未提交任务标 superseded，旧 UI 结果丢弃。A 的 origins、收藏、歌单仍保留。只有 B 的绑定可以被当前适配器使用。

退出/确认失效同样递增代次并标 logged_out/expired，不删除收藏。重登相同账号递增代次，旧 access 观察失效；来源事实仍在，播放实时 Resolve。未知错误/网络失败不能冒充账号失效。

vault 与 DB 不具备跨文件事务：登录先安全发布新 vault 记录，再提交 credential_ref/state；启动发现引用缺失就标 logged_out，不推断成功。退出先提交状态/代次拒用旧凭据，再清理对应 vault 记录；清理失败提示重试但不能继续使用。不自动使用保存私人会话进行迁移验证。

按2026-11-01最新用户决定，采用单账号假设：每个(profile_id,source,source_playlist_id)只有一份导入歌单副本，playlists不保存账号字段。imports.account_id仅记录最近一次导入/更新使用的账号；启动新任务时可改成当前账号，失败也保留该次尝试记录。它不限制更新，不证明现存快照属于该账号；已提交快照的账号以最新成功run为准，迁移基线以manifest为准。local_playlist_id只校验profile/source/真实远端ID一致。

imports唯一键为(profile_id,kind,remote_collection_id)。由于不含source列，remote_collection_id统一存紧凑确定性JSON数组[source,真实远端ID]，例如["huawei","123"]；收藏使用["huawei",""]。写入由唯一构造器编码，读取校验重新编码一致，防空白等不同JSON文本绕过唯一性；平台请求只传第二项真实ID。该命名空间避免跨平台空键及相同平台ID碰撞。收藏imports同样允许换号更新，但只对账本次run账号的绑定来源，收藏成员不删除。

UI展示全部本地歌单及已有导入副本，不因切号隐藏或禁用更新。未登录/失效优先显示“去登录/需登录”；已登录且与已有快照来源账号不同，按钮仍显示“更新”，点击先按§1.1确认，确认后允许完整快照原子覆盖唯一副本的成员、顺序和计数。相同账号直接更新；切号本身不改快照。首次导入、失败重试和deleted按§8.2处理。切换时取消旧账号任务和旧结果，不恢复账号权限门槛，也不要求切回原账号。

快照来源查询使用同一一致读事务：先校验imports.kind=playlist、status不是deleted、local_playlist_id指向现存导入副本；否则视为无当前副本快照。再取该imports最新committed run（以提交revision排序，不按最近尝试或开始时间）：

```sql
SELECT id, account_id, outcome, committed_revision
FROM import_runs
WHERE import_id=:import_id AND state='committed'
ORDER BY committed_revision DESC, id DESC
LIMIT 1;
```

committed行必须有非空committed_revision；最新结果为snapshot才提供快照账号，remote_deleted不代表现存快照，不能回溯更旧snapshot冒充当前副本。现有idx_runs_import_state_time可用(import_id,state)前缀缩小候选，再按committed_revision排序；读取单个集合的成功历史，不扫全库，不新增索引或snapshot_account列。确认判断不读取可变imports.account_id；它与快照账号相同或不同都不构成证据。

没有成功run时，读取completed_phase至少db_committed的data_migrations.inputs_json：选取decision=selected、import_id及local_playlist_id均匹配当前副本、has_snapshot=true的manifest条目，其account_id是迁移基线账号。manifest在§10.4发布事务中固定这些字段；只有list元数据、缺detail时has_snapshot=false，空但有效detail仍为true。清单可按迁移计划加载到内存按副本ID索引，不为JSON扫描增设DB冗余列；查找结果必须唯一。来源证据缺失/冲突且副本确有快照时报告一致性错误，不把它当无快照来绕过确认，也不根据最近尝试账号猜测。旧副本硬删后manifest的旧local_playlist_id不能用于新副本。

更新入口先只读生成准备结果，包含import_id、local_playlist_id、playlists.revision、imports.target_generation、当前account_id/session_generation和快照证据标识（run.id/committed_revision或migration.id/input_digest/条目标识）。账号不同且已有快照才返回需确认；dialog等待在事务外，关闭等同取消，不写imports/run/代次/错误状态，也不取消已有任务。点击“仍然更新”后携带该准备结果进入§6.1短事务，逐项重读比较；任何变化则不创建run，重新读取并按当前条件重新提示，不能用旧确认授权新账号或新副本。相同账号无需dialog，但创建run前同样复查。确认仅供本次任务创建消费，不作为永久授权；失败后重试重新判断，不能因imports.account_id已被尝试改写而跳过提示。创建后仍由run账号/会话及目标代次阻止切号后的旧任务提交。

歌单播放只写onlySource并走同一个Resolve。仍有效的副本来源所指向的源身份可投影为瞬态候选，使用当前账号实时Resolve；不得复用其他账号binding的access/URL，不因此写新origins或改变副本。该候选投影在统一Resolve内部校验当前导入副本上下文及有效来源；来源已失效不可绕过对账。队列不增加账号权限字段，没有任何有效来源或源不可用则报错。

已知边界：切号后经上述确认更新导入歌单，会用新账号完整快照覆盖旧成员，无法同时保留两账号的同一歌单镜像；不做per-account副本，等实际issue再迁移。未来迁移可使用source、真实远端ID、稳定本地ID、imports.account_id及状态/代次、成功run.account_id、成员refs、账号binding origins和迁移manifest；不得仅凭最近尝试账号推断旧快照账号，历史账号快照未保存的部分无法恢复。

source_accounts.id是随机本地ID；稳定account_key通过(source,account_key)查找。imports.account_id、import_runs.account_id、source_bindings.account_id都引用该id。run显式捕获启动时account_id，account_generation捕获该账号session_generation，target_generation捕获imports.target_generation；这些run字段不随imports.account_id变化。提交、阶段写入、错误/失效处理及UI回调均核对当前账号=run.account_id、session_generation与run.account_generation一致，并核对目标代次/任务状态。重登同一账号也不能沿用旧代次；旧任务不得修改新账号登录态或新任务状态。expires_at由源明确给出时保存，真实鉴权成功更新last_verified_at；退出保留最近验证时间。

## 6. 完整快照导入

### 6.1 接口与任务准备

适配器输入：`CollectionRequest{AccountID, SessionGeneration, Kind, RemoteID, Cursor, SnapshotToken}`。
输出：`Page{Tracks, NextCursor, Complete, SnapshotToken, CollectionMetadata}` 或分类错误。
`RemoteDeleted` 只能由已确认的远端删除语义产生；超时、认证失败、403/临时不可见、解码失败、空列表都不能直接解释为删除。真实返回明确永久不可访问但不能证明删除时保留副本并报失败。

先完成§5.3只读准备及必要确认；需要确认但尚未确认或已取消时不得进入任务创建，无需确认的情形直接复查后创建。短事务中按唯一集合键创建/读取imports，复查准备结果和源当前账号已登录，将imports.account_id记录为当前账号并使target_generation+1；旧同目标run置superseded，新run为fetching，显式捕获不可变的account_id、account_generation和target_generation。允许当前账号不同于上次导入账号，不新建副本或第二条imports。import.status保留上次可展示状态，进度从run读；过期状态按当前登录态解释。所有任务ID仅本地生成。

分页在事务外执行，每页以短事务写 stage：主键 `(run_id,source_track_id)` 去重；ordinal 保存首次出现位置；重复身份的元数据按该轮最后有效值更新。游标只存适配器审查过的不含凭据的 opaque 值，不持久化含 token 的原始 URL。

ordinal为首次遇到不同身份时递增的run内序号；重复source ID更新原行，禁止改ordinal或为重试页重分配。stage和游标/unique_count在同一短事务发布；`UNIQUE(run_id,ordinal)`冲突回滚该页并报协议错误。第二遍完整性确认用独立内存/临时序列比较，不将同run ordinal重用给另一身份。

### 6.2 快照完整性

只有协议明确 Complete、游标无循环、所有页解析成功且账号/集合一致才标 ready。空集合仅在正常完成时有效。坏曲目不能静默跳过后对账：若无法提取身份，则整轮不能用于差集；保留旧正式数据并报协议错误。

源支持快照 token 时要求各页同 token。源没有一致性游标时，至少重复获取完整身份有序列表，连续两遍一致才允许破坏性镜像/来源停用；不同则本轮 failed，等待用户重试，不自动无限重拉。两遍一致不是远端数据库快照保证，适配器必须标明该边界；不能声称任意并发远端修改均可被原子捕获。

ready 后在事务外为整个 stage 生成匹配计划，包含预生成 home ID、已有根、元数据摘要和读取的 library revision；同批也互相匹配，不能只匹配任务开始前的库。

### 6.3 原子发布

`BEGIN IMMEDIATE` 后依次检查：run=ready、对应 imports 的 target_generation 相同、run.account_id对应账号current/logged_in且session_generation=run.account_generation、当前账号ID=run.account_id、源一致、目标歌单未被显式删除、匹配计划通过§5.1行级及候选集合复查。plan_revision不同只触发复查，不直接判计划作废。

检查通过后一个事务完成：

1. 按 ordinal upsert 身份、账号绑定及该集合 origins，必要时应用匹配合并。
2. 收藏增加 member_refs 并刷新投影；歌单按 §8 镜像。
3. 只停用当前imports.id且binding.account_id=run.account_id下stage未出现的origins；其他集合/local/其他账号不动。共享imports不意味着跨账号对账。
4. origins 更新后有效性自然由 EXISTS 得出，不双写 binding；不删除 identity、binding、tracks 或收藏。
5. 更新 imports 成功状态/计数/时间、歌单 track_count/last_synced_at；run写committed、committed_revision和冻结added_count/updated_count，library revision+1，提交。统计定义见§4.3。

来源对账等价于以下限定范围的更新（参数均绑定）：

```sql
UPDATE binding_origins
SET active=0, updated_at=:now
WHERE origin_kind=:kind AND origin_key=:import_id AND active=1
  AND EXISTS (
    SELECT 1 FROM source_bindings scope_binding
    WHERE scope_binding.id=binding_origins.binding_id
      AND scope_binding.account_id=:account_id
  )
  AND NOT EXISTS (
    SELECT 1 FROM source_bindings b
    JOIN source_identities i ON i.id=b.identity_id
    JOIN import_stage s ON s.run_id=:run_id AND s.source_track_id=i.source_track_id
    WHERE b.id=binding_origins.binding_id AND b.account_id=:account_id
      AND i.source=:source
  );
```

origin_key指向同一稳定imports.id，可被历史不同账号binding引用；:account_id必须取已校验的run.account_id，不能从可变imports.account_id推导或由UI任意传入。写入/校验历史origin不要求binding账号等于imports最近尝试账号。有效性查询使用 §4.1 的 EXISTS；账号登录条件在播放查询处理，没有 binding.active 修复路径。

失败回滚整个发布事务，正式集合保持上次提交状态；另开短事务，在代次仍匹配时记 run.failed/imports.failed。取消在提交开始前标 cancelled，入口恢复上次状态；如果 COMMIT 已完成则显示完成，不假装取消并倒删已发布数据。

进程重启：fetching/ready 默认取消，不沿用可能失效的分页快照；保留必要诊断后重建新 run。committed 的数据不重放。stage 是临时工作数据，任务结束可清理，不属于“更新删收藏”的禁止范围。

## 7. 收藏、绑定与播放

### 7.1 收藏导入与本地收藏

导入的每个身份在 member_refs 写 `('favorites',profile,'identity:'+id,home_track_id,position,added_at)`。冲突保持原 position/added_at；新成员在当前末尾追加，按 stage ordinal 排列。即使同一根已有其他 ref，也保留该来源原始归属，用于将来撤销错误合并。投影仍是一行。

远端不再返回身份时仅停用该集合 origin，member_refs/favorites 不变。另有歌单/local origin 则 binding 仍有有效来源；无任何有效来源则标不可播放。

搜索点“收藏”或“加入本地歌单”：事务外匹配，事务内精确复查；写 identity、当前账号 binding、local origin 和成员 ref。只有此明确收录动作进入 DB。重复点击同一 ref 幂等。平台收藏更新不能停用 local origin。

本地取消收藏：删除该 profile 下所有 Resolve 后指向选中根的收藏 refs 和对应投影；不删除 tracks 或其他歌单成员。这是显式用户操作。后续主动导入仍可重新加入远端存在的歌曲，本稿不新增“永不再导入”黑名单需求。

### 7.2 解析与文件队列

`ResolveTrackID(id)`：在一致读事务中逐边查询 merged_into，记录 visited；根不存在、引用断裂或遇环返回结构错误，不回退把该 ID 当平台 ID。禁止路径压缩覆盖撤销所需的原始别名边；可用 revision 标记的内存缓存加速。

有trackId的队列项先Resolve根，再收集其整个别名子树的身份与当前可用绑定（导入歌单播放的瞬态候选按§5.3处理），以 `(import_seq,identity.id)` 排序。无绑定即不可播放，不能用队列里的旧 sources 绕过对账。

没有 trackId 的临时项使用内联 sources，仍检查当前源登录态。onlySource 在候选层过滤；不存在该平台候选就失败，不回退别的平台。它不改变 Resolve 接口和权限验证。

按序解析寻找已确认完整且可打开的资源；unknown 和 preview 分开保留，无 full 时按既有策略 unknown 次之、preview 兜底，不将 unknown 标作完整。音质跟随设置，access 观察的账号代次/TTL 过期后按 unknown 实时验证。试听/提前 EOF/URL 过期沿用统一解析状态机。

队列文件继续 schemaVersion=2，包含可空 trackId、内联元数据、源限定 ID、onlySource；历史同形，最多 200 条。不把别名 trackId 永久替换成根，保证撤销后仍指向原录音；显示可在运行时投影为当前根。旧 ID 永久可解析，所以合并无需与文件队列跨介质原子提交。

## 8. 歌单与导入状态

### 8.1 歌单镜像与删除

首次导入只有快照成功发布才创建playlists；失败只留下imports/run，不显示假装完整的空歌单。按(profile_id,source,source_playlist_id)部分唯一索引保留本地ID；同账号重复导入幂等，切号不新建副本。唯一键命中后复用副本ID；允许切号后更新，不能用INSERT OR REPLACE重建ID。

导入歌单在发布事务中核对当前账号=run.account_id、session_generation和target_generation有效、源及副本远端ID一致；以远端身份为ref_key，只更新owner_id等于该唯一副本ID的refs和ordinal，删除该副本本轮不存在的refs，Resolve后去重重算playlist_tracks/track_count。经§5.3确认后，新账号完整快照允许覆盖旧成员/位置/数量；失败或旧run代次不符则全部保留原状。origins仅按§6.3对账本次账号；历史账号origin不代表当前镜像成员，成员只由该副本refs/投影决定。不同源ID合并成同一根时取远端最早ordinal；无有效绑定仍保留该远端成员显示行。

歌单快照不改变 favorites。成员移除仅影响该歌单的 refs/投影和该集合 origin；其他收藏、歌单、local origin 不变。

显式删除导入副本：这是用户操作本地收藏夹，不要求源登录；事务内递增唯一imports.target_generation、取消相关run，硬删该副本member_refs/playlist_tracks/playlists，清空imports.local_playlist_id并记deleted，仅该imports.id的playlist origins置active=0，不动tracks/favorites，也不发远端删除请求。确认RemoteDeleted属于远端更新，必须核对当前账号=run.account_id及账号/目标代次有效；有效新账号请求也可确认删除，旧run结果不得删除副本。硬删副本时该imports.id的playlist origins跨账号全部停用，避免已删除集合继续提供来源；这不同于普通快照仅对账当前账号。

显式删除本地歌单则按profile和local kind核对，硬删其成员依据/投影/实体，没有远端imports状态需要改变。移除本地歌单歌曲、取消收藏、删除本地歌单均不撤销local origin：它是§5.2的“曾显式收录”记录，不是当前成员计数或引用计数。只减少对应成员，不让独立队列中的已收录歌曲因此失去音源；若账号失效或实时Resolve失败，仍正常不可播放。

删除后显式再次导入会创建新本地pl_ID，复用稳定imports键并记录当前账号；任一当前已登录账号均可发起。旧回调不能重新创建已删除副本。远端删除只在该账号代次和目标代次仍有效时发布。

### 8.2 状态机

| 状态/事件 | 写入与按钮 |
|---|---|
| never | 无已提交快照；已登录显示“导入” |
| imported | 首次完整快照成功；显示“更新” |
| updated | 后续完整快照成功；显示“更新”及 last_success_at |
| failed | 本轮失败，旧数据保留；有成功记录/迁移副本显示“重试更新”，否则“重试导入” |
| expired | 当前源账号确定失效时显示“去登录/需登录”，不删除数据；已换到有效账号则按副本状态恢复入口 |
| deleted | 仅歌单；副本不存在；源已登录显示“导入”，优先于历史成功时间 |
| 跨账号更新确认 | 已登录且有成功快照/迁移基线、账号不同时仍显示“更新”；点击弹“取消 / 仍然更新”，取消零写入；确认后才建run，不新增imports状态 |
| fetching/ready | 属于 run，不新增 imports 状态；显示当前进度和取消 |
| cancelled/superseded | 不覆盖新任务或新的登录状态；保留上次已提交集合 |

按钮以该源当前登录态为准，未登录显示“去登录/需登录”，不因imports.account_id与当前账号不同禁用更新/重试；旧账号的expired不阻止新账号更新。已有快照的跨账号更新按§5.3确认；failed但仍保留快照也适用，此时跨账号按钮显示“更新”（可另示上次失败原因），同账号保留“重试更新”。从未导入、deleted或仅有元数据无快照不弹，收藏更新不弹。不显示“由其他账号导入”。登录成功不伪造导入成功，根据现有副本/last_success_at恢复入口。过期先按 §1.1 尝试 RefreshLogin；华为不支持则退出该源并 toast 提示重新登录，刷新失败不能删除库。

导入远端 ID 永远是真实 source_playlist_id，不能拿本地 pl_ID 调平台接口。已验证的远端简介/删除能力若保留，仍是独立有授权的华为动作，不与这里“删除本地副本”混用。

## 9. 合并审计与撤销

### 9.1 应用合并

在提交事务中重验两个有效根不同、版本约束兼容、无 merge_blocks 冲突。winner 保持自己的展示元数据；每个原始 track/identity 保留自身元数据，不把 loser 信息覆盖到 winner 后丢弃。

记录 merge_decisions；merge_before 至少保存：两个根完整 before/after、loser.merged_into 的实际字段修改、双方身份集合与 home_track_id、受影响 member_refs 及 favorites/playlist_tracks 投影 before/after。未改字段的 changed_fields=[]，仅作为证明。新增身份并入时也先生成 home 行和相应 before-state。

设置 loser.merged_into=winner；原 identity.home_track_id、member_refs.anchor_track_id 不移动。扫描受影响 owner，将 refs Resolve 后按根重建投影。旧投影合并为一行是同一成员的内部表示变化，不是远端删除收藏；归属 refs 全部保留。

若参与集合包含先前未撤销合并，记录 merge_dependencies。整个审计、别名边和投影变更一次提交，不存在“只合并没审计”的窗口。

### 9.2 撤销和后续编辑

请求撤销 M 时，找出依赖 M 的未撤销后继，计算逆拓扑顺序；在同一事务撤销依赖合并再撤销 M，UI 在执行前展示实际受影响合并集合。不自动丢弃或覆盖之后的收藏/歌单编辑。

验证每条待撤销别名边仍等于审计 after 中的边；不一致则整个事务失败并报告结构冲突，不做部分恢复。只恢复 merge 实际改过的 merged_into 等结构字段，不用旧完整 JSON 覆盖后来刷新过的源标题/封面/权益。

member_refs 是活的归属事实：后续增加仍在，用户已删除的 ref 不复活，已经硬删除的歌单不重建。撤销后从当前 refs 重算投影：原来 A、B 都在的歌单恢复两个成员；后加到明确 A 的项仍归 A。用户在合并行上直接添加且无源上下文时 anchor=当时 winner，撤销后仍归该 winner；不猜测用户本意。

保留旧 ID 不重写，使队列按原 anchor 自然恢复。撤销写 revoked_at、失效相关 Jev 缓存，并将当时两侧所有原始 anchors 写 merge_blocks。候选合并检查两个拟合并子树是否跨越被阻止的两组，防止 A-B 被撤销后又经 C 传递重合并。blocks 不受模型/规则版本改变自动清除。

迁移前 JSON 已经合并但没有审计的身份，保留其已存在共同 home，标记为历史基线；不能声称能自动还原历史上从未保存的拆分依据。本稿保证新系统产生的合并可撤销。

### 9.3 审计格式、分块与保留

merge_before/merge_blocks.format_version当前为1；读取器遇未知版本拒绝撤销并保留审计，不当作空记录。升级增加新的格式解码器后才迁移CHECK/格式；不得自动用最新DTO解旧before-state。

merge_before一行记录一个实体，before/after各<=256KiB，changed_fields<=4KiB且仅允许该entity_kind的既定字段。tracks只可恢复合并实际改变的merged_into，身份/成员/投影快照为证据，通过当前refs重算而非任意JSON反序列化覆盖。实体列表按seq分多行，单事务整体保存，绝不把整个库塞进一行。

merge_blocks的左右anchor集合分块：每边每行最多512个本地ID且<=64KiB，主键 `(merge_id,chunk_no)`；按ID排序独立分块，较短一侧以空数组补足。读取时分别取所有chunk左侧并集和右侧并集再做组间禁止判断，不能仅比较同chunk，以免漏掉跨块传递合并。

schema日志、迁移计划/attempt、merge_decisions/before/dependencies/blocks保留，不按TTL删除；这是可撤销与禁止重合并的依据。已硬删歌单不因审计保留而恢复。stage在任务终止且恢复信息已冻结后清理，Jev过期缓存可清理；历史成功run保留以支持统计核对。未来若需压缩审计，必须无损且保留版本解码器，本稿不引入自动不可逆清理。

## 10. 旧数据迁移与恢复

### 10.1 迁移总协议

启动先获取本机 profile 写锁，完成 schema 后执行数据迁移，正常导入/播放队列写入在相关阶段完成前不启动。不以“DB 为空”判断。每类输入建立固定migration_key；目录清单、相对路径、摘要和目标在prepared阶段冻结。data_migrations.id为随机mig_ID，唯一键是(migration_key,input_digest)。同摘要重试复用计划，事务内新增attempt_no，旧attempt错误/时间永不覆盖；进程崩溃时running attempt标interrupted，新attempt从completed_phase恢复。

输入变化必须新增计划ID/摘要记录，不复用旧计划。若旧计划尚未提交，终止其运行attempt再创建新计划；若该迁移类型已有db_committed/published/done，则新计划只保留changed_input_after_commit诊断，不能自动重新灌入旧成员或覆盖新队列。自动迁移第一次发布后即以新库为真相，避免用户已删歌单又因旧缓存变动复活。人工恢复不是自动重放策略。

流程：`prepared → db_committed → file_ready → published → done`，无文件输出可从 db_committed 直达 done；每次成功阶段同步推进 completed_phase，失败只改 state=failed/error_code，不回退 completed_phase。重试根据 completed_phase 和实际文件摘要恢复，不从零猜测。DB 数据和 db_committed/completed_phase 标记必须同一事务。新 schema/profile 已存在不影响旧数据导入。

启动恢复先读日志：DB 已提交则只补文件发布/归档；目标文件摘要已匹配则补记 published；目标是有效但不同的新格式文件则保留它并报告冲突，不覆盖。目标无效也不自动覆盖，保留现场并停止该模块写入。输入变化须重新建计划，不能拿旧摘要重放。

JSON 大小按旧格式上限接受：收藏旧库为 32 MiB，不误用通用 5 MiB 上限。未知版本、无效 ID、冲突映射、溢出/非法时间导致迁移失败回滚，不静默跳过导致收藏少行。

### 10.2 favorites.local.json

读取公开源码定义的 DTO，不读取私人凭据。保留旧 CanonicalTrack.ID、顺序、created/updatedAt、全部源快照。每个旧规范行建立一个 favorite ref（`legacy:<oldID>`），position 按原数组；各 link 建永久身份与账号绑定。若同 source+ID 指向多个旧规范 ID，停止并报告冲突，不任选一个吞数据。

旧链接没有账号证据时绑定到 legacy:unassigned 的 logged_out 账号，保存其来源事实但不可播放；不根据“当前登录用户”猜归属。以后明确导入同一 source identity 在当前账号下建立新 binding，序号和规范 ID保持不变。

映射：Duration 秒乘 1000 后按统一规则四舍五入为 int64，检查有限、非负、溢出；Access 0/1/2 分别 unknown/full/preview；旧 Disabled=true 不因 SQL 默认值恢复。旧权益仅保存为失效观察，checked_generation 不匹配，不直接证明当前可播。Artists/Album/Version/CoverURL 无损进入声明的 JSON/字段；非法资源地址不发起请求，隔离诊断不输出敏感 URL。

高水位取 `max(旧 Document.ImportSeq, 所有旧 link.ImportSeq, 当前 library_clock.import_seq)`，先完成所有旧身份预扫描再分配歌单新增身份序号。旧重复序号不重排，按稳定 identity.id 打破平局；identity 插入顺序按旧曲目/链接数组确定。

事务提交后将旧文件以同目录安全归档名保存，名称用 UTC `YYYYMMDDTHHMMSSmmmZ` 加随机后缀，不含 Windows 禁用冒号；禁止覆盖已有备份。归档失败只重试归档，不回滚已成功 DB 数据或重新导入。旧输入存在不等于迁移未完成。

### 10.3 queue.json 与 history

优先保护已有有效queue.local.json；一旦新格式发布，后续attachAccount不得再加载旧queue.json覆盖它。迁移选择遵循旧启动顺序：先根queue.json，再由实际attach的当前账号queue.json覆盖；因此已确认当前账号的有效队列优先，即使账号队列为空也覆盖根队列，否则回退有效根队列，mtime不参与选择。绝不能把其他历史账号目录的队列当当前账号。

迁移器不为挑队列主动读取/联网验证私人会话。正常已授权登录/恢复流程提供当前account_key及“是否实际attach”的结果，再完成账号队列选择；如果尚未有该结果，保留所有账号输入和摘要、暂停最终队列发布，可在内存展示根队列，待正常会话流程结束后自动选定，无需重新导入。没有账号attach的确定结果时根队列就是最终选择，账号文件仍保留；不能用“目录只有一个”或mtime猜账号。界面允许用户在等待期间操作队列时，应以操作产生的新队列为最终版本并标记user_owned，迟到账号结果不得覆盖。

选定后将account_key/选择依据及输入摘要冻结进migration inputs manifest；重启不重新挑文件。已存在新队列（含空队列）或用户操作后形成的新版本始终优先；只有新文件不存在且无已发布计划时可迁移。候选损坏按旧读取失败的回退语义选根并报告错误，保留损坏输入，不默默删除；根也损坏则保留现场并暂停持久写入。

旧 huawei.Track.ID 先匹配已迁移 tracks.id，命中则写原规范 trackId；否则匹配华为永久身份，命中写其 home_track_id；否则保留为没有 trackId 的临时华为曲目。若同一 ID 同时匹配两个不同归属且没有证据区分，停止迁移并报告歧义，不能默默错绑。旧列表位置/播放索引/进度保留，不因去重改变正在选中项；新队列加载不自动播放。

旧条目没有歌单上下文，不猜测 onlySource。新导入歌单产生的条目才明确写 onlySource。迁移成功不把临时曲目自动收藏。

单副本迁移输入选择复用正常启动的当前/最后实际attach账号证据，不按缓存mtime选择；队列仍按本节账号优先/根回退，不依据歌单迁移输入改写。既有队列/历史不记录副本账号，不因切号复制队列；trackId仍是全局规范曲目ID，onlySource只限制平台、实时权限使用当前账号。

history.local.json 已存在则保留；不存在时对旧根/账号 history.json 使用同样确定规则转换，最多 200 条，不用网络补数据，不按相同裸 ID 跨源去重。

发布流程：在目标同目录创建0600临时文件，写入schema、migration marker（migration_id/migration_key/input_digest）、Flush/Sync/Close，登记file_ready和输出摘要，再使用平台原子替换/首次发布原语，最后记published/done。程序在发布前崩溃可重新生成；发布后DB标记前崩溃用marker/摘要识别。历史旧文件保留不删除。

不能假定 Windows 的普通 rename 覆盖语义等同 POSIX；Windows 以平台原子文件替换 API 实现，首次发布用不覆盖模式。持锁/权限失败保留旧文件及临时文件、提示重试，不先删旧目标再 rename。

### 10.4 旧歌单 list/detail 缓存

严格按账号目录分别处理，不扫描内容猜 kind。`collectionName(kind,id)` 为 `collection-` + hex(SHA256(kind+":"+id)) + `.json`：

1. 读取该账号目录下 `collectionName("playlists", "")`，从 Playlists 得到自己歌单的 ID/元数据；空列表也是有效结果。
2. 对列表内每个 ID，只读 `collectionName("detail", id)`；详情中的 Tracks 才是曲目，不能读取 Playlists[0]。
3. 列表外的详情缓存不迁移；详情缺失仅建元数据，数量0且 last_synced_at=NULL，入口显示更新。损坏详情单独记迁移失败并保留文件，不伪装空完整快照。
4. 详情存在则迁移 refs/投影、身份/账号绑定/playlist origin；不写 favorites。保存 SavedAt 作为缓存观察时间；缓存不是本轮远端完整性证据，不凭缓存对账删除现有成员。
5. 每个账号目录hash映射到source_accounts.id；同一真实远端歌单出现于多个账号目录时，仅选正常启动已确认的当前账号（没有当前账号时用有明确记录的最后实际attach账号）的那份创建唯一副本，并将选中账号写入imports.account_id。不按SavedAt/mtime择一，不合并成员，不建第二副本。仅一个账号输入时直接选该目录，不据此推断登录态；多个输入而缺少当前/最后attach证据时仅等待正常启动选择结果，不猜目录。该等待只因输入选择证据缺失；已有imports记录其他账号不构成冲突，不因此暂停。
6. 对选定账号先用list建元数据，detail缺失仍按原规则保留待更新状态，不能拿其他账号detail补全。每个唯一副本建立或复用一条imports，写选中account_id及带源集合键；last_success_at保持NULL，后续任一当前账号完整提交后才填。迁移manifest逐项记录decision=selected/ignored、account_id、相对路径、摘要、选择依据和ignored_duplicate_other_account原因；选中条目另固定import_id、local_playlist_id、has_snapshot（有效detail包括空列表为true，缺detail仅元数据为false），用于§5.3查询迁移基线账号；被忽略输入原文件保留，不自动重放、不宣称其成员已迁移。若正常attach选择的账号不在候选集合，冲突项继续待定，不能随意选另一个。缓存本身不能证明可播放，播放按§5.3实时校验。

账号摘要沿用目录 hash 创建 logged_out source_accounts，不读 session 文件认领账号。整个缓存批次幂等键是 migration_key/输入摘要及现有远端唯一键；再次启动不能复活已被用户硬删除的歌单，done 后永不自动重放旧缓存。

### 10.5 文件路径边界

destination_relpath/temp_relpath与inputs manifest路径都由迁移仓储构造，落库和每次恢复使用前重复验证。仅允许profile根下的相对路径：拒绝绝对路径、盘符/UNC/设备路径、`..`路径段、空段、NUL、Windows ADS冒号和保留设备名；落库统一`/`，读取后filepath.FromSlash转换。禁止先Clean消去`..`再认为安全。

先按平台规则检查，再用filepath.Rel确认仍在profile内；逐级通过受约束目录句柄打开并拒绝符号链接/Windows重解析点（含junction），不能仅用字符串前缀或先EvalSymlinks后无保护地打开。目标父目录、临时文件、归档目标均校验，处理大小写不敏感路径别名，避免源/目标解析到同一文件；发布使用已校验目录句柄与不覆盖标志防TOCTOU。日志只记相对路径和脱敏错误码。DB字段CHECK不能代替文件系统实体验证。

## 11. Jev 门控、缓存与隐私

精确身份命中不调用。相同规范标题可用idx_tracks_candidates快速查找；模糊标题先通过track_artists的artist_norm倒排取有艺人交集的home tracks，再Resolve为有效根去重，最后计算标题/版本/时长门控。norm_artists JSON只用于摘要/展示，不宣称可加速集合交集。B-tree不支持任意标题相似度检索，不能用严格标题相等提前排除灰区。新增/元数据变更项增量评估，不全库两两重判。

沿用门控：代码 exact/达到 AutoMerge 则直接合并，different/低于 CandidateFloor 则跳过；gray 且清洗后标题相似度>=0.85、艺人集合交集非空、版本兼容、双方有时长则差<=8s，才允许 Jev。检查 merge_blocks 优先于代码和模型。无固定候选/调用次数上限；并发限流、单请求超时、用户取消、429退避是资源控制，不把未调用项判为不同。

缓存键使用确定性编码后 SHA256：排序后的两个身份/候选子树证据摘要、两侧匹配元数据摘要、规则版本、模板版本、所选模型ID/别名、规范化完整 endpoint、credential generation。同一任务相同键 singleflight。不能只用规范根标题或裸曲目ID；候选子树成员变化也使键变化。

只读取 `invalidated_at IS NULL AND expires_at > now` 且完整键匹配的 jev_judgements。默认 TTL：别名或服务端实际版本未知24小时，已确认具体版本30天；这些是缓存新鲜度默认值，不是调用次数上限。服务器实际版本有则记录，别名不宣称被锁定。

只有业务成功、指定问题 answer 存在且为有限 [0,1] 分数才写缓存。模型不存在/认证/限流/网络/协议失败分类提示，灰区保留分开，不缓存为永久不匹配。阈值变更重新解释尚有效分数，不直接复用旧合并布尔值，也不追溯自动拆分已有库。

merge_decisions 仅审计，永远不能成为免检缓存命中入口。撤销时将对应 jev_cache_key/pair_digest 标 invalidated，并写 merge_blocks；新模型不能绕过显式撤销隔离。

设置交互保留已确认的三份状态：服务端列表、本机手输项、当前选择；刷新仅替换服务端部分。Enter 仅保存，不发曲目；成功回调只更新对应配置/模型验证记录，不写回旧选择，缓存命中不刷新验证时间。删除非当前项不改选择；删除当前项仍被服务端列出则保留，否则清空提示。

启用告知仅发送两首候选的标题、艺人、专辑、时长、版本，可能暴露偏好；不发音频、源凭据、账号ID、歌单名或播放地址。API Key 只发用户授权端点；改端点需确认复用Key，禁认证跨源重定向，切配置/关闭功能使旧结果失效。DB 的 endpoint_identity/credential_generation 只含公开端点和本地代次，不含 Key。

## 12. TTML 时间与资源处理

使用 encoding/xml token 流，先处理 BOM/前导空白，通过根元素命名空间和 local name 识别 tt，不依赖字面 `<tt`，兼容带前缀根。512 KiB、嵌套32层；不解析外部实体，不按 XML 中的外部地址访问网络。不支持的时间基准返回明确错误，不生成假时间轴覆盖已有缓存。

采用有界有理数/检查溢出的整数计算，最终转毫秒；clock/frame/tick/offset 统一单位。frameRate、frameRateMultiplier、tickRate 按支持的 TTML media timeBase 规则求值；分母0、负时长、溢出和不能支持的 smpte/clock timeBase 拒绝。不从帧号直接当毫秒。

解析维护容器栈：每层保存同步基准、绝对起点、有效终点、timeContainer、子节点时间。par 子节点 begin/end 相对父同步基准；dur 相对自身 begin；绝对结束取显式 end、begin+dur 和父结束的最小有效值，限制到父有效区间。例：p begin=10s 内 span begin=1s，词绝对起点11s。

首期明确拒绝 `timeContainer="seq"` 并返回 unsupported timing，不将顺序容器当 par；不静默生成错位歌词。对缺 end/dur 的词，先完成同一父容器下的绝对时间解析，再以其后第一个有效兄弟 begin 推断结束，并被父结束裁剪；不可跨 p/div 找下一项。末词无可用结束时保留行文本，不制造无限长逐字高亮。

body/div/p/span 的继承时间都计算；br 断行；嵌套 span 文本不重复计入父子两份词。空行/局部坏时间可跳过并返回诊断，但结构错误、超限、未知时间容器不得把部分数据当完整有效歌词发布。

translation/romanization role 映射到既有副行，继承语言和时间；仅语言不同不能断言是翻译，按显式 role 或已识别并行结构与时间区间关联，歧义保留独立文本而非错配。忽略样式/背景/metadata，不忽略时间容器语义。

ParseAny 接入源内联/URL、本地 .lrc/.ttml/.xml 及现有歌词回退内容；缓存记录格式和解析器版本。只在完整解析成功后原子替换文件，不因错误覆盖旧有效缓存。不新增 Apple Music 抓取或其他音频来源。

## 13. UI 与工具链实施顺序

1. schema/仓储/迁移：先完成唯一约束、身份/refs/投影、ResolveTrackID、迁移日志与文件发布；用独立合成 profile 验证崩溃点，不能将实际用户目录当夹具。相关迁移失败不得静默进入可写空库。
2. 导入与并发：华为适配器提供完整快照契约，接 staging/代次/单事务发布；集合失效只更新 origins，落实歌单镜像/硬删除。此阶段不实现网易适配器。
3. 合并与 Jev：接候选计划、before-state、依赖撤销、分组阻止重合并和缓存；网络不持 DB 锁。核对撤销后用户后续增删和旧队列引用仍成立。
4. UI：收藏导入 dialog、源搜索 tabs、歌单入口与状态、toast。总数/列表只读已提交投影；状态及进度按 run/账号代次过滤。源未注册不提供假可用入口。搜索点歌追加队列，不替换队列、不自动收藏。
5. TTML：先验证时间基准及拒绝行为，再接本地导入、缓存和舞台显示；保持现有 LRC 可用。
6. 去 Node：建立 Go `cmd/tinglan-tool` 的 build/start/mac-app/package/ui-check/assets/icons/licenses/ci-targets/version，以及现有包校验、签名检查等必要子命令。先验证等价产物与 CI 调用，再移除旧 Node 入口；本稿不执行删除。
7. 元数据移到 `internal/appmeta`，使用 `org.tinglan.unofficial.internal.viva`、`tinglan/native-v1`、显示名“听澜”，旧目录由 `MigrateDataDir` 迁移；不通过改目录绕过迁移。保留 Lucide ISC 和 icon 许可。
8. 品牌图标使用现有 PNG 母版生成 PNG/ICO/ICNS，不以不支持滤镜的 SVG 渲染器悄悄改变图标。Viva headless UI 检查转 Go；浏览器 JS 授权探测不能直接以 net/http 200 冒充等价，可将其保留为不属于正式工具链的历史记录。
9. 构建/vet/离线数据库和UI回归完成后，再依授权进行真实账号与 Windows 实机验证。没有真实平台证据不能宣称 full 权益或 Windows 验收完成；不自动提交、推送、发布。

## 14. Finding 追溯

| 编号/严重度 | 原问题 | 设计位置 | 修法 |
|---|---|---|---|
| 1 blocker | 不完整分页触发删除 | §6、§8 | run/stage 完整性门槛；短事务原子发布；失败无差集；RemoteDeleted 分类 |
| 2 blocker | 合并不能恢复成员/队列 | §4、§7.2、§9 | 原始 home/ref 保留，before/after 审计，依赖逆序撤销，旧ID沿别名解析 |
| 3 blocker | 详情缓存错误读取 Playlists[0] | §10.4 | 按 kind:id 哈希定位 list/detail，详情只读 Tracks |
| 4 major | 收藏对账删掉歌单共用绑定 | §4.1、§6.3、§7.1 | binding_origins 集合隔离，EXISTS 推导有效性，无派生 active 双写；其他来源不受影响 |
| 5 major | 删绑定丢稳定ID和导入顺序 | §4、§5.2 | 永久 source_identities、home_track_id、首次seq；有效绑定独立 |
| 6 major | 切账号对错集合/旧响应覆盖 | §5.3、§6、§8、§10.4 | run显式捕获account_id/代次；跨账号覆盖已有歌单快照先确认，取消零写入；最新committed/manifest定来源，不用最近尝试账号；旧run不得提交；多账号缓存只选一份 |
| 7 major | 漏根目录队列/混淆规范ID | §10.3 | 账号优先/根回退，待账号确认不猜mtime；先规范ID再源ID；保护新格式 |
| 8 major | 空库判断与跨文件崩溃 | §10.1—10.3 | 按输入摘要分计划、attempt留史；DB同事务标记和原子文件发布恢复 |
| 9 major | 歌单幂等唯一约束缺失 | §4、§8、§10.4 | (profile,source,remote ID)部分唯一；切号复用同副本，同账号幂等；imports按带源集合键唯一 |
| 10 major | deleted/failed冲突与入口漂移 | §1、§8.2 | 稳定带源集合键；deleted清唯一副本引用；旧run账号/代次不符不能触发删除；有效当前账号可更新/确认远端删除 |
| 11 major | 并发查插/长网络事务 | §5.1、§6.3 | BEGIN IMMEDIATE + 行revision/候选集合/代次校验；网络事务外；seq同事务分配 |
| 12 major | 源快照无损字段缺失 | §4.1、§10.2 | §4.2限额及Unicode/空值/单位映射；完整源JSON和Access/Disabled映射 |
| 13 major | 审计当缓存绕过撤销/TTL | §9.2、§11 | 审计不命中；完整缓存键/TTL；失效缓存与merge_blocks阻断传递重合并 |
| 14 major | TTML父级时间基准缺失 | §12 | 容器栈转绝对时间/父区间裁剪；seq明确拒绝；兄弟范围推断end |

## 15. 字段级工程审查与修订（A–R）

| 项 | 决定 | 工程理由与具体落点 |
|---|---|---|
| A | 接受，独立计划ID+输入唯一键+attempt表 | §4 data_migrations改为随机id主键和UNIQUE(migration_key,input_digest)，migration_attempts保留每次重试；§10.1输入变化创建新计划，不覆盖旧计划，提交后变化不得自动重放 |
| B | 接受，删除派生active列 | §4删source_bindings.active，§4.1/§6.3仅以origin EXISTS推导；部分索引idx_origin_active_binding支撑查询，消除双写漂移而非事后修补 |
| C | 接受 | §4新增idx_favorites_track、idx_playlist_track，合并/撤销按track反查无需扫描所有owner |
| D | 接受 | §4新增idx_merge_dependants(depends_on,merge_id)，§9.2用反向依赖遍历逆序撤销 |
| E | 接受并补无状态查询索引 | §4新增(import_id,state,started_at DESC,id)和(import_id,started_at DESC,id)；前者筛状态、后者查跨状态最近run，避免state夹在中间不能直接按时间取最近 |
| F | 接受倒排表，拒绝继续把艺人JSON放交集索引 | §4增加track_artists及(artist_norm,track_id)反向索引；标题索引改norm_title/version_key；§11先艺人交集再模糊标题，原文/JSON保留但不承诺无效索引收益 |
| G | 接受，统一派生清单 | §4.3逐项给来源、唯一写入口与verify/rebuild_projection；删除binding.active，保留可验证的version_key/count；run结果是冻结历史而不是按当前库反算的缓存 |
| H | 接受 | §4补source_accounts.expires_at/last_verified_at可空，§5.3规定成功验证才更新；不存token，不把未知过期字段猜成登录失效 |
| I | 接受 | §4增加UNIQUE(run_id,ordinal)；§6.1首次不同身份递增，重复页只刷新原行、保留ordinal，stage/游标同事务防重试漂移 |
| J | 接受并支持分块 | §4为before/blocks增加format_version与字节CHECK；§9.3明确每实体审计、blocks分块后分别求并集、白名单恢复、未知版本拒绝、不可按TTL删可撤销依据 |
| K | 接受仓储层路径约束 | §10.5对落库/恢复双重校验相对路径，拒绝绝对/..、ADS、symlink及重解析点，用目录句柄发布防TOCTOU；不以SQL文本CHECK冒充文件系统安全 |
| L | 接受行级复查 | §5.1全局revision仅失效提示；计划记录行revision/子树/候选集合，检测新候选phantom；§6.3不再因无关全局写入整批重新调用Jev，单事务原子发布保持 |
| M | 接受统一映射 | §4.2给UTC毫秒、秒转换、Access、Disabled、账号归属、NFKC+case-fold、原文保留、JSON形状及NULL/空串；§10.2复用同DTO，不各自解析 |
| N | 接受，按最新单账号决定保留任务隔离 | §4.2/§5.3 account_id指随机source_accounts.id；playlists不存账号，imports只记最近使用账号；run显式捕获不可变账号及会话/目标代次；§5.3按committed/manifest判断跨账号确认，取消不建run，确认后可更新，同账号不弹；旧任务不可跨账号提交，同账号幂等 |
| O | 接受并保持永久高水位 | §5.1/§10.2换账号、重导、恢复、合并都不重分配身份seq；origin停用或显式删除成员不回退高水位，修复只允许上调 |
| P | 接受双层限额 | §4 DDL限制artists/album/version字节、艺人数量；§4.2固定DTO与单字段上限；超限输入整轮失败/保留旧数据，不截断元数据后误匹配 |
| Q | 接受，删除mtime选择规则 | §10.3与旧openProfile→attachAccount等价：实际当前账号有效队列（空也有效）优先，否则根；未知账号待正常会话结果、不猜历史目录；发布后禁旧attach覆盖，新用户操作优先 |
| R | 接受编码字典 | §4.2按kind定义origin_key格式和逻辑检查，local/legacy用固定JSON数组；legacy指唯一migration ID，不拼接有歧义的profile/identity或可重复迁移类型 |

本轮对全部23张表的字段写入边界见§4.4；14条finding追溯仍保留于§14。索引不是越多越好：只补实际反查/排序路径，未给norm_artists JSON保留无用的集合查询索引。上述命令/接口均为设计契约，本轮不新增实现代码。

### 15.1 单账号决定后的校验记录（2026-11-01）

m03618本轮不修改DDL，仅修订流程、查询、迁移manifest条目契约及验证。重新在内存SQLite执行完整DDL，并模拟确认流程，共25/25项通过：

- 1–5：完整DDL（23表）；无新增snapshot_account或playlists账号列；单副本唯一索引不变；重复副本拒绝；重复集合imports拒绝。
- 6–8：按committed_revision而非开始时间选择最新成功run；失败尝试不改快照账号；EXPLAIN QUERY PLAN确认现有idx_runs_import_state_time支持单集合committed筛选。
- 9–15：异账号且有成功快照需确认；同账号不弹；迁移基线使用selected账号触发确认；仅元数据无快照不弹；从未导入不弹；已删除不弹；收藏更新不弹。
- 16–18：取消不创建run且DB全量导出前后相同；确认后才创建当前账号run；确认后账号/代次相符可通过提交条件。
- 19–23：确认后切号旧run提交被拒；dialog期间账号变化使旧准备失效；同账号重登代次变化被拒；确认期间目标代次变化失效；最新remote_deleted不回溯旧snapshot。
- 24–25：14条finding追溯完整；A–R 18条回应完整。

上述账号/代次、确认及取消为内存合成流程模拟，manifest为合成数据；不声称DDL自动执行交互规则或真实UI/迁移/播放已验收。上一轮26项属于此前设计校验记录，本轮25项为重新执行的当前增量口径；不将历史执行计入本轮数量。当前流程为单副本保留、跨账号覆盖已有歌单快照须确认、取消零写入、同账号不弹，确认后仍拒绝失效run。

## 16. 必须确认

无新的必须拍板需求。对账采用 active 状态更新、历史无账号证据保持未认领、TTML seq 首期明确拒绝，均为满足现有约束的保守实现默认值。
用户已接受单账号简化边界：切号后更新已有导入歌单快照须确认，确认后新账号完整快照覆盖该歌单旧成员，不删除收录音乐或收藏，不做per-account副本，等实际issue再迁移。旧缓存按当前/最后attach选一份，其余记ignored；账号不同不阻止更新或迁移。缺少输入选择证据时等待正常账号流程，不伪造完成；未来迁移证据见§5.3。
历史 JSON 没有合并前证据的部分无法凭空恢复，旧缓存也不能证明远端现状；这些作为迁移证据边界呈现，不通过猜测补全。
队列当前账号由正常已授权会话流程提供；没有账号证据时保留输入等待该流程，不向用户询问产品偏好、不以mtime选择。若应用无账号attach则使用根队列，后续不以旧账号文件覆盖新文件。

## 结论

设计层已为3 blocker + 11 major及字段级A–R全部给出DDL、事务或接口落点，追溯见§14、§15。
没有新增必须用户拍板的产品歧义；用户确认本次设计后按§13实施，本轮不写实现代码。
真实华为快照/权益、迁移故障恢复及 Windows 实机仍须按实施阶段验证，网易适配器暂不实现。
