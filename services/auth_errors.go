package services

import "errors"

// ErrInvalidCode is returned when VerifyCode receives unknown or mismatched email/code.
var ErrInvalidCode = errors.New("invalid or expired verification code")

// ErrRateLimited is returned when too many code requests for the same email.
var ErrRateLimited = errors.New("too many code requests; try again later")

// ErrInvitationExpired is returned when the invite token has passed its ExpiresAt.
var ErrInvitationExpired = errors.New("invitation has expired")
