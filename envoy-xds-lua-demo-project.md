# Project Specification: Envoy + Lua + Go xDS/ECDS Dynamic Tenant Mapping Demo

## Objective

Build a complete runnable demo showing how to replace a Cribl-style token-to-tenant header mapping layer with Envoy.

The demo must implement this request flow:

```text
Public/OTLP client
    |
    v
Envoy
    |
    |-- read `Authorization: Bearer <token>`
    |-- look up token locally in Lua
    |-- map token -> tenant
    |-- set `X-Scope-OrgID: <tenant>`
    |-- optionally remove Authorization before forwarding
    v
Upstream demo service
```

The key requirement is that token mappings can change frequently with **no Envoy restart and no dropped listener connections**.

Dynamic token updates must be delivered from a Go xDS control-plane service using Envoy's dynamic extension configuration mechanism (ECDS).

The project must be self-contained and runnable locally with Docker Compose.

The project should also be structured so that the Envoy and xDS services could later be deployed as ECS services.

---

# Architecture

Implement these containers/services:

```text
                         +----------------------+
                         |   config source      |
                         | tokens.yaml / JSON   |
                         +----------+-----------+
                                    |
                                    | watched/polled
                                    v
                         +----------------------+
                         | Go xDS control plane |
                         | ADS + ECDS           |
                         +----------+-----------+
                                    |
                          persistent gRPC/xDS
                                    |
                  +-----------------+-----------------+
                  |                                   |
                  v                                   v
          +---------------+                   +---------------+
          |    Envoy 1    |                   |    Envoy 2    |
          |               |                   |               |
          | Lua filter    |                   | Lua filter    |
          | local map     |                   | local map     |
          +-------+-------+                   +-------+-------+
                  |                                   |
                  +-----------------+-----------------+
                                    |
                                    v
                            +---------------+
                            | upstream demo |
                            | echo server   |
                            +---------------+
```

For local development one Envoy instance is sufficient, but Docker Compose should make it easy to scale Envoy to multiple replicas.

---

# Functional Requirements

## 1. Envoy bootstrap

Create a static Envoy bootstrap config.

It must contain:

- one listener for HTTP traffic on port `8080`
- admin interface on port `9901`
- static upstream cluster pointing at the demo backend
- static cluster pointing at the xDS service
- ADS/xDS configuration using gRPC
- HTTP connection manager
- dynamically discovered Lua HTTP filter using ECDS
- router filter after the Lua filter

The Envoy process must start even if the xDS service is temporarily unavailable.

The desired route is:

```text
0.0.0.0:8080
    -> Lua auth filter
    -> upstream demo service
```

The Lua filter configuration itself must **not** be hard-coded in the Envoy bootstrap.

It must be delivered dynamically by ECDS.

---

# 2. Lua filter behavior

The Go xDS service must generate Lua source similar to:

```lua
local tokens = {
  ["token-a"] = "tenant-a",
  ["token-b"] = "tenant-b"
}

function envoy_on_request(handle)
  local auth = handle:headers():get("authorization")

  if auth == nil then
    handle:respond(
      {[":status"] = "401"},
      "missing authorization\n"
    )
    return
  end

  local token = string.match(auth, "^Bearer%s+(.+)$")

  if token == nil then
    handle:respond(
      {[":status"] = "401"},
      "invalid authorization format\n"
    )
    return
  end

  local tenant = tokens[token]

  if tenant == nil then
    handle:respond(
      {[":status"] = "403"},
      "unknown token\n"
    )
    return
  end

  handle:headers():replace("x-scope-orgid", tenant)

  -- Make this configurable.
  handle:headers():remove("authorization")
end
```

Requirements:

- token lookup must be a direct Lua table lookup
- do not make HTTP, AWS, filesystem, DNS, or external service calls from Lua
- do not parse or modify request bodies
- preserve streaming behavior
- support HTTP/1.1 and HTTP/2 where practical
- preserve arbitrary OTLP or binary bodies unchanged
- return `401` for missing or malformed Authorization
- return `403` for unknown tokens
- set `X-Scope-OrgID` for known tokens
- remove `Authorization` before forwarding by default

Lua should only process request headers.

---

# 3. Token configuration file

Create a config file:

```text
config/tokens.yaml
```

Example:

```yaml
version: 1
remove_authorization_header: true

tokens:
  token-a: tenant-a
  token-b: tenant-b
  token-c: tenant-c
```

The xDS server must watch this configuration for changes.

Either of these approaches is acceptable:

1. filesystem watcher using `fsnotify`
2. polling modification time every 1-2 seconds

Prefer `fsnotify` if straightforward.

When the file changes:

1. read it
2. validate it
3. generate new Lua source
4. increment/publish an xDS version
5. update the ECDS resource

Invalid config must **not replace the last-known-good configuration**.

Log validation errors clearly.

---

# 4. Zero downtime token rotation

Demonstrate overlapping token rotation.

Initial:

```yaml
tokens:
  old-token: tenant-a
```

Then update to:

```yaml
tokens:
  old-token: tenant-a
  new-token: tenant-a
```

Then after clients have moved:

```yaml
tokens:
  new-token: tenant-a
```

Envoy must update dynamically without:

- restarting Envoy
- restarting Docker containers
- dropping the listener
- requiring an ECS redeployment

Document this sequence in the README.

---

# 5. Go xDS control-plane service

Language:

```text
Go
```

Use:

```text
github.com/envoyproxy/go-control-plane
```

Implement a real xDS server using gRPC.

Required functionality:

- listen on `:18000`
- support ADS
- support ECDS resources
- use go-control-plane snapshot cache where practical
- use a stable node ID such as:

```text
edge-envoy
```

- publish an `envoy.config.core.v3.TypedExtensionConfig`
- resource name should be:

```text
tenant-auth
```

- typed config must contain:

```text
envoy.extensions.filters.http.lua.v3.Lua
```

- Lua source must be generated from `tokens.yaml`

Structure the code cleanly, for example:

```text
cmd/xds-server/main.go
internal/config/loader.go
internal/config/watcher.go
internal/lua/generator.go
internal/xds/server.go
internal/xds/publisher.go
```

Exact package names may differ if there is a clearer structure.

---

# 6. xDS ACK/NACK logging

Implement xDS callback logging.

For each Envoy interaction, log useful information such as:

```text
node=edge-envoy
resource=tenant-auth
version=42
event=ACK
```

and on failure:

```text
node=edge-envoy
resource=tenant-auth
version=42
event=NACK
error="..."
```

Use go-control-plane server callbacks where appropriate.

This is important because in production token removal should ideally happen only after all Envoy instances have accepted the new config.

---

# 7. Multiple Envoy clients

The xDS service must work correctly with multiple Envoy clients connected simultaneously.

Do not make configuration state specific to one Envoy instance unless required by go-control-plane APIs.

All Envoy instances using the node ID/group should receive the same tenant mapping.

The README should show how to scale Envoy locally, for example:

```bash
docker compose up --scale envoy=3
```

If Docker Compose port binding prevents direct scale-out with the same host port, document an alternative such as exposing only one instance directly or adding a local load balancer.

---

# 8. Demo upstream service

Implement a very small Go HTTP server.

It should listen on `:8081`.

For every request it should return JSON containing at least:

```json
{
  "method": "POST",
  "path": "/test",
  "tenant": "tenant-a",
  "authorization_present": false,
  "content_type": "application/json",
  "content_length": 123
}
```

It must read:

```text
X-Scope-OrgID
```

and indicate whether the `Authorization` header was forwarded.

Do not log bearer token values.

The upstream should not need to understand OTLP.

---

# 9. Docker Compose

Create:

```text
docker-compose.yaml
```

Services:

```text
xds
backend
envoy
```

Use official/minimal images where practical.

Suggested ports:

```text
Envoy listener: 8080
Envoy admin:    9901
xDS gRPC:       18000
backend:        8081
```

The containers must communicate via Compose DNS names.

Example:

```text
xds:18000
backend:8081
```

Mount `config/tokens.yaml` into the xDS container so updates made on the host are detected without rebuilding containers.

---

# 10. Dockerfiles

Create multi-stage Dockerfiles for the Go services.

Example requirements:

- build with Go image
- compile static Linux binary where practical
- run in distroless or alpine/scratch-style runtime
- run as non-root where practical

Envoy may use an official Envoy image.

---

# 11. Makefile

Provide a Makefile with targets such as:

```text
make build
make up
make down
make logs
make test
make rotate-demo
make fmt
make vet
```

`make test` must run unit tests.

`make rotate-demo` may provide instructions or automate token rotation if safe and simple.

---

# 12. Tests

Include meaningful tests.

## Unit tests

Test Lua generation from config.

At minimum:

- known mappings generate expected Lua entries
- quotes/backslashes in tokens or tenant IDs are escaped safely
- duplicate or invalid configuration is rejected
- empty mappings handled according to documented behavior
- `remove_authorization_header` changes generated Lua appropriately

Security is important: never generate syntactically unsafe Lua from arbitrary YAML strings.

Do not simply interpolate unescaped token strings into Lua source.

Implement a robust Lua string escaping helper and unit test it.

## Integration smoke test

Provide either:

- shell script
- Go integration test

that performs:

```bash
curl -i \
  -H 'Authorization: Bearer token-a' \
  http://localhost:8080/test
```

Expected:

```text
200
X-Scope-OrgID seen upstream as tenant-a
Authorization not seen upstream
```

Unknown token:

```bash
curl -i \
  -H 'Authorization: Bearer bad-token' \
  http://localhost:8080/test
```

Expected:

```text
403
```

No auth:

```bash
curl -i http://localhost:8080/test
```

Expected:

```text
401
```

---

# 13. Dynamic update integration test

Include a demo script:

```text
scripts/test-dynamic-update.sh
```

Flow:

1. call Envoy with `token-a`
2. verify tenant-a
3. modify `tokens.yaml` to add:

```yaml
new-token: tenant-new
```

4. wait until xDS logs show new version / ACK, or retry briefly
5. call Envoy using `new-token`
6. verify `tenant-new`
7. prove Envoy container ID/process did not change

Do not restart Envoy during this test.

Restore the original config at the end if practical.

---

# 14. Observability

Use structured logging in the Go xDS service.

Log:

- startup
- config loaded
- config version
- number of token mappings
- config changes detected
- validation failures
- publication success
- xDS stream connect/disconnect
- ACK/NACK

Never log bearer token values.

Token count is safe to log.

Expose a simple HTTP health endpoint from the xDS server, for example:

```text
:8082/healthz
```

Optional metrics endpoint:

```text
:8082/metrics
```

If metrics are implemented, expose Prometheus-format metrics such as:

```text
xds_config_version
xds_token_count
xds_publish_total
xds_publish_error_total
xds_connected_clients
xds_ack_total
xds_nack_total
```

Metrics are desirable but not mandatory if they add excessive complexity.

---

# 15. Failure behavior

Document and demonstrate these scenarios.

## xDS server unavailable

Once Envoy has successfully received configuration, stopping the xDS service must not stop request processing using the last accepted config.

Example demonstration:

```bash
docker compose stop xds

curl \
  -H 'Authorization: Bearer token-a' \
  http://localhost:8080/test
```

should still succeed using the last-known-good Lua configuration.

## Invalid token file

If `tokens.yaml` becomes invalid:

- xDS service logs an error
- it keeps the previously published configuration active
- Envoy continues serving existing mappings

## Empty config

Choose and document behavior.

Preferred behavior:

- empty `tokens` map is valid
- all bearer tokens result in `403`

---

# 16. Security requirements

Do not log:

- Authorization headers
- bearer tokens
- complete generated Lua source containing token values

The demo may use plaintext development tokens in `tokens.yaml`, but document that production mappings would normally originate from AWS Secrets Manager or an equivalent secret store.

In production, the xDS service should:

```text
AWS Secrets Manager
      |
      v
xDS control plane
      |
      v
Envoy
```

Do not make Envoy or Lua query Secrets Manager per request.

Do not make the Lua filter call external authentication services.

The hot path must remain local.

Mention that the xDS channel should use TLS/mTLS in production even if the local demo uses plaintext gRPC.

---

# 17. Performance design

The project is intended for very high telemetry throughput.

The design must preserve these properties:

```text
request
  |
  +-- get Authorization header
  +-- parse Bearer prefix
  +-- Lua table lookup
  +-- set X-Scope-OrgID
  +-- remove Authorization
  |
  v
forward unchanged body
```

The Lua filter must never:

- buffer the complete body unnecessarily
- parse protobuf
- parse JSON payloads
- inspect OTLP records
- call the network
- call a database
- call AWS APIs

The filter cost should be essentially independent of request body size.

Add comments in the code explaining this.

---

# 18. ECS deployment notes

Do not need to deploy AWS infrastructure for the demo, but include documentation showing how the design maps to ECS.

Target architecture:

```text
Internet
   |
   v
Public ALB
   |
   v
ECS service: Envoy
   |
   v
Private NLB
   |
   v
EKS / Kubernetes
   |
   +-- Mimir
   +-- Loki
   +-- Tempo
```

Separate ECS service:

```text
ECS service: xDS control plane
```

The xDS service may sit behind an internal NLB or use service discovery.

Explain:

- multiple Envoy ECS tasks all subscribe to the same xDS configuration
- updating tenant mappings does not require updating the ECS task definition
- changing Lua logic may be pushed via ECDS as part of the resource
- changing Envoy bootstrap/binary still requires an ECS deployment
- token rotation only requires config publication

---

# 19. Production config source extension

Design the config loader behind an interface so `tokens.yaml` can later be replaced by AWS Secrets Manager.

Example interface:

```go
type ConfigSource interface {
    Load(ctx context.Context) (*TenantConfig, error)
    Watch(ctx context.Context, updates chan<- *TenantConfig) error
}
```

Or a simpler equivalent if preferred.

Implement the filesystem version now.

Add a stub or documented extension point for:

```text
SecretsManagerConfigSource
```

Do not require AWS credentials for the local demo.

---

# 20. Suggested repository layout

Aim for something similar to:

```text
envoy-xds-lua-demo/
|
+-- README.md
+-- Makefile
+-- docker-compose.yaml
+-- go.mod
+-- go.sum
|
+-- config/
|   +-- envoy.yaml
|   +-- tokens.yaml
|
+-- cmd/
|   +-- xds-server/
|   |   +-- main.go
|   |
|   +-- backend/
|       +-- main.go
|
+-- internal/
|   +-- config/
|   |   +-- model.go
|   |   +-- loader.go
|   |   +-- watcher.go
|   |
|   +-- lua/
|   |   +-- generator.go
|   |   +-- generator_test.go
|   |
|   +-- xds/
|       +-- server.go
|       +-- callbacks.go
|       +-- publisher.go
|
+-- docker/
|   +-- Dockerfile.xds
|   +-- Dockerfile.backend
|
+-- scripts/
    +-- smoke-test.sh
    +-- test-dynamic-update.sh
    +-- rotate-demo.sh
```

This is a guideline, not an absolute constraint.

---

# 21. README requirements

The generated README must explain:

## What the demo proves

- Envoy replaces token-to-header mapping
- Lua performs only local header processing
- Go xDS server pushes dynamic Lua/token configuration
- token updates require no Envoy restart
- xDS outage does not interrupt traffic after config is loaded

## Quick start

Expected commands should be close to:

```bash
make up
```

Then:

```bash
curl -s \
  -H 'Authorization: Bearer token-a' \
  http://localhost:8080/test | jq
```

Expected response:

```json
{
  "tenant": "tenant-a",
  "authorization_present": false
}
```

Then explain how to edit:

```text
config/tokens.yaml
```

and immediately test the new token without restarting Envoy.

## Troubleshooting

Include commands such as:

```bash
docker compose logs -f xds
```

```bash
docker compose logs -f envoy
```

```bash
curl http://localhost:9901/config_dump
```

```bash
curl http://localhost:9901/stats
```

Show how to inspect whether the dynamic Lua config is loaded.

---

# 22. Acceptance criteria

The project is complete only when all of these work:

### Initial mapping

```bash
curl -H 'Authorization: Bearer token-a' http://localhost:8080/test
```

returns `200` and backend sees:

```text
X-Scope-OrgID: tenant-a
```

### Unknown token

```bash
curl -H 'Authorization: Bearer invalid' http://localhost:8080/test
```

returns:

```text
403
```

### Missing token

```bash
curl http://localhost:8080/test
```

returns:

```text
401
```

### Dynamic update

Adding:

```yaml
new-token: tenant-new
```

to `tokens.yaml` causes `new-token` to work without restarting Envoy.

### Rotation overlap

Both old and new token may temporarily map to the same tenant.

### xDS failure

After loading config:

```bash
docker compose stop xds
```

must not break existing valid token requests.

### Restart xDS

Restarting the xDS service should reconnect Envoy and allow subsequent dynamic updates.

### Multiple Envoys

More than one Envoy client can subscribe to the same config.

### Tests

```bash
make test
```

must pass.

---

# 23. Important implementation instruction for Codex

Do not merely generate placeholder files or pseudo-code.

Produce a fully compilable and runnable repository.

Before declaring completion:

1. run `go fmt`
2. run `go test ./...`
3. run `go vet ./...` where practical
4. build all binaries
5. run Docker Compose
6. execute the smoke tests
7. execute the dynamic update test
8. verify Envoy did not restart during config update
9. verify existing mappings still work when xDS is stopped

If the exact go-control-plane ECDS API differs from expectations, inspect the installed/current library types and implement the correct current API rather than replacing ECDS with a different mechanism.

Do not silently fall back to embedding Lua statically in Envoy.

The core purpose of the demo is:

```text
Go xDS control plane
       |
       | ECDS dynamic config
       v
Envoy Lua filter
       |
       | local token -> tenant lookup
       v
X-Scope-OrgID
```

---

# 24. Future enhancements to mention but not implement unless trivial

Document these as possible next steps:

- AWS Secrets Manager configuration source
- EventBridge/SQS notification instead of polling
- mTLS on xDS
- per-tenant metadata beyond tenant ID
- allowed signal types: metrics/logs/traces
- per-tenant destination routing
- token hashing strategy
- Prometheus metrics
- deployment to ECS/Fargate
- internal NLB/service discovery for xDS
- high availability control plane
- config version convergence dashboard
- graceful token rotation workflow

Do not add per-request network authentication as a future recommendation; the desired architecture intentionally keeps authentication lookup in-process inside Envoy.

---

# Final desired outcome

After running the project, a user should be able to prove this entire sequence:

```text
1. Start Envoy + xDS + backend.

2. Send:
   Authorization: Bearer token-a

3. Backend receives:
   X-Scope-OrgID: tenant-a

4. Edit tokens.yaml and add:
   token-new: tenant-new

5. xDS detects the change and pushes a new ECDS version.

6. Envoy accepts the new filter configuration without restarting.

7. Immediately send:
   Authorization: Bearer token-new

8. Backend receives:
   X-Scope-OrgID: tenant-new

9. Stop the xDS control plane.

10. Existing configured tokens continue to work because Envoy keeps the last accepted configuration.
```

That end-to-end behavior is the primary acceptance test for the project.
