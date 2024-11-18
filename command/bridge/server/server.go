package server

import (
	"archive/tar"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/signal"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	dockerclient "github.com/docker/docker/client"
	"github.com/docker/docker/pkg/stdcopy"
	"github.com/docker/go-connections/nat"
	"github.com/spf13/cobra"

	"github.com/0xPolygon/polygon-edge/command"
	"github.com/0xPolygon/polygon-edge/command/bridge/helper"
	"github.com/0xPolygon/polygon-edge/helper/common"
)

const (
	gethConsoleImage = "stefanethernal/go-ethereum-console:v0.0.2"
	gethImage        = "ethereum/client-go:v1.9.25"

	defaultHostIP = "127.0.0.1"
	defaultPort   = 8545
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

	cmd.Flags().Uint64Var(
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
	if err := PingServer(closeCh, params.port); err != nil {
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
	var (
		err           error
		webSocketPort = params.port + 1
		authPort      = params.port + 2
	)

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

	dockerfile := fmt.Sprintf("FROM %s\nEXPOSE %d\n", image, params.port)
	buildContext, err := createBuildContext(dockerfile)
	if err != nil {
		return err
	}

	imageName := fmt.Sprintf("geth-external-chain-%d", params.chainID)
	build, err := dockerClient.ImageBuild(ctx, buildContext, types.ImageBuildOptions{
		Tags: []string{imageName},
	})
	if err != nil {
		return err
	}

	defer build.Body.Close()

	if _, err = io.Copy(outputter, build.Body); err != nil {
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

	// set http port value
	args = append(args, "--http.port", strconv.FormatUint(params.port, 10))

	// set websocket port +1 from start port value
	args = append(args, "--ws.port", strconv.FormatUint(webSocketPort, 10))

	// set authrpc port +2 from start port value
	args = append(args, "--authrpc.port", strconv.FormatUint(authPort, 10))

	config := &container.Config{
		Image: imageName,
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
			return err
		} else {
			mountDir = filepath.Join(pwdDir, params.dataDir)
		}
	}

	port := nat.Port(fmt.Sprintf("%d/tcp", params.port))
	hostConfig := &container.HostConfig{
		Binds: []string{
			mountDir + fmt.Sprintf(":%s", folderName),
		},
		PortBindings: nat.PortMap{
			port: []nat.PortBinding{
				{
					HostIP:   defaultHostIP,
					HostPort: strconv.FormatUint(params.port, 10),
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

// createBuildContext creates a tar archive with the Dockerfile content, which is used as the build context for the image,
// after that it removes temporary directory
func createBuildContext(dockerfileContent string) (io.Reader, error) {
	tmpDir := "./temp-build-context"
	if err := os.MkdirAll(tmpDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create temporary directory: %w", err)
	}

	defer os.RemoveAll(tmpDir)

	dockerfilePath := fmt.Sprintf("%s/Dockerfile", tmpDir)
	if err := os.WriteFile(dockerfilePath, []byte(dockerfileContent), 0600); err != nil {
		return nil, fmt.Errorf("failed to write Dockerfile: %w", err)
	}

	// Create the tar archive in memory
	var buf bytes.Buffer
	tarWriter := tar.NewWriter(&buf)

	// Add the Dockerfile to the tar archive
	fileInfo := &tar.Header{
		Name: "Dockerfile",
		Mode: 0600,
		Size: int64(len(dockerfileContent)),
	}

	if err := tarWriter.WriteHeader(fileInfo); err != nil {
		return nil, fmt.Errorf("failed to write tar header: %w", err)
	}

	if _, err := tarWriter.Write([]byte(dockerfileContent)); err != nil {
		return nil, fmt.Errorf("failed to write Dockerfile content: %w", err)
	}

	if err := tarWriter.Close(); err != nil {
		return nil, fmt.Errorf("failed to close tar writer: %w", err)
	}

	return &buf, nil
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

func PingServer(closeCh <-chan struct{}, port uint64) error {
	httpTimer := time.NewTimer(30 * time.Second)
	httpClient := http.Client{
		Timeout: 5 * time.Second,
	}

	for {
		select {
		case <-time.After(500 * time.Millisecond):
			resp, err := httpClient.Post(fmt.Sprintf("http://%s:%d", defaultHostIP, port), "application/json", nil)
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
