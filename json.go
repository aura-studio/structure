package structure

import (
	"encoding/json"
)

type _json struct{}

var JSON = _json{}

// Merge merges two deep nested JSON objects and returns the merged result as a byte slice.
func (j *_json) Merge(jsonData1, jsonData2 []byte) ([]byte, error) {
	// Parse the first JSON data into a map structure
	var map1 map[string]interface{}
	err := json.Unmarshal(jsonData1, &map1)
	if err != nil {
		return nil, err
	}

	// Parse the second JSON data into a map structure
	var map2 map[string]interface{}
	err = json.Unmarshal(jsonData2, &map2)
	if err != nil {
		return nil, err
	}

	// Merge the two map structures
	mergedMap := j.mergeMaps(map1, map2)

	// Convert the merged map structure back to JSON
	mergedJSON, err := json.Marshal(mergedMap)
	if err != nil {
		return nil, err
	}

	return mergedJSON, nil
}

// mergeMaps merges two map structures recursively
func (j *_json) mergeMaps(map1, map2 map[string]interface{}) map[string]interface{} {
	mergedMap := make(map[string]interface{})

	// Iterate over the keys of the first map structure
	// and copy the key-value pairs to the merged map
	for key, value := range map1 {
		mergedMap[key] = value
	}

	// Iterate over the keys of the second map structure
	// and copy the key-value pairs to the merged map.
	// If a key already exists in the merged map, recursively merge the values.
	for key, value := range map2 {
		if existingValue, ok := mergedMap[key]; ok {
			// If the key already exists, recursively merge the values
			mergedMap[key] = j.mergeValues(existingValue, value)
		} else {
			// If the key doesn't exist, simply copy the value
			mergedMap[key] = value
		}
	}

	return mergedMap
}

// mergeValues merges two values recursively
func (j *_json) mergeValues(value1, value2 interface{}) interface{} {
	switch value1 := value1.(type) {
	case map[string]interface{}:
		// If the value is a map structure, recursively merge the two map structures
		if value2, ok := value2.(map[string]interface{}); ok {
			return j.mergeMaps(value1, value2)
		}
	case []interface{}:
		// If the value is a slice, merge the two slices
		if value2, ok := value2.([]interface{}); ok {
			return append(value1, value2...)
		}
	}

	// For other cases, simply use the value from the second object to overwrite the value from the first object
	return value2
}
