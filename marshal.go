package comm

import (
	"context"
	"encoding"
	"errors"
	"fmt"
	"reflect"
	"regexp"
	"strconv"
	"strings"
)

// ArgsMarshaler provides an implementation to marshal objects into their
// respective set of command line arguments.
type ArgsMarshaler interface {
	// MarshalArgs is called with a context that is expected to be bounded -
	// whether to the process lifetime or something else entirely. This allows
	// callers to (in theory) use the context cancellation signal to convey
	// _resource cleanup_ signaling. For example, buildings args for a temporary
	// directory may be opaque in the arguments provided and automatically
	// cleaned up when the context is cancelled.
	MarshalArgs(ctx context.Context) ([]string, error)
}

// MarshalArgs marshals the provided data into the string list of command line
// arguments.
func MarshalArgs(ctx context.Context, data any) ([]string, error) {
	return marshalArgs(ctx, data)
}

// ArgsMarshalerFunc is a type wrapper for a function that produces and handles
// context cancellation (as in ArgsMarshaler) which may be used independently of
// a method func. For example, one can use this to embed common behavior or
// closure bound handling in a struct (`type Foo struct { ArgsMarshalerFunc }`).
type ArgsMarshalerFunc func(context.Context) ([]string, error)

// MarshalArgs implements ArgsMarshaler
func (fn ArgsMarshalerFunc) MarshalArgs(ctx context.Context) ([]string, error) {
	return fn(ctx)
}

var _ ArgsMarshaler = (ArgsMarshalerFunc)(nil)

// marshalArgs is the internal walker that builds the command line arguments -
// descending into the types and marshaling the objects into the resulting set
// of arguments.
func marshalArgs(ctx context.Context, data any) ([]string, error) {
	if data == nil {
		return nil, nil
	}

	switch v := data.(type) {
	case ArgsMarshaler:
		return v.MarshalArgs(ctx)
	case string:
		return []string{v}, nil
	case *string:
		if v == nil {
			return nil, nil
		}
		return []string{*v}, nil
	case bool:
		return []string{strconv.FormatBool(v)}, nil
	case *bool:
		if v == nil {
			return nil, nil
		}
		return []string{strconv.FormatBool(*v)}, nil
	case []string:
		ds := make([]string, len(v))
		copy(ds, v)
		return ds, nil
	case []*string:
		ds := []string{}
		for _, s := range v {
			if s != nil {
				ds = append(ds, *s)
			}
		}
		return ds, nil
	case encoding.TextMarshaler:
		text, err := v.MarshalText()
		if err != nil {
			return nil, err
		}
		return []string{string(text)}, nil
	case fmt.Stringer:
		return []string{v.String()}, nil
	}

	reflected := reflect.ValueOf(data)
	switch reflected.Kind() {
	case reflect.Struct:
		return marshalStructFields(ctx, reflected)
	case reflect.Ptr:
		// not that its expected, but we could see **commStruct, so unwrap and
		// marshal again on that value.
		if reflected.Elem().CanInterface() {
			return marshalArgs(ctx, reflected.Elem().Interface())
		}
	}

	return nil, errors.New("unsupported type(s)")
}

// marshalStructFields marshals the fields of a struct into args. The field's
// structtag may be used to configure, even omit, the transformation of the
// field value into args.
func marshalStructFields(ctx context.Context, value reflect.Value) ([]string, error) {
	if value.Kind() != reflect.Struct {
		return nil, errors.New("not a struct value")
	}

	structType := value.Type()
	numFields := value.NumField()

	var marshaledArgs []string
	for i := 0; i < numFields; i++ {
		if value.Field(i).CanInterface() {
			structField := structType.Field(i)
			tagSpec := structField.Tag.Get(structTagName(ctx))

			fieldArgs, err := marshalSpec(ctx, tagSpec, value.Field(i))
			if err != nil {
				return nil, newMarshalArgFieldError(structField, err)
			}

			marshaledArgs = append(marshaledArgs, fieldArgs...)
		}
	}

	return marshaledArgs, nil
}

func newMarshalArgFieldError(structField reflect.StructField, err error) error {
	return MarshalStructFieldError{
		FieldName: structField.Name,
		FieldNum:  structField.Index[0],
		Err:       err,
	}
}

func marshalSpec(ctx context.Context, specString string, value reflect.Value) ([]string, error) {
	spec, err := parseSpec(specString)
	if err != nil {
		return nil, err
	}

	return spec.Marshal(ctx, value.Interface())
}

var separatorRegex = regexp.MustCompile(",[^,]")

func parseSpec(spec string) (*fieldSpec, error) {
	if spec == "" {
		return &fieldSpec{OmitField: false}, nil
	}
	if spec == "-" || strings.HasPrefix(spec, "-,") {
		return &fieldSpec{OmitField: true}, nil
	}

	// TODO: reader w/ splits matching on ",[^,]"
	specElements := strings.Split(spec, ",")
	if len(specElements) == 0 {
		return &fieldSpec{}, nil
	}

	parsed := &fieldSpec{}

	first := specElements[0]

	if strings.HasPrefix(first, "-") {
		if strings.HasSuffix(first, "=") {
			// Writing "-flag=" or "--flag=" will merge into a single argc
			// value.
			//
			// "--flag=value0 value1 value2"
			//
			// "-flag=value0 value1 value2"
			parsed.SingleArgc = P(first)
		} else {
			parsed.Prepend = append(parsed.Prepend, first)
		}
	}

	for i := 0; i < len(specElements); i++ {
		kvparts := strings.SplitN(specElements[i], "=", 2)
		if len(kvparts) == 1 {
			switch kvparts[0] {
			case "omitempty":
				parsed.OmitEmpty = true

				// TODO: the below.
				//
				// case "join":
				// 	parsed.Separator = P(",")
			}
		} else {
			k, v := kvparts[0], kvparts[1]
			switch k {
			case "true":
				parsed.Bool.True = P(v)
			case "false":
				parsed.Bool.False = P(v)
			}
		}
	}

	return parsed, nil
}

type fieldSpec struct {
	OmitField bool
	OmitEmpty bool

	SingleArgc *string
	Prepend    []string
	Append     []string
	Separator  *string

	Bool struct {
		True  *string
		False *string
	}
}

func (spec fieldSpec) Marshal(ctx context.Context, data any) ([]string, error) {
	if spec.OmitField {
		return nil, nil
	}

	dataArgs, err := spec.marshalArgs(ctx, data)
	if err != nil {
		return nil, err
	}

	if len(dataArgs) == 0 {
		return nil, nil
	}

	if spec.SingleArgc != nil {
		if len(dataArgs) == 1 {
			dataArgs = []string{F(spec.SingleArgc) + dataArgs[0]}
		} else {
			joined := strings.Join(dataArgs, " ")
			dataArgs = []string{F(spec.SingleArgc) + joined}
		}
	}

	args := append(spec.Prepend, dataArgs...)
	args = append(args, spec.Append...)

	return args, nil
}

func (spec *fieldSpec) marshalArgs(ctx context.Context, data any) ([]string, error) {
	// apply: omitempty
	if spec.OmitEmpty {
		// Check if the type has a method to decide if the value is zero (or
		// empty) rather than general reflection.
		switch v := data.(type) {
		case interface{ IsEmpty() bool }:
			if v.IsEmpty() {
				return nil, nil
			}
			// not empty, carry on

		case interface{ IsZero() bool }:
			if v.IsZero() {
				return nil, nil
			}
			// not a zero value, carry on
		}

		// fallback to reflection of the type's zero value.
		dataValue := reflect.ValueOf(data)
		if dataValue.IsZero() {
			return nil, nil
		} else {
			// not a zero value, carry on
		}
	}

	marshaledArgs, err := marshalArgs(ctx, data)
	if err != nil {
		return nil, err
	}

	if len(marshaledArgs) == 1 {
		switch {
		// apply: true
		case marshaledArgs[0] == "true" &&
			F(spec.Bool.True) != "":

			return []string{F(spec.Bool.True)}, nil

		// apply: false
		case marshaledArgs[0] == "false":
			// we have a value, use it instead
			if F(spec.Bool.False) != "" {
				return []string{F(spec.Bool.False)}, nil
			}

			// omit if there's a "true" value but no false one.
			if F(spec.Bool.True) != "" {
				return nil, nil
			}

			return marshaledArgs, nil

		default:
			return marshaledArgs, nil
		}
	}

	// apply:
	if spec.Separator != nil {
		joined := strings.Join(marshaledArgs, F(spec.Separator))
		// TODO: support escaping convenience
		marshaledArgs = []string{joined}
	}

	// apply: --foo=, -foo=,
	if spec.SingleArgc != nil {
		if len(marshaledArgs) == 1 {
			marshaledArgs = []string{F(spec.SingleArgc) + marshaledArgs[0]}
		} else {
			joined := strings.Join(marshaledArgs, " ")
			marshaledArgs = []string{F(spec.SingleArgc) + joined}
		}
	}

	return marshaledArgs, nil
}

func structTagName(context.Context) string {
	return "comm"
}

type MarshalStructFieldError struct {
	FieldName string
	FieldNum  int
	Err       error
}

func (e MarshalStructFieldError) Error() string {
	return fmt.Sprintf("field %q error: %v", e.FieldName, e.Err)
}

func (e MarshalStructFieldError) Unwrap() error {
	return e.Err
}
