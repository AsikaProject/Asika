package utils

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestGenerateUUID(t *testing.T) {
	uuid1 := GenerateUUID()
	uuid2 := GenerateUUID()

	if uuid1 == "" {
		t.Error("GenerateUUID should return non-empty string")
	}
	if uuid2 == "" {
		t.Error("GenerateUUID should return non-empty string")
	}
	if uuid1 == uuid2 {
		t.Error("GenerateUUID should return unique values")
	}
	// UUID format check (simple)
	if !strings.Contains(uuid1, "-") {
		t.Errorf("GenerateUUID should return valid UUID format, got %q", uuid1)
	}
}

func TestFormatTime(t *testing.T) {
	now := time.Now()
	formatted := FormatTime(now)

	if formatted == "" {
		t.Error("FormatTime should return non-empty string")
	}
	// Check format (simple)
	if len(formatted) != len("2006-01-02 15:04:05") {
		t.Errorf("FormatTime returned unexpected format: %q", formatted)
	}
}

func TestParseDuration(t *testing.T) {
	tests := []struct {
		input    string
		defaultD time.Duration
		want     time.Duration
	}{
		{"5m", 30 * time.Second, 5 * time.Minute},
		{"1h", 30 * time.Second, 1 * time.Hour},
		{"", 30 * time.Second, 30 * time.Second},
		{"invalid", 30 * time.Second, 30 * time.Second},
		{"30s", 1 * time.Minute, 30 * time.Second},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := ParseDuration(tt.input, tt.defaultD)
			if got != tt.want {
				t.Errorf("ParseDuration(%q, %v) = %v, want %v", tt.input, tt.defaultD, got, tt.want)
			}
		})
	}
}

func TestSplitRepo(t *testing.T) {
	tests := []struct {
		input     string
		wantOwner string
		wantRepo  string
		wantErr   bool
	}{
		{"owner/repo", "owner", "repo", false},
		{"org/subgroup/repo", "org", "subgroup/repo", false}, // Split only first /
		{"invalid", "", "", true},
		{"owner/", "owner", "", false}, // Allow empty repo?
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			owner, repo, err := SplitRepo(tt.input)
			if tt.wantErr && err == nil {
				t.Errorf("SplitRepo(%q) should return error", tt.input)
			}
			if !tt.wantErr && err != nil {
				t.Errorf("SplitRepo(%q) unexpected error: %v", tt.input, err)
			}
			if owner != tt.wantOwner {
				t.Errorf("owner = %q, want %q", owner, tt.wantOwner)
			}
			if repo != tt.wantRepo {
				t.Errorf("repo = %q, want %q", repo, tt.wantRepo)
			}
		})
	}
}

func TestJoinRepo(t *testing.T) {
	result := JoinRepo("owner", "repo")
	if result != "owner/repo" {
		t.Errorf("JoinRepo = %q, want owner/repo", result)
	}
}

func TestStringSliceContains(t *testing.T) {
	slice := []string{"a", "b", "c"}

	tests := []struct {
		value string
		want  bool
	}{
		{"a", true},
		{"b", true},
		{"c", true},
		{"d", false},
		{"", false},
	}

	for _, tt := range tests {
		t.Run(tt.value, func(t *testing.T) {
			got := StringSliceContains(slice, tt.value)
			if got != tt.want {
				t.Errorf("StringSliceContains(%q) = %v, want %v", tt.value, got, tt.want)
			}
		})
	}
}

func TestUniqueStrings(t *testing.T) {
	tests := []struct {
		input []string
		want  []string
	}{
		{[]string{"a", "b", "a", "c", "b"}, []string{"a", "b", "c"}},
		{[]string{"x", "y", "z"}, []string{"x", "y", "z"}},
		{[]string{}, []string{}},
		{[]string{"a", "a", "a"}, []string{"a"}},
	}

	for _, tt := range tests {
		t.Run(strings.Join(tt.input, ","), func(t *testing.T) {
			got := UniqueStrings(tt.input)
			if len(got) != len(tt.want) {
				t.Errorf("UniqueStrings() length = %d, want %d", len(got), len(tt.want))
			}
			for i, v := range got {
				if v != tt.want[i] {
					t.Errorf("UniqueStrings()[%d] = %q, want %q", i, v, tt.want[i])
				}
			}
		})
	}
}

func TestTruncateString(t *testing.T) {
	tests := []struct {
		input  string
		maxLen int
		want   string
	}{
		{"short", 10, "short"},
		{"exactly 10", 10, "exactly 10"}, // Length check
		{"this is a long string", 10, "this is..."},
		{"test", 4, "test"},
		{"ab", 5, "ab"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := TruncateString(tt.input, tt.maxLen)
			if got != tt.want {
				t.Errorf("TruncateString(%q, %d) = %q, want %q", tt.input, tt.maxLen, got, tt.want)
			}
		})
	}
}

func TestParseBool(t *testing.T) {
	tests := []struct {
		input      string
		defaultVal bool
		want       bool
	}{
		{"true", false, true},
		{"false", true, false},
		{"1", false, true},
		{"0", true, false},
		{"yes", false, true},
		{"no", true, false},
		{"", true, true},
		{"", false, false},
		{"invalid", true, true},
		{"invalid", false, false},
	}

	for _, tt := range tests {
		name := tt.input + "_" + func() string {
			if tt.defaultVal {
				return "true"
			}
			return "false"
		}()
		t.Run(name, func(t *testing.T) {
			got := ParseBool(tt.input, tt.defaultVal)
			if got != tt.want {
				t.Errorf("ParseBool(%q, %v) = %v, want %v", tt.input, tt.defaultVal, got, tt.want)
			}
		})
	}
}

func TestDefaultRetryConfig(t *testing.T) {
	config := DefaultRetryConfig()

	if config.MaxRetries != 3 {
		t.Errorf("MaxRetries = %d, want 3", config.MaxRetries)
	}
	if config.InitialWait != 1*time.Second {
		t.Errorf("InitialWait = %v, want 1s", config.InitialWait)
	}
	if config.Backoff != 2.0 {
		t.Errorf("Backoff = %v, want 2.0", config.Backoff)
	}
}

func TestRetry_SuccessFirstTry(t *testing.T) {
	config := RetryConfig{
		MaxRetries:  3,
		InitialWait: 10 * time.Millisecond,
		MaxWait:     100 * time.Millisecond,
		Backoff:     2.0,
	}

	callCount := 0
	err := Retry(context.Background(), config, func() error {
		callCount++
		return nil
	})

	if err != nil {
		t.Errorf("Retry should succeed, got error: %v", err)
	}
	if callCount != 1 {
		t.Errorf("Retry should call function 1 time, got %d", callCount)
	}
}

func TestRetry_SuccessAfterRetries(t *testing.T) {
	config := RetryConfig{
		MaxRetries:  3,
		InitialWait: 10 * time.Millisecond,
		MaxWait:     100 * time.Millisecond,
		Backoff:     2.0,
	}

	callCount := 0
	err := Retry(context.Background(), config, func() error {
		callCount++
		if callCount < 3 {
			return context.DeadlineExceeded // Simulate error
		}
		return nil
	})

	if err != nil {
		t.Errorf("Retry should succeed after retries, got error: %v", err)
	}
	if callCount != 3 {
		t.Errorf("Retry should call function 3 times, got %d", callCount)
	}
}

func TestRetry_AllAttemptsFail(t *testing.T) {
	config := RetryConfig{
		MaxRetries:  2,
		InitialWait: 10 * time.Millisecond,
		MaxWait:     100 * time.Millisecond,
		Backoff:     2.0,
	}

	callCount := 0
	err := Retry(context.Background(), config, func() error {
		callCount++
		return context.DeadlineExceeded
	})

	if err == nil {
		t.Error("Retry should return error when all attempts fail")
	}
	if callCount != 3 { // MaxRetries + 1 initial attempt
		t.Errorf("Retry should call function %d times, got %d", 3, callCount)
	}
}

func TestRetryWithFixedDelay(t *testing.T) {
	callCount := 0
	err := RetryWithFixedDelay(context.Background(), 2, 10*time.Millisecond, func() error {
		callCount++
		if callCount < 2 {
			return context.DeadlineExceeded
		}
		return nil
	})

	if err != nil {
		t.Errorf("RetryWithFixedDelay should succeed, got error: %v", err)
	}
	if callCount != 2 {
		t.Errorf("RetryWithFixedDelay should call function 2 times, got %d", callCount)
	}
}
