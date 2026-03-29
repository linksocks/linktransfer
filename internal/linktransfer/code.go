package linktransfer

import (
	"crypto/rand"
	"encoding/hex"
)

func getRandomCode() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
