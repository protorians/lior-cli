package pkg

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
)

// Password-based key derivation parameters (OWASP-recommended for PBKDF2-HMAC-SHA256).
const (
	KDFIterations = 210_000
	kdfSaltSize   = 16
)

// DeriveKey derives a 32-byte AES key from a passphrase and a salt using
// PBKDF2-HMAC-SHA256 (XKDF). The salt should be random and persisted alongside
// the ciphertext (see EncryptVault).
func DeriveKey(passphrase string, salt []byte) ([]byte, error) {
	if len(salt) == 0 {
		return nil, errors.New("a salt is required for key derivation")
	}
	key := pbkdf2SHA256([]byte(passphrase), salt, KDFIterations, 32)
	return key, nil
}

// NewRandomKey returns 32 cryptographically-random bytes (an AES-256 key).
func NewRandomKey() ([]byte, error) {
	key := make([]byte, 32)
	if _, err := io.ReadFull(rand.Reader, key); err != nil {
		return nil, fmt.Errorf("failed to generate the random key: %w", err)
	}
	return key, nil
}

// EncryptVault encrypts plaintext with AES-256-GCM using a key derived from
// secret via PBKDF2. The output layout is `salt || nonce || ciphertext`.
// Passwords are never hard-coded: `secret` must come from a per-user source
// (e.g. the machine secret file, see MachineSecret).
func EncryptVault(secret []byte, plaintext []byte) ([]byte, error) {
	salt := make([]byte, kdfSaltSize)
	if _, err := io.ReadFull(rand.Reader, salt); err != nil {
		return nil, fmt.Errorf("failed to generate the salt: %w", err)
	}
	key, err := DeriveKey(string(secret), salt)
	if err != nil {
		return nil, err
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize the cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize GCM: %w", err)
	}

	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, fmt.Errorf("failed to generate the nonce: %w", err)
	}

	sealed := gcm.Seal(nonce, nonce, plaintext, nil)
	out := make([]byte, 0, len(salt)+len(sealed))
	out = append(out, salt...)
	out = append(out, sealed...)
	return out, nil
}

// DecryptVault decrypts ciphertext produced by EncryptVault.
func DecryptVault(secret []byte, data []byte) ([]byte, error) {
	if len(data) < kdfSaltSize {
		return nil, errors.New("invalid encrypted data")
	}
	salt, sealed := data[:kdfSaltSize], data[kdfSaltSize:]
	key, err := DeriveKey(string(secret), salt)
	if err != nil {
		return nil, err
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize the cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize GCM: %w", err)
	}

	nonceSize := gcm.NonceSize()
	if len(sealed) < nonceSize {
		return nil, errors.New("invalid encrypted data")
	}
	nonce, payload := sealed[:nonceSize], sealed[nonceSize:]

	plaintext, err := gcm.Open(nil, nonce, payload, nil)
	if err != nil {
		return nil, errors.New("decryption denied (access refused)")
	}
	return plaintext, nil
}

// pbkdf2SHA256 implements PBKDF2-HMAC-SHA256 (RFC 2898 / NIST SP 800-132) on
// the standard library only, keeping the binary dependency-free (NFR-001).
func pbkdf2SHA256(password, salt []byte, iter, keyLen int) []byte {
	prf := hmac.New(sha256.New, password)
	hashLen := prf.Size()
	numBlocks := (keyLen + hashLen - 1) / hashLen

	var buf [4]byte
	dk := make([]byte, 0, numBlocks*hashLen)
	u := make([]byte, hashLen)
	for block := 1; block <= numBlocks; block++ {
		prf.Reset()
		prf.Write(salt)
		buf[0] = byte(block >> 24)
		buf[1] = byte(block >> 16)
		buf[2] = byte(block >> 8)
		buf[3] = byte(block)
		prf.Write(buf[:4])
		dk = prf.Sum(dk)
		t := dk[len(dk)-hashLen:]
		copy(u, t)

		for n := 2; n <= iter; n++ {
			prf.Reset()
			prf.Write(u)
			u = u[:0]
			u = prf.Sum(u)
			for x := range t {
				t[x] ^= u[x]
			}
		}
	}
	return dk[:keyLen]
}

// EncryptAESGCM encrypts plaintext with AES-256-GCM using a key derived from
// the given passphrase (SHA-256). Returns a random nonce prepended to the
// ciphertext.
//
// Deprecated: prefer EncryptVault, which derives the key with PBKDF2 and a
// per-message salt. The stores no longer rely on this helper, so no
// hard-coded secret feeds it.
func EncryptAESGCM(passphrase string, plaintext []byte) ([]byte, error) {
	key := sha256.Sum256([]byte(passphrase))
	block, err := aes.NewCipher(key[:])
	if err != nil {
		return nil, fmt.Errorf("failed to initialize the cipher: %w", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize GCM: %w", err)
	}

	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, fmt.Errorf("failed to generate the nonce: %w", err)
	}

	return gcm.Seal(nonce, nonce, plaintext, nil), nil
}

// DecryptAESGCM decrypts ciphertext produced by EncryptAESGCM.
// Deprecated: use DecryptVault.
func DecryptAESGCM(passphrase string, ciphertext []byte) ([]byte, error) {
	key := sha256.Sum256([]byte(passphrase))
	block, err := aes.NewCipher(key[:])
	if err != nil {
		return nil, fmt.Errorf("failed to initialize the cipher: %w", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize GCM: %w", err)
	}

	nonceSize := gcm.NonceSize()
	if len(ciphertext) < nonceSize {
		return nil, errors.New("invalid encrypted data")
	}
	nonce, payload := ciphertext[:nonceSize], ciphertext[nonceSize:]

	plaintext, err := gcm.Open(nil, nonce, payload, nil)
	if err != nil {
		return nil, errors.New("decryption denied (access refused)")
	}
	return plaintext, nil
}
