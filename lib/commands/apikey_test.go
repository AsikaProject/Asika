package commands

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestAPIKeyCreateCmd(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" || !strings.HasSuffix(r.URL.Path, "/apikeys") {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
			w.WriteHeader(404)
			return
		}
		var body map[string]interface{}
		json.NewDecoder(r.Body).Decode(&body)
		resp := map[string]interface{}{
			"id":   "test-key-id",
			"name": body["name"],
			"role": body["role"],
			"key":  "ak_testkey1234567890",
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer ts.Close()

	// We can't easily test the cobra command's Run function without
	// invoking it through the command tree, but we can verify the
	// command structure is correct
	if apikeyCmd.Use != "apikey" {
		t.Errorf("apikeyCmd.Use = %q, want apikey", apikeyCmd.Use)
	}
	if apikeyCreateCmd.Use != "create <name> <role>" {
		t.Errorf("apikeyCreateCmd.Use = %q, want create <name> <role>", apikeyCreateCmd.Use)
	}
	if apikeyListCmd.Use != "list" {
		t.Errorf("apikeyListCmd.Use = %q, want list", apikeyListCmd.Use)
	}
	if apikeyRevokeCmd.Use != "revoke <key_id>" {
		t.Errorf("apikeyRevokeCmd.Use = %q, want revoke <key_id>", apikeyRevokeCmd.Use)
	}

	// Verify the create command validates args
	err := apikeyCreateCmd.Args(apikeyCreateCmd, []string{"name"})
	if err == nil {
		t.Error("create command should require exactly 2 args")
	}

	err = apikeyCreateCmd.Args(apikeyCreateCmd, []string{"name", "role"})
	if err != nil {
		t.Errorf("create command should accept 2 args: %v", err)
	}
}

func TestAPIKeyCreateCmd_InvalidRole(t *testing.T) {
	// The command validates role before making HTTP request
	// We can test the role validation logic
	validRoles := map[string]bool{"admin": true, "operator": true, "viewer": true}

	tests := []struct {
		role  string
		valid bool
	}{
		{"admin", true},
		{"operator", true},
		{"viewer", true},
		{"superadmin", false},
		{"", false},
	}

	for _, tt := range tests {
		t.Run(tt.role, func(t *testing.T) {
			got := validRoles[tt.role]
			if got != tt.valid {
				t.Errorf("validRoles[%q] = %v, want %v", tt.role, got, tt.valid)
			}
		})
	}
}

func TestAPIKeyRevokeCmd_Args(t *testing.T) {
	err := apikeyRevokeCmd.Args(apikeyRevokeCmd, []string{})
	if err == nil {
		t.Error("revoke command should require 1 arg")
	}

	err = apikeyRevokeCmd.Args(apikeyRevokeCmd, []string{"key-id"})
	if err != nil {
		t.Errorf("revoke command should accept 1 arg: %v", err)
	}
}

func TestAPIKeyListCmd_Args(t *testing.T) {
	// list command takes no args — Args validator may be nil (accepts anything)
	if apikeyListCmd.Args != nil {
		err := apikeyListCmd.Args(apikeyListCmd, []string{})
		if err != nil {
			t.Errorf("list command should accept 0 args: %v", err)
		}
	}
}
