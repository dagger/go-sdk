package main

import (
	"context"
	"fmt"

	"dagger.io/dagger/core"
	clientdep "example.com/shortcut/internal/dagger/clients/client-dep"
)

func main() {
	ctx := context.Background()
	for _, name := range []string{"first", "reopened"} {
		greeting, err := clientdep.New().Greet(ctx, name)
		if err != nil {
			panic(err)
		}
		fmt.Println(greeting)
		if err := core.Close(); err != nil {
			panic(err)
		}
	}
}
