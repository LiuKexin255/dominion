// Package config provides the deploy-config SDK for dominion services.
//
// Deploy config lets a service declare named config blocks in service.yaml
// and read them at runtime by (block, key) addressing. The full usage:
//
// # 1. Declare config blocks in service.yaml
//
// The blocks form a shared definition pool for all artifacts; each block
// holds typed (json|yaml) data entries. Schema:
// specs/045-deploy-config/contracts/yaml-schema.md §1.
//
//	version: "3.0"
//	name: service
//	app: grpc-hello-world
//	kind: stateless
//	ports:
//	  - name: grpc
//	    port: 50051
//	configs:
//	  - name: service_config
//	    data:
//	      - name: greeting
//	        value: |
//	          message: "hello from config"
//	          times: 3
//	        type: yaml
//	      - name: limits
//	        value: '{"maxConn": 100}'
//	        type: json
//
// # 2. Select blocks per artifact in deploy.yaml
//
// Selection only names blocks and never overrides their data; unselected
// blocks are not provided to any artifact
// (specs/045-deploy-config/contracts/yaml-schema.md §2).
//
//	services:
//	  - artifact:
//	      path: //path/to/service.yaml
//	      name: service
//	      configs:
//	        - service_config
//
// # 3. Read at runtime
//
// The platform mounts the selected blocks at
// {DOMINION_CONFIG_DIR}/{block}/{key} and injects the DOMINION_CONFIG_DIR
// environment variable (specs/045-deploy-config/contracts/runtime-contract.md §1).
// Read discovers the root directory through that variable, so service code
// never hardcodes a config path:
//
//	type Greeting struct {
//	    Message string `yaml:"message"`
//	    Times   int    `yaml:"times"`
//	}
//	greeting, err := config.Read("service_config", "greeting",
//	    Greeting{Message: "hello", Times: 1})
//
// # 4. Deep merge over defaults
//
// Read deep-merges the config file over defaults before decoding into the
// result: objects merge recursively, arrays and scalars are replaced
// wholesale by the config value, keys absent from the config keep their
// defaults, and an explicit null overrides a default with the zero value
// (specs/045-deploy-config/data-model.md "Deep Merge Semantics").
//
// # 5. Non-sensitive data only
//
// Config carries non-sensitive data only: it is projected from a ConfigMap,
// never a Secret (specs/045-deploy-config/contracts/runtime-contract.md §5).
// Sensitive data must keep using the secret mechanism
// (specs/002-deploy-secret-config).
package config
