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

func (c *Client) ProCreateInstance(ctx context.Context, spec ProInstanceCreate) (ProInstance, string, error) {
	return doJSON[ProInstance](ctx, c, http.MethodPost, "/api/v1/dev/instance/pro/create", spec, requestOptions{})
}

func (c *Client) ProStopInstance(ctx context.Context, instanceUUID string) (string, error) {
	_, requestID, err := doJSON[map[string]any](ctx, c, http.MethodPost, "/api/v1/dev/instance/pro/stop", map[string]string{
		"instance_uuid": instanceUUID,
	}, requestOptions{})
	return requestID, err
}

func (c *Client) ProSaveImage(ctx context.Context, spec ProImageSave) (Image, string, error) {
	return doJSON[Image](ctx, c, http.MethodPost, "/api/v1/dev/instance/pro/image/save", spec, requestOptions{})
}
