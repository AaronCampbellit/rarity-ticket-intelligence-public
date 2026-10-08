package mutation

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

var ErrIdempotencyConflict = errors.New("idempotency key payload conflict")

type ReplayError struct{ Response []byte }

func (e *ReplayError) Error() string { return "idempotent replay" }

func Fingerprint(value any) (string, error) {
	body, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(body)
	return hex.EncodeToString(sum[:]), nil
}
func DecodeReplay(err error, target any) (bool, error) {
	var replay *ReplayError
	if !errors.As(err, &replay) {
		return false, err
	}
	if decodeErr := json.Unmarshal(replay.Response, target); decodeErr != nil {
		return false, fmt.Errorf("decode idempotent replay: %w", decodeErr)
	}
	return true, nil
}
func ValidIdempotencyKey(v string) bool {
	v = strings.TrimSpace(v)
	if v == "" || len(v) > 200 {
		return false
	}
	for _, r := range v {
		if r < 0x21 || r > 0x7e {
			return false
		}
	}
	return true
}
