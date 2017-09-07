package utility

import (
	"bytes"
	"encoding/json"
)

func MapCopy(input map[string]interface{}) map[string]interface{} {
	new_map := make(map[string]interface{})

	for k, v := range input {
		new_map[k] = v
	}

	return new_map
}


func JsonDump(value interface{}) string {
	buffer := &bytes.Buffer{}
	encoder := json.NewEncoder(buffer)
	encoder.SetEscapeHTML(false)
	encoder.SetIndent("", "  ")
	err := encoder.Encode(value)
	if err != nil {
		panic(err)
	}

	return buffer.String()
}
