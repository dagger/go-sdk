package templates

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"go/format"
	"go/parser"
	"go/token"
	"go/types"
	"path"
	"strconv"
	"strings"

	. "github.com/dave/jennifer/jen" //nolint:staticcheck
	"github.com/iancoleman/strcase"
	"golang.org/x/mod/semver"
	"golang.org/x/tools/go/packages"
	"golang.org/x/tools/imports"

	"github.com/dagger/go-sdk/cmd/dagger-go-sdk-codegen/generator"
	"github.com/dagger/go-sdk/cmd/dagger-go-sdk-codegen/introspection"
)

// V2Artifacts are the generated files that replace the legacy Go runtime
// entrypoint. The Go source keeps the established static dispatcher and codec.
type V2Artifacts struct {
	ModuleSource     []byte
	DispatchSource   []byte
	EntrypointSource []byte
}

// EntrypointSources is what a module's build reads outside its module
// directory. The entrypoint's sources function returns these files beside the
// module directory, and the module's manifest includes the same files (see
// ManifestInclude). An engine that reads the manifest's include loads them
// with the module and does not run sources; an older one runs sources. Either
// way the engine builds the module and keys its results on these files.
type EntrypointSources struct {
	// Anchor is the directory that holds the module directory and every
	// included path, relative to the module directory, such as "..".
	Anchor string
	// Include holds patterns relative to Anchor.
	Include []string
}

// sourcesFloor is the first Dagger release that calls the entrypoint's
// sources function and passes no workspace to types and call.
const sourcesFloor = "v1.0.0-beta.18"

// platformFloor is the first Dagger release with Directory.withPlatform and
// a prebuild for another platform (Module._prebuild's platform): an
// entrypoint of a module that targets it declares its process's binary as
// made for its platform, which an engine image of another platform can
// then ship (not yet released).
const platformFloor = "v1.0.0-beta.26"

// identifierCasingFloor is the first Dagger release whose view names enum
// members by their values, FunctionCachePolicy.PER_SESSION rather than
// PerSession; no view has both. The entrypoint of a module that targets it
// is served at that view (not yet released).
const identifierCasingFloor = "v1.0.0-beta.26"

// casesIdentifiers reports whether the entrypoint names enum members as the
// view of identifierCasingFloor and later does: whether the module targets
// identifierCasingFloor or later.
func (mod *v2Module) casesIdentifiers() bool {
	return mod.targetVersion != "" && semver.Compare(mod.targetVersion, identifierCasingFloor) >= 0
}

// declaresPlatform reports whether the entrypoint builds its process's
// binary for a platform it is given and declares it for that platform:
// whether the module targets platformFloor or later.
func (mod *v2Module) declaresPlatform() bool {
	return mod.targetVersion != "" && semver.Compare(mod.targetVersion, platformFloor) >= 0
}

// GenerateV2Artifacts analyzes a Go module with the existing parser and emits
// a manifest-v2 entrypoint around its established static dispatcher. The
// entrypoint declares targetVersion, the Dagger release the generated code
// targets, to the module's process; empty declares nothing. Nil sources mean
// the build reads nothing outside the module directory.
func GenerateV2Artifacts(
	ctx context.Context,
	schema *introspection.Schema,
	schemaVersion string,
	cfg generator.Config,
	pkg *packages.Package,
	fset *token.FileSet,
	packageImport string,
	moduleSubpath string,
	goImage string,
	runtimeImage string,
	daggerGo bool,
	targetVersion string,
	sources *EntrypointSources,
) (*V2Artifacts, error) {
	funcs := goTemplateFuncs{
		CommonFunctions: generator.NewCommonFunctions(schemaVersion, &FormatTypeFunc{}),
		ctx:             ctx,
		cfg:             cfg,
		modulePkg:       pkg,
		moduleFset:      fset,
		schema:          schema,
		fullSchema:      schema,
		schemaVersion:   schemaVersion,
		pass:            1,
	}

	return funcs.generateV2Artifacts(packageImport, moduleSubpath, goImage, runtimeImage, daggerGo, targetVersion, sources)
}

type v2Module struct {
	objects        []*parsedObjectType
	interfaces     []*parsedIfaceType
	enums          []*parsedEnumType
	functionCases  map[string][]Code
	implementation *Statement
	sources        *EntrypointSources
	targetVersion  string
	// runtimeImage is the image the module's process runs in; empty for
	// defaultRuntimeImage.
	runtimeImage string
	// daggerGo builds the module's process with dagger/go, a client of the
	// module, instead of a go build of its own.
	daggerGo bool
}

// defaultRuntimeImage is the image a module's process runs in when the
// generator isn't given one.
const defaultRuntimeImage = "alpine:3.22"

func (funcs goTemplateFuncs) generateV2Artifacts(packageImport, moduleSubpath, goImage, runtimeImage string, daggerGo bool, targetVersion string, sources *EntrypointSources) (*V2Artifacts, error) {
	if !exposesModuleContext(funcs.schema) {
		return nil, errors.New("the generated Dang entrypoint needs Dagger " + sourcesFloor + " or later")
	}
	mod := &v2Module{
		functionCases:  map[string][]Code{},
		implementation: Empty(),
		sources:        sources,
		targetVersion:  targetVersion,
		runtimeImage:   runtimeImage,
		daggerGo:       daggerGo,
	}

	err := funcs.visitTypes(true, &visitorFuncs{
		RootVisitor: func(_ string) error { return nil },
		StructVisitor: func(ps *parseState, named *types.Named, _ *types.TypeName, spec *parsedObjectType, _ *types.Struct) error {
			if err := ps.fillObjectFunctionCases(named, mod.functionCases); err != nil {
				return fmt.Errorf("generate function cases for %s: %w", spec.name, err)
			}
			impl, err := spec.ImplementationCode()
			if err != nil {
				return fmt.Errorf("generate object codec for %s: %w", spec.name, err)
			}
			mod.implementation.Add(impl).Line()
			mod.objects = append(mod.objects, spec)
			return nil
		},
		IfaceVisitor: func(_ *parseState, _ *types.Named, _ *types.TypeName, spec *parsedIfaceType, _ *types.Interface) error {
			impl, err := spec.ImplementationCode()
			if err != nil {
				return fmt.Errorf("generate interface codec for %s: %w", spec.name, err)
			}
			mod.implementation.Add(impl).Line()
			mod.interfaces = append(mod.interfaces, spec)
			return nil
		},
		EnumVisitor: func(_ *parseState, _ *types.Named, _ *types.TypeName, spec *parsedEnumType, _ *types.Basic) error {
			impl, err := spec.ImplementationCode()
			if err != nil {
				return fmt.Errorf("generate enum codec for %s: %w", spec.name, err)
			}
			mod.implementation.Add(impl).Line()
			mod.enums = append(mod.enums, spec)
			return nil
		},
	})
	if err != nil {
		if errors.Is(err, errEmptyCode) {
			return nil, fmt.Errorf("module has no Go source")
		}
		return nil, err
	}

	moduleSource, err := mod.renderModuleSource(funcs.modulePkg.Name, packageImport, funcs.cfg.GlobalClient)
	if err != nil {
		return nil, err
	}
	var typeImports strings.Builder
	if funcs.cfg.UnifiedClient {
		typeImports.WriteString("core \"dagger.io/dagger/core\"\n")
	}
	parsedSource, err := parser.ParseFile(token.NewFileSet(), "dagger.gen.go", moduleSource, parser.ImportsOnly)
	if err != nil {
		return nil, fmt.Errorf("read dispatcher imports: %w", err)
	}
	existingImports := map[string]bool{}
	for _, spec := range parsedSource.Imports {
		path, _ := strconv.Unquote(spec.Path.Value)
		existingImports[path] = true
	}
	for path := range funcs.modulePkg.Imports {
		if path != "dagger.io/dagger/core" && !existingImports[path] {
			name := sourceImportName(funcs.modulePkg, path)
			if path == "dagger.io/dagger" && name == "dagger" {
				name = "daggerRuntime"
			}
			fmt.Fprintf(&typeImports, "%s %q\n", name, path)
		}
	}
	moduleSource = bytes.Replace(moduleSource, []byte("import ("), []byte("import (\n"+typeImports.String()), 1)
	moduleSource, err = imports.Process("dagger.gen.go", moduleSource, nil)
	if err != nil {
		return nil, fmt.Errorf("format dispatcher imports: %w", err)
	}
	dispatchSource, err := mod.renderDispatchSource(funcs.cfg.ModuleConfig.ModuleName, packageImport, funcs.modulePkg.Name)
	if err != nil {
		return nil, err
	}
	entrypointSource, err := mod.renderEntrypointSource(funcs.cfg.ModuleConfig.ModuleName, moduleSubpath, goImage, funcs.modulePkg.Name)
	if err != nil {
		return nil, err
	}

	return &V2Artifacts{
		ModuleSource:     moduleSource,
		DispatchSource:   dispatchSource,
		EntrypointSource: entrypointSource,
	}, nil
}

func (mod *v2Module) renderModuleSource(packageName, packageImport string, globalClient bool) ([]byte, error) {
	dispatch := v2InvokeSrc(mod.functionCases, mod.objects)
	global := ""
	if globalClient {
		global = "// Deprecated: use explicit core constructors and generated module clients.\nvar dag = dagger.Connect()\n"
	}
	source := fmt.Sprintf(`// Code generated by dagger. DO NOT EDIT.

package %s

import (
	"context"
	"encoding/json"
	"fmt"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/trace"
	"github.com/dagger/querybuilder"

	"%s/internal/dagger"
)

%s

func Tracer() trace.Tracer {
	return otel.Tracer("dagger.io/sdk.go")
}

var marshalCtx = context.Background()

func setMarshalContext(ctx context.Context) {
	marshalCtx = ctx
	dagger.SetMarshalContext(ctx)
}

type DaggerObject interface {
	querybuilder.GraphQLMarshaller
	ID(ctx context.Context) (dagger.ID, error)
}

type ExecError = dagger.ExecError

func ptr[T any](v T) *T { return &v }

func convertSlice[I any, O any](in []I, f func(I) O) []O {
	out := make([]O, len(in))
	for i, v := range in {
		out[i] = f(v)
	}
	return out
}

%#v

%s
`, packageName, packageImport, global, mod.implementation, dispatch)

	formatted, err := format.Source([]byte(source))
	if err != nil {
		return nil, fmt.Errorf("format generated module dispatcher: %w\n%s", err, source)
	}
	return formatted, nil
}

func v2InvokeSrc(functionCases map[string][]Code, objects []*parsedObjectType) string {
	objNames := make([]string, 0, len(objects))
	for _, obj := range objects {
		objNames = append(objNames, obj.name)
		if _, ok := functionCases[obj.name]; !ok {
			functionCases[obj.name] = nil
		}
		if obj.constructor == nil {
			// An object without functions has no cases yet, so it also
			// needs the default case that the function cases end with.
			if len(functionCases[obj.name]) == 0 {
				functionCases[obj.name] = []Code{Default().Block(
					Return(Nil(), Qual("fmt", "Errorf").Call(Lit("unknown function %s"), Id(fnNameVar))),
				)}
			}
			functionCases[obj.name] = append([]Code{
				Case(Lit("")).Block(Return(Op("&").Id(obj.name).Values(), Nil())),
			}, functionCases[obj.name]...)
		}
	}
	slicesSortStrings(objNames)

	constructorCases := make([]Code, 0, len(objNames)*2)
	objectCases := make([]Code, 0, len(objNames)+1)
	for _, objName := range objNames {
		constructorCases = append(constructorCases,
			Case(Lit(objName), Lit(strcase.ToLowerCamel(objName))).Block(
				Id(parentNameVar).Op("=").Lit(objName),
				Id(fnNameVar).Op("=").Lit(""),
			),
		)
		cases := functionCases[objName]
		objectCases = append(objectCases,
			Case(Lit(objName)).Block(
				If(Id(fnNameVar).Op("==").Lit(constructorFuncName)).Block(Id(fnNameVar).Op("=").Lit("")),
				Switch(Id(fnNameVar)).Block(cases...),
			),
		)
	}
	objectCases = append(objectCases, Default().Block(
		Return(Nil(), Qual("fmt", "Errorf").Call(Lit("unknown object %s"), Id(parentNameVar))),
	))

	fn := Func().Id("DaggerDispatch").Params(
		Id("ctx").Qual("context", "Context"),
		Id(parentJSONVar).Index().Byte(),
		Id(parentNameVar).String(),
		Id(fnNameVar).String(),
		Id(inputArgsVar).Map(String()).Index().Byte(),
	).Params(Id("_").Id("any"), Id("err").Error()).Block(
		Id("setMarshalContext").Call(Id("ctx")),
		Id("_").Op("=").Id(inputArgsVar),
		If(Id(parentNameVar).Op("==").Lit("")).Block(Switch(Id(fnNameVar)).Block(constructorCases...)),
		Switch(Id(parentNameVar)).Block(objectCases...),
	)
	return fmt.Sprintf("%#v", fn)
}

func slicesSortStrings(values []string) {
	for i := 1; i < len(values); i++ {
		for j := i; j > 0 && values[j] < values[j-1]; j-- {
			values[j], values[j-1] = values[j-1], values[j]
		}
	}
}

func (mod *v2Module) renderEntrypointSource(moduleName, moduleSubpath, goImage string, packageNames ...string) ([]byte, error) {
	var defs []string
	for _, obj := range mod.objects {
		def, err := renderDangObject(obj, mod.casesIdentifiers())
		if err != nil {
			return nil, err
		}
		// The entrypoint protocol needs an explicit constructor to expose the
		// module's main object. Match the Go runtime's implicit zero-value New.
		if obj.constructor == nil && strcase.ToCamel(obj.name) == strcase.ToCamel(moduleName) {
			def += "\n  .withConstructor(function(\"\", typeDef.withObject(" + strconv.Quote(obj.name) + ")))"
		}
		defs = append(defs, def)
	}
	for _, iface := range mod.interfaces {
		def, err := renderDangInterface(iface, mod.casesIdentifiers())
		if err != nil {
			return nil, err
		}
		defs = append(defs, def)
	}
	for _, enum := range mod.enums {
		def, err := renderDangEnum(enum)
		if err != nil {
			return nil, err
		}
		defs = append(defs, def)
	}

	var b bytes.Buffer
	b.WriteString("# Code generated by dagger. Do not edit.\n\n")
	// Older engines refuse this entrypoint with a Dang error at the type line
	// and print the two lines above it, so this line explains the error.
	fmt.Fprintf(&b, "# Needs Dagger %s or later; older engines report a missing \"workspace\" argument.\n", sourcesFloor)
	b.WriteString("type Entrypoint implements ModuleEntrypoint {\n")
	writeSources(&b, mod.sources)
	workdir := `"/workspace/" + currentModule.sourceSubpath`
	buildTarget := "./cmd/" + strcase.ToKebab(moduleName) + "-dispatch"
	if len(packageNames) > 0 && packageNames[0] == "main" {
		buildTarget = "."
	}
	declares := mod.declaresPlatform()
	if mod.daggerGo {
		writeDaggerGoDispatch(&b, moduleSubpath, buildTarget, declares)
	} else {
		writeGoBuildDispatch(&b, goImage, workdir, buildTarget, declares)
	}
	// The engine evaluates prebuild before the module's first call, so the
	// call finds its binary built, or joins the build in flight. For another
	// platform's engine, such as an engine image of another platform ships,
	// the engine asks for that platform's.
	dispatch := "dispatch"
	if declares {
		dispatch = "dispatch(defaultPlatform)"
		b.WriteString("  pub prebuild(platform: Platform): File! {\n    dispatch(platform ?? defaultPlatform)\n  }\n\n")
	} else {
		b.WriteString("  pub prebuild: File! {\n    dispatch\n  }\n\n")
	}
	runtimeImage := mod.runtimeImage
	if runtimeImage == "" {
		runtimeImage = defaultRuntimeImage
	}
	// The engine reads the image the module's calls run in from
	// runtimeImage, as it reads prebuild, such as to ship it in an engine
	// image of any platform; the calls start from it. It needs nothing newer
	// than the entrypoint itself does, so every target declares it, and an
	// engine that doesn't read it ignores it.
	fmt.Fprintf(&b, "  pub runtimeImage(platform: Platform): Container! {\n    container(platform: platform ?? defaultPlatform).from(%s)\n  }\n\n", strconv.Quote(runtimeImage))
	b.WriteString("  pub types: [TypeDef!]! {\n")
	b.WriteString("    [\n")
	for i, def := range defs {
		b.WriteString(indentDang(def, 6))
		if i != len(defs)-1 {
			b.WriteByte(',')
		}
		b.WriteByte('\n')
	}
	b.WriteString("    ]\n")
	b.WriteString("  }\n\n")
	b.WriteString(`  pub call(
    receiverType: String!,
    receiverValue: JSON,
    fnName: String!,
    fnArgs: JSON!,
    callDigest: String,
  ): JSON! {
    let request = JSON.encode({{
      receiverType: receiverType,
      receiverValue: receiverValue,
      fnName: fnName,
      fnArgs: fnArgs,
    }})
    # A build failure stays out of the rescue below, so it surfaces unchanged.
    let binary = ` + dispatch + `.sync
    let result = runtimeImage(defaultPlatform)
      .withFile("/usr/local/bin/dispatch", binary, permissions: 755)
      .withMountedDirectory("/workspace", currentModule.contextDirectory)
`)
	fmt.Fprintf(&b, "      .withWorkdir(%s)\n", workdir)
	b.WriteString("      .withEnvVariable(\"DAGGER_MODULE_CALL_DIGEST\", callDigest ?? \"\")\n")
	if mod.targetVersion != "" {
		fmt.Fprintf(&b, "      .withEnvVariable(\"DAGGER_CLIENT_VERSION\", %s)\n", strconv.Quote(mod.targetVersion))
	}
	b.WriteString(`      .withExec(
        ["/usr/local/bin/dispatch", "engine-call", "/dagger/result.json"],
        stdin: request,
        experimentalPrivilegedNesting: true,
      )
      .file("/dagger/result.json")
      .contents rescue {
        err: GraphQLError => raiseFunctionError(err)
      }
    (result :: JSON!)
  }

  # The exec error carries the end of the dispatcher's stderr, where its last
  # marker line holds the function's error; without one, the exec failed
  # before the function returned, and its error is raised again.
  let raiseFunctionError(err: GraphQLError!): String! {
    let marker = "` + functionErrorMarker + `"
    let stderr = ((JSON.decode(err.extensions) :: {{stderr: String}}).stderr ?? "") rescue ""
    let lines = stderr.split("\n").filter { line => line.hasPrefix(marker) }
    if (lines.isEmpty) {
      raise err
    }
    raise ((JSON.decode(lines.takeLast(1).join("").trimPrefix(marker)) :: String!) rescue err.message)
  }
}
`)
	return b.Bytes(), nil
}

// functionErrorMarker starts the stderr line on which the dispatcher reports
// the function's error. The function's own output shares that stderr.
const functionErrorMarker = "::dagger-function-error::"

// writeSources writes the sources function, which returns the module
// directory and the files its build reads outside it at their paths from the
// workspace root.
func writeSources(b *bytes.Buffer, sources *EntrypointSources) {
	b.WriteString("  pub sources(workspace: Workspace!): Directory! {\n    directory\n")
	if sources != nil && len(sources.Include) > 0 {
		fmt.Fprintf(b, `      .withDirectory(workspace.cwd + %s, workspace.directory(%s,
        include: %s,
        exclude: ["**/.git", "**/*_test.go"],
        gitignore: true,
      ))
`, strconv.Quote("/"+sources.Anchor), strconv.Quote(sources.Anchor), dangStringList(sources.Include))
	}
	b.WriteString(`      .withDirectory(workspace.cwd, workspace.directory(".", exclude: ["**/.git"], gitignore: true))
  }

`)
}

// ManifestInclude is the manifest's include list for what a module's build
// reads outside its module directory, selecting what sources returns: each
// pattern relative to the module directory, then every test file under the
// anchor excluded, which the build never reads, and the module's own test
// files kept, as part of the module directory. A module that reads nothing
// outside gets an empty list, which still replaces sources.
func ManifestInclude(sources *EntrypointSources) []string {
	include := []string{}
	if sources == nil || len(sources.Include) == 0 {
		return include
	}
	for _, pattern := range sources.Include {
		include = append(include, path.Join(sources.Anchor, pattern))
	}
	return append(include, "!"+path.Join(sources.Anchor, "**", "*_test.go"), "**/*_test.go")
}

func exposesModuleContext(schema *introspection.Schema) bool {
	currentModule := schema.Types.Get("CurrentModule")
	if currentModule == nil {
		return false
	}
	var context, subpath bool
	for _, field := range currentModule.Fields {
		switch field.Name {
		case "contextDirectory":
			context = true
		case "sourceSubpath":
			subpath = true
		}
	}
	return context && subpath
}

func dangStringList(values []string) string {
	quoted := make([]string, len(values))
	for i, value := range values {
		quoted[i] = strconv.Quote(value)
	}
	return "[" + strings.Join(quoted, ", ") + "]"
}

func renderDangObject(spec *parsedObjectType, cased bool) (string, error) {
	base := "typeDef.withObject(" + strings.Join(append([]string{strconv.Quote(spec.name)}, objectOpts(spec.doc, spec.sourceMap, spec.deprecated)...), ", ") + ")"
	if spec.isCollection {
		base += "\n  .withCollection"
	}
	for _, method := range spec.methods {
		fn, err := renderDangFunction(method, cased)
		if err != nil {
			return "", err
		}
		base += "\n  .withFunction(\n" + indentDang(fn, 4) + "\n  )"
		if method.isCollectionGet {
			base += "\n  .withCollectionGet(" + strconv.Quote(method.name) + ")"
		}
	}
	for _, field := range spec.fields {
		if field.isPrivate {
			continue
		}
		typeDef, err := renderDangType(field.typeSpec)
		if err != nil {
			return "", err
		}
		args := []string{strconv.Quote(field.name), typeDef}
		args = append(args, fieldOpts(field.doc, field.sourceMap, field.deprecated)...)
		base += "\n  .withField(" + strings.Join(args, ", ") + ")"
		if field.isCollectionKeys {
			base += "\n  .withCollectionKeys(" + strconv.Quote(field.name) + ")"
		}
		if field.isCollectionDelta {
			base += "\n  .withCollectionDelta(" + strconv.Quote(field.name) + ")"
		}
	}
	if spec.constructor != nil {
		fn, err := renderDangFunctionNamed(spec.constructor, "", cased)
		if err != nil {
			return "", err
		}
		base += "\n  .withConstructor(\n" + indentDang(fn, 4) + "\n  )"
	}
	return base, nil
}

func renderDangInterface(spec *parsedIfaceType, cased bool) (string, error) {
	args := []string{strconv.Quote(spec.name)}
	args = append(args, objectOpts(spec.doc, spec.sourceMap, nil)...)
	base := "typeDef.withInterface(" + strings.Join(args, ", ") + ")"
	for _, method := range spec.methods {
		fn, err := renderDangFunction(method, cased)
		if err != nil {
			return "", err
		}
		base += "\n  .withFunction(\n" + indentDang(fn, 4) + "\n  )"
	}
	return base, nil
}

func renderDangEnum(spec *parsedEnumType) (string, error) {
	args := []string{strconv.Quote(spec.name)}
	args = append(args, objectOpts(spec.doc, spec.sourceMap, nil)...)
	base := "typeDef.withEnum(" + strings.Join(args, ", ") + ")"
	for _, value := range spec.values {
		member := []string{strconv.Quote(value.name)}
		if value.value != "" {
			member = append(member, "value: "+strconv.Quote(value.value))
		}
		if value.doc != "" {
			member = append(member, "description: "+strconv.Quote(strings.TrimSpace(value.doc)))
		}
		if value.sourceMap != nil {
			member = append(member, "sourceMap: "+renderDangSourceMap(value.sourceMap))
		}
		if value.deprecated != nil {
			member = append(member, "deprecated: "+strconv.Quote(strings.TrimSpace(*value.deprecated)))
		}
		base += "\n  .withEnumMember(" + strings.Join(member, ", ") + ")"
	}
	return base, nil
}

// renderDangFunction renders a function's definition. cased names enum
// members as the view of identifierCasingFloor and later does.
func renderDangFunction(spec *funcTypeSpec, cased bool) (string, error) {
	return renderDangFunctionNamed(spec, spec.name, cased)
}

func renderDangFunctionNamed(spec *funcTypeSpec, name string, cased bool) (string, error) {
	returnType := "typeDef.withKind(TypeDefKind.VOID_KIND).withOptional(true)"
	if spec.returnSpec != nil {
		var err error
		returnType, err = renderDangType(spec.returnSpec)
		if err != nil {
			return "", err
		}
	}
	base := "function(" + strconv.Quote(name) + ", " + returnType + ")"
	if spec.doc != "" {
		base += "\n  .withDescription(" + strconv.Quote(strings.TrimSpace(spec.doc)) + ")"
	}
	never, perSession, defaultPolicy := "Never", "PerSession", "Default"
	if cased {
		never, perSession, defaultPolicy = "NEVER", "PER_SESSION", "DEFAULT"
	}
	switch spec.cachePolicy {
	case "never":
		base += "\n  .withCachePolicy(FunctionCachePolicy." + never + ")"
	case "session":
		base += "\n  .withCachePolicy(FunctionCachePolicy." + perSession + ")"
	case "":
	default:
		base += "\n  .withCachePolicy(FunctionCachePolicy." + defaultPolicy + ", timeToLive: " + strconv.Quote(strings.TrimSpace(spec.cachePolicy)) + ")"
	}
	if spec.sourceMap != nil {
		base += "\n  .withSourceMap(" + renderDangSourceMap(spec.sourceMap) + ")"
	}
	if spec.deprecated != nil {
		base += "\n  .withDeprecated(reason: " + strconv.Quote(strings.TrimSpace(*spec.deprecated)) + ")"
	}
	if spec.isCheck {
		base += "\n  .withCheck"
	}
	if spec.isGenerator {
		base += "\n  .withGenerator"
	}
	if spec.isUp {
		base += "\n  .withUp"
	}
	if spec.isAgent {
		base += "\n  .withAgent"
	}
	for _, arg := range spec.argSpecs {
		if arg.isContext {
			continue
		}
		typeDef, err := renderDangType(arg.typeSpec)
		if err != nil {
			return "", err
		}
		if arg.optional {
			typeDef += ".withOptional(true)"
		}
		args := []string{strconv.Quote(arg.name), typeDef}
		if arg.description != "" {
			args = append(args, "description: "+strconv.Quote(strings.TrimSpace(arg.description)))
		}
		if arg.hasDefaultValue {
			defaultValue, err := renderDefaultValue(arg)
			if err != nil {
				return "", err
			}
			// A cast keeps the JSON text. JSON.decode would materialize the
			// decoded value instead, which the engine cannot read back.
			args = append(args, "defaultValue: ("+strconv.Quote(defaultValue)+" :: Dagger.JSON!)")
		}
		if arg.defaultPath != "" {
			args = append(args, "defaultPath: "+strconv.Quote(arg.defaultPath))
		}
		if len(arg.ignore) > 0 {
			values := make([]string, len(arg.ignore))
			for i, value := range arg.ignore {
				values[i] = strconv.Quote(value)
			}
			args = append(args, "ignore: ["+strings.Join(values, ", ")+"]")
		}
		if arg.sourceMap != nil {
			args = append(args, "sourceMap: "+renderDangSourceMap(arg.sourceMap))
		}
		if arg.deprecated != nil {
			args = append(args, "deprecated: "+strconv.Quote(*arg.deprecated))
		}
		if arg.defaultAddress != "" {
			args = append(args, "defaultAddress: "+strconv.Quote(arg.defaultAddress))
		}
		base += "\n  .withArg(" + strings.Join(args, ", ") + ")"
	}
	return base, nil
}

func renderDefaultValue(arg paramSpec) (string, error) {
	if enumType, ok := arg.typeSpec.(*parsedEnumTypeReference); ok {
		value, ok := arg.defaultValue.(string)
		if !ok {
			return "", fmt.Errorf("enum default for %s is not a string", arg.name)
		}
		member := enumType.lookup(value)
		if member == nil {
			return "", fmt.Errorf("unknown enum default %q for %s", value, arg.name)
		}
		return strconv.Quote(member.name), nil
	}
	encoded, err := json.Marshal(arg.defaultValue)
	if err != nil {
		return "", fmt.Errorf("encode default for %s: %w", arg.name, err)
	}
	return string(encoded), nil
}

func renderDangType(spec ParsedType) (string, error) {
	switch value := spec.(type) {
	case *parsedPrimitiveType:
		var result string
		if value.scalarType != nil {
			result = "typeDef.withScalar(" + strconv.Quote(value.scalarType.Obj().Name()) + ")"
		} else if value.goType.Kind() == types.Invalid {
			result = "typeDef.withKind(TypeDefKind.VOID_KIND)"
		} else {
			switch value.goType.Info() {
			case types.IsString:
				result = "typeDef.withKind(TypeDefKind.STRING_KIND)"
			case types.IsInteger:
				result = "typeDef.withKind(TypeDefKind.INTEGER_KIND)"
			case types.IsBoolean:
				result = "typeDef.withKind(TypeDefKind.BOOLEAN_KIND)"
			case types.IsFloat:
				result = "typeDef.withKind(TypeDefKind.FLOAT_KIND)"
			default:
				return "", fmt.Errorf("unsupported basic type %s", value.goType)
			}
		}
		if value.isPtr {
			result += ".withOptional(true)"
		}
		return result, nil
	case *parsedSliceType:
		element, err := renderDangType(value.underlying)
		if err != nil {
			return "", err
		}
		return "typeDef.withListOf(" + element + ")", nil
	case *parsedObjectTypeReference:
		return "typeDef.withObject(" + strconv.Quote(value.name) + ")", nil
	case *parsedIfaceTypeReference:
		return "typeDef.withInterface(" + strconv.Quote(value.name) + ")", nil
	case *parsedEnumTypeReference:
		return "typeDef.withEnum(" + strconv.Quote(value.name) + ")", nil
	default:
		return "", fmt.Errorf("unsupported parsed type %T", spec)
	}
}

func objectOpts(description string, source *sourceMap, deprecated *string) []string {
	var opts []string
	if description != "" {
		opts = append(opts, "description: "+strconv.Quote(strings.TrimSpace(description)))
	}
	if source != nil {
		opts = append(opts, "sourceMap: "+renderDangSourceMap(source))
	}
	if deprecated != nil {
		opts = append(opts, "deprecated: "+strconv.Quote(strings.TrimSpace(*deprecated)))
	}
	return opts
}

func fieldOpts(description string, source *sourceMap, deprecated *string) []string {
	return objectOpts(description, source, deprecated)
}

func renderDangSourceMap(source *sourceMap) string {
	return fmt.Sprintf("sourceMap(%s, %d, %d)", strconv.Quote(source.filename), source.line, source.column)
}

func indentDang(value string, spaces int) string {
	pad := strings.Repeat(" ", spaces)
	return pad + strings.ReplaceAll(value, "\n", "\n"+pad)
}

func (mod *v2Module) renderDispatchSource(moduleName, packageImport string, packageNames ...string) ([]byte, error) {
	type cliFunction struct {
		DisplayName string
		Receiver    string
		FnName      string
		Args        []paramSpec
	}
	var functions []cliFunction
	for _, obj := range mod.objects {
		constructor := cliFunction{DisplayName: strcase.ToLowerCamel(obj.name), Receiver: obj.name}
		if obj.constructor != nil {
			constructor.Args = obj.constructor.argSpecs
		}
		functions = append(functions, constructor)
		for _, fn := range obj.methods {
			functions = append(functions, cliFunction{DisplayName: strcase.ToLowerCamel(fn.name), Receiver: obj.name, FnName: fn.name, Args: fn.argSpecs})
		}
	}

	var table strings.Builder
	table.WriteString("var functions = []functionSpec{\n")
	for _, fn := range functions {
		fmt.Fprintf(&table, "\t{Name: %q, Receiver: %q, FnName: %q, Args: map[string]argumentSpec{", fn.DisplayName, fn.Receiver, fn.FnName)
		for _, arg := range fn.Args {
			if arg.isContext {
				continue
			}
			defaultValue := ""
			if arg.hasDefaultValue {
				defaultValue, _ = renderDefaultValue(arg)
			}
			fmt.Fprintf(&table, "%q: {Required: %t, Default: %q, Encoding: %q},", arg.name, !arg.isOptional(), defaultValue, developerArgEncoding(arg.typeSpec))
		}
		table.WriteString("}},\n")
	}
	table.WriteString("}\n")

	source := fmt.Sprintf(`// Code generated by dagger. DO NOT EDIT.

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	telemetry "github.com/dagger/otel-go"
	"go.opentelemetry.io/otel/sdk/resource"
	semconv "go.opentelemetry.io/otel/semconv/v1.40.0"

	module %q
)

// callRequest is the ModuleEntrypoint.call input. The entrypoint passes each
// JSON value as JSON text in a string; a JSON value is accepted too.
type callRequest struct {
	ReceiverType string `+"`json:\"receiverType\"`"+`
	ReceiverValue json.RawMessage `+"`json:\"receiverValue\"`"+`
	FnName string `+"`json:\"fnName\"`"+`
	FnArgs json.RawMessage `+"`json:\"fnArgs\"`"+`
}

type argumentSpec struct { Required bool; Default string; Encoding string }
type functionSpec struct {
	Name string
	Receiver string
	FnName string
	Args map[string]argumentSpec
}

%s

func main() {
	if len(os.Args) < 2 { fatalf("usage: %%s engine-call | call <FUNCTION> [--ARG VALUE]", os.Args[0]) }
	// The engine passes the function call's trace in TRACEPARENT and its
	// collector in OTEL_*; without them, spans of the author's code are lost.
	ctx := telemetry.InitEmbedded(context.Background(), resource.NewWithAttributes(
		semconv.SchemaURL,
		semconv.ServiceNameKey.String("dagger-go-sdk"),
	))
	var err error
	switch os.Args[1] {
	case "engine-call":
        output := os.Stdout
        if len(os.Args) > 2 {
            if err = os.MkdirAll(filepath.Dir(os.Args[2]), 0755); err != nil { break }
            output, err = os.Create(os.Args[2])
            if err != nil { break }
            defer output.Close()
        }
        err = engineCall(ctx, os.Stdin, output)
	case "call": err = developerCall(ctx, os.Args[2:], os.Stdout)
	default: err = fmt.Errorf("unknown mode %%q", os.Args[1])
	}
	// fatalf exits without running deferred calls, so spans are flushed first.
	telemetry.Close()
	if err != nil { fatalf("%%v", err) }
}

// fatalf reports the error last on stderr, as one marker line: a failed exec
// gives the entrypoint only its last 100 KiB of stderr. JSON can encode a
// byte in six, so 16 KiB of text keeps the line within that.
func fatalf(format string, args ...any) {
	message := fmt.Sprintf(format, args...)
	reported := message
	if len(reported) > 16<<10 {
		reported = strings.ToValidUTF8(reported[:16<<10], "") + "..."
	}
	encoded, _ := json.Marshal(reported)
	fmt.Fprintf(os.Stderr, "%%s\n%%s%%s\n", message, %q, encoded)
	os.Exit(2)
}

func engineCall(ctx context.Context, input io.Reader, output io.Writer) error {
	dec := json.NewDecoder(input)
	var req callRequest
	if err := dec.Decode(&req); err != nil { return fmt.Errorf("decode call request: %%w", err) }
	var extra any
	if err := dec.Decode(&extra); err != io.EOF { return fmt.Errorf("call request has trailing data") }
	result, err := dispatch(ctx, req)
	if err != nil { return err }
	enc := json.NewEncoder(output)
	enc.SetEscapeHTML(false)
	return enc.Encode(result)
}

func dispatch(ctx context.Context, req callRequest) (any, error) {
	receiver, err := jsonValue("receiverValue", req.ReceiverValue)
	if err != nil { return nil, err }
	fnArgs, err := jsonValue("fnArgs", req.FnArgs)
	if err != nil { return nil, err }
	values := map[string]json.RawMessage{}
	if err := json.Unmarshal(fnArgs, &values); err != nil { return nil, fmt.Errorf("fnArgs is not a JSON object: %%w", err) }
	args := make(map[string][]byte, len(values))
	for name, value := range values { args[name] = value }
	return module.DaggerDispatch(ctx, receiver, req.ReceiverType, req.FnName, args)
}

// jsonValue returns the JSON value in a request field. A JSON string holds
// JSON text, so it is decoded once. A missing field reads as null.
func jsonValue(field string, raw json.RawMessage) (json.RawMessage, error) {
	if len(raw) == 0 || string(raw) == "null" { return json.RawMessage("null"), nil }
	if raw[0] != '"' { return raw, nil }
	var text string
	if err := json.Unmarshal(raw, &text); err != nil { return nil, fmt.Errorf("decode %%s: %%w", field, err) }
	if !json.Valid([]byte(text)) { return nil, fmt.Errorf("%%s is not valid JSON text", field) }
	return json.RawMessage(text), nil
}

func developerCall(ctx context.Context, argv []string, output io.Writer) error {
	if len(argv) == 0 { return fmt.Errorf("call requires a function name; available: %%s", strings.Join(availableFunctions(), ", ")) }
	spec, err := findFunction(argv[0])
	if err != nil { return err }
	values := map[string]json.RawMessage{}
	receiver := json.RawMessage("{}")
	for argv = argv[1:]; len(argv) > 0; argv = argv[2:] {
		if len(argv) < 2 || !strings.HasPrefix(argv[0], "--") { return fmt.Errorf("expected --ARG VALUE") }
		name, raw := strings.TrimPrefix(argv[0], "--"), argv[1]
		if name == "receiver-json" {
			if !json.Valid([]byte(raw)) { return fmt.Errorf("--receiver-json is not valid JSON") }
			receiver = json.RawMessage(raw)
			continue
		}
		arg, ok := spec.Args[name]
		if !ok { return fmt.Errorf("unknown argument --%%s", name) }
		encoded, err := encodeDeveloperArgument(raw, arg.Encoding)
		if err != nil { return fmt.Errorf("invalid --%%s: %%w", name, err) }
		values[name] = encoded
	}
	for name, arg := range spec.Args {
		if _, ok := values[name]; ok { continue }
		if arg.Default != "" { values[name] = json.RawMessage(arg.Default); continue }
		if arg.Required { return fmt.Errorf("required argument --%%s is not set", name) }
	}
	fnArgs, err := json.Marshal(values)
	if err != nil { return err }
	result, err := dispatch(ctx, callRequest{ReceiverType: spec.Receiver, ReceiverValue: receiver, FnName: spec.FnName, FnArgs: fnArgs})
	if err != nil { return err }
	encoded, err := json.Marshal(result)
	if err != nil { return err }
	var text string
	if string(encoded) != "null" && json.Unmarshal(encoded, &text) == nil { _, err = fmt.Fprintln(output, text) } else { _, err = fmt.Fprintln(output, string(encoded)) }
	return err
}

func availableFunctions() []string {
	names := make([]string, 0, len(functions))
	for _, fn := range functions { names = append(names, fn.Name) }
	sort.Strings(names)
	return names
}

func encodeDeveloperArgument(raw, encoding string) (json.RawMessage, error) {
	if raw == "null" { return json.RawMessage("null"), nil }
	if encoding == "string" {
		encoded, err := json.Marshal(raw)
		return encoded, err
	}
	if !json.Valid([]byte(raw)) { return nil, fmt.Errorf("expected a JSON value") }
	return json.RawMessage(raw), nil
}

func normalize(value string) string { return strings.ToLower(strings.NewReplacer("-", "", "_", "").Replace(value)) }

func findFunction(name string) (functionSpec, error) {
	var found []functionSpec
	for _, fn := range functions { if normalize(fn.Name) == normalize(name) { found = append(found, fn) } }
	if len(found) == 0 { return functionSpec{}, fmt.Errorf("unknown function %%q; available: %%s", name, strings.Join(availableFunctions(), ", ")) }
	if len(found) > 1 { return functionSpec{}, fmt.Errorf("function %%q is ambiguous; use engine-call with receiverType", name) }
	return found[0], nil
}
`, packageImport, table.String(), functionErrorMarker)
	if len(packageNames) > 0 && packageNames[0] == "main" {
		source = strings.Replace(source, "\tmodule "+strconv.Quote(packageImport)+"\n", "", 1)
		source = strings.Replace(source, "module.DaggerDispatch(", "DaggerDispatch(", 1)
	}

	formatted, err := format.Source([]byte(source))
	if err != nil {
		return nil, fmt.Errorf("format dispatch command: %w\n%s", err, source)
	}
	_ = moduleName
	return formatted, nil
}

func developerArgEncoding(spec ParsedType) string {
	switch value := spec.(type) {
	case *parsedPrimitiveType:
		if value.scalarType != nil && value.scalarType.Obj().Name() == "JSON" {
			return "json"
		}
		if value.scalarType != nil || value.goType.Info()&types.IsString != 0 {
			return "string"
		}
	case *parsedEnumTypeReference:
		return "string"
	case *parsedObjectTypeReference:
		if value.moduleName == "" {
			return "string"
		}
	case *parsedIfaceTypeReference:
		if value.moduleName == "" {
			return "string"
		}
	}
	return "json"
}

// writeGoBuildDispatch writes the entrypoint's dispatch member as a go build
// of the module's process at buildTarget, in goImage, with the module's own
// Go cache volumes. With declares, it builds for a platform it is given,
// which the engine's own Go toolchain compiles for, and declares the binary
// as made for that platform (Directory.withPlatform): an engine of that
// platform finds the one an engine of another platform made, such as in the
// entries its image ships.
func writeGoBuildDispatch(b *bytes.Buffer, goImage, workdir, buildTarget string, declares bool) {
	if declares {
		b.WriteString("  let dispatch(platform: Platform!): File! {\n")
		b.WriteString("    # Platform is its own scalar; a template is how Dang turns one into text.\n")
		b.WriteString("    let target = `${platform}`.split(\"/\")\n")
	} else {
		b.WriteString("  let dispatch: File! {\n")
	}
	b.WriteString("    container\n")
	fmt.Fprintf(b, "      .from(%s)\n", strconv.Quote(goImage))
	b.WriteString("      .withMountedCache(\"/go/pkg/mod\", cacheVolume(\"go-sdk-v2-mod\"))\n")
	b.WriteString("      .withMountedCache(\"/root/.cache/go-build\", cacheVolume(\"go-sdk-v2-build\"))\n")
	b.WriteString("      .withMountedDirectory(\"/workspace\", currentModule.contextDirectory)\n")
	fmt.Fprintf(b, "      .withWorkdir(%s)\n", workdir)
	b.WriteString("      .withEnvVariable(\"CGO_ENABLED\", \"0\")\n")
	b.WriteString("      .withEnvVariable(\"GOWORK\", \"off\")\n")
	output := "/tmp/dispatch"
	if declares {
		b.WriteString("      .withEnvVariable(\"GOOS\", target.takeFirst(1).join(\"\"))\n")
		b.WriteString("      .withEnvVariable(\"GOARCH\", target.takeFirst(2).takeLast(1).join(\"\"))\n")
		output = "/tmp/dispatch/dispatch"
	}
	// Go reports a file that sources leaves out as a missing module or
	// package and suggests go get; the fix is to generate again.
	script := "go build -o " + output + " " + buildTarget + " 2>/tmp/build.log && exit 0; cat /tmp/build.log >&2; " +
		"grep -q -e 'no required module provides package' -e 'replacement directory' -e 'no matching files found' /tmp/build.log && " +
		"echo 'The build misses files outside the module directory: run dagger generate after changing go.mod replace directives, " +
		"the imports from a replace target or their embeds.' >&2; exit 1"
	fmt.Fprintf(b, "      .withExec([\"sh\", \"-c\", %s])\n", strconv.Quote(script))
	if declares {
		b.WriteString("      .directory(\"/tmp/dispatch\")\n")
		b.WriteString("      .withPlatform(platform: platform)\n")
		b.WriteString("      .file(\"dispatch\")\n")
	} else {
		b.WriteString("      .file(\"/tmp/dispatch\")\n")
	}
	b.WriteString("  }\n\n")
}

// writeDaggerGoDispatch writes the entrypoint's dispatch member as dagger/go's
// build of the module's process at buildTarget: the same per-package builds,
// cached the same way, as any dagger/go build, at the toolchain of the
// module's go.mod. dagger/go is the module's client "go". moduleSubpath is
// the module's directory under its Go module's.
func writeDaggerGoDispatch(b *bytes.Buffer, moduleSubpath, buildTarget string, declares bool) {
	if declares {
		b.WriteString("  let dispatch(platform: Platform!): File! {\n")
	} else {
		b.WriteString("  let dispatch: File! {\n")
	}
	b.WriteString("    let ws = currentModule.contextDirectory.asWorkspace\n")
	b.WriteString("    let src = currentModule.sourceSubpath\n")
	// dagger/go keys a package by its directory in the workspace.
	if moduleSubpath == "." {
		b.WriteString("    let root = src\n")
	} else {
		fmt.Fprintf(b, "    let root = if (src == %s) { \".\" } else { src.trimSuffix(%s) }\n",
			strconv.Quote(moduleSubpath), strconv.Quote("/"+moduleSubpath))
	}
	pkg := strings.TrimPrefix(buildTarget, "./")
	if pkg == "." {
		b.WriteString("    let pkg = src\n")
	} else {
		fmt.Fprintf(b, "    let pkg = if (src == \".\") { %s } else { src + %s }\n", strconv.Quote(pkg), strconv.Quote("/"+pkg))
	}
	if declares {
		// dagger/go compiles for any platform with the engine's toolchain,
		// and declares the binary as made for it.
		b.WriteString("    go.module(ws, path: root, findUp: false).packages(ws).get(pkg).platforms.get(`${platform}`.split(\"/\").takeFirst(2).join(\"/\")).binary(ws)\n")
	} else {
		b.WriteString("    go.module(ws, path: root, findUp: false).packages(ws).get(pkg).binary(ws)\n")
	}
	b.WriteString("  }\n\n")
}
