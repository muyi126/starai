package videoparams

import (
	"fmt"
	"regexp"
	"strings"
)

var videoReferencePattern = regexp.MustCompile(`(?i)@(图片|圖片|图像|视频|視頻|音频|音頻|image|video|audio)([0-9]+)`)
var videoReferenceFamily = regexp.MustCompile(`(?i)seedance[_-]2[._-][05]|minimax[_-]h3`)

func convertVideoReferencePrompt(prompt, adapter, model string) string {
	adapter = strings.ToLower(adapter)
	nativeSeedance := adapter == "volcengine_seedance_2" || adapter == "topenrouter_seedance_2"
	nativeMiniMax := adapter == "minimax_h3_v2"
	if !nativeSeedance && !nativeMiniMax && !(adapter == "zex_video" && videoReferenceFamily.MatchString(model)) {
		return prompt
	}
	matches := [][]int{}
	end := -1
	for _, m := range videoReferencePattern.FindAllStringSubmatchIndex(prompt, -1) {
		if m[0] != end && m[0] > 0 && strings.ContainsRune("abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789_@", rune(prompt[m[0]-1])) {
			continue
		}
		if m[1] < len(prompt) && strings.ContainsRune("abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789_", rune(prompt[m[1]])) {
			continue
		}
		end = m[1]
		matches = append(matches, m)
	}
	// Replace backwards so offsets remain correct; preserve punctuation and plain text.
	for i := len(matches) - 1; i >= 0; i-- {
		m := matches[i]
		label, number := strings.ToLower(prompt[m[2]:m[3]]), prompt[m[4]:m[5]]
		kind, tag := "音频", "Audio"
		switch label {
		case "图片", "圖片", "图像", "image":
			kind, tag = "图片", "Picture"
		case "视频", "視頻", "video":
			kind, tag = "视频", "Video"
		}
		replacement := kind + " " + number
		if nativeSeedance {
			replacement = kind + number
		}
		if nativeMiniMax {
			replacement = fmt.Sprintf("<%s %s>", tag, number)
		}
		prompt = prompt[:m[0]] + replacement + prompt[m[5]:]
	}
	return prompt
}
