package cryptpw

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"strings"

	"github.com/GehirnInc/crypt/sha512_crypt"
)

func SHA512(password string) (string, error) {
	if password == "" {
		return "", fmt.Errorf("empty password")
	}
	saltRaw := make([]byte, 12)
	if _, err := rand.Read(saltRaw); err != nil {
		return "", err
	}
	salt := strings.NewReplacer("+", ".", "/", ".", "=", "").Replace(base64.RawStdEncoding.EncodeToString(saltRaw))
	if len(salt) > 16 {
		salt = salt[:16]
	}
	return sha512_crypt.New().Generate([]byte(password), []byte("$6$rounds=5000$"+salt))
}
