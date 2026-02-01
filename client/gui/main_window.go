package gui

import (
	"fmt"
	"image/color"
	"log"
	"path"
	"strings"
	"sync"
	"time"

	kcpclient "certstone.cc/simpleKcpFileManager/kcpclient"
	"certstone.cc/simpleKcpFileManager/kcpclient/tasks"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

// EventInterceptLayer is a transparent layer that intercepts secondary tap (right-click)
// and double tap events. It sits on TOP of the content in a Stack layout.
// This approach ensures our event handlers are found by Fyne's event system
// because we're a separate widget in the visual tree, not wrapping content.
type EventInterceptLayer struct {
	widget.BaseWidget
	mainWindow   *MainWindow
	onRightClick func(fyne.Position) // Called when right-clicking empty area
}

// NewEventInterceptLayer creates a new event intercept layer
func NewEventInterceptLayer(mainWindow *MainWindow, onRightClick func(fyne.Position)) *EventInterceptLayer {
	layer := &EventInterceptLayer{
		mainWindow:   mainWindow,
		onRightClick: onRightClick,
	}
	layer.ExtendBaseWidget(layer)
	return layer
}

// CreateRenderer creates an empty renderer - this is a transparent overlay
func (l *EventInterceptLayer) CreateRenderer() fyne.WidgetRenderer {
	// Use a transparent rectangle that fills the space
	// This ensures the layer has a size and can receive events
	rect := canvas.NewRectangle(color.Transparent)
	return widget.NewSimpleRenderer(rect)
}

// MinSize returns a small minimum size - actual size is controlled by container
func (l *EventInterceptLayer) MinSize() fyne.Size {
	return fyne.NewSize(1, 1)
}

// Tapped handles single click - select the item in the list
// We need to implement this to intercept the tap event and forward selection to the list
func (l *EventInterceptLayer) Tapped(e *fyne.PointEvent) {
	log.Printf("[DEBUG] EventInterceptLayer.Tapped called at Pos=%v", e.Position)

	clickedIndex := l.getClickedIndex(e.Position)
	if clickedIndex >= 0 && clickedIndex < len(l.mainWindow.serverFiles) {
		log.Printf("[DEBUG] EventInterceptLayer.Tapped: Selecting index %d", clickedIndex)
		l.mainWindow.fileList.Select(widget.ListItemID(clickedIndex))
	}
}

// getClickedIndex calculates which list item was clicked based on local position
func (l *EventInterceptLayer) getClickedIndex(localPos fyne.Position) int {
	if l.mainWindow == nil || l.mainWindow.fileList == nil {
		return -1
	}

	fileCount := len(l.mainWindow.serverFiles)
	if fileCount == 0 {
		return -1
	}

	// Use local Y within the overlay (same size as scroll viewport)
	// Then add scroll offset to map to content coordinates
	scrollOffsetY := float32(0)
	if l.mainWindow.fileListScroll != nil {
		scrollOffsetY = l.mainWindow.fileListScroll.Offset.Y
	}
	adjustedY := localPos.Y + scrollOffsetY

	log.Printf("[DEBUG] EventInterceptLayer.getClickedIndex: localY=%v, scrollOffsetY=%v, adjustedY=%v",
		localPos.Y, scrollOffsetY, adjustedY)

	// Fyne's default list item height
	const estimatedRowHeight = float32(37)

	if adjustedY >= 0 {
		clickedIndex := int(adjustedY / estimatedRowHeight)
		log.Printf("[DEBUG] EventInterceptLayer.getClickedIndex: estimated clickedIndex=%d (fileCount=%d)", clickedIndex, fileCount)
		return clickedIndex
	}
	return -1
}

// TappedSecondary handles right-click on the layer
// It determines whether the click is on a list item or empty area
func (l *EventInterceptLayer) TappedSecondary(e *fyne.PointEvent) {
	log.Printf("[DEBUG] EventInterceptLayer.TappedSecondary called at Pos=%v, AbsolutePos=%v", e.Position, e.AbsolutePosition)

	fileCount := len(l.mainWindow.serverFiles)
	clickedIndex := l.getClickedIndex(e.Position)

	log.Printf("[DEBUG] EventInterceptLayer.TappedSecondary: clickedIndex=%d, fileCount=%d", clickedIndex, fileCount)

	if clickedIndex >= 0 && clickedIndex < fileCount {
		// Clicked on a valid file item
		file := l.mainWindow.serverFiles[clickedIndex]
		fileCopy := file
		l.mainWindow.selectedFile = &fileCopy
		l.mainWindow.fileList.Select(widget.ListItemID(clickedIndex))
		contextMenu := NewContextMenu(l.mainWindow)
		contextMenu.ShowFileListMenu(&fileCopy, e.AbsolutePosition)
		return
	}

	// Clicked on empty area (below last item or outside)
	if l.onRightClick != nil {
		l.onRightClick(e.AbsolutePosition)
	}
}

// DoubleTapped handles double-click to enter folders
func (l *EventInterceptLayer) DoubleTapped(e *fyne.PointEvent) {
	log.Printf("[DEBUG] EventInterceptLayer.DoubleTapped called at Pos=%v", e.Position)

	fileCount := len(l.mainWindow.serverFiles)
	clickedIndex := l.getClickedIndex(e.Position)

	log.Printf("[DEBUG] EventInterceptLayer.DoubleTapped: clickedIndex=%d, fileCount=%d", clickedIndex, fileCount)

	if clickedIndex >= 0 && clickedIndex < fileCount {
		file := l.mainWindow.serverFiles[clickedIndex]

		// Select the item first
		l.mainWindow.fileList.Select(widget.ListItemID(clickedIndex))

		if file.IsDir {
			// Navigate into folder
			cleanPath := strings.TrimPrefix(file.Path, "/")
			log.Printf("[DEBUG] DoubleTapped: Navigating to folder: %s", cleanPath)
			l.mainWindow.navigateToPath(cleanPath)
		} else {
			// Open file for editing (for text files)
			log.Printf("[DEBUG] DoubleTapped: Opening file: %s", file.Name)
			contextMenu := NewContextMenu(l.mainWindow)
			contextMenu.editFile(&file)
		}
	}
}

// MainWindow represents the main application window
type MainWindow struct {
	app                 fyne.App
	window              fyne.Window
	client              *kcpclient.Client
	serverAddr          string
	encryptionKey       string
	taskManager         *tasks.Manager
	taskQueue           *TaskQueue
	currentPath         string
	serverFiles         []kcpclient.ListItem
	serverFilesOriginal []kcpclient.ListItem // Keep original order
	selectedFile        *kcpclient.ListItem
	selectedIndex       int // Track selected index for visual feedback
	saveDir             string
	packTransferConfig  kcpclient.PackTransferConfig // Pack transfer settings
	uiMutex             sync.Mutex
	doubleTapMutex      sync.Mutex // Protects double-tap detection state
	lastTapTime         int64
	lastTapID           widget.ListItemID
	refreshFunc         func()
	fileList            *widget.List
	fileListScroll      *container.Scroll // Store scroll reference for position calculation
	taskList            *fyne.Container
	pathContainer       *fyne.Container // New: breadcrumb navigation
	statusLabel         *widget.Label
	infoLabel           *widget.Label
	directoryTree       *DirectoryTree
	sortButtons         map[string]*widget.Button
	sortColumn          string // "name", "size", "time", "mode"
	sortAscending       bool
}

// MainWindowConfig holds configuration for the main window
type MainWindowConfig struct {
	App           fyne.App
	ServerAddr    string
	EncryptionKey string
	SaveDir       string
}

// NewMainWindow creates a new main window
func NewMainWindow(config MainWindowConfig) *MainWindow {
	log.Printf("[DEBUG] NewMainWindow: Creating window")
	window := config.App.NewWindow("KCP File Manager")
	window.Resize(fyne.NewSize(1500, 800))
	window.CenterOnScreen()

	// Add window event listeners for debugging
	window.SetCloseIntercept(func() {
		log.Printf("[DEBUG] Window: CloseIntercept called")
		window.Close()
	})

	// Create client and task manager
	kcpClient := kcpclient.NewClient(config.ServerAddr, config.EncryptionKey)
	taskManager := tasks.NewManager(kcpClient, 3, kcpclient.DefaultPackTransferConfig())

	mw := &MainWindow{
		app:                config.App,
		window:             window,
		client:             kcpClient,
		serverAddr:         config.ServerAddr,
		encryptionKey:      config.EncryptionKey,
		taskManager:        taskManager,
		currentPath:        "",
		saveDir:            config.SaveDir,
		packTransferConfig: kcpclient.DefaultPackTransferConfig(),
	}

	log.Printf("[DEBUG] NewMainWindow: Creating task queue")
	// Create task queue first
	mw.taskQueue = NewTaskQueue(mw)

	log.Printf("[DEBUG] NewMainWindow: Setting up UI")
	mw.setupUI()
	log.Printf("[DEBUG] NewMainWindow: UI setup complete")

	// Setup drag and drop for file uploads
	dragDropHandler := NewDragDropHandler(mw)
	dragDropHandler.SetupWindowDragDrop()
	log.Printf("[DEBUG] NewMainWindow: Drag-drop setup complete")

	// Store connection config for later use
	if config.ServerAddr != "" && config.EncryptionKey != "" {
		log.Printf("[DEBUG] NewMainWindow: Will auto-connect after window is shown")
		// Schedule connection after window is shown and event loop is running
		mw.window.Content() // Force content creation
		mw.serverAddr = config.ServerAddr
		mw.encryptionKey = config.EncryptionKey
	}

	log.Printf("[DEBUG] NewMainWindow: Created successfully")
	return mw
}

// NewMainWindowWithWindow creates a new main window using an existing window
func NewMainWindowWithWindow(config MainWindowConfig, window fyne.Window) *MainWindow {
	log.Printf("[DEBUG] NewMainWindowWithWindow: Creating window")
	window.SetTitle("KCP File Manager")
	window.Resize(fyne.NewSize(1500, 800))
	window.CenterOnScreen()

	// Add window event listeners for debugging
	window.SetCloseIntercept(func() {
		log.Printf("[DEBUG] Window: CloseIntercept called")
		window.Close()
	})

	// Create client and task manager
	kcpClient := kcpclient.NewClient(config.ServerAddr, config.EncryptionKey)
	taskManager := tasks.NewManager(kcpClient, 3, kcpclient.DefaultPackTransferConfig())

	mw := &MainWindow{
		app:                config.App,
		window:             window,
		client:             kcpClient,
		serverAddr:         config.ServerAddr,
		encryptionKey:      config.EncryptionKey,
		taskManager:        taskManager,
		currentPath:        "",
		saveDir:            config.SaveDir,
		packTransferConfig: kcpclient.DefaultPackTransferConfig(),
	}

	log.Printf("[DEBUG] NewMainWindowWithWindow: Creating task queue")
	// Create task queue first
	mw.taskQueue = NewTaskQueue(mw)

	log.Printf("[DEBUG] NewMainWindowWithWindow: Setting up UI")
	mw.setupUI()
	log.Printf("[DEBUG] NewMainWindowWithWindow: UI setup complete")

	// Setup drag and drop for file uploads
	dragDropHandler := NewDragDropHandler(mw)
	dragDropHandler.SetupWindowDragDrop()
	log.Printf("[DEBUG] NewMainWindowWithWindow: Drag-drop setup complete")

	// Store connection config for later use
	if config.ServerAddr != "" && config.EncryptionKey != "" {
		log.Printf("[DEBUG] NewMainWindowWithWindow: Will auto-connect after window is shown")
		// Schedule connection after window is shown and event loop is running
		mw.window.Content() // Force content creation
		mw.serverAddr = config.ServerAddr
		mw.encryptionKey = config.EncryptionKey
	}

	log.Printf("[DEBUG] NewMainWindowWithWindow: Created successfully")
	return mw
}

// Show displays the main window (for use when window is already shown)
func (mw *MainWindow) Show() {
	log.Printf("[DEBUG] MainWindow.Show: Starting")

	// If we have connection credentials, connect after window is shown
	if mw.serverAddr != "" && mw.encryptionKey != "" {
		log.Printf("[DEBUG] MainWindow.Show: Scheduling connection after window display")
		// Start connection in background after a short delay
		go func() {
			time.Sleep(100 * time.Millisecond)
			log.Printf("[DEBUG] MainWindow.Show: Triggering delayed connection")
			mw.connectToServer()
		}()
	}

	// Don't call ShowAndRun() here - the window is already being shown by the caller
	log.Printf("[DEBUG] MainWindow.Show: Complete")
}

// ShowAndRun displays the main window and starts the event loop
func (mw *MainWindow) ShowAndRun() {
	log.Printf("[DEBUG] MainWindow.ShowAndRun: Starting")

	// If we have connection credentials, connect after window is shown
	if mw.serverAddr != "" && mw.encryptionKey != "" {
		log.Printf("[DEBUG] MainWindow.ShowAndRun: Scheduling connection after window display")
		// Start connection in background after a short delay
		go func() {
			time.Sleep(100 * time.Millisecond)
			log.Printf("[DEBUG] MainWindow.ShowAndRun: Triggering delayed connection")
			mw.connectToServer()
		}()
	}

	mw.window.ShowAndRun()
	log.Printf("[DEBUG] MainWindow.ShowAndRun: ShowAndRun returned (this should only happen on window close)")
}

// setupUI sets up the user interface
func (mw *MainWindow) setupUI() {
	// Directory tree (left panel)
	mw.directoryTree = NewDirectoryTree(mw)
	treeLabel := widget.NewLabel("Directories")
	treeToolbar := mw.directoryTree.GetToolbar()

	treeScroll := container.NewScroll(mw.directoryTree.GetWidget())
	treeContainer := container.NewBorder(
		container.NewVBox(treeLabel, treeToolbar),
		nil, nil, nil,
		treeScroll,
	)

	// File list (center panel)
	mw.infoLabel = widget.NewLabel("Select a file or folder")
	mw.statusLabel = widget.NewLabel("Not connected")

	mw.fileList = mw.createFileList()

	// Navigation toolbar
	navToolbar := mw.createNavToolbar()

	// Create sort buttons row
	sortToolbar := mw.createSortToolbar()

	// File list with scroll - will be center of Border layout
	fileListScroll := container.NewScroll(mw.fileList)

	// Store scroll reference for right-click position calculation
	mw.fileListScroll = fileListScroll

	// Create an event intercept layer that sits on TOP of the scroll area
	// This layer intercepts right-click and double-click events
	// It's placed on top in a Stack layout so events reach it first
	eventLayer := NewEventInterceptLayer(mw, func(pos fyne.Position) {
		log.Printf("[DEBUG] EventInterceptLayer: right-click in empty area")
		contextMenu := NewContextMenu(mw)
		contextMenu.ShowBackgroundMenu(pos)
	})

	// Stack layout: fileListScroll at bottom, eventLayer on top
	// Events first hit eventLayer for right-click/double-click handling
	// Left clicks pass through to fileListScroll (because eventLayer doesn't implement Tappable)
	fileListWrapper := container.NewStack(fileListScroll, eventLayer)

	// Create breadcrumb navigation container (will be updated dynamically)
	pathContainer := container.NewHBox()
	mw.pathContainer = pathContainer

	// Create header container with fixed height controls
	headerContainer := container.NewVBox(
		navToolbar,
		pathContainer,
		sortToolbar,
		widget.NewSeparator(),
	)

	// Create footer container with status info
	footerContainer := container.NewVBox(
		widget.NewSeparator(),
		mw.infoLabel,
		mw.statusLabel,
	)

	// Use Border layout: header fixed at top, footer fixed at bottom, list fills remaining space
	// This ensures the file list takes up all available vertical space
	fileListContainer := container.NewBorder(
		headerContainer, // top - fixed height
		footerContainer, // bottom - fixed height
		nil,             // left - none
		nil,             // right - none
		fileListWrapper, // center - fills remaining space (with right-click support)
	)

	// Task queue (right panel)
	taskLabel := widget.NewLabel("Tasks")
	taskScroll := container.NewScroll(mw.taskQueue.GetContainer())
	taskContainer := container.NewBorder(taskLabel, nil, nil, nil, taskScroll)

	// Create main content with proper sizing
	leftSplit := container.NewHSplit(treeContainer, fileListContainer)
	leftSplit.SetOffset(0.25)

	rightSplit := container.NewHSplit(leftSplit, taskContainer)
	rightSplit.SetOffset(0.75)

	// Use Border layout to fill the entire window
	mainContent := container.NewBorder(
		nil,        // top
		nil,        // bottom
		nil,        // left
		nil,        // right
		rightSplit, // center
	)

	mw.window.SetContent(mainContent)
}

// createFileList creates the file list widget
func (mw *MainWindow) createFileList() *widget.List {
	fileList := widget.NewList(
		func() int {
			return len(mw.serverFiles)
		},
		func() fyne.CanvasObject {
			// Use simple container - let widget.List handle selection
			icon := widget.NewIcon(nil)
			nameLabel := widget.NewLabel("")
			sizeLabel := widget.NewLabel("")
			modeLabel := widget.NewLabel("")
			dateLabel := widget.NewLabel("")
			return container.NewHBox(icon, nameLabel, sizeLabel, modeLabel, dateLabel)
		},
		func(i widget.ListItemID, o fyne.CanvasObject) {
			if i >= len(mw.serverFiles) {
				return
			}

			row := o.(*fyne.Container)
			icon := row.Objects[0].(*widget.Icon)
			nameLabel := row.Objects[1].(*widget.Label)
			sizeLabel := row.Objects[2].(*widget.Label)
			modeLabel := row.Objects[3].(*widget.Label)
			dateLabel := row.Objects[4].(*widget.Label)

			file := mw.serverFiles[i]

			if file.IsDir {
				icon.SetResource(theme.FolderIcon())
			} else {
				icon.SetResource(theme.FileIcon())
			}

			nameLabel.SetText(file.Name)
			sizeLabel.SetText(formatSize(file.Size))
			modeLabel.SetText(formatMode(file.Mode))
			dateLabel.SetText(formatTime(file.ModTime))
		},
	)

	// Store the selected index for visual feedback
	mw.selectedIndex = -1

	// Add selection handler - single click only selects the item
	fileList.OnSelected = func(id widget.ListItemID) {
		log.Printf("[DEBUG] FileList.OnSelected: id=%d", id)
		if id >= 0 && id < widget.ListItemID(len(mw.serverFiles)) {
			// Copy the file info to avoid pointer issues when slice is refreshed
			fileCopy := mw.serverFiles[id]
			mw.selectedFile = &fileCopy
			mw.selectedIndex = int(id)
			mw.updateInfoLabel(mw.selectedFile)
			log.Printf("[DEBUG] FileList.OnSelected: Selected file=%s", mw.selectedFile.Name)
		}
	}

	// Add double-tap handler for navigating into folders
	fileList.OnUnselected = func(id widget.ListItemID) {
		log.Printf("[DEBUG] FileList.OnUnselected: id=%d", id)
		// Don't clear selection here - keep the visual selection
	}

	mw.fileList = fileList
	mw.setupFileListDoubleTap()
	return fileList
}

// setupFileListDoubleTap sets up double-tap detection for file list
// Note: Double-tap is now handled by RightClickOverlay.DoubleTapped()
// This function is kept for compatibility but does minimal work
func (mw *MainWindow) setupFileListDoubleTap() {
	// No longer need to override OnSelected for double-tap detection
	// RightClickOverlay handles double-tap via DoubleTapped() method
}

// updateInfoLabel updates the info label based on selected file
func (mw *MainWindow) updateInfoLabel(file *kcpclient.ListItem) {
	if file == nil {
		mw.infoLabel.SetText("Select a file or folder")
		return
	}

	mt := formatTime(file.ModTime)
	kind := "File"
	if file.IsDir {
		kind = "Folder"
	}
	mw.infoLabel.SetText(fmt.Sprintf("%s | %s | %s", kind, formatSize(file.Size), mt))
}

// createNavToolbar creates the navigation toolbar
func (mw *MainWindow) createNavToolbar() *fyne.Container {
	contextMenu := NewContextMenu(mw)

	upBtn := widget.NewButtonWithIcon("", theme.MoveUpIcon(), func() {
		mw.navigateUp()
	})

	refreshBtn := widget.NewButtonWithIcon("", theme.ViewRefreshIcon(), func() {
		mw.directoryTree.Refresh()
		mw.refreshFileList()
	})

	homeBtn := widget.NewButtonWithIcon("", theme.HomeIcon(), func() {
		mw.navigateToPath("")
	})

	// Action buttons for selected file
	downloadBtn := widget.NewButtonWithIcon("Download", theme.DownloadIcon(), func() {
		if mw.selectedFile != nil {
			if mw.selectedFile.IsDir {
				contextMenu.downloadFolder(mw.selectedFile)
			} else {
				contextMenu.downloadFile(mw.selectedFile)
			}
		}
	})

	uploadBtn := widget.NewButtonWithIcon("Upload", theme.UploadIcon(), func() {
		contextMenu.ShowBackgroundMenu(fyne.NewPos(0, 0)) // Will show file dialog
	})

	actionsBtn := widget.NewButtonWithIcon("Actions", theme.ContentClearIcon(), func() {
		if mw.selectedFile != nil {
			// Show context menu at center of window
			pos := fyne.NewPos(mw.window.Canvas().Size().Width/2, mw.window.Canvas().Size().Height/2)
			contextMenu.ShowFileListMenu(mw.selectedFile, pos)
		} else {
			// Show background menu
			pos := fyne.NewPos(mw.window.Canvas().Size().Width/2, mw.window.Canvas().Size().Height/2)
			contextMenu.ShowBackgroundMenu(pos)
		}
	})

	// Settings button
	settingsBtn := widget.NewButtonWithIcon("Settings", theme.SettingsIcon(), func() {
		settingsDialog := NewSettingsDialog(mw)
		settingsDialog.Show()
	})

	return container.NewHBox(homeBtn, upBtn, refreshBtn, widget.NewSeparator(), downloadBtn, uploadBtn, actionsBtn, widget.NewSeparator(), settingsBtn)
}

// createSortToolbar creates the sort toolbar with clickable column headers
func (mw *MainWindow) createSortToolbar() *fyne.Container {
	// Initialize default sort column
	mw.sortColumn = "name"
	mw.sortAscending = false // Default descending (A-Z at top)

	nameBtn := widget.NewButton("Name ▼", func() {
		mw.toggleSort("name")
	})
	sizeBtn := widget.NewButton("Size", func() {
		mw.toggleSort("size")
	})
	modeBtn := widget.NewButton("Mode", func() {
		mw.toggleSort("mode")
	})
	dateBtn := widget.NewButton("Date", func() {
		mw.toggleSort("time")
	})

	// Store buttons for updating sort indicators (use display names as keys)
	mw.sortButtons = map[string]*widget.Button{
		"name": nameBtn,
		"size": sizeBtn,
		"mode": modeBtn,
		"time": dateBtn,
	}

	return container.NewHBox(nameBtn, sizeBtn, modeBtn, dateBtn)
}

// toggleSort toggles sort order for the given column
func (mw *MainWindow) toggleSort(column string) {
	if mw.sortColumn == column {
		// Same column, reverse order
		mw.sortAscending = !mw.sortAscending
	} else {
		// New column, set ascending
		mw.sortColumn = column
		mw.sortAscending = true
	}

	mw.sortFiles()
	mw.updateSortButtons()
	mw.fileList.Refresh()
	log.Printf("[DEBUG] Sort: column=%s ascending=%v", mw.sortColumn, mw.sortAscending)
}

// sortFiles sorts the serverFiles based on current sort column
func (mw *MainWindow) sortFiles() {
	files := mw.serverFiles
	n := len(files)

	// Simple bubble sort for small datasets
	for i := 0; i < n-1; i++ {
		for j := 0; j < n-i-1; j++ {
			if mw.shouldSwap(files[j], files[j+1]) {
				files[j], files[j+1] = files[j+1], files[j]
			}
		}
	}
}

// shouldSwap returns true if two files should be swapped
func (mw *MainWindow) shouldSwap(a, b kcpclient.ListItem) bool {
	var less bool

	switch mw.sortColumn {
	case "name":
		less = a.Name < b.Name
	case "size":
		less = a.Size < b.Size
	case "time":
		less = a.ModTime < b.ModTime
	case "mode":
		less = a.Mode < b.Mode
	default:
		less = a.Name < b.Name
	}

	if mw.sortAscending {
		return !less // Want ascending order, so swap if b < a
	}
	return less
}

// updateSortButtons updates the sort indicator on buttons
func (mw *MainWindow) updateSortButtons() {
	// Map column keys to display names
	displayNames := map[string]string{
		"name": "Name",
		"size": "Size",
		"mode": "Mode",
		"time": "Date",
	}

	for col, btn := range mw.sortButtons {
		displayName := displayNames[col]
		if col == mw.sortColumn {
			if mw.sortAscending {
				btn.SetText(displayName + " ▲")
			} else {
				btn.SetText(displayName + " ▼")
			}
		} else {
			btn.SetText(displayName)
		}
	}
}

// connectToServer connects to the server
func (mw *MainWindow) connectToServer() {
	log.Printf("[DEBUG] connectToServer: Starting connection to %s", mw.serverAddr)
	fyne.Do(func() {
		mw.statusLabel.SetText("Connecting...")
	})

	go func() {
		log.Printf("[DEBUG] connectToServer: Calling client.Connect()")
		err := mw.client.Connect()
		if err != nil {
			log.Printf("[DEBUG] connectToServer: Connection failed - %v", err)
			fyne.Do(func() {
				mw.statusLabel.SetText("Connection failed")

				// Show key input dialog
				ShowKeyInputDialog(mw.window, func(newKey string) {
					// Update key and reconnect
					mw.encryptionKey = newKey

					// Create new client with new key
					newClient := kcpclient.NewClient(mw.serverAddr, newKey)

					// Update both client and taskManager
					mw.client = newClient
					mw.taskManager = tasks.NewManager(newClient, 3, mw.packTransferConfig)
					mw.taskQueue.taskManager = mw.taskManager

					// Try connecting again
					mw.connectToServer()
				})
			})
			return
		}

		log.Printf("[DEBUG] connectToServer: Connection successful")
		fyne.Do(func() {
			mw.statusLabel.SetText("Connected")
		})

		// Load directory tree first (async)
		log.Printf("[DEBUG] connectToServer: Loading directory tree")
		mw.directoryTree.LoadTree()
		log.Printf("[DEBUG] connectToServer: Directory tree load initiated")

		// Then load file list (async)
		log.Printf("[DEBUG] connectToServer: Loading file list")
		mw.refreshFileList()
		log.Printf("[DEBUG] connectToServer: File list load initiated")
	}()
}

// safeUpdateStatus safely updates the status label from any thread
func (mw *MainWindow) safeUpdateStatus(text string) {
	fyne.Do(func() {
		mw.statusLabel.SetText(text)
	})
}

// safeUpdateFileList safely updates the file list from any thread
func (mw *MainWindow) safeUpdateFileList(files []kcpclient.ListItem) {
	fyne.Do(func() {
		// Bug 4: Removed ".." parent directory - now we use directory tree and breadcrumbs for navigation
		mw.serverFiles = files
		mw.fileList.Refresh()
		mw.updatePathBreadcrumbs(mw.currentPath)
		mw.statusLabel.SetText(fmt.Sprintf("%d items", len(files)))

		// Bug 2 fix: Force refresh of entire window content to fix layout issues on initial load
		if mw.window != nil && mw.window.Canvas() != nil {
			// Refresh the entire content to recalculate layouts
			mw.window.Canvas().Content().Refresh()
		}
	})
}

// updatePathBreadcrumbs updates the breadcrumb navigation
func (mw *MainWindow) updatePathBreadcrumbs(path string) {
	// Clear existing buttons
	mw.pathContainer.Objects = nil

	// Add root button
	rootBtn := widget.NewButton("🏠 Root", func() {
		mw.navigateToPath("")
	})
	mw.pathContainer.Add(rootBtn)

	// If at root, don't add more
	if path == "" {
		mw.pathContainer.Refresh()
		return
	}

	// Split path and add buttons for each level
	parts := strings.Split(path, "/")
	builtPath := ""

	for i, part := range parts {
		if part == "" {
			continue
		}

		// Add separator
		mw.pathContainer.Add(widget.NewLabel("→"))

		// Build path to this level
		if builtPath == "" {
			builtPath = part
		} else {
			builtPath = builtPath + "/" + part
		}

		// Capture the path value for the closure
		navPath := builtPath
		displayText := part

		// Create clickable button for this level
		if i == len(parts)-1 {
			// Current level - make it bold (using different style)
			btn := widget.NewButton("📂 "+displayText, func() {
				mw.navigateToPath(navPath)
			})
			mw.pathContainer.Add(btn)
		} else {
			// Parent level - clickable
			btn := widget.NewButton(displayText, func() {
				mw.navigateToPath(navPath)
			})
			mw.pathContainer.Add(btn)
		}
	}

	mw.pathContainer.Refresh()
}

// refreshFileList refreshes the file list
func (mw *MainWindow) refreshFileList() {
	log.Printf("[DEBUG] refreshFileList: Starting, currentPath=%s", mw.currentPath)
	if mw.client == nil || !mw.client.IsConnected() {
		log.Printf("[DEBUG] refreshFileList: Not connected")
		mw.safeUpdateStatus("Not connected")
		return
	}

	fyne.Do(func() {
		mw.statusLabel.SetText("Loading...")
	})

	// Load data in background, then update UI safely
	go func() {
		log.Printf("[DEBUG] refreshFileList: Calling ListFiles")
		files, err := mw.client.ListFiles(mw.currentPath, false)
		if err != nil {
			log.Printf("[DEBUG] refreshFileList: ListFiles failed - %v", err)
			mw.safeUpdateStatus("Load failed: " + err.Error())
			return
		}

		log.Printf("[DEBUG] refreshFileList: Got %d files", len(files))
		log.Printf("[DEBUG] refreshFileList: Updating UI")
		mw.safeUpdateFileList(files)
		log.Printf("[DEBUG] refreshFileList: UI updated")
	}()
}

// navigateUp navigates to parent directory
func (mw *MainWindow) navigateUp() {
	if mw.currentPath == "" {
		return
	}
	mw.currentPath = path.Dir(mw.currentPath)
	if mw.currentPath == "." {
		mw.currentPath = ""
	}
	mw.refreshFileList()

	// Synchronize with directory tree
	if mw.directoryTree != nil {
		mw.directoryTree.UpdateCurrentPath(mw.currentPath)
	}
}

// navigateToPath navigates to a specific path
func (mw *MainWindow) navigateToPath(p string) {
	mw.currentPath = p
	mw.refreshFileList()

	// Synchronize with directory tree
	if mw.directoryTree != nil {
		mw.directoryTree.UpdateCurrentPath(p)
	}
}

// formatSize formats file size for display
func formatSize(size int64) string {
	if size < 1024 {
		return fmt.Sprintf("%d B", size)
	}
	if size < 1024*1024 {
		return fmt.Sprintf("%.1f KB", float64(size)/1024)
	}
	if size < 1024*1024*1024 {
		return fmt.Sprintf("%.1f MB", float64(size)/(1024*1024))
	}
	return fmt.Sprintf("%.2f GB", float64(size)/(1024*1024*1024))
}

// formatTime formats Unix timestamp for display
func formatTime(t int64) string {
	if t == 0 {
		return "-"
	}
	tm := time.Unix(t, 0)
	return tm.Format("2006-01-02 15:04")
}

// formatMode formats file mode/permissions for display
func formatMode(mode string) string {
	if mode == "" {
		return "rw-r--r--"
	}
	// Simplified permission display
	// mode from server should be like "rw-r--r--" or "-rw-r--r--"
	if len(mode) > 9 {
		return mode[len(mode)-9:]
	}
	return mode
}
