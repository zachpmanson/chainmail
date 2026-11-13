package hassles

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/emersion/go-webdav/caldav"
)

func groupPending(t *testing.T, s *groupMemoryStore) *groupOperation {
	t.Helper()
	state, err := groupDecodeState(s.raw)
	if err != nil {
		t.Fatal(err)
	}
	if state.Pending == nil {
		t.Fatal("pending intent disappeared")
	}
	return state.Pending
}

func TestGroupFailureBoundariesRetainIntentAndNeverResumeWrites(t *testing.T) {
	for _, action := range []string{"create", "delete"} {
		for _, position := range []int{1, 12, 24} {
			for _, boundary := range []string{"entered-before", "entered-after", "http-before", "http-lost-ack", "ack-before", "ack-after", "read-after-write", "receipt-before", "next-snapshot", "identity-before-write"} {
				t.Run(fmt.Sprintf("%s/%d/%s", action, position, boundary), func(t *testing.T) {
					s := groupTestStore(t)
					schedule := dailyV2Schedule("2027-02-12T09:17", 0.5, false, "")
					r := groupTestRequest(t, s, schedule)
					b := groupTestBackend(nil)
					if action == "delete" {
						originals := dailyGroupObjects(t, dailyV2Compile(t, schedule))
						r = groupDeleteRequest(t, s, originals)
						b = groupTestBackend(originals)
					}
					failed := false
					writeInvocations := 0
					isSave := func(raw []byte, phase string) bool {
						var state groupState
						if err := json.Unmarshal(raw, &state); err != nil {
							t.Fatal(err)
						}
						return state.Pending != nil && len(state.Pending.Targets) >= position && state.Pending.Targets[position-1].Phase == phase
					}
					s.before = func(_ int, raw []byte) error {
						if !failed && ((boundary == "entered-before" && isSave(raw, "entered")) || (boundary == "ack-before" && isSave(raw, "ack")) || (boundary == "receipt-before" && isSave(raw, "verified"))) {
							failed = true
							return groupTestError
						}
						return nil
					}
					s.after = func(_ int, raw []byte) error {
						if !failed && ((boundary == "entered-after" && isSave(raw, "entered")) || (boundary == "ack-after" && isSave(raw, "ack"))) {
							failed = true
							return groupTestError
						}
						return nil
					}
					b.before = func(method string, _ int) error {
						if method == "Write" {
							writeInvocations++
							if boundary == "http-before" && writeInvocations == position {
								failed = true
								return groupTestError
							}
						}
						if !failed && len(b.writes) == position && ((boundary == "read-after-write" && method == "Probe") || (boundary == "next-snapshot" && method == "Snapshot")) {
							failed = true
							return groupTestError
						}
						return nil
					}
					b.afterWrite = func(n int) error {
						if boundary == "http-lost-ack" && n == position {
							failed = true
							return groupTestError
						}
						return nil
					}
					checks := 0
					check := func(ctx context.Context, binding GroupBinding) error {
						checks++
						// Two admission checks, then two checks per target; fail the one after
						// entered has been durably saved, immediately before the conditional call.
						if boundary == "identity-before-write" && checks == 2+2*position {
							failed = true
							return groupTestError
						}
						return groupTestCheck(ctx, binding)
					}
					out, err := ExecuteGroup(context.Background(), b, s, r, groupTestBinding, check, now)
					if err == nil || !failed || !out.Pending || out.Calls > 512 {
						t.Fatalf("failure not contained %+v %v failed=%v", out, err, failed)
					}
					groupPending(t, s)
					if len(b.writes) > position {
						t.Fatal("continued writing beyond failure")
					}
					// The same original request only reads. A different request, operation ID,
					// actor, location or fence cannot bypass the single global pending intent.
					count := len(b.writes)
					b.before = nil
					b.afterWrite = nil
					s.before = nil
					s.after = nil
					b.deadline = time.Time{}
					_, _ = ReconcileGroup(context.Background(), groupOnlyReader{b}, s, r, groupTestBinding, groupTestCheck)
					if len(b.writes) != count {
						t.Fatal("read-only replay wrote")
					}
					// Full desired state at the last failure may settle; partial never clears.
					_, pending, decodeErr := GroupStateFence(s.raw)
					if decodeErr != nil {
						t.Fatal(decodeErr)
					}
					if count < 24 && !pending {
						t.Fatal("partial state cleared")
					}
					if pending {
						for _, mutate := range []func(*GroupRequest, *GroupBinding){
							func(r *GroupRequest, _ *GroupBinding) { r.OperationID = "bbbb1111-1111-4111-8111-111111111111" },
							func(r *GroupRequest, _ *GroupBinding) { r.Fence = strings.Split(r.Fence, ":")[0] + ":1" },
							func(r *GroupRequest, _ *GroupBinding) { r.Task = "bbbb1111-1111-4111-8111-111111111111" },
							func(_ *GroupRequest, b *GroupBinding) { b.Actor.Email = "other@example.test" },
							func(_ *GroupRequest, b *GroupBinding) {
								b.Location.CalendarID = "/caldav/v2/other@example.test/events/"
							},
						} {
							different := r
							binding := groupTestBinding
							mutate(&different, &binding)
							b.deadline = time.Time{}
							_, err = ExecuteGroup(context.Background(), b, s, different, binding, groupTestCheck, now)
							if err == nil || len(b.writes) != count {
								t.Fatal("global pending bypass", err)
							}
						}
						b.deadline = time.Time{}
						_, _ = ExecuteGroup(context.Background(), b, s, r, groupTestBinding, groupTestCheck, now)
						if len(b.writes) != count {
							t.Fatal("exact repeat resumed writes")
						}
					}
				})
			}
		}
	}
}

func TestGroupLostFinalAcknowledgementReopensReadOnlyAndSettles(t *testing.T) {
	for _, action := range []string{"create", "delete"} {
		t.Run(action, func(t *testing.T) {
			s := groupTestStore(t)
			schedule := dailyV2Schedule("2027-02-12T09:17", 8, false, "")
			r := groupTestRequest(t, s, schedule)
			b := groupTestBackend(nil)
			if action == "delete" {
				objects := dailyGroupObjects(t, dailyV2Compile(t, schedule))
				r = groupDeleteRequest(t, s, objects)
				b = groupTestBackend(objects)
			}
			b.afterWrite = func(n int) error {
				if n == 3 {
					return groupTestError
				}
				return nil
			}
			if _, err := groupRun(t, b, s, r); err == nil {
				t.Fatal("missing lost-ack failure")
			}
			op := groupPending(t, s)
			if op.Targets[2].Phase != "entered" {
				t.Fatal("uncertainty not persisted")
			}
			reopened := &groupMemoryStore{raw: append([]byte(nil), s.raw...)}
			b.afterWrite = nil
			b.deadline = time.Time{}
			out, err := ReconcileGroup(context.Background(), groupOnlyReader{b}, reopened, r, groupTestBinding, groupTestCheck)
			if err != nil || !out.Settled || out.Calls != 4 || len(b.writes) != 3 {
				t.Fatalf("replay %+v %v writes=%d", out, err, len(b.writes))
			}
			b.objects = map[string]caldav.CalendarObject{}
			_, err = groupRun(t, b, reopened, r)
			if !errors.Is(err, ErrConflict) || len(b.writes) != 3 {
				t.Fatal("stale replay wrote", err)
			}
		})
	}
}

func TestGroupClearStoreFailureAndDisconnectedReplay(t *testing.T) {
	s := groupTestStore(t)
	r := groupTestRequest(t, s, dailyV2Schedule("2027-02-12T09:17", 24, false, ""))
	b := groupTestBackend(nil)
	s.before = func(_ int, raw []byte) error {
		var state groupState
		_ = json.Unmarshal(raw, &state)
		if state.Pending == nil {
			return groupTestError
		}
		return nil
	}
	if _, err := groupRun(t, b, s, r); err == nil || len(b.writes) != 1 {
		t.Fatal("clear failure not retained", err)
	}
	groupPending(t, s)
	before := string(s.raw)
	s.before = nil
	b.deadline = time.Time{}
	disconnected := func(context.Context, GroupBinding) error { return groupTestError }
	if _, err := ReconcileGroup(context.Background(), groupOnlyReader{b}, s, r, groupTestBinding, disconnected); err == nil || string(s.raw) != before {
		t.Fatal("disconnection cleared pending")
	}
	b.deadline = time.Time{}
	out, err := ReconcileGroup(context.Background(), groupOnlyReader{b}, s, r, groupTestBinding, groupTestCheck)
	if err != nil || !out.Settled || len(b.writes) != 1 {
		t.Fatal(out, err)
	}
}

func TestGroupDeleteReportGapsAndReplacementsStopFurtherDeletes(t *testing.T) {
	for _, kind := range []string{"VTODO", "noUID", "weak", "malformed", "changed-raw", "retag", "replaced-deleted-prefix", "omitted-current"} {
		t.Run(kind, func(t *testing.T) {
			objects := dailyGroupObjects(t, dailyV2Compile(t, dailyV2Schedule("2027-02-12T09:17", 8, false, "")))
			s := groupTestStore(t)
			r := groupDeleteRequest(t, s, objects)
			b := groupTestBackend(objects)
			// Replace the deleted first href after its immediate 404 receipt, then omit
			// the replacement in REPORT. Only its actual GET can establish absence.
			prefix := objects[0].Path
			probeCount := 0
			b.probe = func(h string, p GroupProbe) (GroupProbe, error) {
				if h == prefix && len(b.writes) == 1 {
					probeCount++
					if probeCount == 1 {
						return p, nil
					}
					replacement := dailyPartsCloneObject(objects[0])
					switch kind {
					case "VTODO":
						replacement.Data.Children[0].Name = "VTODO"
					case "noUID":
						replacement.Data.Events()[0].Props.Del("UID")
					case "weak":
						replacement.ETag = `W/"weak"`
					case "malformed":
						replacement.Data = nil
					case "changed-raw":
						replacement.Data.Events()[0].Props.SetText("X-CHANGED", "yes")
					case "retag":
						replacement.ETag = `"retag"`
					}
					return GroupProbe{Object: replacement}, nil
				}
				return p, nil
			}
			if kind == "replaced-deleted-prefix" {
				b.afterWrite = func(n int) error {
					if n == 1 {
						o := dailyPartsCloneObject(objects[0])
						o.Data.Children[0].Name = "VTODO"
						b.objects[prefix] = o
					}
					return nil
				}
			}
			if kind == "omitted-current" {
				b.report = func(out []caldav.CalendarObject) []caldav.CalendarObject {
					if len(b.writes) == 1 {
						return nil
					}
					return out
				}
			}
			_, err := groupRun(t, b, s, r)
			if err == nil || len(b.writes) != 1 {
				t.Fatal("deleted beyond replacement/gap", err, len(b.writes))
			}
			groupPending(t, s)
		})
	}
}

func TestGroupOnceReceiptCapturedAnyLaterRawChangeBlocks(t *testing.T) {
	for _, key := range []string{"DTSTAMP", "LAST-MODIFIED", "PRODID", "ETag", "X-EXTRA"} {
		t.Run(key, func(t *testing.T) {
			s := groupTestStore(t)
			r := groupTestRequest(t, s, dailyV2Schedule("2027-02-12T09:17", 8, false, ""))
			b := groupTestBackend(nil)
			changed := false
			b.report = func(out []caldav.CalendarObject) []caldav.CalendarObject {
				if len(b.writes) == 1 && !changed {
					changed = true
					for _, o := range b.objects {
						switch key {
						case "ETag":
							o.ETag = `"retag"`
						case "PRODID":
							o.Data.Props.SetText(key, "another provider")
						default:
							o.Data.Events()[0].Props.SetText(key, "20280212T000000Z")
						}
						b.objects[o.Path] = o
					}
					out = nil
					for _, o := range b.objects {
						out = append(out, dailyPartsCloneObject(o))
					}
				}
				return out
			}
			_, err := groupRun(t, b, s, r)
			if err == nil || len(b.writes) != 1 {
				t.Fatal("later metadata normalization granted authority", err, len(b.writes))
			}
		})
	}
}

func TestGroupInvocationDeadlineCallLimitAndGenerationOverflow(t *testing.T) {
	b := groupTestBackend(nil)
	calls := &groupCalls{reader: b}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	for i := 0; i < 512; i++ {
		if _, err := calls.Snapshot(ctx, dailyGroupLocation.CalendarID); err != nil {
			t.Fatal(i, err)
		}
	}
	if _, err := calls.Probe(ctx, dailyGroupLocation.CalendarID, dailyGroupLocation.CalendarID+"a.ics"); err == nil || len(b.methods) != 512 {
		t.Fatal("call bound exceeded")
	}
	cancel()
	if err := calls.take(ctx); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	s := groupTestStore(t)
	state, _ := groupDecodeState(s.raw)
	state.Generation = ^uint64(0)
	s.raw, _ = groupEncodeState(state)
	r := groupTestRequest(t, s, dailyV2Schedule("2027-02-12T09:17", 24, false, ""))
	b = groupTestBackend(nil)
	if _, err := groupRun(t, b, s, r); !errors.Is(err, ErrConflict) || len(b.methods) != 0 {
		t.Fatal("overflow admitted", err)
	}
	// Cancellation between durable entered and write leaves uncertainty but no PUT.
	s = groupTestStore(t)
	r = groupTestRequest(t, s, *r.Schedule)
	b = groupTestBackend(nil)
	ctx, cancel = context.WithCancel(context.Background())
	defer cancel()
	s.after = func(_ int, raw []byte) error {
		var state groupState
		_ = json.Unmarshal(raw, &state)
		if state.Pending != nil && state.Pending.Targets[0].Phase == "entered" {
			cancel()
		}
		return nil
	}
	_, err := ExecuteGroup(ctx, b, s, r, groupTestBinding, groupTestCheck, now)
	if !errors.Is(err, context.Canceled) || len(b.writes) != 0 {
		t.Fatal("write after cancellation", err)
	}
	groupPending(t, s)
}

func TestGroupUnexpectedOrOversizedCreateResponseStopsWrites(t *testing.T) {
	for _, position := range []int{1, 12, 24} {
		for _, kind := range []string{"wrong-body", "oversized-data", "oversized-etag"} {
			t.Run(fmt.Sprintf("%d/%s", position, kind), func(t *testing.T) {
				s := groupTestStore(t)
				r := groupTestRequest(t, s, dailyV2Schedule("2027-02-12T09:17", 0.5, false, ""))
				b := groupTestBackend(nil)
				b.probe = func(_ string, p GroupProbe) (GroupProbe, error) {
					if len(b.writes) == position && !p.Absent {
						switch kind {
						case "wrong-body":
							p.Object.Data.Events()[0].Props.SetText("SUMMARY", "unexpected")
						case "oversized-data":
							p.Object.Data.Props.SetText("PRODID", strings.Repeat("x", groupReceiptGrowth+10000))
						case "oversized-etag":
							p.Object.ETag = `"` + strings.Repeat("x", groupReceiptTagLimit) + `"`
						}
					}
					return p, nil
				}
				if _, err := groupRun(t, b, s, r); err == nil || len(b.writes) != position {
					t.Fatal("continued past unexpected response", err, len(b.writes))
				}
				op := groupPending(t, s)
				if op.Targets[position-1].Phase != "ack" || op.Targets[position-1].Receipt != nil {
					t.Fatal("bad response recorded as authoritative receipt")
				}
			})
		}
	}
}

func TestGroupAdmissionAndSettlementStoreAmbiguity(t *testing.T) {
	for _, clear := range []bool{false, true} {
		t.Run(fmt.Sprint(clear), func(t *testing.T) {
			s := groupTestStore(t)
			r := groupTestRequest(t, s, dailyV2Schedule("2027-02-12T09:17", 24, false, ""))
			b := groupTestBackend(nil)
			s.after = func(_ int, raw []byte) error {
				var state groupState
				_ = json.Unmarshal(raw, &state)
				if (state.Pending == nil) == clear {
					return groupTestError
				}
				return nil
			}
			_, err := groupRun(t, b, s, r)
			if err == nil {
				t.Fatal("missing ambiguous store failure")
			}
			s.after = nil
			if !clear {
				if len(b.writes) != 0 {
					t.Fatal("write after admission-save error")
				}
				groupPending(t, s)
			} else {
				if len(b.writes) != 1 {
					t.Fatal("bad write count")
				}
				_, pending, err := GroupStateFence(s.raw)
				if err != nil || pending {
					t.Fatal("atomic settlement not committed", err)
				}
			}
			count := len(b.writes)
			_, _ = groupRun(t, b, s, r)
			if len(b.writes) != count {
				t.Fatal("retry wrote after ambiguous store save")
			}
		})
	}
}

func TestGroupDiscoveredReceiptDurableBeforeLaterFailure(t *testing.T) {
	for _, failure := range []string{"clear", "later-probe", "actor", "membership"} {
		for _, change := range []string{"PRODID", "DTSTAMP", "ETag"} {
			t.Run(failure+"/"+change, func(t *testing.T) {
				s := groupTestStore(t)
				r := groupTestRequest(t, s, dailyV2Schedule("2027-02-12T09:17", 8, false, ""))
				lostAck := 1
				if failure == "clear" {
					lostAck = 3 // Last PUT after a durably verified prefix.
				}
				b := groupTestBackend(nil)
				b.afterWrite = func(n int) error {
					if n == lostAck {
						return groupTestError
					}
					return nil
				}
				if _, err := groupRun(t, b, s, r); err == nil || len(b.writes) != lostAck {
					t.Fatal("missing lost PUT acknowledgement", err)
				}
				op := groupPending(t, s)
				if op.Targets[lostAck-1].Receipt != nil || op.Targets[lostAck-1].Phase != "entered" {
					t.Fatal("fixture did not retain uncertainty")
				}
				// For earlier discoveries, synthesize the complete desired state
				// while the journal still has only the first uncertain write.
				for i, target := range op.Targets {
					if i >= lostAck {
						o := dailyPartsCloneObject(*target.Planned)
						o.ETag = fmt.Sprintf(`"external-%d"`, i)
						b.objects[o.Path] = o
					}
				}
				b.afterWrite = nil
				failed := false
				s.before = func(_ int, raw []byte) error {
					var state groupState
					if err := json.Unmarshal(raw, &state); err != nil {
						t.Fatal(err)
					}
					if failure == "clear" && state.Pending == nil {
						failed = true
						return groupTestError
					}
					return nil
				}
				b.probe = func(href string, p GroupProbe) (GroupProbe, error) {
					if failure == "later-probe" && href == op.Targets[1].Href {
						failed = true
						return GroupProbe{}, groupTestError
					}
					return p, nil
				}
				checks := 0
				check := func(ctx context.Context, binding GroupBinding) error {
					checks++
					if failure == "actor" && checks == 2 {
						failed = true
						return groupTestError
					}
					return groupTestCheck(ctx, binding)
				}
				b.report = func(objects []caldav.CalendarObject) []caldav.CalendarObject {
					if failure == "membership" {
						failed = true
						return nil
					}
					return objects
				}
				b.deadline = time.Time{}
				out, err := ReconcileGroup(context.Background(), groupOnlyReader{b}, s, r, groupTestBinding, check)
				if err == nil || !failed || !out.Pending || out.Settled || len(b.writes) != lostAck {
					t.Fatal("later failure not contained", out, err, failed)
				}
				captured := groupPending(t, s).Targets[lostAck-1]
				if captured.Receipt == nil || captured.Phase != "verified" || !groupSame(*captured.Receipt, b.objects[captured.Href]) {
					t.Fatal("discovered receipt not durably frozen")
				}
				o := b.objects[captured.Href]
				switch change {
				case "PRODID":
					o.Data.Props.SetText("PRODID", "another allowed provider")
				case "DTSTAMP":
					groupSetRaw(o.Data.Events()[0].Props, "DTSTAMP", "20280212T000000Z")
				case "ETag":
					o.ETag = `"another-strong-tag"`
				}
				b.objects[o.Path] = o
				if err := groupInitialReceipt(captured, o); err != nil {
					t.Fatal("changed fixture must still satisfy initial policy", err)
				}
				reopened := &groupMemoryStore{raw: append([]byte(nil), s.raw...)}
				before := string(reopened.raw)
				b.probe, b.report = nil, nil
				b.deadline = time.Time{}
				out, err = ReconcileGroup(context.Background(), groupOnlyReader{b}, reopened, r, groupTestBinding, groupTestCheck)
				if !errors.Is(err, ErrConflict) || !out.Pending || out.Settled || string(reopened.raw) != before || len(b.writes) != lostAck {
					t.Fatal("reopened reconciliation reaccepted changed receipt", out, err)
				}
			})
		}
	}
}

func TestGroupDiscoverySaveFailureStopsBeforeLaterReadsOrClear(t *testing.T) {
	for _, committed := range []bool{false, true} {
		t.Run(fmt.Sprint(committed), func(t *testing.T) {
			s := groupTestStore(t)
			r := groupTestRequest(t, s, dailyV2Schedule("2027-02-12T09:17", 8, false, ""))
			b := groupTestBackend(nil)
			b.afterWrite = func(int) error { return groupTestError }
			if _, err := groupRun(t, b, s, r); err == nil {
				t.Fatal("missing lost acknowledgement")
			}
			op := groupPending(t, s)
			for i, target := range op.Targets {
				if i > 0 {
					o := dailyPartsCloneObject(*target.Planned)
					o.ETag = `"external"`
					b.objects[o.Path] = o
				}
			}
			before := string(s.raw)
			failed := false
			fault := func(_ int, raw []byte) error {
				var state groupState
				if err := json.Unmarshal(raw, &state); err != nil {
					t.Fatal(err)
				}
				if state.Pending == nil {
					t.Fatal("continued to clear after discovery-save failure")
				}
				if state.Pending.Targets[0].Phase == "verified" && state.Pending.Targets[0].Receipt != nil {
					failed = true
					return groupTestError
				}
				return nil
			}
			if committed {
				s.after = fault
			} else {
				s.before = fault
			}
			b.afterWrite = nil
			b.deadline = time.Time{}
			methods := len(b.methods)
			out, err := ReconcileGroup(context.Background(), groupOnlyReader{b}, s, r, groupTestBinding, groupTestCheck)
			if !errors.Is(err, groupTestError) || !failed || !out.Pending || out.Settled || out.Calls != 2 || len(b.methods)-methods != 2 || len(b.writes) != 1 {
				t.Fatal("continued after discovery-save failure", out, err)
			}
			saved := groupPending(t, s).Targets[0]
			if committed {
				if saved.Receipt == nil || saved.Phase != "verified" {
					t.Fatal("committed receipt lost")
				}
			} else if string(s.raw) != before || saved.Receipt != nil || saved.Phase != "entered" {
				t.Fatal("uncommitted save changed conservative state")
			}
		})
	}
}
