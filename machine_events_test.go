package main

import "testing"

func TestMachineEventSequencerAnnotate(t *testing.T) {
	sequencer := newMachineEventSequencer()
	first := sequencer.annotate(buildEvent{Type: eventRunStarted})
	second := sequencer.annotate(buildEvent{Type: eventRunFinished})
	if first.Seq != 1 || second.Seq != 2 {
		t.Fatalf("unexpected sequence values: first=%d second=%d", first.Seq, second.Seq)
	}
	if first.Schema != machineSchemaVersion || second.Schema != machineSchemaVersion {
		t.Fatalf("unexpected schema annotation: first=%q second=%q", first.Schema, second.Schema)
	}
}

func TestAnnotateMachineEvents(t *testing.T) {
	in := []buildEvent{
		{Type: eventRunStarted},
		{Type: eventStepStarted, StepName: "Prepare"},
		{Type: eventRunFinished},
	}
	out := annotateMachineEvents(in)
	if len(out) != len(in) {
		t.Fatalf("len(out) = %d, want %d", len(out), len(in))
	}
	for i := range out {
		if out[i].Seq != i+1 {
			t.Fatalf("event %d seq = %d, want %d", i, out[i].Seq, i+1)
		}
		if out[i].Schema != machineSchemaVersion {
			t.Fatalf("event %d schema = %q, want %q", i, out[i].Schema, machineSchemaVersion)
		}
	}
}
