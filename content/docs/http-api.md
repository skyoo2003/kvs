---
title: "HTTP API"
weight: 2
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
| `500` | Internal server error |
