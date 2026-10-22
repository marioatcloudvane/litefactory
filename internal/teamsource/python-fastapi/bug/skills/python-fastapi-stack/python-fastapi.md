# Python and FastAPI guidance
Use the repository's Python, FastAPI and Pydantic versions; see supported-versions.md for manifest selection and conditional checks. Use typed request and response models with explicit validation, constrained fields and documented errors. Do not expose persistence objects or secrets through response serialization. Respect repository conventions for Pydantic strictness and coercion.

Use dependency injection with explicit request versus application lifetimes. Initialize shared clients in lifespan and close them on shutdown; yield dependencies release resources on every path. Override dependencies in API tests and restore overrides after each fixture.

Use async handlers for asynchronous I/O. Blocking clients or CPU work must not block the event loop: use the existing synchronous handler, thread offload or task architecture. Do not invent background infrastructure. Propagate cancellation and use bounded timeouts.

Translate domain failures consistently into documented HTTP error responses; retain unexpected causes internally without exposing secrets. Test successful requests, validation failures, authorization/error paths and cleanup using pytest and the existing HTTPX/TestClient conventions.

The backend OpenAPI schema is the frontend interface reference. Review request/response and status-code changes together with typed client use. Respect repository conventions for client generation, authentication, persistence and deployment.
