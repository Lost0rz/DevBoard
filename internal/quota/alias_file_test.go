package quota

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

func TestLoadIdentityKeyRequiresAbsoluteRegular0600AndMinimumLength(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "identity.key")
	if err := os.WriteFile(path, []byte("01234567890123456789012345678901"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadIdentityKey(path); err != nil {
		t.Fatalf("valid identity key rejected: %v", err)
	}
	if _, err := LoadIdentityKey("relative/identity.key"); err == nil {
		t.Fatal("relative identity key accepted")
	}
	for _, mode := range []os.FileMode{0o640, 0o644, 0o400} {
		if err := os.Chmod(path, mode); err != nil {
			t.Fatal(err)
		}
		if _, err := LoadIdentityKey(path); err == nil {
			t.Fatalf("identity key mode %o accepted", mode)
		}
	}
	if err := os.Chmod(path, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("too-short"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadIdentityKey(path); err == nil {
		t.Fatal("short identity key accepted")
	}
	if _, err := LoadIdentityKey(filepath.Join(dir, "missing.key")); err == nil {
		t.Fatal("missing identity key accepted")
	}
}

func TestLoadAliasFileCanonicalizesOnlySafeMappings(t *testing.T) {
	dir := t.TempDir()
	key := []byte("shared-test-identity-key-32-bytes-long")
	keyA := AccountKey(key, "codex", "fixture-account-a")
	keyB := AccountKey(key, "codex", "fixture-account-b")
	path := filepath.Join(dir, "aliases")
	contents := keyB + "=Codex B\n" + keyA + "=Codex A\n"
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
	canonical, err := LoadAliasFile(path)
	if err != nil {
		t.Fatalf("valid alias file rejected: %v", err)
	}
	keys := []string{keyA, keyB}
	sort.Strings(keys)
	want := keys[0] + "=" + map[string]string{keyA: "Codex A", keyB: "Codex B"}[keys[0]] + "," + keys[1] + "=" + map[string]string{keyA: "Codex A", keyB: "Codex B"}[keys[1]]
	if canonical != want {
		t.Fatalf("canonical aliases=%q", canonical)
	}
	if strings.Contains(canonical, "fixture-account") || strings.Contains(canonical, "@") {
		t.Fatalf("alias output contains identity material: %q", canonical)
	}

	for name, body := range map[string]string{
		"invalid label":   keyA + "=bad,label",
		"duplicate label": keyA + "=Codex A," + keyB + "=Codex A",
		"duplicate key":   keyA + "=Codex A," + keyA + "=Codex B",
		"malformed key":   "account@example.invalid=Codex A",
		"empty":           "\n,\n",
	} {
		t.Run(name, func(t *testing.T) {
			if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
				t.Fatal(err)
			}
			if _, err := LoadAliasFile(path); err == nil {
				t.Fatalf("unsafe alias file accepted: %q", body)
			}
		})
	}
	if err := os.WriteFile(path, []byte(keyA+"=Codex A"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadAliasFile(path); err == nil {
		t.Fatal("alias file with non-0600 mode accepted")
	}
}
