package main

import (
	"context"
	"dagger/include-caller/internal/dagger"
	appClient "dagger/include-caller/internal/dagger/clients/include-app"
)

type IncludeCaller struct {
	App *appClient.IncludeApp // +private
}

func New(ws *dagger.Workspace) *IncludeCaller {
	return &IncludeCaller{App: appClient.New(ws)}
}

func (c *IncludeCaller) Greeting(ctx context.Context, ws *dagger.Workspace) (string, error) {
	return c.App.Greeting(ctx, ws)
}
