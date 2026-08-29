package web

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"unicode/utf8"
)

const (
	// The Hub is intended for a trusted LAN. Keep a non-empty guard so an
	// accidental blank password cannot open the Admin page, but do not impose
	// a long-password policy on this local-only console.
	adminPasswordMinLength  = 1
	adminPasswordMaxLength  = 256
	adminPasswordSaltBytes  = 16
	adminPasswordHashBytes  = 32
	adminPasswordIterations = 120000
)

// loadAdminPassword returns the opaque password record and whether the file
// exists. A missing record is the supported first-run state; malformed or
// weakly protected records fail closed.
func loadAdminPassword(path string) (string, bool, error) {
	if strings.TrimSpace(path) == "" {
		return "", false, fmt.Errorf("admin password file path is empty")
	}
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return "", false, nil
	}
	if err != nil || info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return "", false, fmt.Errorf("admin password file unreadable")
	}
	if info.Mode().Perm()&0o077 != 0 {
		return "", false, fmt.Errorf("admin password file permissions must be 0600 or stricter")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return "", false, fmt.Errorf("admin password file unreadable")
	}
	record := strings.TrimSpace(string(raw))
	if _, _, _, err := parseAdminPasswordRecord(record); err != nil {
		return "", false, fmt.Errorf("admin password file malformed")
	}
	return record, true, nil
}

func hashAdminPassword(password string) (string, error) {
	salt := make([]byte, adminPasswordSaltBytes)
	if _, err := io.ReadFull(rand.Reader, salt); err != nil {
		return "", err
	}
	digest := pbkdf2SHA256([]byte(password), salt, adminPasswordIterations, adminPasswordHashBytes)
	return fmt.Sprintf("v1$pbkdf2-sha256$%d$%s$%s", adminPasswordIterations, hex.EncodeToString(salt), hex.EncodeToString(digest)), nil
}

func verifyAdminPassword(record, password string) bool {
	iterations, salt, expected, err := parseAdminPasswordRecord(record)
	if err != nil {
		return false
	}
	got := pbkdf2SHA256([]byte(password), salt, iterations, len(expected))
	return subtle.ConstantTimeCompare(got, expected) == 1
}

func parseAdminPasswordRecord(record string) (int, []byte, []byte, error) {
	parts := strings.Split(record, "$")
	if len(parts) != 5 || parts[0] != "v1" || parts[1] != "pbkdf2-sha256" {
		return 0, nil, nil, fmt.Errorf("unsupported admin password record")
	}
	iterations, err := strconv.Atoi(parts[2])
	if err != nil || iterations < 100000 || iterations > 1000000 {
		return 0, nil, nil, fmt.Errorf("invalid password work factor")
	}
	salt, err := hex.DecodeString(parts[3])
	if err != nil || len(salt) != adminPasswordSaltBytes {
		return 0, nil, nil, fmt.Errorf("invalid password salt")
	}
	expected, err := hex.DecodeString(parts[4])
	if err != nil || len(expected) != adminPasswordHashBytes {
		return 0, nil, nil, fmt.Errorf("invalid password digest")
	}
	return iterations, salt, expected, nil
}

// saveAdminPassword writes the opaque record with the same private-file
// boundary as the existing token/config files. The rename keeps a complete
// old record available if the process is interrupted during a change.
func saveAdminPassword(path, record string) error {
	if strings.TrimSpace(path) == "" {
		return fmt.Errorf("admin password file path is empty")
	}
	if _, _, _, err := parseAdminPasswordRecord(record); err != nil {
		return fmt.Errorf("invalid admin password record")
	}
	if info, err := os.Lstat(path); err == nil {
		if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() || info.Mode().Perm()&0o077 != 0 {
			return fmt.Errorf("admin password file is not private")
		}
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("admin password file unavailable")
	}
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".admin-password-*")
	if err != nil {
		return fmt.Errorf("admin password file unavailable")
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("admin password file unavailable")
	}
	if _, err := io.WriteString(tmp, record+"\n"); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("admin password file unavailable")
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("admin password file unavailable")
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("admin password file unavailable")
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return fmt.Errorf("admin password file unavailable")
	}
	return nil
}

func pbkdf2SHA256(password, salt []byte, iterations, keyLen int) []byte {
	if keyLen <= 0 {
		return nil
	}
	hashSize := sha256.Size
	blocks := (keyLen + hashSize - 1) / hashSize
	derived := make([]byte, 0, blocks*hashSize)
	for block := 1; block <= blocks; block++ {
		mac := hmac.New(sha256.New, password)
		_, _ = mac.Write(salt)
		var counter [4]byte
		binary.BigEndian.PutUint32(counter[:], uint32(block))
		_, _ = mac.Write(counter[:])
		u := mac.Sum(nil)
		t := append([]byte(nil), u...)
		for i := 1; i < iterations; i++ {
			mac = hmac.New(sha256.New, password)
			_, _ = mac.Write(u)
			u = mac.Sum(nil)
			for j := range t {
				t[j] ^= u[j]
			}
		}
		derived = append(derived, t...)
	}
	return derived[:keyLen]
}

func validAdminPassword(password string) bool {
	length := utf8.RuneCountInString(password)
	return utf8.ValidString(password) && length >= adminPasswordMinLength && length <= adminPasswordMaxLength
}
