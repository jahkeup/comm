package comm

func P[T comparable](value T) *T {
	return &value
}

func PS[T comparable](value []T) []*T {
	ps := make([]*T, len(value))
	for i := range ps {
		ps[i] = P(value[i])
	}
	return ps
}

func F[T comparable](p *T) T {
	var zero T
	if p != nil {
		return *p
	}
	return zero
}

func FS[T comparable](value []*T) []T {
	fs := []T{}

	for i := range value {
		if value[i] == nil {
			continue
		}
		fs = append(fs, F(value[i]))
	}

	return fs
}
