# Codex judge transport evidence

The manual judge uses a private native Codex invocation, separate from workflow,
Doctor and conversation sessions. The factory supplies a model catalogue for the
requested model with command execution disabled, no patch tool and no optional
model tools. Native feature gates disable the other execution, browser, image,
agent, application and plugin surfaces. Evidence and report operations use the
attempt's five scoped broker tools. The broker provides no MCP resources. The
factory preserves the exact model's bundled wire-protocol settings, context
limits and native default effort while replacing its tool capabilities and
instructions. Explicit effort must also appear in that client's bundled model
capabilities. An unknown literal model with effort unset keeps its exact name;
it uses native fallback metadata rather than substituting another model. A
bounded, credential-free version check admits the reviewed 0.160.x client family;
other versions fail visibly without authorizing an availability fallback.

On 2026-10-16, read-only inspection outside the repository found Codex CLI
**0.160.0**. `codex exec --help` advertised strict configuration, ignored user
configuration and rules, ephemeral execution and JSON output. In a temporary
directory with a fresh `HOME` and `CODEX_HOME`, without authentication or ambient
provider configuration, `codex debug models --bundled` returned the binary's
bundled model metadata. A second `codex debug models` invocation accepted a
factory catalogue with the same model slug and these tool controls:

```json
{
  "shell_type": "disabled",
  "apply_patch_tool_type": null,
  "experimental_supported_tools": [],
  "tool_mode": "direct",
  "node_repl_disabled": true,
  "supports_search_tool": false
}
```

Both local model commands exited zero. An initialize-only
`codex app-server --strict-config` probe also accepted the generated settings and
catalogue with empty stderr. It created no thread and started no turn. The first
strict parser check rejected an unknown `features.skills` setting; production
instead uses the supported controls for skipping host skill discovery and
disabling skill search and dependency installation.

These local checks did not execute a model turn,
inspect credentials, run a native sandbox or contact a forge. Catalogue parsing
and advertised command syntax establish compatibility of those controls with
this installation; they do not certify model quality or native enforcement.

The upstream [configuration schema](https://github.com/openai/codex/blob/main/codex-rs/core/config.schema.json)
describes the startup catalogue override. The upstream
[tool registry](https://github.com/openai/codex/blob/6ae8b8aa7c608730d4315d59c5c081897ae02780/codex-rs/core/src/tools/spec_plan.rs)
inspected during this repair registers the patch handler only when the selected
model's patch-tool type is present, and skips shell registration when the model's
shell type is disabled. These are source observations, not a substitute for
an installed-provider enforcement demonstration.

Automated checks use a controlled native executable through the production
adapter and shared judging service. They check launch controls, the private
catalogue, the scoped broker result route, successful completion and visible
failures. They never invoke paid providers. A native enforcement demonstration
with authenticated model execution remains a separate owner check. After a
client upgrade, repeat version/help and credential-free catalogue parsing,
review the tool registry and repeat that enforcement check before treating the
new version as certified.
