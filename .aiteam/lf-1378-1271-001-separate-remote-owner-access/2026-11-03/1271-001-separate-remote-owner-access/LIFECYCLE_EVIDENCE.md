# Connector lifecycle extraction evidence

The connector now owns configuration validation, irreversible owner reconciliation,
local-control readiness, dedicated secret decoding, listener creation, serving and
idempotent listener close. The daemon supplies narrow dependencies and coordinates
cancellation and joining before the shared database closes. Its remaining server
field accepts only the four bounded core-service attachment methods.

## Executed checks

Before production extraction:

```sh
go test ./internal/daemon -run '^TestRemoteOwnerPinAwayAndBackSurvivesPreparationFailure$' -timeout=3m
```

Passed. A real stored grant and access token remain invalid after the configured
owner pin changes away and back, including missing local control, missing secrets
and occupied-listener preparation failures.

After extraction and final focused review:

```sh
go test ./internal/connector -run '^Test(Prepare|RuntimeClose)' -race -timeout=3m
go test ./internal/daemon -run '^Test(RemoteConnector|RemoteOwner|FactoryConnector|MCPActionsAndHistorySurviveUnavailableDiscord|LazyDiscordJoinsExistingOwnerActionExecutor|DiscordSharedApprovalWithRemoteEnabledOrDisabled|DiscordLateTokenKeepsCoordinatorAndProtectsCoreRequests)' -timeout=5m
```

Both passed. Connector tests cover disabled mode with no dependencies, reconciliation
before an actual local-readiness refusal, withholding secret resolution until
readiness succeeds, and concurrent idempotent close releasing a prepared listener.
The readiness fixture uses separate wrappers over the same underlying backend;
its explicit refusal check prevents an unrelated backend mismatch from satisfying
the test.

Daemon checks cover invalid configuration, missing controls and secrets, malformed
cookie keys, launchd environment references, owner mismatch, occupied ports, real
HTTP discovery, cancellation/drain, and close after serving has stopped. They also
cover shipped local grant-control composition without daemon-specific bypasses.

Shared runtime checks cover the existing Discord confirmation/admission/execution
path with remote mode enabled and disabled, one shared coordinator when Discord
joins later, remote action execution and persisted inspection/history when Discord
is disabled or its token is unavailable, and refusal to treat remote authorization
as Discord-required approval. These use controlled providers and migrated temporary
SQLite stores; they make no claim about live-provider certification.

```sh
make lint PKGS='./internal/connector ./internal/daemon'
```

Passed with zero issues, including the final comment and fixture-identity correction
(repeated with a writable temporary linter cache). The final combined package tests
and lint are owned by the story lead and recorded separately.

## Resource and boundary review

There is no fallible acquisition after listener creation: successful listen returns
the owned runtime immediately. Failures before listen hold no listener; occupied
listen returns no runtime. A later daemon composition failure closes the prepared
facade in its existing deferred cleanup. Explicit close is safe before serving and
after bounded shutdown. The existing HTTP serving implementation retains its
five-second drain and joined shutdown goroutine.

The daemon retains no direct owner reconciliation or listener operation. The public
server constructor preserves its existing standalone behavior; the private
constructor used by the preparation facade skips only the already-completed owner
reconciliation. Authentication/target backend and local-control backend checks
remain in place. No schema or serialized authorization format changes were made.
