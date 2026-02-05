package client

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"certstone.cc/simpleKcpFileManager/common"

	"github.com/xtaci/kcp-go/v5"
	"github.com/xtaci/smux"
)

// Client represents the KCP file manager client
type Client struct {
	serverAddr string
	key        string
	session    *smux.Session
	sessionMu  sync.Mutex
	httpClient *http.Client
}

// ListItem represents a file or directory
type ListItem struct {
	Name    string `json:"name"`
	Path    string `json:"path"`
	Size    int64  `json:"size"`
	ModTime int64  `json:"modTime"`
	IsDir   bool   `json:"isDir"`
	Mode    string `json:"mode"`
}

const (
	connectionTimeout = 3 * time.Second
	defaultChunkSize  = 4 * 1024 * 1024 // 4MB
)

// NewClient creates a new file manager client
func NewClient(serverAddr, key string) *Client {
	return &Client{
		serverAddr: serverAddr,
		key:        key,
	}
}

// Connect establishes a KCP connection to the server
func (c *Client) Connect() error {
	c.sessionMu.Lock()
	defer c.sessionMu.Unlock()

	if c.session != nil && !c.session.IsClosed() {
		return nil // Already connected
	}

	// Create KCP connection with timeout
	ctx, cancel := context.WithTimeout(context.Background(), connectionTimeout)
	defer cancel()

	type connResult struct {
		session *smux.Session
		err     error
	}
	resultCh := make(chan connResult, 1)

	go func() {
		crypt, err := common.GetBlockCrypt(c.key)
		if err != nil {
			resultCh <- connResult{err: fmt.Errorf("failed to create encryption: %w", err)}
			return
		}

		kcpConn, err := kcp.DialWithOptions(c.serverAddr, crypt, 10, 3)
		if err != nil {
			resultCh <- connResult{err: err}
			return
		}
		common.ConfigKCP(kcpConn)

		session, err := smux.Client(kcpConn, common.SmuxConfig())
		if err != nil {
			kcpConn.Close()
			resultCh <- connResult{err: err}
			return
		}

		// Test connection
		testStream, err := session.OpenStream()
		if err != nil {
			session.Close()
			resultCh <- connResult{err: fmt.Errorf("open stream failed: %w", err)}
			return
		}

		testStream.SetDeadline(time.Now().Add(connectionTimeout))
		_, err = testStream.Write([]byte("HEAD / HTTP/1.1\r\nHost: test\r\nConnection: close\r\n\r\n"))
		if err != nil {
			testStream.Close()
			session.Close()
			resultCh <- connResult{err: fmt.Errorf("connection failed: %w", err)}
			return
		}

		buf := make([]byte, 1)
		_, err = testStream.Read(buf)
		testStream.Close()
		if err != nil {
			session.Close()
			resultCh <- connResult{err: fmt.Errorf("connection failed (possibly wrong key): %w", err)}
			return
		}

		select {
		case <-ctx.Done():
			session.Close()
			return
		case resultCh <- connResult{session: session}:
		}
	}()

	select {
	case result := <-resultCh:
		if result.err != nil {
			return result.err
		}
		c.session = result.session
		c.setupHTTPClient()
		return nil
	case <-ctx.Done():
		return fmt.Errorf("connection timeout (server unreachable or wrong key)")
	}
}

// setupHTTPClient configures the HTTP client to use the KCP session
func (c *Client) setupHTTPClient() {
	dialer := func(ctx context.Context, network, addr string) (net.Conn, error) {
		c.sessionMu.Lock()
		session := c.session
		c.sessionMu.Unlock()

		if session == nil || session.IsClosed() {
			return nil, fmt.Errorf("not connected")
		}
		return session.OpenStream()
	}
	c.httpClient = &http.Client{
		Transport: &http.Transport{DialContext: dialer},
		Timeout:   0, // No timeout for long transfers
	}
}

// Close closes the connection
func (c *Client) Close() error {
	c.sessionMu.Lock()
	defer c.sessionMu.Unlock()
	if c.session != nil && !c.session.IsClosed() {
		return c.session.Close()
	}
	return nil
}

// IsConnected returns true if connected to server
func (c *Client) IsConnected() bool {
	c.sessionMu.Lock()
	defer c.sessionMu.Unlock()
	return c.session != nil && !c.session.IsClosed()
}

// ListFiles lists files in the specified directory
func (c *Client) ListFiles(relPath string, recursive bool) ([]ListItem, error) {
	log.Printf("[DEBUG] Client.ListFiles: START relPath=%s recursive=%v", relPath, recursive)
	if !c.IsConnected() {
		log.Printf("[DEBUG] Client.ListFiles: Not connected")
		return nil, fmt.Errorf("not connected")
	}

	q := "?action=list"
	if relPath != "" {
		q += "&path=" + url.QueryEscape(relPath)
	}
	if recursive {
		q += "&recursive=1"
	}

	url := fmt.Sprintf("http://%s/%s", c.serverAddr, q)
	log.Printf("[DEBUG] Client.ListFiles: GET %s", url)

	resp, err := c.httpClient.Get(url)
	if err != nil {
		log.Printf("[DEBUG] Client.ListFiles: GET failed - %v", err)
		return nil, err
	}
	defer resp.Body.Close()

	log.Printf("[DEBUG] Client.ListFiles: Response status=%d", resp.StatusCode)
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("server returned status %d", resp.StatusCode)
	}

	var files []ListItem
	if err := json.NewDecoder(resp.Body).Decode(&files); err != nil {
		log.Printf("[DEBUG] Client.ListFiles: JSON decode failed - %v", err)
		return nil, err
	}
	log.Printf("[DEBUG] Client.ListFiles: Got %d items", len(files))
	return files, nil
}

// progressReader wraps a reader to track progress
type progressReader struct {
	reader     io.Reader
	total      int64
	written    int64
	onProgress func(written int64, total int64)
}

func (pr *progressReader) Read(p []byte) (int, error) {
	n, err := pr.reader.Read(p)
	if n > 0 {
		pr.written += int64(n)
		if pr.onProgress != nil {
			pr.onProgress(pr.written, pr.total)
		}
	}
	return n, err
}

// UploadFile uploads a file to the server with multi-threading support
func (c *Client) UploadFile(localPath, remotePath string, onProgress func(written int64, total int64)) error {
	if !c.IsConnected() {
		return fmt.Errorf("not connected")
	}

	// Get file size
	file, err := os.Open(localPath)
	if err != nil {
		return fmt.Errorf("open file: %w", err)
	}
	defer file.Close()

	info, err := file.Stat()
	if err != nil {
		return fmt.Errorf("stat file: %w", err)
	}
	fileSize := info.Size()

	// For small files (< 4MB), use single-threaded upload
	if fileSize < defaultChunkSize {
		return c.uploadFileSingle(localPath, remotePath, onProgress)
	}

	// Multi-threaded upload for larger files
	return c.uploadFileParallel(localPath, remotePath, fileSize, onProgress)
}

// uploadFileSingle uploads a file using single thread (for small files)
func (c *Client) uploadFileSingle(localPath, remotePath string, onProgress func(written int64, total int64)) error {
	file, err := os.Open(localPath)
	if err != nil {
		return fmt.Errorf("open file: %w", err)
	}
	defer file.Close()

	info, err := file.Stat()
	if err != nil {
		return fmt.Errorf("stat file: %w", err)
	}
	fileSize := info.Size()

	// Wrap reader with progress tracking
	pr := &progressReader{
		reader:     file,
		total:      fileSize,
		onProgress: onProgress,
	}

	// Create request
	url := fmt.Sprintf("http://%s?action=upload&path=%s", c.serverAddr, url.QueryEscape(remotePath))
	req, err := http.NewRequest("PUT", url, pr)
	if err != nil {
		return err
	}
	req.ContentLength = fileSize

	// Execute request
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("upload failed (status %d): %s", resp.StatusCode, string(body))
	}

	return nil
}

// uploadFileParallel uploads a file using multiple parallel threads
func (c *Client) uploadFileParallel(localPath, remotePath string, fileSize int64, onProgress func(written int64, total int64)) error {
	const numWorkers = 8
	chunkSize := (fileSize + int64(numWorkers) - 1) / int64(numWorkers)

	// Align chunk size to defaultChunkSize (4MB) boundary
	if chunkSize < defaultChunkSize {
		chunkSize = defaultChunkSize
	}

	// Calculate number of chunks
	numChunks := (fileSize + chunkSize - 1) / chunkSize

	log.Printf("[DEBUG] Parallel upload: size=%d, chunks=%d, chunkSize=%d", fileSize, numChunks, chunkSize)

	type chunkResult struct {
		index int
		err   error
	}

	results := make(chan chunkResult, numChunks)
	var bytesDone atomic.Int64

	// Progress reporter
	var lastProgress int64
	progressDone := make(chan struct{})
	go func() {
		ticker := time.NewTicker(500 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				done := bytesDone.Load()
				if onProgress != nil && done != lastProgress {
					onProgress(done, fileSize)
					lastProgress = done
				}
			case <-progressDone:
				return
			}
		}
	}()

	// Start workers
	var wg sync.WaitGroup
	for i := int64(0); i < numChunks; i++ {
		wg.Add(1)
		go func(chunkIndex int64) {
			defer wg.Done()

			start := chunkIndex * chunkSize
			end := start + chunkSize
			if end > fileSize {
				end = fileSize
			}

			// Upload chunk
			err := c.uploadChunk(localPath, remotePath, start, end, fileSize, chunkIndex)

			if err == nil {
				bytesDone.Add(end - start)
			}

			results <- chunkResult{index: int(chunkIndex), err: err}
		}(i)
	}

	// Wait for all chunks
	go func() {
		wg.Wait()
		close(results)
	}()

	// Collect results
	for result := range results {
		if result.err != nil {
			close(progressDone)
			return fmt.Errorf("chunk %d failed: %w", result.index, result.err)
		}
	}

	close(progressDone)

	// Final progress update
	if onProgress != nil {
		onProgress(fileSize, fileSize)
	}

	return nil
}

// uploadChunk uploads a single chunk
func (c *Client) uploadChunk(localPath, remotePath string, start, end, fileSize int64, chunkIndex int64) error {
	file, err := os.Open(localPath)
	if err != nil {
		return err
	}
	defer file.Close()

	// Seek to chunk position
	if _, err := file.Seek(start, 0); err != nil {
		return err
	}

	// Create limited reader for this chunk
	chunkReader := io.LimitReader(file, end-start)

	// Create request with Content-Range header
	url := fmt.Sprintf("http://%s?action=upload&path=%s", c.serverAddr, url.QueryEscape(remotePath))
	req, err := http.NewRequest("PUT", url, chunkReader)
	if err != nil {
		return err
	}

	// Set Content-Range header for chunked upload
	req.Header.Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, end-1, fileSize))
	req.ContentLength = end - start

	// Execute request
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("chunk upload failed (status %d): %s", resp.StatusCode, string(body))
	}

	return nil
}

// DownloadFile downloads a file from the server with resume support and multi-threading
func (c *Client) DownloadFile(remotePath, localPath string, onProgress func(percent float64, speedMBps float64)) error {
	if !c.IsConnected() {
		return fmt.Errorf("not connected")
	}

	// HEAD request to get file size
	url := fmt.Sprintf("http://%s%s", c.serverAddr, remotePath)
	headReq, _ := http.NewRequest("HEAD", url, nil)
	headResp, err := c.httpClient.Do(headReq)
	if err != nil {
		return fmt.Errorf("head request failed: %w", err)
	}
	headResp.Body.Close()

	fileSize := headResp.ContentLength
	if fileSize <= 0 {
		return fmt.Errorf("unknown file size")
	}

	// For small files (< 4MB), use single-threaded download
	if fileSize < defaultChunkSize {
		return c.downloadFileSingle(remotePath, localPath, onProgress)
	}

	// Multi-threaded download for larger files
	return c.downloadFileParallel(remotePath, localPath, fileSize, onProgress)
}

// downloadFileSingle downloads a file using single thread (for small files)
func (c *Client) downloadFileSingle(remotePath, localPath string, onProgress func(percent float64, speedMBps float64)) error {
	// Check for partial download
	var startByte int64
	if info, err := os.Stat(localPath); err == nil {
		startByte = info.Size()
	}

	url := fmt.Sprintf("http://%s%s", c.serverAddr, remotePath)

	// HEAD to get file size
	headReq, _ := http.NewRequest("HEAD", url, nil)
	headResp, err := c.httpClient.Do(headReq)
	if err != nil {
		return fmt.Errorf("head request failed: %w", err)
	}
	headResp.Body.Close()

	fileSize := headResp.ContentLength

	// Check if already complete
	if startByte >= fileSize {
		return nil
	}

	// Open file
	if err := os.MkdirAll(filepath.Dir(localPath), 0755); err != nil {
		return fmt.Errorf("create directory: %w", err)
	}

	var file *os.File
	if startByte > 0 {
		file, err = os.OpenFile(localPath, os.O_RDWR, 0644)
	} else {
		file, err = os.OpenFile(localPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
	}
	if err != nil {
		return fmt.Errorf("open file: %w", err)
	}
	defer file.Close()

	// Download
	req, _ := http.NewRequest("GET", url, nil)
	if startByte > 0 {
		req.Header.Set("Range", fmt.Sprintf("bytes=%d-", startByte))
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("download request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusPartialContent {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("download failed (status %d): %s", resp.StatusCode, string(bodyBytes))
	}

	if startByte > 0 {
		if _, err := file.Seek(startByte, 0); err != nil {
			return fmt.Errorf("seek error: %w", err)
		}
	}

	// Copy with progress tracking
	startTime := time.Now()
	downloaded := startByte
	buf := make([]byte, 32*1024)
	for {
		n, readErr := resp.Body.Read(buf)
		if n > 0 {
			_, writeErr := file.Write(buf[:n])
			if writeErr != nil {
				return fmt.Errorf("write error: %w", writeErr)
			}
			downloaded += int64(n)
			if onProgress != nil {
				percent := float64(downloaded) / float64(fileSize)
				elapsed := time.Since(startTime).Seconds()
				var speed float64
				if elapsed > 0 {
					speed = (float64(downloaded) / (1024 * 1024)) / elapsed
				}
				onProgress(percent, speed)
			}
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return fmt.Errorf("read error: %w", readErr)
		}
	}

	return c.verifyChecksum(remotePath, localPath)
}

// downloadFileParallel downloads a file using multiple parallel threads
func (c *Client) downloadFileParallel(remotePath, localPath string, fileSize int64, onProgress func(percent float64, speedMBps float64)) error {
	const maxWorkers = 8
	chunkSize := (fileSize + int64(maxWorkers) - 1) / int64(maxWorkers)

	// Align chunk size to defaultChunkSize (4MB) boundary for efficiency
	if chunkSize < defaultChunkSize {
		chunkSize = defaultChunkSize
	}

	// Calculate number of chunks needed
	numChunks := (fileSize + chunkSize - 1) / chunkSize

	log.Printf("[DEBUG] Parallel download: size=%d, chunks=%d, chunkSize=%d", fileSize, numChunks, chunkSize)

	// Create output directory
	if err := os.MkdirAll(filepath.Dir(localPath), 0755); err != nil {
		return fmt.Errorf("create directory: %w", err)
	}

	// Create temp directory for chunk files
	tempDir := filepath.Dir(localPath) + "/.tmp_" + filepath.Base(localPath)
	os.RemoveAll(tempDir) // Clean up any previous attempt
	if err := os.MkdirAll(tempDir, 0755); err != nil {
		return fmt.Errorf("create temp directory: %w", err)
	}
	defer os.RemoveAll(tempDir)

	type chunkResult struct {
		index int
		err   error
	}

	results := make(chan chunkResult, numChunks)
	var bytesDone atomic.Int64
	startTime := time.Now()

	// Progress reporter
	progressDone := make(chan struct{})
	go func() {
		ticker := time.NewTicker(500 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				done := bytesDone.Load()
				if onProgress != nil {
					percent := float64(done) / float64(fileSize)
					elapsed := time.Since(startTime).Seconds()
					var speed float64
					if elapsed > 0 {
						speed = (float64(done) / (1024 * 1024)) / elapsed
					}
					onProgress(percent, speed)
				}
			case <-progressDone:
				return
			}
		}
	}()

	// Limit concurrent workers
	semaphore := make(chan struct{}, maxWorkers)
	var wg sync.WaitGroup

	// Download chunks to separate temp files
	for i := int64(0); i < numChunks; i++ {
		wg.Add(1)
		go func(chunkIndex int64) {
			defer wg.Done()

			// Acquire semaphore slot
			semaphore <- struct{}{}
			defer func() { <-semaphore }()

			start := chunkIndex * chunkSize
			end := start + chunkSize
			if end > fileSize {
				end = fileSize
			}

			// Download chunk to separate temp file
			chunkFile := fmt.Sprintf("%s/chunk_%04d.tmp", tempDir, chunkIndex)
			err := c.downloadChunkToFile(remotePath, chunkFile, start, end)

			if err == nil {
				bytesDone.Add(end - start)
			}

			results <- chunkResult{index: int(chunkIndex), err: err}
		}(i)
	}

	// Wait for all chunks
	go func() {
		wg.Wait()
		close(results)
	}()

	// Collect results
	for result := range results {
		if result.err != nil {
			close(progressDone)
			return fmt.Errorf("chunk %d failed: %w", result.index, result.err)
		}
	}

	close(progressDone)

	// Merge chunk files into final file
	if err := c.mergeChunks(tempDir, localPath, numChunks); err != nil {
		return err
	}

	// Verify checksum
	return c.verifyChecksum(remotePath, localPath)
}

// downloadChunkToFile downloads a chunk to a separate temp file
func (c *Client) downloadChunkToFile(remotePath, chunkFile string, start, end int64) error {
	url := fmt.Sprintf("http://%s%s", c.serverAddr, remotePath)

	req, _ := http.NewRequest("GET", url, nil)
	req.Header.Set("Range", fmt.Sprintf("bytes=%d-%d", start, end-1))

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusPartialContent && resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("chunk download failed (status %d): %s", resp.StatusCode, string(bodyBytes))
	}

	// Create chunk file
	file, err := os.Create(chunkFile)
	if err != nil {
		return err
	}
	defer file.Close()

	// Write chunk data
	_, err = io.Copy(file, resp.Body)
	return err
}

// mergeChunks merges chunk temp files into the final file
func (c *Client) mergeChunks(tempDir, finalFile string, numChunks int64) error {
	// Create final file
	outFile, err := os.Create(finalFile)
	if err != nil {
		return fmt.Errorf("create final file: %w", err)
	}
	defer outFile.Close()

	// Merge chunks in order
	for i := int64(0); i < numChunks; i++ {
		chunkFile := fmt.Sprintf("%s/chunk_%04d.tmp", tempDir, i)
		data, err := os.ReadFile(chunkFile)
		if err != nil {
			outFile.Close()
			os.Remove(finalFile)
			return fmt.Errorf("read chunk %d: %w", i, err)
		}

		if _, err := outFile.Write(data); err != nil {
			outFile.Close()
			os.Remove(finalFile)
			return fmt.Errorf("write chunk %d: %w", i, err)
		}
	}

	return nil
}

// verifyChecksum verifies file integrity using SHA256
func (c *Client) verifyChecksum(remotePath, localPath string) error {
	// Get remote checksum
	url := fmt.Sprintf("http://%s%s?action=checksum", c.serverAddr, remotePath)
	resp, err := c.httpClient.Get(url)
	if err != nil {
		return fmt.Errorf("get remote checksum: %w", err)
	}
	defer resp.Body.Close()

	remoteHash, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read remote checksum: %w", err)
	}

	// Calculate local checksum
	localHash, err := calcFileChecksum(localPath)
	if err != nil {
		return fmt.Errorf("calculate local checksum: %w", err)
	}

	if string(remoteHash) != localHash {
		return fmt.Errorf("checksum mismatch")
	}

	return nil
}

// DeleteFile deletes a file or directory on the server
func (c *Client) DeleteFile(path string) error {
	if !c.IsConnected() {
		return fmt.Errorf("not connected")
	}

	url := fmt.Sprintf("http://%s?action=delete&path=%s", c.serverAddr, url.QueryEscape(path))
	req, err := http.NewRequest("DELETE", url, nil)
	if err != nil {
		return err
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("delete failed (status %d): %s", resp.StatusCode, string(body))
	}

	return nil
}

// CreateDirectory creates a directory on the server
func (c *Client) CreateDirectory(path string) error {
	if !c.IsConnected() {
		return fmt.Errorf("not connected")
	}

	url := fmt.Sprintf("http://%s?action=mkdir&path=%s", c.serverAddr, url.QueryEscape(path))
	resp, err := c.httpClient.Post(url, "application/json", nil)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("mkdir failed (status %d): %s", resp.StatusCode, string(body))
	}

	return nil
}

// RenameFile renames a file or directory on the server
func (c *Client) RenameFile(oldPath, newPath string) error {
	if !c.IsConnected() {
		return fmt.Errorf("not connected")
	}

	url := fmt.Sprintf("http://%s?action=rename&old=%s&new=%s", c.serverAddr, url.QueryEscape(oldPath), url.QueryEscape(newPath))
	resp, err := c.httpClient.Post(url, "application/json", nil)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("rename failed (status %d): %s", resp.StatusCode, string(body))
	}

	return nil
}

// CopyFile copies a file or directory on the server
func (c *Client) CopyFile(srcPath, dstPath string) error {
	if !c.IsConnected() {
		return fmt.Errorf("not connected")
	}

	url := fmt.Sprintf("http://%s?action=copy&src=%s&dst=%s", c.serverAddr, url.QueryEscape(srcPath), url.QueryEscape(dstPath))
	resp, err := c.httpClient.Post(url, "application/json", nil)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("copy failed (status %d): %s", resp.StatusCode, string(body))
	}

	return nil
}

// MoveFile moves a file or directory on the server (alias for RenameFile)
func (c *Client) MoveFile(srcPath, dstPath string) error {
	return c.RenameFile(srcPath, dstPath)
}

// ReadFile reads a text file from the server
func (c *Client) ReadFile(path string) (string, error) {
	if !c.IsConnected() {
		return "", fmt.Errorf("not connected")
	}

	url := fmt.Sprintf("http://%s?action=edit&path=%s", c.serverAddr, url.QueryEscape(path))
	resp, err := c.httpClient.Get(url)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("read failed (status %d): %s", resp.StatusCode, string(body))
	}

	content, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	return string(content), nil
}

// SaveFile saves content to a text file on the server
func (c *Client) SaveFile(path string, content string) error {
	if !c.IsConnected() {
		return fmt.Errorf("not connected")
	}

	url := fmt.Sprintf("http://%s?action=edit&path=%s", c.serverAddr, url.QueryEscape(path))
	req, err := http.NewRequest("PUT", url, bytes.NewReader([]byte(content)))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "text/plain; charset=utf-8")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("save failed (status %d): %s", resp.StatusCode, string(body))
	}

	return nil
}

// Compress compresses files/folders on the server
func (c *Client) Compress(paths []string, outputPath, format string) error {
	if !c.IsConnected() {
		return fmt.Errorf("not connected")
	}

	pathsStr := ""
	for i, p := range paths {
		if i > 0 {
			pathsStr += ","
		}
		pathsStr += p
	}

	url := fmt.Sprintf("http://%s?action=compress&paths=%s&output=%s&format=%s",
		c.serverAddr, url.QueryEscape(pathsStr), url.QueryEscape(outputPath), format)
	resp, err := c.httpClient.Post(url, "application/json", nil)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("compress failed (status %d): %s", resp.StatusCode, string(body))
	}

	return nil
}

// Extract extracts an archive on the server
func (c *Client) Extract(archivePath, destPath string) error {
	if !c.IsConnected() {
		return fmt.Errorf("not connected")
	}

	dest := destPath
	if dest == "" {
		dest = archivePath[:len(archivePath)-len(filepath.Ext(archivePath))]
	}

	url := fmt.Sprintf("http://%s?action=extract&path=%s&dest=%s",
		c.serverAddr, url.QueryEscape(archivePath), url.QueryEscape(dest))
	resp, err := c.httpClient.Post(url, "application/json", nil)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("extract failed (status %d): %s", resp.StatusCode, string(body))
	}

	return nil
}

// calcFileChecksum calculates SHA256 checksum of a local file
func calcFileChecksum(path string) (string, error) {
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

// PackTransferConfig holds configuration for pack transfer feature
type PackTransferConfig struct {
	Enabled        bool  // Enable pack transfer
	ThresholdBytes int64 // File size threshold (default: 10MB)
}

// DefaultPackTransferConfig returns default pack transfer configuration
func DefaultPackTransferConfig() PackTransferConfig {
	return PackTransferConfig{
		Enabled:        false,
		ThresholdBytes: 10 * 1024 * 1024, // 10MB
	}
}

// UploadFilePacked uploads a file or folder with optional compression
// If pack transfer is enabled and path is a folder or large file, it will be compressed first
func (c *Client) UploadFilePacked(localPath, remotePath string, config PackTransferConfig, onProgress func(written int64, total int64)) error {
	// Check if pack transfer is enabled
	if !config.Enabled {
		return c.UploadFile(localPath, remotePath, onProgress)
	}

	// Check if it's a directory
	info, err := os.Stat(localPath)
	if err != nil {
		return fmt.Errorf("stat path: %w", err)
	}

	shouldCompress := false

	if info.IsDir() {
		// Always compress directories
		shouldCompress = true
	} else if info.Size() >= config.ThresholdBytes {
		// Compress large files
		shouldCompress = true
	}

	if shouldCompress {
		log.Printf("[DEBUG] Pack transfer enabled, compressing before upload: %s", localPath)

		// Create temporary tar.gz file in system temp directory for security
		tempFile := filepath.Join(os.TempDir(), fmt.Sprintf("pack_upload_%d.tar.gz", time.Now().UnixNano()))

		// Compress
		if err := common.CompressToTarGz(localPath, tempFile); err != nil {
			return fmt.Errorf("compress before upload: %w", err)
		}

		// Upload compressed file to server with .tar.gz extension
		remotePathPacked := remotePath + ".tar.gz"

		// Upload with auto-extract header
		if !c.IsConnected() {
			os.Remove(tempFile) // Cleanup on early return
			return fmt.Errorf("not connected")
		}

		file, err := os.Open(tempFile)
		if err != nil {
			os.Remove(tempFile) // Cleanup on early return
			return fmt.Errorf("open compressed file: %w", err)
		}

		// Use a function to ensure proper cleanup order:
		// 1. Close file handle first
		// 2. Then delete the file
		uploadAndCleanup := func() error {
			defer func() {
				file.Close()        // Close file handle first
				os.Remove(tempFile) // Then remove temp file
				log.Printf("[DEBUG] Pack upload temp file cleaned up: %s", tempFile)
			}()

			fileInfo, _ := file.Stat()
			fileSize := fileInfo.Size()

			// Wrap reader with progress tracking
			pr := &progressReader{
				reader:     file,
				total:      fileSize,
				onProgress: onProgress,
			}

			// Create request with auto-extract header
			url := fmt.Sprintf("http://%s?action=upload&path=%s", c.serverAddr, url.QueryEscape(remotePathPacked))
			req, err := http.NewRequest("PUT", url, pr)
			if err != nil {
				return err
			}
			req.Header.Set("X-Auto-Extract", "1") // Tell server to auto-extract
			req.ContentLength = fileSize

			// Execute request
			resp, err := c.httpClient.Do(req)
			if err != nil {
				return err
			}
			defer resp.Body.Close()

			if resp.StatusCode != http.StatusOK {
				body, _ := io.ReadAll(resp.Body)
				return fmt.Errorf("upload failed (status %d): %s", resp.StatusCode, string(body))
			}

			return nil
		}

		if err := uploadAndCleanup(); err != nil {
			return err
		}

		log.Printf("[DEBUG] Pack transfer upload completed: %s -> %s", localPath, remotePath)
		return nil
	}

	// No compression needed, use regular upload
	return c.UploadFile(localPath, remotePath, onProgress)
}

// DownloadFilePacked downloads a file or folder with optional server-side compression
func (c *Client) DownloadFilePacked(remotePath, localPath string, config PackTransferConfig, onProgress func(percent float64, speedMBps float64)) error {
	if !c.IsConnected() {
		return fmt.Errorf("not connected")
	}

	// Check if remote path is a directory
	// First, get file info to determine if it's a directory
	statURL := fmt.Sprintf("http://%s?action=stat&path=%s", c.serverAddr, url.QueryEscape(remotePath))
	statResp, err := c.httpClient.Get(statURL)
	if err != nil {
		// If stat fails, try regular download
		return c.DownloadFile(remotePath, localPath, onProgress)
	}
	defer statResp.Body.Close()

	if statResp.StatusCode != http.StatusOK {
		// Stat failed, try regular download
		return c.DownloadFile(remotePath, localPath, onProgress)
	}

	var statInfo struct {
		Size  int64 `json:"size"`
		IsDir bool  `json:"isDir"`
	}

	if err := json.NewDecoder(statResp.Body).Decode(&statInfo); err != nil {
		// JSON decode failed, try regular download
		return c.DownloadFile(remotePath, localPath, onProgress)
	}

	// Determine if we should use pack transfer
	shouldCompress := false

	if config.Enabled {
		if statInfo.IsDir {
			// Always compress directories
			shouldCompress = true
		} else if statInfo.Size >= config.ThresholdBytes {
			// Compress large files
			shouldCompress = true
		}
	}

	if shouldCompress {
		log.Printf("[DEBUG] Pack transfer enabled, requesting server compression: %s", remotePath)

		// Request server to compress the file/folder
		// We'll use the compress action to create a tar.gz on the server
		compressURL := fmt.Sprintf("http://%s?action=compress&paths=%s&output=%s.tar.gz&format=targz",
			c.serverAddr, url.QueryEscape(remotePath), url.QueryEscape(remotePath))

		// Use POST for compress action
		resp, err := c.httpClient.Post(compressURL, "application/json", nil)
		if err != nil {
			return fmt.Errorf("request compression: %w", err)
		}
		resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			// Compression failed, fall back to regular download
			log.Printf("[DEBUG] Server compression failed, falling back to regular download")
			return c.DownloadFile(remotePath, localPath, onProgress)
		}

		// Download the compressed file to system temp directory
		tempTarGz := filepath.Join(os.TempDir(), fmt.Sprintf("pack_download_%d.tar.gz", time.Now().UnixNano()))

		if err := c.DownloadFile(remotePath+".tar.gz", tempTarGz, onProgress); err != nil {
			return fmt.Errorf("download compressed file: %w", err)
		}

		// Extract the downloaded file to an appropriate destination
		// For files, extract to parent directory so the file lands at localPath
		// For folders, avoid double nesting by checking the base name
		extractDest := filepath.Dir(localPath)
		if statInfo.IsDir {
			remoteBase := filepath.Base(remotePath)
			localBase := filepath.Base(localPath)
			if localBase != remoteBase {
				extractDest = localPath
			}
		}
		if err := common.DecompressFromTarGz(tempTarGz, extractDest); err != nil {
			return fmt.Errorf("extract downloaded file: %w", err)
		}

		// Remove temporary tar.gz file
		if err := os.Remove(tempTarGz); err != nil {
			log.Printf("[DEBUG] Warning: failed to remove temporary file: %v", err)
		}

		// Clean up server-side temporary tar.gz
		deleteURL := fmt.Sprintf("http://%s?action=delete&path=%s.tar.gz", c.serverAddr, url.QueryEscape(remotePath))
		req, _ := http.NewRequest("DELETE", deleteURL, nil)
		c.httpClient.Do(req)

		log.Printf("[DEBUG] Pack transfer download completed: %s -> %s", remotePath, localPath)
		return nil
	}

	// No compression needed, use regular download
	return c.DownloadFile(remotePath, localPath, onProgress)
}
