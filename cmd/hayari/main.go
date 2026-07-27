package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"

	"forge.harakara.site/littleisland/hayari/src/platform"
	"forge.harakara.site/littleisland/hayari/src/server"
	"forge.harakara.site/littleisland/hayari/src/storage"
	"forge.harakara.site/littleisland/hayari/src/worker"
)

var Version = "dev"

func main() {
	var (
		addr                 = flag.String("addr", "127.0.0.1:7070", "listen address")
		dbPath               = flag.String("db", defaultDBPath(), "database path")
		username             = flag.String("user", "", "username for authentication")
		password             = flag.String("pass", "", "password for authentication")
		allowGReaderLoginGET = flag.Bool("allow-greader-login-get", false, "allow insecure GET requests to /accounts/ClientLogin")
		allowInsecureNoAuth  = flag.Bool("allow-insecure-no-auth", false, "allow unauthenticated non-loopback listener")
		secureCookie         = flag.Bool("secure-cookie", false, "mark session cookies Secure (required behind HTTPS proxy)")
		version              = flag.Bool("version", false, "print version and exit")
		open                 = flag.Bool("open", false, "open browser on start")
	)
	flag.Parse()

	if *version {
		fmt.Println(Version)
		return
	}
	worker.SetVersion(Version)

	db, err := storage.Open(*dbPath)
	if err != nil {
		log.Fatalf("failed to open database: %v", err)
	}
	defer db.Close()

	s := server.New(db, *addr, *username, *password, Version)
	s.AllowGReaderLoginGET = *allowGReaderLoginGET
	s.AllowInsecureNoAuth = *allowInsecureNoAuth
	s.SecureCookie = *secureCookie

	if *open {
		go platform.OpenBrowser("http://" + *addr)
	}

	platform.Start(s)
}

func defaultDBPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return "hayari.db"
	}
	return filepath.Join(home, ".hayari", "hayari.db")
}
