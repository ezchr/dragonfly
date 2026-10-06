package wire

import (
	"bufio"
	"bytes"
	"compress/zlib"
	"crypto/aes"
	"crypto/cipher"
	"errors"
	"fmt"
	"io"
	"net"
	"sync"

	"github.com/ezchr/go-mc/net/CFB8"
)

// Limits the vanilla server uses.
const (
	MaxFrame        = 2097151 // largest length a 3-byte VarInt holds
	MaxDecompressed = 8388608
)

// Conn is a Java Edition connection: VarInt-length frames, optional zlib compression above a
// threshold, optional AES/CFB8 encryption. Reads and writes reuse their buffers and zlib state, so
// steady-state traffic doesn't allocate. One goroutine may read while others write.
type Conn struct {
	nc net.Conn

	rmu       sync.Mutex
	br        *bufio.Reader
	frame     []byte // last frame read
	inflated  bytes.Buffer
	zr        io.ReadCloser
	decrypt   cipher.Stream
	threshold int // -1 = no compression; shared by both directions

	wmu     sync.Mutex
	bw      *bufio.Writer
	out     io.Writer // nc, or nc wrapped in encryption
	hdr     []byte
	zbuf    bytes.Buffer
	zw      *zlib.Writer
	encrypt cipher.Stream
	crypt   []byte
}

// NewConn wraps a TCP connection.
func NewConn(nc net.Conn) *Conn {
	c := &Conn{nc: nc, threshold: -1}
	c.br = bufio.NewReaderSize(readerFunc(c.readRaw), 1<<15)
	c.out = nc
	c.bw = bufio.NewWriterSize(writerFunc(c.writeRaw), 1<<15)
	return c
}

type readerFunc func([]byte) (int, error)

func (f readerFunc) Read(b []byte) (int, error) { return f(b) }

type writerFunc func([]byte) (int, error)

func (f writerFunc) Write(b []byte) (int, error) { return f(b) }

func (c *Conn) readRaw(b []byte) (int, error) {
	n, err := c.nc.Read(b)
	if c.decrypt != nil && n > 0 {
		c.decrypt.XORKeyStream(b[:n], b[:n])
	}
	return n, err
}

func (c *Conn) writeRaw(b []byte) (int, error) {
	if c.encrypt == nil {
		return c.nc.Write(b)
	}
	// Not in place: for writes bigger than its buffer bufio passes the caller's slice straight
	// through, and that may be shared (a cached chunk sent to many players).
	if cap(c.crypt) < len(b) {
		c.crypt = make([]byte, len(b))
	}
	out := c.crypt[:len(b)]
	c.encrypt.XORKeyStream(out, b)
	if _, err := c.nc.Write(out); err != nil {
		return 0, err
	}
	return len(b), nil
}

// NetConn is the underlying connection.
func (c *Conn) NetConn() net.Conn { return c.nc }

// Close closes the connection.
func (c *Conn) Close() error { return c.nc.Close() }

// SetThreshold turns on compression for packets of at least t bytes (negative turns it off).
// Call it right after writing the Set Compression packet, with no other writes in between.
func (c *Conn) SetThreshold(t int) {
	c.rmu.Lock()
	c.wmu.Lock()
	c.threshold = t
	c.wmu.Unlock()
	c.rmu.Unlock()
}

// EnableEncryption starts AES/CFB8 with the shared secret (key and IV are both the secret) in
// both directions. Bytes already buffered from the network are decrypted too.
func (c *Conn) EnableEncryption(secret []byte) error {
	if len(secret) != 16 {
		return errors.New("wire: shared secret must be 16 bytes")
	}
	blk, err := aes.NewCipher(secret)
	if err != nil {
		return err
	}
	c.rmu.Lock()
	c.wmu.Lock()
	defer c.wmu.Unlock()
	defer c.rmu.Unlock()
	if err := c.bw.Flush(); err != nil {
		return err
	}
	c.encrypt = CFB8.NewCFB8Encrypt(blk, secret)
	c.decrypt = CFB8.NewCFB8Decrypt(blk, secret)
	if n := c.br.Buffered(); n > 0 {
		pending, _ := c.br.Peek(n)
		c.decrypt.XORKeyStream(pending, pending)
	}
	return nil
}

func readVarIntFrom(r io.ByteReader) (int32, error) {
	var u uint32
	for i := 0; i < 5; i++ {
		b, err := r.ReadByte()
		if err != nil {
			return 0, err
		}
		u |= uint32(b&0x7f) << (7 * i)
		if b&0x80 == 0 {
			return int32(u), nil
		}
	}
	return 0, ErrVarInt
}

// ReadPacket reads the next packet. body is only valid until the next ReadPacket.
func (c *Conn) ReadPacket() (id int32, body []byte, err error) {
	c.rmu.Lock()
	defer c.rmu.Unlock()
	n, err := readVarIntFrom(c.br)
	if err != nil {
		return 0, nil, err
	}
	if n <= 0 || n > MaxFrame {
		return 0, nil, fmt.Errorf("%w: frame of %d bytes", ErrTooLarge, n)
	}
	if cap(c.frame) < int(n) {
		c.frame = make([]byte, n)
	}
	c.frame = c.frame[:n]
	if _, err := io.ReadFull(c.br, c.frame); err != nil {
		return 0, nil, err
	}
	payload := c.frame
	if c.threshold >= 0 {
		r := NewReader(c.frame)
		dataLen := int(r.VarInt())
		if r.Err != nil {
			return 0, nil, r.Err
		}
		rest := c.frame[r.Off:]
		if dataLen != 0 {
			if dataLen < c.threshold || dataLen > MaxDecompressed {
				return 0, nil, fmt.Errorf("%w: compressed packet claims %d bytes", ErrTooLarge, dataLen)
			}
			if c.zr == nil {
				c.zr, err = zlib.NewReader(bytes.NewReader(rest))
			} else {
				err = c.zr.(zlib.Resetter).Reset(bytes.NewReader(rest), nil)
			}
			if err != nil {
				return 0, nil, err
			}
			c.inflated.Reset()
			c.inflated.Grow(dataLen)
			if _, err := io.CopyN(&c.inflated, c.zr, int64(dataLen)); err != nil {
				return 0, nil, fmt.Errorf("wire: inflate: %w", err)
			}
			payload = c.inflated.Bytes()
		} else {
			payload = rest
		}
	}
	r := NewReader(payload)
	id = r.VarInt()
	if r.Err != nil {
		return 0, nil, r.Err
	}
	return id, payload[r.Off:], nil
}

// WritePacket buffers a packet; call Flush to send. Safe to call from several goroutines.
func (c *Conn) WritePacket(id int32, body []byte) error {
	c.wmu.Lock()
	defer c.wmu.Unlock()
	return c.writePacket(id, body)
}

func (c *Conn) writePacket(id int32, body []byte) error {
	dataLen := VarIntSize(id) + len(body)
	if dataLen > MaxDecompressed {
		return fmt.Errorf("%w: packet %#x is %d bytes", ErrTooLarge, id, dataLen)
	}
	c.hdr = c.hdr[:0]
	if c.threshold < 0 {
		c.hdr = AppendVarInt(c.hdr, int32(dataLen))
		c.hdr = AppendVarInt(c.hdr, id)
		c.bw.Write(c.hdr)
		_, err := c.bw.Write(body)
		return err
	}
	if dataLen < c.threshold {
		c.hdr = AppendVarInt(c.hdr, int32(dataLen+1))
		c.hdr = append(c.hdr, 0) // uncompressed
		c.hdr = AppendVarInt(c.hdr, id)
		c.bw.Write(c.hdr)
		_, err := c.bw.Write(body)
		return err
	}
	c.zbuf.Reset()
	if c.zw == nil {
		c.zw = zlib.NewWriter(&c.zbuf)
	} else {
		c.zw.Reset(&c.zbuf)
	}
	var idb [5]byte
	c.zw.Write(AppendVarInt(idb[:0], id))
	c.zw.Write(body)
	if err := c.zw.Close(); err != nil {
		return err
	}
	frame := VarIntSize(int32(dataLen)) + c.zbuf.Len()
	if frame > MaxFrame {
		return fmt.Errorf("%w: compressed packet %#x is %d bytes", ErrTooLarge, id, frame)
	}
	c.hdr = AppendVarInt(c.hdr, int32(frame))
	c.hdr = AppendVarInt(c.hdr, int32(dataLen))
	c.bw.Write(c.hdr)
	_, err := c.bw.Write(c.zbuf.Bytes())
	return err
}

// Flush sends buffered packets.
func (c *Conn) Flush() error {
	c.wmu.Lock()
	defer c.wmu.Unlock()
	return c.bw.Flush()
}

// Send writes one packet and flushes.
func (c *Conn) Send(id int32, body []byte) error {
	c.wmu.Lock()
	defer c.wmu.Unlock()
	if err := c.writePacket(id, body); err != nil {
		return err
	}
	return c.bw.Flush()
}
