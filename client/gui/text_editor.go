package gui

import (
	"fmt"
	"log"
	"strings"
	"time"

	kcpclient "github.com/CertStone/simpleKcpFileManager/kcpclient"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/widget"
)

// TextEditor manages the text editor window
type TextEditor struct {
	mainWindow *MainWindow
	window     fyne.Window
	file       *kcpclient.ListItem
	textEntry  *widget.Entry
	saveBtn    *widget.Button
	statusLabel *widget.Label
	isModified bool
}

// NewTextEditor creates a new text editor
func NewTextEditor(mainWindow *MainWindow, file *kcpclient.ListItem) *TextEditor {
	// Final size check (1MB limit as per documentation)
	const maxSize = 1 * 1024 * 1024
	if file.Size > maxSize {
		dialog.ShowError(fmt.Errorf("file too large for editing (>%d MB)", maxSize/(1024*1024)), mainWindow.window)
		return nil
	}

	te := &TextEditor{
		mainWindow: mainWindow,
		file:       file,
		isModified: false,
	}

	// Create editor window
	te.window = mainWindow.app.NewWindow(fmt.Sprintf("Editing: %s", file.Name))
	te.window.Resize(fyne.NewSize(800, 600))
	te.window.CenterOnScreen()

	te.setupUI()

	return te
}

// setupUI sets up the editor UI
func (te *TextEditor) setupUI() {
	log.Printf("[DEBUG] TextEditor.setupUI: Setting up UI for %s", te.file.Name)

	// Create text entry (multiline)
	te.textEntry = widget.NewMultiLineEntry()
	te.textEntry.SetPlaceHolder("Loading file content...")
	te.textEntry.TextStyle = fyne.TextStyle{Monospace: true}

	// Track modifications
	te.textEntry.OnChanged = func(s string) {
		if !te.isModified {
			te.isModified = true
			te.updateWindowTitle()
		}
	}

	// Status label
	te.statusLabel = widget.NewLabel("Loading...")

	// Toolbar buttons
	te.saveBtn = widget.NewButton("Save", func() {
		te.saveFile()
	})
	te.saveBtn.Disable()

	closeBtn := widget.NewButton("Close", func() {
		te.close()
	})

	toolbar := container.NewHBox(
		te.saveBtn,
		closeBtn,
		widget.NewSeparator(),
		te.statusLabel,
	)

	// Main layout
	content := container.NewBorder(
		toolbar,
		nil,
		nil,
		nil,
		te.textEntry,
	)

	te.window.SetContent(content)

	// Add keyboard shortcuts
	te.window.Canvas().SetOnTypedKey(func(key *fyne.KeyEvent) {
		// Check for Escape key to close
		if key.Name == fyne.KeyEscape {
			log.Printf("[DEBUG] TextEditor: Escape pressed, closing")
			te.close()
		}
	})

	// Add Ctrl+S shortcut to save via canvas shortcut
	te.window.Canvas().AddShortcut(&desktop.CustomShortcut{
			KeyName: fyne.KeyS,
			Modifier: fyne.KeyModifierControl,
		}, func(sh fyne.Shortcut) {
		log.Printf("[DEBUG] TextEditor: Ctrl+S pressed")
		te.saveFile()
	})

	// Load file content
	log.Printf("[DEBUG] TextEditor.setupUI: Loading file content")
	go te.loadContent()
}

// loadContent loads the file content from server
func (te *TextEditor) loadContent() {
	log.Printf("[DEBUG] TextEditor.loadContent: START for %s", te.file.Path)

	fyne.Do(func() {
		te.statusLabel.SetText("Loading...")
	})

	content, err := te.mainWindow.client.ReadFile(te.file.Path)
	if err != nil {
		log.Printf("[DEBUG] TextEditor.loadContent: Error - %v", err)
		fyne.Do(func() {
			dialog.ShowError(err, te.window)
			te.statusLabel.SetText("Load failed")
		})
		return
	}

	log.Printf("[DEBUG] TextEditor.loadContent: Loaded %d bytes", len(content))

	// Check if content is valid UTF-8 text
	if !te.isLikelyText(content) {
		log.Printf("[DEBUG] TextEditor.loadContent: Content is not text")
		fyne.Do(func() {
			dialog.ShowError(fmt.Errorf("file does not appear to be text (may contain binary data)"), te.window)
			te.statusLabel.SetText("Not a text file")
		})
		return
	}

	// Update UI
	fyne.Do(func() {
		te.textEntry.SetText(content)
		te.textEntry.Refresh()
		te.statusLabel.SetText(fmt.Sprintf("Loaded %d bytes", len(content)))
		te.saveBtn.Enable()
		te.isModified = false
		te.updateWindowTitle()
	})

	log.Printf("[DEBUG] TextEditor.loadContent: END")
}

// isLikelyText checks if content is likely text
func (te *TextEditor) isLikelyText(content string) bool {
	// Empty files are considered text
	if len(content) == 0 {
		return true
	}

	// Check for null bytes (common in binary files)
	if strings.Contains(content, "\x00") {
		return false
	}

	// Check if most characters are printable ASCII or common UTF-8
	printableCount := 0
	for _, r := range content {
		if r == '\n' || r == '\r' || r == '\t' ||
			(r >= 32 && r <= 126) || // Printable ASCII
			(r > 127) { // UTF-8
			printableCount++
		}
	}

	// If less than 90% printable, likely binary
	ratio := float64(printableCount) / float64(len(content))
	return ratio > 0.9
}

// saveFile saves the file content to server
func (te *TextEditor) saveFile() {
	log.Printf("[DEBUG] TextEditor.saveFile: START for %s", te.file.Path)

	content := te.textEntry.Text

	fyne.Do(func() {
		te.saveBtn.Disable()
		te.statusLabel.SetText("Saving...")
	})

	// Save in background
	go func() {
		err := te.mainWindow.client.SaveFile(te.file.Path, content)
		if err != nil {
			log.Printf("[DEBUG] TextEditor.saveFile: Error - %v", err)
			fyne.Do(func() {
				dialog.ShowError(err, te.window)
				te.statusLabel.SetText("Save failed")
				te.saveBtn.Enable()
			})
			return
		}

		log.Printf("[DEBUG] TextEditor.saveFile: Saved %d bytes", len(content))

		fyne.Do(func() {
			te.isModified = false
			te.updateWindowTitle()
			te.statusLabel.SetText(fmt.Sprintf("Saved at %s", time.Now().Format("15:04:05")))
			te.saveBtn.Enable()

			// Refresh file list in main window
			te.mainWindow.refreshFileList()
		})

		log.Printf("[DEBUG] TextEditor.saveFile: END")
	}()
}

// close closes the editor
func (te *TextEditor) close() {
	log.Printf("[DEBUG] TextEditor.close: Closing editor for %s", te.file.Path)

	if te.isModified {
		dialog.ShowConfirm("Unsaved Changes",
			"You have unsaved changes. Do you want to close without saving?",
			func(confirmed bool) {
				if confirmed {
					te.window.Close()
				}
			},
			te.window)
		return
	}

	te.window.Close()
}

// updateWindowTitle updates the window title to show modification status
func (te *TextEditor) updateWindowTitle() {
	title := te.file.Name
	if te.isModified {
		title = "* " + title
	}
	te.window.SetTitle(title)
}

// Show shows the editor window
func (te *TextEditor) Show() {
	log.Printf("[DEBUG] TextEditor.Show: Showing editor for %s", te.file.Path)
	te.window.Show()
}
