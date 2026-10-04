package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestMediaNativeResultContractsAndExactTaskID(t *testing.T) {
	for _, test := range []struct{ body, id, result string }{
		{`{"request_id":"grok-1","status":"done","video":{"url":"https://example.test/grok.mp4"}}`, "grok-1", "https://example.test/grok.mp4"},
		{`{"task_id":911088422699962369,"state":"success","creations":[{"url":"https://example.test/vidu.mp4"}]}`, "911088422699962369", "https://example.test/vidu.mp4"},
		{`{"task_id":"vidu-2","state":"created","images":["https://example.test/reference.png"]}`, "vidu-2", ""},
	} {
		items, id := parseUpstreamMedia([]byte(test.body))
		if id != test.id {
			t.Fatalf("id=%q want=%q", id, test.id)
		}
		if test.result == "" {
			if len(items) != 0 {
				t.Fatalf("input echoed as output: %#v", items)
			}
			continue
		}
		if len(items) != 1 || items[0].URL != test.result {
			t.Fatalf("items=%#v", items)
		}
	}
}

func TestMappedMediaAsyncFileDownloadAndBilling(t *testing.T) {
	const exactID = "911088422699962369"
	var calls []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls = append(calls, r.URL.Path)
		if r.Header.Get("Authorization") != "Bearer fixture-key" {
			t.Errorf("missing gateway authentication")
		}
		switch r.URL.Path {
		case "/fork/create":
			var body map[string]interface{}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body["duration"] != float64(5) || body["images"] == nil {
				t.Errorf("payload=%#v err=%v", body, err)
			}
			fmt.Fprintf(w, `{"payload":{"job":%s,"phase":"WAITING","images":["https://example.test/input.png"]}}`, exactID)
		case "/fork/status":
			if r.URL.Query().Get("task_id") != exactID {
				t.Errorf("task id was rounded: %s", r.URL.RawQuery)
			}
			fmt.Fprint(w, `{"payload":{"phase":"READY","file":911088422699962371,"meter":{"input":100,"total":300,"seconds":6}}}`)
		case "/fork/files":
			if r.URL.Query().Get("file_id") != "911088422699962371" {
				t.Errorf("file id was rounded: %s", r.URL.RawQuery)
			}
			fmt.Fprintf(w, `{"file":{"download_url":"http://%s/media.mp4"},"base_resp":{"status_code":0}}`, r.Host)
		case "/media.mp4":
			w.Header().Set("Content-Type", "video/mp4")
			_, _ = w.Write([]byte("\x00\x00\x00\x18ftypmp42fixture-video"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	rule := map[string]interface{}{"upstream": map[string]interface{}{
		"adapter": "native_media", "include": []interface{}{"duration", "reference_images"}, "map": map[string]interface{}{"reference_images": "images"},
		"poll_path": "/fork/status?task_id={id}", "poll_interval_sec": 0.001, "poll_timeout_sec": 1,
		"response_map":     map[string]interface{}{"task_id": "payload.job", "status": "payload.phase", "file_id": "payload.file", "input_tokens": "payload.meter.input", "total_tokens": "payload.meter.total", "output_seconds": "payload.meter.seconds"},
		"success_statuses": []interface{}{"READY"}, "failure_statuses": []interface{}{"BROKEN"},
		"result_path": "/fork/files?file_id={file_id}", "result_response_map": map[string]interface{}{"media_url": "file.download_url"},
	}}
	conn := connectionConfig{BaseURL: server.URL, APIKey: "fixture-key", AuthType: "bearer"}
	input := map[string]interface{}{"prompt": "sample", "duration": 5, "reference_images": []string{"https://example.test/ref.png"}, "count": 2, "_price_rule_snapshot": map[string]interface{}{"billing_type": "per_token", "input_price_per_m": 2.0, "output_price_per_m": 4.0}}
	result, err := executeWorkerGenerationAttempt(context.Background(), nil, ImageTaskPayload{TaskNo: "fixture", ModelCode: "custom", Input: input}, workerModelRoute{Connection: conn, Endpoint: "/fork/create", UpstreamModel: "custom", RuntimeRule: rule}, true, false, false, "sample")
	if err != nil {
		t.Fatal(err)
	}
	items, id := parseUpstreamMediaWithRule(result.ResponseBody, rule)
	if id != exactID || len(items) != 0 {
		t.Fatalf("id=%s items=%#v", id, items)
	}
	items, usage, err := pollUpstreamTask(context.Background(), nil, conn, parsePollConfig(rule, "/fork/create"), id, "")
	if err != nil || len(items) != 1 || usage.PromptTokens != 100 || usage.OutputTokens != 200 {
		t.Fatalf("items=%#v usage=%#v err=%v", items, usage, err)
	}
	media, _, err := downloadAuthenticatedMedia(context.Background(), conn, items[0].URL, 1024)
	if err != nil || !strings.Contains(string(media), "fixture-video") {
		t.Fatalf("download err=%v data=%q", err, media)
	}
	billed := workerBillingParams(inputWithActualUpstreamUsage(input, usage), "audio")
	if cost := estimateModelCostByIDWorker(context.Background(), nil, 1, billed, usage.PromptTokens, usage.OutputTokens, 0, 0); cost != 0.001 {
		t.Fatalf("snapshot aggregate-token cost=%v", cost)
	}
	if seconds := workerBillableSeconds(billed); seconds != 6 {
		t.Fatalf("aggregate seconds=%v", seconds)
	}
	if strings.Join(calls, ",") != "/fork/create,/fork/status,/fork/files,/media.mp4" {
		t.Fatalf("calls=%#v", calls)
	}
}

func TestMediaBusinessErrorsAndFailureStatuses(t *testing.T) {
	for _, body := range []string{`{"base_resp":{"status_code":2013,"status_msg":"bad params"},"images":["https://example.test/input.png"]}`, `{"status":"Fail","message":"failed task"}`} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, body) }))
		_, _, err := pollUpstreamTask(context.Background(), nil, connectionConfig{BaseURL: server.URL}, pollConfig{Path: "/tasks/{id}", Interval: time.Millisecond, Timeout: time.Second}, "job", "")
		server.Close()
		if err == nil {
			t.Fatalf("accepted business failure %s", body)
		}
	}
	if !mediaSuccessStatus("succeed", nil) {
		t.Fatal("OpenLux succeed state missing")
	}
}

func TestMultipartMediaWithoutReferenceAndExplicitCustomPoll(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseMultipartForm(1024); err != nil || r.FormValue("model") != "sora-2" {
			t.Errorf("multipart model=%q err=%v", r.FormValue("model"), err)
		}
		fmt.Fprint(w, `{"id":"video-1"}`)
	}))
	defer server.Close()
	rule := map[string]interface{}{"upstream": map[string]interface{}{"adapter": "native_media", "request_format": "multipart", "poll_path": "/v1/video/generations/{id}"}}
	_, status, err := postVideoUpstream(context.Background(), connectionConfig{BaseURL: server.URL}, "/v1/videos", map[string]interface{}{"model": "sora-2", "prompt": "test"}, rule, "")
	if err != nil || status != 200 {
		t.Fatalf("status=%d err=%v", status, err)
	}
	if cfg := parsePollConfig(rule, "/v1/videos"); cfg.Path != "/v1/video/generations/{id}" {
		t.Fatalf("custom poll overwritten: %q", cfg.Path)
	}
}

func TestMultipartMediaInputReferenceIsAFile(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		file, header, err := r.FormFile("input_reference")
		if err != nil {
			t.Errorf("input_reference not a file: %v", err)
		} else {
			defer file.Close()
			if header.Size != 15 {
				t.Errorf("file size=%d", header.Size)
			}
		}
		fmt.Fprint(w, `{"id":"video-1"}`)
	}))
	defer server.Close()
	rule := map[string]interface{}{"upstream": map[string]interface{}{"request_format": "multipart"}}
	_, _, err := postVideoUpstream(context.Background(), connectionConfig{BaseURL: server.URL}, "/v1/videos", map[string]interface{}{"model": "sora-2", "input_reference": "data:image/png;base64,iVBORw0KGgpmaXh0dXJl"}, rule, "")
	if err != nil {
		t.Fatal(err)
	}
}

func TestMediaResultRetrievalRejectsCrossOrigin(t *testing.T) {
	_, err := resolveUpstreamMediaResult(context.Background(), connectionConfig{BaseURL: "https://gateway.test"}, pollConfig{RuntimeRule: map[string]interface{}{"upstream": map[string]interface{}{"result_path": "https://other.test/file?file_id={file_id}"}}}, map[string]interface{}{"file_id": "123"})
	if err == nil {
		t.Fatal("result retrieval could leak credentials across origins")
	}
}

func TestUsageNormalizationAndAggregatePrice(t *testing.T) {
	for _, test := range []struct {
		body          string
		input, output int
	}{
		{`{"usage":{"input_tokens":100,"total_tokens":300}}`, 100, 200},
		{`{"usage":{"total_tokens":300}}`, 0, 300},
		{`{"usage":{"input_tokens":100,"output_tokens":200,"total_tokens":300}}`, 100, 200},
		{`{"usage":{"input_tokens":0,"output_tokens":0}}`, 0, 0},
	} {
		usage := upstreamUsageFromBody([]byte(test.body))
		if !usage.HasTokens || usage.PromptTokens != test.input || usage.OutputTokens != test.output {
			t.Fatalf("usage=%#v", usage)
		}
		params := workerBillingParams(inputWithActualUpstreamUsage(map[string]interface{}{"count": 3, "prompt": "estimate must not count"}, usage), "audio")
		rule := map[string]interface{}{"billing_type": "per_token", "input_price": 2.0, "output_price": 4.0}
		if got, want := estimatePriceRuleCostWorker(rule, params, usage.PromptTokens, usage.OutputTokens, 0, 0), float64(test.input*2+test.output*4); got != want {
			t.Fatalf("cost=%v want=%v", got, want)
		}
	}
	params := map[string]interface{}{"duration_seconds": 2.5, "count": 3}
	if got := workerRouteProviderCost(workerModelRoute{CostRule: map[string]interface{}{"billing_type": "per_second", "unit_cost": 2.0}}, params, 0, 0, 0, 0); got != 15 {
		t.Fatalf("provider seconds cost=%v", got)
	}
}

func TestInvalidUsageFallsBackAndTokenAuthentication(t *testing.T) {
	for _, body := range []string{`{"usage":{"total_tokens":null}}`, `{"usage":{"total_tokens":"bad","output_seconds":"NaN"}}`, `{"usage":{"input_tokens":-1,"output_tokens":-10}}`} {
		if usage := upstreamUsageFromBody([]byte(body)); usage.hasAny() {
			t.Fatalf("invalid usage accepted: %#v", usage)
		}
	}
	input := workerBillingParams(map[string]interface{}{"count": 3, "_estimated_input_tokens": 10, "_estimated_output_tokens": 20, "_price_rule_snapshot": map[string]interface{}{"billing_type": "per_token", "input_price": 2.0, "output_price": 4.0}}, "audio")
	if cost := estimateModelCostByIDWorker(context.Background(), nil, 1, input, 0, 0, 0, 0); cost != 300 {
		t.Fatalf("no-usage estimate lost audio count: %v", cost)
	}
	req := httptest.NewRequest("GET", "https://example.test", nil)
	applyConnectionHeaders(req, connectionConfig{AuthType: "token", APIKey: "fixture-token"})
	if req.Header.Get("Authorization") != "Token fixture-token" {
		t.Fatal("Vidu Token auth missing")
	}
	if shouldRepriceMediaUsage(upstreamUsageDetails{OutputSeconds: 5, HasOutputSeconds: true}, input) {
		t.Fatal("seconds-only usage must retain frozen token estimate")
	}
}

func TestResponseMappingHandlesEnvelopesWithoutUsingEchoedImages(t *testing.T) {
	rule := map[string]interface{}{"upstream": map[string]interface{}{"response_map": map[string]interface{}{"task_id": "task_id", "status": "state", "media_url": "creations", "error": "err_code"}}}
	items, id := parseUpstreamMediaWithRule([]byte(`{"data":{"task_id":911088422699962369,"state":"success","err_code":0,"images":["https://example.test/ref.png"],"creations":[{"url":"https://example.test/result.mp4"}]}}`), rule)
	if id != "911088422699962369" || len(items) != 1 || items[0].URL != "https://example.test/result.mp4" {
		t.Fatalf("items=%#v id=%s", items, id)
	}
	items, _ = parseUpstreamMediaWithRule([]byte(`{"task_id":"job","state":"success","images":["https://example.test/ref.png"]}`), rule)
	if len(items) != 0 {
		t.Fatalf("explicit missing output fell back to inputs: %#v", items)
	}
	raw, _ := mediaResponseForRule([]byte(`{"task_id":"job","state":"success","images":["https://example.test/ref.png"]}`), rule)
	if fallback := firstSuccessMediaURL(raw, "job", connectionConfig{BaseURL: "https://gateway.test"}); fallback != "" {
		t.Fatalf("explicit mapping fell back: %q", fallback)
	}
}

func TestUsagePartialMergeAndMillisecondsScale(t *testing.T) {
	previous := upstreamUsageFromBody([]byte(`{"usage":{"input_tokens":100,"input_seconds":2}}`))
	latest := upstreamUsageFromBodyWithRule([]byte(`{"extra_info":{"audio_length":6000},"usage":{"output_tokens":200}}`), map[string]interface{}{"upstream": map[string]interface{}{"response_map": map[string]interface{}{"output_seconds": "extra_info.audio_length"}, "output_seconds_scale": 0.001}})
	merged := mergeMediaUsage(previous, latest)
	if merged.PromptTokens != 100 || merged.OutputTokens != 200 || merged.VideoTokens != 300 || merged.InputSeconds != 2 || merged.OutputSeconds != 6 {
		t.Fatalf("partial usage=%#v", merged)
	}
	merged = mergeMediaUsage(previous, upstreamUsageFromBody([]byte(`{"usage":{"total_tokens":300}}`)))
	if merged.PromptTokens != 100 || merged.OutputTokens != 200 {
		t.Fatalf("total usage merge=%#v", merged)
	}
}

func TestWorkerTokenCacheOverflowKeepsNonnegativePartition(t *testing.T) {
	params := map[string]interface{}{"_actual_token_usage": true}
	rule := map[string]interface{}{"billing_type": "per_token", "input_price": 2.0, "cache_read_price": 1.0, "cache_write_price": 3.0, "output_price": 4.0}
	if cost := estimatePriceRuleCostWorker(rule, params, 1000, 100, 5000, 2000); cost != 1400 {
		t.Fatalf("cache partition cost=%v", cost)
	}
	route := workerModelRoute{CostRule: map[string]interface{}{"billing_type": "per_token", "input_cost_per_m": 2.0, "cache_read_cost_per_m": 1.0, "cache_write_cost_per_m": 3.0, "output_cost_per_m": 4.0}}
	if cost := workerRouteProviderCost(route, params, 1000, 100, 5000, 2000); math.Abs(cost-0.0014) > 0.000000001 {
		t.Fatalf("provider cache partition cost=%v", cost)
	}
}

func TestMiniMaxMappedImageURLAndBase64Arrays(t *testing.T) {
	rule := map[string]interface{}{"upstream": map[string]interface{}{"response_map": map[string]interface{}{"media_url": "data.image_urls", "media_base64": "data.image_base64"}}}
	encoded := base64.StdEncoding.EncodeToString([]byte("fixture-image-bytes-longer-than-32"))
	for _, payload := range []map[string]interface{}{
		{"data": map[string]interface{}{"image_urls": []string{"https://example.test/one.png", "https://example.test/two.png"}}},
		{"data": map[string]interface{}{"image_urls": []string{"https://example.test/one.png", "https://example.test/two.png"}, "image_base64": []string{}}},
		{"data": map[string]interface{}{"image_urls": []string{}, "image_base64": []string{encoded, encoded}}},
		{"data": map[string]interface{}{"image_base64": []string{encoded, encoded}}},
	} {
		body, _ := json.Marshal(payload)
		items, _ := parseUpstreamMediaWithRule(body, rule)
		if len(items) != 2 {
			t.Fatalf("body=%s items=%#v", body, items)
		}
		data := payload["data"].(map[string]interface{})
		if urls, ok := data["image_urls"].([]string); ok && len(urls) > 0 {
			if items[0].URL != urls[0] || items[1].URL != urls[1] {
				t.Fatalf("URL array lost: %#v", items)
			}
		} else if items[0].B64JSON != encoded || items[1].B64JSON != encoded {
			t.Fatalf("base64 array lost: %#v", items)
		}
	}
	for i := 0; i < 50; i++ {
		body, _ := json.Marshal(map[string]interface{}{"data": map[string]interface{}{"image_urls": []string{"https://example.test/one.png"}, "image_base64": []string{encoded, encoded}}})
		items, _ := parseUpstreamMediaWithRule(body, rule)
		if len(items) != 1 || items[0].URL != "https://example.test/one.png" {
			t.Fatalf("URL preference depends on map order: %#v", items)
		}
	}
	body, _ := json.Marshal(map[string]interface{}{"data": map[string]interface{}{"image_base64": encoded}})
	items, _ := parseUpstreamMediaWithRule(body, rule)
	if len(items) != 1 || items[0].B64JSON != encoded {
		t.Fatalf("scalar base64 lost: %#v", items)
	}
}

func TestMeasuredMediaDurationAndCancellation(t *testing.T) {
	if _, err := exec.LookPath("ffprobe"); err != nil {
		t.Skip("ffprobe unavailable; deployed worker requires ffmpeg/ffprobe for measured duration")
	}
	if _, err := ffmpegBinaryPath(); err != nil {
		t.Skip("ffmpeg unavailable")
	}
	source := filepath.Join(t.TempDir(), "duration.wav")
	if err := runFFmpeg(context.Background(), "-y", "-f", "lavfi", "-i", "anullsrc=r=8000:cl=mono", "-t", "1.25", source); err != nil {
		t.Fatal(err)
	}
	duration, err := measureMediaOutputSeconds(context.Background(), []string{source, source})
	if err != nil || math.Abs(duration-2.5) > 0.01 {
		t.Fatalf("duration=%v err=%v", duration, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := measureMediaOutputSeconds(ctx, []string{source}); err == nil {
		t.Fatal("canceled duration probe succeeded")
	}
}
