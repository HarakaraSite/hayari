//go:build gui

package platform

import (
	"log"

	"fyne.io/systray"
	"github.com/nkanaev/yarr2/src/server"
)

func Start(s *server.Server) {
	onReady := func() {
		systray.SetTitle("yarr2")
		systray.SetTooltip("yarr2 RSS reader")

		mOpen := systray.AddMenuItem("Open", "Open in browser")
		systray.AddSeparator()
		mQuit := systray.AddMenuItem("Quit", "Quit yarr2")

		go func() {
			for {
				select {
				case <-mOpen.ClickedCh:
					OpenBrowser("http://" + s.Addr)
				case <-mQuit.ClickedCh:
					systray.Quit()
				}
			}
		}()

		go func() {
			if err := s.Start(); err != nil {
				log.Printf("server error: %v", err)
			}
		}()
	}

	onExit := func() {
		s.Stop()
	}

	systray.Run(onReady, onExit)
}
