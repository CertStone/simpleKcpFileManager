package gui

import (
	"fmt"
	"log"
	"path/filepath"
	"strings"

	kcpclient "certstone.cc/simpleKcpFileManager/kcpclient"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/widget"
)

// ContextMenu manages right-click context menus
type ContextMenu struct {
	mainWindow *MainWindow
}

// NewContextMenu creates a new context menu
func NewContextMenu(mainWindow *MainWindow) *ContextMenu {
	return &ContextMenu{
		mainWindow: mainWindow,
	}
}

// ShowFileListMenu shows context menu for file list items
func (cm *ContextMenu) ShowFileListMenu(file *kcpclient.ListItem, pos fyne.Position) {
	if file == nil {
		return
	}

	var items []*fyne.MenuItem

	// Add common items
	if file.IsDir {
		// Directory items
		items = append(items,
			fyne.NewMenuItem("Download Folder", func() {
				cm.downloadFolder(file)
			}),
			fyne.NewMenuItem("Open", func() {
				cm.mainWindow.navigateToPath(strings.TrimPrefix(file.Path, "/"))
			}),
			fyne.NewMenuItemSeparator(),
			fyne.NewMenuItem("Rename", func() {
				cm.showRenameDialog(file)
			}),
			fyne.NewMenuItem("Delete", func() {
				cm.showDeleteDialog(file)
			}),
			fyne.NewMenuItemSeparator(),
			fyne.NewMenuItem("Compress (ZIP)", func() {
				cm.compressItem(file, "zip")
			}),
			fyne.NewMenuItem("Compress (TAR)", func() {
				cm.compressItem(file, "tar")
			}),
			fyne.NewMenuItem("Compress (TAR.GZ)", func() {
				cm.compressItem(file, "tar.gz")
			}),
		)
	} else {
		// File items
		items = append(items,
			fyne.NewMenuItem("Download", func() {
				cm.downloadFile(file)
			}),
			fyne.NewMenuItemSeparator(),
			fyne.NewMenuItem("Rename", func() {
				cm.showRenameDialog(file)
			}),
			fyne.NewMenuItem("Delete", func() {
				cm.showDeleteDialog(file)
			}),
			fyne.NewMenuItemSeparator(),
		)

		// Check if file is an archive
		if cm.isArchive(file.Name) {
			items = append(items, fyne.NewMenuItem("Extract", func() {
				cm.extractArchive(file)
			}))
		} else {
			items = append(items, fyne.NewMenuItem("Edit", func() {
				cm.editFile(file)
			}))
		}

		// Add compress option for regular files too
		items = append(items,
			fyne.NewMenuItem("Compress (ZIP)", func() {
				cm.compressItem(file, "zip")
			}),
			fyne.NewMenuItem("Compress (TAR)", func() {
				cm.compressItem(file, "tar")
			}),
			fyne.NewMenuItem("Compress (TAR.GZ)", func() {
				cm.compressItem(file, "tar.gz")
			}),
		)
	}

	// Add common options
	items = append(items, fyne.NewMenuItemSeparator())
	items = append(items, fyne.NewMenuItem("Copy Path", func() {
		cm.mainWindow.window.Clipboard().SetContent(file.Path)
		dialog.ShowInformation("Copied", "Path copied to clipboard:\n"+file.Path, cm.mainWindow.window)
	}))

	// Add refresh option
	items = append(items, fyne.NewMenuItem("Refresh", func() {
		cm.mainWindow.refreshFileList()
	}))

	menu := fyne.NewMenu("File Options", items...)
	popUpMenu := widget.NewPopUpMenu(menu, cm.mainWindow.window.Canvas())
	popUpMenu.ShowAtPosition(pos)
}

// ShowBackgroundMenu shows context menu for empty space
func (cm *ContextMenu) ShowBackgroundMenu(pos fyne.Position) {
	items := []*fyne.MenuItem{
		fyne.NewMenuItem("Upload File", func() {
			cm.mainWindow.taskQueue.ShowUploadDialog(cm.mainWindow.saveDir)
		}),
		fyne.NewMenuItem("Upload Folder", func() {
			cm.mainWindow.taskQueue.ShowUploadFolderDialog(cm.mainWindow.saveDir)
		}),
		fyne.NewMenuItemSeparator(),
		fyne.NewMenuItem("New Folder", func() {
			cm.showNewFolderDialog()
		}),
		fyne.NewMenuItem("New File", func() {
			cm.showNewFileDialog()
		}),
		fyne.NewMenuItemSeparator(),
		fyne.NewMenuItem("Refresh", func() {
			cm.mainWindow.refreshFileList()
		}),
	}

	menu := fyne.NewMenu("Options", items...)
	popUpMenu := widget.NewPopUpMenu(menu, cm.mainWindow.window.Canvas())
	popUpMenu.ShowAtPosition(pos)
}

// downloadFile downloads a single file
func (cm *ContextMenu) downloadFile(file *kcpclient.ListItem) {
	// Use default download directory
	localPath := cm.mainWindow.saveDir + "/" + file.Name

	// Use pack transfer if enabled
	config := cm.mainWindow.packTransferConfig

	// Add download task (will use pack transfer based on config)
	if err := cm.mainWindow.taskQueue.AddDownloadTask(file.Path, localPath); err != nil {
		dialog.ShowError(err, cm.mainWindow.window)
	} else {
		statusMsg := fmt.Sprintf("Downloading '%s' to:\n%s", file.Name, localPath)
		if config.Enabled {
			statusMsg += "\n(打包传输已启用)"
		}
		dialog.ShowInformation("Download Started", statusMsg, cm.mainWindow.window)
	}
}

// downloadFolder downloads a folder recursively
func (cm *ContextMenu) downloadFolder(file *kcpclient.ListItem) {
	// Use default download directory
	saveDir := cm.mainWindow.saveDir + "/" + file.Name

	// List all files in the folder recursively
	go func() {
		files, err := cm.mainWindow.client.ListFiles(file.Path, true)
		if err != nil {
			dialog.ShowError(err, cm.mainWindow.window)
			return
		}

		log.Printf("[DEBUG] DownloadFolder: Found %d files", len(files))

		// Queue all files for download
		for _, f := range files {
			if !f.IsDir {
				remotePath := f.Path
				relativePath := strings.TrimPrefix(f.Path, file.Path)
				localPath := saveDir + relativePath

				log.Printf("[DEBUG] DownloadFolder: Queuing %s -> %s", remotePath, localPath)

				// Add download task
				if err := cm.mainWindow.taskQueue.AddDownloadTask(remotePath, localPath); err != nil {
					log.Printf("[DEBUG] DownloadFolder: Error queueing task - %v", err)
					dialog.ShowError(err, cm.mainWindow.window)
					return
				}
			}
		}

		dialog.ShowInformation("Download Started",
			fmt.Sprintf("Downloading %d files from '%s' to:\n%s", len(files), file.Name, saveDir),
			cm.mainWindow.window)
	}()
}

// showRenameDialog shows the rename dialog
func (cm *ContextMenu) showRenameDialog(file *kcpclient.ListItem) {
	entry := widget.NewEntry()
	entry.SetPlaceHolder("Enter new name")
	entry.SetText(file.Name)

	content := container.NewVBox(
		widget.NewLabel(fmt.Sprintf("Rename '%s':", file.Name)),
		entry,
	)

	dialog.ShowCustomConfirm("Rename", "Rename", "Cancel", content, func(confirmed bool) {
		if !confirmed || entry.Text == "" {
			return
		}

		newName := entry.Text
		oldPath := file.Path
		newPath := filepath.Dir(file.Path)
		if newPath == "." || newPath == "" {
			newPath = "/" + newName
		} else {
			newPath = newPath + "/" + newName
		}

		// Rename on server
		err := cm.mainWindow.client.RenameFile(oldPath, newPath)
		if err != nil {
			dialog.ShowError(err, cm.mainWindow.window)
			return
		}

		// Clear selection and refresh
		cm.mainWindow.selectedFile = nil
		cm.mainWindow.refreshFileList()
		cm.mainWindow.directoryTree.Refresh()
	}, cm.mainWindow.window)
}

// showDeleteDialog shows the delete confirmation dialog
func (cm *ContextMenu) showDeleteDialog(file *kcpclient.ListItem) {
	var msg string
	if file.IsDir {
		msg = fmt.Sprintf("Are you sure you want to delete the folder '%s' and all its contents?", file.Name)
	} else {
		msg = fmt.Sprintf("Are you sure you want to delete '%s'?", file.Name)
	}

	dialog.ShowConfirm("Delete", msg, func(confirmed bool) {
		if !confirmed {
			return
		}

		err := cm.mainWindow.client.DeleteFile(file.Path)
		if err != nil {
			dialog.ShowError(err, cm.mainWindow.window)
			return
		}

		// Clear selection
		cm.mainWindow.selectedFile = nil

		// Refresh
		cm.mainWindow.refreshFileList()
		cm.mainWindow.directoryTree.Refresh()
	}, cm.mainWindow.window)
}

// showNewFolderDialog shows the new folder dialog
func (cm *ContextMenu) showNewFolderDialog() {
	entry := widget.NewEntry()
	entry.SetPlaceHolder("Folder name")

	content := container.NewVBox(
		widget.NewLabel("Enter folder name:"),
		entry,
	)

	dialog.ShowCustomConfirm("New Folder", "Create", "Cancel", content, func(confirmed bool) {
		if !confirmed || entry.Text == "" {
			return
		}

		folderName := entry.Text
		var newPath string
		if cm.mainWindow.currentPath == "" {
			newPath = "/" + folderName
		} else {
			newPath = "/" + cm.mainWindow.currentPath + "/" + folderName
		}

		err := cm.mainWindow.client.CreateDirectory(newPath)
		if err != nil {
			dialog.ShowError(err, cm.mainWindow.window)
			return
		}

		// Refresh
		cm.mainWindow.refreshFileList()
		cm.mainWindow.directoryTree.Refresh()
	}, cm.mainWindow.window)
}

// showNewFileDialog shows the new file dialog
func (cm *ContextMenu) showNewFileDialog() {
	nameEntry := widget.NewEntry()
	nameEntry.SetPlaceHolder("File name (e.g., test.txt)")

	contentEntry := widget.NewMultiLineEntry()
	contentEntry.SetPlaceHolder("File content (for text files)")
	contentEntry.Resize(fyne.NewSize(400, 200))

	content := container.NewVBox(
		widget.NewLabel("Enter file name:"),
		nameEntry,
		widget.NewLabel("Enter content (optional):"),
		contentEntry,
	)

	dialog.ShowCustomConfirm("New File", "Create", "Cancel", content, func(confirmed bool) {
		if !confirmed || nameEntry.Text == "" {
			return
		}

		fileName := nameEntry.Text
		var newPath string
		if cm.mainWindow.currentPath == "" {
			newPath = "/" + fileName
		} else {
			newPath = "/" + cm.mainWindow.currentPath + "/" + fileName
		}

		// Save file content (can be empty for binary files)
		err := cm.mainWindow.client.SaveFile(newPath, contentEntry.Text)
		if err != nil {
			dialog.ShowError(err, cm.mainWindow.window)
			return
		}

		dialog.ShowInformation("Success",
			fmt.Sprintf("File '%s' created successfully", fileName),
			cm.mainWindow.window)

		// Refresh
		cm.mainWindow.refreshFileList()
		cm.mainWindow.directoryTree.Refresh()
	}, cm.mainWindow.window)
}

// compressItem compresses a file or folder
func (cm *ContextMenu) compressItem(file *kcpclient.ListItem, format string) {
	entry := widget.NewEntry()
	entry.SetPlaceHolder("Output archive name")
	defaultName := file.Name + "." + format
	entry.SetText(defaultName)

	content := container.NewVBox(
		widget.NewLabel(fmt.Sprintf("Compress '%s' to %s format:", file.Name, strings.ToUpper(format))),
		entry,
	)

	dialog.ShowCustomConfirm("Compress", "Compress", "Cancel", content, func(confirmed bool) {
		if !confirmed || entry.Text == "" {
			return
		}

		outputName := entry.Text
		if !strings.HasSuffix(outputName, "."+format) {
			outputName += "." + format
		}

		var outputPath string
		if cm.mainWindow.currentPath == "" {
			outputPath = "/" + outputName
		} else {
			outputPath = "/" + cm.mainWindow.currentPath + "/" + outputName
		}

		// Add compress task to queue
		if err := cm.mainWindow.taskQueue.AddCompressTask([]string{file.Path}, outputPath, format); err != nil {
			dialog.ShowError(err, cm.mainWindow.window)
		}
	}, cm.mainWindow.window)
}

// editFile edits a text file
func (cm *ContextMenu) editFile(file *kcpclient.ListItem) {
	// Check file size (1MB limit for editor as per documentation)
	const maxSize = 1 * 1024 * 1024
	if file.Size > maxSize {
		dialog.ShowError(fmt.Errorf("file too large for editing (>%d MB)", maxSize/(1024*1024)), cm.mainWindow.window)
		return
	}

	// Create and show text editor (it will do additional checks)
	editor := NewTextEditor(cm.mainWindow, file)
	if editor == nil {
		// NewTextEditor already showed an error dialog
		return
	}
	editor.Show()
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// isArchive checks if a file is an archive based on its extension
func (cm *ContextMenu) isArchive(filename string) bool {
	ext := strings.ToLower(filepath.Ext(filename))
	return ext == ".zip" || ext == ".tar" || ext == ".gz" || ext == ".tgz"
}

// extractArchive extracts an archive file
func (cm *ContextMenu) extractArchive(file *kcpclient.ListItem) {
	// Ask for destination directory
	entry := widget.NewEntry()
	defaultDest := strings.TrimSuffix(file.Name, filepath.Ext(file.Name))
	entry.SetText(defaultDest)
	entry.SetPlaceHolder("Destination folder name")

	content := container.NewVBox(
		widget.NewLabel(fmt.Sprintf("Extract '%s' to:", file.Name)),
		entry,
	)

	dialog.ShowCustomConfirm("Extract Archive", "Extract", "Cancel", content, func(confirmed bool) {
		if !confirmed || entry.Text == "" {
			return
		}

		destName := entry.Text
		var destPath string
		if cm.mainWindow.currentPath == "" {
			destPath = "/" + destName
		} else {
			destPath = "/" + cm.mainWindow.currentPath + "/" + destName
		}

		// Extract in background
		go func() {
			err := cm.mainWindow.client.Extract(file.Path, destPath)
			if err != nil {
				fyne.Do(func() {
					dialog.ShowError(err, cm.mainWindow.window)
				})
				return
			}

			// Refresh
			fyne.Do(func() {
				dialog.ShowInformation("Extract",
					fmt.Sprintf("Successfully extracted to %s", destPath),
					cm.mainWindow.window)
				cm.mainWindow.refreshFileList()
				cm.mainWindow.directoryTree.Refresh()
			})
		}()
	}, cm.mainWindow.window)
}
