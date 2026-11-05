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