package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

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
		SourceRootPath: ".dagger/modules/e2e/out/beta16-migration",
	})
	check(module.Serve(ctx))

	var returned struct {
		Beta16Frozen struct {
			Message   string
			Container struct{ File struct{ Contents string } }
		}
	}
	check(client.Do(ctx, &dagger.Request{
		Query: `{beta16Frozen{message container(value:"returned beta16 handle"){file(path:"/value"){contents}}}}`,
	}, &dagger.Response{Data: &returned}))
	if returned.Beta16Frozen.Message != "beta16 generated global" {
		panic("the retained author global did not call the shared session")
	}
	if returned.Beta16Frozen.Container.File.Contents != "returned beta16 handle" {
		panic("the migrated module did not return its core handle")
	}

	id, err := query.Container().WithNewFile("/value", "caller-created core handle").ID(ctx)
	check(err)
	var forwarded struct {
		Beta16Frozen struct {
			ReadContainer string
			Echo          struct{ File struct{ Contents string } }
		}
	}
	check(client.Do(ctx, &dagger.Request{
		Query:     `query($id:ID!){beta16Frozen{readContainer(value:$id) echo(value:$id){file(path:"/value"){contents}}}}`,
		Variables: map[string]any{"id": id},
	}, &dagger.Response{Data: &forwarded}))
	if forwarded.Beta16Frozen.ReadContainer != "caller-created core handle" ||
		forwarded.Beta16Frozen.Echo.File.Contents != "caller-created core handle" {
		panic("a shared core handle did not decode and round trip")
	}

	// Check the persisted decision structurally as well as exercising its API.
	data, err := os.ReadFile(".dagger-generated.json")
	check(err)
	var metadata struct {
		Compatibility struct {
			GlobalClient *bool `json:"globalClient"`
		} `json:"compatibility"`
	}
	check(json.Unmarshal(data, &metadata))
	if metadata.Compatibility.GlobalClient == nil || !*metadata.Compatibility.GlobalClient {
		panic("the migrated compatibility decision was not preserved")
	}
	fmt.Println("beta16 migration passed")
}

func check(err error) {
	if err != nil {
		panic(err)
	}
}
