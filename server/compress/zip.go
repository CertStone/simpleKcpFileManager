package compress

import (
	"archive/zip"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// CreateZip creates a ZIP archive from multiple sources
func CreateZip(output string, sources []string) error {
	file, err := os.Create(output)
	if err != nil {
		return err
	}
	defer file.Close()

	zipWriter := zip.NewWriter(file)
	defer zipWriter.Close()

	for _, source := range sources {
		err = filepath.Walk(source, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}

			// Create header
			header, err := zip.FileInfoHeader(info)
			if err != nil {
				return err
			}

			// Calculate relative path from source
			relPath, err := filepath.Rel(source, path)
			if err != nil {
				return err
			}

			// Determine archive entry name
			var archiveName string
			if relPath == "." {
				// This is the root of what we're compressing
				// If it's a single file, use just the filename
				// If it's a directory, use the directory name as the root
				baseName := filepath.Base(source)
				if info.IsDir() {
					archiveName = baseName + "/"
				} else {
					archiveName = baseName
				}
			} else {
				// Nested path - combine base name with relative path
				baseName := filepath.Base(source)
				archiveName = filepath.ToSlash(filepath.Join(baseName, relPath))
			}

			header.Name = archiveName

			// Handle directory
			if info.IsDir() {
				header.Name += "/"
			}

			// Create writer
			writer, err := zipWriter.CreateHeader(header)
			if err != nil {
				return err
			}

			// Write file content
			if !info.IsDir() {
				file, err := os.Open(path)
				if err != nil {
					return err
				}
				defer file.Close()
				_, err = io.Copy(writer, file)
				if err != nil {
					return err
				}
			}

			return nil
		})

		if err != nil {
			return err
		}
	}

	return nil
}

// ExtractZip extracts a ZIP archive to destination
func ExtractZip(archive, dest string) error {
	zipReader, err := zip.OpenReader(archive)
	if err != nil {
		return err
	}
	defer zipReader.Close()

	// Get absolute destination path for security check
	absDest, err := filepath.Abs(dest)
	if err != nil {
		return err
	}

	for _, file := range zipReader.File {
		if err := extractZipFile(file, absDest); err != nil {
			return err
		}
	}

	return nil
}

// extractZipFile extracts a single file from zip archive
func extractZipFile(file *zip.File, dest string) error {
	// Construct destination path
	path := filepath.Join(dest, file.Name)

	// Security check: prevent Zip Slip attack
	absPath, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	if !strings.HasPrefix(absPath, dest+string(filepath.Separator)) && absPath != dest {
		return fmt.Errorf("illegal file path: %s", file.Name)
	}

	// Create directory
	if file.FileInfo().IsDir() {
		return os.MkdirAll(path, file.Mode())
	}

	// Create parent directory
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}

	// Extract file
	fileReader, err := file.Open()
	if err != nil {
		return err
	}
	defer fileReader.Close()

	destFile, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, file.Mode())
	if err != nil {
		return err
	}
	defer destFile.Close()

	_, err = io.Copy(destFile, fileReader)
	return err
}
