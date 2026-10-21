package settings

import (
	"crypto/sha256"
	"encoding/hex"
)

// Document schema settings-documents/v1alpha1. JSON names match the contract.

const (
	DocumentSchemaVersion = "settings-documents/v1alpha1"

	RevisionAbsent = "absent"

	RevisionConflictCode    = "revision_conflict"
	ReadbackFailedCode      = "readback_failed"
	CodeIdentityMismatch    = "identity_mismatch"
	CodeNotFound            = "not_found"
	CodeInvalidPath         = "invalid_path"
	CodeCancelled           = "cancelled"
	CodeIO                  = "io_error"
	CodeOversized           = "oversized"
	IdentityMismatchMessage = "target camp identity no longer matches its registry entry; register it again"
	RevisionConflictMessage = "Document changed since it was read; reload and compare before saving"
	ReadbackFailedMessage   = "The write succeeded; readback failed and must be retried"

	SeverityError   = "error"
	SeverityWarning = "warning"

	ScopeCamp = "camp"

	DocumentYAML      = "yaml"
	DocumentJSON      = "json"
	DocumentMarkdown  = "markdown"
	DocumentText      = "text"
	DocumentBinary    = "binary"
	DocumentDirectory = "directory"
	DocumentSymlink   = "symlink"
)

// DefaultLimits is the catalog bound published with settings-documents/v1alpha1.
func DefaultLimits() Limits {
	return Limits{MaxDepth: 16, MaxEntries: 10000, MaxDocumentBytes: 1048576}
}

// RevisionOf is sha256:<lowercase-hex> over the exact document bytes.
func RevisionOf(content []byte) string {
	sum := sha256.Sum256(content)
	return "sha256:" + hex.EncodeToString(sum[:])
}

// Diagnostic is one field or source finding.
type Diagnostic struct {
	Severity string `json:"severity"`
	Message  string `json:"message"`
	Path     string `json:"path,omitempty"`
	Line     int    `json:"line,omitempty"`
	Column   int    `json:"column,omitempty"`
}

// OwnerAction is a capability advertisement. Supported actions are executable
// through the document action command; other entries name the owning command.
type OwnerAction struct {
	ID        string   `json:"id"`
	Label     string   `json:"label"`
	Command   []string `json:"command"`
	Supported bool     `json:"supported"`
	Reason    string   `json:"reason,omitempty"`
}

// Field is one reflected schema node. Path is relative to the enclosing object.
// Items of an array use an empty path.
type Field struct {
	Path                 []string `json:"path"`
	Label                string   `json:"label"`
	Type                 string   `json:"type"`
	Description          string   `json:"description,omitempty"`
	Enum                 []any    `json:"enum,omitempty"`
	Default              any      `json:"default,omitempty"`
	ReadOnly             bool     `json:"read_only,omitzero"`
	Properties           []Field  `json:"properties,omitempty"`
	Items                *Field   `json:"items,omitempty"`
	AdditionalProperties *Field   `json:"additional_properties,omitempty"`
}

// Capabilities is what the document API will do with one path.
type Capabilities struct {
	Read       bool          `json:"read"`
	SourceEdit bool          `json:"source_edit"`
	Create     bool          `json:"create"`
	Rename     bool          `json:"rename"`
	Delete     bool          `json:"delete"`
	Form       bool          `json:"form"`
	Reason     string        `json:"reason,omitempty"`
	Actions    []OwnerAction `json:"actions"`
}

// DocumentEntry is one catalog or snapshot row.
type DocumentEntry struct {
	ID           string       `json:"id"`
	Path         string       `json:"path"`
	Title        string       `json:"title"`
	Scope        string       `json:"scope"`
	Format       string       `json:"format"`
	Owner        string       `json:"owner"`
	Exists       bool         `json:"exists"`
	Capabilities Capabilities `json:"capabilities"`
	Fields       []Field      `json:"fields,omitempty"`
	LinkTarget   string       `json:"link_target,omitempty"`
}

// Identity is the target camp stamped on successful payloads.
type Identity struct {
	SchemaVersion string `json:"schema_version"`
	CampID        string `json:"camp_id"`
	CampRoot      string `json:"camp_root"`
}

// Limits bounds discovery and document bodies.
type Limits struct {
	MaxDepth         int `json:"max_depth"`
	MaxEntries       int `json:"max_entries"`
	MaxDocumentBytes int `json:"max_document_bytes"`
}

// Catalog is the machine-readable document index.
type Catalog struct {
	SchemaVersion string          `json:"schema_version"`
	CampID        string          `json:"camp_id"`
	CampRoot      string          `json:"camp_root"`
	Documents     []DocumentEntry `json:"documents"`
	Limits        Limits          `json:"limits"`
	Truncated     bool            `json:"truncated"`
	Diagnostics   []Diagnostic    `json:"diagnostics"`
}

// Snapshot is one document plus its bytes and parsed values.
type Snapshot struct {
	SchemaVersion   string       `json:"schema_version"`
	CampID          string       `json:"camp_id"`
	CampRoot        string       `json:"camp_root"`
	ID              string       `json:"id"`
	Path            string       `json:"path"`
	Title           string       `json:"title"`
	Scope           string       `json:"scope"`
	Format          string       `json:"format"`
	Owner           string       `json:"owner"`
	Exists          bool         `json:"exists"`
	Capabilities    Capabilities `json:"capabilities"`
	Fields          []Field      `json:"fields,omitempty"`
	LinkTarget      string       `json:"link_target,omitempty"`
	Revision        string       `json:"revision"`
	Content         *string      `json:"content"`
	Values          any          `json:"values,omitempty"`
	EffectiveValues any          `json:"effective_values,omitempty"`
	Inheritance     any          `json:"inheritance,omitempty"`
	Diagnostics     []Diagnostic `json:"diagnostics"`
	Applied         bool         `json:"applied"`
}

// Validation is the result of checking a candidate without writing it.
type Validation struct {
	SchemaVersion string       `json:"schema_version"`
	CampID        string       `json:"camp_id"`
	CampRoot      string       `json:"camp_root"`
	Path          string       `json:"path"`
	Valid         bool         `json:"valid"`
	Diagnostics   []Diagnostic `json:"diagnostics"`
}

// ErrorDetail is the nested error object.
type ErrorDetail struct {
	Code        string       `json:"code"`
	Message     string       `json:"message"`
	Diagnostics []Diagnostic `json:"diagnostics,omitempty"`
	Applied     bool         `json:"applied,omitempty"`
	Path        string       `json:"path,omitempty"`
}

// ErrorResponse is the stderr envelope. CampID and CampRoot are set when the
// target was resolved, including readback_failed after a successful write.
type ErrorResponse struct {
	SchemaVersion string      `json:"schema_version"`
	Error         ErrorDetail `json:"error"`
	CampID        string      `json:"camp_id,omitempty"`
	CampRoot      string      `json:"camp_root,omitempty"`
}

// EmptyDiagnostics returns a non-nil slice so JSON encodes [].
func EmptyDiagnostics() []Diagnostic {
	return []Diagnostic{}
}

// EmptyActions returns a non-nil action list.
func EmptyActions() []OwnerAction {
	return []OwnerAction{}
}

// HasErrors reports whether any diagnostic is severity error.
func HasErrors(diags []Diagnostic) bool {
	for _, d := range diags {
		if d.Severity == SeverityError {
			return true
		}
	}
	return false
}

// OpError is a document operation failure. Reads and validation use it for
// path, identity, size, cancellation, and I/O. Field diagnostics stay on the
// success payload so an invalid document can still be repaired.
type OpError struct {
	Code        string
	Message     string
	Path        string
	CampID      string
	CampRoot    string
	Applied     bool
	Diagnostics []Diagnostic
	Err         error
}

// Error returns the user-facing message.
func (e *OpError) Error() string {
	if e == nil {
		return ""
	}
	if e.Message != "" {
		return e.Message
	}
	if e.Err != nil {
		return e.Err.Error()
	}
	return e.Code
}

// Unwrap returns the underlying cause.
func (e *OpError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

// Response is the stderr envelope for this failure.
func (e *OpError) Response() ErrorResponse {
	if e == nil {
		return ErrorResponse{SchemaVersion: DocumentSchemaVersion}
	}
	detail := ErrorDetail{Code: e.Code, Message: e.Message, Path: e.Path, Applied: e.Applied}
	if len(e.Diagnostics) > 0 {
		detail.Diagnostics = e.Diagnostics
	}
	return ErrorResponse{
		SchemaVersion: DocumentSchemaVersion,
		Error:         detail,
		CampID:        e.CampID,
		CampRoot:      e.CampRoot,
	}
}
