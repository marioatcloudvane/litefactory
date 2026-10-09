These are generic Go review questions. Existing repository conventions take precedence; for litefactory, litefactory.md scopes SQL, migrations, packages, testing and lint. No new test tier is authorized.


# Go / Backend Platform Constraints

Read these constraints against the feature spec. For each constraint, determine whether it applies to the planned feature and produce a bullet for the Architecture Notes section.

## Project Layout

| Directory | Purpose |
|---|---|
| `cmd/<binary>/` | `main.go` entry points only — wiring, flag/env parsing, startup/shutdown. No business logic. |
| `internal/` | Private application and library code. The Go compiler enforces this: packages under `internal/` cannot be imported by any module outside the module rooted at `internal/`'s parent — this is a language-level constraint, not a convention. |
| `pkg/` | Code intended for import by external modules. Only put something here if there is a real external consumer; when in doubt, use `internal/`. |

- Default to `internal/` for all application code. Promote to `pkg/` only when external reuse is an actual requirement.
- **Flag if:** the feature adds a new top-level package → verify it lands in `internal/` unless external consumption is explicitly required.
- **Flag if:** the feature needs to share code across multiple `cmd/` binaries → shared code belongs in `internal/`, not duplicated per binary.

## Error Handling

- Wrap errors with context using `fmt.Errorf("doing X: %w", err)` — never discard the original error or reformat it with `%v` when the caller may need to unwrap it.
- Define sentinel errors (`var ErrNotFound = errors.New("not found")`) or custom error types for conditions callers need to branch on; check them with `errors.Is` / `errors.As`, never string-matching `err.Error()`.
- Never use `panic` for expected or recoverable failures (bad input, not-found, network errors) — reserve `panic`/`recover` for truly unrecoverable programmer errors, and only `recover` at a defined boundary (e.g. an HTTP middleware) to prevent a full process crash.
- Every returned `error` must be checked. An ignored error (`_ = doThing()` outside of a documented no-op case, or a bare unchecked call) is a defect.
- **Flag if:** the feature introduces a new error condition callers must branch on → needs a sentinel error or typed error, not a string comparison.
- **Flag if:** the feature calls a function that returns `error` → confirm every call site checks it.

**Wrapping — do this, not that:**
```go
// Wrong — loses the underlying error, caller can't unwrap
if err != nil {
    return fmt.Errorf("failed to load project: %v", err)
}

// Correct — preserves the chain for errors.Is / errors.As
if err != nil {
    return fmt.Errorf("loading project %s: %w", id, err)
}
```

**Sentinel errors — do this, not that:**
```go
// Wrong — brittle string match
if err.Error() == "not found" { ... }

// Correct
var ErrProjectNotFound = errors.New("project not found")
if errors.Is(err, ErrProjectNotFound) { ... }
```

## Concurrency

- Every goroutine must have a clear exit path. A goroutine started with no corresponding signal to stop it (no `context` cancellation, no closed channel, no `WaitGroup`) is a leak — it never gets flagged by the compiler and accumulates in production until the process exhausts memory or file descriptors.
- `context.Context` is the standard mechanism for cancellation, timeouts, and deadlines. It should be the first parameter of any function that can block (`ctx context.Context, ...`) — never store a `Context` in a struct field.
- Run concurrency-sensitive code and tests with `go test -race`. A data race is a defect regardless of whether it reproduces reliably.
- Channel pitfalls: an unbuffered send with no ready receiver deadlocks; a send on a closed channel panics; a receive on a closed channel returns the zero value immediately (check the second `, ok` return to distinguish "closed" from "zero value delivered").
- Choose the right primitive:

| Need | Use |
|---|---|
| Protect shared mutable state (a map, counter, struct field) | `sync.Mutex` / `sync.RWMutex` |
| Hand off a value or signal completion between goroutines | channel |
| Run a group of goroutines and collect the first error / wait for all | `errgroup.Group` (`golang.org/x/sync/errgroup`) |
| Run exactly once (lazy init) | `sync.Once` |

- **Flag if:** the feature starts a goroutine → verify it has an explicit exit path (context cancellation, closed channel, or bounded lifetime tied to the caller).
- **Flag if:** the feature performs a blocking operation (network, DB, file I/O, channel op) → the function must accept `context.Context` as its first parameter and respect cancellation.
- **Flag if:** the feature shares state across goroutines → needs a mutex or a channel-based handoff, not an unsynchronized shared variable.

**Goroutine lifecycle — do this, not that:**
```go
// Wrong — goroutine leaks if the caller gives up waiting
go func() {
    result := doExpensiveWork()
    resultCh <- result // blocks forever if nobody ever reads resultCh
}()

// Correct — bounded by context, will not leak
go func() {
    select {
    case resultCh <- doExpensiveWork():
    case <-ctx.Done():
    }
}()
```

## Interfaces

- Define interfaces at the point of consumption, not alongside the implementation. The caller declares the small interface it needs; the concrete type satisfies it implicitly.
- Follow "accept interfaces, return structs": function parameters should be the narrowest interface that satisfies the need; return concrete types so callers get full access to what was built.
- Avoid premature abstraction — do not introduce an interface for a single concrete implementation "for future testability." Add the interface when a second implementation (or a test fake) actually requires it.
- **Flag if:** the feature defines a new interface → confirm it is declared by the consumer package and sized to only the methods that package needs.
- **Flag if:** the feature introduces an interface with exactly one production implementation and no test fake requiring substitution → likely premature; use the concrete type directly.

## Dependency Management

- `go.mod` / `go.sum` are the source of truth for versions; `go.sum` must always be committed alongside `go.mod`.
- Go uses minimal version selection (MVS) — the build uses the lowest version that satisfies all `go.mod` constraints across the module graph, not automatically the newest. Run `go get -u` deliberately, not by default.
- Evaluate every new dependency for maintenance status and licence before adding it; run `go mod tidy` after any dependency change to keep `go.mod`/`go.sum` accurate.
- Keep `internal/` core/domain logic's third-party dependency surface shallow — pull heavy or wide-reaching dependencies in at the edges (`cmd/`, adapters), not deep in business logic, so the core stays easy to test and to port.
- Only introduce vendoring (`go mod vendor`) if the project already vendors or has a specific offline/reproducibility requirement — it is not the default.
- **Flag if:** the feature requires a new third-party package → must be evaluated before the plan is confirmed.
- **Flag if:** the feature adds a dependency directly inside `internal/<domain>` core logic → consider whether it belongs behind an interface at the edge instead.

## HTTP / API Layer

- Check whether the project already has a router convention (stdlib `net/http` with `http.ServeMux`, or a library such as `chi`, `echo`, `gin`) before introducing a new one — defer to the existing choice; do not mix routers across the codebase.
- Chain middleware explicitly and in a consistent order (recover → logging → auth → handler is a common default); keep each middleware single-purpose.
- Implement graceful shutdown: listen for `SIGTERM`/`SIGINT`, cancel a root `context.Context`, and call `http.Server.Shutdown(ctx)` with a bounded timeout so in-flight requests can finish instead of being cut off.
- **Flag if:** the feature adds a new HTTP entry point → confirm it follows the project's existing router/middleware convention.
- **Flag if:** the feature is a new long-running service (`cmd/` binary with a listener) → confirm graceful shutdown is wired up.

**Graceful shutdown — do this, not that:**
```go
// Wrong — process exits with active connections mid-request
srv.ListenAndServe()

// Correct — SIGTERM triggers a bounded, graceful drain
ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
defer stop()

go func() {
    if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
        log.Fatalf("server error: %v", err)
    }
}()

<-ctx.Done()
shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
defer cancel()
srv.Shutdown(shutdownCtx)
```

## Database Access

- Use `database/sql` conventions consistently: pass `context.Context` to every query (`QueryContext`, `ExecContext`), and always `defer rows.Close()` after a successful `Query`/`QueryContext`.
- Configure the connection pool explicitly — do not rely on defaults for a production service: `SetMaxOpenConns`, `SetMaxIdleConns`, `SetConnMaxLifetime`.
- Parameterized queries only. Never build SQL by string concatenation or `fmt.Sprintf` with user input — this is a SQL-injection risk, not a style preference.
- If the project has no migration tooling convention yet, `golang-migrate` or `goose` are the standard choices; check for an existing convention first and defer to it.
- **Flag if:** the feature adds a new query → confirm it is parameterized and passes `context.Context`.
- **Flag if:** the feature adds a new table or column → confirm a migration file is included, not a manual schema change.

**Parameterized queries — do this, not that:**
```go
// Wrong — SQL injection
query := fmt.Sprintf("SELECT * FROM users WHERE email = '%s'", email)
db.QueryContext(ctx, query)

// Correct — placeholder binding
db.QueryContext(ctx, "SELECT * FROM users WHERE email = $1", email)
```

## Testing Conventions

- Table-driven tests are the Go-idiomatic default for anything with multiple input/output cases — prefer them over a series of individually named test functions covering variants of the same behavior.
- Test files are co-located with the code they test as `<file>_test.go` in the same package (or `<package>_test` for black-box tests).
- Run `go test -race -cover ./...` as the standard local/CI check — race detection and coverage are not optional extras.
- Use `testify` (`assert`/`require`) for assertions only if the project already depends on it; do not introduce a new assertion library into a codebase using plain `if got != want { t.Errorf(...) }`.
- Prefer small, hand-written fakes for simple interfaces over a mocking framework — a mock is justified when the interface is large or the interaction sequence itself needs verifying; otherwise a fake adds less noise.
- **Flag if:** the feature adds a function with multiple input variants → should be a table-driven test, not repeated near-duplicate test functions.
- **Flag if:** the feature needs a test double for an interface → default to a hand-written fake; justify a mocking framework only for complex interaction verification.

**Table-driven test — shape to follow:**
```go
func TestParseID(t *testing.T) {
    tests := []struct {
        name    string
        input   string
        want    int
        wantErr bool
    }{
        {"valid", "42", 42, false},
        {"empty", "", 0, true},
        {"non-numeric", "abc", 0, true},
    }
    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            got, err := ParseID(tt.input)
            if (err != nil) != tt.wantErr {
                t.Fatalf("ParseID(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
            }
            if got != tt.want {
                t.Errorf("ParseID(%q) = %d, want %d", tt.input, got, tt.want)
            }
        })
    }
}
```

## Logging & Observability

- Use structured logging — `log/slog` (standard library, Go 1.21+) by default, or `zap`/`zerolog` if the project already depends on one. Do not introduce a new logging library alongside an existing one.
- Do not use `fmt.Println`/`fmt.Printf` for application logging — it produces unstructured output that cannot be filtered, leveled, or parsed downstream.
- Log at the appropriate level (`debug`/`info`/`warn`/`error`) and include structured fields (request ID, user ID, etc.) rather than interpolating context into the message string.
- This is a constraint check, not a mandate: do not add metrics/tracing infrastructure (Prometheus, OpenTelemetry, etc.) that the project has not already adopted — flag the gap in Architecture Notes if it's relevant, but don't introduce new observability tooling unprompted.
- **Flag if:** the feature adds new log statements → confirm they use the project's structured logger, not `fmt.Println`/`log.Println`.

## Linting & Formatting

- `gofmt` (or `goimports`, which also manages import grouping) formatting is non-negotiable — code that is not `gofmt`-clean should not be committed.
- `go vet` and `golangci-lint` (or the project's configured linter) are the standard static-analysis gate; a plan should assume CI runs these and account for any known project-specific lint config.
- **Flag if:** the feature is expected to introduce noticeable new lint surface (e.g. a new package, generated code) → confirm the plan accounts for running formatting/linting before the change is considered done.

## Output

For each constraint that applies, add to the plan's Architecture Notes:

```
Platform: <constraint summary> — <specific implication for this feature>
```

For constraints that do not apply:

```
Platform: <constraint> — not applicable (<one-word reason>)
```
