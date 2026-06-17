package version

import (
	"testing"
)

func TestCompare(t *testing.T) {
	cases := []struct {
		name string
		a, b string
		want int
	}{
		// Date ordering.
		{"older date", "20260510", "20260511", -1},
		{"newer date", "20260512", "20260511", 1},
		{"equal release", "20260511", "20260511", 0},

		// Same-date suffix priority (HF > CVE > "" > DEP > DEV).
		{"plain vs HF", "20260511", "20260511HF", -1},
		{"plain vs CVE", "20260511", "20260511CVE", -1},
		{"CVE vs HF", "20260511CVE", "20260511HF", -1},
		{"plain vs DEP", "20260511", "20260511DEP", 1},
		{"plain vs DEV", "20260511", "20260511DEV", 1},
		{"HF vs DEV", "20260511DEV", "20260511HF", -1},
		{"DEP vs DEV", "20260511DEV", "20260511DEP", -1},

		// Hash does not affect release ordering.
		{"release vs dev-same-day", "20260511", "20260511DEV-abc1234", 1},
		{"dev hash ordering stable", "20260511DEV-aaa", "20260511DEV-bbb", -1},

		// Cross-date suffix ordering still dominated by date.
		{"old HF vs new plain", "20260510HF", "20260511", -1},
		{"new DEV vs old HF", "20260511DEV-xxx", "20260510HF", 1},

		// Malformed input falls back to lexical comparison.
		{"both malformed", "abc", "def", -1},
		{"malformed sorts after release", "zzz", "20260511", 1}, // 'z' > '2'
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := Compare(tc.a, tc.b)
			// Normalise so we only compare sign — callers only care about
			// -1/0/+1, not the magnitude.
			if tc.want == 0 && got != 0 {
				t.Errorf("Compare(%q, %q) = %d, want 0", tc.a, tc.b, got)
			} else if tc.want < 0 && got >= 0 {
				t.Errorf("Compare(%q, %q) = %d, want < 0", tc.a, tc.b, got)
			} else if tc.want > 0 && got <= 0 {
				t.Errorf("Compare(%q, %q) = %d, want > 0", tc.a, tc.b, got)
			}
		})
	}
}

func TestIsDevBuild(t *testing.T) {
	cases := []struct {
		v    string
		want bool
	}{
		{"dev", true},
		{"20260511DEV-abc1234", true},
		{"20260511DEP-deadbee", true},
		{"20260511", false},
		{"20260511HF", false},
		{"20260511CVE", false},
		{"20260511DEP", false},
		{"", false},
	}
	for _, c := range cases {
		if got := IsDevBuild(c.v); got != c.want {
			t.Errorf("IsDevBuild(%q) = %v, want %v", c.v, got, c.want)
		}
	}
}

func TestIsUpgradeable(t *testing.T) {
	cases := []struct {
		name            string
		current, latest string
		want            bool
	}{
		{"upgrade across dates", "20260510", "20260511", true},
		{"same version", "20260511HF", "20260511HF", false},
		{"downgrade protection across dates", "20260511HF", "20260510", false},
		{"downgrade protection same date (HF to plain)", "20260511HF", "20260511", false},
		{"same date CVE to HF upgrade", "20260511CVE", "20260511HF", true},

		// Dev rules: dev -> release always allowed, release -> dev never.
		{"dev to release", "20260511DEV-abc1234", "20260511", true},
		{"release to dev", "20260511", "20260511DEV-abc1234", false},
		{"dev to dev", "20260511DEV-aaa", "20260512DEV-bbb", false},
		{"literal dev to release", "dev", "20260511", true},
		{"release to literal dev", "20260511", "dev", false},

		// Edge cases.
		{"empty latest", "20260511", "", false},
		{"dev current empty latest", "20260511DEV-aaa", "", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := IsUpgradeable(c.current, c.latest); got != c.want {
				t.Errorf("IsUpgradeable(%q, %q) = %v, want %v", c.current, c.latest, got, c.want)
			}
		})
	}
}
