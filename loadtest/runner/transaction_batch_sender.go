package runner

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/0xPolygon/polygon-edge/types"
	"github.com/umbracle/ethgo/jsonrpc/codec"
	"github.com/valyala/fasthttp"
)

// TransactionBatchSender is an http transport for sending transactions in a batch
type TransactionBatchSender struct {
	addr   string
	client *fasthttp.Client
}

// newTransactionBatchSender creates a new TransactionBatchSender instance with the given address
func newTransactionBatchSender(addr string) *TransactionBatchSender {
	return &TransactionBatchSender{
		addr: addr,
		client: &fasthttp.Client{
			DialDualStack: true,
		},
	}
}

// SendBatch implements sends transactions in a batch
func (h *TransactionBatchSender) SendBatch(params []string) ([]types.Hash, error) {
	if len(params) == 0 {
		return nil, nil
	}

	var requests = make([]codec.Request, 0, len(params))

	for i, param := range params {
		request := codec.Request{
			JsonRPC: "2.0",
			Method:  "eth_sendRawTransaction",
			ID:      uint64(i),
		}

		data, err := json.Marshal([]string{param})
		if err != nil {
			return nil, err
		}

		request.Params = data
		requests = append(requests, request)
	}

	raw, err := json.Marshal(requests)
	if err != nil {
		return nil, err
	}

	req := fasthttp.AcquireRequest()
	res := fasthttp.AcquireResponse()

	defer fasthttp.ReleaseRequest(req)
	defer fasthttp.ReleaseResponse(res)

	req.SetRequestURI(h.addr)
	req.Header.SetMethod("POST")
	req.Header.SetContentType("application/json")
	req.SetBody(raw)

	if err := h.client.Do(req, res); err != nil {
		return nil, err
	}

	if sc := res.StatusCode(); sc != fasthttp.StatusOK {
		return nil, fmt.Errorf("status code is %d. response = %s", sc, string(res.Body()))
	}

	// Decode json-rpc response
	var responses []*codec.Response
	if err := json.Unmarshal(res.Body(), &responses); err != nil {
		return nil, err
	}

	txHashes := make([]types.Hash, 0, len(responses))

	for _, response := range responses {
		if response.Error != nil {
			return nil, fmt.Errorf("error: %w", response.Error)
		}

		txHashes = append(txHashes, types.StringToHash(strings.Trim(string(response.Result), "\"")))
	}

	return txHashes, nil
}
