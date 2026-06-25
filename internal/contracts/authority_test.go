package contracts

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"testing"

	marketdomain "github.com/market-raccoon/internal/core/marketdata/domain"
	"github.com/market-raccoon/internal/shared/codec"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/descriptorpb"
)

func TestContractAuthority_ProtoFieldsAreFullyMapped(t *testing.T) {
	t.Parallel()

	requiredMessages := map[string]struct{}{
		"marketdata.v1.TradeTickV1":        {},
		"marketdata.v1.BookDeltaV1":        {},
		"marketdata.v1.MarkPriceTickV1":    {},
		"marketdata.v1.LiquidationTickV1":  {},
		"marketdata.v1.OpenInterestTickV1": {},
	}
	seenMessages := make(map[string]struct{}, len(marketDataAuthorityBindings))

	for _, binding := range marketDataAuthorityBindings {
		msgName, fields := validateAuthorityBinding(t, binding, seenMessages)
		assertProtoFieldMappings(t, binding, fields)
		assertConverterContracts(t, binding)
		seenMessages[msgName] = struct{}{}
	}

	assertExpectedAuthorityMessages(t, requiredMessages, seenMessages)
}

func validateAuthorityBinding(
	t *testing.T,
	binding authorityBinding,
	seenMessages map[string]struct{},
) (string, protoreflect.FieldDescriptors) {
	t.Helper()

	if binding.EventType == "" {
		t.Fatalf("%s: EventType must be set", binding.MessageName)
	}
	if binding.Version < 1 {
		t.Fatalf("%s: Version must be >= 1, got %d", binding.MessageName, binding.Version)
	}
	if binding.RegistryProto == "" {
		t.Fatalf("%s: RegistryProto must be set", binding.MessageName)
	}
	if binding.NewProto == nil {
		t.Fatalf("%s: NewProto converter must be set", binding.MessageName)
	}
	if binding.ProtoToDomain == nil {
		t.Fatalf("%s: ProtoToDomain converter must be set", binding.MessageName)
	}
	if binding.DomainToProto == nil {
		t.Fatalf("%s: DomainToProto converter must be set", binding.MessageName)
	}

	msg := binding.NewProto()
	if msg == nil {
		t.Fatalf("%s: NewProto returned nil", binding.MessageName)
	}

	msgName := string(msg.ProtoReflect().Descriptor().FullName())
	if binding.MessageName != msgName {
		t.Fatalf("manifest message_name=%q does not match descriptor=%q", binding.MessageName, msgName)
	}
	if _, dup := seenMessages[msgName]; dup {
		t.Fatalf("duplicate authority binding for %s", msgName)
	}

	return msgName, msg.ProtoReflect().Descriptor().Fields()
}

func assertConverterContracts(t *testing.T, binding authorityBinding) {
	t.Helper()

	msg := binding.NewProto()
	domainValue := binding.ProtoToDomain(msg)
	if got := reflect.TypeOf(domainValue); got != binding.DomainType {
		t.Fatalf("%s: ProtoToDomain type=%v, want=%v", binding.MessageName, got, binding.DomainType)
	}
	zeroDomain := reflect.New(binding.DomainType).Elem().Interface()
	protoValue := binding.DomainToProto(zeroDomain)
	if protoValue == nil {
		t.Fatalf("%s: DomainToProto returned nil", binding.MessageName)
	}
	if got := string(protoValue.ProtoReflect().Descriptor().FullName()); got != binding.MessageName {
		t.Fatalf("%s: DomainToProto returned message=%s", binding.MessageName, got)
	}
}

func assertExpectedAuthorityMessages(
	t *testing.T,
	requiredMessages map[string]struct{},
	seenMessages map[string]struct{},
) {
	t.Helper()

	for messageName := range requiredMessages {
		if _, ok := seenMessages[messageName]; !ok {
			t.Fatalf("missing authority binding for %s", messageName)
		}
	}
	if len(seenMessages) == len(requiredMessages) {
		return
	}

	var extras []string
	for messageName := range seenMessages {
		if _, ok := requiredMessages[messageName]; !ok {
			extras = append(extras, messageName)
		}
	}
	slices.Sort(extras)
	t.Fatalf("unexpected authority binding messages: %v", extras)
}

func assertProtoFieldMappings(t *testing.T, binding authorityBinding, fields protoreflect.FieldDescriptors) {
	t.Helper()

	ignoredDomainFields := collectIgnoredDomainFields(t, binding)
	ignoredProtoFields := collectIgnoredProtoFields(t, binding, fields)
	mappedDomainFields := mapProtoFieldsToDomainFields(t, binding, fields, ignoredDomainFields, ignoredProtoFields)
	assertMappingReferences(t, binding, fields, ignoredProtoFields)
	assertDomainFieldCoverage(t, binding, mappedDomainFields, ignoredDomainFields)
	assertIgnoredFieldsUnmapped(t, binding, mappedDomainFields, ignoredDomainFields, ignoredProtoFields)
}

func collectIgnoredDomainFields(t *testing.T, binding authorityBinding) map[string]struct{} {
	t.Helper()

	ignoredDomainFields := make(map[string]struct{}, len(binding.IgnoredDomainFields))
	for _, domainField := range binding.IgnoredDomainFields {
		if _, ok := binding.DomainType.FieldByName(domainField); !ok {
			t.Fatalf("%s: ignored domain field %q does not exist", binding.MessageName, domainField)
		}
		ignoredDomainFields[domainField] = struct{}{}
	}
	return ignoredDomainFields
}

func collectIgnoredProtoFields(
	t *testing.T,
	binding authorityBinding,
	fields protoreflect.FieldDescriptors,
) map[string]struct{} {
	t.Helper()

	ignoredProtoFields := make(map[string]struct{}, len(binding.IgnoredProtoFields))
	for _, protoField := range binding.IgnoredProtoFields {
		field := fields.ByName(protoreflect.Name(protoField))
		if field == nil {
			t.Fatalf("%s: ignored proto field %q does not exist", binding.MessageName, protoField)
		}
		opts, ok := field.Options().(*descriptorpb.FieldOptions)
		if !ok || !opts.GetDeprecated() {
			t.Fatalf("%s: ignored proto field %q must be explicitly deprecated", binding.MessageName, protoField)
		}
		ignoredProtoFields[protoField] = struct{}{}
	}
	return ignoredProtoFields
}

func mapProtoFieldsToDomainFields(
	t *testing.T,
	binding authorityBinding,
	fields protoreflect.FieldDescriptors,
	ignoredDomainFields map[string]struct{},
	ignoredProtoFields map[string]struct{},
) map[string]struct{} {
	t.Helper()

	mappedDomainFields := map[string]struct{}{}
	for i := 0; i < fields.Len(); i++ {
		field := fields.Get(i)
		protoField := string(field.Name())
		domainField, ok := binding.ProtoToDomainFieldMap[protoField]
		if !ok {
			if _, ignored := ignoredProtoFields[protoField]; ignored {
				continue
			}
			t.Fatalf("%s: missing mapping for proto field %q", binding.MessageName, protoField)
		}
		if _, ok := binding.DomainType.FieldByName(domainField); !ok {
			t.Fatalf("%s: mapping %q -> %q references missing domain field", binding.MessageName, protoField, domainField)
		}
		if _, ignored := ignoredDomainFields[domainField]; ignored {
			t.Fatalf("%s: mapping %q -> %q conflicts with ignored domain field list", binding.MessageName, protoField, domainField)
		}
		mappedDomainFields[domainField] = struct{}{}
	}
	return mappedDomainFields
}

func assertMappingReferences(
	t *testing.T,
	binding authorityBinding,
	fields protoreflect.FieldDescriptors,
	ignoredProtoFields map[string]struct{},
) {
	t.Helper()

	for protoField, domainField := range binding.ProtoToDomainFieldMap {
		if fields.ByName(protoreflect.Name(protoField)) == nil {
			t.Fatalf("%s: mapping references unknown proto field %q", binding.MessageName, protoField)
		}
		if _, ignored := ignoredProtoFields[protoField]; ignored {
			t.Fatalf("%s: proto field %q cannot be both mapped and ignored", binding.MessageName, protoField)
		}
		if _, ok := binding.DomainType.FieldByName(domainField); !ok {
			t.Fatalf("%s: mapping references unknown domain field %q", binding.MessageName, domainField)
		}
	}
}

func assertDomainFieldCoverage(
	t *testing.T,
	binding authorityBinding,
	mappedDomainFields map[string]struct{},
	ignoredDomainFields map[string]struct{},
) {
	t.Helper()

	for i := 0; i < binding.DomainType.NumField(); i++ {
		field := binding.DomainType.Field(i)
		if field.PkgPath != "" {
			continue
		}
		if _, ok := mappedDomainFields[field.Name]; ok {
			continue
		}
		if _, ignored := ignoredDomainFields[field.Name]; ignored {
			continue
		}
		t.Fatalf("%s: exported domain field %q has no proto mapping", binding.MessageName, field.Name)
	}
}

func assertIgnoredFieldsUnmapped(
	t *testing.T,
	binding authorityBinding,
	mappedDomainFields map[string]struct{},
	ignoredDomainFields map[string]struct{},
	ignoredProtoFields map[string]struct{},
) {
	t.Helper()

	for ignoredDomainField := range ignoredDomainFields {
		if _, mapped := mappedDomainFields[ignoredDomainField]; mapped {
			t.Fatalf("%s: ignored domain field %q cannot be mapped", binding.MessageName, ignoredDomainField)
		}
	}

	for ignoredProtoField := range ignoredProtoFields {
		if _, mapped := binding.ProtoToDomainFieldMap[ignoredProtoField]; mapped {
			t.Fatalf("%s: ignored proto field %q cannot be mapped", binding.MessageName, ignoredProtoField)
		}
	}
}

func TestContractAuthority_SchemaIdentityMatchesRegistry(t *testing.T) {
	t.Parallel()

	registrySchemas := readSchemaRegistry(t)
	registeredPayloadCodecs := registerPayloadCodecs(t)
	seenSchemaKeys := map[string]struct{}{}

	for _, binding := range marketDataAuthorityBindings {
		schemaKey := fmt.Sprintf("%s:%d", binding.EventType, binding.Version)
		if _, dup := seenSchemaKeys[schemaKey]; dup {
			t.Fatalf("duplicate schema identity in authority bindings: %s", schemaKey)
		}
		seenSchemaKeys[schemaKey] = struct{}{}

		registrySchema, ok := registrySchemas[schemaKey]
		if !ok {
			t.Fatalf("registry.json is missing authority schema %s", schemaKey)
		}
		if registrySchema.Message != binding.MessageName {
			t.Fatalf("%s: registry message=%q, authority message=%q", schemaKey, registrySchema.Message, binding.MessageName)
		}
		if registrySchema.ProtoFile != binding.RegistryProto {
			t.Fatalf("%s: registry proto_file=%q, authority proto=%q", schemaKey, registrySchema.ProtoFile, binding.RegistryProto)
		}
		msg := binding.NewProto()
		fullName := string(msg.ProtoReflect().Descriptor().FullName())
		if registrySchema.Message != fullName {
			t.Fatalf("%s: registry message=%q does not match generated proto message=%q", schemaKey, registrySchema.Message, fullName)
		}

		jsonKey := codec.SchemaKey{Type: binding.EventType, Version: binding.Version, Format: codec.FormatJSON}
		if _, ok := registeredPayloadCodecs.Encoder(jsonKey); !ok {
			t.Fatalf("%s: JSON payload codec is not registered", schemaKey)
		}
		protoKey := codec.SchemaKey{Type: binding.EventType, Version: binding.Version, Format: codec.FormatProto}
		if _, ok := registeredPayloadCodecs.Encoder(protoKey); !ok {
			t.Fatalf("%s: protobuf payload codec is not registered", schemaKey)
		}
	}
}

func registerPayloadCodecs(t *testing.T) *codec.Registry {
	t.Helper()

	reg := codec.NewRegistry()
	if p := RegisterMarketDataPayloadV1(reg); p != nil {
		t.Fatalf("RegisterMarketDataPayloadV1: %v", p)
	}
	return reg
}

type schemaRegistryManifest struct {
	Schemas []schemaRegistryEntry `json:"schemas"`
}

type schemaRegistryEntry struct {
	Type      string `json:"type"`
	Version   int32  `json:"version"`
	ProtoFile string `json:"proto_file"`
	Message   string `json:"message"`
}

func readSchemaRegistry(t *testing.T) map[string]schemaRegistryEntry {
	t.Helper()

	registryPath := filepath.Join(findRepoRoot(t), "proto", "registry.json")
	// #nosec G304 -- registryPath is a fixed repository-internal location.
	raw, err := os.ReadFile(registryPath)
	if err != nil {
		t.Fatalf("read schema registry %s: %v", registryPath, err)
	}
	var manifest schemaRegistryManifest
	if err := json.Unmarshal(raw, &manifest); err != nil {
		t.Fatalf("decode schema registry %s: %v", registryPath, err)
	}
	if len(manifest.Schemas) == 0 {
		t.Fatalf("schema registry %s has no schemas", registryPath)
	}

	out := make(map[string]schemaRegistryEntry, len(manifest.Schemas))
	for _, schema := range manifest.Schemas {
		key := fmt.Sprintf("%s:%d", schema.Type, schema.Version)
		if _, dup := out[key]; dup {
			t.Fatalf("schema registry duplicate entry for %s", key)
		}
		out[key] = schema
	}
	return out
}

func TestContractAuthority_DomainPayloadsUseCanonicalJSONTags(t *testing.T) {
	t.Parallel()

	domainPayloadTypes := []reflect.Type{
		reflect.TypeOf(marketdomain.TradeTickV1{}),
		reflect.TypeOf(marketdomain.BookDeltaV1{}),
		reflect.TypeOf(marketdomain.PriceLevel{}),
		reflect.TypeOf(marketdomain.MarkPriceTickV1{}),
		reflect.TypeOf(marketdomain.LiquidationTickV1{}),
		reflect.TypeOf(marketdomain.OpenInterestTickV1{}),
	}

	for _, payloadType := range domainPayloadTypes {
		for i := 0; i < payloadType.NumField(); i++ {
			field := payloadType.Field(i)
			jsonTag := field.Tag.Get("json")
			if jsonTag == "" || jsonTag == "-" {
				t.Fatalf("%s.%s must define canonical json tag, got %q", payloadType.Name(), field.Name, jsonTag)
			}
			for _, ch := range jsonTag {
				if ch >= 'A' && ch <= 'Z' {
					t.Fatalf("%s.%s json tag must be lowercase/snake_case, got %q", payloadType.Name(), field.Name, jsonTag)
				}
			}
			if tag := field.Tag.Get("protobuf"); tag != "" {
				t.Fatalf("%s.%s must not define protobuf tag, got %q", payloadType.Name(), field.Name, tag)
			}
			if tag := field.Tag.Get("cbor"); tag != "" {
				t.Fatalf("%s.%s must not define cbor tag, got %q", payloadType.Name(), field.Name, tag)
			}
		}
	}
}
