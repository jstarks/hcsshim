package main

import (
	"context"
	"errors"

	"github.com/Microsoft/hcsshim/internal/hcsoci"
	"github.com/Microsoft/hcsshim/internal/logfields"
	"github.com/Microsoft/hcsshim/internal/shimdiag"
	"github.com/Microsoft/hcsshim/internal/uvm"
	"github.com/sirupsen/logrus"
)

func execInUvm(ctx context.Context, vm *uvm.UtilityVM, req *shimdiag.ExecProcessRequest) (int, error) {
	if len(req.Args) == 0 {
		return 0, errors.New("missing command")
	}
	np, err := newNpipeIO(ctx, "", "", req.Stdin, req.Stdout, req.Stderr, req.Terminal)
	if err != nil {
		return 0, err
	}
	defer np.Close()
	cmd := hcsoci.CommandWithContext(ctx, vm, vm.OS(), req.Args[0], req.Args[1:]...)
	if req.Workdir != "" {
		cmd.Spec.Cwd = req.Workdir
	}
	cmd.Spec.Terminal = req.Terminal
	cmd.Stdin = np.Stdin()
	cmd.Stdout = np.Stdout()
	cmd.Stderr = np.Stderr()
	cmd.Log = logrus.WithField(logfields.UVMID, vm.ID())
	err = cmd.Start()
	if err != nil {
		return 0, err
	}
	cmd.Process.Wait()
	np.CloseStdin()
	err = cmd.Wait()
	return cmd.ExitState.ExitCode(), err
}
