package corpus

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"hash"
	"time"
)

// Bump for changes anywhere in HTML/Peel/preamble/image/token/link reduction.
const twinBodyVersion = 3

// Cache limits do not limit matching: oversized or budget-exhausted entries
// are reduced normally, without retaining a payload for publication.
const twinBodyMaxPayload = 1 << 20
const twinBodyMaxStrings = 1 << 18
const twinBodyMaxPendingBytes = 64 << 20
const twinBodyMaxPendingRows = 8192

type twinBodyRow struct {
	id      int64
	digest  [sha256.Size]byte
	payload []byte
}

// Per invocation only. A hit still reads the raw body and participates in the
// complete current author plan; these counts are not logical skipped work.
type twinBodyCache struct {
	inspected, hits, reductions int
	pending                     []twinBodyRow
	pendingBytes                int
}

func twinBodyDigest(source, body, html string, quoted bool) [sha256.Size]byte {
	h := sha256.New()
	frameTwinInput(h, "twins-body")
	frameTwinInput(h, source)
	frameTwinInput(h, body)
	frameTwinInput(h, html)
	var versions [17]byte
	binary.LittleEndian.PutUint64(versions[:8], twinBodyVersion)
	binary.LittleEndian.PutUint64(versions[8:16], reconciliationVersion)
	if quoted {
		versions[16] = 1
	}
	h.Write(versions[:])
	var out [sha256.Size]byte
	h.Sum(out[:0])
	return out
}

func frameTwinInput(h hash.Hash, s string) {
	var size [8]byte
	binary.LittleEndian.PutUint64(size[:], uint64(len(s)))
	h.Write(size[:])
	h.Write([]byte(s))
}

func (cache *twinBodyCache) reduce(c *twinCopy, body, source string, version int64, digest, payload []byte) {
	cache.inspected++
	key := twinBodyDigest(source, body, c.bodyHTML, c.quoted)
	if version == twinBodyVersion && bytes.Equal(key[:], digest) && decodeTwinBody(payload, c) {
		cache.hits++
		return
	}
	cache.reductions++
	reduceTwinBody(c, body)
	// An older reader must not downgrade a row written by a newer binary.
	if version > twinBodyVersion {
		return
	}
	if len(cache.pending) >= twinBodyMaxPendingRows || cache.pendingBytes >= twinBodyMaxPendingBytes {
		return
	}
	if payload := encodeTwinBody(c); payload != nil && len(payload) <= twinBodyMaxPendingBytes-cache.pendingBytes {
		cache.pending = append(cache.pending, twinBodyRow{c.id, key, payload})
		cache.pendingBytes += len(payload)
	}
}

// Binary length framing preserves arbitrary URL bytes, ordered repetitions, and
// nil versus empty arrays. Checksum verification precedes all token allocation.
func encodeTwinBody(c *twinCopy) []byte {
	arrays := [][]string{c.words, c.prose, c.links.shape, c.links.urls, {c.plain}}
	size, count := sha256.Size+6*4, 0
	for _, a := range arrays {
		count += len(a)
		if count > twinBodyMaxStrings {
			return nil
		}
		for _, s := range a {
			if len(s) > twinBodyMaxPayload-size-4 {
				return nil
			}
			size += 4 + len(s)
		}
	}
	if size > twinBodyMaxPayload || c.links.words < 0 || c.links.words > twinBodyMaxStrings {
		return nil
	}
	out := make([]byte, sha256.Size, size)
	out = binary.LittleEndian.AppendUint32(out, uint32(c.links.words))
	for _, a := range arrays {
		n := uint32(len(a))
		if a == nil {
			n = ^uint32(0)
		}
		out = binary.LittleEndian.AppendUint32(out, n)
		for _, s := range a {
			out = binary.LittleEndian.AppendUint32(out, uint32(len(s)))
			out = append(out, s...)
		}
	}
	sum := sha256.Sum256(out[sha256.Size:])
	copy(out, sum[:])
	return out
}

func decodeTwinBody(payload []byte, c *twinCopy) bool {
	if len(payload) < sha256.Size+6*4 || len(payload) > twinBodyMaxPayload {
		return false
	}
	sum := sha256.Sum256(payload[sha256.Size:])
	if !bytes.Equal(payload[:sha256.Size], sum[:]) {
		return false
	}
	p := payload[sha256.Size:]
	take := func() (uint32, bool) {
		if len(p) < 4 {
			return 0, false
		}
		n := binary.LittleEndian.Uint32(p)
		p = p[4:]
		return n, true
	}
	words, _ := take()
	if words > twinBodyMaxStrings {
		return false
	}
	var arrays [5][]string
	total := 0
	for i := range arrays {
		n, ok := take()
		if !ok {
			return false
		}
		if n == ^uint32(0) {
			continue
		}
		if n > twinBodyMaxStrings || int(n) > len(p)/4 {
			return false
		}
		total += int(n)
		if total > twinBodyMaxStrings {
			return false
		}
		a := make([]string, int(n))
		for j := range a {
			size, ok := take()
			if !ok || uint64(size) > uint64(len(p)) {
				return false
			}
			a[j] = string(p[:int(size)])
			p = p[int(size):]
		}
		arrays[i] = a
	}
	if len(p) != 0 || len(arrays[4]) != 1 {
		return false
	}
	c.words, c.prose = arrays[0], arrays[1]
	c.plain = arrays[4][0]
	c.links = quoteLinks{shape: arrays[2], urls: arrays[3], words: int(words)}
	return true
}

func publishTwinBodies(ctx context.Context, rows []twinBodyRow, original int64, write twinWrite) (time.Duration, error) {
	var wait time.Duration
	for len(rows) > 0 {
		if len(rows[0].payload) > twinBodyMaxPayload {
			rows = rows[1:]
			continue
		}
		// Accepted payloads are at most 1 MiB, so even a single-row batch
		// obeys the byte target. Limit statement count independently.
		n, size := 0, 0
		for n < len(rows) && n < 64 && (n == 0 || size+len(rows[n].payload) <= 1<<20) {
			size += len(rows[n].payload)
			n++
		}
		batch := rows[:n]
		expected := original
		elapsed, err := write(ctx, &expected, func(w corpusWriter) error {
			for _, row := range batch {
				if _, err := w.Exec(`insert into twins_body_cache(entry_id,format_version,input_digest,payload)
     values(?,?,?,?) on conflict(entry_id) do update set
     format_version=excluded.format_version,input_digest=excluded.input_digest,payload=excluded.payload
     where typeof(twins_body_cache.format_version)<>'integer'
        or twins_body_cache.format_version<=excluded.format_version`, row.id, twinBodyVersion, row.digest[:], row.payload); err != nil {
					return err
				}
			}
			return nil
		})
		wait += elapsed
		if err != nil {
			return wait, err
		}
		rows = rows[n:]
	}
	return wait, nil
}
