package hcsshim

import (
	"io"
	"time"
)

// For now, these should just be the structures that we convert to JSON to pass to HCS.
type ContainerConfig struct{}
type ProcessConfig struct{}

// CreateContainer creates a new container with the given configuration but does not start it.
func CreateContainer(id string, c *ContainerConfig) (Container, error)

// OpenContainer opens an existing container by ID.
func OpenContainer(id string) (Container, error)

// Container represents a created (but not necessarily running) container.
type Container interface {
	// Start synchronously starts the container.
	Start() error

	// Terminate requests a container terminate, but it may not actually be terminated until Wait() succeeds.
	Terminate() error

	// Waits synchronously waits for the container to terminate.
	Wait() error

	// WaitTimeout synchronously waits for the container to terminate or the duration to elapse. It
	// returns false if timeout occurs.
	WaitTimeout(time.Duration) (bool, error)

	// CreateProcess launches a new process within the container.
	CreateProcess(c *ProcessConfig) (Process, error)

	// OpenProcess gets an interface to an existing process within the container.
	OpenProcess(pid int) (Process, error)

	// Close cleans up any state associated with the container but does not terminate or wait for it.
	Close() error
}

// Process represents a running or exited process.
type Process interface {
	// Pid returns the process ID of the process within the container.
	Pid() int

	// Kill signals the process to terminate but does not wait for it to finish terminating.
	Kill() error

	// Wait waits for the process to exit.
	Wait() error

	// WaitTimeout waits for the process to exit or the duration to elapse. It returns
	// false if timeout occurs.
	WaitTimeout(time.Duration) (bool, error)

	// ExitCode returns the exit code of the process. The process must have
	// already terminated.
	ExitCode() (int, error)

	// ResizeConsole resizes the console of the process.
	ResizeConsole(height, width int16) error

	// Stdio returns the stdin, stdout, and stderr pipes, respectively. Closing
	// these pipes does not close the underlying pipes; it should be possible to
	// call this multiple times to get multiple interfaces.
	Stdio() (io.WriteCloser, io.ReadCloser, io.ReadCloser, error)

	// CloseStdin closes the write side of the stdin pipe so that the process is
	// notified on the read side that there is no more data in stdin.
	CloseStdin() error

	// Close cleans up any state associated with the process but does not kill
	// or wait on it.
	Close() error
}
