package sqlite

import "encoding/json"

func jsonUnmarshal(s string, v any) error {
	return json.Unmarshal([]byte(s), v)
}
