package config

import (
	"errors"
	"fmt"
	"runtime"

	"github.com/zalando/go-keyring"
)

const keyringService = "trade-republic-exporter"

// KeychainName is the user-facing name of the platform's secret store.
func KeychainName() string {
	switch runtime.GOOS {
	case "darwin":
		return "macOS Keychain"
	case "windows":
		return "Windows Credential Manager"
	default:
		return "system keyring"
	}
}

// LoadPIN reads the PIN stored for a phone number. A missing entry returns an
// empty PIN and no error.
func LoadPIN(phone string) (string, error) {
	if phone == "" {
		return "", nil
	}
	pin, err := keyring.Get(keyringService, phone)
	if errors.Is(err, keyring.ErrNotFound) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("read PIN from the %s: %w", KeychainName(), err)
	}
	return pin, nil
}

// SavePIN stores the PIN for a phone number.
func SavePIN(phone, pin string) error {
	if phone == "" {
		return errors.New("set account.phone_number before storing a PIN")
	}
	if err := validatePIN(pin); err != nil {
		return err
	}
	if err := keyring.Set(keyringService, phone, pin); err != nil {
		return fmt.Errorf("store PIN in the %s: %w (set %s in the environment instead)", KeychainName(), err, EnvPIN)
	}
	return nil
}

// DeletePIN removes the PIN stored for a phone number. Deleting a missing
// entry is not an error.
func DeletePIN(phone string) error {
	if phone == "" {
		return nil
	}
	err := keyring.Delete(keyringService, phone)
	if err != nil && !errors.Is(err, keyring.ErrNotFound) {
		return fmt.Errorf("delete PIN from the %s: %w", KeychainName(), err)
	}
	return nil
}
