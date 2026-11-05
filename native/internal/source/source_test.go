package source

import (
	"context"
	"errors"
	"testing"

	"tinglan/native/internal/music"
)

type fakeSource struct {
	// Only ID is used by the registry tests; the rest is never called.
	Source
	id   music.SourceID
	page ImportPage
	err  error
}

func (f fakeSource) ID() music.SourceID { return f.id }

func TestRegistryPreservesRegistrationOrder(t *testing.T) {
	registry := NewRegistry()
	if err := registry.RegisterSource(fakeSource{id: music.SourceHuawei}); err != nil {
		t.Fatal(err)
	}
	if err := registry.RegisterSource(fakeSource{id: music.SourceNetEase}); err != nil {
		t.Fatal(err)
	}
	order := registry.Order()
	if len(order) != 2 || order[0] != music.SourceHuawei || order[1] != music.SourceNetEase {
		t.Fatalf("order = %v", order)
	}
	if _, ok := registry.Source(music.SourceNetEase); !ok {
		t.Fatal("source missing")
	}
	if sources := registry.SourceList(); len(sources) != 2 {
		t.Fatalf("sources = %d", len(sources))
	}
}

func TestRegistryReplaceKeepsPosition(t *testing.T) {
	registry := NewRegistry()
	registry.RegisterSource(fakeSource{id: music.SourceHuawei})
	registry.RegisterSource(fakeSource{id: music.SourceNetEase})
	registry.RegisterSource(fakeSource{id: music.SourceHuawei})
	order := registry.Order()
	if len(order) != 2 || order[0] != music.SourceHuawei {
		t.Fatalf("replace changed order: %v", order)
	}
}

func TestRegistryRejectsInvalidSources(t *testing.T) {
	registry := NewRegistry()
	if err := registry.RegisterSource(nil); err == nil {
		t.Fatal("nil source accepted")
	}
	if err := registry.RegisterSource(fakeSource{id: ""}); err == nil {
		t.Fatal("empty id accepted")
	}
	if err := registry.RegisterSource(fakeSource{id: "bad/id"}); err == nil {
		t.Fatal("invalid id accepted")
	}
}

func TestRegistryReorderIgnoresUnknownAndKeepsRest(t *testing.T) {
	registry := NewRegistry()
	for _, id := range []music.SourceID{music.SourceHuawei, music.SourceNetEase, "third"} {
		registry.RegisterSource(fakeSource{id: id})
	}
	registry.Reorder([]music.SourceID{"third", "unknown", music.SourceHuawei, "third"})
	order := registry.Order()
	want := []music.SourceID{"third", music.SourceHuawei, music.SourceNetEase}
	if len(order) != len(want) {
		t.Fatalf("order = %v", order)
	}
	for i := range want {
		if order[i] != want[i] {
			t.Fatalf("order = %v, want %v", order, want)
		}
	}
}

func TestErrorTaxonomy(t *testing.T) {
	base := errors.New("boom")
	err := NewError(music.SourceHuawei, "import", KindRateLimit, base)
	if KindOf(err) != KindRateLimit {
		t.Fatalf("kind = %v", KindOf(err))
	}
	if !Retryable(err) {
		t.Fatal("rate limit should be retryable")
	}
	if !errors.Is(err, base) {
		t.Fatal("wrapped error lost")
	}
	if KindOf(NewError(music.SourceHuawei, "import", KindLogin, base)) != KindLogin {
		t.Fatal("login kind lost")
	}
	if Retryable(NewError(music.SourceHuawei, "import", KindPermission, base)) {
		t.Fatal("permission should not retry")
	}
	if NewError(music.SourceHuawei, "import", KindLogin, nil) != nil {
		t.Fatal("nil error should stay nil")
	}
	if KindOf(context.Canceled) != KindCanceled {
		t.Fatal("canceled not classified")
	}
	if KindOf(errors.New("plain")) != KindTransient {
		t.Fatal("unknown error should be transient")
	}
}

func TestImportPageDone(t *testing.T) {
	if !(ImportPage{}).Done() {
		t.Fatal("empty cursor should be done")
	}
	if (ImportPage{Next: "50"}).Done() {
		t.Fatal("next cursor should not be done")
	}
}
