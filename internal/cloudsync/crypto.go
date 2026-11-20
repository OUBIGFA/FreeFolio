package cloudsync

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/pbkdf2"
	"crypto/rand"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"io"
)

// Password-derived AES-GCM authenticates both content and format parameters.
const (
	sealedFormat        = "freemd-encrypted-file"
	sealedKDF           = "pbkdf2-hmac-sha256"
	sealedCipher        = "aes-256-gcm"
	sealedIterations    = 600_000
	sealedMinIterations = 100_000
	sealedMaxIterations = 10_000_000
)

var (
	errBackupPassword = errors.New("备份密码不正确，或者云端备份已损坏")
	errBackupLocked   = errors.New("云端备份已加密；请在云端同步中填写该备份的密码")
	errNoPassword     = errors.New("已开启备份加密，请先填写并保存备份密码")
)

type sealedBackup struct {
	Format     string `json:"format"`
	Version    int    `json:"version"`
	KDF        string `json:"kdf"`
	Iterations int    `json:"iterations"`
	Salt       []byte `json:"salt"`
	Cipher     string `json:"cipher"`
	Nonce      []byte `json:"nonce"`
	Data       []byte `json:"data"`
}

func (b sealedBackup) associated() []byte {
	b.Salt, b.Nonce, b.Data = nil, nil, nil
	data, _ := json.Marshal(b)
	return data
}

func sealedKey(password string, salt []byte, iterations int) (cipher.AEAD, error) {
	key, err := pbkdf2.Key(sha256.New, password, salt, iterations, 32)
	if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}

func sealBackup(plain []byte, password string) ([]byte, error) {
	if password == "" {
		return nil, errNoPassword
	}
	sealed := sealedBackup{Format: sealedFormat, Version: 1, KDF: sealedKDF, Iterations: sealedIterations,
		Salt: make([]byte, 16), Cipher: sealedCipher, Nonce: make([]byte, 12)}
	if _, err := io.ReadFull(rand.Reader, sealed.Salt); err != nil {
		return nil, err
	}
	if _, err := io.ReadFull(rand.Reader, sealed.Nonce); err != nil {
		return nil, err
	}
	aead, err := sealedKey(password, sealed.Salt, sealed.Iterations)
	if err != nil {
		return nil, err
	}
	sealed.Data = aead.Seal(nil, sealed.Nonce, plain, sealed.associated())
	return json.Marshal(sealed)
}

func openBackup(data []byte, password string) ([]byte, error) {
	var sealed sealedBackup
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 || trimmed[0] != '{' || json.Unmarshal(trimmed, &sealed) != nil || sealed.Format != sealedFormat {
		return data, nil
	}
	if sealed.Version != 1 || sealed.KDF != sealedKDF || sealed.Cipher != sealedCipher {
		return nil, errors.New("这份云端备份由更新的 FreeMD 加密，请先更新这台电脑上的 FreeMD")
	}
	if sealed.Iterations < sealedMinIterations || sealed.Iterations > sealedMaxIterations || len(sealed.Salt) < 8 || len(sealed.Nonce) != 12 {
		return nil, errors.New("云端备份的加密参数无效，文件可能已损坏")
	}
	if password == "" {
		return nil, errBackupLocked
	}
	aead, err := sealedKey(password, sealed.Salt, sealed.Iterations)
	if err != nil {
		return nil, err
	}
	plain, err := aead.Open(nil, sealed.Nonce, sealed.Data, sealed.associated())
	if err != nil {
		return nil, errBackupPassword
	}
	return plain, nil
}
