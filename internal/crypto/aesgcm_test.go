package crypto

import (
	"bytes"
	"testing"
)

func TestCipher_EncryptDecryptRoundTrip(t *testing.T) {
	key := []byte("0123456789abcdef0123456789abcdef")
	c, err := NewCipher(key)
	if err != nil {
		t.Fatalf("NewCipher failed: %v", err)
	}

	secret := []byte("tech-authtoken-99887766-secret")
	ct1, nonce1, err := c.Encrypt(secret)
	if err != nil {
		t.Fatalf("Encrypt failed: %v", err)
	}
	ct2, nonce2, err := c.Encrypt(secret)
	if err != nil {
		t.Fatalf("Encrypt 2 failed: %v", err)
	}

	if bytes.Equal(nonce1, nonce2) || bytes.Equal(ct1, ct2) {
		t.Fatal("expected random nonces and distinct ciphertexts across encryptions")
	}

	pt, err := c.Decrypt(ct1, nonce1)
	if err != nil {
		t.Fatalf("Decrypt failed: %v", err)
	}
	if !bytes.Equal(pt, secret) {
		t.Fatalf("expected %q, got %q", secret, pt)
	}
}

func TestCipher_TamperAndKeyValidation(t *testing.T) {
	if _, err := NewCipher([]byte("short-key")); err == nil {
		t.Fatal("expected error for invalid key length")
	}

	c1, _ := NewCipher([]byte("0123456789abcdef0123456789abcdef"))
	c2, _ := NewCipher([]byte("fedcba9876543210fedcba9876543210"))

	ct, nonce, err := c1.Encrypt([]byte("my-token"))
	if err != nil {
		t.Fatalf("Encrypt failed: %v", err)
	}

	// Decrypt with wrong key
	if _, err := c2.Decrypt(ct, nonce); err == nil {
		t.Fatal("expected error when decrypting with wrong key")
	}

	// Decrypt with tampered ciphertext
	tampered := append([]byte(nil), ct...)
	tampered[0] ^= 0xff
	if _, err := c1.Decrypt(tampered, nonce); err == nil {
		t.Fatal("expected error when decrypting tampered ciphertext")
	}

	// Decrypt with bad nonce size
	if _, err := c1.Decrypt(ct, []byte("short")); err == nil {
		t.Fatal("expected error for invalid nonce length")
	}
}
