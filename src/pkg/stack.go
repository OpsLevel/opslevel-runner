package pkg

type Stack[T any] struct {
	Push   func(T)
	Peek   func() T
	Pop    func() T
	Length func() int
}

func NewStack[T any](defaultValue T) Stack[T] {
	slice := make([]T, 0)
	return Stack[T]{
		Push: func(i T) {
			slice = append(slice, i)
		},
		Peek: func() T {
			if len(slice) == 0 {
				return defaultValue
			}
			return slice[len(slice)-1]
		},
		Pop: func() T {
			if len(slice) == 0 {
				return defaultValue
			}
			res := slice[len(slice)-1]
			slice = slice[:len(slice)-1]
			return res
		},
		Length: func() int {
			return len(slice)
		},
	}
}
