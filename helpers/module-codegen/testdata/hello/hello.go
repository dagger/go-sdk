// Package hello is the end-to-end manifest-v2 generator fixture.
package hello

import "context"

type Hello struct {
	Prefix string
}

func New(
	// +default="Hello"
	prefix string,
) *Hello {
	return &Hello{Prefix: prefix}
}

func (m *Hello) Greet(ctx context.Context, name string) (string, error) {
	return m.Prefix + ", " + name, nil
}

func (m *Hello) Repeat(value string, count int) []string {
	result := make([]string, count)
	for i := range result {
		result[i] = value
	}
	return result
}

type Status string

const (
	StatusReady Status = "ready"
	StatusDone  Status = "done"
)

func (m *Hello) Status() Status { return StatusReady }
