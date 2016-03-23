package manager

import (
	"fmt"
	"github.com/stretchr/testify/assert"
	"io/ioutil"
	"testing"
)

func TestGetTotalReadCount(t *testing.T) {
	runPath := getTestFilePath("HTESTBCXX")
	numCycles := GetNumCycles(runPath)
	assert.Equal(t, numCycles, 125)
}

func TestCellRangerAlloc(t *testing.T) {
	mro := getTestFilePath("mro/test_cellranger.mro")
	mroPaths := []string{}
	var invocation Invocation
	if source, err := ioutil.ReadFile(mro); err != nil {
		assert.Fail(t, fmt.Sprintf("Could not read file: %s", mro))
	} else {
		invocation = InvocationFromMRO(string(source), mro, mroPaths)
	}
	alloc := GetAllocation("test_cellranger", invocation)
	assert.Equal(t, 6, alloc.inputSize)
	assert.Equal(t, 79, alloc.weightedSize)
}