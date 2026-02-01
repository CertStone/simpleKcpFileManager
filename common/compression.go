package common

import (
	"archive/tar"
	"compress/gzip"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// CompressToTarGz compresses a file or folder to tar.gz format
// srcPath: source file or folder path
// dstPath: destination .tar.gz file path (should end with .tar.gz)
func CompressToTarGz(srcPath, dstPath string) error {
	// Ensure destination ends with .tar.gz
	if !strings.HasSuffix(dstPath, ".tar.gz") {
		dstPath += ".tar.gz"
	}

	// Create destination file
	dstFile, err := os.Create(dstPath)
	if err != nil {
		return fmt.Errorf("create destination file: %w", err)
	}
	defer dstFile.Close()

	// Create gzip writer
	gzw := gzip.NewWriter(dstFile)
	defer gzw.Close()

	// Create tar writer
	tw := tar.NewWriter(gzw)
	defer tw.Close()

	// Get the parent directory of source to calculate relative paths
	// This ensures consistent behavior: tar always contains the source name as root
	srcParentDir := filepath.Dir(srcPath)

	// Walk through source path
	err = filepath.Walk(srcPath, func(file string, fi os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		// Create tar header
		header, err := tar.FileInfoHeader(fi, "")
		if err != nil {
			return fmt.Errorf("create header: %w", err)
		}

		// Calculate relative path from source's parent directory
		// This way the archive contains: srcBaseName/... or just srcBaseName for single file
		relPath, err := filepath.Rel(srcParentDir, file)
		if err != nil {
			return fmt.Errorf("calculate relative path: %w", err)
		}

		// Use forward slashes for tar archive (standard format)
		header.Name = relPath
		if filepath.Separator != '/' {
			header.Name = strings.ReplaceAll(header.Name, string(filepath.Separator), "/")
		}

		// Skip directory entry for root folder (will be created implicitly)
		if file == srcPath && fi.IsDir() {
			return nil
		}

		// Write header
		if err := tw.WriteHeader(header); err != nil {
			return fmt.Errorf("write header: %w", err)
		}

		// Write file content if not a directory
		if !fi.IsDir() {
			fileObj, err := os.Open(file)
			if err != nil {
				return fmt.Errorf("open file: %w", err)
			}
			defer fileObj.Close()

			if _, err := io.Copy(tw, fileObj); err != nil {
				return fmt.Errorf("write file content: %w", err)
			}
		}

		return nil
	})

	if err != nil {
		return fmt.Errorf("walk source path: %w", err)
	}

	return nil
}

// DecompressFromTarGz decompresses a tar.gz file to destination folder
// srcPath: source .tar.gz file path
// dstPath: destination folder path (will be created if not exists)
func DecompressFromTarGz(srcPath, dstPath string) error {
	// Open source file
	srcFile, err := os.Open(srcPath)
	if err != nil {
		return fmt.Errorf("open source file: %w", err)
	}
	defer srcFile.Close()

	// Create gzip reader
	gzr, err := gzip.NewReader(srcFile)
	if err != nil {
		return fmt.Errorf("create gzip reader: %w", err)
	}
	defer gzr.Close()

	// Create tar reader
	tr := tar.NewReader(gzr)

	// Create destination directory
	if err := os.MkdirAll(dstPath, 0755); err != nil {
		return fmt.Errorf("create destination directory: %w", err)
	}

	// Get absolute destination path for security check
	absDest, err := filepath.Abs(dstPath)
	if err != nil {
		return fmt.Errorf("get absolute dest path: %w", err)
	}

	// Extract files
	for {
		header, err := tr.Next()
		if err == io.EOF {
			break // End of archive
		}
		if err != nil {
			return fmt.Errorf("read tar header: %w", err)
		}

		// Build target path
		targetPath := filepath.Join(dstPath, header.Name)

		// Security check: prevent path traversal attack (Tar Slip)
		absPath, err := filepath.Abs(targetPath)
		if err != nil {
			return fmt.Errorf("get absolute path: %w", err)
		}
		if !strings.HasPrefix(absPath, absDest+string(filepath.Separator)) && absPath != absDest {
			return fmt.Errorf("illegal file path (tar slip detected): %s", header.Name)
		}

		// Create directory or file
		switch header.Typeflag {
		case tar.TypeDir:
			// Create directory
			if err := os.MkdirAll(targetPath, os.FileMode(header.Mode)); err != nil {
				return fmt.Errorf("create directory: %w", err)
			}

		case tar.TypeReg, tar.TypeRegA:
			// Create file
			if err := os.MkdirAll(filepath.Dir(targetPath), 0755); err != nil {
				return fmt.Errorf("create parent directory: %w", err)
			}

			fileObj, err := os.OpenFile(targetPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, os.FileMode(header.Mode))
			if err != nil {
				return fmt.Errorf("create file: %w", err)
			}

			if _, err := io.Copy(fileObj, tr); err != nil {
				fileObj.Close()
				return fmt.Errorf("write file content: %w", err)
			}
			fileObj.Close()

		default:
			// Skip unsupported types (symlinks, etc.)
		}
	}

	return nil
}

// ShouldCompressFile returns true if file size exceeds threshold
func ShouldCompressFile(filePath string, thresholdBytes int64) bool {
	info, err := os.Stat(filePath)
	if err != nil {
		return false
	}
	return !info.IsDir() && info.Size() >= thresholdBytes
}

// IsTarGzFile checks if a file is a tar.gz archive
func IsTarGzFile(filePath string) bool {
	return strings.HasSuffix(filePath, ".tar.gz") || strings.HasSuffix(filePath, ".tgz")
}
