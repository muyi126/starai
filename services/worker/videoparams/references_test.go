package videoparams

import "testing"

func TestReferencePromptUsesFinalRouteWithoutMutatingTask(t *testing.T) {
	input := map[string]interface{}{"prompt": "@图片1的主体，参考@video1；@音频1。x@image9.com", "generation_mode": "image_video_audio", "reference_images": []string{"https://e.test/i"}, "reference_videos": []string{"https://e.test/v"}, "reference_audios": []string{"https://e.test/a"}, "duration": 5}
	for _, tc := range []struct{ adapter, want string }{
		{"zex_video", "图片 1的主体，参考视频 1；音频 1。x@image9.com"},
		{"volcengine_seedance_2", "图片1的主体，参考视频1；音频1。x@image9.com"},
		{"topenrouter_seedance_2", "图片1的主体，参考视频1；音频1。x@image9.com"},
		{"minimax_h3_v2", "<Picture 1>的主体，参考<Video 1>；<Audio 1>。x@image9.com"},
	} {
		t.Run(tc.adapter, func(t *testing.T) {
			params := map[string]interface{}{}
			for key, value := range input {
				params[key] = value
			}
			if tc.adapter == "minimax_h3_v2" {
				params["generation_mode"] = "reference"
			}
			rule := map[string]interface{}{"upstream": map[string]interface{}{"adapter": tc.adapter}}
			payload := BuildUpstreamVideoPayload("seedance-2.0", "seedance-2.0", rule, nil, params)
			prompt, _ := payload["prompt"].(string)
			if content, ok := payload["content"].([]interface{}); ok {
				prompt, _ = content[0].(map[string]interface{})["text"].(string)
			}
			if prompt != tc.want {
				t.Fatalf("prompt=%q, want=%q", prompt, tc.want)
			}
			if params["prompt"] != input["prompt"] {
				t.Fatal("route mutated original task prompt")
			}
		})
	}
	for _, adapter := range []string{"native_media", "unknown"} {
		if got := convertVideoReferencePrompt("@image1", adapter, "seedance-2.0"); got != "@image1" {
			t.Fatal(got)
		}
	}
	if got := convertVideoReferencePrompt("@image1", "zex_video", "grok-imagine-video-1.5"); got != "@image1" {
		t.Fatal(got)
	}
}

func TestAdjacentReferencesConvertWithoutChangingEmails(t *testing.T) {
	got := convertVideoReferencePrompt("@图片1@视频2 @audio1 x@image9.com 123@image9.com @image9suffix", "minimax_h3_v2", "minimax-h3")
	want := "<Picture 1><Video 2> <Audio 1> x@image9.com 123@image9.com @image9suffix"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}
