# Feature Specification: Deploy Secret Configuration

**Feature Branch**: `002-deploy-secret-config`

**Created**: 2026-06-02

**Status**: Draft

**Input**: User description: "现在需要为 deploy 工具增加 secret 配置支持，可以在 service 配置中声明有哪些 secret 配置，在 deploy 中将声明的 secret 与 k8s secret 和 key 进行绑定（如果有未绑定的则终止部署）。帮我设计方案，并给出 service 和 deploy 变更后的 scheme 样例。所有 secret 都会挂载到 /mnt/dominion/secret/{secret_name} (service 配置中声明的名称）, /mnt/dominion/secret 目录则以环境变量的方式注入。"

## User Scenarios & Testing *(mandatory)*

### User Story 1 - 声明服务所需 secret (Priority: P1)

服务维护者在服务配置中声明某个服务产物运行时必须读取的 secret 名称，让 secret 需求成为服务契约的一部分，而不是散落在部署环境中。

**Why this priority**: 没有服务侧声明，部署侧无法知道哪些 secret 必须绑定，也无法可靠阻止缺失 secret 的部署。

**Independent Test**: 准备一个包含 secret 声明的服务配置并校验配置文件；配置被接受后，部署者可以明确看到该服务产物需要绑定哪些 secret。

**Acceptance Scenarios**:

1. **Given** 服务配置中的某个产物声明 `secrets: [database-url, api-token]`，**When** 部署者读取或校验该服务配置，**Then** 系统识别出该产物需要 `database-url` 和 `api-token` 两个 secret。
2. **Given** 服务配置声明了重复或空白 secret 名称，**When** 部署者校验该服务配置，**Then** 系统拒绝该配置并指出无效的 secret 声明。

---

### User Story 2 - 在部署环境中绑定 secret 来源 (Priority: P1)

部署者在部署配置中为每个被引用的服务 secret 绑定实际的 Kubernetes Secret 名称和 key，确保同一个服务在不同环境中可以使用不同的 secret 来源。

**Why this priority**: Secret 来源是环境相关配置；只有部署环境完成绑定后，部署才能生成完整、可运行的期望状态。

**Independent Test**: 准备一个服务配置声明 secret，并准备一个部署配置为所有声明项提供绑定；执行部署校验时，系统接受该配置并展示每个服务 secret 对应的外部 secret 来源。

**Acceptance Scenarios**:

1. **Given** 服务产物声明 `database-url`，且部署配置将其绑定到 Kubernetes Secret `orders-prod-secrets` 的 key `DATABASE_URL`，**When** 部署者执行部署，**Then** 部署流程继续并在运行时为该产物提供 `database-url` 文件。
2. **Given** 服务产物声明 `database-url` 和 `api-token`，但部署配置只绑定 `database-url`，**When** 部署者执行部署，**Then** 部署在提交环境变更前终止，并明确提示 `api-token` 未绑定。

---

### User Story 3 - 服务在运行时通过稳定路径读取 secret (Priority: P2)

服务开发者通过统一目录读取 secret 文件，并通过自动注入的环境变量发现 secret 根目录，避免将集群 secret 名称或 key 写入服务代码。

**Why this priority**: 统一路径让服务代码只依赖服务配置中声明的逻辑名称，降低环境切换和 secret 轮换成本。

**Independent Test**: 部署一个绑定完整 secret 的服务后，在运行时检查环境变量和挂载文件；目录变量指向 secret 根目录，且每个声明名称都存在对应文件。

**Acceptance Scenarios**:

1. **Given** 服务产物声明并绑定 `database-url`，**When** 服务容器启动，**Then** secret 内容可通过 `/mnt/dominion/secret/database-url` 读取。
2. **Given** 服务产物声明任意数量 secret，**When** 服务容器启动，**Then** 环境变量 `DOMINION_SECRET_DIR` 的值为 `/mnt/dominion/secret`。

---

### Scheme Examples

#### service.yaml

```yaml
version: "3.0"
name: orders-api
app: orders
desc: orders API service
kind: stateless
artifacts:
  - name: api
    target: :api_image
    secrets:
      - database-url
      - stripe-api-key
    ports:
      - name: http
        port: 8080
```

#### deploy.yaml

```yaml
version: "3.0"
name: orders.prod
desc: "orders production environment"
type: prod
services:
  - artifact:
      path: //projects/orders/service.yaml
      name: api
      secrets:
        database-url:
          secret: orders-prod-secrets
          key: DATABASE_URL
        stripe-api-key:
          secret: orders-prod-secrets
          key: STRIPE_API_KEY
```

#### Runtime Contract

For the example above, the deployed service observes:

- `DOMINION_SECRET_DIR=/mnt/dominion/secret`
- `/mnt/dominion/secret/database-url` contains the value of `orders-prod-secrets` key `DATABASE_URL`
- `/mnt/dominion/secret/stripe-api-key` contains the value of `orders-prod-secrets` key `STRIPE_API_KEY`

### Edge Cases

- If a service artifact declares no secrets, deployment behavior remains unchanged and no secret directory contract is required for that artifact.
- If a deploy config binds a secret name that the selected service artifact does not declare, the deployment is rejected to prevent stale or misspelled bindings.
- If a service artifact declares a secret and the deploy config omits that binding, the deployment is rejected before submitting the environment change.
- If a binding omits either Kubernetes Secret name or key, the deployment is rejected before submitting the environment change.
- If multiple declared secret names would resolve to the same mounted file path after normalization, the service configuration is rejected.
- If the Kubernetes Secret or key is absent in the target cluster, the environment must not be considered successfully ready; the failure must be visible in deployment status.

## Requirements *(mandatory)*

### Functional Requirements

- **FR-001**: The system MUST allow each deployable service artifact to declare zero or more logical secret names in the service configuration.
- **FR-002**: The system MUST reject service configurations containing empty, duplicate, or path-unsafe logical secret names.
- **FR-003**: The system MUST allow each deployed artifact entry to bind declared logical secret names to a Kubernetes Secret name and key in the deploy configuration.
- **FR-004**: The system MUST require every logical secret declared by the selected service artifact to have exactly one deploy binding before the environment change is submitted.
- **FR-005**: The system MUST reject deploy bindings for logical secret names not declared by the selected service artifact.
- **FR-006**: The system MUST reject deploy bindings that omit either the Kubernetes Secret name or key.
- **FR-007**: The system MUST preserve existing deployment behavior for artifacts that declare no secrets.
- **FR-008**: The system MUST make every bound secret available to the service at `/mnt/dominion/secret/{secret_name}`, where `{secret_name}` is the logical name declared in the service configuration.
- **FR-009**: The system MUST inject an environment variable named `DOMINION_SECRET_DIR` with value `/mnt/dominion/secret` into services that use the secret directory contract.
- **FR-010**: The system MUST prevent user-provided environment variables from overriding `DOMINION_SECRET_DIR`.
- **FR-011**: The system MUST include secret binding information in the environment desired state so the deploy control plane can reconcile the same state after restarts.
- **FR-012**: The system MUST surface missing or invalid runtime secret sources as deployment failure status rather than reporting the environment as ready.
- **FR-013**: The system MUST document the updated service and deploy configuration schemes with examples equivalent to the examples in this specification.

### Key Entities *(include if feature involves data)*

- **Logical Secret Declaration**: A service artifact requirement identified by a stable logical name, used by service code as the mounted file name.
- **Secret Binding**: An environment-specific mapping from a logical secret name to an external Kubernetes Secret name and key.
- **Secret Runtime Contract**: The runtime promise that bound secret values are exposed as read-only files under `/mnt/dominion/secret` and that the root directory is discoverable through `DOMINION_SECRET_DIR`.
- **Deploy Artifact Entry**: The deploy configuration entry that selects a service artifact and provides environment-specific settings such as replicas, environment variables, routing, and secret bindings.

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: 100% of deployments for artifacts with declared secrets fail before environment submission when any declared secret lacks a binding.
- **SC-002**: 100% of successful deployments for artifacts with declared secrets expose each declared name as a readable file under `/mnt/dominion/secret`.
- **SC-003**: 100% of successful deployments for artifacts with declared secrets expose `DOMINION_SECRET_DIR=/mnt/dominion/secret` to the service process.
- **SC-004**: Existing deploy configurations for artifacts without declared secrets continue to validate and deploy without requiring any new fields.
- **SC-005**: A service owner can determine the full secret contract for a service artifact by reading only the service configuration and the deploy binding section, without knowing cluster-internal naming conventions.

## Assumptions

- Secret declarations belong to individual service artifacts because deploy configuration currently selects an artifact by name and artifact-level runtime features already exist.
- Logical secret names are treated as file names and therefore must be non-empty, unique within the selected artifact, and path-safe.
- `DOMINION_SECRET_DIR` is the default injected environment variable name because the user specified the directory must be injected as an environment variable but did not provide a variable name.
- Secret files are read-only from the service perspective.
- The deploy tool can validate binding completeness before submitting an environment change; actual existence of the external Kubernetes Secret and key is validated by the runtime reconciliation surface and reported as deployment status.
