package stale

import (
	"bytes"
	"fmt"
	"text/template"
)

type commentData struct {
	Days        int
	CloseIn     string
	ExemptLabel string
}

func renderComment(tmpl string, data commentData) (string, error) {
	t, err := template.New("comment").Parse(tmpl)
	if err != nil {
		return "", fmt.Errorf("failed to parse template: %w", err)
	}
	var buf bytes.Buffer
	if err := t.Execute(&buf, data); err != nil {
		return "", fmt.Errorf("failed to render template: %w", err)
	}
	return buf.String(), nil
}
