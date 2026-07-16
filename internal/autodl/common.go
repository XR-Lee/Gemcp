package autodl

import (
	"context"
	"net/http"
)

func (c *Client) WalletBalance(ctx context.Context) (WalletBalance, string, error) {
	return doJSON[WalletBalance](ctx, c, http.MethodPost, "/api/v1/dev/wallet/balance", nil, requestOptions{idempotent: true})
}
