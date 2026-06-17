package security

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"

	"fmt"
	"io"
	"os"
	"strings"
)

// Algorithm used for encryption/decryption
const algorithm = "aes-256-cbc"

func getKey() ([]byte, error) {
	emailEncryption := os.Getenv("EMAIL_ENCRYPTION")
	if emailEncryption == "" {
		return nil, errors.New("EMAIL_ENCRYPTION environment variable is not set")
	}
	hash := sha256.Sum256([]byte(emailEncryption))
	return hash[:], nil
}

func Encrypt(text string) (string, error) {
	key, err := getKey()
	if err != nil {
		return "", err
	}

	iv := make([]byte, aes.BlockSize)
	if _, err := io.ReadFull(rand.Reader, iv); err != nil {
		return "", err
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}

	plaintext := PKCS5Padding([]byte(text), aes.BlockSize, len(text))
	ciphertext := make([]byte, len(plaintext))
	mode := cipher.NewCBCEncrypter(block, iv)
	mode.CryptBlocks(ciphertext, plaintext)

	return fmt.Sprintf("%x:%x", iv, ciphertext), nil
}

func Decrypt(encryptedText string) (string, error) {
	key, err := getKey()
	if err != nil {
		return "", err
	}

	parts := splitString(encryptedText, ":")
	if len(parts) != 2 {
		return "", errors.New("invalid encrypted text format")
	}

	iv, err := hex.DecodeString(parts[0])
	if err != nil {
		return "", err
	}

	ciphertext, err := hex.DecodeString(parts[1])
	if err != nil {
		return "", err
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}

	if len(ciphertext)%aes.BlockSize != 0 {
		return "", errors.New("ciphertext is not a multiple of the block size")
	}

	mode := cipher.NewCBCDecrypter(block, iv)
	mode.CryptBlocks(ciphertext, ciphertext)

	return string(ciphertext), nil
}

func splitString(s, sep string) []string {
	return strings.SplitN(s, sep, 2)
}

func PKCS5Padding(ciphertext []byte, blockSize int, after int) []byte {
	padding := (blockSize - len(ciphertext)%blockSize)
	padtext := bytes.Repeat([]byte{byte(padding)}, padding)
	return append(ciphertext, padtext...)
}

/*
Security Information for Clients
Our commitment to your data security is paramount. We utilize advanced AES-256-CBC encryption,
a robust standard trusted by cybersecurity experts worldwide. This ensures that your sensitive
information is protected with military-grade encryption, making it virtually impossible for
unauthorized parties to access. Your data's confidentiality is our top priority, and we
continuously update our security measures to stay ahead of potential threats.
*/
