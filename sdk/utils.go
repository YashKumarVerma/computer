package sdk

type Set[T comparable] map[T]struct{}

// NewSet pre-allocates map size to avoid resizing overhead
func NewSet[T comparable](list []T) Set[T] {
	s := make(Set[T], len(list))
	for _, v := range list {
		s.Add(v)
	}
	return s
}

func MergeSets[T comparable](s1 Set[T], s2 Set[T]) Set[T] {
	// Pre-allocate for the largest possible size
	s := make(Set[T], len(s1)+len(s2))
	s.Merge(s1)
	s.Merge(s2)
	return s
}

func (s Set[T]) Exists(item T) bool {
	_, ok := s[item]
	return ok
}

func (s Set[T]) Add(elem T) {
	s[elem] = struct{}{}
}

func (s Set[T]) List() []T {
	list := make([]T, 0, len(s))
	for k := range s {
		list = append(list, k)
	}
	return list
}

func (s Set[T]) Remove(elem T) {
	delete(s, elem)
}

func (s Set[T]) Merge(s2 Set[T]) {
	for v := range s2 {
		s[v] = struct{}{}
	}
}

func (s Set[T]) IsDisjoint(s2 Set[T]) bool {
	// Iterate over the smaller set for performance
	small, large := s, s2
	if len(s) > len(s2) {
		small, large = s2, s
	}

	for k := range small {
		if _, ok := large[k]; ok {
			return false
		}
	}
	return true
}

func (s Set[T]) Intersection(s2 Set[T]) Set[T] {
	result := make(Set[T])

	small, large := s, s2
	if len(s) > len(s2) {
		small, large = s2, s
	}

	for k := range small {
		if large.Exists(k) {
			result.Add(k)
		}
	}
	return result
}

func (s Set[T]) IsEmpty() bool {
	return len(s) == 0
}

func (s Set[T]) Difference(s2 Set[T]) Set[T] {
	diff := make(Set[T])
	for k := range s {
		if !s2.Exists(k) {
			diff.Add(k)
		}
	}
	return diff
}
