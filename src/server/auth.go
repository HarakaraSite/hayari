package server

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"net/http"
	"strconv"
	"strings"
	"time"

	"forge.harakara.site/littleisland/hayari/src/assets"
)

const sessionCookieName = "session"
const sessionDuration = 7 * 24 * time.Hour

// ── Secret key (persisted in DB) ──────────────────────────────

func loadOrCreateAuthKey(getSetting func(string) (string, error), setSetting func(string, string) error) ([]byte, error) {
	val, err := getSetting("auth_secret")
	if err == nil && val != "" {
		key, err := hex.DecodeString(val)
		if err == nil && len(key) == 32 {
			return key, nil
		}
	}
	// Generate a fresh key
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		return nil, err
	}
	if err := setSetting("auth_secret", hex.EncodeToString(key)); err != nil {
		return nil, err
	}
	return key, nil
}

// ── Cookie signing / verification ──────────────────────────────
//
// Cookie value format:  username ":" expiry_unix ":" hex(HMAC-SHA256)
// HMAC covers the first two fields: "username:expiry_unix"

func signCookie(key []byte, username string) string {
	exp := strconv.FormatInt(time.Now().Add(sessionDuration).Unix(), 10)
	msg := username + ":" + exp
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte(msg))
	sig := hex.EncodeToString(mac.Sum(nil))
	return msg + ":" + sig
}

func verifyCookie(key []byte, value string) (username string, ok bool) {
	// Expect exactly 3 colon-separated parts.
	// Username may itself contain ":" so split from the right.
	last := strings.LastIndex(value, ":")
	if last < 0 {
		return "", false
	}
	sig := value[last+1:]
	rest := value[:last]

	// rest = "username:expiry"
	sep := strings.LastIndex(rest, ":")
	if sep < 0 {
		return "", false
	}
	expStr := rest[sep+1:]
	username = rest[:sep]

	// Verify expiry
	expUnix, err := strconv.ParseInt(expStr, 10, 64)
	if err != nil || time.Now().Unix() > expUnix {
		return "", false
	}

	// Verify HMAC
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte(rest))
	expected := hex.EncodeToString(mac.Sum(nil))
	if subtle.ConstantTimeCompare([]byte(sig), []byte(expected)) != 1 {
		return "", false
	}
	return username, true
}

// ── Credential comparison (timing-safe) ───────────────────────

func credentialsMatch(user, pass, wantUser, wantPass string) bool {
	uOK := subtle.ConstantTimeCompare([]byte(user), []byte(wantUser))
	pOK := subtle.ConstantTimeCompare([]byte(pass), []byte(wantPass))
	return uOK&pOK == 1
}

// ── /login  /logout handlers ───────────────────────────────────

func (s *Server) handleWebLogin(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		http.ServeFileFS(w, r, assets.FS, "login.html")
	case http.MethodPost:
		ip := clientIP(r)
		if !s.logins.allowed(ip, time.Now()) {
			http.Error(w, "too many login attempts", http.StatusTooManyRequests)
			return
		}
		r.ParseForm()
		username := r.FormValue("username")
		password := r.FormValue("password")

		if s.Username != "" || s.Password != "" {
			if !credentialsMatch(username, password, s.Username, s.Password) {
				s.logins.failure(ip, time.Now())
				http.Redirect(w, r, "/login?error=1", http.StatusSeeOther)
				return
			}
		}
		s.logins.success(ip)

		cookieVal := signCookie(s.authKey, username)
		http.SetCookie(w, &http.Cookie{
			Name:     sessionCookieName,
			Value:    cookieVal,
			Path:     "/",
			Expires:  time.Now().Add(sessionDuration),
			HttpOnly: true,
			SameSite: http.SameSiteLaxMode,
			Secure:   s.SecureCookie,
		})
		http.Redirect(w, r, "/", http.StatusSeeOther)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
	})
	http.Redirect(w, r, "/login", http.StatusSeeOther)
}
