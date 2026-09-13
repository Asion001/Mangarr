package modules

import (
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strconv"
	"strings"
)

// Field describes one setting of an implementation so the UI can render a
// form generically (like Sonarr's FieldDefinition).
//
// Struct tags on settings fields:
//
//	json:"baseUrl" label:"Base URL" help:"..." type:"url" order:"1"
//	required:"true" advanced:"true" secret:"true" placeholder:"http://..."
//	options:"value:Label,value2:Label 2"
type Field struct {
	Name        string   `json:"name"`
	Label       string   `json:"label"`
	Type        string   `json:"type"` // text, password, number, bool, select, url, textarea, tags, keyvalue
	Help        string   `json:"help,omitempty"`
	Placeholder string   `json:"placeholder,omitempty"`
	Order       int      `json:"order"`
	Required    bool     `json:"required,omitempty"`
	Advanced    bool     `json:"advanced,omitempty"`
	Secret      bool     `json:"secret,omitempty"`
	Options     []Option `json:"options,omitempty"`
	Default     any      `json:"default,omitempty"`
}

type Option struct {
	Value string `json:"value"`
	Label string `json:"label"`
}

// SecretMask replaces secret values in API responses.
const SecretMask = "********"

// FieldsOf reflects over a settings struct pointer.
func FieldsOf(settings any) []Field {
	v := reflect.ValueOf(settings)
	if v.Kind() == reflect.Pointer {
		v = v.Elem()
	}
	t := v.Type()
	fields := make([]Field, 0, t.NumField())
	for i := 0; i < t.NumField(); i++ {
		sf := t.Field(i)
		name := jsonName(sf)
		if name == "" || name == "-" || !sf.IsExported() {
			continue
		}
		f := Field{
			Name:        name,
			Label:       sf.Tag.Get("label"),
			Help:        sf.Tag.Get("help"),
			Placeholder: sf.Tag.Get("placeholder"),
			Type:        sf.Tag.Get("type"),
			Required:    sf.Tag.Get("required") == "true",
			Advanced:    sf.Tag.Get("advanced") == "true",
			Secret:      sf.Tag.Get("secret") == "true",
			Default:     v.Field(i).Interface(),
		}
		if f.Label == "" {
			f.Label = sf.Name
		}
		if o, err := strconv.Atoi(sf.Tag.Get("order")); err == nil {
			f.Order = o
		} else {
			f.Order = 100 + i
		}
		if f.Type == "" {
			f.Type = defaultType(sf.Type)
		}
		if f.Secret {
			f.Type = "password"
			f.Default = nil
		}
		if opts := sf.Tag.Get("options"); opts != "" {
			for _, p := range strings.Split(opts, ",") {
				val, label, ok := strings.Cut(p, ":")
				if !ok {
					label = val
				}
				f.Options = append(f.Options, Option{Value: val, Label: label})
			}
			if sf.Tag.Get("type") == "" {
				f.Type = "select"
			}
		}
		fields = append(fields, f)
	}
	return fields
}

func jsonName(sf reflect.StructField) string {
	tag := sf.Tag.Get("json")
	if tag == "" {
		return sf.Name
	}
	name, _, _ := strings.Cut(tag, ",")
	return name
}

func defaultType(t reflect.Type) string {
	switch t.Kind() {
	case reflect.Bool:
		return "bool"
	case reflect.Int, reflect.Int32, reflect.Int64, reflect.Float32, reflect.Float64:
		return "number"
	case reflect.Slice:
		return "tags"
	case reflect.Map:
		return "keyvalue"
	default:
		return "text"
	}
}

// DecodeSettings builds a settings value for impl from a stored map, starting
// from defaults, and validates required fields.
func DecodeSettings(impl *Implementation, raw map[string]any) (any, error) {
	s := impl.Settings()
	if len(raw) > 0 {
		b, err := json.Marshal(raw)
		if err != nil {
			return nil, err
		}
		if err := json.Unmarshal(b, s); err != nil {
			return nil, fmt.Errorf("invalid settings: %w", err)
		}
	}
	var errs []error
	v := reflect.ValueOf(s).Elem()
	for i, f := range FieldsOf(s) {
		_ = i
		if !f.Required {
			continue
		}
		fv := fieldByJSON(v, f.Name)
		if fv.IsValid() && fv.IsZero() {
			errs = append(errs, fmt.Errorf("%s is required", f.Label))
		}
	}
	if val, ok := s.(interface{ Validate() error }); ok {
		if err := val.Validate(); err != nil {
			errs = append(errs, err)
		}
	}
	return s, errors.Join(errs...)
}

func fieldByJSON(v reflect.Value, name string) reflect.Value {
	t := v.Type()
	for i := 0; i < t.NumField(); i++ {
		if jsonName(t.Field(i)) == name {
			return v.Field(i)
		}
	}
	return reflect.Value{}
}

// MaskSecrets returns a copy of raw with secret fields masked.
func MaskSecrets(impl *Implementation, raw map[string]any) map[string]any {
	out := make(map[string]any, len(raw))
	for k, v := range raw {
		out[k] = v
	}
	for _, f := range FieldsOf(impl.Settings()) {
		if f.Secret {
			if s, ok := out[f.Name].(string); ok && s != "" {
				out[f.Name] = SecretMask
			}
		}
	}
	return out
}

// MergeSecrets keeps previously stored secrets when the client sends the mask.
func MergeSecrets(impl *Implementation, incoming, stored map[string]any) map[string]any {
	for _, f := range FieldsOf(impl.Settings()) {
		if f.Secret {
			if s, ok := incoming[f.Name].(string); ok && s == SecretMask {
				incoming[f.Name] = stored[f.Name]
			}
		}
	}
	return incoming
}
