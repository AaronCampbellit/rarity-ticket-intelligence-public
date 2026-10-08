package main

import (
	"bytes"
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/printer"
	"go/token"
	"os"
	"reflect"
	"strconv"
	"strings"
)

type violation struct {
	Path    string `json:"path"`
	Line    int    `json:"line"`
	Message string `json:"message"`
}

type result struct {
	Violations []violation `json:"violations"`
	Contracts  []contract  `json:"contracts"`
}

type contract struct {
	Path      string `json:"path"`
	Type      string `json:"type"`
	Field     string `json:"field"`
	FieldType string `json:"field_type"`
	JSON      string `json:"json"`
}

func main() {
	result := result{Violations: []violation{}, Contracts: []contract{}}
	files := token.NewFileSet()
	for _, path := range os.Args[1:] {
		if path == "--" {
			continue
		}
		source, err := parser.ParseFile(files, path, nil, parser.AllErrors)
		if err != nil {
			result.Violations = append(result.Violations, violation{Path: path, Line: 1, Message: "Go contract cannot be parsed: " + err.Error()})
			continue
		}
		for _, declaration := range source.Decls {
			general, ok := declaration.(*ast.GenDecl)
			if !ok || general.Tok != token.TYPE {
				continue
			}
			for _, specification := range general.Specs {
				typeSpec, ok := specification.(*ast.TypeSpec)
				if !ok {
					continue
				}
				if structure, ok := typeSpec.Type.(*ast.StructType); ok {
					for _, field := range structure.Fields.List {
						jsonName := fieldJSONName(field)
						for _, name := range field.Names {
							result.Contracts = append(result.Contracts, contract{
								Path: path, Type: typeSpec.Name.Name, Field: name.Name,
								FieldType: expressionName(files, field.Type), JSON: jsonName,
							})
						}
					}
					continue
				}
				result.Contracts = append(result.Contracts, contract{
					Path: path, Type: typeSpec.Name.Name,
					FieldType: expressionName(files, typeSpec.Type),
				})
			}
		}
		ast.Inspect(source, func(node ast.Node) bool {
			switch value := node.(type) {
			case *ast.Field:
				jsonName := fieldJSONName(value)
				for _, name := range value.Names {
					if name.Name == "Category" && jsonName != "category" && jsonName != "forecast_category" && !isForecastCategory(value.Type) {
						result.Violations = append(result.Violations, violation{Path: path, Line: files.Position(name.Pos()).Line, Message: "generic Category field remains in a Go contract"})
					}
				}
				if jsonName == "category" {
					result.Violations = append(result.Violations, violation{Path: path, Line: files.Position(value.Pos()).Line, Message: "generic category JSON field remains in a Go contract"})
				}
			case *ast.BasicLit:
				if value.Kind == token.STRING {
					raw, err := strconv.Unquote(value.Value)
					if err == nil && hasCategoryPath(raw) {
						result.Violations = append(result.Violations, violation{Path: path, Line: files.Position(value.Pos()).Line, Message: "generic Category API path remains"})
					}
				}
			}
			return true
		})
	}
	_ = json.NewEncoder(os.Stdout).Encode(result)
}

func fieldJSONName(field *ast.Field) string {
	if field.Tag == nil {
		return ""
	}
	raw, err := strconv.Unquote(field.Tag.Value)
	if err != nil {
		return ""
	}
	return strings.Split(reflect.StructTag(raw).Get("json"), ",")[0]
}

func expressionName(files *token.FileSet, expression ast.Expr) string {
	var buffer bytes.Buffer
	if printer.Fprint(&buffer, files, expression) != nil {
		return ""
	}
	return buffer.String()
}

func isForecastCategory(expression ast.Expr) bool {
	switch value := expression.(type) {
	case *ast.Ident:
		return value.Name == "ForecastCategory"
	case *ast.SelectorExpr:
		return value.Sel.Name == "ForecastCategory"
	}
	return false
}

func hasCategoryPath(value string) bool {
	for _, part := range strings.FieldsFunc(value, func(r rune) bool { return r == '/' || r == '?' || r == '&' || r == '=' }) {
		if strings.EqualFold(part, "category") {
			return true
		}
	}
	return false
}
