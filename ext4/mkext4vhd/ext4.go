package main

import (
	"bytes"
	"encoding/binary"
	"io"

	"github.com/Microsoft/hcsshim/ext4/internal/format"
)

const (
	blockSize         = 4096
	blocksPerGroup    = blockSize * 8
	inodeSize         = 256
	inodeRatio        = 16384
	extraIsize        = 152 - 128
	inodeFirst        = 11
	inodeLostAndFound = inodeFirst
)

func FormatExt4(w io.WriterAt, size int64) error {
	blocks := uint32(size / blockSize)
	groups := (blocks-1)/blocksPerGroup + 1
	inodeCount := uint32(size / inodeRatio)
	inodesPerGroup := inodeCount / groups

	totalUsedBlocks := uint32(0)
	totalUsedInodes := uint32(0)

	var blk [blockSize]byte
	b := bytes.NewBuffer(blk[:1024])
	sb := &format.SuperBlock{
		InodesCount:        inodeCount,
		BlocksCountLow:     blocks,
		FreeBlocksCountLow: blocksPerGroup*groups - totalUsedBlocks,
		FreeInodesCount:    inodesPerGroup*groups - totalUsedInodes,
		FirstDataBlock:     0,
		LogBlockSize:       2, // 2^(10 + 2)
		LogClusterSize:     2,
		BlocksPerGroup:     blocksPerGroup,
		ClustersPerGroup:   blocksPerGroup,
		InodesPerGroup:     inodesPerGroup,
		Magic:              format.SuperBlockMagic,
		State:              1, // cleanly unmounted
		Errors:             1, // continue on error?
		CreatorOS:          0, // Linux
		RevisionLevel:      1, // dynamic inode sizes
		FirstInode:         inodeFirst,
		LpfInode:           inodeLostAndFound,
		InodeSize:          inodeSize,
		FeatureCompat:      format.CompatSparseSuper2 | format.CompatExtAttr,
		FeatureIncompat:    format.IncompatFiletype | format.IncompatExtents | format.IncompatFlexBg,
		FeatureRoCompat:    format.RoCompatExtraIsize,
		MinExtraIsize:      extraIsize,
		WantExtraIsize:     extraIsize,
		LogGroupsPerFlex:   31,
	}
	binary.Write(b, binary.LittleEndian, sb)
	_, err := w.WriteAt(b.Bytes(), 0)
	if err != nil {
		return err
	}

	// write gds
	// write first block bitmap
	// write first data bitmap

	return nil
}
