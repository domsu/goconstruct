package main

import (
	"bytes"
	"flag"
	"fmt"
	"go/ast"
	"go/parser"
	"go/printer"
	"go/token"
	"io/ioutil"
	"log"
	"os"
	"path"
	"strings"
)

var (
	typeFlag = flag.String("type", "", "comma-separated list of type names; optional")
)

func main() {
	log.SetPrefix("goconstruct: ")
	flag.Usage = usage
	flag.Parse()
	if len(flag.Args()) == 0 {
		flag.Usage()
		os.Exit(2)
	}

	var typeFilter []string
	if len(*typeFlag) != 0 {
		typeFilter = strings.Split(*typeFlag, ",")
	}
	fileNames := getGoFileNamesInDirectory(flag.Args()[0])
	if len(fileNames) == 0 {
		log.Println("No files to process")
		return
	}

	for _, fileName := range fileNames {
		processFile(fileName, typeFilter)
	}
}

func usage() {
	fmt.Println("Usage of goconstruct")
	fmt.Println("goconstruct -type T directory")
	flag.PrintDefaults()
}

func getGoFileNamesInDirectory(path string) []string {
	var fileNames []string
	files, err := ioutil.ReadDir(path)
	if err != nil {
		log.Fatal(err)
	}

	for _, file := range files {
		if strings.HasSuffix(file.Name(), ".go") {
			fileNames = append(fileNames, file.Name())
		}
	}
	return fileNames
}

func processFile(fileName string, typeFilter []string) {
	fSet := token.NewFileSet()
	node, err := parser.ParseFile(fSet, fileName, nil, parser.ParseComments)
	if err != nil {
		log.Fatal(err)
	}

	structTypeSpecsToProcess := gerStrutSpecsToProcess(node, typeFilter)

	if len(structTypeSpecsToProcess) == 0 {
		return
	}

	constructors := generateConstructors(fSet, structTypeSpecsToProcess)
	imports := generateImports(node, structTypeSpecsToProcess)

	ext := path.Ext(fileName)
	outFile := fileName[0:len(fileName)-len(ext)] + "_gen.go"
	genFile, err := os.OpenFile(outFile, os.O_RDWR|os.O_CREATE, 0666)
	if err != nil {
		log.Fatal(err)
	}
	defer genFile.Close()

	if _, err = genFile.WriteString(fmt.Sprintf(header, node.Name.Name, strings.Join(imports, "\n"))); err != nil {
		log.Fatal(err)
	}
	for _, constructor := range constructors {
		if _, err = genFile.WriteString(constructor + "\n"); err != nil {
			log.Fatal(err)
		}
	}
}

func generateConstructors(fSet *token.FileSet, structTypeSpecs []*ast.TypeSpec) []string {
	var constructors []string
	for _, structTypeSpec := range structTypeSpecs {
		structName := structTypeSpec.Name.Name

		var exported = structName[0] == strings.ToUpper(structName)[0]
		var functionPrefix = "new"
		if exported {
			functionPrefix = "New"
		}

		var args []string
		var body []string
		body = append(body, fmt.Sprintf("\ts := %s{}", structName))

		structType := structTypeSpec.Type.(*ast.StructType)
		for _, field := range structType.Fields.List {
			var buf bytes.Buffer
			if err := printer.Fprint(&buf, fSet, field.Type); err != nil {
				log.Fatal(err)
			}
			for _, name := range field.Names {
				fieldArg := fmt.Sprintf("%s %s", name, buf.String())
				args = append(args, fieldArg)
				body = append(body, fmt.Sprintf("\ts.%s = %s", name, name))
			}
		}
		body = append(body, fmt.Sprintf("\treturn &s"))

		constructorStructName := strings.ToUpper(structName)[0:1] + structName[1:]
		constructor := fmt.Sprintf("func %s%s(%s) *%s {\n%s\n}\n", functionPrefix, constructorStructName, strings.Join(args, ","), structName, strings.Join(body, "\n"))
		constructors = append(constructors, constructor)
	}
	return constructors
}

func gerStrutSpecsToProcess(node *ast.File, typeFilter []string) []*ast.TypeSpec {
	structTypeSpecs := getStructTypeSpec(node)
	var structTypeSpecsToProcess []*ast.TypeSpec
	if len(typeFilter) != 0 {
		for _, structTypeSpec := range structTypeSpecs {
			for _, filter := range typeFilter {
				if structTypeSpec.Name.Name == filter {
					structTypeSpecsToProcess = append(structTypeSpecsToProcess, structTypeSpec)
					break
				}
			}
		}
	} else {
		structTypeSpecsToProcess = structTypeSpecs
	}
	return structTypeSpecsToProcess
}

func generateImports(node *ast.File, structTypeSpecs []*ast.TypeSpec) []string {
	var result []string
	allImports := getPackageNameToPathMap(node)
	usedPackages := getPackageNamesUsedInStructFields(node, structTypeSpecs)
	for packageValue, pathValue := range allImports {
		if packageValue == "." {
			result = append(result, fmt.Sprintf("\t. %s", pathValue))
		} else {
			if _, used := usedPackages[packageValue]; used {
				result = append(result, fmt.Sprintf("\t%s", pathValue))
			}
		}
	}
	return result
}

func getPackageNamesUsedInStructFields(node *ast.File, structTypeSpecs []*ast.TypeSpec) map[string]bool {
	result := make(map[string]bool)
	var inspectingStruct = false
	var depth = 0

	ast.Inspect(node, func(n ast.Node) bool {
		var shouldInspect = false
		if n == nil && inspectingStruct {
			depth--
			if depth == 0 {
				inspectingStruct = false
			}
		}

		if inspectingStruct {
			shouldInspect = true
		} else if genDecl, genDeclOk := n.(*ast.GenDecl); genDeclOk {
			if typeSpec, typeSpecOk := genDecl.Specs[0].(*ast.TypeSpec); typeSpecOk {
				for _, v := range structTypeSpecs {
					if typeSpec == v {
						shouldInspect = true
						inspectingStruct = true
						break
					}
				}
			}
		} else if _, fileOk := n.(*ast.File); fileOk {
			shouldInspect = true
		}

		if selExpr, selExprOk := n.(*ast.SelectorExpr); selExprOk {
			if ident, identOk := selExpr.X.(*ast.Ident); identOk {
				result[ident.Name] = true
			}
		}

		if n != nil && inspectingStruct {
			depth++
		}

		return shouldInspect
	})
	return result
}

func getPackageNameToPathMap(node *ast.File) (result map[string]string) {
	result = make(map[string]string)
	ast.Inspect(node, func(n ast.Node) bool {
		if impSpec, impSpecOk := n.(*ast.ImportSpec); impSpecOk {
			if impSpec.Name != nil {
				result[impSpec.Name.Name] = impSpec.Path.Value
			} else {
				pathSplit := strings.Split(impSpec.Path.Value[1:len(impSpec.Path.Value)-1], "/")
				packageName := pathSplit[len(pathSplit)-1]
				result[packageName] = impSpec.Path.Value
			}
			return false
		}
		return true
	})
	return result
}

func getStructTypeSpec(node *ast.File) (result []*ast.TypeSpec) {
	for _, decl := range node.Decls {
		if genDecl, genDeclOk := decl.(*ast.GenDecl); genDeclOk {
			if typeSpec, typeSpecOk := genDecl.Specs[0].(*ast.TypeSpec); typeSpecOk {
				if _, structTypeOk := typeSpec.Type.(*ast.StructType); structTypeOk {
					result = append(result, typeSpec)
				}
			}
		}
	}
	return result
}

const header = `// Code generated by goconstruct. DO NOT EDIT

package %s

import (
%s
)

`
