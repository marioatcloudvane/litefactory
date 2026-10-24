package messaging

import "testing"

func TestStandardsQuestionReferencesStayExactAndBounded(t *testing.T) {
	t.Parallel()
	ref := ConversationReference{ID: "saved-session", Arguments: map[string]string{"kind": "author", "id": "draft-1", "version": "3", "question_id_2": "q-2", "question_id_1": "q-1"}}
	refs := expandStandardsReferences([]ConversationReference{ref})
	if len(refs) != 3 || refs[1].Arguments["question_id"] != "q-1" || refs[2].Arguments["question_id"] != "q-2" {
		t.Fatalf("ambiguous question references: %+v", refs)
	}
	if len(expandStandardsReferences(refs)) != 3 {
		t.Fatal("repeated lookup duplicated question references")
	}
	for _, q := range refs[1:] {
		if q.Arguments["id"] != "draft-1" || q.Arguments["version"] != "3" {
			t.Fatal("question reference lost exact session version")
		}
	}
}
