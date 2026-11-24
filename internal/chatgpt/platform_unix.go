//go:build !windows

package chatgpt

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"golang.org/x/sys/unix"
)

func tryLock(f *os.File) (bool, error) {
	err := unix.Flock(int(f.Fd()), unix.LOCK_EX|unix.LOCK_NB)
	if err == unix.EWOULDBLOCK {
		return false, nil
	}
	return err == nil, err
}
func unlock(f *os.File) { _ = unix.Flock(int(f.Fd()), unix.LOCK_UN) }

// Only a random encryption key enters the OS secret service. The encrypted
// profile stays in FreeFolio's directory. Missing/locked services fail explicitly.
func protect(dir string, data []byte, seal bool) ([]byte, error) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return nil, err
	}
	name := fmt.Sprintf("%x", sha256.Sum256([]byte(abs)))
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	lookup := func() ([]byte, error) {
		if runtime.GOOS == "darwin" {
			return exec.CommandContext(ctx, "/usr/bin/security", "find-generic-password", "-s", "FreeFolio.ChatGPT", "-a", name, "-w").Output()
		}
		return exec.CommandContext(ctx, "secret-tool", "lookup", "application", "FreeFolio.ChatGPT", "store", name).Output()
	}
	encoded, lookupErr := lookup()
	if lookupErr != nil {
		code := -1
		if exit, ok := lookupErr.(*exec.ExitError); ok {
			code = exit.ExitCode()
		}
		missing := runtime.GOOS == "darwin" && code == 44 || runtime.GOOS == "linux" && code == 1 && len(bytes.TrimSpace(encoded)) == 0
		if !seal || !missing {
			return nil, failure("encryption_unavailable")
		}
		key := make([]byte, 32)
		if _, err = rand.Read(key); err != nil {
			return nil, err
		}
		encoded = []byte(base64.StdEncoding.EncodeToString(key))
		var cmd *exec.Cmd
		if runtime.GOOS == "darwin" {
			// Interactive stdin avoids putting the secret in process arguments.
			cmd = exec.CommandContext(ctx, "/usr/bin/security", "-i")
			cmd.Stdin = strings.NewReader("add-generic-password -s FreeFolio.ChatGPT -a " + name + " -w " + string(encoded) + "\n")
		} else {
			cmd = exec.CommandContext(ctx, "secret-tool", "store", "--label=FreeFolio ChatGPT", "application", "FreeFolio.ChatGPT", "store", name)
			cmd.Stdin = bytes.NewReader(encoded)
		}
		if err = cmd.Run(); err != nil {
			return nil, failure("encryption_unavailable")
		}
		// Confirm persistence before publishing ciphertext; some interactive
		// keychain commands can exit cleanly after reporting an operation error.
		confirmed, err := lookup()
		if err != nil || strings.TrimSpace(string(confirmed)) != string(encoded) {
			return nil, failure("encryption_unavailable")
		}
	}
	key, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(encoded)))
	if err != nil || len(key) != 32 {
		return nil, failure("encryption_unavailable")
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	if seal {
		nonce := make([]byte, aead.NonceSize())
		if _, err = rand.Read(nonce); err != nil {
			return nil, err
		}
		return aead.Seal(nonce, nonce, data, nil), nil
	}
	if len(data) < aead.NonceSize() {
		return nil, failure("storage_invalid")
	}
	return aead.Open(nil, data[:aead.NonceSize()], data[aead.NonceSize():], nil)
}
