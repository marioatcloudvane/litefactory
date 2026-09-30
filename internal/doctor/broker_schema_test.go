package doctor

import (
	"encoding/json"
	"testing"
)

func TestDiagnosisInputSchemaDescribesItemShapes(t *testing.T) {
	for _, repair := range []bool{false, true} {
		var schema struct {
			Properties struct {
				Diagnosis struct {
					Properties struct {
						Findings struct {
							Items struct {
								Required []string `json:"required"`
							} `json:"items"`
						} `json:"findings"`
					} `json:"properties"`
				} `json:"diagnosis"`
			} `json:"properties"`
		}
		if err := json.Unmarshal(diagnosisInputSchema(repair), &schema); err != nil {
			t.Fatalf("repair=%v: invalid schema JSON: %v", repair, err)
		}
		got := schema.Properties.Diagnosis.Properties.Findings.Items.Required
		if len(got) != 3 || got[0] != "explanation" || got[1] != "evidence" || got[2] != "uncertainty" {
			t.Fatalf("repair=%v: finding shape not described: %v", repair, got)
		}
	}
}
