package gui

import (
	"fmt"
	"log"
	"os"
	"path"
	"path/filepath"
	"strings"

	kcpclient "github.com/CertStone/simpleKcpFileManager/kcpclient"

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

// filterNestedPaths filters out paths that are children of other paths in the list
// This prevents duplicate file uploads when both a folder and its contents are dropped
func filterNestedPaths(paths []string) []string {
	if len(paths) <= 1 {
		return paths
	}

	// Normalize paths for comparison
	normalizedPaths := make([]string, len(paths))
	for i, p := range paths {
		normalizedPaths[i] = filepath.Clean(p)
	}

	var result []string
	for i, path := range normalizedPaths {
		isNested := false
		for j, otherPath := range normalizedPaths {
			if i == j {
				continue
			}
			// Check if path is a child of otherPath
			if strings.HasPrefix(path, otherPath+string(filepath.Separator)) {
				isNested = true
				break
			}
		}
		if !isNested {
			result = append(result, paths[i]) // Use original path
		}
	}
	return result
}

// handleDroppedFiles processes files dropped onto the window
func (dd *DragDropHandler) handleDroppedFiles(uris []fyne.URI) {
	// Check if pack transfer is enabled
	packTransferEnabled := dd.mainWindow.packTransferConfig.Enabled

	// Separate folders and files
	var folders []string
	var files []string

	// First pass: collect all root paths and filter out nested ones
	var rootPaths []string
	for _, uri := range uris {
		rootPaths = append(rootPaths, uri.Path())
	}
	filteredPaths := filterNestedPaths(rootPaths)

	for _, localPath := range filteredPaths {
		info, err := os.Stat(localPath)
		if err != nil {
			log.Printf("[DEBUG] DragDrop: Cannot stat %s: %v", localPath, err)
			continue
		}

		if info.IsDir() {
			folders = append(folders, localPath)
		} else {
			files = append(files, localPath)
		}
	}

	// If pack transfer is enabled, handle folders separately as packed uploads
	if packTransferEnabled && len(folders) > 0 {
		dd.handlePackedFolderUpload(folders, files)
		return
	}

	// Pack transfer disabled or no folders - use traditional file-by-file upload
	dd.handleTraditionalUpload(filteredPaths)
}

// handlePackedFolderUpload handles folder uploads with pack transfer
func (dd *DragDropHandler) handlePackedFolderUpload(folders, files []string) {
	baseRemotePath := "/" + dd.mainWindow.currentPath
	if dd.mainWindow.currentPath == "" {
		baseRemotePath = ""
	}

	// Calculate total size
	var totalSize int64
	var folderCount int
	var fileCount int

	for _, folder := range folders {
		filepath.Walk(folder, func(path string, info os.FileInfo, err error) error {
			if err == nil && !info.IsDir() {
				totalSize += info.Size()
				fileCount++
			}
			return nil
		})
		folderCount++
	}

	for _, file := range files {
		info, _ := os.Stat(file)
		if info != nil {
			totalSize += info.Size()
			fileCount++
		}
	}

	// Show confirmation dialog
	sizeStr := formatSize(totalSize)
	displayPath := dd.mainWindow.currentPath
	if displayPath == "" {
		displayPath = "/"
	}

	var message string
	if len(folders) > 0 && len(files) > 0 {
		message = fmt.Sprintf("Upload %d folder(s) and %d file(s) (%s) to:\n%s\n\n(Pack transfer: folders will be compressed)",
			len(folders), len(files), sizeStr, displayPath)
	} else if len(folders) > 0 {
		message = fmt.Sprintf("Upload %d folder(s) (%d files, %s) to:\n%s\n\n(Pack transfer: folders will be compressed)",
			len(folders), fileCount, sizeStr, displayPath)
	} else {
		message = fmt.Sprintf("Upload %d file(s) (%s) to:\n%s", len(files), sizeStr, displayPath)
	}

	dialog.ShowConfirm("Upload Files", message, func(confirmed bool) {
		if !confirmed {
			return
		}

		// Upload folders as packed tasks
		for _, folder := range folders {
			folderName := filepath.Base(folder)
			remotePath := baseRemotePath + "/" + folderName

			log.Printf("[DEBUG] DragDrop: Queuing packed folder upload %s -> %s", folder, remotePath)

			if err := dd.mainWindow.taskQueue.AddUploadFolderTask(folder, remotePath); err != nil {
				log.Printf("[DEBUG] DragDrop: Error queueing folder upload: %v", err)
				dialog.ShowError(err, dd.mainWindow.window)
				return
			}
		}

		// Upload individual files normally
		for _, file := range files {
			fileName := filepath.Base(file)
			remotePath := baseRemotePath + "/" + fileName

			log.Printf("[DEBUG] DragDrop: Queuing file upload %s -> %s", file, remotePath)

			if err := dd.mainWindow.taskQueue.AddUploadTask(file, remotePath); err != nil {
				log.Printf("[DEBUG] DragDrop: Error queueing upload: %v", err)
				dialog.ShowError(err, dd.mainWindow.window)
				return
			}
		}

		dialog.ShowInformation("Upload Started",
			fmt.Sprintf("Uploading to %s", displayPath),
			dd.mainWindow.window)
	}, dd.mainWindow.window)
}

// handleTraditionalUpload handles file-by-file uploads (no pack transfer)
func (dd *DragDropHandler) handleTraditionalUpload(paths []string) {
	// Build list of files to upload with their relative paths
	type uploadFile struct {
		localPath  string
		remotePath string
	}
	var filesToUpload []uploadFile
	var totalSize int64

	// Use map to track added files and avoid duplicates
	addedFiles := make(map[string]bool)

	// Base remote path
	baseRemotePath := "/" + dd.mainWindow.currentPath
	if dd.mainWindow.currentPath == "" {
		baseRemotePath = ""
	}

	for _, localPath := range paths {
		// Check if file exists and get info
		info, err := os.Stat(localPath)
		if err != nil {
			log.Printf("[DEBUG] DragDrop: Cannot stat %s: %v", localPath, err)
			continue
		}

		if info.IsDir() {
			// For directories, preserve the folder structure
			folderName := filepath.Base(localPath)
			basePath := localPath

			err := filepath.Walk(localPath, func(filePath string, fi os.FileInfo, err error) error {
				if err != nil {
					return err
				}
				if !fi.IsDir() {
					if addedFiles[filePath] {
						return nil
					}

					relPath, err := filepath.Rel(basePath, filePath)
					if err != nil {
						return err
					}
					relPath = filepath.ToSlash(relPath)
					remotePath := baseRemotePath + "/" + folderName + "/" + relPath

					filesToUpload = append(filesToUpload, uploadFile{
						localPath:  filePath,
						remotePath: remotePath,
					})
					addedFiles[filePath] = true
					totalSize += fi.Size()
				}
				return nil
			})
			if err != nil {
				log.Printf("[DEBUG] DragDrop: Error walking directory %s: %v", localPath, err)
			}
		} else {
			if addedFiles[localPath] {
				continue
			}

			fileName := filepath.Base(localPath)
			remotePath := baseRemotePath + "/" + fileName

			filesToUpload = append(filesToUpload, uploadFile{
				localPath:  localPath,
				remotePath: remotePath,
			})
			addedFiles[localPath] = true
			totalSize += info.Size()
		}
	}

	if len(filesToUpload) == 0 {
		dialog.ShowInformation("No Files", "No valid files found to upload", dd.mainWindow.window)
		return
	}

	// Show confirmation dialog
	sizeStr := formatSize(totalSize)
	displayPath := dd.mainWindow.currentPath
	if displayPath == "" {
		displayPath = "/"
	}
	message := fmt.Sprintf("Upload %d file(s) (%s) to:\n%s", len(filesToUpload), sizeStr, displayPath)

	dialog.ShowConfirm("Upload Files", message, func(confirmed bool) {
		if !confirmed {
			return
		}

		for _, file := range filesToUpload {
			remoteDir := path.Dir(file.remotePath)
			if remoteDir != "" && remoteDir != "/" {
				if err := dd.mainWindow.client.CreateDirectory(remoteDir); err != nil {
					log.Printf("[DEBUG] DragDrop: Failed to create remote directory %s: %v", remoteDir, err)
				}
			}

			log.Printf("[DEBUG] DragDrop: Queuing upload %s -> %s", file.localPath, file.remotePath)

			if err := dd.mainWindow.taskQueue.AddUploadTask(file.localPath, file.remotePath); err != nil {
				log.Printf("[DEBUG] DragDrop: Error queueing upload: %v", err)
				dialog.ShowError(err, dd.mainWindow.window)
				return
			}
		}

		dialog.ShowInformation("Upload Started",
			fmt.Sprintf("Uploading %d file(s) to %s", len(filesToUpload), displayPath),
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
