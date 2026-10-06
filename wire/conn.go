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
	"slices"
	"sync"

	"github.com/ezchr/go-mcjava/internal/cfb8"
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
	inflated  []byte // last decompressed packet
	maxPacket int    // SetMaxPacket; 0 = protocol limits
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
	c.encrypt = cfb8.NewCFB8Encrypt(blk, secret)
	c.decrypt = cfb8.NewCFB8Decrypt(blk, secret)
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

// readChunk is how far ahead of the data that actually arrived ReadPacket grows its buffers: a
// frame length or a compressed packet's claimed size alone never makes it allocate more than this.
const readChunk = 64 << 10

// keepBuffer is the largest read buffer kept between packets. A bigger one (from one big packet)
// is dropped on the next ReadPacket so it does not stay pinned for the life of the connection.
const keepBuffer = 1 << 20

// SetMaxPacket limits the packets ReadPacket accepts to n bytes, both on the wire and after
// decompression (0 restores the protocol limits, MaxFrame and MaxDecompressed). A server sets a
// small limit until the client has logged in: login and configuration packets are small, so a
// client that is not even authenticated cannot make it inflate megabytes.
func (c *Conn) SetMaxPacket(n int) {
	c.rmu.Lock()
	c.maxPacket = n
	c.rmu.Unlock()
}

// ReadPacket reads the next packet. body is only valid until the next ReadPacket.
func (c *Conn) ReadPacket() (id int32, body []byte, err error) {
	c.rmu.Lock()
	defer c.rmu.Unlock()
	if cap(c.frame) > keepBuffer {
		c.frame = nil
	}
	if cap(c.inflated) > keepBuffer {
		c.inflated = nil
	}
	maxFrame, maxData := MaxFrame, MaxDecompressed
	if c.maxPacket > 0 {
		maxFrame, maxData = min(maxFrame, c.maxPacket), min(maxData, c.maxPacket)
	}
	n, err := readVarIntFrom(c.br)
	if err != nil {
		return 0, nil, err
	}
	if n <= 0 || int(n) > maxFrame {
		return 0, nil, fmt.Errorf("%w: frame of %d bytes", ErrTooLarge, n)
	}
	if c.frame, err = readGrowing(c.br, c.frame[:0], int(n)); err != nil {
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
			if dataLen < c.threshold || dataLen > maxData {
				return 0, nil, fmt.Errorf("%w: compressed packet claims %d bytes", ErrTooLarge, dataLen)
			}
			if c.zr == nil {
				c.zr, err = zlib.NewReader(bytes.NewReader(rest))
			} else {
				err = c.zr.(zlib.Resetter).Reset(bytes.NewReader(rest), nil)
			}
			if err != nil {
				return 0, nil, fmt.Errorf("wire: inflate: %w", err)
			}
			if c.inflated, err = readGrowing(c.zr, c.inflated[:0], dataLen); err != nil {
				return 0, nil, fmt.Errorf("wire: inflate: %w (packet claims %d bytes)", err, dataLen)
			}
			// Like vanilla, the packet must inflate to exactly the size it claims: nothing may be
			// left (and the stream must end with a valid checksum).
			var one [1]byte
			if _, err := io.ReadFull(c.zr, one[:]); err != io.EOF {
				if err == nil {
					return 0, nil, fmt.Errorf("wire: inflate: packet is longer than the %d bytes it claims", dataLen)
				}
				return 0, nil, fmt.Errorf("wire: inflate: %w", err)
			}
			payload = c.inflated
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

// readGrowing reads exactly n bytes from r, appending them to buf. buf grows at most readChunk
// bytes ahead of the data read so far, so a claimed length alone allocates little.
func readGrowing(r io.Reader, buf []byte, n int) ([]byte, error) {
	for len(buf) < n {
		k := min(n-len(buf), readChunk)
		buf = slices.Grow(buf, k)
		got, err := io.ReadFull(r, buf[len(buf):len(buf)+k])
		buf = buf[:len(buf)+got]
		if err != nil {
			if err == io.EOF && len(buf) > 0 {
				err = io.ErrUnexpectedEOF
			}
			return buf, err
		}
	}
	return buf, nil
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
	// An uncompressed frame holds the packet (plus the 0 data length byte when compression is on);
	// its length must fit the 3-byte VarInt the client's frame decoder reads.
	if frame := dataLen + 1; c.threshold < 0 && dataLen > MaxFrame || c.threshold >= 0 && dataLen < c.threshold && frame > MaxFrame {
		return fmt.Errorf("%w: uncompressed packet %#x is %d bytes", ErrTooLarge, id, dataLen)
	}
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
