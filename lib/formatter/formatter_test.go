package formatter

import (
	"bytes"
	"reflect"
	"strings"
	"testing"
)

func TestParseFormat(t *testing.T) {
	tests := []struct {
		input    string
		expected OutputFormat
	}{
		{"json", FormatJSON},
		{"JSON", FormatJSON},
		{"Json", FormatJSON},
		{"yaml", FormatYAML},
		{"YAML", FormatYAML},
		{"table", FormatTable},
		{"TABLE", FormatTable},
		{"csv", FormatTable},
		{"", FormatTable},
		{"anything", FormatTable},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := ParseFormat(tt.input)
			if got != tt.expected {
				t.Errorf("ParseFormat(%q) = %q, want %q", tt.input, got, tt.expected)
			}
		})
	}
}

func TestNewFormatter(t *testing.T) {
	var buf bytes.Buffer
	f := NewFormatter("json", &buf)
	if f.format != FormatJSON {
		t.Errorf("format = %q, want json", f.format)
	}
	if f.writer != &buf {
		t.Error("writer not set correctly")
	}
}

type testStruct struct {
	Name  string `json:"name"`
	Age   int    `json:"age"`
	Email string `json:"email,omitempty"`
}

type testStructUnexported struct {
	name string
	Age  int
}

type testStructWithComplex struct {
	Name  string            `json:"name"`
	Tags  []string          `json:"tags"`
	Data  map[string]string `json:"data"`
	Inner testStruct        `json:"inner"`
	Score int               `json:"score"`
}

func TestOutput_JSON(t *testing.T) {
	var buf bytes.Buffer
	f := NewFormatter("json", &buf)

	data := testStruct{Name: "Alice", Age: 30, Email: "alice@example.com"}
	err := f.Output(data)
	if err != nil {
		t.Fatalf("Output failed: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, `"name": "Alice"`) {
		t.Errorf("JSON output missing name: %s", output)
	}
	if !strings.Contains(output, `"age": 30`) {
		t.Errorf("JSON output missing age: %s", output)
	}
}

func TestOutput_YAML(t *testing.T) {
	var buf bytes.Buffer
	f := NewFormatter("yaml", &buf)

	data := testStruct{Name: "Bob", Age: 25, Email: "bob@example.com"}
	err := f.Output(data)
	if err != nil {
		t.Fatalf("Output failed: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "name: Bob") {
		t.Errorf("YAML output missing name: %s", output)
	}
}

func TestOutput_Table_Nil(t *testing.T) {
	var buf bytes.Buffer
	f := NewFormatter("table", &buf)

	err := f.Output(nil)
	if err != nil {
		t.Fatalf("Output failed: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "No data") {
		t.Errorf("expected 'No data' for nil, got: %s", output)
	}
}

func TestOutput_Table_EmptySlice(t *testing.T) {
	var buf bytes.Buffer
	f := NewFormatter("table", &buf)

	err := f.Output([]testStruct{})
	if err != nil {
		t.Fatalf("Output failed: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "No data") {
		t.Errorf("expected 'No data' for empty slice, got: %s", output)
	}
}

func TestOutput_Table_StructSlice(t *testing.T) {
	var buf bytes.Buffer
	f := NewFormatter("table", &buf)

	data := []testStruct{
		{Name: "Alice", Age: 30, Email: "alice@example.com"},
		{Name: "Bob", Age: 25, Email: "bob@example.com"},
	}
	err := f.Output(data)
	if err != nil {
		t.Fatalf("Output failed: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "name") {
		t.Errorf("table output missing header 'name': %s", output)
	}
	if !strings.Contains(output, "Alice") {
		t.Errorf("table output missing Alice: %s", output)
	}
	if !strings.Contains(output, "Bob") {
		t.Errorf("table output missing Bob: %s", output)
	}
}

func TestOutput_Table_SingleStruct(t *testing.T) {
	var buf bytes.Buffer
	f := NewFormatter("table", &buf)

	data := testStruct{Name: "Charlie", Age: 35, Email: "charlie@example.com"}
	err := f.Output(data)
	if err != nil {
		t.Fatalf("Output failed: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "Name") {
		t.Errorf("table output missing Name: %s", output)
	}
	if !strings.Contains(output, "Charlie") {
		t.Errorf("table output missing Charlie: %s", output)
	}
}

func TestOutput_Table_MapSlice(t *testing.T) {
	var buf bytes.Buffer
	f := NewFormatter("table", &buf)

	data := []map[string]interface{}{
		{"name": "Alice", "score": 95},
		{"name": "Bob", "score": 87},
	}
	err := f.Output(data)
	if err != nil {
		t.Fatalf("Output failed: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "name") {
		t.Errorf("table output missing 'name' header: %s", output)
	}
	if !strings.Contains(output, "Alice") {
		t.Errorf("table output missing Alice: %s", output)
	}
}

func TestOutput_Table_SingleMap(t *testing.T) {
	var buf bytes.Buffer
	f := NewFormatter("table", &buf)

	data := map[string]interface{}{
		"name":  "Dave",
		"score": 100,
	}
	err := f.Output(data)
	if err != nil {
		t.Fatalf("Output failed: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "name") {
		t.Errorf("table output missing 'name': %s", output)
	}
	if !strings.Contains(output, "Dave") {
		t.Errorf("table output missing Dave: %s", output)
	}
}

func TestOutput_Table_SimpleSlice(t *testing.T) {
	var buf bytes.Buffer
	f := NewFormatter("table", &buf)

	data := []string{"apple", "banana", "cherry"}
	err := f.Output(data)
	if err != nil {
		t.Fatalf("Output failed: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "apple") {
		t.Errorf("output missing apple: %s", output)
	}
}

func TestOutput_Table_Primitive(t *testing.T) {
	var buf bytes.Buffer
	f := NewFormatter("table", &buf)

	err := f.Output(42)
	if err != nil {
		t.Fatalf("Output failed: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "42") {
		t.Errorf("output missing 42: %s", output)
	}
}

func TestExtractStructFields(t *testing.T) {
	typ := reflect.TypeOf(testStruct{})
	fields := extractStructFields(typ)
	if len(fields) != 3 {
		t.Errorf("expected 3 fields, got %d: %v", len(fields), fields)
	}
	if fields[0] != "name" {
		t.Errorf("first field = %q, want 'name'", fields[0])
	}
}

func TestExtractStructFields_WithComplex(t *testing.T) {
	typ := reflect.TypeOf(testStructWithComplex{})
	fields := extractStructFields(typ)
	expected := []string{"name", "score"}
	if len(fields) != len(expected) {
		t.Errorf("expected %d fields, got %d: %v", len(expected), len(fields), fields)
	}
	for i, f := range expected {
		if fields[i] != f {
			t.Errorf("field[%d] = %q, want %q", i, fields[i], f)
		}
	}
}

func TestExtractStructValues(t *testing.T) {
	data := testStruct{Name: "Eve", Age: 28, Email: "eve@example.com"}
	values := extractStructValues(reflect.ValueOf(data))
	if len(values) != 3 {
		t.Errorf("expected 3 values, got %d: %v", len(values), values)
	}
	if values[0] != "Eve" {
		t.Errorf("first value = %q, want 'Eve'", values[0])
	}
}

func TestSortedKeys(t *testing.T) {
	keySet := map[string]bool{
		"zebra":  true,
		"apple":  true,
		"mango":  true,
		"banana": true,
	}
	keys := sortedKeys(keySet)
	expected := []string{"apple", "banana", "mango", "zebra"}
	for i, k := range expected {
		if keys[i] != k {
			t.Errorf("keys[%d] = %q, want %q", i, keys[i], k)
		}
	}
}

func TestSortedKeys_Empty(t *testing.T) {
	keys := sortedKeys(map[string]bool{})
	if len(keys) != 0 {
		t.Errorf("expected 0 keys, got %d", len(keys))
	}
}

func TestOutput_Table_JSONTagOverride(t *testing.T) {
	type tagged struct {
		FullName string `json:"full_name"`
		Value    int    `json:"value"`
	}

	var buf bytes.Buffer
	f := NewFormatter("table", &buf)

	data := []tagged{{FullName: "Test", Value: 42}}
	err := f.Output(data)
	if err != nil {
		t.Fatalf("Output failed: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "full_name") {
		t.Errorf("output should use JSON tag 'full_name': %s", output)
	}
}

func TestOutput_Table_MapSliceMissingKeys(t *testing.T) {
	var buf bytes.Buffer
	f := NewFormatter("table", &buf)

	data := []map[string]interface{}{
		{"name": "Alice", "score": 95},
		{"name": "Bob"},
	}
	err := f.Output(data)
	if err != nil {
		t.Fatalf("Output failed: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "Alice") {
		t.Errorf("output missing Alice: %s", output)
	}
	if !strings.Contains(output, "Bob") {
		t.Errorf("output missing Bob: %s", output)
	}
}

func TestDefaultWriter(t *testing.T) {
	w := DefaultWriter()
	if w == nil {
		t.Fatal("DefaultWriter returned nil")
	}
}
