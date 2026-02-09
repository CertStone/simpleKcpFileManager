package gui

import (
	"fmt"
	"strconv"

	kcpclient "github.com/CertStone/simpleKcpFileManager/kcpclient"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/widget"
)

// SettingsDialog manages transfer settings
type SettingsDialog struct {
	mainWindow        *MainWindow
	packTransferCheck *widget.Check
	thresholdEntry    *widget.Entry
	downloadDirEntry  *widget.Entry
	config            kcpclient.PackTransferConfig
}

// NewSettingsDialog creates a new settings dialog
func NewSettingsDialog(mainWindow *MainWindow) *SettingsDialog {
	return &SettingsDialog{
		mainWindow: mainWindow,
		config:     mainWindow.packTransferConfig,
	}
}

// Show displays the settings dialog
func (sd *SettingsDialog) Show() {
	// Create download directory entry
	sd.downloadDirEntry = widget.NewEntry()
	sd.downloadDirEntry.SetText(sd.mainWindow.saveDir)
	sd.downloadDirEntry.Disable() // Disable direct editing

	browseBtn := widget.NewButton("浏览...", func() {
		sd.showBrowseDialog()
	})

	downloadDirContainer := container.NewBorder(
		nil, nil,
		widget.NewLabel("下载文件夹:"),
		browseBtn,
		sd.downloadDirEntry,
	)

	// Create pack transfer checkbox
	sd.packTransferCheck = widget.NewCheck("启用打包传输", func(checked bool) {
		sd.config.Enabled = checked
	})
	sd.packTransferCheck.Checked = sd.config.Enabled

	// Create threshold entry
	sd.thresholdEntry = widget.NewEntry()
	sd.thresholdEntry.SetPlaceHolder("10")
	if sd.config.ThresholdBytes > 0 {
		thresholdMB := sd.config.ThresholdBytes / (1024 * 1024)
		sd.thresholdEntry.SetText(fmt.Sprintf("%d", thresholdMB))
	} else {
		sd.thresholdEntry.SetText("10")
	}

	thresholdContainer := container.NewBorder(
		nil, nil,
		widget.NewLabel("大文件阈值:"),
		widget.NewLabel("MB"),
		sd.thresholdEntry,
	)

	// Create description label
	description := widget.NewLabel("说明:\n" +
		"• 开启后，文件夹和大文件会自动压缩为 .tar.gz 格式传输\n" +
		"• 服务器端接收后自动解压\n" +
		"• 可显著提升传输速度，特别是小文件较多的文件夹\n" +
		"• 阈值: 单个文件大于该值时启用压缩")
	description.Wrapping = fyne.TextWrapWord

	// Create content
	content := container.NewVBox(
		widget.NewSeparator(),
		downloadDirContainer,
		widget.NewLabel(""),
		widget.NewSeparator(),
		sd.packTransferCheck,
		widget.NewLabel(""),
		thresholdContainer,
		widget.NewLabel(""),
		widget.NewSeparator(),
		description,
	)

	// Show dialog
	d := dialog.NewCustomConfirm("传输设置", "保存", "取消", content, func(confirmed bool) {
		if confirmed {
			sd.saveSettings()
		}
	}, sd.mainWindow.window)
	d.Resize(fyne.NewSize(500, 450))
	d.Show()
}

// saveSettings saves the settings
func (sd *SettingsDialog) saveSettings() {
	// Save download directory
	if sd.downloadDirEntry.Text != "" {
		sd.mainWindow.saveDir = sd.downloadDirEntry.Text
	}

	// Get threshold value
	thresholdMB := 10 // default
	if sd.thresholdEntry.Text != "" {
		if val, err := strconv.Atoi(sd.thresholdEntry.Text); err == nil && val > 0 {
			thresholdMB = val
		}
	}

	// Update config
	sd.config.Enabled = sd.packTransferCheck.Checked
	sd.config.ThresholdBytes = int64(thresholdMB) * 1024 * 1024

	// Save to main window
	sd.mainWindow.packTransferConfig = sd.config

	// Update task manager configuration
	sd.mainWindow.taskManager.SetPackTransferConfig(sd.config)

	// Show confirmation
	dialog.ShowInformation("设置已保存",
		"设置已更新\n"+
			fmt.Sprintf("• 下载文件夹: %s\n", sd.mainWindow.saveDir)+
			fmt.Sprintf("• 打包传输: %s\n", getEnabledStatus(sd.config.Enabled))+
			fmt.Sprintf("• 阈值: %d MB", thresholdMB),
		sd.mainWindow.window)
}

// showBrowseDialog shows a folder browse dialog
func (sd *SettingsDialog) showBrowseDialog() {
	dialog.ShowFolderOpen(func(uri fyne.ListableURI, err error) {
		if err != nil || uri == nil {
			return
		}

		// Get the path
		path := uri.Path()
		sd.downloadDirEntry.SetText(path)
	}, sd.mainWindow.window)
}

// getEnabledStatus returns human-readable status
func getEnabledStatus(enabled bool) string {
	if enabled {
		return "已启用"
	}
	return "已禁用"
}
