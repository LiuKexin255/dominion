package gameconst

import "testing"

func TestTargetConstants(t *testing.T) {
	tests := []struct {
		name string
		got  string
		want string
	}{
		{name: "TargetRuntimeHTTP", got: TargetRuntimeHTTP, want: "game/runtime:http"},
		{name: "TargetRuntimeGRPC", got: TargetRuntimeGRPC, want: "game/runtime:grpc"},
		{name: "TargetSessionGRPC", got: TargetSessionGRPC, want: "game/session:grpc"},
		{name: "TargetMongo", got: TargetMongo, want: "game/mongo"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.got != tt.want {
				t.Fatalf("%s = %q, want %q", tt.name, tt.got, tt.want)
			}
		})
	}
}

func TestServiceNameConstants(t *testing.T) {
	tests := []struct {
		name string
		got  string
		want string
	}{
		{name: "AppGame", got: AppGame, want: "game"},
		{name: "ServiceGateway", got: ServiceGateway, want: "gateway"},
		{name: "ServiceRuntime", got: ServiceRuntime, want: "runtime"},
		{name: "ServiceSession", got: ServiceSession, want: "session"},
		{name: "ServiceMongo", got: ServiceMongo, want: "mongo"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.got != tt.want {
				t.Fatalf("%s = %q, want %q", tt.name, tt.got, tt.want)
			}
		})
	}
}

func TestObsFieldConstants(t *testing.T) {
	tests := []struct {
		name string
		got  string
		want string
	}{
		{name: "ObsFieldSessionID", got: ObsFieldSessionID, want: "session_id"},
		{name: "ObsFieldOwnerRuntimeID", got: ObsFieldOwnerRuntimeID, want: "owner_runtime_id"},
		{name: "ObsFieldOwnerGeneration", got: ObsFieldOwnerGeneration, want: "owner_generation"},
		{name: "ObsFieldReconnectGeneration", got: ObsFieldReconnectGeneration, want: "reconnect_generation"},
		{name: "ObsFieldGatewayInstance", got: ObsFieldGatewayInstance, want: "gateway_instance"},
		{name: "ObsFieldRuntimeInstance", got: ObsFieldRuntimeInstance, want: "runtime_instance"},
		{name: "ObsFieldWSConnectionID", got: ObsFieldWSConnectionID, want: "ws_connection_id"},
		{name: "ObsFieldRouteResolutionSource", got: ObsFieldRouteResolutionSource, want: "route_resolution_source"},
		{name: "ObsFieldRouteTarget", got: ObsFieldRouteTarget, want: "route_target"},
		{name: "ObsFieldTokenParseResult", got: ObsFieldTokenParseResult, want: "token_parse_result"},
		{name: "ObsFieldErrorClass", got: ObsFieldErrorClass, want: "error_class"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.got != tt.want {
				t.Fatalf("%s = %q, want %q", tt.name, tt.got, tt.want)
			}
		})
	}
}
