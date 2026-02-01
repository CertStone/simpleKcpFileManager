package gui

import (
	"fmt"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/widget"
)

// ConnectionDialogResult holds the connection parameters
type ConnectionDialogResult struct {
	ServerAddress string
	EncryptionKey string
	Canceled      bool
}

// ShowConnectionDialog shows the connection dialog
func ShowConnectionDialog(window fyne.Window, defaultAddr, defaultKey string, onConfirm func(serverAddr, key string), onCancel func()) {
	addrEntry := widget.NewEntry()
	addrEntry.SetPlaceHolder("127.0.0.1:8080")
	if defaultAddr != "" {
		addrEntry.SetText(defaultAddr)
	}

	keyEntry := widget.NewPasswordEntry()
	keyEntry.SetPlaceHolder("Enter encryption key")
	if defaultKey != "" {
		keyEntry.SetText(defaultKey)
	}

	form := container.NewVBox(
		widget.NewLabel("Connect to KCP File Manager Server"),
		widget.NewSeparator(),
		widget.NewLabel("Server Address:"),
		addrEntry,
		widget.NewLabel("Encryption Key:"),
		keyEntry,
	)

	dialog.ShowCustomConfirm("Connect to Server", "Connect", "Cancel", form, func(confirmed bool) {
		if confirmed {
			serverAddr := addrEntry.Text
			key := keyEntry.Text

			// Validate inputs
			if serverAddr == "" {
				dialog.ShowError(fmt.Errorf("Server address is required"), window)
				return
			}
			if key == "" {
				dialog.ShowError(fmt.Errorf("Encryption key is required"), window)
				return
			}

			// Check if address contains port
			if !containsPort(serverAddr) {
				serverAddr = serverAddr + ":8080" // Default port
			}
			onConfirm(serverAddr, key)
		} else {
			// User clicked Cancel
			if onCancel != nil {
				onCancel()
			}
		}
	}, window)
}

// containsPort checks if address contains a port number
func containsPort(addr string) bool {
	for i := len(addr) - 1; i >= 0; i-- {
		if addr[i] == ':' {
			// Found a colon, check if it's followed by a port number
			// IPv6 addresses have multiple colons, but the port separator is the last one
			if i < len(addr)-1 {
				return true
			}
		}
	}
	return false
}

// ShowKeyInputDialog shows a dialog for entering encryption key
func ShowKeyInputDialog(window fyne.Window, onConfirm func(key string)) {
	keyEntry := widget.NewPasswordEntry()
	keyEntry.SetPlaceHolder("Enter encryption key")

	content := container.NewVBox(
		widget.NewLabel("Connection failed. The server may be using a different encryption key."),
		widget.NewLabel("Please enter the server's encryption key:"),
		keyEntry,
	)

	dialog.ShowCustomConfirm("Encryption Key Required", "Connect", "Cancel", content, func(confirm bool) {
		if confirm && keyEntry.Text != "" {
			onConfirm(keyEntry.Text)
		}
	}, window)
}
