package formatter

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"reflect"
	"strings"
	"text/tabwriter"

	"gopkg.in/yaml.v3"
)

// OutputFormat represents the output format
type OutputFormat string

const (
	FormatTable OutputFormat = "table"
	FormatJSON  OutputFormat = "json"
	FormatYAML  OutputFormat = "yaml"
)

// Formatter formats output
type Formatter struct {
	format OutputFormat
	writer io.Writer
}

// NewFormatter creates a new formatter
func NewFormatter(format string, writer io.Writer) *Formatter {
	f := ParseFormat(format)
	return &Formatter{
		format: f,
		writer: writer,
	}
}

// Output outputs data in the specified format
func (f *Formatter) Output(data interface{}) error {
	switch f.format {
	case FormatJSON:
		return f.outputJSON(data)
	case FormatYAML:
		return f.outputYAML(data)
	default:
		return f.outputTable(data)
	}
}

// outputJSON outputs as JSON
func (f *Formatter) outputJSON(data interface{}) error {
	encoder := json.NewEncoder(f.writer)
	encoder.SetIndent("", "  ")
	return encoder.Encode(data)
}

// outputYAML outputs as YAML
func (f *Formatter) outputYAML(data interface{}) error {
	encoder := yaml.NewEncoder(f.writer)
	return encoder.Encode(data)
}

// outputTable outputs as a formatted table
func (f *Formatter) outputTable(data interface{}) error {
	w := tabwriter.NewWriter(f.writer, 0, 0, 2, ' ', 0)

	val := reflect.ValueOf(data)

	// Handle nil
	if !val.IsValid() {
		fmt.Fprintln(w, "No data")
		w.Flush()
		return nil
	}

	// Handle slices/arrays
	if val.Kind() == reflect.Slice || val.Kind() == reflect.Array {
		if val.Len() == 0 {
			fmt.Fprintln(w, "No data")
			w.Flush()
			return nil
		}

		// Get the first element to determine columns
		first := val.Index(0)
		if first.Kind() == reflect.Map {
			// Map type — extract keys as headers
			return f.outputMapSliceTable(w, val)
		}

		if first.Kind() == reflect.Struct {
			// Struct type — use field names as headers
			return f.outputStructSliceTable(w, val)
		}

		// Simple values
		for i := 0; i < val.Len(); i++ {
			fmt.Fprintf(w, "%v\n", val.Index(i).Interface())
		}
		w.Flush()
		return nil
	}

	// Handle single map
	if val.Kind() == reflect.Map {
		return f.outputMapTable(w, val)
	}

	// Handle single struct
	if val.Kind() == reflect.Struct {
		return f.outputStructTable(w, val)
	}

	// Default: just print the value
	fmt.Fprintf(w, "%v\n", data)
	w.Flush()
	return nil
}

func (f *Formatter) outputStructSliceTable(w *tabwriter.Writer, val reflect.Value) error {
	first := val.Index(0)
	typ := first.Type()

	// Print headers
	headers := extractStructFields(typ)
	fmt.Fprintln(w, strings.Join(headers, "\t"))

	// Print rows
	for i := 0; i < val.Len(); i++ {
		row := extractStructValues(val.Index(i))
		fmt.Fprintln(w, strings.Join(row, "\t"))
	}

	w.Flush()
	return nil
}

func (f *Formatter) outputStructTable(w *tabwriter.Writer, val reflect.Value) error {
	typ := val.Type()

	for i := 0; i < typ.NumField(); i++ {
		field := typ.Field(i)
		value := val.Field(i).Interface()

		// Skip complex types (slices, maps, structs)
		if val.Field(i).Kind() == reflect.Slice || val.Field(i).Kind() == reflect.Map || val.Field(i).Kind() == reflect.Struct {
			continue
		}

		fmt.Fprintf(w, "%s:\t%v\n", field.Name, value)
	}

	w.Flush()
	return nil
}

func (f *Formatter) outputMapSliceTable(w *tabwriter.Writer, val reflect.Value) error {
	if val.Len() == 0 {
		fmt.Fprintln(w, "No data")
		w.Flush()
		return nil
	}

	// Collect all keys from all maps
	keySet := make(map[string]bool)
	for i := 0; i < val.Len(); i++ {
		iter := val.Index(i).MapRange()
		for iter.Next() {
			keySet[fmt.Sprintf("%v", iter.Key())] = true
		}
	}

	keys := sortedKeys(keySet)

	// Print headers
	fmt.Fprintln(w, strings.Join(keys, "\t"))

	// Print rows
	for i := 0; i < val.Len(); i++ {
		row := make([]string, len(keys))
		for j, key := range keys {
			mapVal := val.Index(i).MapIndex(reflect.ValueOf(key))
			if mapVal.IsValid() {
				row[j] = fmt.Sprintf("%v", mapVal.Interface())
			} else {
				row[j] = ""
			}
		}
		fmt.Fprintln(w, strings.Join(row, "\t"))
	}

	w.Flush()
	return nil
}

func (f *Formatter) outputMapTable(w *tabwriter.Writer, val reflect.Value) error {
	iter := val.MapRange()
	for iter.Next() {
		fmt.Fprintf(w, "%v:\t%v\n", iter.Key(), iter.Value())
	}
	w.Flush()
	return nil
}

func extractStructFields(typ reflect.Type) []string {
	fields := make([]string, 0, typ.NumField())
	for i := 0; i < typ.NumField(); i++ {
		field := typ.Field(i)
		// Skip unexported and complex fields
		if !field.IsExported() {
			continue
		}
		if field.Type.Kind() == reflect.Slice || field.Type.Kind() == reflect.Map || field.Type.Kind() == reflect.Struct {
			continue
		}
		name := field.Name
		// Use JSON tag name if available
		if tag := field.Tag.Get("json"); tag != "" {
			parts := strings.Split(tag, ",")
			if parts[0] != "" && parts[0] != "-" {
				name = parts[0]
			}
		}
		fields = append(fields, name)
	}
	return fields
}

func extractStructValues(val reflect.Value) []string {
	typ := val.Type()
	values := make([]string, 0, typ.NumField())
	for i := 0; i < typ.NumField(); i++ {
		field := typ.Field(i)
		if !field.IsExported() {
			continue
		}
		if val.Field(i).Kind() == reflect.Slice || val.Field(i).Kind() == reflect.Map || val.Field(i).Kind() == reflect.Struct {
			continue
		}
		values = append(values, fmt.Sprintf("%v", val.Field(i).Interface()))
	}
	return values
}

func sortedKeys(keySet map[string]bool) []string {
	keys := make([]string, 0, len(keySet))
	for k := range keySet {
		keys = append(keys, k)
	}
	// Simple sort
	for i := 0; i < len(keys); i++ {
		for j := i + 1; j < len(keys); j++ {
			if keys[i] > keys[j] {
				keys[i], keys[j] = keys[j], keys[i]
			}
		}
	}
	return keys
}

// ParseFormat parses output format from string
func ParseFormat(s string) OutputFormat {
	switch strings.ToLower(s) {
	case "json":
		return FormatJSON
	case "yaml":
		return FormatYAML
	default:
		return FormatTable
	}
}

// DefaultWriter returns os.Stdout as default writer
func DefaultWriter() io.Writer {
	return os.Stdout
}
