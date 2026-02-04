package gui

import (
	"log"
	"path/filepath"
	"strings"
	"sync"

	kcpclient "certstone.cc/simpleKcpFileManager/kcpclient"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

// DirectoryTree manages the directory tree widget
type DirectoryTree struct {
	tree          *widget.Tree
	mainWindow    *MainWindow
	treeMutex     sync.RWMutex
	treeData      map[string][]string           // parent -> children
	treeItemMap   map[string]kcpclient.ListItem // path -> item info
	expandedNodes map[string]bool
	selectedPath  string
	loadingNodes  map[string]bool // nodes being loaded
	toolbar       *fyne.Container // toolbar with refresh button
}

// NewDirectoryTree creates a new directory tree
func NewDirectoryTree(mainWindow *MainWindow) *DirectoryTree {
	dt := &DirectoryTree{
		mainWindow:    mainWindow,
		treeData:      make(map[string][]string),
		treeItemMap:   make(map[string]kcpclient.ListItem),
		expandedNodes: make(map[string]bool),
		loadingNodes:  make(map[string]bool),
	}

	dt.tree = widget.NewTree(
		dt.childIDsFunc,
		dt.hasChildrenFunc,
		dt.templateFunc,
		dt.updateFunc,
	)

	dt.tree.OnSelected = func(id widget.TreeNodeID) {
		dt.onNodeSelected(id)
	}

	dt.tree.OnBranchOpened = func(id widget.TreeNodeID) {
		dt.onBranchOpened(id)
	}

	dt.tree.OnBranchClosed = func(id widget.TreeNodeID) {
		dt.onBranchClosed(id)
	}

	// Create toolbar
	dt.createToolbar()

	return dt
}

// createToolbar creates the directory tree toolbar
func (dt *DirectoryTree) createToolbar() {
	refreshBtn := widget.NewButtonWithIcon("", theme.ViewRefreshIcon(), func() {
		dt.Refresh()
	})

	collapseAllBtn := widget.NewButtonWithIcon("", theme.ContentRemoveIcon(), func() {
		dt.collapseAll()
	})

	dt.toolbar = container.NewHBox(refreshBtn, collapseAllBtn)
}

// GetToolbar returns the toolbar widget
func (dt *DirectoryTree) GetToolbar() *fyne.Container {
	return dt.toolbar
}

// childIDsFunc returns the child IDs for a given node
func (dt *DirectoryTree) childIDsFunc(id widget.TreeNodeID) []string {
	dt.treeMutex.RLock()
	defer dt.treeMutex.RUnlock()

	pathStr := string(id)

	// Show loading indicator if loading
	if dt.loadingNodes[pathStr] {
		return []string{} // Return empty while loading - prevents special ID issues
	}

	// Root level
	if id == "" || id == "/" {
		if children, ok := dt.treeData["/"]; ok {
			return children
		}
		return []string{}
	}

	// Other levels
	if children, ok := dt.treeData[pathStr]; ok {
		return children
	}
	return []string{}
}

// hasChildrenFunc returns true if a node has children
func (dt *DirectoryTree) hasChildrenFunc(id widget.TreeNodeID) bool {
	dt.treeMutex.RLock()
	defer dt.treeMutex.RUnlock()

	pathStr := string(id)

	// Loading state always shows as having children
	if dt.loadingNodes[pathStr] {
		return true
	}

	if id == "" || id == "/" {
		return len(dt.treeData["/"]) > 0
	}

	// Check if this is a directory with children
	if item, ok := dt.treeItemMap[pathStr]; ok {
		if !item.IsDir {
			return false // Files don't have children
		}
		// Check if we've loaded children
		if children, ok := dt.treeData[pathStr]; ok {
			return len(children) > 0
		}
		// Directory but not loaded yet - assume it might have children
		return true
	}
	return false
}

// templateFunc creates the template for tree items
func (dt *DirectoryTree) templateFunc(branch bool) fyne.CanvasObject {
	icon := widget.NewIcon(nil)
	label := widget.NewLabel("")

	// Add loading indicator for loading state
	label.TextStyle = fyne.TextStyle{}

	return container.NewHBox(icon, label)
}

// updateFunc updates the content of a tree item
func (dt *DirectoryTree) updateFunc(id widget.TreeNodeID, branch bool, obj fyne.CanvasObject) {
	dt.treeMutex.RLock()
	defer dt.treeMutex.RUnlock()

	c := obj.(*fyne.Container)
	icon := c.Objects[0].(*widget.Icon)
	label := c.Objects[1].(*widget.Label)

	pathStr := string(id)

	// Handle loading state
	if dt.loadingNodes[pathStr] {
		icon.SetResource(theme.SearchIcon()) // Use search icon as loading indicator
		label.SetText("Loading...")
		label.TextStyle = fyne.TextStyle{Italic: true}
		return
	}

	// Root node
	if pathStr == "" || pathStr == "/" {
		icon.SetResource(theme.HomeIcon())
		label.SetText("Root")
		label.TextStyle = fyne.TextStyle{Bold: true}
		return
	}

	// File or directory node
	if item, ok := dt.treeItemMap[pathStr]; ok {
		label.SetText(item.Name)

		// Check if this is the current path
		currentPath := "/" + dt.mainWindow.currentPath
		if pathStr == currentPath {
			label.TextStyle = fyne.TextStyle{Bold: true}
		} else {
			label.TextStyle = fyne.TextStyle{}
		}

		if item.IsDir {
			// Check if expanded
			if dt.expandedNodes[pathStr] {
				icon.SetResource(theme.FolderOpenIcon())
			} else {
				icon.SetResource(theme.FolderIcon())
			}
		} else {
			icon.SetResource(theme.FileIcon())
		}
	} else {
		// Fallback - shouldn't happen
		label.SetText(filepath.Base(pathStr))
		icon.SetResource(theme.FolderIcon())
	}
}

// onNodeSelected handles node selection (single click)
func (dt *DirectoryTree) onNodeSelected(id widget.TreeNodeID) {
	if id == "" || id == "/" {
		dt.selectedPath = "/"
		dt.mainWindow.navigateToPath("")
		return
	}

	pathStr := string(id)
	dt.selectedPath = pathStr

	// Get item info
	dt.treeMutex.RLock()
	item, ok := dt.treeItemMap[pathStr]
	dt.treeMutex.RUnlock()

	if !ok {
		return
	}

	// Remove leading slash and navigate
	cleanPath := strings.TrimPrefix(pathStr, "/")

	if item.IsDir {
		// Navigate into directory
		dt.mainWindow.navigateToPath(cleanPath)
	} else {
		// File selected - just update info, don't navigate
		dt.mainWindow.selectedFile = &item
		dt.mainWindow.updateInfoLabel(&item)
	}
}

// onBranchOpened handles branch expansion
func (dt *DirectoryTree) onBranchOpened(id widget.TreeNodeID) {
	pathStr := string(id)

	dt.treeMutex.Lock()
	dt.expandedNodes[pathStr] = true

	// Check if already loaded
	_, alreadyLoaded := dt.treeData[pathStr]

	if !alreadyLoaded && !dt.loadingNodes[pathStr] {
		// Mark as loading
		dt.loadingNodes[pathStr] = true
		dt.treeMutex.Unlock()

		// Trigger refresh to show loading state
		fyne.Do(func() {
			dt.tree.Refresh()
		})

		// Load children
		dt.loadDirectoryChildren(pathStr)
	} else {
		dt.treeMutex.Unlock()
	}

	// Update UI to show open folder icon
	fyne.Do(func() {
		dt.tree.Refresh()
	})
}

// onBranchClosed handles branch collapse
func (dt *DirectoryTree) onBranchClosed(id widget.TreeNodeID) {
	pathStr := string(id)

	dt.treeMutex.Lock()
	dt.expandedNodes[pathStr] = false
	dt.treeMutex.Unlock()

	// Update UI to show closed folder icon
	fyne.Do(func() {
		dt.tree.Refresh()
	})
}

// loadDirectoryChildren loads children for a directory
func (dt *DirectoryTree) loadDirectoryChildren(dirPath string) {
	log.Printf("[DEBUG] loadDirectoryChildren: START for path=%s", dirPath)

	if dt.mainWindow.client == nil || !dt.mainWindow.client.IsConnected() {
		log.Printf("[DEBUG] loadDirectoryChildren: Not connected")
		dt.treeMutex.Lock()
		delete(dt.loadingNodes, dirPath)
		dt.treeMutex.Unlock()
		// Schedule refresh on main thread
		fyne.Do(func() {
			dt.tree.Refresh()
		})
		return
	}

	// Convert to relative path for API
	relPath := strings.TrimPrefix(dirPath, "/")
	if relPath == "/" {
		relPath = ""
	}
	log.Printf("[DEBUG] loadDirectoryChildren: relPath=%s", relPath)

	// Run in background
	go func() {
		log.Printf("[DEBUG] loadDirectoryChildren: Calling ListFiles for %s", relPath)
		files, err := dt.mainWindow.client.ListFiles(relPath, false)

		dt.treeMutex.Lock()
		delete(dt.loadingNodes, dirPath)

		if err != nil {
			log.Printf("[DEBUG] loadDirectoryChildren: ListFiles failed - %v", err)
			dt.treeData[dirPath] = []string{} // Empty on error
			dt.treeMutex.Unlock()
			// Schedule refresh on main thread
			fyne.Do(func() {
				dt.tree.Refresh()
			})
			return
		}

		log.Printf("[DEBUG] loadDirectoryChildren: Got %d files", len(files))

		var children []string
		for _, file := range files {
			// Only add directories to the tree
			if !file.IsDir {
				continue
			}

			// Build full path for this child
			var fullPath string
			if dirPath == "/" || dirPath == "" {
				fullPath = "/" + file.Name
			} else {
				fullPath = dirPath + "/" + file.Name
			}

			// Store item info
			dt.treeItemMap[fullPath] = file

			// Add to children
			children = append(children, fullPath)

			log.Printf("[DEBUG] loadDirectoryChildren: Added child %s (dir=%v)", fullPath, file.IsDir)
		}

		// Handle root path
		key := dirPath
		if key == "" {
			key = "/"
		}
		dt.treeData[key] = children
		dt.treeMutex.Unlock()

		log.Printf("[DEBUG] loadDirectoryChildren: Stored %d children for key=%s", len(children), key)

		// Refresh tree on UI thread
		log.Printf("[DEBUG] loadDirectoryChildren: Calling tree.Refresh()")
		fyne.Do(func() {
			dt.tree.Refresh()
		})
		log.Printf("[DEBUG] loadDirectoryChildren: DONE for path=%s", dirPath)
	}()
}

// loadDirectoryChildrenWithCallback loads children for a directory and calls callback when done
func (dt *DirectoryTree) loadDirectoryChildrenWithCallback(dirPath string, callback func()) {
	log.Printf("[DEBUG] loadDirectoryChildrenWithCallback: START for path=%s", dirPath)

	if dt.mainWindow.client == nil || !dt.mainWindow.client.IsConnected() {
		log.Printf("[DEBUG] loadDirectoryChildrenWithCallback: Not connected")
		dt.treeMutex.Lock()
		delete(dt.loadingNodes, dirPath)
		dt.treeMutex.Unlock()
		// Schedule refresh on main thread
		fyne.Do(func() {
			dt.tree.Refresh()
		})
		if callback != nil {
			callback()
		}
		return
	}

	// Convert to relative path for API
	relPath := strings.TrimPrefix(dirPath, "/")
	if relPath == "/" {
		relPath = ""
	}
	log.Printf("[DEBUG] loadDirectoryChildrenWithCallback: relPath=%s", relPath)

	// Run in background
	go func() {
		log.Printf("[DEBUG] loadDirectoryChildrenWithCallback: Calling ListFiles for %s", relPath)
		files, err := dt.mainWindow.client.ListFiles(relPath, false)

		dt.treeMutex.Lock()
		delete(dt.loadingNodes, dirPath)

		if err != nil {
			log.Printf("[DEBUG] loadDirectoryChildrenWithCallback: ListFiles failed - %v", err)
			dt.treeData[dirPath] = []string{} // Empty on error
			dt.treeMutex.Unlock()
			// Schedule refresh on main thread
			fyne.Do(func() {
				dt.tree.Refresh()
			})
			if callback != nil {
				callback()
			}
			return
		}

		log.Printf("[DEBUG] loadDirectoryChildrenWithCallback: Got %d files", len(files))

		var children []string
		for _, file := range files {
			// Only add directories to the tree
			if !file.IsDir {
				continue
			}

			// Build full path for this child
			var fullPath string
			if dirPath == "/" || dirPath == "" {
				fullPath = "/" + file.Name
			} else {
				fullPath = dirPath + "/" + file.Name
			}

			// Store item info
			dt.treeItemMap[fullPath] = file

			// Add to children
			children = append(children, fullPath)

			log.Printf("[DEBUG] loadDirectoryChildrenWithCallback: Added child %s (dir=%v)", fullPath, file.IsDir)
		}

		// Handle root path
		key := dirPath
		if key == "" {
			key = "/"
		}
		dt.treeData[key] = children
		dt.treeMutex.Unlock()

		log.Printf("[DEBUG] loadDirectoryChildrenWithCallback: Stored %d children for key=%s", len(children), key)

		// Refresh tree on UI thread
		log.Printf("[DEBUG] loadDirectoryChildrenWithCallback: Calling tree.Refresh()")
		fyne.Do(func() {
			dt.tree.Refresh()
		})
		log.Printf("[DEBUG] loadDirectoryChildrenWithCallback: DONE for path=%s", dirPath)

		// Call callback after loading is complete
		if callback != nil {
			callback()
		}
	}()
}

// LoadTree loads the entire directory tree starting from root
func (dt *DirectoryTree) LoadTree() {
	log.Printf("[DEBUG] LoadTree: START")

	if dt.mainWindow.client == nil || !dt.mainWindow.client.IsConnected() {
		log.Printf("[DEBUG] LoadTree: Not connected")
		return
	}

	// Clear existing data
	log.Printf("[DEBUG] LoadTree: Clearing existing data")
	dt.treeMutex.Lock()
	dt.treeData = make(map[string][]string)
	dt.treeItemMap = make(map[string]kcpclient.ListItem)
	dt.expandedNodes = make(map[string]bool)
	dt.loadingNodes = make(map[string]bool)
	dt.treeMutex.Unlock()

	// Load root level with callback to open root branch after loading
	log.Printf("[DEBUG] LoadTree: Loading root level")
	dt.loadDirectoryChildrenWithCallback("/", func() {
		// Open root branch to trigger display after data is loaded
		fyne.Do(func() {
			dt.tree.OpenBranch("/")
			dt.tree.Refresh()
		})
		log.Printf("[DEBUG] LoadTree: END")
	})
}

// Refresh refreshes the directory tree
func (dt *DirectoryTree) Refresh() {
	log.Printf("[DEBUG] DirectoryTree.Refresh: Reloading tree")
	dt.LoadTree()
}

// collapseAll collapses all expanded branches
func (dt *DirectoryTree) collapseAll() {
	dt.treeMutex.Lock()

	// Get list of expanded paths to close
	pathsToClose := make([]string, 0, len(dt.expandedNodes))
	for path := range dt.expandedNodes {
		// Only close valid paths (skip root)
		if path != "" && path != "/" {
			pathsToClose = append(pathsToClose, path)
		}
	}

	// Clear the map BEFORE calling CloseBranch to prevent race conditions
	// OnBranchClosed callback won't find anything to modify
	dt.expandedNodes = make(map[string]bool)
	dt.treeMutex.Unlock()

	// Close branches without holding lock (OnBranchClosed will try to lock but map is empty)
	for _, path := range pathsToClose {
		log.Printf("[DEBUG] collapseAll: Closing branch %s", path)
		dt.tree.CloseBranch(path)
	}
}

// GetCurrentSelection returns the currently selected directory
func (dt *DirectoryTree) GetCurrentSelection() string {
	dt.treeMutex.RLock()
	defer dt.treeMutex.RUnlock()
	return dt.selectedPath
}

// SetSelection sets the selected directory and expands path to it
func (dt *DirectoryTree) SetSelection(path string) {
	log.Printf("[DEBUG] DirectoryTree.SetSelection: path=%s", path)

	// Build full path
	var fullPath string
	if path == "" || path == "/" {
		fullPath = "/"
	} else {
		fullPath = "/" + strings.TrimPrefix(path, "/")
	}

	dt.treeMutex.Lock()
	dt.selectedPath = fullPath

	// Mark path as expanded
	dt.expandToPath(fullPath)
	expandedPaths := make(map[string]bool)
	for p := range dt.expandedNodes {
		expandedPaths[p] = true
	}
	dt.treeMutex.Unlock()

	// Open all branches in path
	for p := range expandedPaths {
		if p != "/" && p != "" {
			dt.tree.OpenBranch(p)
		}
	}

	// Select the node
	dt.tree.Select(fullPath)
}

// expandToPath expands the tree to show the given path
func (dt *DirectoryTree) expandToPath(targetPath string) {
	// Build path components
	components := strings.Split(strings.Trim(targetPath, "/"), "/")
	currentPath := ""

	for _, component := range components {
		if component == "" {
			continue
		}

		if currentPath == "" {
			currentPath = "/" + component
		} else {
			currentPath = currentPath + "/" + component
		}

		// Mark as expanded
		dt.expandedNodes[currentPath] = true
	}
}

// UpdateCurrentPath updates the selected path based on current navigation
func (dt *DirectoryTree) UpdateCurrentPath(currentPath string) {
	log.Printf("[DEBUG] DirectoryTree.UpdateCurrentPath: %s", currentPath)
	dt.SetSelection(currentPath)
}

// GetWidget returns the underlying tree widget
func (dt *DirectoryTree) GetWidget() *widget.Tree {
	return dt.tree
}

// showContextMenu shows right-click context menu for tree nodes
func (dt *DirectoryTree) showContextMenu(path string, pos fyne.Position) {
	if dt.mainWindow.client == nil || !dt.mainWindow.client.IsConnected() {
		return
	}

	contextMenu := NewContextMenu(dt.mainWindow)

	// Create a dummy ListItem for the context menu
	var item *kcpclient.ListItem
	dt.treeMutex.RLock()
	if realItem, ok := dt.treeItemMap[path]; ok {
		item = &realItem
	}
	dt.treeMutex.RUnlock()

	if item != nil {
		contextMenu.ShowFileListMenu(item, pos)
	} else {
		// Show background menu for empty space
		contextMenu.ShowBackgroundMenu(pos)
	}
}
