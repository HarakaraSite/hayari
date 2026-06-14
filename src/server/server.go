package server

import (
	"context"
	"fmt"
	"net/http"

	"github.com/nkanaev/yarr2/src/storage"
	"github.com/nkanaev/yarr2/src/worker"
)

type Server struct {
	Addr     string
	Username string
	Password string

	authKey []byte
	db      *storage.Storage
	worker  *worker.Worker
	http    *http.Server
}

func New(db *storage.Storage, addr, username, password string) *Server {
	s := &Server{
		Addr:     addr,
		Username: username,
		Password: password,
		db:       db,
	}
	s.worker = worker.New(db)
	return s
}

func (s *Server) Start() error {
	key, err := loadOrCreateAuthKey(s.db.GetSetting, s.db.SetSetting)
	if err != nil {
		return fmt.Errorf("auth key: %w", err)
	}
	s.authKey = key

	s.worker.Start()

	mux := http.NewServeMux()
	s.registerRoutes(mux)
	s.registerGReaderRoutes(mux)

	s.http = &http.Server{
		Addr:    s.Addr,
		Handler: mux,
	}
	return s.http.ListenAndServe()
}

func (s *Server) Stop() {
	s.worker.Stop()
	if s.http != nil {
		s.http.Shutdown(context.Background())
	}
}
