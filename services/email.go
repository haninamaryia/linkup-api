package services

// EmailSender sends transactional emails. Implement with Resend, SendGrid, etc.
type EmailSender interface {
	SendInvite(to, eventTitle, organizerName, inviteLink string) error
	SendReminder(to, eventTitle, inviteLink string) error
	SendConfirmationToOrganizer(to, eventTitle string) error
}

// StubEmailSender logs emails to stdout instead of sending.
type StubEmailSender struct{}

func NewStubEmailSender() *StubEmailSender {
	return &StubEmailSender{}
}

func (s *StubEmailSender) SendInvite(to, eventTitle, organizerName, inviteLink string) error {
	// TODO: integrate real email provider (Resend, SendGrid, etc.)
	return nil
}

func (s *StubEmailSender) SendReminder(to, eventTitle, inviteLink string) error {
	return nil
}

func (s *StubEmailSender) SendConfirmationToOrganizer(to, eventTitle string) error {
	return nil
}
