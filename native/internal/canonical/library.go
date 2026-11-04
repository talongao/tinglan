// Package canonical owns the local favorites library: one row per recording,
// independent of any source account, plus the import pipeline that merges
// provider tracks into those rows.
//
// The library is the single truth for the favorites list. Source accounts only
// contribute links; signing out of a source deactivates its links but never
// deletes the local rows. Rows live in the SQLite library database; the legacy
// JSON document is imported once and then archived.
package canonical

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"time"

	"tinglan/native/internal/librarydb"
	"tinglan/native/internal/music"
	"tinglan/native/internal/source"
)

const (
	// SchemaVersion is the legacy JSON document version. Documents from a newer
	// version are rejected instead of being silently rewritten.
	SchemaVersion = 1
	fileName      = "favorites.local.json"
	maxDocument   = 32 * 1024 * 1024
	// migrationKey names the one-time JSON import in data_migrations.
	migrationKey = "canonical-favorites-json"
)

// Document is the legacy persisted shape.
type Document struct {
	SchemaVersion int                    `json:"schemaVersion"`
	ImportSeq     int64                  `json:"importSeq"`
	UpdatedAt     time.Time              `json:"updatedAt"`
	Tracks        []music.CanonicalTrack `json:"tracks"`
}

// ImportResult summarizes one import run.
type ImportResult struct {
	Added     int
	Merged    int
	Refreshed int
	Skipped   int
	Pages     int
}

// Progress is reported after each page so the UI can show progress without
// holding the library lock.
type Progress struct {
	Pages  int
	Tracks int
	Result ImportResult
}

// Library is the in-memory canonical library backed by the SQLite database.
type Library struct {
	mu       sync.RWMutex
	dir      string
	db       *librarydb.DB
	doc      Document
	index    *music.Index
	position map[string]int
	now      func() time.Time
}

// Open loads the library from dir, importing a legacy JSON document first when
// the database is still empty. An unreadable or newer document is an error so
// the caller can keep a backup.
func Open(dir string) (*Library, error) {
	db, err := librarydb.Open(dir)
	if err != nil {
		return nil, err
	}
	library := &Library{dir: dir, db: db, index: music.NewIndex(), position: make(map[string]int), now: time.Now}
	library.doc.SchemaVersion = SchemaVersion
	if err := library.load(context.Background()); err != nil {
		db.Close()
		return nil, err
	}
	return library, nil
}

// Close releases the database handle.
func (l *Library) Close() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.db == nil {
		return nil
	}
	err := l.db.Close()
	l.db = nil
	return err
}

// ImportState reports whether one remote collection was already imported and by
// which account. The UI uses it to choose between 导入 and 更新.
func (l *Library) ImportState(sourceID music.SourceID, kind, remoteID string) (librarydb.ImportRecord, bool, error) {
	return l.db.ImportByKey(context.Background(), librarydb.DefaultProfile, sourceID.String(), kind, remoteID)
}

// AccountGeneration reports the current account and its session generation for
// one source, or zero values when the source has no account yet.
func (l *Library) AccountGeneration(sourceID music.SourceID) (librarydb.Account, bool, error) {
	account, err := l.db.Account(context.Background(), sourceID.String())
	if err != nil {
		return librarydb.Account{}, false, err
	}
	return account, account.ID != "", nil
}

// Dir reports the profile directory.
func (l *Library) Dir() string { return l.dir }

func (l *Library) load(ctx context.Context) error {
	if err := l.migrateLegacy(ctx); err != nil {
		return err
	}
	rows, seq, err := l.db.Load(ctx, librarydb.DefaultProfile)
	if err != nil {
		return err
	}
	l.doc.Tracks = make([]music.CanonicalTrack, 0, len(rows))
	for _, row := range rows {
		l.doc.Tracks = append(l.doc.Tracks, row.Track)
	}
	l.doc.ImportSeq = seq
	l.doc.UpdatedAt = l.now().UTC()
	l.rebuildIndex()
	return nil
}

// migrateLegacy imports favorites.local.json once, in a single transaction, and
// then archives the file. Completion is recorded in data_migrations so a crash
// between commit and archive cannot import the same data twice.
func (l *Library) migrateLegacy(ctx context.Context) error {
	path := filepath.Join(l.dir, fileName)
	if info, err := os.Lstat(path); err == nil && info.Mode()&os.ModeSymlink != 0 {
		return errors.New("canonical: library file must not be a symlink")
	}
	data, err := readLimited(path, maxDocument)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	var doc Document
	if err := json.Unmarshal(data, &doc); err != nil {
		return errors.New("canonical: library document is not valid JSON")
	}
	if doc.SchemaVersion != SchemaVersion {
		return fmt.Errorf("canonical: unsupported library schema %d", doc.SchemaVersion)
	}
	sum := sha256.Sum256(data)
	digest := hex.EncodeToString(sum[:])
	state, err := l.db.MigrationState(ctx, migrationKey, digest)
	if err != nil {
		return err
	}
	if state != "done" {
		count, err := l.db.CountTracks(ctx)
		if err != nil {
			return err
		}
		if count == 0 && len(doc.Tracks) > 0 {
			id := "mig_" + digest[:16]
			// inputs_json is a JSON array in the schema; each element describes
			// one input that fed this migration plan.
			inputs, _ := json.Marshal([]any{map[string]any{"file": fileName, "tracks": len(doc.Tracks)}})
			if err := l.db.BeginMigration(ctx, id, migrationKey, digest, string(inputs)); err != nil {
				return err
			}
			for i := range doc.Tracks {
				doc.Tracks[i].Favorite = true
			}
			if err := l.db.SaveRows(ctx, librarydb.DefaultProfile, doc.Tracks); err != nil {
				_ = l.db.FinishMigration(ctx, id, "failed", "none")
				return err
			}
			if err := l.db.FinishMigration(ctx, id, "done", "done"); err != nil {
				return err
			}
		} else if count > 0 {
			// The database already holds rows written by a newer flow; the JSON
			// file is stale and must not be merged on top of it.
			return nil
		}
	}
	return archiveLegacy(path)
}

func readLimited(path string, limit int64) ([]byte, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, limit+1))
	if err != nil {
		return nil, fmt.Errorf("canonical: read library: %w", err)
	}
	if int64(len(data)) > limit {
		return nil, errors.New("canonical: library document too large")
	}
	return data, nil
}

// archiveLegacy renames the imported document so later launches skip it.
func archiveLegacy(path string) error {
	if _, err := os.Lstat(path); errors.Is(err, os.ErrNotExist) {
		return nil
	}
	target := path + ".migrated"
	if _, err := os.Lstat(target); err == nil {
		target = fmt.Sprintf("%s.migrated-%d", path, time.Now().Unix())
	}
	if err := os.Rename(path, target); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("canonical: archive legacy library: %w", err)
	}
	return nil
}

func (l *Library) rebuildIndex() {
	l.index = music.NewIndex()
	l.position = make(map[string]int, len(l.doc.Tracks))
	for i, track := range l.doc.Tracks {
		l.position[track.ID] = i
		l.index.Add(i, track.Track())
	}
}

// Snapshot returns a copy of the rows in display order (import order).
func (l *Library) Snapshot() []music.CanonicalTrack {
	l.mu.RLock()
	defer l.mu.RUnlock()
	out := make([]music.CanonicalTrack, 0, len(l.doc.Tracks))
	for _, row := range l.doc.Tracks {
		if row.Favorite {
			out = append(out, row)
		}
	}
	return out
}

// Len reports the number of favorited rows.
func (l *Library) Len() int {
	l.mu.RLock()
	defer l.mu.RUnlock()
	count := 0
	for _, row := range l.doc.Tracks {
		if row.Favorite {
			count++
		}
	}
	return count
}

// ImportSeq reports the last used import sequence.
func (l *Library) ImportSeq() int64 {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return l.doc.ImportSeq
}

// Import pages through one adapter collection and publishes the whole snapshot
// in a single transaction. Pages are staged as they arrive; only a run that
// reaches normal completion can change the library, so a failed or cancelled
// import never deletes or rewrites anything.
//
// Only the code pass narrows candidates; judge (Jev) is consulted for gray-zone
// pairs only, and a judge failure keeps rows separate.
func (l *Library) Import(ctx context.Context, src source.Source, collection source.Collection, judge music.Judge, policy music.MatchPolicy, progress func(Progress)) (ImportResult, error) {
	if src == nil {
		return ImportResult{}, errors.New("canonical: nil source")
	}
	if policy == (music.MatchPolicy{}) {
		policy = music.DefaultMatchPolicy()
	}
	if collection.Kind == "" {
		collection = source.Collection{Kind: "favorites"}
		if capabilities := src.Describe().Capabilities; len(capabilities.Collections) > 0 {
			collection = capabilities.Collections[0]
		}
	}
	limit := src.Describe().Capabilities.MaxPageSize
	if limit <= 0 {
		limit = 50
	}

	account, err := l.ensureAccount(ctx, src)
	if err != nil {
		return ImportResult{}, err
	}
	importRecord, err := l.db.EnsureImport(ctx, librarydb.DefaultProfile, account.ID,
		src.ID().String(), collection.Kind, collection.ID, "")
	if err != nil {
		return ImportResult{}, err
	}
	runID, err := l.db.BeginRun(ctx, importRecord, account.ID, account.SessionGeneration)
	if err != nil {
		return ImportResult{}, err
	}

	var result ImportResult
	var fetched []music.Track
	cursor := ""
	for {
		if err := ctx.Err(); err != nil {
			l.failRun(runID, importRecord.ID)
			return result, err
		}
		page, err := src.Library().Import(ctx, source.ImportRequest{Collection: collection, Cursor: cursor, Limit: limit, RunID: runID})
		if err != nil {
			l.failRun(runID, importRecord.ID)
			return result, err
		}
		result.Pages++
		for _, track := range page.Tracks {
			if !track.Valid() {
				result.Skipped++
				continue
			}
			if err := l.db.StageTrack(ctx, runID, len(fetched), track); err != nil {
				l.failRun(runID, importRecord.ID)
				return result, err
			}
			fetched = append(fetched, track)
		}
		if progress != nil {
			progress(Progress{Pages: result.Pages, Tracks: len(fetched), Result: result})
		}
		if page.Done() {
			break
		}
		if page.Next == cursor {
			l.failRun(runID, importRecord.ID)
			return result, errors.New("canonical: import cursor did not advance")
		}
		cursor = page.Next
	}
	// An empty snapshot from a source that already contributed local tracks is
	// far more likely an expired or wrong-account session than a user who
	// really deleted everything. Publishing it would drop every binding for
	// that source, so fail the run and keep the local library intact.
	if len(fetched) == 0 {
		l.mu.Lock()
		local := 0
		for _, track := range l.doc.Tracks {
			for _, link := range track.Links {
				if link.Source == src.ID() {
					local++
					break
				}
			}
		}
		l.mu.Unlock()
		if local > 0 {
			l.failRun(runID, importRecord.ID)
			return result, source.NewError(src.ID(), "import", source.KindLogin, errors.New("empty snapshot from a source with local tracks"))
		}
	}
	if err := l.db.SetRunState(ctx, runID, librarydb.RunReady, ""); err != nil {
		return result, err
	}

	l.mu.Lock()
	for _, track := range fetched {
		_, outcome := l.merge(track, judge, policy)
		switch outcome {
		case mergedNew:
			result.Added++
		case mergedExisting:
			result.Merged++
		case mergedRefresh:
			result.Refreshed++
		default:
			result.Skipped++
		}
	}
	rows := append([]music.CanonicalTrack(nil), l.doc.Tracks...)
	l.mu.Unlock()
	seen := make(map[string]bool, len(fetched))
	for _, track := range fetched {
		seen[track.Source.String()+"\x00"+track.ID] = true
	}

	if err := l.db.PublishFavorites(ctx, librarydb.PublishInput{
		ProfileID: librarydb.DefaultProfile,
		ImportID:  importRecord.ID,
		RunID:     runID,
		AccountID: account.ID,
		Source:    src.ID().String(),
		Rows:      rows,
		Seen:      seen,
		Added:     result.Added + result.Merged,
		Updated:   result.Refreshed,
	}); err != nil {
		l.failRun(runID, importRecord.ID)
		l.mu.Lock()
		_ = l.load(ctx)
		l.mu.Unlock()
		return result, err
	}

	l.mu.Lock()
	err = l.load(ctx)
	l.mu.Unlock()
	if err != nil {
		return result, err
	}
	return result, nil
}

// ensureAccount records the source's current account so bindings can be scoped
// to it. Sources without a session fall back to a stable placeholder key.
func (l *Library) ensureAccount(ctx context.Context, src source.Source) (librarydb.Account, error) {
	key, label := "default", "默认账号"
	status, err := src.Auth().Status(ctx)
	if err == nil && status.AccountKey != "" {
		key = status.AccountKey
		if status.DisplayName != "" {
			label = status.DisplayName
		}
	}
	return l.db.EnsureAccount(ctx, src.ID().String(), key, label)
}

func (l *Library) failRun(runID, importID string) {
	ctx := context.Background()
	_ = l.db.SetRunState(ctx, runID, librarydb.RunFailed, "import_failed")
	_ = l.db.MarkImportFailed(ctx, importID)
}

// Add inserts one source track, merging it into an existing canonical row when
// the matcher or optional judge says it is the same recording. It is the local
// "favorite" path for search results.
func (l *Library) Add(ctx context.Context, track music.Track, judge music.Judge, policy music.MatchPolicy) (bool, error) {
	if !track.Valid() {
		return false, errors.New("canonical: invalid track")
	}
	if err := ctx.Err(); err != nil {
		return false, err
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	position, outcome := l.merge(track, judge, policy)
	if outcome == mergedSkipped {
		return false, nil
	}
	l.doc.Tracks[position].Favorite = true
	if err := l.persistLocked(ctx, map[int]bool{position: true}); err != nil {
		return false, err
	}
	return outcome != mergedRefresh, nil
}

// AddToLibrary inserts one source track into 收录音乐 without adding it to 我的
// 收藏. Adding a search result to a playlist goes through this path.
func (l *Library) AddToLibrary(ctx context.Context, track music.Track, judge music.Judge, policy music.MatchPolicy) (string, error) {
	if !track.Valid() {
		return "", errors.New("canonical: invalid track")
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	position, outcome := l.merge(track, judge, policy)
	if outcome == mergedSkipped {
		return l.doc.Tracks[position].ID, nil
	}
	if err := l.persistLocked(ctx, map[int]bool{position: true}); err != nil {
		return "", err
	}
	return l.doc.Tracks[position].ID, nil
}

type mergeOutcome int

const (
	mergedSkipped mergeOutcome = iota
	mergedNew
	mergedExisting
	mergedRefresh
)

// merge inserts or refreshes one provider track and returns the affected row
// index. Caller holds the write lock.
func (l *Library) merge(track music.Track, judge music.Judge, policy music.MatchPolicy) (int, mergeOutcome) {
	// Exact identity first: re-importing the same source row must be idempotent
	// and must not disturb its original import sequence.
	if position, ok := l.findLink(track.Source, track.ID); ok {
		row := &l.doc.Tracks[position]
		link, _ := row.Link(track.Source)
		refreshed := music.Link(track, link.ImportSeq, l.now())
		if !row.PutLink(refreshed, l.now()) {
			return position, mergedSkipped
		}
		return position, mergedRefresh
	}

	probe := track
	candidates := l.index.Candidates(probe, policy, func(position int) float64 {
		return music.Similarity(l.doc.Tracks[position].Track(), probe)
	})
	for _, position := range candidates {
		row := &l.doc.Tracks[position]
		verdict := music.JudgePair(context.Background(), judge, policy, row.Track(), probe)
		if !verdict.Same {
			continue
		}
		seq := l.nextSeqLocked()
		if !row.PutLink(music.Link(track, seq, l.now()), l.now()) {
			return position, mergedSkipped
		}
		l.doc.Tracks[position] = *row
		return position, mergedExisting
	}

	seq := l.nextSeqLocked()
	row := music.NewCanonical(track, seq, l.now())
	l.doc.Tracks = append(l.doc.Tracks, row)
	l.position[row.ID] = len(l.doc.Tracks) - 1
	l.index.Add(len(l.doc.Tracks)-1, row.Track())
	return len(l.doc.Tracks) - 1, mergedNew
}

// nextSeqLocked allocates the next import sequence from the durable clock.
// WAL mode keeps the per-track reservation cheap; numbers are never reused so a
// crashed import cannot hand the same sequence to a later run.
func (l *Library) nextSeqLocked() int64 {
	seq, err := l.db.NextSeqs(context.Background(), 1)
	if err != nil {
		// Falling back to the in-memory counter keeps the merge working; the
		// page commit surfaces the database error to the caller.
		l.doc.ImportSeq++
		return l.doc.ImportSeq
	}
	l.doc.ImportSeq = seq
	return seq
}

func (l *Library) findLink(id music.SourceID, trackID string) (int, bool) {
	for i := range l.doc.Tracks {
		for _, link := range l.doc.Tracks[i].Links {
			if link.Source == id && link.TrackID == trackID {
				return i, true
			}
		}
	}
	return 0, false
}

// DisableSource deactivates every binding of one source without deleting the
// canonical rows. Sign-out calls this; re-importing the same source id
// refreshes the binding and clears the flag.
func (l *Library) DisableSource(id music.SourceID) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	for i := range l.doc.Tracks {
		for j := range l.doc.Tracks[i].Links {
			if l.doc.Tracks[i].Links[j].Source == id {
				l.doc.Tracks[i].Links[j].Disabled = true
			}
		}
	}
	return l.db.DisableSource(context.Background(), id)
}

// RemoveLink removes the track from 我的收藏. The canonical row and its source
// bindings stay: 收录音乐 is the library, favorites are only a collection, and
// a song that is still in a playlist must keep playing after un-favoriting.
func (l *Library) RemoveLink(id music.SourceID, trackID string) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	ctx := context.Background()
	for i := range l.doc.Tracks {
		row := &l.doc.Tracks[i]
		for _, link := range row.Links {
			if link.Source != id || link.TrackID != trackID {
				continue
			}
			if err := l.db.RemoveFavorite(ctx, librarydb.DefaultProfile, row.ID); err != nil {
				return err
			}
			row.Favorite = false
			row.UpdatedAt = l.now().UTC()
			l.rebuildIndex()
			return nil
		}
	}
	return nil
}

// Delete removes a canonical row and its links.
func (l *Library) Delete(canonicalID string) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	position, ok := l.position[canonicalID]
	if !ok {
		return nil
	}
	if err := l.db.DeleteTrack(context.Background(), librarydb.DefaultProfile, canonicalID); err != nil {
		return err
	}
	l.doc.Tracks = append(l.doc.Tracks[:position], l.doc.Tracks[position+1:]...)
	l.rebuildIndex()
	return nil
}

// Save persists every row. Mutations already write through; this exists for
// callers that changed nothing but want a durable checkpoint.
func (l *Library) Save() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.persistLocked(context.Background(), nil)
}

// persistLocked writes the given row indices (or every row when nil) in one
// transaction. Caller holds the write lock.
func (l *Library) persistLocked(ctx context.Context, changed map[int]bool) error {
	rows := make([]music.CanonicalTrack, 0, len(l.doc.Tracks))
	if changed == nil {
		rows = append(rows, l.doc.Tracks...)
	} else {
		for position := range changed {
			if position >= 0 && position < len(l.doc.Tracks) {
				rows = append(rows, l.doc.Tracks[position])
			}
		}
	}
	if len(rows) == 0 {
		return nil
	}
	l.doc.UpdatedAt = l.now().UTC()
	return l.db.SaveRows(ctx, librarydb.DefaultProfile, rows)
}

// Row returns the canonical row with the given id.
func (l *Library) Row(id string) (music.CanonicalTrack, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	position, ok := l.position[id]
	if !ok || position < 0 || position >= len(l.doc.Tracks) {
		return music.CanonicalTrack{}, false
	}
	return l.doc.Tracks[position], true
}

// RowForLink returns the canonical row that owns one source binding.
func (l *Library) RowForLink(source music.SourceID, trackID string) (music.CanonicalTrack, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	for _, row := range l.doc.Tracks {
		for _, link := range row.Links {
			if link.Source == source && link.TrackID == trackID {
				return row, true
			}
		}
	}
	return music.CanonicalTrack{}, false
}

// EnsureAccount records the current account of one source so bindings and
// imports can be scoped to it. The key must be stable and non-secret.
func (l *Library) EnsureAccount(ctx context.Context, id music.SourceID, key, label string) (librarydb.Account, error) {
	acc, err := l.db.EnsureAccount(ctx, id.String(), key, label)
	if err != nil {
		return acc, err
	}
	// Reload so links recompute Disabled against the account state we just
	// wrote. Without this, a session restored after open() would keep every
	// binding disabled until the next launch.
	l.mu.Lock()
	defer l.mu.Unlock()
	if err := l.load(ctx); err != nil {
		return acc, err
	}
	return acc, nil
}

// LastRunAccount reports the account that produced the newest committed import
// snapshot, so the UI can warn before a different account overwrites it.
func (l *Library) LastRunAccount(id music.SourceID, kind, remoteID string) (string, bool, error) {
	imp, ok, err := l.ImportState(id, kind, remoteID)
	if err != nil || !ok {
		return "", false, err
	}
	accountID, _, has, err := l.db.LastCommittedRun(context.Background(), imp.ID)
	if err != nil {
		return "", false, err
	}
	return accountID, has, nil
}

// Playlists lists local and imported playlists for the default profile.
func (l *Library) Playlists(ctx context.Context) ([]librarydb.Playlist, error) {
	return l.db.Playlists(ctx, librarydb.DefaultProfile)
}

// Playlist returns one playlist by local ID.
func (l *Library) Playlist(ctx context.Context, id string) (librarydb.Playlist, error) {
	return l.db.Playlist(ctx, id)
}

// CreatePlaylist creates an empty local playlist.
func (l *Library) CreatePlaylist(ctx context.Context, name, description string) (librarydb.Playlist, error) {
	return l.db.CreateLocalPlaylist(ctx, librarydb.DefaultProfile, name, description)
}

// PlaylistTracks returns the canonical IDs in playlist order.
func (l *Library) PlaylistTracks(ctx context.Context, id string) ([]string, error) {
	return l.db.PlaylistTracks(ctx, id)
}

// Tracks resolves canonical IDs to rows, preserving the given order.
func (l *Library) Tracks(ctx context.Context, ids []string) ([]music.CanonicalTrack, error) {
	return l.db.TracksByID(ctx, ids)
}

// AddToPlaylist adds one canonical track to a local playlist.
func (l *Library) AddToPlaylist(ctx context.Context, playlistID, trackID string) error {
	return l.db.AddPlaylistTrack(ctx, playlistID, trackID)
}

// RemoveFromPlaylist removes one track from a local playlist.
func (l *Library) RemoveFromPlaylist(ctx context.Context, playlistID, trackID string) error {
	return l.db.RemovePlaylistTrack(ctx, playlistID, trackID)
}

// DeletePlaylist removes a playlist. Imported copies also clear their import.
func (l *Library) DeletePlaylist(ctx context.Context, id string) error {
	return l.db.DeletePlaylist(ctx, id)
}

// RenamePlaylist updates the name and description of a local playlist.
func (l *Library) RenamePlaylist(ctx context.Context, id, name, description string) error {
	return l.db.RenamePlaylist(ctx, id, name, description)
}

// ImportedPlaylist looks up the single local copy of one remote playlist.
func (l *Library) ImportedPlaylist(ctx context.Context, sourceID music.SourceID, remoteID string) (librarydb.Playlist, bool, error) {
	playlist, err := l.db.ImportedPlaylist(ctx, librarydb.DefaultProfile, sourceID.String(), remoteID)
	if errors.Is(err, librarydb.ErrPlaylistNotFound) {
		return librarydb.Playlist{}, false, nil
	}
	if err != nil {
		return librarydb.Playlist{}, false, err
	}
	return playlist, true, nil
}

// EnsureImportedPlaylist creates or refreshes the local copy metadata.
func (l *Library) EnsureImportedPlaylist(ctx context.Context, sourceID music.SourceID, remoteID, name, description, coverURL string) (librarydb.Playlist, error) {
	return l.db.EnsureImportedPlaylist(ctx, librarydb.DefaultProfile, sourceID.String(), remoteID, name, description, coverURL)
}

// UpdateImportedMeta refreshes display metadata for an already-imported
// playlist without creating a local copy for remote-only entries.
func (l *Library) UpdateImportedMeta(ctx context.Context, sourceID music.SourceID, remoteID, name, description, coverURL string) error {
	return l.db.UpdateImportedPlaylistMeta(ctx, librarydb.DefaultProfile, sourceID.String(), remoteID, name, description, coverURL)
}

// ReplaceImportedMembers mirrors a remote snapshot into the local copy.
func (l *Library) ReplaceImportedMembers(ctx context.Context, playlistID string, trackIDs []string, name, description, coverURL string) error {
	return l.db.ReplacePlaylistMembers(ctx, playlistID, trackIDs, name, description, coverURL)
}

// SplitIncompatibleMerges repairs rows where an import merged source tracks
// whose versions differ (studio vs chorus vs a re-recorded edition).
func (l *Library) SplitIncompatibleMerges(ctx context.Context) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	n, err := l.db.SplitIncompatibleMerges(ctx, librarydb.DefaultProfile)
	if err != nil {
		return 0, err
	}
	if n > 0 {
		if err := l.load(context.Background()); err != nil {
			return n, err
		}
	}
	return n, nil
}
