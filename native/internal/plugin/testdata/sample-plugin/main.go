package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
)

type msg struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
}

func main() {
	reader := bufio.NewReaderSize(os.Stdin, 1<<16)
	for {
		body, err := readFrame(reader)
		if err != nil {
			return
		}
		var m msg
		if json.Unmarshal(body, &m) != nil {
			return
		}
		if m.Method == "shutdown" {
			return
		}
		if m.Method == "" {
			continue
		}
		var result any
		switch m.Method {
		case "initialize":
			result = map[string]any{
				"schemaVersion": 1, "id": "sample", "name": "示例音源", "shortName": "示例",
				"accent": "#2F6FEB",
				"capabilities": map[string]any{"search": true, "playlists": true, "playlistDetail": true,
					"albumDetail": true, "artistDetail": true, "history": true, "lyrics": false,
					"import": true, "resolve": true, "collections": []any{map[string]any{"kind": "favorites", "name": "示例收藏"}},
					"maxPageSize": 20},
				"resourceDomains": []string{"*.example.com"},
			}
		case "source.auth.status":
			result = map[string]any{"state": "signedOut"}
		case "source.catalog.search":
			result = map[string]any{"items": []any{
				map[string]any{"source": "sample", "id": "s1", "title": "示例歌曲 · 第一首", "artists": []any{map[string]any{"name": "示例艺人"}},
					"album": map[string]any{"title": "示例专辑"}, "accessible": true},
			}, "hasMore": false}
		case "source.catalog.playlists":
			result = map[string]any{"items": []any{map[string]any{"source": "sample", "id": "p1", "kind": "owned", "name": "示例歌单"}}, "hasMore": false}
		case "source.catalog.playlistDetail":
			result = map[string]any{"playlist": map[string]any{"source": "sample", "id": "p1", "name": "示例歌单"}, "tracks": []any{
				map[string]any{"source": "sample", "id": "s1", "title": "示例歌曲 · 第一首"},
			}, "hasMore": false}
		case "source.catalog.history":
			result = map[string]any{"items": []any{}, "hasMore": false}
		case "source.library.import":
			result = map[string]any{"tracks": []any{map[string]any{"source": "sample", "id": "s1", "title": "示例歌曲 · 第一首"}}}
		case "source.library.resolve":
			result = map[string]any{"quality": "standard", "access": "unknown"}
		default:
			result = map[string]any{}
		}
		raw, _ := json.Marshal(result)
		reply := msg{JSONRPC: "2.0", ID: m.ID}
		reply.Result = raw
		out, _ := json.Marshal(reply)
		fmt.Fprintf(os.Stdout, "Content-Length: %d\r\n\r\n%s", len(out), out)
	}
}

func readFrame(r *bufio.Reader) ([]byte, error) {
	length := -1
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			return nil, err
		}
		line = strings.TrimRight(line, "\r\n")
		if line == "" {
			break
		}
		if name, value, ok := strings.Cut(line, ":"); ok && strings.EqualFold(strings.TrimSpace(name), "Content-Length") {
			n, _ := strconv.Atoi(strings.TrimSpace(value))
			length = n
		}
	}
	if length < 0 {
		return nil, io.ErrUnexpectedEOF
	}
	body := make([]byte, length)
	_, err := io.ReadFull(r, body)
	return body, err
}
