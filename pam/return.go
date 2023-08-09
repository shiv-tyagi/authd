package main

// Various signalling return messaging to PAM.

// pamSuccess signals PAM module to return PAM_SUCCESS and Quit tea.Model
type pamSuccess struct {
}

func (err pamSuccess) Error() string {
	return ""
}

// pamIgnore signals PAM module to return PAM_IGNORE and Quit tea.Model
type pamIgnore struct {
	msg string
}

func (err pamIgnore) Error() string {
	return err.msg
}

// pamAbort signals PAM module to return PAM_ABORT and Quit tea.Model
type pamAbort struct {
	msg string
}

func (err pamAbort) Error() string {
	return err.msg
}

// pamSystemError signals PAM module to return PAM_SYSTEM_ERROR and Quit tea.Model
type pamSystemError struct {
	msg string
}

func (err pamSystemError) Error() string {
	return err.msg
}

// pamAuthError signals PAM module to return PAM_AUTH_ERROR and Quit tea.Model
type pamAuthError struct {
	msg string
}

func (err pamAuthError) Error() string {
	return err.msg
}
