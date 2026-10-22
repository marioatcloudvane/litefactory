package graphexec_test

import (
	"fmt"
	"strconv"
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/graphexec"
)

func TestSlotCostPrecedence(t *testing.T) {
	for _, tc := range []struct {
		name, decl, attrs string
		want              int
	}{
		{"omitted", "", "", 1},
		{"graph", `lf_slot_budget="2";`, "", 2},
		{"node default", `lf_slot_budget="2"; node [lf_slot_budget="3"];`, "", 3},
		{"node override", `lf_slot_budget="2"; node [lf_slot_budget="3"];`, `,lf_slot_budget="4"`, 4},
		{"leading zero", "", `,lf_slot_budget="003"`, 3},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p, err := graphexec.ParseProfile([]byte(`digraph {team=test;mode=implement;profile_version=1;start=work;` + tc.decl + `work [lf_action=lf_implement_work,lf_outcomes=done,lf_budget="7m"` + tc.attrs + `]; end [lf_terminal=true]; work -> end [on=done];}`))
			if err != nil {
				t.Fatal(err)
			}
			if err = graphexec.Validate(p, registry(t)); err != nil {
				t.Fatal(err)
			}
			got, err := graphexec.SlotCost(p, "work")
			if err != nil || got != tc.want {
				t.Fatalf("cost=%d err=%v want=%d", got, err, tc.want)
			}
			terminal, err := graphexec.SlotCost(p, "end")
			if err != nil || terminal != 0 {
				t.Fatalf("terminal=%d err=%v", terminal, err)
			}
			if p.Nodes[0].Attrs["lf_budget"] != "7m" {
				t.Fatal("time budget changed")
			}
		})
	}
}

func TestSlotCostRejectsEverySuppliedInvalidValue(t *testing.T) {
	for _, value := range []string{"", "0", "-1", "+1", "1.5", " 2", "2 ", "NaN", strconv.FormatUint(^uint64(0), 10)} {
		for _, decl := range []string{
			`lf_slot_budget=%q; node [lf_slot_budget="3"]; work [lf_slot_budget="4"];`,
			`graph [lf_slot_budget=%q]; graph [lf_slot_budget="2"];`,
			`node [lf_slot_budget=%q]; node [lf_slot_budget="2"];`,
			`node [lf_slot_budget=%q]; work [lf_slot_budget="2"];`,
			`work [lf_slot_budget=%q,lf_slot_budget="2"];`,
			`end [lf_terminal=true,lf_slot_budget=%q];`,
		} {
			_, err := graphexec.ParseProfile([]byte(`digraph {` + fmt.Sprintf(decl, value) + `}`))
			if err == nil || !strings.Contains(err.Error(), "lf_slot_budget") {
				t.Fatalf("value=%q decl=%s err=%v", value, decl, err)
			}
		}
	}
}

func TestSlotCostRefusesMissingAuthorityAndInvalidProfiles(t *testing.T) {
	if _, err := graphexec.SlotCost(nil, "work"); err == nil {
		t.Fatal("nil profile accepted")
	}
	p := profile()
	if _, err := graphexec.SlotCost(p, "missing"); err == nil {
		t.Fatal("missing node accepted")
	}
	p.Attrs = map[string]string{"lf_slot_budget": "0"}
	p.Nodes[0].Attrs["lf_slot_budget"] = "3"
	if _, err := graphexec.SlotCost(p, "work"); err == nil {
		t.Fatal("invalid overridden graph value accepted")
	}
	if err := graphexec.Validate(p, registry(t)); err == nil || !strings.Contains(err.Error(), "lf_slot_budget") {
		t.Fatalf("validation=%v", err)
	}
}
