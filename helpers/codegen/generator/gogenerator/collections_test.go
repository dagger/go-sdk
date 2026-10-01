package gogenerator

import (
	"codegen/generator"
	"codegen/introspection"
	"github.com/stretchr/testify/require"
	"testing"
)

// Collections remain ordinary objects in the public schema. The standalone
// generator must preserve the engine projection, including the batch object.
func TestGenerateCollectionClient(t *testing.T) {
	schema := buildClientSchema()
	ref := func(kind introspection.TypeKind, name string) *introspection.TypeRef {
		return &introspection.TypeRef{Kind: introspection.TypeKindNonNull, OfType: &introspection.TypeRef{Kind: kind, Name: name}}
	}
	list := func(elem *introspection.TypeRef) *introspection.TypeRef {
		return &introspection.TypeRef{Kind: introspection.TypeKindNonNull, OfType: &introspection.TypeRef{Kind: introspection.TypeKindList, OfType: elem}}
	}
	stringRef := ref(introspection.TypeKindScalar, "String")
	collectionRef := ref(introspection.TypeKindObject, "Items")
	itemRef := ref(introspection.TypeKindObject, "Item")
	batchRef := ref(introspection.TypeKindObject, "Items_Batch")
	schema.Types[1].Fields = append(schema.Types[1].Fields, &introspection.Field{Name: "items", TypeRef: collectionRef})
	for _, obj := range []*introspection.Type{
		{Kind: introspection.TypeKindObject, Name: "Items", Fields: []*introspection.Field{
			{Name: "keys", TypeRef: list(stringRef)},
			{Name: "list", TypeRef: list(itemRef)},
			{Name: "get", TypeRef: itemRef, Args: introspection.InputValues{{Name: "key", TypeRef: stringRef}}},
			{Name: "subset", TypeRef: collectionRef, Args: introspection.InputValues{{Name: "keys", TypeRef: list(stringRef)}}},
			{Name: "batch", TypeRef: batchRef},
		}},
		{Kind: introspection.TypeKindObject, Name: "Item", Fields: []*introspection.Field{{Name: "name", TypeRef: stringRef}}},
		{Kind: introspection.TypeKindObject, Name: "Items_Batch", Fields: []*introspection.Field{{Name: "summary", TypeRef: stringRef}}},
	} {
		obj.Directives = introspection.Directives{newSourceMapDirective("hello")}
		schema.Types = append(schema.Types, obj)
	}
	generator.SetSchemaParents(schema)
	gen := &GoGenerator{Config: generator.Config{OutputDir: t.TempDir(), PackageImport: "example.com/client", ClientConfig: &generator.ClientGeneratorConfig{BoundModule: generator.BoundModule{Kind: "DIR_SOURCE", Path: "hello"}}}}
	state, err := gen.GenerateClient(t.Context(), schema, "v1.0.0-beta.14")
	require.NoError(t, err)
	got := readOverlay(t, state, "hello.gen.go")
	for _, method := range []string{"Keys(ctx context.Context) ([]string, error)", "List(ctx context.Context) ([]Item, error)", "Get(key string) *Item", "Subset(keys []string) *Items", "Batch() *ItemsBatch"} {
		require.Contains(t, got, "func (r *Items) "+method)
	}
	require.Contains(t, got, "func (r *ItemsBatch) Summary(ctx context.Context) (string, error)")
	require.NotContains(t, got, "__daggerCollectionBase")
}
