package librarydb

import (
	"context"
	"fmt"
)

// maxMergeDepth bounds merged_into chains. A cycle or an absurdly long chain is
// treated as corruption instead of looping forever.
const maxMergeDepth = 32

// ResolveTrackID follows tracks.merged_into so callers that hold an ID from an
// older snapshot (queue file, playlist row, cached search result) still find the
// song. It returns the input when the ID is unknown or already canonical.
func (d *DB) ResolveTrackID(ctx context.Context, id string) (string, error) {
	resolved, err := d.resolveTrackIDs(ctx, []string{id})
	if err != nil {
		return "", err
	}
	return resolved[0], nil
}

// resolveTrackIDs resolves a batch in one query and preserves input order.
func (d *DB) resolveTrackIDs(ctx context.Context, ids []string) ([]string, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	rows, err := d.sql.QueryContext(ctx, `SELECT id, merged_into FROM tracks WHERE merged_into IS NOT NULL`)
	if err != nil {
		return nil, fmt.Errorf("librarydb: load merge refs: %w", err)
	}
	defer rows.Close()
	next := make(map[string]string)
	for rows.Next() {
		var id, target string
		if err := rows.Scan(&id, &target); err != nil {
			return nil, fmt.Errorf("librarydb: scan merge ref: %w", err)
		}
		next[id] = target
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	out := make([]string, len(ids))
	for i, id := range ids {
		value, err := followMerge(next, id)
		if err != nil {
			return nil, err
		}
		out[i] = value
	}
	return out, nil
}

func followMerge(next map[string]string, id string) (string, error) {
	current := id
	for depth := 0; depth < maxMergeDepth; depth++ {
		target, ok := next[current]
		if !ok {
			return current, nil
		}
		if target == current {
			return current, nil
		}
		current = target
	}
	return "", fmt.Errorf("librarydb: merge chain too long at %s", id)
}
