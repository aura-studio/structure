package structure

import (
	"encoding/json"
	"errors"
	"reflect"

	"github.com/fatih/structs"
	"github.com/mitchellh/mapstructure"
)

var (
	errTypeConvertionNotSupported = errors.New("type conversion not supported")
)

type Structure = map[string]any

func DecodeStruct(a any) (Structure, error) {
	if isStruct(a) || isPointerOfStruct(a) {
		return structs.Map(a), nil
	}

	panic(errTypeConvertionNotSupported)
}

func EncodeStruct(s Structure, a any) error {
	if isStruct(a) || isPointerOfStruct(a) {
		return mapstructure.Decode(s, a)
	}

	panic(errTypeConvertionNotSupported)
}

func DecodeJSON(s string) (Structure, error) {
	m := make(Structure)
	err := json.Unmarshal([]byte(s), &m)
	return m, err
}

func EncodeJSON(s Structure) (string, error) {
	b, err := json.Marshal(s)
	return string(b), err
}

func isStruct(v any) bool {
	t := reflect.TypeOf(v)
	return t.Kind() == reflect.Struct
}

func isPointerOfStruct(v any) bool {
	t := reflect.TypeOf(v)
	if t.Kind() == reflect.Ptr && t.Elem().Kind() == reflect.Struct {
		return true
	}
	return false
}
