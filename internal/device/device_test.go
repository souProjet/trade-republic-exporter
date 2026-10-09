package device

import (
	"encoding/base64"
	"encoding/json"
	"testing"
)

func TestInfo(t *testing.T) {
	first, err := Info()
	if err != nil {
		t.Fatalf("Info: %v", err)
	}

	raw, err := base64.StdEncoding.DecodeString(first)
	if err != nil {
		t.Fatalf("Info is not base64: %v", err)
	}
	var payload map[string]string
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatalf("Info payload is not JSON: %v", err)
	}
	if len(payload["stableDeviceId"]) != 128 {
		t.Errorf("stableDeviceId has %d characters, want 128", len(payload["stableDeviceId"]))
	}

	second, err := Info()
	if err != nil {
		t.Fatal(err)
	}
	if first == second {
		t.Error("two calls returned the same device id")
	}
}
