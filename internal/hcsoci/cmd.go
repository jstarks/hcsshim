package hcsoci

import (
	"bytes"
	"io"
	"strings"
	"sync"

	"github.com/Microsoft/hcsshim/internal/hcs"
	"github.com/Microsoft/hcsshim/internal/lcow"
	hcsschema "github.com/Microsoft/hcsshim/internal/schema2"
	specs "github.com/opencontainers/runtime-spec/specs-go"
	"golang.org/x/sys/windows"
)

type Cmd struct {
	OS      string
	InUVM   bool
	Spec    *specs.Process
	Host    *hcs.System
	Stdin   io.Reader
	Stdout  io.Writer
	Stderr  io.Writer
	Process *hcs.Process
	iowg    sync.WaitGroup
}

// escapeArgs makes a Windows-style escaped command line from a set of arguments
func escapeArgs(args []string) string {
	escapedArgs := make([]string, len(args))
	for i, a := range args {
		escapedArgs[i] = windows.EscapeArg(a)
	}
	return strings.Join(escapedArgs, " ")
}

func Command(host *hcs.System, os string, name string, arg ...string) *Cmd {
	cmd := &Cmd{
		Host: host,
		OS:   os,
		Spec: &specs.Process{
			Args: append([]string{name}, arg...),
		},
	}
	if os == "windows" {
		cmd.Spec.Cwd = "C:\\"
	} else {
		cmd.Spec.Cwd = "/"
		cmd.Spec.Env = []string{"PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin"}
	}
	return cmd
}

func (c *Cmd) Start() error {
	var x interface{}
	if c.OS == "windows" || c.InUVM {
		wpp := &hcsschema.ProcessParameters{
			CommandLine:      c.Spec.CommandLine,
			User:             c.Spec.User.Username,
			WorkingDirectory: c.Spec.Cwd,
			EmulateConsole:   c.Spec.Terminal,
			CreateStdInPipe:  c.Stdin != nil,
			CreateStdOutPipe: c.Stdout != nil,
			CreateStdErrPipe: c.Stderr != nil,
		}

		if c.Spec.CommandLine == "" {
			if c.OS == "windows" {
				wpp.CommandLine = escapeArgs(c.Spec.Args)
			} else {
				wpp.CommandArgs = c.Spec.Args
			}
		}

		environment := make(map[string]string)
		for _, v := range c.Spec.Env {
			s := strings.SplitN(v, "=", 2)
			if len(s) == 2 && len(s[1]) > 0 {
				environment[s[0]] = s[1]
			}
		}
		wpp.Environment = environment

		if c.Spec.ConsoleSize != nil {
			wpp.ConsoleSize = []int32{
				int32(c.Spec.ConsoleSize.Height),
				int32(c.Spec.ConsoleSize.Width),
			}
		}
		x = wpp

	} else {
		lpp := &lcow.ProcessParameters{
			ProcessParameters: hcsschema.ProcessParameters{
				CreateStdInPipe:  c.Stdin != nil,
				CreateStdOutPipe: c.Stdout != nil,
				CreateStdErrPipe: c.Stderr != nil,
			},
			OCIProcess: c.Spec,
		}
		x = lpp
	}
	p, err := c.Host.CreateProcess(x)
	if err != nil {
		return err
	}
	c.Process = p
	stdin, stdout, stderr := p.Stdio()
	if c.Stdin != nil {
		go func() {
			io.Copy(stdin, c.Stdin)
			p.CloseStdin()
		}()
	}

	copyOut := func(w io.Writer, r io.Reader, name string) {
		c.iowg.Add(1)
		go func() {
			io.Copy(w, r)
			c.iowg.Done()
		}()
	}

	if c.Stdout != nil {
		copyOut(c.Stdout, stdout, "stdout")
	}

	if c.Stderr != nil {
		copyOut(c.Stderr, stderr, "stderr")
	}
	return nil
}

func (c *Cmd) Wait() (int, error) {
	err := c.Process.Wait()
	if err != nil {
		return -1, err
	}
	c.iowg.Wait()
	ec, err := c.Process.ExitCode()
	c.Process.Close()
	return ec, err
}

func (c *Cmd) Run() (int, error) {
	err := c.Start()
	if err != nil {
		return -1, err
	}
	return c.Wait()
}

func (c *Cmd) Output() ([]byte, int, error) {
	var b bytes.Buffer
	c.Stdout = &b
	ec, err := c.Run()
	return b.Bytes(), ec, err
}
