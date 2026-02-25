package services

import "errors"

// ErrInvalidCode is returned when VerifyCode receives unknown or mismatched email/code.
var ErrInvalidCode = errors.New("invalid or expired verification code")
