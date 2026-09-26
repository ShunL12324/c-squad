package squad

import "time"

// Only ledger-backed routine notices may share a native queued turn. Arbitrary
// text, failures and decisions retain their own delivery and ordering boundary.
func routineNotice(m *Message) bool {
	if m.Report == nil || m.Report.Question != "" || m.Report.Milestone != "" {
		return false
	}
	switch m.Report.Kind {
	case "delivery", "ready", "available", "assigned", "cc":
		return true
	}
	return false
}

const routineBatchCount = 8
const routineBatchBytes = 24 * 1024

// claimRoutineBatch runs under the recipient lock already held by deliver.
// It never waits to fill a batch or takes a record claimed by another sender.
// Individual IDs, senders and transport outcomes remain in the ledger.
func (st *Store) claimRoutineBatch(first *Message, attempt string, generation int, thread string) ([]*Message, error) {
	var batch []*Message
	err := st.update(func(s *State) error {
		s.expireReports()
		member := s.Members[first.To]
		if !s.Active || member == nil || member.Generation != generation || member.EngineID != thread {
			return nil
		}
		found, size := false, 0
		for _, item := range s.Messages {
			if item.ID == first.ID {
				if item.State != DeliveryStateSending || item.Attempt != attempt || !s.reportCurrent(item) {
					return nil
				}
				if deliveryPaused(member.State) || member.State == MemberStateRemoved || deferBusyCodexNotice(member, item) {
					item.State, item.Attempt = DeliveryStatePending, ""
					item.Attempts--
					item.Error = "routine notice waiting for recipient availability"
					return nil
				}
				found = true
			} else {
				if !found || item.To != first.To || item.State == DeliveryStateSent || item.State == DeliveryStateAcknowledged || item.State == DeliveryStateSuperseded {
					continue
				}
				if len(batch) >= routineBatchCount || item.State != DeliveryStatePending || !routineNotice(item) || item.BootstrapGeneration != 0 || legacyBrief(item) {
					break
				}
				at, _ := time.Parse(time.RFC3339Nano, item.Attempt)
				if item.Attempt != "" && time.Since(at) < time.Duration(1<<min(item.Attempts, 5))*time.Second {
					break
				}
			}
			copy := *item
			copy.Text = s.reportText(item)
			bytes := len(messageBody(&copy, generation, true)) + 2
			if len(batch) > 0 && size+bytes > routineBatchBytes {
				break
			}
			if item.ID != first.ID {
				item.State, item.Attempt = DeliveryStateSending, attempt
				item.Attempts++
			}
			size += bytes
			batch = append(batch, &copy)
		}
		return nil
	})
	return batch, err
}
