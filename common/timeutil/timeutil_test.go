package timeutil

import (
	"testing"
	"time"
)

func TestTimeConstants(t *testing.T) {
	tests := []struct {
		name     string
		got      time.Duration
		expected time.Duration
	}{
		{"Second", Second, time.Second},
		{"Minute", Minute, time.Minute},
		{"Hour", Hour, time.Hour},
		{"Day", Day, 24 * time.Hour},
		{"Week", Week, 7 * 24 * time.Hour},
		{"TwoWeeks", TwoWeeks, 14 * 24 * time.Hour},
		{"Month", Month, 30 * 24 * time.Hour},
		{"ThreeMonths", ThreeMonths, 90 * 24 * time.Hour},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.got != tt.expected {
				t.Errorf("%s = %v, want %v", tt.name, tt.got, tt.expected)
			}
		})
	}
}

func TestTimeConstantRelations(t *testing.T) {
	if Day != 24*Hour {
		t.Errorf("Day = %v, want %v", Day, 24*Hour)
	}
	if Week != 7*Day {
		t.Errorf("Week = %v, want %v", Week, 7*Day)
	}
	if TwoWeeks != 2*Week {
		t.Errorf("TwoWeeks = %v, want %v", TwoWeeks, 2*Week)
	}
	if Month != 30*Day {
		t.Errorf("Month = %v, want %v", Month, 30*Day)
	}
	if ThreeMonths != 3*Month {
		t.Errorf("ThreeMonths = %v, want %v", ThreeMonths, 3*Month)
	}
	if ThreeMonths != 90*Day {
		t.Errorf("ThreeMonths = %v, want %v", ThreeMonths, 90*Day)
	}
}
