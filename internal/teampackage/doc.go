// Package teampackage manages fetched immutable releases and editable local clones. It never
// opens a repository or database and treats downloaded resources only as data.
// Publication and removal share a cross-process lock with verified local reads.
// Workflow policy is supplied by the factory through a validation callback.
package teampackage
