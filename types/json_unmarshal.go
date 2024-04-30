package types

import (
	"github.com/valyala/fastjson"
)

// UnmarshalJSON implements the unmarshal interface
func (b *Block) UnmarshalJSON(buf []byte) error {
	p := DefaultPool.Get()
	defer DefaultPool.Put(p)

	v, err := p.Parse(string(buf))
	if err != nil {
		return err
	}

	// header
	b.Header = new(Header)

	if err := b.Header.unmarshalJSON(v); err != nil {
		return err
	}

	// transactions
	b.Transactions = b.Transactions[:0]

	elems := v.GetArray("transactions")
	if len(elems) != 0 && elems[0].Type() != fastjson.TypeString {
		for _, elem := range elems {
			txn := new(Transaction)
			if err := txn.UnmarshalJSONWith(elem); err != nil {
				return err
			}

			b.Transactions = append(b.Transactions, txn)
		}
	}

	// uncles
	b.Uncles = b.Uncles[:0]

	uncles := v.GetArray("uncles")
	if len(uncles) != 0 && uncles[0].Type() != fastjson.TypeString {
		for _, elem := range uncles {
			h := new(Header)
			if err := h.unmarshalJSON(elem); err != nil {
				return err
			}

			b.Uncles = append(b.Uncles, h)
		}
	}

	return nil
}

func (h *Header) UnmarshalJSON(buf []byte) error {
	p := DefaultPool.Get()
	defer DefaultPool.Put(p)

	v, err := p.Parse(string(buf))
	if err != nil {
		return err
	}

	return h.unmarshalJSON(v)
}

func (h *Header) unmarshalJSON(v *fastjson.Value) error {
	var err error

	if h.Hash, err = UnmarshalJSONHash(v, "hash"); err != nil {
		return err
	}

	if h.ParentHash, err = UnmarshalJSONHash(v, "parentHash"); err != nil {
		return err
	}

	if h.Sha3Uncles, err = UnmarshalJSONHash(v, "sha3Uncles"); err != nil {
		return err
	}

	if h.TxRoot, err = UnmarshalJSONHash(v, "transactionsRoot"); err != nil {
		return err
	}

	if h.StateRoot, err = UnmarshalJSONHash(v, "stateRoot"); err != nil {
		return err
	}

	if h.ReceiptsRoot, err = UnmarshalJSONHash(v, "receiptsRoot"); err != nil {
		return err
	}

	if h.Miner, err = UnmarshalJSONBytes(v, "miner"); err != nil {
		return err
	}

	if h.Number, err = UnmarshalJSONUint64(v, "number"); err != nil {
		return err
	}

	if h.GasLimit, err = UnmarshalJSONUint64(v, "gasLimit"); err != nil {
		return err
	}

	if h.GasUsed, err = UnmarshalJSONUint64(v, "gasUsed"); err != nil {
		return err
	}

	if h.MixHash, err = UnmarshalJSONHash(v, "mixHash"); err != nil {
		return err
	}

	if err = UnmarshalJSONNonce(&h.Nonce, v, "nonce"); err != nil {
		return err
	}

	if h.Timestamp, err = UnmarshalJSONUint64(v, "timestamp"); err != nil {
		return err
	}

	if h.Difficulty, err = UnmarshalJSONUint64(v, "difficulty"); err != nil {
		return err
	}

	if h.ExtraData, err = UnmarshalJSONBytes(v, "extraData"); err != nil {
		return err
	}

	if h.BaseFee, err = UnmarshalJSONUint64(v, "baseFee"); err != nil {
		if err.Error() != "field 'baseFee' not found" {
			return err
		}
	}

	return nil
}

// UnmarshalJSON implements the unmarshal interface
func (t *Transaction) UnmarshalJSON(buf []byte) error {
	p := DefaultPool.Get()
	defer DefaultPool.Put(p)

	v, err := p.Parse(string(buf))
	if err != nil {
		return err
	}

	return t.UnmarshalJSONWith(v)
}

func (t *Transaction) UnmarshalJSONWith(v *fastjson.Value) error {
	if HasJSONKey(v, "type") {
		txnType, err := UnmarshalJSONUint64(v, "type")
		if err != nil {
			return err
		}

		t.InitInnerData(TxType(txnType))
	} else {
		if HasJSONKey(v, "chainId") {
			if HasJSONKey(v, "maxFeePerGas") {
				t.InitInnerData(DynamicFeeTxType)
			} else {
				t.InitInnerData(AccessListTxType)
			}
		} else {
			t.InitInnerData(LegacyTxType)
		}
	}

	return t.Inner.unmarshalJSON(v)
}

// UnmarshalJSON implements the unmarshal interface
func (r *Receipt) UnmarshalJSON(buf []byte) error {
	p := DefaultPool.Get()
	defer DefaultPool.Put(p)

	v, err := p.Parse(string(buf))
	if err != nil {
		return nil
	}

	if HasJSONKey(v, "contractAddress") {
		contractAddr, err := UnmarshalJSONAddr(v, "contractAddress")
		if err != nil {
			return err
		}

		r.ContractAddress = &contractAddr
	}

	if r.TxHash, err = UnmarshalJSONHash(v, "transactionHash"); err != nil {
		return err
	}

	if r.GasUsed, err = UnmarshalJSONUint64(v, "gasUsed"); err != nil {
		return err
	}

	if r.CumulativeGasUsed, err = UnmarshalJSONUint64(v, "cumulativeGasUsed"); err != nil {
		return err
	}

	if err = UnmarshalJSONBloom(&r.LogsBloom, v, "logsBloom"); err != nil {
		return err
	}

	if r.Root, err = UnmarshalJSONHash(v, "root"); err != nil {
		return err
	}

	if HasJSONKey(v, "status") {
		// post-byzantium fork
		status, err := UnmarshalJSONUint64(v, "status")
		if err != nil {
			return err
		}

		r.SetStatus(ReceiptStatus(status))
	}

	// logs
	r.Logs = r.Logs[:0]

	for _, elem := range v.GetArray("logs") {
		log := new(Log)
		if err := log.unmarshalJSON(elem); err != nil {
			return err
		}

		r.Logs = append(r.Logs, log)
	}

	return nil
}

// UnmarshalJSON implements the unmarshal interface
func (l *Log) UnmarshalJSON(buf []byte) error {
	p := DefaultPool.Get()
	defer DefaultPool.Put(p)

	v, err := p.Parse(string(buf))
	if err != nil {
		return nil
	}

	return l.unmarshalJSON(v)
}

func (l *Log) unmarshalJSON(v *fastjson.Value) error {
	var err error

	if l.Address, err = UnmarshalJSONAddr(v, "address"); err != nil {
		return err
	}

	if l.Data, err = UnmarshalJSONBytes(v, "data"); err != nil {
		return err
	}

	l.Topics = l.Topics[:0]

	for _, topic := range v.GetArray("topics") {
		b, err := topic.StringBytes()
		if err != nil {
			return err
		}

		var t Hash
		if err := t.UnmarshalText(b); err != nil {
			return err
		}

		l.Topics = append(l.Topics, t)
	}

	return nil
}
