// Fails as a command it runs fails.
package error_values

import (
	"context"

	"dagger.io/dagger/core"
)

type ErrorValues struct{}

// Fail returns the error of a command that prints output and exits with 42.
func (*ErrorValues) Fail(ctx context.Context) (string, error) {
	return core.NewContainer().From("alpine:3.22").
		WithExec([]string{"sh", "-c", "echo some output; echo some error >&2; exit 42"}).
		Stdout(ctx)
}
