// Package teampackage manages immutable machine-level team releases. It never
// opens a repository or database and treats downloaded resources only as data.
// Publication and removal share a cross-process lock with verified local reads.
// Workflow policy is supplied by the factory through a validation callback.
package teampackage
