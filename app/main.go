package main

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
)

func copyFile(destination string, source string) error {
	/*
		Use defer and Close() whenever we are working on files
		(i.e., using APIs such as os.Open or os.Create) as it kills the process associated with it.
		Otherwise we would encounter "text file busy" error.
	*/
	sourceFile, err := os.Open(source)
	if err != nil {
		return err
	}
	defer sourceFile.Close()
	sourceStat, err := sourceFile.Stat()
	if err != nil {
		return err
	}
	sourcePermission := sourceStat.Mode()
	destinationFile, err := os.Create(destination)
	if err != nil {
		return err
	}
	defer destinationFile.Close()
	_, err = io.Copy(destinationFile, sourceFile)
	if err != nil {
		return err
	}
	err = destinationFile.Chmod(sourcePermission)
	if err != nil {
		return err
	}
	return nil
}

func main() {
	command := os.Args[3]
	args := os.Args[4:len(os.Args)]
	cmd := exec.Command(command, args...)

	// Create root of executable command
	tempDir, err := os.MkdirTemp("", "sandbox")
	if err != nil {
		fmt.Printf("Err: %v", err)
	}

	defer os.RemoveAll(tempDir)
	chrootCommand := filepath.Join(tempDir, filepath.Base(command))

	// Copy the binary command to the new root
	command, err = exec.LookPath(command)
	if err != nil {
		fmt.Printf("Err: %v", err)
	}

	if err := copyFile(chrootCommand, command); err != nil {
		fmt.Printf("Err: %v", err)
	}

	// enter the chroot
	if err := syscall.Chroot(tempDir); err != nil {
		fmt.Printf("Err: %v", err)
	}

	os.Mkdir("/dev", 0755)
	devNull, _ := os.Create("/dev/null")
	devNull.Close()

	chrootCommand = filepath.Join("/", filepath.Base(command))

	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	cmd.SysProcAttr = &syscall.SysProcAttr{
		Cloneflags: syscall.CLONE_NEWPID | syscall.CLONE_NEWUTS,
	}
	err := cmd.Run()

	if err != nil {
		fmt.Printf("Err: %v", err)
		if exitError, ok := err.(*exec.ExitError); ok {
			os.Exit(exitError.ExitCode())
		}
	}
	os.Exit(0)
}
