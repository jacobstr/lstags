package client

import (
	"io"
	"io/ioutil"
	"time"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/go-connections/nat"
	"github.com/moby/moby/client"

	"golang.org/x/net/context"

	"github.com/ivanilves/lstags/docker/config"
	"github.com/ivanilves/lstags/repository"
)

// DockerSocket is a socket we use to connect to the Docker daemon
var DockerSocket = "/var/run/docker.sock"

// RetryPulls is a number of retries we do in case of Docker pull failure
var RetryPulls = 0

// RetryDelay is a delay between retries of the failed Docker pulls
var RetryDelay = 5 * time.Second

// DockerClient is a raw Docker client convenience wrapper
type DockerClient struct {
	cli *client.Client
	cnf *config.Config
}

// New creates new instance of DockerClient (our Docker client wrapper)
// Use DOCKER_HOST to set the URL to the Docker server.
// This depends on the operating system: for Linux unix:///var/run/docker.sock and for Windows npipe:////./pipe/docker_engine
// Use DOCKER_API_VERSION to set the version of the API to reach, leave empty for latest.
// API_VERSION is by default 1.27 (this may change)
// Use DOCKER_CERT_PATH to load the TLS certificates from.
// DOCKER_CERT_PATH/ca.pem
// DOCKER_CERT_PATH/cert.pem
// DOCKER_CERT_PATH/key.pem
// Use DOCKER_TLS_VERIFY to enable or disable TLS verification, off by default.
func New(cnf *config.Config) (*DockerClient, error) {
	cli, err := client.NewEnvClient()
	if err != nil {
		return nil, err
	}

	return &DockerClient{cli: cli, cnf: cnf}, nil
}

// Config returns Docker client configuration
func (dc *DockerClient) Config() *config.Config {
	return dc.cnf
}

// ListImagesForRepo lists images present locally for the repo specified
func (dc *DockerClient) ListImagesForRepo(repo string) ([]types.ImageSummary, error) {
	listOptions, err := buildImageListOptions(repo)
	if err != nil {
		return nil, err
	}

	return dc.cli.ImageList(context.Background(), listOptions)
}

func buildImageListOptions(repo string) (types.ImageListOptions, error) {
	repoFilter := "reference=" + repo
	filterArgs := filters.NewArgs()

	filterArgs, err := filters.ParseFlag(repoFilter, filterArgs)
	if err != nil {
		return types.ImageListOptions{}, err
	}

	return types.ImageListOptions{Filters: filterArgs}, nil
}

// Pull pulls Docker image specified
func (dc *DockerClient) Pull(ref string) error {
	registryAuth := dc.cnf.GetRegistryAuth(
		repository.GetRegistry(ref),
	)

	pullOptions := types.ImagePullOptions{RegistryAuth: registryAuth}
	if registryAuth == "" {
		pullOptions = types.ImagePullOptions{}
	}

	tries := 1

	if RetryPulls > 0 {
		tries = tries + RetryPulls
	}

	var resp io.ReadCloser
	var err error

	for try := 1; try <= tries; try++ {
		resp, err = dc.cli.ImagePull(context.Background(), ref, pullOptions)

		if err == nil {
			break
		}

		time.Sleep(RetryDelay)

		RetryDelay += RetryDelay
	}

	if err != nil {
		return err
	}

	_, err = ioutil.ReadAll(resp)

	return err
}

// Push pushes Docker image specified
func (dc *DockerClient) Push(ref string) error {
	registryAuth := dc.cnf.GetRegistryAuth(
		repository.GetRegistry(ref),
	)

	pushOptions := types.ImagePushOptions{RegistryAuth: registryAuth}
	if registryAuth == "" {
		pushOptions = types.ImagePushOptions{RegistryAuth: "IA=="}
	}

	resp, err := dc.cli.ImagePush(context.Background(), ref, pushOptions)
	if err != nil {
		return err
	}

	_, err = ioutil.ReadAll(resp)

	return err
}

// Tag puts a "dst" tag on "src" Docker image
func (dc *DockerClient) Tag(src, dst string) error {
	return dc.cli.ImageTag(context.Background(), src, dst)
}

// RePush pulls, tags and re-pushes given image references
func (dc *DockerClient) RePush(src, dst string) error {
	if err := dc.Pull(src); err != nil {
		return err
	}

	if err := dc.Tag(src, dst); err != nil {
		return err
	}

	return dc.Push(dst)
}

// Run runs Docker container from the image specified (like "docker run")
func (dc *DockerClient) Run(ref, name string, portSpecs []string) (string, error) {
	exposedPorts, portBindings, err := nat.ParsePortSpecs(portSpecs)
	if err != nil {
		return "", err
	}

	ctx := context.Background()

	if err := dc.Pull(ref); err != nil {
		return "", err
	}

	resp, err := dc.cli.ContainerCreate(
		ctx,
		&container.Config{Image: ref, ExposedPorts: exposedPorts},
		&container.HostConfig{PortBindings: portBindings},
		nil,
		name,
	)
	if err != nil {
		return "", err
	}

	if err := dc.cli.ContainerStart(ctx, resp.ID, types.ContainerStartOptions{}); err != nil {
		return "", err
	}

	return resp.ID, nil
}

// ForceRemove kills & removes Docker container having the ID specified (like "docker rm -f")
func (dc *DockerClient) ForceRemove(id string) error {
	return dc.cli.ContainerRemove(
		context.Background(),
		id,
		types.ContainerRemoveOptions{Force: true},
	)
}
