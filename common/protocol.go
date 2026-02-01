package common

const (
	// HTTP Query parameters
	QueryAction  = "action"
	QueryPath    = "path"
	QueryOld     = "old"
	QueryNew     = "new"
	QueryRecursive = "recursive"

	// Action values
	ActionList     = "list"
	ActionChecksum = "checksum"
	ActionDownload = "download"
	ActionUpload   = "upload"
	ActionDelete   = "delete"
	ActionMkdir    = "mkdir"
	ActionRename   = "rename"
	ActionCompress = "compress"
	ActionExtract  = "extract"
	ActionEdit     = "edit"
)

// HTTP methods
const (
	MethodGet    = "GET"
	MethodPost   = "POST"
	MethodPut    = "PUT"
	MethodDelete = "DELETE"
)
