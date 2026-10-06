// Package rights manages repository rights and reviewer assignments for graph
// actions that are explicitly tagged lf_rights, and answers one shared
// question: may this person take this action here?
//
// Only tagged actions are covered. Rights live in the database and change only
// through the owner's local commands; every change is written to an
// append-only history in the same transaction.
package rights
