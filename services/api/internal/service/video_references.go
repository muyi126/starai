package service

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

var videoReferencePattern = regexp.MustCompile(`(?i)@(图片|圖片|图像|视频|視頻|音频|音頻|image|video|audio)([0-9]+|\?)`)
var videoReferenceFamily = regexp.MustCompile(`(?i)seedance[_-]2[._-][05]|minimax[_-]h3`)

func supportsVideoPromptReferences(model *ModelFull) bool {
	adapter := strings.ToLower(parseUpstreamConfig(model.RuntimeRule).Adapter)
	return adapter == "volcengine_seedance_2" || adapter == "topenrouter_seedance_2" || adapter == "minimax_h3_v2" ||
		(adapter == "zex_video" && videoReferenceFamily.MatchString(model.Code+" "+model.NewAPIModel+" "+fmt.Sprint(model.RuntimeRule["template_key"])))
}

// Video agents reserve a workflow balance instead of calling TaskService.Create.
// Validate their submitted references before that reservation as well.
func (s *AgentService) validateWorkflowVideoReferences(ctx context.Context, runtime, inputs map[string]interface{}) error {
	if !videoReferencePattern.MatchString(stringValue(inputs["prompt"])) {
		return nil
	}
	code := stringValue(runtime["generation_model_code"])
	if code == "" {
		return nil
	}
	model, err := NewModelService(s.db).GetFullByCode(ctx, code)
	if err != nil {
		return err
	}
	if model.RequestMode != "video" || !supportsVideoPromptReferences(model) {
		return nil
	}
	return ValidateVideoParams(model, MergeMediaTaskParams(model.DefaultParams, inputs))
}

// Validate references before freezing funds. Conversion belongs to the worker,
// where the final route (including fallback channels) is known.
func validateVideoPromptReferences(model *ModelFull, cfg videoRuntimeConfig, params map[string]interface{}) error {
	adapter := strings.ToLower(parseUpstreamConfig(model.RuntimeRule).Adapter)
	native := adapter == "volcengine_seedance_2" || adapter == "topenrouter_seedance_2" || adapter == "minimax_h3_v2"
	if !supportsVideoPromptReferences(model) {
		return nil
	}
	prompt, _ := params["prompt"].(string)
	counts := map[string]int{}
	seen := map[string]bool{}
	add := func(kind string, raw interface{}) {
		urls, _ := zexMediaURLs(raw)
		for _, value := range urls {
			value = strings.TrimSpace(value)
			if value != "" && (!native || !seen[value]) {
				counts[kind]++
				seen[value] = true
			}
		}
	}
	mode := strings.ToLower(strings.TrimSpace(stringValue(params[cfg.ModeParam])))
	if mode == "first_frame" || mode == "first_last" {
		add("image", params[cfg.FirstFrameKey])
	}
	if mode == "last_frame" || mode == "first_last" {
		add("image", params[cfg.LastFrameKey])
	}
	if mode == "reference" || strings.Contains(mode, "image") || strings.Contains(mode, "video") || mode == "audio" {
		if native && adapter != "minimax_h3_v2" {
			if id := strings.TrimSpace(stringValue(params["portrait_asset_id"])); id != "" {
				kind := "image"
				if params["portrait_asset_type"] == "video" {
					kind = "video"
				}
				add(kind, "asset://"+strings.TrimPrefix(id, "asset://"))
			}
		}
		for _, field := range [][2]string{{"image", cfg.ReferenceImagesKey}, {"video", cfg.ReferenceVideosKey}, {"audio", cfg.ReferenceAudiosKey}} {
			if mode == "reference" || strings.Contains(mode, field[0]) {
				add(field[0], params[field[1]])
			}
		}
	}
	end := -1
	for _, location := range videoReferencePattern.FindAllStringSubmatchIndex(prompt, -1) {
		// Do not interpret an email address as a material mention.
		if location[0] != end && location[0] > 0 && strings.ContainsRune("abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789_@", rune(prompt[location[0]-1])) {
			continue
		}
		if location[1] < len(prompt) && strings.ContainsRune("abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789_", rune(prompt[location[1]])) {
			continue
		}
		end = location[1]
		label, number := prompt[location[2]:location[3]], prompt[location[4]:location[5]]
		kind := "audio"
		switch strings.ToLower(label) {
		case "图片", "圖片", "图像", "image":
			kind = "image"
		case "视频", "視頻", "video":
			kind = "video"
		}
		index, err := strconv.Atoi(number)
		if err != nil || index < 1 || index > counts[kind] {
			return fmt.Errorf("素材引用 @%s%s 无效，请重新选择当前素材组合中的素材", label, number)
		}
	}
	return nil
}
