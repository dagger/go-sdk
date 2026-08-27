package templates

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"codegen/generator"
	"codegen/introspection"
)

func TestGoTemplateFuncsForModulePass(t *testing.T) {
	schema := &introspection.Schema{}
	cfg := generator.Config{ModuleConfig: &generator.ModuleGeneratorConfig{ModuleName: "test"}}

	bootstrap := GoTemplateFuncsForModule(context.Background(), schema, schema, "v1.0.0", cfg, nil, nil, 0)
	require.True(t, bootstrap["IsPartial"].(func() bool)(), "pass 0 is the bootstrap pass")
	require.True(t, bootstrap["IsModuleCode"].(func() bool)())

	final := GoTemplateFuncsForModule(context.Background(), schema, schema, "v1.0.0", cfg, nil, nil, 1)
	require.False(t, final["IsPartial"].(func() bool)(), "pass 1 renders the module's own source")
}

func TestGoTemplateFuncsClientUnchanged(t *testing.T) {
	schema := &introspection.Schema{}
	fm := GoTemplateFuncs(schema, schema, "v1.0.0", generator.Config{ClientConfig: &generator.ClientGeneratorConfig{}})
	require.Contains(t, fm, "BoundModule")
	require.False(t, fm["IsModuleCode"].(func() bool)())
	require.True(t, fm["IsStandaloneClient"].(func() bool)())
}
