// Package binding converts format-independent configuration values into settings.
package binding

import (
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"math"
	"math/big"
	"reflect"
	"slices"
	"strconv"
	"strings"

	"github.com/go-viper/mapstructure/v2"
)

// Decoder validates and replaces a value using the shared configuration model.
type Decoder interface{ DecodeConfig(any) error }

// Encoder projects settings into the shared configuration model.
type Encoder interface{ EncodeConfig() (any, error) }

// ErrUnknownKey marks an unsupported object member. Its source is the key token.
var ErrUnknownKey = errors.New("unknown key")

// Error identifies the configuration value that failed to bind.
type Error struct {
	Path  string
	Cause error
}

func (e *Error) Error() string {
	if e.Path == "" {
		return e.Cause.Error()
	}
	return e.Path + ": " + e.Cause.Error()
}
func (e *Error) Unwrap() error { return e.Cause }

// At prepends one object key or array index to an error's JSON pointer.
func At(part string, err error) error {
	if err == nil {
		return nil
	}
	pointer := "/" + strings.NewReplacer("~", "~0", "/", "~1").Replace(part)
	return prefixError(pointer, err)
}

// Context adds a readable description while retaining each failing value's path.
func Context(message string, err error) error {
	if err == nil {
		return nil
	}
	if failure, ok := currentError[*Error](err); ok {
		return &Error{Path: failure.Path, Cause: fmt.Errorf("%s: %w", message, failure.Cause)}
	}
	if group, ok := err.(interface{ Unwrap() []error }); ok {
		var failures []error
		for _, failure := range group.Unwrap() {
			failures = append(failures, Context(message, failure))
		}
		return errors.Join(failures...)
	}
	return fmt.Errorf("%s: %w", message, err)
}

// Decode binds values strictly. Unknown struct fields and scalar coercions fail.
// A failed decode leaves dst unchanged.
func Decode(value any, dst any) error { return decode(value, dst, true) }

// DecodeKnown permits ignored fields in legacy mappings. Nested Decoder hooks
// continue to apply their own strict contracts.
func DecodeKnown(value any, dst any) error { return decode(value, dst, false) }

func decode(value any, dst any, strict bool) error {
	target := reflect.ValueOf(dst)
	if target.Kind() != reflect.Pointer || target.IsNil() {
		return errors.New("configuration destination must be a non-nil pointer")
	}
	next := reflect.New(target.Elem().Type())
	metadata := new(mapstructure.Metadata)
	decoder, err := mapstructure.NewDecoder(&mapstructure.DecoderConfig{
		Result: next.Interface(), TagName: "yaml", SquashTagOption: "inline", Metadata: metadata,
		ZeroFields: true, DecodeNil: true, MatchName: func(a, b string) bool { return a == b },
		DecodeHook: mapstructure.DecodeHookFuncValue(func(from, to reflect.Value) (any, error) {
			var input any
			if from.IsValid() && !isNil(from) {
				input = from.Interface()
			}
			if from.IsValid() && from.Type() == to.Type() {
				return input, nil
			}
			if from.IsValid() && from.Kind() == reflect.Pointer && !from.IsNil() && from.Type().Elem() == to.Type() {
				return from.Elem().Interface(), nil
			}
			if input == nil && to.Kind() == reflect.Pointer {
				return nil, nil
			}
			if from.IsValid() && from.Type() == reflect.TypeFor[json.Number]() && to.Kind() == reflect.String {
				return nil, errors.New("expected a string, got a number")
			}
			if to.Kind() == reflect.Interface {
				return Native(input), nil
			}
			selected := to
			if selected.Kind() == reflect.Pointer {
				if selected.IsNil() {
					selected.Set(reflect.New(selected.Type().Elem()))
				}
			} else if selected.CanAddr() {
				selected = selected.Addr()
			}
			if selected.CanInterface() {
				if custom, ok := reflect.TypeAssert[Decoder](selected); ok {
					if err := custom.DecodeConfig(input); err != nil {
						return nil, err
					}
					return to.Interface(), nil
				}
			}
			if number, ok := input.(json.Number); ok {
				return decodeNumber(number, to.Type())
			}
			return input, nil
		}),
	})
	if err != nil {
		return err
	}
	if err := decoder.Decode(value); err != nil {
		return decodeError(err, value)
	}
	if strict && len(metadata.Unused) > 0 {
		slices.Sort(metadata.Unused)
		var failures []error
		for _, name := range metadata.Unused {
			failures = append(failures, &Error{Path: fieldPointer(name, value), Cause: ErrUnknownKey})
		}
		return errors.Join(failures...)
	}
	target.Elem().Set(next.Elem())
	return nil
}

func decodeNumber(number json.Number, target reflect.Type) (any, error) {
	kind := target.Kind()
	switch {
	case kind >= reflect.Int && kind <= reflect.Int64 || kind >= reflect.Uint && kind <= reflect.Uint64:
		exact, ok := new(big.Rat).SetString(string(number))
		if !ok || !exact.IsInt() {
			return nil, errors.New("expected a whole number")
		}
		if target.Kind() >= reflect.Uint && target.Kind() <= reflect.Uint64 {
			return strconv.ParseUint(exact.Num().String(), 10, target.Bits())
		}
		return strconv.ParseInt(exact.Num().String(), 10, target.Bits())
	case kind == reflect.Float32 || kind == reflect.Float64:
		return strconv.ParseFloat(string(number), target.Bits())
	default:
		return number, nil
	}
}

// Native owns a runtime value and converts numbers only when a native Go number
// represents them exactly. Large integers and inexact decimals retain Number.
func Native(value any) any {
	switch selected := value.(type) {
	case json.Number:
		exact, ok := new(big.Rat).SetString(string(selected))
		if !ok {
			return selected
		}
		if exact.IsInt() {
			integer := exact.Num()
			if !integer.IsInt64() {
				return selected
			}
			native := integer.Int64()
			if strconv.IntSize == 64 || native >= math.MinInt32 && native <= math.MaxInt32 {
				return int(native)
			}
			return native
		}
		fraction, err := selected.Float64()
		if err != nil || math.IsNaN(fraction) || math.IsInf(fraction, 0) {
			return selected
		}
		if new(big.Rat).SetFloat64(fraction).Cmp(exact) != 0 {
			return selected
		}
		return fraction
	case map[string]any:
		if selected == nil {
			return map[string]any(nil)
		}
		result := make(map[string]any, len(selected))
		for key, child := range selected {
			result[key] = Native(child)
		}
		return result
	case []any:
		if selected == nil {
			return []any(nil)
		}
		result := make([]any, len(selected))
		for i, child := range selected {
			result[i] = Native(child)
		}
		return result
	default:
		return value
	}
}

func isNil(value reflect.Value) bool {
	kind := value.Kind()
	return (kind == reflect.Pointer || kind == reflect.Map || kind == reflect.Slice || kind == reflect.Interface) && value.IsNil()
}

// currentError matches one node before its children are visited. A general As
// lookup alone would skip wrappers and could attach an inner path twice.
func currentError[T error](err error) (T, bool) {
	if err == nil {
		var zero T
		return zero, false
	}
	return reflect.TypeAssert[T](reflect.ValueOf(err))
}

func decodeError(err error, value any) error {
	if _, ok := currentError[*Error](err); ok {
		return err
	}
	if group, ok := err.(interface{ Unwrap() []error }); ok {
		var failures []error
		for _, failure := range group.Unwrap() {
			failures = append(failures, decodeError(failure, value))
		}
		return errors.Join(failures...)
	}
	if failure, ok := currentError[*mapstructure.DecodeError](err); ok {
		pointer := fieldPointer(failure.Name(), value)
		cause := failure.Unwrap()
		return prefixError(pointer, cause)
	}
	if cause := errors.Unwrap(err); cause != nil {
		return decodeError(cause, value)
	}
	return err
}

func prefixError(pointer string, err error) error {
	if nested, ok := currentError[*Error](err); ok {
		return &Error{Path: pointer + nested.Path, Cause: nested.Cause}
	}
	if group, ok := err.(interface{ Unwrap() []error }); ok {
		var failures []error
		for _, failure := range group.Unwrap() {
			failures = append(failures, prefixError(pointer, failure))
		}
		return errors.Join(failures...)
	}
	if cause := errors.Unwrap(err); cause != nil {
		return prefixError(pointer, cause)
	}
	return &Error{Path: pointer, Cause: err}
}

func fieldPointer(name string, value any) string {
	if name == "" {
		return ""
	}
	switch current := value.(type) {
	case map[string]any:
		keys := make([]string, 0, len(current))
		for key := range current {
			keys = append(keys, key)
		}
		slices.SortFunc(keys, func(a, b string) int { return len(b) - len(a) })
		for _, key := range keys {
			for _, prefix := range []string{key, "[" + key + "]"} {
				if name == prefix || strings.HasPrefix(name, prefix+".") || strings.HasPrefix(name, prefix+"[") {
					rest := strings.TrimPrefix(name[len(prefix):], ".")
					return "/" + strings.NewReplacer("~", "~0", "/", "~1").Replace(key) + fieldPointer(rest, current[key])
				}
			}
		}
	case []any:
		for i, child := range current {
			prefix := "[" + strconv.Itoa(i) + "]"
			if strings.HasPrefix(name, prefix) {
				return "/" + strconv.Itoa(i) + fieldPointer(strings.TrimPrefix(name[len(prefix):], "."), child)
			}
		}
	}
	return "/" + strings.NewReplacer("~", "~0", "/", "~1").Replace(name)
}

// Encode returns detached standard containers and scalar values. Serialization
// tags supply field names only; format libraries own syntax and presentation.
func Encode(value any) (any, error) { return encode(reflect.ValueOf(value)) }

func encode(value reflect.Value) (any, error) {
	if !value.IsValid() || isNil(value) {
		return nil, nil
	}
	if value.CanInterface() {
		if number, ok := reflect.TypeAssert[json.Number](value); ok {
			return number, nil
		}
	}
	if value.CanInterface() {
		if custom, ok := reflect.TypeAssert[Encoder](value); ok {
			next, err := custom.EncodeConfig()
			if err != nil {
				return nil, err
			}
			return Encode(next)
		}
	}
	if value.Kind() == reflect.Interface || value.Kind() == reflect.Pointer {
		return encode(value.Elem())
	}
	switch value.Kind() {
	case reflect.Struct:
		result := map[string]any{}
		for i := range value.NumField() {
			field := value.Type().Field(i)
			if !field.IsExported() {
				continue
			}
			parts := strings.Split(field.Tag.Get("yaml"), ",")
			if parts[0] == "-" {
				continue
			}
			child := value.Field(i)
			if slices.Contains(parts[1:], "omitempty") && empty(child) {
				continue
			}
			item, err := encode(child)
			if err != nil {
				return nil, At(parts[0], err)
			}
			if slices.Contains(parts[1:], "inline") {
				if item == nil {
					continue
				}
				entries, ok := item.(map[string]any)
				if !ok {
					return nil, errors.New("inline configuration field must be an object")
				}
				maps.Copy(result, entries)
				continue
			}
			name := parts[0]
			if name == "" {
				name = strings.ToLower(field.Name)
			}
			result[name] = item
		}
		return result, nil
	case reflect.Map:
		if value.Type().Key().Kind() != reflect.String {
			return nil, errors.New("configuration object keys must be strings")
		}
		result := map[string]any{}
		iterator := value.MapRange()
		for iterator.Next() {
			key := iterator.Key().String()
			child, err := encode(iterator.Value())
			if err != nil {
				return nil, At(key, err)
			}
			result[key] = child
		}
		return result, nil
	case reflect.Slice, reflect.Array:
		result := make([]any, value.Len())
		for i := range result {
			child, err := encode(value.Index(i))
			if err != nil {
				return nil, At(strconv.Itoa(i), err)
			}
			result[i] = child
		}
		return result, nil
	case reflect.String:
		return value.String(), nil
	case reflect.Bool:
		return value.Bool(), nil
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return value.Int(), nil
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return value.Uint(), nil
	case reflect.Float32, reflect.Float64:
		return value.Float(), nil
	case reflect.Invalid, reflect.Uintptr, reflect.Complex64, reflect.Complex128, reflect.Chan, reflect.Func, reflect.Interface, reflect.Pointer, reflect.UnsafePointer:
		return nil, fmt.Errorf("unsupported configuration value %s", value.Type())
	default:
		return nil, fmt.Errorf("unsupported configuration value %s", value.Type())
	}
}

func empty(value reflect.Value) bool {
	if value.CanInterface() {
		if zero, ok := reflect.TypeAssert[interface{ IsZero() bool }](value); ok {
			return zero.IsZero()
		}
	}
	kind := value.Kind()
	if kind == reflect.Slice || kind == reflect.Map || kind == reflect.String || kind == reflect.Array {
		return value.Len() == 0
	}
	return value.IsZero()
}
