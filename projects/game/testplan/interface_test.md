# game-session Interface Test

* name：game-session HTTP REST 接口测试
* deploy：//projects/game/testplan/test_deploy.yaml

## Scope

* Service: game-session
* Interface: grpc-gateway exposed HTTP REST API
* Base path: `/v1`
* Covered scenarios:
  1. CreateSession — `POST /v1/sessions` with `type=SESSION_TYPE_SAOLEI` returns `200` with `session` and `agentConnectUrl`
  2. GetSession — `GET /v1/sessions/{id}` returns `200` with `session`
  3. ReconnectSession — `POST /v1/sessions/{id}:reconnect` returns `200` with updated `session` and new `agentConnectUrl`
  4. DeleteSession — `DELETE /v1/sessions/{id}` returns `200`
  5. GetSession not found — `GET /v1/sessions/nonexistent` returns `404`
  6. CreateSession invalid type — `POST /v1/sessions` with `type=SESSION_TYPE_UNSPECIFIED` returns `400`

## Test cases

* //projects/game/testplan:testplan_test
