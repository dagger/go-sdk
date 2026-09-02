package generate_app

import (
	"context"
	"errors"

	"dagger/generate-app/internal/dagger"
)

type GenerateApp struct {
	Prefix string
}

func New(
	// +default="Hello"
	prefix string,
) *GenerateApp {
	return &GenerateApp{Prefix: prefix}
}

func (m *GenerateApp) Echo(value string) string {
	return value
}

func (m *GenerateApp) Greet(name string) string {
	return m.Prefix + ", " + name
}

func (m *GenerateApp) DaggerEcho(ctx context.Context, value string) (string, error) {
	return dag.Container().
		From("alpine:3.22").
		WithExec([]string{"echo", "-n", value}).
		Stdout(ctx)
}

func (m *GenerateApp) Container(value string) *dagger.Container {
	return dag.Container().
		From("alpine:3.22").
		WithExec([]string{"echo", "-n", value})
}

func (m *GenerateApp) ReadContainer(ctx context.Context, container *dagger.Container) (string, error) {
	return container.Stdout(ctx)
}

func (m *GenerateApp) Repeat(value string, count int) []string {
	result := make([]string, count)
	for i := range result {
		result[i] = value
	}
	return result
}

func (m *GenerateApp) Optional(value *string) *string {
	return value
}

func (m *GenerateApp) Status() GenerateStatus {
	return GenerateStatusReady
}

func (m *GenerateApp) Fail() (string, error) {
	return "", errors.New("fixture failure")
}

func (m *GenerateApp) Void() {}

type GenerateStatus string

const (
	GenerateStatusReady GenerateStatus = "ready"
	GenerateStatusDone  GenerateStatus = "done"
)
