package shim

import "encoding/json"

func jsonMarshalImpl(v any) ([]byte, error) {
	return json.Marshal(v)
}
