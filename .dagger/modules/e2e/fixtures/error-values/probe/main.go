package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	"dagger.io/dagger"
	"dagger.io/dagger/core"
)

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
	description, err := mod.Description(ctx)
	if err != nil {
		panic(err)
	}
	if description != "Fails as a command it runs fails." {
		panic(fmt.Sprintf("the module's description is %q, not its package's doc comment", description))
	}

	err = c.Do(ctx, &dagger.Request{Query: `{errorValues{fail}}`}, &dagger.Response{})
	var execErr *dagger.ExecError
	if !errors.As(err, &execErr) {
		var extended interface{ Extensions() map[string]any }
		if errors.As(err, &extended) {
			panic(fmt.Sprintf("the function's error is not the exec's: %s, with extensions %v", err, extended.Extensions()))
		}
		panic(fmt.Sprintf("the function's error is not the exec's: %#v", err))
	}
	if execErr.ExitCode != 42 || strings.TrimSpace(execErr.Stdout) != "some output" || strings.TrimSpace(execErr.Stderr) != "some error" {
		panic(fmt.Sprintf("the exec's error lost its values: exit code %d, stdout %q, stderr %q", execErr.ExitCode, execErr.Stdout, execErr.Stderr))
	}
	if strings.Contains(err.Error(), "call module entrypoint") {
		panic("the error comes from the entrypoint: " + err.Error())
	}
	fmt.Printf("error values passed\nerror: %s\nextensions: %v\n", err, execErr.Extensions())
}
