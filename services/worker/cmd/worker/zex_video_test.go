package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
	"time"

	"github.com/starai/worker/internal/storage"
	"github.com/starai/worker/videoparams"
)

func TestZexNestedProviderError(t *testing.T) {
	message := `{"error":{"code":"resolution_conflict","message":"请求的 resolution 与模型固定参数不一致","retryable":false}}`
	if got := upstreamErrorMessage([]byte(message)); got != "resolution_conflict: 请求的 resolution 与模型固定参数不一致" {
		t.Fatalf("direct error lost provider code: %q", got)
	}
	for i := 0; i < 3; i++ {
		body, err := json.Marshal(map[string]interface{}{"code": "fail_to_fetch_task", "message": message, "data": nil})
		if err != nil {
			t.Fatal(err)
		}
		message = string(body)
	}
	if got := upstreamErrorMessage([]byte(message)); got != "resolution_conflict: 请求的 resolution 与模型固定参数不一致" {
		t.Fatalf("nested error lost provider cause: %q", got)
	}
}

func TestZexVideoSubmitAndPoll(t *testing.T) {
	for _, model := range []string{"grok-imagine-video-1.5", "seedance-2.0", "minimax-h3-max"} {
		for _, format := range []string{"json", "multipart"} {
			t.Run(model+"/"+format, func(t *testing.T) {
				refs := []string{"https://example.test/first.png", "https://example.test/second.png"}
				resolution := "720p"
				if model == "minimax-h3-max" {
					resolution = "768p"
				}
				input := map[string]interface{}{"prompt": "test", "duration": "9s", "reference_images": refs, "resolution": resolution}
				if model != "grok-imagine-video-1.5" {
					input["reference_videos"] = []string{"https://example.test/ref.mp4"}
					input["reference_audios"] = []string{"https://example.test/ref.mp3"}
				}
				rule := map[string]interface{}{"upstream": map[string]interface{}{"adapter": "zex_video", "request_format": format, "poll_path": "/v1/videos/{id}", "response_map": map[string]interface{}{"task_id": "id", "status": "status", "media_url": []interface{}{"url", "video_url"}}}}
				polls := 0
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if r.Header.Get("Authorization") != "Bearer test-token" {
						t.Error("missing auth")
					}
					if r.Method == "POST" && r.URL.Path == "/v1/videos" {
						if format == "json" {
							var body map[string]interface{}
							if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
								t.Error(err)
							}
							if body["seconds"] != "9" || body["model"] != model || body["resolution"] != resolution || body["duration"] != nil || body["image_url"] != nil {
								t.Errorf("body=%#v", body)
							}
							if got := referenceImageSources(body["images"]); !reflect.DeepEqual(got, refs) {
								t.Errorf("lost references: %#v", got)
							}
							if model != "grok-imagine-video-1.5" && (body["videos"] == nil || body["audios"] == nil) {
								t.Errorf("lost multimodal references: %#v", body)
							}
						} else {
							if err := r.ParseMultipartForm(1 << 20); err != nil {
								t.Error(err)
							}
							want := append([]string{}, refs...)
							if model != "grok-imagine-video-1.5" {
								want = append(want, "https://example.test/ref.mp4", "https://example.test/ref.mp3")
							}
							if !reflect.DeepEqual(r.MultipartForm.Value["input_reference"], want) || r.FormValue("seconds") != "9" || r.FormValue("resolution") != resolution {
								t.Errorf("form=%#v", r.MultipartForm.Value)
							}
						}
						fmt.Fprint(w, `{"id":"job-1","status":"queued","images":["https://example.test/input.png"]}`)
					} else if r.Method == "GET" && r.URL.Path == "/v1/videos/job-1" {
						polls++
						if polls == 1 {
							fmt.Fprint(w, `{"id":"job-1","status":"in_progress","images":["https://example.test/input.png"]}`)
						} else {
							fmt.Fprint(w, `{"id":"job-1","status":"completed","video_url":"https://example.test/result.mp4"}`)
						}
					} else {
						t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
						w.WriteHeader(404)
					}
				}))
				defer server.Close()
				payload := videoparams.BuildUpstreamVideoPayload(model, model, rule, nil, input)
				payload = videoparams.SanitizeUpstreamPayload(payload, "/v1/videos")
				conn := connectionConfig{BaseURL: server.URL, APIKey: "test-token"}
				body, status, err := postVideoUpstream(context.Background(), conn, "/v1/videos", payload, rule, "")
				if err != nil || status != 200 {
					t.Fatalf("submit: %d %v", status, err)
				}
				items, id := parseUpstreamMediaWithRule(body, rule)
				if len(items) != 0 || id != "job-1" {
					t.Fatalf("submit items=%#v id=%s", items, id)
				}
				cfg := resolveMediaPollConfig(rule, "/v1/videos", body, server.URL)
				cfg.Interval, cfg.Timeout = time.Millisecond, time.Second
				items, _, err = pollUpstreamTask(context.Background(), nil, conn, cfg, id, "")
				if err != nil || len(items) != 1 || items[0].URL != "https://example.test/result.mp4" || polls != 2 {
					t.Fatalf("poll items=%#v polls=%d error=%v", items, polls, err)
				}
			})
		}
	}
}

func TestZexTerminalFailureDoesNotRetryTimeout(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"id":"accepted","status":"failed","error":{"message":"provider generation timeout"}}`)
	}))
	defer server.Close()
	rule := map[string]interface{}{"upstream": map[string]interface{}{"adapter": "zex_video"}}
	cfg := resolveMediaPollConfig(rule, "/v1/videos", nil, server.URL)
	cfg.Interval, cfg.Timeout = time.Millisecond, time.Second
	_, _, err := pollUpstreamTask(context.Background(), nil, connectionConfig{BaseURL: server.URL}, cfg, "accepted", "")
	var terminal *upstreamTerminalError
	if !errors.As(err, &terminal) {
		t.Fatalf("provider failure must be terminal: %v", err)
	}
}

func TestZexResumedTaskBillingDatabase(t *testing.T) {
	pool := productTestDatabase(t)
	ctx := context.Background()
	prices := map[string]interface{}{"billing_type": "per_request", "unit_price": 0, "unit_price_by_duration": map[string]interface{}{"10": 2, "15": 3.5, "30": 8}}
	runtime := map[string]interface{}{"upstream": map[string]interface{}{"adapter": "zex_video", "model_template": "seedance-2.5-{duration}s", "poll_path": "/v1/videos/{id}", "response_map": map[string]interface{}{"media_url": "video_url"}}}
	for index, tt := range []struct {
		seconds int
		price   float64
		failed  bool
		submit  bool
	}{{10, 2, false, false}, {15, 3.5, false, false}, {30, 8, false, false}, {30, 8, true, false}, {30, 8, true, true}, {15, 3.5, false, true}} {
		t.Run(fmt.Sprintf("%ds/failed=%v/submit=%v", tt.seconds, tt.failed, tt.submit), func(t *testing.T) {
			posts, polls := 0, 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch {
				case r.Method == "POST":
					posts++
					if !tt.submit {
						t.Error("accepted task must not submit another generation")
					}
					var payload map[string]interface{}
					if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
						t.Error(err)
					}
					if payload["model"] != fmt.Sprintf("seedance-2.5-%ds", tt.seconds) || payload["seconds"] != fmt.Sprint(tt.seconds) {
						t.Errorf("model/duration mismatch: %#v", payload)
					}
					if payload["prompt"] != "图片 2参考图片 1" || !reflect.DeepEqual(payload["images"], []interface{}{"https://e.test/first", "https://e.test/last"}) {
						t.Errorf("reference prompt/order mismatch: %#v", payload)
					}
					if _, sent := payload["resolution"]; sent {
						t.Errorf("fixed model must use server default resolution: %#v", payload)
					}
					if tt.failed {
						fmt.Fprint(w, `{"id":"accepted","status":"failed","error":{"message":"provider generation timeout"}}`)
					} else {
						fmt.Fprint(w, `{"id":"accepted","status":"queued"}`)
					}
				case r.URL.Path == "/v1/videos/accepted":
					polls++
					if tt.failed {
						fmt.Fprint(w, `{"status":"failed","error":{"message":"provider generation timeout"}}`)
					} else {
						fmt.Fprintf(w, `{"status":"completed","video_url":"http://%s/result.mp4","usage":{"output_seconds":9.8}}`, r.Host)
					}
				case r.URL.Path == "/result.mp4":
					w.Header().Set("Content-Type", "video/mp4")
					// Storage transport fixture; codec processing is not used for fixed prices.
					_, _ = w.Write([]byte("video-transport-fixture"))
				default:
					t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
					w.WriteHeader(404)
				}
			}))
			defer server.Close()
			store, err := storage.NewLocal(t.TempDir(), server.URL+"/media")
			if err != nil {
				t.Fatal(err)
			}
			previous := objectStore
			objectStore = store
			defer func() { objectStore = previous }()
			userID := int64(700 + index)
			taskNo := fmt.Sprintf("zex-audit-%d", index)
			var modelID, routeID int64
			// Current prices differ deliberately: settlement must use the task's snapshot.
			err = pool.QueryRow(ctx, `INSERT INTO models(code,display_name,category,request_mode,new_api_model,new_api_endpoint,price_rule,runtime_rule) VALUES($1,'章鱼哥 Seedance2.5','video','video','seedance-2.5-10s','/v1/videos','{"billing_type":"per_request","unit_price":99}',$2) RETURNING id`, taskNo, mustJSON(runtime)).Scan(&modelID)
			if err != nil {
				t.Fatal(err)
			}
			err = pool.QueryRow(ctx, `INSERT INTO model_routes(model_id,route_name,upstream_model,endpoint,base_url,runtime_rule,cost_rule) VALUES($1,'audit','seedance-2.5-10s','/v1/videos',$2,$3,$4) RETURNING id`, modelID, server.URL, mustJSON(runtime), mustJSON(map[string]interface{}{"billing_type": "per_request", "unit_cost_by_duration": map[string]interface{}{"10": 1, "15": 2, "30": 4}})).Scan(&routeID)
			if err != nil {
				t.Fatal(err)
			}
			input := map[string]interface{}{"prompt": "@图片2参考@图片1", "generation_mode": "first_last", "first_frame": "https://e.test/first", "last_frame": "https://e.test/last", "duration": tt.seconds, "resolution": "auto", "_price_rule_snapshot": prices}
			var receipt interface{} = "accepted"
			if tt.submit {
				receipt = nil
			}
			_, err = pool.Exec(ctx, `INSERT INTO tasks(task_no,user_id,model_id,type,status,input,estimated_cost,upstream_task_id,route_id) VALUES($1,$2,$3,'video','running',$4,$5,$7,$6)`, taskNo, userID, modelID, mustJSON(input), tt.price, routeID, receipt)
			if err != nil {
				t.Fatal(err)
			}
			_, err = pool.Exec(ctx, `INSERT INTO wallets(user_id,compute_balance,frozen_compute) VALUES($1,100,$2)`, userID, tt.price)
			if err != nil {
				t.Fatal(err)
			}
			_, err = pool.Exec(ctx, `INSERT INTO balance_freezes(user_id,amount,ref_type,ref_id,status) VALUES($1,$2,'task',$3,'frozen')`, userID, tt.price, taskNo)
			if err != nil {
				t.Fatal(err)
			}
			p := ImageTaskPayload{TaskNo: taskNo, UserID: userID, ModelID: modelID, ModelCode: taskNo, Input: input}
			for delivery := 0; delivery < 2; delivery++ {
				if err = processImageTask(ctx, pool, server.URL, "", p); err != nil {
					t.Fatal(err)
				}
			}
			if p.Input["prompt"] != "@图片2参考@图片1" {
				t.Fatal("upstream conversion changed the persisted task prompt")
			}
			var status, freezeStatus string
			var actual, balance, frozen, provider float64
			var charges int
			if err = pool.QueryRow(ctx, `SELECT status,actual_cost,provider_cost FROM tasks WHERE task_no=$1`, taskNo).Scan(&status, &actual, &provider); err != nil {
				t.Fatal(err)
			}
			if err = pool.QueryRow(ctx, `SELECT compute_balance,frozen_compute FROM wallets WHERE user_id=$1`, userID).Scan(&balance, &frozen); err != nil {
				t.Fatal(err)
			}
			if err = pool.QueryRow(ctx, `SELECT status FROM balance_freezes WHERE ref_id=$1`, taskNo).Scan(&freezeStatus); err != nil {
				t.Fatal(err)
			}
			if err = pool.QueryRow(ctx, `SELECT COUNT(*) FROM wallet_transactions WHERE ref_id=$1 AND direction='out'`, taskNo).Scan(&charges); err != nil {
				t.Fatal(err)
			}
			wantPosts, wantPolls := 0, 1
			if tt.submit {
				wantPosts = 1
				if tt.failed {
					wantPolls = 0
				}
			}
			if posts != wantPosts || polls != wantPolls || frozen != 0 {
				t.Fatalf("posts=%d polls=%d frozen=%v", posts, polls, frozen)
			}
			if tt.failed {
				if status != "failed" || balance != 100 || actual != 0 || charges != 0 || freezeStatus != "released" {
					t.Fatalf("failure status=%s balance=%v actual=%v charges=%d reservation=%s", status, balance, actual, charges, freezeStatus)
				}
			} else if status != "succeeded" || balance != 100-tt.price || actual != tt.price || charges != 1 || freezeStatus != "charged" || provider != map[int]float64{10: 1, 15: 2, 30: 4}[tt.seconds] {
				t.Fatalf("success status=%s balance=%v actual=%v charges=%d reservation=%s provider=%v", status, balance, actual, charges, freezeStatus, provider)
			}
		})
	}
}

func TestZexFramesAndFailureResponses(t *testing.T) {
	rule := map[string]interface{}{"upstream": map[string]interface{}{"adapter": "zex_video", "response_map": map[string]interface{}{"media_url": []interface{}{"url", "video_url"}}}}
	payload := videoparams.BuildUpstreamVideoPayload("grok", "grok-imagine-video-1.5", rule, nil, map[string]interface{}{"prompt": "transition", "duration": 15, "first_frame": "https://e.test/f", "last_frame": "https://e.test/l", "_price_rule_snapshot": map[string]interface{}{"unit_price": 9}, "asset_context": "private"})
	payload = videoparams.SanitizeUpstreamPayload(payload, "/v1/videos")
	if payload["first_last_frame"] != true || payload["seconds"] != "15" || !reflect.DeepEqual(payload["images"], []string{"https://e.test/f", "https://e.test/l"}) || payload["asset_context"] != nil || payload["_price_rule_snapshot"] != nil {
		t.Fatalf("payload=%#v", payload)
	}
	if msg := configuredMediaBusinessError([]byte(`{"status":"failed","error":{"code":"invalid_request","message":"bad model"}}`), rule); msg != "bad model" {
		t.Fatalf("message=%s", msg)
	}
	items, _ := parseUpstreamMediaWithRule([]byte(`{"status":"completed","images":["https://e.test/input"]}`), rule)
	if len(items) != 0 {
		t.Fatalf("input echo treated as output: %#v", items)
	}
}

func TestZexConfiguredFrameAndReferenceKeys(t *testing.T) {
	runtime := map[string]interface{}{"upstream": map[string]interface{}{"adapter": "zex_video"}, "video": map[string]interface{}{"frames": map[string]interface{}{"first": map[string]interface{}{"key": "start"}, "last": map[string]interface{}{"key": "end"}}, "reference_images": map[string]interface{}{"key": "refs"}, "reference_videos": map[string]interface{}{"key": "clips"}, "reference_audios": map[string]interface{}{"key": "sounds"}}}
	for _, frames := range []bool{true, false} {
		input := map[string]interface{}{"prompt": "test", "duration": 10}
		if frames {
			input["start"], input["end"] = "https://e.test/start", "https://e.test/end"
		} else {
			input["refs"], input["clips"], input["sounds"] = []string{"https://e.test/i"}, []string{"https://e.test/v"}, []string{"https://e.test/a"}
		}
		payload := videoparams.SanitizeUpstreamPayload(videoparams.BuildUpstreamVideoPayload("logical", "seedance-2.0", runtime, nil, input), "/v1/videos")
		if frames {
			if payload["first_last_frame"] != true || !reflect.DeepEqual(payload["images"], []string{"https://e.test/start", "https://e.test/end"}) {
				t.Fatalf("frames lost: %#v", payload)
			}
		} else if !reflect.DeepEqual(payload["images"], []string{"https://e.test/i"}) || !reflect.DeepEqual(payload["videos"], []string{"https://e.test/v"}) || !reflect.DeepEqual(payload["audios"], []string{"https://e.test/a"}) {
			t.Fatalf("references lost: %#v", payload)
		}
	}
}

func TestZexSeedance2ResolutionSettlement(t *testing.T) {
	fixed := map[string]interface{}{"billing_type": "per_request", "unit_price": 1.0, "unit_price_by_resolution": map[string]interface{}{"480p": 5.5, "720p": 8.75}}
	for _, seconds := range []int{5, 10, 15} {
		for resolution, price := range fixed["unit_price_by_resolution"].(map[string]interface{}) {
			params := map[string]interface{}{"duration": seconds, "resolution": resolution, "_actual_output_seconds": 9.8, "_actual_request_count": 2, "_price_rule_snapshot": fixed}
			if got := estimateModelCostByIDWorker(context.Background(), nil, 1, params, 0, 0, 0, 0); got != price {
				t.Fatalf("%s %ds fixed snapshot price=%v", resolution, seconds, got)
			}
			costRule := map[string]interface{}{"billing_type": "per_request", "unit_cost": 1.0, "unit_cost_by_resolution": fixed["unit_price_by_resolution"]}
			if got := workerRouteProviderCost(workerModelRoute{CostRule: costRule}, params, 0, 0, 0, 0); got != 2*floatAny(price) {
				t.Fatalf("%s fixed provider cost=%v", resolution, got)
			}
		}
	}
	rule := map[string]interface{}{"billing_type": "per_second", "unit_price": 0.5, "unit_price_by_resolution": map[string]interface{}{"480p": 0.2, "720p": 0.4, "768p": 0.6, "1080p": 0.0}}
	costRule := map[string]interface{}{"billing_type": "per_second", "unit_cost": 0.25, "unit_cost_by_resolution": map[string]interface{}{"480p": 0.1, "720p": 0.3, "768p": 0.35, "1080p": 0.0}}
	for _, tt := range []struct {
		resolution  string
		price, cost float64
	}{{"480p", 0.2, 0.1}, {"720p", 0.4, 0.3}, {"768P", 0.6, 0.35}, {"1080p", 0, 0}, {"4k", 0.5, 0.25}, {"", 0.5, 0.25}} {
		params := map[string]interface{}{"duration": 10, "resolution": tt.resolution, "count": 2, "_price_rule_snapshot": rule}
		if got := estimateModelCostByIDWorker(context.Background(), nil, 1, params, 0, 0, 0, 0); math.Abs(got-20*tt.price) > 1e-9 {
			t.Fatalf("%s snapshot price=%v", tt.resolution, got)
		}
		if got := workerRouteProviderCost(workerModelRoute{CostRule: costRule}, params, 0, 0, 0, 0); math.Abs(got-20*tt.cost) > 1e-9 {
			t.Fatalf("%s estimated provider cost=%v", tt.resolution, got)
		}
		params["_actual_output_seconds"] = 9.8
		if got := estimateModelCostByIDWorker(context.Background(), nil, 1, params, 0, 0, 0, 0); math.Abs(got-9.8*tt.price) > 1e-9 {
			t.Fatalf("%s actual snapshot price=%v", tt.resolution, got)
		}
		if got := workerRouteProviderCost(workerModelRoute{CostRule: costRule}, params, 0, 0, 0, 0); math.Abs(got-9.8*tt.cost) > 1e-9 {
			t.Fatalf("%s actual provider cost=%v", tt.resolution, got)
		}
	}
}

func TestSeedance25SelectedModelAndFixedSettlement(t *testing.T) {
	prices := map[string]interface{}{"10": 2.0, "15": 3.5, "30": 8.0}
	rule := map[string]interface{}{"billing_type": "per_request", "unit_price": 1.0, "unit_price_by_duration": prices}
	runtime := map[string]interface{}{"upstream": map[string]interface{}{"adapter": "zex_video", "model_template": "seedance-2.5-{duration}s", "include": []interface{}{"duration"}, "map": map[string]interface{}{"duration": "seconds"}}}
	for _, tt := range []struct {
		seconds int
		price   float64
	}{{10, 2}, {15, 3.5}, {30, 8}} {
		for _, duration := range []interface{}{tt.seconds, float64(tt.seconds), json.Number(fmt.Sprint(tt.seconds)), fmt.Sprintf("%ds", tt.seconds)} {
			params := map[string]interface{}{"duration": duration, "prompt": "test", "_price_rule_snapshot": rule, "_actual_output_seconds": 9.8, "_actual_request_count": 2}
			payload := videoparams.BuildUpstreamVideoPayload("logical-seedance25", "seedance-2.5-10s", runtime, nil, params)
			payload = videoparams.SanitizeUpstreamPayload(payload, "/v1/videos")
			if err := ensureVideoModel(payload, "seedance-2.5-10s", "logical-seedance25"); err != nil {
				t.Fatal(err)
			}
			if payload["model"] != fmt.Sprintf("seedance-2.5-%ds", tt.seconds) || payload["seconds"] != fmt.Sprint(tt.seconds) {
				t.Fatalf("duration=%#v payload=%#v", duration, payload)
			}
			if got := estimateModelCostByIDWorker(context.Background(), nil, 1, params, 0, 0, 0, 0); got != tt.price {
				t.Fatalf("duration=%#v snapshot settlement=%v want=%v", duration, got, tt.price)
			}
			costRule := map[string]interface{}{"billing_type": "per_request", "unit_cost": 0.1, "unit_cost_by_duration": prices}
			if got := workerRouteProviderCost(workerModelRoute{CostRule: costRule}, params, 0, 0, 0, 0); got != 2*tt.price {
				t.Fatalf("duration=%#v provider cost=%v want=%v", duration, got, 2*tt.price)
			}
		}
	}
}
