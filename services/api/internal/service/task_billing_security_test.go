package service

import (
	"context"
	"strings"
	"testing"
)

func TestCreateTaskRejectsClientBillingControlsBeforeModelLookup(t *testing.T) {
	svc := &TaskService{}
	for _, key := range []string{
		"_skip_billing", "_workflow_project", "_product_refine",
		"_billing_reservation", "_execution_budget", "_actual_output_seconds",
		"_actual_video_tokens", "_actual_input_image_count",
		"_actual_request_count", "_actual_output_image_count",
		"_price_rule_snapshot", "_estimated_input_tokens", "_estimated_output_tokens",
		"_billing_item_count", "estimated_input_tokens", "estimated_output_tokens",
	} {
		t.Run(key, func(t *testing.T) {
			_, err := svc.Create(context.Background(), 1, CreateTaskInput{
				ModelCode: "test", Params: map[string]interface{}{key: true},
			})
			if err == nil || !strings.Contains(err.Error(), key) {
				t.Fatalf("client billing control was accepted: %v", err)
			}
		})
	}
}
