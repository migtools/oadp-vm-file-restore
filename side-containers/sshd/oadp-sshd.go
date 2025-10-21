package main

import (
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"syscall"
)

const (
	sftpServer = "/usr/libexec/openssh/sftp-server"
	rsyncBin   = "/bin/rsync"
	scpBin     = "/bin/scp"
	bashBin    = "/bin/bash"
)

func main() {
	// Get the original SSH command
	cmd := os.Getenv("SSH_ORIGINAL_COMMAND")

	// Log to stderr (goes to container logs)
	fmt.Fprintf(os.Stderr, "DEBUG: Command received: %s\n", cmd)

	// If no command, deny interactive shell
	if cmd == "" {
		fmt.Fprintln(os.Stderr, "ERROR - Interactive shell access is disabled.")
		fmt.Fprintln(os.Stderr, "Allowed operations: rsync (read-only), scp (download only), sftp")
		os.Exit(1)
	}

	// Handle SFTP
	if cmd == "internal-sftp" {
		execCommand(sftpServer, "-d", "/restores")
	}

	// Handle rsync (read-only mode only)
	if isRsyncCommand(cmd) {
		handleRsync(cmd)
	}

	// Handle scp (download mode only)
	if isScpCommand(cmd) {
		handleScp(cmd)
	}

	// Deny everything else
	fmt.Fprintf(os.Stderr, "ERROR - Command not allowed: %s\n", cmd)
	fmt.Fprintln(os.Stderr, "Allowed operations: rsync (read-only), scp (download), sftp")
	os.Exit(1)
}

// isRsyncCommand checks if command starts with "rsync --server"
func isRsyncCommand(cmd string) bool {
	match, _ := regexp.MatchString(`^rsync\s+--server`, cmd)
	return match
}

// isScpCommand checks if command starts with "scp"
func isScpCommand(cmd string) bool {
	return strings.HasPrefix(cmd, "scp ")
}

// handleRsync validates and executes rsync in read-only mode
func handleRsync(cmd string) {
	// Check if it's sender mode (read-only)
	isSender, _ := regexp.MatchString(`--sender`, cmd)

	if !isSender {
		fmt.Fprintln(os.Stderr, "ERROR - rsync write operations are disabled.")
		os.Exit(1)
	}

	// Replace rsync with full path
	cmdWithPath := strings.Replace(cmd, "rsync", rsyncBin, 1)
	execBash(cmdWithPath)
}

// handleScp validates and executes scp in download mode only
func handleScp(cmd string) {
	hasDownloadFlag, _ := regexp.MatchString(`-f`, cmd)
	hasUploadFlag, _ := regexp.MatchString(`-t`, cmd)

	if hasDownloadFlag {
		// Download mode - ALLOW
		cmdWithPath := strings.Replace(cmd, "scp", scpBin, 1)
		execBash(cmdWithPath)
	} else if hasUploadFlag {
		// Upload mode - DENY
		fmt.Fprintln(os.Stderr, "ERROR - scp upload is disabled (read-only)")
		os.Exit(1)
	} else {
		// Invalid scp command
		fmt.Fprintln(os.Stderr, "ERROR - Invalid scp command")
		os.Exit(1)
	}
}

// execCommand executes a command with arguments
func execCommand(path string, args ...string) {
	binary, err := exec.LookPath(path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "ERROR - Command not found: %s\n", path)
		os.Exit(1)
	}

	// Build argv with program name as argv[0]
	argv := append([]string{path}, args...)

	// Execute and replace current process
	err = syscall.Exec(binary, argv, os.Environ())
	if err != nil {
		fmt.Fprintf(os.Stderr, "ERROR - Failed to execute: %v\n", err)
		os.Exit(1)
	}
}

// execBash executes a command string through bash
func execBash(cmdStr string) {
	binary, err := exec.LookPath(bashBin)
	if err != nil {
		fmt.Fprintf(os.Stderr, "ERROR - bash not found\n")
		os.Exit(1)
	}

	// Execute: bash -c "command string"
	argv := []string{"bash", "-c", cmdStr}

	err = syscall.Exec(binary, argv, os.Environ())
	if err != nil {
		fmt.Fprintf(os.Stderr, "ERROR - Failed to execute: %v\n", err)
		os.Exit(1)
	}
}
