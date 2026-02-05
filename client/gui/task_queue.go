package gui

import (
	"fmt"
	"log"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"certstone.cc/simpleKcpFileManager/kcpclient/tasks"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

// TaskQueue manages the task queue display
type TaskQueue struct {
	mainWindow   *MainWindow
	taskManager  *tasks.Manager
	container    *fyne.Container
	taskWidgets  map[string]*TaskWidget
	taskMutex    sync.RWMutex
	isRebuilding bool // Flag to prevent concurrent rebuilds
}

// TaskWidget represents a single task in the queue
type TaskWidget struct {
	task         *tasks.Task
	fileLabel    *widget.Label
	statusLabel  *widget.Label
	progressBar  *widget.ProgressBar
	retryBtn     *widget.Button
	pauseBtn     *widget.Button
	resumeBtn    *widget.Button
	cancelBtn    *widget.Button
	manualCancel bool
	lastUpdate   time.Time
	updateTicker *time.Ticker
	stopTicker   chan struct{}
	container    *fyne.Container // Reference to prevent duplicate UI adds
}

// NewTaskQueue creates a new task queue
func NewTaskQueue(mainWindow *MainWindow) *TaskQueue {
	tq := &TaskQueue{
		mainWindow:  mainWindow,
		taskManager: mainWindow.taskManager,
		taskWidgets: make(map[string]*TaskWidget),
		container:   container.NewVBox(),
	}

	// Set up completion callback to refresh file list when tasks complete
	tasks.OnTaskCompleted = func(task *tasks.Task) {
		if task.Status == tasks.StatusCompleted {
			// Refresh file list
			fyne.Do(func() {
				mainWindow.refreshFileList()
				if mainWindow.directoryTree != nil {
					mainWindow.directoryTree.Refresh()
				}
			})
		}
	}

	// Start update ticker
	go tq.updateLoop()

	return tq
}

// GetContainer returns the container for the task queue
func (tq *TaskQueue) GetContainer() *fyne.Container {
	return tq.container
}

// updateLoop periodically updates the task display
func (tq *TaskQueue) updateLoop() {
	ticker := time.NewTicker(500 * time.Millisecond) // Update every 500ms for smoother progress
	defer ticker.Stop()

	for range ticker.C {
		allTasks := tq.taskManager.GetAllTasks()

		// Update each task widget
		for _, task := range allTasks {
			tq.updateTaskWidget(task)
		}

		// Check for completed tasks to remove
		tq.taskMutex.Lock()
		var toRemove []string
		for id, tw := range tq.taskWidgets {
			if tw.task.Status == tasks.StatusCompleted || tw.task.Status == tasks.StatusCanceled {
				// Remove completed tasks after 3 seconds
				if time.Since(tw.lastUpdate) > 3*time.Second {
					log.Printf("[DEBUG] TaskQueue.updateLoop: Marking for removal: %s", id)
					toRemove = append(toRemove, id)
				}
			}
		}

		// Remove from both taskWidgets map AND taskManager to prevent recreation
		for _, id := range toRemove {
			delete(tq.taskWidgets, id)
			// Also remove from taskManager to prevent updateTaskWidget from recreating it
			tq.taskManager.RemoveTask(id)
		}
		tq.taskMutex.Unlock()

		// Rebuild UI if needed (no need to wrap in fyne.Do, rebuildTaskList handles it)
		if len(toRemove) > 0 {
			tq.rebuildTaskList()
		}
	}
}

// refreshTasks updates the display of all tasks
func (tq *TaskQueue) refreshTasks() {
	log.Printf("[DEBUG] TaskQueue.refreshTasks: START")
	allTasks := tq.taskManager.GetAllTasks()

	// Update widgets (minimal locking)
	for _, task := range allTasks {
		tq.updateTaskWidget(task)
	}

	// Check for completed tasks to remove (separate pass)
	tq.taskMutex.Lock()
	var toRemove []string
	for id, tw := range tq.taskWidgets {
		if tw.task.Status == tasks.StatusCompleted || tw.task.Status == tasks.StatusCanceled {
			// Remove completed tasks after 5 seconds
			if time.Since(tw.lastUpdate) > 5*time.Second {
				log.Printf("[DEBUG] TaskQueue.refreshTasks: Marking for removal: %s", id)
				toRemove = append(toRemove, id)
			}
		}
	}

	// Remove from map
	for _, id := range toRemove {
		delete(tq.taskWidgets, id)
	}
	tq.taskMutex.Unlock()

	// Now rebuild UI (outside of lock)
	if len(toRemove) > 0 {
		log.Printf("[DEBUG] TaskQueue.refreshTasks: Removing %d tasks", len(toRemove))
		tq.rebuildTaskList()
	}

	log.Printf("[DEBUG] TaskQueue.refreshTasks: END")
}

// rebuildTaskList rebuilds the task list UI
func (tq *TaskQueue) rebuildTaskList() {
	// Check if already rebuilding to prevent concurrent rebuilds
	tq.taskMutex.Lock()
	if tq.isRebuilding {
		log.Printf("[DEBUG] TaskQueue.rebuildTaskList: Already rebuilding, skipping")
		tq.taskMutex.Unlock()
		return
	}
	tq.isRebuilding = true
	tq.taskMutex.Unlock()

	log.Printf("[DEBUG] TaskQueue.rebuildTaskList: START")

	// Use fyne.Do to ensure all UI operations happen on main thread atomically
	fyne.Do(func() {
		// Get all widgets (with read lock)
		tq.taskMutex.RLock()
		widgets := make([]*TaskWidget, 0, len(tq.taskWidgets))
		for _, tw := range tq.taskWidgets {
			widgets = append(widgets, tw)
		}
		tq.taskMutex.RUnlock()

		// Clear container and reset widget container references
		tq.container.Objects = nil
		for _, tw := range widgets {
			tw.container = nil // Reset so addTaskToUI can re-add
		}

		// Rebuild UI
		for _, tw := range widgets {
			tw.container = nil // Ensure it's reset
			// Create task row directly without calling addTaskToUI to avoid duplicate checks
			buttonBar := container.NewHBox(tw.pauseBtn, tw.resumeBtn, tw.retryBtn, tw.cancelBtn)
			taskRow := container.NewVBox(
				container.NewBorder(nil, nil, tw.fileLabel, buttonBar, tw.statusLabel),
				tw.progressBar,
				widget.NewSeparator(),
			)
			tw.container = taskRow
			tq.container.Add(taskRow)
		}

		log.Printf("[DEBUG] TaskQueue.rebuildTaskList: Calling container.Refresh()")
		tq.container.Refresh()
		log.Printf("[DEBUG] TaskQueue.rebuildTaskList: END")

		// Clear rebuilding flag
		tq.taskMutex.Lock()
		tq.isRebuilding = false
		tq.taskMutex.Unlock()
	})
}

// updateTaskWidget creates or updates a task widget
func (tq *TaskQueue) updateTaskWidget(task *tasks.Task) {
	// First, check if widget exists (with read lock for performance)
	tq.taskMutex.RLock()
	tw, exists := tq.taskWidgets[task.ID]
	isRebuilding := tq.isRebuilding
	tq.taskMutex.RUnlock()

	// Skip adding new tasks during rebuild to prevent race conditions
	if !exists && isRebuilding {
		log.Printf("[DEBUG] TaskQueue.updateTaskWidget: Skipping new task %s during rebuild", task.ID)
		return
	}

	if !exists {
		// Need write lock to create new widget
		tq.taskMutex.Lock()
		// Double-check after acquiring write lock
		tw, exists = tq.taskWidgets[task.ID]
		if !exists {
			tw = tq.createTaskWidget(task)
			tq.taskWidgets[task.ID] = tw
			tq.taskMutex.Unlock()
			// Add to UI
			fyne.Do(func() {
				tq.addTaskToUI(task.ID, tw)
			})
			return
		}
		tq.taskMutex.Unlock()
	}

	// Update widget content
	if tw != nil {
		fyne.Do(func() {
			tw.update(task)
		})
	}
}

// createTaskWidget creates a new task widget
func (tq *TaskQueue) createTaskWidget(task *tasks.Task) *TaskWidget {
	// Create a more descriptive label with task type and info
	taskType := tq.getTaskTypeString(task)
	fileLabel := widget.NewLabel(taskType + " - " + tq.getTaskTarget(task))

	tw := &TaskWidget{
		task:         task,
		fileLabel:    fileLabel,
		statusLabel:  widget.NewLabel("Pending"),
		progressBar:  widget.NewProgressBar(),
		retryBtn:     widget.NewButtonWithIcon("", theme.ViewRefreshIcon(), nil),
		pauseBtn:     widget.NewButton("Pause", nil),
		resumeBtn:    widget.NewButton("Resume", nil),
		cancelBtn:    widget.NewButtonWithIcon("", theme.CancelIcon(), nil),
		manualCancel: false,
		stopTicker:   make(chan struct{}),
	}

	// Setup button handlers
	tw.retryBtn.OnTapped = func() {
		tq.retryTask(task.ID)
	}
	tw.retryBtn.Hide()

	tw.pauseBtn.OnTapped = func() {
		tw.manualCancel = true
		tq.cancelTask(task.ID)
	}

	tw.resumeBtn.OnTapped = func() {
		tw.manualCancel = false
		tq.retryTask(task.ID)
	}
	tw.resumeBtn.Hide()

	tw.cancelBtn.OnTapped = func() {
		tw.manualCancel = true
		tq.cancelTask(task.ID)
	}
	// Cancel button should be enabled initially
	tw.cancelBtn.Enable()

	return tw
}

// getTaskTypeString returns a human-readable task type string
func (tq *TaskQueue) getTaskTypeString(task *tasks.Task) string {
	switch task.Type {
	case tasks.TaskTypeDownload:
		return "⬇ Download"
	case tasks.TaskTypeUpload:
		return "⬆ Upload"
	case tasks.TaskTypeCompress:
		return "📦 Compress"
	default:
		return "Task"
	}
}

// getTaskTarget returns the target file/folder name for the task
func (tq *TaskQueue) getTaskTarget(task *tasks.Task) string {
	// Extract filename from path for display
	target := task.LocalPath
	if target == "" && task.RemotePath != "" {
		target = task.RemotePath
	}

	// Get just the filename/folder name
	if len(target) > 0 {
		lastSlash := -1
		for i := len(target) - 1; i >= 0; i-- {
			if target[i] == '/' || target[i] == '\\' {
				lastSlash = i
				break
			}
		}
		if lastSlash >= 0 {
			target = target[lastSlash+1:]
		}
	}

	// Truncate if too long
	if len(target) > 30 {
		target = target[:27] + "..."
	}

	return target
}

// update updates the task widget content
func (tw *TaskWidget) update(task *tasks.Task) {
	tw.task = task
	tw.lastUpdate = time.Now()

	// Update status label
	var statusText string
	switch task.Status {
	case tasks.StatusPending:
		statusText = "Pending"
		tw.pauseBtn.Enable()
		tw.cancelBtn.Enable()
		tw.retryBtn.Hide()
		tw.resumeBtn.Hide()
	case tasks.StatusRunning:
		if task.Type == tasks.TaskTypeUpload {
			statusText = fmt.Sprintf("Uploading: %.2f MB/s", task.Speed)
		} else {
			statusText = fmt.Sprintf("Downloading: %.2f MB/s", task.Speed)
		}
		tw.pauseBtn.Show()
		tw.cancelBtn.Enable()
		tw.retryBtn.Hide()
		tw.resumeBtn.Hide()
	case tasks.StatusPaused:
		statusText = "Paused"
		tw.pauseBtn.Hide()
		tw.retryBtn.Hide()
		tw.resumeBtn.Show()
		tw.cancelBtn.Disable()
	case tasks.StatusCompleted:
		statusText = "Completed ✔"
		tw.pauseBtn.Hide()
		tw.retryBtn.Hide()
		tw.resumeBtn.Hide()
		tw.cancelBtn.Disable()
	case tasks.StatusFailed:
		statusText = fmt.Sprintf("Failed: %v", task.Error)
		tw.pauseBtn.Hide()
		tw.retryBtn.Show()
		tw.resumeBtn.Hide()
		tw.cancelBtn.Disable()
	case tasks.StatusCanceled:
		statusText = "Canceled"
		tw.pauseBtn.Hide()
		tw.retryBtn.Hide()
		tw.resumeBtn.Hide()
		tw.cancelBtn.Disable()
	}

	tw.statusLabel.SetText(statusText)
	tw.progressBar.SetValue(task.Progress)
}

// addTaskToUI adds a task to the UI
func (tq *TaskQueue) addTaskToUI(taskID string, tw *TaskWidget) {
	// Check if already in container (avoid duplicates)
	if tw.container != nil {
		log.Printf("[DEBUG] TaskQueue.addTaskToUI: Task %s already has container, skipping", taskID)
		return
	}

	// Create button bar
	buttonBar := container.NewHBox(tw.pauseBtn, tw.resumeBtn, tw.retryBtn, tw.cancelBtn)

	// Create task row
	taskRow := container.NewVBox(
		container.NewBorder(nil, nil, tw.fileLabel, buttonBar, tw.statusLabel),
		tw.progressBar,
		widget.NewSeparator(),
	)

	// Store reference to prevent duplicate adds
	tw.container = taskRow

	fyne.Do(func() {
		tq.container.Add(taskRow)
		tq.container.Refresh() // Refresh to show new task immediately
	})
	log.Printf("[DEBUG] TaskQueue.addTaskToUI: Added task %s to UI", taskID)
}

// retryTask retries a failed task
func (tq *TaskQueue) retryTask(taskID string) {
	// This would need to be implemented in the task manager
	// For now, just remove the task widget
	tq.taskMutex.Lock()
	delete(tq.taskWidgets, taskID)
	tq.taskMutex.Unlock()
}

// cancelTask cancels a task
func (tq *TaskQueue) cancelTask(taskID string) {
	tq.taskManager.CancelTask(taskID)
}

// AddDownloadTask adds a download task to the queue
func (tq *TaskQueue) AddDownloadTask(remotePath, localPath string) error {
	task, err := tq.taskManager.AddDownloadTask(remotePath, localPath)
	if err != nil {
		return err
	}
	// Immediately add to UI
	go func() {
		tq.updateTaskWidget(task)
	}()
	return nil
}

// AddUploadTask adds an upload task to the queue
func (tq *TaskQueue) AddUploadTask(localPath, remotePath string) error {
	task, err := tq.taskManager.AddUploadTask(localPath, remotePath)
	if err != nil {
		return err
	}
	// Immediately add to UI
	go func() {
		tq.updateTaskWidget(task)
	}()
	return nil
}

// AddUploadFolderTask adds a folder upload task (for pack transfer mode)
func (tq *TaskQueue) AddUploadFolderTask(localPath, remotePath string) error {
	task, err := tq.taskManager.AddUploadFolderTask(localPath, remotePath)
	if err != nil {
		return err
	}
	// Immediately add to UI
	go func() {
		tq.updateTaskWidget(task)
	}()
	return nil
}

// AddCompressTask adds a compress task to the queue
func (tq *TaskQueue) AddCompressTask(paths []string, outputPath, format string) error {
	task, err := tq.taskManager.AddCompressTask(paths, outputPath, format)
	if err != nil {
		return err
	}
	// Immediately add to UI
	go func() {
		tq.updateTaskWidget(task)
	}()
	return nil
}

// ShowDownloadDialog shows a file download dialog
func (tq *TaskQueue) ShowDownloadDialog(remotePath string) {
	fileName := remotePath
	if len(remotePath) > 1 {
		fileName = remotePath[1:] // Remove leading slash
	}

	dialog.ShowFileOpen(func(reader fyne.URIReadCloser, err error) {
		if err != nil || reader == nil {
			return
		}
		defer reader.Close()

		// Get the directory path
		saveDir := reader.URI().Path()
		localPath := saveDir + "/" + fileName

		// Add download task
		if err := tq.AddDownloadTask(remotePath, localPath); err != nil {
			dialog.ShowError(err, tq.mainWindow.window)
		}
	}, tq.mainWindow.window)
}

// ShowUploadDialog shows a file upload dialog
func (tq *TaskQueue) ShowUploadDialog(localDir string) {
	dialog.ShowFileOpen(func(reader fyne.URIReadCloser, err error) {
		if err != nil || reader == nil {
			return
		}
		defer reader.Close()

		// Get the selected file path
		localPath := reader.URI().Path()
		fileName := reader.URI().Name()

		// Ask for remote path
		entry := widget.NewEntry()
		entry.SetText("/" + fileName)
		entry.SetPlaceHolder("/remote/path/to/file")

		content := container.NewVBox(
			widget.NewLabel("Enter remote path:"),
			entry,
		)

		dialog.ShowCustomConfirm("Upload File", "Upload", "Cancel", content, func(confirmed bool) {
			if confirmed {
				remotePath := entry.Text
				if err := tq.AddUploadTask(localPath, remotePath); err != nil {
					dialog.ShowError(err, tq.mainWindow.window)
				}
			}
		}, tq.mainWindow.window)
	}, tq.mainWindow.window)
}

// ShowUploadFolderDialog shows a folder upload dialog
func (tq *TaskQueue) ShowUploadFolderDialog(localDir string) {
	dialog.ShowFolderOpen(func(uri fyne.ListableURI, err error) {
		if err != nil || uri == nil {
			return
		}

		// Get the selected folder path
		localPath := uri.Path()
		folderName := uri.Name()

		// Ask for remote path
		entry := widget.NewEntry()
		entry.SetText("/" + folderName)
		entry.SetPlaceHolder("/remote/path/to/folder")

		content := container.NewVBox(
			widget.NewLabel("Enter remote folder path:"),
			entry,
		)

		dialog.ShowCustomConfirm("Upload Folder", "Upload", "Cancel", content, func(confirmed bool) {
			if !confirmed {
				return
			}

			remotePath := entry.Text
			tq.uploadFolder(localPath, remotePath)
		}, tq.mainWindow.window)
	}, tq.mainWindow.window)
}

// uploadFolder uploads a folder recursively
func (tq *TaskQueue) uploadFolder(localPath, remotePath string) {
	// Check if pack transfer is enabled - if so, upload folder as a single packed task
	if tq.mainWindow.packTransferConfig.Enabled {
		tq.uploadFolderPacked(localPath, remotePath)
		return
	}

	// Pack transfer disabled - upload files individually
	// Walk directory to get all files
	var filesToUpload []struct {
		local  string
		remote string
	}
	var totalSize int64

	err := filepath.Walk(localPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() {
			// Calculate relative path (OS-specific)
			relPath, err := filepath.Rel(localPath, path)
			if err != nil {
				return err
			}

			// Convert to forward slashes for server (Unix-style paths)
			relPath = filepath.ToSlash(relPath)

			// Build remote path preserving directory structure with forward slashes
			// Ensure remotePath ends with / for proper joining
			remoteBase := remotePath
			if !strings.HasSuffix(remoteBase, "/") {
				remoteBase += "/"
			}
			remoteFilePath := remoteBase + relPath

			filesToUpload = append(filesToUpload, struct {
				local  string
				remote string
			}{
				local:  path,
				remote: remoteFilePath,
			})
			totalSize += info.Size()
		}
		return nil
	})

	if err != nil {
		dialog.ShowError(err, tq.mainWindow.window)
		return
	}

	if len(filesToUpload) == 0 {
		dialog.ShowInformation("Empty Folder", "The selected folder is empty", tq.mainWindow.window)
		return
	}

	// Show confirmation
	sizeStr := formatSize(totalSize)
	message := fmt.Sprintf("Upload %d file(s) (%s) to:\n%s", len(filesToUpload), sizeStr, remotePath)

	dialog.ShowConfirm("Upload Folder", message, func(confirmed bool) {
		if !confirmed {
			return
		}

		// Queue all files for upload
		for _, file := range filesToUpload {
			// Create remote directory (use path.Dir for Unix-style remote paths)
			remoteDir := path.Dir(file.remote)
			if err := tq.mainWindow.client.CreateDirectory(remoteDir); err != nil {
				log.Printf("[DEBUG] Failed to create remote directory %s: %v", remoteDir, err)
			}

			if err := tq.AddUploadTask(file.local, file.remote); err != nil {
				log.Printf("[DEBUG] Error queueing upload: %v", err)
			}
		}

		dialog.ShowInformation("Upload Started",
			fmt.Sprintf("Uploading %d file(s) to %s", len(filesToUpload), remotePath),
			tq.mainWindow.window)
	}, tq.mainWindow.window)
}

// uploadFolderPacked uploads a folder as a single packed task (tar.gz compression)
func (tq *TaskQueue) uploadFolderPacked(localPath, remotePath string) {
	// Calculate total folder size for display
	var totalSize int64
	var fileCount int

	err := filepath.Walk(localPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() {
			totalSize += info.Size()
			fileCount++
		}
		return nil
	})

	if err != nil {
		dialog.ShowError(err, tq.mainWindow.window)
		return
	}

	if fileCount == 0 {
		dialog.ShowInformation("Empty Folder", "The selected folder is empty", tq.mainWindow.window)
		return
	}

	// Show confirmation
	folderName := filepath.Base(localPath)
	sizeStr := formatSize(totalSize)
	message := fmt.Sprintf("Upload folder '%s' (%d files, %s) to:\n%s\n\n(Pack transfer enabled: folder will be compressed before upload)",
		folderName, fileCount, sizeStr, remotePath)

	dialog.ShowConfirm("Upload Folder (Packed)", message, func(confirmed bool) {
		if !confirmed {
			return
		}

		// Add single upload task for the entire folder
		// The UploadFilePacked function will handle compression
		if err := tq.AddUploadFolderTask(localPath, remotePath); err != nil {
			log.Printf("[DEBUG] Error queueing folder upload: %v", err)
			dialog.ShowError(err, tq.mainWindow.window)
			return
		}

		dialog.ShowInformation("Upload Started",
			fmt.Sprintf("Uploading folder '%s' (packed) to %s", folderName, remotePath),
			tq.mainWindow.window)
	}, tq.mainWindow.window)
}
