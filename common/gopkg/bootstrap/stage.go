package bootstrap

import "fmt"

// Stage defines the lifecycle stage of a component.
type Stage int

const (
	// StageFoundation represents the foundational stage (logging, config, metrics).
	StageFoundation Stage = 100
	// StageClient represents the client connection stage (database, cache, etc.).
	StageClient Stage = 200
	// StageServer represents the server serving stage (HTTP, gRPC, etc.).
	StageServer Stage = 300
)

// String returns the human-readable name of the Stage.
func (s Stage) String() string {
	switch s {
	case StageFoundation:
		return "Foundation"
	case StageClient:
		return "Client"
	case StageServer:
		return "Server"
	default:
		return fmt.Sprintf("Stage(%d)", s)
	}
}
