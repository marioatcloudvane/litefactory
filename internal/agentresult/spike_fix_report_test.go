package agentresult

import (
	"strings"
	"testing"
)

func writeContext() Context {
	c := spikeContext(true)
	c.WriteAccess = true
	return c
}

func fixEnvelope(report string) string {
	spike := `"request":"r","work_done":["edited"]`
	if report != "" {
		spike += `,"fix_report":` + report
	}
	return `{"version":1,"response":"done","result":{"outcome":"completed","comment":"Fixed.","payload":{"spike":{` + spike + `}}}}`
}

const validFixReport = `{"defect":"wrong flag","files":["a.go","a_test.go"],"regression_test":"a_test.go","change_kinds":["api"],"summary":"corrected"}`

func TestFixReportIsRequiredOnAFinishedWriteNodeResultOnly(t *testing.T) {
	if _, d := DecodeSpikeNode([]byte(fixEnvelope(validFixReport)), writeContext()); !d.Valid || !d.Complete {
		t.Fatalf("valid fix report refused: %+v", d)
	}
	if _, d := DecodeSpikeNode([]byte(fixEnvelope("")), writeContext()); d.Valid {
		t.Fatal("a finished write-node result without a fix report was accepted")
	}
	// A node that could not edit has nothing to claim.
	if _, d := DecodeSpikeNode([]byte(fixEnvelope(validFixReport)), spikeContext(true)); d.Valid {
		t.Fatal("a fix report from a read-only node was accepted")
	}
	if _, d := DecodeSpikeNode([]byte(fixEnvelope("")), spikeContext(true)); !d.Valid {
		t.Fatalf("a read-only node result needs no fix report: %+v", d)
	}
	// An unfinished result from a write node is not a claim of a change.
	if _, d := DecodeSpikeNode([]byte(spikeAskEnvelope), writeContext()); !d.Valid {
		t.Fatalf("an ask from a write node refused: %+v", d)
	}
}

func TestFixReportFieldsAreEnforced(t *testing.T) {
	for name, tc := range map[string]struct{ from, to string }{
		"blank defect":            {`"defect":"wrong flag"`, `"defect":" "`},
		"blank summary":           {`"summary":"corrected"`, `"summary":""`},
		"blank regression test":   {`"regression_test":"a_test.go"`, `"regression_test":"  "`},
		"test outside the files":  {`"regression_test":"a_test.go"`, `"regression_test":"b_test.go"`},
		"no files":                {`"files":["a.go","a_test.go"],"regression_test":"a_test.go"`, `"files":[],"regression_test":"a_test.go"`},
		"duplicate file":          {`"files":["a.go","a_test.go"]`, `"files":["a.go","a.go","a_test.go"]`},
		"unsafe path":             {`"files":["a.go","a_test.go"]`, `"files":["../a.go","a_test.go"]`},
		"unknown kind":            {`"change_kinds":["api"]`, `"change_kinds":["code"]`},
		"duplicate kind":          {`"change_kinds":["api"]`, `"change_kinds":["api","api"]`},
		"unknown member":          {`"summary":"corrected"`, `"summary":"corrected","status":"ok"`},
		"authored check outcome":  {`"summary":"corrected"}`, `"summary":"corrected"},"check":{"outcome":"passed"}`},
		"authored filing outcome": {`"summary":"corrected"}`, `"summary":"corrected"},"issue":{"state":"filed"}`},
	} {
		t.Run(name, func(t *testing.T) {
			envelope := fixEnvelope(validFixReport)
			if !strings.Contains(envelope, tc.from) {
				t.Fatalf("fixture lacks %q", tc.from)
			}
			envelope = strings.Replace(envelope, tc.from, tc.to, 1)
			if _, d := DecodeSpikeNode([]byte(envelope), writeContext()); d.Valid {
				t.Fatalf("accepted: %s", envelope)
			}
		})
	}
	if _, d := DecodeSpikeNode([]byte(fixEnvelope(strings.Replace(validFixReport, `,"change_kinds":["api"]`, "", 1))), writeContext()); !d.Valid {
		t.Fatalf("omitting the risky kinds must be valid: %+v", d)
	}
}
