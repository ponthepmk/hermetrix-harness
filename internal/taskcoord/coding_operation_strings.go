package taskcoord

import (
	"reflect"
	"unicode/utf8"
)

func codingStringsValid(v reflect.Value, depth int) bool {
	if depth > 32 {
		return false
	}
	switch v.Kind() {
	case reflect.String:
		return utf8.ValidString(v.String())
	case reflect.Struct:
		for i := 0; i < v.NumField(); i++ {
			if !codingStringsValid(v.Field(i), depth+1) {
				return false
			}
		}
		return true
	case reflect.Slice, reflect.Array:
		for i := 0; i < v.Len(); i++ {
			if !codingStringsValid(v.Index(i), depth+1) {
				return false
			}
		}
		return true
	case reflect.Ptr, reflect.Interface:
		if v.IsNil() {
			return true
		}
		return codingStringsValid(v.Elem(), depth+1)
	case reflect.Map:
		for _, key := range v.MapKeys() {
			if !codingStringsValid(key, depth+1) {
				return false
			}
			if !codingStringsValid(v.MapIndex(key), depth+1) {
				return false
			}
		}
		return true
	default:
		return true
	}
}
