package core

import (
	"reflect"
	"testing"
)

func TestToStringList(t *testing.T) {
	tests := []struct {
		input    []string
		expected []interface{}
	}{
		{[]string{"a", "b", "c"}, []interface{}{"a", "b", "c"}},
		{[]string{}, []interface{}{}},
		{[]string{"single"}, []interface{}{"single"}},
		{nil, []interface{}{}},
	}

	for _, tt := range tests {
		t.Run("", func(t *testing.T) {
			got := toStringList(tt.input)
			if len(got) != len(tt.expected) {
				t.Fatalf("expected %d elements, got %d", len(tt.expected), len(got))
			}
			if !reflect.DeepEqual(got, tt.expected) {
				t.Errorf("toStringList(%v) = %v, want %v", tt.input, got, tt.expected)
			}
		})
	}
}
