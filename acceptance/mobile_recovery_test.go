package acceptance

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"io"
	"net/http"
	"strconv"
	"testing"
)

func recoveryNativeSeal(t *testing.T, aad string) (func([]byte) error, func() []byte) {
	t.Helper()
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		t.Fatal(err)
	}
	block := environmentValue(aes.NewCipher(key))
	gcm := environmentValue(cipher.NewGCM(block))
	clear(key)
	var sealed []byte
	return func(plain []byte) error {
			nonce := make([]byte, gcm.NonceSize())
			if _, err := rand.Read(nonce); err != nil {
				return err
			}
			sealed = gcm.Seal(nonce, nonce, plain, []byte(aad))
			return nil
		}, func() []byte {
			t.Helper()
			if len(sealed) < gcm.NonceSize() {
				t.Fatal("native protected recovery state missing")
			}
			return environmentValue(gcm.Open(nil, sealed[:gcm.NonceSize()], sealed[gcm.NonceSize():], []byte(aad)))
		}
}

func environmentValue2[A any, B any](a A, b B, err error) (A, B) {
	if err != nil {
		panic(err)
	}
	return a, b
}

func authorityLostResponse(r *http.Response) {
	_ = r.Body.Close()
	data := []byte(`{"error":"synthetic_lost_authority_receipt"}`)
	r.StatusCode = 502
	r.Body = io.NopCloser(bytes.NewReader(data))
	r.ContentLength = int64(len(data))
	r.Header.Set("Content-Length", strconv.Itoa(len(data)))
}
