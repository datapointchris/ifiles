package auth

import (
	"encoding/base64"
	"testing"
)

// signedLikeAToken builds a JWT-shaped string with the given payload. The
// signature is nonsense on purpose: nothing here verifies it, and a test that
// needed a real one would be testing the server's job rather than this one's.
func signedLikeAToken(payload string) string {
	encode := func(s string) string { return base64.RawURLEncoding.EncodeToString([]byte(s)) }
	return encode(`{"alg":"HS256","typ":"JWT"}`) + "." + encode(payload) + ".c2lnbmF0dXJl"
}

func TestExpiresAtReadsTheExpClaim(t *testing.T) {
	t.Parallel()

	token := signedLikeAToken(`{"iat":1754000000,"exp":1786000000,"iss":"FileBrowser"}`)

	expiry, err := ExpiresAt(token)
	if err != nil {
		t.Fatalf("ExpiresAt() returned error: %v", err)
	}
	if expiry.Unix() != 1786000000 {
		t.Errorf("ExpiresAt() = %v, want the exp claim", expiry)
	}
}

// TestExpiresAtIgnoresAnUnverifiableSignature is the whole premise. The claim is
// read without checking the signature, because the server checks it on every
// request and a client rejecting its own token would only hide the date.
func TestExpiresAtIgnoresAnUnverifiableSignature(t *testing.T) {
	t.Parallel()

	token := signedLikeAToken(`{"exp":1786000000}`) + "tampered"

	if _, err := ExpiresAt(token); err != nil {
		t.Errorf("ExpiresAt() returned error: %v", err)
	}
}

// TestExpiresAtRejectsWhatItCannotRead covers the cases auth status reports as
// unknown rather than as a failure: the token may still authenticate, and the
// server is the authority on that.
func TestExpiresAtRejectsWhatItCannotRead(t *testing.T) {
	t.Parallel()

	cases := map[string]string{
		"empty":            "",
		"not a JWT":        "plain-api-key",
		"two segments":     "aGVhZGVy.cGF5bG9hZA",
		"payload not b64":  "aGVhZGVy.!!!not-base64!!!.c2ln",
		"payload not JSON": signedLikeAToken(`not json at all`),
		"no exp claim":     signedLikeAToken(`{"iat":1754000000}`),
	}

	for name, token := range cases {
		if _, err := ExpiresAt(token); err == nil {
			t.Errorf("ExpiresAt(%s) succeeded, want an error", name)
		}
	}
}

// TestExpiresAtSurvivesPaddinglessEncoding is the mistake this would most likely
// have shipped with. A JWT drops the base64 padding, so decoding with
// base64.URLEncoding rather than RawURLEncoding fails on most real tokens and
// succeeds on the occasional one whose payload length happens to be a multiple
// of three.
func TestExpiresAtSurvivesPaddinglessEncoding(t *testing.T) {
	t.Parallel()

	// Byte lengths of 26, 27, and 28: one for each remainder mod 3, which is what
	// decides how many padding characters the encoding would otherwise carry.
	payloads := []string{
		`{"exp":1786000000,"a":"b"}`,
		`{"exp":1786000000,"ab":"b"}`,
		`{"exp":1786000000,"abc":"b"}`,
	}
	for _, payload := range payloads {
		expiry, err := ExpiresAt(signedLikeAToken(payload))
		if err != nil {
			t.Errorf("ExpiresAt() returned error for a %d-byte payload: %v", len(payload), err)
			continue
		}
		if expiry.Unix() != 1786000000 {
			t.Errorf("ExpiresAt() = %v for a %d-byte payload", expiry, len(payload))
		}
	}
}
