package main

import (
	"context"

	sdk "dagger.io/dagger"
	"example.com/legacy-client-dep/internal/dagger"
)

type ClientDep struct{}

// Greet exercises the public SDK imports and root methods used before the split.
func (*ClientDep) Greet(ctx context.Context, name string) (string, error) {
	client, err := sdk.Connect(ctx)
	if err != nil {
		return "", err
	}
	defer client.Close()
	message := "hello " + name
	return client.Container().WithNewFile("/greet", message).File("/greet").Contents(ctx)
}

func (*ClientDep) Container(value string) *dagger.Container {
	return dag.Container().WithNewFile("/value", value)
}

func (*ClientDep) Read(ctx context.Context, value *dagger.Container) (string, error) {
	return value.File("/value").Contents(ctx)
}
