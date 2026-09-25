package provider_test

import (
	"testing"

	"github.com/DhanushRamesh/personal-assistant/internal/provider"
)

func TestKindPredicates(t *testing.T) {
	for _, k := range []provider.Kind{provider.KindUpdate, provider.KindFinal, provider.KindError} {
		if !k.Valid() {
			t.Errorf("%s.Valid() = false, want true", k)
		}
	}
	if provider.Kind("chatter").Valid() {
		t.Error(`Kind("chatter").Valid() = true, want false`)
	}

	if provider.KindUpdate.Terminal() {
		t.Error("an update must not end the stream")
	}
	for _, k := range []provider.Kind{provider.KindFinal, provider.KindError} {
		if !k.Terminal() {
			t.Errorf("%s.Terminal() = false, want true", k)
		}
	}
}

func TestMessageConstructors(t *testing.T) {
	cases := []struct {
		msg      provider.Message
		wantKind provider.Kind
	}{
		{provider.Update("working"), provider.KindUpdate},
		{provider.Final("done"), provider.KindFinal},
		{provider.Failure("GitLab did not respond in time.", "timeout", ""), provider.KindError},
	}

	for _, tc := range cases {
		if tc.msg.Kind != tc.wantKind {
			t.Errorf("Kind = %q, want %q", tc.msg.Kind, tc.wantKind)
		}
		if tc.msg.Text == "" {
			t.Errorf("%s message has no text", tc.msg.Kind)
		}
		if tc.msg.At.IsZero() {
			t.Errorf("%s message has no timestamp", tc.msg.Kind)
		}
		if tc.msg.At.Location().String() != "UTC" {
			t.Errorf("%s timestamp location = %v, want UTC", tc.msg.Kind, tc.msg.At.Location())
		}
	}
}
