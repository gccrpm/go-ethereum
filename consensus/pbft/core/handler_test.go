package core

import (
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/consensus/pbft"
	"github.com/ethereum/go-ethereum/common"
)

// notice: the normal case have been tested in integration tests.
func TestHandleMsg(t *testing.T) {
	N := uint64(4)
	F := uint64(1)
	sys := NewTestSystemWithBackend(N, F)

	closer := sys.Run(true)
	defer closer()

	v0 := sys.backends[0]
	r0 := v0.engine.(*core)

	m, _ := Encode(&pbft.Subject{
		View: &pbft.View{
			Sequence: big.NewInt(0),
			Round:    big.NewInt(0),
		},
		Digest: common.StringToHash("1234567890"),
	})
	// with a matched payload. msgPreprepare should match with *pbft.Preprepare in normal case.
	msg := &message{
		Code:    msgPreprepare,
		Msg:     m,
		Address: v0.Address(),
	}

	_, val := v0.Validators().GetByAddress(v0.Address())
	if err := r0.handle(msg, val); err != errFailedDecodePreprepare {
		t.Error("message should decode failed")
	}

	m, _ = Encode(&pbft.Preprepare{
		View: &pbft.View{
			Sequence: big.NewInt(0),
			Round:    big.NewInt(0),
		},
		Proposal: makeBlock(1),
	})
	// with a unmatched payload. msgPrepare should match with *pbft.Subject in normal case.
	msg = &message{
		Code:    msgPrepare,
		Msg:     m,
		Address: v0.Address(),
	}

	_, val = v0.Validators().GetByAddress(v0.Address())
	if err := r0.handle(msg, val); err != errFailedDecodePrepare {
		t.Error("message should decode failed")
	}

	m, _ = Encode(&pbft.Preprepare{
		View: &pbft.View{
			Sequence: big.NewInt(0),
			Round:    big.NewInt(0),
		},
		Proposal: makeBlock(2),
	})
	// with a unmatched payload. pbft.MsgCommit should match with *pbft.Subject in normal case.
	msg = &message{
		Code:    msgCommit,
		Msg:     m,
		Address: v0.Address(),
	}

	_, val = v0.Validators().GetByAddress(v0.Address())
	if err := r0.handle(msg, val); err != errFailedDecodeCommit {
		t.Error("message should decode failed")
	}

	m, _ = Encode(&pbft.Preprepare{
		View: &pbft.View{
			Sequence: big.NewInt(0),
			Round:    big.NewInt(0),
		},
		Proposal: makeBlock(3),
	})
	// invalid message code. message code is not exists in list
	msg = &message{
		Code:    uint64(99),
		Msg:     m,
		Address: v0.Address(),
	}

	_, val = v0.Validators().GetByAddress(v0.Address())
	if err := r0.handle(msg, val); err != nil {
		t.Error("should not return failed message, but:", err)
	}

	// with malicious payload
	if err := r0.handleMsg([]byte{1}); err == nil {
		t.Error("message should decode failed..., but:", err)
	}
}
