// Copyright 2025 The Envoy XDS Controller Authors.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package xds

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"hash"
	"math"
	"slices"
	"sort"
	"sync"

	listener "github.com/envoyproxy/go-control-plane/envoy/config/listener/v3"
	"github.com/envoyproxy/go-control-plane/pkg/cache/types"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"
	"google.golang.org/protobuf/types/known/anypb"
)

type digest = [sha256.Size]byte

// GetHash returns a version for the resources that ignores the order of resources and of repeated fields.
func GetHash(resources map[string][]types.Resource) string {
	var h hasher
	return h.resources(resources)
}

// snapshotMemo carries work from one snapshot pass to the next.
type snapshotMemo struct {
	mu     sync.Mutex
	hashes hasher
	routes passMemo[[]string]
}

func (s *snapshotMemo) endPass() {
	s.hashes.endPass()
	s.routes.endPass()
}

// passMemo caches values per message pointer. Controllers replace resources instead of
// changing them, and built Any values are shared, never modified. The memo keeps its keys
// alive, so an address cannot be reused while it is cached.
type passMemo[V any] struct {
	cur, prev map[proto.Message]V
}

// endPass forgets messages the last pass did not reach.
func (p *passMemo[V]) endPass() {
	p.prev, p.cur = p.cur, make(map[proto.Message]V, len(p.cur))
}

func (p *passMemo[V]) get(m proto.Message, compute func() V) V {
	if p == nil {
		return compute()
	}
	if v, ok := p.cur[m]; ok {
		return v
	}
	v, ok := p.prev[m]
	if !ok {
		v = compute()
	}
	if p.cur == nil {
		p.cur = make(map[proto.Message]V)
	}
	p.cur[m] = v
	return v
}

// hasher digests messages bottom-up.
type hasher struct {
	passMemo[digest]
	free []hash.Hash
	// unpacking counts Any values being decoded; messages inside them are fresh copies, not worth caching.
	unpacking int
}

func (h *hasher) resources(resources map[string][]types.Resource) string {
	keys := make([]string, 0, len(resources))
	for k := range resources {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	w := h.writer()
	defer h.put(w)
	for _, k := range keys {
		ds := make([]digest, 0, len(resources[k]))
		for _, r := range resources[k] {
			ds = append(ds, h.get(r, func() digest { return h.message(r.ProtoReflect()) }))
		}
		slices.SortFunc(ds, func(a, b digest) int { return bytes.Compare(a[:], b[:]) })
		writeBytes(w, []byte(k))
		writeUint(w, uint64(len(ds)))
		for i := range ds {
			w.Write(ds[i][:])
		}
	}
	return hex.EncodeToString(w.Sum(nil))
}

func (h *hasher) writer() hash.Hash {
	if n := len(h.free); n > 0 {
		w := h.free[n-1]
		h.free = h.free[:n-1]
		w.Reset()
		return w
	}
	return sha256.New()
}

func (h *hasher) put(w hash.Hash) { h.free = append(h.free, w) }

func (h *hasher) message(m protoreflect.Message) digest {
	switch msg := m.Interface().(type) {
	case *anypb.Any:
		if h.unpacking > 0 {
			return h.any(msg)
		}
		return h.get(msg, func() digest { return h.any(msg) })
	case *listener.FilterChain:
		// LDS shares built chains across listener rebuilds.
		if h.unpacking == 0 {
			return h.get(msg, func() digest { return h.fields(m) })
		}
	}
	return h.fields(m)
}

func (h *hasher) fields(m protoreflect.Message) digest {
	w := h.writer()
	defer h.put(w)

	fields := m.Descriptor().Fields()
	for i := range fields.Len() {
		fd := fields.Get(i)
		if !m.Has(fd) {
			continue
		}
		writeInt(w, int64(fd.Number()))
		v := m.Get(fd)
		switch {
		case fd.IsList():
			l := v.List()
			elems := make([][]byte, l.Len())
			for j := range elems {
				elems[j] = h.value(fd, l.Get(j))
			}
			writeSorted(w, elems)
		case fd.IsMap():
			var entries [][]byte
			v.Map().Range(func(k protoreflect.MapKey, mv protoreflect.Value) bool {
				e := h.writer()
				writeBytes(e, h.value(fd.MapKey(), k.Value()))
				writeBytes(e, h.value(fd.MapValue(), mv))
				entries = append(entries, e.Sum(nil))
				h.put(e)
				return true
			})
			writeSorted(w, entries)
		default:
			writeBytes(w, h.value(fd, v))
		}
	}
	writeBytes(w, m.GetUnknown())
	return sum(w)
}

// any digests the packed message, so equal configs packed with different map orders match.
func (h *hasher) any(a *anypb.Any) digest {
	w := h.writer()
	defer h.put(w)
	writeBytes(w, []byte(a.GetTypeUrl()))
	if mt, err := protoregistry.GlobalTypes.FindMessageByURL(a.GetTypeUrl()); err == nil {
		inner := mt.New()
		if proto.Unmarshal(a.GetValue(), inner.Interface()) == nil {
			h.unpacking++
			d := h.message(inner)
			h.unpacking--
			w.Write(d[:])
			return sum(w)
		}
	}
	writeBytes(w, a.GetValue())
	return sum(w)
}

func (h *hasher) value(fd protoreflect.FieldDescriptor, v protoreflect.Value) []byte {
	var b [binary.MaxVarintLen64]byte
	switch fd.Kind() {
	case protoreflect.MessageKind, protoreflect.GroupKind:
		d := h.message(v.Message())
		return d[:]
	case protoreflect.StringKind:
		return []byte(v.String())
	case protoreflect.BytesKind:
		return v.Bytes()
	case protoreflect.BoolKind:
		if v.Bool() {
			return []byte{1}
		}
		return []byte{0}
	case protoreflect.EnumKind:
		return b[:binary.PutVarint(b[:], int64(v.Enum()))]
	case protoreflect.FloatKind, protoreflect.DoubleKind:
		return b[:binary.PutUvarint(b[:], math.Float64bits(v.Float()))]
	case protoreflect.Uint32Kind, protoreflect.Uint64Kind, protoreflect.Fixed32Kind, protoreflect.Fixed64Kind:
		return b[:binary.PutUvarint(b[:], v.Uint())]
	default:
		return b[:binary.PutVarint(b[:], v.Int())]
	}
}

func writeSorted(w hash.Hash, elems [][]byte) {
	slices.SortFunc(elems, bytes.Compare)
	writeUint(w, uint64(len(elems)))
	for _, e := range elems {
		writeBytes(w, e)
	}
}

func writeBytes(w hash.Hash, b []byte) {
	writeUint(w, uint64(len(b)))
	w.Write(b)
}

func writeInt(w hash.Hash, n int64) {
	var b [binary.MaxVarintLen64]byte
	w.Write(b[:binary.PutVarint(b[:], n)])
}

func writeUint(w hash.Hash, n uint64) {
	var b [binary.MaxVarintLen64]byte
	w.Write(b[:binary.PutUvarint(b[:], n)])
}

func sum(w hash.Hash) (d digest) {
	w.Sum(d[:0])
	return d
}
