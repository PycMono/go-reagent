package conversation

import (
	"database/sql/driver"
	"reflect"
	"testing"
)

func TestConversationTableEntitiesMatchPersistenceSchema(t *testing.T) {
	tests := []struct {
		name      string
		entity    any
		tableName string
		columns   map[string]string
	}{
		{
			name: "conversation", entity: Conversation{}, tableName: "agent_conversations",
			columns: map[string]string{"ID": "column:id", "UserID": "column:user_id", "ConversationID": "column:conversation_id", "Name": "column:name", "Version": "column:version"},
		},
		{
			name: "message", entity: Message{}, tableName: "agent_messages",
			columns: map[string]string{"ID": "column:id", "ConversationID": "column:conversation_id", "TurnVersion": "column:turn_version", "Ordinal": "column:ordinal", "RunID": "column:run_id", "Role": "column:role", "Payload": "column:payload"},
		},
		{
			name: "model invocation", entity: ModelInvocation{}, tableName: "agent_model_invocations",
			columns: map[string]string{"ID": "column:id", "ConversationID": "column:conversation_id", "TurnVersion": "column:turn_version", "RunID": "column:run_id", "Sequence": "column:sequence", "Phase": "column:phase", "PlatformID": "column:platform_id", "Model": "column:model"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			table, ok := tt.entity.(interface{ TableName() string })
			if !ok || table.TableName() != tt.tableName {
				t.Fatalf("TableName() = %q, want %q", table.TableName(), tt.tableName)
			}
			typeOf := reflect.TypeOf(tt.entity)
			for fieldName, wantTag := range tt.columns {
				field, ok := typeOf.FieldByName(fieldName)
				if !ok {
					t.Fatalf("missing field %s", fieldName)
				}
				if got := field.Tag.Get("gorm"); !containsTag(got, wantTag) {
					t.Fatalf("%s gorm tag = %q, want containing %q", fieldName, got, wantTag)
				}
			}
		})
	}
}

func TestConversationNameMatchesWebChatSchema(t *testing.T) {
	field, ok := reflect.TypeOf(Conversation{}).FieldByName("Name")
	if !ok {
		t.Fatal("Conversation.Name is missing")
	}
	tag := field.Tag.Get("gorm")
	for _, want := range []string{"column:name", "size:255", "not null"} {
		if !containsTag(tag, want) {
			t.Fatalf("Name gorm tag = %q, want containing %q", tag, want)
		}
	}
}

func TestMessagePayloadImplementsSQLJSONContract(t *testing.T) {
	var _ driver.Valuer = MessagePayload{}
	var _ interface{ Scan(any) error } = (*MessagePayload)(nil)

	want := MessagePayload{Content: []ContentBlock{{Type: ContentTypeText, Text: "hello"}}}
	value, err := want.Value()
	if err != nil {
		t.Fatal(err)
	}
	var got MessagePayload
	if err := got.Scan(value); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("payload round trip = %#v, want %#v", got, want)
	}
}

func TestAllPersistedEntityIDsAreStrings(t *testing.T) {
	for _, entity := range []any{Conversation{}, Message{}, ModelInvocation{}} {
		field, ok := reflect.TypeOf(entity).FieldByName("ID")
		if !ok || field.Type.Kind() != reflect.String {
			t.Fatalf("%T ID type = %v, want string", entity, field.Type)
		}
	}
	for _, entity := range []any{Message{}, ModelInvocation{}} {
		field, ok := reflect.TypeOf(entity).FieldByName("ConversationID")
		if !ok || field.Type.Kind() != reflect.String {
			t.Fatalf("%T ConversationID type = %v, want string", entity, field.Type)
		}
	}
}

func containsTag(tag string, part string) bool {
	for start := 0; start+len(part) <= len(tag); start++ {
		if tag[start:start+len(part)] == part {
			return true
		}
	}
	return false
}
