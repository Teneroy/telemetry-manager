package main

import (
	"flag"
	"fmt"
	"os"
	"regexp"
	"sort"
	"strings"

	"sigs.k8s.io/yaml"
)

// TODO: consider move to param?
//const APIVersion = "v1alpha1"

// // TODO: use relative path or from param
// const CRDFilename = `/Users/I572465/telemetry-manager/config/crd/bases/telemetry.kyma-project.io_logpipelines.yaml`

// // TODO: use relative path or from param
// const MDFilename = `/Users/I572465/Go/src/github.com/kyma-project/kyma/docs/01-overview/main-areas/telemetry/telemetry-02-logs.md`

const FunctionSpecIdentifier = `FUNCTION-SPEC`
const REFunctionSpecPattern = `(?s)<!--\s*` + FunctionSpecIdentifier + `-START\s* -->.*<!--\s*` + FunctionSpecIdentifier + `-END\s*-->`

const KeepThisIdentifier = `KEEP-THIS`
const REKeepThisPattern = `[^\S\r\n]*[|]\s*\*{2}([^*]+)\*{2}.*<!--\s*` + KeepThisIdentifier + `\s*-->`

const SkipIdentifier = `SKIP-ELEMENT`
const RESkipPattern = `<!--\s*` + SkipIdentifier + `\s*([^\s]+)\s*-->`
const SkipWithAncestorsIdentifier = `SKIP-WITH-ANCESTORS`
const RESkipWithAncestorsPattern = `<!--\s*` + SkipWithAncestorsIdentifier + `\s*([^\s-]+)\s*-->`

type FunctionSpecGenerator struct {
	elementsToKeep map[string]string
	elementsToSkip map[string]bool
}

var (
	CRDFilename string
	MDFilename  string
	APIVersion  string
)

func main() {
	// TODO add description, think about default value
	flag.StringVar(&CRDFilename, "crd-filename", "", "")
	// TODO add description, think about default value
	flag.StringVar(&MDFilename, "md-filename", "", "")
	// TODO add description, think about default value
	flag.StringVar(&APIVersion, "api-version", "v1alpha1", "")

	flag.Parse()

	println(MDFilename)
	println(CRDFilename)

	println("Start script")
	toKeep := getElementsToKeep()
	println("elements to keep: ", len(toKeep))
	toSkip := getElementsToSkip()
	println("elements to skip: ", len(toSkip))
	gen := CreateFunctionSpecGenerator(toKeep, toSkip)
	println("Function is created")
	doc := gen.generateDocFromCRD()
	println("Doc is done:")
	println(doc)
	replaceDocInMD(doc)
}

func getElementsToKeep() map[string]string {
	inDoc, err := os.ReadFile(MDFilename)
	if err != nil {
		panic(err)
	}

	reFunSpec := regexp.MustCompile(REFunctionSpecPattern)
	funSpecPart := reFunSpec.FindString(string(inDoc))
	reKeep := regexp.MustCompile(REKeepThisPattern)
	rowsToKeep := reKeep.FindAllStringSubmatch(funSpecPart, -1)

	toKeep := map[string]string{}
	for _, pair := range rowsToKeep {
		rowContent := pair[0]
		paramName := pair[1]
		toKeep[paramName] = rowContent
	}
	return toKeep
}

func getElementsToSkip() map[string]bool {
	inDoc, err := os.ReadFile(MDFilename)
	if err != nil {
		panic(err)
	}

	doc := string(inDoc)
	reSkip := regexp.MustCompile(RESkipPattern)
	toSkip := map[string]bool{}
	for _, pair := range reSkip.FindAllStringSubmatch(doc, -1) {
		paramName := pair[1]
		toSkip[paramName] = false
	}

	reSkipWithAncestors := regexp.MustCompile(RESkipWithAncestorsPattern)
	for _, pair := range reSkipWithAncestors.FindAllStringSubmatch(doc, -1) {
		paramName := pair[1]
		toSkip[paramName] = true
	}

	return toSkip
}

func replaceDocInMD(doc string) {
	inDoc, err := os.ReadFile(MDFilename)
	if err != nil {
		panic(err)
	}

	newContent := strings.Join([]string{
		"<!-- " + FunctionSpecIdentifier + "-START -->",
		doc + "<!-- " + FunctionSpecIdentifier + "-END -->",
	}, "\n")
	re := regexp.MustCompile(REFunctionSpecPattern)
	outDoc := re.ReplaceAll(inDoc, []byte(newContent))

	outFile, err := os.OpenFile(MDFilename, os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0755)
	if err != nil {
		panic(err)
	}
	defer outFile.Close()
	outFile.Write(outDoc)
}

func CreateFunctionSpecGenerator(toKeep map[string]string, toSkip map[string]bool) FunctionSpecGenerator {
	return FunctionSpecGenerator{
		elementsToKeep: toKeep,
		elementsToSkip: toSkip,
	}
}

func (g *FunctionSpecGenerator) generateDocFromCRD() string {
	input, err := os.ReadFile(CRDFilename)
	if err != nil {
		panic(err)
	}

	// why unmarshalling to CustomResource don't work?
	var obj interface{}
	if err := yaml.Unmarshal(input, &obj); err != nil {
		panic(err)
	}

	docElements := map[string]string{}
	versions := getElement(obj, "spec", "versions")

	println("Started for")
	for _, version := range versions.([]interface{}) {
		name := getElement(version, "name")
		println("name: ", name.(string))
		println("APIVersion: ", APIVersion)
		if name.(string) != APIVersion {
			continue
		}
		functionSpec := getElement(version, "schema", "openAPIV3Schema", "properties", "spec")
		for k, v := range g.generateElementDoc(functionSpec, "spec", true, "") {
			docElements[k] = v
		}
	}
	println("Ended for")

	for k, v := range g.elementsToKeep {
		docElements[k] = v
	}
	println("DocElementsLen: ", len(docElements))

	var doc []string
	for _, propName := range sortedKeys(docElements) {
		doc = append(doc, docElements[propName])
	}
	println("Generated doc: ", doc)
	return strings.Join(doc, "\n")
}

func (g *FunctionSpecGenerator) generateElementDoc(obj interface{}, name string, required bool, parentPath string) map[string]string {
	result := map[string]string{}
	element := obj.(map[string]interface{})
	elementType := element["type"].(string)
	description := ""
	if d := element["description"]; d != nil {
		description = d.(string)
	}

	fullName := fmt.Sprintf("%s%s", parentPath, name)
	skipWithAncestors, shouldBeSkipped := g.elementsToSkip[fullName]
	if shouldBeSkipped && skipWithAncestors {
		return result
	}
	_, isRowToKeep := g.elementsToKeep[fullName]
	if !shouldBeSkipped && !isRowToKeep {
		result[fullName] =
			fmt.Sprintf("| **%s** | %s | %s |",
				fullName, yesNo(required), normalizeDescription(description, name))
	}

	if elementType == "object" {
		for k, v := range g.generateObjectDoc(element, name, parentPath) {
			result[k] = v
		}
	}
	return result
}

func (g *FunctionSpecGenerator) generateObjectDoc(element map[string]interface{}, name string, parentPath string) map[string]string {
	result := map[string]string{}
	properties := getElement(element, "properties")
	if properties == nil {
		return result
	}

	var requiredChildren []interface{}
	if rc := getElement(element, "required"); rc != nil {
		requiredChildren = rc.([]interface{})
	}

	propMap := properties.(map[string]interface{})
	for _, propName := range sortedKeys(propMap) {
		propRequired := contains(requiredChildren, name)
		for k, v := range g.generateElementDoc(propMap[propName], propName, propRequired, parentPath+name+".") {
			result[k] = v
		}
	}
	return result
}

func getElement(obj interface{}, path ...string) interface{} {
	elem := obj
	for _, p := range path {
		elem = elem.(map[string]interface{})[p]
	}
	return elem
}

func normalizeDescription(description string, name string) any {
	d := strings.Trim(description, " ")
	n := strings.Trim(name, " ")
	if len(n) == 0 {
		return d
	}
	dParts := strings.SplitN(d, " ", 2)
	if len(dParts) < 2 {
		return description
	}
	if !strings.EqualFold(n, dParts[0]) {
		return description
	}
	d = strings.Trim(dParts[1], " ")
	d = strings.ToUpper(d[:1]) + d[1:]
	return d
}

func sortedKeys[T any](propMap map[string]T) []string {
	var keys []string
	for key := range propMap {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func yesNo(b bool) string {
	if b {
		return "Yes"
	}
	return "No"
}

func contains(s []interface{}, e string) bool {
	for _, a := range s {
		if a.(string) == e {
			return true
		}
	}
	return false
}
