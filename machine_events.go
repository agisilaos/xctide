package main

type machineEventSequencer struct {
	nextSeq int
}

func newMachineEventSequencer() *machineEventSequencer {
	return &machineEventSequencer{nextSeq: 1}
}

func (s *machineEventSequencer) annotate(event buildEvent) buildEvent {
	copyEvent := event
	copyEvent.Schema = machineSchemaVersion
	copyEvent.Seq = s.nextSeq
	s.nextSeq++
	return copyEvent
}

func annotateMachineEvents(events []buildEvent) []buildEvent {
	sequencer := newMachineEventSequencer()
	out := make([]buildEvent, 0, len(events))
	for _, event := range events {
		out = append(out, sequencer.annotate(event))
	}
	return out
}
