// Package main calls the include fixture through its generated client.
package main

import (
	"context"

	appClient "dagger/include-caller/internal/dagger/clients/include-app"
	"example.com/lib/greet"
)

type IncludeCaller struct{}

// Greeting returns this module's embedded greeting and the app's.
func (*IncludeCaller) Greeting(ctx context.Context) (string, error) {
	app, err := appClient.New().Greeting(ctx)
	return greet.Greeting() + " " + app, err
}
