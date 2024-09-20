package server

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/docker/docker/api/types/container"
	dockerImg "github.com/docker/docker/api/types/image"
	dockerclient "github.com/docker/docker/client"
	"github.com/docker/docker/pkg/stdcopy"
	"github.com/docker/go-connections/nat"
	"github.com/spf13/cobra"

	"github.com/0xPolygon/polygon-edge/command"
	"github.com/0xPolygon/polygon-edge/command/bridge/helper"
	"github.com/0xPolygon/polygon-edge/helper/common"
)

const (
	gethConsoleImage = "ghcr.io/0xpolygon/go-ethereum-console:latest"
	gethImage        = "ethereum/client-go:v1.9.25"

	defaultHostIP = "127.0.0.1"
	defaultPort   = "8545"
)

var (
	params            serverParams
	dockerClient      *dockerclient.Client
	dockerContainerID string
)

// GetCommand returns the bridge server command
func GetCommand() *cobra.Command {
	externalChainServerCmd := &cobra.Command{
		Use:     "server",
		Short:   "Start the external chain command",
		PreRunE: runPreRun,
		Run:     runCommand,
	}

	setFlags(externalChainServerCmd)

	return externalChainServerCmd
}

func setFlags(cmd *cobra.Command) {
	cmd.Flags().StringVar(
		&params.dataDir,
		dataDirFlag,
		"test-external-chain",
		"target directory for the chain",
	)

	cmd.Flags().BoolVar(
		&params.noConsole,
		noConsole,
		false,
		"use the official geth image instead of the console fork",
	)

	cmd.Flags().Uint64Var(
		&params.chainID,
		"chain-id",
		101,
		"custom chain id for external chain",
	)

	cmd.Flags().StringVar(
		&params.port,
		"port",
		defaultPort,
		"port for external chain",
	)
}

func runPreRun(_ *cobra.Command, _ []string) error {
	return nil
}

func runCommand(cmd *cobra.Command, _ []string) {
	ctx := cmd.Context()

	outputter := command.InitializeOutputter(cmd)
	defer outputter.WriteOutput()

	closeCh := make(chan struct{})

	// Check if the client is already running
	if cid, err := helper.GetBridgeChainID(); !errors.Is(err, helper.ErrExternalChainNotFound) {
		if err != nil {
			outputter.SetError(err)
		} else if cid != "" {
			outputter.SetError(fmt.Errorf("external chain already running: %s", cid))
		}

		return
	}

	// Start the client
	if err := runExternalChain(ctx, outputter, closeCh); err != nil {
		outputter.SetError(fmt.Errorf("failed to run external chain: %w", err))

		return
	}

	// Ping geth server to make sure everything is up and running
	if err := PingServer(closeCh); err != nil {
		close(closeCh)

		if ip, err := helper.ReadBridgeChainIP(params.port); err != nil {
			outputter.SetError(fmt.Errorf("failed to ping external chain server: %w", err))
		} else {
			outputter.SetError(fmt.Errorf("failed to ping external chain server at address %s: %w", ip, err))
		}

		return
	}

	// Gather the logs
	go func() {
		if err := gatherLogs(ctx, outputter); err != nil {
			outputter.SetError(fmt.Errorf("failed to gether logs: %w", err))

			return
		}
	}()

	if err := handleSignals(ctx, closeCh); err != nil {
		outputter.SetError(fmt.Errorf("failed to handle signals: %w", err))
	}
}

func runExternalChain(ctx context.Context, outputter command.OutputFormatter, closeCh chan struct{}) error {
	var err error
	if dockerClient, err = dockerclient.NewClientWithOpts(dockerclient.FromEnv,
		dockerclient.WithAPIVersionNegotiation()); err != nil {
		return err
	}

	// target directory for the chain
	if err = common.CreateDirSafe(params.dataDir, 0700); err != nil {
		return err
	}

	image := gethConsoleImage
	if params.noConsole {
		image = gethImage
	}

	// try to pull the image
	reader, err := dockerClient.ImagePull(ctx, image, dockerImg.PullOptions{})
	if err != nil {
		return err
	}
	defer reader.Close()

	if _, err = io.Copy(outputter, reader); err != nil {
		return fmt.Errorf("cannot copy: %w", err)
	}

	folderName := fmt.Sprintf("/ethdata_%d", params.chainID)

	// create the client
	args := []string{"--dev"}

	// add period of 2 seconds
	args = append(args, "--dev.period", "2")

	// add data dir
	args = append(args, "--datadir", folderName)

	// add ipcpath
	args = append(args, "--ipcpath", path.Join(folderName, "geth.ipc"))

	// enable rpc
	args = append(args, "--http", "--http.addr", "0.0.0.0", "--http.api", "eth,net,web3,debug")

	// enable ws
	args = append(args, "--ws", "--ws.addr", "0.0.0.0")

	// set chain id
	args = append(args, "--networkid", fmt.Sprintf("%d", params.chainID))

	config := &container.Config{
		Image: image,
		Cmd:   args,
		Labels: map[string]string{
			"edge-type": "external-chain",
		},
	}

	mountDir := params.dataDir

	// we need to use the full path
	if !strings.HasPrefix(params.dataDir, "/") {
		// if the path is not absolute, assume we want to create it locally
		// in current folder
		pwdDir, err := os.Getwd()
		if err != nil {
			log.Fatal(err)
		} else {
			mountDir = filepath.Join(pwdDir, params.dataDir)
		}
	}

	port := nat.Port(fmt.Sprintf("%s/tcp", params.port))
	hostConfig := &container.HostConfig{
		Binds: []string{
			mountDir + fmt.Sprintf(":%s", folderName),
		},
		PortBindings: nat.PortMap{
			port: []nat.PortBinding{
				{
					HostIP:   defaultHostIP,
					HostPort: params.port,
				},
			},
		},
	}

	resp, err := dockerClient.ContainerCreate(ctx, config, hostConfig, nil, nil, "")
	if err != nil {
		return err
	}

	// start the client
	if err = dockerClient.ContainerStart(ctx, resp.ID, container.StartOptions{}); err != nil {
		return err
	}

	dockerContainerID = resp.ID

	// wait for it to finish
	go func() {
		statusCh, errCh := dockerClient.ContainerWait(ctx, dockerContainerID, container.WaitConditionNotRunning)
		select {
		case err = <-errCh:
			outputter.SetError(err)
		case status := <-statusCh:
			outputter.SetCommandResult(newContainerStopResult(status))
		}
		close(closeCh)
	}()

	return nil
}

func gatherLogs(ctx context.Context, outputter command.OutputFormatter) error {
	opts := container.LogsOptions{
		ShowStderr: true,
		ShowStdout: true,
		Follow:     true,
	}

	out, err := dockerClient.ContainerLogs(ctx, dockerContainerID, opts)
	if err != nil {
		return fmt.Errorf("failed to retrieve container logs: %w", err)
	}

	if _, err = stdcopy.StdCopy(outputter, outputter, out); err != nil {
		return fmt.Errorf("failed to write container logs to the stdout: %w", err)
	}

	return nil
}

func PingServer(closeCh <-chan struct{}) error {
	httpTimer := time.NewTimer(30 * time.Second)
	httpClient := http.Client{
		Timeout: 5 * time.Second,
	}

	for {
		select {
		case <-time.After(500 * time.Millisecond):
			resp, err := httpClient.Post(fmt.Sprintf("http://%s:%s", defaultHostIP, params.port), "application/json", nil)
			if err == nil {
				return resp.Body.Close()
			}
		case <-httpTimer.C:
			return fmt.Errorf("timeout to start http")
		case <-closeCh:
			return fmt.Errorf(
				"closed before connecting with http. Is there any other process running and using external chain dir?")
		}
	}
}

func handleSignals(ctx context.Context, closeCh <-chan struct{}) error {
	signalCh := make(chan os.Signal, 4)
	signal.Notify(signalCh, os.Interrupt, syscall.SIGTERM, syscall.SIGHUP)

	stop := true
	select {
	case <-signalCh:
	case <-closeCh:
		stop = false
	}

	// close the container if possible
	if stop {
		if err := dockerClient.ContainerStop(ctx, dockerContainerID, container.StopOptions{}); err != nil {
			return fmt.Errorf("failed to stop container: %w", err)
		}
	}

	return nil
}
