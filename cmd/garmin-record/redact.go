package main

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

// redactor removes personal data from responses by field name. Activity IDs are
// renumbered consistently across files; every removed value is remembered so the
// output can be searched for it afterwards.
type redactor struct {
	ids     map[string]string // original activity ID → replacement
	uuids   map[string]string // original UUID → replacement
	removed map[string]string // removed value → the field it came from
}

func newRedactor() *redactor {
	return &redactor{ids: map[string]string{}, uuids: map[string]string{}, removed: map[string]string{}}
}

// uuid replaces a Garmin UUID with a fake one, the same for every occurrence.
func (r *redactor) uuid(id string) string {
	if fake, ok := r.uuids[id]; ok {
		return fake
	}
	fake := fmt.Sprintf("00000000-0000-4000-8000-%012d", len(r.uuids)+1)
	r.uuids[id] = fake
	r.remember(id, "uuid", 5)
	return fake
}

func (r *redactor) activityID(id string) string {
	if fake, ok := r.ids[id]; ok {
		return fake
	}
	fake := itoa(int64(100001 + len(r.ids)))
	r.ids[id] = fake
	r.removed[id] = "activityId"
	return fake
}

// text replaces known activity IDs and UUIDs inside s, e.g. in paths and file names.
func (r *redactor) text(s string) string {
	for _, m := range []map[string]string{r.ids, r.uuids} {
		for id, fake := range m {
			s = strings.ReplaceAll(s, id, fake)
		}
	}
	return s
}

func (r *redactor) path(p string, query url.Values) string {
	if len(query) > 0 {
		p += "?" + query.Encode()
	}
	return r.text(p)
}

// value returns v with personal data removed; key is the field holding it.
func (r *redactor) value(key string, v any) any {
	// Field-name fragments, matched against the lower-cased name.
	var (
		activityIDFields = []string{"activityid", "parentid"}
		personalIDFields = []string{"profilepk", "profileid", "ownerid", "userid", "deviceid", "unitid", "serialnumber"}
		personalText     = []string{
			"displayname", "fullname", "username", "firstname", "lastname", "email", "imageurl",
			"locationname", "activityname", "description", "notes", "comment",
		}
		coordinateFields = []string{"latitude", "longitude"}
		geometryFields   = []string{"polyline", "geopolygon", "geopoint"}
	)
	k := strings.ToLower(key)
	if matches(k, geometryFields) {
		return nil
	}
	switch v := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(v))
		for name, child := range v {
			out[name] = r.value(name, child)
		}
		return out
	case []any:
		out := make([]any, len(v))
		for i, child := range v {
			out[i] = r.value(key, child)
		}
		return out
	}
	switch {
	case matches(k, []string{"uuid", "guid"}):
		if s, ok := v.(string); ok && s != "" {
			return r.uuid(s)
		}
	case matches(k, activityIDFields):
		if n, ok := v.(json.Number); ok {
			return json.Number(r.activityID(n.String()))
		}
	case matches(k, personalIDFields):
		if n, ok := v.(json.Number); ok {
			r.remember(n.String(), key, 5)
			return json.Number("1")
		}
		if s, ok := v.(string); ok && s != "" {
			r.remember(s, key, 5)
			return "redacted"
		}
	case matches(k, coordinateFields) || k == "lat" || k == "lon" || k == "lng":
		if n, ok := v.(json.Number); ok {
			r.remember(n.String(), key, 6)
			return json.Number("0")
		}
	case matches(k, personalText):
		if s, ok := v.(string); ok && s != "" {
			r.remember(s, key, 3)
			return "redacted"
		}
	}
	if s, ok := v.(string); ok {
		return r.text(s)
	}
	return v
}

// remember records a removed value long enough to search for without false alarms.
func (r *redactor) remember(value, field string, minLen int) {
	if len(value) >= minLen {
		r.removed[value] = field
	}
}

func matches(name string, fragments []string) bool {
	for _, f := range fragments {
		if strings.Contains(name, f) {
			return true
		}
	}
	return false
}

// checkLeaks fails if a removed value appears in any written file, including
// inside ZIPs. It names the field, never the value.
func (r *redactor) checkLeaks(dir string, files []string) error {
	for _, name := range files {
		b, err := os.ReadFile(filepath.Join(dir, name)) //nolint:gosec // G304: files this program just wrote
		if err != nil {
			return fmt.Errorf("reading %s for the leak check: %w", name, err)
		}
		contents := [][]byte{b}
		if strings.HasSuffix(name, ".zip") {
			zr, err := zip.NewReader(bytes.NewReader(b), int64(len(b)))
			if err != nil {
				return fmt.Errorf("reading %s for the leak check: %w", name, err)
			}
			for _, f := range zr.File {
				content, err := readZipFile(f)
				if err != nil {
					return err
				}
				contents = append(contents, []byte(f.Name), content)
			}
		}
		for value, field := range r.removed {
			for _, c := range contents {
				if bytes.Contains(c, []byte(value)) {
					return fmt.Errorf("possible leak in %s: a value removed from %q appears in it; nothing was committed, inspect the file", name, field)
				}
			}
		}
	}
	return nil
}
