package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
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

	data, err := os.ReadFile(".dagger-generated.json")
	check(err)
	var ownership struct {
		Version       int               `json:"version"`
		Files         map[string]string `json:"files"`
		Compatibility json.RawMessage   `json:"compatibility"`
	}
	check(json.Unmarshal(data, &ownership))
	if ownership.Version != 1 {
		panic("the migrated generated-file ownership has an unexpected version")
	}
	if len(ownership.Compatibility) != 0 {
		panic("the migrated generated-file ownership retained compatibility state")
	}
	for _, path := range []string{"dagger.gen.go", "internal/dagger/dagger.gen.go"} {
		want, ok := ownership.Files[path]
		if !ok {
			panic("the migrated generated-file ownership does not track " + path)
		}
		contents, err := os.ReadFile(path)
		check(err)
		sum := sha256.Sum256(contents)
		if hex.EncodeToString(sum[:]) != want {
			panic("the migrated generated-file ownership has a stale hash for " + path)
		}
	}
	fmt.Println("beta16 migration passed")
}

func check(err error) {
	if err != nil {
		panic(err)
	}
}
