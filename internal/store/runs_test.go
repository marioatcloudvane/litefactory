package store

import "testing"

// TestRun_Succeeded is US-4 (T-4.5, AC-4.1): [Run.Succeeded] is the durable,
// stored-row predicate a resumed run's prompt is built from — distinct from
// [agent.RunResult.Succeeded], which this test never touches. It is a pure
// function of Status and Error, so the table runs every status against both
// an empty and a non-empty Error without touching the database at all.
func TestRun_Succeeded(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		run  Run
		want bool
	}{
		{name: "succeeded, no error", run: Run{Status: RunSucceeded, Error: ""}, want: true},
		{name: "succeeded, error somehow set", run: Run{Status: RunSucceeded, Error: "should not happen"}, want: true},
		{name: "reported, empty error", run: Run{Status: RunReported, Error: ""}, want: true},
		{name: "reported, non-empty error", run: Run{Status: RunReported, Error: "claude exited with status 2"}, want: false},
		{name: "failed, no error", run: Run{Status: RunFailed, Error: ""}, want: false},
		{name: "failed, with error", run: Run{Status: RunFailed, Error: ReasonAbandoned}, want: false},
		{name: "running, no error", run: Run{Status: RunRunning, Error: ""}, want: false},
		{name: "running, error somehow set", run: Run{Status: RunRunning, Error: "should not happen"}, want: false},
		{name: "unrecognised status", run: Run{Status: RunStatus("bogus"), Error: ""}, want: false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := tc.run.Succeeded()

			if got != tc.want {
				t.Errorf("Run{Status: %q, Error: %q}.Succeeded() = %t, want %t",
					tc.run.Status, tc.run.Error, got, tc.want)
			}
		})
	}
}
