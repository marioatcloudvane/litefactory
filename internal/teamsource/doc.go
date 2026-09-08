// Package teamsource loads centrally installed team definitions and captures
// exact mode rosters with their transitive skill resources. Resolve never
// consults a repository or falls back to embedded defaults. Explicit management
// fetches independently released web content or imports a local package.
//
// Capture holds the same management lock as catalog writers. os.Root confines
// reads to the catalog even through symlinks; bounded regular-file reads,
// frontmatter agreement and a second byte comparison reject unsafe or changing
// input before rendering. Snapshot files retain their catalog-relative trees.
// Skill requires entries may reference another skill ID or a relative Markdown
// path; resources are relative paths. Every transitive byte affects the digest.
//
// This package is the bridge from team metadata to render's pure adapters;
// neither team nor render needs knowledge of source acquisition.
package teamsource
