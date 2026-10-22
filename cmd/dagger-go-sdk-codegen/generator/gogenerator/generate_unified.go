package gogenerator

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/dagger/go-sdk/cmd/dagger-go-sdk-codegen/generator"
	"github.com/dagger/go-sdk/cmd/dagger-go-sdk-codegen/generator/gogenerator/templates"
	"github.com/dagger/go-sdk/cmd/dagger-go-sdk-codegen/introspection"
	"github.com/psanford/memfs"
	"golang.org/x/tools/imports"
)

// GenerateUnifiedClient emits module bindings that reuse core's Go types.
// The generated root owns no engine lifecycle and exposes only module fields.
func (g *GoGenerator) GenerateUnifiedClient(ctx context.Context, schema *introspection.Schema, schemaVersion string) (*generator.GeneratedState, error) {
	generator.SetSchemaParents(schema)
	generator.SetSchema(schema)
	if g.Config.PackageImport == "" {
		return nil, fmt.Errorf("package import path is required")
	}
	name := g.Config.PackageName
	if name == "" {
		name = clientPackageName(g.Config.OutputDir, g.Config.PackageImport)
	}
	pkg := &PackageInfo{PackageName: name, PackageImport: g.Config.PackageImport}
	mfs := memfs.New()
	if err := generateDependencyFiles(ctx, g.Config, schema, schemaVersion, mfs, pkg, schema.DependencyNames()); err != nil {
		return nil, err
	}
	var bound generator.BoundModule
	if g.Config.ClientConfig != nil {
		bound = g.Config.ClientConfig.BoundModule
	}
	address := bound.Ref
	if bound.Kind != generator.ModuleKindGit {
		address = bound.Path
		if address != "" && !strings.HasPrefix(address, "/") {
			address = "/" + address
		}
	}
	// Add aliases only for core types referenced by module bindings. They retain
	// type identity across generated clients and dagger.io/dagger/core.
	referenced := map[string]bool{"ID": true, "Node": true}
	coreNames := map[string]bool{}
	for _, typ := range schema.Types {
		if typ.Name == "Query" || typ.Directives.SourceMap() != nil {
			continue
		}
		if typ.Kind == introspection.TypeKindScalar && (typ.Name == "String" || typ.Name == "Int" || typ.Name == "Float" || typ.Name == "Boolean") {
			continue
		}
		coreNames[typ.Name] = true
		if typ.Kind == introspection.TypeKindInterface {
			coreNames[typ.Name+"Client"] = true
		}
	}
	files, err := fs.ReadDir(mfs, ".")
	if err != nil {
		return nil, err
	}
	for _, file := range files {
		data, err := fs.ReadFile(mfs, file.Name())
		if err != nil {
			return nil, err
		}
		parsed, err := parser.ParseFile(token.NewFileSet(), file.Name(), data, 0)
		if err != nil {
			return nil, err
		}
		ast.Inspect(parsed, func(n ast.Node) bool {
			if id, ok := n.(*ast.Ident); ok && coreNames[id.Name] {
				referenced[id.Name] = true
			}
			return true
		})
	}
	constants, owners := map[string]bool{}, map[string]string{}
	var deps []dependencyClient
	if g.Config.ModuleConfig != nil {
		if g.Config.GlobalClient {
			if deps, err = dependencyClients(g.Config); err != nil {
				return nil, err
			}
		}
		// Public signatures omit private fields and function bodies. Preserve
		// the core aliases author code uses there, from the complete bootstrap
		// surface emitted before module analysis.
		funcs := templates.GoTemplateFuncs(schema, schema, schemaVersion, g.Config)
		helpers := coreHelpers(schema, funcs["FormatName"].(func(string) string), funcs["IsSelfChainable"].(func(introspection.Type) bool))
		if err := addAuthorCoreAliases(ctx, g.Config, coreNames, helpers, deps, referenced, constants, owners); err != nil {
			return nil, err
		}
	}
	var aliases bytes.Buffer
	names := make([]string, 0, len(referenced))
	for n := range referenced {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		pkg := "core"
		if owner, ok := owners[n]; ok {
			pkg = owner
		}
		if constants[n] {
			fmt.Fprintf(&aliases, "const %s = %s.%s\n", n, pkg, n)
			continue
		}
		target := n
		if pkg == "core" && n != "ID" && strings.HasSuffix(n, "ID") {
			target = "ID"
		}
		fmt.Fprintf(&aliases, "type %s = %s.%s\n", n, pkg, target)
	}
	// The global client of legacy author code reaches each dependency the way
	// the embedded bindings did: dag.Wolfi() is promoted from the dependency's
	// client, which shares the connection.
	var depImports, clientFields, clientInits strings.Builder
	for _, dep := range deps {
		fmt.Fprintf(&depImports, " %s %q\n", dep.qualifier, dep.importPath)
		fmt.Fprintf(&aliases, "type %sClient = %s.Client\n", dep.qualifier, dep.qualifier)
		fmt.Fprintf(&clientFields, "; *%sClient", dep.qualifier)
		fmt.Fprintf(&clientInits, ",%sClient:%s.Connect(connection)", dep.qualifier, dep.qualifier)
	}
	transport := fmt.Sprintf("dagger.ModuleGraphQLClient(connection, %q, %q)", address, bound.Pin)
	workspaceHelper := ""
	switch {
	case address == "":
		transport = "defaultOrExplicit(connection)"
	case bound.Kind != generator.ModuleKindGit:
		// A module bound by path is in the repository of the calling module.
		// The transport finds its Git ref at run time, so the generated client
		// names no commit and does not change from one commit to the next.
		transport = fmt.Sprintf("workspaceModuleClient(connection, %q)", address)
		workspaceHelper = templates.WorkspaceModuleHelper
	}
	embeddedCore, initQuery := "", "&Query{query:q}"
	if g.Config.ModuleConfig != nil {
		embeddedCore = "; *core.Query"
		initQuery = "&Query{query:q,Query:new(core.Query).WithGraphQLQuery(q)}"
	}
	source := fmt.Sprintf(`// Code generated by dagger. DO NOT EDIT.
package %s
import (
 "context"
 "encoding/json"
 "fmt"
 "reflect"
 "runtime"
 "sync"
 "weak"
 "dagger.io/dagger"
 "dagger.io/dagger/core"
 "github.com/dagger/querybuilder"
 "github.com/Khan/genqlient/graphql"
%s)
%s
// Client borrows a shared connection. Its API contains only the bound module.
type Client struct { *Query%s }
type Query struct { query *querybuilder.Selection; refetchID bool %s }
type DaggerObject = core.DaggerObject
type ExecError = dagger.ExecError
var marshalCtx = context.Background()
func SetMarshalContext(ctx context.Context) {marshalCtx=ctx;core.SetMarshalContext(ctx)}
// Connect constructs lazy bindings. An omitted connection uses the shared session.
func Connect(connections ...*dagger.Client) *Client {
 if len(connections)>1 {panic("Connect accepts at most one connection")}
 var connection *dagger.Client
 if len(connections)==1 {connection=connections[0]}
 q := querybuilder.Query().Client(%s)
 return &Client{Query:%s%s}
}

func defaultOrExplicit(client *dagger.Client) graphql.Client {
 if client==nil{return dagger.DefaultGraphQLClient()};return client.GraphQLClient()
}
func (c *Client) QueryBuilder() *querybuilder.Selection {return c.query}
func selectNode(q *querybuilder.Selection,id any,name string)*querybuilder.Selection{return q.Select("node").Arg("id",id).InlineFragment(name)}
func assertNotNil(name string,value any){if reflect.ValueOf(value).IsNil(){panic(fmt.Sprintf("unexpected nil pointer for argument %%q",name))}}
func Ref[T core.Loadable[T]](client *Client,id core.ID)T{return core.Ref[T](new(core.Query).WithGraphQLQuery(client.query),id)}
func Load[T core.Loadable[T]](ctx context.Context,client *Client,id core.ID)(T,error){return core.Load[T](ctx,new(core.Query).WithGraphQLQuery(client.query),id)}

%s
%s`, name, depImports.String(), aliases.String(), clientFields.String(), embeddedCore, transport, initQuery, clientInits.String(), templates.IDMemoHelper, workspaceHelper)
	formatted, err := imports.Process(filepath.Join(g.Config.OutputDir, "dagger.gen.go"), []byte(source), nil)
	if err != nil {
		return nil, err
	}
	if err = mfs.WriteFile("dagger.gen.go", formatted, 0644); err != nil {
		return nil, err
	}
	return &generator.GeneratedState{Overlay: mfs}, nil
}

// coreHelpers names the option structs and With functions that core types
// generate. The bootstrap client also has those of dependency types, which
// core does not.
func coreHelpers(schema *introspection.Schema, formatName func(string) string, selfChainable func(introspection.Type) bool) map[string]bool {
	moduleType := func(ref *introspection.TypeRef) bool {
		for ref != nil && ref.Name == "" {
			ref = ref.OfType
		}
		if ref == nil {
			return false
		}
		typ := schema.Types.Get(ref.Name)
		return typ != nil && typ.Directives.SourceMap() != nil
	}
	names := map[string]bool{}
	for _, typ := range schema.Types {
		if typ.Directives.SourceMap() != nil {
			continue
		}
		query := typ.Name == generator.QueryStructName
		if typ.Kind == introspection.TypeKindObject && !query && selfChainable(*typ) {
			names["With"+formatName(typ.Name)+"Func"] = true
		}
		for _, field := range typ.Fields {
			if !field.Args.HasOptionals() || field.Directives.SourceMap() != nil {
				continue
			}
			if !query {
				names[formatName(typ.Name)+formatName(field.Name)+"Opts"] = true
			} else if !moduleType(field.TypeRef) {
				names[formatName(field.Name)+"Opts"] = true
			}
		}
	}
	return names
}

type dependencyClient struct {
	// qualifier imports the client: its package name can be core or any other
	// name the module package already uses.
	qualifier, importPath string
	// exported names the types the client declares and the constants of its
	// enums, true for a constant.
	exported map[string]bool
}

func dependencyClients(cfg generator.Config) ([]dependencyClient, error) {
	dirs, err := filepath.Glob(filepath.Join(cfg.OutputDir, "internal", "dagger", "clients", "*"))
	if err != nil {
		return nil, err
	}
	var deps []dependencyClient
	for _, dir := range dirs {
		files, err := filepath.Glob(filepath.Join(dir, "*.go"))
		if err != nil {
			return nil, err
		}
		dep := dependencyClient{
			qualifier:  fmt.Sprintf("dep%d", len(deps)),
			importPath: cfg.PackageImport + "/clients/" + filepath.Base(dir),
			exported:   map[string]bool{},
		}
		generated := false
		enums := map[string]string{}
		for _, path := range files {
			data, err := os.ReadFile(path)
			if err != nil {
				return nil, err
			}
			if !bytes.HasPrefix(data, []byte("// Code generated by dagger. DO NOT EDIT.")) {
				continue
			}
			file, err := parser.ParseFile(token.NewFileSet(), path, data, 0)
			if err != nil {
				return nil, err
			}
			generated = true
			for _, decl := range file.Decls {
				gen, ok := decl.(*ast.GenDecl)
				if !ok {
					continue
				}
				for _, spec := range gen.Specs {
					switch spec := spec.(type) {
					case *ast.TypeSpec:
						// Aliases are the core types the client reuses.
						if name := spec.Name.Name; ast.IsExported(name) && !spec.Assign.IsValid() && name != "Client" && name != "Query" {
							dep.exported[name] = false
						}
					case *ast.ValueSpec:
						if enum, ok := spec.Type.(*ast.Ident); ok && gen.Tok == token.CONST {
							for _, name := range spec.Names {
								enums[name.Name] = enum.Name
							}
						}
					}
				}
			}
		}
		for name, enum := range enums {
			if _, ok := dep.exported[enum]; ok {
				dep.exported[name] = true
			}
		}
		if generated {
			deps = append(deps, dep)
		}
	}
	return deps, nil
}

func addAuthorCoreAliases(ctx context.Context, cfg generator.Config, coreNames, helpers map[string]bool, deps []dependencyClient, referenced, constants map[string]bool, owners map[string]string) error {
	// Dependency names never exist in core, and a name two dependencies
	// export stays unaliased.
	type depSymbol struct {
		pkg      string
		constant bool
	}
	depSymbols, shared := map[string]depSymbol{}, map[string]bool{}
	for _, dep := range deps {
		for name, constant := range dep.exported {
			if _, ok := depSymbols[name]; ok {
				shared[name] = true
			}
			depSymbols[name] = depSymbol{dep.qualifier, constant}
		}
	}
	for name := range shared {
		delete(depSymbols, name)
	}
	use := func(name string, symbols map[string]bool) {
		if shared[name] {
			return
		}
		if dep, ok := depSymbols[name]; ok {
			referenced[name], constants[name], owners[name] = true, dep.constant, dep.pkg
			// Generated code holds an interface value as its client struct.
			if client, ok := depSymbols[name+"Client"]; ok && !shared[name+"Client"] && client.pkg == dep.pkg {
				referenced[name+"Client"], constants[name+"Client"], owners[name+"Client"] = true, client.constant, client.pkg
			}
		} else if constant, exists := symbols[name]; exists {
			referenced[name], constants[name] = true, constant
		}
	}
	symbols := map[string]bool{} // value is true for a constant, false for a type
	bootstrap, err := filepath.Glob(filepath.Join(cfg.OutputDir, "internal", "dagger", "*.go"))
	if err != nil {
		return err
	}
	for _, path := range bootstrap {
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if !bytes.HasPrefix(data, []byte("// Code generated by dagger. DO NOT EDIT.")) {
			continue
		}
		file, err := parser.ParseFile(token.NewFileSet(), path, data, 0)
		if err != nil {
			return err
		}
		for _, decl := range file.Decls {
			gen, ok := decl.(*ast.GenDecl)
			if !ok {
				continue
			}
			for _, spec := range gen.Specs {
				switch spec := spec.(type) {
				case *ast.TypeSpec:
					name := spec.Name.Name
					if coreNames[name] || helpers[name] {
						symbols[name] = false
					}
				case *ast.ValueSpec:
					// Enum constants are typed by their enum; core has only
					// those of core enums.
					if enum, ok := spec.Type.(*ast.Ident); ok && gen.Tok == token.CONST && coreNames[enum.Name] {
						for _, name := range spec.Names {
							symbols[name.Name] = true
						}
					}
				}
			}
		}
	}
	// Ask Go which packages/files the module actually builds. Unused testdata,
	// hidden directories, nested modules and excluded build-tag files must not
	// contribute aliases or cause otherwise valid generation to fail.
	if _, err := os.Stat(filepath.Join(cfg.OutputDir, "go.mod")); os.IsNotExist(err) {
		return nil
	} else if err != nil {
		return err
	}
	cmd := exec.CommandContext(ctx, "go", "list", "-deps", "-json=Dir,GoFiles,CgoFiles", "./...")
	cmd.Dir = cfg.OutputDir
	for _, setting := range os.Environ() {
		if !strings.HasPrefix(setting, "CGO_ENABLED=") {
			cmd.Env = append(cmd.Env, setting)
		}
	}
	cmd.Env = append(cmd.Env, "CGO_ENABLED=0")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	output, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("list module source files: %w: %s", err, &stderr)
	}
	var authorFiles []string
	decoder := json.NewDecoder(bytes.NewReader(output))
	for {
		var pkg struct {
			Dir               string
			GoFiles, CgoFiles []string
		}
		if err := decoder.Decode(&pkg); err == io.EOF {
			break
		} else if err != nil {
			return err
		}
		rel, err := filepath.Rel(cfg.OutputDir, pkg.Dir)
		if err != nil {
			return err
		}
		if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) ||
			rel == filepath.Join("internal", "dagger") || strings.HasPrefix(rel, filepath.Join("internal", "dagger")+string(filepath.Separator)) {
			continue
		}
		for _, name := range append(pkg.GoFiles, pkg.CgoFiles...) {
			authorFiles = append(authorFiles, filepath.Join(pkg.Dir, name))
		}
	}
	for _, path := range authorFiles {
		file, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
		if err != nil {
			return err
		}
		imports := map[string]bool{}
		dotImport := false
		for _, spec := range file.Imports {
			path, err := strconv.Unquote(spec.Path.Value)
			if err != nil {
				return err
			}
			if path == cfg.PackageImport {
				name := "dagger"
				if spec.Name != nil {
					name = spec.Name.Name
				}
				dotImport = dotImport || name == "."
				imports[name] = true
			}
		}
		ast.Inspect(file, func(node ast.Node) bool {
			if id, ok := node.(*ast.Ident); ok && dotImport {
				use(id.Name, symbols)
			}
			selector, ok := node.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			pkg, ok := selector.X.(*ast.Ident)
			if !ok || !imports[pkg.Name] {
				return true
			}
			use(selector.Sel.Name, symbols)
			return true
		})
	}
	return nil
}
