package user_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/canonical/authd/internal/testutils"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
)

func TestUserDeleteCommand(t *testing.T) {
	daemonSocket := testutils.StartAuthd(t, daemonPath,
		testutils.WithGroupFile(filepath.Join("testdata", "empty.group")),
		testutils.WithPreviousDBState("multiple_users_and_groups"),
		testutils.WithCurrentUserAsRoot,
	)

	err := os.Setenv("AUTHD_SOCKET", daemonSocket)
	require.NoError(t, err, "Failed to set AUTHD_SOCKET environment variable")

	tests := map[string]struct {
		args             []string
		stdin            string
		authdUnavailable bool

		expectedExitCode int
	}{
		"Delete_user_success": {
			args:             []string{"delete", "--yes", "user1@example.com"},
			expectedExitCode: 0,
		},

		"Confirmation_prompt_accepted_with_y": {
			args:             []string{"delete", "user2@example.com"},
			stdin:            "y\n",
			expectedExitCode: 0,
		},
		"Confirmation_prompt_accepted_with_yes": {
			args:             []string{"delete", "user3@example.com"},
			stdin:            "yes\n",
			expectedExitCode: 0,
		},
		"Confirmation_prompt_accepted_case_insensitively": {
			args:             []string{"delete", "user4@example.com"},
			stdin:            "YES\n",
			expectedExitCode: 0,
		},
		"Confirmation_prompt_aborted_with_n": {
			args:             []string{"delete", "user1@example.com"},
			stdin:            "n\n",
			expectedExitCode: 0,
		},
		"Confirmation_prompt_aborted_with_empty_input": {
			args:             []string{"delete", "user1@example.com"},
			stdin:            "\n",
			expectedExitCode: 0,
		},

		"Error_when_user_does_not_exist": {
			args:             []string{"delete", "--yes", "nonexistent@example.com"},
			expectedExitCode: int(codes.NotFound),
		},
		"Error_when_authd_is_unavailable": {
			args:             []string{"delete", "--yes", "user1@example.com"},
			authdUnavailable: true,
			expectedExitCode: int(codes.Unavailable),
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			if tc.authdUnavailable {
				origValue := os.Getenv("AUTHD_SOCKET")
				err := os.Setenv("AUTHD_SOCKET", "/non-existent")
				require.NoError(t, err, "Failed to set AUTHD_SOCKET environment variable")
				t.Cleanup(func() {
					err := os.Setenv("AUTHD_SOCKET", origValue)
					require.NoError(t, err, "Failed to restore AUTHD_SOCKET environment variable")
				})
			}

			//nolint:gosec // G204 it's safe to use exec.Command with a variable here
			cmd := exec.Command(authctlPath, append([]string{"user"}, tc.args...)...)
			if tc.stdin != "" {
				cmd.Stdin = strings.NewReader(tc.stdin)
			}
			testutils.CheckCommand(t, cmd, tc.expectedExitCode)
		})
	}
}
