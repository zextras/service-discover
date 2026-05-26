package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestMigrateDeprecatedTLSFields(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "main.json")
	legacy := map[string]any{
		"ports":                  map[string]any{"grpc": 8502, "grpc_tls": 8503},
		"ca_file":                "/x/ca.pem",
		"cert_file":              "/x/c.pem",
		"key_file":               "/x/k.pem",
		"verify_incoming":        true,
		"verify_outgoing":        true,
		"verify_server_hostname": true,
	}
	b, _ := json.MarshalIndent(legacy, "", "  ")
	if err := os.WriteFile(path, b, 0644); err != nil {
		t.Fatal(err)
	}
	if e := migrateDeprecatedTLSFields(path); e != nil {
		t.Fatalf("migration failed: %+v", e)
	}
	out, _ := os.ReadFile(path)
	var got map[string]any
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{"ca_file", "cert_file", "key_file", "verify_incoming", "verify_outgoing", "verify_server_hostname"} {
		if _, ok := got[f]; ok {
			t.Errorf("expected top-level field %q to be removed", f)
		}
	}
	tls, ok := got["tls"].(map[string]any)
	if !ok {
		t.Fatalf("expected tls map, got %T", got["tls"])
	}
	defaults := tls["defaults"].(map[string]any)
	if defaults["ca_file"] != "/x/ca.pem" {
		t.Errorf("ca_file not moved: %v", defaults["ca_file"])
	}
	if defaults["verify_outgoing"] != true {
		t.Errorf("verify_outgoing not moved: %v", defaults["verify_outgoing"])
	}
	internalRPC := tls["internal_rpc"].(map[string]any)
	if internalRPC["verify_server_hostname"] != true {
		t.Errorf("verify_server_hostname not moved: %v", internalRPC["verify_server_hostname"])
	}
}

func TestMigrateDeprecatedTLSFieldsNoop(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "main.json")
	cfg := map[string]any{
		"ports": map[string]any{"grpc": 8502, "grpc_tls": 8503},
		"tls": map[string]any{
			"defaults":     map[string]any{"ca_file": "/x/ca.pem"},
			"internal_rpc": map[string]any{"verify_server_hostname": true},
		},
	}
	b, _ := json.MarshalIndent(cfg, "", "  ")
	os.WriteFile(path, b, 0644)
	if e := migrateDeprecatedTLSFields(path); e != nil {
		t.Fatalf("noop migration failed: %+v", e)
	}
}
