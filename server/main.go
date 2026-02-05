package main

import (
	"crypto/sha256"
	"encoding/hex"
	"flag"
	"io"
	"log"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"

	"certstone.cc/simpleKcpFileManager/common"
	"certstone.cc/simpleKcpFileManager/server/handlers"

	"github.com/xtaci/kcp-go/v5"
	"github.com/xtaci/smux"
)

func main() {
	port := flag.String("p", "8080", "Port to listen")
	dir := flag.String("d", ".", "Directory to serve")
	key := flag.String("key", "", "Encryption key")
	flag.Parse()

	// Require encryption key
	if *key == "" {
		log.Fatal("\033[31m[ERROR] Encryption key is required. Please specify a key with -key parameter.\033[0m")
	}

	// KCP listener
	crypt, err := common.GetBlockCrypt(*key)
	if err != nil {
		log.Fatal("Failed to create encryption:", err)
	}
	listener, err := kcp.ListenWithOptions(":"+*port, crypt, 10, 3)
	if err != nil {
		log.Fatal(err)
	}

	log.Printf("KCP File Manager serving %s on :%s", *dir, *port)

	// Create handlers
	fileHandler := handlers.NewFileHandler(*dir)
	uploadHandler := handlers.NewUploadHandler(*dir)
	compressHandler := handlers.NewCompressHandler(*dir)
	editHandler := handlers.NewEditHandler(*dir)

	// Create main HTTP handler
	mainHandler := createMainHandler(*dir, fileHandler, uploadHandler, compressHandler, editHandler)

	for {
		conn, err := listener.AcceptKCP()
		if err != nil {
			continue
		}
		common.ConfigKCP(conn)

		go func(c *kcp.UDPSession) {
			mux, err := smux.Server(c, common.SmuxConfig())
			if err != nil {
				c.Close()
				return
			}
			defer mux.Close()

			smuxLis := &common.SmuxListener{Session: mux}

			// HTTP server with all handlers
			http.Serve(smuxLis, mainHandler)
		}(conn)
	}
}

// createMainHandler creates the main HTTP handler with all routes
func createMainHandler(rootDir string, fileHandler *handlers.FileHandler, uploadHandler *handlers.UploadHandler, compressHandler *handlers.CompressHandler, editHandler *handlers.EditHandler) http.Handler {
	mux := http.NewServeMux()

	// File download handler with checksum support
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		log.Printf("%s %s from %s", r.Method, r.URL.String(), r.RemoteAddr)

		action := r.URL.Query().Get("action")

		switch action {
		case "checksum":
			handleChecksum(rootDir, w, r)
		case "list":
			fileHandler.HandleList(w, r)
		case "delete":
			fileHandler.HandleDelete(w, r)
		case "mkdir":
			fileHandler.HandleMkdir(w, r)
		case "rename":
			fileHandler.HandleRename(w, r)
		case "copy":
			fileHandler.HandleCopy(w, r)
		case "stat":
			fileHandler.HandleStat(w, r)
		case "chmod":
			fileHandler.HandleChmod(w, r)
		case "compress":
			compressHandler.HandleCompress(w, r)
		case "extract":
			compressHandler.HandleExtract(w, r)
		case "edit":
			if r.Method == http.MethodGet {
				editHandler.HandleGetFile(w, r)
			} else if r.Method == http.MethodPut {
				editHandler.HandleSaveFile(w, r)
			} else {
				http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			}
		case "upload":
			uploadHandler.HandleUpload(w, r)
		default:
			// Default: serve file for download
			if r.Method == http.MethodGet || r.Method == http.MethodHead {
				http.FileServer(http.Dir(rootDir)).ServeHTTP(w, r)
			} else if r.Method == http.MethodPut {
				uploadHandler.HandleUpload(w, r)
			} else {
				http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			}
		}
	})

	return mux
}

// handleChecksum handles file checksum requests
func handleChecksum(root string, w http.ResponseWriter, r *http.Request) {
	filePath, safe := isPathSafe(root, r.URL.Path)
	if !safe {
		http.Error(w, "Invalid path", http.StatusBadRequest)
		return
	}

	sum, err := getFileChecksum(filePath)
	if err != nil {
		http.Error(w, "File not found or unreadable", http.StatusNotFound)
		return
	}

	w.Write([]byte(sum))
}

// isPathSafe checks if a path is safe (prevents directory traversal)
func isPathSafe(root, requestPath string) (string, bool) {
	// Clean path
	cleanPath := path.Clean("/" + requestPath)
	// Build full path
	fullPath := filepath.Join(root, filepath.FromSlash(cleanPath))
	// Get absolute paths
	absRoot, err := filepath.Abs(root)
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

// getFileChecksum calculates SHA256 checksum of a file
func getFileChecksum(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}

	return hex.EncodeToString(h.Sum(nil)), nil
}
