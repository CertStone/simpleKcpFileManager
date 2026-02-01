package handlers

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"certstone.cc/simpleKcpFileManager/server/compress"
)

// CompressHandler handles compression and extraction operations
type CompressHandler struct {
	fileHandler *FileHandler
}

// NewCompressHandler creates a new compress handler
func NewCompressHandler(rootDir string) *CompressHandler {
	return &CompressHandler{
		fileHandler: NewFileHandler(rootDir),
	}
}

// HandleCompress handles file/folder compression
func (h *CompressHandler) HandleCompress(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	paths := r.URL.Query().Get("paths")
	outputPath := r.URL.Query().Get("output")
	format := r.URL.Query().Get("format")

	if paths == "" || outputPath == "" {
		http.Error(w, "Missing paths or output parameter", http.StatusBadRequest)
		return
	}

	if format == "" {
		format = "zip"
	}

	// Validate output path
	cleanOutputPath, safe := h.fileHandler.isPathSafe(outputPath)
	if !safe {
		http.Error(w, "Invalid output path", http.StatusBadRequest)
		return
	}

	// Parse source paths
	sourcePaths := strings.Split(paths, ",")
	var validPaths []string
	for _, p := range sourcePaths {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		cleanPath, safe := h.fileHandler.isPathSafe(p)
		if !safe {
			continue
		}
		validPaths = append(validPaths, cleanPath)
	}

	if len(validPaths) == 0 {
		http.Error(w, "No valid source paths", http.StatusBadRequest)
		return
	}

	// Create output directory
	if err := os.MkdirAll(filepath.Dir(cleanOutputPath), 0755); err != nil {
		http.Error(w, "Failed to create output directory: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Compress based on format
	var err error
	switch format {
	case "zip":
		err = compress.CreateZip(cleanOutputPath, validPaths)
	case "tar":
		// Create plain tar without gzip
		err = compress.CreateTar(cleanOutputPath, validPaths)
	case "targz", "tar.gz":
		// Create tar.gz (gzipped tar)
		err = compress.CreateTarGz(cleanOutputPath, validPaths)
	case "gzip":
		if len(validPaths) != 1 {
			http.Error(w, "Gzip only supports single file", http.StatusBadRequest)
			return
		}
		err = compress.CreateGzip(cleanOutputPath, validPaths[0])
	default:
		http.Error(w, "Unsupported format: "+format, http.StatusBadRequest)
		return
	}

	if err != nil {
		http.Error(w, "Compression failed: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
	fmt.Fprintf(w, "OK\nCompressed %d items to %s", len(validPaths), outputPath)
}

// HandleExtract handles archive extraction
func (h *CompressHandler) HandleExtract(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	archivePath := r.URL.Query().Get("path")
	destPath := r.URL.Query().Get("dest")

	if archivePath == "" {
		http.Error(w, "Missing path parameter", http.StatusBadRequest)
		return
	}

	// Validate archive path
	cleanArchivePath, safe := h.fileHandler.isPathSafe(archivePath)
	if !safe {
		http.Error(w, "Invalid archive path", http.StatusBadRequest)
		return
	}

	// Set destination path (same directory as archive if not specified)
	if destPath == "" {
		destPath = archivePath[:len(archivePath)-len(filepath.Ext(archivePath))]
	}

	cleanDestPath, safe := h.fileHandler.isPathSafe(destPath)
	if !safe {
		http.Error(w, "Invalid destination path", http.StatusBadRequest)
		return
	}

	// Create destination directory
	if err := os.MkdirAll(cleanDestPath, 0755); err != nil {
		http.Error(w, "Failed to create destination directory: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Detect archive type and extract
	ext := strings.ToLower(filepath.Ext(cleanArchivePath))
	var err error

	switch ext {
	case ".zip":
		err = compress.ExtractZip(cleanArchivePath, cleanDestPath)
	case ".tar", ".gz", ".tgz":
		err = compress.ExtractTar(cleanArchivePath, cleanDestPath)
	default:
		http.Error(w, "Unsupported archive format: "+ext, http.StatusBadRequest)
		return
	}

	if err != nil {
		http.Error(w, "Extraction failed: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
	fmt.Fprintf(w, "OK\nExtracted to %s", destPath)
}
