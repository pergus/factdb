# FactDB

FactDB is a small Go HTTP service for keeping entities, source-attributed facts about those entities, and directed relationships between them. It uses only the Go standard library and targets Go 1.22 or newer.

## Quick Start

Install [Task](https://taskfile.dev/), then build and start the server:

```sh
task             # list available tasks
task build       # compile both commands into bin/
task server      # listen on 127.0.0.1:8080
```

In another terminal, load the demo inventory:

```sh
task seed-demo
```

The seeder uses `./bin/factdb-client` and the default server URL `http://127.0.0.1:8080`. Build first and keep the server running. To use another URL:

```sh
SERVER=http://localhost:9090 task seed-demo
```

The server accepts `-listen` and `-data` flags. The defaults are `127.0.0.1:8080` and `./factdb-data.json`:

```sh
mkdir -p data
./bin/factdb-server -listen 0.0.0.0:8080 -data ./data/facts.json
```

`task clean` removes the compiled `bin/` directory. It does not remove the data snapshot. Without Task, compile and run directly:

```sh
mkdir -p bin
go build -o bin/factdb-server ./cmd/factdb-server
go build -o bin/factdb-client ./cmd/factdb-client
./bin/factdb-server
```

## How It Works

### Data Model

- An **entity** has a stable ID, a type, creation and update timestamps, and a map of facts. Entity types and fact keys are caller-defined strings; the service does not enforce a schema.
- A **fact** is one JSON value for an entity and key. Values can be strings, numbers, booleans, arrays, objects, or `null`. Each fact records its source, observation time, and a `verified` boolean. Writing the same key replaces its previous value; FactDB does not retain fact history.
- A **relationship** is a directed edge with an ID, `fromId`, type, `toId`, source, and creation time. Both endpoint entities must exist. The same `(fromId, type, toId)` edge is stored only once, even if a later request supplies a different relationship ID or source.

For example, `vm-web-01 --ownedBy--> team-platform` is an outgoing `ownedBy` relationship from the VM and an incoming relationship to the team. It does not imply an edge in the opposite direction.

### Storage and Queries

On startup, the store loads entities and relationships from the JSON snapshot file. If the file does not exist, it starts empty. Each successful mutation writes a new snapshot through a temporary file and renames it into place. The snapshot contains the entities and relationships; query indexes are rebuilt when the server starts.

The in-memory store uses a read/write mutex. It maintains relationship indexes for both endpoints, so queries can find outgoing and incoming edges without duplicating relationship records. It also indexes IP addresses found in facts named exactly `ipAddress` or `ipAddresses`. These facts may be a single IP string or an array of IP strings; invalid IP values are ignored by the IP query.

Subnet search scans the distinct indexed IP addresses and returns each matching entity once. The optional verified filter requires a verified IP fact on that entity with an address inside the requested subnet. It does not validate reachability or whether the address is currently assigned.

## Using the Client

The client defaults to `http://127.0.0.1:8080` with a 10-second HTTP timeout. Put global options before the command:

```sh
./bin/factdb-client -server http://127.0.0.1:8080 -timeout 5s health
```

Create entities, then attach facts and relationships:

```sh
./bin/factdb-client create --type VirtualMachine --id vm-web-01
./bin/factdb-client create --type Team --id team-platform

# Fact values are JSON. Quote JSON strings and arrays so the shell passes them intact.
./bin/factdb-client fact vm-web-01 hostname '"web-01.example.internal"' --source dns-sync --verified
./bin/factdb-client fact vm-web-01 ipAddresses '["192.168.2.15","10.0.0.15"]' --source netbox --verified
./bin/factdb-client fact vm-web-01 powerState '"running"' --source hypervisor-api

./bin/factdb-client relate vm-web-01 ownedBy team-platform --source cmdb
```

Inspect an entity and traverse relationships in either direction:

```sh
./bin/factdb-client get vm-web-01
./bin/factdb-client relationships vm-web-01 --direction outgoing
./bin/factdb-client relationships team-platform --type ownedBy --direction incoming
./bin/factdb-client relationships vm-web-01 --direction both --source cmdb
```

Find entities with addresses in a subnet. `--verified` only matches entities whose in-subnet IP fact is marked verified:

```sh
./bin/factdb-client query-ip 192.168.2.0/24
./bin/factdb-client query-ip 192.168.2.0/24 --type VirtualMachine
./bin/factdb-client query-ip 192.168.2.0/24 --type VirtualMachine --verified
```

Commands are `create`, `fact`, `relate`, `get`, `relationships`, `query-ip`, and `health`. Run the client without a command to print its usage. The client prints JSON responses and exits with an error for non-2xx HTTP responses or request failures.

## HTTP API

The service listens on the address configured by `-listen`. All request and response bodies use JSON. Error responses have the form `{"error":"message"}`. Request bodies are limited to 1 MiB.

### Health Check

`GET /health` returns `200 OK` when the HTTP handler is responding:

```sh
curl -i http://127.0.0.1:8080/health
```

```json
{"status":"ok"}
```

### Create an Entity

`POST /v1/entities` creates an entity. `id` and `type` are required and trimmed of surrounding whitespace.

```sh
curl -i -X POST http://127.0.0.1:8080/v1/entities \
    -H 'Content-Type: application/json' \
    -d '{"id":"vm-web-01","type":"VirtualMachine"}'
```

Success is `201 Created`, with the created entity in the response. Reusing an existing entity ID returns `409 Conflict`; missing fields return `400 Bad Request`.

### Get an Entity

`GET /v1/entities/{id}` returns the entity, including all current facts:

```sh
curl -sS http://127.0.0.1:8080/v1/entities/vm-web-01
```

An example entity response:

```json
{
    "id": "vm-web-01",
    "type": "VirtualMachine",
    "createdAt": "2026-10-11T12:00:00Z",
    "updatedAt": "2026-10-11T12:05:00Z",
    "facts": {
        "hostname": {
            "value": "web-01.example.internal",
            "source": "dns-sync",
            "observedAt": "2026-10-11T12:05:00Z",
            "verified": true
        }
    }
}
```

The timestamps above are illustrative. A missing entity returns `404 Not Found`.

### Write or Replace a Fact

`PUT /v1/entities/{id}/facts/{key}` creates or replaces the fact at that key. `value` is any JSON value; `source` and `verified` are optional and default to `""` and `false`.

```sh
curl -i -X PUT http://127.0.0.1:8080/v1/entities/vm-web-01/facts/ipAddresses \
    -H 'Content-Type: application/json' \
    -d '{"value":["192.168.2.15","10.0.0.15"],"source":"netbox","verified":true}'
```

The response is `200 OK` and contains the updated entity. An empty key returns `400 Bad Request`; a missing entity returns `404 Not Found`.

Other JSON value examples:

```json
{"value":"running","source":"hypervisor-api","verified":true}
```

```json
{"value":{"cores":8,"memoryGB":32},"source":"inventory-scan"}
```

### Create a Relationship

`POST /v1/relationships` creates a directed edge. `fromId`, `type`, and `toId` are required. `id` and `source` are optional; if omitted, the server generates an ID prefixed with `rel-`.

```sh
curl -i -X POST http://127.0.0.1:8080/v1/relationships \
    -H 'Content-Type: application/json' \
    -d '{"fromId":"vm-web-01","type":"ownedBy","toId":"team-platform","source":"cmdb"}'
```

Success is `201 Created`. A missing endpoint returns `404 Not Found`, and reusing a relationship ID returns `409 Conflict`. If an edge with the same `fromId`, `type`, and `toId` already exists, the existing edge is returned instead of creating a duplicate.

### Query Relationships

`GET /v1/entities/{id}/relationships` supports these optional query parameters:

| Parameter | Meaning | Default |
| --- | --- | --- |
| `direction` | `outgoing`, `incoming`, or `both` | `both` |
| `type` | Match only this relationship type | no filter |
| `source` | Match only this source | no filter |

For example, find VMs related to a team by outgoing `ownedBy` edges from the team (the VM-to-team ownership edges are incoming to the team):

```sh
curl -sS 'http://127.0.0.1:8080/v1/entities/team-platform/relationships?direction=incoming&type=ownedBy'
```

The response has a `relationships` array. Each result has `id`, `fromId`, `type`, `toId`, `source`, and `createdAt`. Results are sorted by relationship ID. An unknown direction returns `400 Bad Request`; an unknown entity returns `404 Not Found`.

### Query IP Addresses by Subnet

`POST /v1/query/ip-in-subnet` accepts a CIDR prefix. `type` and `requireVerified` are optional:

```sh
curl -sS -X POST http://127.0.0.1:8080/v1/query/ip-in-subnet \
    -H 'Content-Type: application/json' \
    -d '{"cidr":"192.168.2.0/24","type":"VirtualMachine","requireVerified":true}'
```

The response includes the requested CIDR and matching entities:

```json
{
    "cidr": "192.168.2.0/24",
    "entities": []
}
```

An empty `type` matches every entity type. Invalid or missing CIDRs return `400 Bad Request`.

## Limitations

FactDB is a demonstrator, not a production database. It has no authentication or authorization, schema enforcement, pagination, entity deletion, relationship deletion, fact history, or source conflict resolution. Mutations rewrite the JSON snapshot, so this design is intended for small inventories. Subnet lookup scans distinct indexed IPs and may need a radix tree for very large inventories. `verified` is only a boolean, not a trust policy.
