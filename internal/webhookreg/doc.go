// Package webhookreg manages installation-owned webhook subscriptions while
// keeping polling available when hook administration fails.
//
// Subscriptions saves version-qualified, revision-checked intent before dispatch
// and claims each remote operation durably. Lost responses remain uncertain and
// are reconciled only against an exact opaque installation-owned callback and
// nonsecret configuration. Disable blocks local admission immediately and repair
// preserves that intent. No provider can return the signing secret, so observed
// configuration never claims direct secret equality verification.
//
// Registrar is the legacy repository registration compatibility surface. New CLI
// operations use Subscriptions and connection-specific authenticated adapters;
// they do not infer remote ownership from a shared public listener URL.
package webhookreg
