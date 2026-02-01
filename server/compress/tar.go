package compress

import (
	"archive/tar"
	"compress/gzip"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// CreateTar creates a TAR archive from multiple sources
func CreateTar(output string, sources []string) error {
	file, err := os.Create(output)
	if err != nil {
		return err
	}
	defer file.Close()

	// Check if output should be gzipped
	tarWriter := tar.NewWriter(file)
	defer tarWriter.Close()

	for _, source := range sources {
		// Get parent directory for relative path calculation
		srcParentDir := filepath.Dir(source)

		err = filepath.Walk(source, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}

			// Create header
			header, err := tar.FileInfoHeader(info, "")
			if err != nil {
				return err
			}

			// Calculate relative path from source's parent directory
			// This way the archive contains: srcBaseName/... or just srcBaseName for single file
			relPath, err := filepath.Rel(srcParentDir, path)
			if err != nil {
				return err
			}

			// Use forward slashes for tar archive (standard format)
			header.Name = filepath.ToSlash(relPath)

			// Skip directory entry for root folder (will be created implicitly)
			if path == source && info.IsDir() {
				return nil
			}

			// Write header
			if err := tarWriter.WriteHeader(header); err != nil {
				return err
			}

			// Write file content
			if !info.IsDir() {
				file, err := os.Open(path)
				if err != nil {
					return err
				}
				defer file.Close()
				_, err = io.Copy(tarWriter, file)
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

// ExtractTar extracts a TAR or TAR.GZ archive to destination
func ExtractTar(archive, dest string) error {
	file, err := os.Open(archive)
	if err != nil {
		return err
	}
	defer file.Close()

	// Get absolute destination path for security check
	absDest, err := filepath.Abs(dest)
	if err != nil {
		return err
	}

	var tarReader *tar.Reader

	// Check if gzipped
	if strings.HasSuffix(archive, ".gz") || strings.HasSuffix(archive, ".tgz") {
		gzReader, err := gzip.NewReader(file)
		if err != nil {
			return err
		}
		defer gzReader.Close()
		tarReader = tar.NewReader(gzReader)
	} else {
		tarReader = tar.NewReader(file)
	}

	for {
		header, err := tarReader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}

		if err := extractTarFile(tarReader, header, absDest); err != nil {
			return err
		}
	}

	return nil
}

// extractTarFile extracts a single file from tar archive
func extractTarFile(tarReader *tar.Reader, header *tar.Header, dest string) error {
	// Construct destination path
	path := filepath.Join(dest, header.Name)

	// Security check: prevent path traversal attack
	absPath, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	if !strings.HasPrefix(absPath, dest+string(filepath.Separator)) && absPath != dest {
		return fmt.Errorf("illegal file path: %s", header.Name)
	}

	// Create directory
	if header.Typeflag == tar.TypeDir {
		return os.MkdirAll(path, os.FileMode(header.Mode))
	}

	// Create parent directory
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}

	// Extract file
	destFile, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, os.FileMode(header.Mode))
	if err != nil {
		return err
	}
	defer destFile.Close()

	_, err = io.Copy(destFile, tarReader)
	return err
}

// CreateGzip creates a Gzip compressed file
func CreateGzip(output, source string) error {
	file, err := os.Create(output)
	if err != nil {
		return err
	}
	defer file.Close()

	writer := gzip.NewWriter(file)
	defer writer.Close()

	sourceFile, err := os.Open(source)
	if err != nil {
		return err
	}
	defer sourceFile.Close()

	_, err = io.Copy(writer, sourceFile)
	return err
}

// CreateTarGz creates a gzipped TAR archive from multiple sources
func CreateTarGz(output string, sources []string) error {
	file, err := os.Create(output)
	if err != nil {
		return err
	}
	defer file.Close()

	// Create gzip writer
	gzWriter := gzip.NewWriter(file)
	defer gzWriter.Close()

	// Create tar writer on top of gzip
	tarWriter := tar.NewWriter(gzWriter)
	defer tarWriter.Close()

	for _, source := range sources {
		// Get parent directory for relative path calculation
		srcParentDir := filepath.Dir(source)

		err = filepath.Walk(source, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}

			// Create header
			header, err := tar.FileInfoHeader(info, "")
			if err != nil {
				return err
			}

			// Calculate relative path from source's parent directory
			// This way the archive contains: srcBaseName/... or just srcBaseName for single file
			relPath, err := filepath.Rel(srcParentDir, path)
			if err != nil {
				return err
			}

			// Use forward slashes for tar archive (standard format)
			header.Name = filepath.ToSlash(relPath)

			// Skip directory entry for root folder (will be created implicitly)
			if path == source && info.IsDir() {
				return nil
			}

			// Write header
			if err := tarWriter.WriteHeader(header); err != nil {
				return err
			}

			// Write file content
			if !info.IsDir() {
				f, err := os.Open(path)
				if err != nil {
					return err
				}
				defer f.Close()
				_, err = io.Copy(tarWriter, f)
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
