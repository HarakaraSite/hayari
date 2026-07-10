package server

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"strconv"
	"strings"
	"testing"
	"time"
)

func testKey(t *testing.T) []byte {
	t.Helper()
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		t.Fatal(err)
	}
	return key
}

func TestSignVerifyCookie(t *testing.T) {
	key := testKey(t)

	val := signCookie(key, "alice")
	if strings.Count(val, ":") < 2 {
		t.Fatalf("unexpected cookie format: %q", val)
	}

	user, ok := verifyCookie(key, val)
	if !ok {
		t.Fatal("valid cookie rejected")
	}
	if user != "alice" {
		t.Fatalf("got user %q, want %q", user, "alice")
	}
}

func TestVerifyCookieWrongKey(t *testing.T) {
	key := testKey(t)
	val := signCookie(key, "alice")

	otherKey := testKey(t)
	_, ok := verifyCookie(otherKey, val)
	if ok {
		t.Fatal("cookie accepted with wrong key")
	}
}

func TestVerifyCookieTamperedSig(t *testing.T) {
	key := testKey(t)
	val := signCookie(key, "alice")

	last := strings.LastIndex(val, ":")
	tampered := val[:last+1] + strings.Repeat("0", 64)
	_, ok := verifyCookie(key, tampered)
	if ok {
		t.Fatal("tampered signature should be rejected")
	}
}

func TestVerifyCookieTamperedUsername(t *testing.T) {
	key := testKey(t)
	val := signCookie(key, "alice")

	// Change username but keep the rest (signature will not match)
	first := strings.Index(val, ":")
	tampered := "mallory" + val[first:]
	_, ok := verifyCookie(key, tampered)
	if ok {
		t.Fatal("tampered username should be rejected")
	}
}

func TestVerifyCookieExpired(t *testing.T) {
	key := testKey(t)

	// Build an expired cookie manually
	exp := strconv.FormatInt(time.Now().Add(-time.Second).Unix(), 10)
	msg := "bob:" + exp
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte(msg))
	val := msg + ":" + hex.EncodeToString(mac.Sum(nil))

	_, ok := verifyCookie(key, val)
	if ok {
		t.Fatal("expired cookie should be rejected")
	}
}

func TestCredentialsMatch(t *testing.T) {
	if !credentialsMatch("user", "pass", "user", "pass") {
		t.Fatal("matching credentials returned false")
	}
	if credentialsMatch("user", "wrong", "user", "pass") {
		t.Fatal("wrong password returned true")
	}
	if credentialsMatch("other", "pass", "user", "pass") {
		t.Fatal("wrong username returned true")
	}
}

func TestLoginRateLimiterLocksAndResets(t *testing.T) {
	l := newLoginRateLimiter(2, time.Minute)
	now := time.Now()
	l.failure("127.0.0.1", now)
	if !l.allowed("127.0.0.1", now) {
		t.Fatal("first failure should be allowed")
	}
	l.failure("127.0.0.1", now)
	if l.allowed("127.0.0.1", now) {
		t.Fatal("second failure should lock")
	}
	l.success("127.0.0.1")
	if !l.allowed("127.0.0.1", now) {
		t.Fatal("successful login should reset attempts")
	}
}

func TestLoginRateLimiterExpiresOldFailuresAndCapsEntries(t *testing.T) {
	l := newLoginRateLimiter(5, time.Minute)
	l.retention = time.Minute
	l.capacity = 2
	now := time.Now()
	l.failure("one", now)
	l.failure("two", now)
	l.failure("three", now)
	if len(l.attempts) != 2 {
		t.Fatalf("attempts = %d, want capacity 2", len(l.attempts))
	}
	l.failure("four", now.Add(2*time.Minute))
	if len(l.attempts) != 1 {
		t.Fatalf("expired attempts were not pruned: %d remain", len(l.attempts))
	}
}
