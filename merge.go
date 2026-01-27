package structure

// mergeStructure merges two map structures recursively
func mergeStructure(m1, m2 Structure) Structure {
	mergedMap := make(Structure)

	// Iterate over the keys of the first map structure
	// and copy the key-value pairs to the merged map
	for key, value := range m1 {
		mergedMap[key] = value
	}

	// Iterate over the keys of the second map structure
	// and copy the key-value pairs to the merged map.
	// If a key already exists in the merged map, recursively merge the values.
	for key, value := range m2 {
		if existingValue, ok := mergedMap[key]; ok {
			// If the key already exists, recursively merge the values
			mergedMap[key] = mergeValues(existingValue, value)
		} else {
			// If the key doesn't exist, simply copy the value
			mergedMap[key] = value
		}
	}

	return mergedMap
}

// mergeValues merges two values recursively
func mergeValues(value1, value2 interface{}) interface{} {
	switch value1 := value1.(type) {
	case Structure:
		// If the value is a map structure, recursively merge the two map structures
		if value2, ok := value2.(Structure); ok {
			return mergeStructure(value1, value2)
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
