package gogenerator

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/iancoleman/strcase"

	"codegen/introspection"
)

// mergeSchema is the userland form of core.Schema.Merge. It is kept as a
// direct port so module self-call bindings use the same schema shape.
func mergeSchema(base *introspection.Response, moduleJSON []byte, moduleName string) (*introspection.Response, error) {
	if moduleName == "" {
		return nil, fmt.Errorf("module name is required")
	}
	var module introspection.Response
	if err := json.Unmarshal(moduleJSON, &module); err != nil {
		return nil, fmt.Errorf("parse module types: %w", err)
	}
	if module.Schema == nil {
		return nil, fmt.Errorf("module types have no __schema")
	}
	merged, err := cloneResponse(base)
	if err != nil {
		return nil, err
	}
	target := merged.Schema
	if moduleAlreadyMerged(target, moduleName) {
		return merged, nil
	}
	for _, typ := range module.Schema.Types {
		if !isModuleDefinedType(typ) {
			continue
		}
		if target.Types.Get(typ.Name) != nil {
			return nil, fmt.Errorf("type %q already exists in schema", typ.Name)
		}
		typ.Directives = append(typ.Directives, sourceMapDirective(moduleName))
		target.Types = append(target.Types, typ)
	}
	if err := mergeQueryConstructor(target, module.Schema, moduleName); err != nil {
		return nil, err
	}
	return merged, nil
}

func isModuleDefinedType(typ *introspection.Type) bool {
	switch typ.Kind {
	case introspection.TypeKindObject, introspection.TypeKindInterface, introspection.TypeKindEnum, introspection.TypeKindScalar:
	default:
		return false
	}
	if strings.HasPrefix(typ.Name, "__") {
		return false
	}
	switch typ.Name {
	case "Query", "Mutation", "Subscription":
		return false
	}
	return true
}

func mergeQueryConstructor(target, module *introspection.Schema, moduleName string) error {
	query := target.Query()
	if query == nil {
		return fmt.Errorf("schema has no Query type")
	}
	fieldName := strcase.ToLowerCamel(moduleName)
	if findField(query, fieldName) != nil {
		return nil
	}
	if moduleQuery := module.Query(); moduleQuery != nil {
		if field := findField(moduleQuery, fieldName); field != nil {
			field.Directives = append(field.Directives, sourceMapDirective(moduleName))
			query.Fields = append(query.Fields, field)
			return nil
		}
	}
	mainObject := target.Types.Get(strcase.ToCamel(moduleName))
	if mainObject == nil {
		return nil
	}
	query.Fields = append(query.Fields, &introspection.Field{
		Name:        fieldName,
		Description: mainObject.Description,
		TypeRef:     &introspection.TypeRef{Kind: introspection.TypeKindNonNull, OfType: &introspection.TypeRef{Kind: introspection.TypeKindObject, Name: mainObject.Name}},
		Args:        introspection.InputValues{},
		Directives:  introspection.Directives{sourceMapDirective(moduleName)},
	})
	return nil
}

func moduleAlreadyMerged(schema *introspection.Schema, moduleName string) bool {
	for _, typ := range schema.Types {
		if source := typ.Directives.SourceMap(); source != nil && source.Module == moduleName {
			return true
		}
	}
	if query := schema.Query(); query != nil {
		for _, field := range query.Fields {
			if source := field.Directives.SourceMap(); source != nil && source.Module == moduleName {
				return true
			}
		}
	}
	return false
}

func sourceMapDirective(moduleName string) *introspection.Directive {
	value, _ := json.Marshal(moduleName)
	encoded := string(value)
	return &introspection.Directive{
		Name: "sourceMap",
		Args: []*introspection.DirectiveArg{{Name: "module", Value: &encoded}},
	}
}

func findField(typ *introspection.Type, name string) *introspection.Field {
	for _, field := range typ.Fields {
		if field.Name == name {
			return field
		}
	}
	return nil
}

func cloneResponse(response *introspection.Response) (*introspection.Response, error) {
	data, err := json.Marshal(response)
	if err != nil {
		return nil, fmt.Errorf("clone schema: %w", err)
	}
	var cloned introspection.Response
	if err := json.Unmarshal(data, &cloned); err != nil {
		return nil, fmt.Errorf("clone schema: %w", err)
	}
	return &cloned, nil
}
