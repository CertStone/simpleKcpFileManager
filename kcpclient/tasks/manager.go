package tasks

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	kcpclient "github.com/CertStone/simpleKcpFileManager/kcpclient"
)

// TaskType represents the type of task
type TaskType int

const (
	TaskTypeDownload TaskType = iota
	TaskTypeUpload
	TaskTypeCompress
	TaskTypeExtract
)

// TaskStatus represents the status of a task
type TaskStatus int

const (
	StatusPending TaskStatus = iota
	StatusRunning
	StatusPaused
	StatusCompleted
	StatusFailed
	StatusCanceled
)

// Task represents a file operation task
type Task struct {
	ID         string
	Type       TaskType
	Status     TaskStatus
	Progress   float64
	Speed      float64
	Error      error
	LocalPath  string
	RemotePath string
	FileSize   int64
	BytesDone  int64
	CancelFunc context.CancelFunc
	Canceled   atomic.Bool
}

// Manager manages file operation tasks
type Manager struct {
	client             *kcpclient.Client
	packTransferConfig kcpclient.PackTransferConfig
	tasks              map[string]*Task
	tasksMutex         sync.RWMutex
	taskQueue          chan *Task
	maxParallel        int
	semaphore          chan struct{}
}

// NewManager creates a new task manager
func NewManager(client *kcpclient.Client, maxParallel int, packConfig kcpclient.PackTransferConfig) *Manager {
	if maxParallel <= 0 {
		maxParallel = 3
	}

	return &Manager{
		client:             client,
		packTransferConfig: packConfig,
		tasks:              make(map[string]*Task),
		taskQueue:          make(chan *Task, 100),
		maxParallel:        maxParallel,
		semaphore:          make(chan struct{}, maxParallel),
	}
}

// SetPackTransferConfig updates the pack transfer configuration
func (m *Manager) SetPackTransferConfig(config kcpclient.PackTransferConfig) {
	m.packTransferConfig = config
}

// AddDownloadTask adds a download task
func (m *Manager) AddDownloadTask(remotePath, localPath string) (*Task, error) {
	m.tasksMutex.Lock()
	defer m.tasksMutex.Unlock()

	task := &Task{
		ID:         generateTaskID(),
		Type:       TaskTypeDownload,
		Status:     StatusPending,
		RemotePath: remotePath,
		LocalPath:  localPath,
	}
	m.tasks[task.ID] = task

	go m.runDownloadTask(task)
	return task, nil
}

// AddUploadTask adds an upload task
func (m *Manager) AddUploadTask(localPath, remotePath string) (*Task, error) {
	m.tasksMutex.Lock()
	defer m.tasksMutex.Unlock()

	// Get file size
	info, err := os.Stat(localPath)
	if err != nil {
		return nil, fmt.Errorf("stat file: %w", err)
	}

	task := &Task{
		ID:         generateTaskID(),
		Type:       TaskTypeUpload,
		Status:     StatusPending,
		LocalPath:  localPath,
		RemotePath: remotePath,
		FileSize:   info.Size(),
	}
	m.tasks[task.ID] = task

	go m.runUploadTask(task)
	return task, nil
}

// AddUploadFolderTask adds a folder upload task (for pack transfer)
func (m *Manager) AddUploadFolderTask(localPath, remotePath string) (*Task, error) {
	m.tasksMutex.Lock()
	defer m.tasksMutex.Unlock()

	// Calculate total folder size
	var totalSize int64
	err := filepath.Walk(localPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() {
			totalSize += info.Size()
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("walk folder: %w", err)
	}

	task := &Task{
		ID:         generateTaskID(),
		Type:       TaskTypeUpload,
		Status:     StatusPending,
		LocalPath:  localPath,
		RemotePath: remotePath,
		FileSize:   totalSize, // Use total folder size for progress tracking
	}
	m.tasks[task.ID] = task

	go m.runUploadFolderTask(task)
	return task, nil
}

// AddCompressTask adds a compress task
func (m *Manager) AddCompressTask(paths []string, outputPath, format string) (*Task, error) {
	m.tasksMutex.Lock()
	defer m.tasksMutex.Unlock()

	task := &Task{
		ID:         generateTaskID(),
		Type:       TaskTypeCompress,
		Status:     StatusPending,
		RemotePath: outputPath,
	}
	m.tasks[task.ID] = task

	go m.runCompressTask(task, paths, outputPath, format)
	return task, nil
}

// runDownloadTask executes a download task
func (m *Manager) runDownloadTask(task *Task) {
	m.semaphore <- struct{}{}
	defer func() { <-m.semaphore }()

	task.Status = StatusRunning
	_, cancel := context.WithCancel(context.Background())
	task.CancelFunc = cancel

	var err error
	// Use pack transfer if enabled
	if m.packTransferConfig.Enabled {
		err = m.client.DownloadFilePacked(task.RemotePath, task.LocalPath, m.packTransferConfig, func(percent float64, speed float64) {
			task.Progress = percent
			task.Speed = speed
		})
	} else {
		err = m.client.DownloadFile(task.RemotePath, task.LocalPath, func(percent float64, speed float64) {
			task.Progress = percent
			task.Speed = speed
		})
	}

	if task.Canceled.Load() {
		task.Status = StatusCanceled
		// Remove partial file
		os.Remove(task.LocalPath)
	} else if err != nil {
		task.Status = StatusFailed
		task.Error = err
	} else {
		task.Status = StatusCompleted
		task.Progress = 1.0
	}

	// Notify completion callback
	if OnTaskCompleted != nil {
		OnTaskCompleted(task)
	}
}

// runUploadTask executes an upload task
func (m *Manager) runUploadTask(task *Task) {
	m.semaphore <- struct{}{}
	defer func() { <-m.semaphore }()

	task.Status = StatusRunning
	_, cancel := context.WithCancel(context.Background())
	task.CancelFunc = cancel

	var err error
	// Use pack transfer if enabled
	if m.packTransferConfig.Enabled {
		err = m.client.UploadFilePacked(task.LocalPath, task.RemotePath, m.packTransferConfig, func(written, total int64) {
			if total > 0 {
				task.Progress = float64(written) / float64(total)
				task.BytesDone = written
			}
		})
	} else {
		err = m.client.UploadFile(task.LocalPath, task.RemotePath, func(written, total int64) {
			if total > 0 {
				task.Progress = float64(written) / float64(total)
				task.BytesDone = written
			}
		})
	}

	if task.Canceled.Load() {
		task.Status = StatusCanceled
	} else if err != nil {
		task.Status = StatusFailed
		task.Error = err
	} else {
		task.Status = StatusCompleted
		task.Progress = 1.0
	}

	// Notify completion callback
	if OnTaskCompleted != nil {
		OnTaskCompleted(task)
	}
}

// runUploadFolderTask executes a folder upload task (always uses pack transfer)
func (m *Manager) runUploadFolderTask(task *Task) {
	m.semaphore <- struct{}{}
	defer func() { <-m.semaphore }()

	task.Status = StatusRunning
	_, cancel := context.WithCancel(context.Background())
	task.CancelFunc = cancel

	// Always use pack transfer for folder uploads
	err := m.client.UploadFilePacked(task.LocalPath, task.RemotePath, m.packTransferConfig, func(written, total int64) {
		if total > 0 {
			task.Progress = float64(written) / float64(total)
			task.BytesDone = written
		}
	})

	if task.Canceled.Load() {
		task.Status = StatusCanceled
	} else if err != nil {
		task.Status = StatusFailed
		task.Error = err
	} else {
		task.Status = StatusCompleted
		task.Progress = 1.0
	}

	// Notify completion callback
	if OnTaskCompleted != nil {
		OnTaskCompleted(task)
	}
}

// CompletionCallback is called when a task completes
type CompletionCallback func(task *Task)

// OnTaskCompleted is called when any task completes (set by GUI)
var OnTaskCompleted CompletionCallback

// runCompressTask executes a compress task
func (m *Manager) runCompressTask(task *Task, paths []string, outputPath, format string) {
	m.semaphore <- struct{}{}
	defer func() { <-m.semaphore }()

	task.Status = StatusRunning
	_, cancel := context.WithCancel(context.Background())
	task.CancelFunc = cancel

	err := m.client.Compress(paths, outputPath, format)

	if task.Canceled.Load() {
		task.Status = StatusCanceled
	} else if err != nil {
		task.Status = StatusFailed
		task.Error = err
	} else {
		task.Status = StatusCompleted
		task.Progress = 1.0
	}

	// Notify completion callback
	if OnTaskCompleted != nil {
		OnTaskCompleted(task)
	}
}

// CancelTask cancels a task
func (m *Manager) CancelTask(taskID string) error {
	m.tasksMutex.RLock()
	task, exists := m.tasks[taskID]
	m.tasksMutex.RUnlock()

	if !exists {
		return fmt.Errorf("task not found")
	}

	task.Canceled.Store(true)
	if task.CancelFunc != nil {
		task.CancelFunc()
	}

	return nil
}

// GetTask returns a task by ID
func (m *Manager) GetTask(taskID string) (*Task, bool) {
	m.tasksMutex.RLock()
	defer m.tasksMutex.RUnlock()
	task, exists := m.tasks[taskID]
	return task, exists
}

// GetAllTasks returns all tasks
func (m *Manager) GetAllTasks() []*Task {
	m.tasksMutex.RLock()
	defer m.tasksMutex.RUnlock()

	tasks := make([]*Task, 0, len(m.tasks))
	for _, task := range m.tasks {
		tasks = append(tasks, task)
	}
	return tasks
}

// RemoveTask removes a task from the manager
func (m *Manager) RemoveTask(taskID string) {
	m.tasksMutex.Lock()
	defer m.tasksMutex.Unlock()
	delete(m.tasks, taskID)
}

// generateTaskID generates a unique task ID
func generateTaskID() string {
	return fmt.Sprintf("%d-%d", time.Now().UnixNano(), os.Getpid())
}
