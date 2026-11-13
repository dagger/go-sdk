package main

import (
	"context"
	"fmt"
	"os"

	"dagger.io/dagger"
	"dagger.io/dagger/core"
)

// The probe serves the module at MODULE_PATH in /workspace and prints what
// its hello function returns.
func main() {
	ctx := context.Background()
	c, err := dagger.Connect(ctx)
	if err != nil {
		panic(err)
	}
	defer c.Close()
	mod := core.NewQuery(c).Host().Directory("/workspace").AsModule(core.DirectoryAsModuleOpts{
		SourceRootPath: os.Getenv("MODULE_PATH"),
	})
	if err := mod.Serve(ctx); err != nil {
		panic(err)
	}
	var got struct{ MovedApp struct{ Hello string } }
	if err := c.Do(ctx, &dagger.Request{Query: `{movedApp{hello}}`}, &dagger.Response{Data: &got}); err != nil {
		panic(err)
	}
	fmt.Println(got.MovedApp.Hello)
}
