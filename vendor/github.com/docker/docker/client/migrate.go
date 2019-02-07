package client

// MATT ADDED THIS FILE

import (
	"context"
	"encoding/json"

	"github.com/docker/docker/api/types/container"
)

func (cli *Client) CreatePageServer(ctx context.Context, containerID string) (container.CreatePageServerBody, error) {
	var response container.CreatePageServerBody

	serverResp, err := cli.get(ctx, "/containers/"+containerID+"/createpageserver", nil, nil)

	if err != nil {
		return response, err
	}

	err = json.NewDecoder(serverResp.body).Decode(&response)
	ensureReaderClosed(serverResp)

	return response, err
}

func (cli *Client) StartIter(ctx context.Context, containerID string) error {

	serverResp, err := cli.get(ctx, "/containers/"+containerID+"/startiter", nil, nil)

	ensureReaderClosed(serverResp)
	return err
}

func (cli *Client) StopIter(ctx context.Context, containerID string) error {

	serverResp, err := cli.get(ctx, "/containers/"+containerID+"/stopiter", nil, nil)

	ensureReaderClosed(serverResp)
	return err
}
