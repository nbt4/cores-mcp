package mcpserver

import "testing"

func TestRankMasterCandidatesDistinguishesCodeAndAmbiguity(t *testing.T) {
	rows := []map[string]any{
		{"id": int64(1), "name": "MA Lighting International GmbH", "code": "MAL", "context": "MAL"},
		{"id": int64(2), "name": "MA Lighting", "code": "MA2", "context": "MA2"},
		{"id": int64(3), "name": "Other Supplier", "code": "OTHER", "context": "OTHER"},
	}
	byCode := rankMasterCandidates(rows, "MAL", 10)
	resolution, selected := classifyMasterCandidates(byCode)
	if resolution != "exact" || selected["id"] != int64(1) || byCode[0]["match_kind"] != "exact" {
		t.Fatalf("code resolution = %q, selected = %#v, candidates = %#v", resolution, selected, byCode)
	}
	byName := rankMasterCandidates(rows, "MA Lighting", 10)
	resolution, selected = classifyMasterCandidates(byName)
	if resolution != "exact" || selected["id"] != int64(2) {
		t.Fatalf("name resolution = %q, selected = %#v", resolution, selected)
	}
	miss := rankMasterCandidates(rows, "unrelated", 10)
	resolution, selected = classifyMasterCandidates(miss)
	if resolution != "not_found" || selected != nil {
		t.Fatalf("missing resolution = %q, selected = %#v", resolution, selected)
	}
	resolution, selected = classifyMasterCandidates([]map[string]any{{"match_kind": "similar"}, {"match_kind": "similar"}})
	if resolution != "ambiguous" || selected != nil {
		t.Fatalf("ambiguous resolution = %q, selected = %#v", resolution, selected)
	}
}
