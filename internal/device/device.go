// Package device generates the device identity the Trade Republic web client
// sends with every authentication request.
package device

import (
	"crypto/rand"
	"crypto/sha512"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
)

// Info returns a base64-encoded x-tr-device-info value holding a random stable
// device id. Store it in the configuration to reuse the same identity across
// runs, which is what the web client does.
func Info() (string, error) {
	seed := make([]byte, 16)
	if _, err := rand.Read(seed); err != nil {
		return "", err
	}
	sum := sha512.Sum512(seed)
	payload, err := json.Marshal(map[string]string{
		"stableDeviceId": hex.EncodeToString(sum[:]),
	})
	if err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(payload), nil
}
