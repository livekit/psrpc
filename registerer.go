package psrpc

import "reflect"

type Registerer struct {
	register   any
	deregister any
}

func NewRegisterer(register, deregister any) Registerer {
	return Registerer{register, deregister}
}

func anySliceReflectValues(anys []any) []reflect.Value {
	vals := make([]reflect.Value, len(anys))
	for i, a := range anys {
		vals[i] = reflect.ValueOf(a)
	}
	return vals
}

type RegistererSlice []Registerer

func (rs RegistererSlice) Register(params ...any) error {
	paramVals := anySliceReflectValues(params)
	for i, p := range rs {
		res := reflect.ValueOf(p.register).Call(paramVals)
		if !res[0].IsNil() {
			rs[:i].Deregister(params)
			return res[0].Interface().(error)
		}
	}
	return nil
}

func (rs RegistererSlice) Deregister(params ...any) {
	paramVals := anySliceReflectValues(params)
	for _, p := range rs {
		reflect.ValueOf(p.deregister).Call(paramVals)
	}
}
