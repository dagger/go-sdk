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
