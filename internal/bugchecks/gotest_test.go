package bugchecks

import (
	"errors"
	"reflect"
	"strings"
	"testing"
)

const goTestFailingRun = `ok  	example.com/app/a	0.012s
--- FAIL: TestFlaky (0.01s)
    flaky_test.go:12: unlucky
--- FAIL: TestTable (0.00s)
    --- FAIL: TestTable/case_one (0.00s)
        table_test.go:30: wrong
    --- FAIL: TestTable/case_two (0.00s)
--- FAIL: TestFlaky (0.01s)
FAIL
FAIL	example.com/app/b	0.104s
?   	example.com/app/cmd	[no test files]
--- FAIL: ExampleRender (0.00s)
got:
x
want:
y
FAIL
FAIL	example.com/app/c/v2	0.020s
FAIL
`

func TestParseGoTestFailures(t *testing.T) {
	t.Parallel()
	r, err := ParseGoTestFailures(goTestFailingRun, true)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string][]string{"example.com/app/b": {"TestFlaky", "TestTable"}, "example.com/app/c/v2": {"ExampleRender"}}
	if !reflect.DeepEqual(r.Failed, want) || !reflect.DeepEqual(r.Passed, []string{"example.com/app/a"}) {
		t.Fatalf("report = %+v", r)
	}
	if r.FailedTests() != 3 || strings.Join(r.Packages(), ",") != "example.com/app/b,example.com/app/c/v2" {
		t.Fatalf("counts = %d %v", r.FailedTests(), r.Packages())
	}
	passing, err := ParseGoTestReport("ok  \texample.com/app/b\t0.011s\nok  \texample.com/app/c/v2\t0.020s\n", true)
	if err != nil || len(passing.Failed) != 0 || len(passing.Passed) != 2 {
		t.Fatalf("passing rerun = %+v %v", passing, err)
	}
	if _, err = ParseGoTestFailures("ok  \texample.com/app/b\t0.011s\n", true); !errors.Is(err, ErrUnattributable) {
		t.Fatalf("passing output attributed as a failure: %v", err)
	}
}

func TestParseGoTestFailuresUnattributable(t *testing.T) {
	t.Parallel()
	for name, output := range map[string]string{
		"build failure":  "# example.com/app/b\nb.go:3:1: syntax error\nFAIL\texample.com/app/b [build failed]\nFAIL\n",
		"setup failure":  "FAIL\texample.com/app/b [setup failed]\n",
		"timeout":        "panic: test timed out after 10m0s\n\trunning tests:\n\t\tTestSlow (10m0s)\n--- FAIL: TestSlow (600.00s)\nFAIL\texample.com/app/b\t600.1s\n",
		"unnamed panic":  "panic: boom\n\ngoroutine 1 [running]:\nFAIL\texample.com/app/b\t0.01s\n",
		"exit in test":   "FAIL\texample.com/app/b\t0.01s\n",
		"no summary":     "--- FAIL: TestCut (0.00s)\n",
		"no failure":     "",
		"bad test name":  "--- FAIL: TestX;rm (0.00s)\nFAIL\texample.com/app/b\t0.01s\n",
		"regexp name":    "--- FAIL: Test.* (0.00s)\nFAIL\texample.com/app/b\t0.01s\n",
		"non-test name":  "--- FAIL: helper (0.00s)\nFAIL\texample.com/app/b\t0.01s\n",
		"option package": "--- FAIL: TestX (0.00s)\nFAIL\t-exec=sh\t0.01s\n",
		"parent package": "--- FAIL: TestX (0.00s)\nFAIL\t../escape\t0.01s\n",
		"spaced package": "--- FAIL: TestX (0.00s)\nFAIL\texample.com/app b\t0.01s\n",
		"twice":          "--- FAIL: TestX (0.00s)\nFAIL\texample.com/app/b\t0.01s\n--- FAIL: TestY (0.00s)\nFAIL\texample.com/app/b\t0.01s\n",
		"no tests ran":   "--- FAIL: TestX (0.00s)\nFAIL\texample.com/app/b\t0.01s\nok  \texample.com/app/c\t0.01s [no tests to run]\n",
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if r, err := ParseGoTestFailures(output, true); !errors.Is(err, ErrUnattributable) {
				t.Fatalf("attributed %+v, %v", r, err)
			}
		})
	}
	if _, err := ParseGoTestFailures(goTestFailingRun, false); !errors.Is(err, ErrUnattributable) {
		t.Fatalf("incomplete capture attributed: %v", err)
	}
	var many strings.Builder
	for i := 0; i <= maxRerunTests; i++ {
		many.WriteString("--- FAIL: TestN" + strings.Repeat("x", i) + " (0.00s)\n")
	}
	many.WriteString("FAIL\texample.com/app/b\t0.01s\n")
	if _, err := ParseGoTestFailures(many.String(), true); !errors.Is(err, ErrUnattributable) {
		t.Fatalf("unbounded rerun attributed: %v", err)
	}
}

func TestExpandGoTestRerun(t *testing.T) {
	t.Parallel()
	failed := map[string][]string{"example.com/app/c/v2": {"ExampleRender"}, "example.com/app/b": {"TestTable", "TestFlaky"}}
	got, err := ExpandGoTestRerun([]string{"go", "test", "-count=1", "-run", "{tests}", "{packages}"}, failed)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"go", "test", "-count=1", "-run", "^(ExampleRender|TestFlaky|TestTable)$", "example.com/app/b", "example.com/app/c/v2"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("argv = %q, want %q", got, want)
	}
	for name, bad := range map[string]map[string][]string{
		"empty":        {},
		"no tests":     {"example.com/app/b": nil},
		"bad name":     {"example.com/app/b": {"Test|.*"}},
		"bad package":  {"-exec": {"TestX"}},
		"dot segments": {"example.com/../b": {"TestX"}},
	} {
		if _, err := ExpandGoTestRerun([]string{"go", "{tests}", "{packages}"}, bad); !errors.Is(err, ErrUnattributable) {
			t.Errorf("%s expanded: %v", name, err)
		}
	}
}
