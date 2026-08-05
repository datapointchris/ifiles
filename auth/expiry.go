package auth

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// ExpiresAt reports when an API token stops being accepted, read out of the
// token itself rather than asked of the server.
//
// A FileBrowser API token is a JWT, and its exp claim is public: verifying the
// signature proves the server minted it, which is the server's job on every
// request and not a client's here. Nothing is trusted from this beyond a date to
// show — the server remains the only authority on whether a token works.
//
// Reading it locally is the point. The API's only signal is a 401 once the token
// has already died, and the X-Renew-Token header the server sets fires thirty
// minutes out, which on a year-long token is no warning at all. A token that
// expires silently turns every command into an authentication failure on a day
// nobody chose; a date in `auth status` turns that into a diary entry.
func ExpiresAt(token string) (time.Time, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return time.Time{}, fmt.Errorf("token is not a JWT")
	}

	// Raw encoding: a JWT omits the padding base64.URLEncoding would require.
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return time.Time{}, fmt.Errorf("token payload is not base64url: %w", err)
	}

	var claims struct {
		Expires int64 `json:"exp"`
	}
	if err := json.Unmarshal(payload, &claims); err != nil {
		return time.Time{}, fmt.Errorf("token payload is not JSON: %w", err)
	}
	if claims.Expires == 0 {
		return time.Time{}, fmt.Errorf("token carries no expiry")
	}
	return time.Unix(claims.Expires, 0), nil
}
