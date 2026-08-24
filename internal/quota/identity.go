package quota

import (
	"crypto/rand"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// IdentityKeyBytes is the minimum product-generated HMAC key size. The key is
// written as raw bytes and is never returned by a product command.
const IdentityKeyBytes = 32

// EnsureIdentityKey returns an existing valid key or atomically creates a new
// product key. Existing files are never replaced. The parent directory is
// private to the product and the key is mode 0600.
func EnsureIdentityKey(path string) ([]byte, bool, error) {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return nil, false, fmt.Errorf("quota identity key path must be absolute and clean")
	}
	parent := filepath.Dir(path)
	if err := ensurePrivateKeyDir(parent); err != nil {
		return nil, false, err
	}

	if _, err := os.Lstat(path); err == nil {
		key, loadErr := LoadIdentityKey(path)
		return key, false, loadErr
	} else if !os.IsNotExist(err) {
		return nil, false, fmt.Errorf("quota identity key unavailable")
	}

	key := make([]byte, IdentityKeyBytes)
	if _, err := io.ReadFull(rand.Reader, key); err != nil {
		return nil, false, fmt.Errorf("generate quota identity key")
	}
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		if os.IsExist(err) {
			loaded, loadErr := LoadIdentityKey(path)
			return loaded, false, loadErr
		}
		return nil, false, fmt.Errorf("create quota identity key")
	}
	created := true
	defer func() {
		if created {
			_ = os.Remove(path)
		}
	}()
	if err := file.Chmod(0o600); err != nil {
		_ = file.Close()
		return nil, false, fmt.Errorf("protect quota identity key")
	}
	if _, err := file.Write(key); err != nil {
		_ = file.Close()
		return nil, false, fmt.Errorf("write quota identity key")
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		return nil, false, fmt.Errorf("sync quota identity key")
	}
	if err := file.Close(); err != nil {
		return nil, false, fmt.Errorf("close quota identity key")
	}
	created = false
	return key, true, nil
}

func ensurePrivateKeyDir(path string) error {
	if err := os.MkdirAll(path, 0o700); err != nil {
		return fmt.Errorf("prepare quota identity directory")
	}
	info, err := os.Lstat(path)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("quota identity directory is not private")
	}
	if err := os.Chmod(path, 0o700); err != nil {
		return fmt.Errorf("protect quota identity directory")
	}
	return nil
}
