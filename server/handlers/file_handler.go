package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
)

// ListItem represents a file or directory in the listing
type ListItem struct {
	Name    string `json:"name"`
	Path    string `json:"path"`
	Size    int64  `json:"size"`
	ModTime int64  `json:"modTime"`
	IsDir   bool   `json:"isDir"`
	Mode    string `json:"mode"` // Simplified permissions string
}

// FileHandler handles file operations
type FileHandler struct {
	rootDir   string
	hashCache sync.Map
}

// NewFileHandler creates a new file handler
func NewFileHandler(rootDir string) *FileHandler {
	return &FileHandler{
		rootDir: rootDir,
	}
}

// cleanRelPath cleans a relative path and prevents directory traversal
func (h *FileHandler) cleanRelPath(rel string) string {
	if rel == "" {
		return ""
	}
	clean := path.Clean("/" + rel)
	clean = strings.TrimPrefix(clean, "/")
	// Prevent directory traversal
	if strings.HasPrefix(clean, "..") || strings.Contains(clean, "/../") {
		return ""
	}
	return clean
}

// isPathSafe checks if a path is safe (prevents directory traversal)
func (h *FileHandler) isPathSafe(requestPath string) (string, bool) {
	// Clean path
	cleanPath := path.Clean("/" + requestPath)
	// Build full path
	fullPath := filepath.Join(h.rootDir, filepath.FromSlash(cleanPath))
	// Get absolute paths
	absRoot, err := filepath.Abs(h.rootDir)
	if err != nil {
		return "", false
	}
	absPath, err := filepath.Abs(fullPath)
	if err != nil {
		return "", false
	}
	// Ensure path is under root directory
	if !strings.HasPrefix(absPath, absRoot+string(filepath.Separator)) && absPath != absRoot {
		return "", false
	}
	return fullPath, true
}

// ListFiles returns a list of files in the specified directory
func (h *FileHandler) ListFiles(rel string, recursive bool) ([]ListItem, error) {
	rel = h.cleanRelPath(rel)
	target, safe := h.isPathSafe(rel)
	if !safe {
		return nil, os.ErrPermission
	}

	if recursive {
		var items []ListItem
		err := filepath.WalkDir(target, func(p string, d os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if p == target {
				return nil
			}
			info, err := d.Info()
			if err != nil {
				return nil
			}
			relPath, _ := filepath.Rel(h.rootDir, p)
			items = append(items, ListItem{
				Name:    d.Name(),
				Path:    "/" + filepath.ToSlash(relPath),
				Size:    info.Size(),
				ModTime: info.ModTime().Unix(),
				IsDir:   info.IsDir(),
				Mode:    info.Mode().String(),
			})
			return nil
		})
		return items, err
	}

	entries, err := os.ReadDir(target)
	if err != nil {
		return nil, err
	}
	var items []ListItem
	for _, e := range entries {
		info, err := e.Info()
		if err != nil {
			continue
		}
		items = append(items, ListItem{
			Name:    e.Name(),
			Path:    "/" + path.Join(rel, e.Name()),
			Size:    info.Size(),
			ModTime: info.ModTime().Unix(),
			IsDir:   e.IsDir(),
			Mode:    info.Mode().String(),
		})
	}
	return items, nil
}

// HandleList handles the list action
func (h *FileHandler) HandleList(w http.ResponseWriter, r *http.Request) {
	rel := r.URL.Query().Get("path")
	recursive := r.URL.Query().Get("recursive") == "1"
	files, err := h.ListFiles(rel, recursive)
	if err != nil {
		http.Error(w, "Cannot list files", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(files)
}

// HandleDelete handles file/directory deletion
func (h *FileHandler) HandleDelete(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	filePath := r.URL.Query().Get("path")
	cleanPath, safe := h.isPathSafe(filePath)
	if !safe {
		http.Error(w, "Invalid path", http.StatusBadRequest)
		return
	}

	// Check if path exists
	info, err := os.Stat(cleanPath)
	if err != nil {
		if os.IsNotExist(err) {
			http.Error(w, "File not found", http.StatusNotFound)
			return
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Delete file or directory
	if info.IsDir() {
		err = os.RemoveAll(cleanPath)
	} else {
		err = os.Remove(cleanPath)
	}

	if err != nil {
		http.Error(w, "Failed to delete: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
	w.Write([]byte("OK"))
}

// HandleMkdir handles directory creation
func (h *FileHandler) HandleMkdir(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	dirPath := r.URL.Query().Get("path")
	cleanPath, safe := h.isPathSafe(dirPath)
	if !safe {
		http.Error(w, "Invalid path", http.StatusBadRequest)
		return
	}

	// Create directory with parents
	err := os.MkdirAll(cleanPath, 0755)
	if err != nil {
		http.Error(w, "Failed to create directory: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusCreated)
	w.Write([]byte("OK"))
}

// HandleRename handles file/directory renaming
func (h *FileHandler) HandleRename(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	oldPath := r.URL.Query().Get("old")
	newPath := r.URL.Query().Get("new")

	if oldPath == "" || newPath == "" {
		http.Error(w, "Missing old or new path", http.StatusBadRequest)
		return
	}

	cleanOldPath, safe := h.isPathSafe(oldPath)
	if !safe {
		http.Error(w, "Invalid old path", http.StatusBadRequest)
		return
	}

	cleanNewPath, safe := h.isPathSafe(newPath)
	if !safe {
		http.Error(w, "Invalid new path", http.StatusBadRequest)
		return
	}

	// Check if old path exists
	_, err := os.Stat(cleanOldPath)
	if err != nil {
		if os.IsNotExist(err) {
			http.Error(w, "Source not found", http.StatusNotFound)
			return
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Rename
	err = os.Rename(cleanOldPath, cleanNewPath)
	if err != nil {
		http.Error(w, "Failed to rename: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
	w.Write([]byte("OK"))
}

// FileStatInfo represents detailed file information
type FileStatInfo struct {
	Name    string `json:"name"`
	Path    string `json:"path"`
	Size    int64  `json:"size"`
	ModTime int64  `json:"modTime"`
	IsDir   bool   `json:"isDir"`
	Mode    string `json:"mode"`
	ModeNum uint32 `json:"modeNum"` // Numeric mode for chmod
}

// HandleStat handles GET /stat requests to get file attributes
func (h *FileHandler) HandleStat(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	filePath := r.URL.Query().Get("path")
	if filePath == "" {
		http.Error(w, "Missing path parameter", http.StatusBadRequest)
		return
	}

	cleanPath, safe := h.isPathSafe(filePath)
	if !safe {
		http.Error(w, "Invalid path", http.StatusBadRequest)
		return
	}

	info, err := os.Stat(cleanPath)
	if err != nil {
		if os.IsNotExist(err) {
			http.Error(w, "File not found", http.StatusNotFound)
			return
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	statInfo := FileStatInfo{
		Name:    info.Name(),
		Path:    filePath,
		Size:    info.Size(),
		ModTime: info.ModTime().Unix(),
		IsDir:   info.IsDir(),
		Mode:    info.Mode().String(),
		ModeNum: uint32(info.Mode().Perm()),
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(statInfo)
}

// HandleChmod handles POST /chmod requests to change file permissions
func (h *FileHandler) HandleChmod(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	filePath := r.URL.Query().Get("path")
	modeStr := r.URL.Query().Get("mode")

	if filePath == "" || modeStr == "" {
		http.Error(w, "Missing path or mode parameter", http.StatusBadRequest)
		return
	}

	cleanPath, safe := h.isPathSafe(filePath)
	if !safe {
		http.Error(w, "Invalid path", http.StatusBadRequest)
		return
	}

	// Parse mode (supports octal like "755" or "0755")
	mode, err := strconv.ParseUint(modeStr, 8, 32)
	if err != nil {
		http.Error(w, "Invalid mode format (use octal like 755)", http.StatusBadRequest)
		return
	}

	// Check if file exists
	if _, err := os.Stat(cleanPath); err != nil {
		if os.IsNotExist(err) {
			http.Error(w, "File not found", http.StatusNotFound)
			return
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Change mode
	err = os.Chmod(cleanPath, os.FileMode(mode))
	if err != nil {
		http.Error(w, "Failed to change permissions: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
	fmt.Fprintf(w, "OK\nMode changed to %o", mode)
}
