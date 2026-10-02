package archive_test

import (
	"errors"
	"reflect"
	"testing"
)

// checker is a tiny stdlib-only stand-in for the assertion helpers; the
// module does not depend on testify.
type checker struct{ fatal bool }

var (
	assert  = checker{}
	require = checker{fatal: true}
)

func (c checker) fail(t testing.TB, format string, args ...any) {
	t.Helper()
	if c.fatal {
		t.Fatalf(format, args...)
	}
	t.Errorf(format, args...)
}

func (c checker) Equal(t testing.TB, want, got any, _ ...any) {
	t.Helper()
	if !reflect.DeepEqual(want, got) {
		c.fail(t, "want %#v, got %#v", want, got)
	}
}

func (c checker) NotEqual(t testing.TB, a, b any, _ ...any) {
	t.Helper()
	if reflect.DeepEqual(a, b) {
		c.fail(t, "values unexpectedly equal: %#v", a)
	}
}

func (c checker) NoError(t testing.TB, err error, _ ...any) {
	t.Helper()
	if err != nil {
		c.fail(t, "unexpected error: %v", err)
	}
}

func (c checker) Error(t testing.TB, err error, _ ...any) {
	t.Helper()
	if err == nil {
		c.fail(t, "expected an error")
	}
}

func (c checker) ErrorIs(t testing.TB, err, target error, _ ...any) {
	t.Helper()
	if !errors.Is(err, target) {
		c.fail(t, "error %v is not %v", err, target)
	}
}

func (c checker) Positive(t testing.TB, n int64, _ ...any) {
	t.Helper()
	if n <= 0 {
		c.fail(t, "want positive, got %d", n)
	}
}

func (c checker) Greater(t testing.TB, a, b int64, _ ...any) {
	t.Helper()
	if a <= b {
		c.fail(t, "want %d > %d", a, b)
	}
}

func (c checker) Zero(t testing.TB, v any, _ ...any) {
	t.Helper()
	if !reflect.ValueOf(v).IsZero() {
		c.fail(t, "want zero, got %v", v)
	}
}

func (c checker) Len(t testing.TB, v any, n int, _ ...any) {
	t.Helper()
	if l := reflect.ValueOf(v).Len(); l != n {
		c.fail(t, "want len %d, got %d", n, l)
	}
}

func (c checker) Empty(t testing.TB, v any, _ ...any) {
	t.Helper()
	if v != nil && reflect.ValueOf(v).Len() != 0 {
		c.fail(t, "want empty, got %v", v)
	}
}
