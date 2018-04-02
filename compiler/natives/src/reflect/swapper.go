// +build js

package reflect

import "github.com/gopherjs/gopherjs/js"

func Swapper(slice interface{}) func(i, j int) {
	v := ValueOf(slice)
	if v.Kind() != Slice {
		panic(&ValueError{Method: "Swapper", Kind: v.Kind()})
	}
	// Fast path for slices of size 0 and 1. Nothing to swap.
	vLen := uint(v.Len())
	switch vLen {
	case 0:
		return func(i, j int) { panic("reflect: slice index out of range") }
	case 1:
		return func(i, j int) {
			if i != 0 || j != 0 {
				panic("reflect: slice index out of range")
			}
		}
	}
	s := js.InternalObject(slice).Get("$array")
	return func(i, j int) {
		if uint(i) >= vLen || uint(j) >= vLen {
			panic("reflect: slice index out of range")
		}
		tmp := s.Index(i)
		s.SetIndex(i, s.Index(j))
		s.SetIndex(j, tmp)
	}
}
