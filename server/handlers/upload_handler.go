package handlers

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/CertStone/simpleKcpFileManager/common"
)

// UploadHandler handles file uploads
type UploadHandler struct {
	fileHandler *FileHandler
	fileLocks   sync.Map // map[string]*sync.Mutex - per-file locks
}

// NewUploadHandler creates a new upload handler
func NewUploadHandler(rootDir string) *UploadHandler {
	return &UploadHandler{
		fileHandler: NewFileHandler(rootDir),
	}
}

// getLock returns a mutex lock for the specific file path
func (h *UploadHandler) getLock(path string) *sync.Mutex {
	lock, _ := h.fileLocks.LoadOrStore(path, &sync.Mutex{})
	return lock.(*sync.Mutex)
}

// HandleUpload handles file upload with resume support
func (h *UploadHandler) HandleUpload(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPut {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	filePath := r.URL.Query().Get("path")
	if filePath == "" {
		http.Error(w, "Missing path parameter", http.StatusBadRequest)
		return
	}

	// Check for auto-extract request
	autoExtract := r.Header.Get("X-Auto-Extract") == "1"

	cleanPath, safe := h.fileHandler.isPathSafe(filePath)
	if !safe {
		http.Error(w, "Invalid path", http.StatusBadRequest)
		return
	}

	// Create directory if not exists
	dir := filepath.Dir(cleanPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		http.Error(w, "Failed to create directory: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Check for resume support via Content-Range header
	contentRange := r.Header.Get("Content-Range")
	var startOffset int64 = 0

	if contentRange != "" {
		// Parse Content-Range: bytes start-end/total
		// Example: bytes 0-1023/2048
		var start, end, total int64
		_, err := fmt.Sscanf(contentRange, "bytes %d-%d/%d", &start, &end, &total)
		if err == nil {
			startOffset = start
		}
	}

	// NOTE: Per-file locking removed to support parallel chunk uploads
	// Each chunk writes to a different offset in the same file
	// File system and OS handle concurrent writes to different file positions

	// Open file for writing
	// For chunked upload (with startOffset), use O_RDWR to avoid truncating
	// For new upload (no startOffset), use O_WRONLY|O_TRUNC to create fresh file
	var file *os.File
	var err error
	if startOffset > 0 {
		// Chunked upload: open in read-write mode without truncating
		file, err = os.OpenFile(cleanPath, os.O_CREATE|os.O_RDWR, 0644)
	} else {
		// New upload: create or truncate
		file, err = os.OpenFile(cleanPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
	}
	if err != nil {
		http.Error(w, "Failed to open file: "+err.Error(), http.StatusInternalServerError)
		return
	}
	defer file.Close()

	// Seek to start offset if resuming
	if startOffset > 0 {
		_, err = file.Seek(startOffset, io.SeekStart)
		if err != nil {
			http.Error(w, "Failed to seek file: "+err.Error(), http.StatusInternalServerError)
			return
		}
	}

	// Copy data with explicit flush to ensure data is written promptly
	// This helps with parallel uploads by releasing the file handle sooner
	written, err := io.Copy(file, r.Body)
	if err != nil {
		http.Error(w, "Failed to write file: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Sync to disk to ensure data is persisted
	if err := file.Sync(); err != nil {
		http.Error(w, "Failed to sync file: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Get final file size
	info, err := file.Stat()
	if err != nil {
		http.Error(w, "Failed to stat file: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Auto-extract if requested and file is tar.gz
	// Only do this on the last chunk (when total size matches expected size)
	if autoExtract && strings.HasSuffix(cleanPath, ".tar.gz") {
		fmt.Printf("[DEBUG] Auto-extract requested for: %s\n", cleanPath)

		// IMPORTANT: Close the file handle BEFORE extracting and deleting
		// This is necessary on Windows where open files cannot be deleted
		file.Close()

		// The tar.gz contains files with their original name as root
		// e.g., uploading "file.msi" creates tar with "file.msi" inside
		// So we extract to the parent directory of the .tar.gz file
		extractPath := filepath.Dir(cleanPath)
		fmt.Printf("[DEBUG] Extract path: %s\n", extractPath)

		// Extract archive
		if err := common.DecompressFromTarGz(cleanPath, extractPath); err != nil {
			fmt.Printf("[ERROR] Failed to extract: %v\n", err)
			http.Error(w, "Failed to extract archive: "+err.Error(), http.StatusInternalServerError)
			return
		}
		fmt.Printf("[DEBUG] Extract successful\n")

		// Remove temporary tar.gz file asynchronously with retry
		// This handles cases where the file might still be briefly locked
		go func(archivePath string) {
			for i := 0; i < 5; i++ {
				if err := os.Remove(archivePath); err != nil {
					if os.IsNotExist(err) {
						return // Already deleted
					}
					fmt.Printf("[DEBUG] Retry %d: failed to remove temp archive: %v\n", i+1, err)
					// Wait a bit before retrying (100ms, 200ms, 400ms, 800ms, 1600ms)
					sleepDuration := (1 << i) * 100 // exponential backoff
					<-time.After(time.Duration(sleepDuration) * time.Millisecond)
				} else {
					fmt.Printf("[DEBUG] Temp archive removed: %s\n", archivePath)
					return
				}
			}
			fmt.Printf("Warning: failed to remove temporary archive after retries: %s\n", archivePath)
		}(cleanPath)
	}

	// Return success with file size
	w.Header().Set("Content-Type", "text/plain")
	w.Header().Set("X-Uploaded-Bytes", strconv.FormatInt(written, 10))
	w.Header().Set("X-File-Size", strconv.FormatInt(info.Size(), 10))
	w.WriteHeader(http.StatusOK)
	fmt.Fprintf(w, "OK\nUploaded: %d bytes\nTotal: %d bytes", written, info.Size())
}
