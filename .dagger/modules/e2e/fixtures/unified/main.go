package unified_app

import (
	"context"
	_ "embed"
	"fmt"
	"time"

	"dagger.io/dagger/core"
	dep "example.com/unified/internal/dagger/clients/client-dep"
)

type UnifiedApp struct{}

//go:embed own.txt
var ownSource string

func (*UnifiedApp) OwnSource() string { return ownSource }

func (*UnifiedApp) Greet(ctx context.Context) (string, error) {
	return dep.New().ClientDep().Greet(ctx, "entrypoint")
}

func (*UnifiedApp) LegacyContainer(value string) *core.Container {
	return dep.New().ClientDep().Container(value)
}

func (*UnifiedApp) LegacyRead(ctx context.Context, value *core.Container) (string, error) {
	return dep.New().ClientDep().Read(ctx, value)
}

func (*UnifiedApp) Echo(value *core.Container) *core.Container { return value }

func (*UnifiedApp) Logged() string {
	fmt.Println("user stdout")
	return "result"
}

// +cache="never"
func (*UnifiedApp) Fresh() string { return time.Now().UTC().Format(time.RFC3339Nano) }

type Status string

const Ready Status = "ready"

func (*UnifiedApp) State(status Status) Status { return status }
func (*UnifiedApp) Items() *Items              { return &Items{Keys: []string{"a", "b"}} }

// +collection
type Items struct {
	// +keys
	Keys []string
}

// +get
func (*Items) Lookup(key string) *Item { return &Item{Name: key} }

type Item struct{ Name string }
