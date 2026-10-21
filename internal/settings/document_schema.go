package settings

import (
	"reflect"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/Obedience-Corp/camp/internal/artifacts"
	"github.com/Obedience-Corp/camp/internal/config"
	"github.com/Obedience-Corp/camp/internal/leverage"
	"github.com/Obedience-Corp/camp/internal/notice"
)

type schemaTag int

const (
	schemaYAML schemaTag = iota
	schemaJSON
)

// SchemaFor returns the reflected field tree for a known authored document.
// The path is relative to .campaign/. Unknown paths return nil. The result is
// safe to share; callers must not mutate it.
func SchemaFor(rel string) []Field {
	switch rel {
	case "campaign.yaml":
		return campaignFields()
	case "settings/jumps.yaml":
		return jumpsFields()
	case "settings/fresh.yaml":
		return freshFields()
	case "settings/local.json":
		return localFields()
	case "settings/allowlist.json":
		return allowlistFields()
	case "artifacts.yaml":
		return artifactFields()
	case "notices.yaml":
		return noticeFields()
	case "leverage/config.json":
		return leverageFields()
	default:
		return nil
	}
}

var (
	campaignFields = sync.OnceValue(func() []Field {
		fields := reflectStruct(reflect.TypeFor[config.CampaignConfig](), schemaYAML, nil)
		markProjectLifecycle(fields)
		return fields
	})
	jumpsFields = sync.OnceValue(func() []Field {
		return reflectStruct(reflect.TypeFor[config.JumpsConfig](), schemaYAML, nil)
	})
	freshFields = sync.OnceValue(func() []Field {
		return reflectStruct(reflect.TypeFor[config.FreshConfig](), schemaYAML, nil)
	})
	localFields = sync.OnceValue(func() []Field {
		return reflectStruct(reflect.TypeFor[config.LocalSettings](), schemaJSON, nil)
	})
	allowlistFields = sync.OnceValue(func() []Field {
		return reflectStruct(reflect.TypeFor[config.Allowlist](), schemaJSON, nil)
	})
	artifactFields = sync.OnceValue(func() []Field {
		return reflectStruct(reflect.TypeFor[artifacts.File](), schemaYAML, nil)
	})
	noticeFields = sync.OnceValue(func() []Field {
		return reflectStruct(reflect.TypeFor[notice.DismissalFile](), schemaYAML, nil)
	})
	leverageFields = sync.OnceValue(func() []Field {
		return reflectStruct(reflect.TypeFor[leverage.LeverageConfig](), schemaJSON, nil)
	})
)

// markProjectLifecycle marks registry identity fields owned by camp project.
// This document API does not expose project add, remove, or rename.
func markProjectLifecycle(fields []Field) {
	for i := range fields {
		field := &fields[i]
		if len(field.Path) != 1 || field.Path[0] != "projects" || field.Items == nil {
			continue
		}
		for j := range field.Items.Properties {
			prop := &field.Items.Properties[j]
			if len(prop.Path) != 1 {
				continue
			}
			switch prop.Path[0] {
			case "name", "path", "url", "branch":
				prop.ReadOnly = true
			}
		}
	}
}

func reflectStruct(t reflect.Type, tag schemaTag, stack []reflect.Type) []Field {
	t = deref(t)
	if t.Kind() != reflect.Struct || t == reflect.TypeFor[time.Time]() {
		return nil
	}
	var fields []Field
	for i := range t.NumField() {
		sf := t.Field(i)
		if sf.PkgPath != "" {
			continue
		}
		name, ok, inline := exportedName(sf, tag)
		if !ok {
			continue
		}
		if inline {
			fields = append(fields, reflectStruct(sf.Type, tag, stack)...)
			continue
		}
		fields = append(fields, buildField(name, sf.Type, tag, stack))
	}
	return fields
}

func buildField(name string, t reflect.Type, tag schemaTag, stack []reflect.Type) Field {
	label := fieldLabel(name)
	field := Field{Path: []string{name}, Label: label}
	applyType(&field, deref(t), tag, stack)
	if name == "id" || name == "created_at" {
		field.ReadOnly = true
	}
	applyEnum(&field, name, deref(t))
	return field
}

func applyType(field *Field, t reflect.Type, tag schemaTag, stack []reflect.Type) {
	if t == reflect.TypeFor[time.Time]() {
		field.Type = "string"
		return
	}
	switch t.Kind() {
	case reflect.Bool:
		field.Type = "boolean"
	case reflect.String:
		field.Type = "string"
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64,
		reflect.Float32, reflect.Float64:
		field.Type = "number"
	case reflect.Slice, reflect.Array:
		field.Type = "array"
		elem := deref(t.Elem())
		item := Field{Path: []string{}, Label: singular(field.Label)}
		applyType(&item, elem, tag, stack)
		field.Items = &item
	case reflect.Map:
		field.Type = "object"
		elem := deref(t.Elem())
		value := Field{Path: []string{}, Label: singular(field.Label)}
		applyType(&value, elem, tag, stack)
		applyEnum(&value, "", elem)
		field.AdditionalProperties = &value
	case reflect.Struct:
		field.Type = "object"
		if typeDepth(stack, t) >= 2 {
			return
		}
		next := append(slices.Clone(stack), t)
		field.Properties = reflectStruct(t, tag, next)
	default:
		field.Type = "string"
	}
}

func typeDepth(stack []reflect.Type, t reflect.Type) int {
	n := 0
	for _, seen := range stack {
		if seen == t {
			n++
		}
	}
	return n
}

func deref(t reflect.Type) reflect.Type {
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	return t
}

// exportedName returns the serialized name. inline is true for anonymous fields
// that should contribute their children to the parent object.
func exportedName(sf reflect.StructField, tag schemaTag) (name string, ok bool, inline bool) {
	raw := sf.Tag.Get(tagKey(tag))
	if raw == "" {
		raw = sf.Tag.Get(tagKey(otherTag(tag)))
	}
	if raw == "-" {
		return "", false, false
	}
	if sf.Anonymous && (raw == "" || strings.HasPrefix(raw, ",")) {
		return "", true, true
	}
	name, _, _ = strings.Cut(raw, ",")
	if name == "-" {
		return "", false, false
	}
	if name == "" {
		name = strings.ToLower(sf.Name)
	}
	return name, true, false
}

func tagKey(tag schemaTag) string {
	if tag == schemaJSON {
		return "json"
	}
	return "yaml"
}

func otherTag(tag schemaTag) schemaTag {
	if tag == schemaJSON {
		return schemaYAML
	}
	return schemaJSON
}

func fieldLabel(name string) string {
	if name == "id" {
		return "ID"
	}
	parts := strings.Split(name, "_")
	for i, part := range parts {
		if part == "" {
			continue
		}
		parts[i] = strings.ToUpper(part[:1]) + part[1:]
	}
	return strings.Join(parts, " ")
}

func singular(label string) string {
	if strings.HasSuffix(label, "s") && len(label) > 1 {
		return strings.TrimSuffix(label, "s")
	}
	return label
}

func applyEnum(field *Field, name string, t reflect.Type) {
	switch {
	case t == reflect.TypeFor[config.CampaignType]():
		field.Enum = []any{"product", "research", "tools", "personal"}
	case name == "theme_override":
		names := config.ValidThemeNames()
		field.Enum = make([]any, len(names))
		for i, n := range names {
			field.Enum[i] = n
		}
	case name == "completed_runs":
		field.Enum = []any{"prompt", "sweep", "report", "off"}
	case name == "merged_workitems":
		field.Enum = []any{"prompt", "report", "off"}
	case name == "policy":
		field.Enum = []any{artifacts.PolicyAlways, artifacts.PolicyOnDemand}
	case name == "cocomo_project_type":
		field.Enum = []any{leverage.COCOMOOrganic, "semi-detached", "embedded"}
	}
}
