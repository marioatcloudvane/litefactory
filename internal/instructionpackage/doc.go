// Package instructionpackage supplies a versioned, checked instruction baseline,
// read-only owner overlays, and retained role captures. The production catalogue
// is explicitly empty: this foundation migrates no live prompt callers or text.
// Add real catalogue entries with the later role migrations and change the shipped
// version whenever shipped bytes change. Agent names are lowercase words joined
// by hyphens, without extensions, and select only agents/<name>.md.
//
// Package metadata.yaml contains schema: 1 and a nonblank version string;
// catalogue.yaml is a YAML sequence (the embedded foundation ships []). Files
// are agents/<name>.md. Metadata and each text file are limited to 1 MiB, total
// baseline/capture content to 16 MiB, and shipped inventory to 4096 files.
// Identity hashes sorted paths and exact bytes using length framing; whitespace
// changes therefore change the digest. The shipped version is release-managed.
//
// Owner content under config.Paths.InternalInstructionsDir is optional data.
// Updates never create, synchronize, overwrite, or delete owner files. Valid
// overrides win; invalid overrides select complete shipped content and diagnostics.
// The owner catalogue must list exactly the shipped roles (order may differ).
// An invalid or missing owner catalogue selects the entire shipped capture; an
// invalid or missing requested role selects the shipped role with its owner
// catalogue. An absent owner directory is a clean shipped selection. Diagnostics
// accompany a successful fallback and must be surfaced by callers through their
// existing logging. Reads are confined through an os.Root, reject symlinks in
// overlay paths, and discard changed candidates; no cross-file editing
// transaction is promised.
// Removed-role files can remain on disk without becoming callable roles.
//
// A caller must capture and complete its required durable retention before launch.
// Retained restore is independent of current files and fails closed on corruption.
// For example, call New(ctx, paths), Capture(ctx, role), then capture.Encode();
// persist the encoding before handing capture.Instructions() to the launcher.
// On replay call Restore(retainedBytes) without opening current sources.
// Integrity covers retained identity, provenance, diagnostics, and file hashes;
// it detects corruption but does not authenticate externally supplied records.
// Persistence belongs to each later caller migration. Later appended resources or
// skills must join the same validated captured closure and effective digest; they
// must not be reread live during execution. This package grants no tools, modes,
// credentials, gates, retries, result schemas, or execution authority: existing
// factory code continues to own those safeguards.
package instructionpackage
