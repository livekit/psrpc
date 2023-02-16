package registration

type Registerer1[T0 any] struct {
	register   func(T0) error
	deregister func(T0)
}

func NewRegisterer1[T0 any](
	register func(T0) error,
	deregister func(T0),
) Registerer1[T0] {
	return Registerer1[T0]{register, deregister}
}

type RegistererSlice1[T0 any] []Registerer1[T0]

func (rs RegistererSlice1[T0]) Register(t0 T0) error {
	for i, p := range rs {
		if err := p.register(t0); err != nil {
			rs[:i].Deregister(t0)
			return err
		}
	}
	return nil
}

func (rs RegistererSlice1[T0]) Deregister(t0 T0) {
	for _, p := range rs {
		p.deregister(t0)
	}
}

type Registerer2[T0, T1 any] struct {
	register   func(T0, T1) error
	deregister func(T0, T1)
}

func NewRegisterer2[T0, T1 any](
	register func(T0, T1) error,
	deregister func(T0, T1),
) Registerer2[T0, T1] {
	return Registerer2[T0, T1]{register, deregister}
}

type RegistererSlice2[T0, T1 any] []Registerer2[T0, T1]

func (rs RegistererSlice2[T0, T1]) Register(t0 T0, t1 T1) error {
	for i, p := range rs {
		if err := p.register(t0, t1); err != nil {
			rs[:i].Deregister(t0, t1)
			return err
		}
	}
	return nil
}

func (rs RegistererSlice2[T0, T1]) Deregister(t0 T0, t1 T1) {
	for _, p := range rs {
		p.deregister(t0, t1)
	}
}

type Registerer3[T0, T1, T2 any] struct {
	register   func(T0, T1, T2) error
	deregister func(T0, T1, T2)
}

func NewRegisterer3[T0, T1, T2 any](
	register func(T0, T1, T2) error,
	deregister func(T0, T1, T2),
) Registerer3[T0, T1, T2] {
	return Registerer3[T0, T1, T2]{register, deregister}
}

type RegistererSlice3[T0, T1, T2 any] []Registerer3[T0, T1, T2]

func (rs RegistererSlice3[T0, T1, T2]) Register(t0 T0, t1 T1, t2 T2) error {
	for i, p := range rs {
		if err := p.register(t0, t1, t2); err != nil {
			rs[:i].Deregister(t0, t1, t2)
			return err
		}
	}
	return nil
}

func (rs RegistererSlice3[T0, T1, T2]) Deregister(t0 T0, t1 T1, t2 T2) {
	for _, p := range rs {
		p.deregister(t0, t1, t2)
	}
}

type Registerer4[T0, T1, T2, T3 any] struct {
	register   func(T0, T1, T2, T3) error
	deregister func(T0, T1, T2, T3)
}

func NewRegisterer4[T0, T1, T2, T3 any](
	register func(T0, T1, T2, T3) error,
	deregister func(T0, T1, T2, T3),
) Registerer4[T0, T1, T2, T3] {
	return Registerer4[T0, T1, T2, T3]{register, deregister}
}

type RegistererSlice4[T0, T1, T2, T3 any] []Registerer4[T0, T1, T2, T3]

func (rs RegistererSlice4[T0, T1, T2, T3]) Register(t0 T0, t1 T1, t2 T2, t3 T3) error {
	for i, p := range rs {
		if err := p.register(t0, t1, t2, t3); err != nil {
			rs[:i].Deregister(t0, t1, t2, t3)
			return err
		}
	}
	return nil
}

func (rs RegistererSlice4[T0, T1, T2, T3]) Deregister(t0 T0, t1 T1, t2 T2, t3 T3) {
	for _, p := range rs {
		p.deregister(t0, t1, t2, t3)
	}
}

type Registerer5[T0, T1, T2, T3, T4 any] struct {
	register   func(T0, T1, T2, T3, T4) error
	deregister func(T0, T1, T2, T3, T4)
}

func NewRegisterer5[T0, T1, T2, T3, T4 any](
	register func(T0, T1, T2, T3, T4) error,
	deregister func(T0, T1, T2, T3, T4),
) Registerer5[T0, T1, T2, T3, T4] {
	return Registerer5[T0, T1, T2, T3, T4]{register, deregister}
}

type RegistererSlice5[T0, T1, T2, T3, T4 any] []Registerer5[T0, T1, T2, T3, T4]

func (rs RegistererSlice5[T0, T1, T2, T3, T4]) Register(t0 T0, t1 T1, t2 T2, t3 T3, t4 T4) error {
	for i, p := range rs {
		if err := p.register(t0, t1, t2, t3, t4); err != nil {
			rs[:i].Deregister(t0, t1, t2, t3, t4)
			return err
		}
	}
	return nil
}

func (rs RegistererSlice5[T0, T1, T2, T3, T4]) Deregister(t0 T0, t1 T1, t2 T2, t3 T3, t4 T4) {
	for _, p := range rs {
		p.deregister(t0, t1, t2, t3, t4)
	}
}

type Registerer6[T0, T1, T2, T3, T4, T5 any] struct {
	register   func(T0, T1, T2, T3, T4, T5) error
	deregister func(T0, T1, T2, T3, T4, T5)
}

func NewRegisterer6[T0, T1, T2, T3, T4, T5 any](
	register func(T0, T1, T2, T3, T4, T5) error,
	deregister func(T0, T1, T2, T3, T4, T5),
) Registerer6[T0, T1, T2, T3, T4, T5] {
	return Registerer6[T0, T1, T2, T3, T4, T5]{register, deregister}
}

type RegistererSlice6[T0, T1, T2, T3, T4, T5 any] []Registerer6[T0, T1, T2, T3, T4, T5]

func (rs RegistererSlice6[T0, T1, T2, T3, T4, T5]) Register(t0 T0, t1 T1, t2 T2, t3 T3, t4 T4, t5 T5) error {
	for i, p := range rs {
		if err := p.register(t0, t1, t2, t3, t4, t5); err != nil {
			rs[:i].Deregister(t0, t1, t2, t3, t4, t5)
			return err
		}
	}
	return nil
}

func (rs RegistererSlice6[T0, T1, T2, T3, T4, T5]) Deregister(t0 T0, t1 T1, t2 T2, t3 T3, t4 T4, t5 T5) {
	for _, p := range rs {
		p.deregister(t0, t1, t2, t3, t4, t5)
	}
}

type Registerer7[T0, T1, T2, T3, T4, T5, T6 any] struct {
	register   func(T0, T1, T2, T3, T4, T5, T6) error
	deregister func(T0, T1, T2, T3, T4, T5, T6)
}

func NewRegisterer7[T0, T1, T2, T3, T4, T5, T6 any](
	register func(T0, T1, T2, T3, T4, T5, T6) error,
	deregister func(T0, T1, T2, T3, T4, T5, T6),
) Registerer7[T0, T1, T2, T3, T4, T5, T6] {
	return Registerer7[T0, T1, T2, T3, T4, T5, T6]{register, deregister}
}

type RegistererSlice7[T0, T1, T2, T3, T4, T5, T6 any] []Registerer7[T0, T1, T2, T3, T4, T5, T6]

func (rs RegistererSlice7[T0, T1, T2, T3, T4, T5, T6]) Register(t0 T0, t1 T1, t2 T2, t3 T3, t4 T4, t5 T5, t6 T6) error {
	for i, p := range rs {
		if err := p.register(t0, t1, t2, t3, t4, t5, t6); err != nil {
			rs[:i].Deregister(t0, t1, t2, t3, t4, t5, t6)
			return err
		}
	}
	return nil
}

func (rs RegistererSlice7[T0, T1, T2, T3, T4, T5, T6]) Deregister(t0 T0, t1 T1, t2 T2, t3 T3, t4 T4, t5 T5, t6 T6) {
	for _, p := range rs {
		p.deregister(t0, t1, t2, t3, t4, t5, t6)
	}
}

type Registerer8[T0, T1, T2, T3, T4, T5, T6, T7 any] struct {
	register   func(T0, T1, T2, T3, T4, T5, T6, T7) error
	deregister func(T0, T1, T2, T3, T4, T5, T6, T7)
}

func NewRegisterer8[T0, T1, T2, T3, T4, T5, T6, T7 any](
	register func(T0, T1, T2, T3, T4, T5, T6, T7) error,
	deregister func(T0, T1, T2, T3, T4, T5, T6, T7),
) Registerer8[T0, T1, T2, T3, T4, T5, T6, T7] {
	return Registerer8[T0, T1, T2, T3, T4, T5, T6, T7]{register, deregister}
}

type RegistererSlice8[T0, T1, T2, T3, T4, T5, T6, T7 any] []Registerer8[T0, T1, T2, T3, T4, T5, T6, T7]

func (rs RegistererSlice8[T0, T1, T2, T3, T4, T5, T6, T7]) Register(t0 T0, t1 T1, t2 T2, t3 T3, t4 T4, t5 T5, t6 T6, t7 T7) error {
	for i, p := range rs {
		if err := p.register(t0, t1, t2, t3, t4, t5, t6, t7); err != nil {
			rs[:i].Deregister(t0, t1, t2, t3, t4, t5, t6, t7)
			return err
		}
	}
	return nil
}

func (rs RegistererSlice8[T0, T1, T2, T3, T4, T5, T6, T7]) Deregister(t0 T0, t1 T1, t2 T2, t3 T3, t4 T4, t5 T5, t6 T6, t7 T7) {
	for _, p := range rs {
		p.deregister(t0, t1, t2, t3, t4, t5, t6, t7)
	}
}

type Registerer9[T0, T1, T2, T3, T4, T5, T6, T7, T8 any] struct {
	register   func(T0, T1, T2, T3, T4, T5, T6, T7, T8) error
	deregister func(T0, T1, T2, T3, T4, T5, T6, T7, T8)
}

func NewRegisterer9[T0, T1, T2, T3, T4, T5, T6, T7, T8 any](
	register func(T0, T1, T2, T3, T4, T5, T6, T7, T8) error,
	deregister func(T0, T1, T2, T3, T4, T5, T6, T7, T8),
) Registerer9[T0, T1, T2, T3, T4, T5, T6, T7, T8] {
	return Registerer9[T0, T1, T2, T3, T4, T5, T6, T7, T8]{register, deregister}
}

type RegistererSlice9[T0, T1, T2, T3, T4, T5, T6, T7, T8 any] []Registerer9[T0, T1, T2, T3, T4, T5, T6, T7, T8]

func (rs RegistererSlice9[T0, T1, T2, T3, T4, T5, T6, T7, T8]) Register(t0 T0, t1 T1, t2 T2, t3 T3, t4 T4, t5 T5, t6 T6, t7 T7, t8 T8) error {
	for i, p := range rs {
		if err := p.register(t0, t1, t2, t3, t4, t5, t6, t7, t8); err != nil {
			rs[:i].Deregister(t0, t1, t2, t3, t4, t5, t6, t7, t8)
			return err
		}
	}
	return nil
}

func (rs RegistererSlice9[T0, T1, T2, T3, T4, T5, T6, T7, T8]) Deregister(t0 T0, t1 T1, t2 T2, t3 T3, t4 T4, t5 T5, t6 T6, t7 T7, t8 T8) {
	for _, p := range rs {
		p.deregister(t0, t1, t2, t3, t4, t5, t6, t7, t8)
	}
}

type Registerer10[T0, T1, T2, T3, T4, T5, T6, T7, T8, T9 any] struct {
	register   func(T0, T1, T2, T3, T4, T5, T6, T7, T8, T9) error
	deregister func(T0, T1, T2, T3, T4, T5, T6, T7, T8, T9)
}

func NewRegisterer10[T0, T1, T2, T3, T4, T5, T6, T7, T8, T9 any](
	register func(T0, T1, T2, T3, T4, T5, T6, T7, T8, T9) error,
	deregister func(T0, T1, T2, T3, T4, T5, T6, T7, T8, T9),
) Registerer10[T0, T1, T2, T3, T4, T5, T6, T7, T8, T9] {
	return Registerer10[T0, T1, T2, T3, T4, T5, T6, T7, T8, T9]{register, deregister}
}

type RegistererSlice10[T0, T1, T2, T3, T4, T5, T6, T7, T8, T9 any] []Registerer10[T0, T1, T2, T3, T4, T5, T6, T7, T8, T9]

func (rs RegistererSlice10[T0, T1, T2, T3, T4, T5, T6, T7, T8, T9]) Register(t0 T0, t1 T1, t2 T2, t3 T3, t4 T4, t5 T5, t6 T6, t7 T7, t8 T8, t9 T9) error {
	for i, p := range rs {
		if err := p.register(t0, t1, t2, t3, t4, t5, t6, t7, t8, t9); err != nil {
			rs[:i].Deregister(t0, t1, t2, t3, t4, t5, t6, t7, t8, t9)
			return err
		}
	}
	return nil
}

func (rs RegistererSlice10[T0, T1, T2, T3, T4, T5, T6, T7, T8, T9]) Deregister(t0 T0, t1 T1, t2 T2, t3 T3, t4 T4, t5 T5, t6 T6, t7 T7, t8 T8, t9 T9) {
	for _, p := range rs {
		p.deregister(t0, t1, t2, t3, t4, t5, t6, t7, t8, t9)
	}
}
