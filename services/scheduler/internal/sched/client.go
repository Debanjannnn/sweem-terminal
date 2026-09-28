package sched

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"

	"github.com/snehendu098/sweem-terminal/internal/httpx"
)

// Executor calls the executor's internal user routes.
type Executor struct {
	URL, Secret string
	HTTP        *http.Client
}

type ExecRequest struct {
	ExecutionID string `json:"executionId"`
}

type ExecResponse struct {
	ExecutionID string `json:"executionId"`
	Status      string `json:"status"`
	TxHash      string `json:"txHash"`
}

type ExecError struct {
	Status int
	Code   string `json:"error"`
}

func (e *ExecError) Error() string { return fmt.Sprintf("executor %d: %s", e.Status, e.Code) }

// Call POSTs {executionId} to /api/v1/user/{op}, op = mint|redeem.
func (x Executor) Call(ctx context.Context, op, executionID string) (ExecResponse, error) {
	var out ExecResponse
	body, _ := json.Marshal(ExecRequest{executionID})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, x.URL+"/api/v1/user/"+op, bytes.NewReader(body))
	if err != nil {
		return out, err
	}
	req.Header.Set("Content-Type", "application/json")
	httpx.SetInternal(req, x.Secret, "scheduler")
	res, err := x.HTTP.Do(req)
	if err != nil {
		return out, err
	}
	defer res.Body.Close()
	if res.StatusCode/100 != 2 {
		e := &ExecError{Status: res.StatusCode}
		_ = json.NewDecoder(res.Body).Decode(e)
		return out, e
	}
	return out, json.NewDecoder(res.Body).Decode(&out)
}

// Quotes reads suggested min out from market-data.
type Quotes struct {
	URL  string
	HTTP *http.Client
}

type quote struct {
	MinOut string `json:"minOut"`
	Stale  bool   `json:"stale"`
}

// MinOut returns an error when the quote is unavailable or stale; the caller skips the tick.
func (q Quotes) MinOut(ctx context.Context, basket, side, amount string, slippageBps int) (string, error) {
	u := fmt.Sprintf("%s/api/v1/quote/%s?%s", q.URL, basket, url.Values{
		"side": {side}, "amount": {amount}, "slippageBps": {fmt.Sprint(slippageBps)},
	}.Encode())
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return "", err
	}
	res, err := q.HTTP.Do(req)
	if err != nil {
		return "", err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return "", fmt.Errorf("quote %s: status %d", basket, res.StatusCode)
	}
	var v quote
	if err := json.NewDecoder(res.Body).Decode(&v); err != nil {
		return "", err
	}
	if v.Stale || v.MinOut == "" {
		return "", fmt.Errorf("quote %s: stale", basket)
	}
	return v.MinOut, nil
}
