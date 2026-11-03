package contracts

import "errors"

// ErrReadTargetInvalid means required retained factory identity is absent or inconsistent.
var ErrReadTargetInvalid = errors.New("factory: invalid retained read target")

// ReadTaskTarget carries authoritative retained factory identity.
type ReadTaskTarget struct {
	TaskRef, RepositoryRef string
	SessionID, RepoID      int64
}

// ReadUsageTarget carries authoritative retained factory identity.
type ReadUsageTarget struct {
	TaskRef, RepositoryRef string
	SessionID, RepoID      int64
}

// ReadRepository carries authoritative retained factory identity.
type ReadRepository struct {
	RepositoryRef string
	RepoID        int64
}

// ReadCodeTarget carries authoritative retained factory identity.
type ReadCodeTarget struct {
	TaskRef, RepositoryRef, ClonePath, RemoteURL, TaskBranch, BaseBranch, CodeRemoteID string
	SessionID, RepoID, CodeVersionID                                                   int64
}
