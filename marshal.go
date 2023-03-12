package comm

import (
	"context"
	"encoding"
	"errors"
	"fmt"
	"reflect"
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
		FieldNum:  structField.Type.NumField(),
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

func parseSpec(spec string) (*fieldSpec, error) {
	if spec == "" {
		return &fieldSpec{OmitField: false}, nil
	}
	if spec == "-" || strings.HasPrefix(spec, "-,") {
		return &fieldSpec{OmitField: true}, nil
	}

	specElements := strings.Split(spec, ",")
	if len(specElements) == 0 {
		return &fieldSpec{}, nil
	}

	first := specElements[0]
	if strings.HasPrefix(first, "-") {
		if strings.HasSuffix(first, "=") {
			return &fieldSpec{
				// Writing "-flag=" or "--flag=" will merge into a single argc
				// value.
				//
				// "--flag=value0 value1 value2"
				//
				// "-flag=value0 value1 value2"
				SingleArgc: P(first),
			}, nil
		} else {
			// Writing "-flag" or "--flag" will prepend these as arguments in
			// the final list.
			return &fieldSpec{
				Prepend: []string{first},
			}, nil
		}
	}

	return &fieldSpec{}, nil
}

type fieldSpec struct {
	OmitField  bool
	SingleArgc *string
	Prepend    []string
	Append     []string
	Separator  *string
}

func (spec fieldSpec) Marshal(ctx context.Context, data any) ([]string, error) {
	if spec.OmitField {
		return nil, nil
	}

	dataArgs, err := marshalArgs(ctx, data)
	if err != nil {
		return nil, err
	}

	if len(dataArgs) == 0 {
		return nil, nil
	}

	if spec.Separator != nil {
		joined := strings.Join(dataArgs, F(spec.Separator))
		dataArgs = []string{joined}
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
