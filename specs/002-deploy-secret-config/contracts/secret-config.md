# Contracts: Deploy Secret Configuration

## 1. Service Configuration Schema Extension

### Contract: `service.yaml` → `artifacts[].secrets`

**Interface**: YAML configuration file consumed by deploy CLI.

**Schema Addition** (to `artifacts` items in `service.schema.json`):

```json
"secrets": {
  "type": "array",
  "description": "该产物运行时所需的 secret 逻辑名列表",
  "items": {
    "type": "string",
    "minLength": 1,
    "pattern": "^[a-z][a-z0-9_-]{0,63}$"
  },
  "uniqueItems": true
}
```

**Behavior**:
- Optional field. Absence equivalent to empty array.
- Values become file names under `/mnt/dominion/secret/` at runtime.
- Must be non-empty, unique within artifact, and match the pattern.

**Example**:

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

---

## 2. Deploy Configuration Schema Extension

### Contract: `deploy.yaml` → `services[].artifact.secrets`

**Interface**: YAML configuration file consumed by deploy CLI.

**Schema Addition** (to `artifact` object in `deploy.schema.json`):

```json
"secrets": {
  "type": "object",
  "description": "逻辑 secret 名到 Kubernetes Secret 的绑定",
  "additionalProperties": {
    "type": "object",
    "additionalProperties": false,
    "required": ["secret", "key"],
    "properties": {
      "secret": {
        "type": "string",
        "minLength": 1,
        "description": "Kubernetes Secret 资源名"
      },
      "key": {
        "type": "string",
        "minLength": 1,
        "description": "Kubernetes Secret 中的 key"
      }
    }
  }
}
```

**Behavior**:
- Optional field. Absence means no bindings.
- Each key must match a secret declared in the referenced service artifact.
- Every declared secret must have exactly one binding.
- Bindings for undeclared secrets are rejected.

**Example**:

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

---

## 3. Compiler Validation Contract

### Contract: `compiler.Compile` secret binding validation

**Precondition**: `deployConfig` and `serviceConfigs` are non-nil and parsed.

**Postcondition**:
- If any declared secret lacks a binding → returns error naming the unbound secret.
- If any binding references a secret not declared by the artifact → returns error naming the extra binding.
- If all bindings are complete and valid → `ArtifactSpec.SecretBindings` populated in output.

**Error Format**:
- Unbound: `"artifact %s in %s: secret %q declared but not bound"`
- Extra: `"artifact %s in %s: secret binding %q not declared by service artifact"`

---

## 4. Proto Contract

### Contract: `SecretBinding` message + `ArtifactSpec` extension

```protobuf
// Secret binding maps a logical secret name to a Kubernetes Secret key.
message SecretBinding {
  // Logical name declared in service.yaml artifacts[].secrets
  string logical_name = 1;

  // Kubernetes Secret resource name
  string secret_name = 2;

  // Key within the Kubernetes Secret
  string key = 3;
}

message ArtifactSpec {
  // ... fields 1-10 unchanged ...
  reserved 11;

  // Secret bindings for this artifact. Empty if the service artifact
  // declares no secrets. Each entry corresponds to one mounted file
  // under /mnt/dominion/secret/.
  repeated SecretBinding secret_bindings = 12;
}
```

---

## 5. Runtime Contract

### Contract: Container secret mount and environment variable

**Precondition**: Artifact has non-empty `secret_bindings`.

**Guarantees**:
1. Container has a projected volume named `dominion-secrets`.
2. Each `SecretBinding` produces one `SecretProjection` in the projected volume:
   - `LocalObjectReference.Name` = `binding.SecretName`
   - `Items[0].Key` = `binding.Key`
   - `Items[0].Path` = `binding.LogicalName`
3. Container has a volume mount: `{Name: "dominion-secrets", MountPath: "/mnt/dominion/secret", ReadOnly: true}`
4. Container env contains `DOMINION_SECRET_DIR=/mnt/dominion/secret`.
5. `DOMINION_SECRET_DIR` overrides any user-provided value with the same key.

**Postcondition**: At runtime, for each binding, the file `/mnt/dominion/secret/{logical_name}` contains the value of the referenced Kubernetes Secret key.

---

## 6. Backward Compatibility Contract

### Contract: No-secret artifacts unchanged

**Guarantee**: If a service artifact does not declare `secrets` (or declares an empty list), the deployment behavior is identical to the current behavior:
- No `dominion-secrets` volume is created.
- No `/mnt/dominion/secret` mount exists.
- No `DOMINION_SECRET_DIR` env var is injected.
- `ArtifactSpec.SecretBindings` is empty or nil in proto.
