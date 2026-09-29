---
title: "HTTP API"
weight: 3
---

The HTTP server exposes a JSON REST API for key-value operations.

## Endpoints

### `GET /v1/keys/{key}`

Retrieve a value by key.

```sh
curl http://localhost:3456/v1/keys/mykey
```

Response `200`:
```json
{"key": "mykey", "value": "hello"}
```

Response `404`:
```json
{"error": "key not found"}
```

### `PUT /v1/keys/{key}`

Store a value.

```sh
curl -X PUT http://localhost:3456/v1/keys/mykey \
  -H "Content-Type: application/json" \
  -d '{"value": "hello"}'
```

Response `204` (no content).

### `DELETE /v1/keys/{key}`

Delete a key.

```sh
curl -X DELETE http://localhost:3456/v1/keys/mykey
```

Response `204` (no content).

Response `404`:
```json
{"error": "key not found"}
```

### `GET /healthz`

Health check endpoint.

```sh
curl http://localhost:3456/healthz
```

Response `200` (no content).

## Error Responses

All errors return a JSON body:

```json
{"error": "description of the error"}
```

| Status | Description |
|--------|-------------|
| `400` | Key is missing or request body is invalid |
| `404` | Key not found |
| `405` | Method not allowed |
| `409` | This node is in a cluster and is not the leader, so it cannot take the write |
| `500` | Internal server error |

A `409` names the leader in its message, so a client can be pointed at the node that will take
the write:

```json
{"error": "not the leader, 127.0.0.1:6381 is"}
```

During an election nobody is the leader yet and the message says so instead. Reads are
unaffected: any node answers `GET`, though a follower's copy may be behind the leader. See
[Durability and Clustering](../clustering/).

`GET /healthz` reports that this process is listening and nothing more — a node cut off from the
cluster's majority, unable to accept a single write, still answers `200`.
