// Package attachments implements SharedCal's upload sanitization pipeline,
// content-addressed blob storage, optional ClamAV scanning and the HTTP
// serving headers for attachments.
//
// Every upload is treated as hostile: the real type is detected from magic
// bytes and must match both the allow-list and the file extension, images are
// decoded and re-encoded (dropping all metadata and any polyglot payload), and
// files are only ever served with headers that stop the browser from sniffing
// or executing them.
package attachments

import "errors"

// Sentinel errors returned by the pipeline. The HTTP layer maps them to status
// codes; see the comment on each.
var (
	// ErrTooLarge means the upload exceeded Pipeline.MaxSize (HTTP 413).
	ErrTooLarge = errors.New("attachments: file too large")
	// ErrEmpty means the upload had zero bytes (HTTP 400).
	ErrEmpty = errors.New("attachments: empty file")
	// ErrTypeNotAllowed means the detected type or the extension is not on
	// the allow-list (HTTP 415).
	ErrTypeNotAllowed = errors.New("attachments: file type not allowed")
	// ErrTypeMismatch means the detected type is allowed but disagrees with
	// the file extension, e.g. a PNG named .jpg (HTTP 415).
	ErrTypeMismatch = errors.New("attachments: file content does not match its extension")
	// ErrImageTooLarge means an image exceeds the pixel or dimension limit
	// (HTTP 422).
	ErrImageTooLarge = errors.New("attachments: image dimensions too large")
	// ErrCorrupt means the file claims an allowed type but could not be
	// decoded or validated as one (HTTP 422).
	ErrCorrupt = errors.New("attachments: file is corrupt or malformed")
	// ErrNotFound means a blob does not exist in the store (HTTP 404).
	ErrNotFound = errors.New("attachments: blob not found")
	// ErrInvalidKey means a storage key does not have the exact
	// "<2 hex>/<64 hex>" shape. It indicates a programming error or tampering.
	ErrInvalidKey = errors.New("attachments: invalid storage key")
	// ErrScanTooLarge means clamd refused the stream because it exceeded its
	// StreamMaxLength; configure clamd's limit >= ATTACHMENT_MAX_SIZE.
	ErrScanTooLarge = errors.New("attachments: file exceeds clamd StreamMaxLength")
)
