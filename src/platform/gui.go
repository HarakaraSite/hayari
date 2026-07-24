//go:build gui

package platform

import (
	"log"

	"forge.harakara.site/littleisland/hayari/src/server"
	"fyne.io/systray"
)

func Start(s *server.Server) {
	onReady := func() {
		systray.SetTitle("Hayari")
		systray.SetTooltip("Hayari RSS reader")

		mOpen := systray.AddMenuItem("Open", "Open in browser")
		systray.AddSeparator()
		mQuit := systray.AddMenuItem("Quit", "Quit Hayari")

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
