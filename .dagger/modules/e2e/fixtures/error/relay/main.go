package main

import "context"

type ErrorRelay struct{}

// RelayCheck fails with the error of its dependency.
// +check
func (*ErrorRelay) RelayCheck(ctx context.Context) error {
	return dag.ErrorLeaf().Fail(ctx)
}
