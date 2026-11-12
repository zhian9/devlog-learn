package redact

import (
	"reflect"
	"time"
)

var timeType = reflect.TypeOf(time.Time{})

// WalkStrings applies fn to every settable string in a value graph. It is used
// as a defensive second pass over model output before it is persisted.
func (r *Redactor) WalkStrings(value any, fn func(string) string) {
	walkStrings(reflect.ValueOf(value), fn, map[uintptr]bool{})
}

func walkStrings(v reflect.Value, fn func(string) string, seen map[uintptr]bool) {
	if !v.IsValid() {
		return
	}

	switch v.Kind() {
	case reflect.Ptr:
		if v.IsNil() {
			return
		}
		ptr := v.Pointer()
		if ptr != 0 {
			if seen[ptr] {
				return
			}
			seen[ptr] = true
		}
		walkStrings(v.Elem(), fn, seen)
	case reflect.Interface:
		if v.IsNil() {
			return
		}
		walkStrings(v.Elem(), fn, seen)
	case reflect.Struct:
		if v.Type() == timeType {
			return
		}
		for i := 0; i < v.NumField(); i++ {
			field := v.Type().Field(i)
			if field.PkgPath != "" { // unexported
				continue
			}
			walkStrings(v.Field(i), fn, seen)
		}
	case reflect.Slice, reflect.Array:
		for i := 0; i < v.Len(); i++ {
			walkStrings(v.Index(i), fn, seen)
		}
	case reflect.Map:
		iter := v.MapRange()
		for iter.Next() {
			key := iter.Key()
			value := iter.Value()
			if value.Kind() == reflect.String {
				v.SetMapIndex(key, reflect.ValueOf(fn(value.String())).Convert(value.Type()))
				continue
			}
			walkStrings(value, fn, seen)
		}
	case reflect.String:
		if v.CanSet() {
			v.SetString(fn(v.String()))
		}
	}
}
