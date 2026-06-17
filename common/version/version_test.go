package version

import "testing"

func TestVersion(t *testing.T) {
	if Version == "" {
		t.Error("Version should not be empty")
	}
}

func TestVersionDefault(t *testing.T) {
	// Default value is "dev"
	if Version != "dev" {
		t.Errorf("Default Version = %q, want dev", Version)
	}
}

func TestChannelDefault(t *testing.T) {
	if Channel != "manual" {
		t.Errorf("Default Channel = %q, want manual", Channel)
	}
}
