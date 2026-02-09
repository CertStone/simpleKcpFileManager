package gui

import (
	"log"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	kcpclient "github.com/CertStone/simpleKcpFileManager/kcpclient"
)

// FileListItem represents a single file list item with tap handlers
type FileListItem struct {
	widget.BaseWidget
	file       *kcpclient.ListItem
	mainWindow *MainWindow
	index      int // Track index for selection
	icon       *widget.Icon
	nameLabel  *widget.Label
	sizeLabel  *widget.Label
	modeLabel  *widget.Label
	dateLabel  *widget.Label
	container  *fyne.Container
}

// NewFileListItem creates a new file list item
func NewFileListItem(mainWindow *MainWindow) *FileListItem {
	icon := widget.NewIcon(nil)
	nameLabel := widget.NewLabel("")
	sizeLabel := widget.NewLabel("")
	modeLabel := widget.NewLabel("")
	dateLabel := widget.NewLabel("")

	row := container.NewHBox(icon, nameLabel, sizeLabel, modeLabel, dateLabel)

	item := &FileListItem{
		mainWindow: mainWindow,
		index:      -1,
		icon:       icon,
		nameLabel:  nameLabel,
		sizeLabel:  sizeLabel,
		modeLabel:  modeLabel,
		dateLabel:  dateLabel,
		container:  row,
	}
	item.ExtendBaseWidget(item)
	return item
}

// CreateRenderer creates the widget renderer
func (fli *FileListItem) CreateRenderer() fyne.WidgetRenderer {
	return widget.NewSimpleRenderer(fli.container)
}

// SetFile updates the file data for this item
func (fli *FileListItem) SetFile(file *kcpclient.ListItem, index int) {
	fli.file = file
	fli.index = index

	if file.IsDir {
		fli.icon.SetResource(theme.FolderIcon())
	} else {
		fli.icon.SetResource(theme.FileIcon())
	}

	fli.nameLabel.SetText(file.Name)
	fli.sizeLabel.SetText(formatSize(file.Size))
	fli.modeLabel.SetText(formatMode(file.Mode))
	fli.dateLabel.SetText(formatTime(file.ModTime))
}

// Tapped handles single tap - actively select the item in the list
func (fli *FileListItem) Tapped(*fyne.PointEvent) {
	log.Printf("[DEBUG] FileListItem.Tapped: index=%d", fli.index)
	if fli.mainWindow != nil && fli.mainWindow.fileList != nil && fli.index >= 0 {
		// Actively select this item in the list
		fli.mainWindow.fileList.Select(widget.ListItemID(fli.index))
	}
}

// TappedSecondary handles right-click
func (fli *FileListItem) TappedSecondary(e *fyne.PointEvent) {
	if fli.file == nil {
		return
	}

	// Show context menu at mouse position
	contextMenu := NewContextMenu(fli.mainWindow)
	pos := e.AbsolutePosition
	contextMenu.ShowFileListMenu(fli.file, pos)
}

// DoubleTapped handles double-tap (navigate into folders)
func (fli *FileListItem) DoubleTapped(*fyne.PointEvent) {
	if fli.file == nil {
		return
	}

	if fli.file.IsDir {
		// Navigate into folder
		fli.mainWindow.navigateToPath(fli.file.Path)
	}
}
