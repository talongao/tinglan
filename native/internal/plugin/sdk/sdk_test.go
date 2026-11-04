package sdk

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"testing"

	"tinglan/native/internal/lyrics"
	"tinglan/native/internal/music"
	"tinglan/native/internal/plugin/wire"
	"tinglan/native/internal/source"
)

type lyricsStub struct {
	source.Source
	ref source.TrackRef
}

func (s *lyricsStub) Describe() source.Descriptor {
	return source.Descriptor{SchemaVersion: 1, ID: "stub", Name: "Stub"}
}

func (s *lyricsStub) Lyrics() source.LyricsSource { return lyricsOnly{s} }

type lyricsOnly struct{ s *lyricsStub }

func (l lyricsOnly) Lyrics(ctx context.Context, ref source.TrackRef) ([]lyrics.Line, error) {
	l.s.ref = ref
	return []lyrics.Line{{Time: 0, Text: "hello " + ref.ID, Words: []lyrics.Word{{Time: 0, Text: "hello"}, {Time: .4, Text: ref.ID}}}}, nil
}

func raw(s string) json.RawMessage { return json.RawMessage(s) }

func TestLyricsUnwrapsRefParam(t *testing.T) {
	stub := &lyricsStub{}
	inR, inW := io.Pipe()
	outR, outW := io.Pipe()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- (&Plugin{Source: stub}).Serve(ctx, inR, outW) }()

	reader := bufio.NewReader(outR)
	send := func(msg wire.Message) {
		t.Helper()
		if err := wire.Write(inW, msg); err != nil {
			t.Fatalf("write: %v", err)
		}
	}
	expect := func(id string) wire.Message {
		t.Helper()
		msg, err := wire.Read(reader)
		if err != nil {
			t.Fatalf("read: %v", err)
		}
		if msg.IDString() != id {
			t.Fatalf("id = %q, want %q", msg.IDString(), id)
		}
		if msg.Error != nil {
			t.Fatalf("error response: %+v", msg.Error)
		}
		return msg
	}

	init, _ := wire.Request("h:1", "initialize", map[string]any{"assignedId": "stub", "protocolVersion": map[string]int{"min": 1, "max": 1}})
	send(init)
	expect("h:1")
	send(wire.Message{JSONRPC: "2.0", Method: "initialized", Params: raw(`{"generation":1}`)})

	req, _ := wire.Request("h:2", "source.lyrics.get", map[string]any{"ref": source.TrackRef{Source: music.SourceHuawei, ID: "t1"}})
	send(req)
	msg := expect("h:2")
	if stub.ref.ID != "t1" || stub.ref.Source != music.SourceHuawei {
		t.Fatalf("ref = %+v, want source huawei id t1", stub.ref)
	}
	// Word timings must survive the wire: that is what makes the shared lyric
	// format useful for every provider, not just the built-in one.
	var lines []lyrics.Line
	if err := json.Unmarshal(msg.Result, &lines); err != nil {
		t.Fatalf("lyrics result: %v", err)
	}
	if len(lines) != 1 || len(lines[0].Words) != 2 || lines[0].Words[1].Time != .4 || lines[0].Words[1].Text != "t1" {
		t.Fatalf("lyrics words = %+v", lines)
	}

	cancel()
	_ = inW.Close()
	<-done
}
