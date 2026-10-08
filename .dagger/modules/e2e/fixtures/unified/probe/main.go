package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"reflect"

	"dagger.io/dagger"
	"dagger.io/dagger/core"
)

func main() {
	ctx := context.Background()
	c, err := dagger.Connect(ctx)
	check(err)
	defer c.Close()
	q := core.NewQuery(c)
	mod := q.Host().Directory("/workspace").AsModule(core.DirectoryAsModuleOpts{
		SourceRootPath: ".dagger/modules/e2e/out/unified-app",
	})
	if ref := os.Getenv("MODULE_REF"); ref != "" {
		mod = q.ModuleSource(ref).AsModule()
	}
	check(mod.Serve(ctx))
	query := func(text string) map[string]any {
		var value map[string]any
		check(c.Do(ctx, &dagger.Request{Query: text}, &dagger.Response{Data: &value}))
		return value["unifiedApp"].(map[string]any)
	}
	got := query(`{unifiedApp{state(status:READY) logged ownSource greet legacyContainer(value:"legacy handle"){file(path:"/value"){contents}} items{keys list{name} get(key:"a"){name} subset(keys:["b"]){keys list{name}}}}}`)
	want := map[string]any{
		"state": "READY", "logged": "result", "greet": os.Getenv("EXPECTED_GREETING"),
		"ownSource":       "own module",
		"legacyContainer": map[string]any{"file": map[string]any{"contents": "legacy handle"}},
		"items": map[string]any{
			"keys":   []any{"a", "b"},
			"list":   []any{map[string]any{"name": "a"}, map[string]any{"name": "b"}},
			"get":    map[string]any{"name": "a"},
			"subset": map[string]any{"keys": []any{"b"}, "list": []any{map[string]any{"name": "b"}}},
		},
	}
	if !reflect.DeepEqual(got, want) {
		panic(fmt.Sprintf("got %#v; want %#v", got, want))
	}
	if reflect.DeepEqual(query(`{unifiedApp{fresh}}`), query(`{unifiedApp{fresh}}`)) {
		panic("cache=never returned a cached result")
	}
	id, err := q.Container().From("alpine:3.22").WithNewFile("/value", "shared core handle").ID(ctx)
	check(err)
	var echoed struct {
		UnifiedApp struct {
			Echo       struct{ File struct{ Contents string } }
			LegacyRead string
		}
	}
	check(c.Do(ctx, &dagger.Request{
		Query:     `query($id:ID!){unifiedApp{echo(value:$id){file(path:"/value"){contents}} legacyRead(value:$id)}}`,
		Variables: map[string]any{"id": id},
	}, &dagger.Response{Data: &echoed}))
	if echoed.UnifiedApp.Echo.File.Contents != "shared core handle" {
		panic("core handle did not round trip")
	}
	if echoed.UnifiedApp.LegacyRead != "shared core handle" {
		panic("legacy module did not accept the shared core handle")
	}
	fmt.Println("unified entrypoint passed")
}

func check(err error) {
	if err != nil {
		var execErr *dagger.ExecError
		if errors.As(err, &execErr) {
			fmt.Fprintln(os.Stderr, execErr.Stderr)
		}
		panic(err)
	}
}
