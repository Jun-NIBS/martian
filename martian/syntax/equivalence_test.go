// Copyright (c) 2018 10X Genomics, Inc. All rights reserved.

package syntax

import (
	"testing"
)

func TestParsedEquivalence(t *testing.T) {
	if ast1, ast2 := testGood(t, fmtTestSrc), testGood(t, fmtTestSrc); ast1 != nil && ast2 != nil {
		if !ast1.EquivalentCall(ast2) {
			t.Errorf("Expected equivalent calls for identical source.")
		}
	}
}

func TestEquivalenceIgnoresComments(t *testing.T) {
	if ast1, ast2 := testGood(t, `
# File comment

# Stage comment
# This describes the stage.
stage SUM_SQUARES(
    in  float[] values,
    # sum comment
    out float   sum,
    src py      "stages/sum_squares",
)

pipeline SUM_SQUARE_PIPELINE(
    in  float[] values,
    out float   sum,
)
{
    call SUM_SQUARES(
        values = self.values,
    )
    return (
        sum = SUM_SQUARES.sum,
    )
}

call SUM_SQUARE_PIPELINE(
    values = [10.0, 2.0e1, 3.0e+1, 400.0e-1, 5e1, 6e+1, 700e-1],
)
`), testGood(t, `
stage SUM_SQUARES(
    in  float[] values,
    out float   sum,
    src py      "stages/sum_squares",
)

pipeline SUM_SQUARE_PIPELINE(
    in  float[] values,
    out float   sum,
)
{
    call SUM_SQUARES(
        values = self.values,
    )
    return (
        sum = SUM_SQUARES.sum,
    )
}

call SUM_SQUARE_PIPELINE(
    values = [
        10.0, 2.0e1, 3.0e+1, 400.0e-1, 5e1, 6e+1, 700e-1],
)
`); ast1 != nil && ast2 != nil {
		if !ast1.EquivalentCall(ast2) {
			t.Errorf("Expected equivalent calls for formatting changes.")
		}
	}
}

func TestEquivalenceAliasFailure(t *testing.T) {
	if ast1, ast2 := testGood(t, `
stage SUM_SQUARES(
    in  float[] values,
    out float   sum,
    src py      "stages/sum_squares",
)

pipeline SUM_SQUARE_PIPELINE(
    in  float[] values,
    out float   sum,
)
{
    call SUM_SQUARES as DO_COMPUTE(
        values = self.values,
    )
    return (
        sum = DO_COMPUTE.sum,
    )
}

call SUM_SQUARE_PIPELINE(
    values = [
        10.0, 2.0e1, 3.0e+1, 400.0e-1, 5e1, 6e+1, 700e-1,
    ],
)
`), testGood(t, `
stage SUM_SQUARES(
    in  float[] values,
    out float   sum,
    src py      "stages/sum_squares",
)

pipeline SUM_SQUARE_PIPELINE(
    in  float[] values,
    out float   sum,
)
{
    call SUM_SQUARES(
        values = self.values,
    )
    return (
        sum = SUM_SQUARES.sum,
    )
}

call SUM_SQUARE_PIPELINE(
    values = [
        10.0, 2.0e1, 3.0e+1, 400.0e-1, 5e1, 6e+1, 700e-1],
)
`); ast1 != nil && ast2 != nil {
		if ast1.EquivalentCall(ast2) {
			t.Errorf("Expected non-equivalence for calls with changed aliases.")
		}
	}
}

func TestEquivalenceOutNameChange(t *testing.T) {
	if ast1, ast2 := testGood(t, `
stage SUM_SQUARES(
    in  float[] values,
    out float   sum_of_squares,
    src py      "stages/sum_squares",
)

pipeline SUM_SQUARE_PIPELINE(
    in  float[] values,
    out float   sum,
)
{
    call SUM_SQUARES(
        values = self.values,
    )
    return (
        sum = SUM_SQUARES.sum_of_squares,
    )
}

call SUM_SQUARE_PIPELINE(
    values = [
        10.0, 2.0e1, 3.0e+1, 400.0e-1, 5e1, 6e+1, 700e-1,
    ],
)
`), testGood(t, `
stage SUM_SQUARES(
    in  float[] values,
    out float   sum,
    src py      "stages/sum_squares",
)

pipeline SUM_SQUARE_PIPELINE(
    in  float[] values,
    out float   sum,
)
{
    call SUM_SQUARES(
        values = self.values,
    )
    return (
        sum = SUM_SQUARES.sum,
    )
}

call SUM_SQUARE_PIPELINE(
    values = [
        10.0, 2.0e1, 3.0e+1, 400.0e-1, 5e1, 6e+1, 700e-1],
)
`); ast1 != nil && ast2 != nil {
		if ast1.EquivalentCall(ast2) {
			t.Errorf("Expected non-equivalence for calls with changed aliases.")
		}
	}
}

func TestEquivalenceInNameChange(t *testing.T) {
	if ast1, ast2 := testGood(t, `
stage SUM_SQUARES(
    in  float[] values_in,
    out float   sum,
    src py      "stages/sum_squares",
)

pipeline SUM_SQUARE_PIPELINE(
    in  float[] values,
    out float   sum,
)
{
    call SUM_SQUARES(
        values_in = self.values,
    )
    return (
        sum = SUM_SQUARES.sum,
    )
}

call SUM_SQUARE_PIPELINE(
    values = [
        10.0, 2.0e1, 3.0e+1, 400.0e-1, 5e1, 6e+1, 700e-1,
    ],
)
`), testGood(t, `
stage SUM_SQUARES(
    in  float[] values,
    out float   sum,
    src py      "stages/sum_squares",
)

pipeline SUM_SQUARE_PIPELINE(
    in  float[] values,
    out float   sum,
)
{
    call SUM_SQUARES(
        values = self.values,
    )
    return (
        sum = SUM_SQUARES.sum,
    )
}

call SUM_SQUARE_PIPELINE(
    values = [
        10.0, 2.0e1, 3.0e+1, 400.0e-1, 5e1, 6e+1, 700e-1],
)
`); ast1 != nil && ast2 != nil {
		if ast1.EquivalentCall(ast2) {
			t.Errorf("Expected non-equivalence for calls with changed aliases.")
		}
	}
}

func TestNumExpEquivalence(t *testing.T) {
	exp := ValExp{
		Kind:  KindInt,
		Value: parseInt([]byte("1")),
	}
	i1 := ValExp{
		Kind:  KindInt,
		Value: parseInt([]byte("1")),
	}
	i2 := ValExp{
		Kind:  KindInt,
		Value: parseInt([]byte("2")),
	}
	f1 := ValExp{
		Kind:  KindFloat,
		Value: parseFloat([]byte("1.0")),
	}
	f2 := ValExp{
		Kind:  KindFloat,
		Value: parseFloat([]byte("2.0")),
	}
	f3 := ValExp{
		Kind:  KindFloat,
		Value: parseFloat([]byte("1.1")),
	}
	if !exp.equal(&i1) {
		t.Errorf("Expected 1 == 1")
	}
	if exp.equal(&i2) {
		t.Errorf("Expected 1 != 2")
	}
	if !exp.equal(&f1) {
		t.Errorf("Expected 1 == 1.0")
	}
	if exp.equal(&f2) {
		t.Errorf("Expected 1 != 2.0")
	}
	if exp.equal(&f3) {
		t.Errorf("Expected 1 != 1.1")
	}
	if !f1.equal(&exp) {
		t.Errorf("Expected 1.0 == 1")
	}
	if f2.equal(&exp) {
		t.Errorf("Expected 2.0 != 1")
	}
	if f1.equal(&f3) {
		t.Errorf("Expected 1.0 != 1.1")
	}
	if !f1.equal(&f1) {
		t.Error("Expcted 1.0 == 1.0")
	}
}
