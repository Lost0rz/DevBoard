package quota

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEnsureIdentityKeyGeneratesPrivateIdempotentKey(t *testing.T) {
	path := filepath.Join(t.TempDir(), "private", "identity.key")
	first, created, err := EnsureIdentityKey(path)
	if err != nil || !created || len(first) < IdentityKeyBytes {
		t.Fatalf("first key created=%v len=%d err=%v", created, len(first), err)
	}
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0o600 {
		t.Fatalf("key info=%v err=%v", info, err)
	}
	parent, err := os.Stat(filepath.Dir(path))
	if err != nil || !parent.IsDir() || parent.Mode().Perm() != 0o700 {
		t.Fatalf("parent info=%v err=%v", parent, err)
	}
	second, created, err := EnsureIdentityKey(path)
	if err != nil || created || string(first) != string(second) {
		t.Fatalf("second key created=%v equal=%v err=%v", created, string(first) == string(second), err)
	}
}

func TestEnsureIdentityKeyRejectsSymlinkAndWidePermissions(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "target.key")
	if err := os.WriteFile(target, []byte(strings.Repeat("k", IdentityKeyBytes)), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "identity.key")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	if _, _, err := EnsureIdentityKey(link); err == nil {
		t.Fatal("symlink identity key accepted")
	}
	wide := filepath.Join(dir, "wide.key")
	if err := os.WriteFile(wide, []byte(strings.Repeat("k", IdentityKeyBytes)), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, _, err := EnsureIdentityKey(wide); err == nil {
		t.Fatal("wide-permission identity key accepted")
	}
}

func TestDetectedAccountJSONContainsOnlySanitizedFields(t *testing.T) {
	key := []byte(strings.Repeat("k", IdentityKeyBytes))
	runner := productQuotaRunner{responses: map[string][]byte{
		"codex": []byte(`[{"provider":"codex","account":"Codex A","usage":{"identity":{"accountID":"private-account","accountEmail":"person@example.invalid","loginMethod":"oauth"},"primary":{"usedPercent":10}}}]`),
		"zai":   []byte(`[{"provider":"zai","account":"GLM","usage":{"identity":{"accountID":"glm-private","accountEmail":"glm@example.invalid","loginMethod":"api"},"primary":{"usedPercent":20}}}]`),
	}}
	detection, err := DetectAccounts(context.Background(), runner, key, map[string]string{})
	if err != nil {
		t.Fatal(err)
	}
	body, err := json.Marshal(detection)
	if err != nil {
		t.Fatal(err)
	}
	encoded := string(body)
	for _, forbidden := range []string{"private-account", "person@example.invalid", "oauth", "glm-private", "glm@example.invalid", "api"} {
		if strings.Contains(encoded, forbidden) {
			t.Fatalf("detection leaked %q: %s", forbidden, encoded)
		}
	}
	if !strings.Contains(encoded, "acct_") || !strings.Contains(encoded, "GLM") {
		t.Fatalf("sanitized detection missing expected fields: %s", encoded)
	}
}

type productQuotaRunner struct {
	responses map[string][]byte
	err       error
}

func (r productQuotaRunner) Run(_ context.Context, _ string, args ...string) ([]byte, error) {
	if r.err != nil {
		return nil, r.err
	}
	return r.responses[args[2]], nil
}
