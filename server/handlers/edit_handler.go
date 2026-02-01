package handlers

import (
	"io"
	"net/http"
	"os"
	"path/filepath"
)

// EditHandler handles text file editing operations
type EditHandler struct {
	fileHandler *FileHandler
}

// NewEditHandler creates a new edit handler
func NewEditHandler(rootDir string) *EditHandler {
	return &EditHandler{
		fileHandler: NewFileHandler(rootDir),
	}
}

const maxEditSize = 1 * 1024 * 1024 // 1MB limit for editing

// HandleGetFile handles GET requests to read file content for editing
func (h *EditHandler) HandleGetFile(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	filePath := r.URL.Query().Get("path")
	if filePath == "" {
		http.Error(w, "Missing path parameter", http.StatusBadRequest)
		return
	}

	cleanPath, safe := h.fileHandler.isPathSafe(filePath)
	if !safe {
		http.Error(w, "Invalid path", http.StatusBadRequest)
		return
	}

	// Check if file exists and get info
	info, err := os.Stat(cleanPath)
	if err != nil {
		if os.IsNotExist(err) {
			http.Error(w, "File not found", http.StatusNotFound)
			return
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Check size limit
	if info.Size() > maxEditSize {
		http.Error(w, "File too large for editing (max 1MB)", http.StatusBadRequest)
		return
	}

	// Check if it's a regular file
	if info.IsDir() {
		http.Error(w, "Cannot edit directory", http.StatusBadRequest)
		return
	}

	// Read file content
	content, err := os.ReadFile(cleanPath)
	if err != nil {
		http.Error(w, "Failed to read file: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Set content type
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	w.Write(content)
}

// HandleSaveFile handles PUT requests to save file content
func (h *EditHandler) HandleSaveFile(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPut {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	filePath := r.URL.Query().Get("path")
	if filePath == "" {
		http.Error(w, "Missing path parameter", http.StatusBadRequest)
		return
	}

	cleanPath, safe := h.fileHandler.isPathSafe(filePath)
	if !safe {
		http.Error(w, "Invalid path", http.StatusBadRequest)
		return
	}

	// Check content length
	if r.ContentLength > maxEditSize {
		http.Error(w, "Content too large (max 1MB)", http.StatusBadRequest)
		return
	}

	// Create directory if not exists
	dir := filepath.Dir(cleanPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		http.Error(w, "Failed to create directory: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Read content
	defer r.Body.Close()
	content, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "Failed to read content: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Check size limit again
	if len(content) > maxEditSize {
		http.Error(w, "Content too large (max 1MB)", http.StatusBadRequest)
		return
	}

	// Write file
	err = os.WriteFile(cleanPath, content, 0644)
	if err != nil {
		http.Error(w, "Failed to write file: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
	w.Write([]byte("OK"))
}
