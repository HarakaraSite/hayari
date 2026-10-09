package server

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"net/url"
	"strings"
	"testing"
	"time"
)

var loginPaths = []string{
	"/login",
	"/accounts/ClientLogin",
	freshRSSGReaderPrefix + "/accounts/ClientLogin",
}

func loginRequest(t *testing.T, handler http.Handler, path, peer, xff, password string) *httptest.ResponseRecorder {
	t.Helper()
	form := url.Values{"username": {"user"}, "password": {password}, "Email": {"user"}, "Passwd": {password}}
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(form.Encode()))
	req.RemoteAddr = peer
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if xff != "" {
		req.Header.Set("X-Forwarded-For", xff)
	}
	resp := httptest.NewRecorder()
	handler.ServeHTTP(resp, req)
	return resp
}

func TestLoginRateLimitBehindCaddy(t *testing.T) {
	for _, peer := range []string{"127.0.0.1:1234", "[::1]:1234", "192.0.2.10:1234", "[2001:db8::10]:1234"} {
		for _, failurePath := range append(append([]string{}, loginPaths...), "mixed") {
			t.Run(peer+"/"+failurePath, func(t *testing.T) {
				proxies, err := parseTrustedProxies("127.0.0.1,::1,192.0.2.10,2001:db8::10")
				if err != nil {
					t.Fatal(err)
				}
				s := &Server{
					Username: "user", Password: "pass", authKey: testKey(t),
					logins:         newLoginRateLimiter(maxLoginFailures, loginLockDuration),
					tokens:         make(map[string]time.Time),
					trustedProxies: proxies,
				}
				handler := s.buildMux()
				const clientA = "198.51.100.1"
				const clientB = "2001:db8::2"
				for i := 0; i < 5; i++ {
					path := failurePath
					if path == "mixed" {
						path = loginPaths[i%len(loginPaths)]
					}
					// Changing an arbitrary prefix must not change Caddy's client IP.
					xff := fmt.Sprintf("203.0.113.%d, %s", i+1, clientA)
					resp := loginRequest(t, handler, path, peer, xff, "wrong")
					want := http.StatusUnauthorized
					if path == "/login" {
						want = http.StatusSeeOther
						if resp.Header().Get("Location") != "/login?error=1" {
							t.Fatalf("failed Web login location = %q", resp.Header().Get("Location"))
						}
					}
					if resp.Code != want {
						t.Fatalf("failure %d at %s = %d, want %d", i+1, path, resp.Code, want)
					}
				}
				for _, path := range loginPaths {
					resp := loginRequest(t, handler, path, peer, clientB, "pass")
					want := http.StatusOK
					if path == "/login" {
						want = http.StatusSeeOther
					}
					if resp.Code != want {
						t.Fatalf("client B login at %s = %d, want %d", path, resp.Code, want)
					}
					assertLoginCredential(t, s, path, resp)
					for _, lockedPath := range loginPaths {
						locked := loginRequest(t, handler, lockedPath, peer, clientA, "pass")
						if locked.Code != http.StatusTooManyRequests {
							t.Fatalf("client A after B success: %s = %d, want 429", lockedPath, locked.Code)
						}
					}
				}
			})
		}
	}
}

func TestParseTrustedProxies(t *testing.T) {
	for _, value := range []string{"", "127.0.0.1", "::1", " 127.0.0.1 , ::1 , 2001:0db8::10 ", "::ffff:127.0.0.1,127.0.0.1"} {
		t.Run(value, func(t *testing.T) {
			if _, err := parseTrustedProxies(value); err != nil {
				t.Fatalf("valid config rejected: %v", err)
			}
		})
	}
	proxies, err := parseTrustedProxies("::ffff:127.0.0.1,127.0.0.1,2001:0db8::10")
	if err != nil {
		t.Fatal(err)
	}
	for _, value := range []string{"127.0.0.1", "2001:db8::10"} {
		if _, ok := proxies[netip.MustParseAddr(value)]; !ok {
			t.Fatalf("missing canonical address %s", value)
		}
	}
	if len(proxies) != 2 {
		t.Fatalf("duplicate addresses not normalized: %v", proxies)
	}
}

func TestStartRejectsInvalidTrustedProxies(t *testing.T) {
	for _, value := range []string{"localhost", "192.0.2.0/24", "127.0.0.1:7070", "[::1]", "fe80::1%eth0", "invalid", "127.0.0.1,", ",::1", "127.0.0.1,,::1", " "} {
		t.Run(value, func(t *testing.T) {
			t.Setenv("HAYARI_TRUSTED_PROXIES", value)
			// Invalid configuration must fail before DB or worker startup.
			s := &Server{}
			if err := s.Start(); err == nil || !strings.Contains(err.Error(), "HAYARI_TRUSTED_PROXIES") {
				t.Fatalf("Start error = %v, want invalid trusted proxy configuration", err)
			}
		})
	}
}

func TestClientIP(t *testing.T) {
	proxies, err := parseTrustedProxies("127.0.0.1,::1,192.0.2.10,2001:db8::10,fe80::10")
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, peer string
		xff        []string
		want       string
	}{
		{"loopback IPv4", "127.0.0.1:1234", []string{"198.51.100.1"}, "198.51.100.1"},
		{"loopback IPv6", "[::1]:1234", []string{"2001:0db8::1"}, "2001:db8::1"},
		{"separate LXC", "192.0.2.10:1234", []string{"198.51.100.1"}, "198.51.100.1"},
		{"rightmost value", "192.0.2.10:1234", []string{"203.0.113.1, 198.51.100.1"}, "198.51.100.1"},
		{"multiple header lines", "192.0.2.10:1234", []string{"203.0.113.1", "198.51.100.1"}, "198.51.100.1"},
		{"missing header", "192.0.2.10:1234", nil, "192.0.2.10"},
		{"invalid rightmost", "192.0.2.10:1234", []string{"198.51.100.1, invalid"}, "192.0.2.10"},
		{"empty rightmost", "192.0.2.10:1234", []string{"198.51.100.1,"}, "192.0.2.10"},
		{"untrusted peer", "192.0.2.11:1234", []string{"198.51.100.1"}, "192.0.2.11"},
		{"mapped peer", "[::ffff:127.0.0.1]:1234", []string{"::ffff:198.51.100.1"}, "198.51.100.1"},
		{"IPv6 peer with interface zone", "[fe80::10%eth0]:1234", []string{"198.51.100.1"}, "198.51.100.1"},
		{"unparseable peer", "unknown", []string{"198.51.100.1"}, "unknown"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := &Server{trustedProxies: proxies}
			req := httptest.NewRequest(http.MethodPost, "/login", nil)
			req.RemoteAddr = tc.peer
			for _, value := range tc.xff {
				req.Header.Add("X-Forwarded-For", value)
			}
			if got := s.clientIP(req); got != tc.want {
				t.Fatalf("clientIP = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestLoginRateLimitIgnoresUntrustedHeaders(t *testing.T) {
	for _, config := range []string{"", "192.0.2.10"} {
		t.Run(config, func(t *testing.T) {
			proxies, err := parseTrustedProxies(config)
			if err != nil {
				t.Fatal(err)
			}
			s := &Server{
				Username: "user", Password: "pass", authKey: testKey(t),
				logins: newLoginRateLimiter(maxLoginFailures, loginLockDuration),
				tokens: make(map[string]time.Time), trustedProxies: proxies,
			}
			handler := s.buildMux()
			for i := 0; i < 5; i++ {
				path := loginPaths[i%len(loginPaths)]
				xff := fmt.Sprintf("198.51.100.%d", i+1)
				loginRequest(t, handler, path, "127.0.0.1:1234", xff, "wrong")
			}
			for _, path := range loginPaths {
				resp := loginRequest(t, handler, path, "127.0.0.1:5678", "2001:db8::2", "pass")
				if resp.Code != http.StatusTooManyRequests {
					t.Fatalf("spoofed XFF at %s = %d, want 429", path, resp.Code)
				}
			}
		})
	}
}

func TestLoginRateLimiterDefaultLockDuration(t *testing.T) {
	l := newLoginRateLimiter(maxLoginFailures, loginLockDuration)
	now := time.Now()
	for i := 0; i < 5; i++ {
		if !l.allowed("198.51.100.1", now) {
			t.Fatalf("locked before failure %d", i+1)
		}
		l.failure("198.51.100.1", now)
	}
	if l.allowed("198.51.100.1", now.Add(15*time.Minute-time.Nanosecond)) {
		t.Fatal("lock expired before 15 minutes")
	}
	if !l.allowed("198.51.100.1", now.Add(15*time.Minute)) {
		t.Fatal("lock did not expire after 15 minutes")
	}
}

func assertLoginCredential(t *testing.T, s *Server, path string, resp *httptest.ResponseRecorder) {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	var handler http.HandlerFunc
	accepted := func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusNoContent) }
	if path == "/login" {
		if resp.Header().Get("Location") != "/" || len(resp.Result().Cookies()) == 0 {
			t.Fatal("successful Web login must redirect to / and issue a cookie")
		}
		for _, cookie := range resp.Result().Cookies() {
			req.AddCookie(cookie)
		}
		handler = s.authMiddleware(accepted)
	} else {
		var token string
		for _, line := range strings.Split(resp.Body.String(), "\n") {
			if strings.HasPrefix(line, "Auth=") {
				token = strings.TrimPrefix(line, "Auth=")
			}
		}
		if token == "" {
			t.Fatal("successful GReader login must issue an Auth token")
		}
		req.Header.Set("Authorization", "GoogleLogin auth="+token)
		handler = s.greaderAuthMiddleware(accepted)
	}
	check := httptest.NewRecorder()
	handler(check, req)
	if check.Code != http.StatusNoContent {
		t.Fatalf("issued credential rejected: status %d", check.Code)
	}
}
