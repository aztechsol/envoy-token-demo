# Envoy dynamic tenant mapping demo

This runnable demo replaces a token-to-tenant mapping proxy with an Envoy HTTP
filter whose Lua configuration is rendered from an embedded Lua template and
delivered dynamically by a Go xDS control plane. Requests do a local header read, Bearer myteste, Lua table lookup, and
`X-Scope-OrgID` update. Bodies are never read, mytested, or buffered by the Lua
filter, so OTLP, JSON, and other streaming or binary payloads pass through
unchanged.

Token changes are published with ECDS over Envoy's persistent ADS stream. They
do not restart Envoy, replace its listener, or require an image deployment.

## Architecture

```text
client -> Envoy :8080 -> backend :8081
             ^
             | ADS/ECDS (gRPC)
             |
        xDS server :18000 <- config/tokens.yaml
```

Envoy's admin API is exposed on `localhost:9901`; the xDS health endpoint is on
`localhost:8082/healthz`. The local xDS channel is plaintext. Production should
use TLS or mTLS.

## Quick start

Requirements are Docker with Compose v2 and `curl`. `jq` is useful for the
examples but is not needed by the test scripts.

```bash
make up
docker compose logs -f xds
```

Wait for the xDS log to report publication and an ACK, then in another shell:

```bash
curl -s \
  -H 'Authorization: Bearer 123456-3290902-2-2323' \
  http://localhost:8080/test | jq
```

The response includes:

```json
{
  "tenant": "mytest--dev",
  "authorization_present": false
}
```

The complete basic check is:

```bash
make smoke-test
```

It verifies a known token returns `200`, an unknown token returns `403`, a
missing header returns `401`, and that the backend never sees `Authorization`.

To add a mapping, edit [config/tokens.yaml](config/tokens.yaml):

```yaml
tenants:
  mytest--dev: new-token
```

Save the file, wait for the ACK, and call Envoy without restarting anything:

```bash
curl -s \
  -H 'Authorization: Bearer new-token' \
  http://localhost:8080/test | jq
```

`make dynamic-test` automates that flow and narrates every stage: initial
request, configuration update, xDS publication, Envoy ACK, effective mapping,
and the unchanged Envoy container ID. It never prints a bearer token value and
restores the original file on exit.

## Lua template

The filter source is a normal, readable Go `text/template` at
[`internal/xds/tenant_auth.lua.tmpl`](internal/xds/tenant_auth.lua.tmpl), embedded
into the xDS binary at build time. The xDS service renders it with an ordered
list of mappings and the authorization-removal flag. Tokens and tenants are
intentionally limited to letters, digits, and hyphens (for example, UUIDs and
`tenant--dev`); the template uses Go's built-in `printf "%q"` to quote them.

## Token rotation with overlap

A safe rotation publishes both credentials before removing the old one:

```yaml
# 1. initial
tenants:
  mytest--dev: old-token

# 2. overlap while clients migrate
tenants:
  mytest--dev: new-token
  mytest--dev-previous: old-token

# 3. remove only after every Envoy has ACKed and clients have migrated
tenants:
  mytest--dev: new-token
```

Each save produces a new ECDS version. The xDS logs identify ACKs and NACKs by
node, resource, and version. In production, use those acknowledgements to
confirm that every Envoy task accepted the overlap configuration before
retiring the old token. Run `make rotate-demo` for a local automated example;
it restores the starting file afterward.

## Failure behaviour

The listener waits for its first valid `tenant-auth` resource, which is a
fail-closed startup posture. Envoy itself and its admin API still start if xDS
is temporarily unavailable, and the ADS client reconnects automatically.

After the first configuration has been accepted, Envoy retains it if xDS goes
away:

```bash
docker compose stop xds
curl -s \
  -H 'Authorization: Bearer 123456-3290902-2-2323' \
  http://localhost:8080/test | jq
docker compose start xds
```

An invalid YAML file is logged and rejected by xDS; the last-known-good snapshot
stays active. An empty `tenants: {}` map is valid and makes every well-formed
Bearer token return `403`. Missing or malformed Authorization returns `401`.

## Multiple Envoy clients

The normal Compose file publishes fixed host ports and therefore runs one
directly reachable Envoy. To exercise multiple clients, Compose v2.24.4 or
newer can apply the included port-reset overlay:

```bash
docker compose \
  -f docker-compose.yaml \
  -f docker-compose.scale.yaml \
  up -d --scale envoy=3
```

All replicas use node ID `edge-envoy` and receive the same snapshot. With the
host binding removed, issue a request from the Compose network:

```bash
docker run --rm --network envoy-demo_default curlimages/curl:8.12.1 \
  -s -H 'Authorization: Bearer 123456-3290902-2-2323' http://envoy:8080/test
```

For a stable entry point to scaled replicas, put a local load balancer in front
of `envoy:8080`. This mirrors the production load-balancer arrangement.

## Commands

```text
make build          build both Go container images
make up             build and start the demo
make down           stop and remove the Compose services
make logs           follow all service logs
make test           run Go unit tests
make smoke-test     verify the static request behaviours
make dynamic-test   verify an in-place ECDS update
make rotate-demo    demonstrate overlap then removal
make fmt            format Go source
make vet            run Go static checks
```

## Troubleshooting and inspection

Follow the control-plane and proxy logs:

```bash
docker compose logs -f xds
docker compose logs -f envoy
```

Inspect the active dynamic extension configuration and Envoy statistics:

```bash
curl -s http://localhost:9901/config_dump | \
  jq '.. | objects | select(.name? == "tenant-auth")'
curl -s http://localhost:9901/stats | grep -E 'extension_config|config_reload'
```

Check service state and xDS health:

```bash
docker compose ps
curl -i http://localhost:8082/healthz
```

If port `8080`, `8081`, `9901`, `18000`, or `8082` is already in use, stop the
conflicting process or change the corresponding left-hand port in
`docker-compose.yaml`. If Envoy remains in warming state, inspect xDS logs for a
configuration validation error or ECDS NACK.

## Security and performance notes

The sample file contains development-only plaintext tokens. Production token
mappings should normally come from AWS Secrets Manager (or an equivalent
secret store) through another implementation of the config-source interface:

```text
AWS Secrets Manager -> xDS control plane -> Envoy local Lua table
```

Envoy and Lua must not query Secrets Manager, DNS, a database, or an auth
service per request. Logs deliberately contain counts and versions, never
bearer tokens, Authorization headers, or generated Lua source. Protect the
configuration source, admin endpoint, and xDS transport in production.

The hot path is independent of body size: it examines request headers only,
does one Lua table lookup, writes the tenant header, and removes Authorization
by default. Set `remove_authorization_header: false` only when the upstream
explicitly needs the original credential.

## ECS deployment mapping

A production layout maps directly to separate ECS services:

```text
Internet -> public ALB -> ECS Envoy service -> private NLB -> EKS
                                                           |-- Mimir
                                                           |-- Loki
                                                           `-- Tempo

                    ECS xDS control-plane service
                              |
                    internal NLB/service discovery
                              |
                       all Envoy ECS tasks
```

Every Envoy task subscribes to the same ECDS configuration. Token rotation and
Lua logic changes are configuration publications and do not change the ECS task
definition. Updating the Envoy bootstrap or Envoy binary still requires an ECS
deployment. Run the xDS service with multiple instances behind internal service
discovery or an internal NLB, store its source data in Secrets Manager, and use
mTLS for control-plane traffic.
