package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// Keep task/file identifiers exact even when a gateway emits int64 JSON numbers.
func decodeMediaResponse(body []byte) (map[string]interface{}, error) {
	var raw map[string]interface{}
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.UseNumber()
	err := decoder.Decode(&raw)
	return raw, err
}

func upstreamMediaConfig(rule map[string]interface{}) map[string]interface{} {
	up, _ := rule["upstream"].(map[string]interface{})
	return up
}

// A small dot-path reader suffices for custom gateways; numeric segments index arrays.
func mediaResponseValue(value interface{}, path string) interface{} {
	for _, segment := range strings.Split(strings.TrimSpace(path), ".") {
		if segment == "" {
			return nil
		}
		switch current := value.(type) {
		case map[string]interface{}:
			value = current[segment]
		case []interface{}:
			index, err := strconv.Atoi(segment)
			if err != nil || index < 0 || index >= len(current) {
				return nil
			}
			value = current[index]
		default:
			return nil
		}
	}
	return value
}

func mappedMediaResponse(raw map[string]interface{}, mapping map[string]interface{}) map[string]interface{} {
	out := unwrapUpstreamBody(raw)
	if path, ok := mapping["usage"].(string); ok {
		if usage, ok := mediaResponseValue(raw, path).(map[string]interface{}); ok {
			out["usage"] = copyMap(usage)
		}
	}
	_, urlMapped := mapping["media_url"]
	_, base64Mapped := mapping["media_base64"]
	if urlMapped || base64Mapped {
		out["_explicit_media_response"] = true
		out["_mapped_media_data"] = nil
	}
	for field, pathValue := range mapping {
		path, _ := pathValue.(string)
		value := mediaResponseValue(raw, path)
		if value == nil {
			value = mediaResponseValue(unwrapUpstreamBody(raw), path)
		}
		if value == nil {
			continue
		}
		switch field {
		case "usage":
			continue
		case "input_tokens", "output_tokens", "total_tokens", "video_tokens", "input_seconds", "output_seconds", "total_seconds", "input_image_count":
			usage, _ := out["usage"].(map[string]interface{})
			if usage == nil {
				usage = map[string]interface{}{}
				out["usage"] = usage
			}
			usage[field] = value
		case "task_id", "status", "progress", "file_id":
			out[field] = value
		case "error":
			out["error_message"] = value
		case "media_url":
			if items := normalizedMappedMediaValues(value, false); len(items) > 0 {
				out["_mapped_media_data"] = items
			}
		case "media_base64":
			if out["_mapped_media_data"] == nil {
				if items := normalizedMappedMediaValues(value, true); len(items) > 0 {
					out["_mapped_media_data"] = items
				}
			}
		}
	}
	return out
}

func normalizedMappedMediaValues(value interface{}, encoded bool) []interface{} {
	values, ok := value.([]interface{})
	if !ok {
		if stringsValue, stringList := value.([]string); stringList {
			for _, item := range stringsValue {
				values = append(values, item)
			}
		} else {
			values = []interface{}{value}
		}
	}
	var items []interface{}
	for _, item := range values {
		if encoded {
			if data, ok := item.(string); ok && looksLikeEncodedMedia(data) {
				items = append(items, map[string]interface{}{"b64_json": data})
			}
		} else if _, valid := mediaItemFromValue(item, nil); valid {
			items = append(items, item)
		}
	}
	return items
}

func mediaResponseForRule(body []byte, rule map[string]interface{}) (map[string]interface{}, error) {
	raw, err := decodeMediaResponse(body)
	if err != nil {
		return nil, err
	}
	mapping, _ := upstreamMediaConfig(rule)["response_map"].(map[string]interface{})
	return mappedMediaResponse(raw, mapping), nil
}

func parseUpstreamMediaWithRule(body []byte, rule map[string]interface{}) ([]mediaItem, string) {
	raw, err := mediaResponseForRule(body, rule)
	if err != nil {
		if item, ok := rawAudioMediaItem(body); ok {
			return []mediaItem{item}, ""
		}
		return nil, ""
	}
	id := scalarString(raw, "task_no", "taskNo", "task_id", "taskId", "generation_id", "generationId", "job_id", "jobId", "prediction_id", "request_id", "id")
	if id == "" {
		id = nestedScalarString(raw, "task_no", "taskNo", "task_id", "taskId", "generation_id", "generationId", "job_id", "jobId", "prediction_id", "request_id", "id")
	}
	// Created/processing responses often echo reference images. These are inputs.
	if mediaBusinessErrorWithRule(raw, rule) != "" {
		return nil, id
	}
	if status := strings.ToLower(firstString(raw, "status", "state", "task_status")); mediaResponseAwaitingTask(status, id, rule) {
		return nil, id
	}
	return extractMediaItems(raw), id
}

func mediaResponseAwaitingTask(status, id string, rule map[string]interface{}) bool {
	if status == "" || mediaSuccessStatus(status, rule) {
		return false
	}
	if mediaFailureStatus(status, rule) || id != "" {
		return true
	}
	// Synchronous MiniMax audio uses data.status=2 as its completed-chunk marker.
	// Only an explicit task-status contract or a known pending state blocks a synchronous result.
	mapping, _ := upstreamMediaConfig(rule)["response_map"].(map[string]interface{})
	if _, explicit := mapping["status"]; explicit {
		return true
	}
	return mediaStatusIn(status, []string{"queued", "queueing", "pending", "created", "processing", "running", "in_progress", "not_start", "preparing"})
}

func mediaStatusIn(status string, values interface{}) bool {
	for _, item := range stringSlice(values) {
		if strings.EqualFold(strings.TrimSpace(item), status) {
			return true
		}
	}
	return false
}

func mediaSuccessStatus(status string, rule map[string]interface{}) bool {
	if configured := upstreamMediaConfig(rule)["success_statuses"]; len(stringSlice(configured)) > 0 {
		return mediaStatusIn(status, configured)
	}
	return mediaStatusIn(status, []string{"succeeded", "succeed", "success", "completed", "done", "finished", "5"})
}

func mediaFailureStatus(status string, rule map[string]interface{}) bool {
	if configured := upstreamMediaConfig(rule)["failure_statuses"]; len(stringSlice(configured)) > 0 {
		return mediaStatusIn(status, configured)
	}
	return mediaStatusIn(status, []string{"failed", "fail", "error", "cancelled", "canceled", "failure", "expired", "timeout", "deleted", "6"})
}

func mediaBusinessError(raw map[string]interface{}) string {
	return mediaBusinessErrorWithRule(raw, nil)
}

func mediaBusinessErrorWithRule(raw, rule map[string]interface{}) string {
	if base, ok := raw["base_resp"].(map[string]interface{}); ok {
		if _, exists := base["status_code"]; exists {
			if code, valid := mediaUsageNumber(base, "status_code"); !valid || code != 0 {
				return firstNonEmpty(firstString(base, "status_msg", "message"), "上游业务请求失败")
			}
		}
	}
	if detail, ok := raw["error"].(map[string]interface{}); ok {
		return firstString(detail, "message", "code")
	}
	if message, ok := raw["error"].(string); ok && strings.TrimSpace(message) != "" && strings.TrimSpace(message) != "0" && !mediaSuccessStatus(strings.ToLower(firstString(raw, "status", "state", "task_status")), rule) {
		return message
	}
	if message := firstString(raw, "error_message"); message != "" {
		if message == "0" {
			return ""
		}
		if base, ok := raw["base_resp"].(map[string]interface{}); ok && message == firstString(base, "status_msg") {
			if code, valid := mediaUsageNumber(base, "status_code"); valid && code == 0 {
				return ""
			}
		}
		return message
	}
	return ""
}

func upstreamUsageFromBodyWithRule(body []byte, rule map[string]interface{}) upstreamUsageDetails {
	raw, err := mediaResponseForRule(body, rule)
	if err != nil {
		return upstreamUsageDetails{}
	}
	normalized, _ := json.Marshal(raw)
	usage := upstreamUsageFromBody(normalized)
	if scale, valid := mediaUsageNumber(upstreamMediaConfig(rule), "output_seconds_scale"); valid && scale > 0 && usage.HasOutputSeconds {
		usage.OutputSeconds *= scale
	}
	return usage
}

func configuredMediaBusinessError(body []byte, rule map[string]interface{}) string {
	raw, err := mediaResponseForRule(body, rule)
	if err != nil {
		return ""
	}
	if message := mediaBusinessErrorWithRule(raw, rule); message != "" {
		return message
	}
	if mediaFailureStatus(strings.ToLower(firstString(raw, "status", "state", "task_status")), rule) {
		return firstNonEmpty(firstString(raw, "error_message", "message", "error", "err_code"), "上游任务失败")
	}
	return ""
}

// File retrieval is configured per gateway, so prefixed/forked APIs share the same credentials.
func resolveUpstreamMediaResult(ctx context.Context, conn connectionConfig, cfg pollConfig, raw map[string]interface{}) ([]mediaItem, error) {
	up := upstreamMediaConfig(cfg.RuntimeRule)
	path := strings.TrimSpace(stringAny(up["result_path"]))
	if path == "" {
		return nil, nil
	}
	fileID := scalarString(raw, "file_id")
	if strings.Contains(path, "{file_id}") && fileID == "" {
		return nil, nil
	}
	path = strings.ReplaceAll(path, "{file_id}", url.QueryEscape(fileID))
	path = strings.ReplaceAll(path, "{id}", url.QueryEscape(scalarString(raw, "task_id", "request_id", "id")))
	target := joinBaseEndpoint(conn.BaseURL, path)
	if !sameOriginURL(target, conn.BaseURL) {
		return nil, fmt.Errorf("结果查询地址必须与上游网关同源")
	}
	body, status, err := doJSONRequest(ctx, connectionForTaskPoll(conn), "GET", target, nil, 60*time.Second)
	if err != nil {
		return nil, err
	}
	if status >= 400 {
		return nil, fmt.Errorf("结果查询失败(HTTP %d): %s", status, upstreamErrorMessage(body))
	}
	result, err := decodeMediaResponse(body)
	if err != nil {
		if validDownloadedMedia("video", "", body) || detectRawAudioContentType(body) != "" {
			return []mediaItem{{URL: target}}, nil
		}
		return nil, fmt.Errorf("结果查询未返回有效媒体或 JSON: %w", err)
	}
	mapping, _ := up["result_response_map"].(map[string]interface{})
	result = mappedMediaResponse(result, mapping)
	if message := mediaBusinessErrorWithRule(result, cfg.RuntimeRule); message != "" {
		return nil, fmt.Errorf("%s", message)
	}
	if mediaFailureStatus(strings.ToLower(firstString(result, "status", "state", "task_status")), cfg.RuntimeRule) {
		return nil, fmt.Errorf("%s", firstNonEmpty(firstString(result, "message", "error"), "上游结果查询失败"))
	}
	return extractMediaItems(result), nil
}

func waitMediaPoll(ctx context.Context, interval time.Duration) bool {
	timer := time.NewTimer(interval)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

func resolveMediaPollConfig(rule map[string]interface{}, endpoint string, response []byte, baseURL string) pollConfig {
	config := parsePollConfig(rule, endpoint)
	if strings.TrimSpace(stringAny(upstreamMediaConfig(rule)["poll_path"])) == "" {
		if responsePath := upstreamPollPath(response, baseURL); responsePath != "" {
			config.Path = responsePath
		}
	}
	return config
}

func workerBillableSeconds(input map[string]interface{}) float64 {
	if actual, exists := input["_actual_output_seconds"]; exists {
		return max(0, floatAny(actual))
	}
	count := floatAny(firstNonNil(input["count"], input["n"]))
	if count <= 0 {
		count = 1
	}
	return workerDurationSeconds(input) * count
}

func shouldRepriceMediaUsage(usage upstreamUsageDetails, input map[string]interface{}) bool {
	if _, valid := mediaUsageTokens(input, "_actual_output_image_count"); valid {
		snapshot, frozen := input["_price_rule_snapshot"].(map[string]interface{})
		if !frozen || strings.EqualFold(strings.TrimSpace(stringAny(snapshot["billing_type"])), "per_image") {
			return true
		}
	}
	if !usage.hasAny() {
		return false
	}
	if snapshot, ok := input["_price_rule_snapshot"].(map[string]interface{}); ok && strings.EqualFold(strings.TrimSpace(stringAny(snapshot["billing_type"])), "per_token") && !usage.HasTokens {
		return false
	}
	return true
}

// These counters come from the completed attempt and persisted outputs, never task input.
func workerActualOutputBillingInput(input map[string]interface{}, requests, images int) map[string]interface{} {
	out := copyMap(input)
	delete(out, "_actual_request_count")
	delete(out, "_actual_output_image_count")
	out["_actual_request_count"] = max(1, requests)
	if images >= 0 {
		out["_actual_output_image_count"] = images
	}
	return out
}

func mediaUsageNumber(usage map[string]interface{}, keys ...string) (float64, bool) {
	for _, key := range keys {
		value := usage[key]
		if value == nil {
			continue
		}
		switch value.(type) {
		case json.Number, string, float64, float32, int, int64:
		default:
			continue
		}
		number, err := strconv.ParseFloat(fmt.Sprint(value), 64)
		if err == nil && !math.IsNaN(number) && !math.IsInf(number, 0) && number >= 0 {
			return number, true
		}
	}
	return 0, false
}

func mediaUsageTokens(usage map[string]interface{}, keys ...string) (float64, bool) {
	value, valid := mediaUsageNumber(usage, keys...)
	if !valid || math.Trunc(value) != value || value >= float64(int(^uint(0)>>1)) {
		return 0, false
	}
	return value, true
}

func normalizedMediaUsage(usage map[string]interface{}) upstreamUsageDetails {
	input, hasInput := mediaUsageTokens(usage, "prompt_tokens", "input_tokens", "text_tokens")
	output, hasOutput := mediaUsageTokens(usage, "completion_tokens", "output_tokens", "audio_tokens")
	total, hasTotal := mediaUsageTokens(usage, "video_tokens", "total_tokens")
	if !hasOutput && hasTotal {
		output = max(0, total-input)
	}
	if !hasTotal {
		total = input + output
	}
	result := upstreamUsageDetails{PromptTokens: int(input), OutputTokens: int(output), VideoTokens: int(total), HasTokens: hasInput || hasOutput || hasTotal, HasPromptTokens: hasInput, HasOutputTokens: hasOutput, HasVideoTokens: hasTotal}
	result.InputSeconds, result.HasInputSeconds = mediaUsageNumber(usage, "input_seconds")
	result.OutputSeconds, result.HasOutputSeconds = mediaUsageNumber(usage, "output_seconds")
	imageCount, hasImageCount := mediaUsageNumber(usage, "input_image_count")
	result.InputImageCount, result.HasInputImageCount = int(imageCount), hasImageCount
	if !result.HasOutputSeconds {
		if seconds, valid := mediaUsageNumber(usage, "total_seconds"); valid {
			result.OutputSeconds, result.HasOutputSeconds = max(0, seconds-result.InputSeconds), true
		}
	}
	return result
}

func mergeMediaUsage(previous, latest upstreamUsageDetails) upstreamUsageDetails {
	merged := previous
	if latest.HasPromptTokens {
		merged.PromptTokens, merged.HasPromptTokens = latest.PromptTokens, true
	}
	if latest.HasOutputTokens {
		merged.OutputTokens, merged.HasOutputTokens = latest.OutputTokens, true
	}
	if latest.HasVideoTokens {
		merged.VideoTokens, merged.HasVideoTokens = latest.VideoTokens, true
		if !latest.HasOutputTokens {
			merged.OutputTokens = max(0, latest.VideoTokens-merged.PromptTokens)
		}
	} else if latest.HasPromptTokens || latest.HasOutputTokens {
		merged.VideoTokens = merged.PromptTokens + merged.OutputTokens
	}
	merged.HasTokens = previous.HasTokens || latest.HasTokens
	if latest.HasInputSeconds {
		merged.InputSeconds, merged.HasInputSeconds = latest.InputSeconds, true
	}
	if latest.HasOutputSeconds {
		merged.OutputSeconds, merged.HasOutputSeconds = latest.OutputSeconds, true
	}
	if latest.HasInputImageCount {
		merged.InputImageCount, merged.HasInputImageCount = latest.InputImageCount, true
	}
	return merged
}

func mediaNeedsSecondsBilling(input, costRule map[string]interface{}) bool {
	price, _ := input["_price_rule_snapshot"].(map[string]interface{})
	return strings.EqualFold(strings.TrimSpace(stringAny(price["billing_type"])), "per_second") || strings.EqualFold(strings.TrimSpace(stringAny(costRule["billing_type"])), "per_second")
}

func measureMediaOutputSeconds(ctx context.Context, sources []string) (float64, error) {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	var total float64
	for _, source := range sources {
		seconds, err := probeComicAudioDuration(ctx, source)
		if err != nil {
			return 0, err
		}
		total += seconds
	}
	if total <= 0 {
		return 0, fmt.Errorf("未能读取产物时长")
	}
	return total, nil
}
