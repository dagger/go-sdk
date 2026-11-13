package moved_app

import (
	"context"

	dep "example.com/moved-app/internal/dagger/clients/moved-dep"
)

type MovedApp struct{}

// Hello returns what the module's local dependency says.
func (*MovedApp) Hello(ctx context.Context) (string, error) {
	return dep.New().Hello(ctx)
}
