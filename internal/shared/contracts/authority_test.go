package contracts

import (
	"reflect"
	"slices"
	"testing"

	marketdomain "github.com/market-raccoon/internal/core/marketdata/domain"
	"google.golang.org/protobuf/reflect/protoreflect"
)

func TestContractAuthority_ProtoFieldsAreFullyMapped(t *testing.T) {
	t.Parallel()

	requiredMessages := map[string]struct{}{
		"marketdata.v1.TradeTickV1":       {},
		"marketdata.v1.BookDeltaV1":       {},
		"marketdata.v1.MarkPriceTickV1":   {},
		"marketdata.v1.LiquidationTickV1": {},
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

	mappedDomainFields := map[string]struct{}{}
	for i := 0; i < fields.Len(); i++ {
		field := fields.Get(i)
		protoField := string(field.Name())
		domainField, ok := binding.ProtoToDomainFieldMap[protoField]
		if !ok {
			t.Fatalf("%s: missing mapping for proto field %q", binding.MessageName, protoField)
		}
		if _, ok := binding.DomainType.FieldByName(domainField); !ok {
			t.Fatalf("%s: mapping %q -> %q references missing domain field", binding.MessageName, protoField, domainField)
		}
		mappedDomainFields[domainField] = struct{}{}
	}

	for protoField, domainField := range binding.ProtoToDomainFieldMap {
		if fields.ByName(protoreflect.Name(protoField)) == nil {
			t.Fatalf("%s: mapping references unknown proto field %q", binding.MessageName, protoField)
		}
		if _, ok := binding.DomainType.FieldByName(domainField); !ok {
			t.Fatalf("%s: mapping references unknown domain field %q", binding.MessageName, domainField)
		}
	}

	for i := 0; i < binding.DomainType.NumField(); i++ {
		field := binding.DomainType.Field(i)
		if field.PkgPath != "" {
			continue
		}
		if _, ok := mappedDomainFields[field.Name]; !ok {
			t.Fatalf("%s: exported domain field %q has no proto mapping", binding.MessageName, field.Name)
		}
	}
}

func TestContractAuthority_DomainPayloadsHaveNoJSONTags(t *testing.T) {
	t.Parallel()

	domainPayloadTypes := []reflect.Type{
		reflect.TypeOf(marketdomain.TradeTickV1{}),
		reflect.TypeOf(marketdomain.BookDeltaV1{}),
		reflect.TypeOf(marketdomain.PriceLevel{}),
		reflect.TypeOf(marketdomain.MarkPriceTickV1{}),
		reflect.TypeOf(marketdomain.LiquidationTickV1{}),
	}

	for _, payloadType := range domainPayloadTypes {
		for i := 0; i < payloadType.NumField(); i++ {
			field := payloadType.Field(i)
			if tag := field.Tag.Get("json"); tag != "" {
				t.Fatalf("%s.%s must not define json tag, got %q", payloadType.Name(), field.Name, tag)
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
