package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"dagger.io/dagger"
	"dagger.io/dagger/core"
)

func main() {
	ctx := context.Background()
	client, err := dagger.Connect(ctx)
	check(err)
	defer client.Close()
	query := core.NewQuery(client)
	module := query.Host().Directory("/workspace").AsModule(core.DirectoryAsModuleOpts{
		SourceRootPath: os.Getenv("MODULE_PATH"),
	})
	check(module.Serve(ctx))
	request := &dagger.Request{Query: os.Getenv("QUERY")}
	// The CLI fills a Workspace argument; a raw query passes it as $ws.
	if strings.Contains(request.Query, "$ws") {
		ws, err := query.CurrentWorkspace().ID(ctx)
		check(err)
		request.Variables = map[string]any{"ws": ws}
	}
	var data json.RawMessage
	check(client.Do(ctx, request, &dagger.Response{Data: &data}))
	fmt.Print(string(data))
}

func check(err error) {
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
