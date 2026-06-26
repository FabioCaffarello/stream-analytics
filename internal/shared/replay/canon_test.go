package replay

import (
	"bytes"
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/FabioCaffarello/stream-analytics/internal/shared/problem"
)

// ---------------------------------------------------------------------------
// 1. canonicalizeJSONRaw
// ---------------------------------------------------------------------------

func TestCanonicalizeJSONRaw_RoundTrip(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "simple object key ordering",
			input: `{"z":1,"a":2}`,
			want:  `{"a":2,"z":1}`,
		},
		{
			name:  "nested object key ordering",
			input: `{"b":{"d":4,"c":3},"a":1}`,
			want:  `{"a":1,"b":{"c":3,"d":4}}`,
		},
		{
			name:  "array preserved",
			input: `[3,1,2]`,
			want:  `[3,1,2]`,
		},
		{
			name:  "whitespace normalized",
			input: `  { "b" : 1 , "a" : 2 }  `,
			want:  `{"a":2,"b":1}`,
		},
		{
			name:  "string value with special chars",
			input: `{"msg":"hello\nworld"}`,
			want:  `{"msg":"hello\nworld"}`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, p := canonicalizeJSONRaw([]byte(tt.input))
			if p != nil {
				t.Fatalf("unexpected problem: %v", p)
			}
			if string(got) != tt.want {
				t.Fatalf("got %s, want %s", got, tt.want)
			}

			// Round-trip: canonicalizing the output again must produce identical bytes.
			got2, p := canonicalizeJSONRaw(got)
			if p != nil {
				t.Fatalf("round-trip unexpected problem: %v", p)
			}
			if !bytes.Equal(got, got2) {
				t.Fatalf("round-trip instability: first=%s second=%s", got, got2)
			}
		})
	}
}

func TestCanonicalizeJSONRaw_EmptyInput(t *testing.T) {
	_, p := canonicalizeJSONRaw([]byte{})
	if p == nil {
		t.Fatal("expected problem for empty input")
	}
	if p.Code != problem.ValidationFailed {
		t.Fatalf("code=%s want=%s", p.Code, problem.ValidationFailed)
	}
}

func TestCanonicalizeJSONRaw_WhitespaceOnlyInput(t *testing.T) {
	_, p := canonicalizeJSONRaw([]byte("   \t\n  "))
	if p == nil {
		t.Fatal("expected problem for whitespace-only input")
	}
	if p.Code != problem.ValidationFailed {
		t.Fatalf("code=%s want=%s", p.Code, problem.ValidationFailed)
	}
}

func TestCanonicalizeJSONRaw_InvalidJSON(t *testing.T) {
	inputs := []string{
		`{not json}`,
		`{"key": }`,
		`[1, 2,]`,
		`"unterminated`,
	}
	for _, input := range inputs {
		t.Run(input, func(t *testing.T) {
			_, p := canonicalizeJSONRaw([]byte(input))
			if p == nil {
				t.Fatal("expected problem for invalid JSON")
			}
		})
	}
}

// ---------------------------------------------------------------------------
// 2. parseJSONValue
// ---------------------------------------------------------------------------

func TestParseJSONValue_NumberPreservation(t *testing.T) {
	raw := []byte(`{"big":12345678901234567890}`)
	val, p := parseJSONValue(raw)
	if p != nil {
		t.Fatalf("unexpected problem: %v", p)
	}

	m, ok := val.(map[string]any)
	if !ok {
		t.Fatalf("expected map, got %T", val)
	}
	num, ok := m["big"].(json.Number)
	if !ok {
		t.Fatalf("expected json.Number, got %T", m["big"])
	}
	if num.String() != "12345678901234567890" {
		t.Fatalf("number=%s want=12345678901234567890", num)
	}
}

func TestParseJSONValue_TrailingContent(t *testing.T) {
	raw := []byte(`{"a":1}{"b":2}`)
	_, p := parseJSONValue(raw)
	if p == nil {
		t.Fatal("expected problem for trailing content")
	}
	if p.Code != problem.ValidationFailed {
		t.Fatalf("code=%s want=%s", p.Code, problem.ValidationFailed)
	}
	if !strings.Contains(p.Message, "multiple documents") {
		t.Fatalf("message should mention multiple documents: %s", p.Message)
	}
}

func TestParseJSONValue_WhitespaceInput(t *testing.T) {
	_, p := parseJSONValue([]byte("   "))
	if p == nil {
		t.Fatal("expected problem for whitespace input")
	}
	if p.Code != problem.ValidationFailed {
		t.Fatalf("code=%s want=%s", p.Code, problem.ValidationFailed)
	}
}

func TestParseJSONValue_NilInput(t *testing.T) {
	_, p := parseJSONValue(nil)
	if p == nil {
		t.Fatal("expected problem for nil input")
	}
	if p.Code != problem.ValidationFailed {
		t.Fatalf("code=%s want=%s", p.Code, problem.ValidationFailed)
	}
}

func TestParseJSONValue_ValidScalars(t *testing.T) {
	tests := []struct {
		name string
		raw  string
	}{
		{"null", "null"},
		{"true", "true"},
		{"false", "false"},
		{"string", `"hello"`},
		{"number", "42"},
		{"float", "3.14"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, p := parseJSONValue([]byte(tt.raw))
			if p != nil {
				t.Fatalf("unexpected problem: %v", p)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// 3. writeCanonicalFloat
// ---------------------------------------------------------------------------

func TestWriteCanonicalFloat_Float64(t *testing.T) {
	tests := []struct {
		name    string
		value   float64
		want    string
		wantErr bool
	}{
		{name: "zero", value: 0.0, want: "0"},
		{name: "negative zero", value: math.Copysign(0, -1), want: "0"},
		{name: "positive", value: 3.14, want: "3.14"},
		{name: "negative", value: -2.5, want: "-2.5"},
		{name: "large integer", value: 1e18, want: "1e+18"},
		{name: "small fraction", value: 0.000001, want: "1e-06"},
		{name: "NaN", value: math.NaN(), wantErr: true},
		{name: "positive infinity", value: math.Inf(1), wantErr: true},
		{name: "negative infinity", value: math.Inf(-1), wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var b bytes.Buffer
			handled, p := writeCanonicalFloat(&b, tt.value)
			if !handled {
				t.Fatal("expected handled=true for float64")
			}
			if tt.wantErr {
				if p == nil {
					t.Fatal("expected problem for invalid float")
				}
				if p.Code != problem.ValidationFailed {
					t.Fatalf("code=%s want=%s", p.Code, problem.ValidationFailed)
				}
				return
			}
			if p != nil {
				t.Fatalf("unexpected problem: %v", p)
			}
			if b.String() != tt.want {
				t.Fatalf("got=%q want=%q", b.String(), tt.want)
			}
		})
	}
}

func TestWriteCanonicalFloat_Float32(t *testing.T) {
	tests := []struct {
		name    string
		value   float32
		want    string
		wantErr bool
	}{
		{name: "zero", value: 0.0, want: "0"},
		{name: "positive", value: 1.5, want: "1.5"},
		{name: "negative", value: -0.25, want: "-0.25"},
		{name: "NaN", value: float32(math.NaN()), wantErr: true},
		{name: "positive infinity", value: float32(math.Inf(1)), wantErr: true},
		{name: "negative infinity", value: float32(math.Inf(-1)), wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var b bytes.Buffer
			handled, p := writeCanonicalFloat(&b, tt.value)
			if !handled {
				t.Fatal("expected handled=true for float32")
			}
			if tt.wantErr {
				if p == nil {
					t.Fatal("expected problem for invalid float32")
				}
				if p.Code != problem.ValidationFailed {
					t.Fatalf("code=%s want=%s", p.Code, problem.ValidationFailed)
				}
				return
			}
			if p != nil {
				t.Fatalf("unexpected problem: %v", p)
			}
			if b.String() != tt.want {
				t.Fatalf("got=%q want=%q", b.String(), tt.want)
			}
		})
	}
}

func TestWriteCanonicalFloat_NonFloatReturnsFalse(t *testing.T) {
	var b bytes.Buffer
	handled, p := writeCanonicalFloat(&b, "not a float")
	if handled {
		t.Fatal("expected handled=false for string")
	}
	if p != nil {
		t.Fatalf("unexpected problem: %v", p)
	}
}

func TestWriteCanonicalFloat_NegativeZeroVsPositiveZero(t *testing.T) {
	var bNeg bytes.Buffer
	_, p := writeCanonicalFloat(&bNeg, math.Copysign(0, -1))
	if p != nil {
		t.Fatalf("negative zero: unexpected problem: %v", p)
	}

	var bPos bytes.Buffer
	_, p = writeCanonicalFloat(&bPos, 0.0)
	if p != nil {
		t.Fatalf("positive zero: unexpected problem: %v", p)
	}

	// Both must produce identical canonical output to preserve determinism.
	if bNeg.String() != bPos.String() {
		t.Fatalf("-0 produced %q, +0 produced %q -- must be identical", bNeg.String(), bPos.String())
	}
}

// ---------------------------------------------------------------------------
// 4. writeCanonicalInt / writeCanonicalUint boundaries
// ---------------------------------------------------------------------------

func TestWriteCanonicalInt_Boundaries(t *testing.T) {
	tests := []struct {
		name  string
		value any
		want  string
	}{
		{name: "int64 max", value: int64(math.MaxInt64), want: "9223372036854775807"},
		{name: "int64 min", value: int64(math.MinInt64), want: "-9223372036854775808"},
		{name: "int64 zero", value: int64(0), want: "0"},
		{name: "int32 max", value: int32(math.MaxInt32), want: "2147483647"},
		{name: "int32 min", value: int32(math.MinInt32), want: "-2147483648"},
		{name: "int16 max", value: int16(math.MaxInt16), want: "32767"},
		{name: "int8 max", value: int8(math.MaxInt8), want: "127"},
		{name: "int8 min", value: int8(math.MinInt8), want: "-128"},
		{name: "int zero", value: int(0), want: "0"},
		{name: "int negative", value: int(-42), want: "-42"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var b bytes.Buffer
			handled, p := writeCanonicalInt(&b, tt.value)
			if !handled {
				t.Fatal("expected handled=true")
			}
			if p != nil {
				t.Fatalf("unexpected problem: %v", p)
			}
			if b.String() != tt.want {
				t.Fatalf("got=%q want=%q", b.String(), tt.want)
			}
		})
	}
}

func TestWriteCanonicalUint_Boundaries(t *testing.T) {
	tests := []struct {
		name  string
		value any
		want  string
	}{
		{name: "uint64 max", value: uint64(math.MaxUint64), want: "18446744073709551615"},
		{name: "uint64 zero", value: uint64(0), want: "0"},
		{name: "uint32 max", value: uint32(math.MaxUint32), want: "4294967295"},
		{name: "uint16 max", value: uint16(math.MaxUint16), want: "65535"},
		{name: "uint8 max", value: uint8(math.MaxUint8), want: "255"},
		{name: "uint zero", value: uint(0), want: "0"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var b bytes.Buffer
			handled, p := writeCanonicalUint(&b, tt.value)
			if !handled {
				t.Fatal("expected handled=true")
			}
			if p != nil {
				t.Fatalf("unexpected problem: %v", p)
			}
			if b.String() != tt.want {
				t.Fatalf("got=%q want=%q", b.String(), tt.want)
			}
		})
	}
}

func TestWriteCanonicalInt_NonIntReturnsFalse(t *testing.T) {
	var b bytes.Buffer
	handled, p := writeCanonicalInt(&b, "string")
	if handled {
		t.Fatal("expected handled=false for string")
	}
	if p != nil {
		t.Fatalf("unexpected problem: %v", p)
	}
}

func TestWriteCanonicalUint_NonUintReturnsFalse(t *testing.T) {
	var b bytes.Buffer
	handled, p := writeCanonicalUint(&b, int64(42))
	if handled {
		t.Fatal("expected handled=false for int64 in uint handler")
	}
	if p != nil {
		t.Fatalf("unexpected problem: %v", p)
	}
}

// ---------------------------------------------------------------------------
// 5. writeCanonicalObject
// ---------------------------------------------------------------------------

func TestWriteCanonicalObject_KeyOrdering(t *testing.T) {
	obj := map[string]any{
		"zebra":    1,
		"apple":    2,
		"mango":    3,
		"banana":   4,
		"1numeric": 5,
	}
	var b bytes.Buffer
	p := writeCanonicalObject(&b, obj)
	if p != nil {
		t.Fatalf("unexpected problem: %v", p)
	}
	want := `{"1numeric":5,"apple":2,"banana":4,"mango":3,"zebra":1}`
	if b.String() != want {
		t.Fatalf("got=%s\nwant=%s", b.String(), want)
	}
}

func TestWriteCanonicalObject_Empty(t *testing.T) {
	var b bytes.Buffer
	p := writeCanonicalObject(&b, map[string]any{})
	if p != nil {
		t.Fatalf("unexpected problem: %v", p)
	}
	if b.String() != "{}" {
		t.Fatalf("got=%q want={}", b.String())
	}
}

func TestWriteCanonicalObject_NestedObjects(t *testing.T) {
	obj := map[string]any{
		"outer_b": map[string]any{
			"inner_z": "last",
			"inner_a": "first",
		},
		"outer_a": "val",
	}
	var b bytes.Buffer
	p := writeCanonicalObject(&b, obj)
	if p != nil {
		t.Fatalf("unexpected problem: %v", p)
	}
	want := `{"outer_a":"val","outer_b":{"inner_a":"first","inner_z":"last"}}`
	if b.String() != want {
		t.Fatalf("got=%s\nwant=%s", b.String(), want)
	}
}

func TestWriteCanonicalObject_Deterministic(t *testing.T) {
	// Repeat encoding the same map many times -- must always produce the same output.
	obj := map[string]any{
		"c": 3,
		"a": 1,
		"b": 2,
	}
	var first string
	for i := 0; i < 100; i++ {
		var b bytes.Buffer
		p := writeCanonicalObject(&b, obj)
		if p != nil {
			t.Fatalf("iteration %d: unexpected problem: %v", i, p)
		}
		if i == 0 {
			first = b.String()
		} else if b.String() != first {
			t.Fatalf("iteration %d produced %q, want %q", i, b.String(), first)
		}
	}
}

// ---------------------------------------------------------------------------
// 6. writeCanonicalArray
// ---------------------------------------------------------------------------

func TestWriteCanonicalArray_Empty(t *testing.T) {
	var b bytes.Buffer
	p := writeCanonicalArray(&b, []any{})
	if p != nil {
		t.Fatalf("unexpected problem: %v", p)
	}
	if b.String() != "[]" {
		t.Fatalf("got=%q want=[]", b.String())
	}
}

func TestWriteCanonicalArray_NestedArrays(t *testing.T) {
	arr := []any{
		[]any{json.Number("1"), json.Number("2")},
		[]any{json.Number("3")},
	}
	var b bytes.Buffer
	p := writeCanonicalArray(&b, arr)
	if p != nil {
		t.Fatalf("unexpected problem: %v", p)
	}
	if b.String() != "[[1,2],[3]]" {
		t.Fatalf("got=%q want=[[1,2],[3]]", b.String())
	}
}

func TestWriteCanonicalArray_MixedTypes(t *testing.T) {
	arr := []any{
		nil,
		true,
		false,
		json.Number("42"),
		"hello",
		map[string]any{"b": json.Number("2"), "a": json.Number("1")},
		[]any{json.Number("99")},
	}
	var b bytes.Buffer
	p := writeCanonicalArray(&b, arr)
	if p != nil {
		t.Fatalf("unexpected problem: %v", p)
	}
	want := `[null,true,false,42,"hello",{"a":1,"b":2},[99]]`
	if b.String() != want {
		t.Fatalf("got=%s\nwant=%s", b.String(), want)
	}
}

func TestWriteCanonicalArray_PreservesOrder(t *testing.T) {
	arr := []any{json.Number("3"), json.Number("1"), json.Number("2")}
	var b bytes.Buffer
	p := writeCanonicalArray(&b, arr)
	if p != nil {
		t.Fatalf("unexpected problem: %v", p)
	}
	if b.String() != "[3,1,2]" {
		t.Fatalf("array order must be preserved: got=%q", b.String())
	}
}

// ---------------------------------------------------------------------------
// 7. writeCanonicalScalar
// ---------------------------------------------------------------------------

func TestWriteCanonicalScalar_Nil(t *testing.T) {
	var b bytes.Buffer
	handled, p := writeCanonicalScalar(&b, nil)
	if !handled {
		t.Fatal("expected handled=true for nil")
	}
	if p != nil {
		t.Fatalf("unexpected problem: %v", p)
	}
	if b.String() != "null" {
		t.Fatalf("got=%q want=null", b.String())
	}
}

func TestWriteCanonicalScalar_Bool(t *testing.T) {
	tests := []struct {
		value bool
		want  string
	}{
		{true, "true"},
		{false, "false"},
	}
	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			var b bytes.Buffer
			handled, p := writeCanonicalScalar(&b, tt.value)
			if !handled {
				t.Fatal("expected handled=true for bool")
			}
			if p != nil {
				t.Fatalf("unexpected problem: %v", p)
			}
			if b.String() != tt.want {
				t.Fatalf("got=%q want=%q", b.String(), tt.want)
			}
		})
	}
}

func TestWriteCanonicalScalar_StringSpecialChars(t *testing.T) {
	tests := []struct {
		name  string
		value string
		want  string
	}{
		{name: "empty string", value: "", want: `""`},
		{name: "newline", value: "line\nbreak", want: `"line\nbreak"`},
		{name: "tab", value: "with\ttab", want: `"with\ttab"`},
		{name: "quotes", value: `say "hello"`, want: `"say \"hello\""`},
		{name: "backslash", value: `back\slash`, want: `"back\\slash"`},
		{name: "unicode", value: "cafe\u0301", want: `"caf\u00e9"`},
		{name: "angle brackets", value: "<script>", want: `"\u003cscript\u003e"`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var b bytes.Buffer
			handled, p := writeCanonicalScalar(&b, tt.value)
			if !handled {
				t.Fatal("expected handled=true for string")
			}
			if p != nil {
				t.Fatalf("unexpected problem: %v", p)
			}
			// Validate that the output is valid JSON by round-tripping.
			var decoded string
			if err := json.Unmarshal(b.Bytes(), &decoded); err != nil {
				t.Fatalf("output is not valid JSON string: %v (raw=%s)", err, b.String())
			}
			if decoded != tt.value {
				t.Fatalf("round-trip mismatch: decoded=%q want=%q", decoded, tt.value)
			}
		})
	}
}

func TestWriteCanonicalScalar_JSONNumber(t *testing.T) {
	tests := []struct {
		name    string
		value   json.Number
		want    string
		wantErr bool
	}{
		{name: "integer", value: json.Number("42"), want: "42"},
		{name: "negative", value: json.Number("-17"), want: "-17"},
		{name: "float", value: json.Number("3.14"), want: "3.14"},
		{name: "exponent", value: json.Number("1e10"), want: "1e10"},
		{name: "zero", value: json.Number("0"), want: "0"},
		{name: "large number", value: json.Number("99999999999999999999"), want: "99999999999999999999"},
		{name: "invalid number", value: json.Number("not_a_number"), wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var b bytes.Buffer
			handled, p := writeCanonicalScalar(&b, tt.value)
			if !handled {
				t.Fatal("expected handled=true for json.Number")
			}
			if tt.wantErr {
				if p == nil {
					t.Fatal("expected problem for invalid json.Number")
				}
				if p.Code != problem.ValidationFailed {
					t.Fatalf("code=%s want=%s", p.Code, problem.ValidationFailed)
				}
				return
			}
			if p != nil {
				t.Fatalf("unexpected problem: %v", p)
			}
			if b.String() != tt.want {
				t.Fatalf("got=%q want=%q", b.String(), tt.want)
			}
		})
	}
}

func TestWriteCanonicalScalar_UnhandledType(t *testing.T) {
	var b bytes.Buffer
	handled, p := writeCanonicalScalar(&b, int64(42))
	if handled {
		t.Fatal("expected handled=false for int64")
	}
	if p != nil {
		t.Fatalf("unexpected problem: %v", p)
	}
}

// ---------------------------------------------------------------------------
// 8. marshalCanonicalJSON full round-trip
// ---------------------------------------------------------------------------

func TestMarshalCanonicalJSON_FullRoundTrip(t *testing.T) {
	tests := []struct {
		name  string
		value any
		want  string
	}{
		{
			name:  "nil",
			value: nil,
			want:  "null",
		},
		{
			name:  "bool true",
			value: true,
			want:  "true",
		},
		{
			name:  "string",
			value: "hello",
			want:  `"hello"`,
		},
		{
			name:  "json number",
			value: json.Number("42"),
			want:  "42",
		},
		{
			name:  "empty object",
			value: map[string]any{},
			want:  "{}",
		},
		{
			name:  "empty array",
			value: []any{},
			want:  "[]",
		},
		{
			name: "complex nested structure",
			value: map[string]any{
				"z_key": []any{
					map[string]any{"b": json.Number("2"), "a": json.Number("1")},
					nil,
					true,
				},
				"a_key": "value",
			},
			want: `{"a_key":"value","z_key":[{"a":1,"b":2},null,true]}`,
		},
		{
			name:  "int64",
			value: int64(math.MaxInt64),
			want:  "9223372036854775807",
		},
		{
			name:  "uint64",
			value: uint64(math.MaxUint64),
			want:  "18446744073709551615",
		},
		{
			name:  "float64",
			value: float64(1.5),
			want:  "1.5",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, p := marshalCanonicalJSON(tt.value)
			if p != nil {
				t.Fatalf("unexpected problem: %v", p)
			}
			if string(got) != tt.want {
				t.Fatalf("got=%s want=%s", got, tt.want)
			}
		})
	}
}

func TestMarshalCanonicalJSON_ErrorPropagation(t *testing.T) {
	// NaN in a nested structure must propagate the error.
	nested := map[string]any{
		"value": math.NaN(),
	}
	_, p := marshalCanonicalJSON(nested)
	if p == nil {
		t.Fatal("expected problem for NaN nested in object")
	}
}

func TestMarshalCanonicalJSON_Idempotent(t *testing.T) {
	// Parse JSON input and canonicalize. Second pass must produce identical output.
	input := `{"c":3,"a":[{"z":true,"m":null}],"b":"text"}`
	val, p := parseJSONValue([]byte(input))
	if p != nil {
		t.Fatalf("parse: %v", p)
	}
	first, p := marshalCanonicalJSON(val)
	if p != nil {
		t.Fatalf("first marshal: %v", p)
	}

	val2, p := parseJSONValue(first)
	if p != nil {
		t.Fatalf("second parse: %v", p)
	}
	second, p := marshalCanonicalJSON(val2)
	if p != nil {
		t.Fatalf("second marshal: %v", p)
	}

	if !bytes.Equal(first, second) {
		t.Fatalf("not idempotent:\nfirst= %s\nsecond=%s", first, second)
	}
}

func TestMarshalCanonicalJSON_RawMessage(t *testing.T) {
	raw := json.RawMessage(`{"b":2,"a":1}`)
	got, p := marshalCanonicalJSON(raw)
	if p != nil {
		t.Fatalf("unexpected problem: %v", p)
	}
	want := `{"a":1,"b":2}`
	if string(got) != want {
		t.Fatalf("got=%s want=%s", got, want)
	}
}

// ---------------------------------------------------------------------------
// 9. NewReader edge cases
// ---------------------------------------------------------------------------

func TestNewReader_FileNotFound(t *testing.T) {
	_, p := NewReader("/nonexistent/path/to/fixture.jsonl")
	if p == nil {
		t.Fatal("expected problem for nonexistent file")
	}
	if p.Code != problem.Internal {
		t.Fatalf("code=%s want=%s", p.Code, problem.Internal)
	}
}

func TestNewReader_EmptyFilePath(t *testing.T) {
	_, p := NewReader("")
	if p == nil {
		t.Fatal("expected problem for empty file path")
	}
	if p.Code != problem.ValidationFailed {
		t.Fatalf("code=%s want=%s", p.Code, problem.ValidationFailed)
	}
}

func TestNewReader_WhitespaceFilePath(t *testing.T) {
	_, p := NewReader("   ")
	if p == nil {
		t.Fatal("expected problem for whitespace-only file path")
	}
	if p.Code != problem.ValidationFailed {
		t.Fatalf("code=%s want=%s", p.Code, problem.ValidationFailed)
	}
}

// ---------------------------------------------------------------------------
// 10. Reader.Next edge cases
// ---------------------------------------------------------------------------

func TestReaderNext_EmptyFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "empty.jsonl")
	if err := os.WriteFile(path, []byte{}, 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	r, p := NewReader(path)
	if p != nil {
		t.Fatalf("NewReader: %v", p)
	}
	defer func() { _ = r.Close() }()

	_, ok, p := r.Next()
	if p != nil {
		t.Fatalf("unexpected problem: %v", p)
	}
	if ok {
		t.Fatal("expected ok=false (EOF) for empty file")
	}
}

func TestReaderNext_MalformedJSONLine(t *testing.T) {
	path := filepath.Join(t.TempDir(), "malformed.jsonl")
	if err := os.WriteFile(path, []byte("{invalid json}\n"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	r, p := NewReader(path)
	if p != nil {
		t.Fatalf("NewReader: %v", p)
	}
	defer func() { _ = r.Close() }()

	_, _, p = r.Next()
	if p == nil {
		t.Fatal("expected problem for malformed JSON line")
	}
	if p.Code != problem.ValidationFailed {
		t.Fatalf("code=%s want=%s", p.Code, problem.ValidationFailed)
	}
}

func TestReaderNext_AfterClose(t *testing.T) {
	path := filepath.Join(t.TempDir(), "closed.jsonl")
	if err := os.WriteFile(path, []byte(`{"subject":"test"}`+"\n"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	r, p := NewReader(path)
	if p != nil {
		t.Fatalf("NewReader: %v", p)
	}
	if p := r.Close(); p != nil {
		t.Fatalf("Close: %v", p)
	}

	_, _, p = r.Next()
	if p == nil {
		t.Fatal("expected problem for reading after close")
	}
	if p.Code != problem.ValidationFailed {
		t.Fatalf("code=%s want=%s", p.Code, problem.ValidationFailed)
	}
}

func TestReaderClose_Idempotent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "idempotent.jsonl")
	if err := os.WriteFile(path, []byte{}, 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	r, p := NewReader(path)
	if p != nil {
		t.Fatalf("NewReader: %v", p)
	}

	// Close twice -- second call must not panic or return error.
	if p := r.Close(); p != nil {
		t.Fatalf("first Close: %v", p)
	}
	if p := r.Close(); p != nil {
		t.Fatalf("second Close: %v", p)
	}
}

func TestReaderClose_NilReceiver(t *testing.T) {
	var r *Reader
	if p := r.Close(); p != nil {
		t.Fatalf("Close on nil receiver should return nil, got: %v", p)
	}
}

func TestReaderNext_WhitespaceOnlyLines(t *testing.T) {
	// A file with only whitespace lines should produce EOF, not a JSON parse error,
	// because bufio.Scanner will return each line including empty ones.
	// However, whitespace-only lines ARE returned by Scanner and will fail JSON parse.
	path := filepath.Join(t.TempDir(), "whitespace.jsonl")
	if err := os.WriteFile(path, []byte("   \n\n"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	r, p := NewReader(path)
	if p != nil {
		t.Fatalf("NewReader: %v", p)
	}
	defer func() { _ = r.Close() }()

	// The scanner will return the whitespace line, which should fail JSON unmarshal.
	_, _, p = r.Next()
	if p == nil {
		t.Fatal("expected problem for whitespace-only line")
	}
	if p.Code != problem.ValidationFailed {
		t.Fatalf("code=%s want=%s", p.Code, problem.ValidationFailed)
	}
}

// ---------------------------------------------------------------------------
// writeCanonicalComposite: json.RawMessage pass-through
// ---------------------------------------------------------------------------

func TestWriteCanonicalComposite_RawMessage(t *testing.T) {
	raw := json.RawMessage(`{"z":1,"a":2}`)
	var b bytes.Buffer
	p := writeCanonicalComposite(&b, raw)
	if p != nil {
		t.Fatalf("unexpected problem: %v", p)
	}
	want := `{"a":2,"z":1}`
	if b.String() != want {
		t.Fatalf("got=%s want=%s", b.String(), want)
	}
}

func TestWriteCanonicalComposite_EmptyRawMessageFails(t *testing.T) {
	raw := json.RawMessage(``)
	var b bytes.Buffer
	p := writeCanonicalComposite(&b, raw)
	if p == nil {
		t.Fatal("expected problem for empty RawMessage")
	}
}

// ---------------------------------------------------------------------------
// writeCanonicalValue: dispatch smoke test
// ---------------------------------------------------------------------------

func TestWriteCanonicalValue_DispatchesCorrectly(t *testing.T) {
	tests := []struct {
		name  string
		value any
		want  string
	}{
		{name: "nil dispatches to scalar", value: nil, want: "null"},
		{name: "bool dispatches to scalar", value: true, want: "true"},
		{name: "string dispatches to scalar", value: "x", want: `"x"`},
		{name: "json.Number dispatches to scalar", value: json.Number("7"), want: "7"},
		{name: "int dispatches to int handler", value: int(5), want: "5"},
		{name: "int64 dispatches to int handler", value: int64(-1), want: "-1"},
		{name: "uint dispatches to uint handler", value: uint(10), want: "10"},
		{name: "uint64 dispatches to uint handler", value: uint64(100), want: "100"},
		{name: "float64 dispatches to float handler", value: float64(2.5), want: "2.5"},
		{name: "float32 dispatches to float handler", value: float32(1.0), want: "1"},
		{name: "slice dispatches to array", value: []any{json.Number("1")}, want: "[1]"},
		{name: "map dispatches to object", value: map[string]any{"k": json.Number("1")}, want: `{"k":1}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var b bytes.Buffer
			p := writeCanonicalValue(&b, tt.value)
			if p != nil {
				t.Fatalf("unexpected problem: %v", p)
			}
			if b.String() != tt.want {
				t.Fatalf("got=%q want=%q", b.String(), tt.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Integration: parse -> canonical -> parse consistency
// ---------------------------------------------------------------------------

func TestParseAndCanonicalizeRoundTrip(t *testing.T) {
	inputs := []string{
		`null`,
		`true`,
		`false`,
		`42`,
		`"hello"`,
		`[1,2,3]`,
		`{"a":1}`,
		`{"z":{"y":{"x":1}},"a":"b"}`,
		`[{"b":2,"a":1},null,true,"str",42,[]]`,
	}
	for _, input := range inputs {
		t.Run(input, func(t *testing.T) {
			val, p := parseJSONValue([]byte(input))
			if p != nil {
				t.Fatalf("parse: %v", p)
			}
			out, p := marshalCanonicalJSON(val)
			if p != nil {
				t.Fatalf("marshal: %v", p)
			}

			// Re-parse the canonical output.
			val2, p := parseJSONValue(out)
			if p != nil {
				t.Fatalf("re-parse: %v", p)
			}
			out2, p := marshalCanonicalJSON(val2)
			if p != nil {
				t.Fatalf("re-marshal: %v", p)
			}

			if !bytes.Equal(out, out2) {
				t.Fatalf("not stable:\nfirst= %s\nsecond=%s", out, out2)
			}
		})
	}
}
