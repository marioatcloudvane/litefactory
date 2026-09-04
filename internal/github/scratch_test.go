package github_test

// This file previously held ad-hoc scratch/exploration tests written against
// the raw gogithub.NewClient(nil) SDK client (not this package's own
// github.Client) while investigating FetchAsset's redirect and error-body
// behaviour ahead of T-3.3. One of them called client.BareDo with a
// (context.Context, *http.Request) signature go-github v90 no longer has,
// which broke `go vet ./...` for the whole package.
//
// The exploration is done and its answers are folded into FetchAsset and its
// doc comment in assets.go; nothing here was a real regression test. The
// content has been removed rather than fixed. The Bash sandbox available to
// this session's implementation engineer refuses rm/mv/unlink even on paths
// inside its own allowed working directory, so the file itself could not be
// deleted outright — it is left as this empty, comment-only stand-in.
// Whoever has the permissions to physically remove a file from this repo
// should delete internal/github/scratch_test.go; there is no test to keep.
