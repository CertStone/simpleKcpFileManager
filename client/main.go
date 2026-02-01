package main

import (
	"flag"
	"os"

	"certstone.cc/simpleKcpFileManager/client/gui"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
)

func main() {
	serverAddr := flag.String("server", "", "KCP server address (e.g., 127.0.0.1:8080)")
	encryptionKey := flag.String("key", "", "Encryption key")
	saveDir := flag.String("dir", "./downloads", "Directory for downloads")
	flag.Parse()

	myApp := app.New()

	// If server address or key not provided, show connection dialog
	if *serverAddr == "" || *encryptionKey == "" {
		// Create temporary window for connection dialog
		window := myApp.NewWindow("KCP File Manager - Connect")
		window.Resize(fyne.NewSize(500, 350))
		window.CenterOnScreen()

		gui.ShowConnectionDialog(window, *serverAddr, *encryptionKey,
			func(addr, key string) {
				// User clicked Connect - reuse this window as main window
				os.MkdirAll(*saveDir, 0755)

				config := gui.MainWindowConfig{
					App:           myApp,
					ServerAddr:    addr,
					EncryptionKey: key,
					SaveDir:       *saveDir,
				}

				mainWindow := gui.NewMainWindowWithWindow(config, window)
				mainWindow.Show()
			},
			func() {
				// User clicked Cancel - quit application
				window.Close()
				myApp.Quit()
			})

		window.ShowAndRun()
	} else {
		// Direct connection
		os.MkdirAll(*saveDir, 0755)

		config := gui.MainWindowConfig{
			App:           myApp,
			ServerAddr:    *serverAddr,
			EncryptionKey: *encryptionKey,
			SaveDir:       *saveDir,
		}

		mainWindow := gui.NewMainWindow(config)
		mainWindow.ShowAndRun()
	}
}
