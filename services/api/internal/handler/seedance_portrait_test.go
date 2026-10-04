package handler

import "testing"

func TestSeedanceResultSupportsVolcengineEnvelope(t *testing.T) {
	got := seedanceResult(map[string]interface{}{
		"ResponseMetadata": map[string]interface{}{"RequestId": "req"},
		"Result":           map[string]interface{}{"GroupId": "group-1", "H5Link": "https://example.test/verify"},
	})
	if seedanceString(got, "groupid") != "group-1" || seedanceString(got, "h5link") != "https://example.test/verify" {
		t.Fatalf("unexpected normalized result: %#v", got)
	}
}

func TestSeedanceGatewayErrorReadsNestedProviderMessage(t *testing.T) {
	err := seedanceGatewayError(403, []byte(`{"ResponseMetadata":{"Error":{"Code":"InvalidAccessKey","Message":"access key is invalid"}}}`), nil)
	want := "网关 HTTP 403：InvalidAccessKey：access key is invalid"
	if err == nil || err.Error() != want {
		t.Fatalf("got %v, want %q", err, want)
	}
}
