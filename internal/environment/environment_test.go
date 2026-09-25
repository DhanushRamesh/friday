package environment_test

import (
	"testing"

	"github.com/DhanushRamesh/personal-assistant/internal/environment"
)

func TestKindPredicates(t *testing.T) {
	for _, k := range []environment.Kind{environment.KindUpdate, environment.KindFinal, environment.KindError} {
		if !k.Valid() {
			t.Errorf("%s.Valid() = false, want true", k)
		}
	}
	if environment.Kind("chatter").Valid() {
		t.Error(`Kind("chatter").Valid() = true, want false`)
	}

	if environment.KindUpdate.Terminal() {
		t.Error("an update must not end the stream")
	}
	for _, k := range []environment.Kind{environment.KindFinal, environment.KindError} {
		if !k.Terminal() {
			t.Errorf("%s.Terminal() = false, want true", k)
		}
	}
}

func TestMessageConstructors(t *testing.T) {
	cases := []struct {
		msg      environment.Message
		wantKind environment.Kind
	}{
		{environment.Update("working"), environment.KindUpdate},
		{environment.Final("done"), environment.KindFinal},
		{environment.Failure("GitLab did not respond in time.", "timeout", ""), environment.KindError},
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
