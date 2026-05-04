package index

import (
	"encoding/binary"
	"fmt"
	"io"
)

// IndexSerializer 索引序列化器
type IndexSerializer struct{}

// Save 将 InvertedIndex 序列化到 writer
// 格式：Header(16B) + [TermEntry + Postings...] * N
func Save(idx *InvertedIndex, w io.Writer) error {
	terms := make([]string, 0, len(idx.index))
	for term := range idx.index {
		terms = append(terms, term)
	}

	if err := writeHeader(w, idx.docCount, uint32(len(terms))); err != nil {
		return err
	}

	for _, term := range terms {
		pl := idx.index[term]
		postings := pl.Postings()

		// 写目录条目
		if err := writeTermEntry(w, term, uint32(len(postings)), 0); err != nil {
			return err
		}

		// 紧跟写该 term 的 postings
		for _, p := range postings {
			if err := writePosting(w, p); err != nil {
				return err
			}
		}
	}

	return nil
}

func writeHeader(w io.Writer, docCount, termCount uint32) error {
	buf := make([]byte, 16)
	copy(buf[0:4], []byte("LUCY"))
	binary.LittleEndian.PutUint32(buf[4:8], 1) // Version
	binary.LittleEndian.PutUint32(buf[8:12], docCount)
	binary.LittleEndian.PutUint32(buf[12:16], termCount)
	_, err := w.Write(buf)
	return err
}

func writeTermEntry(w io.Writer, term string, postingCnt uint32, offset uint64) error {
	termBytes := []byte(term)
	if len(termBytes) > 65535 {
		return fmt.Errorf("term too long: %d bytes", len(termBytes))
	}

	buf := make([]byte, 2+len(termBytes)+4+8)
	binary.LittleEndian.PutUint16(buf[0:2], uint16(len(termBytes)))
	copy(buf[2:2+len(termBytes)], termBytes)
	binary.LittleEndian.PutUint32(buf[2+len(termBytes):2+len(termBytes)+4], postingCnt)
	binary.LittleEndian.PutUint64(buf[2+len(termBytes)+4:], offset)
	_, err := w.Write(buf)
	return err
}

func writePosting(w io.Writer, p Posting) error {
	// DocID + TermFreq + PosCount
	buf := make([]byte, 4+4+4+len(p.Positions)*4)
	binary.LittleEndian.PutUint32(buf[0:4], p.DocID)
	binary.LittleEndian.PutUint32(buf[4:8], p.TermFreq)
	binary.LittleEndian.PutUint32(buf[8:12], uint32(len(p.Positions)))
	for i, pos := range p.Positions {
		binary.LittleEndian.PutUint32(buf[12+i*4:16+i*4], pos)
	}
	_, err := w.Write(buf)
	return err
}

// Load 从 reader 加载 InvertedIndex
func Load(r io.Reader) (*InvertedIndex, error) {
	// 读文件头
	header := make([]byte, 16)
	if _, err := io.ReadFull(r, header); err != nil {
		return nil, fmt.Errorf("read header: %w", err)
	}

	if string(header[0:4]) != "LUCY" {
		return nil, fmt.Errorf("invalid magic: %s", string(header[0:4]))
	}

	version := binary.LittleEndian.Uint32(header[4:8])
	if version != 1 {
		return nil, fmt.Errorf("unsupported version: %d", version)
	}

	docCount := binary.LittleEndian.Uint32(header[8:12])
	termCount := binary.LittleEndian.Uint32(header[12:16])

	idx := NewInvertedIndex()
	idx.docCount = docCount

	// 读 Term 目录表
	for i := uint32(0); i < termCount; i++ {
		// TermLen (2 bytes)
		lenBuf := make([]byte, 2)
		if _, err := io.ReadFull(r, lenBuf); err != nil {
			return nil, fmt.Errorf("read term len: %w", err)
		}
		termLen := binary.LittleEndian.Uint16(lenBuf)

		// Term
		termBuf := make([]byte, termLen)
		if _, err := io.ReadFull(r, termBuf); err != nil {
			return nil, fmt.Errorf("read term: %w", err)
		}
		term := string(termBuf)

		// PostingCnt (4 bytes)
		cntBuf := make([]byte, 4)
		if _, err := io.ReadFull(r, cntBuf); err != nil {
			return nil, fmt.Errorf("read posting count: %w", err)
		}
		postingCnt := binary.LittleEndian.Uint32(cntBuf)

		// Offset (8 bytes) - 我们顺序读取，不需要 seek
		offsetBuf := make([]byte, 8)
		if _, err := io.ReadFull(r, offsetBuf); err != nil {
			return nil, fmt.Errorf("read offset: %w", err)
		}
		_ = binary.LittleEndian.Uint64(offsetBuf) // 顺序读取，跳过

		// 创建 PostingList
		pl := NewPostingList()

		// 读取 postings 数据
		for j := uint32(0); j < postingCnt; j++ {
			posting, err := readPosting(r)
			if err != nil {
				return nil, fmt.Errorf("read posting: %w", err)
			}
			pl.Add(posting.DocID, posting.TermFreq, posting.Positions)
		}

		idx.index[term] = pl
	}

	return idx, nil
}

func readPosting(r io.Reader) (Posting, error) {
	// DocID + TermFreq + PosCount
	header := make([]byte, 12)
	if _, err := io.ReadFull(r, header); err != nil {
		return Posting{}, err
	}

	docID := binary.LittleEndian.Uint32(header[0:4])
	termFreq := binary.LittleEndian.Uint32(header[4:8])
	posCount := binary.LittleEndian.Uint32(header[8:12])

	// Positions
	positions := make([]uint32, posCount)
	if posCount > 0 {
		posBuf := make([]byte, posCount*4)
		if _, err := io.ReadFull(r, posBuf); err != nil {
			return Posting{}, err
		}
		for i := uint32(0); i < posCount; i++ {
			positions[i] = binary.LittleEndian.Uint32(posBuf[i*4 : (i+1)*4])
		}
	}

	return Posting{
		DocID:     docID,
		TermFreq:  termFreq,
		Positions: positions,
	}, nil
}
