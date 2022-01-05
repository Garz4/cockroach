// Code generated by execgen; DO NOT EDIT.
// Copyright 2018 The Cockroach Authors.
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.txt.
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0, included in the file
// licenses/APL.txt.

package colexecagg

import (
	"unsafe"

	"github.com/cockroachdb/apd/v2"
	"github.com/cockroachdb/cockroach/pkg/col/coldata"
	"github.com/cockroachdb/cockroach/pkg/sql/colexec/execgen"
	"github.com/cockroachdb/cockroach/pkg/sql/colexecerror"
	"github.com/cockroachdb/cockroach/pkg/sql/colmem"
	"github.com/cockroachdb/cockroach/pkg/sql/sem/tree"
	"github.com/cockroachdb/cockroach/pkg/sql/types"
	"github.com/cockroachdb/cockroach/pkg/util/duration"
	"github.com/cockroachdb/errors"
)

// Workaround for bazel auto-generated code. goimports does not automatically
// pick up the right packages when run within the bazel sandbox.
var (
	_ tree.AggType
	_ apd.Context
	_ duration.Duration
)

func newSumHashAggAlloc(
	allocator *colmem.Allocator, t *types.T, allocSize int64,
) (aggregateFuncAlloc, error) {
	allocBase := aggAllocBase{allocator: allocator, allocSize: allocSize}
	switch t.Family() {
	case types.IntFamily:
		switch t.Width() {
		case 16:
			return &sumInt16HashAggAlloc{aggAllocBase: allocBase}, nil
		case 32:
			return &sumInt32HashAggAlloc{aggAllocBase: allocBase}, nil
		case -1:
		default:
			return &sumInt64HashAggAlloc{aggAllocBase: allocBase}, nil
		}
	case types.DecimalFamily:
		switch t.Width() {
		case -1:
		default:
			return &sumDecimalHashAggAlloc{aggAllocBase: allocBase}, nil
		}
	case types.FloatFamily:
		switch t.Width() {
		case -1:
		default:
			return &sumFloat64HashAggAlloc{aggAllocBase: allocBase}, nil
		}
	case types.IntervalFamily:
		switch t.Width() {
		case -1:
		default:
			return &sumIntervalHashAggAlloc{aggAllocBase: allocBase}, nil
		}
	}
	return nil, errors.Errorf("unsupported sum agg type %s", t.Name())
}

type sumInt16HashAgg struct {
	unorderedAggregateFuncBase
	// curAgg holds the running total, so we can index into the slice once per
	// group, instead of on each iteration.
	curAgg apd.Decimal
	// col points to the output vector we are updating.
	col coldata.Decimals
	// numNonNull tracks the number of non-null values we have seen for the group
	// that is currently being aggregated.
	numNonNull     uint64
	overloadHelper execgen.OverloadHelper
}

var _ AggregateFunc = &sumInt16HashAgg{}

func (a *sumInt16HashAgg) SetOutput(vec coldata.Vec) {
	a.unorderedAggregateFuncBase.SetOutput(vec)
	a.col = vec.Decimal()
}

func (a *sumInt16HashAgg) Compute(
	vecs []coldata.Vec, inputIdxs []uint32, startIdx, endIdx int, sel []int,
) {
	// In order to inline the templated code of overloads, we need to have a
	// "_overloadHelper" local variable of type "overloadHelper".
	_overloadHelper := a.overloadHelper
	oldCurAggSize := tree.SizeOfDecimal(&a.curAgg)
	vec := vecs[inputIdxs[0]]
	col, nulls := vec.Int16(), vec.Nulls()
	a.allocator.PerformOperation([]coldata.Vec{a.vec}, func() {
		{
			sel = sel[startIdx:endIdx]
			if nulls.MaybeHasNulls() {
				for _, i := range sel {

					var isNull bool
					isNull = nulls.NullAt(i)
					if !isNull {
						v := col.Get(i)

						{

							tmpDec := &_overloadHelper.TmpDec1
							tmpDec.SetInt64(int64(v))
							if _, err := tree.ExactCtx.Add(&a.curAgg, &a.curAgg, tmpDec); err != nil {
								colexecerror.ExpectedError(err)
							}
						}

						a.numNonNull++
					}
				}
			} else {
				for _, i := range sel {

					var isNull bool
					isNull = false
					if !isNull {
						v := col.Get(i)

						{

							tmpDec := &_overloadHelper.TmpDec1
							tmpDec.SetInt64(int64(v))
							if _, err := tree.ExactCtx.Add(&a.curAgg, &a.curAgg, tmpDec); err != nil {
								colexecerror.ExpectedError(err)
							}
						}

						a.numNonNull++
					}
				}
			}
		}
	},
	)
	newCurAggSize := tree.SizeOfDecimal(&a.curAgg)
	if newCurAggSize != oldCurAggSize {
		a.allocator.AdjustMemoryUsage(int64(newCurAggSize - oldCurAggSize))
	}
}

func (a *sumInt16HashAgg) Flush(outputIdx int) {
	// The aggregation is finished. Flush the last value. If we haven't found
	// any non-nulls for this group so far, the output for this group should be
	// null.
	if a.numNonNull == 0 {
		a.nulls.SetNull(outputIdx)
	} else {
		a.col[outputIdx] = a.curAgg
	}
}

func (a *sumInt16HashAgg) Reset() {
	a.curAgg = zeroDecimalValue
	a.numNonNull = 0
}

type sumInt16HashAggAlloc struct {
	aggAllocBase
	aggFuncs []sumInt16HashAgg
}

var _ aggregateFuncAlloc = &sumInt16HashAggAlloc{}

const sizeOfSumInt16HashAgg = int64(unsafe.Sizeof(sumInt16HashAgg{}))
const sumInt16HashAggSliceOverhead = int64(unsafe.Sizeof([]sumInt16HashAgg{}))

func (a *sumInt16HashAggAlloc) newAggFunc() AggregateFunc {
	if len(a.aggFuncs) == 0 {
		a.allocator.AdjustMemoryUsage(sumInt16HashAggSliceOverhead + sizeOfSumInt16HashAgg*a.allocSize)
		a.aggFuncs = make([]sumInt16HashAgg, a.allocSize)
	}
	f := &a.aggFuncs[0]
	f.allocator = a.allocator
	a.aggFuncs = a.aggFuncs[1:]
	return f
}

type sumInt32HashAgg struct {
	unorderedAggregateFuncBase
	// curAgg holds the running total, so we can index into the slice once per
	// group, instead of on each iteration.
	curAgg apd.Decimal
	// col points to the output vector we are updating.
	col coldata.Decimals
	// numNonNull tracks the number of non-null values we have seen for the group
	// that is currently being aggregated.
	numNonNull     uint64
	overloadHelper execgen.OverloadHelper
}

var _ AggregateFunc = &sumInt32HashAgg{}

func (a *sumInt32HashAgg) SetOutput(vec coldata.Vec) {
	a.unorderedAggregateFuncBase.SetOutput(vec)
	a.col = vec.Decimal()
}

func (a *sumInt32HashAgg) Compute(
	vecs []coldata.Vec, inputIdxs []uint32, startIdx, endIdx int, sel []int,
) {
	// In order to inline the templated code of overloads, we need to have a
	// "_overloadHelper" local variable of type "overloadHelper".
	_overloadHelper := a.overloadHelper
	oldCurAggSize := tree.SizeOfDecimal(&a.curAgg)
	vec := vecs[inputIdxs[0]]
	col, nulls := vec.Int32(), vec.Nulls()
	a.allocator.PerformOperation([]coldata.Vec{a.vec}, func() {
		{
			sel = sel[startIdx:endIdx]
			if nulls.MaybeHasNulls() {
				for _, i := range sel {

					var isNull bool
					isNull = nulls.NullAt(i)
					if !isNull {
						v := col.Get(i)

						{

							tmpDec := &_overloadHelper.TmpDec1
							tmpDec.SetInt64(int64(v))
							if _, err := tree.ExactCtx.Add(&a.curAgg, &a.curAgg, tmpDec); err != nil {
								colexecerror.ExpectedError(err)
							}
						}

						a.numNonNull++
					}
				}
			} else {
				for _, i := range sel {

					var isNull bool
					isNull = false
					if !isNull {
						v := col.Get(i)

						{

							tmpDec := &_overloadHelper.TmpDec1
							tmpDec.SetInt64(int64(v))
							if _, err := tree.ExactCtx.Add(&a.curAgg, &a.curAgg, tmpDec); err != nil {
								colexecerror.ExpectedError(err)
							}
						}

						a.numNonNull++
					}
				}
			}
		}
	},
	)
	newCurAggSize := tree.SizeOfDecimal(&a.curAgg)
	if newCurAggSize != oldCurAggSize {
		a.allocator.AdjustMemoryUsage(int64(newCurAggSize - oldCurAggSize))
	}
}

func (a *sumInt32HashAgg) Flush(outputIdx int) {
	// The aggregation is finished. Flush the last value. If we haven't found
	// any non-nulls for this group so far, the output for this group should be
	// null.
	if a.numNonNull == 0 {
		a.nulls.SetNull(outputIdx)
	} else {
		a.col[outputIdx] = a.curAgg
	}
}

func (a *sumInt32HashAgg) Reset() {
	a.curAgg = zeroDecimalValue
	a.numNonNull = 0
}

type sumInt32HashAggAlloc struct {
	aggAllocBase
	aggFuncs []sumInt32HashAgg
}

var _ aggregateFuncAlloc = &sumInt32HashAggAlloc{}

const sizeOfSumInt32HashAgg = int64(unsafe.Sizeof(sumInt32HashAgg{}))
const sumInt32HashAggSliceOverhead = int64(unsafe.Sizeof([]sumInt32HashAgg{}))

func (a *sumInt32HashAggAlloc) newAggFunc() AggregateFunc {
	if len(a.aggFuncs) == 0 {
		a.allocator.AdjustMemoryUsage(sumInt32HashAggSliceOverhead + sizeOfSumInt32HashAgg*a.allocSize)
		a.aggFuncs = make([]sumInt32HashAgg, a.allocSize)
	}
	f := &a.aggFuncs[0]
	f.allocator = a.allocator
	a.aggFuncs = a.aggFuncs[1:]
	return f
}

type sumInt64HashAgg struct {
	unorderedAggregateFuncBase
	// curAgg holds the running total, so we can index into the slice once per
	// group, instead of on each iteration.
	curAgg apd.Decimal
	// col points to the output vector we are updating.
	col coldata.Decimals
	// numNonNull tracks the number of non-null values we have seen for the group
	// that is currently being aggregated.
	numNonNull     uint64
	overloadHelper execgen.OverloadHelper
}

var _ AggregateFunc = &sumInt64HashAgg{}

func (a *sumInt64HashAgg) SetOutput(vec coldata.Vec) {
	a.unorderedAggregateFuncBase.SetOutput(vec)
	a.col = vec.Decimal()
}

func (a *sumInt64HashAgg) Compute(
	vecs []coldata.Vec, inputIdxs []uint32, startIdx, endIdx int, sel []int,
) {
	// In order to inline the templated code of overloads, we need to have a
	// "_overloadHelper" local variable of type "overloadHelper".
	_overloadHelper := a.overloadHelper
	oldCurAggSize := tree.SizeOfDecimal(&a.curAgg)
	vec := vecs[inputIdxs[0]]
	col, nulls := vec.Int64(), vec.Nulls()
	a.allocator.PerformOperation([]coldata.Vec{a.vec}, func() {
		{
			sel = sel[startIdx:endIdx]
			if nulls.MaybeHasNulls() {
				for _, i := range sel {

					var isNull bool
					isNull = nulls.NullAt(i)
					if !isNull {
						v := col.Get(i)

						{

							tmpDec := &_overloadHelper.TmpDec1
							tmpDec.SetInt64(int64(v))
							if _, err := tree.ExactCtx.Add(&a.curAgg, &a.curAgg, tmpDec); err != nil {
								colexecerror.ExpectedError(err)
							}
						}

						a.numNonNull++
					}
				}
			} else {
				for _, i := range sel {

					var isNull bool
					isNull = false
					if !isNull {
						v := col.Get(i)

						{

							tmpDec := &_overloadHelper.TmpDec1
							tmpDec.SetInt64(int64(v))
							if _, err := tree.ExactCtx.Add(&a.curAgg, &a.curAgg, tmpDec); err != nil {
								colexecerror.ExpectedError(err)
							}
						}

						a.numNonNull++
					}
				}
			}
		}
	},
	)
	newCurAggSize := tree.SizeOfDecimal(&a.curAgg)
	if newCurAggSize != oldCurAggSize {
		a.allocator.AdjustMemoryUsage(int64(newCurAggSize - oldCurAggSize))
	}
}

func (a *sumInt64HashAgg) Flush(outputIdx int) {
	// The aggregation is finished. Flush the last value. If we haven't found
	// any non-nulls for this group so far, the output for this group should be
	// null.
	if a.numNonNull == 0 {
		a.nulls.SetNull(outputIdx)
	} else {
		a.col[outputIdx] = a.curAgg
	}
}

func (a *sumInt64HashAgg) Reset() {
	a.curAgg = zeroDecimalValue
	a.numNonNull = 0
}

type sumInt64HashAggAlloc struct {
	aggAllocBase
	aggFuncs []sumInt64HashAgg
}

var _ aggregateFuncAlloc = &sumInt64HashAggAlloc{}

const sizeOfSumInt64HashAgg = int64(unsafe.Sizeof(sumInt64HashAgg{}))
const sumInt64HashAggSliceOverhead = int64(unsafe.Sizeof([]sumInt64HashAgg{}))

func (a *sumInt64HashAggAlloc) newAggFunc() AggregateFunc {
	if len(a.aggFuncs) == 0 {
		a.allocator.AdjustMemoryUsage(sumInt64HashAggSliceOverhead + sizeOfSumInt64HashAgg*a.allocSize)
		a.aggFuncs = make([]sumInt64HashAgg, a.allocSize)
	}
	f := &a.aggFuncs[0]
	f.allocator = a.allocator
	a.aggFuncs = a.aggFuncs[1:]
	return f
}

type sumDecimalHashAgg struct {
	unorderedAggregateFuncBase
	// curAgg holds the running total, so we can index into the slice once per
	// group, instead of on each iteration.
	curAgg apd.Decimal
	// col points to the output vector we are updating.
	col coldata.Decimals
	// numNonNull tracks the number of non-null values we have seen for the group
	// that is currently being aggregated.
	numNonNull uint64
}

var _ AggregateFunc = &sumDecimalHashAgg{}

func (a *sumDecimalHashAgg) SetOutput(vec coldata.Vec) {
	a.unorderedAggregateFuncBase.SetOutput(vec)
	a.col = vec.Decimal()
}

func (a *sumDecimalHashAgg) Compute(
	vecs []coldata.Vec, inputIdxs []uint32, startIdx, endIdx int, sel []int,
) {
	oldCurAggSize := tree.SizeOfDecimal(&a.curAgg)
	vec := vecs[inputIdxs[0]]
	col, nulls := vec.Decimal(), vec.Nulls()
	a.allocator.PerformOperation([]coldata.Vec{a.vec}, func() {
		{
			sel = sel[startIdx:endIdx]
			if nulls.MaybeHasNulls() {
				for _, i := range sel {

					var isNull bool
					isNull = nulls.NullAt(i)
					if !isNull {
						v := col.Get(i)

						{

							_, err := tree.ExactCtx.Add(&a.curAgg, &a.curAgg, &v)
							if err != nil {
								colexecerror.ExpectedError(err)
							}
						}

						a.numNonNull++
					}
				}
			} else {
				for _, i := range sel {

					var isNull bool
					isNull = false
					if !isNull {
						v := col.Get(i)

						{

							_, err := tree.ExactCtx.Add(&a.curAgg, &a.curAgg, &v)
							if err != nil {
								colexecerror.ExpectedError(err)
							}
						}

						a.numNonNull++
					}
				}
			}
		}
	},
	)
	newCurAggSize := tree.SizeOfDecimal(&a.curAgg)
	if newCurAggSize != oldCurAggSize {
		a.allocator.AdjustMemoryUsage(int64(newCurAggSize - oldCurAggSize))
	}
}

func (a *sumDecimalHashAgg) Flush(outputIdx int) {
	// The aggregation is finished. Flush the last value. If we haven't found
	// any non-nulls for this group so far, the output for this group should be
	// null.
	if a.numNonNull == 0 {
		a.nulls.SetNull(outputIdx)
	} else {
		a.col[outputIdx] = a.curAgg
	}
}

func (a *sumDecimalHashAgg) Reset() {
	a.curAgg = zeroDecimalValue
	a.numNonNull = 0
}

type sumDecimalHashAggAlloc struct {
	aggAllocBase
	aggFuncs []sumDecimalHashAgg
}

var _ aggregateFuncAlloc = &sumDecimalHashAggAlloc{}

const sizeOfSumDecimalHashAgg = int64(unsafe.Sizeof(sumDecimalHashAgg{}))
const sumDecimalHashAggSliceOverhead = int64(unsafe.Sizeof([]sumDecimalHashAgg{}))

func (a *sumDecimalHashAggAlloc) newAggFunc() AggregateFunc {
	if len(a.aggFuncs) == 0 {
		a.allocator.AdjustMemoryUsage(sumDecimalHashAggSliceOverhead + sizeOfSumDecimalHashAgg*a.allocSize)
		a.aggFuncs = make([]sumDecimalHashAgg, a.allocSize)
	}
	f := &a.aggFuncs[0]
	f.allocator = a.allocator
	a.aggFuncs = a.aggFuncs[1:]
	return f
}

type sumFloat64HashAgg struct {
	unorderedAggregateFuncBase
	// curAgg holds the running total, so we can index into the slice once per
	// group, instead of on each iteration.
	curAgg float64
	// col points to the output vector we are updating.
	col coldata.Float64s
	// numNonNull tracks the number of non-null values we have seen for the group
	// that is currently being aggregated.
	numNonNull uint64
}

var _ AggregateFunc = &sumFloat64HashAgg{}

func (a *sumFloat64HashAgg) SetOutput(vec coldata.Vec) {
	a.unorderedAggregateFuncBase.SetOutput(vec)
	a.col = vec.Float64()
}

func (a *sumFloat64HashAgg) Compute(
	vecs []coldata.Vec, inputIdxs []uint32, startIdx, endIdx int, sel []int,
) {
	var oldCurAggSize uintptr
	vec := vecs[inputIdxs[0]]
	col, nulls := vec.Float64(), vec.Nulls()
	a.allocator.PerformOperation([]coldata.Vec{a.vec}, func() {
		{
			sel = sel[startIdx:endIdx]
			if nulls.MaybeHasNulls() {
				for _, i := range sel {

					var isNull bool
					isNull = nulls.NullAt(i)
					if !isNull {
						v := col.Get(i)

						{

							a.curAgg = float64(a.curAgg) + float64(v)
						}

						a.numNonNull++
					}
				}
			} else {
				for _, i := range sel {

					var isNull bool
					isNull = false
					if !isNull {
						v := col.Get(i)

						{

							a.curAgg = float64(a.curAgg) + float64(v)
						}

						a.numNonNull++
					}
				}
			}
		}
	},
	)
	var newCurAggSize uintptr
	if newCurAggSize != oldCurAggSize {
		a.allocator.AdjustMemoryUsage(int64(newCurAggSize - oldCurAggSize))
	}
}

func (a *sumFloat64HashAgg) Flush(outputIdx int) {
	// The aggregation is finished. Flush the last value. If we haven't found
	// any non-nulls for this group so far, the output for this group should be
	// null.
	if a.numNonNull == 0 {
		a.nulls.SetNull(outputIdx)
	} else {
		a.col[outputIdx] = a.curAgg
	}
}

func (a *sumFloat64HashAgg) Reset() {
	a.curAgg = zeroFloat64Value
	a.numNonNull = 0
}

type sumFloat64HashAggAlloc struct {
	aggAllocBase
	aggFuncs []sumFloat64HashAgg
}

var _ aggregateFuncAlloc = &sumFloat64HashAggAlloc{}

const sizeOfSumFloat64HashAgg = int64(unsafe.Sizeof(sumFloat64HashAgg{}))
const sumFloat64HashAggSliceOverhead = int64(unsafe.Sizeof([]sumFloat64HashAgg{}))

func (a *sumFloat64HashAggAlloc) newAggFunc() AggregateFunc {
	if len(a.aggFuncs) == 0 {
		a.allocator.AdjustMemoryUsage(sumFloat64HashAggSliceOverhead + sizeOfSumFloat64HashAgg*a.allocSize)
		a.aggFuncs = make([]sumFloat64HashAgg, a.allocSize)
	}
	f := &a.aggFuncs[0]
	f.allocator = a.allocator
	a.aggFuncs = a.aggFuncs[1:]
	return f
}

type sumIntervalHashAgg struct {
	unorderedAggregateFuncBase
	// curAgg holds the running total, so we can index into the slice once per
	// group, instead of on each iteration.
	curAgg duration.Duration
	// col points to the output vector we are updating.
	col coldata.Durations
	// numNonNull tracks the number of non-null values we have seen for the group
	// that is currently being aggregated.
	numNonNull uint64
}

var _ AggregateFunc = &sumIntervalHashAgg{}

func (a *sumIntervalHashAgg) SetOutput(vec coldata.Vec) {
	a.unorderedAggregateFuncBase.SetOutput(vec)
	a.col = vec.Interval()
}

func (a *sumIntervalHashAgg) Compute(
	vecs []coldata.Vec, inputIdxs []uint32, startIdx, endIdx int, sel []int,
) {
	var oldCurAggSize uintptr
	vec := vecs[inputIdxs[0]]
	col, nulls := vec.Interval(), vec.Nulls()
	a.allocator.PerformOperation([]coldata.Vec{a.vec}, func() {
		{
			sel = sel[startIdx:endIdx]
			if nulls.MaybeHasNulls() {
				for _, i := range sel {

					var isNull bool
					isNull = nulls.NullAt(i)
					if !isNull {
						v := col.Get(i)
						a.curAgg = a.curAgg.Add(v)
						a.numNonNull++
					}
				}
			} else {
				for _, i := range sel {

					var isNull bool
					isNull = false
					if !isNull {
						v := col.Get(i)
						a.curAgg = a.curAgg.Add(v)
						a.numNonNull++
					}
				}
			}
		}
	},
	)
	var newCurAggSize uintptr
	if newCurAggSize != oldCurAggSize {
		a.allocator.AdjustMemoryUsage(int64(newCurAggSize - oldCurAggSize))
	}
}

func (a *sumIntervalHashAgg) Flush(outputIdx int) {
	// The aggregation is finished. Flush the last value. If we haven't found
	// any non-nulls for this group so far, the output for this group should be
	// null.
	if a.numNonNull == 0 {
		a.nulls.SetNull(outputIdx)
	} else {
		a.col[outputIdx] = a.curAgg
	}
}

func (a *sumIntervalHashAgg) Reset() {
	a.curAgg = zeroIntervalValue
	a.numNonNull = 0
}

type sumIntervalHashAggAlloc struct {
	aggAllocBase
	aggFuncs []sumIntervalHashAgg
}

var _ aggregateFuncAlloc = &sumIntervalHashAggAlloc{}

const sizeOfSumIntervalHashAgg = int64(unsafe.Sizeof(sumIntervalHashAgg{}))
const sumIntervalHashAggSliceOverhead = int64(unsafe.Sizeof([]sumIntervalHashAgg{}))

func (a *sumIntervalHashAggAlloc) newAggFunc() AggregateFunc {
	if len(a.aggFuncs) == 0 {
		a.allocator.AdjustMemoryUsage(sumIntervalHashAggSliceOverhead + sizeOfSumIntervalHashAgg*a.allocSize)
		a.aggFuncs = make([]sumIntervalHashAgg, a.allocSize)
	}
	f := &a.aggFuncs[0]
	f.allocator = a.allocator
	a.aggFuncs = a.aggFuncs[1:]
	return f
}
