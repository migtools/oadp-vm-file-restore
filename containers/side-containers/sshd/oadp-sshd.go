package main

import (
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"syscall"
)

// Security Architecture:
//
// This ForceCommand wrapper implements defense-in-depth security for SSH access
// to VM backup files. Security is enforced at multiple layers:
//
// Container-Level Controls (Primary Defense):
//   - Chroot jail: User confined to /restores directory
//   - Read-only filesystem: Data volumes mounted read-only
//   - Limited capabilities: DAC_READ_SEARCH only
//   - Short-lived containers: Exist only during restore operations
//   - Kubernetes resource limits: CPU/memory quotas prevent DoS
//   - Network isolation: OADP namespace isolation
//
// Application-Level Controls (Secondary Defense):
//   - Command whitelisting: Only rsync, scp, and sftp allowed
//   - Interactive shell blocking: No shell access permitted
//   - Operation mode enforcement: rsync sender mode only, scp download only
//   - Argument parsing: Direct exec without shell interpretation
//   - Flag validation: Dangerous flags blocked
//   - Binary path validation: Full paths prevent PATH manipulation
//   - Command length limits: Prevent parser DoS attacks
//
// This multi-layer approach ensures security even if individual controls fail.

const (
	sftpServer = "/usr/libexec/openssh/sftp-server"
	rsyncBin   = "/bin/rsync"
	scpBin     = "/bin/scp"

	// Maximum command length (10KB) to prevent parser DoS attacks
	maxCommandLength = 10240
)

// Dangerous rsync/scp flags that could enable command execution or write operations
var dangerousFlags = []string{
	"--rsh=",        // Custom remote shell (command execution)
	"--config=",     // Load custom rsync config
	"--rsync-path=", // Custom rsync path (command execution)
	"--daemon",      // Daemon mode
	"--delete",      // Delete operations (any variant)
}

func main() {
	// Get the original SSH command
	cmd := os.Getenv("SSH_ORIGINAL_COMMAND")

	// Log to stderr (goes to container logs)
	fmt.Fprintf(os.Stderr, "DEBUG: Command received: %s\n", cmd)

	// Security check: Enforce command length limit
	if len(cmd) > maxCommandLength {
		fmt.Fprintf(os.Stderr, "ERROR - Command exceeds maximum length (%d bytes)\n", maxCommandLength)
		os.Exit(1)
	}

	// Security check: Deny interactive shell (empty command)
	if cmd == "" {
		fmt.Fprintln(os.Stderr, "ERROR - Interactive shell access is disabled.")
		fmt.Fprintln(os.Stderr, "Allowed operations: rsync (read-only), scp (download only), sftp")
		os.Exit(1)
	}

	// Security check: Deny whitespace-only commands (shell access attempts)
	if strings.TrimSpace(cmd) == "" {
		fmt.Fprintln(os.Stderr, "ERROR - Invalid command (whitespace only)")
		fmt.Fprintln(os.Stderr, "Allowed operations: rsync (read-only), scp (download only), sftp")
		os.Exit(1)
	}

	// Handle SFTP - exact string match prevents argument injection
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
	// Parse command into arguments (without shell interpretation)
	args, err := parseCommand(cmd)
	if err != nil {
		fmt.Fprintf(os.Stderr, "ERROR - Invalid rsync command: %v\n", err)
		os.Exit(1)
	}

	// Remove "rsync" from args (it's the command name)
	if len(args) > 0 && args[0] == "rsync" {
		args = args[1:]
	}

	// Security check: Verify sender mode (read-only)
	if !containsFlag(args, "--sender") {
		fmt.Fprintln(os.Stderr, "ERROR - rsync write operations are disabled (sender mode required)")
		os.Exit(1)
	}

	// Security check: Block dangerous flags
	if containsAnyDangerousFlag(args) {
		fmt.Fprintln(os.Stderr, "ERROR - Dangerous rsync flags not allowed (--rsh, --config, --rsync-path, --daemon)")
		os.Exit(1)
	}

	// Execute rsync with parsed arguments (no shell interpretation)
	execCommand(rsyncBin, args...)
}

// handleScp validates and executes scp in download mode only
func handleScp(cmd string) {
	// Parse command into arguments (without shell interpretation)
	args, err := parseCommand(cmd)
	if err != nil {
		fmt.Fprintf(os.Stderr, "ERROR - Invalid scp command: %v\n", err)
		os.Exit(1)
	}

	// Remove "scp" from args (it's the command name)
	if len(args) > 0 && args[0] == "scp" {
		args = args[1:]
	}

	// Security check: Verify download mode (-f flag required)
	hasDownloadFlag := containsFlag(args, "-f")
	hasUploadFlag := containsFlag(args, "-t")

	if hasDownloadFlag && !hasUploadFlag {
		// Download mode - ALLOW
		execCommand(scpBin, args...)
	} else if hasUploadFlag {
		// Upload mode - DENY
		fmt.Fprintln(os.Stderr, "ERROR - scp upload is disabled (read-only mode)")
		os.Exit(1)
	} else {
		// Invalid scp command (missing -f or -t flag)
		fmt.Fprintln(os.Stderr, "ERROR - Invalid scp command (missing mode flag)")
		os.Exit(1)
	}
}

// execCommand executes a command with arguments using syscall.Exec
// This replaces the current process and does NOT use shell interpretation
func execCommand(path string, args ...string) {
	// Validate binary exists
	binary, err := exec.LookPath(path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "ERROR - Command not found: %s\n", path)
		os.Exit(1)
	}

	// Build argv with program name as argv[0]
	argv := append([]string{path}, args...)

	// Security: Filter environment variables to prevent LD_PRELOAD attacks
	// Note: In practice, read-only filesystem already prevents this attack,
	// but we filter anyway for defense-in-depth
	safeEnv := filterEnvironment(os.Environ())

	// Execute and replace current process (no shell, direct exec)
	err = syscall.Exec(binary, argv, safeEnv)
	if err != nil {
		fmt.Fprintf(os.Stderr, "ERROR - Failed to execute: %v\n", err)
		os.Exit(1)
	}
}

// parseCommand safely parses a shell command string into arguments
// This function handles quoted arguments and escaping without using bash
func parseCommand(cmd string) ([]string, error) {
	var args []string
	var current strings.Builder
	inQuote := false
	quoteChar := rune(0)
	escaped := false

	for _, r := range cmd {
		switch {
		case escaped:
			// Previous character was backslash, add this char literally
			current.WriteRune(r)
			escaped = false

		case r == '\\' && inQuote && quoteChar == '"':
			// Backslash in double quotes (escaping)
			escaped = true

		case r == '"' || r == '\'':
			// Quote character
			if !inQuote {
				// Start quoted section
				inQuote = true
				quoteChar = r
			} else if r == quoteChar {
				// End quoted section
				inQuote = false
				quoteChar = 0
			} else {
				// Different quote inside quoted section
				current.WriteRune(r)
			}

		case r == ' ' || r == '\t':
			// Whitespace
			if inQuote {
				// Inside quotes, add literally
				current.WriteRune(r)
			} else if current.Len() > 0 {
				// Outside quotes, this is argument separator
				args = append(args, current.String())
				current.Reset()
			}

		default:
			// Regular character
			current.WriteRune(r)
		}
	}

	// Add final argument
	if current.Len() > 0 {
		args = append(args, current.String())
	}

	// Security check: Ensure quotes are balanced
	if inQuote {
		return nil, fmt.Errorf("unbalanced quotes in command")
	}

	return args, nil
}

// containsFlag checks if a specific flag exists in the argument list
func containsFlag(args []string, flag string) bool {
	for _, arg := range args {
		if arg == flag {
			return true
		}
	}
	return false
}

// containsAnyDangerousFlag checks if any dangerous flag exists in the argument list
// Dangerous flags are those that could enable command execution or write operations
func containsAnyDangerousFlag(args []string) bool {
	for _, arg := range args {
		for _, dangerous := range dangerousFlags {
			if arg == dangerous || strings.HasPrefix(arg, dangerous) {
				return true
			}
		}
	}
	return false
}

// filterEnvironment removes potentially dangerous environment variables
// This prevents attacks like LD_PRELOAD, even though read-only filesystem
// already prevents such attacks (defense-in-depth)
func filterEnvironment(env []string) []string {
	// Dangerous environment variables that could affect command execution
	dangerous := map[string]bool{
		"LD_PRELOAD":      true,
		"LD_LIBRARY_PATH": true,
		"LD_AUDIT":        true,
		"LD_BIND_NOW":     true,
		"BASH_ENV":        true,
		"ENV":             true,
		"SHELLOPTS":       true,
	}

	var safe []string
	for _, e := range env {
		// Split on first '=' to get variable name
		parts := strings.SplitN(e, "=", 2)
		if len(parts) == 0 {
			continue
		}

		varName := parts[0]
		if !dangerous[varName] {
			safe = append(safe, e)
		}
	}

	return safe
}
