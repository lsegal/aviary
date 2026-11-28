package projects

import (
	"encoding/json"
	"hash/fnv"
	"slices"
	"strconv"
	"sync"
)

// listDeltaRevisions is how many job list revisions the server remembers to
// diff a later request against. A board keeps two lists (jobs and
// schedules), so this covers a few dozen open boards; an older revision gets
// the full list back.
const listDeltaRevisions = 64

// listSnapshot is what a job list revision held: its entries' keys in order
// and a hash of each entry, enough to tell which entries a later list added,
// changed or removed.
type listSnapshot struct {
	revision string
	keys     []string
	hashes   map[string]uint64
}

// listDeltas remembers recent job list revisions (issue #1661). Its zero
// value is ready to use.
type listDeltas struct {
	mu        sync.Mutex
	snapshots []listSnapshot
}

func (d *listDeltas) get(revision string) (listSnapshot, bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	for _, s := range d.snapshots {
		if s.revision == revision {
			return s, true
		}
	}
	return listSnapshot{}, false
}

// put records a revision, keeping only the newest listDeltaRevisions.
func (d *listDeltas) put(s listSnapshot) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.snapshots = slices.DeleteFunc(d.snapshots, func(old listSnapshot) bool { return old.revision == s.revision })
	d.snapshots = append(d.snapshots, s)
	if extra := len(d.snapshots) - listDeltaRevisions; extra > 0 {
		d.snapshots = slices.Delete(d.snapshots, 0, extra)
	}
}

// snapshotList hashes each of a list's entries and the rest of the list,
// returning the list's revision and its snapshot. Each entry is serialized
// once. ok is false when two entries share a key, which can't be diffed.
func snapshotList(list map[string]any, page []map[string]any) (listSnapshot, bool) {
	s := listSnapshot{keys: make([]string, 0, len(page)), hashes: make(map[string]uint64, len(page))}
	ok := true
	sum := fnv.New64a()
	for _, entry := range page {
		key, _ := entry["key"].(string)
		h := hashJSON(entry)
		if _, dup := s.hashes[key]; dup {
			ok = false
		}
		s.keys = append(s.keys, key)
		s.hashes[key] = h
		sum.Write(strconv.AppendUint(nil, h, 36))
		sum.Write([]byte{0})
	}
	rest := make(map[string]any, len(list))
	for k, v := range list {
		if k != "jobs" {
			rest[k] = v
		}
	}
	sum.Write(strconv.AppendUint(nil, hashJSON(rest), 36))
	s.revision = strconv.FormatUint(sum.Sum64(), 36)
	return s, ok
}

func hashJSON(v any) uint64 {
	data, err := json.Marshal(v)
	if err != nil {
		return 0
	}
	sum := fnv.New64a()
	sum.Write(data)
	return sum.Sum64()
}

// applyListDelta turns list, whose page is now, into the changes since
// before: jobs holds only the entries added or changed, removed the keys
// gone, and keys the new order when it differs from before's.
func applyListDelta(list map[string]any, page []map[string]any, now, before listSnapshot) {
	changed := make([]map[string]any, 0)
	for i, entry := range page {
		if h, ok := before.hashes[now.keys[i]]; !ok || h != now.hashes[now.keys[i]] {
			changed = append(changed, entry)
		}
	}
	removed := make([]string, 0)
	for _, key := range before.keys {
		if _, ok := now.hashes[key]; !ok {
			removed = append(removed, key)
		}
	}
	list["jobs"] = changed
	list["removed"] = removed
	list["delta"] = true
	if !slices.Equal(now.keys, before.keys) {
		list["keys"] = now.keys
	}
}
