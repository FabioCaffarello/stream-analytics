package replay

import (
	"encoding/json"
	"sort"

	"github.com/market-raccoon/internal/shared/envelope"
	"github.com/market-raccoon/internal/shared/problem"
)

// DriftResult captures structural differences between golden and actual output.
type DriftResult struct {
	EnvelopeIndex   int
	FieldMismatches []FieldMismatch
	NewFields       []string
	DroppedFields   []string
	Compatible      bool // true if only additive changes (new fields only)
}

// FieldMismatch records a value difference at a specific JSON path.
type FieldMismatch struct {
	Path     string
	Expected string
	Actual   string
}

// DetectDrift compares two envelope slices field-by-field and returns drift results.
// Returns one DriftResult per mismatched envelope. Empty slice means no drift.
func DetectDrift(actual, golden []envelope.Envelope) ([]DriftResult, *problem.Problem) {
	if len(actual) != len(golden) {
		return nil, problem.Newf(problem.ValidationFailed,
			"envelope count mismatch: actual=%d golden=%d", len(actual), len(golden))
	}

	var results []DriftResult
	for i := range actual {
		dr, p := compareEnvelopes(i, actual[i], golden[i])
		if p != nil {
			return nil, p
		}
		if dr != nil {
			results = append(results, *dr)
		}
	}
	return results, nil
}

// DetectPayloadDrift compares two JSON payload slices field-by-field.
func DetectPayloadDrift(actual, golden []json.RawMessage) ([]DriftResult, *problem.Problem) {
	if len(actual) != len(golden) {
		return nil, problem.Newf(problem.ValidationFailed,
			"payload count mismatch: actual=%d golden=%d", len(actual), len(golden))
	}

	var results []DriftResult
	for i := range actual {
		dr, p := comparePayloads(i, actual[i], golden[i])
		if p != nil {
			return nil, p
		}
		if dr != nil {
			results = append(results, *dr)
		}
	}
	return results, nil
}

func compareEnvelopes(idx int, actual, golden envelope.Envelope) (*DriftResult, *problem.Problem) {
	actualJSON, err := json.Marshal(envelopeToMap(actual))
	if err != nil {
		return nil, problem.Wrap(err, problem.Internal, "marshal actual envelope")
	}
	goldenJSON, err := json.Marshal(envelopeToMap(golden))
	if err != nil {
		return nil, problem.Wrap(err, problem.Internal, "marshal golden envelope")
	}
	return compareJSONBytes(idx, actualJSON, goldenJSON)
}

func comparePayloads(idx int, actual, golden json.RawMessage) (*DriftResult, *problem.Problem) {
	return compareJSONBytes(idx, actual, golden)
}

func compareJSONBytes(idx int, actualJSON, goldenJSON []byte) (*DriftResult, *problem.Problem) {
	var actualMap map[string]any
	var goldenMap map[string]any

	if err := json.Unmarshal(actualJSON, &actualMap); err != nil {
		return nil, problem.Wrap(err, problem.Internal, "unmarshal actual json")
	}
	if err := json.Unmarshal(goldenJSON, &goldenMap); err != nil {
		return nil, problem.Wrap(err, problem.Internal, "unmarshal golden json")
	}

	dr := DriftResult{EnvelopeIndex: idx, Compatible: true}
	compareMaps("", actualMap, goldenMap, &dr)

	if len(dr.FieldMismatches) == 0 && len(dr.NewFields) == 0 && len(dr.DroppedFields) == 0 {
		return nil, nil
	}
	return &dr, nil
}

func compareMaps(prefix string, actual, golden map[string]any, dr *DriftResult) {
	allKeys := mergeKeys(actual, golden)
	sort.Strings(allKeys)

	for _, key := range allKeys {
		path := joinPath(prefix, key)
		aVal, aOK := actual[key]
		gVal, gOK := golden[key]

		if aOK && !gOK {
			dr.NewFields = append(dr.NewFields, path)
			continue
		}
		if !aOK && gOK {
			dr.DroppedFields = append(dr.DroppedFields, path)
			dr.Compatible = false
			continue
		}

		compareValues(path, aVal, gVal, dr)
	}
}

func compareValues(path string, actual, golden any, dr *DriftResult) {
	// Both maps: recurse.
	aMap, aIsMap := actual.(map[string]any)
	gMap, gIsMap := golden.(map[string]any)
	if aIsMap && gIsMap {
		compareMaps(path, aMap, gMap, dr)
		return
	}

	// Both slices: compare element-wise.
	aSlice, aIsSlice := actual.([]any)
	gSlice, gIsSlice := golden.([]any)
	if aIsSlice && gIsSlice {
		compareSlices(path, aSlice, gSlice, dr)
		return
	}

	// Scalar comparison via JSON representation.
	aStr := jsonScalar(actual)
	gStr := jsonScalar(golden)
	if aStr != gStr {
		dr.FieldMismatches = append(dr.FieldMismatches, FieldMismatch{
			Path:     path,
			Expected: gStr,
			Actual:   aStr,
		})
		dr.Compatible = false
	}
}

func compareSlices(path string, actual, golden []any, dr *DriftResult) {
	minLen := len(actual)
	if len(golden) < minLen {
		minLen = len(golden)
	}
	for i := 0; i < minLen; i++ {
		elemPath := path + "[" + intToStr(i) + "]"
		compareValues(elemPath, actual[i], golden[i], dr)
	}
	if len(actual) > len(golden) {
		for i := len(golden); i < len(actual); i++ {
			dr.NewFields = append(dr.NewFields, path+"["+intToStr(i)+"]")
		}
	}
	if len(golden) > len(actual) {
		for i := len(actual); i < len(golden); i++ {
			dr.DroppedFields = append(dr.DroppedFields, path+"["+intToStr(i)+"]")
			dr.Compatible = false
		}
	}
}

func envelopeToMap(env envelope.Envelope) map[string]any {
	m := map[string]any{
		"type":            env.Type,
		"version":         env.Version,
		"venue":           env.Venue,
		"instrument":      env.Instrument,
		"ts_exchange":     env.TsExchange,
		"ts_ingest":       env.TsIngest,
		"seq":             env.Seq,
		"idempotency_key": env.IdempotencyKey,
		"content_type":    env.ContentType,
	}
	if len(env.Meta) > 0 {
		meta := make(map[string]any, len(env.Meta))
		for k, v := range env.Meta {
			meta[k] = v
		}
		m["meta"] = meta
	}
	if len(env.Payload) > 0 {
		var payloadMap any
		if err := json.Unmarshal(env.Payload, &payloadMap); err == nil {
			m["payload"] = payloadMap
		}
	}
	return m
}

func mergeKeys(a, b map[string]any) []string {
	seen := make(map[string]struct{}, len(a)+len(b))
	for k := range a {
		seen[k] = struct{}{}
	}
	for k := range b {
		seen[k] = struct{}{}
	}
	out := make([]string, 0, len(seen))
	for k := range seen {
		out = append(out, k)
	}
	return out
}

func joinPath(prefix, key string) string {
	if prefix == "" {
		return key
	}
	return prefix + "." + key
}

func jsonScalar(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		return "<marshal-error>"
	}
	return string(b)
}

func intToStr(i int) string {
	if i == 0 {
		return "0"
	}
	buf := make([]byte, 0, 8)
	v := i
	for v > 0 {
		buf = append(buf, byte(v%10)+'0')
		v /= 10
	}
	for l, r := 0, len(buf)-1; l < r; l, r = l+1, r-1 {
		buf[l], buf[r] = buf[r], buf[l]
	}
	return string(buf)
}
