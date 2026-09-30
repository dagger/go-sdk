package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/dagger/go-sdk/cmd/dagger-go-sdk-codegen/generator"
	"github.com/dagger/go-sdk/cmd/dagger-go-sdk-codegen/generator/gogenerator/templates"
	"github.com/dagger/go-sdk/cmd/dagger-go-sdk-codegen/introspection"
)

// Built-in module codegen supplies repository links for Git workspaces. Rewrite
// only links into that exact workspace commit to the local source positions the
// same generator emits for a Directory workspace. Keep the original workspace
// and its resolved dependencies; reconstructing it would discard configuration.
func runNormalizeSourceLinks(args []string) error {
	flags := flag.NewFlagSet("normalize-source-links", flag.ContinueOnError)
	schemaPath := flags.String("introspection-json-path", "", "dependency introspection schema")
	output := flags.String("output", "", "generated scope directory")
	scope := flags.String("scope", ".", "scope directory relative to the workspace root")
	repo := flags.String("workspace-repo-url", "", "workspace HTML repository URL")
	pin := flags.String("workspace-pin", "", "workspace commit")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if *schemaPath == "" || *output == "" || *repo == "" || *pin == "" {
		return fmt.Errorf("normalizing source links requires a schema, output, workspace repository, and commit")
	}
	data, err := os.ReadFile(*schemaPath)
	if err != nil {
		return err
	}
	var response introspection.Response
	if err := json.Unmarshal(data, &response); err != nil {
		return err
	}
	if response.Schema == nil {
		return fmt.Errorf("introspection JSON has no __schema")
	}
	catalog := newSourceLinkCatalog(response.Schema, response.SchemaVersion)
	anchor, err := workspaceSourceLinkAnchor(*repo, *pin)
	if err != nil {
		return err
	}
	return filepath.WalkDir(*output, func(filename string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() || entry.Type()&fs.ModeSymlink != 0 || !strings.HasSuffix(filename, ".go") {
			return nil
		}
		data, err := os.ReadFile(filename)
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(*output, filepath.Dir(filename))
		if err != nil {
			return err
		}
		contextRoot, err := filepath.Rel(filepath.Join(*scope, rel), ".")
		if err != nil {
			return err
		}
		normalized, err := normalizeGeneratedSourceLinks(filename, data, catalog, anchor, contextRoot)
		if err != nil {
			return err
		}
		if bytes.Equal(data, normalized) {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		return os.WriteFile(filename, normalized, info.Mode().Perm())
	})
}

// Match the forge-specific links produced by ModuleSource.Git.Link. The trailing
// path separator prevents matching another repository or commit prefix.
func workspaceSourceLinkAnchor(repo, pin string) (string, error) {
	if pin == "" {
		return "", fmt.Errorf("workspace commit is empty")
	}
	u, err := url.Parse(repo)
	if err != nil || repo == "" {
		// Some clone URL forms cannot produce a source link. Keep those links
		// unchanged rather than introducing a new generation failure.
		return "", nil
	}
	repo = strings.TrimSuffix(repo, "/")
	switch u.Host {
	case "github.com", "gitlab.com":
		return repo + "/tree/" + pin + "/", nil
	case "dev.azure.com":
		return repo + "/commit/" + pin + "?path=/", nil
	default:
		return repo + "/src/" + pin + "/", nil
	}
}

type sourceLinkCatalog struct {
	types   map[string]*introspection.SourceMap
	methods map[string]*introspection.SourceMap
	fields  map[string]*introspection.SourceMap
	values  map[string]*introspection.SourceMap
}

func newSourceLinkCatalog(schema *introspection.Schema, version string) sourceLinkCatalog {
	generator.SetSchemaParents(schema)
	funcs := templates.GoTemplateFuncs(schema, schema, version, generator.Config{})
	name := funcs["FormatName"].(func(string) string)
	enum := funcs["FormatEnum"].(func(string, string) string)
	opts := funcs["FieldOptionsStructName"].(func(introspection.Field, ...string) string)
	iface := funcs["InterfaceClientName"].(func(string) string)
	groupEnums := funcs["GroupEnumByValue"].(func([]introspection.EnumValue) [][]introspection.EnumValue)
	catalog := sourceLinkCatalog{
		types: map[string]*introspection.SourceMap{}, methods: map[string]*introspection.SourceMap{},
		fields: map[string]*introspection.SourceMap{}, values: map[string]*introspection.SourceMap{},
	}
	for _, typ := range schema.Types {
		catalog.types[name(typ.Name)] = typ.Directives.SourceMap()
		if typ.Kind == introspection.TypeKindEnum {
			// Enum declarations use the schema name without Go initialism
			// formatting; their values use FormatEnum below.
			catalog.types[typ.Name] = typ.Directives.SourceMap()
		}
		for _, field := range typ.Fields {
			receiver := name(typ.Name)
			if typ.Name == generator.QueryStructName {
				receiver = "Client"
			} else if typ.Kind == introspection.TypeKindInterface {
				receiver = iface(typ.Name)
			}
			catalog.methods[receiver+"."+name(field.Name)] = field.Directives.SourceMap()
			if typ.Name == generator.QueryStructName {
				// Module bindings use Query; standalone clients use Client.
				catalog.methods["Query."+name(field.Name)] = field.Directives.SourceMap()
			}
			for _, arg := range field.Args {
				catalog.fields[opts(*field)+"."+name(arg.Name)] = arg.Directives.SourceMap()
			}
		}
		for _, field := range typ.InputFields {
			catalog.fields[name(typ.Name)+"."+name(field.Name)] = field.Directives.SourceMap()
		}
		for _, group := range groupEnums(typ.EnumValues) {
			for _, value := range group {
				catalog.values[enum(typ.Name, value.Name)] = value.Directives.SourceMap()
				catalog.values[enum("", value.Name)] = value.Directives.SourceMap()
			}
		}
	}
	return catalog
}

func normalizeGeneratedSourceLinks(filename string, data []byte, catalog sourceLinkCatalog, anchor, contextRoot string) ([]byte, error) {
	if anchor == "" || !bytes.HasPrefix(data, []byte("// Code generated by dagger. DO NOT EDIT.\n")) {
		return data, nil
	}
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, filename, data, parser.ParseComments)
	if err != nil {
		return nil, fmt.Errorf("parse generated file %s: %w", filename, err)
	}
	// Go declarations identify source maps even when a method and its arguments
	// have the same URL (repository links omit the column).
	byLine := map[int]*introspection.SourceMap{}
	add := func(pos token.Pos, source *introspection.SourceMap) {
		if source != nil && source.Filename != "" && strings.HasPrefix(source.URL, anchor) {
			byLine[fset.Position(pos).Line] = source
		}
	}
	for _, decl := range file.Decls {
		switch decl := decl.(type) {
		case *ast.FuncDecl:
			if decl.Recv == nil || decl.Body == nil || len(decl.Recv.List) != 1 {
				continue
			}
			receiver := decl.Recv.List[0].Type
			if pointer, ok := receiver.(*ast.StarExpr); ok {
				receiver = pointer.X
			}
			if ident, ok := receiver.(*ast.Ident); ok {
				add(decl.Body.Lbrace, catalog.methods[ident.Name+"."+decl.Name.Name])
			}
		case *ast.GenDecl:
			for _, spec := range decl.Specs {
				switch spec := spec.(type) {
				case *ast.TypeSpec:
					add(spec.Type.Pos(), catalog.types[spec.Name.Name])
					if structure, ok := spec.Type.(*ast.StructType); ok {
						for _, field := range structure.Fields.List {
							for _, ident := range field.Names {
								add(field.Pos(), catalog.fields[spec.Name.Name+"."+ident.Name])
							}
						}
					}
				case *ast.ValueSpec:
					for _, ident := range spec.Names {
						add(spec.Pos(), catalog.values[ident.Name])
					}
				}
			}
		}
	}
	type replacement struct {
		start, end int
		text       string
	}
	var replacements []replacement
	for _, group := range file.Comments {
		for _, comment := range group.List {
			source := byLine[fset.Position(comment.Pos()).Line]
			if source == nil {
				continue
			}
			renderedURL := source.URL
			if !strings.HasPrefix(renderedURL, "http://") && !strings.HasPrefix(renderedURL, "https://") {
				renderedURL = filepath.ToSlash(filepath.Join(contextRoot, renderedURL))
			}
			if comment.Text != "// "+source.Module+" ("+renderedURL+")" {
				continue
			}
			local := filepath.ToSlash(filepath.Join(contextRoot, source.Filename))
			replacements = append(replacements, replacement{
				fset.Position(comment.Pos()).Offset, fset.Position(comment.End()).Offset,
				fmt.Sprintf("// %s (%s:%d:%d)", source.Module, local, source.Line, source.Column),
			})
		}
	}
	// Apply byte edits from the end; formatting, strings, and all other comments
	// remain byte-for-byte unchanged.
	slices.Reverse(replacements)
	result := slices.Clone(data)
	for _, replacement := range replacements {
		result = append(result[:replacement.start], append([]byte(replacement.text), result[replacement.end:]...)...)
	}
	return result, nil
}
