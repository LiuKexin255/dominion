package schema

import (
	"bytes"
	"embed"
	"fmt"
	"sync"

	"github.com/goccy/go-yaml"
	"github.com/santhosh-tekuri/jsonschema/v6"
)

const (
	deploySchemaFileName  = "deploy.schema.json"
	serviceSchemaFileName = "service.schema.json"
)

//go:embed deploy.schema.json
var deploySchemaFS embed.FS

//go:embed service.schema.json
var serviceSchemaFS embed.FS

var (
	loadDeploySchemaOnce sync.Once
	loadedDeploySchema   *jsonschema.Schema

	loadServiceSchemaOnce sync.Once
	loadedServiceSchema   *jsonschema.Schema
)

// ValidateDeployYAML validates deploy YAML configuration against the embedded schema.
// Returns an error if the YAML is invalid or doesn't match the schema.
func ValidateDeployYAML(raw []byte) error {
	return validateYAML(raw, loadDeploySchema())
}

// ValidateServiceYAML validates service YAML configuration against the embedded schema.
// Returns an error if the YAML is invalid or doesn't match the schema.
func ValidateServiceYAML(raw []byte) error {
	return validateYAML(raw, loadServiceSchema())
}

func loadDeploySchema() *jsonschema.Schema {
	loadDeploySchemaOnce.Do(func() {
		var err error
		loadedDeploySchema, err = compileEmbeddedSchema(deploySchemaFS, deploySchemaFileName)
		if err != nil {
			panic(err)
		}
	})

	return loadedDeploySchema
}

func loadServiceSchema() *jsonschema.Schema {
	loadServiceSchemaOnce.Do(func() {
		var err error
		loadedServiceSchema, err = compileEmbeddedSchema(serviceSchemaFS, serviceSchemaFileName)
		if err != nil {
			panic(err)
		}
	})

	return loadedServiceSchema
}

func compileEmbeddedSchema(schemaFS embed.FS, fileName string) (*jsonschema.Schema, error) {
	raw, err := schemaFS.ReadFile(fileName)
	if err != nil {
		return nil, fmt.Errorf("读取内置 schema %s 失败: %w", fileName, err)
	}

	schemaJSON, err := jsonschema.UnmarshalJSON(bytes.NewReader(raw))
	if err != nil {
		return nil, fmt.Errorf("解析内置 schema %s 失败: %w", fileName, err)
	}

	compiler := jsonschema.NewCompiler()
	resourcePath := "/" + fileName
	if err := compiler.AddResource(resourcePath, schemaJSON); err != nil {
		return nil, fmt.Errorf("注册内置 schema %s 失败: %w", fileName, err)
	}

	schema, err := compiler.Compile(resourcePath)
	if err != nil {
		return nil, fmt.Errorf("编译内置 schema %s 失败: %w", fileName, err)
	}

	return schema, nil
}

func validateYAML(raw []byte, schema *jsonschema.Schema) error {
	jsonRaw, err := yaml.YAMLToJSON(raw)
	if err != nil {
		return err
	}

	inst, err := jsonschema.UnmarshalJSON(bytes.NewReader(jsonRaw))
	if err != nil {
		return err
	}

	return schema.Validate(inst)
}
