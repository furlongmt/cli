package container

// MATT ADDED THIS WHOLE FILE

import (
	"context"
	"fmt"
	"io/ioutil"
	"math/rand"
	"net"
	"strings"

	"github.com/docker/cli/cli"
	"github.com/docker/cli/cli/command"
	"github.com/docker/cli/cli/debug"
	"github.com/docker/docker/api/types"
	"github.com/docker/docker/client"

	//cliflags "github.com/docker/cli/cli/flags"
	//"github.com/docker/cli/internal/containerizedengine"
	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"

	//"github.com/checkpoint-restore/criu/lib/go/src/criu"
	//"github.com/checkpoint-restore/criu/phaul/src/phaul"
	criu "github.com/checkpoint-restore/go-criu"
	"github.com/checkpoint-restore/go-criu/phaul"
)

type migrateOptions struct {
	containerId string
	destAddress string
}

type ctrLocal struct {
	ctx         context.Context
	containerID string
	localClient client.APIClient
	destClient  client.APIClient
}

type ctrRemote struct {
	ctx         context.Context
	containerID string
	client      client.APIClient
}

func (r *ctrRemote) StartIter() error {
	logrus.Debugf("Start iter() called")
	err := r.client.StartIter(r.ctx, r.containerID)
	logrus.Debugf("Start iter succeeded")
	return err
}

func (r *ctrRemote) StopIter() error {
	logrus.Debugf("Stop iter() called")
	err := r.client.StopIter(r.ctx, r.containerID)
	logrus.Debugf("Stop iter() succeeded")
	return err
}

func (r *ctrRemote) DoRestore() error {
	logrus.Debugf("DoRestore() called")
	return nil
}

func (r *ctrRemote) PostDump() error {
	logrus.Debugf("PostDump() called")
	return nil
}

// from https://medium.com/@kpbird/golang-generate-fixed-size-random-string-dd6dbd5e63c0
func RandomString(len int) string {
	bytes := make([]byte, len)
	for i := 0; i < len; i++ {
		bytes[i] = byte(65 + rand.Intn(25)) //A=65 and Z = 65+25
	}
	return string(bytes)
}

func (l *ctrLocal) DumpCopyRestore(cr *criu.Criu, cfg phaul.Config, last_cln_images_dir string) error {
	logrus.Debugf("DumpCopyRestore() called")

	checkpointId := RandomString(10)
	checkpointDir := "/tmp/docker_ckpts"

	logrus.Debugf("Checkpointing to " + checkpointDir + "/" + checkpointId)

	// todo: what to call these checkpoints?
	checkpointOpts := types.CheckpointCreateOptions{
		PageServer:    fmt.Sprintf("%s:%d", cfg.Addr, cfg.Port),
		ParentPath:    last_cln_images_dir,
		CheckpointID:  checkpointId,
		CheckpointDir: checkpointDir,

		// TODO: both of these options should be configurable
		Exit:    true,
		OpenTcp: true,
	}

	err := l.localClient.CheckpointCreate(context.Background(), l.containerID, checkpointOpts)
	if err != nil {
		logrus.Error("Failed to checkpoint the container!")
		return err
	}

	logrus.Debugf("Successfully checkpointed the container!")

	// TODO: now we'll just transfer the files to other host
	// TODO: we should also transfer the root fs here
	// use the phaul fs library to transfer the checkpoints
	checkpointLib := checkpointDir + "/" + checkpointId
	phaulFS, err := phaul.MakeFS([]string{checkpointLib}, cfg.Addr)
	if err != nil {
		fmt.Printf("Couldn't make phaul fs\n")
		return err
	}

	err = phaulFS.Migrate() // using rsync here!
	if err != nil {
		fmt.Printf("Failed to migrate checkpoint dirs!\n")
		return err
	}

	// now we need to create the container on the other daemon
	c, err := l.localClient.ContainerInspect(context.Background(), l.containerID)
	if err != nil {
		logrus.Debugf("Container inspect failed!")
	}

	container, err := l.destClient.ContainerCreate(context.Background(), c.Config, c.HostConfig, nil, c.Name)
	if err != nil {
		return err
	}

	// now resume the container on the other daemon
	//var startOptions types.ContainerStartOptions
	startOptions := types.ContainerStartOptions{
		CheckpointID:  checkpointId,
		CheckpointDir: checkpointDir,
	}
	err = l.destClient.ContainerStart(context.Background(), container.ID, startOptions)
	if err != nil {
		logrus.Error("Failed to start container!")
		return err
	}

	logrus.Debugf("Finished DumpCopyRestore")

	return nil
}

// NewMigrateCommand creates a new cobra.Command for 'docker migrate'
func NewMigrateCommand(dockerCli command.Cli) *cobra.Command {
	var opts migrateOptions

	cmd := &cobra.Command{
		Use:   "migrate [OPTIONS] CONTAINER [CONTAINER...] DESTINATION-ADDRESS [DEST_ADDR]",
		Short: "Migrate a running container to destination address",
		Args:  cli.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			debug.Enable()
			opts.containerId = args[0]
			opts.destAddress = args[1]
			logrus.Debugf("The migrate command has been called")
			return migrateContainer(dockerCli, &opts)
		},
	}

	// no flags currently
	//flags := cmd.Flags()

	return cmd
}

func makePhaulConfig(ctx context.Context, src client.APIClient, destAddr string, id string, wdir string) (phaul.Config, error) {
	var cfg phaul.Config
	c, err := src.ContainerInspect(ctx, id)
	if err != nil {
		logrus.Debugf("Container inspect failed!")
	}
	pid := c.State.Pid
	destIP, _, err := net.SplitHostPort(strings.TrimPrefix(destAddr, "tcp://"))
	if err != nil {
		return cfg, err
	}
	dest, err := client.NewClientWithOpts(client.WithHost(destAddr))
	if err != nil {
		logrus.Debugf("Couldn't create new client!")
		return cfg, err
	}
	defer dest.Close()
	// TODO: add support for page server
	pageServer, err := dest.CreatePageServer(ctx, id)
	if err != nil {
		logrus.Debugf("Failed to create page server!")
		return cfg, err
	}
	/*dest, err := containerd.New(destAddr)
	if err != nil {
		return cfg, err
	}
	defer dest.Close()
	pageServer, err := dest.TaskService().CreatePageServer(ctx, &taskApi.CreatePageServerRequest{ContainerID: id})
	if err != nil {
		fmt.Printf("Failed to create page server\n")
		return cfg, err
	}*/
	return phaul.Config{
		Pid:  int(pid),
		Addr: destIP,
		Port: int32(pageServer.Port),
		Wdir: wdir,
	}, nil
}

func makePhaulClient(ctx context.Context, src command.Cli, destAddr string, id string, wdir string) (*phaul.Client, error) {

	dest, err := client.NewClientWithOpts(client.WithHost(destAddr))
	if err != nil {
		logrus.Debugf("Couldn't create new client!")
		return nil, err
	}

	/*dest := command.NewDockerCli(stdin, stdout, stderr, true, containerizedengine.NewClient)


	if err := dest.Initialize(opts); err != nil {
		logrus.Debugf("Failed to initialize a new client!")
		return nil, err
	}*/

	logrus.Debugf("We initialized a new docker client for phaul")

	ctrL := &ctrLocal{ctx: ctx, containerID: id, localClient: src.Client(), destClient: dest}
	remote := &ctrRemote{
		ctx:         ctx,
		containerID: id,
		client:      dest,
	}
	cfg, err := makePhaulConfig(ctx, src.Client(), destAddr, id, wdir)
	if err != nil {
		fmt.Printf("failed config\n")
		return nil, err
	}
	pc, err := phaul.MakePhaulClient(ctrL, remote, cfg)
	if err != nil {
		fmt.Printf("failed client\n")
		return nil, err
	}
	return pc, nil
}

func migrateContainer(dockerCli command.Cli, mopts *migrateOptions) error {

	ctx := context.Background()

	wdir, err := ioutil.TempDir("", "docker-criu.work")
	if err != nil {
		return err
	}
	/*defer func () {
		if err == nil {
			os.RemoveAll(wdir)
		}
	}*/

	phaulClient, err := makePhaulClient(ctx, dockerCli, mopts.destAddress, mopts.containerId, wdir)
	if err != nil {
		return err
	}
	fmt.Println(phaulClient)

	return phaulClient.Migrate()
}
