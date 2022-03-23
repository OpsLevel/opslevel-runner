package pkg

import (
	"crypto/aes"
	"crypto/cipher"
	"encoding/hex"
	"fmt"
)

type Cipher struct {
	key   []byte
	nonce []byte
}

func NewCipher(key, nonce string) Cipher {
	// Never use more than 2^32 random nonces with a given key because of the risk of a repeat.
	// TODO: check for this?
	return Cipher{
		key:   []byte(fmt.Sprintf("%032s", key))[:32],
		nonce: []byte(fmt.Sprintf("%012s", nonce))[:12],
	}
}

func (s *Cipher) Encrypt(data string) (string, error) {

	block, err := aes.NewCipher(s.key)
	if err != nil {
		return "", err
	}

	aesgcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}

	return hex.EncodeToString(aesgcm.Seal(nil, s.nonce, []byte(data), nil)), nil
}

func (s Cipher) Decrypt(data string) ([]byte, error) {
	ciphertext, _ := hex.DecodeString(data)

	block, err := aes.NewCipher(s.key)
	if err != nil {
		return nil, err
	}

	aesgcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}

	return aesgcm.Open(nil, s.nonce, ciphertext, nil)
}
