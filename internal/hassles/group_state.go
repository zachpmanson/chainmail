package hassles

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
)

const groupStoreLimit = 2_000_000
const groupEnvelopeBytes = 36
const groupStateVersion = 1

// The future persistence layer must supply an exclusive lease covering Load,
// admission, every Save and settlement. This interface is not a remote CAS or a
// promise of disk durability; implementations must atomically replace the value.
type GroupStore interface {
	Load(context.Context) ([]byte, error)
	Save(context.Context, []byte) error
}

type groupState struct {
	Version    int
	Epoch      string
	Generation uint64
	Pending    *groupOperation
}

func (s groupState) fence() string { return s.Epoch + ":" + strconv.FormatUint(s.Generation, 10) }
func groupFenceValid(s string) bool {
	parts := strings.Split(s, ":")
	if len(parts) != 2 || !dailyPartsV2UUID(parts[0]) {
		return false
	}
	n, err := strconv.ParseUint(parts[1], 10, 64)
	return err == nil && strconv.FormatUint(n, 10) == parts[1]
}

// NewGroupState creates bytes for explicit store provisioning by a later layer.
// Missing/unknown store data is never silently initialized by the executor.
func NewGroupState() ([]byte, error) {
	var id [16]byte
	if _, err := rand.Read(id[:]); err != nil {
		return nil, err
	}
	id[6] = id[6]&15 | 64
	id[8] = id[8]&63 | 128
	epoch := fmt.Sprintf("%x-%x-%x-%x-%x", id[:4], id[4:6], id[6:8], id[8:10], id[10:])
	return json.Marshal(groupState{Version: groupStateVersion, Epoch: epoch})
}
func GroupStateFence(raw []byte) (string, bool, error) {
	s, err := groupDecodeState(raw)
	return s.fence(), s.Pending != nil, err
}
func groupDecodeState(raw []byte) (groupState, error) {
	var s groupState
	if err := groupCanonicalDecode(raw, &s); err != nil {
		return s, err
	}
	if s.Version != groupStateVersion || !dailyPartsV2UUID(s.Epoch) {
		return s, errors.New("unsupported or invalid group state")
	}
	if s.Pending != nil {
		if s.Generation == math.MaxUint64 {
			return s, errors.New("group generation overflow")
		}
		if err := groupValidateOperation(*s.Pending); err != nil {
			return s, err
		}
		r, err := DecodeGroupRequest([]byte(s.Pending.Request))
		if err != nil || r.Fence != s.fence() {
			return s, errors.New("pending fence mismatch")
		}
	}
	return s, nil
}
func groupEncodeState(s groupState) ([]byte, error) {
	raw, err := json.Marshal(s)
	if err != nil {
		return nil, err
	}
	if len(raw) > groupStoreLimit-groupEnvelopeBytes {
		return nil, errors.New("group store capacity exceeded")
	}
	return raw, nil
}

// Upper bound the actual encoded record, not wire iCalendar bytes. Receipt Data
// is embedded JSON (no second escaping), Path is escaped exactly, and a 512-byte
// ASCII ETag needs at most 3072 bytes (HTML-safe JSON escapes <, > and &). The 256-byte allowance per object exceeds
// fixed field names, quotes, any int64 length and JSON time representation.
// All growth allowances use subtraction checks, so arithmetic cannot overflow.
func groupMaximumRecordBytes(s groupState) (int, error) {
	raw, err := groupEncodeState(s)
	if err != nil {
		return 0, err
	}
	total := len(raw) + groupEnvelopeBytes
	add := func(n int) bool {
		if n < 0 || n > groupStoreLimit-total {
			return false
		}
		total += n
		return true
	}
	if !add(20) {
		return 0, errors.New("fence growth exceeds group store budget")
	}
	if s.Pending == nil {
		return total, nil
	}
	for _, t := range s.Pending.Targets {
		if !add(32) {
			return 0, errors.New("phase growth exceeds group store budget")
		}
		if t.Planned != nil && t.Receipt == nil {
			data, err := json.Marshal(t.Planned.Data)
			if err != nil {
				return 0, err
			}
			path, err := json.Marshal(t.Href)
			if err != nil {
				return 0, err
			}
			for _, n := range []int{len(data), groupReceiptGrowth, len(path), 6 * groupReceiptTagLimit, 256} {
				if !add(n) {
					return 0, errors.New("maximum receipts exceed group store budget")
				}
			}
		}
	}
	return total, nil
}
