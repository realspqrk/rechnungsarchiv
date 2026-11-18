package bag

import (
	"encoding/binary"
	"errors"
	"fmt"
	"hash/crc32"
	"io"
)

// A minimal ZIP writer for the Ablage profile (spec §2): Stored only, UTF-8
// flag, fixed 1980-01-01 00:00 time, no extra fields except zip64 when a
// value does not fit in 32 bits, no data descriptors (sizes and CRC are
// patched into the local header after streaming), no comments.
//
// archive/zip is not used because its streaming API always writes data
// descriptors and setting FileHeader.Modified adds an 0x5455 extra field.

const (
	sigLocal      = 0x04034b50
	sigCentral    = 0x02014b50
	sigEOCD       = 0x06054b50
	sigZip64EOCD  = 0x06064b50
	sigZip64Loc   = 0x07064b50
	flagUTF8      = 0x0800
	dosDate1980   = 0x0021 // 1980-01-01
	dosTime0000   = 0
	max32         = 0xFFFFFFFF
	max16         = 0xFFFF
	versionStd    = 20
	versionZip64  = 45
	zip64ExtraTag = 0x0001
)

type zipWriter struct {
	w       io.WriteSeeker
	off     uint64
	entries []zipEntry
}

func newZipWriter(w io.WriteSeeker) *zipWriter { return &zipWriter{w: w} }

func (z *zipWriter) write(b []byte) error {
	n, err := z.w.Write(b)
	z.off += uint64(n)
	return err
}

// add streams one file entry. size is the expected size (it decides the
// local header layout); fewer or more bytes from r is an error. Every byte
// written to the ZIP is also written to tee.
func (z *zipWriter) add(name string, size int64, r io.Reader, tee io.Writer) error {
	if len(name) > max16 {
		return fmt.Errorf("zip: name too long")
	}
	start := z.off
	zip64 := uint64(size) >= max32
	var extra []byte
	ver := uint16(versionStd)
	if zip64 {
		ver = versionZip64
		extra = make([]byte, 20)
		binary.LittleEndian.PutUint16(extra[0:], zip64ExtraTag)
		binary.LittleEndian.PutUint16(extra[2:], 16)
		binary.LittleEndian.PutUint64(extra[4:], uint64(size))
		binary.LittleEndian.PutUint64(extra[12:], uint64(size))
	}
	h := make([]byte, 30)
	le := binary.LittleEndian
	le.PutUint32(h[0:], sigLocal)
	le.PutUint16(h[4:], ver)
	le.PutUint16(h[6:], flagUTF8)
	le.PutUint16(h[8:], 0) // Stored
	le.PutUint16(h[10:], dosTime0000)
	le.PutUint16(h[12:], dosDate1980)
	le.PutUint32(h[14:], 0) // CRC, patched below
	s32 := uint32(size)
	if zip64 {
		s32 = max32
	}
	le.PutUint32(h[18:], s32)
	le.PutUint32(h[22:], s32)
	le.PutUint16(h[26:], uint16(len(name)))
	le.PutUint16(h[28:], uint16(len(extra)))
	if err := z.write(h); err != nil {
		return err
	}
	if err := z.write([]byte(name)); err != nil {
		return err
	}
	if err := z.write(extra); err != nil {
		return err
	}
	crc := crc32.NewIEEE()
	cw := &countWriter{w: z.w}
	dst := io.MultiWriter(cw, crc, tee)
	_, err := io.Copy(dst, io.LimitReader(r, size+1))
	z.off += uint64(cw.n)
	if err != nil {
		return err
	}
	if cw.n != size {
		return ErrChanged
	}
	sum := crc.Sum32()
	if _, err := z.w.Seek(int64(start)+14, io.SeekStart); err != nil {
		return err
	}
	var c [4]byte
	le.PutUint32(c[:], sum)
	if _, err := z.w.Write(c[:]); err != nil {
		return err
	}
	if _, err := z.w.Seek(int64(z.off), io.SeekStart); err != nil {
		return err
	}
	z.entries = append(z.entries, zipEntry{name: name, size: uint64(size), crc: sum, headerOff: start})
	return nil
}

// close writes the central directory and end records.
func (z *zipWriter) close() error {
	le := binary.LittleEndian
	cdStart := z.off
	for _, e := range z.entries {
		var vals []uint64
		usize, csize, off := uint32(e.size), uint32(e.size), uint32(e.headerOff)
		if e.size >= max32 {
			vals = append(vals, e.size, e.size)
			usize, csize = max32, max32
		}
		if e.headerOff >= max32 {
			vals = append(vals, e.headerOff)
			off = max32
		}
		var extra []byte
		ver := uint16(versionStd)
		if e.size >= max32 {
			ver = versionZip64 // must equal the local header
		}
		if len(vals) > 0 {
			extra = make([]byte, 4+8*len(vals))
			le.PutUint16(extra[0:], zip64ExtraTag)
			le.PutUint16(extra[2:], uint16(8*len(vals)))
			for i, v := range vals {
				le.PutUint64(extra[4+8*i:], v)
			}
		}
		h := make([]byte, 46)
		le.PutUint32(h[0:], sigCentral)
		le.PutUint16(h[4:], versionZip64) // version made by: MS-DOS, 4.5
		le.PutUint16(h[6:], ver)
		le.PutUint16(h[8:], flagUTF8)
		le.PutUint16(h[10:], 0)
		le.PutUint16(h[12:], dosTime0000)
		le.PutUint16(h[14:], dosDate1980)
		le.PutUint32(h[16:], e.crc)
		le.PutUint32(h[20:], csize)
		le.PutUint32(h[24:], usize)
		le.PutUint16(h[28:], uint16(len(e.name)))
		le.PutUint16(h[30:], uint16(len(extra)))
		// comment length, disk start, internal and external attributes: 0
		le.PutUint32(h[42:], off)
		if err := z.write(h); err != nil {
			return err
		}
		if err := z.write([]byte(e.name)); err != nil {
			return err
		}
		if err := z.write(extra); err != nil {
			return err
		}
	}
	cdEnd := z.off
	n, cdSize := uint64(len(z.entries)), cdEnd-cdStart
	if n >= max16 || cdSize >= max32 || cdStart >= max32 {
		r := make([]byte, 56+20)
		le.PutUint32(r[0:], sigZip64EOCD)
		le.PutUint64(r[4:], 44)
		le.PutUint16(r[12:], versionZip64)
		le.PutUint16(r[14:], versionZip64)
		le.PutUint64(r[24:], n)
		le.PutUint64(r[32:], n)
		le.PutUint64(r[40:], cdSize)
		le.PutUint64(r[48:], cdStart)
		le.PutUint32(r[56:], sigZip64Loc)
		le.PutUint64(r[64:], cdEnd)
		le.PutUint32(r[72:], 1)
		if err := z.write(r); err != nil {
			return err
		}
		n, cdSize, cdStart = max16, max32, max32
	}
	e := make([]byte, 22)
	le.PutUint32(e[0:], sigEOCD)
	le.PutUint16(e[8:], uint16(min(n, max16)))
	le.PutUint16(e[10:], uint16(min(n, max16)))
	le.PutUint32(e[12:], uint32(min(cdSize, max32)))
	le.PutUint32(e[16:], uint32(min(cdStart, max32)))
	return z.write(e)
}

type countWriter struct {
	w io.Writer
	n int64
}

func (c *countWriter) Write(p []byte) (int, error) {
	n, err := c.w.Write(p)
	c.n += int64(n)
	if err == nil && n < len(p) {
		err = errors.New("short write")
	}
	return n, err
}
