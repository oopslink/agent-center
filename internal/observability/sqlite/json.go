package sqlite

import "encoding/json"

func jsonUnmarshal(s string, v any) error {
	return json.Unmarshal([]byte(s), v)
}

// nullableString returns nil for the empty string so the column is stored as
// SQL NULL rather than an empty text value.
func nullableString(s string) any {
	if s == "" {
		return nil
	}
	return s
}
