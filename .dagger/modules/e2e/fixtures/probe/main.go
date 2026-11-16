package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"dagger.io/dagger"
	"dagger.io/dagger/core"
)

// The probe serves the module at MODULE_PATH in /workspace, runs QUERY
// against it, and prints the result's data as JSON, or "error: " and the
// error the caller got.
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
	var data map[string]any
	if err := c.Do(ctx, &dagger.Request{Query: os.Getenv("QUERY")}, &dagger.Response{Data: &data}); err != nil {
		fmt.Println("error: " + err.Error())
		return
	}
	encoded, err := json.Marshal(data)
	if err != nil {
		panic(err)
	}
	fmt.Println(string(encoded))
}
