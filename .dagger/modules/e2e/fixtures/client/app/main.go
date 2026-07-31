package main

import "context"

type ClientApp struct{}

func (m *ClientApp) Hello(ctx context.Context, name string) (string, error) {
	return dag.ClientDep().Greet(ctx, name)
}
