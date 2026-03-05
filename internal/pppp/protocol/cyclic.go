package protocol

import "fmt"

const (
	cyclicMask       uint32 = 0xFFFF
	defaultWrapLimit uint16 = 0x0100
	halfUint16Range  uint16 = 0x8000
)

// CyclicU16 is a uint16 counter with wrap-around helper methods.
type CyclicU16 uint16

// NewCyclicU16 truncates the input to 16 bits.
func NewCyclicU16(v int) CyclicU16 {
	return CyclicU16(uint32(v) & cyclicMask)
}

// Add returns a wrapped sum.
func (c CyclicU16) Add(v uint16) CyclicU16 {
	return CyclicU16(uint16(c) + v)
}

// Sub returns a wrapped subtraction.
func (c CyclicU16) Sub(v uint16) CyclicU16 {
	return CyclicU16(uint16(c) - v)
}

// Diff returns the forward cyclic distance from a to b.
func Diff(a, b CyclicU16) uint16 {
	return uint16(uint16(b) - uint16(a))
}

// IsAfter returns true if b is after a in cyclic order.
func IsAfter(a, b CyclicU16) bool {
	d := Diff(a, b)
	return d != 0 && d < halfUint16Range
}

// IsAfterOrEqual returns true if b is equal to or after a in cyclic order.
func IsAfterOrEqual(a, b CyclicU16) bool {
	return a == b || IsAfter(a, b)
}

// Less emulates the Python CyclicU16 __lt__ behavior with wrap window 0x100.
func (c CyclicU16) Less(other CyclicU16) bool {
	if (uint16(c)^uint16(other))&halfUint16Range != 0 {
		return uint16(c.Sub(defaultWrapLimit)) < uint16(other.Sub(defaultWrapLimit))
	}
	return c < other
}

// Greater emulates the Python CyclicU16 __gt__ behavior with wrap window 0x100.
func (c CyclicU16) Greater(other CyclicU16) bool {
	if (uint16(c)^uint16(other))&halfUint16Range != 0 {
		return uint16(c.Sub(defaultWrapLimit)) > uint16(other.Sub(defaultWrapLimit))
	}
	return c > other
}

func (c CyclicU16) String() string {
	return fmt.Sprintf("0x%04x", uint16(c))
}
