package service

import "testing"

func TestSeedanceAccessKeyIsAlwaysRedacted(t *testing.T) {
	if !isSensitiveConfigKey("seedance_volc_access_key") || !isSensitiveDetailKey("seedance_volc_access_key") {
		t.Fatal("Seedance Access Key must be masked in admin responses and operation logs")
	}
}
