# Blazor practices

Respect the existing repository's hosting and render modes. For new Blazor Web App
work default to Interactive Server, recording the tradeoff for the current story.
Static SSR provides request/response rendering without an interactive circuit;
use server form handling and progressive enhancement where appropriate. Interactive
Server runs component behavior on the server: account for scoped circuit lifetimes,
disconnects, reconnection and per-user memory. Interactive WebAssembly executes in
the browser and needs a client project and explicit API trust boundary. Auto may
start on the server and later run in the browser; don't assume identical service
instances or persistent in-memory state across hosts. Standalone WebAssembly uses
browser execution without Blazor Web App server render modes. Do not migrate an
existing mode merely because this team was selected.

Keep Razor components cohesive, with explicit parameters, callbacks and ownership
of state. Avoid hidden shared mutable state. Respect render-mode boundaries and
serializable parameters. Use stable keys for repeated stateful content. Separate
presentation from reusable behavior when the repository already does so; do not
introduce a new application architecture solely to satisfy a pattern.

Prerendering may initialize a component more than once. Make initialization safe,
use persisted prerender state where appropriate and avoid duplicate side effects.
DOM-dependent JavaScript interop belongs after interactive rendering, commonly in
OnAfterRenderAsync with a first-render guard. Avoid infinite rendering loops.
Implement disposal for subscriptions, timers, cancellation sources and JS object
references. Circuit disconnects can make disposal interop unavailable; handle that
case without swallowing unrelated errors.

Await asynchronous work. Avoid async void except unavoidable event contracts and
avoid blocking waits. Use cancellation for superseded loads and disposal, expose
loading/error states, and guard against stale async results overwriting newer state.
Use InvokeAsync for state changes originating outside the renderer context. Do not
hide failures behind indefinite spinners or fake success.

Respect DI lifetimes: server scoped services can live for an entire circuit;
browser scoped services generally live for the browser application. Do not retain
user-specific data in singletons. Resolve per-operation services appropriately and
follow the existing repository's data access patterns; don't prescribe a database.

Use EditForm and the existing validation model, accessible labels, validation
summaries and meaningful field errors. Static SSR forms need explicit server form
binding and antiforgery handling. Treat browser input as untrusted and repeat
validation on the server. Preserve keyboard navigation, focus management, semantic
HTML, accessible names, contrast and meaningful status announcements. Test loading,
empty, error and reconnect behavior as part of the user-facing feature.

Choose state ownership deliberately: local component state, shared scoped state,
URL state and durable state solve different lifetimes. Document refresh and circuit
loss behavior. Never put secrets in browser assemblies, local storage or rendered
markup. Client visibility, AuthorizeView and hidden controls are not authorization:
enforce permission and ownership checks at server endpoints and service boundaries.
Keep authentication tokens and interop data within existing trust boundaries.
Avoid raw HTML injection and unsafe script construction. Use narrow JS modules,
validate inputs and dispose references; do not let browser interop widen authority.

Use the repository's unit and component tools for deterministic behavior, lifecycle,
forms and state transitions. Use browser checks when DOM, accessibility, focus or
real interactivity matters; server circuit/reconnect behavior needs relevant server
and browser evidence. Component tests alone cannot establish browser or circuit
behavior. Record what actually ran, its target and runner, and limitations.
Package/profile validation proves contracts only; it proves neither an application
build nor a live Claude/Codex workflow. Validate only reviews retained evidence and
must not rerun or commission checks. Approval, merge and publication remain factory
owned in every mode.
