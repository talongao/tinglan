package source

import (
	"context"
	"errors"
	"testing"

	"tinglan/native/internal/music"
)

type fakeAdapter struct {
	id   music.SourceID
	page ImportPage
	err  error
}

func (f fakeAdapter) ID() music.SourceID { return f.id }
func (f fakeAdapter) Capabilities() Capabilities {
	return Capabilities{Import: true, Resolve: true}
}
func (f fakeAdapter) Login(context.Context) (LoginState, error) {
	return LoginState{LoggedIn: true}, nil
}
func (f fakeAdapter) Import(context.Context, ImportRequest) (ImportPage, error) {
	return f.page, f.err
}
func (f fakeAdapter) Resolve(context.Context, ResolveRequest) (music.Playback, error) {
	return music.Playback{URL: "https://cdn.example/a"}, nil
}

func TestRegistryPreservesRegistrationOrder(t *testing.T) {
	registry := NewRegistry()
	if err := registry.Register(fakeAdapter{id: music.SourceHuawei}); err != nil {
		t.Fatal(err)
	}
	if err := registry.Register(fakeAdapter{id: music.SourceNetEase}); err != nil {
		t.Fatal(err)
	}
	order := registry.Order()
	if len(order) != 2 || order[0] != music.SourceHuawei || order[1] != music.SourceNetEase {
		t.Fatalf("order = %v", order)
	}
	if _, ok := registry.Adapter(music.SourceNetEase); !ok {
		t.Fatal("adapter missing")
	}
	if adapters := registry.Adapters(); len(adapters) != 2 {
		t.Fatalf("adapters = %d", len(adapters))
	}
}

func TestRegistryReplaceKeepsPosition(t *testing.T) {
	registry := NewRegistry()
	registry.Register(fakeAdapter{id: music.SourceHuawei})
	registry.Register(fakeAdapter{id: music.SourceNetEase})
	registry.Register(fakeAdapter{id: music.SourceHuawei})
	order := registry.Order()
	if len(order) != 2 || order[0] != music.SourceHuawei {
		t.Fatalf("replace changed order: %v", order)
	}
}

func TestRegistryRejectsInvalidAdapters(t *testing.T) {
	registry := NewRegistry()
	if err := registry.Register(nil); err == nil {
		t.Fatal("nil adapter accepted")
	}
	if err := registry.Register(fakeAdapter{id: ""}); err == nil {
		t.Fatal("empty id accepted")
	}
	if err := registry.Register(fakeAdapter{id: "bad/id"}); err == nil {
		t.Fatal("invalid id accepted")
	}
}

func TestRegistryReorderIgnoresUnknownAndKeepsRest(t *testing.T) {
	registry := NewRegistry()
	for _, id := range []music.SourceID{music.SourceHuawei, music.SourceNetEase, "third"} {
		registry.Register(fakeAdapter{id: id})
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
