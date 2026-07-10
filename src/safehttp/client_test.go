package safehttp

import (
	"errors"
	"net/http"
	"net/url"
	"testing"
	"time"
)

func TestValidateURLRejectsNonPublicAddresses(t *testing.T) {
	for _, raw := range []string{
		"http://127.0.0.1/", "http://[::1]/", "http://10.0.0.1/",
		"http://169.254.169.254/", "http://192.168.1.1/", "file:///tmp/a",
	} {
		u, err := url.Parse(raw)
		if err != nil {
			t.Fatal(err)
		}
		if !errors.Is(ValidateURL(u), ErrUnsafeAddress) {
			t.Errorf("ValidateURL(%q) should reject", raw)
		}
	}
}

func TestSafeClientRejectsLoopbackBeforeDialing(t *testing.T) {
	client := NewClient(time.Second)
	_, err := client.Get("http://127.0.0.1:1/")
	if !errors.Is(err, ErrUnsafeAddress) {
		t.Fatalf("error = %v, want ErrUnsafeAddress", err)
	}
}

func TestSafeClientRejectsRedirectToPrivateAddress(t *testing.T) {
	client := NewClient(time.Second)
	target, err := url.Parse("http://169.254.169.254/latest/meta-data")
	if err != nil {
		t.Fatal(err)
	}
	if !errors.Is(client.CheckRedirect(&http.Request{URL: target}, nil), ErrUnsafeAddress) {
		t.Fatal("redirect to private address should be rejected")
	}
}
