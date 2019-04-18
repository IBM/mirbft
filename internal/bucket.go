/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: Apache-2.0
*/

package internal

import (
	"github.com/IBM/mirbft/consumer"
	pb "github.com/IBM/mirbft/mirbftpb"
)

type BucketID uint64
type SeqNo uint64
type NodeID uint64

type Bucket struct {
	EpochConfig *EpochConfig

	Leader NodeID

	// Sequences are the current active sequence numbers in this bucket
	Sequences map[SeqNo]*Sequence

	NextAssigned   SeqNo
	NextPreprepare SeqNo
	NextPrepare    SeqNo
	NextCommit     SeqNo

	// The variables below are only set if this Bucket is led locally
	Queue     [][]byte
	SizeBytes int
	Pending   [][][]byte
}

func NewBucket(config *EpochConfig, bucketID BucketID) *Bucket {
	sequences := map[SeqNo]*Sequence{}
	for i := config.LowWatermark; i <= config.HighWatermark; i++ {
		sequences[i] = NewSequence(config, i, bucketID)
	}

	return &Bucket{
		Leader:         config.Buckets[bucketID],
		EpochConfig:    config,
		Sequences:      sequences,
		NextAssigned:   config.LowWatermark,
		NextPreprepare: config.LowWatermark,
		NextPrepare:    config.LowWatermark,
		NextCommit:     config.LowWatermark,
	}
}

// TODO, update the Next* vars as appropriate

func (b *Bucket) IAmLeader() bool {
	return b.Leader == NodeID(b.EpochConfig.MyConfig.ID)
}

func (b *Bucket) Propose(data []byte) *consumer.Actions {
	if !b.IAmLeader() {
		panic("I cannot propose data in a bucket for which  I'm not the leader")
	}

	b.Queue = append(b.Queue, data)
	b.SizeBytes += len(data)
	if b.SizeBytes < b.EpochConfig.MyConfig.BatchParameters.CutSizeBytes {
		return &consumer.Actions{}
	}

	b.Pending = append(b.Pending, b.Queue)
	b.Queue = nil

	if b.NextAssigned > b.EpochConfig.HighWatermark {
		return &consumer.Actions{}
	}

	result := &consumer.Actions{
		Broadcast: []*pb.Msg{
			{
				Type: &pb.Msg_Preprepare{
					Preprepare: &pb.Preprepare{
						Epoch: b.EpochConfig.Number,
						SeqNo: uint64(b.NextAssigned),
						Batch: b.Pending[0],
					},
				},
			},
		},
	}
	b.NextAssigned++
	b.Pending = b.Pending[1:]

	return result
}

func (b *Bucket) ApplyPreprepare(seqNo SeqNo, batch [][]byte) *consumer.Actions {
	return b.Sequences[seqNo].ApplyPreprepare(batch)
}

func (b *Bucket) ApplyDigestResult(seqNo SeqNo, digest []byte) *consumer.Actions {
	s := b.Sequences[seqNo]
	actions := s.ApplyDigestResult(digest)
	if b.IAmLeader() {
		// We are the leader, no need to check ourselves for byzantine behavior
		// And no need to send the resulting prepare
		_ = s.ApplyValidateResult(true)
		return s.ApplyPrepare(b.Leader, digest)
	}
	return actions
}

func (b *Bucket) ApplyValidateResult(seqNo SeqNo, valid bool) *consumer.Actions {
	s := b.Sequences[seqNo]
	actions := s.ApplyValidateResult(valid)
	if !b.IAmLeader() {
		// We are not the leader, so let's apply a virtual prepare from
		// the leader that will not be sent, as there is no need to prepare
		actions.Append(s.ApplyPrepare(b.Leader, s.Digest))
	}
	return actions
}

func (b *Bucket) ApplyPrepare(source NodeID, seqNo SeqNo, digest []byte) *consumer.Actions {
	return b.Sequences[seqNo].ApplyPrepare(source, digest)
}

func (b *Bucket) ApplyCommit(source NodeID, seqNo SeqNo, digest []byte) *consumer.Actions {
	return b.Sequences[seqNo].ApplyCommit(source, digest)
}
