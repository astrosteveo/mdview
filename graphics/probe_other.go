//go:build !unix

package graphics

type Capability struct{ CellWidth, CellHeight float64 }

func Eligible() bool                   { return false }
func Probe() (Capability, bool)        { return Capability{}, false }
func CellSize(c Capability) Capability { return c }
