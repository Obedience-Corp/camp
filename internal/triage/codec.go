package triage

import (
	"bytes"
	"encoding/json"
	"reflect"
	"strings"
	"time"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
)

// Document is one top-level triage file format. The interface is closed (it
// carries an unexported method) because the set of formats is the schema:
// manifest, run state, evidence record, decision event, apply plan, receipt,
// verification report.
type Document interface {
	// Normalize puts the document in canonical form: schema_version filled
	// in, nil slices replaced with empty ones, timestamps in UTC at second
	// precision. Encoding always normalizes first, which is what makes a
	// run's files byte-reproducible from run data.
	Normalize()

	// Validate returns every rule the document violates, in field order. An
	// empty result means the document is valid.
	Validate() []Violation

	// kind names the format in diagnostics, e.g. "evidence record".
	kind() string

	// version reports the schema_version the document carries.
	version() string
}

// DecodeOptions controls how strictly a document is parsed.
//
// The asymmetry is deliberate (spec doc 04, v1alpha1 is additive-friendly):
// documents supplied from outside camp are parsed strictly so a typo in an
// agent-produced record is reported rather than silently dropped, while the
// store tolerates additive fields within the version so a run written by a
// newer camp still opens in an older one.
type DecodeOptions struct {
	// AllowUnknownFields tolerates JSON keys the format does not declare.
	AllowUnknownFields bool
}

// Strict is the DecodeOptions for documents supplied by a user or an agent:
// `camp triage evidence set --file`, `propose --file`.
var Strict = DecodeOptions{}

// Lenient is the DecodeOptions the run store uses for its own files.
var Lenient = DecodeOptions{AllowUnknownFields: true}

// MarshalDocument encodes doc in the canonical pretty form: two-space indent,
// HTML escaping off, exactly one trailing newline. It normalizes and validates
// first, so an invalid document can never reach disk.
func MarshalDocument(doc Document) ([]byte, error) {
	doc.Normalize()
	if err := newValidationError(doc.kind(), doc.Validate()); err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(doc); err != nil {
		return nil, camperrors.Wrap(err, "encode "+doc.kind())
	}
	return buf.Bytes(), nil
}

// MarshalTemplate encodes a document for a human or an agent to finish, in the
// same canonical form as MarshalDocument but without validating it.
//
// This is the one encoder that skips validation, and the exception is the
// point: a template is deliberately incomplete. It has no original_goal and no
// confidence precisely because those are the judgment the reader has to
// supply. Validating here would make it impossible to hand someone the form
// they are supposed to fill in. Everything that reaches disk still goes
// through MarshalDocument.
func MarshalTemplate(doc Document) ([]byte, error) {
	doc.Normalize()
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(doc); err != nil {
		return nil, camperrors.Wrap(err, "encode "+doc.kind()+" template")
	}
	return buf.Bytes(), nil
}

// MarshalLine encodes doc as one JSONL record: compact, HTML escaping off,
// exactly one trailing newline. Used for the append-only streams
// (decisions.jsonl, receipts.jsonl).
func MarshalLine(doc Document) ([]byte, error) {
	doc.Normalize()
	if err := newValidationError(doc.kind(), doc.Validate()); err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(doc); err != nil {
		return nil, camperrors.Wrap(err, "encode "+doc.kind())
	}
	return buf.Bytes(), nil
}

// ParseDocument decodes data into doc and reports every problem at once:
// unknown fields (unless opts allows them), a wrong schema_version, and every
// rule violation. doc must be a pointer to one of the format types.
func ParseDocument(data []byte, doc Document, opts DecodeOptions) error {
	var violations []Violation
	if !opts.AllowUnknownFields {
		violations = append(violations, unknownFields("", data, reflect.TypeOf(doc))...)
	}

	if err := json.Unmarshal(data, doc); err != nil {
		// A structural or type error stops decoding, so report it together
		// with whatever unknown fields were already found rather than
		// discarding that context.
		if len(violations) > 0 {
			violations = append(violations, Violation{Field: "", Message: err.Error()})
			return newValidationError(doc.kind(), violations)
		}
		return camperrors.Wrap(err, "parse "+doc.kind())
	}

	if v := doc.version(); v != SchemaVersion {
		// A document with no version at all is a different mistake from one
		// carrying a version this camp does not know, and the fix differs
		// too, so the message says which happened.
		message := "unsupported schema version " + quote(v)
		if v == "" {
			message = "is required"
		}
		violations = append(violations, Violation{
			Field:   "schema_version",
			Message: message,
			Allowed: []string{SchemaVersion},
		})
		// A document from another schema version cannot be judged by this
		// version's rules; reporting them all would be noise.
		return newValidationError(doc.kind(), violations)
	}

	violations = append(violations, doc.Validate()...)
	return newValidationError(doc.kind(), violations)
}

// timeType is compared by identity so the field walker treats timestamps as
// scalars instead of descending into time.Time's unexported fields.
var timeType = reflect.TypeOf(time.Time{})

// unknownFields reports every JSON key in raw that the Go type t does not
// declare, as a dotted/indexed field path. It walks nested objects, slices,
// and maps so a typo deep inside an anchor is named at its real location.
//
// Type mismatches are not this function's job: raw that does not shape-match t
// simply yields no unknown fields, and json.Unmarshal reports the real problem.
func unknownFields(path string, raw json.RawMessage, t reflect.Type) []Violation {
	for t != nil && t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	if t == nil || len(bytes.TrimSpace(raw)) == 0 {
		return nil
	}

	switch t.Kind() {
	case reflect.Struct:
		if t == timeType {
			return nil
		}
		var obj map[string]json.RawMessage
		if err := json.Unmarshal(raw, &obj); err != nil {
			return nil
		}
		declared := declaredFields(t)
		var out []Violation
		for _, key := range sortedKeys(obj) {
			ft, ok := declared[key]
			if !ok {
				out = append(out, Violation{
					Field:   joinPath(path, key),
					Message: "unknown field",
				})
				continue
			}
			out = append(out, unknownFields(joinPath(path, key), obj[key], ft)...)
		}
		return out

	case reflect.Slice, reflect.Array:
		if t.Elem().Kind() == reflect.Uint8 {
			return nil // []byte / json.RawMessage are opaque
		}
		var arr []json.RawMessage
		if err := json.Unmarshal(raw, &arr); err != nil {
			return nil
		}
		var out []Violation
		for i, elem := range arr {
			out = append(out, unknownFields(indexPath(path, i), elem, t.Elem())...)
		}
		return out

	case reflect.Map:
		var obj map[string]json.RawMessage
		if err := json.Unmarshal(raw, &obj); err != nil {
			return nil
		}
		var out []Violation
		for _, key := range sortedKeys(obj) {
			out = append(out, unknownFields(joinPath(path, key), obj[key], t.Elem())...)
		}
		return out
	}
	return nil
}

// declaredFields maps a struct's JSON key names to their field types.
func declaredFields(t reflect.Type) map[string]reflect.Type {
	out := make(map[string]reflect.Type, t.NumField())
	for i := range t.NumField() {
		f := t.Field(i)
		if !f.IsExported() {
			continue
		}
		name, ok := jsonName(f.Tag.Get("json"), f.Name)
		if !ok {
			continue
		}
		out[name] = f.Type
	}
	return out
}

// jsonName resolves the JSON key a struct field serializes under. It reports
// false for fields excluded with `json:"-"`.
func jsonName(tag, fieldName string) (string, bool) {
	if tag == "-" {
		return "", false
	}
	name := tag
	if i := strings.IndexByte(tag, ','); i >= 0 {
		name = tag[:i]
	}
	if name == "" {
		return fieldName, true
	}
	return name, true
}

// normalizeTime canonicalizes a timestamp in place: UTC at second precision.
// Triage documents are committed files read by humans in diffs; sub-second
// wall-clock noise would churn them without adding information.
func normalizeTime(t *time.Time) {
	if t.IsZero() {
		return
	}
	*t = t.UTC().Truncate(time.Second)
}

// normalizeStrings replaces a nil slice with an empty one so JSON output is
// `[]` rather than `null`, matching camp's existing workitem JSON convention.
func normalizeStrings(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}
