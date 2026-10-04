package service

import (
	"encoding/json"
	"math"
	"testing"
)

func TestMediaQuoteAndTaskShareCanonicalBillingParams(t *testing.T) {
	model := &ModelFull{RequestMode: "images", ModelDTO: ModelDTO{
		DefaultParams: map[string]interface{}{"n": 1},
		InputSchema: map[string]interface{}{"required": []interface{}{"prompt"}, "properties": map[string]interface{}{
			"count": map[string]interface{}{"type": "integer", "minimum": 1, "maximum": 6},
		}},
		PriceRule: map[string]interface{}{"billing_type": "per_image", "unit_price": 2.0},
	}}
	params := MergeMediaTaskParams(model.DefaultParams, map[string]interface{}{"count": "3"})
	if err := NormalizeMediaEstimateParams(model, params); err != nil {
		t.Fatal(err)
	}
	if params["n"] != 3 || params["count"] != 3 || (&ModelService{}).EstimateCost(model, params, 0, 0) != 6 {
		t.Fatalf("quote parameters = %#v", params)
	}
	if err := NormalizeMediaTaskParams(model, params); err == nil {
		t.Fatal("creation accepted a missing required prompt")
	}
	params["prompt"] = "test"
	if err := NormalizeMediaTaskParams(model, params); err != nil {
		t.Fatal(err)
	}
	for _, requested := range []map[string]interface{}{
		{"n": 1.5}, {"count": 0}, {"n": 1, "count": 3}, {"count": 7}, {"count": "NaN"}, {"count": 51},
	} {
		if err := NormalizeMediaEstimateParams(model, requested); err == nil {
			t.Fatalf("invalid quantity accepted: %#v", requested)
		}
	}
}

func TestMediaSecondsAliasesQuoteAndProviderCost(t *testing.T) {
	model := &ModelFull{RequestMode: "video", ModelDTO: ModelDTO{PriceRule: map[string]interface{}{"billing_type": "per_second", "unit_price": 2.0}}}
	model.InputSchema = map[string]interface{}{"properties": map[string]interface{}{"duration": map[string]interface{}{"type": "number", "minimum": 1, "maximum": 10}}}
	route := &ModelRoute{CostRule: map[string]interface{}{"billing_type": "per_second", "unit_cost": 0.5}}
	for _, key := range []string{"duration", "duration_seconds", "duration_sec", "seconds"} {
		params := MergeMediaTaskParams(map[string]interface{}{"duration": 5}, map[string]interface{}{key: "8s", "count": 2})
		if err := NormalizeMediaEstimateParams(model, params); err != nil {
			t.Fatal(err)
		}
		if got := (&ModelService{}).EstimateCost(model, params, 0, 0); got != 32 {
			t.Fatalf("%s quoted %v, want 32", key, got)
		}
		if got := EstimateRouteProviderCost(route, params, 0, 0); got != 8 {
			t.Fatalf("%s provider %v, want 8", key, got)
		}
		params["_actual_output_seconds"] = 12.0
		if got := (&ModelService{}).EstimateCost(model, params, 0, 0); got != 24 {
			t.Fatalf("aggregate seconds charged %v, want 24", got)
		}
		if got := EstimateRouteProviderCost(route, params, 0, 0); got != 6 {
			t.Fatalf("aggregate provider cost %v, want 6", got)
		}
	}
	for _, params := range []map[string]interface{}{{"duration": 5, "seconds": 8}, {"duration": "Inf"}, {"duration": -1}} {
		if err := NormalizeMediaEstimateParams(model, params); err == nil {
			t.Fatalf("invalid seconds accepted: %#v", params)
		}
	}
}

func TestActualSecondsZeroDiffersFromMissingOrInvalid(t *testing.T) {
	model := &ModelFull{RequestMode: "video", ModelDTO: ModelDTO{PriceRule: map[string]interface{}{"billing_type": "per_second", "unit_price": 2.0}}}
	route := &ModelRoute{CostRule: map[string]interface{}{"billing_type": "per_second", "unit_cost": 0.5}}
	for _, invalid := range []interface{}{nil, "bad", math.NaN(), math.Inf(1), -1} {
		params := map[string]interface{}{"duration": 8, "count": 2, "_actual_output_seconds": invalid}
		if got := (&ModelService{}).EstimateCost(model, params, 0, 0); got != 32 {
			t.Fatalf("invalid actual seconds should retain estimate, got %v", got)
		}
	}
	params := map[string]interface{}{"duration": 8, "count": 2, "_actual_output_seconds": 0.0}
	if got := (&ModelService{}).EstimateCost(model, params, 0, 0); got != 0 {
		t.Fatalf("trusted explicit zero seconds charged estimate: %v", got)
	}
	if got := EstimateRouteProviderCost(route, params, 0, 0); got != 0 {
		t.Fatalf("trusted explicit zero provider seconds charged estimate: %v", got)
	}
}

func TestSeedanceDynamicBillingPreservesExplicitZeroUsage(t *testing.T) {
	rule := map[string]interface{}{"billing_type": "dynamic", "strategy": "seedance_2_tokens"}
	requested := map[string]interface{}{"duration": 5, "generation_mode": "video", "reference_videos": []string{"https://example.com/v.mp4"}}
	if estimateDynamicCost(rule, requested) <= 0 {
		t.Fatal("missing actual usage should retain a positive estimate")
	}
	requested["_actual_video_tokens"] = 0
	if got := estimateDynamicCost(rule, requested); got != 0 {
		t.Fatalf("explicit zero actual video tokens estimated a charge: %v", got)
	}
	delete(requested, "_actual_video_tokens")
	requested["_actual_output_seconds"], requested["_actual_input_seconds"] = 0, 0
	if got := estimateDynamicCost(rule, requested); got != 0 {
		t.Fatalf("explicit zero input/output seconds were defaulted: %v", got)
	}
}

func TestMediaSaleAndProviderCostUseActualIndependentQuantities(t *testing.T) {
	model := &ModelFull{ModelDTO: ModelDTO{PriceRule: map[string]interface{}{"billing_type": "per_image", "unit_price": 2.0}}}
	requestCost := &ModelRoute{CostRule: map[string]interface{}{"billing_type": "per_request", "unit_cost": 0.5}}
	imageCost := &ModelRoute{CostRule: map[string]interface{}{"billing_type": "per_image", "unit_cost": 0.5}}
	params := map[string]interface{}{"n": 3}
	if got := EstimateRouteProviderCost(requestCost, params, 0, 0); got != 0.5 {
		t.Fatalf("one batch POST was estimated as multiple requests: %v", got)
	}
	params["_actual_output_image_count"], params["_actual_request_count"] = 1, 3
	if got := (&ModelService{}).EstimateCost(model, params, 0, 0); got != 2 {
		t.Fatalf("one successful image was charged as requested quantity: %v", got)
	}
	if got := EstimateRouteProviderCost(imageCost, params, 0, 0); got != 0.5 {
		t.Fatalf("one successful image provider cost = %v", got)
	}
	if got := EstimateRouteProviderCost(requestCost, params, 0, 0); got != 1.5 {
		t.Fatalf("three actual POST requests cost = %v", got)
	}
	model.PriceRule["billing_type"] = "per_request"
	if got := (&ModelService{}).EstimateCost(model, params, 0, 0); got != 2 {
		t.Fatalf("platform per-request sale was multiplied by upstream calls: %v", got)
	}
	for _, key := range []string{"_actual_output_image_count", "_actual_request_count"} {
		params := map[string]interface{}{key: 0}
		if _, exists := actualBillingCount(params, key); !exists {
			t.Fatalf("explicit zero %s confused with missing quantity", key)
		}
		for _, invalid := range []interface{}{nil, "bad", -1, 1.5, math.NaN(), math.Inf(1)} {
			params[key] = invalid
			if _, exists := actualBillingCount(params, key); exists {
				t.Fatalf("invalid trusted quantity accepted: %s=%v", key, invalid)
			}
		}
	}
}

func TestMediaSchemaTypeAndBounds(t *testing.T) {
	schema := map[string]interface{}{"properties": map[string]interface{}{
		"speed":        map[string]interface{}{"type": "number", "minimum": 0.5, "maximum": 2.0, "multipleOf": 0.25},
		"seed":         map[string]interface{}{"type": "integer"},
		"instrumental": map[string]interface{}{"type": "boolean"},
	}}
	for _, params := range []map[string]interface{}{{"speed": "1"}, {"speed": 0.25}, {"speed": 3.0}, {"speed": 1.1}, {"seed": 1.5}, {"instrumental": "false"}} {
		if err := validateSchemaParams(schema, params); err == nil {
			t.Fatalf("invalid schema values accepted: %#v", params)
		}
	}
	if err := validateSchemaParams(schema, map[string]interface{}{"speed": 1.25, "seed": 3, "instrumental": false}); err != nil {
		t.Fatal(err)
	}
}

func TestPriceRulesNormalizeAndRejectInvalidMoney(t *testing.T) {
	rule := map[string]interface{}{"billing_type": " PER_SECOND ", "unit_price": "1.25"}
	if err := validateModelPriceRule(rule); err != nil || rule["billing_type"] != "per_second" || rule["unit_price"] != 1.25 {
		t.Fatalf("normalized rule = %#v, error = %v", rule, err)
	}
	for _, value := range []interface{}{-1, math.NaN(), math.Inf(1), "NaN", "bad", true, json.Number("invalid")} {
		for _, validate := range []func(map[string]interface{}) error{validateModelPriceRule, validateRouteCostRule} {
			if err := validate(map[string]interface{}{"billing_type": "per_second", "unit_price": value, "unit_cost": value}); err == nil {
				t.Fatalf("invalid money accepted: %v", value)
			}
		}
	}
	if err := validateModelPriceRule(map[string]interface{}{"billing_type": "dynamic", "strategy": "seedance_2_tokens", "rates_per_m_tokens": map[string]interface{}{"720p": map[string]interface{}{"with_video": -1.0}}}); err == nil {
		t.Fatal("negative nested dynamic price was accepted")
	}
}

func TestActualTokenUsageChargesAggregateOnce(t *testing.T) {
	model := &ModelFull{ModelDTO: ModelDTO{Category: "audio", PriceRule: map[string]interface{}{
		"billing_type": "per_token", "input_price_per_m": 10.0, "output_price_per_m": 20.0,
	}}}
	got := (&ModelService{}).EstimateCostWithTokenDetails(model, map[string]interface{}{"count": 3}, 0, 1000, 0, 0)
	if math.Abs(got-0.02) > 1e-9 {
		t.Fatalf("aggregate actual token cost = %v, want 0.02", got)
	}
}

func TestOverreportedCacheTokensNeverCreateNegativeUsage(t *testing.T) {
	rule := map[string]interface{}{"input_price": 10.0, "output_price": 10.0, "cache_read_price": 1.0, "cache_write_price": 2.0}
	if got := tokenCostFromRule(rule, 1000, 0, 5000, 2000); got != 1000 {
		t.Fatalf("clamped cache usage cost = %v, want 1000", got)
	}
}

func TestFirstFrameVideoProfileAllowsTextOrSingleFrame(t *testing.T) {
	model := &ModelFull{RequestMode: "video", RuntimeRule: map[string]interface{}{"video": map[string]interface{}{"upload_profile": "first_frame"}}}
	for _, params := range []map[string]interface{}{{"prompt": "test"}, {"prompt": "test", "first_frame": "https://example.com/a.png"}} {
		if err := ValidateVideoParams(model, params); err != nil {
			t.Fatal(err)
		}
	}
	for _, params := range []map[string]interface{}{{"last_frame": "https://example.com/b.png"}, {"reference_images": []interface{}{"https://example.com/a.png"}}, {"first_frame": []interface{}{"https://example.com/a.png", "https://example.com/b.png"}}} {
		if err := ValidateVideoParams(model, params); err == nil {
			t.Fatalf("unsupported first frame parameters accepted: %#v", params)
		}
	}
	model.InputSchema = map[string]interface{}{"required": []interface{}{"first_frame"}, "properties": map[string]interface{}{"first_frame": map[string]interface{}{"type": "string"}}}
	if err := ValidateVideoParams(model, map[string]interface{}{"prompt": "test"}); err == nil {
		t.Fatal("image-to-video schema accepted missing first frame")
	}
	if err := ValidateVideoParams(model, map[string]interface{}{"prompt": "test", "first_frame": " "}); err == nil {
		t.Fatal("image-to-video schema accepted an empty first frame")
	}
}

func TestViduTokenAuthAcceptedAndNormalized(t *testing.T) {
	input := ModelRouteInput{RouteName: "Vidu", UpstreamModel: "viduq2", BaseURL: "https://example.com", AuthType: " TOKEN "}
	if err := normalizeModelRouteInput(&input); err != nil || input.AuthType != "token" {
		t.Fatalf("token auth = %q, err = %v", input.AuthType, err)
	}
	connection := map[string]interface{}{"base_url": "https://example.com", "api_key": "test-only", "auth_type": " TOKEN "}
	if err := validateModelConnection(CreateModelInput{RequestMode: "video", NewAPIExtraParams: map[string]interface{}{"connection": connection}}); err != nil || connection["auth_type"] != "token" {
		t.Fatalf("model token auth = %#v, err = %v", connection["auth_type"], err)
	}
}

func TestMediaQuantityFollowsRuntimeAndSingleAudioResult(t *testing.T) {
	for _, mode := range []string{"images", "video"} {
		section := "image"
		if mode == "video" {
			section = "video"
		}
		model := &ModelFull{RequestMode: mode, RuntimeRule: map[string]interface{}{section: map[string]interface{}{"count_max": 3, "count_allow_custom": false, "count_options": []interface{}{1, 3}}}}
		for _, count := range []int{2, 4} {
			if err := NormalizeMediaEstimateParams(model, map[string]interface{}{"count": count}); err == nil {
				t.Fatalf("%s unsupported runtime count %d accepted", mode, count)
			}
		}
		if err := NormalizeMediaEstimateParams(model, map[string]interface{}{"count": 3}); err != nil {
			t.Fatal(err)
		}
	}
	model := &ModelFull{RequestMode: "audio"}
	if err := NormalizeMediaEstimateParams(model, map[string]interface{}{"count": 2}); err == nil {
		t.Fatal("audio count caused reservation for unsupported multiple outputs")
	}
	if err := NormalizeMediaEstimateParams(model, map[string]interface{}{"count": 1}); err != nil {
		t.Fatal(err)
	}
}

func TestNativeMediaEmptyIncludeAndInternalFieldBoundary(t *testing.T) {
	model := &ModelFull{NewAPIModel: "native", NewAPIExtraParams: map[string]interface{}{"connection": map[string]interface{}{"api_key": "test-only"}}, RuntimeRule: map[string]interface{}{"upstream": map[string]interface{}{"adapter": "native_media", "include": []interface{}{}}}}
	params := map[string]interface{}{"prompt": "hello", "count": 2, "_price_rule_snapshot": map[string]interface{}{"unit_price": 3}, "connection": map[string]interface{}{"api_key": "test-only"}}
	for _, build := range []func(*ModelFull, map[string]interface{}) map[string]interface{}{BuildUpstreamVideoPayload, BuildUpstreamAudioPayload} {
		body := build(model, params)
		if len(body) != 2 || body["model"] != "native" || body["prompt"] != "hello" {
			t.Fatalf("explicit empty whitelist sent extra fields: %#v", body)
		}
	}
	delete(model.RuntimeRule["upstream"].(map[string]interface{}), "include")
	for _, build := range []func(*ModelFull, map[string]interface{}) map[string]interface{}{BuildUpstreamVideoPayload, BuildUpstreamAudioPayload} {
		body := build(model, params)
		if body["count"] != float64(2) || body["_price_rule_snapshot"] != nil || body["connection"] != nil {
			t.Fatalf("legacy inclusion failed internal boundary: %#v", body)
		}
	}
}

func TestReferenceImageTemplateEnforcesRuntimeMinimum(t *testing.T) {
	model := &ModelFull{RequestMode: "images", RuntimeRule: map[string]interface{}{"image": map[string]interface{}{"min_reference_images": 1, "max_reference_images": 7}}}
	for _, params := range []map[string]interface{}{{}, {"reference_images": []interface{}{""}}} {
		if err := NormalizeMediaTaskParams(model, params); err == nil {
			t.Fatalf("reference generation accepted missing images: %#v", params)
		}
	}
	if err := NormalizeMediaTaskParams(model, map[string]interface{}{"reference_images": []interface{}{"https://example.com/a.png"}}); err != nil {
		t.Fatal(err)
	}
}

func TestViduImageToVideoAllowsEmptyOptionalPrompt(t *testing.T) {
	model := &ModelFull{RequestMode: "video", RuntimeRule: map[string]interface{}{"video": map[string]interface{}{"upload_profile": "multi_ref", "min_reference_images": 1, "max_reference_images": 1, "prompt_required": false}}}
	params := map[string]interface{}{"prompt": "", "reference_images": []interface{}{"https://example.com/a.png"}}
	if err := NormalizeMediaTaskParams(model, params); err != nil {
		t.Fatalf("valid image-to-video optional prompt rejected: %v", err)
	}
}
