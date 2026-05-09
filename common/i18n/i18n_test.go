package i18n

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSetLocale_Valid(t *testing.T) {
	Register("fr", map[string]string{"hello": "bonjour"})
	SetLocale("fr")
	if Locale() != "fr" {
		t.Errorf("Locale() = %q, want 'fr'", Locale())
	}
	SetLocale("en")
}

func TestSetLocale_Invalid(t *testing.T) {
	SetLocale("nonexistent")
	if Locale() != "en" {
		t.Errorf("expected fallback to 'en', got %q", Locale())
	}
}

func TestLocale_Default(t *testing.T) {
	SetLocale("en")
	if Locale() != "en" {
		t.Errorf("default locale = %q, want 'en'", Locale())
	}
}

func TestT_SimpleKey(t *testing.T) {
	Register("en", map[string]string{"greeting": "hello"})
	SetLocale("en")
	result := T("greeting")
	if result != "hello" {
		t.Errorf("T('greeting') = %q, want 'hello'", result)
	}
}

func TestT_WithArgs(t *testing.T) {
	Register("en", map[string]string{"welcome": "hello %s"})
	SetLocale("en")
	result := T("welcome", "Alice")
	if result != "hello Alice" {
		t.Errorf("T('welcome', 'Alice') = %q, want 'hello Alice'", result)
	}
}

func TestT_FallbackToEn(t *testing.T) {
	Register("en", map[string]string{"fallback_key": "english value"})
	Register("de", map[string]string{})
	SetLocale("de")
	result := T("fallback_key")
	if result != "english value" {
		t.Errorf("fallback = %q, want 'english value'", result)
	}
}

func TestT_KeyNotFound(t *testing.T) {
	SetLocale("en")
	result := T("nonexistent.key")
	if result != "nonexistent.key" {
		t.Errorf("missing key should return key itself, got %q", result)
	}
}

func TestT_FallbackEnMissing(t *testing.T) {
	Register("en", map[string]string{})
	Register("de", map[string]string{})
	SetLocale("de")
	result := T("totally.missing")
	if result != "totally.missing" {
		t.Errorf("missing key in both locales should return key, got %q", result)
	}
}

func TestLoadLocale(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.json")
	content := `{"hello": "hola", "bye": "adios"}`
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("failed to write test locale: %v", err)
	}

	err := LoadLocale("es", path)
	if err != nil {
		t.Fatalf("LoadLocale failed: %v", err)
	}

	SetLocale("es")
	result := T("hello")
	if result != "hola" {
		t.Errorf("T('hello') = %q, want 'hola'", result)
	}
	SetLocale("en")
}

func TestLoadLocale_FileNotFound(t *testing.T) {
	err := LoadLocale("xx", "/nonexistent/path.json")
	if err == nil {
		t.Fatal("expected error for missing file")
	}
}

func TestLoadLocale_InvalidJSON(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.json")
	if err := os.WriteFile(path, []byte("{invalid"), 0644); err != nil {
		t.Fatalf("failed to write: %v", err)
	}

	err := LoadLocale("bad", path)
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

func TestRegister(t *testing.T) {
	Register("test_lang", map[string]string{"key1": "val1"})
	SetLocale("test_lang")
	result := T("key1")
	if result != "val1" {
		t.Errorf("T('key1') = %q, want 'val1'", result)
	}
	SetLocale("en")
}

func TestRegister_Merge(t *testing.T) {
	Register("merge_test", map[string]string{"a": "1"})
	Register("merge_test", map[string]string{"b": "2"})
	SetLocale("merge_test")
	if T("a") != "1" {
		t.Errorf("T('a') = %q, want '1'", T("a"))
	}
	if T("b") != "2" {
		t.Errorf("T('b') = %q, want '2'", T("b"))
	}
	SetLocale("en")
}

func TestParseAcceptLanguage_Empty(t *testing.T) {
	result := ParseAcceptLanguage("")
	if result != "en" {
		t.Errorf("empty header should return 'en', got %q", result)
	}
}

func TestParseAcceptLanguage_Zh(t *testing.T) {
	tests := []string{
		"zh",
		"zh-CN",
		"zh-TW",
		"zh-Hans",
		"zh;q=0.9",
	}
	for _, header := range tests {
		t.Run(header, func(t *testing.T) {
			result := ParseAcceptLanguage(header)
			if result != "zh" {
				t.Errorf("ParseAcceptLanguage(%q) = %q, want 'zh'", header, result)
			}
		})
	}
}

func TestParseAcceptLanguage_En(t *testing.T) {
	tests := []string{
		"en",
		"en-US",
		"en-GB",
		"en;q=0.8",
	}
	for _, header := range tests {
		t.Run(header, func(t *testing.T) {
			result := ParseAcceptLanguage(header)
			if result != "en" {
				t.Errorf("ParseAcceptLanguage(%q) = %q, want 'en'", header, result)
			}
		})
	}
}

func TestParseAcceptLanguage_Multiple(t *testing.T) {
	result := ParseAcceptLanguage("fr, en;q=0.8, zh;q=0.6")
	if result != "en" {
		t.Errorf("first non-zh/non-en should fallback to 'en', got %q", result)
	}
}

func TestParseAcceptLanguage_ZhFirst(t *testing.T) {
	result := ParseAcceptLanguage("zh-CN, en;q=0.8")
	if result != "zh" {
		t.Errorf("zh-CN first should return 'zh', got %q", result)
	}
}

func TestT_ArgsWithFallback(t *testing.T) {
	Register("en", map[string]string{"welcome_user": "welcome %s"})
	Register("jp", map[string]string{})
	SetLocale("jp")
	result := T("welcome_user", "Taro")
	if result != "welcome Taro" {
		t.Errorf("args with fallback = %q, want 'welcome Taro'", result)
	}
}
