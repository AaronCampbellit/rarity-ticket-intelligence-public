package setup

import (
	"encoding/json"
	"testing"
)

func TestVerificationSafeDiffIsTypedJSON(t *testing.T) {
	body, err := verificationSafeDiff("object_storage", "storage_verified")
	if err != nil {
		t.Fatal(err)
	}
	var value map[string]string
	if err := json.Unmarshal(body, &value); err != nil {
		t.Fatal(err)
	}
	if value["section"] != "object_storage" ||
		value["result"] != "storage_verified" || len(value) != 2 {
		t.Fatalf("safe diff=%v", value)
	}
}
