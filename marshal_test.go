package comm

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/jahkeup/testthings"
)

func TestMarshalArgs_primitives(t *testing.T) {
	testcases := map[string]struct {
		data     any
		expected []string
		err      error
	}{
		"string": {
			data:     "some string",
			expected: []string{"some string"},
		},
		"strings": {
			data:     []string{"some string", "", "foo"},
			expected: []string{"some string", "", "foo"},
		},
		"*string": {
			data:     P("some string"),
			expected: []string{"some string"},
		},
		"[]*string": {
			data:     PS([]string{"some string", "", "neat"}),
			expected: []string{"some string", "", "neat"},
		},
		"[]*string with nils": {
			data:     []*string{P("head"), nil, nil, P("tail"), nil},
			expected: []string{"head", "tail"},
		},
	}

	for name, tc := range testcases {
		t.Run(name, func(t *testing.T) {
			actual, err := marshalArgs(testthings.C(t), tc.data)
			assert.NoError(t, err)
			assert.ElementsMatch(t, tc.expected, actual)
		})
	}
}

type HardcodedArgs struct {
	extraField string // used to detect inappropriate fallback

	args []string

	extraTrailingField string // used to detect inappropriate fallback
}

func (h HardcodedArgs) MarshalArgs(context.Context) ([]string, error) {
	return []string(h.args), nil
}

func TestMarshalArgs_trivialTypes(t *testing.T) {
	testcases := map[string]struct {
		data      any
		expected  []string
		assertErr assert.ErrorAssertionFunc
	}{
		"trivial type": {
			data: struct {
				Field1 string
				Field2 *string

				Positional []string
			}{
				Field1:     "one",
				Positional: []string{"last"},
			},
			expected:  []string{"one", "last"},
			assertErr: assert.NoError,
		},
		"unsupported value": {
			data:      map[string]string{"key": "value"},
			expected:  nil,
			assertErr: assert.Error,
		},
		"nil": {
			data:      nil,
			expected:  nil,
			assertErr: assert.NoError,
		},
		"ArgsMarshaler": {
			data: HardcodedArgs{
				args:               []string{"list", "of", "args"},
				extraField:         "foo",
				extraTrailingField: "foo",
			},
			expected:  []string{"list", "of", "args"},
			assertErr: assert.NoError,
		},
		"omit field": {
			data: struct {
				OmittedField             string   `comm:"-"`
				OmittedFieldWithExtra    string   `comm:"-,foo,k=v,eee,,"`
				OmittedFieldBool         bool     `comm:"true=--dry-run,eee"`
				OmittedBecauseEmpty      string   `comm:"--no-value=,omitempty"`
				OmittedBecauseEmptySlice []string `comm:"--omittedbecauseemptyslice"`

				SomeBool  bool `comm:"true=--some-bool=yes"`
				SomeField string
			}{
				OmittedField:          "omitted",
				OmittedFieldWithExtra: "also omitted",

				SomeBool:  true,
				SomeField: "some field",
			},
			expected:  []string{"--some-bool=yes", "some field"},
			assertErr: assert.NoError,
		},
		"spec basic": {
			data: struct {
				DashDashConfig string `comm:"--dashdashconfig"`
				DashConfig     string `comm:"-dashconfig"`

				DashConfigSingle     string `comm:"-dashconfig="`
				DashDashConfigSingle string `comm:"--dashdashconfig="`

				DashDashConfigSingleSlice  []string  `comm:"--dashconfigslice="`
				DashDashConfigSingleSliceP []*string `comm:"--dashconfigslicep="`

				DashDashCommaJoined []string `comm:"--dashdashcommajoined,join"`
			}{
				DashDashConfig: "value1",
				DashConfig:     "value1",

				DashConfigSingle:     "value1",
				DashDashConfigSingle: "value1",

				DashDashConfigSingleSlice:  []string{"value1", "value2", "value3"},
				DashDashConfigSingleSliceP: PS([]string{"value1", "value2", "value3"}),

				DashDashCommaJoined: []string{"value1", "value2", "value3"},
			},
			expected: []string{
				"--dashdashconfig", "value1",
				"-dashconfig", "value1",

				"-dashconfig=value1",
				"--dashdashconfig=value1",

				"--dashconfigslice=value1 value2 value3",
				"--dashconfigslicep=value1 value2 value3",
				"--dashdashcommajoined", "value1,value2,value3",
			},
			assertErr: assert.NoError,
		},
		"nested": {
			data: struct {
				Foo struct {
					NestedThing bool `comm:"--config"`
				}
				// should be omitted because its nil
				TopLevelTrailer *string `comm:"--trailer"`
			}{
				Foo: struct {
					NestedThing bool "comm:\"--config\""
				}{
					NestedThing: true,
				},
				TopLevelTrailer: nil,
			},
			expected: []string{
				"--config", "true",
			},
			assertErr: assert.NoError,
		},
		"embed": {
			data: struct {
				ArgsMarshalerFunc
				IgnoredField string
			}{
				ArgsMarshalerFunc: func(ctx context.Context) ([]string, error) {
					return []string{"called"}, nil
				},
				IgnoredField: "ignored because embedded MarshalArgs called^^",
			},
			expected:  []string{"called"},
			assertErr: assert.NoError,
		},
	}

	for name, tc := range testcases {
		t.Run(name, func(t *testing.T) {
			actual, err := MarshalArgs(testthings.C(t), tc.data)
			tc.assertErr(t, err)
			t.Log(actual)
			assert.Equal(t, tc.expected, actual)

		})
	}
}
