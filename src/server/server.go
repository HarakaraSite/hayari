package server

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"sync"
	"time"

	"forge.harakara.site/littleisland/hayari/src/storage"
	"forge.harakara.site/littleisland/hayari/src/worker"
)

const (
	readHeaderTimeout = 10 * time.Second
	readTimeout       = 30 * time.Second
	writeTimeout      = 30 * time.Second
	idleTimeout       = 2 * time.Minute
	shutdownTimeout   = 10 * time.Second
)

type Server struct {
	Addr                 string
	Username             string
	Password             string
	Version              string
	AllowGReaderLoginGET bool
	AllowInsecureNoAuth  bool
	SecureCookie         bool

	authKey []byte
	db      *storage.Storage
	worker  *worker.Worker
	http    *http.Server
	logins  *loginRateLimiter
	tokenMu sync.RWMutex
	tokens  map[string]time.Time
}

func New(db *storage.Storage, addr, username, password, version string) *Server {
	s := &Server{
		Addr:     addr,
		Username: username,
		Password: password,
		Version:  version,
		db:       db,
		logins:   newLoginRateLimiter(maxLoginFailures, loginLockDuration),
		tokens:   make(map[string]time.Time),
	}
	s.worker = worker.New(db)
	return s
}

func (s *Server) Start() error {
	if s.Username == "" && s.Password == "" && !s.AllowInsecureNoAuth && !isLoopbackAddress(s.Addr) {
		return fmt.Errorf("refusing unauthenticated non-loopback listener %q; configure --user/--pass or explicitly allow insecure access", s.Addr)
	}
	key, err := loadOrCreateAuthKey(s.db.GetSetting, s.db.SetSetting)
	if err != nil {
		return fmt.Errorf("auth key: %w", err)
	}
	s.authKey = key

	s.worker.Start()

	mux := http.NewServeMux()
	s.registerRoutes(mux)
	s.registerGReaderRoutes(mux)

	s.http = s.newHTTPServer(mux)
	return s.http.ListenAndServe()
}

func isLoopbackAddress(addr string) bool {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return false
	}
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func (s *Server) newHTTPServer(handler http.Handler) *http.Server {
	return &http.Server{
		Addr:              s.Addr,
		Handler:           handler,
		ReadHeaderTimeout: readHeaderTimeout,
		ReadTimeout:       readTimeout,
		WriteTimeout:      writeTimeout,
		IdleTimeout:       idleTimeout,
	}
}

func (s *Server) Stop() {
	s.worker.Stop()
	if s.http != nil {
		ctx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
		defer cancel()
		s.http.Shutdown(ctx)
	}
}
