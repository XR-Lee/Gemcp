package autodl

import (
	"context"
	"net/http"
)

func (c *Client) ProInstances(ctx context.Context, pageIndex, pageSize int) (Page[ProInstance], string, error) {
	return doJSON[Page[ProInstance]](ctx, c, http.MethodPost, "/api/v1/dev/instance/pro/list", map[string]int{
		"page_index": pageIndex,
		"page_size":  pageSize,
	}, requestOptions{idempotent: true})
}

func (c *Client) ProImages(ctx context.Context, pageIndex, pageSize int) (Page[Image], string, error) {
	return doJSON[Page[Image]](ctx, c, http.MethodPost, "/api/v1/dev/instance/pro/image/private/list", map[string]int{
		"page_index": pageIndex,
		"page_size":  pageSize,
	}, requestOptions{idempotent: true})
}
