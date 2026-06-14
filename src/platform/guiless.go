//go:build !gui

package platform

import (
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/nkanaev/yarr2/src/server"
)

func Start(s *server.Server) {
	log.Printf("Starting yarr2 on http://%s", s.Addr)

	errCh := make(chan error, 1)
	go func() {
		errCh <- s.Start()
	}()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)

	select {
	case err := <-errCh:
		if err != nil {
			log.Fatalf("server: %v", err)
		}
	case sig := <-sigCh:
		log.Printf("received signal: %v, shutting down", sig)
		s.Stop()
	}
}
