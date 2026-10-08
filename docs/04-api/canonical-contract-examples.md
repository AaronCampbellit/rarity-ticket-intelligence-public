# Canonical API Contract Examples

**Status:** Proposed convention\
**Version:** 0.1\
**Last updated:** 2026-07-25

Examples are illustrative and do not represent live credentials or customer data.

## Create an incident

```http
POST /api/v1/incidents
Authorization: Bearer <token>
Idempotency-Key: 019f9a38-8f65-75a9-a6b5-bf0f8095f82c
Content-Type: application/json
```

```json
{
  "client_id": "019f99dd-10a5-755e-b4a4-7db083259778",
  "title": "VPN unavailable at Dallas office",
  "description": "Multiple users report connection failures.",
  "impact": "site",
  "urgency": "high",
  "queue_id": "019f9a22-12cc-70e4-9683-8572ee751e33",
  "affected_service_ids": [
    "019f9a28-e765-7060-8954-5ed37a8d8e11"
  ]
}
```

```http
HTTP/1.1 201 Created
Location: /api/v1/incidents/019f9a39-d4db-720d-b137-c9629c847cdf
ETag: "1"
X-Correlation-ID: 019f9a38-91cf-7a8c-b6b1-f9b15d3853bb
```

```json
{
  "data": {
    "id": "019f9a39-d4db-720d-b137-c9629c847cdf",
    "display_id": "INC-10482",
    "object_type": "incident",
    "client_id": "019f99dd-10a5-755e-b4a4-7db083259778",
    "title": "VPN unavailable at Dallas office",
    "status": "new",
    "workflow": {
      "id": "019f9a05-8fca-79da-854d-98e969e835bc",
      "version": 3,
      "selection_explanation_url": "/api/v1/incidents/019f9a39-d4db-720d-b137-c9629c847cdf/workflow-selection"
    },
    "owner_id": null,
    "collaborator_ids": [],
    "version": 1,
    "created_at": "2026-07-25T20:06:41.202Z"
  }
}
```

## Claim ownership

Domain actions use explicit endpoints when a field patch could bypass workflow or audit semantics.

```http
POST /api/v1/incidents/019f9a39-d4db-720d-b137-c9629c847cdf/actions/claim
If-Match: "1"
Content-Type: application/json

{"reason":"Beginning triage"}
```

A successful claim returns the updated resource and `ETag: "2"`. A stale version returns `409 concurrency_conflict`.

## Cursor list

```http
GET /api/v1/work-records?client_id=019f99dd-10a5-755e-b4a4-7db083259778&queue_id=019f9a22-12cc-70e4-9683-8572ee751e33&sort=-created_at&limit=50
```

```json
{
  "data": [],
  "page": {
    "limit": 50,
    "next_cursor": "opaque-signed-cursor-or-null"
  }
}
```

## Validation error

```json
{
  "error": {
    "code": "validation_failed",
    "message": "The request contains invalid fields.",
    "correlation_id": "019f9a40-8060-75dc-88e4-346403a23b23",
    "fields": [
      {
        "path": "urgency",
        "code": "invalid_enum",
        "message": "Choose one of: low, medium, high, critical."
      }
    ],
    "documentation_url": "/docs/api/errors#validation_failed"
  }
}
```

## Authorization behavior

An authenticated caller without access receives an enumeration-resistant response. APIs do not reveal whether an inaccessible object exists. Internally, the audit/security decision retains the precise denial reason and correlation ID.

## Idempotency

Mutation idempotency records bind principal, route, key, and request-body digest for a documented retention period. Repeating an identical request returns the original accepted outcome. Reusing the key with a different body returns `409 idempotency_key_reused`.

## Approval gate

These examples remain proposed until object envelopes, event schemas, error codes, pagination cursors, idempotency retention, and OpenAPI compatibility tests are jointly approved.
