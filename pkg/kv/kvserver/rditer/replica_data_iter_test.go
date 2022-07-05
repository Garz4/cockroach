// Copyright 2015 The Cockroach Authors.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.txt.
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0, included in the file
// licenses/APL.txt.

package rditer

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"testing"

	"github.com/cockroachdb/cockroach/pkg/keys"
	"github.com/cockroachdb/cockroach/pkg/kv/kvserver/spanset"
	"github.com/cockroachdb/cockroach/pkg/roachpb"
	"github.com/cockroachdb/cockroach/pkg/settings/cluster"
	"github.com/cockroachdb/cockroach/pkg/storage"
	"github.com/cockroachdb/cockroach/pkg/testutils"
	"github.com/cockroachdb/cockroach/pkg/testutils/skip"
	"github.com/cockroachdb/cockroach/pkg/util/hlc"
	"github.com/cockroachdb/cockroach/pkg/util/leaktest"
	"github.com/cockroachdb/cockroach/pkg/util/randutil"
	"github.com/cockroachdb/cockroach/pkg/util/uuid"
	"github.com/stretchr/testify/require"
)

func uuidFromString(input string) uuid.UUID {
	u, err := uuid.FromString(input)
	if err != nil {
		panic(err)
	}
	return u
}

// createRangeData creates sample range data (point and range keys) in all
// possible areas of the key space. Returns a pair of slices containing
// an ordered mix of MVCCKey and MVCCRangeKey with:
// - the encoded keys of all created data.
// - the subset of the encoded keys that are replicated keys.
//
// TODO(sumeer): add lock table and corrsponding MVCC keys.
func createRangeData(
	t *testing.T, eng storage.Engine, desc roachpb.RangeDescriptor,
) ([]interface{}, []interface{}) {

	ctx := context.Background()
	unreplicatedPrefix := keys.MakeRangeIDUnreplicatedPrefix(desc.RangeID)
	replicatedPrefix := keys.MakeRangeIDReplicatedPrefix(desc.RangeID)

	testTxnID := uuidFromString("0ce61c17-5eb4-4587-8c36-dcf4062ada4c")
	testTxnID2 := uuidFromString("9855a1ef-8eb9-4c06-a106-cab1dda78a2b")
	value := roachpb.MakeValueFromString("value")

	ts0 := hlc.Timestamp{}
	ts := hlc.Timestamp{WallTime: 1}
	localTS := hlc.ClockTimestamp{}

	allKeys := []interface{}{
		storage.MVCCKey{Key: keys.AbortSpanKey(desc.RangeID, testTxnID), Timestamp: ts0},
		storage.MVCCKey{Key: keys.AbortSpanKey(desc.RangeID, testTxnID2), Timestamp: ts0},
		storage.MVCCKey{Key: keys.RangeGCThresholdKey(desc.RangeID), Timestamp: ts0},
		storage.MVCCKey{Key: keys.RangeAppliedStateKey(desc.RangeID), Timestamp: ts0},
		storage.MVCCKey{Key: keys.RangeLeaseKey(desc.RangeID), Timestamp: ts0},
		storage.MVCCKey{Key: keys.RangeTombstoneKey(desc.RangeID), Timestamp: ts0},
		storage.MVCCKey{Key: keys.RaftHardStateKey(desc.RangeID), Timestamp: ts0},
		storage.MVCCKey{Key: keys.RaftLogKey(desc.RangeID, 1), Timestamp: ts0},
		storage.MVCCKey{Key: keys.RaftLogKey(desc.RangeID, 2), Timestamp: ts0},
		storage.MVCCKey{Key: keys.RangeLastReplicaGCTimestampKey(desc.RangeID), Timestamp: ts0},
		storage.MVCCRangeKey{
			StartKey:  append(replicatedPrefix.Clone(), []byte(":a")...),
			EndKey:    append(replicatedPrefix.Clone(), []byte(":x")...),
			Timestamp: ts,
		},
		storage.MVCCRangeKey{
			StartKey:  append(unreplicatedPrefix.Clone(), []byte(":a")...),
			EndKey:    append(unreplicatedPrefix.Clone(), []byte(":x")...),
			Timestamp: ts,
		},
		storage.MVCCKey{Key: keys.RangeDescriptorKey(desc.StartKey), Timestamp: ts},
		storage.MVCCKey{Key: keys.TransactionKey(roachpb.Key(desc.StartKey), uuid.MakeV4()), Timestamp: ts0},
		storage.MVCCKey{Key: keys.TransactionKey(roachpb.Key(desc.StartKey.Next()), uuid.MakeV4()), Timestamp: ts0},
		storage.MVCCKey{Key: keys.TransactionKey(roachpb.Key(desc.EndKey).Prevish(100), uuid.MakeV4()), Timestamp: ts0},
		// TODO(bdarnell): KeyMin.Next() results in a key in the reserved system-local space.
		// Once we have resolved https://github.com/cockroachdb/cockroach/issues/437,
		// replace this with something that reliably generates the first valid key in the range.
		//{r.Desc().StartKey.Next(), ts},
		// The following line is similar to StartKey.Next() but adds more to the key to
		// avoid falling into the system-local space.
		storage.MVCCKey{Key: append(desc.StartKey.AsRawKey().Clone(), '\x02'), Timestamp: ts},
		storage.MVCCKey{Key: roachpb.Key(desc.EndKey).Prevish(100), Timestamp: ts},
		storage.MVCCRangeKey{
			StartKey:  desc.StartKey.AsRawKey().Clone(),
			EndKey:    desc.EndKey.AsRawKey().Clone(),
			Timestamp: ts,
		},
	}

	var replicatedKeys []interface{}
	for _, keyI := range allKeys {
		switch key := keyI.(type) {
		case storage.MVCCKey:
			require.NoError(t, storage.MVCCPut(ctx, eng, nil, key.Key, key.Timestamp, localTS, value, nil))
			if !bytes.HasPrefix(key.Key, unreplicatedPrefix) {
				replicatedKeys = append(replicatedKeys, key)
			}
		case storage.MVCCRangeKey:
			require.NoError(t, eng.PutMVCCRangeKey(key, storage.MVCCValue{}))
			if !bytes.HasPrefix(key.StartKey, unreplicatedPrefix) {
				replicatedKeys = append(replicatedKeys, key)
			}
		}
	}

	return allKeys, replicatedKeys
}

func verifyRDReplicatedOnlyMVCCIter(
	t *testing.T, desc *roachpb.RangeDescriptor, eng storage.Engine, expectedKeys []storage.MVCCKey,
) {
	t.Helper()
	verify := func(t *testing.T, useSpanSet, reverse bool) {
		readWriter := eng.NewReadOnly(storage.StandardDurability)
		defer readWriter.Close()
		if useSpanSet {
			var spans spanset.SpanSet
			spans.AddNonMVCC(spanset.SpanReadOnly, roachpb.Span{
				Key:    keys.MakeRangeIDPrefix(desc.RangeID),
				EndKey: keys.MakeRangeIDPrefix(desc.RangeID).PrefixEnd(),
			})
			spans.AddNonMVCC(spanset.SpanReadOnly, roachpb.Span{
				Key:    keys.MakeRangeKeyPrefix(desc.StartKey),
				EndKey: keys.MakeRangeKeyPrefix(desc.EndKey),
			})
			spans.AddMVCC(spanset.SpanReadOnly, roachpb.Span{
				Key:    desc.StartKey.AsRawKey(),
				EndKey: desc.EndKey.AsRawKey(),
			}, hlc.Timestamp{WallTime: 42})
			readWriter = spanset.NewReadWriterAt(readWriter, &spans, hlc.Timestamp{WallTime: 42})
		}
		iter := NewReplicaMVCCDataIterator(desc, readWriter, reverse /* seekEnd */)
		defer iter.Close()
		actualKeys := []storage.MVCCKey{}
		for {
			ok, err := iter.Valid()
			require.NoError(t, err)
			if !ok {
				break
			}
			if !reverse {
				actualKeys = append(actualKeys, iter.Key())
				iter.Next()
			} else {
				actualKeys = append([]storage.MVCCKey{iter.Key()}, actualKeys...)
				iter.Prev()
			}
		}
		require.Equal(t, expectedKeys, actualKeys)
	}
	testutils.RunTrueAndFalse(t, "reverse", func(t *testing.T, reverse bool) {
		testutils.RunTrueAndFalse(t, "spanset", func(t *testing.T, useSpanSet bool) {
			verify(t, useSpanSet, reverse)
		})
	})
}

// verifyRDEngineIter verifies that the ReplicaEngineDataIterator returns the
// expected keys in the expected order. The expected keys can be either MVCCKey
// or MVCCRangeKey.
func verifyRDEngineIter(
	t *testing.T,
	desc *roachpb.RangeDescriptor,
	eng storage.Engine,
	replicatedOnly bool,
	expectedKeys []interface{},
) {
	readWriter := eng.NewReadOnly(storage.StandardDurability)
	defer readWriter.Close()
	iter := NewReplicaEngineDataIterator(desc, readWriter, replicatedOnly)
	defer iter.Close()

	actualKeys := []interface{}{}
	var ok bool
	var err error
	for ok, err = iter.SeekStart(); ok && err == nil; ok, err = iter.Next() {
		hasPoint, hasRange := iter.HasPointAndRange()
		if hasPoint {
			key, err := iter.UnsafeKey()
			require.NoError(t, err)
			require.True(t, key.IsMVCCKey())
			mvccKey, err := key.ToMVCCKey()
			require.NoError(t, err)
			actualKeys = append(actualKeys, mvccKey.Clone())
		}
		if hasRange {
			bounds, err := iter.RangeBounds()
			require.NoError(t, err)
			for _, rk := range iter.RangeKeys() {
				ts, err := storage.DecodeMVCCTimestampSuffix(rk.Version)
				require.NoError(t, err)
				actualKeys = append(actualKeys, storage.MVCCRangeKey{
					StartKey:  bounds.Key.Clone(),
					EndKey:    bounds.EndKey.Clone(),
					Timestamp: ts,
				})
			}
		}
	}
	require.NoError(t, err)
	require.Equal(t, expectedKeys, actualKeys)
}

// TestReplicaDataIterator verifies correct operation of iterator if
// a range contains no data and never has.
func TestReplicaDataIteratorEmptyRange(t *testing.T) {
	defer leaktest.AfterTest(t)()

	eng := storage.NewDefaultInMemForTesting()
	defer eng.Close()

	desc := &roachpb.RangeDescriptor{
		RangeID:  12345,
		StartKey: roachpb.RKey("a"),
		EndKey:   roachpb.RKey("z"),
	}

	verifyRDReplicatedOnlyMVCCIter(t, desc, eng, []storage.MVCCKey{})
	verifyRDEngineIter(t, desc, eng, false, []interface{}{})
	verifyRDEngineIter(t, desc, eng, true, []interface{}{})
}

// TestReplicaDataIterator creates three ranges (a-b, b-c, c-d) and fills each
// with data, then verifies the contents for MVCC and Engine iterators, both
// replicated and unreplicated.
func TestReplicaDataIterator(t *testing.T) {
	defer leaktest.AfterTest(t)()

	eng := storage.NewDefaultInMemForTesting()
	defer eng.Close()

	descs := []roachpb.RangeDescriptor{
		{
			RangeID:  1,
			StartKey: roachpb.RKey("a"),
			EndKey:   roachpb.RKey("b"),
		},
		{
			RangeID:  2,
			StartKey: roachpb.RKey("b"),
			EndKey:   roachpb.RKey("c"),
		},
		{
			RangeID:  3,
			StartKey: roachpb.RKey("c"),
			EndKey:   roachpb.RKey("d"),
		},
	}

	// Create test cases with test data for each descriptor.
	testcases := make([]struct {
		desc                    roachpb.RangeDescriptor
		allKeys, replicatedKeys []interface{} // mixed MVCCKey and MVCCRangeKey
	}, len(descs))

	for i := range testcases {
		testcases[i].desc = descs[i]
		testcases[i].allKeys, testcases[i].replicatedKeys = createRangeData(t, eng, descs[i])
	}

	// Run tests.
	for _, tc := range testcases {
		t.Run(tc.desc.RSpan().String(), func(t *testing.T) {

			// Verify the replicated and unreplicated engine contents.
			verifyRDEngineIter(t, &tc.desc, eng, false, tc.allKeys)
			verifyRDEngineIter(t, &tc.desc, eng, true, tc.replicatedKeys)

			// Verify the replicated MVCC contents.
			//
			// TODO(erikgrinaker): This currently only supports MVCC point keys, so we
			// ignore MVCC range keys for now.
			var pointKeys []storage.MVCCKey
			for _, key := range tc.replicatedKeys {
				if pointKey, ok := key.(storage.MVCCKey); ok {
					pointKeys = append(pointKeys, pointKey)
				}
			}
			verifyRDReplicatedOnlyMVCCIter(t, &tc.desc, eng, pointKeys)
		})
	}
}

func checkOrdering(t *testing.T, ranges []KeyRange) {
	for i := 1; i < len(ranges); i++ {
		if ranges[i].Start.Compare(ranges[i-1].End) < 0 {
			t.Fatalf("ranges need to be ordered and non-overlapping, but %s > %s",
				ranges[i-1].End, ranges[i].Start)
		}
	}
}

// TestReplicaDataIteratorGlobalRangeKey creates three ranges {a-b, b-c, c-d}
// and writes an MVCC range key across the entire keyspace (replicated and
// unreplicated). It then verifies that the range key is properly truncated and
// filtered to the iterator's key ranges.
func TestReplicaDataIteratorGlobalRangeKey(t *testing.T) {
	defer leaktest.AfterTest(t)()

	// Set up a new engine and write a single range key across the entire span.
	eng := storage.NewDefaultInMemForTesting()
	defer eng.Close()

	require.NoError(t, eng.PutEngineRangeKey(keys.MinKey.Next(), keys.MaxKey, []byte{1}, []byte{}))

	// Use a snapshot for the iteration, because we need consistent
	// iterators.
	snapshot := eng.NewSnapshot()
	defer snapshot.Close()

	// Iterate over three range descriptors, both replicated and unreplicated.
	descs := []roachpb.RangeDescriptor{
		{
			RangeID:  1,
			StartKey: roachpb.RKey("a"),
			EndKey:   roachpb.RKey("b"),
		},
		{
			RangeID:  2,
			StartKey: roachpb.RKey("b"),
			EndKey:   roachpb.RKey("c"),
		},
		{
			RangeID:  3,
			StartKey: roachpb.RKey("c"),
			EndKey:   roachpb.RKey("d"),
		},
	}
	for _, desc := range descs {
		t.Run(desc.KeySpan().String(), func(t *testing.T) {
			// An iterator should see range keys spanning all relevant key ranges.
			testutils.RunTrueAndFalse(t, "replicatedOnly", func(t *testing.T, replicatedOnly bool) {
				rangeIter := NewReplicaEngineDataIterator(&desc, snapshot, replicatedOnly)
				defer rangeIter.Close()

				var expectedRanges []KeyRange
				if replicatedOnly {
					expectedRanges = MakeReplicatedKeyRanges(&desc)
				} else {
					expectedRanges = MakeAllKeyRanges(&desc)
				}

				var actualRanges []KeyRange
				var ok bool
				var err error
				for ok, err = rangeIter.SeekStart(); ok && err == nil; ok, err = rangeIter.Next() {
					bounds, err := rangeIter.RangeBounds()
					require.NoError(t, err)
					actualRanges = append(actualRanges, KeyRange{
						Start: bounds.Key.Clone(),
						End:   bounds.EndKey.Clone(),
					})
				}
				require.NoError(t, err)
				require.Equal(t, expectedRanges, actualRanges)
			})
		})
	}
}

func TestReplicaKeyRanges(t *testing.T) {
	defer leaktest.AfterTest(t)()

	desc := roachpb.RangeDescriptor{
		RangeID:  1,
		StartKey: roachpb.RKeyMin,
		EndKey:   roachpb.RKeyMax,
	}
	checkOrdering(t, MakeAllKeyRanges(&desc))
	checkOrdering(t, MakeReplicatedKeyRanges(&desc))
	checkOrdering(t, MakeReplicatedKeyRangesExceptLockTable(&desc))
	checkOrdering(t, MakeReplicatedKeyRangesExceptRangeID(&desc))
}

func BenchmarkReplicaEngineDataIterator(b *testing.B) {
	skip.UnderShort(b)
	for _, numRanges := range []int{1, 10, 100} {
		b.Run(fmt.Sprintf("ranges=%d", numRanges), func(b *testing.B) {
			for _, numKeysPerRange := range []int{1, 100, 10000} {
				b.Run(fmt.Sprintf("keysPerRange=%d", numKeysPerRange), func(b *testing.B) {
					for _, valueSize := range []int{32} {
						b.Run(fmt.Sprintf("valueSize=%d", valueSize), func(b *testing.B) {
							benchReplicaEngineDataIterator(b, numRanges, numKeysPerRange, valueSize)
						})
					}
				})
			}
		})
	}
}

func benchReplicaEngineDataIterator(b *testing.B, numRanges, numKeysPerRange, valueSize int) {
	ctx := context.Background()

	// Set up ranges.
	var descs []roachpb.RangeDescriptor
	for i := 1; i <= numRanges; i++ {
		desc := roachpb.RangeDescriptor{
			RangeID:  roachpb.RangeID(i),
			StartKey: append([]byte{'k'}, 0, 0, 0, 0),
			EndKey:   append([]byte{'k'}, 0, 0, 0, 0),
		}
		binary.BigEndian.PutUint32(desc.StartKey[1:], uint32(i))
		binary.BigEndian.PutUint32(desc.EndKey[1:], uint32(i+1))
		descs = append(descs, desc)
	}

	// Write data for ranges.
	eng, err := storage.Open(ctx,
		storage.Filesystem(b.TempDir()),
		storage.CacheSize(1e9),
		storage.Settings(cluster.MakeTestingClusterSettings()))
	require.NoError(b, err)
	defer eng.Close()

	batch := eng.NewBatch()
	defer batch.Close()

	rng, _ := randutil.NewTestRand()
	value := randutil.RandBytes(rng, valueSize)

	for _, desc := range descs {
		var keyBuf roachpb.Key
		keyRanges := MakeAllKeyRanges(&desc)
		for i := 0; i < numKeysPerRange; i++ {
			keyBuf = append(keyBuf[:0], keyRanges[i%len(keyRanges)].Start...)
			keyBuf = append(keyBuf, 0, 0, 0, 0)
			binary.BigEndian.PutUint32(keyBuf[len(keyBuf)-4:], uint32(i))
			if err := batch.PutEngineKey(storage.EngineKey{Key: keyBuf}, value); err != nil {
				require.NoError(b, err) // slow, so check err != nil first
			}
		}
	}
	require.NoError(b, batch.Commit(true /* sync */))
	require.NoError(b, eng.Flush())
	require.NoError(b, eng.Compact())

	snapshot := eng.NewSnapshot()
	defer snapshot.Close()

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		for _, desc := range descs {
			iter := NewReplicaEngineDataIterator(&desc, snapshot, false /* replicatedOnly */)
			defer iter.Close()
			var ok bool
			var err error
			for ok, err = iter.SeekStart(); ok && err == nil; ok, err = iter.Next() {
				_, _ = iter.UnsafeKey()
				_ = iter.UnsafeValue()
			}
			if err != nil {
				require.NoError(b, err)
			}
		}
	}
}
