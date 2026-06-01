package pty

import (
	"fmt"
	"os"
	"os/exec"
	
	"github.com/creack/pty"
)

type PTY struct {
	Master *os.File
	Command *exec.Cmd
}

func Spawn(shell string)(*PTY, error){
	if shell == ""{
		shell = detectShell();
	}

	cmd := exec.Command(shell)
	cmd.Env = append(os.Environ(),	"TERM=xterm-256color")

	master, err := pty.Start(cmd)
	if err != nil {
		return nil, fmt.Errorf("pty.Start(%q): %w", shell, err)
	}

	return &PTY{
		Master: master,
		Command: cmd,
	}, nil
}

// Resize sets the terminal window size on the PTY.
func (p *PTY) Resize(rows, cols uint16) error {
	return pty.Setsize(p.Master, &pty.Winsize{
		Rows: rows,
		Cols: cols,
	})
}

// Close sends SIGKILL to the shell and closes the master fd.
func (p *PTY) Close() error {
	if p.Command.Process != nil {
		_ = p.Command.Process.Kill()
		_ = p.Command.Wait() // reap the zombie
	}
	return p.Master.Close()
}

// detectShell tries common shells in order of preference.
func detectShell() string {
	for _, s := range []string{"bash", "zsh", "sh"} {
		if path, err := exec.LookPath(s); err == nil {
			return path
		}
	}
	return "/bin/sh"
}