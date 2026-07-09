package progress

import (
	"context"
	"testing"
)

func TestWithSinkRoundTrip(t *testing.T) {
	t.Parallel()

	var got []Event
	ctx := WithSink(context.Background(), func(e Event) { got = append(got, e) })
	sink := FromContext(ctx)
	if sink == nil {
		t.Fatal("FromContext returned nil after WithSink")
	}
	sink(Event{Repo: "acme/skills", Phase: PhaseFetching})
	if len(got) != 1 || got[0].Repo != "acme/skills" || got[0].Phase != PhaseFetching {
		t.Errorf("sink recorded %+v, want one fetching event for acme/skills", got)
	}
}

func TestFromContextBare(t *testing.T) {
	t.Parallel()

	if FromContext(context.Background()) != nil {
		t.Error("FromContext on a bare context must return nil")
	}
}

func TestEmitWithoutSinkIsNoop(t *testing.T) {
	t.Parallel()

	// Must not panic: emitters run unconditionally in library code.
	Emit(context.Background(), Event{Phase: PhaseDone})
}

func TestEmitDelivers(t *testing.T) {
	t.Parallel()

	var got []Event
	ctx := WithSink(context.Background(), func(e Event) { got = append(got, e) })
	Emit(ctx, Event{Phase: PhaseCached, Commit: "abc"})
	if len(got) != 1 || got[0].Phase != PhaseCached || got[0].Commit != "abc" {
		t.Errorf("Emit delivered %+v, want one cached event", got)
	}
}

// stampedSkill is the skill name the stamping tests decorate events with.
const stampedSkill = "alpha"

func TestStampDecorates(t *testing.T) {
	t.Parallel()

	var got []Event
	ctx := WithSink(context.Background(), func(e Event) { got = append(got, e) })
	ctx = Stamp(ctx, func(e *Event) { e.Skill, e.Index, e.Count = stampedSkill, 2, 3 })
	Emit(ctx, Event{Phase: PhaseReceiving, Percent: 62, Detail: "4.1 MiB | 2.3 MiB/s"})

	if len(got) != 1 {
		t.Fatalf("got %d events, want 1", len(got))
	}
	e := got[0]
	if e.Skill != stampedSkill || e.Index != 2 || e.Count != 3 {
		t.Errorf("stamp not applied: %+v", e)
	}
	if e.Phase != PhaseReceiving || e.Percent != 62 || e.Detail != "4.1 MiB | 2.3 MiB/s" {
		t.Errorf("stamp clobbered original fields: %+v", e)
	}
}

func TestStampWithoutSinkIsIdentity(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	if got := Stamp(ctx, func(e *Event) { e.Skill = "x" }); got != ctx {
		t.Error("Stamp on a sink-less context must return the context unchanged")
	}
}

func TestStampNests(t *testing.T) {
	t.Parallel()

	var got []Event
	ctx := WithSink(context.Background(), func(e Event) { got = append(got, e) })
	ctx = Stamp(ctx, func(e *Event) { e.Skill = stampedSkill })
	ctx = Stamp(ctx, func(e *Event) { e.Repo = "acme/skills" })
	Emit(ctx, Event{Phase: PhaseFetching})

	if len(got) != 1 || got[0].Skill != stampedSkill || got[0].Repo != "acme/skills" {
		t.Errorf("nested stamps not both applied: %+v", got)
	}
}
