package videoparams

import (
	"encoding/json"
	"testing"
)

func TestLegacyDolaPayloadCoexistsWithTopEnJSON(t *testing.T) {
	rule := map[string]interface{}{"upstream": map[string]interface{}{"adapter": "dola_seedance_30s", "include": []interface{}{"duration", "ratio", "reference_images"}}}
	got := BuildUpstreamVideoPayload("custom-dola", "legacy-model", rule, nil, map[string]interface{}{"prompt": "city sunrise", "duration": 30, "ratio": "16:9", "reference_images": []interface{}{"https://example.com/1.png", "https://example.com/2.jpg"}, "_price_rule_snapshot": map[string]interface{}{"unit_price": 1}, "model": "must-not-leak"})
	encoded, err := json.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	want := `{"prompt":"city sunrise","ratio":"16:9","reference_images":["https://example.com/1.png","https://example.com/2.jpg"],"seconds":"30"}`
	if string(encoded) != want {
		t.Fatalf("payload=%s want=%s", encoded, want)
	}
}
