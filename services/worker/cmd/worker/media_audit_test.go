package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/starai/worker/internal/storage"
)

func TestMediaDownloadRedirectDoesNotForwardGatewayCredentials(t *testing.T) {
	cdn := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		for _, header := range []string{"Authorization", "X-API-Key", "X-Custom-Secret"} {
			if r.Header.Get(header) != "" {
				t.Errorf("gateway credential %s leaked on CDN redirect", header)
			}
		}
		w.Header().Set("Content-Type", "audio/mpeg")
		w.Write([]byte("ID3audio"))
	}))
	defer cdn.Close()
	gateway := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-API-Key") != "private-key" || r.Header.Get("X-Custom-Secret") != "secret" {
			t.Error("initial gateway authentication missing")
		}
		http.Redirect(w, r, cdn.URL+"/signed.mp3", http.StatusFound)
	}))
	defer gateway.Close()
	conn := connectionConfig{BaseURL: gateway.URL, AuthType: "api_key_header", APIKeyHeader: "X-API-Key", APIKey: "private-key", Headers: map[string]string{"Authorization": "Bearer private", "X-Custom-Secret": "secret"}}
	data, _, err := downloadAuthenticatedMedia(context.Background(), conn, gateway.URL+"/content", 1024)
	if err != nil || string(data) != "ID3audio" {
		t.Fatalf("signed CDN redirect failed: %q %v", data, err)
	}
}

func TestWorkerBillingRejectsNonFiniteAmountsBeforeDatabase(t *testing.T) {
	for _, invalid := range []float64{math.NaN(), math.Inf(1), math.Inf(-1)} {
		if err := chargeBillingWithFinalize(context.Background(), nil, 1, 1, invalid, "task", "id", "charge", "", nil); err == nil {
			t.Fatal("invalid actual amount accepted")
		}
		if err := chargeBillingWithFinalize(context.Background(), nil, 1, invalid, 1, "task", "id", "charge", "", nil); err == nil {
			t.Fatal("invalid frozen amount accepted")
		}
	}
	maximum := math.MaxFloat64
	if finiteWorkerMoney(maximum - (-maximum)) {
		t.Fatal("overflow is finite")
	}
}

func TestWorkerBillingRoundsToWalletPrecision(t *testing.T) {
	for _, test := range []struct{ input, want float64 }{
		{0.00000001, 0}, {0.0000004, 0}, {0.0000005, 0.000001},
		{0.0000014, 0.000001}, {1.23456789, 1.234568},
	} {
		if actual := roundWorkerBillingAmount(test.input); actual != test.want {
			t.Fatalf("round(%v)=%v want %v", test.input, actual, test.want)
		}
	}
	if err := chargeBillingWithFinalize(context.Background(), nil, 1, 1, math.MaxFloat64, "task", "id", "charge", "", nil); err == nil {
		t.Fatal("scaled overflow accepted")
	}
}

func TestDynamicMediaBillingActualZeroSeconds(t *testing.T) {
	for _, estimate := range []func(map[string]interface{}, map[string]interface{}) float64{estimateMiniMaxH3PriceRuleCostWorker, estimateSeedance2PriceRuleCostWorker} {
		rule := map[string]interface{}{}
		baseline := estimate(rule, map[string]interface{}{"duration": 5})
		if baseline <= 0 {
			t.Fatalf("missing usage must retain estimate: %v", baseline)
		}
		if zero := estimate(rule, map[string]interface{}{"duration": 5, "_actual_output_seconds": 0}); zero != 0 {
			t.Fatalf("actual zero seconds replaced by duration: %v", zero)
		}
		for _, invalid := range []interface{}{nil, "bad", -1, math.NaN(), math.Inf(1)} {
			if actual := estimate(rule, map[string]interface{}{"duration": 5, "_actual_output_seconds": invalid}); actual != baseline {
				t.Fatalf("invalid seconds %v changed fallback: %v vs %v", invalid, actual, baseline)
			}
		}
	}
}

func TestSeedanceActualZeroTokensAndVideoInputSeconds(t *testing.T) {
	usage := upstreamUsageFromBody([]byte(`{"usage":{"input_tokens":0,"output_tokens":0,"total_tokens":0}}`))
	params := inputWithActualUpstreamUsage(map[string]interface{}{"duration": 5, "reference_videos": []string{"https://media.test/input.mp4"}}, usage)
	if value, valid := mediaUsageTokens(params, "_actual_video_tokens"); !valid || value != 0 {
		t.Fatal("real zero token usage was lost")
	}
	if cost := estimateSeedance2PriceRuleCostWorker(map[string]interface{}{}, params); cost != 0 {
		t.Fatalf("actual zero tokens charged estimate: %v", cost)
	}
	params = map[string]interface{}{"duration": 5, "reference_videos": []string{"https://media.test/input.mp4"}, "_actual_input_seconds": 0, "_actual_output_seconds": 0}
	if cost := estimateSeedance2PriceRuleCostWorker(map[string]interface{}{}, params); cost != 0 {
		t.Fatalf("actual zero video seconds charged default input duration: %v", cost)
	}
}

func TestWorkerActualProviderRequestAndOutputImageCounts(t *testing.T) {
	input := map[string]interface{}{"n": 3, "count": 3, "_actual_request_count": 99, "_actual_output_image_count": 99, "_price_rule_snapshot": map[string]interface{}{"billing_type": "per_image", "unit_price": 2.0}}
	actual := workerActualOutputBillingInput(input, 1, 1)
	requestRoute := workerModelRoute{CostRule: map[string]interface{}{"billing_type": "per_request", "unit_cost": 0.5}}
	imageRoute := workerModelRoute{CostRule: map[string]interface{}{"billing_type": "per_image", "unit_cost": 0.25}}
	if cost := workerRouteProviderCost(requestRoute, actual, 0, 0, 0, 0); cost != 0.5 {
		t.Fatalf("one POST with n=3 should cost one request: %v", cost)
	}
	if cost := workerRouteProviderCost(imageRoute, actual, 0, 0, 0, 0); cost != 0.25 {
		t.Fatalf("one persisted output should cost one image: %v", cost)
	}
	if cost := estimatePriceRuleCostWorker(input["_price_rule_snapshot"].(map[string]interface{}), actual, 0, 0, 0, 0); cost != 2 {
		t.Fatalf("sale charged requested images instead of actual output: %v", cost)
	}
	if !shouldRepriceMediaUsage(upstreamUsageDetails{}, actual) {
		t.Fatal("actual output image count did not trigger repricing")
	}
	batch := workerActualOutputBillingInput(input, 3, 3)
	if cost := workerRouteProviderCost(requestRoute, batch, 0, 0, 0, 0); cost != 1.5 {
		t.Fatalf("three Banana generation POSTs should cost three requests: %v", cost)
	}
	if cost := workerRouteProviderCost(requestRoute, map[string]interface{}{"n": 3}, 0, 0, 0, 0); cost != 0.5 {
		t.Fatalf("missing actual request count must estimate one POST: %v", cost)
	}
	if cleaned := workerActualOutputBillingInput(input, 1, -1); cleaned["_actual_output_image_count"] != nil {
		t.Fatal("non-image attempt retained untrusted image count")
	}
}

func TestSuccessfulStatusIgnoresStalePlainError(t *testing.T) {
	rule := map[string]interface{}{"upstream": map[string]interface{}{"success_statuses": []string{"READY"}}}
	body := []byte(`{"status":"READY","error":"stale provider failure","url":"https://media.test/result.png"}`)
	if message := configuredMediaBusinessError(body, rule); message != "" {
		t.Fatalf("configured success rejected due to stale metadata: %s", message)
	}
	if items, _ := parseUpstreamMediaWithRule(body, rule); len(items) != 1 {
		t.Fatal("configured success media was not parsed")
	}
}

func TestLegacyMiniMaxSynchronousStatusTwoAudio(t *testing.T) {
	for _, audio := range []string{hex.EncodeToString([]byte("ID3\x04audio-audio-audio-audio-audio-audio")), "https://example.test/speech.mp3"} {
		body := []byte(fmt.Sprintf(`{"data":{"audio":%q,"status":2},"base_resp":{"status_code":0,"status_msg":"success"}}`, audio))
		items, id := parseUpstreamMediaWithRule(body, nil)
		if len(items) != 1 || id != "" || (items[0].URL != audio && items[0].B64JSON != audio) {
			t.Fatalf("synchronous completed audio rejected: %#v id=%q", items, id)
		}
	}
	items, _ := parseUpstreamMedia([]byte(`{"id":"async-task","status":2,"images":["https://example.test/reference.png"]}`))
	if len(items) != 0 {
		t.Fatalf("asynchronous input echo was accepted: %#v", items)
	}
}

func TestExplicitPollPathWinsResponseURLAndResponseFallbackRemains(t *testing.T) {
	body := []byte(`{"id":"job","poll_url":"/returned/jobs/job"}`)
	rule := map[string]interface{}{"upstream": map[string]interface{}{"poll_path": "/configured/jobs/{id}"}}
	if config := resolveMediaPollConfig(rule, "/create", body, "https://gateway.test"); config.Path != "/configured/jobs/{id}" {
		t.Fatalf("configured poll overwritten: %s", config.Path)
	}
	if config := resolveMediaPollConfig(nil, "/create", body, "https://gateway.test"); config.Path != "/returned/jobs/job" {
		t.Fatalf("returned fallback lost: %s", config.Path)
	}
}

func TestCustomMediaDownloadPathAndVersionedBaseURL(t *testing.T) {
	conn := connectionConfig{BaseURL: "https://gateway.test/v1"}
	if fallback := firstSuccessMediaURL(map[string]interface{}{"status": "success"}, "job", conn); fallback != "https://gateway.test/v1/videos/job/content" {
		t.Fatalf("duplicated API version: %s", fallback)
	}
	candidates := buildMediaDownloadCandidates(conn, "https://gateway.test/custom/result/job.mp4", "job")
	if len(candidates) != 2 || candidates[0] != "https://gateway.test/custom/result/job.mp4" || candidates[1] != "https://gateway.test/v1/videos/job/content" {
		t.Fatalf("custom download URL lost priority: %#v", candidates)
	}
}

func TestCanceledDownloadStopsImmediately(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	started := time.Now()
	_, _, err := downloadAuthenticatedMedia(ctx, connectionConfig{}, "https://unused.invalid/result.mp4", 1024)
	if err == nil || time.Since(started) > time.Second {
		t.Fatalf("canceled download did not stop: %v", err)
	}
}

func TestBase64StorageChecksMediaBytesAndRequiresStorage(t *testing.T) {
	old := objectStore
	defer func() { objectStore = old }()
	objectStore = nil
	if _, err := storeBase64MediaResult(context.Background(), "fixture", 1, base64.StdEncoding.EncodeToString([]byte("hello")), "", "image"); err == nil {
		t.Fatal("missing storage must fail Base64 persistence")
	}
	store, err := storage.NewLocal(t.TempDir(), "https://storage.test/uploads")
	if err != nil {
		t.Fatal(err)
	}
	objectStore = store
	for _, kind := range []string{"image", "audio"} {
		if _, err := storeBase64MediaResult(context.Background(), "fixture", 1, base64.StdEncoding.EncodeToString(bytes.Repeat([]byte("invalid media text"), 3)), "", kind); err == nil {
			t.Fatalf("invalid %s bytes accepted using default MIME", kind)
		}
	}
	png := []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n', 0, 0, 0, 0, 'I', 'H', 'D', 'R'}
	url, err := storeBase64MediaResult(context.Background(), "fixture", 1, base64.StdEncoding.EncodeToString(png), "", "image")
	if err != nil || !strings.HasSuffix(url, ".png") {
		t.Fatalf("Base64 media did not persist: %s %v", url, err)
	}
	data, err := store.ReadAll(context.Background(), "works/image/fixture/1.png", 1024)
	if err != nil || !bytes.Equal(data, png) {
		t.Fatalf("persisted bytes changed: %q err=%v", data, err)
	}
}

func TestPlainBusinessErrorCannotBecomeSuccessfulMedia(t *testing.T) {
	body := []byte(`{"error":"provider rejected request","images":["https://example.test/input.png"]}`)
	if message := configuredMediaBusinessError(body, nil); message != "provider rejected request" {
		t.Fatalf("error diagnostic=%q", message)
	}
	if items, _ := parseUpstreamMedia(body); len(items) > 0 {
		t.Fatalf("business error accepted: %#v", items)
	}
}

func TestRawAACKeepsItsActualFormat(t *testing.T) {
	aac := []byte{0xff, 0xf1, 0x50, 0x80, 0x01, 0x7f, 0xfc, 0x00}
	items, _ := parseUpstreamMedia(aac)
	if len(items) != 1 || items[0].MimeType != "audio/aac" || mediaExtForContentType(items[0].MimeType, "audio") != ".aac" {
		t.Fatalf("ADTS AAC mislabeled: %#v", items)
	}
}

func TestCustomResultURLDownloadsAndPersistsWithGatewayAuth(t *testing.T) {
	old := objectStore
	defer func() { objectStore = old }()
	store, err := storage.NewLocal(t.TempDir(), "https://storage.test/uploads")
	if err != nil {
		t.Fatal(err)
	}
	objectStore = store
	var paths []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path)
		if r.URL.Path != "/custom/output/job.mp4" {
			t.Errorf("requested unrelated Sora fallback: %s", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Token fixture" {
			t.Errorf("gateway media auth missing")
		}
		w.Header().Set("Content-Type", "video/mp4")
		_, _ = w.Write([]byte("\x00\x00\x00\x18ftypmp42fixture-video"))
	}))
	defer server.Close()
	stored, err := mirrorUpstreamMedia(context.Background(), connectionConfig{BaseURL: server.URL + "/v1", APIKey: "fixture", AuthType: "token"}, server.URL+"/custom/output/job.mp4", "job", "fixture", "video")
	if err != nil || stored == "" || len(paths) != 1 {
		t.Fatalf("stored=%s paths=%#v err=%v", stored, paths, err)
	}
}
