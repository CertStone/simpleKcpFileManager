package gui

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"

	kcpclient "certstone.cc/simpleKcpFileManager/kcpclient"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/dialog"
)

// DragDropHandler manages drag and drop functionality
type DragDropHandler struct {
	mainWindow *MainWindow
}

// NewDragDropHandler creates a new drag and drop handler
func NewDragDropHandler(mainWindow *MainWindow) *DragDropHandler {
	return &DragDropHandler{
		mainWindow: mainWindow,
	}
}

// SetupWindowDragDrop sets up window-level drag and drop handling
func (dd *DragDropHandler) SetupWindowDragDrop() {
	dd.mainWindow.window.SetOnDropped(func(pos fyne.Position, uris []fyne.URI) {
		log.Printf("[DEBUG] DragDrop: Received %d files at position (%f, %f)", len(uris), pos.X, pos.Y)

		if len(uris) == 0 {
			return
		}

		// Check if connected
		if !dd.mainWindow.client.IsConnected() {
			dialog.ShowError(fmt.Errorf("not connected to server"), dd.mainWindow.window)
			return
		}

		// Process dropped files
		dd.handleDroppedFiles(uris)
	})
}

// handleDroppedFiles processes files dropped onto the window
func (dd *DragDropHandler) handleDroppedFiles(uris []fyne.URI) {
	// Build list of files to upload
	var filesToUpload []string
	var totalSize int64

	for _, uri := range uris {
		localPath := uri.Path()

		// Check if file exists and get info
		info, err := os.Stat(localPath)
		if err != nil {
			log.Printf("[DEBUG] DragDrop: Cannot stat %s: %v", localPath, err)
			continue
		}

		if info.IsDir() {
			// Walk directory to get all files
			err := filepath.Walk(localPath, func(path string, fi os.FileInfo, err error) error {
				if err != nil {
					return err
				}
				if !fi.IsDir() {
					filesToUpload = append(filesToUpload, path)
					totalSize += fi.Size()
				}
				return nil
			})
			if err != nil {
				log.Printf("[DEBUG] DragDrop: Error walking directory %s: %v", localPath, err)
			}
		} else {
			filesToUpload = append(filesToUpload, localPath)
			totalSize += info.Size()
		}
	}

	if len(filesToUpload) == 0 {
		dialog.ShowInformation("No Files", "No valid files found to upload", dd.mainWindow.window)
		return
	}

	// Show confirmation dialog
	sizeStr := formatSize(totalSize)
	message := fmt.Sprintf("Upload %d file(s) (%s) to:\n/%s", len(filesToUpload), sizeStr, dd.mainWindow.currentPath)

	dialog.ShowConfirm("Upload Files", message, func(confirmed bool) {
		if !confirmed {
			return
		}

		// Queue all files for upload
		for _, localPath := range filesToUpload {
			fileName := filepath.Base(localPath)

			// Build remote path
			var remotePath string
			if dd.mainWindow.currentPath == "" {
				remotePath = "/" + fileName
			} else {
				remotePath = "/" + dd.mainWindow.currentPath + "/" + fileName
			}

			log.Printf("[DEBUG] DragDrop: Queuing upload %s -> %s", localPath, remotePath)

			if err := dd.mainWindow.taskQueue.AddUploadTask(localPath, remotePath); err != nil {
				log.Printf("[DEBUG] DragDrop: Error queueing upload: %v", err)
				dialog.ShowError(err, dd.mainWindow.window)
				return
			}
		}

		dialog.ShowInformation("Upload Started",
			fmt.Sprintf("Uploading %d file(s) to /%s", len(filesToUpload), dd.mainWindow.currentPath),
			dd.mainWindow.window)
	}, dd.mainWindow.window)
}

// downloadFile downloads a single file
func (dd *DragDropHandler) downloadFile(file *kcpclient.ListItem) {
	dialog.ShowFileSave(func(writer fyne.URIWriteCloser, err error) {
		if err != nil || writer == nil {
			return
		}
		defer writer.Close()

		localPath := writer.URI().Path()

		// Add download task
		if err := dd.mainWindow.taskQueue.AddDownloadTask(file.Path, localPath); err != nil {
			dialog.ShowError(err, dd.mainWindow.window)
		}
	}, dd.mainWindow.window)
}

// downloadFolder downloads a folder recursively
func (dd *DragDropHandler) downloadFolder(file *kcpclient.ListItem) {
	dialog.ShowFolderOpen(func(uri fyne.ListableURI, err error) {
		if err != nil || uri == nil {
			return
		}

		saveDir := uri.Path()

		// List all files in the folder recursively
		go func() {
			files, err := dd.mainWindow.client.ListFiles(file.Path, true)
			if err != nil {
				dialog.ShowError(err, dd.mainWindow.window)
				return
			}

			log.Printf("[DEBUG] DownloadFolder: Found %d files", len(files))

			// Queue all files for download
			for _, f := range files {
				if !f.IsDir {
					remotePath := f.Path
					relativePath := strings.TrimPrefix(f.Path, file.Path)
					localPath := filepath.Join(saveDir, relativePath)

					log.Printf("[DEBUG] DownloadFolder: Queuing %s -> %s", remotePath, localPath)

					// Add download task
					if err := dd.mainWindow.taskQueue.AddDownloadTask(remotePath, localPath); err != nil {
						log.Printf("[DEBUG] DownloadFolder: Error queueing task - %v", err)
						dialog.ShowError(err, dd.mainWindow.window)
						return
					}
				}
			}

			dialog.ShowInformation("Download Started",
				fmt.Sprintf("Downloading %d files from %s", len(files), file.Name),
				dd.mainWindow.window)
		}()
	}, dd.mainWindow.window)
}
