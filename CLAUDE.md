# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## 🚨 CRITICAL REMINDERS (Read Before Every Task)

**Before delivering ANY code to user for testing:**

1. ✅ **Use MCP tools** when unsure about APIs - NEVER guess
2. ✅ **Build must pass**: `go build` with ZERO errors
3. ✅ **Code review**: Check logic, errors, thread-safety, security, memory leaks
4. ✅ **Impact analysis**: What else might break? Test critical paths

**User testing is EXPENSIVE. Catch bugs BEFORE delivery!**

See "MANDATORY Pre-Delivery Checklist" below for details.

---

## Project Overview

**simpleKcpFileManager** is a full-featured bidirectional file manager using KCP protocol for high-speed transfers over lossy networks. Built with Go and Fyne v2, it provides a modern GUI similar to WinSCP or FileZilla, with support for file upload, download, rename, delete, compression, and text editing.

**Architecture Stack:**
```
GUI (Fyne v2) → Application (HTTP) → smux (Multiplexing) → KCP (Reliable UDP) → UDP
```

**Key Features:**
- Modern GUI with directory tree, file list, and task queue panels
- Bidirectional file transfer (upload/download)
- Multi-threaded parallel downloads with resume capability
- File operations: rename, delete, create folders, compress (ZIP/TAR)
- Text file editing with remote save
- Real-time transfer progress with pause/resume/cancel
- AES-256 encryption with PBKDF2 key derivation
- KCP protocol for high-speed transfers on high-latency networks

## Build and Run Commands

### Local Development
```bash
# Run server (default port 8080, share current directory)
go run ./server -p 8080 -d /path/to/share

# Run client in GUI mode (shows connection dialog)
go run ./client

# Run client in CLI mode
go run ./client -cli -addr 192.168.1.100:8080 -url /file.mp4 -out ./downloads

# Specify custom encryption key
go run ./server -key "my-secret-key"
go run ./client -cli -key "my-secret-key" -addr 192.168.1.100:8080 -url /file.zip
```

### Building
```bash
# Build for current platform
go build -o server/server.exe ./server
go build -o client/client.exe ./client

# Cross-platform compilation (uses scripts)
./scripts/build.sh      # Linux/macOS
.\scripts\build.ps1     # Windows PowerShell
```

Build scripts handle CGO requirements:
- **Server**: CGO disabled (no GUI dependencies)
- **Client**: CGO enabled (Fyne GUI requires C bindings)
- **Platforms**: Windows amd64/arm64, Linux amd64/arm64

## Code Architecture

### Directory Structure
```
client/
├── main.go                 # Entry point (GUI/CLI mode selection)
├── gui/                    # Fyne GUI components
│   ├── main_window.go      # Main application window
│   ├── directory_tree.go   # Left panel directory tree
│   ├── task_queue.go       # Right panel task queue
│   ├── context_menu.go     # Right-click context menus
│   └── connection_dialog.go # Connection dialog
└── kcpclient/              # KCP/HTTP client library
    ├── client.go           # Core client implementation
    └── tasks/              # Task management system
        ├── manager.go      # Task manager
        └── task.go         # Individual task handling

server/
└── main.go                 # HTTP-over-KCP file server

common/
└── kcp.go                  # Shared KCP/smux configuration
```

### Key Components

**GUI Architecture (`client/gui/`):**
- **main_window.go**: Main window (1600x900) with three-panel layout:
  - Left: Directory tree with lazy loading
  - Center: File list with details (name, size, modified date)
  - Right: Task queue with real-time progress
- **directory_tree.go**: `widget.Tree` with dynamic directory loading
  - Lazy loading: children loaded when parent expanded
  - Thread-safe with `treeMutex` (RWMutex)
  - Path tracking and selection synchronization
- **task_queue.go**: Transfer queue management
  - Auto-refresh at 2Hz (2 times per second)
  - Task widgets with pause/resume/retry/cancel buttons
  - Auto-cleanup completed tasks after 5 seconds
- **context_menu.go**: Right-click menu for file operations
  - Download file/folder, upload file, new folder
  - Rename, delete, compress (ZIP/TAR), edit text files

**Client (`client/kcpclient/client.go`):**
- HTTP client over KCP/smux transport
- Thread-safe session management with `sessionMutex`
- Connection timeout (3 seconds) for quick key validation
- Methods: `ListFiles`, `UploadFile`, `DownloadFile`, `DeleteFile`, `RenameFile`, `CreateDirectory`, `ReadFile`, `SaveFile`, `Compress`, `Extract`

**Task Management (`client/kcpclient/tasks/`):**
- `Manager`: Handles task queue with max 3 parallel downloads
- `Task`: Represents single transfer operation
- Status tracking: Pending, Running, Paused, Completed, Failed, Canceled
- Progress callbacks for UI updates

**Server (`server/main.go`):**
- HTTP server running over KCP/smux via `SmuxListener` adapter
- Actions: list, upload, download, delete, mkdir, rename, compress, extract, edit, checksum
- Path traversal protection with `isPathSafe()`
- SHA256 checksum caching with `hashCache` (sync.Map)
- HTTP Range request support for chunked transfers

**Common (`common/kcp.go`):**
- KCP "fast3" configuration: window 1024, noDelay mode, 10ms interval
- AES-256 encryption with PBKDF2 (4096 iterations, SHA-1 PRF)
- `SmuxListener` adapter for HTTP server compatibility
- Default key: `"your-secret-key"` (change in production!)

## Encryption System

The application uses AES-256 encryption with PBKDF2 key derivation:
- Salt: `"kcp-file-transfer"`
- Default key: `"your-secret-key"` (production should use custom keys via `-key` parameter)
- PBKDF2: 4096 iterations, SHA-1 PRF, 32-byte output

Both client and server must use the same key. Connection will fail with timeout if keys mismatch.

**Connection validation:**
- Client sends `HEAD /` request after KCP connection
- Server responds with single byte if key matches
- 3-second timeout for quick detection of wrong key

### Concurrency Model

**Client:**
- Session management with `sessionMutex` for KCP connection reuse
- Task manager limits concurrent downloads (max 3)
- Each download spawns 8 workers for parallel chunk downloading (4MB chunks)
- GUI updates must NOT use `fyne.Do()` - update widgets directly from goroutines
- Use `go func()` for async operations (HTTP requests, file I/O)

**Directory Tree:**
- `treeMutex` (RWMutex) protects `treeData`, `treeItemMap`, `expandedNodes`
- Lazy loading: children loaded when parent expanded via `OnBranchOpened`
- `loadDirectoryChildren()` runs in goroutine to avoid blocking UI
- Tree refresh (`tree.Refresh()`) called after data update

**Task Queue:**
- `taskMutex` (RWMutex) protects `taskWidgets` map
- `updateLoop()` goroutine refreshes display at 2Hz
- Auto-cleanup: removes completed tasks 5 seconds after completion

**Server:**
- `hashCache` (sync.Map) caches SHA256 checksums
- Standard Go HTTP server concurrency model over smux streams

## Fyne v2 GUI Best Practices (CRITICAL)

**⚠️ IMPORTANT: The following guidelines are based on real issues encountered and official Fyne v2 documentation.**

### Thread Safety and UI Updates from Goroutines

**Official Documentation Reference:**
- https://docs.fyne.io/started/goroutines.html

**✅ CORRECT: Using `fyne.Do()` for UI Updates from Goroutines**

Contrary to previous assumptions, `fyne.Do()` **DOES exist** in Fyne v2 and is the **recommended** way to update UI from goroutines.

```go
// ✅ CORRECT - Update UI from goroutine using fyne.Do()
go func() {
    // Perform background work (network, file I/O, etc.)
    result := doHeavyWork()

    // Update UI on main thread
    fyne.Do(func() {
        label.SetText(result)
        widget.Refresh()
    })
}()
```

**Why `fyne.Do()` is Required:**
- Fyne runs UI rendering on the main thread
- Direct widget updates from goroutines can cause race conditions
- `fyne.Do()` queues updates to execute on the main thread
- Error message: `*** Error in Fyne call thread, this should have been called in fyne.Do[AndWait] ***`

**❌ WRONG: Direct widget updates from goroutines**
```go
// ❌ WRONG - Causes thread safety errors
go func() {
    result := doHeavyWork()
    label.SetText(result)  // ERROR: Not on main thread!
    widget.Refresh()
}()
```

### Canvas.Refresh() vs widget.Refresh()

**Two approaches for safe UI updates:**

**1. Using `fyne.Do()` (Recommended for complex updates):**
```go
go func() {
    data := loadDataFromServer()

    fyne.Do(func() {
        mw.serverFiles = data
        mw.fileList.Refresh()
        mw.updatePathBreadcrumbs(path)
        mw.statusLabel.SetText(fmt.Sprintf("%d items", len(data)))
    })
}()
```

**2. Using `Canvas.Refresh()` (Alternative for simple widget refresh):**
```go
// From goroutine - refresh specific widget via Canvas
go func() {
    dt.treeData[key] = children

    if dt.mainWindow.window != nil && dt.mainWindow.window.Canvas() != nil {
        dt.mainWindow.window.Canvas().Refresh(dt.tree)
    }
}()
```

**Note:** Both approaches are valid, but `fyne.Do()` is more explicit and safer for complex multi-widget updates.

### widget.List Double-Click Detection

**Challenge:** `widget.List` only provides `OnSelected` and `OnUnselected` callbacks. No built-in double-click support.

**Solution: Manual double-tap detection with timing**
```go
type MainWindow struct {
    // ... other fields ...
    doubleTapMutex sync.Mutex  // CRITICAL: Protect double-tap state
    lastTapTime    int64
    lastTapID      widget.ListItemID
}

func (mw *MainWindow) setupFileListDoubleTap() {
    originalOnSelected := mw.fileList.OnSelected
    mw.fileList.OnSelected = func(id widget.ListItemID) {
        mw.doubleTapMutex.Lock()
        now := time.Now().UnixMilli()
        isDoubleTap := (id == mw.lastTapID && now-mw.lastTapTime < 500)

        // Update tracking BEFORE releasing lock
        if !isDoubleTap {
            mw.lastTapID = id
            mw.lastTapTime = now
        } else {
            mw.lastTapID = -1
            mw.lastTapTime = 0
        }
        mw.doubleTapMutex.Unlock()

        // Handle selection
        if originalOnSelected != nil {
            originalOnSelected(id)
        }

        // Refresh only for single tap to avoid infinite loop
        if !isDoubleTap {
            mw.fileList.Refresh()
        }

        // Handle double-tap action
        if isDoubleTap {
            // Navigate, edit file, etc.
        }
    }
}
```

**Key Points:**
- ✅ Use mutex to protect `lastTapTime` and `lastTapID`
- ✅ Call `originalOnSelected()` to ensure selection happens
- ✅ Only refresh on single tap (double-tap navigates away anyway)
- ✅ Capture `id` in goroutine closure for reset timer

### Common Pitfalls

**1. Race Conditions in Shared State**
```go
// ❌ WRONG - Unsynchronized access
var lastTapTime int64
var lastTapID int64

// ✅ CORRECT - Protected with mutex
type MainWindow struct {
    doubleTapMutex sync.Mutex
    lastTapTime    int64
    lastTapID      widget.ListItemID
}
```

**2. Modifying Collections While Iterating**
```go
// ❌ WRONG - Deadlock or race condition
func (dt *DirectoryTree) collapseAll() {
    dt.treeMutex.Lock()
    for path := range dt.expandedNodes {
        dt.tree.CloseBranch(path)  // Calls OnBranchClosed which tries to lock treeMutex!
    }
    dt.expandedNodes = make(map[string]bool)
    dt.treeMutex.Unlock()
}

// ✅ CORRECT - Clear map before calling CloseBranch
func (dt *DirectoryTree) collapseAll() {
    dt.treeMutex.Lock()
    pathsToClose := make([]string, 0, len(dt.expandedNodes))
    for path := range dt.expandedNodes {
        if path != "" && path != "/" {
            pathsToClose = append(pathsToClose, path)
        }
    }
    dt.expandedNodes = make(map[string]bool)  // Clear BEFORE CloseBranch
    dt.treeMutex.Unlock()

    for _, path := range pathsToClose {
        dt.tree.CloseBranch(path)  // OnBranchClosed won't find anything to modify
    }
}
```

**3. Infinite Refresh Loops**
```go
// ❌ RISKY - Calling Refresh() inside OnSelected
list.OnSelected = func(id widget.ListItemID) {
    updateSelection(id)
    list.Refresh()  // Might trigger OnSelected again
}

// ✅ CORRECT - Conditional refresh or avoid Refresh() in callback
list.OnSelected = func(id widget.ListItemID) {
    updateSelection(id)
    if !isDoubleTap {
        list.Refresh()  // Only refresh when safe
    }
}
```

### Layout Issues and Solutions

**Issue: Toolbar Overlapping with List on First Load**

**Problem:** Header container content overlaps with first list items on initial display.

**Solution 1: Add separator and proper container structure**
```go
// ❌ WRONG - VBox without separator
headerContainer := container.NewVBox(navToolbar, pathContainer, sortToolbar)

// ✅ CORRECT - Add separator and proper structure
headerContainer := container.NewBorder(
    nil, nil, nil, nil,
    container.NewVBox(navToolbar, pathContainer, sortToolbar, widget.NewSeparator()),
)
```

**Solution 2: Explicit spacing**
```go
// Add vertical spacing after toolbar
headerContainer := container.NewVBox(
    navToolbar,
    pathContainer,
    sortToolbar,
    widget.NewSeparator(),
    container.NewPadded(theme.Padding()),  // Add spacing
)
```

## Important Implementation Details

### UI Thread Safety (Updated)
**Fyne v2 GUI updates:**
- **USE** `fyne.Do()` to wrap UI updates from goroutines - it's safe and recommended
- Widget updates like `SetText()`, `Refresh()` inside `fyne.Do()` are thread-safe
- Alternative: Use `window.Canvas().Refresh(widget)` for simple refresh operations
- Always wrap multi-widget updates in `fyne.Do()` to ensure atomicity

**Example pattern:**
```go
// Good: Async network request, then UI update
go func() {
    files, err := mw.client.ListFiles(path, false)
    if err != nil {
        mw.statusLabel.SetText("Load failed: " + err.Error())
        return
    }
    mw.serverFiles = files
    mw.fileList.Refresh()
}()

// Bad: Blocking UI with fyne.Do() (not available in Fyne v2)
fyne.Do(func() {
    mw.fileList.Refresh()  // This causes freeze!
})
```

### Path Handling
- Server paths are absolute: `/folder/file.txt`
- Client paths are relative to server root: `""` (root) or `"folder/subfolder"`
- Use `strings.TrimPrefix(path, "/")` to convert absolute to relative
- Use `path.Dir()` and `path.Join()` for path manipulation
- Empty path `""` represents server root directory

### Directory Tree Loading
- **Lazy loading**: Children loaded when parent branch opened
- **OnBranchOpened**: Triggers `loadDirectoryChildren()` if not already loaded
- **Avoid duplicate loads**: Check `treeData[path]` before loading
- **expandToPath()**: Only marks nodes as expanded, doesn't trigger loading

### Task Queue Display
- Update frequency: 2 times per second (`time.Second / 2`)
- Task widgets show: filename, status, progress bar, action buttons
- Buttons: Pause, Resume, Retry, Cancel (shown based on task status)
- Auto-cleanup: Completed tasks removed after 5 seconds

### Connection Flow
1. User enters server address and encryption key in dialog
2. `connectToServer()` called in goroutine to avoid blocking UI
3. Client creates KCP connection with 3-second timeout
4. Sends `HEAD /` request to validate encryption key
5. On success: loads directory tree, then file list
6. On failure: shows key input dialog for retry

### Window Size and Layout
- Main window: 1600x900 (centered on screen)
- Three-panel HSplit layout:
  - Directory tree: 25% width
  - File list: 50% width (adjustable)
  - Task queue: 25% width
- Navigation toolbar: Home, Up, Refresh, Download, Upload, Actions buttons

## Development Notes

### MANDATORY Pre-Delivery Checklist (CRITICAL)

**Before ANY code delivery to user for testing:**

1. **Use MCP Tools for Documentation** (REQUIRED):
   - When unsure about API usage, use `mcp__context7__` to find official docs
   - Example: Search "Fyne widget.List double click" or "Fyne keyboard shortcuts"
   - Use web search for error messages and implementation patterns
   - Never guess - always verify with documentation first

2. **Build Verification** (REQUIRED):
   ```bash
   cd client && go build -o client.exe  # MUST compile without errors
   cd server && go build -o server.exe  # MUST compile without errors
   ```
   - Fix ALL compilation errors before considering task complete
   - No warnings, no errors, clean build only

3. **Code Review** (REQUIRED):
   Before marking task complete, review:
   - ✅ Logic correctness and edge cases
   - ✅ Error handling (all errors checked and handled)
   - ✅ Thread-safety (mutex usage, goroutine safety)
   - ✅ Resource cleanup (defer Close(), file handles, etc.)
   - ✅ Security (path traversal, injection, etc.)
   - ✅ UI thread safety (NO fyne.Do(), proper goroutines)
   - ✅ Memory leaks (unbounded goroutines, unclosed resources)
   - ✅ Integration with existing code (doesn't break other features)

4. **Impact Analysis** (REQUIRED):
   - What other code might be affected?
   - Are there hidden dependencies?
   - Did we break any existing functionality?
   - Test critical paths: connection, download, upload, UI updates

**REMEMBER**: User testing is EXPENSIVE and TIME-CONSUMING. Catch issues BEFORE delivery!

### Using MCP Tools for Documentation (REQUIRED)

**When to use MCP tools:**
- ❌ DON'T guess API usage
- ❌ DON'T assume Fyne widget behavior
- ❌ DON'T trial-and-error with code
- ✅ DO use `mcp__context7__` for official docs
- ✅ DO use `mcp__web-search-prime__webSearchPrime` for recent info

**Examples of good MCP usage:**

```go
// Example 1: How to add keyboard shortcuts in Fyne
// Query: "Fyne AddShortcut keyboard shortcuts canvas"
// Result: Use desktop.CustomShortcut with AddShortcut()

ctrlS := &desktop.CustomShortcut{
    KeyName: fyne.KeyS,
    Modifier: fyne.KeyModifierControl,
}
w.Canvas().AddShortcut(ctrlS, func(shortcut fyne.Shortcut) {
    saveFile()
})

// Example 2: How to handle double-click on List
// Query: "Fyne widget.List double click OnDoubleTapped"
// Result: widget.List does NOT have OnDoubleTapped, use custom widget or other approach

// Example 3: How to detect mouse events
// Query: "Fyne widget mouse events MouseDown MouseUp"
// Result: Implement desktop.Mouseable interface for custom widgets
```

**MCP Tool Quick Reference:**

1. **Context7 (Documentation Search)**:
   - `mcp__context7__resolve-library-id` - Find library ID
   - `mcp__context7__query-docs` - Search documentation
   - Best for: Fyne API, Go standard library

2. **Web Search**:
   - `mcp__web-search-prime__webSearchPrime` - Search web
   - Best for: Error messages, recent issues, examples

**Workflow for uncertain code:**
1. Try MCP documentation search first
2. If no results, web search the exact error
3. Read 2-3 top results for patterns
4. Implement based on verified examples
5. Test with small code snippet before full integration

### Common Pitfalls

**UI Freeze:**
- Problem: Client becomes unresponsive after connection
- Cause: Using `fyne.Do()` or blocking operations on UI thread
- Fix: Use `go func()` for async operations, update widgets directly

**Directory Tree Issues:**
- Problem: Tree doesn't show directories or freezes
- Cause: Infinite loading loops or race conditions with `treeMutex`
- Fix: Check if already loaded before calling `loadDirectoryChildren()`

**Wrong Encryption Key:**
- Problem: Connection hangs indefinitely
- Cause: Server and client using different keys
- Fix: 3-second timeout with retry dialog

**Path Traversal:**
- Problem: Can access files outside shared directory
- Cause: Improper path validation
- Fix: Server uses `isPathSafe()` with `path.Clean()` and absolute path check

### Adding Tests
Currently no test framework exists. To add tests:
1. Create `*_test.go` files alongside source files
2. Use `go test ./...` to run tests
3. Test priority areas:
   - Path safety validation (`isPathSafe()`)
   - KCP configuration (`ConfigKCP()`)
   - Task manager state transitions
   - Directory tree loading logic

### Modifying KCP Configuration
KCP parameters are in `common/kcp.go` in `ConfigKCP()`:
```go
func ConfigKCP(conn *kcp.UDPSession) {
    conn.SetNoDelay(1, 10, 2, 1)  // nodelay, interval, resend, nc
    conn.SetWindowSize(1024, 1024) // sndwnd, rcvwnd
    conn.SetMtu(1350)              // Avoid IP fragmentation
    conn.SetACKNoDelay(true)
}
```

Tuning guidelines:
- `nodelay=1`: Enable immediate ACK (lower latency)
- `interval=10`: 10ms between packets (aggressive)
- `window=1024`: Large window for high throughput
- `MTU=1350`: Safe size to avoid fragmentation

### Client Performance Tuning
Constants in `kcpclient/client.go`:
```go
const (
    connectionTimeout = 3 * time.Second  // Key validation timeout
    defaultChunkSize  = 4 * 1024 * 1024 // 4MB per chunk for parallel downloads
)

// Task manager (tasks/manager.go)
const (
    maxParallelTasks = 3  // Max simultaneous downloads
    defaultWorkers   = 8  // Parallel threads per download
)
```

**To improve transfer speed:**
- Increase `maxParallelTasks` (more concurrent downloads)
- Increase `defaultWorkers` (more parallel chunks per download)
- Decrease KCP `interval` for more aggressive sending

**To reduce CPU usage:**
- Decrease `maxParallelTasks` and `defaultWorkers`
- Increase KCP `interval` (e.g., 20ms instead of 10ms)

### GUI Debugging
If GUI freezes or behaves unexpectedly:
1. Check for `fyne.Do()` calls (remove them)
2. Add debug logging before/after goroutine launches
3. Verify mutex locks are released (use `defer unlock()`)
4. Check for infinite loops in loading logic
5. Monitor goroutine count with `runtime.NumGoroutine()`

### Project Status
**Completed Features:**
- ✅ GUI with three-panel layout (directory tree, file list, task queue)
- ✅ File operations: upload, download, rename, delete, create folder
- ✅ Multi-threaded downloads with resume support
- ✅ Context menu system for file operations
- ✅ Task queue with real-time progress updates
- ✅ Connection dialog with key validation
- ✅ Directory tree with lazy loading

**Pending Features (from Phase 2-4):**
- ⏳ Drag-and-drop file upload
- ⏳ Text editor integration
- ⏳ Compression/decompression UI integration
- ⏳ UI beautification and theme support
- ⏳ Additional file operations (copy, move, permissions)
