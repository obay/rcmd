//go:build windows

package main

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"golang.org/x/sys/windows/svc"
	"golang.org/x/sys/windows/svc/mgr"
)

func addServiceCommands(root *cobra.Command) {
	root.AddCommand(newInstallCmd(), newUninstallCmd(), newServiceCmd(), newStartCmd(), newStopCmd())
}

func newServiceCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "service",
		Short: "Run as a Windows service (invoked by SCM, not by humans)",
		Long: strings.TrimSpace(`
service is the entrypoint invoked by the Windows Service Control
Manager. 'rcmd-agent join' registers the service with this binary's
full path plus the 'service' argument, so SCM runs:

  rcmd-agent.exe service

You generally do not run this yourself.
`),
		RunE: func(cmd *cobra.Command, args []string) error {
			a, err := newAgent()
			if err != nil {
				return err
			}
			return svc.Run(ServiceName, &windowsService{agent: a})
		},
	}
}

func newInstallCmd() *cobra.Command {
	var binPath string
	cmd := &cobra.Command{
		Use:   "install",
		Short: "Register and start the Windows service",
		Long: strings.TrimSpace(`
install registers rcmd-agent as a Windows service that starts at boot.
Requires Administrator. Normally 'rcmd-agent join' does this for you —
this command is here for cases where you want to (re-)install the
service against an existing state file.
`),
		RunE: func(cmd *cobra.Command, args []string) error {
			path := binPath
			if path == "" {
				p, err := os.Executable()
				if err != nil {
					return err
				}
				path = p
			}
			return installService(path)
		},
	}
	cmd.Flags().StringVar(&binPath, "bin-path", "", "explicit path to rcmd-agent.exe (default: this binary)")
	return cmd
}

func newUninstallCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "uninstall",
		Short: "Stop and remove the Windows service",
		RunE: func(cmd *cobra.Command, args []string) error {
			return uninstallService()
		},
	}
}

func newStartCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "start",
		Short: "Start the Windows service",
		RunE: func(cmd *cobra.Command, args []string) error {
			return serviceControl(svc.Cmd(0), "start")
		},
	}
}

func newStopCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "stop",
		Short: "Stop the Windows service",
		RunE: func(cmd *cobra.Command, args []string) error {
			return serviceControl(svc.Stop, "stop")
		},
	}
}

// installService creates and starts the SCM service pointed at binPath.
// Idempotent-ish: if the service already exists it's an error; remove
// with uninstallService first. Exported for use from `join`.
func installService(binPath string) error {
	m, err := mgr.Connect()
	if err != nil {
		return fmt.Errorf("connect to SCM: %w", err)
	}
	defer m.Disconnect()
	if s, err := m.OpenService(ServiceName); err == nil {
		s.Close()
		return fmt.Errorf("service %q already exists; run uninstall (or leave) first", ServiceName)
	}
	s, err := m.CreateService(ServiceName, binPath, mgr.Config{
		DisplayName: "rcmd remote-exec agent",
		Description: "Polls the rcmd relay for encrypted commands.",
		StartType:   mgr.StartAutomatic,
	}, "service")
	if err != nil {
		return fmt.Errorf("create service: %w", err)
	}
	defer s.Close()
	if err := s.Start(); err != nil {
		return fmt.Errorf("start service: %w", err)
	}
	fmt.Printf("installed and started %q (bin: %s)\n", ServiceName, binPath)
	return nil
}

// serviceExists reports whether the SCM service is registered.
func serviceExists() bool {
	m, err := mgr.Connect()
	if err != nil {
		return false
	}
	defer m.Disconnect()
	s, err := m.OpenService(ServiceName)
	if err != nil {
		return false
	}
	s.Close()
	return true
}

// uninstallService stops + deletes the SCM service. No-op safe if the
// service does not exist (returns the open error).
func uninstallService() error {
	m, err := mgr.Connect()
	if err != nil {
		return fmt.Errorf("connect to SCM: %w", err)
	}
	defer m.Disconnect()
	s, err := m.OpenService(ServiceName)
	if err != nil {
		return fmt.Errorf("open service: %w", err)
	}
	defer s.Close()
	_, _ = s.Control(svc.Stop) // best-effort
	time.Sleep(500 * time.Millisecond)
	if err := s.Delete(); err != nil {
		return fmt.Errorf("delete service: %w", err)
	}
	fmt.Printf("removed %q\n", ServiceName)
	return nil
}

func serviceControl(c svc.Cmd, op string) error {
	m, err := mgr.Connect()
	if err != nil {
		return err
	}
	defer m.Disconnect()
	s, err := m.OpenService(ServiceName)
	if err != nil {
		return err
	}
	defer s.Close()
	if op == "start" {
		return s.Start()
	}
	_, err = s.Control(c)
	return err
}

type windowsService struct {
	agent *agent
}

func (ws *windowsService) Execute(args []string, r <-chan svc.ChangeRequest, status chan<- svc.Status) (bool, uint32) {
	const accepted = svc.AcceptStop | svc.AcceptShutdown
	status <- svc.Status{State: svc.StartPending}
	ctx, cancel := context.WithCancel(context.Background())
	go ws.agent.loop(ctx)
	status <- svc.Status{State: svc.Running, Accepts: accepted}
	for c := range r {
		switch c.Cmd {
		case svc.Interrogate:
			status <- c.CurrentStatus
		case svc.Stop, svc.Shutdown:
			status <- svc.Status{State: svc.StopPending}
			cancel()
			return false, 0
		}
	}
	cancel()
	return false, 0
}
