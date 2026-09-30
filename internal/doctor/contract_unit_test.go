package doctor_test

import (
	"testing"

	"github.com/cloudvane/litefactory/internal/doctor"
	"github.com/cloudvane/litefactory/internal/naming"
)

func TestDoctorTarget(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name, repository string
		issue            int
		want             string
	}{
		{"explicit issue/canonical identity", "Owner/Repo", 406, "github:owner/repo#406"},
		{"different issue/retains selected identity", "owner/repo", 432, "github:owner/repo#432"},
		{"missing repository/rejected", "", 1, ""},
		{"missing owner/rejected", "/repo", 1, ""},
		{"missing name/rejected", "owner/", 1, ""},
		{"bare repository/rejected", "repo", 1, ""},
		{"foreign forge/rejected", "gitlab:owner/repo", 1, ""},
		{"full URL/rejected", "https://github.com/owner/repo", 1, ""},
		{"path traversal/rejected", "../repo", 1, ""},
		{"extra path/rejected", "owner/repo/other", 1, ""},
		{"whitespace/rejected", "owner/repo ", 1, ""},
		{"zero issue/rejected", "owner/repo", 0, ""},
		{"negative issue/rejected", "owner/repo", -1, ""},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := doctor.Target(tt.repository, tt.issue)
			if tt.want == "" {
				if err == nil || got != (naming.Ref{}) {
					t.Fatalf("Target returned %v, %v; want empty identity and error", got, err)
				}
				return
			}
			if err != nil || got.String() != tt.want {
				t.Fatalf("Target returned %v, %v; want %s", got, err, tt.want)
			}
		})
	}
}
