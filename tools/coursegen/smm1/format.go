// Package smm1 reads and writes Super Mario Maker (Wii U) course files.
//
// Layout facts (all big endian) come from public documentation and from MIT
// licensed sources; see ../SOURCES.md for the list and the licence of each.
// Nothing here is copied code. The files written by this package have not been
// loaded by a real game yet.
package smm1

import (
	"encoding/binary"
	"errors"
	"fmt"
	"hash/crc32"
	"time"
	"unicode/utf16"
)

// File layout constants.
const (
	CourseSize   = 0x15000 // course_data.cdt and course_data_sub.cdt
	Version      = 11
	crcFrom      = 0x10
	offCRC       = 0x08
	offDate      = 0x10
	offTitle     = 0x28
	titleChars   = 32
	offStyle     = 0x6A
	offTheme     = 0x6D
	offTime      = 0x70
	offScroll    = 0x72
	offWidth     = 0x74
	offMaker     = 0x91
	makerChars   = 10
	offCount     = 0xEC
	offObjects   = 0xF0
	ObjectSize   = 0x20
	MaxObjects   = 2600
	offEffects   = 0x145F0
	EffectSize   = 8
	EffectCount  = 300
	SoundSize    = 55304 // sound.bwv: CRC32 + 55300 bytes
	ThumbSize    = 51200 // thumbnail0.tnl / thumbnail1.tnl
	ThumbMaxJPEG = ThumbSize - 8
)

// Style is the game style code.
type Style string

const (
	SMB1  Style = "M1"
	SMB3  Style = "M3"
	SMW   Style = "MW"
	NSMBU Style = "WU"
)

// Styles lists every style in a fixed order.
var Styles = []Style{SMB1, SMB3, SMW, NSMBU}

func (s Style) Valid() bool { return s == SMB1 || s == SMB3 || s == SMW || s == NSMBU }

// Theme is the course theme code.
type Theme uint8

const (
	Ground Theme = iota
	Underground
	Castle
	Airship
	Water
	GhostHouse
)

// Themes lists every theme in a fixed order.
var Themes = []Theme{Ground, Underground, Castle, Airship, Water, GhostHouse}

var themeNames = [...]string{"ground", "underground", "castle", "airship", "water", "ghost-house"}

func (t Theme) String() string {
	if int(t) < len(themeNames) {
		return themeNames[t]
	}
	return fmt.Sprintf("theme%d", t)
}

// Object type ids as stored in the file (the ".cdt ID" of the Treeki notes).
const (
	TGoomba       = 0
	TKoopa        = 1
	TPiranha      = 2
	TBrick        = 4
	TQuestion     = 5
	THard         = 6
	TGround       = 7
	TCoin         = 8
	TPipe         = 9
	TSpring       = 10
	TBobOmb       = 15
	TSuperMush    = 20
	TCloud        = 22
	TSpiny        = 25
	TGoalGround   = 26
	TGoalPole     = 27
	TBuzzy        = 28
	TFireFlower   = 34
	TStar         = 35
	TStartGround  = 37
	TStartSign    = 38
	TDryBones     = 46
	TCheep        = 56
	TPlayer       = 69
	DefaultFlags  = 0x06000840
	FlagVariant   = 0x4 // red koopa, fire piranha, ...
	NoChild       = -1
	BlockSort     = 23010
	CoinSort      = 13000
	PipeSort      = 44000
	FirstActorKey = 92670
)

// Object is one 32-byte entry of the object list. X, Y are in 1/160 block
// units as stored; use BlockX and BlockY for block coordinates.
type Object struct {
	X, Sort    uint32
	Y          int16
	W, H       int8
	Flags      uint32
	ChildFlags uint32
	Ext        uint32
	Type       int8
	ChildType  int8
	Link       int16
	Effect     int16
	U1, U2     int8
}

// BlockToRaw converts a block index to the stored coordinate (centre of the block).
func BlockToRaw(b int) int { return b*160 + 80 }

// RawToBlock converts a stored coordinate back to a block index.
func RawToBlock(r int) int { return (r - 80) / 160 }

// Course is the content of course_data.cdt (main area). The sub area written by
// this package is always empty.
type Course struct {
	Title      string
	Maker      string
	Style      Style
	Theme      Theme
	SubTheme   Theme
	Time       int
	AutoScroll int
	WidthPx    int // course length in pixels, a multiple of 16
	Modified   time.Time
	Objects    []Object
}

func putUTF16BE(dst []byte, s string, max int) {
	u := utf16.Encode([]rune(s))
	if len(u) > max {
		u = u[:max]
	}
	for i, c := range u {
		binary.BigEndian.PutUint16(dst[i*2:], c)
	}
}

func getUTF16BE(src []byte, n int) string {
	u := make([]uint16, 0, n)
	for i := 0; i < n; i++ {
		c := binary.BigEndian.Uint16(src[i*2:])
		if c == 0 {
			break
		}
		u = append(u, c)
	}
	return string(utf16.Decode(u))
}

// defaultEffect is the "no effect" entry of the sound effect table.
var defaultEffect = [EffectSize]byte{0xFF, 0xFF, 0, 0xFF, 0xFF, 0, 0, 0}

// Marshal builds course_data.cdt.
func (c *Course) Marshal() ([]byte, error) { return c.marshal(false) }

// MarshalSub builds the matching empty course_data_sub.cdt.
func (c *Course) MarshalSub() ([]byte, error) { return c.marshal(true) }

func (c *Course) marshal(sub bool) ([]byte, error) {
	if !c.Style.Valid() {
		return nil, fmt.Errorf("smm1: bad style %q", c.Style)
	}
	if c.Time < 0 || c.Time > 500 {
		return nil, fmt.Errorf("smm1: timer %d out of range", c.Time)
	}
	if len(c.Objects) > MaxObjects {
		return nil, fmt.Errorf("smm1: %d objects exceed the limit of %d", len(c.Objects), MaxObjects)
	}
	b := make([]byte, CourseSize)
	binary.BigEndian.PutUint64(b[0:], Version)
	d := c.Modified
	binary.BigEndian.PutUint16(b[offDate:], uint16(d.Year()))
	b[offDate+2], b[offDate+3], b[offDate+4], b[offDate+5] = byte(d.Month()), byte(d.Day()), byte(d.Hour()), byte(d.Minute())
	putUTF16BE(b[offTitle:], c.Title, titleChars)
	copy(b[offStyle:], string(c.Style))
	theme := c.Theme
	if sub {
		theme = c.SubTheme
	}
	b[offTheme] = byte(theme)
	binary.BigEndian.PutUint16(b[offTime:], uint16(c.Time))
	putUTF16BE(b[offMaker:], c.Maker, makerChars)
	for i := 0; i < EffectCount; i++ {
		copy(b[offEffects+i*EffectSize:], defaultEffect[:])
	}
	if !sub {
		b[offScroll] = byte(c.AutoScroll)
		binary.BigEndian.PutUint32(b[offWidth:], uint32(c.WidthPx))
		binary.BigEndian.PutUint32(b[offCount:], uint32(len(c.Objects)))
		for i, o := range c.Objects {
			p := b[offObjects+i*ObjectSize:]
			binary.BigEndian.PutUint32(p[0:], o.X)
			binary.BigEndian.PutUint32(p[4:], o.Sort)
			binary.BigEndian.PutUint16(p[8:], uint16(o.Y))
			p[10], p[11] = byte(o.W), byte(o.H)
			binary.BigEndian.PutUint32(p[12:], o.Flags)
			binary.BigEndian.PutUint32(p[16:], o.ChildFlags)
			binary.BigEndian.PutUint32(p[20:], o.Ext)
			p[24], p[25] = byte(o.Type), byte(o.ChildType)
			binary.BigEndian.PutUint16(p[26:], uint16(o.Link))
			binary.BigEndian.PutUint16(p[28:], uint16(o.Effect))
			p[30], p[31] = byte(o.U1), byte(o.U2)
		}
	}
	binary.BigEndian.PutUint32(b[offCRC:], crc32.ChecksumIEEE(b[crcFrom:]))
	return b, nil
}

// Errors returned by Parse.
var (
	ErrSize    = errors.New("smm1: wrong file size")
	ErrVersion = errors.New("smm1: unsupported version")
	ErrCRC     = errors.New("smm1: checksum mismatch")
)

// Parse reads a main-area course_data.cdt and verifies size, version and CRC.
func Parse(b []byte) (*Course, error) {
	if len(b) != CourseSize {
		return nil, ErrSize
	}
	if binary.BigEndian.Uint64(b) != Version {
		return nil, ErrVersion
	}
	if binary.BigEndian.Uint32(b[offCRC:]) != crc32.ChecksumIEEE(b[crcFrom:]) {
		return nil, ErrCRC
	}
	n := int(binary.BigEndian.Uint32(b[offCount:]))
	if n > MaxObjects {
		return nil, fmt.Errorf("smm1: object count %d exceeds %d", n, MaxObjects)
	}
	c := &Course{
		Title:      getUTF16BE(b[offTitle:], titleChars),
		Maker:      getUTF16BE(b[offMaker:], makerChars),
		Style:      Style(b[offStyle : offStyle+2]),
		Theme:      Theme(b[offTheme]),
		Time:       int(binary.BigEndian.Uint16(b[offTime:])),
		AutoScroll: int(b[offScroll]),
		WidthPx:    int(binary.BigEndian.Uint32(b[offWidth:])),
		Modified: time.Date(int(binary.BigEndian.Uint16(b[offDate:])), time.Month(b[offDate+2]),
			int(b[offDate+3]), int(b[offDate+4]), int(b[offDate+5]), 0, 0, time.UTC),
	}
	if !c.Style.Valid() {
		return nil, fmt.Errorf("smm1: bad style %q", c.Style)
	}
	c.Objects = make([]Object, n)
	for i := range c.Objects {
		p := b[offObjects+i*ObjectSize:]
		c.Objects[i] = Object{
			X: binary.BigEndian.Uint32(p[0:]), Sort: binary.BigEndian.Uint32(p[4:]),
			Y: int16(binary.BigEndian.Uint16(p[8:])), W: int8(p[10]), H: int8(p[11]),
			Flags: binary.BigEndian.Uint32(p[12:]), ChildFlags: binary.BigEndian.Uint32(p[16:]),
			Ext: binary.BigEndian.Uint32(p[20:]), Type: int8(p[24]), ChildType: int8(p[25]),
			Link: int16(binary.BigEndian.Uint16(p[26:])), Effect: int16(binary.BigEndian.Uint16(p[28:])),
			U1: int8(p[30]), U2: int8(p[31]),
		}
	}
	return c, nil
}

// Sound returns an empty sound.bwv (CRC32 of 55300 zero bytes, then zeros).
func Sound() []byte {
	b := make([]byte, SoundSize)
	binary.BigEndian.PutUint32(b, crc32.ChecksumIEEE(b[4:]))
	return b
}

// PackThumbnail wraps a JPEG as a .tnl file: CRC32 of everything after the
// first four bytes, the JPEG length, the JPEG, zero padding to 51200 bytes.
func PackThumbnail(jpeg []byte) ([]byte, error) {
	if len(jpeg) > ThumbMaxJPEG {
		return nil, fmt.Errorf("smm1: thumbnail JPEG is %d bytes, the limit is %d", len(jpeg), ThumbMaxJPEG)
	}
	b := make([]byte, ThumbSize)
	binary.BigEndian.PutUint32(b[4:], uint32(len(jpeg)))
	copy(b[8:], jpeg)
	binary.BigEndian.PutUint32(b, crc32.ChecksumIEEE(b[4:]))
	return b, nil
}

// UnpackThumbnail returns the JPEG inside a .tnl file after checking its CRC.
func UnpackThumbnail(b []byte) ([]byte, error) {
	if len(b) != ThumbSize {
		return nil, ErrSize
	}
	if binary.BigEndian.Uint32(b) != crc32.ChecksumIEEE(b[4:]) {
		return nil, ErrCRC
	}
	n := int(binary.BigEndian.Uint32(b[4:]))
	if n > ThumbMaxJPEG {
		return nil, fmt.Errorf("smm1: thumbnail length %d out of range", n)
	}
	return b[8 : 8+n], nil
}

// GroundExt returns the auto-tile value for a ground tile with the given
// neighbourhood bits (see groundTable).
func GroundExt(neighbours uint8) uint32 { return uint32(groundTable[neighbours]) }
