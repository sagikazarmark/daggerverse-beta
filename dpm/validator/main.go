package main

import (
	"context"
	"fmt"
	"os"

	"github.com/vito/dang/v2/pkg/dang"
	"github.com/vito/dang/v2/pkg/hm"
	"github.com/vito/dang/v2/pkg/introspection"
)

func main() {
	if len(os.Args) != 4 {
		fmt.Fprintf(os.Stderr, "usage: dpm-validator PACKAGE_FILE PACKAGE_NAME TYPE_NAME\n")
		os.Exit(2)
	}

	if err := validate(os.Args[1], os.Args[2], os.Args[3]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func validate(filename, pkgName, typeName string) error {
	src, err := os.ReadFile(filename)
	if err != nil {
		return fmt.Errorf("read package %s: %w", pkgName, err)
	}

	parsed, err := dang.ParseWithRecovery(filename, src)
	if err != nil {
		return fmt.Errorf("parse package %s: %w", pkgName, err)
	}

	file, ok := parsed.(*dang.FileBlock)
	if !ok {
		return fmt.Errorf("package %s must parse as *dang.FileBlock, got %T", pkgName, parsed)
	}

	if len(file.Forms) != 1 {
		return fmt.Errorf("package %s must contain exactly one top-level declaration", pkgName)
	}

	pkgType, ok := file.Forms[0].(*dang.ObjectDecl)
	if !ok {
		return fmt.Errorf("package %s must contain one top-level type", pkgName)
	}

	if pkgType.Name == nil || pkgType.Name.Name != typeName {
		return fmt.Errorf("package %s must define type %s", pkgName, typeName)
	}

	if err := validatePackageShape(pkgName, pkgType); err != nil {
		return err
	}

	scope, _ := dang.BuildScopesFromImports("", []dang.ImportConfig{{
		Name:   "Dagger",
		Schema: daggerStubSchema(),
	}})
	fresh := hm.NewSimpleFresher()
	if _, err := dang.InferFormsWithPhases(context.Background(), file.Forms, scope, fresh); err != nil {
		return fmt.Errorf("typecheck package %s: %w", pkgName, err)
	}

	return nil
}

func validatePackageShape(pkgName string, pkgType *dang.ObjectDecl) error {
	if pkgType.Value == nil {
		return fmt.Errorf("package %s type body is missing", pkgName)
	}

	return nil
}

func daggerStubSchema() *introspection.Schema {
	schema := &introspection.Schema{
		Types: introspection.Types{
			scalar("ID"),
			scalar("String"),
			{
				Kind: introspection.TypeKindObject,
				Name: "Query",
				Fields: []*introspection.Field{{
					Name:    "container",
					TypeRef: nonNull(objectRef("Container")),
				}},
			},
			{
				Kind: introspection.TypeKindObject,
				Name: "Container",
				Fields: []*introspection.Field{
					{
						Name:    "from",
						TypeRef: nonNull(objectRef("Container")),
						Args: introspection.InputValues{{
							Name:    "address",
							TypeRef: nonNull(scalarRef("String")),
						}},
					},
					{
						Name:    "withExec",
						TypeRef: nonNull(objectRef("Container")),
						Args: introspection.InputValues{
							{
								Name:    "args",
								TypeRef: nonNull(listRef(nonNull(scalarRef("String")))),
							},
							{
								Name:    "stdin",
								TypeRef: scalarRef("String"),
							},
						},
					},
					{
						Name:    "stdout",
						TypeRef: nonNull(scalarRef("String")),
					},
				},
			},
		},
	}
	schema.QueryType.Name = "Query"

	return schema
}

func scalar(name string) *introspection.Type {
	return &introspection.Type{Kind: introspection.TypeKindScalar, Name: name}
}

func scalarRef(name string) *introspection.TypeRef {
	return &introspection.TypeRef{Kind: introspection.TypeKindScalar, Name: name}
}

func objectRef(name string) *introspection.TypeRef {
	return &introspection.TypeRef{Kind: introspection.TypeKindObject, Name: name}
}

func listRef(ofType *introspection.TypeRef) *introspection.TypeRef {
	return &introspection.TypeRef{Kind: introspection.TypeKindList, OfType: ofType}
}

func nonNull(ofType *introspection.TypeRef) *introspection.TypeRef {
	return &introspection.TypeRef{Kind: introspection.TypeKindNonNull, OfType: ofType}
}
