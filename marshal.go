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
	MarshalArgs(ctx context.Context) ([]string, error)
}

// MarshalArgs marshals the provided data into the string list of command line
// arguments.
func MarshalArgs(ctx context.Context, data any) ([]string, error) {
	return marshalArgs(ctx, data)
}

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
		if reflected.Elem().Kind() == reflect.Struct {
			return marshalStructFields(ctx, reflected)
		}
	}

	return nil, errors.New("none")
}

// marshalStructFields marshals the fields of a struct into args. The field's
// structtag may be used to configure, even omit, the transformation of the
// field value into args.
func marshalStructFields(ctx context.Context, value reflect.Value) ([]string, error) {
	if value.Kind() != reflect.Struct {
		return nil, errors.New("not a struct value")
	}

	structType := value.Type()

	var marshaledArgs []string
	numFields := value.NumField()
	for i := 0; i < numFields; i++ {
		if value.Field(i).CanInterface() {
			structField := structType.Field(i)
			tagSpec := structField.Tag.Get(structTagName(ctx))

			fieldArgs, err := marshalSpec(ctx, tagSpec, value.Field(i).Interface())
			if err != nil {
				return nil, newMarshalArgFieldError(structField, err)
			}

			marshaledArgs = append(marshaledArgs, fieldArgs...)
		}
	}

	return marshaledArgs, nil
}

func newMarshalArgFieldError(structField reflect.StructField, err error) error {
	panic("unimplemented")
}

func marshalSpec(ctx context.Context, spec string, data any) ([]string, error) {
	// comm:"-[,...]"
	if strings.HasPrefix(spec, "-") {
		return nil, nil
	}

	return marshalArgs(ctx, data)
}

func structTagName(context.Context) string {
	return "comm"
}
