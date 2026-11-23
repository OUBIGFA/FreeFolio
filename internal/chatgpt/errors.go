package chatgpt

import (
	"errors"
	"fmt"
)

// Error exposes stable diagnostics without untrusted response bodies, tokens,
// callback codes or authorization URLs in logs and support transcripts.
type Error struct {
	Code                    string
	Status                  int
	RequestID, Param, Shape string
	Retryable               bool
}

func (e *Error) Error() string { return fmt.Sprintf("ChatGPT: %s", e.Code) }
func failure(code string) *Error {
	return &Error{Code: code, Retryable: code == "network_error" || code == "identity_unavailable" || code == "refresh_not_ready" || code == "revocation_unconfirmed"}
}
func IsCode(err error, code string) bool { var e *Error; return errors.As(err, &e) && e.Code == code }

func terminalRefresh(err error) bool {
	var e *Error
	if !errors.As(err, &e) {
		return false
	}
	switch e.Code {
	case "invalid_grant", "invalid_refresh_token", "token_expired", "refresh_token_expired", "refresh_token_invalidated", "refresh_token_reused":
		return true
	}
	return false
}
