package types

import "github.com/valyala/fastjson"

// MarshalJSON implements the Marshal interface.
func (t *Transaction) MarshalJSON() ([]byte, error) {
	a := DefaultArena.Get()
	defer a.Reset()

	v := t.marshalJSON(a)
	res := v.MarshalTo(nil)

	DefaultArena.Put(a)

	return res, nil
}

func (t *Transaction) marshalJSON(a *fastjson.Arena) *fastjson.Value {
	return t.Inner.marshalJSON(a)
}
