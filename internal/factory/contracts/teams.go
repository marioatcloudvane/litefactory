package contracts

import "time"

// TeamIdentityView identifies an installed team by exact source and version.
type TeamIdentityView struct{ Source, Slug, Version, Digest string }

// TeamOriginView contains the immutable history of a local team clone.
type TeamOriginView struct {
	Source, Slug, Version, Checksum string
	ClonedAt                        time.Time
}

// TeamEntryView contains safe installed team and workflow metadata.
type TeamEntryView struct {
	Identity  TeamIdentityView
	Path      string
	Workflows []string
	Editable  bool
	Origin    *TeamOriginView
	Build     *TeamBuildView
}

// TeamBuildView names the source checkout an installed team was built from.
type TeamBuildView struct {
	SourceHead  string
	SourceDirty bool
}

// TeamInstallView reports one install from a source folder.
type TeamInstallView struct {
	Entry TeamEntryView
	// Outcome is "installed", "unchanged" or "replaced".
	Outcome string
	// Rebound lists the registered clones whose stored digest a replacement
	// moved to the new bytes.
	Rebound []string
}

// TeamBindingRequest selects a registered clone and installed team version.
type TeamBindingRequest struct{ RepoPath, Source, Slug, Version string }

// TeamBindingView describes a local repository team binding outcome.
type TeamBindingView struct {
	RepoPath, Source, Slug, Version string
	Changed, Unlinked               bool
}

// TeamRepositoryUnavailable marks an unregistered clone during team binding.
type TeamRepositoryUnavailable struct{ Path string }

func (e *TeamRepositoryUnavailable) Error() string {
	return "repository " + e.Path + " is not registered; use `litefactory repo add " + e.Path + "`"
}
