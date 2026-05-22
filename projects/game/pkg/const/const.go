// Package gameconst provides shared constants for game services.
package gameconst

// Service names.
const (
	AppGame        = "game"
	ServiceGateway = "gateway"
	ServiceRuntime = "runtime"
	ServiceSession = "session"
	ServiceMongo   = "mongo"
)

// Deploy/solver target constants.
const (
	TargetRuntimeHTTP = "game/runtime:http"
	TargetRuntimeGRPC = "game/runtime:grpc"
	TargetSessionGRPC = "game/session:grpc"
	TargetMongo       = "game/mongo"
)

// Observability field constants for logs and traces.
const (
	ObsFieldSessionID             = "session_id"
	ObsFieldOwnerRuntimeID        = "owner_runtime_id"
	ObsFieldOwnerGeneration       = "owner_generation"
	ObsFieldReconnectGeneration   = "reconnect_generation"
	ObsFieldGatewayInstance       = "gateway_instance"
	ObsFieldRuntimeInstance       = "runtime_instance"
	ObsFieldWSConnectionID        = "ws_connection_id"
	ObsFieldRouteResolutionSource = "route_resolution_source"
	ObsFieldRouteTarget           = "route_target"
	ObsFieldTokenParseResult      = "token_parse_result"
	ObsFieldErrorClass            = "error_class"
)
