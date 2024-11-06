package systemcontracts

import (
	"strings"
	"testing"
)

// Tests that DAO-fork enabled clients can properly filter out fork-commencing
// blocks based on their extradata fields.
func TestDecodeContractCode(t *testing.T) {

	println(strings.TrimPrefix("0x1234", "0x"))
	println(strings.TrimPrefix("1234", "0x"))

	//if _, err := hex.DecodeString(strings.TrimPrefix(CongressTestNetValidatorContractByteCode, "0x")); err != nil {
	//	panic(err)
	//}
}
