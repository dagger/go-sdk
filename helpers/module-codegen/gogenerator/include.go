package gogenerator

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/scanner"
	"go/token"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/iancoleman/strcase"
	"golang.org/x/mod/modfile"
)

const includeDirective = "//go:mod:include"

// Includes is the //go:mod:include list of a module. An empty Include means
// the module has no directive.
type Includes struct {
	// MainFile declares the module's main object, relative to the module
	// directory. It is empty when no file declares it.
	MainFile string
	// Anchor is the directory that holds the module directory and every
	// included path, relative to the module directory, such as "..".
	Anchor string
	// ModulePath is the module directory relative to the anchor.
	ModulePath string
	Include    []IncludePattern
	// Exclude holds patterns relative to the anchor.
	Exclude []string
	// ModuleExclude holds patterns relative to the module directory.
	ModuleExclude []string
}

type IncludePattern struct {
	// Position is the directive's file:line:column, relative to the module
	// directory.
	Position string
	// Path is the argument as written, relative to the module directory.
	Path string
	// Pattern is relative to the anchor.
	Pattern string
}

type includeArg struct {
	position string
	text     string
}

// ReadIncludes reads the //go:mod:include directives of the module at root.
// workspacePath is the module directory relative to the workspace root.
// Without it, paths are bounded by the file system root instead.
func ReadIncludes(root, moduleName, workspacePath string) (*Includes, error) {
	root, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	typeName := strcase.ToCamel(moduleName)
	mainFile, err := findMainFile(root, typeName)
	if err != nil {
		return nil, err
	}
	header, err := scanIncludeDirectives(root, mainFile, typeName)
	if err != nil {
		return nil, err
	}
	includes := &Includes{MainFile: mainFile, Anchor: ".", ModulePath: "."}
	if len(header) == 0 {
		return includes, nil
	}

	location := filepath.ToSlash(root)
	depth := len(strings.Split(location, "/")) - 1
	if workspacePath != "" {
		location = path.Join("/", workspacePath)
		depth = 0
		if location != "/" {
			depth = len(strings.Split(strings.TrimPrefix(location, "/"), "/"))
		}
	}

	type outward struct {
		arg     includeArg
		cleaned string
		up      int
	}
	var includeArgs, excludeArgs []outward
	for _, arg := range header {
		negated := strings.HasPrefix(arg.text, "!")
		p := strings.TrimPrefix(arg.text, "!")
		if p == "" {
			return nil, fmt.Errorf("%s: %s path %q is empty", arg.position, includeDirective, arg.text)
		}
		if strings.HasPrefix(p, "/") {
			return nil, fmt.Errorf("%s: %s path %q is absolute; write it relative to the module directory", arg.position, includeDirective, arg.text)
		}
		cleaned := path.Clean(p)
		up := leadingParents(cleaned)
		if up > depth {
			return nil, fmt.Errorf("%s: %s path %q leaves the workspace root", arg.position, includeDirective, arg.text)
		}
		resolved := path.Join(location, cleaned)
		if resolved == location || strings.HasPrefix(resolved, strings.TrimSuffix(location, "/")+"/") {
			if !negated {
				return nil, fmt.Errorf("%s: %s path %q is inside the module directory; only %q is allowed there", arg.position, includeDirective, arg.text, "!"+p)
			}
			inside := strings.TrimPrefix(strings.TrimPrefix(resolved, strings.TrimSuffix(location, "/")), "/")
			if inside == "" {
				return nil, fmt.Errorf("%s: %s path %q excludes the whole module directory", arg.position, includeDirective, arg.text)
			}
			includes.ModuleExclude = appendUnique(includes.ModuleExclude, inside)
			continue
		}
		if negated {
			excludeArgs = append(excludeArgs, outward{arg, cleaned, up})
		} else {
			includeArgs = append(includeArgs, outward{arg, cleaned, up})
		}
	}
	if len(includeArgs) == 0 {
		return nil, fmt.Errorf("%s: %s names no path outside the module directory", header[0].position, includeDirective)
	}

	anchorUp := 0
	for _, arg := range includeArgs {
		anchorUp = max(anchorUp, arg.up)
	}
	parts := strings.Split(strings.Trim(location, "/"), "/")
	includes.ModulePath = strings.Join(parts[len(parts)-anchorUp:], "/")
	includes.Anchor = strings.TrimSuffix(strings.Repeat("../", anchorUp), "/")
	anchored := func(cleaned string) string {
		pattern := path.Join(includes.ModulePath, cleaned)
		if pattern == "." {
			return "**"
		}
		return pattern
	}
	for _, arg := range includeArgs {
		pattern := anchored(arg.cleaned)
		if slices.ContainsFunc(includes.Include, func(existing IncludePattern) bool { return existing.Pattern == pattern }) {
			continue
		}
		includes.Include = append(includes.Include, IncludePattern{
			Position: arg.arg.position,
			Path:     arg.arg.text,
			Pattern:  pattern,
		})
	}
	for _, arg := range excludeArgs {
		// An exclude above the anchor cannot match anything the module reads.
		if arg.up <= anchorUp {
			includes.Exclude = appendUnique(includes.Exclude, anchored(arg.cleaned))
		}
	}
	return includes, nil
}

func leadingParents(cleaned string) int {
	up := 0
	for cleaned == ".." || strings.HasPrefix(cleaned, "../") {
		up++
		cleaned = strings.TrimPrefix(strings.TrimPrefix(cleaned, ".."), "/")
	}
	return up
}

func appendUnique(values []string, value string) []string {
	if slices.Contains(values, value) {
		return values
	}
	return append(values, value)
}

// findMainFile returns the non-test Go file of the module directory that
// declares the main object, or an empty name when none does.
func findMainFile(root, typeName string) (string, error) {
	entries, err := os.ReadDir(root)
	if err != nil {
		return "", err
	}
	fset := token.NewFileSet()
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") || name == "dagger.gen.go" {
			continue
		}
		// The build reports syntax errors with full context; a partial file
		// still tells whether it declares the main object.
		file, _ := parser.ParseFile(fset, filepath.Join(root, name), nil, parser.SkipObjectResolution)
		if file == nil {
			continue
		}
		for _, decl := range file.Decls {
			gen, ok := decl.(*ast.GenDecl)
			if !ok || gen.Tok != token.TYPE {
				continue
			}
			for _, spec := range gen.Specs {
				if strcase.ToCamel(spec.(*ast.TypeSpec).Name.Name) == typeName {
					return name, nil
				}
			}
		}
	}
	return "", nil
}

// scanIncludeDirectives returns the arguments of the directives in the
// header of mainFile, in file order, and refuses a directive anywhere else.
func scanIncludeDirectives(root, mainFile, typeName string) ([]includeArg, error) {
	var header []includeArg
	err := filepath.WalkDir(root, func(file string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, file)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if entry.IsDir() {
			if rel == "." {
				return nil
			}
			// vendor and a directory with its own go.mod hold other modules'
			// packages, so their directives are not this module's.
			if entry.Name() == "vendor" {
				return fs.SkipDir
			}
			if _, err := os.Stat(filepath.Join(file, "go.mod")); err == nil {
				return fs.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(rel, ".go") {
			return nil
		}
		src, err := os.ReadFile(file)
		if err != nil {
			return err
		}
		directives, packagePos := scanFileDirectives(rel, src)
		for _, directive := range directives {
			if rel != mainFile || directive.offset > packagePos {
				where := "the file that declares type " + typeName
				if mainFile != "" {
					where = mainFile + ", " + where
				}
				return fmt.Errorf("%s: %s belongs in the header of %s", directive.position, includeDirective, where)
			}
			args, err := splitIncludeArgs(directive.text)
			if err != nil {
				return fmt.Errorf("%s: %w", directive.position, err)
			}
			if len(args) == 0 {
				return fmt.Errorf("%s: %s needs at least one path", directive.position, includeDirective)
			}
			for _, arg := range args {
				header = append(header, includeArg{position: directive.position, text: arg})
			}
		}
		return nil
	})
	return header, err
}

type directiveComment struct {
	position string
	offset   int
	text     string
}

// scanFileDirectives returns the //go:mod:include line comments of a Go file
// and the offset of its package clause. It tokenizes instead of parsing, so a
// file that does not compile is still checked.
func scanFileDirectives(rel string, src []byte) ([]directiveComment, int) {
	fset := token.NewFileSet()
	file := fset.AddFile(rel, -1, len(src))
	var s scanner.Scanner
	s.Init(file, src, nil, scanner.ScanComments)
	packagePos := len(src)
	var directives []directiveComment
	for {
		pos, tok, lit := s.Scan()
		if tok == token.EOF {
			break
		}
		if tok == token.PACKAGE && packagePos == len(src) {
			packagePos = file.Offset(pos)
		}
		if tok != token.COMMENT || !strings.HasPrefix(lit, includeDirective) {
			continue
		}
		rest := strings.TrimPrefix(lit, includeDirective)
		if rest != "" && rest[0] != ' ' && rest[0] != '\t' {
			continue
		}
		position := fset.Position(pos)
		directives = append(directives, directiveComment{
			position: fmt.Sprintf("%s:%d:%d", rel, position.Line, position.Column),
			offset:   file.Offset(pos),
			text:     rest,
		})
	}
	return directives, packagePos
}

// splitIncludeArgs splits directive arguments at white space. An argument
// can be a Go interpreted or raw string, as in //go:embed.
func splitIncludeArgs(text string) ([]string, error) {
	var args []string
	for {
		text = strings.TrimLeft(text, " \t")
		if text == "" {
			return args, nil
		}
		switch text[0] {
		case '"':
			end := 1
			for end < len(text) && text[end] != '"' {
				if text[end] == '\\' {
					end++
				}
				end++
			}
			if end >= len(text) {
				return nil, fmt.Errorf("invalid quoted string in %s: %s", includeDirective, text)
			}
			arg, err := strconv.Unquote(text[:end+1])
			if err != nil {
				return nil, fmt.Errorf("invalid quoted string in %s: %s", includeDirective, text[:end+1])
			}
			args = append(args, arg)
			text = text[end+1:]
		case '`':
			end := strings.IndexByte(text[1:], '`')
			if end < 0 {
				return nil, fmt.Errorf("invalid quoted string in %s: %s", includeDirective, text)
			}
			args = append(args, text[1:end+1])
			text = text[end+2:]
		default:
			end := strings.IndexAny(text, " \t")
			if end < 0 {
				end = len(text)
			}
			args = append(args, text[:end])
			text = text[end:]
		}
	}
}

type localReplace struct {
	module string
	text   string
	dir    string
	line   int
	inside bool
}

// checkIncludeCoverage refuses a module whose build reads a local replace
// target outside the module directory without the files of that target the
// build needs: its go.mod, and the directory of every package imported from
// it. Imports count under every build tag and test files of the module count,
// as go mod tidy loads them.
func checkIncludeCoverage(root, mainFile, typeName string) error {
	goModPath := filepath.Join(root, "go.mod")
	data, err := os.ReadFile(goModPath)
	if err != nil {
		return err
	}
	mod, err := modfile.Parse(goModPath, data, nil)
	if err != nil {
		return err
	}
	var replaces []localReplace
	outside := false
	for _, replace := range mod.Replace {
		if replace.New.Version != "" || !modfile.IsDirectoryPath(replace.New.Path) {
			continue
		}
		dir := replace.New.Path
		if !filepath.IsAbs(dir) {
			dir = filepath.Join(root, filepath.FromSlash(dir))
		}
		rel, err := filepath.Rel(root, dir)
		inside := err == nil && filepath.IsLocal(rel)
		outside = outside || !inside
		replaces = append(replaces, localReplace{
			module: replace.Old.Path,
			text:   strings.TrimSpace(replace.Old.Path + " " + replace.Old.Version),
			dir:    dir,
			line:   replace.Syntax.Start.Line,
			inside: inside,
		})
	}
	if !outside {
		return nil
	}
	home := "the file that declares type " + typeName
	if mainFile != "" {
		home = mainFile
	}

	// A required module's go.mod is read even when no package is imported
	// from it, and a replace target's own requirements are required too.
	required := map[string]bool{}
	for _, req := range mod.Require {
		required[req.Mod.Path] = true
	}
	for changed := true; changed; {
		changed = false
		for _, replace := range replaces {
			if !required[replace.module] {
				continue
			}
			data, err := os.ReadFile(filepath.Join(replace.dir, "go.mod"))
			if err != nil {
				continue
			}
			target, err := modfile.ParseLax("go.mod", data, nil)
			if err != nil {
				continue
			}
			for _, req := range target.Require {
				if !required[req.Mod.Path] {
					required[req.Mod.Path] = true
					changed = true
				}
			}
		}
	}

	checkGoMod := func(replace localReplace) error {
		if _, err := os.Stat(filepath.Join(replace.dir, "go.mod")); err == nil {
			return nil
		}
		target := path.Join(filepath.ToSlash(replaceText(root, replace.dir)), "go.mod")
		return fmt.Errorf("go.mod:%d: replace %s => %s reads %s; add it to %s in %s",
			replace.line, replace.text, replaceText(root, replace.dir), target, includeDirective, home)
	}
	for _, replace := range replaces {
		if !replace.inside && required[replace.module] {
			if err := checkGoMod(replace); err != nil {
				return err
			}
		}
	}

	type pkg struct {
		dir   string
		tests bool
	}
	var queue []pkg
	err = filepath.WalkDir(root, func(file string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !entry.IsDir() {
			return nil
		}
		if file != root && skipPackageDir(file, entry.Name()) {
			return fs.SkipDir
		}
		queue = append(queue, pkg{dir: file, tests: true})
		return nil
	})
	if err != nil {
		return err
	}
	visited := map[string]bool{}
	for len(queue) > 0 {
		next := queue[0]
		queue = queue[1:]
		if visited[next.dir] {
			continue
		}
		visited[next.dir] = true
		imports, err := packageImports(root, next.dir, next.tests)
		if err != nil {
			return err
		}
		for _, imported := range imports {
			var match *localReplace
			for i, replace := range replaces {
				if (imported.path == replace.module || strings.HasPrefix(imported.path, replace.module+"/")) &&
					(match == nil || len(replace.module) > len(match.module)) {
					match = &replaces[i]
				}
			}
			if match == nil {
				continue
			}
			if !match.inside {
				if err := checkGoMod(*match); err != nil {
					return err
				}
			}
			dir := filepath.Join(match.dir, filepath.FromSlash(strings.TrimPrefix(imported.path, match.module)))
			if !match.inside && !hasGoFiles(dir) {
				return fmt.Errorf("%s: package %s is in %s (go.mod:%d); add %s to %s in %s",
					imported.position, imported.path, replaceText(root, match.dir), match.line,
					replaceText(root, dir), includeDirective, home)
			}
			queue = append(queue, pkg{dir: dir})
		}
	}
	return nil
}

// replaceText is a directory as a go.mod or //go:mod:include line names it.
func replaceText(root, dir string) string {
	rel, err := filepath.Rel(root, dir)
	if err != nil {
		return filepath.ToSlash(dir)
	}
	rel = filepath.ToSlash(rel)
	if !strings.HasPrefix(rel, "../") && rel != ".." {
		rel = "./" + rel
	}
	return rel
}

// skipPackageDir reports a directory that Go builds nothing from, or that
// holds another module.
func skipPackageDir(dir, name string) bool {
	if name == "testdata" || name == "vendor" || strings.HasPrefix(name, ".") || strings.HasPrefix(name, "_") {
		return true
	}
	_, err := os.Stat(filepath.Join(dir, "go.mod"))
	return err == nil
}

func hasGoFiles(dir string) bool {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false
	}
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".go") && !strings.HasSuffix(entry.Name(), "_test.go") {
			return true
		}
	}
	return false
}

type packageImport struct {
	path     string
	position string
}

// packageImports lists the imports of every Go file of a package directory,
// whatever its build constraints, in file order.
func packageImports(root, dir string, tests bool) ([]packageImport, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	fset := token.NewFileSet()
	var imports []packageImport
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || (!tests && strings.HasSuffix(name, "_test.go")) {
			continue
		}
		file, err := parser.ParseFile(fset, filepath.Join(dir, name), nil, parser.ImportsOnly)
		if err != nil {
			return nil, err
		}
		for _, spec := range file.Imports {
			value, err := strconv.Unquote(spec.Path.Value)
			if err != nil {
				continue
			}
			position := fset.Position(spec.Pos())
			rel, err := filepath.Rel(root, position.Filename)
			if err != nil {
				rel = position.Filename
			}
			imports = append(imports, packageImport{
				path:     value,
				position: fmt.Sprintf("%s:%d:%d", filepath.ToSlash(rel), position.Line, position.Column),
			})
		}
	}
	return imports, nil
}
