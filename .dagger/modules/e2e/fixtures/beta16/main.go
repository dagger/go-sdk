package main

import (
	"context"

	"example.com/beta16-frozen/internal/dagger"
)

type Beta16Frozen struct{}

func (*Beta16Frozen) Message(ctx context.Context) (string, error) {
	return dag.Container().WithNewFile("/message", "beta16 generated global").File("/message").Contents(ctx)
}

func (*Beta16Frozen) Container(value string) *dagger.Container {
	return dag.Container().WithNewFile("/value", value)
}

func (*Beta16Frozen) ReadContainer(ctx context.Context, value *dagger.Container) (string, error) {
	return value.File("/value").Contents(ctx)
}

func (*Beta16Frozen) Echo(value *dagger.Container) *dagger.Container {
	return value
}
