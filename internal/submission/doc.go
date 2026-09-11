// Package submission implements the bounded local agent feedback transport.
// It owns no forge credentials, SQL, workflow decisions or retry clocks. The
// run orchestrator supplies an authorized handler and joins the broker lifetime.
package submission
