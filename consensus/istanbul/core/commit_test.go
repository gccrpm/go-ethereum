// Copyright 2017 AMIS Technologies
// This file is part of the go-ethereum library.
//
// The go-ethereum library is free software: you can redistribute it and/or modify
// it under the terms of the GNU Lesser General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// The go-ethereum library is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
// GNU Lesser General Public License for more details.
//
// You should have received a copy of the GNU Lesser General Public License
// along with the go-ethereum library. If not, see <http://www.gnu.org/licenses/>.

package core

import (
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/consensus/istanbul"
	"github.com/ethereum/go-ethereum/consensus/istanbul/validator"
	"github.com/ethereum/go-ethereum/crypto"
)

func TestHandleCommit(t *testing.T) {
	N := uint64(4)
	F := uint64(1)

	proposal := newTestProposal()
	expectedSubject := &istanbul.Subject{
		View: &istanbul.View{
			Round:    big.NewInt(0),
			Sequence: proposal.Number(),
		},
		Digest: proposal.Hash(),
	}

	testCases := []struct {
		system      *testSystem
		expectedErr error
	}{
		{
			// normal case
			func() *testSystem {
				sys := NewTestSystemWithBackend(N, F)

				for i, backend := range sys.backends {
					c := backend.engine.(*core)
					c.Start(big.NewInt(0), common.Address{}, nil)
					if i == 0 {
						// replica 0 is primary
						c.state = StatePrepared
					}
				}
				return sys
			}(),
			nil,
		},
		{
			// future message
			func() *testSystem {
				sys := NewTestSystemWithBackend(N, F)

				for i, backend := range sys.backends {
					c := backend.engine.(*core)

					if i == 0 {
						c.Start(new(big.Int).Sub(expectedSubject.View.Sequence, common.Big1), common.Address{}, nil)
						c.state = StatePreprepared
					} else {
						c.Start(big.NewInt(2), common.Address{}, nil)
					}
				}
				return sys
			}(),
			errFutureMessage,
		},
		{
			// subject not match
			func() *testSystem {
				sys := NewTestSystemWithBackend(N, F)

				for i, backend := range sys.backends {
					c := backend.engine.(*core)

					if i == 0 {
						// replica 0 is primary
						c.Start(new(big.Int).Sub(expectedSubject.View.Sequence, common.Big1), common.Address{}, nil)
						c.state = StatePreprepared
					} else {
						c.Start(big.NewInt(0), common.Address{}, nil)
					}
				}
				return sys
			}(),
			errOldMessage,
		},
		{
			// less than 2F+1
			func() *testSystem {
				sys := NewTestSystemWithBackend(N, F)

				// save less than 2*F+1 replica
				sys.backends = sys.backends[2*int(F)+1:]

				for i, backend := range sys.backends {
					c := backend.engine.(*core)
					c.Start(new(big.Int).Sub(expectedSubject.View.Sequence, common.Big1), common.Address{}, nil)
					if i == 0 {
						// replica 0 is primary
						c.state = StatePrepared
					}
				}
				return sys
			}(),
			nil,
		},
		{
			// jump state
			func() *testSystem {
				sys := NewTestSystemWithBackend(N, F)

				for i, backend := range sys.backends {
					c := backend.engine.(*core)
					c.Start(new(big.Int).Sub(proposal.Number(), common.Big1), common.Address{}, nil)

					// only replica0 stays at StatePreprepared
					// other replicas are at StatePrepared
					if i != 0 {
						c.state = StatePrepared
					} else {
						c.state = StatePreprepared
					}
				}
				return sys
			}(),
			nil,
		},
		// TODO: double send message
	}

OUTER:
	for _, test := range testCases {
		test.system.Run(false)

		v0 := test.system.backends[0]
		r0 := v0.engine.(*core)

		for i, v := range test.system.backends {
			c := v.engine.(*core)
			validator := c.valSet.GetByIndex(uint64(i))
			c.current = newTestRoundState(
				&istanbul.View{
					Round:    big.NewInt(0),
					Sequence: c.current.Sequence(),
				},
				c.valSet,
			)
			m, _ := Encode(c.current.Subject())
			if err := r0.handleCommit(&message{
				Code:         msgPrepare,
				Msg:          m,
				Address:      validator.Address(),
				Signature:    []byte{},
				ProposalSeal: validator.Address().Bytes(), // small hack
			}, validator); err != nil {
				if err != test.expectedErr {
					t.Error("unexpected error: ", err)
				}
				continue OUTER
			}
		}

		// prepared is normal case
		if r0.state != StateCommitted {
			// There are not enough commit messages in core
			if r0.state != StatePrepared {
				t.Error("state should be prepared")
			}
			if r0.current.Commits.Size() > 2*r0.valSet.F() {
				t.Error("commit messages size should less than ", 2*r0.valSet.F()+1)
			}

			continue
		}

		// core should have 2F+1 prepare messages
		if r0.current.Commits.Size() <= 2*r0.valSet.F() {
			t.Error("commit messages size should greater than 2F+1, size:", r0.current.Commits.Size())
		}

		// check signatures large than 2F+1
		signedCount := 0
		signers := make([]common.Address, len(v0.committedSeals[0])/common.AddressLength)
		for i := 0; i < len(signers); i++ {
			copy(signers[i][:], v0.committedSeals[0][i*common.AddressLength:])
		}
		for _, validator := range r0.valSet.List() {
			for _, signer := range signers {
				if validator.Address() == signer {
					signedCount++
					break
				}
			}
		}
		if signedCount <= 2*r0.valSet.F() {
			t.Errorf("expected signed count larger than:%v, but got:%v", 2*r0.valSet.F(), signedCount)
		}
	}
}

// round is not checked for now
func TestVerifyCommit(t *testing.T) {
	// for log purpose
	privateKey, _ := crypto.GenerateKey()
	peer := validator.New(getPublicKeyAddress(privateKey))
	valSet := validator.NewSet([]common.Address{peer.Address()}, istanbul.RoundRobin)

	sys := NewTestSystemWithBackend(uint64(1), uint64(0))

	testCases := []struct {
		expected error

		commit     *istanbul.Subject
		roundState *roundState
	}{
		{
			// normal case
			expected: nil,
			commit: &istanbul.Subject{
				View:   &istanbul.View{Round: big.NewInt(0), Sequence: big.NewInt(0)},
				Digest: newTestProposal().Hash(),
			},
			roundState: newTestRoundState(
				&istanbul.View{Round: big.NewInt(0), Sequence: big.NewInt(0)},
				valSet,
			),
		},
		{
			// old message
			expected: errInconsistentSubject,
			commit: &istanbul.Subject{
				View:   &istanbul.View{Round: big.NewInt(0), Sequence: big.NewInt(0)},
				Digest: newTestProposal().Hash(),
			},
			roundState: newTestRoundState(
				&istanbul.View{Round: big.NewInt(1), Sequence: big.NewInt(1)},
				valSet,
			),
		},
		{
			// different digest
			expected: errInconsistentSubject,
			commit: &istanbul.Subject{
				View:   &istanbul.View{Round: big.NewInt(0), Sequence: big.NewInt(0)},
				Digest: common.StringToHash("1234567890"),
			},
			roundState: newTestRoundState(
				&istanbul.View{Round: big.NewInt(1), Sequence: big.NewInt(1)},
				valSet,
			),
		},
		{
			// malicious package(lack of sequence)
			expected: errInconsistentSubject,
			commit: &istanbul.Subject{
				View:   &istanbul.View{Round: big.NewInt(0), Sequence: nil},
				Digest: newTestProposal().Hash(),
			},
			roundState: newTestRoundState(
				&istanbul.View{Round: big.NewInt(1), Sequence: big.NewInt(1)},
				valSet,
			),
		},
		{
			// wrong prepare message with same sequence but different round
			expected: errInconsistentSubject,
			commit: &istanbul.Subject{
				View:   &istanbul.View{Round: big.NewInt(1), Sequence: big.NewInt(0)},
				Digest: newTestProposal().Hash(),
			},
			roundState: newTestRoundState(
				&istanbul.View{Round: big.NewInt(0), Sequence: big.NewInt(0)},
				valSet,
			),
		},
		{
			// wrong prepare message with same round but different sequence
			expected: errInconsistentSubject,
			commit: &istanbul.Subject{
				View:   &istanbul.View{Round: big.NewInt(0), Sequence: big.NewInt(1)},
				Digest: newTestProposal().Hash(),
			},
			roundState: newTestRoundState(
				&istanbul.View{Round: big.NewInt(0), Sequence: big.NewInt(0)},
				valSet,
			),
		},
	}
	for i, test := range testCases {
		c := sys.backends[0].engine.(*core)
		c.current = test.roundState

		if err := c.verifyCommit(test.commit, peer); err != nil {
			if err != test.expected {
				t.Errorf("Results %d are different, err:%v", i, err)
			}
		}
	}
}
