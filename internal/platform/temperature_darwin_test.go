//go:build darwin

package platform

import (
	"testing"
	"unsafe"
)

func TestSMCReadABI(t *testing.T) {
	if unsafe.Sizeof(smcParameters{}) != 80 || unsafe.Offsetof(smcParameters{}.Info) != 28 || unsafe.Offsetof(smcParameters{}.Bytes) != 48 {
		t.Fatal("AppleSMC ABI mismatch")
	}
}
